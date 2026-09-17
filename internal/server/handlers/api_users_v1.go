package handlers

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"unicode/utf8"

	"GoTodo/internal/domain"
	"GoTodo/internal/server/utils"
)

type apiUserSearchHitJSON struct {
	UserName string `json:"user_name"`
}

// APIV1UsersSearch handles GET /api/v2/users/search?q=&project_id=
func APIV1UsersSearch(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		utils.APIJSONError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed.")
		return
	}
	userID, ok := utils.GetAPIUserID(r)
	if !ok {
		utils.APIJSONError(w, http.StatusUnauthorized, "unauthorized", "Not authenticated.")
		return
	}

	projectID := 0
	if raw := strings.TrimSpace(r.URL.Query().Get("project_id")); raw != "" {
		id, err := strconv.Atoi(raw)
		if err != nil || id <= 0 {
			utils.APIJSONError(w, http.StatusBadRequest, "invalid_request", "Invalid project id.")
			return
		}
		projectID = id
	}

	q := strings.TrimSpace(r.URL.Query().Get("q"))
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	if utf8.RuneCountInString(q) < 2 {
		_ = json.NewEncoder(w).Encode([]apiUserSearchHitJSON{})
		return
	}

	names, err := domain.SearchUsernames(r.Context(), userID, q, projectID)
	if err != nil {
		writeWorkflowDomainError(w, err)
		return
	}
	out := make([]apiUserSearchHitJSON, 0, len(names))
	for _, name := range names {
		out = append(out, apiUserSearchHitJSON{UserName: name})
	}
	_ = json.NewEncoder(w).Encode(out)
}
