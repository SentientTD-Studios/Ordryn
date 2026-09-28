package handlers

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"GoTodo/internal/domain"
	"GoTodo/internal/server/utils"
	"GoTodo/internal/storage"
)

type apiOrganizationJSON struct {
	ID           int      `json:"id"`
	Name         string   `json:"name"`
	Description  string   `json:"description,omitempty"`
	CreatedBy    int      `json:"created_by"`
	Role         string   `json:"role,omitempty"`
	RoleName     string   `json:"role_name,omitempty"`
	Permissions  []string `json:"permissions,omitempty"`
	CanManage    bool     `json:"can_manage"`
	MemberCount  int      `json:"member_count"`
	ProjectCount int      `json:"project_count"`
	CreatedAt    string   `json:"created_at"`
	UpdatedAt    string   `json:"updated_at"`
}

type apiOrganizationWriteRequest struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

type apiOrganizationPatchRequest struct {
	Name        *string `json:"name"`
	Description *string `json:"description"`
}

type apiOrgMemberJSON struct {
	UserID    int    `json:"user_id"`
	Email     string `json:"email"`
	UserName  string `json:"user_name"`
	Role      string `json:"role"`
	RoleName  string `json:"role_name,omitempty"`
	CreatedAt string `json:"created_at"`
}

type apiOrganizationInviteJSON struct {
	ID               int    `json:"id"`
	OrganizationID   int    `json:"organization_id"`
	Email            string `json:"email"`
	UserName         string `json:"user_name,omitempty"`
	Role             string `json:"role"`
	ExpiresAt        string `json:"expires_at"`
	CreatedAt        string `json:"created_at"`
	OrganizationName string `json:"organization_name,omitempty"`
	InviterEmail     string `json:"inviter_email,omitempty"`
	InviterUserName  string `json:"inviter_user_name,omitempty"`
}

func organizationToJSON(o storage.Organization) apiOrganizationJSON {
	perms := storage.RolePermissionList(0, o.Role)
	if def := storage.ResolveOrgRoleDef(o.ID, o.Role); def != nil && o.Role != storage.RoleOwner {
		perms = def.Permissions
		if perms == nil {
			perms = []string{}
		}
	}
	return apiOrganizationJSON{
		ID:           o.ID,
		Name:         o.Name,
		Description:  o.Description,
		CreatedBy:    o.CreatedBy,
		Role:         o.Role,
		RoleName:     storage.OrgRoleDisplayName(o.ID, o.Role),
		Permissions:  perms,
		CanManage:    storage.RoleCanManageOrganization(o.ID, o.Role),
		MemberCount:  o.MemberCount,
		ProjectCount: o.ProjectCount,
		CreatedAt:    formatRFC3339(o.CreatedAt),
		UpdatedAt:    formatRFC3339(o.UpdatedAt),
	}
}

func organizationInviteToJSON(inv storage.OrganizationInvite) apiOrganizationInviteJSON {
	return apiOrganizationInviteJSON{
		ID:               inv.ID,
		OrganizationID:   inv.OrganizationID,
		Email:            inv.Email,
		UserName:         inv.UserName,
		Role:             inv.Role,
		ExpiresAt:        formatRFC3339(inv.ExpiresAt),
		CreatedAt:        formatRFC3339(inv.CreatedAt),
		OrganizationName: inv.OrganizationName,
		InviterEmail:     inv.InviterEmail,
		InviterUserName:  inv.InviterUserName,
	}
}

