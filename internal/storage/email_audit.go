package storage

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	"GoTodo/internal/mailer"
)

const (
	DefaultEmailAuditRetentionDays = 7
	MinEmailAuditRetentionDays     = 1
	MaxEmailAuditRetentionDays     = 90
	emailAuditDefaultLimit         = 50
	emailAuditMaxLimit             = 100
)

// EmailAudit is one outbound email attempt stored for admin review.
type EmailAudit struct {
	ID        int
	CreatedAt time.Time
	Trigger   string
	ToEmail   string
	Status    string
	Error     string
	Provider  string
}

// EmailAuditFilter selects rows for the admin list API.
type EmailAuditFilter struct {
	Status  string
	Trigger string
	Query   string
	From    *time.Time
	To      *time.Time
	Limit   int
	Offset  int
}

// CreateEmailAuditTable creates the email_audit table and indexes.
func CreateEmailAuditTable() error {
	pool, err := OpenDatabase()
	if err != nil {
		return err
	}
	defer CloseDatabase(pool)

	stmts := []string{
		`CREATE TABLE IF NOT EXISTS email_audit (
			id SERIAL PRIMARY KEY,
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			trigger TEXT NOT NULL,
			to_email TEXT NOT NULL DEFAULT '',
			status TEXT NOT NULL,
			error TEXT NOT NULL DEFAULT '',
			provider TEXT NOT NULL DEFAULT '',
			CONSTRAINT email_audit_status_check CHECK (status IN ('sent', 'failed', 'not_configured', 'rate_limited'))
		)`,
		`CREATE INDEX IF NOT EXISTS idx_email_audit_created_at ON email_audit (created_at DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_email_audit_status_created ON email_audit (status, created_at DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_email_audit_trigger_created ON email_audit (trigger, created_at DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_email_audit_to_email_created ON email_audit (lower(to_email), created_at DESC)`,
	}
	for _, q := range stmts {
		if _, err := pool.Exec(context.Background(), q); err != nil {
			return fmt.Errorf("failed to create email_audit: %v", err)
		}
	}
	return nil
}

// ClampEmailAuditRetentionDays returns a safe retention window. Values below the
// minimum (including 0 from uninitialized structs) become the default.
func ClampEmailAuditRetentionDays(days int) int {
	if days < MinEmailAuditRetentionDays {
		return DefaultEmailAuditRetentionDays
	}
	if days > MaxEmailAuditRetentionDays {
		return MaxEmailAuditRetentionDays
	}
	return days
}

// SiteEmailConfig returns outbound mail config, or a zero value if settings are missing.
func SiteEmailConfig(s *SiteSettings) mailer.Config {
	if s == nil {
		return mailer.Config{}
	}
	return s.Email
}

// RecordEmailAudit is the mailer.Auditor that persists a send attempt.
func RecordEmailAudit(entry mailer.AuditEntry) {
	if err := InsertEmailAudit(entry); err != nil {
		fmt.Printf("email audit: failed to record %s: %v\n", entry.Trigger, err)
	}
}

// InsertEmailAudit stores one send attempt.
func InsertEmailAudit(entry mailer.AuditEntry) error {
	pool, err := OpenDatabase()
	if err != nil {
		return err
	}
	defer CloseDatabase(pool)

	trigger := strings.TrimSpace(entry.Trigger)
	if trigger == "" {
		trigger = "unknown"
	}
	status := strings.TrimSpace(entry.Status)
	if !mailer.KnownStatus(status) {
		status = mailer.StatusFailed
	}

	_, err = pool.Exec(context.Background(), `
		INSERT INTO email_audit (trigger, to_email, status, error, provider)
		VALUES ($1, $2, $3, $4, $5)`,
		trigger, strings.TrimSpace(entry.ToEmail), status, entry.Error, strings.TrimSpace(entry.Provider))
	if err != nil {
		return fmt.Errorf("insert email_audit: %w", err)
	}
	return nil
}

