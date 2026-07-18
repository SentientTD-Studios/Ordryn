package storage

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
)

// UserProfile is the public account view for /api/v1/me and auth responses.
type UserProfile struct {
	ID            int
	Email         string
	UserName      string
	Timezone      string
	ItemsPerPage  int
	RoleID        int
	Permissions   []string
	DigestEnabled bool
	DigestHour    int
}

// GetUserProfileByID loads profile fields and role permissions for a user.
func GetUserProfileByID(userID int) (*UserProfile, error) {
	pool, err := OpenDatabase()
	if err != nil {
		return nil, err
	}
	defer CloseDatabase(pool)

	var p UserProfile
	err = pool.QueryRow(context.Background(), `
		SELECT u.id, u.email, COALESCE(u.user_name, ''), COALESCE(u.timezone, 'America/New_York'),
		       COALESCE(u.items_per_page, 15), u.role_id, COALESCE(r.permissions, '{}'),
		       COALESCE(u.digest_enabled, false), COALESCE(u.digest_hour, 8)
		FROM users u
		LEFT JOIN roles r ON r.id = u.role_id
		WHERE u.id = $1`, userID).Scan(
		&p.ID, &p.Email, &p.UserName, &p.Timezone, &p.ItemsPerPage, &p.RoleID, &p.Permissions,
		&p.DigestEnabled, &p.DigestHour,
	)
	if err != nil {
		return nil, err
	}
	if p.Permissions == nil {
		p.Permissions = []string{}
	}
	return &p, nil
}

// UpdateUserProfileByID updates mutable profile fields for a user.
func UpdateUserProfileByID(userID int, userName, timezone string, itemsPerPage int, digestEnabled bool, digestHour int) error {
	pool, err := OpenDatabase()
	if err != nil {
		return err
	}
	defer CloseDatabase(pool)

	_, err = pool.Exec(context.Background(), `
		UPDATE users SET user_name = $1, timezone = $2, items_per_page = $3,
		       digest_enabled = $4, digest_hour = $5
		WHERE id = $6`,
		userName, timezone, itemsPerPage, digestEnabled, digestHour, userID)
	return err
}

// GetPasswordHashByID returns the bcrypt hash for a user id.
func GetPasswordHashByID(userID int) (string, error) {
	pool, err := OpenDatabase()
	if err != nil {
		return "", err
	}
	defer CloseDatabase(pool)
	var hash string
	err = pool.QueryRow(context.Background(), `SELECT password FROM users WHERE id = $1`, userID).Scan(&hash)
	return hash, err
}

// UpdatePasswordByID sets a new password hash for a user.
func UpdatePasswordByID(userID int, hashedPassword string) error {
	pool, err := OpenDatabase()
	if err != nil {
		return err
	}
	defer CloseDatabase(pool)
	_, err = pool.Exec(context.Background(), `UPDATE users SET password = $1 WHERE id = $2`, hashedPassword, userID)
	return err
}

// LookupInvite returns invite id and whether it was already used.
func LookupInvite(email, token string) (id int, used bool, err error) {
	pool, err := OpenDatabase()
	if err != nil {
		return 0, false, err
	}
	defer CloseDatabase(pool)

	var inviteUsed int
	err = pool.QueryRow(context.Background(),
		`SELECT id, inviteused FROM invites WHERE email = $1 AND token = $2`,
		strings.TrimSpace(email), strings.TrimSpace(token)).Scan(&id, &inviteUsed)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return 0, false, pgx.ErrNoRows
		}
		return 0, false, err
	}
	return id, inviteUsed == 1, nil
}

// RegisterUser creates a user and optionally consumes an invite in one transaction.
// inviteID <= 0 skips invite updates.
func RegisterUser(email, hashedPassword, timezone string, roleID, inviteID int) (int, error) {
	pool, err := OpenDatabase()
	if err != nil {
		return 0, err
	}
	defer CloseDatabase(pool)

	if timezone == "" {
		timezone = "America/New_York"
	}
	ctx := context.Background()
	tx, err := pool.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(ctx)

	var id int
	err = tx.QueryRow(ctx,
		`INSERT INTO users (email, password, role_id, timezone)
		 VALUES ($1, $2, $3, $4) RETURNING id`,
		strings.TrimSpace(email), hashedPassword, roleID, timezone).Scan(&id)
	if err != nil {
		return 0, fmt.Errorf("register user: %w", err)
	}
	if inviteID > 0 {
		if _, err := tx.Exec(ctx, `UPDATE invites SET inviteused = 1 WHERE id = $1`, inviteID); err != nil {
			return 0, fmt.Errorf("consume invite: %w", err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, err
	}
	return id, nil
}

// GetAuthCredentials loads password hash, role id, and timezone for login.
func GetAuthCredentials(email string) (hashedPassword string, roleID int, timezone string, err error) {
	pool, err := OpenDatabase()
	if err != nil {
		return "", 0, "", err
	}
	defer CloseDatabase(pool)

	err = pool.QueryRow(context.Background(),
		`SELECT password, role_id, COALESCE(timezone, 'America/New_York') FROM users WHERE email = $1`,
		strings.TrimSpace(email)).Scan(&hashedPassword, &roleID, &timezone)
	return hashedPassword, roleID, timezone, err
}
