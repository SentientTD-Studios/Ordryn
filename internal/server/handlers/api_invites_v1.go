package handlers

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"GoTodo/internal/server/utils"
	"GoTodo/internal/storage"
)

type inviteCreateRequest struct {
	Email string `json:"email"`
}

// APIV1InvitesRouter handles /api/v1/invites and /api/v1/invites/{id}.
func APIV1InvitesRouter(w http.ResponseWriter, r *http.Request) {
	sub := utils.ParseAPIV1Subpath(r, "invites")
	if sub == "" {
		switch r.Method {
		case http.MethodGet:
			invites, err := storage.ListInvites()
			if err != nil {
				utils.APIJSONError(w, http.StatusInternalServerError, "internal_error", "Failed to list invites.")
				return
			}
			out := make([]map[string]interface{}, 0, len(invites))
			for _, inv := range invites {
				out = append(out, map[string]interface{}{
					"id":    inv.ID,
					"email": inv.Email,
					"token": inv.Token,
					"used":  inv.Used,
				})
			}
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			_ = json.NewEncoder(w).Encode(out)
		case http.MethodPost:
			var req inviteCreateRequest
			if err := decodeJSONBody(r, &req); err != nil {
				utils.APIJSONError(w, http.StatusBadRequest, "invalid_request", "Invalid JSON body.")
				return
			}
			inv, err := storage.CreateInvite(req.Email)
			if err != nil {
				utils.APIJSONError(w, http.StatusBadRequest, "invalid_request", err.Error())
				return
			}
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"id":    inv.ID,
				"email": inv.Email,
				"token": inv.Token,
				"used":  inv.Used,
			})
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
	if err := storage.DeleteInvite(id); err != nil {
		utils.APIJSONError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