// ListEmailAudit returns newest matching rows and the unpaginated total.
func ListEmailAudit(f EmailAuditFilter) ([]EmailAudit, int, error) {
	if f.Limit <= 0 {
		f.Limit = emailAuditDefaultLimit
	}
	if f.Limit > emailAuditMaxLimit {
		f.Limit = emailAuditMaxLimit
	}
	if f.Offset < 0 {
		f.Offset = 0
	}

	where := []string{"TRUE"}
	args := make([]any, 0, 6)
	n := 1
	if s := strings.TrimSpace(f.Status); s != "" {
		where = append(where, fmt.Sprintf("status = $%d", n))
		args = append(args, s)
		n++
	}
	if t := strings.TrimSpace(f.Trigger); t != "" {
		where = append(where, fmt.Sprintf("trigger = $%d", n))
		args = append(args, t)
		n++
	}
	if q := strings.TrimSpace(f.Query); q != "" {
		where = append(where, fmt.Sprintf("to_email ILIKE $%d", n))
		args = append(args, "%"+q+"%")
		n++
	}
	if f.From != nil {
		where = append(where, fmt.Sprintf("created_at >= $%d", n))
		args = append(args, f.From.UTC())
		n++
	}
	if f.To != nil {
		where = append(where, fmt.Sprintf("created_at <= $%d", n))
		args = append(args, f.To.UTC())
		n++
	}
	whereSQL := strings.Join(where, " AND ")

	pool, err := OpenDatabase()
	if err != nil {
		return nil, 0, err
	}
	defer CloseDatabase(pool)

	var total int
	countArgs := append([]any{}, args...)
	if err := pool.QueryRow(context.Background(),
		"SELECT COUNT(*) FROM email_audit WHERE "+whereSQL, countArgs...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count email_audit: %w", err)
	}

	args = append(args, f.Limit, f.Offset)
	rows, err := pool.Query(context.Background(), `
		SELECT id, created_at, trigger, to_email, status, error, provider
		FROM email_audit
		WHERE `+whereSQL+`
		ORDER BY created_at DESC, id DESC
		LIMIT $`+fmt.Sprintf("%d", n)+` OFFSET $`+fmt.Sprintf("%d", n+1), args...)
	if err != nil {
		return nil, 0, fmt.Errorf("list email_audit: %w", err)
	}
	defer rows.Close()

	out := make([]EmailAudit, 0)
	for rows.Next() {
		var row EmailAudit
		if err := rows.Scan(&row.ID, &row.CreatedAt, &row.Trigger, &row.ToEmail, &row.Status, &row.Error, &row.Provider); err != nil {
			return nil, 0, err
		}
		out = append(out, row)
	}
	return out, total, rows.Err()
}

// PurgeEmailAudit deletes rows older than the given retention window.
func PurgeEmailAudit(retentionDays int) (int64, error) {
	retentionDays = ClampEmailAuditRetentionDays(retentionDays)
	pool, err := OpenDatabase()
	if err != nil {
		return 0, err
	}
	defer CloseDatabase(pool)

	tag, err := pool.Exec(context.Background(),
		`DELETE FROM email_audit WHERE created_at < NOW() - ($1 * INTERVAL '1 day')`, retentionDays)
	if err != nil {
		return 0, fmt.Errorf("purge email_audit: %w", err)
	}
	return tag.RowsAffected(), nil
}

// StartEmailAuditPurgeWorker deletes expired audit rows on an hourly ticker.
func StartEmailAuditPurgeWorker() {
	go func() {
		runEmailAuditPurge()
		ticker := time.NewTicker(time.Hour)
		defer ticker.Stop()
		for range ticker.C {
			runEmailAuditPurge()
		}
	}()
}

func runEmailAuditPurge() {
	days := DefaultEmailAuditRetentionDays
	if s, err := GetSiteSettings(); err == nil && s != nil {
		days = ClampEmailAuditRetentionDays(s.EmailAuditRetentionDays)
	}
	n, err := PurgeEmailAudit(days)
	if err != nil {
		log.Printf("email audit purge: %v", err)
		return
	}
	if n > 0 {
		log.Printf("email audit purge: deleted %d row(s) older than %d day(s)", n, days)
	}
}

// MigrateEmailAuditAddRateLimitedStatus allows audit rows when core mail is rate-limited.
func MigrateEmailAuditAddRateLimitedStatus() error {
	pool, err := OpenDatabase()
	if err != nil {
		return err
	}
	defer CloseDatabase(pool)

	ctx := context.Background()
	if _, err := pool.Exec(ctx, `ALTER TABLE email_audit DROP CONSTRAINT IF EXISTS email_audit_status_check`); err != nil {
		return fmt.Errorf("drop email_audit_status_check: %w", err)
	}
	_, err = pool.Exec(ctx, `
		ALTER TABLE email_audit ADD CONSTRAINT email_audit_status_check
		CHECK (status IN ('sent', 'failed', 'not_configured', 'rate_limited'))`)
	if err != nil {
		return fmt.Errorf("add email_audit_status_check: %w", err)
	}
	return nil
}
