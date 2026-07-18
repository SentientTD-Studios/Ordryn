package server

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"GoTodo/internal/server/utils"
)

const spaDistDir = "web/dist"

// registerSPARoutes serves the Vue SPA from web/dist under /app/.
func registerSPARoutes() {
	info, err := os.Stat(spaDistDir)
	if err != nil || !info.IsDir() {
		http.HandleFunc("/app", spaMissingHandler)
		http.HandleFunc("/app/", spaMissingHandler)
		return
	}

	for _, prefix := range routePaths() {
		appPrefix := prefix + "/app"
		fileServer := http.StripPrefix(appPrefix+"/", http.FileServer(http.Dir(spaDistDir)))

		http.Handle(appPrefix+"/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			serveSPA(w, r, appPrefix, fileServer)
		}))
		http.HandleFunc(appPrefix, func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, appPrefix+"/", http.StatusTemporaryRedirect)
		})
	}
}

func spaRootRedirect(w http.ResponseWriter, r *http.Request) {
	base := strings.TrimSuffix(utils.GetBasePath(), "/")
	target := "/app/"
	if base != "" && base != "/" {
		target = base + "/app/"
	}
	http.Redirect(w, r, target, http.StatusTemporaryRedirect)
}

func serveSPA(w http.ResponseWriter, r *http.Request, mount string, fileServer http.Handler) {
	rel := strings.TrimPrefix(r.URL.Path, mount)
	rel = strings.TrimPrefix(rel, "/")
	if rel == "" {
		http.ServeFile(w, r, filepath.Join(spaDistDir, "index.html"))
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

	http.ServeFile(w, r, filepath.Join(spaDistDir, "index.html"))
}

func spaMissingHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusServiceUnavailable)
	_, _ = w.Write([]byte("SPA build missing. Run: cd web && npm ci && npm run build\n"))
}
