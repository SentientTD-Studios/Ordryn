package utils

import (
	"net/http"

	"GoTodo/internal/storage"
)

// RequireAPIPermission ensures the authenticated API user has a named permission.
func RequireAPIPermission(permission string, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID, ok := GetAPIUserID(r)
		if !ok {
			APIJSONError(w, http.StatusUnauthorized, "unauthorized", "Not authenticated.")
			return
		}

		perms := []string{}
		if _, _, sessionPerms, loggedIn := GetSessionUser(r); loggedIn {
			perms = sessionPerms
		} else {
			profile, err := storage.GetUserProfileByID(userID)
			if err != nil {
				APIJSONError(w, http.StatusInternalServerError, "internal_error", "Failed to load permissions.")
				return
			}
			perms = profile.Permissions
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

// InviteAPIChain allows access if user has 'createinvites' or 'admin' permission,
// or if site_settings.allow_user_invites is enabled.
func InviteAPIChain(handler http.HandlerFunc) http.HandlerFunc {
	return APIChain(func(w http.ResponseWriter, r *http.Request) {
		userID, ok := GetAPIUserID(r)
		if !ok {
			APIJSONError(w, http.StatusUnauthorized, "unauthorized", "Not authenticated.")
			return
		}

		perms := []string{}
		if _, _, sessionPerms, loggedIn := GetSessionUser(r); loggedIn {
			perms = sessionPerms
		} else {
			profile, err := storage.GetUserProfileByID(userID)
			if err != nil {
				APIJSONError(w, http.StatusInternalServerError, "internal_error", "Failed to load permissions.")
				return
			}
			perms = profile.Permissions
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
