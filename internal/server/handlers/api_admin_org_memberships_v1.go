package handlers

import (
	"encoding/json"
	"net/http"

	"GoTodo/internal/server/utils"
	"GoTodo/internal/storage"
)

type apiAdminOrgMembershipJSON struct {
	OrganizationID   int    `json:"organization_id"`
	OrganizationName string `json:"organization_name"`
	IsCompany        bool   `json:"is_company"`
	UserID           int    `json:"user_id"`
	Role             string `json:"role"`
	CanManage        bool   `json:"can_manage"`
}

// APIV1AdminOrganizationMemberships lists every organization membership on the
// site, with whether each member's role can manage the organization. It lets an
// admin integration (e.g. Ordryn Chat directory sync) mirror organizations
// without its service account having to join each one.
func APIV1AdminOrganizationMemberships(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		utils.APIJSONError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed.")
		return
	}
	out, err := listOrgMembershipsJSON()
	if err != nil {
		utils.APIJSONError(w, http.StatusInternalServerError, "internal_error", "Failed to list organization memberships.")
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(out)
}

// listOrgMembershipsJSON is every organization membership, with whether each
// member's role can manage the organization.
func listOrgMembershipsJSON() ([]apiAdminOrgMembershipJSON, error) {
	members, err := storage.ListAllOrganizationMemberships()
	if err != nil {
		return nil, err
	}
	type orgRole struct {
		org  int
		role string
	}
	canManage := map[orgRole]bool{}
	out := make([]apiAdminOrgMembershipJSON, 0, len(members))
	for _, m := range members {
		key := orgRole{m.OrganizationID, m.Role}
		manage, ok := canManage[key]
		if !ok {
			manage = storage.RoleCanManageOrganization(m.OrganizationID, m.Role)
			canManage[key] = manage
		}
		out = append(out, apiAdminOrgMembershipJSON{
			OrganizationID:   m.OrganizationID,
			OrganizationName: m.OrganizationName,
			IsCompany:        m.IsCompany,
			UserID:           m.UserID,
			Role:             m.Role,
			CanManage:        manage,
		})
	}
	return out, nil
}
