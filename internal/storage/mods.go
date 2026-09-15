package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"GoTodo/internal/crypto/secret"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ModSettings is the site-level JSON document stored per mod_id.
type ModSettings struct {
	Enabled        bool              `json:"enabled"`
	AllProjects    bool              `json:"all_projects,omitempty"`
	ProjectIDs     []int             `json:"project_ids,omitempty"`
	Triggers       []string          `json:"triggers,omitempty"`
	Templates      map[string]string `json:"templates,omitempty"`
	StatusOnly     bool              `json:"status_only,omitempty"`
	LastError      string            `json:"last_error,omitempty"`
	LastDeliveryAt string            `json:"last_delivery_at,omitempty"`
}

// ModProjectSettings is the per-project JSON document for a mod.
type ModProjectSettings struct {
	Enabled        bool              `json:"enabled"`
	Triggers       []string          `json:"triggers"`
	Templates      map[string]string `json:"templates"`
	StatusOnly     bool              `json:"status_only"`
	LastError      string            `json:"last_error,omitempty"`
	LastDeliveryAt string            `json:"last_delivery_at,omitempty"`
}

// HookTaskSnapshot is the task view used when rendering hook templates.
type HookTaskSnapshot struct {
	ID          int
	Title       string
	Completed   bool
	Priority    int
	ProjectID   int
	ProjectName string
	StatusName  string
}

// CreateModTables creates generic mod settings and secret tables.
func CreateModTables() error {
	pool, err := OpenDatabase()
	if err != nil {
		return err
	}
	defer CloseDatabase(pool)

	stmts := []string{
		`CREATE TABLE IF NOT EXISTS mod_settings (
			mod_id VARCHAR(64) PRIMARY KEY,
			data JSONB NOT NULL DEFAULT '{}',
			updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)`,
		`CREATE TABLE IF NOT EXISTS mod_secrets (
			mod_id VARCHAR(64) NOT NULL,
			project_id INTEGER NOT NULL DEFAULT 0,
			key VARCHAR(64) NOT NULL,
			value_enc TEXT NOT NULL,
			updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			PRIMARY KEY (mod_id, project_id, key)
		)`,
		`CREATE TABLE IF NOT EXISTS mod_project_settings (
			mod_id VARCHAR(64) NOT NULL,
			project_id INTEGER NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
			data JSONB NOT NULL DEFAULT '{}',
			updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			PRIMARY KEY (mod_id, project_id)
		)`,
	}
	for _, q := range stmts {
		if _, err := pool.Exec(context.Background(), q); err != nil {
			return fmt.Errorf("create mod tables: %w", err)
		}
	}
	if err := migrateModSecretsProjectID(pool); err != nil {
		return err
	}
	return nil
}

func migrateModSecretsProjectID(pool *pgxpool.Pool) error {
	ctx := context.Background()
	if _, err := pool.Exec(ctx, `ALTER TABLE mod_secrets ADD COLUMN IF NOT EXISTS project_id INTEGER NOT NULL DEFAULT 0`); err != nil {
		return fmt.Errorf("mod_secrets project_id: %w", err)
	}
	if _, err := pool.Exec(ctx, `ALTER TABLE mod_secrets DROP CONSTRAINT IF EXISTS mod_secrets_pkey`); err != nil {
		return fmt.Errorf("mod_secrets drop pkey: %w", err)
	}
	if _, err := pool.Exec(ctx, `ALTER TABLE mod_secrets ADD PRIMARY KEY (mod_id, project_id, key)`); err != nil {
		return fmt.Errorf("mod_secrets pkey: %w", err)
	}
	return nil
}

func emptyModSettings() ModSettings {
	return ModSettings{}
}

func emptyModProjectSettings() ModProjectSettings {
	return ModProjectSettings{
		Triggers:  []string{},
		Templates: map[string]string{},
	}
}

