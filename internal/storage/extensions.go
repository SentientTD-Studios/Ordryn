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
	Enabled         bool              `json:"enabled"`
	Triggers        []string          `json:"triggers"`
	Templates       map[string]string `json:"templates"`
	StatusOnly      bool              `json:"status_only"`
	SkipSelf        bool              `json:"skip_self,omitempty"`
	MinPriority     int               `json:"min_priority,omitempty"`
	TagIDs          []int             `json:"tag_ids,omitempty"`
	ClaimedOnly     bool              `json:"claimed_only,omitempty"`
	FieldKey        string            `json:"field_key,omitempty"`
	FieldValue      string            `json:"field_value,omitempty"`
	QuietHoursStart string            `json:"quiet_hours_start,omitempty"`
	QuietHoursEnd   string            `json:"quiet_hours_end,omitempty"`
	Digest          string            `json:"digest,omitempty"`
	MentionMap      map[string]string `json:"mention_map,omitempty"`
	LastError       string            `json:"last_error,omitempty"`
	LastDeliveryAt  string            `json:"last_delivery_at,omitempty"`
}

// ExtensionMemberSettings is per-user destination config (project or personal inbox).
type ExtensionMemberSettings struct {
	Enabled         bool              `json:"enabled"`
	Triggers        []string          `json:"triggers"`
	Templates       map[string]string `json:"templates"`
	SkipSelf        *bool             `json:"skip_self,omitempty"`
	StatusOnly      bool              `json:"status_only,omitempty"`
	MinPriority     int               `json:"min_priority,omitempty"`
	TagIDs          []int             `json:"tag_ids,omitempty"`
	ClaimedOnly     bool              `json:"claimed_only,omitempty"`
	ClaimedIsMe     bool              `json:"claimed_is_me,omitempty"`
	FieldKey        string            `json:"field_key,omitempty"`
	FieldValue      string            `json:"field_value,omitempty"`
	QuietHoursStart string            `json:"quiet_hours_start,omitempty"`
	QuietHoursEnd   string            `json:"quiet_hours_end,omitempty"`
	Digest          string            `json:"digest,omitempty"`
	LastError       string            `json:"last_error,omitempty"`
	LastDeliveryAt  string            `json:"last_delivery_at,omitempty"`
}

// SkipSelfOrDefault is true when skip_self is unset (member/personal default).
func (s ExtensionMemberSettings) SkipSelfOrDefault() bool {
	if s.SkipSelf == nil {
		return true
	}
	return *s.SkipSelf
}

