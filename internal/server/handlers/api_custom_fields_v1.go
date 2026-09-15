package handlers

import (
	"encoding/json"
	"net/http"

	"GoTodo/internal/domain"
	"GoTodo/internal/server/utils"
	"GoTodo/internal/storage"
)

type customFieldsListJSON struct {
	Fields []storage.CustomFieldDef `json:"fields"`
}

func apiV1ProjectCustomFields(w http.ResponseWriter, r *http.Request, projectID int) {
	userID, ok := apiUserFromRequest(r)
	if !ok {
		utils.APIJSONError(w, http.StatusUnauthorized, "unauthorized", "Not authenticated.")
		return
	}
	if r.Method != http.MethodGet {
		utils.APIJSONError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed.")
		return
	}
	if _, err := storage.GetAccessibleProjectByID(projectID, userID); err != nil {
		utils.APIJSONError(w, http.StatusNotFound, "not_found", "Project not found.")
		return
	}
	defs, err := domain.ApplicableFieldDefsForProject(projectID)
	if err != nil {
		utils.APIJSONError(w, http.StatusInternalServerError, "internal_error", "Failed to load custom fields.")
		return
	}
	if defs == nil {
		defs = []storage.CustomFieldDef{}
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(customFieldsListJSON{Fields: defs})
}
