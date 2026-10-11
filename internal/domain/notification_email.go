package domain

import (
	"fmt"
	"log"
	"strconv"
	"strings"
	"time"

	"GoTodo/internal/hooks"
	"GoTodo/internal/mailer"
	"GoTodo/internal/storage"
)

const (
	// instantEmailDelay holds a notification back so one read in-app soon after
	// it arrives is never emailed, and so a burst lands in one email.
	instantEmailDelay = 10 * time.Minute
	// instantEmailMinInterval paces instant email to one per user per window;
	// anything newer waits for the next email.
	instantEmailMinInterval = 10 * time.Minute
	// notificationEmailMaxAge stops a long mail outage from sending stale mail.
	notificationEmailMaxAge = 48 * time.Hour
	// notificationEmailBatchLimit bounds the rows one pass reads.
	notificationEmailBatchLimit = 2000
	// notificationEmailItemsShown caps how many items one email lists.
	notificationEmailItemsShown = 20
	notificationEmailPreviewMax = 200
)

// notificationMailConfig loads outbound mail settings, the site name, and the
// admin switch. Tests replace it.
var notificationMailConfig = func() (cfg mailer.Config, siteName string, enabled bool) {
	s, err := storage.GetSiteSettings()
	if err != nil || s == nil {
		return mailer.Config{}, "", false
	}
	return storage.SiteEmailConfig(s), s.SiteName, s.NotificationEmailsEnabled
}

// sendNotificationMail delivers one email. Tests replace it.
var sendNotificationMail = mailer.SendNotificationEmail

// StartNotificationEmailWorker sends instant notification emails and daily
// digests once a minute.
func StartNotificationEmailWorker() {
	go func() {
		ticker := time.NewTicker(time.Minute)
		defer ticker.Stop()
		for {
			RunNotificationEmailPass(time.Now())
			<-ticker.C
		}
	}()
}

// RunNotificationEmailPass sends whatever instant emails and digests are due
// at now and returns how many emails went out.
func RunNotificationEmailPass(now time.Time) int {
	cfg, siteName, enabled := notificationMailConfig()
	if !enabled || !mailer.Configured(cfg) {
		return 0
	}
	if strings.TrimSpace(siteName) == "" {
		siteName = "Ordryn"
	}
	sent := 0
	sent += runEmailMode(cfg, siteName, storage.EmailModeInstant, instantEmailDelay, now, instantEmailDue)
	sent += runEmailMode(cfg, siteName, storage.EmailModeDigest, 0, now, digestEmailDue)
	return sent
}

func instantEmailDue(first storage.PendingNotificationEmail, now time.Time) bool {
	return first.LastInstantAt == nil || now.Sub(*first.LastInstantAt) >= instantEmailMinInterval
}

// digestEmailDue is true once the user's local digest hour has passed today and
// no digest has gone out yet today.
func digestEmailDue(first storage.PendingNotificationEmail, now time.Time) bool {
	loc := loadLocation(first.Timezone)
	local := now.In(loc)
	if local.Hour() < first.DigestHour {
		return false
	}
	if first.LastDigestAt == nil {
		return true
	}
	return !sameDay(first.LastDigestAt.In(loc), local)
}

func runEmailMode(cfg mailer.Config, siteName, mode string, minAge time.Duration, now time.Time,
	due func(storage.PendingNotificationEmail, time.Time) bool) int {
	rows, err := storage.ListPendingNotificationEmails(mode, minAge, notificationEmailMaxAge, notificationEmailBatchLimit)
	if err != nil {
		log.Printf("notification email: list %s: %v", mode, err)
		return 0
	}
	sent := 0
	for _, batch := range groupPendingByUser(rows) {
		if !due(batch[0], now) {
			continue
		}
		if sendNotificationBatch(cfg, siteName, mode, batch, now) {
			sent++
		}
	}
	return sent
}

func groupPendingByUser(rows []storage.PendingNotificationEmail) [][]storage.PendingNotificationEmail {
	var out [][]storage.PendingNotificationEmail
	for _, r := range rows {
		if n := len(out); n > 0 && out[n-1][0].UserID == r.UserID {
			out[n-1] = append(out[n-1], r)
			continue
		}
		out = append(out, []storage.PendingNotificationEmail{r})
	}
	return out
}

