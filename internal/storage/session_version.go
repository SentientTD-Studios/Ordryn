package storage

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// MigrateUsersAddSessionVersion adds users.session_version. Every session cookie
// records the version current at sign-in; raising it signs out every session
// issued before. It's raised on password change or reset, deactivation, and
// "sign out other devices".
func MigrateUsersAddSessionVersion() error {
	pool, err := OpenDatabase()
	if err != nil {
		return err
	}
	defer CloseDatabase(pool)
	_, err = pool.Exec(context.Background(),
		`ALTER TABLE users ADD COLUMN IF NOT EXISTS session_version INTEGER NOT NULL DEFAULT 1`)
	if err != nil {
		return fmt.Errorf("users.session_version: %w", err)
	}
	return nil
}

// SessionState is what a signed-in request needs to know about its account.
type SessionState struct {
	Version int
	Active  bool
}

// GetSessionState returns the account's current session version and whether it
// may sign in. A missing account comes back inactive, not as an error.
func GetSessionState(userID int) (SessionState, error) {
	pool, err := OpenDatabase()
	if err != nil {
		return SessionState{}, err
	}
	defer CloseDatabase(pool)

	var s SessionState
	var banned bool
	err = pool.QueryRow(context.Background(),
		`SELECT session_version, COALESCE(is_banned, FALSE) FROM users WHERE id = $1`, userID).
		Scan(&s.Version, &banned)
	if errors.Is(err, pgx.ErrNoRows) {
		return SessionState{}, nil
	}
	if err != nil {
		return SessionState{}, err
	}
	s.Active = !banned
	return s, nil
}

// BumpSessionVersion signs out every existing session of the account and
// returns the new version.
func BumpSessionVersion(userID int) (int, error) {
	pool, err := OpenDatabase()
	if err != nil {
		return 0, err
	}
	defer CloseDatabase(pool)

	var v int
	err = pool.QueryRow(context.Background(),
		`UPDATE users SET session_version = session_version + 1 WHERE id = $1 RETURNING session_version`,
		userID).Scan(&v)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, fmt.Errorf("user not found")
	}
	return v, err
}
