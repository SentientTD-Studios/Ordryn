package storage

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
)

// Project links: a registered chat server (with "task events" allowed) links
// one of its channels to a project on behalf of an Ordryn user who can manage
// that project. The project's activity is queued in chat_events for that chat
// server, which reads it with its client credentials. Every event re-checks
// that the user who made the link can still manage the project, so a link
// can't outlive that user's access.
var chatLinkTables = []string{
	`CREATE TABLE IF NOT EXISTS chat_project_links (
		chat_server_id INTEGER NOT NULL REFERENCES chat_servers(id) ON DELETE CASCADE,
		link_id TEXT NOT NULL,
		project_id INTEGER NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
		user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
		active BOOLEAN NOT NULL DEFAULT TRUE,
		paused_reason TEXT NOT NULL DEFAULT '',
		created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
		updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
		PRIMARY KEY (chat_server_id, link_id)
	)`,
	`CREATE INDEX IF NOT EXISTS chat_project_links_project ON chat_project_links (project_id)`,
	`CREATE TABLE IF NOT EXISTS chat_events (
		id BIGSERIAL PRIMARY KEY,
		chat_server_id INTEGER NOT NULL REFERENCES chat_servers(id) ON DELETE CASCADE,
		link_id TEXT NOT NULL,
		payload JSONB NOT NULL,
		created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
	)`,
	`CREATE INDEX IF NOT EXISTS chat_events_server ON chat_events (chat_server_id, id)`,
	`CREATE INDEX IF NOT EXISTS chat_events_created ON chat_events (created_at)`,
}

// ChatEventRetention is how long undelivered chat events are kept.
const ChatEventRetention = 72 * time.Hour

// Reasons a link stops receiving events.
const (
	ChatLinkReasonNoAccess = "The person who linked this project can no longer manage it in Ordryn."
	ChatLinkReasonBanned   = "The person who linked this project is banned in Ordryn."
)

// ChatProject is a project as offered to a chat user for linking.
type ChatProject struct {
	ID        int    `json:"id"`
	Name      string `json:"name"`
	CanManage bool   `json:"can_manage"`
}

// ChatProjectsForUser lists the user's non-archived projects and whether they
// can manage each one (which linking a channel requires).
func ChatProjectsForUser(userID int) ([]ChatProject, error) {
	projects, err := GetAccessibleProjects(userID)
	if err != nil {
		return nil, err
	}
	out := []ChatProject{}
	for _, p := range projects {
		if p.Archived {
			continue
		}
		out = append(out, ChatProject{ID: p.ID, Name: p.Name, CanManage: RoleCanManageProject(p.ID, p.Role)})
	}
	return out, nil
}

// ChatLinkCheck reports whether userID may link (and keep linked) a chat
// channel to projectID. reason is empty when allowed. Missing projects and
// projects the user can't see are reported the same way.
func ChatLinkCheck(userID, projectID int) (projectName, reason string, err error) {
	if userID <= 0 || IsSystemUser(userID) {
		return "", ChatLinkReasonNoAccess, nil
	}
	pool, err := OpenDatabase()
	if err != nil {
		return "", "", err
	}
	defer CloseDatabase(pool)
	var banned, agent bool
	err = pool.QueryRow(context.Background(),
		`SELECT COALESCE(is_banned, FALSE), COALESCE(is_agent, FALSE) FROM users WHERE id = $1`, userID).Scan(&banned, &agent)
	if errors.Is(err, pgx.ErrNoRows) || agent {
		return "", ChatLinkReasonNoAccess, nil
	}
	if err != nil {
		return "", "", err
	}
	if banned {
		return "", ChatLinkReasonBanned, nil
	}
	err = pool.QueryRow(context.Background(), `SELECT name FROM projects WHERE id = $1`, projectID).Scan(&projectName)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ChatLinkReasonNoAccess, nil
	}
	if err != nil {
		return "", "", err
	}
	role, err := GetProjectRole(projectID, userID)
	if err != nil {
		return "", "", err
	}
	if role == "" || !RoleCanManageProject(projectID, role) {
		return projectName, ChatLinkReasonNoAccess, nil
	}
	return projectName, "", nil
}

// ChatLink is one channel ↔ project link as the chat server sees it.
type ChatLink struct {
	LinkID       string `json:"link_id"`
	ProjectID    int    `json:"project_id"`
	ProjectName  string `json:"project_name"`
	UserID       int    `json:"user_id"`
	Active       bool   `json:"active"`
	PausedReason string `json:"paused_reason,omitempty"`
}

// UpsertChatLink creates or re-points a link and marks it active.
func UpsertChatLink(chatServerID int, linkID string, projectID, userID int) error {
	pool, err := OpenDatabase()
	if err != nil {
		return err
	}
	defer CloseDatabase(pool)
	_, err = pool.Exec(context.Background(), `
		INSERT INTO chat_project_links (chat_server_id, link_id, project_id, user_id) VALUES ($1, $2, $3, $4)
		ON CONFLICT (chat_server_id, link_id) DO UPDATE
		SET project_id = EXCLUDED.project_id, user_id = EXCLUDED.user_id, active = TRUE, paused_reason = '', updated_at = NOW()`,
		chatServerID, linkID, projectID, userID)
	if err == nil {
		InvalidateChatLinkCount()
	}
	return err
}

// DeleteChatLink removes a link and any of its queued events.
func DeleteChatLink(chatServerID int, linkID string) error {
	pool, err := OpenDatabase()
	if err != nil {
		return err
	}
	defer CloseDatabase(pool)
	if _, err := pool.Exec(context.Background(),
		`DELETE FROM chat_project_links WHERE chat_server_id = $1 AND link_id = $2`, chatServerID, linkID); err != nil {
		return err
	}
	_, err = pool.Exec(context.Background(),
		`DELETE FROM chat_events WHERE chat_server_id = $1 AND link_id = $2`, chatServerID, linkID)
	InvalidateChatLinkCount()
	return err
}

