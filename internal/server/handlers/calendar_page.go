package handlers

import (
	"GoTodo/internal/server/utils"
	"GoTodo/internal/sessionstore"
	"GoTodo/internal/storage"
	"context"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"
)

const calendarViewMonthKey = "calendar_view_month"

type calendarTask struct {
	ID          int
	Title       string
	Due         string
	Priority    int
	ProjectName string
}

type calendarCell struct {
	Date    string
	Day     int
	InMonth bool
	IsToday bool
	Tasks   []calendarTask
}

type calendarMonthView struct {
	YearMonth  string
	MonthLabel string
	PrevMonth  string
	NextMonth  string
	TodayMonth string
	Weeks      [][]calendarCell
}

// CalendarPageHandler renders the in-app calendar at /calendar.
// The viewed month is stored in session (POST to change month; URL stays /calendar).
func CalendarPageHandler(w http.ResponseWriter, r *http.Request) {
	email, _, permissions, timezone, loggedIn, userName := utils.GetSessionUserWithTimezone(r)
	if !loggedIn {
		http.Redirect(w, r, utils.GetBasePath()+"/", http.StatusSeeOther)
		return
	}

	calendarURL := calendarPageURL()

	// Legacy /calendar/YYYY-MM or ?month= → session + clean URL
	if legacy := calendarYearMonthFromPathString(r.URL.Path); isValidYearMonth(legacy) {
		setCalendarViewMonth(w, r, legacy)
		http.Redirect(w, r, calendarURL, http.StatusMovedPermanently)
		return
	}
	if legacy := strings.TrimSpace(r.URL.Query().Get("month")); isValidYearMonth(legacy) {
		setCalendarViewMonth(w, r, legacy)
		http.Redirect(w, r, calendarURL, http.StatusMovedPermanently)
		return
	}

	if r.Method == http.MethodPost {
		if month := strings.TrimSpace(r.FormValue("month")); isValidYearMonth(month) {
			setCalendarViewMonth(w, r, month)
		}
		http.Redirect(w, r, calendarURL, http.StatusSeeOther)
		return
	}
	if r.Method != http.MethodGet {
		http.Error(w, "Invalid request method", http.StatusMethodNotAllowed)
		return
	}

	userID := utils.GetSessionUserID(r)
	yearMonth := getCalendarViewMonth(r, timezone)

	view := calendarMonthView{}
	if userID != nil {
		view = buildCalendarMonth(*userID, timezone, yearMonth)
	}

	loc := loadLocation(timezone)
	monthStart, _ := time.ParseInLocation("2006-01", yearMonth, loc)

	ctx := map[string]interface{}{
		"LoggedIn":      loggedIn,
		"Permissions":   permissions,
		"UserEmail":     email,
		"UserName":      userName,
		"Timezone":      timezone,
		"YearMonth":     yearMonth,
		"Calendar":      view,
		"ReturnTo":      "calendar",
		"CalendarMonth": yearMonth,
		"CurrentPage":   "1",
		"CalendarYear":  monthStart.Year(),
	}
	if userID != nil {
		if projs, err := storage.GetProjectsForUser(*userID); err == nil {
			projList := make([]map[string]interface{}, 0, len(projs))
			for _, p := range projs {
				projList = append(projList, map[string]interface{}{"ID": p.ID, "Name": p.Name})
			}
			ctx["Projects"] = projList
		}
		ctx["Tags"] = tagsListForFilter(*userID, "")
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := utils.RenderTemplate(w, r, "calendar.html", ctx); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func currentYearMonth(timezone string) string {
	return time.Now().In(loadLocation(timezone)).Format("2006-01")
}

func loadLocation(tz string) *time.Location {
	loc, err := time.LoadLocation(tz)
	if err != nil {
		return time.UTC
	}
	return loc
}

func getCalendarViewMonth(r *http.Request, timezone string) string {
	sess, err := sessionstore.Store.Get(r, "session")
	if err == nil && sess != nil {
		if v, ok := sess.Values[calendarViewMonthKey]; ok {
			if s, ok := v.(string); ok && isValidYearMonth(s) {
				return s
			}
		}
	}
	return currentYearMonth(timezone)
}

func setCalendarViewMonth(w http.ResponseWriter, r *http.Request, month string) {
	if !isValidYearMonth(month) {
		return
	}
	sess, err := sessionstore.Store.Get(r, "session")
	if err != nil {
		return
	}
	sess.Values[calendarViewMonthKey] = month
	_ = sess.Save(r, w)
}

func calendarMonthFromRequest(r *http.Request, timezone string) string {
	if month := strings.TrimSpace(r.FormValue("calendar_month")); isValidYearMonth(month) {
		return month
	}
	if isCalendarReturn(r) {
		return getCalendarViewMonth(r, timezone)
	}
	return currentYearMonth(timezone)
}

func isValidYearMonth(s string) bool {
	if len(s) != 7 || s[4] != '-' {
		return false
	}
	_, err := time.Parse("2006-01", s)
	return err == nil
}

func calendarYearMonthFromPathString(path string) string {
	base := strings.TrimSuffix(utils.GetBasePath(), "/")
	prefix := base + "/calendar"
	path = strings.TrimSuffix(path, "/")
	if path == prefix {
		return ""
	}
	if !strings.HasPrefix(path, prefix+"/") {
		return ""
	}
	segment := strings.TrimPrefix(path, prefix+"/")
	if isValidYearMonth(segment) {
		return segment
	}
	return ""
}

func calendarPageURL() string {
	return utils.GetBasePath() + "/calendar"
}

func isCalendarReturn(r *http.Request) bool {
	return r.URL.Query().Get("from") == "calendar" || r.FormValue("return_to") == "calendar"
}

func respondCalendarRedirect(w http.ResponseWriter, r *http.Request, month, timezone string) {
	if isValidYearMonth(month) {
		setCalendarViewMonth(w, r, month)
	} else {
		setCalendarViewMonth(w, r, currentYearMonth(timezone))
	}
	w.Header().Set("HX-Redirect", calendarPageURL())
	w.WriteHeader(http.StatusOK)
	fmt.Fprint(w, " ")
}

func buildCalendarMonth(userID int, timezone, yearMonth string) calendarMonthView {
	loc := loadLocation(timezone)
	monthStart, err := time.ParseInLocation("2006-01", yearMonth, loc)
	if err != nil {
		return calendarMonthView{YearMonth: yearMonth}
	}

	tasksByDate := map[string][]calendarTask{}
	if userID > 0 {
		tasksByDate = fetchCalendarTasksByDate(userID, timezone, yearMonth)
	}
	today := time.Now().In(loc).Format("2006-01-02")

	gridStart := monthStart
	for gridStart.Weekday() != time.Sunday {
		gridStart = gridStart.AddDate(0, 0, -1)
	}

	weeks := make([][]calendarCell, 0, 6)
	cursor := gridStart
	for w := 0; w < 6; w++ {
		week := make([]calendarCell, 7)
		for d := 0; d < 7; d++ {
			dateStr := cursor.Format("2006-01-02")
			week[d] = calendarCell{
				Date:    dateStr,
				Day:     cursor.Day(),
				InMonth: cursor.Month() == monthStart.Month() && cursor.Year() == monthStart.Year(),
				IsToday: dateStr == today,
				Tasks:   tasksByDate[dateStr],
			}
			cursor = cursor.AddDate(0, 0, 1)
		}
		weeks = append(weeks, week)
	}

	prev := monthStart.AddDate(0, -1, 0).Format("2006-01")
	next := monthStart.AddDate(0, 1, 0).Format("2006-01")

	return calendarMonthView{
		YearMonth:  yearMonth,
		MonthLabel: monthStart.Format("January 2006"),
		PrevMonth:  prev,
		NextMonth:  next,
		TodayMonth: time.Now().In(loc).Format("2006-01"),
		Weeks:      weeks,
	}
}

func fetchCalendarTasksByDate(userID int, timezone, yearMonth string) map[string][]calendarTask {
	pool, err := storage.OpenDatabase()
	if err != nil {
		return nil
	}
	defer storage.CloseDatabase(pool)

	loc := loadLocation(timezone)
	monthStart, err := time.ParseInLocation("2006-01", yearMonth, loc)
	if err != nil {
		return nil
	}
	start := monthStart.Format("2006-01-02")
	end := monthStart.AddDate(0, 1, 0).Format("2006-01-02")

	rows, err := pool.Query(context.Background(), `
		SELECT t.id, t.title, CAST(t.due_date AS TEXT), COALESCE(t.priority, 0), COALESCE(p.name, '')
		FROM tasks t
		LEFT JOIN projects p ON t.project_id = p.id
		WHERE t.user_id = $1 AND t.completed = false AND t.due_date IS NOT NULL
		  AND t.due_date >= $2::date AND t.due_date < $3::date
		ORDER BY t.due_date, t.priority DESC, t.id`, userID, start, end)
	if err != nil {
		return nil
	}
	defer rows.Close()

	byDate := map[string][]calendarTask{}
	for rows.Next() {
		var ct calendarTask
		if err := rows.Scan(&ct.ID, &ct.Title, &ct.Due, &ct.Priority, &ct.ProjectName); err != nil {
			continue
		}
		byDate[ct.Due] = append(byDate[ct.Due], ct)
	}
	return byDate
}

// APICalendarSyncDueDates imports due-date changes from an uploaded ICS file.
func APICalendarSyncDueDates(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	userID := utils.GetSessionUserID(r)
	if userID == nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	if err := r.ParseMultipartForm(2 << 20); err != nil {
		http.Error(w, "Invalid form", http.StatusBadRequest)
		return
	}
	file, _, err := r.FormFile("ics_file")
	if err != nil {
		http.Error(w, "ICS file required", http.StatusBadRequest)
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
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	defer storage.CloseDatabase(pool)
	ctx := context.Background()

	for _, ev := range events {
		taskID, userFromUID, ok := parseGoTodoUID(ev.UID)
		if !ok {
			continue
		}
		if userFromUID != 0 && userFromUID != *userID {
			continue
		}
		if ev.StartDate == "" || len(ev.StartDate) < 8 {
			continue
		}
		due := fmt.Sprintf("%s-%s-%s", ev.StartDate[0:4], ev.StartDate[4:6], ev.StartDate[6:8])
		tag, err := pool.Exec(ctx,
			`UPDATE tasks SET due_date = $1::date, date_modified = NOW() AT TIME ZONE 'UTC'
			 WHERE id = $2 AND user_id = $3 AND completed = false`, due, taskID, *userID)
		if err == nil && tag.RowsAffected() > 0 {
			updated++
		}
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprintf(w, `<p class="text-success mb-0">Updated %d task due date(s) from calendar file.</p>`, updated)
}

type icsEvent struct {
	UID       string
	StartDate string
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

// sortedCalendarDates returns date keys in ascending order (used in tests).
func sortedCalendarDates(byDate map[string][]calendarTask) []string {
	dates := make([]string, 0, len(byDate))
	for d := range byDate {
		dates = append(dates, d)
	}
	sort.Strings(dates)
	return dates
}
