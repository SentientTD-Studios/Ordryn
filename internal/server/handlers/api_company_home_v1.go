package handlers

import (
	"encoding/json"
	"net/http"

	"GoTodo/internal/server/utils"
	"GoTodo/internal/storage"
)

// APIV1CompanyHome handles GET /api/v2/company-home: the Markdown signed-in people see
// in company mode when no project is shared with them yet. Edited in site settings.
func APIV1CompanyHome(w http.ResponseWriter, r *http.Request) {
	userID, ok := apiUserFromRequest(r)
	if !ok {
		utils.APIJSONError(w, http.StatusUnauthorized, "unauthorized", "Not authenticated.")
		return
	}
	if r.Method != http.MethodGet {
		utils.APIJSONError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed.")
		return
	}
	if !storage.IsCompanyMode() {
		utils.APIJSONError(w, http.StatusNotFound, "not_found", "Only used in company mode.")
		return
	}
	content, err := storage.GetCompanyHome()
	if err != nil {
		utils.APIJSONError(w, http.StatusInternalServerError, "internal_error", "Failed to load the home page.")
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"content":  content,
		"can_edit": storage.UserHasSitePermission(userID, storage.SitePermSettingsManage),
	})
}