// APIV1OrganizationsRouter handles /api/v2/organizations and nested resources.
func APIV1OrganizationsRouter(w http.ResponseWriter, r *http.Request) {
	userID, ok := apiUserFromRequest(r)
	if !ok {
		utils.APIJSONError(w, http.StatusUnauthorized, "unauthorized", "Not authenticated.")
		return
	}
	sub := strings.Trim(utils.ParseAPIV1Subpath(r, "organizations"), "/")
	if sub == "" {
		switch r.Method {
		case http.MethodGet:
			orgs, err := domain.ListOrganizationsForUser(r.Context(), userID)
			if err != nil {
				writeProjectRoleDomainError(w, err)
				return
			}
			out := make([]apiOrganizationJSON, 0, len(orgs))
			for _, o := range orgs {
				out = append(out, organizationToJSON(o))
			}
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			_ = json.NewEncoder(w).Encode(out)
		case http.MethodPost:
			var req apiOrganizationWriteRequest
			if err := decodeJSONBody(r, &req); err != nil {
				utils.APIJSONError(w, http.StatusBadRequest, "invalid_request", "Invalid JSON body.")
				return
			}
			created, err := domain.CreateOrganizationForUser(r.Context(), userID, req.Name, req.Description)
			if err != nil {
				writeProjectRoleDomainError(w, err)
				return
			}
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(organizationToJSON(*created))
		default:
			utils.APIJSONError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed.")
		}
		return
	}

	parts := strings.Split(sub, "/")
	orgID, err := strconv.Atoi(parts[0])
	if err != nil || orgID <= 0 {
		utils.APIJSONError(w, http.StatusBadRequest, "invalid_request", "Invalid organization id.")
		return
	}
	if len(parts) == 1 {
		switch r.Method {
		case http.MethodGet:
			org, err := domain.GetOrganizationForUser(r.Context(), userID, orgID)
			if err != nil {
				writeProjectRoleDomainError(w, err)
				return
			}
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			_ = json.NewEncoder(w).Encode(organizationToJSON(*org))
		case http.MethodPatch:
			var req apiOrganizationPatchRequest
			if err := decodeJSONBody(r, &req); err != nil {
				utils.APIJSONError(w, http.StatusBadRequest, "invalid_request", "Invalid JSON body.")
				return
			}
			updated, err := domain.UpdateOrganizationForUser(r.Context(), userID, orgID, req.Name, req.Description)
			if err != nil {
				writeProjectRoleDomainError(w, err)
				return
			}
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			_ = json.NewEncoder(w).Encode(organizationToJSON(*updated))
		case http.MethodDelete:
			if err := domain.DeleteOrganizationForUser(r.Context(), userID, orgID); err != nil {
				writeProjectRoleDomainError(w, err)
				return
			}
			w.WriteHeader(http.StatusNoContent)
		default:
			utils.APIJSONError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed.")
		}
		return
	}

	switch parts[1] {
	case "members":
		handleOrganizationMembers(w, r, userID, orgID, parts[2:])
	case "invites":
		handleOrganizationInvites(w, r, userID, orgID, parts[2:])
	case "roles":
		handleOrganizationRoles(w, r, userID, orgID, parts[2:])
	case "projects":
		handleOrganizationProjects(w, r, userID, orgID, parts[2:])
	default:
		utils.APIJSONError(w, http.StatusBadRequest, "invalid_request", "Unknown organization resource.")
	}
}

