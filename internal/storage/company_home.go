package storage

import (
	"context"
	"fmt"
)

// MaxCompanyHomeLen caps the company home page content.
const MaxCompanyHomeLen = 20000

// MigrateSiteSettingsAddCompanyHome adds site_settings.company_home: Markdown that
// signed-in people see in company mode when no project is shared with them yet.
func MigrateSiteSettingsAddCompanyHome() error {
	pool, err := OpenDatabase()
	if err != nil {
		return err
	}
	defer CloseDatabase(pool)

	if _, err := pool.Exec(context.Background(),
		`ALTER TABLE site_settings ADD COLUMN IF NOT EXISTS company_home TEXT NOT NULL DEFAULT ''`); err != nil {
		return fmt.Errorf("add site_settings.company_home: %w", err)
	}
	return nil
}

// GetCompanyHome returns the company home page content, or "" when unset.
func GetCompanyHome() (string, error) {
	pool, err := OpenDatabase()
	if err != nil {
		return "", err
	}
	defer CloseDatabase(pool)

	var content string
	err = pool.QueryRow(context.Background(),
		`SELECT COALESCE(company_home, '') FROM site_settings WHERE id = 1`).Scan(&content)
	if err != nil {
		return "", err
	}
	return content, nil
}

// SetCompanyHome stores the company home page content.
func SetCompanyHome(content string) error {
	pool, err := OpenDatabase()
	if err != nil {
		return err
	}
	defer CloseDatabase(pool)

	_, err = pool.Exec(context.Background(), `UPDATE site_settings SET company_home = $1 WHERE id = 1`, content)
	return err
}
