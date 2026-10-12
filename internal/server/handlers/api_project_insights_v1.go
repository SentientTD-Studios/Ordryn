package handlers

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"GoTodo/internal/domain"
	"GoTodo/internal/server/utils"
)

type apiInsightsSummaryJSON struct {
	Open       int `json:"open"`
	Done       int `json:"done"`
	Overdue    int `json:"overdue"`
	Unclaimed  int `json:"unclaimed"`
	OpenPoints int `json:"open_points"`
}

type apiInsightsWeekJSON struct {
	WeekStart string `json:"week_start"`
	Tasks     int    `json:"tasks"`
	Points    int    `json:"points"`
}

type apiInsightsMemberJSON struct {
	UserID        int    `json:"user_id"`
	Name          string `json:"name"`
	IsAgent       bool   `json:"is_agent"`
	IsMember      bool   `json:"is_member"`
	ClaimedOpen   int    `json:"claimed_open"`
	ClaimedPoints int    `json:"claimed_points"`
	Completed     int    `json:"completed"`
}

type apiInsightsSprintJSON struct {
	ID        int    `json:"id"`
	Name      string `json:"name"`
	StartDate string `json:"start_date"`
	EndDate   string `json:"end_date"`
}

type apiBurndownDayJSON struct {
	Date            string  `json:"date"`
	RemainingTasks  *int    `json:"remaining_tasks"`
	RemainingPoints *int    `json:"remaining_points"`
	IdealTasks      float64 `json:"ideal_tasks"`
	IdealPoints     float64 `json:"ideal_points"`
}

type apiBurndownJSON struct {
	Sprint      apiInsightsSprintJSON `json:"sprint"`
	TotalTasks  int                   `json:"total_tasks"`
	TotalPoints int                   `json:"total_points"`
	Days        []apiBurndownDayJSON  `json:"days"`
}

type apiProjectInsightsJSON struct {
	ProjectID      int                     `json:"project_id"`
	ProjectName    string                  `json:"project_name"`
	WorkflowMode   string                  `json:"workflow_mode"`
	Summary        apiInsightsSummaryJSON  `json:"summary"`
	Throughput     []apiInsightsWeekJSON   `json:"throughput"`
	CompletedDays  int                     `json:"completed_days"`
	CanViewMembers bool                    `json:"can_view_members"`
	Members        []apiInsightsMemberJSON `json:"members"`
	Sprints        []apiInsightsSprintJSON `json:"sprints"`
	Burndown       *apiBurndownJSON        `json:"burndown"`
}

func insightsSprintJSON(s domain.SprintRef) apiInsightsSprintJSON {
	return apiInsightsSprintJSON{ID: s.ID, Name: s.Name, StartDate: s.StartDate, EndDate: s.EndDate}
}

