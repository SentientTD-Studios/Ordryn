package storage

import (
	"context"
	"fmt"
)

// CreateUserNotificationOptOutsTable creates the per-user notification opt-out store.
// A missing row means the type is enabled, so new types default on without a backfill.
func CreateUserNotificationOptOutsTable() error {
	pool, err := OpenDatabase()
	if err != nil {
		return err
	}
	defer CloseDatabase(pool)

	_, err = pool.Exec(context.Background(), `CREATE TABLE IF NOT EXISTS user_notification_optouts (
		user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
		notification_type TEXT NOT NULL,
		created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
		PRIMARY KEY (user_id, notification_type)
	)`)
	if err != nil {
		return fmt.Errorf("failed to create user_notification_optouts: %v", err)
	}
	return nil
}

// ListUserNotificationOptOuts returns the notification types a user has turned off.
func ListUserNotificationOptOuts(userID int) (map[string]bool, error) {
	pool, err := OpenDatabase()
	if err != nil {
		return nil, err
	}
	defer CloseDatabase(pool)

	rows, err := pool.Query(context.Background(),
		`SELECT notification_type FROM user_notification_optouts WHERE user_id = $1`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := map[string]bool{}
	for rows.Next() {
		var t string
		if err := rows.Scan(&t); err != nil {
			return nil, err
		}
		out[t] = true
	}
	return out, rows.Err()
}

// SetUserNotificationOptOut turns a notification type off (optOut) or back on for a user.
// Callers must only pass optional types; required types are never consulted here.
func SetUserNotificationOptOut(userID int, notificationType string, optOut bool) error {
	pool, err := OpenDatabase()
	if err != nil {
		return err
	}
	defer CloseDatabase(pool)

	if optOut {
		_, err = pool.Exec(context.Background(),
			`INSERT INTO user_notification_optouts (user_id, notification_type)
			 VALUES ($1, $2) ON CONFLICT DO NOTHING`, userID, notificationType)
	} else {
		_, err = pool.Exec(context.Background(),
			`DELETE FROM user_notification_optouts WHERE user_id = $1 AND notification_type = $2`,
			userID, notificationType)
	}
	return err
}

// UsersOptedOut returns which of userIDs have turned off notificationType.
func UsersOptedOut(userIDs []int, notificationType string) (map[int]bool, error) {
	out := map[int]bool{}
	if len(userIDs) == 0 {
		return out, nil
	}
	pool, err := OpenDatabase()
	if err != nil {
		return nil, err
	}
	defer CloseDatabase(pool)

	rows, err := pool.Query(context.Background(),
		`SELECT user_id FROM user_notification_optouts
		 WHERE notification_type = $1 AND user_id = ANY($2)`, notificationType, userIDs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id int
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out[id] = true
	}
	return out, rows.Err()
}
