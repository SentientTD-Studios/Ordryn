package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

const (
	// DefaultTrashRetentionDays is how long deleted tasks stay restorable.
	DefaultTrashRetentionDays = 30
	maxTrashRetentionDays     = 365
)

// ClampTrashRetentionDays keeps the retention window between 1 and 365 days.
func ClampTrashRetentionDays(days int) int {
	if days <= 0 {
		return DefaultTrashRetentionDays
	}
	if days > maxTrashRetentionDays {
		return maxTrashRetentionDays
	}
	return days
}

// MigrateTasksAddTrash adds soft-delete columns. A trashed task keeps every
// related row (comments, links, fields, history) so a restore is lossless.
// delete_batch groups a task with the subtasks trashed alongside it.
func MigrateTasksAddTrash() error {
	pool, err := OpenDatabase()
	if err != nil {
		return err
	}
	defer CloseDatabase(pool)

	stmts := []string{
		`ALTER TABLE tasks ADD COLUMN IF NOT EXISTS deleted_at TIMESTAMPTZ`,
		`ALTER TABLE tasks ADD COLUMN IF NOT EXISTS deleted_by INTEGER REFERENCES users(id) ON DELETE SET NULL`,
		`ALTER TABLE tasks ADD COLUMN IF NOT EXISTS delete_batch TEXT`,
		`CREATE INDEX IF NOT EXISTS idx_tasks_trashed ON tasks (deleted_at) WHERE deleted_at IS NOT NULL`,
		`ALTER TABLE site_settings ADD COLUMN IF NOT EXISTS trash_retention_days INTEGER DEFAULT 30`,
	}
	for _, s := range stmts {
		if _, err := pool.Exec(context.Background(), s); err != nil {
			return fmt.Errorf("trash migration: %v", err)
		}
	}
	return nil
}

// TaskNotTrashedSQL is "<alias>.deleted_at IS NULL" (unaliased: tasks.deleted_at).
func TaskNotTrashedSQL(alias string) string {
	if alias == "" {
		alias = "tasks"
	}
	return alias + ".deleted_at IS NULL"
}

// IsTaskTrashed reports whether a task exists and is in the trash.
func IsTaskTrashed(taskID int) (bool, error) {
	pool, err := OpenDatabase()
	if err != nil {
		return false, err
	}
	defer CloseDatabase(pool)
	var deleted *time.Time
	err = pool.QueryRow(context.Background(), `SELECT deleted_at FROM tasks WHERE id = $1`, taskID).Scan(&deleted)
	if errors.Is(err, pgx.ErrNoRows) || errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	return deleted != nil, err
}

