package storage

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// AuditStreamSettings forwards audit events to an HTTPS endpoint (a SIEM such
// as Splunk HEC, Datadog, or Elastic) in signed JSON batches.
type AuditStreamSettings struct {
	Enabled bool
	URL     string
	// SecretEnc signs each batch (X-Ordryn-Signature); HeaderValueEnc, when set,
	// is sent as the Authorization header (e.g. "Splunk <token>").
	SecretEnc      string
	HeaderValueEnc string
	// Which logs to send. Admin events are always included.
	IncludeProject bool
	IncludeTask    bool
	// Cursors: the last id sent from each table.
	CursorAdmin, CursorProject, CursorTask int64
	LastSuccessAt                          *time.Time
	LastError                              string
	LastErrorAt                            *time.Time
}

// MigrateAuditStream creates the single-row settings table.
func MigrateAuditStream() error {
	pool, err := OpenDatabase()
	if err != nil {
		return err
	}
	defer CloseDatabase(pool)
	_, err = pool.Exec(context.Background(), `
		CREATE TABLE IF NOT EXISTS audit_stream (
			id SMALLINT PRIMARY KEY DEFAULT 1 CHECK (id = 1),
			enabled BOOLEAN NOT NULL DEFAULT FALSE,
			url TEXT NOT NULL DEFAULT '',
			secret_enc TEXT NOT NULL DEFAULT '',
			header_value_enc TEXT NOT NULL DEFAULT '',
			include_project BOOLEAN NOT NULL DEFAULT FALSE,
			include_task BOOLEAN NOT NULL DEFAULT FALSE,
			cursor_admin BIGINT NOT NULL DEFAULT 0,
			cursor_project BIGINT NOT NULL DEFAULT 0,
			cursor_task BIGINT NOT NULL DEFAULT 0,
			last_success_at TIMESTAMPTZ,
			last_error TEXT NOT NULL DEFAULT '',
			last_error_at TIMESTAMPTZ
		)`)
	if err != nil {
		return fmt.Errorf("audit_stream: %w", err)
	}
	return nil
}

// GetAuditStreamSettings returns the settings; never configured is the zero value.
func GetAuditStreamSettings() (*AuditStreamSettings, error) {
	pool, err := OpenDatabase()
	if err != nil {
		return nil, err
	}
	defer CloseDatabase(pool)
	s := &AuditStreamSettings{}
	err = pool.QueryRow(context.Background(), `
		SELECT enabled, url, secret_enc, header_value_enc, include_project, include_task,
		       cursor_admin, cursor_project, cursor_task, last_success_at, last_error, last_error_at
		FROM audit_stream WHERE id = 1`).Scan(&s.Enabled, &s.URL, &s.SecretEnc, &s.HeaderValueEnc,
		&s.IncludeProject, &s.IncludeTask, &s.CursorAdmin, &s.CursorProject, &s.CursorTask,
		&s.LastSuccessAt, &s.LastError, &s.LastErrorAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return s, nil
	}
	return s, err
}

// SaveAuditStreamConfig stores the configuration. Turning streaming on for the
// first time starts the cursors at the newest events, so history isn't replayed.
func SaveAuditStreamConfig(s AuditStreamSettings) error {
	pool, err := OpenDatabase()
	if err != nil {
		return err
	}
	defer CloseDatabase(pool)
	_, err = pool.Exec(context.Background(), `
		INSERT INTO audit_stream (id, enabled, url, secret_enc, header_value_enc, include_project, include_task,
		                          cursor_admin, cursor_project, cursor_task)
		VALUES (1, $1, $2, $3, $4, $5, $6,
		        (SELECT COALESCE(MAX(id), 0) FROM admin_events),
		        (SELECT COALESCE(MAX(id), 0) FROM project_events),
		        (SELECT COALESCE(MAX(id), 0) FROM task_events))
		ON CONFLICT (id) DO UPDATE SET
			enabled = EXCLUDED.enabled, url = EXCLUDED.url, secret_enc = EXCLUDED.secret_enc,
			header_value_enc = EXCLUDED.header_value_enc, include_project = EXCLUDED.include_project,
			include_task = EXCLUDED.include_task,
			cursor_project = CASE WHEN EXCLUDED.include_project AND NOT audit_stream.include_project
			                      THEN EXCLUDED.cursor_project ELSE audit_stream.cursor_project END,
			cursor_task = CASE WHEN EXCLUDED.include_task AND NOT audit_stream.include_task
			                   THEN EXCLUDED.cursor_task ELSE audit_stream.cursor_task END`,
		s.Enabled, s.URL, s.SecretEnc, s.HeaderValueEnc, s.IncludeProject, s.IncludeTask)
	return err
}

// AuditStreamEvent is one event in a batch.
type AuditStreamEvent struct {
	Source     string          `json:"source"` // admin, project, task
	ID         int64           `json:"id"`
	OccurredAt time.Time       `json:"occurred_at"`
	ActorID    int             `json:"actor_user_id"`
	ActorName  string          `json:"actor"`
	EventType  string          `json:"event_type"`
	TargetType string          `json:"target_type"`
	TargetID   int64           `json:"target_id"`
	Target     string          `json:"target,omitempty"`
	Metadata   json.RawMessage `json:"metadata"`
}

