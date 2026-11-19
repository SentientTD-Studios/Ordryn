package storage

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

// Chat account requests: a chat server allowed to create accounts
// (ChatServer.AccountCreate) asks Ordryn to make one for someone who signed up
// on it. Ordryn emails that address a link to choose a password. Only setting
// the password through that link confirms the request, so a chat account is
// linked to an Ordryn account only once its email's owner has acted on an
// email naming that chat account.

var chatAccountTables = []string{
	`CREATE TABLE IF NOT EXISTS chat_account_requests (
		id SERIAL PRIMARY KEY,
		chat_server_id INTEGER NOT NULL REFERENCES chat_servers(id) ON DELETE CASCADE,
		chat_user_ref TEXT NOT NULL,
		email TEXT NOT NULL,
		-- The account made for the request; NULL if the email was already in use
		-- (outcome 'existing') or the account was deleted since.
		user_id INTEGER REFERENCES users(id) ON DELETE SET NULL,
		outcome TEXT NOT NULL,
		-- The welcome link's password_reset id; using that link confirms the request.
		reset_id TEXT NOT NULL DEFAULT '',
		created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
		confirmed_at TIMESTAMPTZ,
		UNIQUE (chat_server_id, chat_user_ref)
	)`,
	`CREATE INDEX IF NOT EXISTS chat_account_requests_reset ON chat_account_requests (reset_id) WHERE reset_id <> ''`,
	`CREATE INDEX IF NOT EXISTS chat_account_requests_recent ON chat_account_requests (chat_server_id, created_at)`,
}

const (
	// ChatAccountCreated: Ordryn made a new account and emailed its welcome link.
	ChatAccountCreated = "created"
	// ChatAccountExisting: the email already had an account; nothing was created.
	ChatAccountExisting = "existing"
)

// Chat account request statuses reported to the chat server.
const (
	ChatAccountStatusPending    = "pending"      // waiting for the person to choose a password
	ChatAccountStatusReady      = "ready"        // confirmed: link the chat account to UserID
	ChatAccountStatusEmailInUse = "email_in_use" // the person should link their existing account
	ChatAccountStatusExpired    = "expired"      // the welcome link lapsed unused
	ChatAccountStatusGone       = "gone"         // the account was deleted or deactivated
)

// ErrChatAccountRequestNotFound means no request with that id belongs to the chat server.
var ErrChatAccountRequestNotFound = errors.New("chat account request not found")

// ChatAccountRequest is one request and what came of it.
type ChatAccountRequest struct {
	ID           int
	ChatServerID int
	ChatUserRef  string
	Email        string
	UserID       *int
	Outcome      string
	ResetID      string
	CreatedAt    time.Time
	ConfirmedAt  *time.Time
	// From the account, when there is one.
	Verified bool
	Banned   bool
}

// Status is what the chat server is told. linkTTL is how long the welcome link lasts.
func (r *ChatAccountRequest) Status(linkTTL time.Duration) string {
	switch {
	case r.Outcome == ChatAccountExisting:
		return ChatAccountStatusEmailInUse
	case r.UserID == nil || r.Banned:
		return ChatAccountStatusGone
	case r.ConfirmedAt != nil && r.Verified:
		return ChatAccountStatusReady
	case time.Since(r.CreatedAt) > linkTTL:
		return ChatAccountStatusExpired
	default:
		return ChatAccountStatusPending
	}
}

const chatAccountRequestSelect = `
	SELECT r.id, r.chat_server_id, r.chat_user_ref, r.email, u.id, r.outcome, r.reset_id, r.created_at, r.confirmed_at,
	       COALESCE(u.email_verified_at IS NOT NULL, FALSE), COALESCE(u.is_banned, FALSE)
	FROM chat_account_requests r LEFT JOIN users u ON u.id = r.user_id`

func scanChatAccountRequest(row pgx.Row) (*ChatAccountRequest, error) {
	var r ChatAccountRequest
	err := row.Scan(&r.ID, &r.ChatServerID, &r.ChatUserRef, &r.Email, &r.UserID, &r.Outcome, &r.ResetID,
		&r.CreatedAt, &r.ConfirmedAt, &r.Verified, &r.Banned)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrChatAccountRequestNotFound
	}
	if err != nil {
		return nil, err
	}
	return &r, nil
}

