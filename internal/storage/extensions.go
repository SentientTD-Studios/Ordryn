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

// ExtensionSettings is the site-level JSON document stored per extension_id.
type ExtensionSettings struct {
	Enabled        bool              `json:"enabled"`
	AllProjects    bool              `json:"all_projects,omitempty"`
	ProjectIDs     []int             `json:"project_ids,omitempty"`
	Triggers       []string          `json:"triggers,omitempty"`
	Templates      map[string]string `json:"templates,omitempty"`
	StatusOnly     bool              `json:"status_only,omitempty"`
	LastError      string            `json:"last_error,omitempty"`
	LastDeliveryAt string            `json:"last_delivery_at,omitempty"`
}

// ExtensionProjectSettings is the per-project JSON document for an extension.
type ExtensionProjectSettings struct {
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

// CreateExtensionTables creates extension settings and secret tables.
func CreateExtensionTables() error {
	pool, err := OpenDatabase()
	if err != nil {
		return err
	}
	defer CloseDatabase(pool)

	if err := migrateRenameModTables(pool); err != nil {
		return err
	}

	stmts := []string{
		`CREATE TABLE IF NOT EXISTS extension_settings (
			extension_id VARCHAR(64) PRIMARY KEY,
			data JSONB NOT NULL DEFAULT '{}',
			updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)`,
		`CREATE TABLE IF NOT EXISTS extension_secrets (
			extension_id VARCHAR(64) NOT NULL,
			project_id INTEGER NOT NULL DEFAULT 0,
			key VARCHAR(64) NOT NULL,
			value_enc TEXT NOT NULL,
			updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			PRIMARY KEY (extension_id, project_id, key)
		)`,
		`CREATE TABLE IF NOT EXISTS extension_project_settings (
			extension_id VARCHAR(64) NOT NULL,
			project_id INTEGER NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
			data JSONB NOT NULL DEFAULT '{}',
			updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			PRIMARY KEY (extension_id, project_id)
		)`,
	}
	for _, q := range stmts {
		if _, err := pool.Exec(context.Background(), q); err != nil {
			return fmt.Errorf("create extension tables: %w", err)
		}
	}
	if err := migrateExtensionSecretColumns(pool); err != nil {
		return err
	}
	return nil
}

func migrateRenameModTables(pool *pgxpool.Pool) error {
	renames := [][2]string{
		{"mod_settings", "extension_settings"},
		{"mod_secrets", "extension_secrets"},
		{"mod_project_settings", "extension_project_settings"},
	}
	for _, pair := range renames {
		if err := renameTableIfNeeded(pool, pair[0], pair[1]); err != nil {
			return err
		}
	}
	if err := renameColumnIfNeeded(pool, "extension_settings", "mod_id", "extension_id"); err != nil {
		return err
	}
	if err := renameColumnIfNeeded(pool, "extension_secrets", "mod_id", "extension_id"); err != nil {
		return err
	}
	if err := renameColumnIfNeeded(pool, "extension_project_settings", "mod_id", "extension_id"); err != nil {
		return err
	}
	return nil
}

func migrateExtensionSecretColumns(pool *pgxpool.Pool) error {
	ctx := context.Background()
	if _, err := pool.Exec(ctx, `ALTER TABLE extension_secrets ADD COLUMN IF NOT EXISTS project_id INTEGER NOT NULL DEFAULT 0`); err != nil {
		return fmt.Errorf("extension_secrets project_id: %w", err)
	}
	if _, err := pool.Exec(ctx, `ALTER TABLE extension_secrets DROP CONSTRAINT IF EXISTS extension_secrets_pkey`); err != nil {
		return fmt.Errorf("extension_secrets drop pkey: %w", err)
	}
	if _, err := pool.Exec(ctx, `ALTER TABLE extension_secrets DROP CONSTRAINT IF EXISTS mod_secrets_pkey`); err != nil {
		return fmt.Errorf("extension_secrets drop legacy pkey: %w", err)
	}
	if _, err := pool.Exec(ctx, `ALTER TABLE extension_secrets ADD PRIMARY KEY (extension_id, project_id, key)`); err != nil {
		return fmt.Errorf("extension_secrets pkey: %w", err)
	}
	return nil
}

func renameTableIfNeeded(pool *pgxpool.Pool, from, to string) error {
	if !relationExists(pool, from) || relationExists(pool, to) {
		return nil
	}
	_, err := pool.Exec(context.Background(), fmt.Sprintf(`ALTER TABLE %s RENAME TO %s`, from, to))
	if err != nil {
		return fmt.Errorf("rename %s to %s: %w", from, to, err)
	}
	return nil
}

func renameColumnIfNeeded(pool *pgxpool.Pool, table, from, to string) error {
	if !relationExists(pool, table) || !columnExists(pool, table, from) || columnExists(pool, table, to) {
		return nil
	}
	_, err := pool.Exec(context.Background(), fmt.Sprintf(`ALTER TABLE %s RENAME COLUMN %s TO %s`, table, from, to))
	if err != nil {
		return fmt.Errorf("rename %s.%s: %w", table, from, err)
	}
	return nil
}

func relationExists(pool *pgxpool.Pool, name string) bool {
	var n int
	_ = pool.QueryRow(context.Background(),
		`SELECT COUNT(*) FROM pg_class c
		 JOIN pg_namespace n ON n.oid = c.relnamespace
		 WHERE n.nspname = 'public' AND c.relname = $1 AND c.relkind = 'r'`, name).Scan(&n)
	return n > 0
}

func columnExists(pool *pgxpool.Pool, table, col string) bool {
	var n int
	_ = pool.QueryRow(context.Background(),
		`SELECT COUNT(*) FROM information_schema.columns
		 WHERE table_schema = 'public' AND table_name = $1 AND column_name = $2`, table, col).Scan(&n)
	return n > 0
}

func emptyExtensionSettings() ExtensionSettings {
	return ExtensionSettings{}
}

func emptyExtensionProjectSettings() ExtensionProjectSettings {
	return ExtensionProjectSettings{
		Triggers:  []string{},
		Templates: map[string]string{},
	}
}

// GetExtensionSettings returns stored site settings or empty defaults.
func GetExtensionSettings(extensionID string) (ExtensionSettings, error) {
	out := emptyExtensionSettings()
	extensionID = strings.TrimSpace(extensionID)
	if extensionID == "" {
		return out, fmt.Errorf("extension id required")
	}
	pool, err := OpenDatabase()
	if err != nil {
		return out, err
	}
	defer CloseDatabase(pool)

	var raw []byte
	err = pool.QueryRow(context.Background(),
		`SELECT data FROM extension_settings WHERE extension_id = $1`, extensionID).Scan(&raw)
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
		return emptyExtensionSettings(), err
	}
	return out, nil
}

