package domain

import (
	"context"
	"errors"
	"testing"

	"GoTodo/internal/storage"
)

// useFreshDatabase points storage at a new, fully migrated database for the rest of
// the test. The shared test database already has projects, so the unlocked path
// needs its own.
func useFreshDatabase(t *testing.T, name string) {
	t.Helper()
	pool, err := storage.OpenDatabase()
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if _, err := pool.Exec(ctx, `DROP DATABASE IF EXISTS `+name); err != nil {
		t.Fatalf("drop %s: %v", name, err)
	}
	if _, err := pool.Exec(ctx, `CREATE DATABASE `+name); err != nil {
		t.Fatalf("create %s: %v", name, err)
	}
	t.Setenv("DB_NAME", name)
	storage.ResetSharedPoolForTests()
	// Runs before t.Setenv restores DB_NAME; the next OpenDatabase reconnects lazily.
	t.Cleanup(storage.ResetSharedPoolForTests)
	if err := storage.RunMigrations(); err != nil {
		t.Fatalf("migrate %s: %v", name, err)
	}
}

func TestSiteModeLocksOnceProjectsOrOrganizationsExist(t *testing.T) {
	useFreshDatabase(t, "gotodo_site_mode_test")
	ctx := context.Background()

	if mode, err := storage.GetSiteMode(); err != nil || mode != storage.SiteModeMulti {
		t.Fatalf("fresh site mode = %q, %v; want multi", mode, err)
	}
	if locked, err := storage.SiteModeLocked(); err != nil || locked {
		t.Fatalf("fresh site locked = %v, %v; want false", locked, err)
	}

	// A fresh install has no settings row yet; choosing a mode must create a usable one.
	if err := storage.SetSiteMode("Company"); err != nil {
		t.Fatalf("set company on empty site: %v", err)
	}
	settings, err := storage.GetSiteSettings()
	if err != nil {
		t.Fatalf("settings after first mode choice: %v", err)
	}
	if settings.SiteMode != storage.SiteModeCompany || settings.SiteName == "" {
		t.Fatalf("settings = mode %q name %q; want company with default name", settings.SiteMode, settings.SiteName)
	}
	if err := storage.SetSiteMode("bogus"); err == nil {
		t.Fatal("unknown mode should be rejected")
	}

	pool, err := storage.OpenDatabase()
	if err != nil {
		t.Fatal(err)
	}
	var userID int
	if err := pool.QueryRow(ctx,
		`INSERT INTO users (email, password, role_id) VALUES ('mode@example.com', 'x', 1) RETURNING id`).Scan(&userID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO projects (user_id, name) VALUES ($1, 'First')`, userID); err != nil {
		t.Fatal(err)
	}
	if err := storage.SetSiteMode(storage.SiteModeMulti); !errors.Is(err, storage.ErrSiteModeLocked) {
		t.Fatalf("switch with a project: err=%v, want ErrSiteModeLocked", err)
	}
	if err := storage.SetSiteMode(storage.SiteModeCompany); err != nil {
		t.Fatalf("re-setting the current mode should succeed: %v", err)
	}

	if _, err := pool.Exec(ctx, `DELETE FROM projects`); err != nil {
		t.Fatal(err)
	}
	if _, err := storage.CreateOrganization(userID, "Org", ""); err != nil {
		t.Fatal(err)
	}
	if err := storage.SetSiteMode(storage.SiteModeMulti); !errors.Is(err, storage.ErrSiteModeLocked) {
		t.Fatalf("switch with an organization: err=%v, want ErrSiteModeLocked", err)
	}
	if mode, _ := storage.GetSiteMode(); mode != storage.SiteModeCompany {
		t.Fatalf("mode = %q after refused switch; want company", mode)
	}
}

func TestNormalizeSiteMode(t *testing.T) {
	cases := map[string]string{"multi": "multi", " COMPANY ": "company", "": "", "single": ""}
	for in, want := range cases {
		if got := storage.NormalizeSiteMode(in); got != want {
			t.Errorf("NormalizeSiteMode(%q) = %q, want %q", in, got, want)
		}
	}
}
