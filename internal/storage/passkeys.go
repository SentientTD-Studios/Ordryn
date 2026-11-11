package storage

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// MaxPasskeysPerUser caps how many passkeys one account can register.
const MaxPasskeysPerUser = 20

// ErrPasskeyNotFound is returned when a passkey or ceremony doesn't exist.
var ErrPasskeyNotFound = errors.New("passkey not found")

// Passkey is a stored WebAuthn credential. Credential holds the library's JSON
// record (public key, flags, sign count); CredentialID is kept apart for lookups.
type Passkey struct {
	ID           int
	UserID       int
	CredentialID []byte
	Credential   []byte
	Name         string
	CreatedAt    time.Time
	LastUsedAt   *time.Time
}

// CreatePasskeyTables creates user_passkeys, webauthn_ceremonies, and
// users.webauthn_handle (the random, stable WebAuthn user handle).
func CreatePasskeyTables() error {
	pool, err := OpenDatabase()
	if err != nil {
		return err
	}
	defer CloseDatabase(pool)
	for _, stmt := range []string{
		`ALTER TABLE users ADD COLUMN IF NOT EXISTS webauthn_handle BYTEA`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_users_webauthn_handle ON users(webauthn_handle) WHERE webauthn_handle IS NOT NULL`,
		`CREATE TABLE IF NOT EXISTS user_passkeys (
			id SERIAL PRIMARY KEY,
			user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			credential_id BYTEA NOT NULL UNIQUE,
			credential JSONB NOT NULL,
			name VARCHAR(60) NOT NULL DEFAULT '',
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			last_used_at TIMESTAMPTZ
		)`,
		`CREATE INDEX IF NOT EXISTS idx_user_passkeys_user ON user_passkeys(user_id)`,
		`CREATE TABLE IF NOT EXISTS webauthn_ceremonies (
			token VARCHAR(64) PRIMARY KEY,
			kind VARCHAR(16) NOT NULL,
			user_id INTEGER REFERENCES users(id) ON DELETE CASCADE,
			session JSONB NOT NULL,
			expires_at TIMESTAMPTZ NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS idx_webauthn_ceremonies_expires ON webauthn_ceremonies(expires_at)`,
	} {
		if _, err := pool.Exec(context.Background(), stmt); err != nil {
			return fmt.Errorf("passkeys: %v", err)
		}
	}
	return nil
}

// WebAuthnHandle returns the user's WebAuthn user handle, creating it on first use.
func WebAuthnHandle(userID int) ([]byte, error) {
	pool, err := OpenDatabase()
	if err != nil {
		return nil, err
	}
	defer CloseDatabase(pool)
	ctx := context.Background()
	var h []byte
	if err := pool.QueryRow(ctx, `SELECT webauthn_handle FROM users WHERE id = $1`, userID).Scan(&h); err != nil {
		return nil, err
	}
	if len(h) > 0 {
		return h, nil
	}
	h = make([]byte, 32)
	if _, err := rand.Read(h); err != nil {
		return nil, err
	}
	// Another request may have set it first; keep whichever won.
	if err := pool.QueryRow(ctx, `
		UPDATE users SET webauthn_handle = COALESCE(webauthn_handle, $2) WHERE id = $1
		RETURNING webauthn_handle`, userID, h).Scan(&h); err != nil {
		return nil, err
	}
	return h, nil
}

// UserIDForWebAuthnHandle finds the account a user handle belongs to.
func UserIDForWebAuthnHandle(handle []byte) (int, error) {
	pool, err := OpenDatabase()
	if err != nil {
		return 0, err
	}
	defer CloseDatabase(pool)
	var id int
	err = pool.QueryRow(context.Background(), `SELECT id FROM users WHERE webauthn_handle = $1`, handle).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, ErrPasskeyNotFound
	}
	return id, err
}

func scanPasskeys(rows pgx.Rows) ([]Passkey, error) {
	defer rows.Close()
	out := []Passkey{}
	for rows.Next() {
		var p Passkey
		if err := rows.Scan(&p.ID, &p.UserID, &p.CredentialID, &p.Credential, &p.Name, &p.CreatedAt, &p.LastUsedAt); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

const passkeyCols = `id, user_id, credential_id, credential, name, created_at, last_used_at`

// ListPasskeys returns a user's passkeys, oldest first.
func ListPasskeys(userID int) ([]Passkey, error) {
	pool, err := OpenDatabase()
	if err != nil {
		return nil, err
	}
	defer CloseDatabase(pool)
	rows, err := pool.Query(context.Background(),
		`SELECT `+passkeyCols+` FROM user_passkeys WHERE user_id = $1 ORDER BY id`, userID)
	if err != nil {
		return nil, err
	}
	return scanPasskeys(rows)
}

// CountPasskeys returns how many passkeys a user has.
func CountPasskeys(userID int) (int, error) {
	pool, err := OpenDatabase()
	if err != nil {
		return 0, err
	}
	defer CloseDatabase(pool)
	var n int
	err = pool.QueryRow(context.Background(), `SELECT COUNT(*) FROM user_passkeys WHERE user_id = $1`, userID).Scan(&n)
	return n, err
}

// AddPasskey stores a new passkey.
func AddPasskey(userID int, credentialID, credential []byte, name string) (*Passkey, error) {
	pool, err := OpenDatabase()
	if err != nil {
		return nil, err
	}
	defer CloseDatabase(pool)
	p := Passkey{UserID: userID, CredentialID: credentialID, Credential: credential, Name: name}
	err = pool.QueryRow(context.Background(), `
		INSERT INTO user_passkeys (user_id, credential_id, credential, name) VALUES ($1, $2, $3, $4)
		RETURNING id, created_at`, userID, credentialID, credential, name).Scan(&p.ID, &p.CreatedAt)
	if err != nil {
		return nil, err
	}
	return &p, nil
}

// UpdatePasskeyAfterLogin saves the credential's new sign count and flags.
func UpdatePasskeyAfterLogin(userID int, credentialID, credential []byte) error {
	pool, err := OpenDatabase()
	if err != nil {
		return err
	}
	defer CloseDatabase(pool)
	_, err = pool.Exec(context.Background(), `
		UPDATE user_passkeys SET credential = $3, last_used_at = NOW()
		WHERE user_id = $1 AND credential_id = $2`, userID, credentialID, credential)
	return err
}

// RenamePasskey changes a passkey's label.
func RenamePasskey(userID, id int, name string) error {
	pool, err := OpenDatabase()
	if err != nil {
		return err
	}
	defer CloseDatabase(pool)
	tag, err := pool.Exec(context.Background(),
		`UPDATE user_passkeys SET name = $3 WHERE user_id = $1 AND id = $2`, userID, id, name)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrPasskeyNotFound
	}
	return nil
}

// DeletePasskey removes one of the user's passkeys.
func DeletePasskey(userID, id int) error {
	pool, err := OpenDatabase()
	if err != nil {
		return err
	}
	defer CloseDatabase(pool)
	tag, err := pool.Exec(context.Background(), `DELETE FROM user_passkeys WHERE user_id = $1 AND id = $2`, userID, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrPasskeyNotFound
	}
	return nil
}

// SaveWebAuthnCeremony stores the server half of a registration or sign-in
// ceremony until it expires, and clears out expired ones.
func SaveWebAuthnCeremony(token, kind string, userID int, session []byte, expires time.Time) error {
	pool, err := OpenDatabase()
	if err != nil {
		return err
	}
	defer CloseDatabase(pool)
	ctx := context.Background()
	_, _ = pool.Exec(ctx, `DELETE FROM webauthn_ceremonies WHERE expires_at < NOW()`)
	var uid any
	if userID > 0 {
		uid = userID
	}
	_, err = pool.Exec(ctx, `
		INSERT INTO webauthn_ceremonies (token, kind, user_id, session, expires_at) VALUES ($1, $2, $3, $4, $5)`,
		token, kind, uid, session, expires)
	return err
}

// TakeWebAuthnCeremony loads and deletes a ceremony, so each can be finished
// only once. It returns ErrPasskeyNotFound when missing, expired, or of another kind.
func TakeWebAuthnCeremony(token, kind string) (userID int, session []byte, err error) {
	pool, err := OpenDatabase()
	if err != nil {
		return 0, nil, err
	}
	defer CloseDatabase(pool)
	var uid *int
	err = pool.QueryRow(context.Background(), `
		DELETE FROM webauthn_ceremonies WHERE token = $1 AND kind = $2 AND expires_at >= NOW()
		RETURNING user_id, session`, token, kind).Scan(&uid, &session)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, nil, ErrPasskeyNotFound
	}
	if err != nil {
		return 0, nil, err
	}
	if uid != nil {
		userID = *uid
	}
	return userID, session, nil
}
