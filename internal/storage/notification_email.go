package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

// Notification email delivery modes. A missing preferences row means "off":
// email is opt-in per user.
const (
	EmailModeOff     = "off"
	EmailModeInstant = "instant"
	EmailModeDigest  = "digest"
)

// Due-date reminder timings. A missing preferences row means
// ReminderTimingDefault.
const (
	ReminderTimingOff       = "off"
	ReminderTimingDayBefore = "day_before"
	ReminderTimingMorning   = "morning"
	ReminderTimingBoth      = "both"
	ReminderTimingDefault   = ReminderTimingBoth

	// DefaultDigestHour is the local hour a daily digest goes out.
	DefaultDigestHour = 8

	// Reminder kinds recorded in task_reminder_sent.
	ReminderKindDayBefore = "day_before"
	ReminderKindMorning   = "morning"

	// NotificationDueReminder is the in-app type for due-date reminders.
	NotificationDueReminder = "due_reminder"
)

// UserEmailPrefs is a user's notification email and reminder settings.
type UserEmailPrefs struct {
	Mode           string
	DigestHour     int
	ReminderTiming string
	// EmailSince is when email was last switched on; older notifications are
	// never emailed, so turning email on cannot send a backlog.
	EmailSince *time.Time
}

func defaultUserEmailPrefs() UserEmailPrefs {
	return UserEmailPrefs{Mode: EmailModeOff, DigestHour: DefaultDigestHour, ReminderTiming: ReminderTimingDefault}
}

// CreateNotificationEmailTables adds notification email and reminder storage.
func CreateNotificationEmailTables() error {
	pool, err := OpenDatabase()
	if err != nil {
		return err
	}
	defer CloseDatabase(pool)

	stmts := []string{
		`CREATE TABLE IF NOT EXISTS user_email_prefs (
			user_id INTEGER PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
			mode TEXT NOT NULL DEFAULT 'off',
			digest_hour SMALLINT NOT NULL DEFAULT 8,
			reminder_timing TEXT NOT NULL DEFAULT 'both',
			email_since TIMESTAMPTZ,
			last_instant_at TIMESTAMPTZ,
			last_digest_at TIMESTAMPTZ,
			updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)`,
		// Presence of a row means the user wants that notification type by email.
		`CREATE TABLE IF NOT EXISTS user_notification_email_types (
			user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			notification_type TEXT NOT NULL,
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			PRIMARY KEY (user_id, notification_type)
		)`,
		`ALTER TABLE user_notifications ADD COLUMN IF NOT EXISTS emailed_at TIMESTAMPTZ`,
		`CREATE INDEX IF NOT EXISTS idx_user_notifications_email_pending
			ON user_notifications (user_id, created_at) WHERE emailed_at IS NULL AND read_at IS NULL`,
		`CREATE TABLE IF NOT EXISTS task_reminder_sent (
			task_id INTEGER NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
			user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			due_date DATE NOT NULL,
			kind TEXT NOT NULL,
			sent_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			PRIMARY KEY (task_id, user_id, due_date, kind)
		)`,
		`ALTER TABLE site_settings ADD COLUMN IF NOT EXISTS notification_emails_enabled BOOLEAN DEFAULT FALSE`,
		`ALTER TABLE site_settings ADD COLUMN IF NOT EXISTS notification_emails_enabled_at TIMESTAMPTZ`,
	}
	for _, s := range stmts {
		if _, err := pool.Exec(context.Background(), s); err != nil {
			return fmt.Errorf("notification email tables: %v", err)
		}
	}
	return nil
}

// GetUserEmailPrefs returns a user's settings, or the defaults when unset.
func GetUserEmailPrefs(userID int) (UserEmailPrefs, error) {
	pool, err := OpenDatabase()
	if err != nil {
		return UserEmailPrefs{}, err
	}
	defer CloseDatabase(pool)

	p := defaultUserEmailPrefs()
	err = pool.QueryRow(context.Background(),
		`SELECT mode, digest_hour, reminder_timing, email_since FROM user_email_prefs WHERE user_id = $1`,
		userID).Scan(&p.Mode, &p.DigestHour, &p.ReminderTiming, &p.EmailSince)
	if errors.Is(err, pgx.ErrNoRows) || errors.Is(err, sql.ErrNoRows) {
		return defaultUserEmailPrefs(), nil
	}
	return p, err
}

