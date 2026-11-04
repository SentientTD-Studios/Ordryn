package storage

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// PasswordReset represents a password reset request
type PasswordReset struct {
	ID        string
	Email     string
	Token     string
	ExpiresAt time.Time
}

// CreatePasswordResetTable ensures the password_reset table exists.
func CreatePasswordResetTable() error {
	pool, err := OpenDatabase()
	if err != nil {
		return err
	}
	defer CloseDatabase(pool)

	_, err = pool.Exec(context.Background(), `
        CREATE TABLE IF NOT EXISTS password_reset (
            id UUID PRIMARY KEY,
            email VARCHAR(255) NOT NULL,
            token VARCHAR(255) NOT NULL,
            expires_at TIMESTAMP NOT NULL,
            created_at TIMESTAMP DEFAULT NOW()
        )
    `)
	if err != nil {
		return fmt.Errorf("failed to create password_reset table: %v", err)
	}

	// Create index on token for faster lookups
	_, err = pool.Exec(context.Background(), `
        CREATE INDEX IF NOT EXISTS idx_password_reset_token ON password_reset(token)
    `)
	if err != nil {
		return fmt.Errorf("failed to create index on password_reset: %v", err)
	}

	return nil
}

// MigratePasswordResetHashTokens stores reset tokens as SHA-256 hashes, so a copy
// of the database can't be used to set passwords. Outstanding links keep working:
// existing plain-text tokens are hashed in place.
func MigratePasswordResetHashTokens() error {
	pool, err := OpenDatabase()
	if err != nil {
		return err
	}
	defer CloseDatabase(pool)
	ctx := context.Background()

	stmts := []string{
		`ALTER TABLE password_reset ADD COLUMN IF NOT EXISTS token_hash VARCHAR(64)`,
		`ALTER TABLE password_reset ALTER COLUMN token DROP NOT NULL`,
	}
	for _, stmt := range stmts {
		if _, err := pool.Exec(ctx, stmt); err != nil {
			return fmt.Errorf("password_reset hash: %w", err)
		}
	}

	rows, err := pool.Query(ctx,
		`SELECT id::text, token FROM password_reset WHERE token_hash IS NULL AND token IS NOT NULL`)
	if err != nil {
		return fmt.Errorf("password_reset hash: %w", err)
	}
	type pending struct{ id, token string }
	var todo []pending
	for rows.Next() {
		var p pending
		if err := rows.Scan(&p.id, &p.token); err != nil {
			rows.Close()
			return fmt.Errorf("password_reset hash: %w", err)
		}
		todo = append(todo, p)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return fmt.Errorf("password_reset hash: %w", err)
	}
	for _, p := range todo {
		if _, err := pool.Exec(ctx,
			`UPDATE password_reset SET token_hash = $1, token = NULL WHERE id = $2`,
			hashResetToken(p.token), p.id); err != nil {
			return fmt.Errorf("password_reset hash: %w", err)
		}
	}

	if _, err := pool.Exec(ctx, `DROP INDEX IF EXISTS idx_password_reset_token`); err != nil {
		return fmt.Errorf("password_reset hash: %w", err)
	}

	// Tie each token to one account. Existing links are matched by email, taking the
	// same account sign-in would pick: an exact-case match, otherwise the oldest.
	accountStmts := []string{
		`ALTER TABLE password_reset ADD COLUMN IF NOT EXISTS user_id INTEGER REFERENCES users(id) ON DELETE CASCADE`,
		`UPDATE password_reset pr SET user_id = (
			SELECT u.id FROM users u WHERE LOWER(u.email) = LOWER(pr.email)
			ORDER BY (u.email = pr.email) DESC, u.id LIMIT 1)
		 WHERE pr.user_id IS NULL`,
		`CREATE INDEX IF NOT EXISTS idx_password_reset_user ON password_reset(user_id)`,
	}
	for _, stmt := range accountStmts {
		if _, err := pool.Exec(ctx, stmt); err != nil {
			return fmt.Errorf("password_reset user_id: %w", err)
		}
	}
	return nil
}

// hashResetToken returns the stored form of a reset token. The token is 256
// random bits, so a fast hash is enough.
func hashResetToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// ErrResetTokenInvalid is returned for an unknown, used or expired reset token.
var ErrResetTokenInvalid = errors.New("invalid or expired reset token")

// GenerateResetToken creates a 15-minute password reset token for one account.
func GenerateResetToken(userID int, email string) (*PasswordReset, error) {
	return GenerateResetTokenTTL(userID, email, 15*time.Minute)
}

