package storage

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
)

// Tasks from chat messages: a chat server allowed "task creation" creates a
// task in a linked project on behalf of one of its users, passing a source
// key (e.g. "chatmsg_42") that is unique per chat server. The key makes the
// call idempotent: asking again returns the same task, until that task is
// deleted, when the key is free again.

var chatTaskTables = []string{
	`CREATE TABLE IF NOT EXISTS chat_task_sources (
		chat_server_id INTEGER NOT NULL REFERENCES chat_servers(id) ON DELETE CASCADE,
		source_key TEXT NOT NULL,
		link_id TEXT NOT NULL,
		task_id INTEGER REFERENCES tasks(id) ON DELETE CASCADE, -- NULL while being created
		created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
		PRIMARY KEY (chat_server_id, source_key)
	)`,
	`CREATE INDEX IF NOT EXISTS chat_task_sources_task ON chat_task_sources (task_id) WHERE task_id IS NOT NULL`,
}

// ErrChatTaskInProgress means another request is creating the task for this source key.
var ErrChatTaskInProgress = errors.New("a task is already being created for this message")

// chatTaskReservationTTL is how long an unfinished reservation blocks its key.
const chatTaskReservationTTL = 2 * time.Minute

// ReserveChatTaskSource claims sourceKey for a new task. If the key already
// has a live (not deleted) task, it returns that task's id instead and claims
// nothing. A key whose task was deleted, or whose reservation was abandoned,
// is reclaimed.
func ReserveChatTaskSource(chatServerID int, sourceKey, linkID string) (existingTaskID int, err error) {
	pool, err := OpenDatabase()
	if err != nil {
		return 0, err
	}
	defer CloseDatabase(pool)
	ctx := context.Background()
	var taskID *int
	var alive bool
	var createdAt time.Time
	err = pool.QueryRow(ctx, `
		SELECT s.task_id, (t.id IS NOT NULL AND t.deleted_at IS NULL), s.created_at
		FROM chat_task_sources s LEFT JOIN tasks t ON t.id = s.task_id
		WHERE s.chat_server_id = $1 AND s.source_key = $2`, chatServerID, sourceKey).Scan(&taskID, &alive, &createdAt)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
	case err != nil:
		return 0, err
	case taskID != nil && alive:
		return *taskID, nil
	case taskID == nil && time.Since(createdAt) < chatTaskReservationTTL:
		return 0, ErrChatTaskInProgress
	default: // its task was deleted, or the reservation was abandoned
		if _, err := pool.Exec(ctx, `
			DELETE FROM chat_task_sources WHERE chat_server_id = $1 AND source_key = $2 AND created_at = $3`,
			chatServerID, sourceKey, createdAt); err != nil {
			return 0, err
		}
	}
	tag, err := pool.Exec(ctx, `
		INSERT INTO chat_task_sources (chat_server_id, source_key, link_id) VALUES ($1, $2, $3)
		ON CONFLICT (chat_server_id, source_key) DO NOTHING`, chatServerID, sourceKey, linkID)
	if err != nil {
		return 0, err
	}
	if tag.RowsAffected() == 0 {
		return 0, ErrChatTaskInProgress
	}
	return 0, nil
}

// SetChatTaskSourceTask records the task made for a reserved source key.
func SetChatTaskSourceTask(chatServerID int, sourceKey string, taskID int) error {
	pool, err := OpenDatabase()
	if err != nil {
		return err
	}
	defer CloseDatabase(pool)
	tag, err := pool.Exec(context.Background(), `
		UPDATE chat_task_sources SET task_id = $3 WHERE chat_server_id = $1 AND source_key = $2 AND task_id IS NULL`,
		chatServerID, sourceKey, taskID)
	if err == nil && tag.RowsAffected() == 0 {
		return ErrChatTaskInProgress // the reservation was taken over
	}
	return err
}

// ReleaseChatTaskSource drops a reservation whose task wasn't made.
func ReleaseChatTaskSource(chatServerID int, sourceKey string) error {
	pool, err := OpenDatabase()
	if err != nil {
		return err
	}
	defer CloseDatabase(pool)
	_, err = pool.Exec(context.Background(), `
		DELETE FROM chat_task_sources WHERE chat_server_id = $1 AND source_key = $2 AND task_id IS NULL`,
		chatServerID, sourceKey)
	return err
}

// ChatTaskSourceKey is the source key a chat server created taskID with, or "".
func ChatTaskSourceKey(chatServerID, taskID int) (string, error) {
	pool, err := OpenDatabase()
	if err != nil {
		return "", err
	}
	defer CloseDatabase(pool)
	var key string
	err = pool.QueryRow(context.Background(), `
		SELECT source_key FROM chat_task_sources WHERE chat_server_id = $1 AND task_id = $2`, chatServerID, taskID).Scan(&key)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil
	}
	return key, err
}

// GetChatLink returns one of a chat server's links.
func GetChatLink(chatServerID int, linkID string) (*ChatLink, error) {
	pool, err := OpenDatabase()
	if err != nil {
		return nil, err
	}
	defer CloseDatabase(pool)
	var l ChatLink
	err = pool.QueryRow(context.Background(), `
		SELECT l.link_id, l.project_id, p.name, l.user_id, l.active, l.paused_reason
		FROM chat_project_links l JOIN projects p ON p.id = l.project_id
		WHERE l.chat_server_id = $1 AND l.link_id = $2`, chatServerID, linkID).
		Scan(&l.LinkID, &l.ProjectID, &l.ProjectName, &l.UserID, &l.Active, &l.PausedReason)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &l, nil
}

// ChatTaskSummary is a task as reported back to a chat server.
type ChatTaskSummary struct {
	TaskID      int    `json:"task_id"`
	Title       string `json:"title"`
	URL         string `json:"url"`
	ProjectID   int    `json:"project_id"`
	ProjectName string `json:"project_name"`
}

// GetChatTaskSummary loads a task's title and project (URL is left for the caller).
func GetChatTaskSummary(taskID int) (*ChatTaskSummary, error) {
	pool, err := OpenDatabase()
	if err != nil {
		return nil, err
	}
	defer CloseDatabase(pool)
	s := ChatTaskSummary{TaskID: taskID}
	err = pool.QueryRow(context.Background(), `
		SELECT t.title, COALESCE(t.project_id, 0), COALESCE(p.name, '')
		FROM tasks t LEFT JOIN projects p ON p.id = t.project_id WHERE t.id = $1`, taskID).
		Scan(&s.Title, &s.ProjectID, &s.ProjectName)
	if err != nil {
		return nil, err
	}
	return &s, nil
}

// ChatUserCanAct reports whether an Ordryn user may act through a chat server
// (exists, not banned, not an agent or the system user).
func ChatUserCanAct(userID int) (bool, error) {
	if userID <= 0 || IsSystemUser(userID) {
		return false, nil
	}
	pool, err := OpenDatabase()
	if err != nil {
		return false, err
	}
	defer CloseDatabase(pool)
	var banned, agent bool
	err = pool.QueryRow(context.Background(),
		`SELECT COALESCE(is_banned, FALSE), COALESCE(is_agent, FALSE) FROM users WHERE id = $1`, userID).Scan(&banned, &agent)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return !banned && !agent, nil
}
