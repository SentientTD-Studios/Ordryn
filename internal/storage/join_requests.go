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
	"github.com/jackc/pgx/v5/pgconn"
)

const (
	JoinRequestPending  = "pending"
	JoinRequestApproved = "approved"
	JoinRequestDenied   = "denied"
)

var (
	ErrJoinRequestNotFound   = errors.New("join request not found")
	ErrJoinRequestNotPending = errors.New("join request is not pending")
)

// JoinRequest is a visitor's request to join the site.
type JoinRequest struct {
	ID          int
	Email       string
	Message     string
	Status      string
	InviteID    *int
	CreatedAt   time.Time
	ReviewedAt  *time.Time
	ReviewedBy  *int
	InviteToken string
}

// CreateJoinRequestsTable ensures the join_requests table exists.
func CreateJoinRequestsTable() error {
	pool, err := OpenDatabase()
	if err != nil {
		return err
	}
	defer CloseDatabase(pool)

	stmts := []string{
		`CREATE TABLE IF NOT EXISTS join_requests (
			id SERIAL PRIMARY KEY,
			email TEXT NOT NULL,
			message TEXT NOT NULL DEFAULT '',
			status TEXT NOT NULL DEFAULT 'pending',
			invite_id INTEGER REFERENCES invites(id) ON DELETE SET NULL,
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			reviewed_at TIMESTAMPTZ,
			reviewed_by INTEGER REFERENCES users(id) ON DELETE SET NULL
		)`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_join_requests_pending_email
			ON join_requests (email) WHERE status = 'pending'`,
		`CREATE INDEX IF NOT EXISTS idx_join_requests_created
			ON join_requests (created_at DESC)`,
	}
	for _, s := range stmts {
		if _, err := pool.Exec(context.Background(), s); err != nil {
			return fmt.Errorf("failed to create join_requests: %v", err)
		}
	}
	return nil
}

// HasPendingJoinRequest reports whether a pending request exists for email.
func HasPendingJoinRequest(email string) (bool, error) {
	pool, err := OpenDatabase()
	if err != nil {
		return false, err
	}
	defer CloseDatabase(pool)

	var exists bool
	err = pool.QueryRow(context.Background(),
		`SELECT EXISTS(SELECT 1 FROM join_requests WHERE email = $1 AND status = $2)`,
		strings.TrimSpace(strings.ToLower(email)), JoinRequestPending).Scan(&exists)
	if err != nil {
		return false, err
	}
	return exists, nil
}

// HasUnusedInvite reports whether an unused site invite exists for email.
func HasUnusedInvite(email string) (bool, error) {
	pool, err := OpenDatabase()
	if err != nil {
		return false, err
	}
	defer CloseDatabase(pool)

	var exists bool
	err = pool.QueryRow(context.Background(),
		`SELECT EXISTS(SELECT 1 FROM invites WHERE email = $1 AND inviteused = 0)`,
		strings.TrimSpace(strings.ToLower(email))).Scan(&exists)
	if err != nil {
		return false, err
	}
	return exists, nil
}

// CreateJoinRequest inserts a pending request. Duplicate pending rows are ignored.
func CreateJoinRequest(email, message string) error {
	email = strings.TrimSpace(strings.ToLower(email))
	message = strings.TrimSpace(message)

	pool, err := OpenDatabase()
	if err != nil {
		return err
	}
	defer CloseDatabase(pool)

	_, err = pool.Exec(context.Background(),
		`INSERT INTO join_requests (email, message, status) VALUES ($1, $2, $3)`,
		email, message, JoinRequestPending)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return nil
		}
		return err
	}
	return nil
}

func scanJoinRequest(scan func(dest ...any) error) (JoinRequest, error) {
	var jr JoinRequest
	var inviteID, reviewedBy *int
	var reviewedAt *time.Time
	var inviteToken *string
	if err := scan(
		&jr.ID, &jr.Email, &jr.Message, &jr.Status, &inviteID,
		&jr.CreatedAt, &reviewedAt, &reviewedBy, &inviteToken,
	); err != nil {
		return JoinRequest{}, err
	}
	jr.InviteID = inviteID
	jr.ReviewedAt = reviewedAt
	jr.ReviewedBy = reviewedBy
	if inviteToken != nil {
		jr.InviteToken = *inviteToken
	}
	return jr, nil
}

const joinRequestSelect = `
	SELECT jr.id, jr.email, jr.message, jr.status, jr.invite_id,
	       jr.created_at, jr.reviewed_at, jr.reviewed_by, i.token
	FROM join_requests jr
	LEFT JOIN invites i ON i.id = jr.invite_id`

// ListJoinRequests returns pending requests first, then newest reviewed.
func ListJoinRequests() ([]JoinRequest, error) {
	pool, err := OpenDatabase()
	if err != nil {
		return nil, err
	}
	defer CloseDatabase(pool)

	rows, err := pool.Query(context.Background(),
		joinRequestSelect+`
		ORDER BY CASE WHEN jr.status = 'pending' THEN 0 ELSE 1 END, jr.created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []JoinRequest
	for rows.Next() {
		jr, err := scanJoinRequest(rows.Scan)
		if err != nil {
			return nil, err
		}
		out = append(out, jr)
	}
	return out, rows.Err()
}

// GetJoinRequest loads a single request by id.
func GetJoinRequest(id int) (*JoinRequest, error) {
	pool, err := OpenDatabase()
	if err != nil {
		return nil, err
	}
	defer CloseDatabase(pool)

	jr, err := scanJoinRequest(pool.QueryRow(context.Background(),
		joinRequestSelect+` WHERE jr.id = $1`, id).Scan)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrJoinRequestNotFound
		}
		return nil, err
	}
	return &jr, nil
}

