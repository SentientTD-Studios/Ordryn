package storage

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

// ChatServer is an Ordryn Chat server a site admin allows to use "Sign in with
// Ordryn". The chat server presents ClientID and its secret when it starts a
// sign-in, and people approving the sign-in see Name and URL.
type ChatServer struct {
	ID         int    `json:"id"`
	Name       string `json:"name"`
	URL        string `json:"url"`
	ClientID   string `json:"client_id"`
	SecretHint string `json:"secret_hint"`
	Enabled    bool   `json:"enabled"`
	// DirectorySync lets the chat server read the user directory (users, site
	// roles, organization memberships) with its client credentials.
	DirectorySync bool       `json:"directory_sync"`
	CreatedBy     int        `json:"created_by"`
	CreatedAt     time.Time  `json:"created_at"`
	LastUsedAt    *time.Time `json:"last_used_at,omitempty"`
}

var (
	ErrChatServerNotFound = errors.New("chat server not found")
	ErrChatServerURLTaken = errors.New("that chat server URL is already registered")
	// ErrChatClientRejected covers every reason a client ID and secret aren't
	// accepted (unknown, wrong secret, server disabled, chat sign-in off), so
	// callers can't tell them apart.
	ErrChatClientRejected = errors.New("chat client not accepted")
)

const (
	chatClientIDPrefix = "chat_"
	chatSecretPrefix   = "ordc_"
)

// CreateChatServersTables creates chat_servers and the one-row chat_settings.
func CreateChatServersTables() error {
	pool, err := OpenDatabase()
	if err != nil {
		return err
	}
	defer CloseDatabase(pool)

	stmts := []string{
		`CREATE TABLE IF NOT EXISTS chat_servers (
			id SERIAL PRIMARY KEY,
			name TEXT NOT NULL,
			url TEXT NOT NULL UNIQUE,
			client_id TEXT NOT NULL UNIQUE,
			secret_hash TEXT NOT NULL,
			secret_hint TEXT NOT NULL DEFAULT '',
			enabled BOOLEAN NOT NULL DEFAULT TRUE,
			created_by INTEGER NOT NULL DEFAULT 0,
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			last_used_at TIMESTAMPTZ
		)`,
		`CREATE TABLE IF NOT EXISTS chat_settings (
			id INTEGER PRIMARY KEY DEFAULT 1 CHECK (id = 1),
			signin_enabled BOOLEAN NOT NULL DEFAULT FALSE
		)`,
		`INSERT INTO chat_settings (id) VALUES (1) ON CONFLICT (id) DO NOTHING`,
		`ALTER TABLE chat_servers ADD COLUMN IF NOT EXISTS directory_sync BOOLEAN NOT NULL DEFAULT FALSE`,
	}
	for _, q := range stmts {
		if _, err := pool.Exec(context.Background(), q); err != nil {
			return fmt.Errorf("failed to create chat server tables: %v", err)
		}
	}
	return nil
}

// NormalizeChatServerURL checks and canonicalizes a chat server's address:
// https (http only for localhost), no path, query, fragment or credentials.
func NormalizeChatServerURL(raw string) (string, error) {
	raw = strings.TrimRight(strings.TrimSpace(raw), "/")
	u, err := url.Parse(raw)
	bad := errors.New("Enter the chat server's address, like https://chat.example.com")
	if err != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") {
		return "", bad
	}
	host := strings.ToLower(u.Hostname())
	switch u.Scheme {
	case "https":
	case "http":
		if host != "localhost" && host != "127.0.0.1" && host != "::1" {
			return "", errors.New("Chat servers must use https (http is only allowed for localhost)")
		}
	default:
		return "", bad
	}
	u.Host = strings.ToLower(u.Host)
	return u.Scheme + "://" + u.Host, nil
}

// GetChatSignInEnabled reports whether chat servers may use Sign in with Ordryn.
func GetChatSignInEnabled() (bool, error) {
	pool, err := OpenDatabase()
	if err != nil {
		return false, err
	}
	defer CloseDatabase(pool)
	var on bool
	err = pool.QueryRow(context.Background(), `SELECT signin_enabled FROM chat_settings WHERE id = 1`).Scan(&on)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	return on, err
}

// SetChatSignInEnabled turns chat sign-in on or off for every chat server.
func SetChatSignInEnabled(on bool) error {
	pool, err := OpenDatabase()
	if err != nil {
		return err
	}
	defer CloseDatabase(pool)
	_, err = pool.Exec(context.Background(),
		`INSERT INTO chat_settings (id, signin_enabled) VALUES (1, $1)
		 ON CONFLICT (id) DO UPDATE SET signin_enabled = EXCLUDED.signin_enabled`, on)
	return err
}

const chatServerCols = `id, name, url, client_id, secret_hint, enabled, directory_sync, created_by, created_at, last_used_at`

func scanChatServer(row pgx.Row) (*ChatServer, error) {
	var s ChatServer
	err := row.Scan(&s.ID, &s.Name, &s.URL, &s.ClientID, &s.SecretHint, &s.Enabled, &s.DirectorySync, &s.CreatedBy, &s.CreatedAt, &s.LastUsedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrChatServerNotFound
	}
	if err != nil {
		return nil, err
	}
	return &s, nil
}

