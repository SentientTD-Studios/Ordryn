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

type apiSitePermJSON struct {
	ID          string `json:"id"`
	Label       string `json:"label"`
	Description string `json:"description"`
	Group       string `json:"group"`
}

type apiSiteRoleJSON struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
	// Label is the name to show; built-in roles keep a fixed name and are renamed by label.
	Label       string   `json:"label"`
	Permissions []string `json:"permissions"`
	UserCount   int      `json:"user_count"`
	BuiltIn     bool     `json:"built_in"`
	// Locked is true for the admin role, whose permissions can't be edited.
	Locked bool `json:"locked"`
}

type apiSiteRolesListJSON struct {
	Catalog []apiSitePermJSON `json:"catalog"`
	Roles   []apiSiteRoleJSON `json:"roles"`
}

type apiSiteRoleWriteRequest struct {
	Name        *string   `json:"name"`
	Permissions *[]string `json:"permissions"`
}

type apiUserSiteRoleRequest struct {
	RoleID int `json:"role_id"`
}

func siteRoleToJSON(r storage.SiteRole) apiSiteRoleJSON {
	perms := r.Permissions
	if perms == nil {
		perms = []string{}
	}
	return apiSiteRoleJSON{
		ID:          r.ID,
		Name:        r.Name,
		Label:       r.Label(),
		Permissions: perms,
		UserCount:   r.UserCount,
		BuiltIn:     r.BuiltIn(),
		Locked:      r.Name == storage.AdminRoleName,
	}
}

// APIV1AdminSiteRolesRouter handles /api/v2/admin/site-roles and /api/v2/admin/site-roles/{id}.
func APIV1AdminSiteRolesRouter(w http.ResponseWriter, r *http.Request) {
	userID, ok := apiUserFromRequest(r)
	if !ok {
		utils.APIJSONError(w, http.StatusUnauthorized, "unauthorized", "Not authenticated.")
		return
	}
	sub := strings.Trim(utils.ParseAPIV1Subpath(r, "admin/site-roles"), "/")
	if sub == "" {
		switch r.Method {
		case http.MethodGet:
			catalog, roles, err := domain.ListSiteRolesForAdmin(r.Context(), userID)
			if err != nil {
				writeProjectRoleDomainError(w, err)
				return
			}
			out := apiSiteRolesListJSON{Catalog: []apiSitePermJSON{}, Roles: []apiSiteRoleJSON{}}
			for _, p := range catalog {
				out.Catalog = append(out.Catalog, apiSitePermJSON{ID: p.ID, Label: p.Label, Description: p.Description, Group: p.Group})
			}
			for _, role := range roles {
				out.Roles = append(out.Roles, siteRoleToJSON(role))
			}
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			_ = json.NewEncoder(w).Encode(out)
		case http.MethodPost:
			var req apiSiteRoleWriteRequest
			if err := decodeJSONBody(r, &req); err != nil {
				utils.APIJSONError(w, http.StatusBadRequest, "invalid_request", "Invalid JSON body.")
				return
			}
			name := ""
			if req.Name != nil {
				name = *req.Name
			}
			var perms []string
			if req.Permissions != nil {
				perms = *req.Permissions
			}
			created, err := domain.CreateSiteRoleForAdmin(r.Context(), userID, name, perms)
			if err != nil {
				writeProjectRoleDomainError(w, err)
				return
			}
			logAdminEvent(r, "site_role_created", "site_role", int64(created.ID), created.Name,
				map[string]interface{}{"to": created.Permissions})
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(siteRoleToJSON(*created))
		default:
			utils.APIJSONError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed.")
		}
		return
	}

	roleID, err := strconv.Atoi(sub)
	if err != nil || roleID <= 0 {
		utils.APIJSONError(w, http.StatusBadRequest, "invalid_request", "Invalid role id.")
		return
	}
	switch r.Method {
	case http.MethodPatch:
		var req apiSiteRoleWriteRequest
		if err := decodeJSONBody(r, &req); err != nil {
			utils.APIJSONError(w, http.StatusBadRequest, "invalid_request", "Invalid JSON body.")
			return
		}
		if req.Name == nil && req.Permissions == nil {
			utils.APIJSONError(w, http.StatusBadRequest, "invalid_request", "Nothing to update.")
			return
		}
		before, _ := storage.GetSiteRole(roleID)
		updated, err := domain.UpdateSiteRoleForAdmin(r.Context(), userID, roleID, req.Name, req.Permissions)
		if err != nil {
			writeProjectRoleDomainError(w, err)
			return
		}
		meta := map[string]interface{}{}
		if before != nil {
			if before.Label() != updated.Label() {
				meta["name"] = map[string]interface{}{"from": before.Label(), "to": updated.Label()}
			}
			if strings.Join(before.Permissions, ",") != strings.Join(updated.Permissions, ",") {
				meta["permissions"] = map[string]interface{}{"from": before.Permissions, "to": updated.Permissions}
			}
		}
		if len(meta) > 0 {
			logAdminEvent(r, "site_role_updated", "site_role", int64(updated.ID), updated.Label(), meta)
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_ = json.NewEncoder(w).Encode(siteRoleToJSON(*updated))
	case http.MethodDelete:
		deleted, moved, err := domain.DeleteSiteRoleForAdmin(r.Context(), userID, roleID)
		if err != nil {
			writeProjectRoleDomainError(w, err)
			return
		}
		logAdminEvent(r, "site_role_deleted", "site_role", int64(roleID), deleted.Label(),
			map[string]interface{}{"from": deleted.Permissions, "users_moved": moved})
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"ok": true, "users_moved": moved})
	default:
		utils.APIJSONError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed.")
	}
}

// apiV1AdminSetUserSiteRole handles PUT /api/v2/admin/users/{id}/role.
func apiV1AdminSetUserSiteRole(w http.ResponseWriter, r *http.Request, targetUserID int) {
	actorID, ok := apiUserFromRequest(r)
	if !ok {
		utils.APIJSONError(w, http.StatusUnauthorized, "unauthorized", "Not authenticated.")
		return
	}
	var req apiUserSiteRoleRequest
	if err := decodeJSONBody(r, &req); err != nil || req.RoleID <= 0 {
		utils.APIJSONError(w, http.StatusBadRequest, "invalid_request", "role_id is required.")
		return
	}
	from, to, err := domain.SetUserSiteRoleForAdmin(r.Context(), actorID, targetUserID, req.RoleID)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			utils.APIJSONError(w, http.StatusNotFound, "not_found", "User not found.")
			return
		}
		writeProjectRoleDomainError(w, err)
		return
	}
	if from.ID != to.ID {
		logAdminEvent(r, "user_site_role_changed", "user", int64(targetUserID), storage.UserAuditLabel(targetUserID),
			map[string]interface{}{"from": from.Label(), "to": to.Label()})
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"ok": true, "role_id": to.ID, "role_name": to.Label()})
}
