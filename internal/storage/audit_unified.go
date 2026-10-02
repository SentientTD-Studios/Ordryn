package storage

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"
)

// Audit sources combined by the unified admin audit view.
const (
	AuditSourceTask    = "task"
	AuditSourceProject = "project"
	AuditSourceComment = "comment"
	AuditSourceEmail   = "email"
	AuditSourceAdmin   = "admin"
)

const (
	auditDefaultLimit = 50
	auditMaxLimit     = 100
	// AuditExportMaxRows caps a single CSV export.
	AuditExportMaxRows = 50000

	// AuditRetentionDays 0 keeps history forever (the default); otherwise rows
	// older than the window are purged from every audit source except email,
	// which has its own retention setting.
	MaxAuditRetentionDays = 3650
	// auditCommentSnippetChars caps comment bodies copied into audit metadata.
	auditCommentSnippetChars = 500
)

// AuditEvent is one row of the unified audit log.
type AuditEvent struct {
	Source        string
	ID            int64
	CreatedAt     time.Time
	EventType     string
	ActorUserID   int
	ActorUserName string
	ActorEmail    string
	TargetType    string
	TargetID      int64
	TargetLabel   string
	ProjectID     int
	ProjectName   string
	Metadata      map[string]interface{}
}

// AuditFilter selects rows from the unified audit log.
type AuditFilter struct {
	UserID    int
	ProjectID int
	Source    string
	EventType string
	Since     *time.Time
	Until     *time.Time
	Limit     int
	Offset    int
}

// AuditEventType is a distinct (source, event_type) pair for filter pickers.
type AuditEventType struct {
	Source    string
	EventType string
}

// KnownAuditSource reports whether s is a valid audit source.
func KnownAuditSource(s string) bool {
	switch s {
	case AuditSourceTask, AuditSourceProject, AuditSourceComment, AuditSourceEmail, AuditSourceAdmin:
		return true
	}
	return false
}

// ClampAuditRetentionDays keeps the retention window within 0 (forever) and the max.
func ClampAuditRetentionDays(days int) int {
	if days < 0 {
		return 0
	}
	if days > MaxAuditRetentionDays {
		return MaxAuditRetentionDays
	}
	return days
}

// CreateAdminEventsTable creates the admin_events table for site-admin actions
// (bans, settings changes, role templates, invites) that have no task/project home.
func CreateAdminEventsTable() error {
	pool, err := OpenDatabase()
	if err != nil {
		return err
	}
	defer CloseDatabase(pool)

	stmts := []string{
		`CREATE TABLE IF NOT EXISTS admin_events (
			id SERIAL PRIMARY KEY,
			actor_user_id INTEGER NOT NULL DEFAULT 0,
			event_type VARCHAR(48) NOT NULL,
			target_type VARCHAR(32) NOT NULL DEFAULT '',
			target_id BIGINT NOT NULL DEFAULT 0,
			target_label TEXT NOT NULL DEFAULT '',
			metadata JSONB NOT NULL DEFAULT '{}',
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)`,
		`CREATE INDEX IF NOT EXISTS idx_admin_events_created ON admin_events (created_at DESC, id DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_task_events_created ON task_events (created_at DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_project_events_created ON project_events (created_at DESC)`,
	}
	for _, q := range stmts {
		if _, err := pool.Exec(context.Background(), q); err != nil {
			return fmt.Errorf("failed to create admin_events: %v", err)
		}
	}
	return nil
}

// MigrateSiteSettingsAddAuditRetention adds audit_retention_days if missing.
func MigrateSiteSettingsAddAuditRetention() error {
	pool, err := OpenDatabase()
	if err != nil {
		return err
	}
	defer CloseDatabase(pool)

	if _, err := pool.Exec(context.Background(),
		"ALTER TABLE site_settings ADD COLUMN IF NOT EXISTS audit_retention_days INTEGER DEFAULT 0"); err != nil {
		return fmt.Errorf("failed to add audit_retention_days column to site_settings: %v", err)
	}
	return nil
}

