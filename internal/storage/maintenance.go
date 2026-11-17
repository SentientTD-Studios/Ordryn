package storage

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
)

// Maintenance is the site's read-only switch. While it's on, only admins can
// change anything; everyone else can still sign in and read.
type Maintenance struct {
	Enabled   bool
	Message   string
	StartedAt *time.Time
	StartedBy int
}

// MigrateMaintenance creates the single-row maintenance table.
func MigrateMaintenance() error {
	pool, err := OpenDatabase()
	if err != nil {
		return err
	}
	defer CloseDatabase(pool)
	_, err = pool.Exec(context.Background(), `
		CREATE TABLE IF NOT EXISTS site_maintenance (
			id SMALLINT PRIMARY KEY DEFAULT 1 CHECK (id = 1),
			enabled BOOLEAN NOT NULL DEFAULT FALSE,
			message TEXT NOT NULL DEFAULT '',
			started_at TIMESTAMPTZ,
			started_by INTEGER REFERENCES users(id) ON DELETE SET NULL
		)`)
	if err != nil {
		return fmt.Errorf("site_maintenance: %w", err)
	}
	return nil
}

var (
	maintMu      sync.Mutex
	maintCached  *Maintenance
	maintFetched time.Time
)

// The switch is checked on every write, so cache it briefly. Turning it on
// clears the cache on this instance; other instances catch up within the TTL.
const maintCacheTTL = 5 * time.Second

// GetMaintenance returns the current state. Errors read as "off" so a database
// hiccup never locks people out.
func GetMaintenance() Maintenance {
	maintMu.Lock()
	if maintCached != nil && time.Since(maintFetched) < maintCacheTTL {
		m := *maintCached
		maintMu.Unlock()
		return m
	}
	maintMu.Unlock()

	m, err := loadMaintenance()
	if err != nil {
		return Maintenance{}
	}
	maintMu.Lock()
	maintCached, maintFetched = &m, time.Now()
	maintMu.Unlock()
	return m
}

func loadMaintenance() (Maintenance, error) {
	pool, err := OpenDatabase()
	if err != nil {
		return Maintenance{}, err
	}
	defer CloseDatabase(pool)
	var m Maintenance
	err = pool.QueryRow(context.Background(),
		`SELECT enabled, message, started_at, COALESCE(started_by, 0) FROM site_maintenance WHERE id = 1`).
		Scan(&m.Enabled, &m.Message, &m.StartedAt, &m.StartedBy)
	if errors.Is(err, pgx.ErrNoRows) {
		return Maintenance{}, nil
	}
	return m, err
}

// SetMaintenance turns read-only mode on or off.
func SetMaintenance(enabled bool, message string, by int) (Maintenance, error) {
	pool, err := OpenDatabase()
	if err != nil {
		return Maintenance{}, err
	}
	defer CloseDatabase(pool)
	var byArg any
	if by > 0 {
		byArg = by
	}
	_, err = pool.Exec(context.Background(), `
		INSERT INTO site_maintenance (id, enabled, message, started_at, started_by)
		VALUES (1, $1, $2, CASE WHEN $1 THEN NOW() END, CASE WHEN $1 THEN $3::int END)
		ON CONFLICT (id) DO UPDATE SET
			enabled = EXCLUDED.enabled, message = EXCLUDED.message,
			started_at = CASE WHEN EXCLUDED.enabled AND NOT site_maintenance.enabled THEN NOW()
			                  WHEN EXCLUDED.enabled THEN site_maintenance.started_at END,
			started_by = CASE WHEN EXCLUDED.enabled AND NOT site_maintenance.enabled THEN EXCLUDED.started_by
			                  WHEN EXCLUDED.enabled THEN site_maintenance.started_by END`,
		enabled, message, byArg)
	if err != nil {
		return Maintenance{}, err
	}
	maintMu.Lock()
	maintCached = nil
	maintMu.Unlock()
	return loadMaintenance()
}
