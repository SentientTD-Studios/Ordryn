package storage

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
)

// GetUserByCalendarToken returns the user ID for a valid, non-banned calendar feed token.
func GetUserByCalendarToken(token string) (int, error) {
	if token == "" {
		return 0, fmt.Errorf("invalid token")
	}
	pool, err := OpenDatabase()
	if err != nil {
		return 0, err
	}
	defer CloseDatabase(pool)

	var userID int
	var isBanned bool
	err = pool.QueryRow(context.Background(),
		`SELECT id, COALESCE(is_banned, FALSE) FROM users WHERE calendar_token = $1`, token).
		Scan(&userID, &isBanned)
	if err != nil {
		return 0, fmt.Errorf("user not found")
	}
	if isBanned {
		return 0, fmt.Errorf("user not found")
	}
	return userID, nil
}

// ClearCalendarToken removes the calendar feed token for a user (e.g. on ban).
func ClearCalendarToken(userID int) error {
	pool, err := OpenDatabase()
	if err != nil {
		return err
	}
	defer CloseDatabase(pool)
	_, err = pool.Exec(context.Background(),
		"UPDATE users SET calendar_token = NULL WHERE id = $1", userID)
	return err
}

// ClearCalendarTokenByEmail clears calendar token by user email.
func ClearCalendarTokenByEmail(email string) error {
	pool, err := OpenDatabase()
	if err != nil {
		return err
	}
	defer CloseDatabase(pool)
	_, err = pool.Exec(context.Background(),
		"UPDATE users SET calendar_token = NULL WHERE email = $1", email)
	return err
}

// GetOrCreateCalendarToken returns the user's calendar token, generating one if needed.
func GetOrCreateCalendarToken(userID int) (string, error) {
	pool, err := OpenDatabase()
	if err != nil {
		return "", err
	}
	defer CloseDatabase(pool)

	var token string
	err = pool.QueryRow(context.Background(),
		"SELECT COALESCE(calendar_token, '') FROM users WHERE id = $1", userID).Scan(&token)
	if err != nil {
		return "", err
	}
	if token != "" {
		return token, nil
	}
	return RegenerateCalendarToken(userID)
}

// RegenerateCalendarToken creates a new calendar token for the user.
func RegenerateCalendarToken(userID int) (string, error) {
	pool, err := OpenDatabase()
	if err != nil {
		return "", err
	}
	defer CloseDatabase(pool)

	token, err := newCalendarToken()
	if err != nil {
		return "", err
	}

	_, err = pool.Exec(context.Background(),
		"UPDATE users SET calendar_token = $1 WHERE id = $2", token, userID)
	if err != nil {
		return "", fmt.Errorf("failed to update calendar token: %v", err)
	}
	return token, nil
}

func newCalendarToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