// ListChatLinks returns every link a chat server has.
func ListChatLinks(chatServerID int) ([]ChatLink, error) {
	pool, err := OpenDatabase()
	if err != nil {
		return nil, err
	}
	defer CloseDatabase(pool)
	rows, err := pool.Query(context.Background(), `
		SELECT l.link_id, l.project_id, p.name, l.user_id, l.active, l.paused_reason
		FROM chat_project_links l JOIN projects p ON p.id = l.project_id
		WHERE l.chat_server_id = $1 ORDER BY l.created_at`, chatServerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ChatLink{}
	for rows.Next() {
		var l ChatLink
		if err := rows.Scan(&l.LinkID, &l.ProjectID, &l.ProjectName, &l.UserID, &l.Active, &l.PausedReason); err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	return out, rows.Err()
}

// ChatLinkTarget is a link that may receive a project's events.
type ChatLinkTarget struct {
	ChatServerID int
	LinkID       string
	UserID       int
	Active       bool
}

// ChatLinksForProject returns the links to a project on chat servers that are
// enabled and allowed task events.
func ChatLinksForProject(projectID int) ([]ChatLinkTarget, error) {
	pool, err := OpenDatabase()
	if err != nil {
		return nil, err
	}
	defer CloseDatabase(pool)
	rows, err := pool.Query(context.Background(), `
		SELECT l.chat_server_id, l.link_id, l.user_id, l.active
		FROM chat_project_links l JOIN chat_servers cs ON cs.id = l.chat_server_id
		WHERE l.project_id = $1 AND cs.enabled AND cs.task_events`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ChatLinkTarget{}
	for rows.Next() {
		var t ChatLinkTarget
		if err := rows.Scan(&t.ChatServerID, &t.LinkID, &t.UserID, &t.Active); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// SetChatLinkState pauses (reason != "") or resumes a link.
func SetChatLinkState(chatServerID int, linkID string, active bool, reason string) error {
	pool, err := OpenDatabase()
	if err != nil {
		return err
	}
	defer CloseDatabase(pool)
	_, err = pool.Exec(context.Background(), `
		UPDATE chat_project_links SET active = $3, paused_reason = $4, updated_at = NOW()
		WHERE chat_server_id = $1 AND link_id = $2`, chatServerID, linkID, active, reason)
	return err
}

var chatLinkCountMu sync.Mutex

var chatLinkCount struct {
	at time.Time
	n  int
}

// CountChatLinks returns how many links exist on enabled chat servers allowed
// task events. Cached for 30 seconds (and reset by link changes) because the
// event sink asks on every event.
func CountChatLinks() int {
	chatLinkCountMu.Lock()
	defer chatLinkCountMu.Unlock()
	if time.Since(chatLinkCount.at) < 30*time.Second {
		return chatLinkCount.n
	}
	n := 0
	if pool, err := OpenDatabase(); err == nil {
		_ = pool.QueryRow(context.Background(), `
			SELECT COUNT(*) FROM chat_project_links l JOIN chat_servers cs ON cs.id = l.chat_server_id
			WHERE cs.enabled AND cs.task_events`).Scan(&n)
		CloseDatabase(pool)
	}
	chatLinkCount.n, chatLinkCount.at = n, time.Now()
	return n
}

// InvalidateChatLinkCount makes the next CountChatLinks read the database.
func InvalidateChatLinkCount() {
	chatLinkCountMu.Lock()
	chatLinkCount.at = time.Time{}
	chatLinkCountMu.Unlock()
}

// InsertChatEvent queues an event for a chat server's link.
func InsertChatEvent(chatServerID int, linkID string, payload any) error {
	raw, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	pool, err := OpenDatabase()
	if err != nil {
		return err
	}
	defer CloseDatabase(pool)
	_, err = pool.Exec(context.Background(),
		`INSERT INTO chat_events (chat_server_id, link_id, payload) VALUES ($1, $2, $3)`, chatServerID, linkID, raw)
	return err
}

// ChatEvent is one queued event.
type ChatEvent struct {
	ID      int64           `json:"id"`
	LinkID  string          `json:"link_id"`
	Payload json.RawMessage `json:"payload"`
}

// TakeChatEvents acknowledges (deletes) a chat server's events up to and
// including after, drops anything past retention, and returns up to limit
// newer events in order.
func TakeChatEvents(chatServerID int, after int64, limit int) ([]ChatEvent, error) {
	pool, err := OpenDatabase()
	if err != nil {
		return nil, err
	}
	defer CloseDatabase(pool)
	ctx := context.Background()
	if after > 0 {
		if _, err := pool.Exec(ctx, `DELETE FROM chat_events WHERE chat_server_id = $1 AND id <= $2`, chatServerID, after); err != nil {
			return nil, err
		}
	}
	if _, err := pool.Exec(ctx, `DELETE FROM chat_events WHERE created_at < $1`, time.Now().Add(-ChatEventRetention)); err != nil {
		return nil, err
	}
	rows, err := pool.Query(ctx, `
		SELECT id, link_id, payload FROM chat_events WHERE chat_server_id = $1 AND id > $2 ORDER BY id LIMIT $3`,
		chatServerID, after, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ChatEvent{}
	for rows.Next() {
		var e ChatEvent
		if err := rows.Scan(&e.ID, &e.LinkID, &e.Payload); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}
