package utils

import (
	"GoTodo/internal/config"
	"fmt"
	"net/http"
	"strings"
)

// RequestScheme returns the external request scheme, honoring proxy and config.
func RequestScheme(r *http.Request) string {
	if config.Cfg.UseHTTPS {
		return "https"
	}
	if r.TLS != nil {
		return "https"
	}
	if proto := r.Header.Get("X-Forwarded-Proto"); proto != "" {
		return strings.TrimSpace(strings.Split(proto, ",")[0])
	}
	return "http"
}

// AbsoluteURLForRequest builds an external URL for an app-relative path.
// Path must start with "/" (e.g. "/auth/device"). When BASE_PATH is configured
// as a full URL (legacy/misconfiguration), the path is appended to that base
// instead of scheme://host+basePath to avoid duplicated hosts.
func AbsoluteURLForRequest(r *http.Request, path string) string {
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	base := GetBasePath()
	if strings.Contains(base, "://") {
		return strings.TrimSuffix(base, "/") + path
	}
	if base == "" || base == "/" {
		return fmt.Sprintf("%s://%s%s", RequestScheme(r), r.Host, path)
	}
	return fmt.Sprintf("%s://%s%s%s", RequestScheme(r), r.Host, base, path)
}