// LogAdminEvent appends a site-admin action to the audit log.
func LogAdminEvent(actorUserID int, eventType, targetType string, targetID int64, targetLabel string, metadata map[string]interface{}) error {
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
		`INSERT INTO admin_events (actor_user_id, event_type, target_type, target_id, target_label, metadata)
		 VALUES ($1, $2, $3, $4, $5, $6)`,
		actorUserID, eventType, targetType, targetID, targetLabel, metaJSON)
	return err
}

// UserNameAndEmail returns a user's username and email, or empty strings if unknown.
func UserNameAndEmail(userID int) (name, email string) {
	pool, err := OpenDatabase()
	if err != nil {
		return "", ""
	}
	defer CloseDatabase(pool)

	if err := pool.QueryRow(context.Background(),
		`SELECT COALESCE(user_name, ''), COALESCE(email, '') FROM users WHERE id = $1`, userID).Scan(&name, &email); err != nil {
		return "", ""
	}
	return name, email
}

// UserAuditLabel returns "username (email)" for audit rows, or "" if unknown.
func UserAuditLabel(userID int) string {
	name, email := UserNameAndEmail(userID)
	switch {
	case name != "" && email != "":
		return name + " (" + email + ")"
	case name != "":
		return name
	default:
		return email
	}
}

// auditUnionSQL normalizes every audit source to one row shape:
// source, id, created_at, event_type, actor_user_id, target_type, target_id,
// target_label, project_id, metadata. Filters are applied by the outer query;
// Postgres pushes them down into each UNION ALL branch.
var auditUnionSQL = fmt.Sprintf(`
	SELECT 'task'::text AS source, te.id::bigint AS id, te.created_at::timestamptz AS created_at,
		te.event_type::text AS event_type, te.user_id AS actor_user_id,
		'task'::text AS target_type, te.task_id::bigint AS target_id,
		COALESCE(t.title, '') AS target_label, t.project_id AS project_id,
		COALESCE(te.metadata, '{}'::jsonb) AS metadata
	FROM task_events te
	LEFT JOIN tasks t ON t.id = te.task_id
	UNION ALL
	SELECT 'project', pe.id::bigint, pe.created_at, pe.event_type::text, pe.actor_user_id,
		'project', pe.project_id::bigint, COALESCE(p.name, ''), pe.project_id,
		COALESCE(pe.metadata, '{}'::jsonb)
	FROM project_events pe
	LEFT JOIN projects p ON p.id = pe.project_id
	UNION ALL
	SELECT 'comment', r.id::bigint, r.created_at, 'comment_' || r.kind, r.edited_by_user_id,
		'comment', r.comment_id::bigint, COALESCE(t.title, ''), t.project_id,
		jsonb_build_object(
			'task_id', r.task_id,
			'from', LEFT(r.body, %[1]d),
			'to', LEFT(COALESCE(nxt.body, c.body, ''), %[1]d))
	FROM task_comment_revisions r
	LEFT JOIN task_comments c ON c.id = r.comment_id
	LEFT JOIN tasks t ON t.id = r.task_id
	LEFT JOIN LATERAL (
		SELECT n.body FROM task_comment_revisions n
		WHERE n.comment_id = r.comment_id AND (n.created_at, n.id) > (r.created_at, r.id)
		ORDER BY n.created_at, n.id
		LIMIT 1
	) nxt ON TRUE
	UNION ALL
	SELECT 'email', ea.id::bigint, ea.created_at, ea.trigger, NULL::integer,
		'email', 0::bigint, ea.to_email, NULL::integer,
		jsonb_build_object('to_email', ea.to_email, 'status', ea.status, 'error', ea.error, 'provider', ea.provider)
	FROM email_audit ea
	UNION ALL
	SELECT 'admin', ae.id::bigint, ae.created_at, ae.event_type::text, ae.actor_user_id,
		ae.target_type::text, ae.target_id, ae.target_label, NULL::integer, ae.metadata
	FROM admin_events ae`, auditCommentSnippetChars)

