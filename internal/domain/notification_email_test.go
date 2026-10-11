package domain

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"GoTodo/internal/mailer"
	"GoTodo/internal/storage"
)

type sentMail struct {
	trigger, subject, body, to string
}

// stubNotificationMail makes the email worker think mail is configured and the
// site switch is on, and records what it sends instead of sending it.
func stubNotificationMail(t *testing.T, enabled bool) *[]sentMail {
	t.Helper()
	var sent []sentMail
	prevCfg, prevSend := notificationMailConfig, sendNotificationMail
	notificationMailConfig = func() (mailer.Config, string, bool) {
		return mailer.Config{
			Provider: mailer.ProviderSMTP, FromAddress: "noreply@example.com",
			SMTPHost: "smtp.example.com", SMTPPort: 587, SMTPUsername: "u", SMTPPasswordEnc: "enc",
		}, "Testing Site", enabled
	}
	sendNotificationMail = func(_ mailer.Config, trigger, subject, body, to string) error {
		sent = append(sent, sentMail{trigger, subject, body, to})
		return nil
	}
	t.Cleanup(func() { notificationMailConfig, sendNotificationMail = prevCfg, prevSend })
	ensureSiteEmailsEnabled(t)
	return &sent
}

func ensureSiteEmailsEnabled(t *testing.T) {
	t.Helper()
	pool, err := storage.OpenDatabase()
	if err != nil {
		t.Fatal(err)
	}
	defer storage.CloseDatabase(pool)
	if _, err := pool.Exec(context.Background(), `
		INSERT INTO site_settings (id, notification_emails_enabled, notification_emails_enabled_at)
		VALUES (1, TRUE, NOW() - INTERVAL '30 days')
		ON CONFLICT (id) DO UPDATE SET notification_emails_enabled = TRUE,
			notification_emails_enabled_at = NOW() - INTERVAL '30 days'`); err != nil {
		t.Fatalf("site settings: %v", err)
	}
}