// HookTaskSnapshot is the task view used when rendering hook templates.
type HookTaskSnapshot struct {
	ID            int
	Title         string
	Completed     bool
	Priority      int
	ProjectID     int
	ProjectName   string
	WorkflowMode  string
	StatusName    string
	OwnerID       int
	ClaimedBy     int
	ClaimedByName string
	DueDate       string
	SprintID      int
	SprintName    string
	Tags          []string
	TagIDs        []int
	CustomFields  map[string]string
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
		`CREATE TABLE IF NOT EXISTS extension_member_settings (
			extension_id VARCHAR(64) NOT NULL,
			project_id INTEGER NOT NULL DEFAULT 0,
			user_id INTEGER NOT NULL,
			data JSONB NOT NULL DEFAULT '{}',
			updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			PRIMARY KEY (extension_id, project_id, user_id)
		)`,
		`CREATE TABLE IF NOT EXISTS hook_overdue_sent (
			task_id INTEGER NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
			due_date DATE NOT NULL,
			sent_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			PRIMARY KEY (task_id, due_date)
		)`,
		`CREATE TABLE IF NOT EXISTS extension_deliveries (
			id BIGSERIAL PRIMARY KEY,
			extension_id VARCHAR(64) NOT NULL,
			project_id INTEGER NOT NULL DEFAULT 0,
			user_id INTEGER NOT NULL DEFAULT 0,
			task_id INTEGER NOT NULL DEFAULT 0,
			event_type VARCHAR(64) NOT NULL DEFAULT '',
			event_id VARCHAR(64) NOT NULL DEFAULT '',
			url_host TEXT NOT NULL DEFAULT '',
			status VARCHAR(32) NOT NULL DEFAULT 'pending',
			http_code INTEGER NOT NULL DEFAULT 0,
			error TEXT NOT NULL DEFAULT '',
			attempts INTEGER NOT NULL DEFAULT 0,
			coalesce_key TEXT NOT NULL DEFAULT '',
			payload TEXT NOT NULL DEFAULT '',
			next_attempt_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)`,
		`CREATE TABLE IF NOT EXISTS project_inbound_webhooks (
			project_id INTEGER PRIMARY KEY REFERENCES projects(id) ON DELETE CASCADE,
			secret_enc TEXT NOT NULL DEFAULT '',
			enabled BOOLEAN NOT NULL DEFAULT FALSE,
			allow_create BOOLEAN NOT NULL DEFAULT TRUE,
			allow_comment BOOLEAN NOT NULL DEFAULT TRUE,
			last_error TEXT NOT NULL DEFAULT '',
			last_delivery_at TIMESTAMPTZ
		)`,
		`CREATE TABLE IF NOT EXISTS hook_task_messages (
			extension_id VARCHAR(64) NOT NULL,
			project_id INTEGER NOT NULL,
			task_id INTEGER NOT NULL,
			message_id TEXT NOT NULL,
			updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			PRIMARY KEY (extension_id, project_id, task_id)
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
	if _, err := pool.Exec(ctx, `ALTER TABLE extension_secrets ADD COLUMN IF NOT EXISTS user_id INTEGER NOT NULL DEFAULT 0`); err != nil {
		return fmt.Errorf("extension_secrets user_id: %w", err)
	}
	if _, err := pool.Exec(ctx, `ALTER TABLE extension_secrets DROP CONSTRAINT IF EXISTS extension_secrets_pkey`); err != nil {
		return fmt.Errorf("extension_secrets drop pkey: %w", err)
	}
	if _, err := pool.Exec(ctx, `ALTER TABLE extension_secrets DROP CONSTRAINT IF EXISTS mod_secrets_pkey`); err != nil {
		return fmt.Errorf("extension_secrets drop legacy pkey: %w", err)
	}
	if _, err := pool.Exec(ctx, `ALTER TABLE extension_secrets ADD PRIMARY KEY (extension_id, project_id, user_id, key)`); err != nil {
		return fmt.Errorf("extension_secrets pkey: %w", err)
	}
	_, _ = pool.Exec(ctx, `CREATE INDEX IF NOT EXISTS idx_extension_deliveries_pending ON extension_deliveries (status, next_attempt_at)`)
	_, _ = pool.Exec(ctx, `CREATE INDEX IF NOT EXISTS idx_extension_deliveries_lookup ON extension_deliveries (extension_id, project_id, user_id, created_at DESC)`)
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

// RecordExtensionSiteDelivery stores last site-level delivery on extension_settings.
func RecordExtensionSiteDelivery(extensionID, lastErr string) error {
	s, err := GetExtensionSettings(extensionID)
	if err != nil {
		return err
	}
	s.LastError = lastErr
	s.LastDeliveryAt = time.Now().UTC().Format(time.RFC3339)
	return writeExtensionSettings(extensionID, s)
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

const SigningSecretKey = "signing_secret"

// SetExtensionSecret encrypts and stores a team/site secret (user_id 0). Empty plaintext is a no-op.
func SetExtensionSecret(extensionID string, projectID int, key, plaintext string) error {
	return SetExtensionSecretForUser(extensionID, projectID, 0, key, plaintext)
}

// SetExtensionSecretForUser encrypts and stores a secret. userID 0 is the team/site slot.
func SetExtensionSecretForUser(extensionID string, projectID, userID int, key, plaintext string) error {
	extensionID = strings.TrimSpace(extensionID)
	key = strings.TrimSpace(key)
	if extensionID == "" || key == "" {
		return fmt.Errorf("extension id and key required")
	}
	if projectID < 0 || userID < 0 {
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
		INSERT INTO extension_secrets (extension_id, project_id, user_id, key, value_enc, updated_at) VALUES ($1, $2, $3, $4, $5, NOW())
		ON CONFLICT (extension_id, project_id, user_id, key) DO UPDATE SET value_enc = EXCLUDED.value_enc, updated_at = NOW()`,
		extensionID, projectID, userID, key, enc)
	return err
}