// SaveUserEmailPrefs stores mode, digest hour, and reminder timing. Switching
// email on (from off) stamps email_since so only newer notifications are sent.
// Callers validate the values.
func SaveUserEmailPrefs(userID int, mode string, digestHour int, reminderTiming string) error {
	pool, err := OpenDatabase()
	if err != nil {
		return err
	}
	defer CloseDatabase(pool)

	_, err = pool.Exec(context.Background(), `
		INSERT INTO user_email_prefs (user_id, mode, digest_hour, reminder_timing, email_since)
		VALUES ($1, $2, $3, $4, CASE WHEN $2 <> 'off' THEN NOW() END)
		ON CONFLICT (user_id) DO UPDATE SET
			mode = EXCLUDED.mode,
			digest_hour = EXCLUDED.digest_hour,
			reminder_timing = EXCLUDED.reminder_timing,
			email_since = CASE
				WHEN EXCLUDED.mode <> 'off' AND user_email_prefs.mode = 'off' THEN NOW()
				ELSE user_email_prefs.email_since END,
			updated_at = NOW()`,
		userID, mode, digestHour, reminderTiming)
	return err
}

// ListUserEmailTypes returns the notification types a user wants by email.
func ListUserEmailTypes(userID int) (map[string]bool, error) {
	pool, err := OpenDatabase()
	if err != nil {
		return nil, err
	}
	defer CloseDatabase(pool)

	rows, err := pool.Query(context.Background(),
		`SELECT notification_type FROM user_notification_email_types WHERE user_id = $1`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]bool{}
	for rows.Next() {
		var t string
		if err := rows.Scan(&t); err != nil {
			return nil, err
		}
		out[t] = true
	}
	return out, rows.Err()
}

// SetUserEmailType turns email on or off for one notification type.
func SetUserEmailType(userID int, notificationType string, on bool) error {
	pool, err := OpenDatabase()
	if err != nil {
		return err
	}
	defer CloseDatabase(pool)

	if on {
		_, err = pool.Exec(context.Background(),
			`INSERT INTO user_notification_email_types (user_id, notification_type)
			 VALUES ($1, $2) ON CONFLICT DO NOTHING`, userID, notificationType)
	} else {
		_, err = pool.Exec(context.Background(),
			`DELETE FROM user_notification_email_types WHERE user_id = $1 AND notification_type = $2`,
			userID, notificationType)
	}
	return err
}

// PendingNotificationEmail is one unread, un-emailed notification that its
// recipient wants by email, with what the email worker needs about them.
type PendingNotificationEmail struct {
	NotificationID int
	Type           string
	Title          string
	Body           string
	TaskID         int
	ProjectName    string
	ActorName      string
	CreatedAt      time.Time

	UserID        int
	Email         string
	UserName      string
	Timezone      string
	DigestHour    int
	LastInstantAt *time.Time
	LastDigestAt  *time.Time
}

