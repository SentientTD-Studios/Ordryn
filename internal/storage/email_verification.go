package storage

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
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

// ErrVerificationPassword is returned when a valid confirmation link is used with
// a password that isn't the account's. The link stays usable.
var ErrVerificationPassword = errors.New("password doesn't match the account")

// VerifyEmailWithToken uses up a confirmation link and marks the account's email
// confirmed, in one transaction. It returns the account id.
//
// The link alone isn't enough: passwordMatches must accept the account's stored
// password hash. Anyone can start a sign-up with someone else's address, and a
// repeat sign-up replaces the stored password, so a link proves only control of
// the inbox, not whose password is on the account. Requiring the password too
// means only a person who has both can activate it: an attacker who signed up
// with the victim's address can't, and the victim can't activate the attacker's
// password by clicking a link in an email they didn't trigger.
//
// The hash is checked while the account row is locked, so a concurrent repeat
// sign-up can't swap the password between the check and the confirmation.
func VerifyEmailWithToken(id, token string, passwordMatches func(hash string) bool) (int, error) {
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
	var hash string
	var alreadyVerified bool
	err = tx.QueryRow(ctx,
		`SELECT v.user_id, v.expires_at, u.password, u.email_verified_at IS NOT NULL
		 FROM email_verifications v JOIN users u ON u.id = v.user_id
		 WHERE v.id = $1 AND v.token_hash = $2
		 FOR UPDATE OF v, u`,
		id, hashResetToken(token)).Scan(&userID, &expiresAt, &hash, &alreadyVerified)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, ErrVerificationInvalid
	}
	if err != nil {
		return 0, err
	}
	// Expired, or the account was confirmed another way (a password reset): the
	// link has nothing left to do, so delete it.
	if alreadyVerified || time.Now().After(expiresAt) {
		if _, err := tx.Exec(ctx, `DELETE FROM email_verifications WHERE id = $1`, id); err != nil {
			return 0, err
		}
		if err := tx.Commit(ctx); err != nil {
			return 0, err
		}
		return 0, ErrVerificationInvalid
	}
	if passwordMatches == nil || !passwordMatches(hash) {
		return 0, ErrVerificationPassword
	}
	if _, err := tx.Exec(ctx, `DELETE FROM email_verifications WHERE user_id = $1`, userID); err != nil {
		return 0, err
	}
	if _, err := tx.Exec(ctx,
		`UPDATE users SET email_verified_at = NOW(), session_version = session_version + 1 WHERE id = $1`,
		userID); err != nil {
		return 0, err
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, err
	}
	return userID, nil
}

// ReplaceUnverifiedRegistration gives an account that hasn't confirmed its email
// the password, username and timezone from a new sign-up with the same address,
// and cancels its outstanding confirmation links. A repeat sign-up is usually the
// owner retrying, and the first sign-up may have been someone else using their
// address; either way the latest sign-up's details are the ones a confirmation
// (which also needs the password) can activate.
//
// It returns false, changing nothing, if the email was confirmed in the meantime.
func ReplaceUnverifiedRegistration(userID int, hashedPassword, timezone, userName string) (bool, error) {
	pool, err := OpenDatabase()
	if err != nil {
		return false, err
	}
	defer CloseDatabase(pool)

	if timezone == "" {
		timezone = "America/New_York"
	}
	ctx := context.Background()
	tx, err := pool.Begin(ctx)
	if err != nil {
		return false, err
	}
	defer tx.Rollback(ctx)

	tag, err := tx.Exec(ctx,
		`UPDATE users SET password = $2, timezone = $3, user_name = $4,
		        session_version = session_version + 1
		 WHERE id = $1 AND email_verified_at IS NULL`,
		userID, hashedPassword, timezone, strings.TrimSpace(userName))
	if err != nil {
		return false, err
	}
	if tag.RowsAffected() == 0 {
		return false, nil
	}
	if _, err := tx.Exec(ctx, `DELETE FROM email_verifications WHERE user_id = $1`, userID); err != nil {
		return false, err
	}
	if err := tx.Commit(ctx); err != nil {
		return false, err
	}
	return true, nil
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