// GetModSettings returns stored site settings or empty defaults.
func GetModSettings(modID string) (ModSettings, error) {
	out := emptyModSettings()
	modID = strings.TrimSpace(modID)
	if modID == "" {
		return out, fmt.Errorf("mod id required")
	}
	pool, err := OpenDatabase()
	if err != nil {
		return out, err
	}
	defer CloseDatabase(pool)

	var raw []byte
	err = pool.QueryRow(context.Background(),
		`SELECT data FROM mod_settings WHERE mod_id = $1`, modID).Scan(&raw)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) || errors.Is(err, sql.ErrNoRows) {
			return out, nil
		}
		return out, err
	}
	if len(raw) == 0 {
		return out, nil
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return emptyModSettings(), err
	}
	return out, nil
}

// UpsertModSettings writes site-level fields and preserves last delivery info.
func UpsertModSettings(modID string, next ModSettings) error {
	modID = strings.TrimSpace(modID)
	if modID == "" {
		return fmt.Errorf("mod id required")
	}
	prev, err := GetModSettings(modID)
	if err != nil {
		return err
	}
	next.LastError = prev.LastError
	next.LastDeliveryAt = prev.LastDeliveryAt
	return writeModSettings(modID, next)
}

func writeModSettings(modID string, data ModSettings) error {
	pool, err := OpenDatabase()
	if err != nil {
		return err
	}
	defer CloseDatabase(pool)

	raw, err := json.Marshal(data)
	if err != nil {
		return err
	}
	_, err = pool.Exec(context.Background(), `
		INSERT INTO mod_settings (mod_id, data, updated_at) VALUES ($1, $2, NOW())
		ON CONFLICT (mod_id) DO UPDATE SET data = EXCLUDED.data, updated_at = NOW()`,
		modID, raw)
	return err
}

// GetModProjectSettings returns per-project settings or empty defaults.
func GetModProjectSettings(modID string, projectID int) (ModProjectSettings, error) {
	out := emptyModProjectSettings()
	modID = strings.TrimSpace(modID)
	if modID == "" {
		return out, fmt.Errorf("mod id required")
	}
	if projectID <= 0 {
		return out, fmt.Errorf("project id required")
	}
	pool, err := OpenDatabase()
	if err != nil {
		return out, err
	}
	defer CloseDatabase(pool)

	var raw []byte
	err = pool.QueryRow(context.Background(),
		`SELECT data FROM mod_project_settings WHERE mod_id = $1 AND project_id = $2`,
		modID, projectID).Scan(&raw)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) || errors.Is(err, sql.ErrNoRows) {
			return out, nil
		}
		return out, err
	}
	if len(raw) == 0 {
		return out, nil
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return emptyModProjectSettings(), err
	}
	if out.Triggers == nil {
		out.Triggers = []string{}
	}
	if out.Templates == nil {
		out.Templates = map[string]string{}
	}
	return out, nil
}

// UpsertModProjectSettings writes owner-controlled project fields and preserves last delivery info.
func UpsertModProjectSettings(modID string, projectID int, next ModProjectSettings) error {
	modID = strings.TrimSpace(modID)
	if modID == "" {
		return fmt.Errorf("mod id required")
	}
	if projectID <= 0 {
		return fmt.Errorf("project id required")
	}
	prev, err := GetModProjectSettings(modID, projectID)
	if err != nil {
		return err
	}
	if next.Triggers == nil {
		next.Triggers = []string{}
	}
	if next.Templates == nil {
		next.Templates = map[string]string{}
	}
	next.LastError = prev.LastError
	next.LastDeliveryAt = prev.LastDeliveryAt
	return writeModProjectSettings(modID, projectID, next)
}

func writeModProjectSettings(modID string, projectID int, data ModProjectSettings) error {
	pool, err := OpenDatabase()
	if err != nil {
		return err
	}
	defer CloseDatabase(pool)

	raw, err := json.Marshal(data)
	if err != nil {
		return err
	}
	_, err = pool.Exec(context.Background(), `
		INSERT INTO mod_project_settings (mod_id, project_id, data, updated_at) VALUES ($1, $2, $3, NOW())
		ON CONFLICT (mod_id, project_id) DO UPDATE SET data = EXCLUDED.data, updated_at = NOW()`,
		modID, projectID, raw)
	return err
}

