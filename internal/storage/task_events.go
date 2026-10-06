package storage

import (
	"context"
	"encoding/json"
	"fmt"
	"time"
)

// TaskEvent represents a single audit log entry for a task.
type TaskEvent struct {
	ID            int
	TaskID        int
	UserID        int
	EventType     string
	Metadata      map[string]interface{}
	CreatedAt     time.Time
	ActorUserName string
	ActorEmail    string
}

// CreateTaskEventsTable creates the task_events audit table.
func CreateTaskEventsTable() error {
	pool, err := OpenDatabase()
	if err != nil {
		return fmt.Errorf("failed to open database: %v", err)
	}
	defer CloseDatabase(pool)

	_, err = pool.Exec(context.Background(), `
		CREATE TABLE IF NOT EXISTS task_events (
			id SERIAL PRIMARY KEY,
			task_id INTEGER NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
			user_id INTEGER NOT NULL,
			event_type VARCHAR(32) NOT NULL,
			metadata JSONB DEFAULT '{}',
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
		);
		CREATE INDEX IF NOT EXISTS idx_task_events_task_id ON task_events(task_id, created_at DESC);
	`)
	if err != nil {
		return fmt.Errorf("failed to create task_events table: %v", err)
	}
	return nil
}

// LogTaskEvent appends an audit event for a task.
func LogTaskEvent(taskID, userID int, eventType string, metadata map[string]interface{}) error {
	pool, err := OpenDatabase()
	if err != nil {
		return err
	}
	defer CloseDatabase(pool)

	metaJSON := []byte("{}")
	if metadata != nil {
		if b, err := json.Marshal(metadata); err == nil {
			metaJSON = b
		}
	}

	_, err = pool.Exec(context.Background(),
		"INSERT INTO task_events (task_id, user_id, event_type, metadata) VALUES ($1, $2, $3, $4)",
		taskID, userID, eventType, metaJSON,
	)
	return err
}

// GetEventsForTask returns recent events for a task, newest first.
func GetEventsForTask(taskID, userID int, limit int) ([]TaskEvent, error) {
	if limit <= 0 {
		limit = 50
	}

	pool, err := OpenDatabase()
	if err != nil {
		return nil, err
	}
	defer CloseDatabase(pool)

	rows, err := pool.Query(context.Background(), `
		SELECT te.id, te.task_id, te.user_id, te.event_type, COALESCE(te.metadata, '{}'), te.created_at,
		       COALESCE(u.user_name, ''), COALESCE(u.email, '')
		FROM task_events te
		JOIN tasks t ON t.id = te.task_id
		LEFT JOIN users u ON u.id = te.user_id
		WHERE te.task_id = $1 AND `+TaskVisibleCondition("t", "$2")+`
		ORDER BY te.created_at DESC
		LIMIT $3`, taskID, userID, limit)
	if err != nil {
		return nil, fmt.Errorf("failed to query task events: %v", err)
	}
	defer rows.Close()

	return scanTaskEvents(rows)
}

func scanTaskEvents(rows interface {
	Next() bool
	Scan(dest ...any) error
	Err() error
}) ([]TaskEvent, error) {
	var out []TaskEvent
	for rows.Next() {
		var ev TaskEvent
		var metaRaw []byte
		if err := rows.Scan(&ev.ID, &ev.TaskID, &ev.UserID, &ev.EventType, &metaRaw, &ev.CreatedAt, &ev.ActorUserName, &ev.ActorEmail); err != nil {
			return nil, err
		}
		ev.Metadata = map[string]interface{}{}
		if len(metaRaw) > 0 {
			_ = json.Unmarshal(metaRaw, &ev.Metadata)
		}
		if IsSystemUser(ev.UserID) {
			ev.ActorUserName = SystemUserDisplayName
			ev.ActorEmail = ""
		}
		out = append(out, ev)
	}
	return out, rows.Err()
}
