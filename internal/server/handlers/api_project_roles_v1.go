package handlers

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"GoTodo/internal/domain"
	"GoTodo/internal/server/utils"
	"GoTodo/internal/storage"
)

type apiProjectPermJSON struct {
	ID          string `json:"id"`
	Label       string `json:"label"`
	Description string `json:"description"`
	Group       string `json:"group"`
}

type apiProjectRoleDefJSON struct {
	ID             int      `json:"id"`
	ProjectID      *int     `json:"project_id,omitempty"`
	OrganizationID *int     `json:"organization_id,omitempty"`
	Slug           string   `json:"slug"`
	Name           string   `json:"name"`
	Description    string   `json:"description,omitempty"`
	Permissions    []string `json:"permissions"`
	IsSystem       bool     `json:"is_system"`
	SortOrder      int      `json:"sort_order"`
	CreatedAt      string   `json:"created_at"`
	OverridesSite  bool     `json:"overrides_site,omitempty"`
}

type apiProjectRolesListJSON struct {
	Catalog []apiProjectPermJSON    `json:"catalog"`
	Roles   []apiProjectRoleDefJSON `json:"roles"`
}

type apiProjectRoleWriteRequest struct {
	Slug        string   `json:"slug"`
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Permissions []string `json:"permissions"`
	SortOrder   *int     `json:"sort_order"`
	CopyFromID  *int     `json:"copy_from_id"`
}

type apiRoleReorderRequest struct {
	RoleIDs []int `json:"role_ids"`
}

type apiProjectRolePatchRequest struct {
	Name        *string   `json:"name"`
	Description *string   `json:"description"`
	Permissions *[]string `json:"permissions"`
	SortOrder   *int      `json:"sort_order"`
}

type apiStatusGatesRequest struct {
	EnterRoleSlugs []string `json:"enter_role_slugs"`
	LeaveRoleSlugs []string `json:"leave_role_slugs"`
}

type apiStatusGateJSON struct {
	StatusID       int      `json:"status_id"`
	EnterRoleSlugs []string `json:"enter_role_slugs"`
	LeaveRoleSlugs []string `json:"leave_role_slugs"`
}

func permCatalogToJSON(list []storage.ProjectPermInfo) []apiProjectPermJSON {
	out := make([]apiProjectPermJSON, 0, len(list))
	for _, p := range list {
		out = append(out, apiProjectPermJSON{ID: p.ID, Label: p.Label, Description: p.Description, Group: p.Group})
	}
	return out
}

func roleDefToJSON(d storage.ProjectRoleDef) apiProjectRoleDefJSON {
	perms := d.Permissions
	if perms == nil {
		perms = []string{}
	}
	return apiProjectRoleDefJSON{
		ID:             d.ID,
		ProjectID:      d.ProjectID,
		OrganizationID: d.OrganizationID,
		Slug:           d.Slug,
		Name:           d.Name,
		Description:    d.Description,
		Permissions:    perms,
		IsSystem:       d.IsSystem,
		SortOrder:      d.SortOrder,
		CreatedAt:      formatRFC3339(d.CreatedAt),
		OverridesSite:  d.OverridesSite,
	}
}

func roleDefsToJSON(list []storage.ProjectRoleDef) []apiProjectRoleDefJSON {
	out := make([]apiProjectRoleDefJSON, 0, len(list))
	for _, d := range list {
		out = append(out, roleDefToJSON(d))
	}
	return out
}

func writeProjectRoleDomainError(w http.ResponseWriter, err error) {
	if errors.Is(err, domain.ErrConflict) {
		utils.APIJSONError(w, http.StatusConflict, "conflict", sharingClientMessage(err, "Conflict."))
		return
	}
	writeSharingDomainError(w, err)
}

func copyFromID(v *int) int {
	if v == nil {
		return 0
	}
	return *v
}

func sortOrderValue(v *int) int {
	if v == nil {
		return 100
	}
	return *v
}

