package handlers

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"GoTodo/internal/domain"
	"GoTodo/internal/server/utils"
	"GoTodo/internal/storage"
	"GoTodo/internal/tasks"
)

type apiProjectStatusJSON struct {
	ID          int    `json:"id"`
	ProjectID   int    `json:"project_id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Position    int    `json:"position"`
	IsDone      bool   `json:"is_done"`
	IsDefault   bool   `json:"is_default"`
	CreatedAt   string `json:"created_at"`
}

type apiStatusCreateRequest struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	IsDone      bool   `json:"is_done"`
	IsDefault   bool   `json:"is_default"`
}

type apiStatusPatchRequest struct {
	Name        *string `json:"name"`
	Description *string `json:"description"`
	IsDone      *bool   `json:"is_done"`
	IsDefault   *bool   `json:"is_default"`
}

type apiStatusReorderRequest struct {
	StatusIDs []int `json:"status_ids"`
}

type apiStatusDeleteRequest struct {
	MoveToStatusID *int `json:"move_to_status_id"`
}

type apiTimeEntryJSON struct {
	ID        int    `json:"id"`
	TaskID    int    `json:"task_id"`
	UserID    int    `json:"user_id"`
	Minutes   int    `json:"minutes"`
	Note      string `json:"note"`
	CreatedAt string `json:"created_at"`
	UserEmail string `json:"user_email,omitempty"`
	UserName  string `json:"user_name,omitempty"`
}

type apiTimeEntryCreateRequest struct {
	Minutes int    `json:"minutes"`
	Note    string `json:"note"`
}

func statusToAPIJSON(s storage.ProjectStatus) apiProjectStatusJSON {
	return apiProjectStatusJSON{
		ID:          s.ID,
		ProjectID:   s.ProjectID,
		Name:        s.Name,
		Description: s.Description,
		Position:    s.Position,
		IsDone:      s.IsDone,
		IsDefault:   s.IsDefault,
		CreatedAt:   s.CreatedAt.UTC().Format(time.RFC3339),
	}
}

func timeEntryToAPIJSON(e storage.TaskTimeEntry) apiTimeEntryJSON {
	return apiTimeEntryJSON{
		ID:        e.ID,
		TaskID:    e.TaskID,
		UserID:    e.UserID,
		Minutes:   e.Minutes,
		Note:      e.Note,
		CreatedAt: e.CreatedAt.UTC().Format(time.RFC3339),
		UserEmail: e.UserEmail,
		UserName:  e.UserName,
	}
}

func writeWorkflowDomainError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, domain.ErrNotFound):
		utils.APIJSONError(w, http.StatusNotFound, "not_found", "Not found.")
	case errors.Is(err, domain.ErrForbidden):
		utils.APIJSONError(w, http.StatusForbidden, "forbidden", sharingClientMessage(err, "Forbidden."))
	case errors.Is(err, domain.ErrConflict):
		utils.APIJSONError(w, http.StatusConflict, "conflict", err.Error())
	case errors.Is(err, domain.ErrValidation):
		utils.APIJSONError(w, http.StatusBadRequest, "invalid_request", err.Error())
	default:
		utils.APIJSONError(w, http.StatusInternalServerError, "internal_error", "Request failed.")
	}
}

func projectToAPIJSON(p *storage.ProjectWithAccess) apiProjectJSON {
	mode := p.WorkflowMode
	if mode == "" {
		mode = storage.WorkflowClassic
	}
	backlogName := p.BacklogName
	if backlogName == "" {
		backlogName = "Backlog"
	}
	return apiProjectJSON{
		ID:            p.ID,
		Name:          p.Name,
		Description:   p.Description,
		WorkflowMode:  mode,
		Archived:      p.Archived,
		BacklogName:   backlogName,
		Role:          p.Role,
		OwnerEmail:    p.OwnerEmail,
		OwnerUserName: p.OwnerUserName,
		OwnerUserID:   p.OwnerUserID,
	}
}

func projectStorageToAPIJSON(p *storage.Project, role string) apiProjectJSON {
	mode := p.WorkflowMode
	if mode == "" {
		mode = storage.WorkflowClassic
	}
	backlogName := p.BacklogName
	if backlogName == "" {
		backlogName = "Backlog"
	}
	return apiProjectJSON{
		ID:           p.ID,
		Name:         p.Name,
		Description:  p.Description,
		WorkflowMode: mode,
		Archived:     p.Archived,
		BacklogName:  backlogName,
		Role:         role,
		OwnerUserID:  p.UserID,
	}
}

// handleProjectStatusesResource routes /projects/{id}/statuses...
func handleProjectStatusesResource(w http.ResponseWriter, r *http.Request, projectID int, rest []string) {
	userID, ok := apiUserFromRequest(r)
	if !ok {
		utils.APIJSONError(w, http.StatusUnauthorized, "unauthorized", "Not authenticated.")
		return
	}

	if len(rest) == 0 {
		switch r.Method {
		case http.MethodGet:
			statuses, err := domain.ListProjectStatusesForUser(r.Context(), userID, projectID)
			if err != nil {
				writeWorkflowDomainError(w, err)
				return
			}
			out := make([]apiProjectStatusJSON, 0, len(statuses))
			for _, s := range statuses {
				out = append(out, statusToAPIJSON(s))
			}
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			json.NewEncoder(w).Encode(out)
		case http.MethodPost:
			var req apiStatusCreateRequest
			if err := decodeJSONBody(r, &req); err != nil {
				utils.APIJSONError(w, http.StatusBadRequest, "invalid_request", "Invalid JSON body.")
				return
			}
			s, err := domain.CreateProjectStatusForUser(r.Context(), userID, projectID, domain.CreateProjectStatusInput{
				Name:        req.Name,
				Description: req.Description,
				IsDone:      req.IsDone,
				IsDefault:   req.IsDefault,
			})
			if err != nil {
				writeWorkflowDomainError(w, err)
				return
			}
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			w.WriteHeader(http.StatusCreated)
			json.NewEncoder(w).Encode(statusToAPIJSON(*s))
		default:
			utils.APIJSONError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed.")
		}
		return
	}

	if len(rest) == 1 && rest[0] == "reorder" {
		if r.Method != http.MethodPost {
			utils.APIJSONError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed.")
			return
		}
		var req apiStatusReorderRequest
		if err := decodeJSONBody(r, &req); err != nil {
			utils.APIJSONError(w, http.StatusBadRequest, "invalid_request", "Invalid JSON body.")
			return
		}
		if err := domain.ReorderProjectStatusesForUser(r.Context(), userID, projectID, req.StatusIDs); err != nil {
			writeWorkflowDomainError(w, err)
			return
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		json.NewEncoder(w).Encode(apiReorderOKResponse{OK: true})
		return
	}

	statusID, err := strconv.Atoi(rest[0])
	if err != nil || statusID <= 0 || len(rest) != 1 {
		utils.APIJSONError(w, http.StatusBadRequest, "invalid_request", "Invalid status id.")
		return
	}

	switch r.Method {
	case http.MethodPatch:
		var req apiStatusPatchRequest
		if err := decodeJSONBody(r, &req); err != nil {
			utils.APIJSONError(w, http.StatusBadRequest, "invalid_request", "Invalid JSON body.")
			return
		}
		s, err := domain.UpdateProjectStatusForUser(r.Context(), userID, projectID, statusID, domain.UpdateProjectStatusInput{
			Name:        req.Name,
			Description: req.Description,
			IsDone:      req.IsDone,
			IsDefault:   req.IsDefault,
		})
		if err != nil {
			writeWorkflowDomainError(w, err)
			return
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		json.NewEncoder(w).Encode(statusToAPIJSON(*s))
	case http.MethodDelete:
		var moveTo *int
		if r.ContentLength != 0 {
			var req apiStatusDeleteRequest
			if err := decodeJSONBody(r, &req); err == nil {
				moveTo = req.MoveToStatusID
			}
		}
		if raw := strings.TrimSpace(r.URL.Query().Get("move_to_status_id")); raw != "" {
			v, err := strconv.Atoi(raw)
			if err != nil || v <= 0 {
				utils.APIJSONError(w, http.StatusBadRequest, "invalid_request", "Invalid move_to_status_id.")
				return
			}
			moveTo = &v
		}
		if err := domain.DeleteProjectStatusForUser(r.Context(), userID, projectID, statusID, moveTo); err != nil {
			writeWorkflowDomainError(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	default:
		utils.APIJSONError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed.")
	}
}

func apiV1TaskClaim(w http.ResponseWriter, r *http.Request, taskID int) {
	userID, ok := apiUserFromRequest(r)
	if !ok {
		utils.APIJSONError(w, http.StatusUnauthorized, "unauthorized", "Not authenticated.")
		return
	}
	switch r.Method {
	case http.MethodPost:
		if err := domain.ClaimTaskForUser(r.Context(), userID, taskID); err != nil {
			writeWorkflowDomainError(w, err)
			return
		}
	case http.MethodDelete:
		if err := domain.UnclaimTaskForUser(r.Context(), userID, taskID); err != nil {
			writeWorkflowDomainError(w, err)
			return
		}
	default:
		utils.APIJSONError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed.")
		return
	}
	tz := GetUserTimezoneByID(userID)
	task, err := tasks.FetchTaskByIDForUser(taskID, userID, tz, 1)
	if err != nil {
		utils.APIJSONError(w, http.StatusInternalServerError, "internal_error", "Failed to load task.")
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(taskToAPIJSON(task))
}

func handleTaskTimeEntries(w http.ResponseWriter, r *http.Request, taskID int, rest []string) {
	userID, ok := apiUserFromRequest(r)
	if !ok {
		utils.APIJSONError(w, http.StatusUnauthorized, "unauthorized", "Not authenticated.")
		return
	}

	if len(rest) == 0 {
		switch r.Method {
		case http.MethodGet:
			entries, err := domain.ListTimeEntriesForUser(r.Context(), userID, taskID)
			if err != nil {
				writeWorkflowDomainError(w, err)
				return
			}
			out := make([]apiTimeEntryJSON, 0, len(entries))
			for _, e := range entries {
				out = append(out, timeEntryToAPIJSON(e))
			}
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			json.NewEncoder(w).Encode(out)
		case http.MethodPost:
			var req apiTimeEntryCreateRequest
			if err := decodeJSONBody(r, &req); err != nil {
				utils.APIJSONError(w, http.StatusBadRequest, "invalid_request", "Invalid JSON body.")
				return
			}
			e, err := domain.AddTimeEntryForUser(r.Context(), userID, taskID, req.Minutes, req.Note)
			if err != nil {
				writeWorkflowDomainError(w, err)
				return
			}
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			w.WriteHeader(http.StatusCreated)
			json.NewEncoder(w).Encode(timeEntryToAPIJSON(*e))
		default:
			utils.APIJSONError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed.")
		}
		return
	}

	entryID, err := strconv.Atoi(rest[0])
	if err != nil || entryID <= 0 || len(rest) != 1 {
		utils.APIJSONError(w, http.StatusBadRequest, "invalid_request", "Invalid time entry id.")
		return
	}
	if r.Method != http.MethodDelete {
		utils.APIJSONError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed.")
		return
	}
	if err := domain.DeleteTimeEntryForUser(r.Context(), userID, taskID, entryID); err != nil {
		writeWorkflowDomainError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

type apiProjectSprintJSON struct {
	ID          int     `json:"id"`
	ProjectID   int     `json:"project_id"`
	Name        string  `json:"name"`
	Description string  `json:"description"`
	StartDate   string  `json:"start_date"`
	EndDate     string  `json:"end_date"`
	LockDate    *string `json:"lock_date"`
	IsActive    bool    `json:"is_active"`
	IsLocked    bool    `json:"is_locked"`
	TaskCount   int     `json:"task_count"`
	CreatedAt   string  `json:"created_at"`
}

type apiSprintCreateRequest struct {
	Name        string  `json:"name"`
	Description string  `json:"description"`
	StartDate   string  `json:"start_date"`
	EndDate     string  `json:"end_date"`
	LockDate    *string `json:"lock_date"`
}

type apiSprintPatchRequest struct {
	Name        *string        `json:"name"`
	Description *string        `json:"description"`
	StartDate   *string        `json:"start_date"`
	EndDate     *string        `json:"end_date"`
	LockDate    optionalString `json:"lock_date"`
}

type apiSprintDeleteRequest struct {
	MoveToSprintID *int `json:"move_to_sprint_id"`
}

func sprintToAPIJSON(s storage.ProjectSprint) apiProjectSprintJSON {
	now := time.Now()
	var lockDate *string
	if s.LockDate != nil {
		d := storage.FormatSprintDate(*s.LockDate)
		lockDate = &d
	}
	return apiProjectSprintJSON{
		ID:          s.ID,
		ProjectID:   s.ProjectID,
		Name:        s.Name,
		Description: s.Description,
		StartDate:   storage.FormatSprintDate(s.StartDate),
		EndDate:     storage.FormatSprintDate(s.EndDate),
		LockDate:    lockDate,
		IsActive:    storage.SprintIsActive(s.StartDate, s.EndDate, now),
		IsLocked:    storage.SprintIsLocked(s.LockDate, now),
		TaskCount:   s.TaskCount,
		CreatedAt:   s.CreatedAt.UTC().Format(time.RFC3339),
	}
}

// handleProjectSprintsResource routes /projects/{id}/sprints...
func handleProjectSprintsResource(w http.ResponseWriter, r *http.Request, projectID int, rest []string) {
	userID, ok := apiUserFromRequest(r)
	if !ok {
		utils.APIJSONError(w, http.StatusUnauthorized, "unauthorized", "Not authenticated.")
		return
	}

	if len(rest) == 0 {
		switch r.Method {
		case http.MethodGet:
			sprints, err := domain.ListProjectSprintsForUser(r.Context(), userID, projectID)
			if err != nil {
				writeWorkflowDomainError(w, err)
				return
			}
			out := make([]apiProjectSprintJSON, 0, len(sprints))
			for _, s := range sprints {
				out = append(out, sprintToAPIJSON(s))
			}
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			json.NewEncoder(w).Encode(out)
		case http.MethodPost:
			var req apiSprintCreateRequest
			if err := decodeJSONBody(r, &req); err != nil {
				utils.APIJSONError(w, http.StatusBadRequest, "invalid_request", "Invalid JSON body.")
				return
			}
			lockDate := ""
			if req.LockDate != nil {
				lockDate = *req.LockDate
			}
			s, err := domain.CreateProjectSprintForUser(r.Context(), userID, projectID, domain.CreateProjectSprintInput{
				Name:        req.Name,
				Description: req.Description,
				StartDate:   req.StartDate,
				EndDate:     req.EndDate,
				LockDate:    lockDate,
			})
			if err != nil {
				writeWorkflowDomainError(w, err)
				return
			}
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			w.WriteHeader(http.StatusCreated)
			json.NewEncoder(w).Encode(sprintToAPIJSON(*s))
		default:
			utils.APIJSONError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed.")
		}
		return
	}

	if len(rest) == 1 && rest[0] == "backlog" {
		handleProjectBacklogSprint(w, r, userID, projectID)
		return
	}

	sprintID, err := strconv.Atoi(rest[0])
	if err != nil || sprintID <= 0 || len(rest) != 1 {
		utils.APIJSONError(w, http.StatusBadRequest, "invalid_request", "Invalid sprint id.")
		return
	}

	switch r.Method {
	case http.MethodPatch:
		var req apiSprintPatchRequest
		if err := decodeJSONBody(r, &req); err != nil {
			utils.APIJSONError(w, http.StatusBadRequest, "invalid_request", "Invalid JSON body.")
			return
		}
		s, err := domain.UpdateProjectSprintForUser(r.Context(), userID, projectID, sprintID, domain.UpdateProjectSprintInput{
			Name:        req.Name,
			Description: req.Description,
			StartDate:   req.StartDate,
			EndDate:     req.EndDate,
			LockDate:    req.LockDate.toPatchString(),
		})
		if err != nil {
			writeWorkflowDomainError(w, err)
			return
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		json.NewEncoder(w).Encode(sprintToAPIJSON(*s))
	case http.MethodDelete:
		var moveTo *int
		if r.ContentLength != 0 {
			var req apiSprintDeleteRequest
			if err := decodeJSONBody(r, &req); err == nil {
				moveTo = req.MoveToSprintID
			}
		}
		if raw := strings.TrimSpace(r.URL.Query().Get("move_to_sprint_id")); raw != "" {
			v, err := strconv.Atoi(raw)
			if err != nil || v < 0 {
				utils.APIJSONError(w, http.StatusBadRequest, "invalid_request", "Invalid move_to_sprint_id.")
				return
			}
			moveTo = &v
		}
		if err := domain.DeleteProjectSprintForUser(r.Context(), userID, projectID, sprintID, moveTo); err != nil {
			writeWorkflowDomainError(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	default:
		utils.APIJSONError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed.")
	}
}

type apiBacklogSprintJSON struct {
	ID        int    `json:"id"`
	ProjectID int    `json:"project_id"`
	Name      string `json:"name"`
	IsActive  bool   `json:"is_active"`
	IsLocked  bool   `json:"is_locked"`
	IsSystem  bool   `json:"is_system"`
	TaskCount int    `json:"task_count"`
}

func handleProjectBacklogSprint(w http.ResponseWriter, r *http.Request, userID, projectID int) {
	switch r.Method {
	case http.MethodGet:
		proj, err := storage.GetAccessibleProjectByID(projectID, userID)
		if err != nil {
			utils.APIJSONError(w, http.StatusNotFound, "not_found", "Project not found.")
			return
		}
		if proj.WorkflowMode != storage.WorkflowKanban {
			utils.APIJSONError(w, http.StatusBadRequest, "invalid_request", "Sprints require a kanban project.")
			return
		}
		count, _ := storage.CountTasksInBacklog(projectID)
		name := proj.BacklogName
		if name == "" {
			name = "Backlog"
		}
		out := apiBacklogSprintJSON{
			ID:        0,
			ProjectID: projectID,
			Name:      name,
			IsActive:  false,
			IsLocked:  false,
			IsSystem:  true,
			TaskCount: count,
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		json.NewEncoder(w).Encode(out)
	case http.MethodPatch:
		var req apiSprintPatchRequest
		if err := decodeJSONBody(r, &req); err != nil {
			utils.APIJSONError(w, http.StatusBadRequest, "invalid_request", "Invalid JSON body.")
			return
		}
		if req.StartDate != nil || req.EndDate != nil || req.LockDate.Set {
			utils.APIJSONError(w, http.StatusBadRequest, "invalid_request", "Dates cannot be set on the backlog system sprint.")
			return
		}
		if req.Name == nil {
			utils.APIJSONError(w, http.StatusBadRequest, "invalid_request", "Nothing to update.")
			return
		}
		proj, err := storage.GetAccessibleProjectByID(projectID, userID)
		if err != nil {
			utils.APIJSONError(w, http.StatusNotFound, "not_found", "Project not found.")
			return
		}
		if proj.WorkflowMode != storage.WorkflowKanban {
			utils.APIJSONError(w, http.StatusBadRequest, "invalid_request", "Sprints require a kanban project.")
			return
		}
		updatedProj, err := domain.UpdateProject(r.Context(), userID, projectID, nil, nil, nil, req.Name)
		if err != nil {
			writeWorkflowDomainError(w, err)
			return
		}
		count, _ := storage.CountTasksInBacklog(projectID)
		out := apiBacklogSprintJSON{
			ID:        0,
			ProjectID: projectID,
			Name:      updatedProj.BacklogName,
			IsActive:  false,
			IsLocked:  false,
			IsSystem:  true,
			TaskCount: count,
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		json.NewEncoder(w).Encode(out)
	case http.MethodDelete:
		utils.APIJSONError(w, http.StatusBadRequest, "invalid_request", "The backlog system sprint cannot be deleted.")
	default:
		utils.APIJSONError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed.")
	}
}
