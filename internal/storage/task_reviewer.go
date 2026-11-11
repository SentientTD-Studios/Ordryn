package storage

import (
	"context"
	"database/sql"
	"fmt"
)

// MigrateTasksAddReviewer adds tasks.reviewer_id: the member asked to review a
// task. Deleting the user clears it.
func MigrateTasksAddReviewer() error {
	pool, err := OpenDatabase()
	if err != nil {
		return err
	}
	defer CloseDatabase(pool)
	ctx := context.Background()

	if _, err := pool.Exec(ctx, `ALTER TABLE tasks ADD COLUMN IF NOT EXISTS reviewer_id INTEGER`); err != nil {
		return fmt.Errorf("failed to add reviewer_id: %v", err)
	}
	var exists bool
	if err := pool.QueryRow(ctx,
		`SELECT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'fk_tasks_reviewer_id')`).Scan(&exists); err != nil {
		return err
	}
	if !exists {
		if _, err := pool.Exec(ctx,
			`ALTER TABLE tasks ADD CONSTRAINT fk_tasks_reviewer_id
			 FOREIGN KEY (reviewer_id) REFERENCES users(id) ON DELETE SET NULL`); err != nil {
			return fmt.Errorf("failed to add reviewer_id FK: %v", err)
		}
	}
	if _, err := pool.Exec(ctx,
		`CREATE INDEX IF NOT EXISTS idx_tasks_reviewer_id ON tasks(reviewer_id) WHERE reviewer_id IS NOT NULL`); err != nil {
		return fmt.Errorf("failed to index reviewer_id: %v", err)
	}
	return nil
}

// SetTaskReviewer sets (reviewerID > 0) or clears (nil) a task's reviewer.
func SetTaskReviewer(taskID int, reviewerID *int) error {
	pool, err := OpenDatabase()
	if err != nil {
		return err
	}
	defer CloseDatabase(pool)

	var arg any
	if reviewerID != nil && *reviewerID > 0 {
		arg = *reviewerID
	}
	_, err = pool.Exec(context.Background(),
		`UPDATE tasks SET reviewer_id = $1, date_modified = NOW() AT TIME ZONE 'UTC' WHERE id = $2`,
		arg, taskID)
	return err
}

// GetTaskReviewer returns a task's reviewer user id, or 0 when none.
func GetTaskReviewer(taskID int) (int, error) {
	pool, err := OpenDatabase()
	if err != nil {
		return 0, err
	}
	defer CloseDatabase(pool)

	var id sql.NullInt64
	if err := pool.QueryRow(context.Background(),
		`SELECT reviewer_id FROM tasks WHERE id = $1`, taskID).Scan(&id); err != nil {
		return 0, err
	}
	if !id.Valid {
		return 0, nil
	}
	return int(id.Int64), nil
}
