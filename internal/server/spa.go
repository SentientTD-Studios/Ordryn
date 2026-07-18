package server

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"GoTodo/internal/server/utils"
)

const spaDistDir = "web/dist"

// registerSPARoutes serves the Vue SPA from web/dist at the configured BASE_PATH
// root (e.g. "/" or "/gotodo/"). Legacy "/app/" URLs redirect to the same mount.
func registerSPARoutes() {
	mount := utils.PublicPathPrefix() // "" or "/gotodo"
	mountSlash := "/"
	if mount != "" {
		mountSlash = mount + "/"
	}

	info, err := os.Stat(spaDistDir)
	if err != nil || !info.IsDir() {
		http.HandleFunc(mountSlash, spaMissingHandler)
		if mount != "" {
			http.HandleFunc(mount, spaMissingHandler)
		}
		registerLegacyAppRedirects()
		return
	}

	fileServer := http.StripPrefix(mountSlash, http.FileServer(http.Dir(spaDistDir)))
	http.Handle(mountSlash, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		serveSPA(w, r, mount, fileServer)
	}))
	if mount != "" {
		http.HandleFunc(mount, func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, mountSlash, http.StatusTemporaryRedirect)
		})
	}

	registerLegacyAppRedirects()
}

func registerLegacyAppRedirects() {
	for _, prefix := range routePaths() {
		appPrefix := prefix + "/app"
		http.HandleFunc(appPrefix, legacyAppRedirect)
		http.HandleFunc(appPrefix+"/", legacyAppRedirect)
	}
}

// legacyAppRedirect sends /app/... (and {BASE_PATH}/app/...) to the SPA mount.
func legacyAppRedirect(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path
	for _, prefix := range routePaths() {
		appPrefix := prefix + "/app"
		if path == appPrefix || strings.HasPrefix(path, appPrefix+"/") {
			rest := strings.TrimPrefix(path, appPrefix)
			if rest == "" {
				rest = "/"
			}
			target := utils.PublicPath(rest)
			if q := r.URL.RawQuery; q != "" {
				target += "?" + q
			}
			http.Redirect(w, r, target, http.StatusTemporaryRedirect)
			return
		}
	}
	http.NotFound(w, r)
}

func serveSPA(w http.ResponseWriter, r *http.Request, mount string, fileServer http.Handler) {
	rel := r.URL.Path
	if mount != "" {
		rel = strings.TrimPrefix(rel, mount)
	}
	rel = strings.TrimPrefix(rel, "/")
	if rel == "" || rel == "index.html" {
		serveSPAIndex(w, r)
		return
	}

	candidate := filepath.Join(spaDistDir, filepath.Clean("/"+rel))
	absDist, err := filepath.Abs(spaDistDir)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	absCandidate, err := filepath.Abs(candidate)
	if err != nil || (!strings.HasPrefix(absCandidate, absDist+string(os.PathSeparator)) && absCandidate != absDist) {
		http.NotFound(w, r)
		return
	}
	if st, err := os.Stat(candidate); err == nil && !st.IsDir() {
		fileServer.ServeHTTP(w, r)
		return
	}

	serveSPAIndex(w, r)
}

func serveSPAIndex(w http.ResponseWriter, r *http.Request) {
	raw, err := os.ReadFile(filepath.Join(spaDistDir, "index.html"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	base := utils.PublicPathPrefix()
	injectBase := "/"
	if base != "" {
		injectBase = base + "/"
	}
	inject := fmt.Sprintf(`<script>window.__GOTODO_BASE__=%q;</script>`, injectBase)
	html := string(raw)
	if strings.Contains(html, "<head>") {
		html = strings.Replace(html, "<head>", "<head>"+inject, 1)
	} else {
		html = inject + html
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(html))
}

func spaMissingHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusServiceUnavailable)
	_, _ = w.Write([]byte("SPA build missing. Run: cd web && npm ci && npm run build\n"))
}