// sendNotificationBatch claims the batch, emails what it won, and releases the
// claim if the send fails so the next pass retries.
func sendNotificationBatch(cfg mailer.Config, siteName, mode string, batch []storage.PendingNotificationEmail, now time.Time) bool {
	ids := make([]int, len(batch))
	for i, r := range batch {
		ids[i] = r.NotificationID
	}
	won, err := storage.ClaimNotificationsForEmail(ids)
	if err != nil {
		log.Printf("notification email: claim user=%d: %v", batch[0].UserID, err)
		return false
	}
	if len(won) == 0 {
		return false
	}
	wonSet := make(map[int]bool, len(won))
	for _, id := range won {
		wonSet[id] = true
	}
	items := make([]storage.PendingNotificationEmail, 0, len(won))
	for _, r := range batch {
		if wonSet[r.NotificationID] {
			items = append(items, r)
		}
	}

	subject, body := composeNotificationEmail(siteName, mode, items, hooks.PublicBaseURL())
	trigger := mailer.TriggerNotification
	if mode == storage.EmailModeDigest {
		trigger = mailer.TriggerNotificationDigest
	}
	if err := sendNotificationMail(cfg, trigger, subject, body, items[0].Email); err != nil {
		log.Printf("notification email: send user=%d: %v", items[0].UserID, err)
		if rerr := storage.ReleaseNotificationEmailClaim(won); rerr != nil {
			log.Printf("notification email: release user=%d: %v", items[0].UserID, rerr)
		}
		return false
	}
	if err := storage.MarkNotificationEmailSent(items[0].UserID, mode, now); err != nil {
		log.Printf("notification email: mark sent user=%d: %v", items[0].UserID, err)
	}
	return true
}

// composeNotificationEmail builds a plain-text email. Links are included only
// when the site has an absolute public URL (PUBLIC_URL), so none are broken.
func composeNotificationEmail(siteName, mode string, items []storage.PendingNotificationEmail, baseURL string) (subject, body string) {
	baseURL = strings.TrimSuffix(strings.TrimSpace(baseURL), "/")
	var b strings.Builder

	switch {
	case mode == storage.EmailModeDigest:
		subject = fmt.Sprintf("[%s] Your daily summary: %s", siteName, pluralize(len(items), "notification"))
		fmt.Fprintf(&b, "Here's what happened on %s since your last summary.\n\n", siteName)
	case len(items) == 1:
		subject = fmt.Sprintf("[%s] %s", siteName, oneLine(items[0].Title, 150))
	default:
		subject = fmt.Sprintf("[%s] %s", siteName, pluralize(len(items), "new notification"))
		fmt.Fprintf(&b, "You have %s on %s.\n\n", pluralize(len(items), "new notification"), siteName)
	}

	shown := items
	if len(shown) > notificationEmailItemsShown {
		shown = shown[:notificationEmailItemsShown]
	}
	for i, it := range shown {
		if i > 0 {
			b.WriteString("\n")
		}
		b.WriteString(oneLine(it.Title, 300))
		b.WriteString("\n")
		if preview := oneLine(it.Body, notificationEmailPreviewMax); preview != "" {
			b.WriteString(preview + "\n")
		}
		var meta []string
		if it.ProjectName != "" {
			meta = append(meta, "Project: "+it.ProjectName)
		}
		if it.ActorName != "" {
			meta = append(meta, "By: "+it.ActorName)
		}
		if len(meta) > 0 {
			b.WriteString(strings.Join(meta, " · ") + "\n")
		}
		if baseURL != "" && it.TaskID > 0 {
			b.WriteString(baseURL + "/tasks/" + strconv.Itoa(it.TaskID) + "\n")
		}
	}
	if extra := len(items) - len(shown); extra > 0 {
		fmt.Fprintf(&b, "\n…and %d more.\n", extra)
		if baseURL != "" {
			b.WriteString("See them all: " + baseURL + "/\n")
		}
	}

	b.WriteString("\n--\n")
	fmt.Fprintf(&b, "You're getting this because you turned on email notifications on %s.\n", siteName)
	if baseURL != "" {
		b.WriteString("Change what you get by email: " + baseURL + "/settings#notifications\n")
	} else {
		b.WriteString("Change what you get by email in Profile → Notifications.\n")
	}
	return subject, b.String()
}

// oneLine collapses whitespace and truncates to max runes.
func oneLine(s string, max int) string {
	s = strings.Join(strings.Fields(s), " ")
	r := []rune(s)
	if max > 0 && len(r) > max {
		return string(r[:max-1]) + "…"
	}
	return s
}

func pluralize(n int, noun string) string {
	if n == 1 {
		return "1 " + noun
	}
	return fmt.Sprintf("%d %ss", n, noun)
}

func loadLocation(tz string) *time.Location {
	if loc, err := time.LoadLocation(strings.TrimSpace(tz)); err == nil && tz != "" {
		return loc
	}
	return time.UTC
}

func sameDay(a, b time.Time) bool {
	ay, am, ad := a.Date()
	by, bm, bd := b.Date()
	return ay == by && am == bm && ad == bd
}
