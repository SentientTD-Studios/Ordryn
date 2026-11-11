package storage

import (
	"context"
	"fmt"
)

// CreateTaskSnoozesTable creates task_snoozes: per-user "hide this task from my
// Today list until a day". Snoozes are private to the user who set them.
func CreateTaskSnoozesTable() error {
	pool, err := OpenDatabase()
	if err != nil {
		return err
	}
	defer CloseDatabase(pool)
	if _, err := pool.Exec(context.Background(), `
		CREATE TABLE IF NOT EXISTS task_snoozes (
			user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			task_id INTEGER NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
			until DATE NOT NULL,
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			PRIMARY KEY (user_id, task_id)
		)`); err != nil {
		return fmt.Errorf("failed to create task_snoozes: %v", err)
	}
	if _, err := pool.Exec(context.Background(),
		`CREATE INDEX IF NOT EXISTS idx_task_snoozes_task ON task_snoozes(task_id)`); err != nil {
		return fmt.Errorf("failed to index task_snoozes: %v", err)
	}
	return nil
}

// SetTaskSnooze snoozes a task for a user until the given YYYY-MM-DD day.
func SetTaskSnooze(userID, taskID int, until string) error {
	pool, err := OpenDatabase()
	if err != nil {
		return err
	}
	defer CloseDatabase(pool)
	_, err = pool.Exec(context.Background(), `
		INSERT INTO task_snoozes (user_id, task_id, until) VALUES ($1, $2, $3::date)
		ON CONFLICT (user_id, task_id) DO UPDATE SET until = EXCLUDED.until, created_at = NOW()`,
		userID, taskID, until)
	return err
}

// ClearTaskSnooze removes a user's snooze on a task (no error when none).
func ClearTaskSnooze(userID, taskID int) error {
	pool, err := OpenDatabase()
	if err != nil {
		return err
	}
	defer CloseDatabase(pool)
	_, err = pool.Exec(context.Background(),
		`DELETE FROM task_snoozes WHERE user_id = $1 AND task_id = $2`, userID, taskID)
	return err
}

// GetTaskSnooze returns the user's snooze day for a task as YYYY-MM-DD, or "".
func GetTaskSnooze(userID, taskID int) (string, error) {
	pool, err := OpenDatabase()
	if err != nil {
		return "", err
	}
	defer CloseDatabase(pool)
	var until string
	err = pool.QueryRow(context.Background(), `
		SELECT COALESCE((SELECT CAST(until AS TEXT) FROM task_snoozes WHERE user_id = $1 AND task_id = $2), '')`,
		userID, taskID).Scan(&until)
	return until, err
}
