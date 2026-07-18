package handlers

import (
	"encoding/json"
	"net/http"

	"GoTodo/internal/server/utils"
	"GoTodo/internal/tasks"
)

// APIV1Dashboard returns aggregate task stats for the current user.
func APIV1Dashboard(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		utils.APIJSONError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed.")
		return
	}
	userID, ok := utils.GetAPIUserID(r)
	if !ok {
		utils.APIJSONError(w, http.StatusUnauthorized, "unauthorized", "Not authenticated.")
		return
	}
	tz := GetUserTimezoneByID(userID)
	stats, err := tasks.GetDashboardStats(userID, tz)
	if err != nil {
		utils.APIJSONError(w, http.StatusInternalServerError, "internal_error", "Failed to load dashboard.")
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(stats)
}