// apiV1ProjectInsights handles GET /api/v2/projects/{id}/insights[?sprint_id=N].
func apiV1ProjectInsights(w http.ResponseWriter, r *http.Request, projectID int) {
	if r.Method != http.MethodGet {
		utils.APIJSONError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed.")
		return
	}
	userID, ok := apiUserFromRequest(r)
	if !ok {
		utils.APIJSONError(w, http.StatusUnauthorized, "unauthorized", "Not authenticated.")
		return
	}
	sprintID := 0
	if raw := strings.TrimSpace(r.URL.Query().Get("sprint_id")); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n <= 0 {
			utils.APIJSONError(w, http.StatusBadRequest, "invalid_request", "Invalid sprint_id.")
			return
		}
		sprintID = n
	}
	ins, err := domain.GetProjectInsightsForUser(r.Context(), userID, projectID, sprintID, GetUserTimezoneByID(userID))
	switch {
	case errors.Is(err, domain.ErrNotFound):
		utils.APIJSONError(w, http.StatusNotFound, "not_found", "Project not found.")
		return
	case errors.Is(err, domain.ErrValidation):
		utils.APIJSONError(w, http.StatusBadRequest, "invalid_request", "Sprint not found or has no dates.")
		return
	case err != nil:
		utils.APIJSONError(w, http.StatusInternalServerError, "internal_error", "Failed to load project insights.")
		return
	}

	out := apiProjectInsightsJSON{
		ProjectID:    ins.ProjectID,
		ProjectName:  ins.ProjectName,
		WorkflowMode: ins.WorkflowMode,
		Summary: apiInsightsSummaryJSON{
			Open: ins.Summary.Open, Done: ins.Summary.Done, Overdue: ins.Summary.Overdue,
			Unclaimed: ins.Summary.Unclaimed, OpenPoints: ins.Summary.OpenPoints,
		},
		Throughput:     make([]apiInsightsWeekJSON, 0, len(ins.Throughput)),
		CompletedDays:  ins.CompletedDays,
		CanViewMembers: ins.CanViewMembers,
		Members:        make([]apiInsightsMemberJSON, 0, len(ins.Members)),
		Sprints:        make([]apiInsightsSprintJSON, 0, len(ins.Sprints)),
	}
	for _, wk := range ins.Throughput {
		out.Throughput = append(out.Throughput, apiInsightsWeekJSON{
			WeekStart: wk.WeekStart.Format("2006-01-02"), Tasks: wk.Tasks, Points: wk.Points,
		})
	}
	for _, m := range ins.Members {
		out.Members = append(out.Members, apiInsightsMemberJSON{
			UserID: m.UserID, Name: m.Name, IsAgent: m.IsAgent, IsMember: m.IsMember,
			ClaimedOpen: m.ClaimedOpen, ClaimedPoints: m.ClaimedPoints, Completed: m.Completed,
		})
	}
	for _, s := range ins.Sprints {
		out.Sprints = append(out.Sprints, insightsSprintJSON(s))
	}
	if bd := ins.Burndown; bd != nil {
		days := make([]apiBurndownDayJSON, 0, len(bd.Days))
		for _, d := range bd.Days {
			days = append(days, apiBurndownDayJSON{
				Date: d.Date, RemainingTasks: d.RemainingTasks, RemainingPoints: d.RemainingPoints,
				IdealTasks: d.IdealTasks, IdealPoints: d.IdealPoints,
			})
		}
		out.Burndown = &apiBurndownJSON{
			Sprint: insightsSprintJSON(bd.Sprint), TotalTasks: bd.TotalTasks, TotalPoints: bd.TotalPoints, Days: days,
		}
	}
	writeJSON(w, http.StatusOK, out)
}

// apiV1ProjectStatusCounts handles GET /api/v2/projects/{id}/status-counts?sprint_id=N|backlog.
func apiV1ProjectStatusCounts(w http.ResponseWriter, r *http.Request, projectID int) {
	if r.Method != http.MethodGet {
		utils.APIJSONError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed.")
		return
	}
	userID, ok := apiUserFromRequest(r)
	if !ok {
		utils.APIJSONError(w, http.StatusUnauthorized, "unauthorized", "Not authenticated.")
		return
	}
	sprintID := 0
	if raw := strings.TrimSpace(r.URL.Query().Get("sprint_id")); raw != "" && raw != "backlog" && raw != "0" {
		n, err := strconv.Atoi(raw)
		if err != nil || n <= 0 {
			utils.APIJSONError(w, http.StatusBadRequest, "invalid_request", "Invalid sprint_id.")
			return
		}
		sprintID = n
	}
	counts, err := domain.ProjectStatusCountsForUser(r.Context(), userID, projectID, sprintID)
	if errors.Is(err, domain.ErrNotFound) {
		utils.APIJSONError(w, http.StatusNotFound, "not_found", "Project not found.")
		return
	}
	if err != nil {
		utils.APIJSONError(w, http.StatusInternalServerError, "internal_error", "Failed to count tasks.")
		return
	}
	out := make(map[string]int, len(counts))
	for status, n := range counts {
		out[strconv.Itoa(status)] = n
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"counts": out})
}
