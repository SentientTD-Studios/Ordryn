package handlers

import (
	"net/http"
	"strconv"
	"time"

	"GoTodo/internal/domain"
	"GoTodo/internal/server/utils"
	"GoTodo/internal/storage"
)

type apiStatusUpdateJSON struct {
	ID         int    `json:"id"`
	ProjectID  int    `json:"project_id"`
	AuthorID   int    `json:"author_id"`
	AuthorName string `json:"author_name"`
	Health     string `json:"health"`
	Body       string `json:"body"`
	CreatedAt  string `json:"created_at"`
}

func statusUpdateJSON(u storage.ProjectStatusUpdate) apiStatusUpdateJSON {
	return apiStatusUpdateJSON{ID: u.ID, ProjectID: u.ProjectID, AuthorID: u.AuthorID, AuthorName: u.AuthorName,
		Health: u.Health, Body: u.Body, CreatedAt: u.CreatedAt.UTC().Format(time.RFC3339)}
}

type apiPortfolioRowJSON struct {
	ID           int                  `json:"id"`
	Name         string               `json:"name"`
	Slug         string               `json:"slug"`
	WorkflowMode string               `json:"workflow_mode"`
	Archived     bool                 `json:"archived"`
	OwnerName    string               `json:"owner_name"`
	Organization string               `json:"organization_name,omitempty"`
	Open         int                  `json:"open"`
	Overdue      int                  `json:"overdue"`
	DueThisWeek  int                  `json:"due_this_week"`
	Unclaimed    int                  `json:"unclaimed"`
	DoneLastWeek int                  `json:"done_last_week"`
	Done         int                  `json:"done"`
	Latest       *apiStatusUpdateJSON `json:"latest_update"`
	CanPost      bool                 `json:"can_post_update"`
}

// APIV1Portfolio handles GET /api/v2/portfolio[?archived=1].
func APIV1Portfolio(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		utils.APIJSONError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed.")
		return
	}
	userID, ok := apiUserFromRequest(r)
	if !ok {
		utils.APIJSONError(w, http.StatusUnauthorized, "unauthorized", "Not authenticated.")
		return
	}
	rows, err := domain.ListPortfolioForUser(r.Context(), userID, GetUserTimezoneByID(userID), r.URL.Query().Get("archived") == "1")
	if err != nil {
		writePeopleDomainError(w, err)
		return
	}
	out := make([]apiPortfolioRowJSON, 0, len(rows))
	for _, row := range rows {
		p := row.Project
		owner := p.OwnerUserName
		if owner == "" {
			owner = p.OwnerEmail
		}
		j := apiPortfolioRowJSON{
			ID: p.ID, Name: p.Name, Slug: p.Slug, WorkflowMode: p.WorkflowMode, Archived: p.Archived,
			OwnerName: owner, Organization: p.OrganizationName,
			Open: row.Counts.Open, Overdue: row.Counts.Overdue, DueThisWeek: row.Counts.DueThisWeek,
			Unclaimed: row.Counts.Unclaimed, DoneLastWeek: row.Counts.DoneLastWeek, Done: row.Counts.Done,
			CanPost: row.CanPost,
		}
		if row.Latest != nil {
			u := statusUpdateJSON(*row.Latest)
			j.Latest = &u
		}
		out = append(out, j)
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"projects": out})
}

// apiV1ProjectStatusUpdates handles /api/v2/projects/{id}/status-updates[/{updateID}].
func apiV1ProjectStatusUpdates(w http.ResponseWriter, r *http.Request, projectID int, rest []string) {
	userID, ok := apiUserFromRequest(r)
	if !ok {
		utils.APIJSONError(w, http.StatusUnauthorized, "unauthorized", "Not authenticated.")
		return
	}
	if len(rest) == 1 && rest[0] != "" {
		if r.Method != http.MethodDelete {
			utils.APIJSONError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed.")
			return
		}
		updateID, err := strconv.Atoi(rest[0])
		if err != nil || updateID <= 0 {
			utils.APIJSONError(w, http.StatusBadRequest, "invalid_request", "Invalid update id.")
			return
		}
		if err := domain.DeleteProjectStatusUpdateForUser(r.Context(), userID, projectID, updateID); err != nil {
			writePeopleDomainError(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
		return
	}
	switch r.Method {
	case http.MethodGet:
		limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
		list, err := domain.ListProjectStatusUpdatesForUser(r.Context(), userID, projectID, limit)
		if err != nil {
			writePeopleDomainError(w, err)
			return
		}
		out := make([]apiStatusUpdateJSON, 0, len(list))
		for _, u := range list {
			out = append(out, statusUpdateJSON(u))
		}
		writeJSON(w, http.StatusOK, map[string]interface{}{"updates": out})
	case http.MethodPost:
		var req struct {
			Health string `json:"health"`
			Body   string `json:"body"`
		}
		if err := decodeJSONBody(r, &req); err != nil {
			utils.APIJSONError(w, http.StatusBadRequest, "invalid_request", "Invalid JSON body.")
			return
		}
		u, err := domain.PostProjectStatusUpdate(r.Context(), userID, projectID, req.Health, req.Body)
		if err != nil {
			writePeopleDomainError(w, err)
			return
		}
		writeJSON(w, http.StatusCreated, statusUpdateJSON(*u))
	default:
		utils.APIJSONError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed.")
	}
}
