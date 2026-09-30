package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// TaskRecurrence is the repeat rule attached to the *active* task of a
// recurring series. When that task is completed the rule is claimed (deleted),
// the next task is created, and the rule is re-attached to the new task. Past
// occurrences keep tasks.recurrence_series_id / recurrence_prev_id so the
// chain of completed instances can be listed.
type TaskRecurrence struct {
	TaskID       int
	Frequency    string
	Interval     int
	WeekdaysMask int
	MonthDay     int
	Basis        string
	EndsOn       string // YYYY-MM-DD or ""
	EndAfter     int    // total occurrences; 0 = unlimited
	Occurrence   int    // 1-based index of TaskID within the series
	SeriesID     int
	CreatedBy    int
	UpdatedAt    time.Time
}

// RecurrenceSeriesItem is one task in a recurring series (history listing).
type RecurrenceSeriesItem struct {
	TaskID      int
	Title       string
	DueDate     string
	Completed   bool
	CompletedAt *time.Time
	CreatedAt   time.Time
}

// RecurrenceSuccessor describes the task spawned from a completed occurrence.
type RecurrenceSuccessor struct {
	TaskID    int
	Completed bool
	// ChildProgress is true when any subtask of the successor is already completed.
	ChildProgress bool
	CommentCount  int
	// Recent is true when the successor was created within the undo window.
	Recent bool
}

// CreateTaskRecurrenceTables creates task_recurrence and the series columns on tasks.
func CreateTaskRecurrenceTables() error {
	pool, err := OpenDatabase()
	if err != nil {
		return fmt.Errorf("failed to open database: %v", err)
	}
	defer CloseDatabase(pool)
	ctx := context.Background()

	stmts := []string{
		`CREATE TABLE IF NOT EXISTS task_recurrence (
			task_id INTEGER PRIMARY KEY REFERENCES tasks(id) ON DELETE CASCADE,
			frequency VARCHAR(16) NOT NULL,
			interval_count INTEGER NOT NULL DEFAULT 1,
			weekdays_mask INTEGER NOT NULL DEFAULT 0,
			month_day INTEGER NOT NULL DEFAULT 0,
			basis VARCHAR(16) NOT NULL DEFAULT 'due',
			ends_on DATE,
			end_after INTEGER NOT NULL DEFAULT 0,
			occurrence INTEGER NOT NULL DEFAULT 1,
			series_id INTEGER NOT NULL,
			created_by INTEGER,
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)`,
		`ALTER TABLE tasks ADD COLUMN IF NOT EXISTS recurrence_series_id INTEGER`,
		`ALTER TABLE tasks ADD COLUMN IF NOT EXISTS recurrence_prev_id INTEGER`,
		`CREATE INDEX IF NOT EXISTS idx_tasks_recurrence_series ON tasks(recurrence_series_id) WHERE recurrence_series_id IS NOT NULL`,
		`CREATE INDEX IF NOT EXISTS idx_tasks_recurrence_prev ON tasks(recurrence_prev_id) WHERE recurrence_prev_id IS NOT NULL`,
	}
	for _, stmt := range stmts {
		if _, err := pool.Exec(ctx, stmt); err != nil {
			return fmt.Errorf("task recurrence migration: %v", err)
		}
	}

	var exists bool
	if err := pool.QueryRow(ctx,
		`SELECT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'fk_tasks_recurrence_prev')`).Scan(&exists); err != nil {
		return fmt.Errorf("check recurrence_prev fk: %v", err)
	}
	if !exists {
		if _, err := pool.Exec(ctx,
			`ALTER TABLE tasks ADD CONSTRAINT fk_tasks_recurrence_prev
			 FOREIGN KEY (recurrence_prev_id) REFERENCES tasks (id) ON DELETE SET NULL`); err != nil {
			return fmt.Errorf("add recurrence_prev fk: %v", err)
		}
	}
	return nil
}

