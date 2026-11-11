package storage

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"GoTodo/internal/branding"

	"github.com/jackc/pgx/v5"
)

var (
	ErrAPIKeyNotFound   = errors.New("api key not found")
	ErrAPIKeyNameExists = errors.New("an API key with that name already exists")
)

// APIKey is a stored API key record (hash only; plaintext shown once at creation).
// Personal keys have a nil ProjectID and act with the owner's full access.
// Project keys act as their creator, limited to one project and Scopes.
type APIKey struct {
	ID         int
	UserID     int
	Name       string
	KeyPrefix  string
	CreatedAt  time.Time
	LastUsedAt *time.Time
	RevokedAt  *time.Time
	ProjectID  *int
	Scopes     []string
	ExpiresAt  *time.Time
	// Kind is "" for standard keys and APIKeyKindLocalAgent for local agent keys.
	Kind string
	// LimitProjectID optionally limits a local agent key to one project.
	LimitProjectID *int
	// CreatorName is filled by ListProjectAPIKeys for display.
	CreatorName string
	// LimitProjectName is filled by ListAPIKeysForUser for display.
	LimitProjectName string
}

// APIKeyKindLocalAgent marks a personal key made for an AI agent running on the
// owner's machine (Claude Code, Codex, Gemini CLI, ...). It acts as the owner,
// but only through the task allowlist and its scopes, optionally in one project.
const APIKeyKindLocalAgent = "local_agent"

// Project API key scopes.
const (
	APIScopeTasksRead     = "tasks:read"
	APIScopeTasksWrite    = "tasks:write"
	APIScopeCommentsWrite = "comments:write"
)

// APIKeyScopes lists every scope a project key may be granted, in display order.
var APIKeyScopes = []string{APIScopeTasksRead, APIScopeTasksWrite, APIScopeCommentsWrite}

// APIKeyPrincipal is the identity resolved from a Bearer key.
type APIKeyPrincipal struct {
	KeyID     int
	UserID    int
	ProjectID int // 0 for personal keys
	Scopes    []string
	// IsAgent is true when the key belongs to an AI agent account.
	IsAgent bool
	// IsLocalAgent is true for a personal local agent key (see APIKeyKindLocalAgent).
	IsLocalAgent bool
	// LimitProjectID is the one project a local agent key may reach, or 0 for
	// every project its owner belongs to.
	LimitProjectID int
}

// IsProjectScoped reports whether the key is limited to a single project.
func (p *APIKeyPrincipal) IsProjectScoped() bool {
	return p != nil && p.ProjectID > 0
}

// HasScope reports whether a project key was granted scope.
func (p *APIKeyPrincipal) HasScope(scope string) bool {
	if p == nil {
		return false
	}
	for _, s := range p.Scopes {
		if s == scope {
			return true
		}
	}
	return false
}

// CreateAPIKeysTable ensures the api_keys table exists.
func CreateAPIKeysTable() error {
	pool, err := OpenDatabase()
	if err != nil {
		return err
	}
	defer CloseDatabase(pool)

	_, err = pool.Exec(context.Background(), `
		CREATE TABLE IF NOT EXISTS api_keys (
			id SERIAL PRIMARY KEY,
			user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			name TEXT NOT NULL,
			key_hash TEXT NOT NULL UNIQUE,
			key_prefix TEXT NOT NULL,
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			last_used_at TIMESTAMPTZ,
			revoked_at TIMESTAMPTZ
		)
	`)
	if err != nil {
		return fmt.Errorf("failed to create api_keys table: %v", err)
	}
	_, err = pool.Exec(context.Background(),
		`CREATE INDEX IF NOT EXISTS idx_api_keys_user_id ON api_keys(user_id)`)
	if err != nil {
		return fmt.Errorf("failed to create api_keys index: %v", err)
	}
	for _, stmt := range []string{
		`ALTER TABLE api_keys ADD COLUMN IF NOT EXISTS project_id INTEGER REFERENCES projects(id) ON DELETE CASCADE`,
		`ALTER TABLE api_keys ADD COLUMN IF NOT EXISTS scopes TEXT[] NOT NULL DEFAULT '{}'`,
		`ALTER TABLE api_keys ADD COLUMN IF NOT EXISTS expires_at TIMESTAMPTZ`,
		`CREATE INDEX IF NOT EXISTS idx_api_keys_project_id ON api_keys(project_id) WHERE project_id IS NOT NULL`,
		`ALTER TABLE api_keys ADD COLUMN IF NOT EXISTS kind TEXT NOT NULL DEFAULT ''`,
		// CASCADE, never SET NULL: clearing the limit would widen the key to every project.
		`ALTER TABLE api_keys ADD COLUMN IF NOT EXISTS limit_project_id INTEGER REFERENCES projects(id) ON DELETE CASCADE`,
	} {
		if _, err := pool.Exec(context.Background(), stmt); err != nil {
			return fmt.Errorf("failed to migrate api_keys: %v", err)
		}
	}
	return nil
}