// ListPendingNotificationEmails returns notifications waiting to be emailed to
// users in the given mode. Rows are older than minAge (so a notification read
// in-app soon after it arrives is never emailed), newer than maxAge, newer than
// both the user's and the site's email switch-on time, and of a type the user
// opted into. Ordered by user, then age.
func ListPendingNotificationEmails(mode string, minAge, maxAge time.Duration, limit int) ([]PendingNotificationEmail, error) {
	pool, err := OpenDatabase()
	if err != nil {
		return nil, err
	}
	defer CloseDatabase(pool)

	rows, err := pool.Query(context.Background(), `
		SELECT n.id, n.type, n.title, n.body, COALESCE(n.task_id, 0),
			COALESCE(pr.name, ''), COALESCE(NULLIF(au.user_name, ''), au.email, ''), n.created_at,
			u.id, u.email, COALESCE(u.user_name, ''), COALESCE(NULLIF(u.timezone, ''), 'UTC'),
			p.digest_hour, p.last_instant_at, p.last_digest_at
		FROM user_notifications n
		JOIN users u ON u.id = n.user_id
		JOIN user_email_prefs p ON p.user_id = n.user_id AND p.mode = $1
		JOIN user_notification_email_types et ON et.user_id = n.user_id AND et.notification_type = n.type
		CROSS JOIN site_settings s
		LEFT JOIN projects pr ON pr.id = n.project_id
		LEFT JOIN users au ON au.id = n.actor_user_id
		WHERE s.id = 1 AND COALESCE(s.notification_emails_enabled, FALSE)
		  AND n.emailed_at IS NULL AND n.read_at IS NULL
		  AND n.created_at <= NOW() - $2::interval
		  AND n.created_at > NOW() - $3::interval
		  AND p.email_since IS NOT NULL AND n.created_at > p.email_since
		  AND (s.notification_emails_enabled_at IS NULL OR n.created_at > s.notification_emails_enabled_at)
		  AND COALESCE(u.email, '') <> ''
		  AND NOT COALESCE(u.is_banned, FALSE) AND NOT COALESCE(u.is_agent, FALSE) AND NOT COALESCE(u.is_system, FALSE)
		ORDER BY u.id, n.created_at, n.id
		LIMIT $4`,
		mode, durationInterval(minAge), durationInterval(maxAge), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []PendingNotificationEmail
	for rows.Next() {
		var r PendingNotificationEmail
		if err := rows.Scan(&r.NotificationID, &r.Type, &r.Title, &r.Body, &r.TaskID,
			&r.ProjectName, &r.ActorName, &r.CreatedAt,
			&r.UserID, &r.Email, &r.UserName, &r.Timezone,
			&r.DigestHour, &r.LastInstantAt, &r.LastDigestAt); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func durationInterval(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	return fmt.Sprintf("%d seconds", int64(d/time.Second))
}

// ClaimNotificationsForEmail marks notifications as emailed and returns the
// ids this caller won. A notification read or claimed in the meantime is
// skipped, so concurrent workers never email the same one twice.
func ClaimNotificationsForEmail(ids []int) ([]int, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	pool, err := OpenDatabase()
	if err != nil {
		return nil, err
	}
	defer CloseDatabase(pool)

	rows, err := pool.Query(context.Background(), `
		UPDATE user_notifications SET emailed_at = NOW()
		WHERE id = ANY($1) AND emailed_at IS NULL AND read_at IS NULL
		RETURNING id`, ids)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var won []int
	for rows.Next() {
		var id int
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		won = append(won, id)
	}
	return won, rows.Err()
}

// ReleaseNotificationEmailClaim undoes a claim after a failed send so the
// next pass retries.
func ReleaseNotificationEmailClaim(ids []int) error {
	if len(ids) == 0 {
		return nil
	}
	pool, err := OpenDatabase()
	if err != nil {
		return err
	}
	defer CloseDatabase(pool)
	_, err = pool.Exec(context.Background(),
		`UPDATE user_notifications SET emailed_at = NULL WHERE id = ANY($1)`, ids)
	return err
}

// MarkNotificationEmailSent records when a user last got an instant email or
// a digest, which paces the next one.
func MarkNotificationEmailSent(userID int, mode string, at time.Time) error {
	col := "last_instant_at"
	if mode == EmailModeDigest {
		col = "last_digest_at"
	}
	pool, err := OpenDatabase()
	if err != nil {
		return err
	}
	defer CloseDatabase(pool)
	_, err = pool.Exec(context.Background(),
		`UPDATE user_email_prefs SET `+col+` = $2 WHERE user_id = $1`, userID, at)
	return err
}

// NotificationEmailSiteState reports whether an admin has enabled notification
// email for the site.
func NotificationEmailsEnabled() (bool, error) {
	pool, err := OpenDatabase()
	if err != nil {
		return false, err
	}
	defer CloseDatabase(pool)
	var on bool
	err = pool.QueryRow(context.Background(),
		`SELECT COALESCE(notification_emails_enabled, FALSE) FROM site_settings WHERE id = 1`).Scan(&on)
	if errors.Is(err, pgx.ErrNoRows) || errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	return on, err
}

// ReminderTask is an open task with a due date near today, plus who could be
// reminded about it.
type ReminderTask struct {
	TaskID         int
	Title          string
	DueDate        time.Time
	OwnerID        int
	ProjectID      int
	ProjectName    string
	ProjectOwnerID int
	ClaimedBy      int
}

// ListReminderTasks returns open, non-archived tasks due between from and to
// (inclusive dates).
func ListReminderTasks(from, to time.Time, limit int) ([]ReminderTask, error) {
	pool, err := OpenDatabase()
	if err != nil {
		return nil, err
	}
	defer CloseDatabase(pool)

	rows, err := pool.Query(context.Background(), `
		SELECT t.id, t.title, t.due_date, COALESCE(t.user_id, 0), COALESCE(t.project_id, 0),
			COALESCE(p.name, ''), COALESCE(p.user_id, 0), COALESCE(t.claimed_by, 0)
		FROM tasks t
		LEFT JOIN projects p ON p.id = t.project_id
		WHERE t.due_date BETWEEN $1::date AND $2::date
		  AND COALESCE(t.completed, FALSE) = FALSE
		  AND NOT `+ArchivedTaskExistsSQL("t.id")+`
		ORDER BY t.due_date, t.id
		LIMIT $3`,
		from.Format("2006-01-02"), to.Format("2006-01-02"), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []ReminderTask
	for rows.Next() {
		var r ReminderTask
		if err := rows.Scan(&r.TaskID, &r.Title, &r.DueDate, &r.OwnerID, &r.ProjectID,
			&r.ProjectName, &r.ProjectOwnerID, &r.ClaimedBy); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// ReminderRecipient is who may get a reminder and how they want it.
type ReminderRecipient struct {
	Timezone string
	Timing   string
}

// ListReminderRecipients returns timezone and reminder timing for userIDs,
// skipping banned, agent, and system accounts.
func ListReminderRecipients(userIDs []int) (map[int]ReminderRecipient, error) {
	out := map[int]ReminderRecipient{}
	if len(userIDs) == 0 {
		return out, nil
	}
	pool, err := OpenDatabase()
	if err != nil {
		return nil, err
	}
	defer CloseDatabase(pool)

	rows, err := pool.Query(context.Background(), `
		SELECT u.id, COALESCE(NULLIF(u.timezone, ''), 'UTC'), COALESCE(p.reminder_timing, $2)
		FROM users u
		LEFT JOIN user_email_prefs p ON p.user_id = u.id
		WHERE u.id = ANY($1)
		  AND NOT COALESCE(u.is_banned, FALSE) AND NOT COALESCE(u.is_agent, FALSE) AND NOT COALESCE(u.is_system, FALSE)`,
		userIDs, ReminderTimingDefault)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id int
		var r ReminderRecipient
		if err := rows.Scan(&id, &r.Timezone, &r.Timing); err != nil {
			return nil, err
		}
		r.Timezone = strings.TrimSpace(r.Timezone)
		out[id] = r
	}
	return out, rows.Err()
}

// TryMarkReminderSent records a reminder and reports whether this caller is
// the first to send it for that task, user, due date, and kind.
func TryMarkReminderSent(taskID, userID int, dueDate time.Time, kind string) (bool, error) {
	pool, err := OpenDatabase()
	if err != nil {
		return false, err
	}
	defer CloseDatabase(pool)
	tag, err := pool.Exec(context.Background(), `
		INSERT INTO task_reminder_sent (task_id, user_id, due_date, kind)
		VALUES ($1, $2, $3::date, $4) ON CONFLICT DO NOTHING`,
		taskID, userID, dueDate.Format("2006-01-02"), kind)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() > 0, nil
}
