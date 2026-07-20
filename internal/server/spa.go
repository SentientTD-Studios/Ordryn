package server

import (
	"fmt"
	"html"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"GoTodo/internal/server/utils"
	"GoTodo/internal/storage"
)

const spaDistDir = "web/dist"

// spaServeMounts returns path prefixes where the SPA is attached on the Go server.
// Always includes "" (site root) so nginx configs that strip BASE_PATH keep working
// (browser /gotodo/ → backend /). Also includes PublicPathPrefix when set, for
// proxies that preserve the prefix.
func spaServeMounts() []string {
	mounts := []string{""}
	if p := utils.PublicPathPrefix(); p != "" {
		mounts = append(mounts, p)
	}
	return mounts
}

// registerSPARoutes serves the Vue SPA from web/dist.
// Public URLs still use BASE_PATH (injected into index.html); the listen paths
// support both strip-prefix and preserve-prefix reverse proxies.
func registerSPARoutes() {
	info, err := os.Stat(spaDistDir)
	missing := err != nil || !info.IsDir()

	for _, mount := range spaServeMounts() {
		mountSlash := "/"
		if mount != "" {
			mountSlash = mount + "/"
		}
		if missing {
			http.HandleFunc(mountSlash, spaMissingHandler)
			if mount != "" {
				http.HandleFunc(mount, spaMissingHandler)
			}
			continue
		}

		fileServer := http.StripPrefix(mountSlash, http.FileServer(http.Dir(spaDistDir)))
		m := mount
		http.Handle(mountSlash, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			serveSPA(w, r, m, fileServer)
		}))
		if mount != "" {
			http.HandleFunc(mount, func(w http.ResponseWriter, r *http.Request) {
				http.Redirect(w, r, mountSlash, http.StatusTemporaryRedirect)
			})
		}
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

// legacyAppRedirect sends /app/... (and {BASE_PATH}/app/...) to the public SPA path.
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
	escaped := htmlAttrEscape(injectBase)
	// Meta for JS pathPrefix. Do NOT inject <base href>: it breaks in-page anchors
	// (href="#x" → {BASE}/#x, dropping /docs/api/v1). Rewrite Vite's relative
	// ./assets URLs to absolute paths under the public prefix instead.
	inject := fmt.Sprintf(`<meta name="gotodo-base" content="%s">`, escaped)
	siteName := spaSiteName()
	inject += fmt.Sprintf(`<meta name="gotodo-site-name" content="%s">`, htmlAttrEscape(siteName))
	page := string(raw)
	if strings.Contains(page, "<head>") {
		page = strings.Replace(page, "<head>", "<head>"+inject, 1)
	} else {
		page = inject + page
	}
	page = replaceHTMLTitle(page, siteName)
	page = absolutizeRelativeAssetURLs(page, injectBase)
	page = nonceInlineScripts(page, utils.GetCSPNonce(r))
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	// Avoid caching a stale branded title after admin renames the site.
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write([]byte(page))
}

func spaSiteName() string {
	const fallback = "GoTodo"
	s, err := storage.GetSiteSettings()
	if err != nil || s == nil {
		return fallback
	}
	if name := strings.TrimSpace(s.SiteName); name != "" {
		return name
	}
	return fallback
}

// replaceHTMLTitle sets the document <title>, creating one when missing.
func replaceHTMLTitle(page, title string) string {
	escaped := html.EscapeString(title)
	const open, close = "<title>", "</title>"
	if i := strings.Index(strings.ToLower(page), open); i >= 0 {
		rest := page[i+len(open):]
		if j := strings.Index(strings.ToLower(rest), close); j >= 0 {
			return page[:i] + open + escaped + close + rest[j+len(close):]
		}
	}
	if i := strings.Index(strings.ToLower(page), "<head>"); i >= 0 {
		insertAt := i + len("<head>")
		return page[:insertAt] + open + escaped + close + page[insertAt:]
	}
	return open + escaped + close + page
}

// absolutizeRelativeAssetURLs rewrites Vite "./…" asset refs so nested routes
// (e.g. /auth/device, /docs/api/v1) still load JS/CSS from the SPA mount.
func absolutizeRelativeAssetURLs(html, base string) string {
	if base == "" {
		base = "/"
	}
	if !strings.HasSuffix(base, "/") {
		base += "/"
	}
	replacer := strings.NewReplacer(
		`src="./`, `src="`+base,
		`href="./`, `href="`+base,
		`src='./`, `src='`+base,
		`href='./`, `href='`+base,
	)
	return replacer.Replace(html)
}

func htmlAttrEscape(s string) string {
	s = strings.ReplaceAll(s, `&`, "&amp;")
	s = strings.ReplaceAll(s, `"`, "&quot;")
	s = strings.ReplaceAll(s, `<`, "&lt;")
	return s
}

// nonceInlineScripts adds the request CSP nonce to inline <script> tags (no src=).
func nonceInlineScripts(html, nonce string) string {
	if nonce == "" {
		return html
	}
	var b strings.Builder
	rest := html
	for {
		i := strings.Index(rest, "<script")
		if i < 0 {
			b.WriteString(rest)
			break
		}
		b.WriteString(rest[:i])
		rest = rest[i:]
		end := strings.Index(rest, ">")
		if end < 0 {
			b.WriteString(rest)
			break
		}
		open := rest[:end+1]
		rest = rest[end+1:]
		lower := strings.ToLower(open)
		if strings.Contains(lower, "src=") || strings.Contains(lower, "nonce=") {
			b.WriteString(open)
			continue
		}
		b.WriteString(open[:len(open)-1])
		fmt.Fprintf(&b, ` nonce="%s">`, nonce)
	}
	return b.String()
}

func spaMissingHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusServiceUnavailable)
	_, _ = w.Write([]byte("SPA build missing. Run: cd web && npm ci && npm run build\n"))
}
