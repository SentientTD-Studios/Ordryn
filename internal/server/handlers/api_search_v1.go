package handlers

import (
	"GoTodo/internal/server/utils"
	"GoTodo/internal/storage"
	"GoTodo/internal/tasks"
	"net/http"
	"strconv"
	"strings"
)

type apiSearchResponse struct {
	Query      string           `json:"query"`
	Tasks      []apiTaskJSON    `json:"tasks"`
	Total      int              `json:"total"`
	Page       int              `json:"page"`
	PerPage    int              `json:"per_page"`
	TotalPages int              `json:"total_pages"`
	Projects   []apiProjectJSON `json:"projects"`
}

// APIV1Search serves GET /api/v2/search: tasks (and projects) the caller can
// open right now that match q. See tasks.ParseSearchQuery for the syntax.
// project=<id> limits it to one project; scope=titles matches free text
// against titles only (live results as you type).
func APIV1Search(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		utils.APIJSONError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed.")
		return
	}
	userID, ok := apiUserFromRequest(r)
	if !ok {
		utils.APIJSONError(w, http.StatusUnauthorized, "unauthorized", "Not authenticated.")
		return
	}
	query := r.URL.Query()
	raw := strings.TrimSpace(query.Get("q"))
	page := 1
	if v, err := strconv.Atoi(query.Get("page")); err == nil && v > 0 {
		page = v
	}
	perPage := 20
	if v, err := strconv.Atoi(query.Get("per_page")); err == nil && v > 0 {
		perPage = min(v, tasks.MaxSearchPageSize)
	}
	projectID := 0
	if v := strings.TrimSpace(query.Get("project")); v != "" {
		id, err := strconv.Atoi(v)
		if err != nil || id <= 0 {
			utils.APIJSONError(w, http.StatusBadRequest, "invalid_request", "project must be a project id.")
			return
		}
		projectID = id
	}

	titlesOnly := false
	switch strings.TrimSpace(query.Get("scope")) {
	case "", "all":
	case "titles":
		titlesOnly = true
	default:
		utils.APIJSONError(w, http.StatusBadRequest, "invalid_request", "scope must be all or titles.")
		return
	}

	resp := apiSearchResponse{Query: raw, Tasks: []apiTaskJSON{}, Page: page, PerPage: perPage, TotalPages: 1, Projects: []apiProjectJSON{}}
	q := tasks.ParseSearchQuery(raw)
	q.TitlesOnly = titlesOnly
	if q.Empty() {
		writeJSON(w, http.StatusOK, resp)
		return
	}
	projects, err := storage.GetAccessibleProjects(userID)
	if err != nil {
		utils.APIJSONError(w, http.StatusInternalServerError, "internal_error", "Search failed.")
		return
	}
	scope := tasks.ResolveSearchScope(projects, q, projectID)
	if projectID > 0 && len(scope.ProjectIDs) == 0 {
		// Same answer for a missing project and one the caller can't open.
		utils.APIJSONError(w, http.StatusNotFound, "not_found", "Project not found.")
		return
	}
	list, total, err := tasks.SiteSearchTasks(userID, GetUserTimezoneByID(userID), q, scope, page, perPage)
	if err != nil {
		utils.APIJSONError(w, http.StatusInternalServerError, "internal_error", "Search failed.")
		return
	}
	for _, t := range list {
		resp.Tasks = append(resp.Tasks, taskToAPIJSONList(t))
	}
	resp.Total = total
	if total > 0 {
		resp.TotalPages = (total + perPage - 1) / perPage
	}
	if projectID == 0 && page == 1 {
		matched := tasks.MatchSearchProjects(projects, q)
		for i := range matched {
			resp.Projects = append(resp.Projects, projectToAPIJSON(&matched[i]))
		}
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, resp)
}
