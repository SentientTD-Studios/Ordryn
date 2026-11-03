package storage

import (
	"GoTodo/internal/mailer"
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
// Generates a unique username from the email (admin create).
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
	if err := JoinCompanyIfEnabled(id); err != nil {
		fmt.Printf("company: could not add user %d: %v\n", id, err)
	}
	return id, nil
}

func defaultSiteSettings() SiteSettings {
	return SiteSettings{
		SiteName:           "GoTodo",
		DefaultTimezone:    "America/New_York",
		ShowChangelog:      true,
		EnableRegistration: true,
		InviteOnly:         true,
		Email: mailer.Config{
			SMTPPort: 587,
			SMTPTLS:  true,
		},
	}
}

// EnsureSiteSettingsRow inserts the default settings row when none exists yet.
// It never touches an existing row, even if reading it fails.
func EnsureSiteSettingsRow() error {
	pool, err := OpenDatabase()
	if err != nil {
		return err
	}
	defer CloseDatabase(pool)

	var exists bool
	if err := pool.QueryRow(context.Background(),
		`SELECT EXISTS (SELECT 1 FROM site_settings WHERE id = 1)`).Scan(&exists); err != nil {
		return err
	}
	if exists {
		return nil
	}
	return UpsertSiteSettings(defaultSiteSettings())
}