// buildAuditWhere returns the outer WHERE clause (over alias "a") and its args.
func buildAuditWhere(f AuditFilter) (string, []any) {
	where := []string{"TRUE"}
	args := make([]any, 0, 6)
	add := func(cond string, v any) {
		args = append(args, v)
		where = append(where, fmt.Sprintf(cond, len(args)))
	}
	if f.UserID > 0 {
		add("a.actor_user_id = $%d", f.UserID)
	}
	if f.ProjectID > 0 {
		add("a.project_id = $%d", f.ProjectID)
	}
	if s := strings.TrimSpace(f.Source); s != "" {
		add("a.source = $%d", s)
	}
	if et := strings.TrimSpace(f.EventType); et != "" {
		add("a.event_type = $%d", et)
	}
	if f.Since != nil {
		add("a.created_at >= $%d", f.Since.UTC())
	}
	if f.Until != nil {
		add("a.created_at <= $%d", f.Until.UTC())
	}
	return strings.Join(where, " AND "), args
}

// buildUnifiedAuditQuery returns the paginated list query and its args.
func buildUnifiedAuditQuery(f AuditFilter) (string, []any) {
	whereSQL, args := buildAuditWhere(f)
	n := len(args)
	args = append(args, f.Limit, f.Offset)
	return `
		SELECT a.source, a.id, a.created_at, a.event_type,
			COALESCE(a.actor_user_id, 0), COALESCE(u.user_name, ''), COALESCE(u.email, ''),
			a.target_type, a.target_id, a.target_label,
			COALESCE(a.project_id, 0), COALESCE(p.name, ''), a.metadata
		FROM (` + auditUnionSQL + `) a
		LEFT JOIN users u ON u.id = a.actor_user_id
		LEFT JOIN projects p ON p.id = a.project_id
		WHERE ` + whereSQL + `
		ORDER BY a.created_at DESC, a.source, a.id DESC
		LIMIT $` + fmt.Sprint(n+1) + ` OFFSET $` + fmt.Sprint(n+2), args
}

func normalizeAuditPaging(f *AuditFilter, maxLimit int) {
	if f.Limit <= 0 {
		f.Limit = auditDefaultLimit
	}
	if f.Limit > maxLimit {
		f.Limit = maxLimit
	}
	if f.Offset < 0 {
		f.Offset = 0
	}
}