// HashAPIKey returns the SHA-256 hex digest of a raw API key.
func HashAPIKey(rawKey string) string {
	sum := sha256.Sum256([]byte(rawKey))
	return hex.EncodeToString(sum[:])
}

func newAPIKeyRaw() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return apiKeyPrefix() + "_" + hex.EncodeToString(b), nil
}

// apiKeyPrefix brands new keys with the site name ("Acme Tasks" -> "acmetasks_…").
func apiKeyPrefix() string {
	name := ""
	if s, err := GetSiteSettings(); err == nil && s != nil {
		name = s.SiteName
	}
	return branding.KeyPrefix(name)
}

// CreateAPIKey generates a new key, stores its hash, and returns the plaintext once.
func CreateAPIKey(userID int, name string) (plaintext string, record *APIKey, err error) {
	return createPersonalAPIKey(APIKey{UserID: userID, Name: name})
}

// CreateOrRotateLocalAgentAPIKey revokes any same-name personal keys, then
// mints a local agent key. Callers validate scopes, the project limit, and the
// expiry first.
func CreateOrRotateLocalAgentAPIKey(userID int, name string, scopes []string, limitProjectID *int, expiresAt *time.Time) (string, *APIKey, error) {
	if err := RevokeAPIKeysByName(userID, name); err != nil {
		return "", nil, err
	}
	return createPersonalAPIKey(APIKey{UserID: userID, Name: name, Kind: APIKeyKindLocalAgent,
		Scopes: scopes, LimitProjectID: limitProjectID, ExpiresAt: expiresAt})
}

func createPersonalAPIKey(k APIKey) (string, *APIKey, error) {
	pool, err := OpenDatabase()
	if err != nil {
		return "", nil, err
	}
	defer CloseDatabase(pool)

	plaintext, err := newAPIKeyRaw()
	if err != nil {
		return "", nil, err
	}
	prefix := plaintext
	if len(prefix) > 24 {
		prefix = prefix[:24] + "…"
	}
	k.KeyPrefix = prefix
	scopes := k.Scopes
	if scopes == nil {
		scopes = []string{}
	}
	err = pool.QueryRow(context.Background(),
		`INSERT INTO api_keys (user_id, name, key_hash, key_prefix, kind, scopes, limit_project_id, expires_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8) RETURNING id, created_at`,
		k.UserID, k.Name, HashAPIKey(plaintext), prefix, k.Kind, scopes, k.LimitProjectID, k.ExpiresAt).Scan(&k.ID, &k.CreatedAt)
	if err != nil {
		return "", nil, err
	}
	return plaintext, &k, nil
}

// ListAPIKeysForUser returns non-revoked keys for display (no hash).
func ListAPIKeysForUser(userID int) ([]APIKey, error) {
	pool, err := OpenDatabase()
	if err != nil {
		return nil, err
	}
	defer CloseDatabase(pool)

	rows, err := pool.Query(context.Background(),
		`SELECT ak.id, ak.user_id, ak.name, ak.key_prefix, ak.created_at, ak.last_used_at, ak.revoked_at,
		        ak.kind, ak.scopes, ak.limit_project_id, COALESCE(p.name, ''), ak.expires_at
		 FROM api_keys ak
		 LEFT JOIN projects p ON p.id = ak.limit_project_id
		 WHERE ak.user_id = $1 AND ak.project_id IS NULL AND ak.revoked_at IS NULL
		 ORDER BY ak.created_at DESC`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]APIKey, 0)
	for rows.Next() {
		var k APIKey
		if err := rows.Scan(&k.ID, &k.UserID, &k.Name, &k.KeyPrefix, &k.CreatedAt, &k.LastUsedAt, &k.RevokedAt,
			&k.Kind, &k.Scopes, &k.LimitProjectID, &k.LimitProjectName, &k.ExpiresAt); err != nil {
			return nil, err
		}
		out = append(out, k)
	}
	return out, rows.Err()
}

