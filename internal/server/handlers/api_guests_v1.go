package handlers

import (
	"encoding/json"
	"net/http"

	"GoTodo/internal/domain"
	"GoTodo/internal/server/utils"
	"GoTodo/internal/storage"
)

type apiGuestPolicyJSON struct {
	Enabled     bool   `json:"enabled"`
	AccessLevel string `json:"access_level"`
	RoleID      *int   `json:"role_id"`
	ViaTeams    bool   `json:"via_teams"`
	Directory   bool   `json:"directory"`
	GuestCount  int    `json:"guest_count"`
}

func guestPolicyToJSON(p storage.GuestPolicy, count int) apiGuestPolicyJSON {
	out := apiGuestPolicyJSON{
		Enabled: p.Enabled, AccessLevel: p.AccessLevel, ViaTeams: p.ViaTeams, Directory: p.Directory, GuestCount: count,
	}
	if p.RoleID > 0 {
		id := p.RoleID
		out.RoleID = &id
	}
	return out
}

// APIV1AdminGuestPolicy handles GET/PUT /api/v2/admin/guest-policy (site admins, company mode).
func APIV1AdminGuestPolicy(w http.ResponseWriter, r *http.Request) {
	actorID, ok := apiUserFromRequest(r)
	if !ok {
		utils.APIJSONError(w, http.StatusUnauthorized, "unauthorized", "Not authenticated.")
		return
	}
	switch r.Method {
	case http.MethodGet:
	case http.MethodPut:
		var req struct {
			Enabled     bool   `json:"enabled"`
			AccessLevel string `json:"access_level"`
			RoleID      *int   `json:"role_id"`
			ViaTeams    bool   `json:"via_teams"`
			Directory   bool   `json:"directory"`
		}
		if err := decodeJSONBody(r, &req); err != nil {
			utils.APIJSONError(w, http.StatusBadRequest, "invalid_request", "Invalid JSON body.")
			return
		}
		in := domain.GuestPolicyInput{
			Enabled: req.Enabled, AccessLevel: req.AccessLevel, ViaTeams: req.ViaTeams, Directory: req.Directory,
		}
		if req.RoleID != nil {
			in.RoleID = *req.RoleID
		}
		if _, err := domain.SaveGuestPolicyForAdmin(r.Context(), actorID, in); err != nil {
			writePeopleDomainError(w, err)
			return
		}
	default:
		utils.APIJSONError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed.")
		return
	}
	p, count, err := domain.GetGuestPolicyForAdmin(r.Context(), actorID)
	if err != nil {
		writePeopleDomainError(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(guestPolicyToJSON(p, count))
}
