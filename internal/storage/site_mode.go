package storage

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
)

// Site modes. Multi is the classic setup where anyone may create organizations;
// company runs the site as one company with admin-created teams.
const (
	SiteModeMulti   = "multi"
	SiteModeCompany = "company"
)

// ErrSiteModeLocked is returned when the mode can no longer change because the
// site already has projects or organizations.
var ErrSiteModeLocked = errors.New("site mode is locked once projects or organizations exist")

// NormalizeSiteMode returns the canonical mode, or "" when raw is not a mode.
func NormalizeSiteMode(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case SiteModeMulti:
		return SiteModeMulti
	case SiteModeCompany:
		return SiteModeCompany
	}
	return ""
}

// MigrateSiteSettingsAddSiteMode adds site_settings.site_mode. Existing sites
// become multi, which is how they already behave.
func MigrateSiteSettingsAddSiteMode() error {
	pool, err := OpenDatabase()
	if err != nil {
		return err
	}
	defer CloseDatabase(pool)

	_, err = pool.Exec(context.Background(),
		`ALTER TABLE site_settings ADD COLUMN IF NOT EXISTS site_mode VARCHAR(16) NOT NULL DEFAULT 'multi'`)
	if err != nil {
		return fmt.Errorf("add site_settings.site_mode: %w", err)
	}
	return nil
}

// siteModeLockedSQL is true once the mode can't safely change. The company
// organization doesn't count: company mode creates it as soon as it is chosen.
const siteModeLockedSQL = `(EXISTS (SELECT 1 FROM projects) OR EXISTS (SELECT 1 FROM organizations WHERE NOT is_company))`

// GetSiteMode returns the site mode, defaulting to multi when unset.
func GetSiteMode() (string, error) {
	pool, err := OpenDatabase()
	if err != nil {
		return "", err
	}
	defer CloseDatabase(pool)

	var mode string
	err = pool.QueryRow(context.Background(),
		`SELECT COALESCE(site_mode, '') FROM site_settings WHERE id = 1`).Scan(&mode)
	if errors.Is(err, pgx.ErrNoRows) {
		return SiteModeMulti, nil
	}
	if err != nil {
		return "", err
	}
	if m := NormalizeSiteMode(mode); m != "" {
		return m, nil
	}
	return SiteModeMulti, nil
}

// SiteModeLocked reports whether projects or organizations exist, which fixes the mode.
func SiteModeLocked() (bool, error) {
	pool, err := OpenDatabase()
	if err != nil {
		return false, err
	}
	defer CloseDatabase(pool)

	var locked bool
	err = pool.QueryRow(context.Background(), `SELECT `+siteModeLockedSQL).Scan(&locked)
	return locked, err
}

// SetSiteMode changes the mode while the site has no projects or organizations.
// Setting the current mode again always succeeds.
func SetSiteMode(mode string) error {
	m := NormalizeSiteMode(mode)
	if m == "" {
		return fmt.Errorf("unknown site mode %q", mode)
	}
	if err := EnsureSiteSettingsRow(); err != nil {
		return err
	}
	pool, err := OpenDatabase()
	if err != nil {
		return err
	}
	defer CloseDatabase(pool)

	// One statement, so a project created concurrently can't slip past the check.
	tag, err := pool.Exec(context.Background(), `
		UPDATE site_settings SET site_mode = $1
		WHERE id = 1 AND (site_mode = $1 OR NOT `+siteModeLockedSQL+`)`, m)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrSiteModeLocked
	}
	return nil
}
