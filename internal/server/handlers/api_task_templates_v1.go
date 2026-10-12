package handlers

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"GoTodo/internal/domain"
	"GoTodo/internal/server/utils"
	"GoTodo/internal/tasks"
)

type apiTaskTemplateJSON struct {
	ID             int      `json:"id"`
	ProjectID      *int     `json:"project_id"`
	OwnerID        int      `json:"owner_id"`
	OwnerName      string   `json:"owner_name"`
	Name           string   `json:"name"`
	Title          string   `json:"title"`
	Description    string   `json:"description"`
	Priority       int      `json:"priority"`
	TagNames       []string `json:"tag_names"`
	EstimatePoints *int     `json:"estimate_points"`
	DueOffsetDays  *int     `json:"due_offset_days"`
	Subtasks       []string `json:"subtasks"`
	CanManage      bool     `json:"can_manage"`
}

type apiTaskTemplateRequest struct {
	ProjectID      *int     `json:"project_id"`
	Name           string   `json:"name"`
	Title          string   `json:"title"`
	Description    string   `json:"description"`
	Priority       int      `json:"priority"`
	TagNames       []string `json:"tag_names"`
	EstimatePoints *int     `json:"estimate_points"`
	DueOffsetDays  *int     `json:"due_offset_days"`
	Subtasks       []string `json:"subtasks"`
}

func (req apiTaskTemplateRequest) input() domain.TaskTemplateInput {
	return domain.TaskTemplateInput{
		Name: req.Name, Title: req.Title, Description: req.Description, Priority: req.Priority,
		TagNames: req.TagNames, EstimatePoints: req.EstimatePoints, DueOffsetDays: req.DueOffsetDays, Subtasks: req.Subtasks,
	}
}

func taskTemplateJSON(v domain.TaskTemplateView) apiTaskTemplateJSON {
	return apiTaskTemplateJSON{
		ID: v.ID, ProjectID: v.ProjectID, OwnerID: v.OwnerID, OwnerName: v.OwnerName, Name: v.Name, Title: v.Title,
		Description: v.Description, Priority: v.Priority, TagNames: v.TagNames, EstimatePoints: v.EstimatePoints,
		DueOffsetDays: v.DueOffsetDays, Subtasks: v.Subtasks, CanManage: v.CanManage,
	}
}

func writeTaskTemplateError(w http.ResponseWriter, err error, fallback string) {
	switch {
	case errors.Is(err, domain.ErrValidation):
		utils.APIJSONError(w, http.StatusBadRequest, "invalid_request", sharingClientMessage(err, "Invalid template."))
	case errors.Is(err, domain.ErrNotFound):
		utils.APIJSONError(w, http.StatusNotFound, "not_found", "Template not found.")
	case errors.Is(err, domain.ErrForbidden):
		utils.APIJSONError(w, http.StatusForbidden, "forbidden", sharingClientMessage(err, "Forbidden."))
	case errors.Is(err, domain.ErrConflict):
		utils.APIJSONError(w, http.StatusConflict, "conflict", sharingClientMessage(err, "Conflict."))
	default:
		utils.APIJSONError(w, http.StatusInternalServerError, "internal_error", fallback)
	}
}

