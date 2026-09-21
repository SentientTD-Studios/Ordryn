package storage

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

// UserProfile is the public account view for /api/v2/me and auth responses.
type UserProfile struct {
	ID                      int
	Email                   string
	UserName                string
	Timezone                string
	ItemsPerPage            int
	RoleID                  int
	Permissions             []string
	AllowProjectInvites     bool
	UsernameChangeAvailable bool
	MFAEnabled              bool
	AvatarURL               string
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
		       COALESCE(u.allow_project_invites, true),
		       COALESCE(u.username_change_available, false),
		       COALESCE(u.mfa_enabled, false),
		       COALESCE(u.avatar_url, '')
		FROM users u
		LEFT JOIN roles r ON r.id = u.role_id
		WHERE u.id = $1`, userID).Scan(
		&p.ID, &p.Email, &p.UserName, &p.Timezone, &p.ItemsPerPage, &p.RoleID, &p.Permissions,
		&p.AllowProjectInvites, &p.UsernameChangeAvailable,
		&p.MFAEnabled, &p.AvatarURL,
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
func UpdateUserProfileByID(userID int, userName, timezone string, itemsPerPage int, allowProjectInvites bool) error {
	pool, err := OpenDatabase()
	if err != nil {
		return err
	}
	defer CloseDatabase(pool)

	_, err = pool.Exec(context.Background(), `
		UPDATE users SET user_name = $1, timezone = $2, items_per_page = $3,
		       allow_project_invites = $4
		WHERE id = $5`,
		userName, timezone, itemsPerPage, allowProjectInvites, userID)
	return err
}

// UpdateUserAvatarURL updates the avatar_url column for a user.
func UpdateUserAvatarURL(userID int, avatarURL string) error {
	pool, err := OpenDatabase()
	if err != nil {
		return err
	}
	defer CloseDatabase(pool)

	_, err = pool.Exec(context.Background(), `UPDATE users SET avatar_url = $1 WHERE id = $2`, avatarURL, userID)
	return err
}

// UserHasPermission reports whether the user's role includes the named permission.
func UserHasPermission(userID int, permission string) bool {
	if userID <= 0 || permission == "" {
		return false
	}
	profile, err := GetUserProfileByID(userID)
	if err != nil {
		return false
	}
	for _, p := range profile.Permissions {
		if p == permission {
			return true
		}
	}
	return false
}

// UserAllowsProjectInvites reports whether the user accepts project invites (default true).
func UserAllowsProjectInvites(userID int) (bool, error) {
	pool, err := OpenDatabase()
	if err != nil {
		return false, err
	}
	defer CloseDatabase(pool)

	var allow bool
	err = pool.QueryRow(context.Background(),
		`SELECT COALESCE(allow_project_invites, true) FROM users WHERE id = $1`, userID).Scan(&allow)
	if err != nil {
		return false, err
	}
	return allow, nil
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

// LookupInvite returns invite id, whether it was already used, and its expiration time if set.
func LookupInvite(email, token string) (id int, used bool, expiresAt *time.Time, err error) {
	pool, err := OpenDatabase()
	if err != nil {
		return 0, false, nil, err
	}
	defer CloseDatabase(pool)

	var inviteUsed int
	err = pool.QueryRow(context.Background(),
		`SELECT id, inviteused, expires_at FROM invites WHERE email = $1 AND token = $2`,
		strings.TrimSpace(email), strings.TrimSpace(token)).Scan(&id, &inviteUsed, &expiresAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return 0, false, nil, pgx.ErrNoRows
		}
		return 0, false, nil, err
	}
	return id, inviteUsed == 1, expiresAt, nil
}

// RegisterUser creates a user and optionally consumes an invite in one transaction.
// inviteID <= 0 skips invite updates. userName must already be validated and unique.
func RegisterUser(email, hashedPassword, timezone, userName string, roleID, inviteID int) (int, error) {
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
		`INSERT INTO users (email, password, role_id, timezone, user_name, username_change_available)
		 VALUES ($1, $2, $3, $4, $5, FALSE) RETURNING id`,
		strings.TrimSpace(email), hashedPassword, roleID, timezone, strings.TrimSpace(userName)).Scan(&id)
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