// ListChatServers returns every registered chat server, oldest first.
func ListChatServers() ([]ChatServer, error) {
	pool, err := OpenDatabase()
	if err != nil {
		return nil, err
	}
	defer CloseDatabase(pool)
	rows, err := pool.Query(context.Background(), `SELECT `+chatServerCols+` FROM chat_servers ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ChatServer{}
	for rows.Next() {
		s, err := scanChatServer(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *s)
	}
	return out, rows.Err()
}

// GetChatServer returns one chat server.
func GetChatServer(id int) (*ChatServer, error) {
	pool, err := OpenDatabase()
	if err != nil {
		return nil, err
	}
	defer CloseDatabase(pool)
	return scanChatServer(pool.QueryRow(context.Background(), `SELECT `+chatServerCols+` FROM chat_servers WHERE id = $1`, id))
}

func randomHex(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func newChatSecret() (secret, hash, hint string, err error) {
	r, err := randomHex(32)
	if err != nil {
		return "", "", "", err
	}
	secret = chatSecretPrefix + r
	return secret, HashAPIKey(secret), "…" + secret[len(secret)-4:], nil
}

// CreateChatServer registers a chat server and returns its secret, which is
// shown once and stored only as a hash. rawURL must already be normalized.
func CreateChatServer(name, rawURL string, directorySync bool, createdBy int) (*ChatServer, string, error) {
	pool, err := OpenDatabase()
	if err != nil {
		return nil, "", err
	}
	defer CloseDatabase(pool)
	id, err := randomHex(12)
	if err != nil {
		return nil, "", err
	}
	secret, hash, hint, err := newChatSecret()
	if err != nil {
		return nil, "", err
	}
	s, err := scanChatServer(pool.QueryRow(context.Background(),
		`INSERT INTO chat_servers (name, url, client_id, secret_hash, secret_hint, directory_sync, created_by)
		 VALUES ($1, $2, $3, $4, $5, $6, $7) RETURNING `+chatServerCols,
		name, rawURL, chatClientIDPrefix+id, hash, hint, directorySync, createdBy))
	if isUniqueViolation(err) {
		return nil, "", ErrChatServerURLTaken
	}
	if err != nil {
		return nil, "", err
	}
	return s, secret, nil
}

// UpdateChatServer changes a chat server's name, URL, enabled or directory
// sync flag (nil = keep).
func UpdateChatServer(id int, name, rawURL *string, enabled, directorySync *bool) (*ChatServer, error) {
	pool, err := OpenDatabase()
	if err != nil {
		return nil, err
	}
	defer CloseDatabase(pool)
	s, err := scanChatServer(pool.QueryRow(context.Background(),
		`UPDATE chat_servers SET name = COALESCE($2, name), url = COALESCE($3, url), enabled = COALESCE($4, enabled),
		        directory_sync = COALESCE($5, directory_sync)
		 WHERE id = $1 RETURNING `+chatServerCols, id, name, rawURL, enabled, directorySync))
	if isUniqueViolation(err) {
		return nil, ErrChatServerURLTaken
	}
	return s, err
}

// RotateChatServerSecret replaces a chat server's secret; the old one stops working at once.
func RotateChatServerSecret(id int) (*ChatServer, string, error) {
	pool, err := OpenDatabase()
	if err != nil {
		return nil, "", err
	}
	defer CloseDatabase(pool)
	secret, hash, hint, err := newChatSecret()
	if err != nil {
		return nil, "", err
	}
	s, err := scanChatServer(pool.QueryRow(context.Background(),
		`UPDATE chat_servers SET secret_hash = $2, secret_hint = $3 WHERE id = $1 RETURNING `+chatServerCols, id, hash, hint))
	if err != nil {
		return nil, "", err
	}
	return s, secret, nil
}

// DeleteChatServer removes a chat server; its credentials stop working at once.
func DeleteChatServer(id int) (*ChatServer, error) {
	pool, err := OpenDatabase()
	if err != nil {
		return nil, err
	}
	defer CloseDatabase(pool)
	return scanChatServer(pool.QueryRow(context.Background(),
		`DELETE FROM chat_servers WHERE id = $1 RETURNING `+chatServerCols, id))
}

// AuthenticateChatServer checks a chat server's client ID and secret. It fails
// with ErrChatClientRejected unless chat sign-in is on, the server is enabled,
// and the secret matches.
func AuthenticateChatServer(clientID, secret string) (*ChatServer, error) {
	clientID, secret = strings.TrimSpace(clientID), strings.TrimSpace(secret)
	if clientID == "" || secret == "" {
		return nil, ErrChatClientRejected
	}
	pool, err := OpenDatabase()
	if err != nil {
		return nil, err
	}
	defer CloseDatabase(pool)
	var s ChatServer
	var hash string
	var on bool
	err = pool.QueryRow(context.Background(),
		`SELECT cs.id, cs.name, cs.url, cs.client_id, cs.secret_hint, cs.enabled, cs.directory_sync, cs.created_by,
		        cs.created_at, cs.last_used_at, cs.secret_hash, COALESCE(st.signin_enabled, FALSE)
		 FROM chat_servers cs LEFT JOIN chat_settings st ON st.id = 1
		 WHERE cs.client_id = $1`, clientID).
		Scan(&s.ID, &s.Name, &s.URL, &s.ClientID, &s.SecretHint, &s.Enabled, &s.DirectorySync, &s.CreatedBy, &s.CreatedAt,
			&s.LastUsedAt, &hash, &on)
	if errors.Is(err, pgx.ErrNoRows) {
		HashAPIKey(secret) // similar timing for unknown IDs
		return nil, ErrChatClientRejected
	}
	if err != nil {
		return nil, err
	}
	if subtle.ConstantTimeCompare([]byte(HashAPIKey(secret)), []byte(hash)) != 1 || !s.Enabled || !on {
		return nil, ErrChatClientRejected
	}
	_, _ = pool.Exec(context.Background(), `UPDATE chat_servers SET last_used_at = NOW() WHERE id = $1`, s.ID)
	return &s, nil
}
