package handlers

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"GoTodo/internal/server/utils"
	"GoTodo/internal/storage"
)

// APIV1AdminInvitesRouter handles /api/v2/admin/invites and /api/v2/admin/invites/{id}.
// Protected by AdminAPIChain.
func APIV1AdminInvitesRouter(w http.ResponseWriter, r *http.Request) {
	userID, ok := utils.GetAPIUserID(r)
	if !ok {
		utils.APIJSONError(w, http.StatusUnauthorized, "unauthorized", "Not authenticated.")
		return
	}

	sub := utils.ParseAPIV1Subpath(r, "admin/invites")
	if sub == "" {
		switch r.Method {
		case http.MethodGet:
			invites, err := storage.ListAllInvites()
			if err != nil {
				utils.APIJSONError(w, http.StatusInternalServerError, "internal_error", "Failed to list invites.")
				return
			}
			out := make([]map[string]interface{}, 0, len(invites))
			for _, inv := range invites {
				out = append(out, inviteToJSON(inv))
			}
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			_ = json.NewEncoder(w).Encode(out)
		case http.MethodPost:
			var req inviteCreateRequest
			if err := decodeJSONBody(r, &req); err != nil {
				utils.APIJSONError(w, http.StatusBadRequest, "invalid_request", "Invalid JSON body.")
				return
			}

			settings, err := storage.GetSiteSettings()
			if err != nil || settings == nil {
				utils.APIJSONError(w, http.StatusInternalServerError, "internal_error", "Failed to load site settings.")
				return
			}

			var expiresAt *time.Time
			if req.BypassExpiration {
				expiresAt = nil
			} else if req.ExpiresAt != nil && *req.ExpiresAt != "" {
				parsed, err := parseInviteExpiration(*req.ExpiresAt)
				if err != nil {
					utils.APIJSONError(w, http.StatusBadRequest, "invalid_expiration", "Invalid expiration date.")
					return
				}
				if parsed != nil && time.Now().After(*parsed) {
					utils.APIJSONError(w, http.StatusBadRequest, "invalid_expiration", "Expiration date must be in the future.")
					return
				}
				expiresAt = parsed
			} else if settings.InviteExpirationDays > 0 {
				t := time.Now().AddDate(0, 0, settings.InviteExpirationDays)
				expiresAt = &t
			}

			inv, err := storage.CreateInvite(req.Email, &userID, expiresAt, false)
			if err != nil {
				utils.APIJSONError(w, http.StatusBadRequest, "invalid_request", err.Error())
				return
			}
			emailSiteInvite(r, inv.Email, inv.Token)
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(inviteToJSON(*inv))
		default:
			utils.APIJSONError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed.")
		}
		return
	}

	id, err := strconv.Atoi(strings.Trim(sub, "/"))
	if err != nil || id <= 0 {
		utils.APIJSONError(w, http.StatusBadRequest, "invalid_request", "Invalid invite id.")
		return
	}
	if r.Method != http.MethodDelete {
		utils.APIJSONError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed.")
		return
	}
	if err := storage.DeleteInvite(id, userID, true); err != nil {
		utils.APIJSONError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
