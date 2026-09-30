package handlers

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"GoTodo/internal/domain"
	"GoTodo/internal/server/utils"
)

type apiWatcherJSON struct {
	UserID int    `json:"user_id"`
	Name   string `json:"name"`
}

type apiTaskWatchJSON struct {
	Watching   bool             `json:"watching"`
	ViaProject bool             `json:"via_project"`
	Watchers   []apiWatcherJSON `json:"watchers"`
}

type apiProjectWatchJSON struct {
	Watching bool `json:"watching"`
}

type apiTaskLinkJSON struct {
	LinkID    int    `json:"link_id"`
	Type      string `json:"type"`
	TaskID    int    `json:"task_id"`
	Title     string `json:"title"`
	Completed bool   `json:"completed"`
	ProjectID *int   `json:"project_id"`
}

type apiTaskLinkCreateRequest struct {
	TaskID int    `json:"task_id"`
	Type   string `json:"type"`
}

func writeWatchLinkError(w http.ResponseWriter, err error, fallback string) {
	switch {
	case errors.Is(err, domain.ErrNotFound):
		utils.APIJSONError(w, http.StatusNotFound, "not_found", "Not found.")
	case errors.Is(err, domain.ErrValidation):
		utils.APIJSONError(w, http.StatusBadRequest, "invalid_request", err.Error())
	case errors.Is(err, domain.ErrForbidden):
		utils.APIJSONError(w, http.StatusForbidden, "forbidden", sharingClientMessage(err, "Forbidden."))
	case errors.Is(err, domain.ErrConflict):
		utils.APIJSONError(w, http.StatusConflict, "conflict", err.Error())
	default:
		utils.APIJSONError(w, http.StatusInternalServerError, "internal_error", fallback)
	}
}

func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// apiV1TaskWatch handles GET/POST/DELETE /api/v2/tasks/{id}/watch for the caller.
func apiV1TaskWatch(w http.ResponseWriter, r *http.Request, taskID int) {
	userID, ok := apiUserFromRequest(r)
	if !ok {
		utils.APIJSONError(w, http.StatusUnauthorized, "unauthorized", "Not authenticated.")
		return
	}
	var (
		state *domain.TaskWatchState
		err   error
	)
	switch r.Method {
	case http.MethodGet:
		state, err = domain.GetTaskWatchState(r.Context(), userID, taskID)
	case http.MethodPost:
		state, err = domain.SetTaskWatching(r.Context(), userID, taskID, true)
	case http.MethodDelete:
		state, err = domain.SetTaskWatching(r.Context(), userID, taskID, false)
	default:
		utils.APIJSONError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed.")
		return
	}
	if err != nil {
		writeWatchLinkError(w, err, "Failed to update watch.")
		return
	}
	out := apiTaskWatchJSON{Watching: state.Watching, ViaProject: state.ViaProject, Watchers: make([]apiWatcherJSON, 0, len(state.Watchers))}
	for _, wt := range state.Watchers {
		out.Watchers = append(out.Watchers, apiWatcherJSON{UserID: wt.UserID, Name: wt.Name})
	}
	writeJSON(w, http.StatusOK, out)
}

// apiV1ProjectWatch handles GET/POST/DELETE /api/v2/projects/{id}/watch for the caller.
func apiV1ProjectWatch(w http.ResponseWriter, r *http.Request, projectID int) {
	userID, ok := apiUserFromRequest(r)
	if !ok {
		utils.APIJSONError(w, http.StatusUnauthorized, "unauthorized", "Not authenticated.")
		return
	}
	var (
		watching bool
		err      error
	)
	switch r.Method {
	case http.MethodGet:
		watching, err = domain.IsWatchingProject(r.Context(), userID, projectID)
	case http.MethodPost:
		watching, err = domain.SetProjectWatching(r.Context(), userID, projectID, true)
	case http.MethodDelete:
		watching, err = domain.SetProjectWatching(r.Context(), userID, projectID, false)
	default:
		utils.APIJSONError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed.")
		return
	}
	if err != nil {
		writeWatchLinkError(w, err, "Failed to update project watch.")
		return
	}
	writeJSON(w, http.StatusOK, apiProjectWatchJSON{Watching: watching})
}

func taskLinkToAPIJSON(v domain.TaskLinkView) apiTaskLinkJSON {
	out := apiTaskLinkJSON{LinkID: v.LinkID, Type: v.Kind, TaskID: v.TaskID, Title: v.Title, Completed: v.Completed}
	if v.ProjectID > 0 {
		pid := v.ProjectID
		out.ProjectID = &pid
	}
	return out
}

// apiV1TaskLinks handles GET/POST /api/v2/tasks/{id}/links and DELETE /api/v2/tasks/{id}/links/{linkId}.
func apiV1TaskLinks(w http.ResponseWriter, r *http.Request, taskID int, rest []string) {
	userID, ok := apiUserFromRequest(r)
	if !ok {
		utils.APIJSONError(w, http.StatusUnauthorized, "unauthorized", "Not authenticated.")
		return
	}
	if len(rest) == 1 && rest[0] != "" {
		linkID, err := strconv.Atoi(rest[0])
		if err != nil || linkID <= 0 {
			utils.APIJSONError(w, http.StatusBadRequest, "invalid_request", "Invalid link id.")
			return
		}
		if r.Method != http.MethodDelete {
			utils.APIJSONError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed.")
			return
		}
		if err := domain.RemoveTaskLink(r.Context(), userID, taskID, linkID); err != nil {
			writeWatchLinkError(w, err, "Failed to remove link.")
			return
		}
		writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
		return
	}
	if len(rest) > 1 || (len(rest) == 1 && rest[0] != "") {
		utils.APIJSONError(w, http.StatusBadRequest, "invalid_request", "Invalid task path.")
		return
	}
	switch r.Method {
	case http.MethodGet:
		links, err := domain.ListTaskLinks(r.Context(), userID, taskID)
		if err != nil {
			writeWatchLinkError(w, err, "Failed to load links.")
			return
		}
		out := make([]apiTaskLinkJSON, 0, len(links))
		for _, l := range links {
			out = append(out, taskLinkToAPIJSON(l))
		}
		writeJSON(w, http.StatusOK, out)
	case http.MethodPost:
		var req apiTaskLinkCreateRequest
		if err := decodeJSONBody(r, &req); err != nil {
			utils.APIJSONError(w, http.StatusBadRequest, "invalid_request", "Invalid JSON body.")
			return
		}
		link, err := domain.AddTaskLink(r.Context(), userID, taskID, req.TaskID, req.Type)
		if err != nil {
			writeWatchLinkError(w, err, "Failed to add link.")
			return
		}
		writeJSON(w, http.StatusCreated, taskLinkToAPIJSON(*link))
	default:
		utils.APIJSONError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed.")
	}
}
