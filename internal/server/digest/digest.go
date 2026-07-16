package digest

import (
	"GoTodo/internal/server/utils"
	"GoTodo/internal/storage"
	"GoTodo/internal/tasks"
	"context"
	"fmt"
	"log"
	"time"
)

// StartDigestWorker runs an hourly check for users with digest_enabled.
func StartDigestWorker() {
	go func() {
		ticker := time.NewTicker(time.Hour)
		defer ticker.Stop()
		for range ticker.C {
			if err := sendDueDigests(); err != nil {
				log.Printf("digest worker: %v", err)
			}
		}
	}()
}

func sendDueDigests() error {
	pool, err := storage.OpenDatabase()
	if err != nil {
		return err
	}
	defer storage.CloseDatabase(pool)

	ctx := context.Background()
	rows, err := pool.Query(ctx, `
		SELECT id, email, COALESCE(timezone, 'America/New_York'), COALESCE(digest_hour, 8)
		FROM users
		WHERE COALESCE(digest_enabled, false) = true AND COALESCE(is_banned, false) = false`)
	if err != nil {
		return err
	}
	defer rows.Close()

	nowUTC := time.Now().UTC()
	for rows.Next() {
		var userID int
		var email, timezone string
		var digestHour int
		if err := rows.Scan(&userID, &email, &timezone, &digestHour); err != nil {
			continue
		}
		loc, err := time.LoadLocation(timezone)
		if err != nil {
			loc = time.UTC
		}
		localNow := nowUTC.In(loc)
		if localNow.Hour() != digestHour {
			continue
		}
		var lastSent *string
		var lastStr string
		_ = pool.QueryRow(ctx, "SELECT last_digest_sent::text FROM users WHERE id = $1", userID).Scan(&lastStr)
		if lastStr != "" {
			lastSent = &lastStr
		}
		today := localNow.Format("2006-01-02")
		if lastSent != nil && *lastSent == today {
			continue
		}
		if err := sendUserDigest(userID, email, timezone); err != nil {
			log.Printf("digest: user %d: %v", userID, err)
			continue
		}
		_, _ = pool.Exec(ctx, "UPDATE users SET last_digest_sent = $1::date WHERE id = $2", today, userID)
	}
	return rows.Err()
}

func sendUserDigest(userID int, email, timezone string) error {
	uid := userID
	overdue, _ := tasks.GetOverdueCount(userID, timezone)
	list, _, err := tasks.ReturnPaginationForUserWithFilters(1, 50, &uid, timezone, tasks.ListFilters{DueFilter: "today"})
	if err != nil {
		return err
	}
	if overdue == 0 && len(list) == 0 {
		return nil
	}

	siteName := "GoTodo"
	if settings, err := storage.GetSiteSettings(); err == nil && settings != nil && settings.SiteName != "" {
		siteName = settings.SiteName
	}

	body := fmt.Sprintf("Hello,\n\nYour %s daily digest:\n\n", siteName)
	if overdue > 0 {
		body += fmt.Sprintf("- %d overdue task(s)\n", overdue)
	}
	if len(list) > 0 {
		body += fmt.Sprintf("- %d task(s) due today:\n", len(list))
		for _, t := range list {
			body += fmt.Sprintf("  • %s\n", t.Title)
		}
	}
	body += "\nLog in to manage your tasks.\n"

	subject := fmt.Sprintf("%s — Daily task digest", siteName)
	return utils.SendEmail(subject, body, email)
}