// GenerateResetTokenTTL creates a password token that expires after ttl. New
// accounts use a longer lifetime so the person has time to open their welcome email.
// The returned Token is the only copy of the plain-text token; only its hash is stored.
// The token is tied to userID, so it can only ever change that one account, even
// when another account's email differs from it only by case.
func GenerateResetTokenTTL(userID int, email string, ttl time.Duration) (*PasswordReset, error) {
	pool, err := OpenDatabase()
	if err != nil {
		return nil, err
	}
	defer CloseDatabase(pool)

	tokenBytes := make([]byte, 32)
	if _, err := rand.Read(tokenBytes); err != nil {
		return nil, fmt.Errorf("failed to generate token: %v", err)
	}
	token := hex.EncodeToString(tokenBytes)
	resetID := uuid.New().String()
	// Use UTC to match the database.
	expiresAt := time.Now().UTC().Add(ttl)

	// A new link replaces the account's earlier ones.
	_, err = pool.Exec(context.Background(), "DELETE FROM password_reset WHERE user_id = $1", userID)
	if err != nil {
		return nil, fmt.Errorf("failed to clean up old tokens: %v", err)
	}

	_, err = pool.Exec(context.Background(),
		"INSERT INTO password_reset (id, user_id, email, token_hash, expires_at) VALUES ($1, $2, $3, $4, $5)",
		resetID, userID, NormalizeEmail(email), hashResetToken(token), expiresAt)
	if err != nil {
		return nil, fmt.Errorf("failed to create reset token: %v", err)
	}

	return &PasswordReset{
		ID:        resetID,
		Email:     email,
		Token:     token,
		ExpiresAt: expiresAt,
	}, nil
}

// ValidateResetToken checks that a token exists and hasn't expired, without using
// it up. It's for showing the reset form; ResetPasswordWithToken does the reset.
func ValidateResetToken(id, token string) (*PasswordReset, error) {
	pool, err := OpenDatabase()
	if err != nil {
		return nil, err
	}
	defer CloseDatabase(pool)

	if _, err := uuid.Parse(id); err != nil {
		return nil, ErrResetTokenInvalid
	}
	reset := PasswordReset{Token: token}
	err = pool.QueryRow(context.Background(),
		"SELECT id::text, email, expires_at FROM password_reset WHERE id = $1 AND token_hash = $2",
		id, hashResetToken(token)).Scan(&reset.ID, &reset.Email, &reset.ExpiresAt)
	if err != nil {
		return nil, ErrResetTokenInvalid
	}
	if time.Now().UTC().After(reset.ExpiresAt) {
		return nil, ErrResetTokenInvalid
	}
	return &reset, nil
}

// ResetPasswordWithToken uses up the token and sets the account's password in one
// transaction, so a token works exactly once even under concurrent requests. It
// also signs out the account's existing sessions, and confirms the email: the link
// was delivered there. It returns the account's email.
func ResetPasswordWithToken(id, token, passwordHash string) (string, error) {
	if _, err := uuid.Parse(id); err != nil {
		return "", ErrResetTokenInvalid
	}
	pool, err := OpenDatabase()
	if err != nil {
		return "", err
	}
	defer CloseDatabase(pool)

	ctx := context.Background()
	tx, err := pool.Begin(ctx)
	if err != nil {
		return "", err
	}
	defer tx.Rollback(ctx)

	var userID *int
	var expiresAt time.Time
	err = tx.QueryRow(ctx,
		`DELETE FROM password_reset WHERE id = $1 AND token_hash = $2 RETURNING user_id, expires_at`,
		id, hashResetToken(token)).Scan(&userID, &expiresAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrResetTokenInvalid
	}
	if err != nil {
		return "", err
	}
	if time.Now().UTC().After(expiresAt) {
		// Keep the expired row deleted; there's nothing left to use it for.
		if err := tx.Commit(ctx); err != nil {
			return "", err
		}
		return "", ErrResetTokenInvalid
	}

	if userID == nil {
		// A link from before tokens were tied to accounts, whose email matched no
		// single account when it was migrated.
		if err := tx.Commit(ctx); err != nil {
			return "", err
		}
		return "", ErrResetTokenInvalid
	}

	var email string
	err = tx.QueryRow(ctx,
		`UPDATE users SET password = $1, session_version = session_version + 1,
		        email_verified_at = COALESCE(email_verified_at, NOW())
		 WHERE id = $2 RETURNING email`,
		passwordHash, *userID).Scan(&email)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrResetTokenInvalid
	}
	if err != nil {
		return "", err
	}
	// The reset confirmed the email, so any outstanding confirmation link is spent.
	if _, err := tx.Exec(ctx, `DELETE FROM email_verifications WHERE user_id = $1`, *userID); err != nil {
		return "", err
	}
	// API keys keep working through a reset, as is usual. But a reset is also how
	// someone takes an account back, and a key made by whoever had it would keep
	// working too, so ask the owner to check their keys.
	if _, err := tx.Exec(ctx, insertAPIKeyReviewNotificationSQL, *userID); err != nil {
		return "", err
	}
	if err := tx.Commit(ctx); err != nil {
		return "", err
	}
	return email, nil
}
