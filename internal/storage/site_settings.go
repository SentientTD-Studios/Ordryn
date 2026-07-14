package storage

import (
	"context"
	"fmt"
)

// SiteSettings represents site-wide settings stored in the database.
type SiteSettings struct {
	SiteName                 string
	DefaultTimezone          string
	ShowChangelog            bool
	SiteVersion              string
	EnableRegistration       bool
	InviteOnly               bool
	MetaDescription          string
	EnableGlobalAnnouncement bool
	GlobalAnnouncementText   string
	EnableAPI                bool
}

// CreateSiteSettingsTable ensures the site_settings table exists.
func CreateSiteSettingsTable() error {
	pool, err := OpenDatabase()
	if err != nil {
		return err
	}
	defer CloseDatabase(pool)

	// id is a single-row table; use id=1 for the single settings row
	_, err = pool.Exec(context.Background(), `
        CREATE TABLE IF NOT EXISTS site_settings (
            id INTEGER PRIMARY KEY DEFAULT 1,
            site_name TEXT,
            default_timezone TEXT,
            show_changelog BOOLEAN DEFAULT TRUE,
			site_version TEXT,
			enable_registration BOOLEAN DEFAULT TRUE,
			invite_only BOOLEAN DEFAULT TRUE,
			meta_description TEXT,
			enable_global_announcement BOOLEAN DEFAULT FALSE,
			global_announcement_text TEXT
        )
    `)
	if err != nil {
		return fmt.Errorf("failed to create site_settings table: %v", err)
	}
	return nil
}

// GetSiteSettings returns the first (and only) settings row from site_settings.
func GetSiteSettings() (*SiteSettings, error) {
	pool, err := OpenDatabase()
	if err != nil {
		return nil, err
	}
	defer CloseDatabase(pool)

	var s SiteSettings
	row := pool.QueryRow(context.Background(), "SELECT site_name, default_timezone, show_changelog, COALESCE(site_version, ''), enable_registration, invite_only, COALESCE(meta_description, ''), COALESCE(enable_global_announcement, FALSE), COALESCE(global_announcement_text, ''), COALESCE(enable_api, FALSE) FROM site_settings WHERE id = 1")
	if err := row.Scan(&s.SiteName, &s.DefaultTimezone, &s.ShowChangelog, &s.SiteVersion, &s.EnableRegistration, &s.InviteOnly, &s.MetaDescription, &s.EnableGlobalAnnouncement, &s.GlobalAnnouncementText, &s.EnableAPI); err != nil {
		return nil, err
	}
	return &s, nil
}

// UpsertSiteSettings inserts or updates the singleton settings row (id=1).
func UpsertSiteSettings(s SiteSettings) error {
	pool, err := OpenDatabase()
	if err != nil {
		return err
	}
	defer CloseDatabase(pool)

	_, err = pool.Exec(context.Background(), `
        INSERT INTO site_settings (id, site_name, default_timezone, show_changelog, site_version, enable_registration, invite_only, meta_description, enable_global_announcement, global_announcement_text, enable_api)
        VALUES (1, $1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
        ON CONFLICT (id) DO UPDATE SET
            site_name = EXCLUDED.site_name,
            default_timezone = EXCLUDED.default_timezone,
            show_changelog = EXCLUDED.show_changelog,
            site_version = EXCLUDED.site_version,
            enable_registration = EXCLUDED.enable_registration,
            invite_only = EXCLUDED.invite_only,
            meta_description = EXCLUDED.meta_description,
            enable_global_announcement = EXCLUDED.enable_global_announcement,
            global_announcement_text = EXCLUDED.global_announcement_text,
            enable_api = EXCLUDED.enable_api
    `, s.SiteName, s.DefaultTimezone, s.ShowChangelog, s.SiteVersion, s.EnableRegistration, s.InviteOnly, s.MetaDescription, s.EnableGlobalAnnouncement, s.GlobalAnnouncementText, s.EnableAPI)
	if err != nil {
		return fmt.Errorf("failed to upsert site_settings: %v", err)
	}
	return nil
}

// MigrateSiteSettingsAddRegistrationOptions adds registration settings columns if they don't exist.
func MigrateSiteSettingsAddRegistrationOptions() error {
	pool, err := OpenDatabase()
	if err != nil {
		return err
	}
	defer CloseDatabase(pool)

	if _, err := pool.Exec(context.Background(), "ALTER TABLE site_settings ADD COLUMN IF NOT EXISTS enable_registration BOOLEAN DEFAULT TRUE"); err != nil {
		return fmt.Errorf("failed to add enable_registration column to site_settings: %v", err)
	}
	if _, err := pool.Exec(context.Background(), "ALTER TABLE site_settings ADD COLUMN IF NOT EXISTS invite_only BOOLEAN DEFAULT TRUE"); err != nil {
		return fmt.Errorf("failed to add invite_only column to site_settings: %v", err)
	}
	return nil
}

// MigrateSiteSettingsAddMetaDescription adds meta_description column if it doesn't exist.
func MigrateSiteSettingsAddMetaDescription() error {
	pool, err := OpenDatabase()
	if err != nil {
		return err
	}
	defer CloseDatabase(pool)

	if _, err := pool.Exec(context.Background(), "ALTER TABLE site_settings ADD COLUMN IF NOT EXISTS meta_description TEXT"); err != nil {
		return fmt.Errorf("failed to add meta_description column to site_settings: %v", err)
	}
	return nil
}

// MigrateSiteSettingsAddGlobalAnnouncement adds global announcement columns if they don't exist.
func MigrateSiteSettingsAddGlobalAnnouncement() error {
	pool, err := OpenDatabase()
	if err != nil {
		return err
	}
	defer CloseDatabase(pool)

	if _, err := pool.Exec(context.Background(), "ALTER TABLE site_settings ADD COLUMN IF NOT EXISTS enable_global_announcement BOOLEAN DEFAULT FALSE"); err != nil {
		return fmt.Errorf("failed to add enable_global_announcement column to site_settings: %v", err)
	}
	if _, err := pool.Exec(context.Background(), "ALTER TABLE site_settings ADD COLUMN IF NOT EXISTS global_announcement_text TEXT"); err != nil {
		return fmt.Errorf("failed to add global_announcement_text column to site_settings: %v", err)
	}
	return nil
}

// MigrateSiteSettingsAddEnableAPI adds enable_api column if it doesn't exist.
func MigrateSiteSettingsAddEnableAPI() error {
	pool, err := OpenDatabase()
	if err != nil {
		return err
	}
	defer CloseDatabase(pool)

	if _, err := pool.Exec(context.Background(), "ALTER TABLE site_settings ADD COLUMN IF NOT EXISTS enable_api BOOLEAN DEFAULT FALSE"); err != nil {
		return fmt.Errorf("failed to add enable_api column to site_settings: %v", err)
	}
	return nil
}