// NextAuditStreamBatch returns up to limit events after the cursors, from each
// enabled log, oldest first per log.
func NextAuditStreamBatch(s *AuditStreamSettings, limit int) ([]AuditStreamEvent, error) {
	pool, err := OpenDatabase()
	if err != nil {
		return nil, err
	}
	defer CloseDatabase(pool)

	actor := `COALESCE((SELECT COALESCE(NULLIF(u.user_name, ''), u.email) FROM users u WHERE u.id = %s), '')`
	queries := []struct {
		on  bool
		sql string
		cur int64
	}{
		{true, `SELECT 'admin', e.id, e.created_at, e.actor_user_id, ` + fmt.Sprintf(actor, "e.actor_user_id") + `,
			e.event_type, e.target_type, e.target_id, e.target_label, e.metadata
			FROM admin_events e WHERE e.id > $1 ORDER BY e.id LIMIT $2`, s.CursorAdmin},
		{s.IncludeProject, `SELECT 'project', e.id, e.created_at, e.actor_user_id, ` + fmt.Sprintf(actor, "e.actor_user_id") + `,
			e.event_type, 'project', e.project_id, COALESCE((SELECT p.name FROM projects p WHERE p.id = e.project_id), ''),
			COALESCE(e.metadata, '{}')
			FROM project_events e WHERE e.id > $1 ORDER BY e.id LIMIT $2`, s.CursorProject},
		{s.IncludeTask, `SELECT 'task', e.id, e.created_at AT TIME ZONE 'UTC', e.user_id, ` + fmt.Sprintf(actor, "e.user_id") + `,
			e.event_type, 'task', e.task_id, COALESCE((SELECT t.title FROM tasks t WHERE t.id = e.task_id), ''),
			COALESCE(e.metadata, '{}')
			FROM task_events e WHERE e.id > $1 ORDER BY e.id LIMIT $2`, s.CursorTask},
	}
	out := []AuditStreamEvent{}
	for _, q := range queries {
		if !q.on {
			continue
		}
		rows, err := pool.Query(context.Background(), q.sql, q.cur, limit)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var e AuditStreamEvent
			var meta []byte
			if err := rows.Scan(&e.Source, &e.ID, &e.OccurredAt, &e.ActorID, &e.ActorName, &e.EventType,
				&e.TargetType, &e.TargetID, &e.Target, &meta); err != nil {
				rows.Close()
				return nil, err
			}
			if len(meta) == 0 {
				meta = []byte("{}")
			}
			e.Metadata = meta
			out = append(out, e)
		}
		rows.Close()
	}
	return out, nil
}

// AdvanceAuditStream records a delivered batch: each log's cursor moves to the
// highest id sent, and the success time is stamped.
func AdvanceAuditStream(events []AuditStreamEvent) error {
	var admin, project, task int64
	for _, e := range events {
		switch e.Source {
		case "admin":
			admin = max(admin, e.ID)
		case "project":
			project = max(project, e.ID)
		case "task":
			task = max(task, e.ID)
		}
	}
	pool, err := OpenDatabase()
	if err != nil {
		return err
	}
	defer CloseDatabase(pool)
	_, err = pool.Exec(context.Background(), `
		UPDATE audit_stream SET
			cursor_admin = GREATEST(cursor_admin, $1), cursor_project = GREATEST(cursor_project, $2),
			cursor_task = GREATEST(cursor_task, $3), last_success_at = NOW(), last_error = ''
		WHERE id = 1`, admin, project, task)
	return err
}

// RecordAuditStreamError stores the latest delivery failure.
func RecordAuditStreamError(msg string) {
	pool, err := OpenDatabase()
	if err != nil {
		return
	}
	defer CloseDatabase(pool)
	if len(msg) > 500 {
		msg = msg[:500]
	}
	_, _ = pool.Exec(context.Background(),
		`UPDATE audit_stream SET last_error = $1, last_error_at = NOW() WHERE id = 1`, msg)
}

// TryAuditStreamLock takes a session-independent advisory lock for one tick so
// only one server instance streams at a time. The returned func releases it.
func TryAuditStreamLock(ctx context.Context) (bool, func(), error) {
	pool, err := OpenDatabase()
	if err != nil {
		return false, func() {}, err
	}
	conn, err := pool.Acquire(ctx)
	if err != nil {
		CloseDatabase(pool)
		return false, func() {}, err
	}
	var ok bool
	if err := conn.QueryRow(ctx, `SELECT pg_try_advisory_lock(hashtext('ordryn_audit_stream'))`).Scan(&ok); err != nil || !ok {
		conn.Release()
		CloseDatabase(pool)
		return false, func() {}, err
	}
	return true, func() {
		_, _ = conn.Exec(context.Background(), `SELECT pg_advisory_unlock(hashtext('ordryn_audit_stream'))`)
		conn.Release()
		CloseDatabase(pool)
	}, nil
}
