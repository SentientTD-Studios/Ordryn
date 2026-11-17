package storage

import (
	"context"
	"fmt"
)

// MigrateTasksAddStartDate adds tasks.start_date, the day work is planned to
// begin. Together with due_date it places a task on the project timeline.
func MigrateTasksAddStartDate() error {
	pool, err := OpenDatabase()
	if err != nil {
		return err
	}
	defer CloseDatabase(pool)
	if _, err := pool.Exec(context.Background(),
		`ALTER TABLE tasks ADD COLUMN IF NOT EXISTS start_date DATE`); err != nil {
		return fmt.Errorf("failed to add start_date: %v", err)
	}
	return nil
}

// SetTaskStartDate sets (YYYY-MM-DD) or clears ("") a task's start date.
func SetTaskStartDate(taskID int, startDate string) error {
	pool, err := OpenDatabase()
	if err != nil {
		return err
	}
	defer CloseDatabase(pool)
	var arg any
	if startDate != "" {
		arg = startDate
	}
	_, err = pool.Exec(context.Background(),
		`UPDATE tasks SET start_date = $1::date, date_modified = NOW() AT TIME ZONE 'UTC' WHERE id = $2`,
		arg, taskID)
	return err
}

// GetTaskStartDate returns a task's start date as YYYY-MM-DD, or "".
func GetTaskStartDate(taskID int) (string, error) {
	pool, err := OpenDatabase()
	if err != nil {
		return "", err
	}
	defer CloseDatabase(pool)
	var s string
	err = pool.QueryRow(context.Background(),
		`SELECT COALESCE(CAST(start_date AS TEXT), '') FROM tasks WHERE id = $1`, taskID).Scan(&s)
	return s, err
}