const recurrenceColumns = `task_id, frequency, interval_count, weekdays_mask, month_day, basis,
	COALESCE(CAST(ends_on AS TEXT), ''), end_after, occurrence, series_id, COALESCE(created_by, 0), updated_at`

func scanTaskRecurrence(row interface{ Scan(dest ...any) error }) (*TaskRecurrence, error) {
	var r TaskRecurrence
	if err := row.Scan(&r.TaskID, &r.Frequency, &r.Interval, &r.WeekdaysMask, &r.MonthDay, &r.Basis,
		&r.EndsOn, &r.EndAfter, &r.Occurrence, &r.SeriesID, &r.CreatedBy, &r.UpdatedAt); err != nil {
		return nil, err
	}
	return &r, nil
}

// GetTaskRecurrence returns the rule attached to a task, or nil when it does not recur.
func GetTaskRecurrence(taskID int) (*TaskRecurrence, error) {
	pool, err := OpenDatabase()
	if err != nil {
		return nil, err
	}
	defer CloseDatabase(pool)
	r, err := scanTaskRecurrence(pool.QueryRow(context.Background(),
		`SELECT `+recurrenceColumns+` FROM task_recurrence WHERE task_id = $1`, taskID))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) || errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return r, nil
}

// GetTaskRecurrencesForTasks batch-loads rules keyed by task id.
func GetTaskRecurrencesForTasks(taskIDs []int) (map[int]TaskRecurrence, error) {
	out := make(map[int]TaskRecurrence)
	if len(taskIDs) == 0 {
		return out, nil
	}
	pool, err := OpenDatabase()
	if err != nil {
		return nil, err
	}
	defer CloseDatabase(pool)
	rows, err := pool.Query(context.Background(),
		`SELECT `+recurrenceColumns+` FROM task_recurrence WHERE task_id = ANY($1)`, taskIDs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		r, err := scanTaskRecurrence(rows)
		if err != nil {
			return nil, err
		}
		out[r.TaskID] = *r
	}
	return out, rows.Err()
}

// SaveTaskRecurrence inserts or replaces the rule for r.TaskID and stamps the
// task with its series id.
func SaveTaskRecurrence(r TaskRecurrence) error {
	pool, err := OpenDatabase()
	if err != nil {
		return err
	}
	defer CloseDatabase(pool)
	ctx := context.Background()

	var endsOn interface{}
	if r.EndsOn != "" {
		endsOn = r.EndsOn
	}
	var createdBy interface{}
	if r.CreatedBy > 0 {
		createdBy = r.CreatedBy
	}
	if r.SeriesID <= 0 {
		r.SeriesID = r.TaskID
	}
	if r.Occurrence <= 0 {
		r.Occurrence = 1
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, `
		INSERT INTO task_recurrence (task_id, frequency, interval_count, weekdays_mask, month_day, basis,
			ends_on, end_after, occurrence, series_id, created_by, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, NOW())
		ON CONFLICT (task_id) DO UPDATE SET
			frequency = EXCLUDED.frequency,
			interval_count = EXCLUDED.interval_count,
			weekdays_mask = EXCLUDED.weekdays_mask,
			month_day = EXCLUDED.month_day,
			basis = EXCLUDED.basis,
			ends_on = EXCLUDED.ends_on,
			end_after = EXCLUDED.end_after,
			occurrence = EXCLUDED.occurrence,
			series_id = EXCLUDED.series_id,
			updated_at = NOW()`,
		r.TaskID, r.Frequency, r.Interval, r.WeekdaysMask, r.MonthDay, r.Basis,
		endsOn, r.EndAfter, r.Occurrence, r.SeriesID, createdBy); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx,
		`UPDATE tasks SET recurrence_series_id = $1 WHERE id = $2 AND recurrence_series_id IS DISTINCT FROM $1`,
		r.SeriesID, r.TaskID); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// DeleteTaskRecurrence removes a task's rule. It reports whether a rule existed.