// RevokeAPIKeysByName revokes all active keys for a user that share the given name.
func RevokeAPIKeysByName(userID int, name string) error {
	pool, err := OpenDatabase()
	if err != nil {
		return err
	}
	defer CloseDatabase(pool)

	_, err = pool.Exec(context.Background(),
		`UPDATE api_keys SET revoked_at = NOW()
		 WHERE user_id = $1 AND name = $2 AND project_id IS NULL AND revoked_at IS NULL`,
		userID, name)
	return err
}

// CreateOrRotateAPIKey revokes any existing same-name keys, then creates a new one.
func CreateOrRotateAPIKey(userID int, name string) (plaintext string, record *APIKey, err error) {
	if err := RevokeAPIKeysByName(userID, name); err != nil {
		return "", nil, err
	}
	return CreateAPIKey(userID, name)
}

// RevokeAPIKey marks a key as revoked for the owning user.
func RevokeAPIKey(keyID, userID int) error {
	pool, err := OpenDatabase()
	if err != nil {
		return err
	}
	defer CloseDatabase(pool)

	tag, err := pool.Exec(context.Background(),
		`UPDATE api_keys SET revoked_at = NOW()
		 WHERE id = $1 AND user_id = $2 AND project_id IS NULL AND revoked_at IS NULL`,
		keyID, userID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrAPIKeyNotFound
	}
	return nil
}

// UpdateAPIKeyName renames an active key owned by the user. The secret is unchanged.
func UpdateAPIKeyName(keyID, userID int, name string) (*APIKey, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, fmt.Errorf("key name is required")
	}
	if len(name) > 80 {
		return nil, fmt.Errorf("key name is too long")
	}

	pool, err := OpenDatabase()
	if err != nil {
		return nil, err
	}
	defer CloseDatabase(pool)

	var exists bool
	err = pool.QueryRow(context.Background(),
		`SELECT EXISTS(
			SELECT 1 FROM api_keys
			WHERE user_id = $1 AND LOWER(name) = LOWER($2) AND project_id IS NULL AND revoked_at IS NULL AND id != $3
		)`, userID, name, keyID).Scan(&exists)
	if err != nil {
		return nil, err
	}
	if exists {
		return nil, ErrAPIKeyNameExists
	}

	var k APIKey
	err = pool.QueryRow(context.Background(),
		`UPDATE api_keys SET name = $3
		 WHERE id = $1 AND user_id = $2 AND project_id IS NULL AND revoked_at IS NULL
		 RETURNING id, user_id, name, key_prefix, created_at, last_used_at, revoked_at`,
		keyID, userID, name).Scan(
		&k.ID, &k.UserID, &k.Name, &k.KeyPrefix, &k.CreatedAt, &k.LastUsedAt, &k.RevokedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrAPIKeyNotFound
		}
		return nil, err
	}
	return &k, nil
}

// LookupAPIKey validates a bearer token and returns who it acts as. Revoked,
// expired, and banned-owner keys are rejected. last_used_at is bumped on success.
func LookupAPIKey(rawKey string) (*APIKeyPrincipal, error) {
	if rawKey == "" {
		return nil, fmt.Errorf("invalid key")
	}
	pool, err := OpenDatabase()
	if err != nil {
		return nil, err
	}
	defer CloseDatabase(pool)

	var p APIKeyPrincipal
	var projectID *int
	var isBanned bool
	var kind string
	var limitProjectID *int
	err = pool.QueryRow(context.Background(),
		`SELECT ak.id, ak.user_id, ak.project_id, ak.scopes, COALESCE(u.is_banned, FALSE), COALESCE(u.is_agent, FALSE),
		        ak.kind, ak.limit_project_id
		 FROM api_keys ak
		 JOIN users u ON u.id = ak.user_id
		 WHERE ak.key_hash = $1 AND ak.revoked_at IS NULL
		   AND (ak.expires_at IS NULL OR ak.expires_at > NOW())`,
		HashAPIKey(rawKey)).Scan(&p.KeyID, &p.UserID, &projectID, &p.Scopes, &isBanned, &p.IsAgent, &kind, &limitProjectID)
	if err != nil || isBanned {
		return nil, fmt.Errorf("invalid key")
	}
	if projectID != nil {
		p.ProjectID = *projectID
	}
	switch kind {
	case "":
	case APIKeyKindLocalAgent:
		// A local agent key is always a personal key of a human account;
		// anything else is a malformed row and fails closed.
		if projectID != nil || p.IsAgent {
			return nil, fmt.Errorf("invalid key")
		}
		p.IsLocalAgent = true
		if limitProjectID != nil {
			p.LimitProjectID = *limitProjectID
		}
	default:
		return nil, fmt.Errorf("invalid key")
	}

	_, _ = pool.Exec(context.Background(),
		`UPDATE api_keys SET last_used_at = NOW() WHERE id = $1`, p.KeyID)
	return &p, nil
}

