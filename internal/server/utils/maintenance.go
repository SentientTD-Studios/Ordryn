package utils

import (
	"net/http"
	"slices"
	"strings"

	"GoTodo/internal/storage"
)

// maintenanceOpenPaths still accept writes during maintenance, so people can
// sign in and out and admins can turn it off.
var maintenanceOpenPaths = []string{
	"/auth/login", "/auth/logout", "/auth/mfa/verify", "/announcements/dismiss", "/admin/maintenance",
}

// MaintenanceGuard refuses writes from everyone but admins while the site is in
// maintenance (read-only) mode. Reads always go through. It costs nothing while
// maintenance is off beyond a cached flag check.
func MaintenanceGuard(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !isWriteMethod(r.Method) {
			next(w, r)
			return
		}
		m := storage.GetMaintenance()
		if !m.Enabled || slices.Contains(maintenanceOpenPaths, apiPathSuffixForMaintenance(r)) || requestIsAdmin(r) {
			next(w, r)
			return
		}
		msg := m.Message
		if msg == "" {
			msg = "The site is in maintenance mode and is read-only for now. Try again shortly."
		}
		w.Header().Set("Retry-After", "120")
		APIJSONError(w, http.StatusServiceUnavailable, "maintenance", msg)
	}
}

// requestIsAdmin reports whether the request's session or API key belongs to an
// admin. Only called during maintenance, so the key lookup cost doesn't matter.
func requestIsAdmin(r *http.Request) bool {
	if token := extractBearerToken(r); token != "" {
		p, err := storage.LookupAPIKey(token)
		return err == nil && !p.IsAgent && storage.UserHasSitePermission(p.UserID, storage.SitePermAdmin)
	}
	if uid := GetSessionUserID(r); uid != nil {
		return storage.UserHasSitePermission(*uid, storage.SitePermAdmin)
	}
	return false
}

// apiPathSuffixForMaintenance is the path after /api/v2 or /api/v1 (and BASE_PATH).
func apiPathSuffixForMaintenance(r *http.Request) string {
	path := r.URL.Path
	if base := strings.TrimSuffix(GetBasePath(), "/"); base != "" && !strings.Contains(base, "://") {
		path = strings.TrimPrefix(path, base)
	}
	for _, ver := range []string{"/api/v2", "/api/v1"} {
		if strings.HasPrefix(path, ver+"/") {
			return strings.TrimSuffix(strings.TrimPrefix(path, ver), "/")
		}
	}
	return path
}