// RecordModProjectDelivery stores the last hook delivery attempt on the project settings row.
func RecordModProjectDelivery(modID string, projectID int, lastErr string) error {
	if projectID <= 0 {
		return fmt.Errorf("project id required")
	}
	s, err := GetModProjectSettings(modID, projectID)
	if err != nil {
		return err
	}
	s.LastError = lastErr
	s.LastDeliveryAt = time.Now().UTC().Format(time.RFC3339)
	return writeModProjectSettings(modID, projectID, s)
}

// SetModSecret encrypts and stores a secret. Empty plaintext is a no-op.
// projectID 0 is the unused site-level slot.
func SetModSecret(modID string, projectID int, key, plaintext string) error {
	modID = strings.TrimSpace(modID)
	key = strings.TrimSpace(key)
	if modID == "" || key == "" {
		return fmt.Errorf("mod id and key required")
	}
	if projectID < 0 {
		return fmt.Errorf("project id required")
	}
	plaintext = strings.TrimSpace(plaintext)
	if plaintext == "" {
		return nil
	}
	enc, err := secret.Encrypt(plaintext)
	if err != nil {
		return err
	}
	pool, err := OpenDatabase()
	if err != nil {
		return err
	}
	defer CloseDatabase(pool)
	_, err = pool.Exec(context.Background(), `
		INSERT INTO mod_secrets (mod_id, project_id, key, value_enc, updated_at) VALUES ($1, $2, $3, $4, NOW())
		ON CONFLICT (mod_id, project_id, key) DO UPDATE SET value_enc = EXCLUDED.value_enc, updated_at = NOW()`,
		modID, projectID, key, enc)
	return err
}

// GetModSecret decrypts a stored secret. Missing returns "".
func GetModSecret(modID string, projectID int, key string) (string, error) {
	modID = strings.TrimSpace(modID)
	key = strings.TrimSpace(key)
	if modID == "" || key == "" {
		return "", fmt.Errorf("mod id and key required")
	}
	pool, err := OpenDatabase()
	if err != nil {
		return "", err
	}
	defer CloseDatabase(pool)
	var enc string
	err = pool.QueryRow(context.Background(),
		`SELECT value_enc FROM mod_secrets WHERE mod_id = $1 AND project_id = $2 AND key = $3`,
		modID, projectID, key).Scan(&enc)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) || errors.Is(err, sql.ErrNoRows) {
			return "", nil
		}
		return "", err
	}
	if strings.TrimSpace(enc) == "" {
		return "", nil
	}
	return secret.Decrypt(enc)
}

// ModSecretIsSet reports whether a secret row exists.
func ModSecretIsSet(modID string, projectID int, key string) bool {
	pool, err := OpenDatabase()
	if err != nil {
		return false
	}
	defer CloseDatabase(pool)
	var n int
	_ = pool.QueryRow(context.Background(),
		`SELECT COUNT(*) FROM mod_secrets WHERE mod_id = $1 AND project_id = $2 AND key = $3 AND value_enc <> ''`,
		modID, projectID, key).Scan(&n)
	return n > 0
}

// GetHookTaskSnapshot loads template fields for a task.
func GetHookTaskSnapshot(taskID int) (*HookTaskSnapshot, error) {
	pool, err := OpenDatabase()
	if err != nil {
		return nil, err
	}
	defer CloseDatabase(pool)

	var s HookTaskSnapshot
	var projectID sql.NullInt64
	err = pool.QueryRow(context.Background(), `
		SELECT t.id, t.title, COALESCE(t.completed, false), COALESCE(t.priority, 0),
		       t.project_id, COALESCE(p.name, ''), COALESCE(ps.name, '')
		FROM tasks t
		LEFT JOIN projects p ON p.id = t.project_id
		LEFT JOIN project_statuses ps ON ps.id = t.status_id
		WHERE t.id = $1`, taskID).Scan(
		&s.ID, &s.Title, &s.Completed, &s.Priority, &projectID, &s.ProjectName, &s.StatusName)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) || errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("task not found")
		}
		return nil, err
	}
	if projectID.Valid {
		s.ProjectID = int(projectID.Int64)
	}
	return &s, nil
}