// TrashTasks moves tasks and their live subtasks to the trash as one batch and
// returns every task id it trashed. Already-trashed tasks are skipped.
func TrashTasks(ids []int, userID int) ([]int, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	pool, err := OpenDatabase()
	if err != nil {
		return nil, err
	}
	defer CloseDatabase(pool)

	batch := uuid.NewString()
	rows, err := pool.Query(context.Background(), `
		WITH RECURSIVE tree AS (
			SELECT id FROM tasks WHERE id = ANY($1) AND deleted_at IS NULL
			UNION
			SELECT c.id FROM tasks c JOIN tree ON c.parent_id = tree.id WHERE c.deleted_at IS NULL
		)
		UPDATE tasks SET deleted_at = NOW(), deleted_by = $2, delete_batch = $3
		WHERE id IN (SELECT id FROM tree)
		RETURNING id`, ids, nullableUserID(userID), batch)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []int
	for rows.Next() {
		var id int
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

func nullableUserID(id int) any {
	if id <= 0 {
		return nil
	}
	return id
}

// ErrTrashedParent means a subtask cannot come back while its parent is trashed.
var ErrTrashedParent = errors.New("parent task is in the trash")

// RestoreTrashedTask restores a trashed task and the subtasks trashed in the
// same batch, returning the restored ids. A subtask whose parent is still in
// the trash cannot be restored on its own.
func RestoreTrashedTask(taskID int) ([]int, error) {
	pool, err := OpenDatabase()
	if err != nil {
		return nil, err
	}
	defer CloseDatabase(pool)
	ctx := context.Background()

	var batch sql.NullString
	var parentTrashed bool
	err = pool.QueryRow(ctx, `
		SELECT t.delete_batch, COALESCE(p.deleted_at IS NOT NULL, FALSE)
		FROM tasks t LEFT JOIN tasks p ON p.id = t.parent_id
		WHERE t.id = $1 AND t.deleted_at IS NOT NULL`, taskID).Scan(&batch, &parentTrashed)
	if errors.Is(err, pgx.ErrNoRows) || errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if parentTrashed {
		return nil, ErrTrashedParent
	}

	rows, err := pool.Query(ctx, `
		WITH RECURSIVE tree AS (
			SELECT id FROM tasks WHERE id = $1
			UNION
			SELECT c.id FROM tasks c JOIN tree ON c.parent_id = tree.id
			WHERE c.deleted_at IS NOT NULL AND c.delete_batch IS NOT DISTINCT FROM $2
		)
		UPDATE tasks SET deleted_at = NULL, deleted_by = NULL, delete_batch = NULL,
			date_modified = NOW() AT TIME ZONE 'UTC'
		WHERE id IN (SELECT id FROM tree)
		RETURNING id`, taskID, batch)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []int
	for rows.Next() {
		var id int
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

// PurgeTrashedTask permanently deletes one trashed task (subtasks cascade).
// It reports false when the task is not in the trash.
func PurgeTrashedTask(taskID int) (bool, error) {
	pool, err := OpenDatabase()
	if err != nil {
		return false, err
	}
	defer CloseDatabase(pool)
	tag, err := pool.Exec(context.Background(),
		`DELETE FROM tasks WHERE id = $1 AND deleted_at IS NOT NULL`, taskID)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() > 0, nil
}

// TrashedTask is one entry in a trash listing: a task trashed on its own, or
// the top of a batch (its trashed subtasks are counted, not listed).
type TrashedTask struct {
	ID           int
	Title        string
	OwnerID      int
	ProjectID    int
	ProjectName  string
	ParentID     int
	ParentTitle  string
	DeletedAt    time.Time
	DeletedByID  int
	DeletedBy    string
	SubtaskCount int
}

// ListTrashCandidates returns trash entries the user could see if the task
// were live; callers still check the delete permission per project.
func ListTrashCandidates(userID, limit int) ([]TrashedTask, error) {
	pool, err := OpenDatabase()
	if err != nil {
		return nil, err
	}
	defer CloseDatabase(pool)

	rows, err := pool.Query(context.Background(), `
		SELECT t.id, t.title, COALESCE(t.user_id, 0), COALESCE(t.project_id, 0), COALESCE(p.name, ''),
			COALESCE(t.parent_id, 0), COALESCE(par.title, ''), t.deleted_at,
			COALESCE(t.deleted_by, 0), COALESCE(NULLIF(u.user_name, ''), u.email, ''),
			(SELECT COUNT(*) FROM tasks c WHERE c.parent_id = t.id AND c.deleted_at IS NOT NULL
				AND c.delete_batch IS NOT DISTINCT FROM t.delete_batch)
		FROM tasks t
		LEFT JOIN projects p ON p.id = t.project_id
		LEFT JOIN tasks par ON par.id = t.parent_id
		LEFT JOIN users u ON u.id = t.deleted_by
		WHERE t.deleted_at IS NOT NULL
		  AND (par.id IS NULL OR par.deleted_at IS NULL OR par.delete_batch IS DISTINCT FROM t.delete_batch)
		  AND (t.user_id = $1 OR (t.project_id IS NOT NULL AND (
			EXISTS (SELECT 1 FROM project_members pm WHERE pm.project_id = t.project_id AND pm.user_id = $1)
			OR p.user_id = $1)))
		ORDER BY t.deleted_at DESC, t.id DESC
		LIMIT $2`, userID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []TrashedTask
	for rows.Next() {
		var tt TrashedTask
		if err := rows.Scan(&tt.ID, &tt.Title, &tt.OwnerID, &tt.ProjectID, &tt.ProjectName,
			&tt.ParentID, &tt.ParentTitle, &tt.DeletedAt, &tt.DeletedByID, &tt.DeletedBy, &tt.SubtaskCount); err != nil {
			return nil, err
		}
		out = append(out, tt)
	}
	return out, rows.Err()
}

// PurgeExpiredTrash permanently deletes tasks trashed more than days ago.
func PurgeExpiredTrash(days int) (int64, error) {
	pool, err := OpenDatabase()
	if err != nil {
		return 0, err
	}
	defer CloseDatabase(pool)
	tag, err := pool.Exec(context.Background(),
		`DELETE FROM tasks WHERE deleted_at IS NOT NULL AND deleted_at < NOW() - make_interval(days => $1)`,
		ClampTrashRetentionDays(days))
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

// StartTrashPurgeWorker empties expired trash hourly.
func StartTrashPurgeWorker() {
	go func() {
		ticker := time.NewTicker(time.Hour)
		defer ticker.Stop()
		for {
			days := DefaultTrashRetentionDays
			if s, err := GetSiteSettings(); err == nil && s != nil {
				days = s.TrashRetentionDays
			}
			if n, err := PurgeExpiredTrash(days); err != nil {
				log.Printf("trash purge: %v", err)
			} else if n > 0 {
				log.Printf("trash purge: deleted %d task(s) older than %d day(s)", n, ClampTrashRetentionDays(days))
			}
			<-ticker.C
		}
	}()
}
