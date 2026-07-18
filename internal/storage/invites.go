package storage

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
)

// Invite is an invite-row for admin/API clients.
type Invite struct {
	ID    int
	Email string
	Token string
	Used  bool
}

// ListInvites returns invites newest-first.
func ListInvites() ([]Invite, error) {
	pool, err := OpenDatabase()
	if err != nil {
		return nil, err
	}
	defer CloseDatabase(pool)

	rows, err := pool.Query(context.Background(),
		`SELECT id, email, token, inviteused FROM invites ORDER BY id DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []Invite
	for rows.Next() {
		var inv Invite
		var used int
		if err := rows.Scan(&inv.ID, &inv.Email, &inv.Token, &used); err != nil {
			return nil, err
		}
		inv.Used = used == 1
		out = append(out, inv)
	}
	return out, nil
}

// CreateInvite inserts a new unused invite and returns it.
func CreateInvite(email string) (*Invite, error) {
	email = strings.TrimSpace(strings.ToLower(email))
	if email == "" {
		return nil, fmt.Errorf("email is required")
	}
	pool, err := OpenDatabase()
	if err != nil {
		return nil, err
	}
	defer CloseDatabase(pool)

	var existingID int
	err = pool.QueryRow(context.Background(), `SELECT id FROM invites WHERE email = $1`, email).Scan(&existingID)
	if err == nil {
		return nil, fmt.Errorf("invite already exists for email")
	}
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
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
		`INSERT INTO invites (email, token, inviteused) VALUES ($1, $2, 0)
		 RETURNING id, email, token, inviteused`, email, token).Scan(&inv.ID, &inv.Email, &inv.Token, &used)
	if err != nil {
		return nil, err
	}
	inv.Used = used == 1
	return &inv, nil
}

// DeleteInvite removes an unused invite by id.
func DeleteInvite(id int) error {
	pool, err := OpenDatabase()
	if err != nil {
		return err
	}
	defer CloseDatabase(pool)

	var used int
	if err := pool.QueryRow(context.Background(), `SELECT inviteused FROM invites WHERE id = $1`, id).Scan(&used); err != nil {
		return fmt.Errorf("invite not found")
	}
	if used == 1 {
		return fmt.Errorf("cannot delete a used invite")
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
