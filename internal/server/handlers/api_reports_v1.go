package handlers

import (
	"encoding/csv"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"GoTodo/internal/domain"
	"GoTodo/internal/server/utils"
)

type apiTimesheetEntryJSON struct {
	ID          int    `json:"id"`
	LoggedAt    string `json:"logged_at"`
	UserID      int    `json:"user_id"`
	UserName    string `json:"user_name"`
	ProjectID   int    `json:"project_id"`
	ProjectName string `json:"project_name"`
	TaskID      int    `json:"task_id"`
	TaskTitle   string `json:"task_title"`
	Minutes     int    `json:"minutes"`
	Note        string `json:"note"`
}

// APIV1ReportsRouter handles /api/v2/reports/time and /api/v2/reports/workload.
func APIV1ReportsRouter(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		utils.APIJSONError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed.")
		return
	}
	userID, ok := apiUserFromRequest(r)
	if !ok {
		utils.APIJSONError(w, http.StatusUnauthorized, "unauthorized", "Not authenticated.")
		return
	}
	switch utils.ParseAPIV1Subpath(r, "reports") {
	case "time":
		apiV1TimeReport(w, r, userID)
	case "workload":
		apiV1WorkloadReport(w, r, userID)
	default:
		utils.APIJSONError(w, http.StatusNotFound, "not_found", "Unknown report.")
	}
}

func apiV1TimeReport(w http.ResponseWriter, r *http.Request, userID int) {
	q := r.URL.Query()
	tz := GetUserTimezoneByID(userID)
	in := domain.TimesheetInput{From: q.Get("from"), To: q.Get("to"), TZ: tz}
	if in.From == "" || in.To == "" {
		loc, err := time.LoadLocation(tz)
		if err != nil {
			loc = time.UTC
		}
		now := time.Now().In(loc)
		in.To = now.Format("2006-01-02")
		in.From = now.AddDate(0, 0, -6).Format("2006-01-02")
	}
	in.UserID, _ = strconv.Atoi(q.Get("user_id"))
	in.ProjectID, _ = strconv.Atoi(q.Get("project_id"))
	ts, err := domain.TimesheetForUser(r.Context(), userID, in)
	if err != nil {
		writePeopleDomainError(w, err)
		return
	}
	if q.Get("format") == "csv" {
		w.Header().Set("Content-Type", "text/csv; charset=utf-8")
		w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="timesheet-%s-to-%s.csv"`, in.From, in.To))
		cw := csv.NewWriter(w)
		_ = cw.Write([]string{"logged_at", "person", "project", "task_id", "task", "minutes", "hours", "note"})
		for _, e := range ts.Entries {
			_ = cw.Write([]string{
				e.LoggedAt.UTC().Format(time.RFC3339), csvSafe(e.UserName), csvSafe(e.ProjectName),
				strconv.Itoa(e.TaskID), csvSafe(e.TaskTitle), strconv.Itoa(e.Minutes),
				strconv.FormatFloat(float64(e.Minutes)/60, 'f', 2, 64), csvSafe(e.Note),
			})
		}
		cw.Flush()
		return
	}
	entries := make([]apiTimesheetEntryJSON, 0, len(ts.Entries))
	for _, e := range ts.Entries {
		entries = append(entries, apiTimesheetEntryJSON{
			ID: e.ID, LoggedAt: e.LoggedAt.UTC().Format(time.RFC3339), UserID: e.UserID, UserName: e.UserName,
			ProjectID: e.ProjectID, ProjectName: e.ProjectName, TaskID: e.TaskID, TaskTitle: e.TaskTitle,
			Minutes: e.Minutes, Note: e.Note,
		})
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"from": in.From, "to": in.To, "entries": entries, "total_minutes": ts.TotalMinutes,
		"by_person": ts.ByPerson, "by_project": ts.ByProject, "truncated": ts.Truncated,
	})
}

func apiV1WorkloadReport(w http.ResponseWriter, r *http.Request, userID int) {
	rows, projects, err := domain.WorkloadForUser(r.Context(), userID, GetUserTimezoneByID(userID))
	if err != nil {
		writePeopleDomainError(w, err)
		return
	}
	out := make([]map[string]interface{}, 0, len(rows))
	for _, row := range rows {
		out = append(out, map[string]interface{}{
			"user_id": row.UserID, "user_name": row.UserName, "claimed_open": row.ClaimedOpen,
			"claimed_hours": row.ClaimedHours, "claimed_overdue": row.ClaimedOver, "done_last_week": row.DoneLastWeek,
			"minutes_last_week": row.MinutesWeek, "projects": row.Projects,
		})
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"people": out, "project_count": projects})
}
