package handlers

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"GoTodo/internal/domain"
	"GoTodo/internal/server/utils"
	"GoTodo/internal/tasks"
)

type apiPlannerTaskJSON struct {
	apiTaskJSON
	SnoozedUntil string `json:"snoozed_until,omitempty"`
}

type apiPlannerDayJSON struct {
	Date  string               `json:"date"`
	Tasks []apiPlannerTaskJSON `json:"tasks"`
}

type apiPlannerJSON struct {
	Today    string               `json:"today"`
	Days     int                  `json:"days"`
	Overdue  []apiPlannerTaskJSON `json:"overdue"`
	DueToday []apiPlannerTaskJSON `json:"due_today"`
	Woken    []apiPlannerTaskJSON `json:"woken"`
	Upcoming []apiPlannerDayJSON  `json:"upcoming"`
	Snoozed  []apiPlannerTaskJSON `json:"snoozed"`
}

func plannerTasksJSON(in []tasks.PlannerTask) []apiPlannerTaskJSON {
	out := make([]apiPlannerTaskJSON, 0, len(in))
	for _, t := range in {
		out = append(out, apiPlannerTaskJSON{apiTaskJSON: taskToAPIJSONList(t.Task), SnoozedUntil: t.SnoozedUntil})
	}
	return out
}

// userToday is today's date (YYYY-MM-DD) in the user's timezone.
func userToday(tz string) string {
	loc, err := time.LoadLocation(tz)
	if err != nil {
		loc = time.UTC
	}
	return time.Now().In(loc).Format("2006-01-02")
}

// APIV1Planner handles GET /api/v2/planner?days=14.
func APIV1Planner(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		utils.APIJSONError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed.")
		return
	}
	userID, ok := apiUserFromRequest(r)
	if !ok {
		utils.APIJSONError(w, http.StatusUnauthorized, "unauthorized", "Not authenticated.")
		return
	}
	days := 14
	if raw := strings.TrimSpace(r.URL.Query().Get("days")); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 1 || n > tasks.PlannerMaxDays {
			utils.APIJSONError(w, http.StatusBadRequest, "invalid_request", "days must be 1-60.")
			return
		}
		days = n
	}
	tz := GetUserTimezoneByID(userID)
	p, err := tasks.FetchPlanner(userID, tz, userToday(tz), days)
	if err != nil {
		utils.APIJSONError(w, http.StatusInternalServerError, "internal_error", "Failed to load the planner.")
		return
	}
	out := apiPlannerJSON{
		Today:    p.Today,
		Days:     days,
		Overdue:  plannerTasksJSON(p.Overdue),
		DueToday: plannerTasksJSON(p.DueToday),
		Woken:    plannerTasksJSON(p.Woken),
		Upcoming: make([]apiPlannerDayJSON, 0, len(p.Upcoming)),
		Snoozed:  plannerTasksJSON(p.Snoozed),
	}
	for _, d := range p.Upcoming {
		out.Upcoming = append(out.Upcoming, apiPlannerDayJSON{Date: d.Date, Tasks: plannerTasksJSON(d.Tasks)})
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(out)
}

type apiSnoozeRequest struct {
	Until string `json:"until"`
}

// apiV1TaskSnooze handles PUT/DELETE /api/v2/tasks/{id}/snooze.
func apiV1TaskSnooze(w http.ResponseWriter, r *http.Request, taskID int) {
	userID, ok := apiUserFromRequest(r)
	if !ok {
		utils.APIJSONError(w, http.StatusUnauthorized, "unauthorized", "Not authenticated.")
		return
	}
	switch r.Method {
	case http.MethodPut:
		var req apiSnoozeRequest
		if err := decodeJSONBody(r, &req); err != nil {
			utils.APIJSONError(w, http.StatusBadRequest, "invalid_request", "Invalid JSON body.")
			return
		}
		today := userToday(GetUserTimezoneByID(userID))
		if err := domain.SnoozeTaskForUser(r.Context(), userID, taskID, req.Until, today); err != nil {
			writeWorkflowDomainError(w, err)
			return
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_ = json.NewEncoder(w).Encode(map[string]string{"snoozed_until": strings.TrimSpace(req.Until)})
	case http.MethodDelete:
		if err := domain.ClearTaskSnoozeForUser(r.Context(), userID, taskID); err != nil {
			writeWorkflowDomainError(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	default:
		utils.APIJSONError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed.")
	}
}