// emailTestUser creates an isolated user (other tests notify users 1-3).
func emailTestUser(t *testing.T, id int, tz string) {
	t.Helper()
	pool, err := storage.OpenDatabase()
	if err != nil {
		t.Fatal(err)
	}
	defer storage.CloseDatabase(pool)
	if _, err := pool.Exec(context.Background(), `
		INSERT INTO users (id, email, password, role_id, timezone) VALUES ($1, $2, 'x', 1, $3)
		ON CONFLICT (id) DO UPDATE SET timezone = EXCLUDED.timezone`,
		id, fmt.Sprintf("email-test-%d@example.com", id), tz); err != nil {
		t.Fatalf("user %d: %v", id, err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM user_notifications WHERE user_id = $1`, id)
	})
}

// enableEmail turns email on for types and backdates email_since so test
// notifications (backdated past the instant delay) qualify.
func enableEmail(t *testing.T, userID int, mode string, digestHour int, types ...string) {
	t.Helper()
	if err := storage.SaveUserEmailPrefs(userID, mode, digestHour, storage.ReminderTimingDefault); err != nil {
		t.Fatalf("prefs: %v", err)
	}
	for _, typ := range types {
		if err := storage.SetUserEmailType(userID, typ, true); err != nil {
			t.Fatalf("email type: %v", err)
		}
	}
	execSQL(t, `UPDATE user_email_prefs SET email_since = NOW() - INTERVAL '1 day',
		last_instant_at = NULL, last_digest_at = NULL WHERE user_id = $1`, userID)
}

func execSQL(t *testing.T, q string, args ...any) {
	t.Helper()
	pool, err := storage.OpenDatabase()
	if err != nil {
		t.Fatal(err)
	}
	defer storage.CloseDatabase(pool)
	if _, err := pool.Exec(context.Background(), q, args...); err != nil {
		t.Fatalf("exec %q: %v", q, err)
	}
}

// notify creates a notification aged by age.
func notify(t *testing.T, userID int, typ, title string, age time.Duration) int {
	t.Helper()
	id, err := storage.CreateUserNotification(storage.UserNotification{UserID: userID, Type: typ, Title: title, Body: "body of " + title})
	if err != nil || id == 0 {
		t.Fatalf("notify: id=%d err=%v", id, err)
	}
	execSQL(t, `UPDATE user_notifications SET created_at = NOW() - make_interval(secs => $2) WHERE id = $1`,
		id, age.Seconds())
	return id
}

func TestInstantEmailBatchesUnreadOptedInNotifications(t *testing.T) {
	sent := stubNotificationMail(t, true)
	const uid = 901
	emailTestUser(t, uid, "UTC")
	enableEmail(t, uid, storage.EmailModeInstant, 8, storage.NotificationTaskMentioned, storage.NotificationTaskCommented)

	notify(t, uid, storage.NotificationTaskMentioned, "Mention A", 15*time.Minute)
	notify(t, uid, storage.NotificationTaskCommented, "Comment B", 14*time.Minute)
	readID := notify(t, uid, storage.NotificationTaskCommented, "Read C", 15*time.Minute)
	if err := storage.MarkUserNotificationRead(uid, readID); err != nil {
		t.Fatal(err)
	}
	tooNew := notify(t, uid, storage.NotificationTaskMentioned, "Too new D", 2*time.Minute)
	notify(t, uid, storage.NotificationTaskCreated, "Not opted in E", 15*time.Minute)

	now := time.Now()
	if n := RunNotificationEmailPass(now); n != 1 {
		t.Fatalf("emails sent=%d want 1 (%+v)", n, *sent)
	}
	m := (*sent)[0]
	if m.trigger != mailer.TriggerNotification || !strings.Contains(m.subject, "2 new notifications") {
		t.Fatalf("trigger=%q subject=%q", m.trigger, m.subject)
	}
	for _, want := range []string{"Mention A", "Comment B"} {
		if !strings.Contains(m.body, want) {
			t.Fatalf("body missing %q:\n%s", want, m.body)
		}
	}
	for _, unwanted := range []string{"Read C", "Too new D", "Not opted in E"} {
		if strings.Contains(m.body, unwanted) {
			t.Fatalf("body should not include %q:\n%s", unwanted, m.body)
		}
	}

	// Nothing new is old enough, and the user was just emailed.
	if n := RunNotificationEmailPass(now); n != 0 {
		t.Fatalf("second pass sent %d, want 0", n)
	}

	// Once D ages past the delay, a pass inside the pacing window still waits...
	execSQL(t, `UPDATE user_notifications SET created_at = NOW() - INTERVAL '15 minutes' WHERE id = $1`, tooNew)
	if n := RunNotificationEmailPass(now.Add(5 * time.Minute)); n != 0 {
		t.Fatalf("pass inside pacing window sent %d, want 0", n)
	}
	// ...and the next one after it sends D alone.
	if n := RunNotificationEmailPass(now.Add(11 * time.Minute)); n != 1 {
		t.Fatalf("pass after pacing window sent %d, want 1", n)
	}
	last := (*sent)[len(*sent)-1]
	if !strings.Contains(last.subject, "Too new D") || strings.Contains(last.body, "Mention A") {
		t.Fatalf("expected a single-item email for D, got subject=%q body=%s", last.subject, last.body)
	}
}

func TestEmailNeverSendsBacklogFromBeforeOptIn(t *testing.T) {
	sent := stubNotificationMail(t, true)
	const uid = 902
	emailTestUser(t, uid, "UTC")
	enableEmail(t, uid, storage.EmailModeInstant, 8, storage.NotificationTaskMentioned)
	// Email was switched on 30 minutes ago; this notification is an hour old.
	execSQL(t, `UPDATE user_email_prefs SET email_since = NOW() - INTERVAL '30 minutes' WHERE user_id = $1`, uid)
	notify(t, uid, storage.NotificationTaskMentioned, "Old mention", time.Hour)

	if n := RunNotificationEmailPass(time.Now()); n != 0 {
		t.Fatalf("sent %d backlog emails: %+v", n, *sent)
	}
}

func TestEmailSiteSwitchOffSendsNothing(t *testing.T) {
	sent := stubNotificationMail(t, false)
	const uid = 903
	emailTestUser(t, uid, "UTC")
	enableEmail(t, uid, storage.EmailModeInstant, 8, storage.NotificationTaskMentioned)
	notify(t, uid, storage.NotificationTaskMentioned, "Mention", 15*time.Minute)

	if n := RunNotificationEmailPass(time.Now()); n != 0 || len(*sent) != 0 {
		t.Fatalf("site switch off but sent %d", n)
	}
}

func TestEmailSendFailureReleasesClaimForRetry(t *testing.T) {
	sent := stubNotificationMail(t, true)
	const uid = 904
	emailTestUser(t, uid, "UTC")
	enableEmail(t, uid, storage.EmailModeInstant, 8, storage.NotificationTaskMentioned)
	id := notify(t, uid, storage.NotificationTaskMentioned, "Retry me", 15*time.Minute)

	working := sendNotificationMail
	sendNotificationMail = func(mailer.Config, string, string, string, string) error { return errors.New("smtp down") }
	if n := RunNotificationEmailPass(time.Now()); n != 0 {
		t.Fatalf("failed send counted as sent: %d", n)
	}
	pool, err := storage.OpenDatabase()
	if err != nil {
		t.Fatal(err)
	}
	defer storage.CloseDatabase(pool)
	var emailedAt *time.Time
	if err := pool.QueryRow(context.Background(), `SELECT emailed_at FROM user_notifications WHERE id = $1`, id).Scan(&emailedAt); err != nil {
		t.Fatal(err)
	}
	if emailedAt != nil {
		t.Fatal("claim should be released after a failed send")
	}

	sendNotificationMail = working
	if n := RunNotificationEmailPass(time.Now()); n != 1 || !strings.Contains((*sent)[0].subject, "Retry me") {
		t.Fatalf("retry pass sent %d: %+v", n, *sent)
	}
}

func TestDigestSendsOncePerLocalDayAfterDigestHour(t *testing.T) {
	sent := stubNotificationMail(t, true)
	const uid = 905
	emailTestUser(t, uid, "America/New_York")
	enableEmail(t, uid, storage.EmailModeDigest, 8, storage.NotificationTaskCommented)
	notify(t, uid, storage.NotificationTaskCommented, "Digest item 1", time.Minute)
	notify(t, uid, storage.NotificationTaskCommented, "Digest item 2", time.Minute)

	loc, _ := time.LoadLocation("America/New_York")
	y, mo, d := time.Now().In(loc).Date()
	at := func(hour int) time.Time { return time.Date(y, mo, d, hour, 0, 0, 0, loc) }

	if n := RunNotificationEmailPass(at(7)); n != 0 {
		t.Fatalf("digest sent before digest hour: %d", n)
	}
	if n := RunNotificationEmailPass(at(9)); n != 1 {
		t.Fatalf("digest at 09:00 local sent %d, want 1", n)
	}
	m := (*sent)[0]
	if m.trigger != mailer.TriggerNotificationDigest || !strings.Contains(m.subject, "daily summary: 2 notifications") {
		t.Fatalf("trigger=%q subject=%q", m.trigger, m.subject)
	}
	notify(t, uid, storage.NotificationTaskCommented, "Later the same day", time.Minute)
	if n := RunNotificationEmailPass(at(15)); n != 0 {
		t.Fatalf("second digest the same local day: %d", n)
	}
}

func TestComposeNotificationEmailLinksAndUserText(t *testing.T) {
	items := []storage.PendingNotificationEmail{{
		Title: "You were mentioned on\r\nShip it", Body: "hey  @you\nlook", TaskID: 42,
		ProjectName: "Launch", ActorName: "ada",
	}}
	subject, body := composeNotificationEmail("Site", storage.EmailModeInstant, items, "https://todo.example.com/")
	if subject != "[Site] You were mentioned on Ship it" {
		t.Fatalf("subject=%q", subject)
	}
	for _, want := range []string{"hey @you look", "Project: Launch · By: ada",
		"https://todo.example.com/tasks/42", "https://todo.example.com/settings#notifications"} {
		if !strings.Contains(body, want) {
			t.Fatalf("body missing %q:\n%s", want, body)
		}
	}
	_, noURL := composeNotificationEmail("Site", storage.EmailModeInstant, items, "")
	if strings.Contains(noURL, "http") || !strings.Contains(noURL, "Profile → Notifications") {
		t.Fatalf("without PUBLIC_URL the email should carry no links:\n%s", noURL)
	}
}

func TestUpdateNotificationSettingsValidatesBeforeApplying(t *testing.T) {
	ctx := context.Background()
	const uid = 906
	emailTestUser(t, uid, "UTC")
	bad := "weekly"
	err := UpdateNotificationSettings(ctx, uid, NotificationSettingsUpdate{
		Email: map[string]bool{storage.NotificationTaskMentioned: true},
		Mode:  &bad,
	})
	if !errors.Is(err, ErrValidation) {
		t.Fatalf("err=%v want validation", err)
	}
	types, _ := storage.ListUserEmailTypes(uid)
	if types[storage.NotificationTaskMentioned] {
		t.Fatal("an invalid request must not apply any of its changes")
	}

	if err := UpdateNotificationSettings(ctx, uid, NotificationSettingsUpdate{
		Email: map[string]bool{storage.NotificationAutomation: true},
	}); !errors.Is(err, ErrValidation) {
		t.Fatalf("automation has no email of its own; err=%v", err)
	}
	hour := 24
	if err := UpdateNotificationSettings(ctx, uid, NotificationSettingsUpdate{DigestHour: &hour}); !errors.Is(err, ErrValidation) {
		t.Fatalf("digest hour 24 accepted; err=%v", err)
	}

	instant, timing, h := storage.EmailModeInstant, storage.ReminderTimingMorning, 7
	if err := UpdateNotificationSettings(ctx, uid, NotificationSettingsUpdate{
		Email: map[string]bool{storage.NotificationDueReminder: true},
		Mode:  &instant, DigestHour: &h, ReminderTiming: &timing,
	}); err != nil {
		t.Fatalf("valid update: %v", err)
	}
	p, err := storage.GetUserEmailPrefs(uid)
	if err != nil {
		t.Fatal(err)
	}
	if p.Mode != instant || p.DigestHour != 7 || p.ReminderTiming != timing || p.EmailSince == nil {
		t.Fatalf("prefs=%+v", p)
	}
	prefs, err := ListNotificationPreferences(ctx, uid)
	if err != nil {
		t.Fatal(err)
	}
	for _, pref := range prefs {
		if pref.Type == storage.NotificationDueReminder && !pref.Email {
			t.Fatal("due reminder email should be on")
		}
	}
}
