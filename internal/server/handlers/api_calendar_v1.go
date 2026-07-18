package handlers

import (
	"encoding/json"
	"net/http"

	"GoTodo/internal/server/utils"
	"GoTodo/internal/storage"
)

// APIV1CalendarRouter handles calendar feed token endpoints.
func APIV1CalendarRouter(w http.ResponseWriter, r *http.Request) {
	sub := utils.ParseAPIV1Subpath(r, "calendar")
	switch {
	case sub == "" && r.Method == http.MethodGet:
		apiV1GetCalendar(w, r)
	case sub == "regenerate" && r.Method == http.MethodPost:
		apiV1RegenerateCalendar(w, r)
	case sub == "sync" && r.Method == http.MethodPost:
		apiV1CalendarSync(w, r)
	default:
		utils.APIJSONError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed.")
	}
}

func apiV1GetCalendar(w http.ResponseWriter, r *http.Request) {
	userID, ok := utils.GetAPIUserID(r)
	if !ok {
		utils.APIJSONError(w, http.StatusUnauthorized, "unauthorized", "Not authenticated.")
		return
	}
	token, err := storage.GetOrCreateCalendarToken(userID)
	if err != nil {
		utils.APIJSONError(w, http.StatusInternalServerError, "internal_error", "Failed to load calendar token.")
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(map[string]string{
		"token":    token,
		"feed_url": calendarFeedURLForRequest(r, token),
	})
}

func apiV1RegenerateCalendar(w http.ResponseWriter, r *http.Request) {
	userID, ok := utils.GetAPIUserID(r)
	if !ok {
		utils.APIJSONError(w, http.StatusUnauthorized, "unauthorized", "Not authenticated.")
		return
	}
	token, err := storage.RegenerateCalendarToken(userID)
	if err != nil {
		utils.APIJSONError(w, http.StatusInternalServerError, "internal_error", "Failed to regenerate calendar token.")
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(map[string]string{
		"token":    token,
		"feed_url": calendarFeedURLForRequest(r, token),
	})
}
