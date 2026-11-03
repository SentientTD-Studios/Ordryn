package storage

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// MaxPersonNameLen caps first_name and last_name.
const MaxPersonNameLen = 100

// MigrateUsersAddNames adds first and last name, and full_name, which Postgres keeps
// as "First Last" (or whichever part is set).
func MigrateUsersAddNames() error {
	pool, err := OpenDatabase()
	if err != nil {
		return err
	}
	defer CloseDatabase(pool)

	stmts := []string{
		`ALTER TABLE users ADD COLUMN IF NOT EXISTS first_name VARCHAR(100) NOT NULL DEFAULT ''`,
		`ALTER TABLE users ADD COLUMN IF NOT EXISTS last_name VARCHAR(100) NOT NULL DEFAULT ''`,
		`ALTER TABLE users ADD COLUMN IF NOT EXISTS full_name TEXT
			GENERATED ALWAYS AS (btrim(first_name || ' ' || last_name)) STORED`,
	}
	for _, s := range stmts {
		if _, err := pool.Exec(context.Background(), s); err != nil {
			return fmt.Errorf("users names: %w", err)
		}
	}
	return nil
}

// SetUserNames sets an account's first and last name.
func SetUserNames(userID int, first, last string) error {
	pool, err := OpenDatabase()
	if err != nil {
		return err
	}
	defer CloseDatabase(pool)

	tag, err := pool.Exec(context.Background(),
		`UPDATE users SET first_name = $2, last_name = $3 WHERE id = $1`, userID, first, last)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return pgx.ErrNoRows
	}
	return nil
}