// APIV1TaskTemplatesRouter handles /api/v2/task-templates, /{id} and /{id}/apply.
func APIV1TaskTemplatesRouter(w http.ResponseWriter, r *http.Request) {
	userID, ok := apiUserFromRequest(r)
	if !ok {
		utils.APIJSONError(w, http.StatusUnauthorized, "unauthorized", "Not authenticated.")
		return
	}
	sub := utils.ParseAPIV1Subpath(r, "task-templates")
	if sub == "" {
		switch r.Method {
		case http.MethodGet:
			apiV1ListTaskTemplates(w, r, userID)
		case http.MethodPost:
			apiV1CreateTaskTemplate(w, r, userID)
		default:
			w.Header().Set("Allow", "GET, POST")
			utils.APIJSONError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed.")
		}
		return
	}

	idPart, action, _ := strings.Cut(sub, "/")
	id, err := strconv.Atoi(idPart)
	if err != nil || id <= 0 {
		utils.APIJSONError(w, http.StatusBadRequest, "invalid_request", "Invalid template id.")
		return
	}
	switch {
	case action == "apply" && r.Method == http.MethodPost:
		apiV1ApplyTaskTemplate(w, r, userID, id)
	case action == "" && (r.Method == http.MethodPut || r.Method == http.MethodPatch):
		apiV1UpdateTaskTemplate(w, r, userID, id)
	case action == "" && r.Method == http.MethodDelete:
		if err := domain.DeleteTaskTemplateForUser(r.Context(), userID, id); err != nil {
			writeTaskTemplateError(w, err, "Failed to delete template.")
			return
		}
		w.WriteHeader(http.StatusNoContent)
	case action == "" || action == "apply":
		utils.APIJSONError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed.")
	default:
		utils.APIJSONError(w, http.StatusNotFound, "not_found", "Not found.")
	}
}

func apiV1ListTaskTemplates(w http.ResponseWriter, r *http.Request, userID int) {
	projectID := 0
	if raw := strings.TrimSpace(r.URL.Query().Get("project_id")); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 0 {
			utils.APIJSONError(w, http.StatusBadRequest, "invalid_request", "Invalid project_id.")
			return
		}
		projectID = n
	}
	list, err := domain.ListTaskTemplatesForUser(r.Context(), userID, projectID)
	if err != nil {
		writeTaskTemplateError(w, err, "Failed to list templates.")
		return
	}
	out := make([]apiTaskTemplateJSON, 0, len(list))
	for _, v := range list {
		out = append(out, taskTemplateJSON(v))
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"templates": out})
}

func apiV1CreateTaskTemplate(w http.ResponseWriter, r *http.Request, userID int) {
	var req apiTaskTemplateRequest
	if err := decodeJSONBody(r, &req); err != nil {
		utils.APIJSONError(w, http.StatusBadRequest, "invalid_request", "Invalid JSON body.")
		return
	}
	v, err := domain.CreateTaskTemplateForUser(r.Context(), userID, req.ProjectID, req.input())
	if err != nil {
		writeTaskTemplateError(w, err, "Failed to save template.")
		return
	}
	writeJSON(w, http.StatusCreated, taskTemplateJSON(*v))
}

func apiV1UpdateTaskTemplate(w http.ResponseWriter, r *http.Request, userID, id int) {
	var req apiTaskTemplateRequest
	if err := decodeJSONBody(r, &req); err != nil {
		utils.APIJSONError(w, http.StatusBadRequest, "invalid_request", "Invalid JSON body.")
		return
	}
	v, err := domain.UpdateTaskTemplateForUser(r.Context(), userID, id, req.input())
	if err != nil {
		writeTaskTemplateError(w, err, "Failed to save template.")
		return
	}
	writeJSON(w, http.StatusOK, taskTemplateJSON(*v))
}

func apiV1ApplyTaskTemplate(w http.ResponseWriter, r *http.Request, userID, id int) {
	var req struct {
		ProjectID *int `json:"project_id"`
		SprintID  *int `json:"sprint_id"`
	}
	if r.ContentLength != 0 {
		if err := decodeJSONBody(r, &req); err != nil {
			utils.APIJSONError(w, http.StatusBadRequest, "invalid_request", "Invalid JSON body.")
			return
		}
	}
	newID, err := domain.ApplyTaskTemplateForUser(r.Context(), userID, id, domain.ApplyTaskTemplateInput{
		ProjectID: req.ProjectID, SprintID: req.SprintID,
	})
	if err != nil {
		writeTaskTemplateError(w, err, "Failed to create a task from the template.")
		return
	}
	task, err := tasks.FetchTaskByIDForUser(newID, userID, GetUserTimezoneByID(userID), 1)
	if err != nil {
		utils.APIJSONError(w, http.StatusInternalServerError, "internal_error", "Task created but failed to load.")
		return
	}
	writeJSON(w, http.StatusCreated, taskToAPIJSON(task))
}