func DeleteTaskRecurrence(taskID int) (bool, error) {
	pool, err := OpenDatabase()
	if err != nil {
		return false, err
	}
	defer CloseDatabase(pool)
	tag, err := pool.Exec(context.Background(), `DELETE FROM task_recurrence WHERE task_id = $1`, taskID)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() > 0, nil
}

// ClaimTaskRecurrence atomically removes and returns a task's rule so only one
// caller can spawn the next occurrence. Returns nil when there is no rule.
func ClaimTaskRecurrence(taskID int) (*TaskRecurrence, error) {
	pool, err := OpenDatabase()
	if err != nil {
		return nil, err
	}
	defer CloseDatabase(pool)
	r, err := scanTaskRecurrence(pool.QueryRow(context.Background(),
		`DELETE FROM task_recurrence WHERE task_id = $1 RETURNING `+recurrenceColumns, taskID))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) || errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return r, nil
}

// LinkRecurrenceTask records that taskID is the occurrence after prevID in seriesID.
func LinkRecurrenceTask(taskID, seriesID, prevID int) error {
	pool, err := OpenDatabase()
	if err != nil {
		return err
	}
	defer CloseDatabase(pool)
	var prev interface{}
	if prevID > 0 {
		prev = prevID
	}
	_, err = pool.Exec(context.Background(),
		`UPDATE tasks SET recurrence_series_id = $1, recurrence_prev_id = $2 WHERE id = $3`,
		seriesID, prev, taskID)
	return err
}

// GetTaskRecurrenceLinks returns a task's series id and previous occurrence id (0 when unset).
func GetTaskRecurrenceLinks(taskID int) (seriesID, prevID int, err error) {
	pool, err := OpenDatabase()
	if err != nil {
		return 0, 0, err
	}
	defer CloseDatabase(pool)
	var series, prev sql.NullInt64
	if err := pool.QueryRow(context.Background(),
		`SELECT recurrence_series_id, recurrence_prev_id FROM tasks WHERE id = $1`, taskID).Scan(&series, &prev); err != nil {
		return 0, 0, err
	}
	return int(series.Int64), int(prev.Int64), nil
}

// FindRecurrenceSuccessor returns the task spawned from prevID, or nil.
// window bounds how old the successor may be to count as Recent.
func FindRecurrenceSuccessor(prevID int, window time.Duration) (*RecurrenceSuccessor, error) {
	pool, err := OpenDatabase()
	if err != nil {
		return nil, err
	}
	defer CloseDatabase(pool)
	var s RecurrenceSuccessor
	err = pool.QueryRow(context.Background(), `
		SELECT t.id, COALESCE(t.completed, false),
			EXISTS (SELECT 1 FROM tasks c WHERE c.parent_id = t.id AND COALESCE(c.completed, false)),
			(SELECT COUNT(*) FROM task_comments tc WHERE tc.task_id = t.id AND tc.deleted_at IS NULL),
			t.time_stamp >= (NOW() AT TIME ZONE 'UTC') - make_interval(secs => $2)
		FROM tasks t
		WHERE t.recurrence_prev_id = $1
		ORDER BY t.id DESC LIMIT 1`, prevID, window.Seconds()).Scan(
		&s.TaskID, &s.Completed, &s.ChildProgress, &s.CommentCount, &s.Recent)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) || errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return &s, nil
}

