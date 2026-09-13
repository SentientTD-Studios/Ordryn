package storage

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

// Invite is an invite-row for admin/API clients.
type Invite struct {
	ID              int
	Email           string
	Token           string
	Used            bool
	CreatedAt       time.Time
	ExpiresAt       *time.Time
	CreatedBy       *int
	CreatorEmail    string
	CreatorUserName string
	IsJoinRequest   bool
}

// Status returns 'used', 'expired', or 'pending'.
func (inv *Invite) Status() string {
	if inv.Used {
		return "used"
	}
	if inv.ExpiresAt != nil && time.Now().After(*inv.ExpiresAt) {
		return "expired"
	}
	return "pending"
}

// ListInvites returns invites newest-first. Maintained for backwards compatibility,
// excluding join requests.
func ListInvites() ([]Invite, error) {
	return ListAllInvites()
}

// ListAllInvites returns all site invites newest-first, excluding join requests.
func ListAllInvites() ([]Invite, error) {
	pool, err := OpenDatabase()
	if err != nil {
		return nil, err
	}
	defer CloseDatabase(pool)

	rows, err := pool.Query(context.Background(),
		`SELECT i.id, i.email, i.token, i.inviteused,
		        COALESCE(i.created_at, NOW()), i.expires_at, i.created_by,
		        COALESCE(u.email, ''), COALESCE(u.user_name, ''),
		        COALESCE(i.is_join_request, FALSE)
		 FROM invites i
		 LEFT JOIN users u ON u.id = i.created_by
		 WHERE COALESCE(i.is_join_request, FALSE) = FALSE
		 ORDER BY i.id DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []Invite
	for rows.Next() {
		var inv Invite
		var used int
		if err := rows.Scan(&inv.ID, &inv.Email, &inv.Token, &used,
			&inv.CreatedAt, &inv.ExpiresAt, &inv.CreatedBy,
			&inv.CreatorEmail, &inv.CreatorUserName,
			&inv.IsJoinRequest); err != nil {
			return nil, err
		}
		inv.Used = used == 1
		out = append(out, inv)
	}
	return out, nil
}

// ListInvitesForUser returns non-join-request invites created by a specific user.
func ListInvitesForUser(userID int) ([]Invite, error) {
	pool, err := OpenDatabase()
	if err != nil {
		return nil, err
	}
	defer CloseDatabase(pool)

	rows, err := pool.Query(context.Background(),
		`SELECT i.id, i.email, i.token, i.inviteused,
		        COALESCE(i.created_at, NOW()), i.expires_at, i.created_by,
		        COALESCE(u.email, ''), COALESCE(u.user_name, ''),
		        COALESCE(i.is_join_request, FALSE)
		 FROM invites i
		 LEFT JOIN users u ON u.id = i.created_by
		 WHERE i.created_by = $1 AND COALESCE(i.is_join_request, FALSE) = FALSE
		 ORDER BY i.id DESC`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []Invite
	for rows.Next() {
		var inv Invite
		var used int
		if err := rows.Scan(&inv.ID, &inv.Email, &inv.Token, &used,
			&inv.CreatedAt, &inv.ExpiresAt, &inv.CreatedBy,
			&inv.CreatorEmail, &inv.CreatorUserName,
			&inv.IsJoinRequest); err != nil {
			return nil, err
		}
		inv.Used = used == 1
		out = append(out, inv)
	}
	return out, nil
}

// CountActiveInvitesByUser counts how many invites a user has sent that are active or used
// (i.e. not expired without being used, and not deleted).
func CountActiveInvitesByUser(userID int) (int, error) {
	pool, err := OpenDatabase()
	if err != nil {
		return 0, err
	}
	defer CloseDatabase(pool)

	var count int
	err = pool.QueryRow(context.Background(),
		`SELECT COUNT(*) FROM invites
		 WHERE created_by = $1
		   AND COALESCE(is_join_request, FALSE) = FALSE
		   AND (inviteused = 1 OR expires_at IS NULL OR expires_at > NOW())`, userID).Scan(&count)
	if err != nil {
		return 0, err
	}
	return count, nil
}

// CreateInvite inserts a new unused invite and returns it.
func CreateInvite(email string, createdBy *int, expiresAt *time.Time, isJoinRequest bool) (*Invite, error) {
	email = strings.TrimSpace(strings.ToLower(email))
	if email == "" {
		return nil, fmt.Errorf("email is required")
	}
	if !strings.Contains(email, "@") {
		return nil, fmt.Errorf("invalid email address")
	}

	pool, err := OpenDatabase()
	if err != nil {
		return nil, err
	}
	defer CloseDatabase(pool)

	// Check if user already exists
	var userExists bool
	_ = pool.QueryRow(context.Background(), `SELECT EXISTS(SELECT 1 FROM users WHERE LOWER(email) = $1)`, email).Scan(&userExists)
	if userExists {
		return nil, fmt.Errorf("a user with this email already exists")
	}

	var existingID int
	err = pool.QueryRow(context.Background(), `SELECT id FROM invites WHERE email = $1`, email).Scan(&existingID)
	if err == nil {
		return nil, fmt.Errorf("invite already exists for email")
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return nil, err
	}

	tokenBytes := make([]byte, 16)
	if _, err := rand.Read(tokenBytes); err != nil {
		return nil, err
	}
	token := hex.EncodeToString(tokenBytes)

	var inv Invite
	var used int
	err = pool.QueryRow(context.Background(),
		`INSERT INTO invites (email, token, inviteused, created_by, expires_at, is_join_request)
		 VALUES ($1, $2, 0, $3, $4, $5)
		 RETURNING id, email, token, inviteused, created_at, expires_at, created_by, is_join_request`,
		email, token, createdBy, expiresAt, isJoinRequest).Scan(
		&inv.ID, &inv.Email, &inv.Token, &used, &inv.CreatedAt, &inv.ExpiresAt, &inv.CreatedBy, &inv.IsJoinRequest)
	if err != nil {
		return nil, err
	}
	inv.Used = used == 1

	if createdBy != nil && *createdBy > 0 {
		_ = pool.QueryRow(context.Background(),
			`SELECT COALESCE(email, ''), COALESCE(user_name, '') FROM users WHERE id = $1`, *createdBy).
			Scan(&inv.CreatorEmail, &inv.CreatorUserName)
	}

	return &inv, nil
}

// DeleteInvite removes an unused invite by id, verifying permissions.
func DeleteInvite(id int, reqUserID int, isAdmin bool) error {
	pool, err := OpenDatabase()
	if err != nil {
		return err
	}
	defer CloseDatabase(pool)

	var used int
	var createdBy *int
	if err := pool.QueryRow(context.Background(),
		`SELECT inviteused, created_by FROM invites WHERE id = $1`, id).Scan(&used, &createdBy); err != nil {
		return fmt.Errorf("invite not found")
	}
	if used == 1 {
		return fmt.Errorf("cannot delete a used invite")
	}
	if !isAdmin {
		if createdBy == nil || *createdBy != reqUserID {
			return fmt.Errorf("not authorized to delete this invite")
		}
	}
	tag, err := pool.Exec(context.Background(), `DELETE FROM invites WHERE id = $1 AND inviteused = 0`, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("invite not found")
	}
	return nil
}

// SetUserBanned updates ban state for a user by id.
func SetUserBanned(userID int, banned bool) error {
	pool, err := OpenDatabase()
	if err != nil {
		return err
	}
	defer CloseDatabase(pool)
	tag, err := pool.Exec(context.Background(),
		`UPDATE users SET is_banned = $1 WHERE id = $2`, banned, userID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("user not found")
	}
	if banned {
		_ = ClearCalendarToken(userID)
	}
	return nil
}
