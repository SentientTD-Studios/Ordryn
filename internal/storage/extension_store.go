package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

// ExtensionStoreDoc is one project-scoped JSON document for an extension.
type ExtensionStoreDoc struct {
	ExtensionID string          `json:"extension_id"`
	ProjectID   int             `json:"project_id"`
	Key         string          `json:"key"`
	Revision    int             `json:"revision"`
	Payload     json.RawMessage `json:"value"`
	UpdatedBy   int             `json:"updated_by,omitempty"`
	UpdatedAt   time.Time       `json:"updated_at"`
}

// GetExtensionStoreDoc returns the document or nil if missing.
func GetExtensionStoreDoc(extensionID string, projectID int, key string) (*ExtensionStoreDoc, error) {
	extensionID = strings.TrimSpace(extensionID)
	key = strings.TrimSpace(key)
	if extensionID == "" || key == "" || projectID <= 0 {
		return nil, fmt.Errorf("extension store lookup requires extension, project, and key")
	}
	pool, err := OpenDatabase()
	if err != nil {
		return nil, err
	}
	defer CloseDatabase(pool)

	var doc ExtensionStoreDoc
	var raw []byte
	err = pool.QueryRow(context.Background(), `
		SELECT extension_id, project_id, doc_key, revision, payload, updated_by, updated_at
		FROM extension_store
		WHERE extension_id = $1 AND project_id = $2 AND doc_key = $3`,
		extensionID, projectID, key,
	).Scan(&doc.ExtensionID, &doc.ProjectID, &doc.Key, &doc.Revision, &raw, &doc.UpdatedBy, &doc.UpdatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) || errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	if len(raw) == 0 {
		raw = []byte("null")
	}
	doc.Payload = json.RawMessage(raw)
	return &doc, nil
}

// ListExtensionStoreKeys returns document keys for an extension on a project.
func ListExtensionStoreKeys(extensionID string, projectID int) ([]string, error) {
	extensionID = strings.TrimSpace(extensionID)
	if extensionID == "" || projectID <= 0 {
		return nil, fmt.Errorf("extension store list requires extension and project")
	}
	pool, err := OpenDatabase()
	if err != nil {
		return nil, err
	}
	defer CloseDatabase(pool)

	rows, err := pool.Query(context.Background(),
		`SELECT doc_key FROM extension_store WHERE extension_id = $1 AND project_id = $2 ORDER BY doc_key`,
		extensionID, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]string, 0)
	for rows.Next() {
		var key string
		if err := rows.Scan(&key); err != nil {
			return nil, err
		}
		out = append(out, key)
	}
	return out, rows.Err()
}

// PutExtensionStoreDoc writes payload when expectedRev matches (0 creates a missing key).
// On revision mismatch, current is non-nil and written is nil.
func PutExtensionStoreDoc(extensionID string, projectID int, key string, expectedRev int, payload json.RawMessage, userID int) (written *ExtensionStoreDoc, current *ExtensionStoreDoc, err error) {
	extensionID = strings.TrimSpace(extensionID)
	key = strings.TrimSpace(key)
	if extensionID == "" || key == "" || projectID <= 0 {
		return nil, nil, fmt.Errorf("extension store write requires extension, project, and key")
	}
	if len(payload) == 0 {
		payload = json.RawMessage([]byte("null"))
	}
	pool, err := OpenDatabase()
	if err != nil {
		return nil, nil, err
	}
	defer CloseDatabase(pool)

	ctx := context.Background()
	tx, err := pool.Begin(ctx)
	if err != nil {
		return nil, nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	existing, err := getExtensionStoreDocTx(ctx, tx, extensionID, projectID, key)
	if err != nil {
		return nil, nil, err
	}
	if existing == nil {
		if expectedRev != 0 {
			return nil, nil, nil
		}
		now := time.Now().UTC()
		_, err = tx.Exec(ctx, `
			INSERT INTO extension_store (extension_id, project_id, doc_key, revision, payload, updated_by, updated_at)
			VALUES ($1, $2, $3, 1, $4, $5, $6)`,
			extensionID, projectID, key, []byte(payload), userID, now)
		if err != nil {
			return nil, nil, err
		}
		if err := tx.Commit(ctx); err != nil {
			return nil, nil, err
		}
		return &ExtensionStoreDoc{
			ExtensionID: extensionID,
			ProjectID:   projectID,
			Key:         key,
			Revision:    1,
			Payload:     payload,
			UpdatedBy:   userID,
			UpdatedAt:   now,
		}, nil, nil
	}
	if existing.Revision != expectedRev {
		return nil, existing, nil
	}
	now := time.Now().UTC()
	nextRev := existing.Revision + 1
	_, err = tx.Exec(ctx, `
		UPDATE extension_store
		SET revision = $4, payload = $5, updated_by = $6, updated_at = $7
		WHERE extension_id = $1 AND project_id = $2 AND doc_key = $3 AND revision = $8`,
		extensionID, projectID, key, nextRev, []byte(payload), userID, now, expectedRev)
	if err != nil {
		return nil, nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, nil, err
	}
	return &ExtensionStoreDoc{
		ExtensionID: extensionID,
		ProjectID:   projectID,
		Key:         key,
		Revision:    nextRev,
		Payload:     payload,
		UpdatedBy:   userID,
		UpdatedAt:   now,
	}, nil, nil
}

func getExtensionStoreDocTx(ctx context.Context, tx pgx.Tx, extensionID string, projectID int, key string) (*ExtensionStoreDoc, error) {
	var doc ExtensionStoreDoc
	var raw []byte
	err := tx.QueryRow(ctx, `
		SELECT extension_id, project_id, doc_key, revision, payload, updated_by, updated_at
		FROM extension_store
		WHERE extension_id = $1 AND project_id = $2 AND doc_key = $3
		FOR UPDATE`,
		extensionID, projectID, key,
	).Scan(&doc.ExtensionID, &doc.ProjectID, &doc.Key, &doc.Revision, &raw, &doc.UpdatedBy, &doc.UpdatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) || errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	if len(raw) == 0 {
		raw = []byte("null")
	}
	doc.Payload = json.RawMessage(raw)
	return &doc, nil
}