// GetChatAccountRequestByRef returns the chat server's request for chatUserRef.
func GetChatAccountRequestByRef(chatServerID int, chatUserRef string) (*ChatAccountRequest, error) {
	pool, err := OpenDatabase()
	if err != nil {
		return nil, err
	}
	defer CloseDatabase(pool)
	return scanChatAccountRequest(pool.QueryRow(context.Background(),
		chatAccountRequestSelect+` WHERE r.chat_server_id = $1 AND r.chat_user_ref = $2`, chatServerID, chatUserRef))
}

// ListChatAccountRequests returns the chat server's requests with these ids;
// ids that aren't its own are left out.
func ListChatAccountRequests(chatServerID int, ids []int) ([]ChatAccountRequest, error) {
	pool, err := OpenDatabase()
	if err != nil {
		return nil, err
	}
	defer CloseDatabase(pool)
	rows, err := pool.Query(context.Background(),
		chatAccountRequestSelect+` WHERE r.chat_server_id = $1 AND r.id = ANY($2) ORDER BY r.id`, chatServerID, ids)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ChatAccountRequest{}
	for rows.Next() {
		r, err := scanChatAccountRequest(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *r)
	}
	return out, rows.Err()
}

// CountRecentChatAccountRequests counts the chat server's requests since `since`.
func CountRecentChatAccountRequests(chatServerID int, since time.Time) (int, error) {
	pool, err := OpenDatabase()
	if err != nil {
		return 0, err
	}
	defer CloseDatabase(pool)
	var n int
	err = pool.QueryRow(context.Background(),
		`SELECT count(*) FROM chat_account_requests WHERE chat_server_id = $1 AND created_at >= $2`, chatServerID, since).Scan(&n)
	return n, err
}

// ReserveChatAccountRequest records a request before any account is made, so
// a retried or concurrent request for the same chatUserRef can't make a second
// one. If the chat server already has a request for chatUserRef, it returns
// that one and reserved=false.
func ReserveChatAccountRequest(chatServerID int, chatUserRef, email, outcome string) (req *ChatAccountRequest, reserved bool, err error) {
	pool, err := OpenDatabase()
	if err != nil {
		return nil, false, err
	}
	defer CloseDatabase(pool)
	var id int
	err = pool.QueryRow(context.Background(),
		`INSERT INTO chat_account_requests (chat_server_id, chat_user_ref, email, outcome)
		 VALUES ($1, $2, $3, $4)
		 ON CONFLICT (chat_server_id, chat_user_ref) DO NOTHING RETURNING id`,
		chatServerID, chatUserRef, NormalizeEmail(email), outcome).Scan(&id)
	reserved = err == nil
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return nil, false, err
	}
	r, err := GetChatAccountRequestByRef(chatServerID, chatUserRef)
	return r, reserved, err
}

// SetChatAccountRequestUser records the account made for a reserved request
// and its welcome link's password_reset id.
func SetChatAccountRequestUser(id, userID int, resetID string) error {
	pool, err := OpenDatabase()
	if err != nil {
		return err
	}
	defer CloseDatabase(pool)
	_, err = pool.Exec(context.Background(),
		`UPDATE chat_account_requests SET user_id = $2, reset_id = $3 WHERE id = $1`, id, userID, resetID)
	return err
}

// DeleteChatAccountRequest drops a reservation whose account couldn't be made.
func DeleteChatAccountRequest(id int) error {
	pool, err := OpenDatabase()
	if err != nil {
		return err
	}
	defer CloseDatabase(pool)
	_, err = pool.Exec(context.Background(), `DELETE FROM chat_account_requests WHERE id = $1`, id)
	return err
}

// ConfirmChatAccountRequest marks the request whose welcome link (password
// reset resetID) was just used by userID. It does nothing for other resets.
func ConfirmChatAccountRequest(resetID string, userID int) error {
	if strings.TrimSpace(resetID) == "" {
		return nil
	}
	pool, err := OpenDatabase()
	if err != nil {
		return err
	}
	defer CloseDatabase(pool)
	_, err = pool.Exec(context.Background(),
		`UPDATE chat_account_requests SET confirmed_at = NOW()
		 WHERE reset_id = $1 AND user_id = $2 AND confirmed_at IS NULL`, resetID, userID)
	if err != nil {
		return fmt.Errorf("confirm chat account request: %w", err)
	}
	return nil
}
