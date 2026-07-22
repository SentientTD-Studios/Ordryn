package storage

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
)

// GetRoleIDByName returns the roles.id for a role name (e.g. "admin", "user").
func GetRoleIDByName(name string) (int, error) {
	pool, err := OpenDatabase()
	if err != nil {
		return 0, err
	}
	defer CloseDatabase(pool)

	var id int
	err = pool.QueryRow(context.Background(),
		`SELECT id FROM roles WHERE name = $1`, name).Scan(&id)
	if err != nil {
		return 0, fmt.Errorf("role %q not found: %w", name, err)
	}
	return id, nil
}

// UserExistsByEmail reports whether a user row exists for the email.
func UserExistsByEmail(email string) (bool, error) {
	pool, err := OpenDatabase()
	if err != nil {
		return false, err
	}
	defer CloseDatabase(pool)

	var id int
	err = pool.QueryRow(context.Background(),
		`SELECT id FROM users WHERE email = $1`, strings.TrimSpace(email)).Scan(&id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

// CreateUser inserts a user and returns the new id.
// Generates a unique username from the email (bootstrap / admin create).
func CreateUser(email, hashedPassword, timezone string, roleID int) (int, error) {
	pool, err := OpenDatabase()
	if err != nil {
		return 0, err
	}
	defer CloseDatabase(pool)

	if timezone == "" {
		timezone = "America/New_York"
	}
	email = strings.TrimSpace(email)
	claimed := make(map[string]int)
	userName, err := allocateGeneratedUsername(email, claimed)
	if err != nil {
		return 0, fmt.Errorf("create user username: %w", err)
	}
	var id int
	err = pool.QueryRow(context.Background(),
		`INSERT INTO users (email, password, role_id, timezone, user_name, username_change_available)
		 VALUES ($1, $2, $3, $4, $5, FALSE) RETURNING id`,
		email, hashedPassword, roleID, timezone, userName).Scan(&id)
	if err != nil {
		return 0, fmt.Errorf("create user: %w", err)
	}
	return id, nil
}

// GetUserIDByEmail returns the user id for an email.
func GetUserIDByEmail(email string) (int, error) {
	pool, err := OpenDatabase()
	if err != nil {
		return 0, err
	}
	defer CloseDatabase(pool)

	var id int
	err = pool.QueryRow(context.Background(),
		`SELECT id FROM users WHERE email = $1`, strings.TrimSpace(email)).Scan(&id)
	if err != nil {
		return 0, err
	}
	return id, nil
}

// HasActiveAPIKeyNamed reports whether the user has a non-revoked key with the given name.
func HasActiveAPIKeyNamed(userID int, name string) (bool, error) {
	pool, err := OpenDatabase()
	if err != nil {
		return false, err
	}
	defer CloseDatabase(pool)

	var id int
	err = pool.QueryRow(context.Background(),
		`SELECT id FROM api_keys
		 WHERE user_id = $1 AND name = $2 AND revoked_at IS NULL
		 LIMIT 1`, userID, name).Scan(&id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

// EnsureEnableAPI upserts site_settings with enable_api=true, preserving other fields when present.
func EnsureEnableAPI() error {
	s, err := GetSiteSettings()
	if err != nil || s == nil {
		s = &SiteSettings{
			SiteName:           "GoTodo",
			DefaultTimezone:    "America/New_York",
			ShowChangelog:      true,
			EnableRegistration: true,
			InviteOnly:         true,
			EnableAPI:          true,
		}
	} else {
		s.EnableAPI = true
	}
	return UpsertSiteSettings(*s)
}