// APIV1ProjectRolesCatalog lists site-level roles and the permission catalog.
func APIV1ProjectRolesCatalog(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		utils.APIJSONError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed.")
		return
	}
	userID, ok := apiUserFromRequest(r)
	if !ok {
		utils.APIJSONError(w, http.StatusUnauthorized, "unauthorized", "Not authenticated.")
		return
	}
	catalog, roles, err := domain.ListSiteProjectRolesForUser(r.Context(), userID)
	if err != nil {
		writeProjectRoleDomainError(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(apiProjectRolesListJSON{
		Catalog: permCatalogToJSON(catalog),
		Roles:   roleDefsToJSON(roles),
	})
}

// APIV1AdminProjectRolesRouter handles /api/v2/admin/project-roles.
func APIV1AdminProjectRolesRouter(w http.ResponseWriter, r *http.Request) {
	userID, ok := apiUserFromRequest(r)
	if !ok {
		utils.APIJSONError(w, http.StatusUnauthorized, "unauthorized", "Not authenticated.")
		return
	}
	sub := strings.Trim(utils.ParseAPIV1Subpath(r, "admin/project-roles"), "/")
	if sub == "" {
		switch r.Method {
		case http.MethodGet:
			catalog, roles, err := domain.ListSiteProjectRolesForUser(r.Context(), userID)
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
			created, err := domain.CreateSiteProjectRoleForAdmin(r.Context(), userID, domain.CreateSiteProjectRoleInput{
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
			logAdminEvent(r, "role_template_created", "role", int64(created.ID), created.Name,
				map[string]interface{}{"slug": created.Slug, "to": created.Permissions})
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(roleDefToJSON(*created))
		default:
			utils.APIJSONError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed.")
		}
		return
	}
	if sub == "reorder" {
		if r.Method != http.MethodPost {
			utils.APIJSONError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed.")
			return
		}
		var req apiRoleReorderRequest
		if err := decodeJSONBody(r, &req); err != nil {
			utils.APIJSONError(w, http.StatusBadRequest, "invalid_request", "Invalid JSON body.")
			return
		}
		if err := domain.ReorderSiteProjectRolesForAdmin(r.Context(), userID, req.RoleIDs); err != nil {
			writeProjectRoleDomainError(w, err)
			return
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_ = json.NewEncoder(w).Encode(apiReorderOKResponse{OK: true})
		return
	}
	roleID, err := strconv.Atoi(sub)
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
		before, _ := storage.GetProjectRoleDef(roleID)
		updated, err := domain.UpdateSiteProjectRoleForAdmin(r.Context(), userID, roleID, domain.UpdateSiteProjectRoleInput{
			Name:        req.Name,
			Description: req.Description,
			Permissions: req.Permissions,
			SortOrder:   req.SortOrder,
		})
		if err != nil {
			writeProjectRoleDomainError(w, err)
			return
		}
		logAdminEvent(r, "role_template_updated", "role", int64(updated.ID), updated.Name,
			roleTemplateChangeMetadata(before, updated))
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_ = json.NewEncoder(w).Encode(roleDefToJSON(*updated))
	case http.MethodDelete:
		before, _ := storage.GetProjectRoleDef(roleID)
		if err := domain.DeleteSiteProjectRoleForAdmin(r.Context(), userID, roleID); err != nil {
			writeProjectRoleDomainError(w, err)
			return
		}
		label, meta := "", map[string]interface{}{}
		if before != nil {
			label = before.Name
			meta["slug"] = before.Slug
			meta["from"] = before.Permissions
		}
		logAdminEvent(r, "role_template_deleted", "role", int64(roleID), label, meta)
		w.WriteHeader(http.StatusNoContent)
	default:
		utils.APIJSONError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed.")
	}
}

func handleProjectRolesResource(w http.ResponseWriter, r *http.Request, projectID int, rest []string) {
	userID, ok := apiUserFromRequest(r)
	if !ok {
		utils.APIJSONError(w, http.StatusUnauthorized, "unauthorized", "Not authenticated.")
		return
	}
	if len(rest) == 0 {
		switch r.Method {
		case http.MethodGet:
			roles, catalog, err := domain.ListProjectRolesForUser(r.Context(), userID, projectID)
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
			created, err := domain.CreateProjectCustomRoleForUser(r.Context(), userID, projectID, domain.CreateSiteProjectRoleInput{
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
		if err := domain.ReorderProjectCustomRolesForUser(r.Context(), userID, projectID, req.RoleIDs); err != nil {
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
		updated, err := domain.UpdateProjectCustomRoleForUser(r.Context(), userID, projectID, roleID, domain.UpdateSiteProjectRoleInput{
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
		if err := domain.DeleteProjectCustomRoleForUser(r.Context(), userID, projectID, roleID); err != nil {
			writeProjectRoleDomainError(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	default:
		utils.APIJSONError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed.")
	}
}

func handleStatusGatesResource(w http.ResponseWriter, r *http.Request, projectID, statusID int) {
	userID, ok := apiUserFromRequest(r)
	if !ok {
		utils.APIJSONError(w, http.StatusUnauthorized, "unauthorized", "Not authenticated.")
		return
	}
	if r.Method != http.MethodPut && r.Method != http.MethodPatch {
		utils.APIJSONError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed.")
		return
	}
	var req apiStatusGatesRequest
	if err := decodeJSONBody(r, &req); err != nil {
		utils.APIJSONError(w, http.StatusBadRequest, "invalid_request", "Invalid JSON body.")
		return
	}
	gate, err := domain.UpdateStatusGatesForUser(r.Context(), userID, projectID, statusID, req.EnterRoleSlugs, req.LeaveRoleSlugs)
	if err != nil {
		writeProjectRoleDomainError(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(apiStatusGateJSON{
		StatusID:       gate.StatusID,
		EnterRoleSlugs: gate.EnterRoleSlugs,
		LeaveRoleSlugs: gate.LeaveRoleSlugs,
	})
}

// roleTemplateChangeMetadata records name/permission changes on a site role template.
func roleTemplateChangeMetadata(before, after *storage.ProjectRoleDef) map[string]interface{} {
	meta := map[string]interface{}{"slug": after.Slug}
	if before == nil {
		meta["to"] = after.Permissions
		return meta
	}
	if before.Name != after.Name {
		meta["name"] = map[string]interface{}{"from": before.Name, "to": after.Name}
	}
	if strings.Join(before.Permissions, ",") != strings.Join(after.Permissions, ",") {
		meta["from"] = before.Permissions
		meta["to"] = after.Permissions
	}
	return meta
}
