package storage

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"time"
)

// APIKey is a stored API key record (hash only; plaintext shown once at creation).
type APIKey struct {
	ID         int
	UserID     int
	Name       string
	KeyPrefix  string
	CreatedAt  time.Time
	LastUsedAt *time.Time
	RevokedAt  *time.Time
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
	return "gotodo_" + hex.EncodeToString(b), nil
}

// CreateAPIKey generates a new key, stores its hash, and returns the plaintext once.
func CreateAPIKey(userID int, name string) (plaintext string, record *APIKey, err error) {
	pool, err := OpenDatabase()
	if err != nil {
		return "", nil, err
	}
	defer CloseDatabase(pool)

	plaintext, err = newAPIKeyRaw()
	if err != nil {
		return "", nil, err
	}
	hash := HashAPIKey(plaintext)
	prefix := plaintext
	if len(prefix) > 24 {
		prefix = prefix[:24] + "…"
	}

	var id int
	var createdAt time.Time
	err = pool.QueryRow(context.Background(),
		`INSERT INTO api_keys (user_id, name, key_hash, key_prefix)
		 VALUES ($1, $2, $3, $4) RETURNING id, created_at`,
		userID, name, hash, prefix).Scan(&id, &createdAt)
	if err != nil {
		return "", nil, err
	}
	return plaintext, &APIKey{
		ID:        id,
		UserID:    userID,
		Name:      name,
		KeyPrefix: prefix,
		CreatedAt: createdAt,
	}, nil
}

// ListAPIKeysForUser returns non-revoked keys for display (no hash).
func ListAPIKeysForUser(userID int) ([]APIKey, error) {
	pool, err := OpenDatabase()
	if err != nil {
		return nil, err
	}
	defer CloseDatabase(pool)

	rows, err := pool.Query(context.Background(),
		`SELECT id, user_id, name, key_prefix, created_at, last_used_at, revoked_at
		 FROM api_keys WHERE user_id = $1 AND revoked_at IS NULL
		 ORDER BY created_at DESC`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]APIKey, 0)
	for rows.Next() {
		var k APIKey
		if err := rows.Scan(&k.ID, &k.UserID, &k.Name, &k.KeyPrefix, &k.CreatedAt, &k.LastUsedAt, &k.RevokedAt); err != nil {
			return nil, err
		}
		out = append(out, k)
	}
	return out, nil
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
		 WHERE user_id = $1 AND name = $2 AND revoked_at IS NULL`,
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
		`UPDATE api_keys SET revoked_at = NOW() WHERE id = $1 AND user_id = $2 AND revoked_at IS NULL`,
		keyID, userID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("api key not found")
	}
	return nil
}

// LookupAPIKeyUserID validates a bearer token and returns the owning user ID.
func LookupAPIKeyUserID(rawKey string) (userID int, err error) {
	if rawKey == "" {
		return 0, fmt.Errorf("invalid key")
	}
	pool, err := OpenDatabase()
	if err != nil {
		return 0, err
	}
	defer CloseDatabase(pool)

	hash := HashAPIKey(rawKey)
	var isBanned bool
	err = pool.QueryRow(context.Background(),
		`SELECT ak.user_id, COALESCE(u.is_banned, FALSE)
		 FROM api_keys ak
		 JOIN users u ON u.id = ak.user_id
		 WHERE ak.key_hash = $1 AND ak.revoked_at IS NULL`,
		hash).Scan(&userID, &isBanned)
	if err != nil {
		return 0, fmt.Errorf("invalid key")
	}
	if isBanned {
		return 0, fmt.Errorf("invalid key")
	}

	_, _ = pool.Exec(context.Background(),
		`UPDATE api_keys SET last_used_at = NOW() WHERE key_hash = $1`, hash)
	return userID, nil
}
