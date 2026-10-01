package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// Task link types as stored. "blocks" is directional (from blocks to);
// "duplicates" is directional (from duplicates to); "relates" is symmetric and
// stored with from_task_id < to_task_id.
const (
	TaskLinkBlocks     = "blocks"
	TaskLinkRelates    = "relates"
	TaskLinkDuplicates = "duplicates"
)

// ErrTaskLinkExists is returned when the same link is added twice.
var ErrTaskLinkExists = errors.New("task link already exists")

// TaskLink is one stored link row.
type TaskLink struct {
	ID         int
	FromTaskID int
	ToTaskID   int
	Type       string
	CreatedBy  int
	CreatedAt  time.Time
}

// TaskWatcher is a user watching a task.
type TaskWatcher struct {
	UserID int
	Name   string
}

// CreateWatchAndLinkTables creates task_watchers, project_watchers and task_links.
func CreateWatchAndLinkTables() error {
	pool, err := OpenDatabase()
	if err != nil {
		return fmt.Errorf("failed to open database: %v", err)
	}
	defer CloseDatabase(pool)
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS task_watchers (
			task_id INTEGER NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
			user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			PRIMARY KEY (task_id, user_id)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_task_watchers_user ON task_watchers(user_id)`,
		`CREATE TABLE IF NOT EXISTS project_watchers (
			project_id INTEGER NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
			user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			PRIMARY KEY (project_id, user_id)
		)`,
		`CREATE TABLE IF NOT EXISTS task_links (
			id SERIAL PRIMARY KEY,
			from_task_id INTEGER NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
			to_task_id INTEGER NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
			link_type VARCHAR(16) NOT NULL CHECK (link_type IN ('blocks', 'relates', 'duplicates')),
			created_by INTEGER,
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			CHECK (from_task_id <> to_task_id),
			UNIQUE (from_task_id, to_task_id, link_type)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_task_links_to ON task_links(to_task_id, link_type)`,
	}
	for _, s := range stmts {
		if _, err := pool.Exec(context.Background(), s); err != nil {
			return fmt.Errorf("watch/link migration: %v", err)
		}
	}
	return nil
}

// --- watchers ---

// AddTaskWatcher subscribes userID to taskID. Reports whether a row was added.
func AddTaskWatcher(taskID, userID int) (bool, error) {
	return execAffected(`INSERT INTO task_watchers (task_id, user_id) VALUES ($1, $2) ON CONFLICT DO NOTHING`, taskID, userID)
}

// RemoveTaskWatcher unsubscribes userID from taskID.
func RemoveTaskWatcher(taskID, userID int) (bool, error) {
	return execAffected(`DELETE FROM task_watchers WHERE task_id = $1 AND user_id = $2`, taskID, userID)
}

// AddProjectWatcher subscribes userID to every task in projectID.
func AddProjectWatcher(projectID, userID int) (bool, error) {
	return execAffected(`INSERT INTO project_watchers (project_id, user_id) VALUES ($1, $2) ON CONFLICT DO NOTHING`, projectID, userID)
}

// RemoveProjectWatcher unsubscribes userID from projectID.
func RemoveProjectWatcher(projectID, userID int) (bool, error) {
	return execAffected(`DELETE FROM project_watchers WHERE project_id = $1 AND user_id = $2`, projectID, userID)
}

// IsTaskWatcher reports direct task watching.
func IsTaskWatcher(taskID, userID int) (bool, error) {
	return queryBool(`SELECT EXISTS (SELECT 1 FROM task_watchers WHERE task_id = $1 AND user_id = $2)`, taskID, userID)
}

// IsProjectWatcher reports project-level watching.
func IsProjectWatcher(projectID, userID int) (bool, error) {
	if projectID <= 0 {
		return false, nil
	}
	return queryBool(`SELECT EXISTS (SELECT 1 FROM project_watchers WHERE project_id = $1 AND user_id = $2)`, projectID, userID)
}

// ListTaskWatchers returns users directly watching a task, ordered by name.
func ListTaskWatchers(taskID int) ([]TaskWatcher, error) {
	pool, err := OpenDatabase()
	if err != nil {
		return nil, err
	}
	defer CloseDatabase(pool)
	rows, err := pool.Query(context.Background(), `
		SELECT w.user_id, COALESCE(NULLIF(u.user_name, ''), u.email, '')
		FROM task_watchers w JOIN users u ON u.id = w.user_id
		WHERE w.task_id = $1 ORDER BY 2, 1`, taskID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []TaskWatcher{}
	for rows.Next() {
		var w TaskWatcher
		if err := rows.Scan(&w.UserID, &w.Name); err != nil {
			return nil, err
		}
		out = append(out, w)
	}
	return out, rows.Err()
}

// TaskWatcherRecipients returns everyone watching the task directly or via its project.
func TaskWatcherRecipients(taskID int) ([]int, error) {
	pool, err := OpenDatabase()
	if err != nil {
		return nil, err
	}
	defer CloseDatabase(pool)
	rows, err := pool.Query(context.Background(), `
		SELECT user_id FROM task_watchers WHERE task_id = $1
		UNION
		SELECT pw.user_id FROM project_watchers pw JOIN tasks t ON t.project_id = pw.project_id WHERE t.id = $1`, taskID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []int{}
	for rows.Next() {
		var id int
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

// --- links ---

// InsertTaskLink stores a normalized link and returns its id.
func InsertTaskLink(fromID, toID int, linkType string, createdBy int) (int, error) {
	pool, err := OpenDatabase()
	if err != nil {
		return 0, err
	}
	defer CloseDatabase(pool)
	var by interface{}
	if createdBy > 0 {
		by = createdBy
	}
	var id int
	err = pool.QueryRow(context.Background(),
		`INSERT INTO task_links (from_task_id, to_task_id, link_type, created_by) VALUES ($1, $2, $3, $4) RETURNING id`,
		fromID, toID, linkType, by).Scan(&id)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return 0, ErrTaskLinkExists
		}
		return 0, err
	}
	return id, nil
}

// GetTaskLink loads one link, or nil.
func GetTaskLink(linkID int) (*TaskLink, error) {
	pool, err := OpenDatabase()
	if err != nil {
		return nil, err
	}
	defer CloseDatabase(pool)
	var l TaskLink
	var by sql.NullInt64
	err = pool.QueryRow(context.Background(),
		`SELECT id, from_task_id, to_task_id, link_type, created_by, created_at FROM task_links WHERE id = $1`, linkID).
		Scan(&l.ID, &l.FromTaskID, &l.ToTaskID, &l.Type, &by, &l.CreatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) || errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	l.CreatedBy = int(by.Int64)
	return &l, nil
}

// DeleteTaskLink removes a link by id.
func DeleteTaskLink(linkID int) (bool, error) {
	return execAffected(`DELETE FROM task_links WHERE id = $1`, linkID)
}

// ListTaskLinks returns every link touching taskID.
func ListTaskLinks(taskID int) ([]TaskLink, error) {
	pool, err := OpenDatabase()
	if err != nil {
		return nil, err
	}
	defer CloseDatabase(pool)
	rows, err := pool.Query(context.Background(), `
		SELECT id, from_task_id, to_task_id, link_type, COALESCE(created_by, 0), created_at
		FROM task_links WHERE from_task_id = $1 OR to_task_id = $1 ORDER BY id`, taskID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []TaskLink{}
	for rows.Next() {
		var l TaskLink
		if err := rows.Scan(&l.ID, &l.FromTaskID, &l.ToTaskID, &l.Type, &l.CreatedBy, &l.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	return out, rows.Err()
}

// BlockingPathExists reports whether startID (transitively) blocks targetID.
func BlockingPathExists(startID, targetID int) (bool, error) {
	return queryBool(`
		WITH RECURSIVE reach(id) AS (
			SELECT to_task_id FROM task_links WHERE from_task_id = $1 AND link_type = 'blocks'
			UNION
			SELECT l.to_task_id FROM task_links l JOIN reach r ON l.from_task_id = r.id WHERE l.link_type = 'blocks'
		)
		SELECT EXISTS (SELECT 1 FROM reach WHERE id = $2)`, startID, targetID)
}

// OpenBlockerCounts returns, per task id, how many incomplete tasks block it.
func OpenBlockerCounts(taskIDs []int) (map[int]int, error) {
	out := map[int]int{}
	if len(taskIDs) == 0 {
		return out, nil
	}
	pool, err := OpenDatabase()
	if err != nil {
		return nil, err
	}
	defer CloseDatabase(pool)
	rows, err := pool.Query(context.Background(), `
		SELECT l.to_task_id, COUNT(*)
		FROM task_links l JOIN tasks b ON b.id = l.from_task_id
		WHERE l.link_type = 'blocks' AND l.to_task_id = ANY($1) AND NOT COALESCE(b.completed, false)
		GROUP BY l.to_task_id`, taskIDs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id, n int
		if err := rows.Scan(&id, &n); err != nil {
			return nil, err
		}
		out[id] = n
	}
	return out, rows.Err()
}

// ListTasksBlockedBy returns ids of tasks that blockerID blocks.
func ListTasksBlockedBy(blockerID int) ([]int, error) {
	pool, err := OpenDatabase()
	if err != nil {
		return nil, err
	}
	defer CloseDatabase(pool)
	rows, err := pool.Query(context.Background(),
		`SELECT to_task_id FROM task_links WHERE from_task_id = $1 AND link_type = 'blocks' ORDER BY to_task_id`, blockerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []int{}
	for rows.Next() {
		var id int
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

func execAffected(q string, args ...interface{}) (bool, error) {
	pool, err := OpenDatabase()
	if err != nil {
		return false, err
	}
	defer CloseDatabase(pool)
	tag, err := pool.Exec(context.Background(), q, args...)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() > 0, nil
}

func queryBool(q string, args ...interface{}) (bool, error) {
	pool, err := OpenDatabase()
	if err != nil {
		return false, err
	}
	defer CloseDatabase(pool)
	var b bool
	err = pool.QueryRow(context.Background(), q, args...).Scan(&b)
	return b, err
}

// CopyTaskWatchers subscribes fromID's watchers to toID (e.g. the next occurrence of a recurring task).
func CopyTaskWatchers(fromID, toID int) error {
	_, err := execAffected(`INSERT INTO task_watchers (task_id, user_id)
		SELECT $2, user_id FROM task_watchers WHERE task_id = $1 ON CONFLICT DO NOTHING`, fromID, toID)
	return err
}