// ListAuditEvents returns the newest matching audit rows and the unpaginated total.
func ListAuditEvents(ctx context.Context, f AuditFilter) ([]AuditEvent, int, error) {
	normalizeAuditPaging(&f, auditMaxLimit)

	pool, err := OpenDatabase()
	if err != nil {
		return nil, 0, err
	}
	defer CloseDatabase(pool)

	whereSQL, countArgs := buildAuditWhere(f)
	var total int
	if err := pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM (`+auditUnionSQL+`) a WHERE `+whereSQL, countArgs...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count audit events: %w", err)
	}

	out := make([]AuditEvent, 0)
	err = eachAuditEvent(ctx, f, func(ev AuditEvent) error {
		out = append(out, ev)
		return nil
	})
	if err != nil {
		return nil, 0, err
	}
	return out, total, nil
}

// EachAuditEvent streams matching rows (newest first, up to AuditExportMaxRows) to fn.
func EachAuditEvent(ctx context.Context, f AuditFilter, fn func(AuditEvent) error) error {
	f.Limit = AuditExportMaxRows
	f.Offset = 0
	return eachAuditEvent(ctx, f, fn)
}

func eachAuditEvent(ctx context.Context, f AuditFilter, fn func(AuditEvent) error) error {
	pool, err := OpenDatabase()
	if err != nil {
		return err
	}
	defer CloseDatabase(pool)

	query, args := buildUnifiedAuditQuery(f)
	rows, err := pool.Query(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("list audit events: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var ev AuditEvent
		var metaRaw []byte
		if err := rows.Scan(
			&ev.Source, &ev.ID, &ev.CreatedAt, &ev.EventType,
			&ev.ActorUserID, &ev.ActorUserName, &ev.ActorEmail,
			&ev.TargetType, &ev.TargetID, &ev.TargetLabel,
			&ev.ProjectID, &ev.ProjectName, &metaRaw,
		); err != nil {
			return err
		}
		ev.Metadata = map[string]interface{}{}
		if len(metaRaw) > 0 {
			_ = json.Unmarshal(metaRaw, &ev.Metadata)
		}
		if err := fn(ev); err != nil {
			return err
		}
	}
	return rows.Err()
}

// ListAuditEventTypes returns every distinct (source, event_type) pair on record.
func ListAuditEventTypes(ctx context.Context) ([]AuditEventType, error) {
	pool, err := OpenDatabase()
	if err != nil {
		return nil, err
	}
	defer CloseDatabase(pool)

	rows, err := pool.Query(ctx, `
		SELECT DISTINCT source, event_type FROM (
			SELECT 'task' AS source, event_type::text AS event_type FROM task_events
			UNION SELECT 'project', event_type::text FROM project_events
			UNION SELECT 'comment', 'comment_' || kind FROM task_comment_revisions
			UNION SELECT 'email', trigger FROM email_audit
			UNION SELECT 'admin', event_type::text FROM admin_events
		) x
		ORDER BY source, event_type`)
	if err != nil {
		return nil, fmt.Errorf("list audit event types: %w", err)
	}
	defer rows.Close()

	out := make([]AuditEventType, 0)
	for rows.Next() {
		var et AuditEventType
		if err := rows.Scan(&et.Source, &et.EventType); err != nil {
			return nil, err
		}
		out = append(out, et)
	}
	return out, rows.Err()
}

// AuditProject is a project choice for the audit filter (all projects, admin view).
type AuditProject struct {
	ID       int
	Name     string
	Archived bool
}

// ListAuditProjects returns every project, archived included, sorted by name.
func ListAuditProjects(ctx context.Context) ([]AuditProject, error) {
	pool, err := OpenDatabase()
	if err != nil {
		return nil, err
	}
	defer CloseDatabase(pool)

	rows, err := pool.Query(ctx,
		`SELECT id, COALESCE(name, ''), COALESCE(archived, FALSE) FROM projects ORDER BY lower(name), id`)
	if err != nil {
		return nil, fmt.Errorf("list audit projects: %w", err)
	}
	defer rows.Close()

	out := make([]AuditProject, 0)
	for rows.Next() {
		var p AuditProject
		if err := rows.Scan(&p.ID, &p.Name, &p.Archived); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// PurgeAuditEvents deletes task, project, comment-revision and admin audit rows
// older than retentionDays. Zero means keep forever and deletes nothing.
func PurgeAuditEvents(retentionDays int) (int64, error) {
	retentionDays = ClampAuditRetentionDays(retentionDays)
	if retentionDays == 0 {
		return 0, nil
	}
	pool, err := OpenDatabase()
	if err != nil {
		return 0, err
	}
	defer CloseDatabase(pool)

	var total int64
	for _, q := range []string{
		`DELETE FROM task_events WHERE created_at::timestamptz < NOW() - ($1 * INTERVAL '1 day')`,
		`DELETE FROM project_events WHERE created_at < NOW() - ($1 * INTERVAL '1 day')`,
		`DELETE FROM task_comment_revisions WHERE created_at < NOW() - ($1 * INTERVAL '1 day')`,
		`DELETE FROM admin_events WHERE created_at < NOW() - ($1 * INTERVAL '1 day')`,
	} {
		tag, err := pool.Exec(context.Background(), q, retentionDays)
		if err != nil {
			return total, fmt.Errorf("purge audit events: %w", err)
		}
		total += tag.RowsAffected()
	}
	return total, nil
}

// StartAuditPurgeWorker deletes expired audit rows on an hourly ticker.
func StartAuditPurgeWorker() {
	go func() {
		runAuditPurge()
		ticker := time.NewTicker(time.Hour)
		defer ticker.Stop()
		for range ticker.C {
			runAuditPurge()
		}
	}()
}

func runAuditPurge() {
	s, err := GetSiteSettings()
	if err != nil || s == nil {
		return
	}
	days := ClampAuditRetentionDays(s.AuditRetentionDays)
	n, err := PurgeAuditEvents(days)
	if err != nil {
		log.Printf("audit purge: %v", err)
		return
	}
	if n > 0 {
		log.Printf("audit purge: deleted %d row(s) older than %d day(s)", n, days)
	}
}
