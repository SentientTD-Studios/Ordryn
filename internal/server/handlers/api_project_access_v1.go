package handlers

import (
	"encoding/json"
	"net/http"
	"strconv"

	"GoTodo/internal/domain"
	"GoTodo/internal/server/utils"
	"GoTodo/internal/storage"
)

type apiProjectShareJSON struct {
	ID     int    `json:"id"`
	Kind   string `json:"kind"`
	TeamID *int   `json:"team_id,omitempty"`
	UserID *int   `json:"user_id,omitempty"`
	Name   string `json:"name"`
	Email  string `json:"email,omitempty"`
	// UserName is the person's username, for changing their share; Name may be their full name.
	UserName string `json:"user_name,omitempty"`
	Level    string `json:"level"`
	// RoleID is the role a full share gives; omitted means each person's own site role.
	RoleID    *int   `json:"role_id,omitempty"`
	RoleLabel string `json:"role_label,omitempty"`
	Company   bool   `json:"is_company,omitempty"`
	Members   int    `json:"member_count,omitempty"`
	SharedAt  string `json:"created_at"`
}

type apiProjectShareRequest struct {
	TeamID   int    `json:"team_id"`
	Username string `json:"username"`
	Level    string `json:"level"`
	RoleID   int    `json:"role_id"`
}

// apiV1ProjectAccess handles /api/v2/projects/{id}/access in company mode: who the
// project is shared with (GET), share it or change a share's level (POST), and stop
// sharing (DELETE /access/{shareId}).
func apiV1ProjectAccess(w http.ResponseWriter, r *http.Request, projectID int, rest []string) {
	userID, ok := apiUserFromRequest(r)
	if !ok {
		utils.APIJSONError(w, http.StatusUnauthorized, "unauthorized", "Not authenticated.")
		return
	}
	if len(rest) == 1 {
		if r.Method != http.MethodDelete {
			utils.APIJSONError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed.")
			return
		}
		grantID, err := strconv.Atoi(rest[0])
		if err != nil || grantID <= 0 {
			utils.APIJSONError(w, http.StatusBadRequest, "invalid_request", "Invalid share id.")
			return
		}
		if err := domain.UnshareProjectForUser(r.Context(), userID, projectID, grantID); err != nil {
			writePeopleDomainError(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if len(rest) != 0 {
		utils.APIJSONError(w, http.StatusNotFound, "not_found", "Not found.")
		return
	}
	switch r.Method {
	case http.MethodGet:
	case http.MethodPost:
		var req apiProjectShareRequest
		if err := decodeJSONBody(r, &req); err != nil {
			utils.APIJSONError(w, http.StatusBadRequest, "invalid_request", "Invalid JSON body.")
			return
		}
		if err := domain.ShareProjectForUser(r.Context(), userID, projectID, domain.ProjectShareInput{
			TeamID: req.TeamID, Username: req.Username, Level: req.Level, RoleID: req.RoleID,
		}); err != nil {
			writePeopleDomainError(w, err)
			return
		}
	default:
		utils.APIJSONError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed.")
		return
	}
	grants, canManage, err := domain.ListProjectSharesForUser(r.Context(), userID, projectID)
	if err != nil {
		writePeopleDomainError(w, err)
		return
	}
	type roleJSON struct {
		ID    int    `json:"id"`
		Label string `json:"label"`
	}
	type teamJSON struct {
		ID        int    `json:"id"`
		Name      string `json:"name"`
		IsCompany bool   `json:"is_company"`
	}
	out := struct {
		Shares    []apiProjectShareJSON `json:"shares"`
		CanManage bool                  `json:"can_manage"`
		// Teams lists every team the project can be shared with, for those who can share it.
		Teams []teamJSON `json:"teams"`
		// Roles lists the roles a full share can give, for those who can share it.
		Roles []roleJSON `json:"roles"`
	}{Shares: make([]apiProjectShareJSON, 0, len(grants)), CanManage: canManage, Teams: []teamJSON{}, Roles: []roleJSON{}}
	if canManage {
		if id, err := storage.CompanyOrganizationID(); err == nil && id > 0 {
			if org, err := storage.GetOrganizationByID(id); err == nil {
				out.Teams = append(out.Teams, teamJSON{ID: org.ID, Name: org.Name, IsCompany: true})
			}
		}
		teams, err := storage.ListTeams()
		if err != nil {
			utils.APIJSONError(w, http.StatusInternalServerError, "internal_error", "Failed to load teams.")
			return
		}
		for _, t := range teams {
			out.Teams = append(out.Teams, teamJSON{ID: t.ID, Name: t.Name})
		}
		roles, err := storage.ListSiteRoles()
		if err != nil {
			utils.APIJSONError(w, http.StatusInternalServerError, "internal_error", "Failed to load roles.")
			return
		}
		for _, role := range roles {
			if !storage.RoleGrantsAdmin(role.Permissions) {
				out.Roles = append(out.Roles, roleJSON{ID: role.ID, Label: role.Label()})
			}
		}
	}
	for _, g := range grants {
		kind := "person"
		if g.OrganizationID != nil {
			kind = "team"
		}
		out.Shares = append(out.Shares, apiProjectShareJSON{
			ID: g.ID, Kind: kind, TeamID: g.OrganizationID, UserID: g.UserID, Name: g.Name, Email: g.Email, UserName: g.UserName,
			Level: g.Level, RoleID: g.RoleID, RoleLabel: g.RoleLabel, Company: g.IsCompany, Members: g.MemberCount, SharedAt: formatRFC3339(g.CreatedAt),
		})
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(out)
}
