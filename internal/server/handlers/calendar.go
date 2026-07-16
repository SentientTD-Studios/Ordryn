package handlers

import (
	"GoTodo/internal/config"
	"GoTodo/internal/server/utils"
	"GoTodo/internal/storage"
	"context"
	"fmt"
	"html"
	"net/http"
	"strings"
	"time"

	"github.com/microcosm-cc/bluemonday"
)

// CalendarFeedHandler serves a read-only ICS feed for incomplete tasks with due dates.
func CalendarFeedHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	token := parseCalendarTokenFromPath(r.URL.Path)
	if token == "" {
		writeEmptyICS(w)
		return
	}

	userID, err := storage.GetUserByCalendarToken(token)
	if err != nil {
		writeEmptyICS(w)
		return
	}

	pool, err := storage.OpenDatabase()
	if err != nil {
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	defer storage.CloseDatabase(pool)

	ctx := context.Background()
	rows, err := pool.Query(ctx, `
		SELECT id, title, COALESCE(description, ''), CAST(due_date AS TEXT)
		FROM tasks
		WHERE user_id = $1 AND completed = false AND due_date IS NOT NULL
		ORDER BY due_date ASC, id ASC`, userID)
	if err != nil {
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	now := time.Now().UTC().Format("20060102T150405Z")
	var b strings.Builder
	writeICSHeader(&b)
	for rows.Next() {
		var id int
		var title, description, dueDate string
		if err := rows.Scan(&id, &title, &description, &dueDate); err != nil {
			http.Error(w, "Database error", http.StatusInternalServerError)
			return
		}
		writeICSEvent(&b, id, userID, title, description, dueDate, now)
	}
	if err := rows.Err(); err != nil {
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	fmt.Fprintf(&b, "END:VCALENDAR\r\n")

	w.Header().Set("Content-Type", "text/calendar; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="gotodo.ics"`)
	w.Header().Set("Cache-Control", "private, no-store")
	w.Write([]byte(b.String()))
}

func writeEmptyICS(w http.ResponseWriter) {
	var b strings.Builder
	writeICSHeader(&b)
	fmt.Fprintf(&b, "END:VCALENDAR\r\n")
	w.Header().Set("Content-Type", "text/calendar; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="gotodo.ics"`)
	w.Header().Set("Cache-Control", "private, no-store")
	w.Write([]byte(b.String()))
}

func writeICSHeader(b *strings.Builder) {
	fmt.Fprintf(b, "BEGIN:VCALENDAR\r\n")
	fmt.Fprintf(b, "VERSION:2.0\r\n")
	fmt.Fprintf(b, "PRODID:-//GoTodo//EN\r\n")
	fmt.Fprintf(b, "CALSCALE:GREGORIAN\r\n")
	fmt.Fprintf(b, "METHOD:PUBLISH\r\n")
	fmt.Fprintf(b, "X-WR-CALNAME:GoTodo\r\n")
}

func writeICSEvent(b *strings.Builder, id, userID int, title, description, dueDate, dtstamp string) {
	icsDate := strings.ReplaceAll(dueDate, "-", "")
	if icsDate == "" {
		return
	}
	endDate := icsDate
	if t, err := time.Parse("20060102", icsDate); err == nil {
		endDate = t.AddDate(0, 0, 1).Format("20060102")
	}
	fmt.Fprintf(b, "BEGIN:VEVENT\r\n")
	icsWriteFolded(b, "UID", fmt.Sprintf("gotodo-u%d-t%d@gotodo", userID, id))
	icsWriteFolded(b, "DTSTAMP", dtstamp)
	icsWriteFolded(b, "DTSTART;VALUE=DATE", icsDate)
	icsWriteFolded(b, "DTEND;VALUE=DATE", endDate)
	icsWriteFolded(b, "SUMMARY", icsEscape(title))
	plainDesc := plainDescriptionForICS(description)
	if plainDesc != "" {
		icsWriteFolded(b, "DESCRIPTION", icsEscape(plainDesc))
	}
	fmt.Fprintf(b, "END:VEVENT\r\n")
}

func parseCalendarTokenFromPath(path string) string {
	path = strings.TrimPrefix(path, "/cal/")
	base := utils.GetBasePath()
	if base != "" && base != "/" {
		path = strings.TrimPrefix(path, strings.TrimSuffix(base, "/")+"/cal/")
		path = strings.TrimPrefix(path, base+"/cal/")
	}
	token := strings.TrimSuffix(path, ".ics")
	token = strings.Trim(token, "/")
	if token == "" || strings.Contains(token, "/") {
		return ""
	}
	return token
}

// APICalendarRegenerateToken rotates the user's calendar feed token.
func APICalendarRegenerateToken(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Invalid request method", http.StatusMethodNotAllowed)
		return
	}

	userID := utils.GetSessionUserID(r)
	if userID == nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	token, err := storage.RegenerateCalendarToken(*userID)
	if err != nil {
		http.Error(w, "Failed to regenerate token", http.StatusInternalServerError)
		return
	}

	feedURL := html.EscapeString(calendarFeedURL(r, token))
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprintf(w, `<label for="calendar-feed-url" class="form-label fw-bold">Subscribe URL</label><div class="input-group mb-2"><input type="text" class="form-control" id="calendar-feed-url" readonly value="%s" /><button type="button" class="btn btn-outline-secondary" id="copy-calendar-url" data-url="%s"><i class="bi bi-clipboard"></i> Copy</button></div><p class="text-muted small mb-0">Previous subscription links are now invalid.</p>`, feedURL, feedURL)
}

func calendarFeedURL(r *http.Request, token string) string {
	return calendarFeedURLForRequest(r, token)
}

func calendarFeedURLForRequest(r *http.Request, token string) string {
	calPath := fmt.Sprintf("/cal/%s.ics", token)
	base := utils.GetBasePath()
	if strings.Contains(base, "://") {
		return strings.TrimSuffix(base, "/") + calPath
	}
	if base == "/" {
		return fmt.Sprintf("%s://%s%s", requestScheme(r), r.Host, calPath)
	}
	return fmt.Sprintf("%s://%s%s%s", requestScheme(r), r.Host, base, calPath)
}

func requestScheme(r *http.Request) string {
	if config.Cfg.UseHTTPS {
		return "https"
	}
	if r.TLS != nil {
		return "https"
	}
	if proto := r.Header.Get("X-Forwarded-Proto"); proto != "" {
		return proto
	}
	return "http"
}

func icsEscape(s string) string {
	s = strings.ReplaceAll(s, "\\", "\\\\")
	s = strings.ReplaceAll(s, ";", "\\;")
	s = strings.ReplaceAll(s, ",", "\\,")
	s = strings.ReplaceAll(s, "\r\n", "\\n")
	s = strings.ReplaceAll(s, "\n", "\\n")
	return s
}

// icsWriteFolded writes a property line with RFC 5545 folding (75 octets).
func icsWriteFolded(b *strings.Builder, prop, value string) {
	line := prop + ":" + value
	for len(line) > 75 {
		b.WriteString(line[:75] + "\r\n ")
		line = line[75:]
	}
	b.WriteString(line + "\r\n")
}

func plainDescriptionForICS(md string) string {
	md = strings.TrimSpace(md)
	if md == "" {
		return ""
	}
	html := utils.RenderMarkdown(md)
	plain := bluemonday.StrictPolicy().Sanitize(html)
	plain = strings.Join(strings.Fields(plain), " ")
	return plain
}
