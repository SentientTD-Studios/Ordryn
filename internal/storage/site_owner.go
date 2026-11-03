package storage

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// The site owner is the one account above admins: by default the person who ran
// first-time setup. Only the owner can make, change, or deactivate admins, and
// nobody can change or deactivate the owner. Ownership moves only by transfer.

// MigrateSiteSettingsAddOwner adds site_settings.owner_user_id and gives sites that
// predate it an owner: the earliest active admin.
func MigrateSiteSettingsAddOwner() error {
	pool, err := OpenDatabase()
	if err != nil {
		return err
	}
	defer CloseDatabase(pool)

	if _, err := pool.Exec(context.Background(),
		`ALTER TABLE site_settings ADD COLUMN IF NOT EXISTS owner_user_id INTEGER REFERENCES users(id) ON DELETE SET NULL`); err != nil {
		return fmt.Errorf("add site_settings.owner_user_id: %w", err)
	}
	return EnsureSiteOwner()
}

// EnsureSiteOwner sets the owner to the earliest active admin when there is none.
// It does nothing on a site with no settings row or no admins yet.
func EnsureSiteOwner() error {
	pool, err := OpenDatabase()
	if err != nil {
		return err
	}
	defer CloseDatabase(pool)

	_, err = pool.Exec(context.Background(), `
		UPDATE site_settings SET owner_user_id = (
			SELECT u.id FROM users u
			JOIN roles r ON r.id = u.role_id
			WHERE $1::text = ANY(r.permissions)
			  AND NOT COALESCE(u.is_agent, FALSE) AND NOT COALESCE(u.is_banned, FALSE)
			ORDER BY u.id LIMIT 1)
		WHERE id = 1 AND owner_user_id IS NULL`, SitePermAdmin)
	return err
}

// SiteOwnerID returns the owner's user id, or 0 when there is none.
func SiteOwnerID() (int, error) {
	pool, err := OpenDatabase()
	if err != nil {
		return 0, err
	}
	defer CloseDatabase(pool)

	var id *int
	err = pool.QueryRow(context.Background(), `SELECT owner_user_id FROM site_settings WHERE id = 1`).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) || id == nil {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	return *id, nil
}

// IsSiteOwner reports whether userID is the site owner.
func IsSiteOwner(userID int) bool {
	if userID <= 0 {
		return false
	}
	owner, err := SiteOwnerID()
	return err == nil && owner == userID
}

// SetSiteOwner makes userID the owner. Callers make sure the account is an active admin.
func SetSiteOwner(userID int) error {
	if err := EnsureSiteSettingsRow(); err != nil {
		return err
	}
	pool, err := OpenDatabase()
	if err != nil {
		return err
	}
	defer CloseDatabase(pool)

	_, err = pool.Exec(context.Background(), `UPDATE site_settings SET owner_user_id = $1 WHERE id = 1`, userID)
	return err
}