// GetExtensionSecret decrypts a team/site secret (user_id 0). Missing returns "".
func GetExtensionSecret(extensionID string, projectID int, key string) (string, error) {
	return GetExtensionSecretForUser(extensionID, projectID, 0, key)
}

// GetExtensionSecretForUser decrypts a stored secret. Missing returns "".
func GetExtensionSecretForUser(extensionID string, projectID, userID int, key string) (string, error) {
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
		`SELECT value_enc FROM extension_secrets WHERE extension_id = $1 AND project_id = $2 AND user_id = $3 AND key = $4`,
		extensionID, projectID, userID, key).Scan(&enc)
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

// ExtensionSecretIsSet reports whether a team/site secret row exists.
func ExtensionSecretIsSet(extensionID string, projectID int, key string) bool {
	return ExtensionSecretIsSetForUser(extensionID, projectID, 0, key)
}

// ExtensionSecretIsSetForUser reports whether a secret row exists.
func ExtensionSecretIsSetForUser(extensionID string, projectID, userID int, key string) bool {
	pool, err := OpenDatabase()
	if err != nil {
		return false
	}
	defer CloseDatabase(pool)
	var n int
	_ = pool.QueryRow(context.Background(),
		`SELECT COUNT(*) FROM extension_secrets WHERE extension_id = $1 AND project_id = $2 AND user_id = $3 AND key = $4 AND value_enc <> ''`,
		extensionID, projectID, userID, key).Scan(&n)
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
		       t.project_id, COALESCE(p.name, ''), COALESCE(p.workflow_mode, 'classic'), COALESCE(ps.name, ''),
		       t.user_id, COALESCE(t.claimed_by, 0), COALESCE(u.user_name, u.email, ''),
		       COALESCE(CAST(t.due_date AS TEXT), ''), COALESCE(t.sprint_id, 0), COALESCE(sp.name, '')
		FROM tasks t
		LEFT JOIN projects p ON p.id = t.project_id
		LEFT JOIN project_statuses ps ON ps.id = t.status_id
		LEFT JOIN users u ON u.id = t.claimed_by
		LEFT JOIN project_sprints sp ON sp.id = t.sprint_id
		WHERE t.id = $1`, taskID).Scan(
		&s.ID, &s.Title, &s.Completed, &s.Priority, &projectID, &s.ProjectName, &s.WorkflowMode, &s.StatusName,
		&s.OwnerID, &s.ClaimedBy, &s.ClaimedByName, &s.DueDate, &s.SprintID, &s.SprintName)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) || errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("task not found")
		}
		return nil, err
	}
	if projectID.Valid {
		s.ProjectID = int(projectID.Int64)
	}
	if tags, err := GetTagsForTask(taskID); err == nil {
		s.Tags = make([]string, 0, len(tags))
		s.TagIDs = make([]int, 0, len(tags))
		for _, tg := range tags {
			s.Tags = append(s.Tags, tg.Name)
			s.TagIDs = append(s.TagIDs, tg.ID)
		}
	}
	if fields, err := GetCustomFieldValuesForTasks([]int{taskID}); err == nil {
		if raw, ok := fields[taskID]; ok {
			s.CustomFields = customFieldsToStrings(raw)
		}
	}
	return &s, nil
}

func customFieldsToStrings(raw map[string]json.RawMessage) map[string]string {
	if len(raw) == 0 {
		return nil
	}
	out := make(map[string]string, len(raw))
	for k, v := range raw {
		s := strings.TrimSpace(string(v))
		s = strings.Trim(s, `"`)
		out[k] = s
	}
	return out
}

// GetHookProjectSnapshot loads project name for project-level hook events.
func GetHookProjectSnapshot(projectID int) (*HookTaskSnapshot, error) {
	if projectID <= 0 {
		return nil, fmt.Errorf("project required")
	}
	pool, err := OpenDatabase()
	if err != nil {
		return nil, err
	}
	defer CloseDatabase(pool)
	var s HookTaskSnapshot
	err = pool.QueryRow(context.Background(),
		`SELECT id, name, user_id, COALESCE(workflow_mode, 'classic') FROM projects WHERE id = $1`, projectID).Scan(&s.ProjectID, &s.ProjectName, &s.OwnerID, &s.WorkflowMode)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) || errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("project not found")
		}
		return nil, err
	}
	return &s, nil
}