// CreateProjectAPIKey mints a key limited to projectID and scopes, acting as userID.
func CreateProjectAPIKey(projectID, userID int, name string, scopes []string, expiresAt *time.Time) (plaintext string, record *APIKey, err error) {
	pool, err := OpenDatabase()
	if err != nil {
		return "", nil, err
	}
	defer CloseDatabase(pool)

	var exists bool
	err = pool.QueryRow(context.Background(),
		`SELECT EXISTS(
			SELECT 1 FROM api_keys
			WHERE project_id = $1 AND LOWER(name) = LOWER($2) AND revoked_at IS NULL
		)`, projectID, name).Scan(&exists)
	if err != nil {
		return "", nil, err
	}
	if exists {
		return "", nil, ErrAPIKeyNameExists
	}

	plaintext, err = newAPIKeyRaw()
	if err != nil {
		return "", nil, err
	}
	prefix := plaintext
	if len(prefix) > 24 {
		prefix = prefix[:24] + "…"
	}
	k := APIKey{
		UserID:    userID,
		Name:      name,
		KeyPrefix: prefix,
		ProjectID: &projectID,
		Scopes:    scopes,
		ExpiresAt: expiresAt,
	}
	err = pool.QueryRow(context.Background(),
		`INSERT INTO api_keys (user_id, name, key_hash, key_prefix, project_id, scopes, expires_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7) RETURNING id, created_at`,
		userID, name, HashAPIKey(plaintext), prefix, projectID, scopes, expiresAt).Scan(&k.ID, &k.CreatedAt)
	if err != nil {
		return "", nil, err
	}
	return plaintext, &k, nil
}

// ListProjectAPIKeys returns a project's active keys (expired ones included so
// managers can see and clean them up).
func ListProjectAPIKeys(projectID int) ([]APIKey, error) {
	pool, err := OpenDatabase()
	if err != nil {
		return nil, err
	}
	defer CloseDatabase(pool)

	rows, err := pool.Query(context.Background(),
		`SELECT ak.id, ak.user_id, ak.name, ak.key_prefix, ak.created_at, ak.last_used_at,
		        ak.project_id, ak.scopes, ak.expires_at,
		        COALESCE(NULLIF(u.full_name, ''), NULLIF(u.user_name, ''), u.email, '')
		 FROM api_keys ak
		 JOIN users u ON u.id = ak.user_id
		 WHERE ak.project_id = $1 AND ak.revoked_at IS NULL AND NOT COALESCE(u.is_agent, FALSE)
		 ORDER BY ak.created_at DESC`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]APIKey, 0)
	for rows.Next() {
		var k APIKey
		if err := rows.Scan(&k.ID, &k.UserID, &k.Name, &k.KeyPrefix, &k.CreatedAt, &k.LastUsedAt,
			&k.ProjectID, &k.Scopes, &k.ExpiresAt, &k.CreatorName); err != nil {
			return nil, err
		}
		out = append(out, k)
	}
	return out, rows.Err()
}

// RevokeProjectAPIKey revokes one of a project's keys.
func RevokeProjectAPIKey(projectID, keyID int) error {
	pool, err := OpenDatabase()
	if err != nil {
		return err
	}
	defer CloseDatabase(pool)

	tag, err := pool.Exec(context.Background(),
		`UPDATE api_keys SET revoked_at = NOW()
		 WHERE id = $1 AND project_id = $2 AND revoked_at IS NULL`,
		keyID, projectID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrAPIKeyNotFound
	}
	return nil
}
