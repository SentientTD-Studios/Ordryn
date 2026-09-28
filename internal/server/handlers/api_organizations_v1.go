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
	case "roles":
		handleOrganizationRoles(w, r, userID, orgID, parts[2:])
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
			member, err := domain.AddOrganizationMemberForUser(r.Context(), userID, orgID, req.Username, req.Role)
			if err != nil {
				writeProjectRoleDomainError(w, err)
				return
			}
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(apiOrgMemberJSON{
				UserID:   member.UserID,
				Email:    member.Email,
				UserName: member.UserName,
				Role:     member.Role,
				RoleName: storage.OrgRoleDisplayName(orgID, member.Role),
			})
		default:
			utils.APIJSONError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed.")
		}
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
