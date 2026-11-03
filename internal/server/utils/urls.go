package utils

import (
	"GoTodo/internal/config"
	"fmt"
	"net/http"
	"os"
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
	// A configured public origin wins over the request's Host header, which the
	// client controls: otherwise a forged Host could point a password-reset email
	// at an attacker's site.
	if public := PublicURL(); public != "" {
		return public + path
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

// PublicURL returns PUBLIC_URL (scheme, host and any BASE_PATH prefix) without a
// trailing slash, or "" when it isn't set to an absolute http(s) URL.
func PublicURL() string {
	u := strings.TrimSuffix(strings.TrimSpace(os.Getenv("PUBLIC_URL")), "/")
	if !strings.HasPrefix(u, "https://") && !strings.HasPrefix(u, "http://") {
		return ""
	}
	return u
}

// TrimPublicPrefix removes BASE_PATH from an incoming request path.
func TrimPublicPrefix(path string) string {
	base := strings.TrimSuffix(GetBasePath(), "/")
	if base != "" && base != "/" && strings.HasPrefix(path, base) {
		return strings.TrimPrefix(path, base)
	}
	return path
}
