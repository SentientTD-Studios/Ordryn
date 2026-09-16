package handlers

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"GoTodo/internal/extensions"
	"GoTodo/internal/server/utils"
)

// APIV1ExtensionsStatic serves extension icons at GET /api/v1/extensions/{id}/icon.
func APIV1ExtensionsStatic(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		utils.APIJSONError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed.")
		return
	}
	if _, ok := apiUserFromRequest(r); !ok {
		utils.APIJSONError(w, http.StatusUnauthorized, "unauthorized", "Not authenticated.")
		return
	}
	path := utils.ParseAPIV1Subpath(r, "extensions")
	parts := strings.Split(path, "/")
	if path == "" || len(parts) != 2 || parts[1] != "icon" {
		utils.APIJSONError(w, http.StatusNotFound, "not_found", "Not found.")
		return
	}
	entry, ok := extensions.Get(strings.TrimSpace(parts[0]))
	if !ok || !entry.Loaded {
		utils.APIJSONError(w, http.StatusNotFound, "not_found", "Extension not found.")
		return
	}
	icon := strings.TrimSpace(entry.Manifest.Icon)
	if icon == "" {
		utils.APIJSONError(w, http.StatusNotFound, "not_found", "This extension has no icon.")
		return
	}
	serveExtensionFile(w, r, entry.Dir, icon, false)
}

func serveExtensionFile(w http.ResponseWriter, r *http.Request, dir, rel string, asHTMLPanel bool) {
	full, err := extensions.ResolveFile(dir, rel)
	if err != nil {
		utils.APIJSONError(w, http.StatusNotFound, "not_found", "File not found.")
		return
	}
	st, err := os.Stat(full)
	if err != nil || st.IsDir() {
		utils.APIJSONError(w, http.StatusNotFound, "not_found", "File not found.")
		return
	}
	ctype := mimeFromExt(filepath.Ext(full))
	if asHTMLPanel {
		w.Header().Set("Content-Security-Policy", "default-src 'none'; script-src 'unsafe-inline'; style-src 'unsafe-inline'; img-src data: https:; font-src data:; connect-src https:; form-action https:; base-uri 'none'; frame-ancestors 'self'")
		w.Header().Set("X-Frame-Options", "SAMEORIGIN")
		w.Header().Set("Referrer-Policy", "no-referrer")
		if ctype == "" {
			ctype = "text/html; charset=utf-8"
		}
	}
	if ctype != "" {
		w.Header().Set("Content-Type", ctype)
	}
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Cache-Control", "private, max-age=60")
	http.ServeFile(w, r, full)
}

func mimeFromExt(ext string) string {
	switch strings.ToLower(ext) {
	case ".html", ".htm":
		return "text/html; charset=utf-8"
	case ".css":
		return "text/css; charset=utf-8"
	case ".js":
		return "text/javascript; charset=utf-8"
	case ".json":
		return "application/json"
	case ".svg":
		return "image/svg+xml"
	case ".png":
		return "image/png"
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".gif":
		return "image/gif"
	case ".webp":
		return "image/webp"
	case ".ico":
		return "image/x-icon"
	case ".woff":
		return "font/woff"
	case ".woff2":
		return "font/woff2"
	default:
		return ""
	}
}
