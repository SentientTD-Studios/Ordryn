package storage

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// EmailVerificationTTL is how long a confirmation link stays valid.
const EmailVerificationTTL = 48 * time.Hour

// ErrVerificationInvalid is returned for an unknown, used or expired confirmation link.
var ErrVerificationInvalid = errors.New("invalid or expired confirmation link")

// MigrateUsersAddEmailVerified adds users.email_verified_at. It defaults to NOW(),
// so existing accounts and every account created by an admin, an invite or setup
// count as verified. Only open self-registration creates an account with NULL,
// which can't sign in until the email is confirmed.
func MigrateUsersAddEmailVerified() error {
	pool, err := OpenDatabase()
	if err != nil {
		return err
	}
	defer CloseDatabase(pool)
	_, err = pool.Exec(context.Background(),
		`ALTER TABLE users ADD COLUMN IF NOT EXISTS email_verified_at TIMESTAMPTZ DEFAULT NOW()`)
	if err != nil {
		return fmt.Errorf("users.email_verified_at: %w", err)
	}
	return nil
}

// CreateEmailVerificationsTable holds outstanding confirmation links. Like reset
// tokens, only a SHA-256 of each token is stored.
func CreateEmailVerificationsTable() error {
	pool, err := OpenDatabase()
	if err != nil {
		return err
	}
	defer CloseDatabase(pool)
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS email_verifications (
			id UUID PRIMARY KEY,
			user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			token_hash VARCHAR(64) NOT NULL,
			expires_at TIMESTAMPTZ NOT NULL,
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)`,
		`CREATE INDEX IF NOT EXISTS idx_email_verifications_user ON email_verifications(user_id)`,
	}
	for _, stmt := range stmts {
		if _, err := pool.Exec(context.Background(), stmt); err != nil {
			return fmt.Errorf("email_verifications: %w", err)
		}
	}
	return nil
}

// EmailVerification is a newly issued confirmation link. Token is the only copy
// of the plain-text token.
type EmailVerification struct {
	ID        string
	Token     string
	ExpiresAt time.Time
}

// IssueEmailVerification creates a confirmation link for the account, replacing
// any earlier one.
func IssueEmailVerification(userID int) (*EmailVerification, error) {
	pool, err := OpenDatabase()
	if err != nil {
		return nil, err
	}
	defer CloseDatabase(pool)

	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return nil, err
	}
	v := &EmailVerification{
		ID:        uuid.New().String(),
		Token:     hex.EncodeToString(b),
		ExpiresAt: time.Now().UTC().Add(EmailVerificationTTL),
	}
	ctx := context.Background()
	if _, err := pool.Exec(ctx, `DELETE FROM email_verifications WHERE user_id = $1`, userID); err != nil {
		return nil, err
	}
	if _, err := pool.Exec(ctx,
		`INSERT INTO email_verifications (id, user_id, token_hash, expires_at) VALUES ($1, $2, $3, $4)`,
		v.ID, userID, hashResetToken(v.Token), v.ExpiresAt); err != nil {
		return nil, err
	}
	return v, nil
}

// VerifyEmailWithToken uses up a confirmation link and marks the account's email
// confirmed, in one transaction. It returns the account id.
func VerifyEmailWithToken(id, token string) (int, error) {
	if _, err := uuid.Parse(id); err != nil {
		return 0, ErrVerificationInvalid
	}
	pool, err := OpenDatabase()
	if err != nil {
		return 0, err
	}
	defer CloseDatabase(pool)

	ctx := context.Background()
	tx, err := pool.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(ctx)

	var userID int
	var expiresAt time.Time
	err = tx.QueryRow(ctx,
		`DELETE FROM email_verifications WHERE id = $1 AND token_hash = $2 RETURNING user_id, expires_at`,
		id, hashResetToken(token)).Scan(&userID, &expiresAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, ErrVerificationInvalid
	}
	if err != nil {
		return 0, err
	}
	if time.Now().After(expiresAt) {
		if err := tx.Commit(ctx); err != nil {
			return 0, err
		}
		return 0, ErrVerificationInvalid
	}
	if _, err := tx.Exec(ctx,
		`UPDATE users SET email_verified_at = COALESCE(email_verified_at, NOW()) WHERE id = $1`, userID); err != nil {
		return 0, err
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, err
	}
	return userID, nil
}

// IsEmailVerified reports whether the account's email has been confirmed.
func IsEmailVerified(userID int) (bool, error) {
	pool, err := OpenDatabase()
	if err != nil {
		return false, err
	}
	defer CloseDatabase(pool)
	var verified bool
	err = pool.QueryRow(context.Background(),
		`SELECT email_verified_at IS NOT NULL FROM users WHERE id = $1`, userID).Scan(&verified)
	return verified, err
}
