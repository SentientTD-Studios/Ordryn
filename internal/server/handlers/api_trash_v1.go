package handlers

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"GoTodo/internal/domain"
	"GoTodo/internal/server/utils"
)

type apiTrashEntryJSON struct {
	ID           int    `json:"id"`
	Title        string `json:"title"`
	ProjectID    *int   `json:"project_id"`
	ProjectName  string `json:"project_name"`
	ParentID     *int   `json:"parent_id"`
	ParentTitle  string `json:"parent_title"`
	SubtaskCount int    `json:"subtask_count"`
	DeletedAt    string `json:"deleted_at"`
	DeletedBy    string `json:"deleted_by"`
	PurgeAt      string `json:"purge_at"`
}

func optionalID(id int) *int {
	if id <= 0 {
		return nil
	}
	return &id
}

// APIV1TrashRouter handles /api/v2/trash (GET list, DELETE empty),
// /api/v2/trash/{id} (DELETE forever), and /api/v2/trash/{id}/restore (POST).
func APIV1TrashRouter(w http.ResponseWriter, r *http.Request) {
	userID, ok := apiUserFromRequest(r)
	if !ok {
		utils.APIJSONError(w, http.StatusUnauthorized, "unauthorized", "Not authenticated.")
		return
	}
	sub := strings.Trim(utils.ParseAPIV1Subpath(r, "trash"), "/")
	if sub == "" {
		switch r.Method {
		case http.MethodGet:
			apiV1ListTrash(w, r, userID)
		case http.MethodDelete:
			n, err := domain.EmptyTrashForUser(r.Context(), userID)
			if err != nil {
				utils.APIJSONError(w, http.StatusInternalServerError, "internal_error", "Failed to empty the trash.")
				return
			}
			writeJSON(w, http.StatusOK, map[string]interface{}{"ok": true, "deleted": n})
		default:
			utils.APIJSONError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed.")
		}
		return
	}

	parts := strings.Split(sub, "/")
	taskID, err := strconv.Atoi(parts[0])
	if err != nil || taskID <= 0 || len(parts) > 2 {
		utils.APIJSONError(w, http.StatusNotFound, "not_found", "Not found.")
		return
	}
	switch {
	case len(parts) == 2 && parts[1] == "restore" && r.Method == http.MethodPost:
		restored, err := domain.RestoreTrashedTaskForUser(r.Context(), userID, taskID)
		if err != nil {
			writeTrashError(w, err, "Failed to restore the task.")
			return
		}
		writeJSON(w, http.StatusOK, map[string]interface{}{"ok": true, "restored": restored})
	case len(parts) == 1 && r.Method == http.MethodDelete:
		if err := domain.PurgeTrashedTaskForUser(r.Context(), userID, taskID); err != nil {
			writeTrashError(w, err, "Failed to delete the task.")
			return
		}
		writeJSON(w, http.StatusOK, map[string]interface{}{"ok": true})
	default:
		utils.APIJSONError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed.")
	}
}

func apiV1ListTrash(w http.ResponseWriter, r *http.Request, userID int) {
	entries, err := domain.ListTrashForUser(r.Context(), userID)
	if err != nil {
		utils.APIJSONError(w, http.StatusInternalServerError, "internal_error", "Failed to load the trash.")
		return
	}
	out := make([]apiTrashEntryJSON, 0, len(entries))
	for _, e := range entries {
		out = append(out, apiTrashEntryJSON{
			ID:           e.ID,
			Title:        e.Title,
			ProjectID:    optionalID(e.ProjectID),
			ProjectName:  e.ProjectName,
			ParentID:     optionalID(e.ParentID),
			ParentTitle:  e.ParentTitle,
			SubtaskCount: e.SubtaskCount,
			DeletedAt:    e.DeletedAt.UTC().Format(time.RFC3339),
			DeletedBy:    e.DeletedBy,
			PurgeAt:      e.PurgeAt.UTC().Format(time.RFC3339),
		})
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"tasks": out})
}

func writeTrashError(w http.ResponseWriter, err error, fallback string) {
	switch {
	case errors.Is(err, domain.ErrNotFound):
		utils.APIJSONError(w, http.StatusNotFound, "not_found", "Task not found in the trash.")
	case errors.Is(err, domain.ErrForbidden):
		utils.APIJSONError(w, http.StatusForbidden, "forbidden", "You can't change this task.")
	case errors.Is(err, domain.ErrConflict):
		utils.APIJSONError(w, http.StatusConflict, "conflict", "Restore its parent task first.")
	default:
		utils.APIJSONError(w, http.StatusInternalServerError, "internal_error", fallback)
	}
}
