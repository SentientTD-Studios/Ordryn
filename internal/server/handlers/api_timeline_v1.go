package handlers

import (
	"encoding/json"
	"errors"
	"net/http"

	"GoTodo/internal/domain"
	"GoTodo/internal/server/utils"
)

type apiTimelineTaskJSON struct {
	ID            int    `json:"id"`
	Title         string `json:"title"`
	StartDate     string `json:"start_date,omitempty"`
	DueDate       string `json:"due_date,omitempty"`
	Completed     bool   `json:"completed"`
	Priority      int    `json:"priority"`
	ParentID      *int   `json:"parent_id"`
	StatusName    string `json:"status_name,omitempty"`
	ClaimedByName string `json:"claimed_by_name,omitempty"`
}

type apiTimelineLinkJSON struct {
	FromTaskID int `json:"from_task_id"`
	ToTaskID   int `json:"to_task_id"`
}

type apiTimelineJSON struct {
	ProjectID int                   `json:"project_id"`
	Tasks     []apiTimelineTaskJSON `json:"tasks"`
	Links     []apiTimelineLinkJSON `json:"links"`
	Undated   int                   `json:"undated_count"`
	Truncated bool                  `json:"truncated"`
}

// apiV1ProjectTimeline handles GET /api/v2/projects/{id}/timeline.
func apiV1ProjectTimeline(w http.ResponseWriter, r *http.Request, projectID int) {
	if r.Method != http.MethodGet {
		utils.APIJSONError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed.")
		return
	}
	userID, ok := apiUserFromRequest(r)
	if !ok {
		utils.APIJSONError(w, http.StatusUnauthorized, "unauthorized", "Not authenticated.")
		return
	}
	tl, err := domain.GetProjectTimelineForUser(r.Context(), userID, projectID)
	switch {
	case errors.Is(err, domain.ErrNotFound):
		utils.APIJSONError(w, http.StatusNotFound, "not_found", "Project not found.")
		return
	case err != nil:
		utils.APIJSONError(w, http.StatusInternalServerError, "internal_error", "Failed to load the timeline.")
		return
	}
	out := apiTimelineJSON{
		ProjectID: tl.ProjectID,
		Tasks:     make([]apiTimelineTaskJSON, 0, len(tl.Tasks)),
		Links:     make([]apiTimelineLinkJSON, 0, len(tl.Links)),
		Undated:   tl.Undated,
		Truncated: tl.Truncated,
	}
	for _, t := range tl.Tasks {
		item := apiTimelineTaskJSON{
			ID: t.ID, Title: t.Title, StartDate: t.StartDate, DueDate: t.DueDate,
			Completed: t.Completed, Priority: t.Priority, StatusName: t.StatusName, ClaimedByName: t.ClaimedByName,
		}
		if t.ParentID > 0 {
			pid := t.ParentID
			item.ParentID = &pid
		}
		out.Tasks = append(out.Tasks, item)
	}
	for _, l := range tl.Links {
		out.Links = append(out.Links, apiTimelineLinkJSON{FromTaskID: l.FromTaskID, ToTaskID: l.ToTaskID})
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(out)
}