// UpsertExtensionSettings writes site-level fields and preserves last delivery info.
func UpsertExtensionSettings(extensionID string, next ExtensionSettings) error {
	extensionID = strings.TrimSpace(extensionID)
	if extensionID == "" {
		return fmt.Errorf("extension id required")
	}
	prev, err := GetExtensionSettings(extensionID)
	if err != nil {
		return err
	}
	next.LastError = prev.LastError
	next.LastDeliveryAt = prev.LastDeliveryAt
	return writeExtensionSettings(extensionID, next)
}

func writeExtensionSettings(extensionID string, data ExtensionSettings) error {
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
		INSERT INTO extension_settings (extension_id, data, updated_at) VALUES ($1, $2, NOW())
		ON CONFLICT (extension_id) DO UPDATE SET data = EXCLUDED.data, updated_at = NOW()`,
		extensionID, raw)
	return err
}

// GetExtensionProjectSettings returns per-project settings or empty defaults.
func GetExtensionProjectSettings(extensionID string, projectID int) (ExtensionProjectSettings, error) {
	out := emptyExtensionProjectSettings()
	extensionID = strings.TrimSpace(extensionID)
	if extensionID == "" {
		return out, fmt.Errorf("extension id required")
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
		`SELECT data FROM extension_project_settings WHERE extension_id = $1 AND project_id = $2`,
		extensionID, projectID).Scan(&raw)
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
		return emptyExtensionProjectSettings(), err
	}
	if out.Triggers == nil {
		out.Triggers = []string{}
	}
	if out.Templates == nil {
		out.Templates = map[string]string{}
	}
	return out, nil
}

// UpsertExtensionProjectSettings writes owner-controlled project fields and preserves last delivery info.
func UpsertExtensionProjectSettings(extensionID string, projectID int, next ExtensionProjectSettings) error {
	extensionID = strings.TrimSpace(extensionID)
	if extensionID == "" {
		return fmt.Errorf("extension id required")
	}
	if projectID <= 0 {
		return fmt.Errorf("project id required")
	}
	prev, err := GetExtensionProjectSettings(extensionID, projectID)
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
	return writeExtensionProjectSettings(extensionID, projectID, next)
}

func writeExtensionProjectSettings(extensionID string, projectID int, data ExtensionProjectSettings) error {
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
		INSERT INTO extension_project_settings (extension_id, project_id, data, updated_at) VALUES ($1, $2, $3, NOW())
		ON CONFLICT (extension_id, project_id) DO UPDATE SET data = EXCLUDED.data, updated_at = NOW()`,
		extensionID, projectID, raw)
	return err
}

