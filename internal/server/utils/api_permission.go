package utils

import (
	"net/http"

	"GoTodo/internal/storage"
)

// RequireAPIPermission ensures the authenticated API user has a named permission.
// Permissions are read live so a role change takes effect without a new login.
func RequireAPIPermission(permission string, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID, ok := GetAPIUserID(r)
		if !ok {
			APIJSONError(w, http.StatusUnauthorized, "unauthorized", "Not authenticated.")
			return
		}

		perms, err := storage.GetUserPermissions(userID)
		if err != nil {
			APIJSONError(w, http.StatusInternalServerError, "internal_error", "Failed to load permissions.")
			return
		}
		for _, p := range perms {
			if p == permission {
				next(w, r)
				return
			}
		}
		APIJSONError(w, http.StatusForbidden, "forbidden", "Missing required permission.")
	}
}

// AdminAPIChain is APIChain + admin permission.
func AdminAPIChain(handler http.HandlerFunc) http.HandlerFunc {
	return APIChain(RequireAPIPermission("admin", handler))
}

// SitePermAPIChain is APIChain + a site permission; admin implies every one.
func SitePermAPIChain(permission string, handler http.HandlerFunc) http.HandlerFunc {
	return APIChain(func(w http.ResponseWriter, r *http.Request) {
		userID, ok := GetAPIUserID(r)
		if !ok {
			APIJSONError(w, http.StatusUnauthorized, "unauthorized", "Not authenticated.")
			return
		}
		if !storage.UserHasSitePermission(userID, permission) {
			APIJSONError(w, http.StatusForbidden, "forbidden", "Missing required permission.")
			return
		}
		handler(w, r)
	})
}

// NotInCompanyMode answers 404 on a company site, where accounts come only from people
// managers: no site invites or join requests.
func NotInCompanyMode(handler http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if storage.IsCompanyMode() {
			APIJSONError(w, http.StatusNotFound, "not_available",
				"Accounts on this site are created by its admins and team managers.")
			return
		}
		handler(w, r)
	}
}

// InviteAPIChain allows access if user has 'createinvites' or 'admin' permission,
// or if site_settings.allow_user_invites is enabled.
func InviteAPIChain(handler http.HandlerFunc) http.HandlerFunc {
	return APIChain(func(w http.ResponseWriter, r *http.Request) {
		userID, ok := GetAPIUserID(r)
		if !ok {
			APIJSONError(w, http.StatusUnauthorized, "unauthorized", "Not authenticated.")
			return
		}

		perms, err := storage.GetUserPermissions(userID)
		if err != nil {
			APIJSONError(w, http.StatusInternalServerError, "internal_error", "Failed to load permissions.")
			return
		}
		for _, p := range perms {
			if p == "createinvites" || p == "admin" {
				handler(w, r)
				return
			}
		}

		settings, err := storage.GetSiteSettings()
		if err == nil && settings != nil && settings.AllowUserInvites {
			handler(w, r)
			return
		}

		APIJSONError(w, http.StatusForbidden, "forbidden", "User invites are not enabled.")
	})
}
