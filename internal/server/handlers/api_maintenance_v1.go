package handlers

import (
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"GoTodo/internal/server/utils"
	"GoTodo/internal/storage"
)

// MaxMaintenanceMessageLen caps the banner text.
const MaxMaintenanceMessageLen = 500

func maintenanceJSON(m storage.Maintenance) map[string]interface{} {
	out := map[string]interface{}{"enabled": m.Enabled, "message": m.Message, "started_at": nil}
	if m.StartedAt != nil {
		out["started_at"] = m.StartedAt.UTC().Format(time.RFC3339)
	}
	return out
}

// APIV1AdminMaintenance handles GET/PUT /api/v2/admin/maintenance (admins only).
func APIV1AdminMaintenance(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		writeJSON(w, http.StatusOK, maintenanceJSON(storage.GetMaintenance()))
	case http.MethodPut:
		var req struct {
			Enabled bool   `json:"enabled"`
			Message string `json:"message"`
		}
		if err := decodeJSONBody(r, &req); err != nil {
			utils.APIJSONError(w, http.StatusBadRequest, "invalid_request", "Invalid JSON body.")
			return
		}
		msg := strings.TrimSpace(req.Message)
		if utf8.RuneCountInString(msg) > MaxMaintenanceMessageLen {
			utils.APIJSONError(w, http.StatusBadRequest, "invalid_request", "The message is too long (500 characters at most).")
			return
		}
		actorID, _ := apiUserFromRequest(r)
		m, err := storage.SetMaintenance(req.Enabled, msg, actorID)
		if err != nil {
			utils.APIJSONError(w, http.StatusInternalServerError, "internal_error", "Failed to save.")
			return
		}
		event := "maintenance_ended"
		if req.Enabled {
			event = "maintenance_started"
		}
		logAdminEvent(r, event, "site", 1, "Maintenance mode", map[string]interface{}{"message": msg})
		writeJSON(w, http.StatusOK, maintenanceJSON(m))
	default:
		utils.APIJSONError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed.")
	}
}