// RecordExtensionProjectDelivery stores the last hook delivery attempt on the project settings row.
func RecordExtensionProjectDelivery(extensionID string, projectID int, lastErr string) error {
	if projectID <= 0 {
		return fmt.Errorf("project id required")
	}
	s, err := GetExtensionProjectSettings(extensionID, projectID)
	if err != nil {
		return err
	}
	s.LastError = lastErr
	s.LastDeliveryAt = time.Now().UTC().Format(time.RFC3339)
	return writeExtensionProjectSettings(extensionID, projectID, s)
}

// SetExtensionSecret encrypts and stores a secret. Empty plaintext is a no-op.
// projectID 0 is the unused site-level slot.
func SetExtensionSecret(extensionID string, projectID int, key, plaintext string) error {
	extensionID = strings.TrimSpace(extensionID)
	key = strings.TrimSpace(key)
	if extensionID == "" || key == "" {
		return fmt.Errorf("extension id and key required")
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
		INSERT INTO extension_secrets (extension_id, project_id, key, value_enc, updated_at) VALUES ($1, $2, $3, $4, NOW())
		ON CONFLICT (extension_id, project_id, key) DO UPDATE SET value_enc = EXCLUDED.value_enc, updated_at = NOW()`,
		extensionID, projectID, key, enc)
	return err
}

// GetExtensionSecret decrypts a stored secret. Missing returns "".
func GetExtensionSecret(extensionID string, projectID int, key string) (string, error) {
	extensionID = strings.TrimSpace(extensionID)
	key = strings.TrimSpace(key)
	if extensionID == "" || key == "" {
		return "", fmt.Errorf("extension id and key required")
	}
	pool, err := OpenDatabase()
	if err != nil {
		return "", err
	}
	defer CloseDatabase(pool)
	var enc string
	err = pool.QueryRow(context.Background(),
		`SELECT value_enc FROM extension_secrets WHERE extension_id = $1 AND project_id = $2 AND key = $3`,
		extensionID, projectID, key).Scan(&enc)
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

// ExtensionSecretIsSet reports whether a secret row exists.
func ExtensionSecretIsSet(extensionID string, projectID int, key string) bool {
	pool, err := OpenDatabase()
	if err != nil {
		return false
	}
	defer CloseDatabase(pool)
	var n int
	_ = pool.QueryRow(context.Background(),
		`SELECT COUNT(*) FROM extension_secrets WHERE extension_id = $1 AND project_id = $2 AND key = $3 AND value_enc <> ''`,
		extensionID, projectID, key).Scan(&n)
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