// ListRecurrenceSeries returns the tasks in a series visible to userID, newest first.
func ListRecurrenceSeries(seriesID, userID, limit int) ([]RecurrenceSeriesItem, error) {
	if seriesID <= 0 {
		return nil, nil
	}
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	pool, err := OpenDatabase()
	if err != nil {
		return nil, err
	}
	defer CloseDatabase(pool)
	rows, err := pool.Query(context.Background(), `
		SELECT t.id, t.title, COALESCE(CAST(t.due_date AS TEXT), ''), COALESCE(t.completed, false),
			(SELECT MAX(e.created_at) FROM task_events e WHERE e.task_id = t.id AND e.event_type = 'completed'),
			t.time_stamp
		FROM tasks t
		WHERE t.recurrence_series_id = $1 AND `+TaskVisibleCondition("t", "$2")+`
		ORDER BY t.id DESC
		LIMIT $3`, seriesID, userID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []RecurrenceSeriesItem{}
	for rows.Next() {
		var it RecurrenceSeriesItem
		var completedAt sql.NullTime
		if err := rows.Scan(&it.TaskID, &it.Title, &it.DueDate, &it.Completed, &completedAt, &it.CreatedAt); err != nil {
			return nil, err
		}
		if completedAt.Valid && it.Completed {
			t := completedAt.Time
			it.CompletedAt = &t
		}
		out = append(out, it)
	}
	return out, rows.Err()
}

// TaskRecurrenceSource is the data copied from a completed occurrence into the next one.
type TaskRecurrenceSource struct {
	TaskID         int
	OwnerID        int
	Title          string
	Description    string
	DueDate        string
	Priority       int
	ProjectID      int
	ParentID       int
	EstimatePoints *int
	ClaimedBy      int
}

// GetTaskRecurrenceSource loads the fields needed to spawn the next occurrence.
func GetTaskRecurrenceSource(taskID int) (*TaskRecurrenceSource, error) {
	pool, err := OpenDatabase()
	if err != nil {
		return nil, err
	}
	defer CloseDatabase(pool)
	var s TaskRecurrenceSource
	var projectID, parentID, estimate, claimed sql.NullInt64
	if err := pool.QueryRow(context.Background(), `
		SELECT id, user_id, title, COALESCE(description, ''), COALESCE(CAST(due_date AS TEXT), ''),
			COALESCE(priority, 0), project_id, parent_id, estimate_points, claimed_by
		FROM tasks WHERE id = $1`, taskID).Scan(&s.TaskID, &s.OwnerID, &s.Title, &s.Description, &s.DueDate,
		&s.Priority, &projectID, &parentID, &estimate, &claimed); err != nil {
		return nil, err
	}
	s.ProjectID = int(projectID.Int64)
	s.ParentID = int(parentID.Int64)
	s.ClaimedBy = int(claimed.Int64)
	if estimate.Valid {
		v := int(estimate.Int64)
		s.EstimatePoints = &v
	}
	return &s, nil
}

// ListRecurrenceChildSources returns direct subtasks to copy into the next occurrence, in order.
func ListRecurrenceChildSources(parentID int) ([]TaskRecurrenceSource, error) {
	pool, err := OpenDatabase()
	if err != nil {
		return nil, err
	}
	defer CloseDatabase(pool)
	rows, err := pool.Query(context.Background(), `
		SELECT id, user_id, title, COALESCE(description, ''), COALESCE(priority, 0)
		FROM tasks WHERE parent_id = $1 AND NOT `+ArchivedTaskExistsSQL("id")+`
		ORDER BY COALESCE(position, 0), id`, parentID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []TaskRecurrenceSource{}
	for rows.Next() {
		var s TaskRecurrenceSource
		if err := rows.Scan(&s.TaskID, &s.OwnerID, &s.Title, &s.Description, &s.Priority); err != nil {
			return nil, err
		}
		s.ParentID = parentID
		out = append(out, s)
	}
	return out, rows.Err()
}

// DeleteTaskByID removes a task row (children cascade). Callers handle
// permissions, events and live notifications.
func DeleteTaskByID(taskID int) error {
	pool, err := OpenDatabase()
	if err != nil {
		return err
	}
	defer CloseDatabase(pool)
	_, err = pool.Exec(context.Background(), `DELETE FROM tasks WHERE id = $1`, taskID)
	return err
}
