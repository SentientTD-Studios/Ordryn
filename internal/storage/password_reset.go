package storage

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"time"

	"github.com/google/uuid"
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

// GenerateResetToken creates a new password reset token for the given email
func GenerateResetToken(email string) (*PasswordReset, error) {
	return GenerateResetTokenTTL(email, 15*time.Minute)
}

// GenerateResetTokenTTL creates a password token that expires after ttl. New
// accounts use a longer lifetime so the person has time to open their welcome email.
func GenerateResetTokenTTL(email string, ttl time.Duration) (*PasswordReset, error) {
	pool, err := OpenDatabase()
	if err != nil {
		return nil, err
	}
	defer CloseDatabase(pool)

	// Generate a secure random token
	tokenBytes := make([]byte, 32)
	if _, err := rand.Read(tokenBytes); err != nil {
		return nil, fmt.Errorf("failed to generate token: %v", err)
	}
	token := hex.EncodeToString(tokenBytes)

	// Generate UUID for the reset ID
	resetID := uuid.New().String()

	// Use UTC to match the database.
	expiresAt := time.Now().UTC().Add(ttl)

	// Delete any existing reset tokens for this email
	_, err = pool.Exec(context.Background(), "DELETE FROM password_reset WHERE email = $1", email)
	if err != nil {
		return nil, fmt.Errorf("failed to clean up old tokens: %v", err)
	}

	// Insert the new reset token
	_, err = pool.Exec(context.Background(),
		"INSERT INTO password_reset (id, email, token, expires_at) VALUES ($1, $2, $3, $4)",
		resetID, email, token, expiresAt)
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

// ValidateResetToken checks if a token is valid and not expired
func ValidateResetToken(id, token string) (*PasswordReset, error) {
	pool, err := OpenDatabase()
	if err != nil {
		return nil, err
	}
	defer CloseDatabase(pool)

	var reset PasswordReset
	err = pool.QueryRow(context.Background(),
		"SELECT id, email, token, expires_at FROM password_reset WHERE id = $1 AND token = $2",
		id, token).Scan(&reset.ID, &reset.Email, &reset.Token, &reset.ExpiresAt)
	if err != nil {
		return nil, fmt.Errorf("invalid token")
	}

	// Check if token has expired
	if time.Now().UTC().After(reset.ExpiresAt) {
		return nil, fmt.Errorf("token expired")
	}

	return &reset, nil
}

// DeleteResetToken removes a reset token from the database
func DeleteResetToken(id, token string) error {
	pool, err := OpenDatabase()
	if err != nil {
		return err
	}
	defer CloseDatabase(pool)

	_, err = pool.Exec(context.Background(),
		"DELETE FROM password_reset WHERE id = $1 AND token = $2", id, token)
	if err != nil {
		return fmt.Errorf("failed to delete reset token: %v", err)
	}

	return nil
}