// ApproveJoinRequest creates (or reuses) a site invite and marks the request approved.
func ApproveJoinRequest(id, reviewerID int) (*JoinRequest, *Invite, error) {
	pool, err := OpenDatabase()
	if err != nil {
		return nil, nil, err
	}
	defer CloseDatabase(pool)

	tx, err := pool.Begin(context.Background())
	if err != nil {
		return nil, nil, err
	}
	defer func() { _ = tx.Rollback(context.Background()) }()

	var jr JoinRequest
	var inviteID *int
	err = tx.QueryRow(context.Background(),
		`SELECT id, email, message, status, invite_id FROM join_requests WHERE id = $1 FOR UPDATE`,
		id).Scan(&jr.ID, &jr.Email, &jr.Message, &jr.Status, &inviteID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil, ErrJoinRequestNotFound
		}
		return nil, nil, err
	}
	if jr.Status != JoinRequestPending {
		return nil, nil, ErrJoinRequestNotPending
	}

	inv := Invite{Email: jr.Email}
	err = tx.QueryRow(context.Background(),
		`SELECT id, email, token, inviteused FROM invites WHERE email = $1 AND inviteused = 0`,
		jr.Email).Scan(&inv.ID, &inv.Email, &inv.Token, new(int))
	if errors.Is(err, pgx.ErrNoRows) {
		tokenBytes := make([]byte, 16)
		if _, err := rand.Read(tokenBytes); err != nil {
			return nil, nil, err
		}
		inv.Token = hex.EncodeToString(tokenBytes)
		if err := tx.QueryRow(context.Background(),
			`INSERT INTO invites (email, token, inviteused, is_join_request) VALUES ($1, $2, 0, TRUE)
			 RETURNING id, email, token`, jr.Email, inv.Token).Scan(&inv.ID, &inv.Email, &inv.Token); err != nil {
			return nil, nil, err
		}
	} else if err != nil {
		return nil, nil, err
	} else {
		_, _ = tx.Exec(context.Background(), `UPDATE invites SET is_join_request = TRUE WHERE id = $1`, inv.ID)
	}

	var reviewer any
	if reviewerID > 0 {
		reviewer = reviewerID
	}
	if err := tx.QueryRow(context.Background(),
		`UPDATE join_requests
		 SET status = $1, invite_id = $2, reviewed_at = NOW(), reviewed_by = $3
		 WHERE id = $4
		 RETURNING id, email, message, status, invite_id, created_at, reviewed_at, reviewed_by`,
		JoinRequestApproved, inv.ID, reviewer, id).Scan(
		&jr.ID, &jr.Email, &jr.Message, &jr.Status, &jr.InviteID,
		&jr.CreatedAt, &jr.ReviewedAt, &jr.ReviewedBy); err != nil {
		return nil, nil, err
	}
	jr.InviteToken = inv.Token
	if err := tx.Commit(context.Background()); err != nil {
		return nil, nil, err
	}
	inv.Used = false
	return &jr, &inv, nil
}

// DenyJoinRequest marks a pending request as denied.
func DenyJoinRequest(id, reviewerID int) (*JoinRequest, error) {
	pool, err := OpenDatabase()
	if err != nil {
		return nil, err
	}
	defer CloseDatabase(pool)

	tx, err := pool.Begin(context.Background())
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(context.Background()) }()

	var status string
	err = tx.QueryRow(context.Background(),
		`SELECT status FROM join_requests WHERE id = $1 FOR UPDATE`, id).Scan(&status)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrJoinRequestNotFound
		}
		return nil, err
	}
	if status != JoinRequestPending {
		return nil, ErrJoinRequestNotPending
	}

	var reviewer any
	if reviewerID > 0 {
		reviewer = reviewerID
	}
	jr, err := scanJoinRequest(tx.QueryRow(context.Background(),
		`UPDATE join_requests
		 SET status = $1, reviewed_at = NOW(), reviewed_by = $2
		 WHERE id = $3
		 RETURNING id, email, message, status, invite_id, created_at, reviewed_at, reviewed_by, NULL::text`,
		JoinRequestDenied, reviewer, id).Scan)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(context.Background()); err != nil {
		return nil, err
	}
	return &jr, nil
}

// ListAdminEmails returns emails of unbanned users with the admin permission.
func ListAdminEmails() ([]string, error) {
	pool, err := OpenDatabase()
	if err != nil {
		return nil, err
	}
	defer CloseDatabase(pool)

	rows, err := pool.Query(context.Background(),
		`SELECT u.email FROM users u
		 JOIN roles r ON r.id = u.role_id
		 WHERE r.permissions @> ARRAY['admin']::text[]
		   AND COALESCE(u.is_banned, FALSE) = FALSE`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []string
	for rows.Next() {
		var email string
		if err := rows.Scan(&email); err != nil {
			return nil, err
		}
		if strings.TrimSpace(email) != "" {
			out = append(out, email)
		}
	}
	return out, rows.Err()
}

// ListAdminUserIDs returns IDs of unbanned users with the admin permission.
func ListAdminUserIDs() ([]int, error) {
	pool, err := OpenDatabase()
	if err != nil {
		return nil, err
	}
	defer CloseDatabase(pool)

	rows, err := pool.Query(context.Background(),
		`SELECT u.id FROM users u
		 JOIN roles r ON r.id = u.role_id
		 WHERE r.permissions @> ARRAY['admin']::text[]
		   AND COALESCE(u.is_banned, FALSE) = FALSE`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []int
	for rows.Next() {
		var id int
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		if id > 0 {
			out = append(out, id)
		}
	}
	return out, rows.Err()
}
