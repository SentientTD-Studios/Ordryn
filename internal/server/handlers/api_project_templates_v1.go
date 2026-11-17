package handlers

import (
	"net/http"
	"strconv"
	"strings"

	"GoTodo/internal/domain"
	"GoTodo/internal/server/utils"
	"GoTodo/internal/storage"
)

// APIV1ProjectTemplatesRouter handles GET /api/v2/project-templates and
// POST /api/v2/project-templates/{id}/projects.
func APIV1ProjectTemplatesRouter(w http.ResponseWriter, r *http.Request) {
	userID, ok := apiUserFromRequest(r)
	if !ok {
		utils.APIJSONError(w, http.StatusUnauthorized, "unauthorized", "Not authenticated.")
		return
	}
	sub := utils.ParseAPIV1Subpath(r, "project-templates")
	if sub == "" {
		if r.Method != http.MethodGet {
			utils.APIJSONError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed.")
			return
		}
		list, err := domain.ListProjectTemplatesForUser(r.Context(), userID)
		if err != nil {
			writePeopleDomainError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]interface{}{"templates": list})
		return
	}
	parts := strings.Split(sub, "/")
	templateID, err := strconv.Atoi(parts[0])
	if err != nil || templateID <= 0 || len(parts) != 2 || parts[1] != "projects" {
		utils.APIJSONError(w, http.StatusNotFound, "not_found", "Not found.")
		return
	}
	if r.Method != http.MethodPost {
		utils.APIJSONError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed.")
		return
	}
	var req struct {
		Name         string  `json:"name"`
		Description  *string `json:"description"`
		IncludeTasks bool    `json:"include_tasks"`
	}
	if err := decodeJSONBody(r, &req); err != nil {
		utils.APIJSONError(w, http.StatusBadRequest, "invalid_request", "Invalid JSON body.")
		return
	}
	res, err := domain.CreateProjectFromTemplate(r.Context(), userID, domain.FromTemplateInput{
		TemplateID: templateID, Name: req.Name, Description: req.Description, IncludeTasks: req.IncludeTasks,
	})
	if err != nil {
		writePeopleDomainError(w, err)
		return
	}
	warnings := res.Warnings
	if warnings == nil {
		warnings = []string{}
	}
	writeJSON(w, http.StatusCreated, map[string]interface{}{
		"project_id": res.Project.ID, "name": res.Project.Name, "tasks_copied": res.TasksCopied,
		"rules_copied": res.RulesCopied, "rules_skipped": res.RulesSkipped, "warnings": warnings,
	})
}

// apiV1ProjectTemplateFlag handles GET/PUT /api/v2/projects/{id}/template.
func apiV1ProjectTemplateFlag(w http.ResponseWriter, r *http.Request, projectID int) {
	userID, ok := apiUserFromRequest(r)
	if !ok {
		utils.APIJSONError(w, http.StatusUnauthorized, "unauthorized", "Not authenticated.")
		return
	}
	switch r.Method {
	case http.MethodGet:
		if _, err := storage.GetAccessibleProjectByID(projectID, userID); err != nil {
			utils.APIJSONError(w, http.StatusNotFound, "not_found", "Project not found.")
			return
		}
		writeJSON(w, http.StatusOK, map[string]bool{"is_template": storage.ProjectIsTemplate(projectID)})
	case http.MethodPut:
		var req struct {
			IsTemplate bool `json:"is_template"`
		}
		if err := decodeJSONBody(r, &req); err != nil {
			utils.APIJSONError(w, http.StatusBadRequest, "invalid_request", "Invalid JSON body.")
			return
		}
		if err := domain.SetProjectTemplateForUser(r.Context(), userID, projectID, req.IsTemplate); err != nil {
			writePeopleDomainError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]bool{"is_template": req.IsTemplate})
	default:
		utils.APIJSONError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed.")
	}
}
