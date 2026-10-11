package handlers

import (
	"encoding/json"
	"errors"
	"net/http"

	"GoTodo/internal/domain"
	"GoTodo/internal/server/utils"
)

type apiNotificationPreferenceJSON struct {
	Type           string `json:"type"`
	Label          string `json:"label"`
	Description    string `json:"description"`
	Enabled        bool   `json:"enabled"`
	Email          bool   `json:"email"`
	EmailAvailable bool   `json:"email_available"`
}

type apiNotificationEmailJSON struct {
	Available      bool   `json:"available"`
	Mode           string `json:"mode"`
	DigestHour     int    `json:"digest_hour"`
	ReminderTiming string `json:"reminder_timing"`
}

type apiNotificationPreferencesPatchRequest struct {
	Preferences    map[string]bool `json:"preferences"`
	EmailTypes     map[string]bool `json:"email_types"`
	EmailMode      *string         `json:"email_mode"`
	DigestHour     *int            `json:"digest_hour"`
	ReminderTiming *string         `json:"reminder_timing"`
}

// APIV1MeNotificationPreferences handles GET/PATCH /api/v2/me/notification-preferences.
func APIV1MeNotificationPreferences(w http.ResponseWriter, r *http.Request) {
	userID, ok := utils.GetAPIUserID(r)
	if !ok {
		utils.APIJSONError(w, http.StatusUnauthorized, "unauthorized", "Not authenticated.")
		return
	}
	var err error
	switch r.Method {
	case http.MethodGet:
	case http.MethodPatch:
		var req apiNotificationPreferencesPatchRequest
		if decodeErr := decodeJSONBody(r, &req); decodeErr != nil {
			utils.APIJSONError(w, http.StatusBadRequest, "invalid_request", "Invalid JSON body.")
			return
		}
		err = domain.UpdateNotificationSettings(r.Context(), userID, domain.NotificationSettingsUpdate{
			InApp:          req.Preferences,
			Email:          req.EmailTypes,
			Mode:           req.EmailMode,
			DigestHour:     req.DigestHour,
			ReminderTiming: req.ReminderTiming,
		})
	default:
		utils.APIJSONError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed.")
		return
	}
	var (
		prefs    []domain.NotificationPreference
		settings domain.NotificationEmailSettings
	)
	if err == nil {
		prefs, err = domain.ListNotificationPreferences(r.Context(), userID)
	}
	if err == nil {
		settings, err = domain.GetNotificationEmailSettings(r.Context(), userID)
	}
	if err != nil {
		if errors.Is(err, domain.ErrValidation) {
			utils.APIJSONError(w, http.StatusBadRequest, "invalid_request", err.Error())
			return
		}
		utils.APIJSONError(w, http.StatusInternalServerError, "internal_error", "Failed to load notification preferences.")
		return
	}

	out := make([]apiNotificationPreferenceJSON, 0, len(prefs))
	for _, p := range prefs {
		out = append(out, apiNotificationPreferenceJSON{
			Type: p.Type, Label: p.Label, Description: p.Description,
			Enabled: p.Enabled, Email: p.Email, EmailAvailable: !p.NoEmail,
		})
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"preferences": out,
		"email": apiNotificationEmailJSON{
			Available:      settings.Available,
			Mode:           settings.Mode,
			DigestHour:     settings.DigestHour,
			ReminderTiming: settings.ReminderTiming,
		},
	})
}
