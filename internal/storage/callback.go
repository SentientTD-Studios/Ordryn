package storage

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
)

const CallbackTokenPrefix = "odycb_"

// ExtensionCallbackToken is a hashed project-scoped callback credential.
type ExtensionCallbackToken struct {
	ExtensionID string
	ProjectID   int
	UserID      int
}

func hashCallbackToken(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}

func newCallbackTokenRaw() (string, error) {
	b := make([]byte, 24)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return CallbackTokenPrefix + hex.EncodeToString(b), nil
}

// RotateCallbackToken replaces the destination token and returns the plaintext once.
func RotateCallbackToken(extensionID string, projectID, userID int) (string, error) {
	extensionID = strings.TrimSpace(extensionID)
	if extensionID == "" {
		return "", fmt.Errorf("extension required")
	}
	raw, err := newCallbackTokenRaw()
	if err != nil {
		return "", err
	}
	if err := replaceCallbackHash(extensionID, projectID, userID, raw); err != nil {
		return "", err
	}
	if err := SetExtensionSecretForUser(extensionID, projectID, userID, CallbackSecretKey, raw); err != nil {
		return "", err
	}
	return raw, nil
}

func replaceCallbackHash(extensionID string, projectID, userID int, raw string) error {
	pool, err := OpenDatabase()
	if err != nil {
		return err
	}
	defer CloseDatabase(pool)
	tx, err := pool.Begin(context.Background())
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	if _, err := tx.Exec(context.Background(),
		`DELETE FROM extension_callback_tokens WHERE extension_id = $1 AND project_id = $2 AND user_id = $3`,
		extensionID, projectID, userID); err != nil {
		return err
	}
	if _, err := tx.Exec(context.Background(),
		`INSERT INTO extension_callback_tokens (token_hash, extension_id, project_id, user_id, created_at)
		 VALUES ($1, $2, $3, $4, NOW())`,
		hashCallbackToken(raw), extensionID, projectID, userID); err != nil {
		return err
	}
	return tx.Commit(context.Background())
}

// GetCallbackTokenLookup resolves a plaintext token to its destination.
func GetCallbackTokenLookup(raw string) (*ExtensionCallbackToken, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" || !strings.HasPrefix(raw, CallbackTokenPrefix) {
		return nil, fmt.Errorf("token not found")
	}
	pool, err := OpenDatabase()
	if err != nil {
		return nil, err
	}
	defer CloseDatabase(pool)
	var t ExtensionCallbackToken
	err = pool.QueryRow(context.Background(),
		`SELECT extension_id, project_id, user_id FROM extension_callback_tokens WHERE token_hash = $1`,
		hashCallbackToken(raw)).Scan(&t.ExtensionID, &t.ProjectID, &t.UserID)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, fmt.Errorf("token not found")
		}
		return nil, err
	}
	return &t, nil
}

// CallbackTokenIsSet reports whether a destination already has a callback token.
func CallbackTokenIsSet(extensionID string, projectID, userID int) bool {
	return ExtensionSecretIsSetForUser(extensionID, projectID, userID, CallbackSecretKey)
}

// EnsureCallbackToken returns the plaintext token for a destination, minting one if needed.
func EnsureCallbackToken(extensionID string, projectID, userID int) (string, error) {
	cur, err := GetExtensionSecretForUser(extensionID, projectID, userID, CallbackSecretKey)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(cur) != "" {
		return cur, nil
	}
	return RotateCallbackToken(extensionID, projectID, userID)
}
