package handlers

import (
	"context"

	"GoTodo/internal/storage"
)

// GetUserTimezoneByID returns the user's timezone or UTC on failure.
func GetUserTimezoneByID(userID int) string {
	pool, err := storage.OpenDatabase()
	if err != nil {
		return "UTC"
	}
	defer storage.CloseDatabase(pool)
	var tz string
	if err := pool.QueryRow(context.Background(),
		`SELECT COALESCE(NULLIF(timezone, ''), 'UTC') FROM users WHERE id = $1`, userID).Scan(&tz); err != nil {
		return "UTC"
	}
	return tz
}
