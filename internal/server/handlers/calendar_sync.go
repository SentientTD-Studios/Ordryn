package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"GoTodo/internal/server/utils"
	"GoTodo/internal/storage"
)

// APIV1DismissAnnouncement marks the global announcement dismissed for the session.
func APIV1DismissAnnouncement(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		utils.APIJSONError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed.")
		return
	}
	session, err := utils.GetSession(r)
	if err != nil {
		utils.APIJSONError(w, http.StatusInternalServerError, "internal_error", "Session error.")
		return
	}
	session.Values["announcement_dismissed"] = true
	if err := session.Save(r, w); err != nil {
		utils.APIJSONError(w, http.StatusInternalServerError, "internal_error", "Session save error.")
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(map[string]bool{"ok": true})
}

type icsEvent struct {
	UID       string
	StartDate string
}

func apiV1CalendarSync(w http.ResponseWriter, r *http.Request) {
	userID, ok := utils.GetAPIUserID(r)
	if !ok {
		utils.APIJSONError(w, http.StatusUnauthorized, "unauthorized", "Not authenticated.")
		return
	}

	if err := r.ParseMultipartForm(2 << 20); err != nil {
		utils.APIJSONError(w, http.StatusBadRequest, "invalid_request", "Invalid form.")
		return
	}
	file, _, err := r.FormFile("ics_file")
	if err != nil {
		utils.APIJSONError(w, http.StatusBadRequest, "invalid_request", "ICS file required.")
		return
	}
	defer file.Close()

	buf := make([]byte, 0, 64*1024)
	tmp := make([]byte, 4096)
	for {
		n, readErr := file.Read(tmp)
		if n > 0 {
			buf = append(buf, tmp[:n]...)
		}
		if readErr != nil {
			break
		}
	}

	events := parseICSEvents(string(buf))
	updated := 0
	pool, err := storage.OpenDatabase()
	if err != nil {
		utils.APIJSONError(w, http.StatusInternalServerError, "internal_error", "Database error.")
		return
	}
	defer storage.CloseDatabase(pool)
	ctx := context.Background()

	for _, ev := range events {
		taskID, userFromUID, ok := parseGoTodoUID(ev.UID)
		if !ok {
			continue
		}
		if userFromUID != 0 && userFromUID != userID {
			continue
		}
		if ev.StartDate == "" || len(ev.StartDate) < 8 {
			continue
		}
		due := fmt.Sprintf("%s-%s-%s", ev.StartDate[0:4], ev.StartDate[4:6], ev.StartDate[6:8])
		tag, err := pool.Exec(ctx,
			`UPDATE tasks SET due_date = $1::date, date_modified = NOW() AT TIME ZONE 'UTC'
			 WHERE id = $2 AND user_id = $3 AND completed = false`, due, taskID, userID)
		if err == nil && tag.RowsAffected() > 0 {
			updated++
		}
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(map[string]int{"updated": updated})
}

func parseICSEvents(content string) []icsEvent {
	var events []icsEvent
	parts := strings.Split(content, "BEGIN:VEVENT")
	for _, p := range parts[1:] {
		end := strings.Index(p, "END:VEVENT")
		if end < 0 {
			continue
		}
		block := p[:end]
		ev := icsEvent{}
		for _, line := range unfoldICSLines(block) {
			upper := strings.ToUpper(line)
			if strings.HasPrefix(upper, "UID:") {
				ev.UID = strings.TrimSpace(line[4:])
			}
			if strings.HasPrefix(upper, "DTSTART;VALUE=DATE:") {
				ev.StartDate = strings.TrimSpace(line[len("DTSTART;VALUE=DATE:"):])
			} else if strings.HasPrefix(upper, "DTSTART:") {
				v := strings.TrimSpace(line[8:])
				if len(v) >= 8 {
					ev.StartDate = v[:8]
				}
			}
		}
		if ev.UID != "" {
			events = append(events, ev)
		}
	}
	return events
}

func unfoldICSLines(block string) []string {
	raw := strings.Split(block, "\n")
	var lines []string
	for _, l := range raw {
		l = strings.TrimSpace(strings.ReplaceAll(l, "\r", ""))
		if l == "" {
			continue
		}
		if len(lines) > 0 && (l[0] == ' ' || l[0] == '\t') {
			lines[len(lines)-1] += strings.TrimSpace(l)
		} else {
			lines = append(lines, l)
		}
	}
	return lines
}

func parseGoTodoUID(uid string) (taskID, userID int, ok bool) {
	if n, _ := fmt.Sscanf(uid, "gotodo-u%d-t%d@gotodo", &userID, &taskID); n == 2 {
		return taskID, userID, true
	}
	if n, _ := fmt.Sscanf(uid, "gotodo-%d@gotodo", &taskID); n == 1 {
		return taskID, 0, true
	}
	return 0, 0, false
}
