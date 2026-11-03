package storage

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

// ErrSetupComplete is returned when first-time setup runs on a site that already has an account.
var ErrSetupComplete = errors.New("setup has already been completed")

// setupAccountsSQL is true once a person has an account. Agent and automation
// accounts don't count: migrations create the automation account on a fresh site.
const setupAccountsSQL = `EXISTS (SELECT 1 FROM users WHERE NOT COALESCE(is_agent, FALSE))`

// SetupRequired reports whether the site still needs first-time setup.
func SetupRequired() (bool, error) {
	pool, err := OpenDatabase()
	if err != nil {
		return false, err
	}
	defer CloseDatabase(pool)

	var done bool
	if err := pool.QueryRow(context.Background(), `SELECT `+setupAccountsSQL).Scan(&done); err != nil {
		return false, err
	}
	return !done, nil
}

// SetupAdmin is the first account, created during setup with the admin role.
type SetupAdmin struct {
	Email        string
	PasswordHash string
	UserName     string
	Timezone     string
}

// CreateSetupAdmin creates the first account with the admin role and returns its id.
// It fails with ErrSetupComplete once any account exists; an advisory lock keeps two
// concurrent setups from both getting through.
func CreateSetupAdmin(a SetupAdmin) (int, error) {
	roleID, err := GetRoleIDByName(AdminRoleName)
	if err != nil {
		return 0, err
	}
	pool, err := OpenDatabase()
	if err != nil {
		return 0, err
	}
	defer CloseDatabase(pool)

	ctx := context.Background()
	tx, err := pool.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(ctx)

	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtext('ordryn_first_time_setup'))`); err != nil {
		return 0, err
	}
	var done bool
	if err := tx.QueryRow(ctx, `SELECT `+setupAccountsSQL).Scan(&done); err != nil {
		return 0, err
	}
	if done {
		return 0, ErrSetupComplete
	}

	var id int
	err = tx.QueryRow(ctx,
		`INSERT INTO users (email, password, role_id, timezone, user_name, username_change_available)
		 VALUES ($1, $2, $3, $4, $5, FALSE) RETURNING id`,
		strings.TrimSpace(a.Email), a.PasswordHash, roleID, a.Timezone, strings.TrimSpace(a.UserName)).Scan(&id)
	if err != nil {
		return 0, fmt.Errorf("create setup admin: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, err
	}
	return id, nil
}