func handleOrganizationMembers(w http.ResponseWriter, r *http.Request, userID, orgID int, rest []string) {
	if len(rest) == 0 {
		switch r.Method {
		case http.MethodGet:
			members, err := domain.ListOrganizationMembersForUser(r.Context(), userID, orgID)
			if err != nil {
				writeProjectRoleDomainError(w, err)
				return
			}
			out := make([]apiOrgMemberJSON, 0, len(members))
			for _, m := range members {
				out = append(out, apiOrgMemberJSON{
					UserID:    m.UserID,
					Email:     m.Email,
					UserName:  m.UserName,
					Role:      m.Role,
					RoleName:  storage.OrgRoleDisplayName(orgID, m.Role),
					CreatedAt: formatRFC3339(m.CreatedAt),
				})
			}
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			_ = json.NewEncoder(w).Encode(out)
		case http.MethodPost:
			var req apiInviteCreateRequest
			if err := decodeJSONBody(r, &req); err != nil {
				utils.APIJSONError(w, http.StatusBadRequest, "invalid_request", "Invalid JSON body.")
				return
			}
			inv, err := domain.InviteToOrganization(r.Context(), userID, orgID, req.Username, req.Role)
			if err != nil {
				writeProjectRoleDomainError(w, err)
				return
			}
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(organizationInviteToJSON(*inv))
		default:
			utils.APIJSONError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed.")
		}
		return
	}
	if len(rest) == 2 && rest[1] == "project-impact" {
		if r.Method != http.MethodGet {
			utils.APIJSONError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed.")
			return
		}
		memberID, err := strconv.Atoi(rest[0])
		if err != nil || memberID <= 0 {
			utils.APIJSONError(w, http.StatusBadRequest, "invalid_request", "Invalid member id.")
			return
		}
		impact, err := domain.OrganizationMemberRoleImpactForUser(r.Context(), userID, orgID, memberID)
		if err != nil {
			writeProjectRoleDomainError(w, err)
			return
		}
		type refJSON struct {
			ID     int    `json:"id"`
			Name   string `json:"name"`
			Role   string `json:"role"`
			Locked bool   `json:"locked"`
		}
		toJSON := func(in []storage.OrgMemberProjectRef) []refJSON {
			out := make([]refJSON, 0, len(in))
			for _, r := range in {
				out = append(out, refJSON{ID: r.ID, Name: r.Name, Role: r.Role, Locked: r.Locked})
			}
			return out
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"locked":   toJSON(impact.Locked),
			"unlocked": toJSON(impact.Unlocked),
		})
		return
	}
	if len(rest) != 1 {
		utils.APIJSONError(w, http.StatusBadRequest, "invalid_request", "Invalid member id.")
		return
	}
	memberID, err := strconv.Atoi(rest[0])
	if err != nil || memberID <= 0 {
		utils.APIJSONError(w, http.StatusBadRequest, "invalid_request", "Invalid member id.")
		return
	}
	switch r.Method {
	case http.MethodPatch:
		var req apiMemberPatchRequest
		if err := decodeJSONBody(r, &req); err != nil {
			utils.APIJSONError(w, http.StatusBadRequest, "invalid_request", "Invalid JSON body.")
			return
		}
		if err := domain.UpdateOrganizationMemberRoleForUser(r.Context(), userID, orgID, memberID, req.Role); err != nil {
			writeProjectRoleDomainError(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	case http.MethodDelete:
		if err := domain.RemoveOrganizationMemberForUser(r.Context(), userID, orgID, memberID); err != nil {
			writeProjectRoleDomainError(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	default:
		utils.APIJSONError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed.")
	}
}

func handleOrganizationProjects(w http.ResponseWriter, r *http.Request, userID, orgID int, rest []string) {
	if len(rest) != 0 {
		utils.APIJSONError(w, http.StatusBadRequest, "invalid_request", "Unknown organization project resource.")
		return
	}
	if r.Method != http.MethodGet {
		utils.APIJSONError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed.")
		return
	}
	rosters, err := domain.ListOrganizationProjectRostersForUser(r.Context(), userID, orgID)
	if err != nil {
		writeProjectRoleDomainError(w, err)
		return
	}
	type memberJSON struct {
		UserID    int    `json:"user_id"`
		Email     string `json:"email"`
		UserName  string `json:"user_name"`
		Role      string `json:"role"`
		RoleName  string `json:"role_name,omitempty"`
		Inherited bool   `json:"inherited,omitempty"`
		CreatedAt string `json:"created_at,omitempty"`
	}
	type rosterJSON struct {
		ID         int          `json:"id"`
		Name       string       `json:"name"`
		OrgManaged bool         `json:"org_managed"`
		Members    []memberJSON `json:"members"`
	}
	out := make([]rosterJSON, 0, len(rosters))
	for _, roster := range rosters {
		members := make([]memberJSON, 0, len(roster.Members))
		for _, m := range roster.Members {
			members = append(members, memberJSON{
				UserID:    m.UserID,
				Email:     m.Email,
				UserName:  m.UserName,
				Role:      m.Role,
				RoleName:  storage.RoleDisplayName(roster.ID, m.Role),
				Inherited: m.Inherited,
				CreatedAt: formatRFC3339(m.CreatedAt),
			})
		}
		out = append(out, rosterJSON{
			ID:         roster.ID,
			Name:       roster.Name,
			OrgManaged: roster.OrgManaged,
			Members:    members,
		})
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(out)
}

func handleOrganizationInvites(w http.ResponseWriter, r *http.Request, userID, orgID int, rest []string) {
	if len(rest) == 0 {
		switch r.Method {
		case http.MethodGet:
			invites, err := domain.ListOrganizationInvitesForUser(r.Context(), userID, orgID)
			if err != nil {
				writeProjectRoleDomainError(w, err)
				return
			}
			out := make([]apiOrganizationInviteJSON, 0, len(invites))
			for _, inv := range invites {
				out = append(out, organizationInviteToJSON(inv))
			}
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			_ = json.NewEncoder(w).Encode(out)
		case http.MethodPost:
			var req apiInviteCreateRequest
			if err := decodeJSONBody(r, &req); err != nil {
				utils.APIJSONError(w, http.StatusBadRequest, "invalid_request", "Invalid JSON body.")
				return
			}
			inv, err := domain.InviteToOrganization(r.Context(), userID, orgID, req.Username, req.Role)
			if err != nil {
				writeProjectRoleDomainError(w, err)
				return
			}
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(organizationInviteToJSON(*inv))
		default:
			utils.APIJSONError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed.")
		}
		return
	}
	if len(rest) != 1 {
		utils.APIJSONError(w, http.StatusBadRequest, "invalid_request", "Invalid invite id.")
		return
	}
	inviteID, err := strconv.Atoi(rest[0])
	if err != nil || inviteID <= 0 {
		utils.APIJSONError(w, http.StatusBadRequest, "invalid_request", "Invalid invite id.")
		return
	}
	if r.Method != http.MethodDelete {
		utils.APIJSONError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed.")
		return
	}
	if err := domain.RevokeOrganizationInviteForUser(r.Context(), userID, orgID, inviteID); err != nil {
		writeProjectRoleDomainError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// APIV1OrganizationInvitesRouter handles /api/v2/organization-invites and accept/decline.
func APIV1OrganizationInvitesRouter(w http.ResponseWriter, r *http.Request) {
	userID, ok := apiUserFromRequest(r)
	if !ok {
		utils.APIJSONError(w, http.StatusUnauthorized, "unauthorized", "Not authenticated.")
		return
	}
	email, err := storage.GetUserEmailByID(userID)
	if err != nil {
		utils.APIJSONError(w, http.StatusInternalServerError, "internal_error", "Failed to load user.")
		return
	}

	sub := strings.Trim(utils.ParseAPIV1Subpath(r, "organization-invites"), "/")
	if sub == "" {
		if r.Method != http.MethodGet {
			utils.APIJSONError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed.")
			return
		}
		invites, err := domain.ListMyOrganizationInvitesForUser(r.Context(), email)
		if err != nil {
			writeProjectRoleDomainError(w, err)
			return
		}
		out := make([]apiOrganizationInviteJSON, 0, len(invites))
		for _, inv := range invites {
			out = append(out, organizationInviteToJSON(inv))
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_ = json.NewEncoder(w).Encode(out)
		return
	}

	parts := strings.Split(sub, "/")
	if len(parts) != 2 {
		utils.APIJSONError(w, http.StatusBadRequest, "invalid_request", "Invalid path.")
		return
	}
	inviteID, err := strconv.Atoi(parts[0])
	if err != nil || inviteID <= 0 {
		utils.APIJSONError(w, http.StatusBadRequest, "invalid_request", "Invalid invite id.")
		return
	}
	if r.Method != http.MethodPost {
		utils.APIJSONError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed.")
		return
	}
	switch parts[1] {
	case "accept":
		if err := domain.AcceptOrganizationInviteForUser(r.Context(), userID, email, inviteID); err != nil {
			writeProjectRoleDomainError(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	case "decline":
		if err := domain.DeclineOrganizationInviteForUser(r.Context(), email, inviteID); err != nil {
			writeProjectRoleDomainError(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	default:
		utils.APIJSONError(w, http.StatusNotFound, "not_found", "Not found.")
	}
}

func handleOrganizationRoles(w http.ResponseWriter, r *http.Request, userID, orgID int, rest []string) {
	if len(rest) == 0 {
		switch r.Method {
		case http.MethodGet:
			roles, catalog, err := domain.ListOrganizationRolesForUser(r.Context(), userID, orgID)
			if err != nil {
				writeProjectRoleDomainError(w, err)
				return
			}
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			_ = json.NewEncoder(w).Encode(apiProjectRolesListJSON{
				Catalog: permCatalogToJSON(catalog),
				Roles:   roleDefsToJSON(roles),
			})
		case http.MethodPost:
			var req apiProjectRoleWriteRequest
			if err := decodeJSONBody(r, &req); err != nil {
				utils.APIJSONError(w, http.StatusBadRequest, "invalid_request", "Invalid JSON body.")
				return
			}
			created, err := domain.CreateOrganizationRoleForUser(r.Context(), userID, orgID, domain.CreateSiteProjectRoleInput{
				Slug:        req.Slug,
				Name:        req.Name,
				Description: req.Description,
				Permissions: req.Permissions,
				SortOrder:   sortOrderValue(req.SortOrder),
				CopyFromID:  copyFromID(req.CopyFromID),
			})
			if err != nil {
				writeProjectRoleDomainError(w, err)
				return
			}
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(roleDefToJSON(*created))
		default:
			utils.APIJSONError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed.")
		}
		return
	}
	if len(rest) == 1 && rest[0] == "reorder" {
		if r.Method != http.MethodPost {
			utils.APIJSONError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed.")
			return
		}
		var req apiRoleReorderRequest
		if err := decodeJSONBody(r, &req); err != nil {
			utils.APIJSONError(w, http.StatusBadRequest, "invalid_request", "Invalid JSON body.")
			return
		}
		if err := domain.ReorderOrganizationRolesForUser(r.Context(), userID, orgID, req.RoleIDs); err != nil {
			writeProjectRoleDomainError(w, err)
			return
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_ = json.NewEncoder(w).Encode(apiReorderOKResponse{OK: true})
		return
	}
	if len(rest) != 1 {
		utils.APIJSONError(w, http.StatusBadRequest, "invalid_request", "Invalid role id.")
		return
	}
	roleID, err := strconv.Atoi(rest[0])
	if err != nil || roleID <= 0 {
		utils.APIJSONError(w, http.StatusBadRequest, "invalid_request", "Invalid role id.")
		return
	}
	switch r.Method {
	case http.MethodPatch:
		var req apiProjectRolePatchRequest
		if err := decodeJSONBody(r, &req); err != nil {
			utils.APIJSONError(w, http.StatusBadRequest, "invalid_request", "Invalid JSON body.")
			return
		}
		updated, err := domain.UpdateOrganizationRoleForUser(r.Context(), userID, orgID, roleID, domain.UpdateSiteProjectRoleInput{
			Name:        req.Name,
			Description: req.Description,
			Permissions: req.Permissions,
			SortOrder:   req.SortOrder,
		})
		if err != nil {
			writeProjectRoleDomainError(w, err)
			return
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_ = json.NewEncoder(w).Encode(roleDefToJSON(*updated))
	case http.MethodDelete:
		if err := domain.DeleteOrganizationRoleForUser(r.Context(), userID, orgID, roleID); err != nil {
			writeProjectRoleDomainError(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	default:
		utils.APIJSONError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed.")
	}
}
