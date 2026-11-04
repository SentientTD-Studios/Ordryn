package utils

import (
	"net/http"
	"net/url"
	"strings"
)

// RejectCrossSite blocks state-changing API requests that a browser sent from
// another site. Session cookies are SameSite=Lax, which still lets a sibling
// subdomain post forms, and doesn't stop login CSRF (signing a victim into the
// attacker's account). This closes both for JSON, form and file-upload requests.
//
// Browsers mark every request with Sec-Fetch-Site; when it's missing (very old
// browsers) the Origin header is compared with the request's host and PUBLIC_URL.
// Requests with neither header aren't from a browser page (curl, the CLI,
// webhooks) and pass, as do Bearer-authenticated requests, which carry no cookie.
func RejectCrossSite(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if isStateChanging(r.Method) && extractBearerToken(r) == "" && isCrossSiteRequest(r) {
			APIJSONError(w, http.StatusForbidden, "cross_site_request",
				"This request came from another website and was blocked.")
			return
		}
		next(w, r)
	}
}

func isStateChanging(method string) bool {
	switch method {
	case http.MethodGet, http.MethodHead, http.MethodOptions:
		return false
	}
	return true
}

func isCrossSiteRequest(r *http.Request) bool {
	switch strings.ToLower(strings.TrimSpace(r.Header.Get("Sec-Fetch-Site"))) {
	case "same-origin", "none":
		// "none" is a request the user started directly (address bar, bookmark).
		return false
	case "":
		// Not sent: fall back to Origin below.
	default:
		// "same-site" (a sibling subdomain) or "cross-site".
		return true
	}

	origin := strings.TrimSpace(r.Header.Get("Origin"))
	if origin == "" {
		return false
	}
	if origin == "null" {
		// Sandboxed frames and some redirects: never our own page.
		return true
	}
	u, err := url.Parse(origin)
	if err != nil || u.Host == "" {
		return true
	}
	originHost := hostWithoutDefaultPort(u.Scheme, u.Host)

	// The host the browser used: the request's own Host, or, behind a trusted
	// proxy that rewrites Host (nginx does by default), the forwarded one.
	candidates := []string{r.Host}
	if PeerIsTrustedProxy(r) {
		if fwd := strings.TrimSpace(strings.Split(r.Header.Get("X-Forwarded-Host"), ",")[0]); fwd != "" {
			candidates = append(candidates, fwd)
		}
	}
	for _, h := range candidates {
		if strings.EqualFold(originHost, hostWithoutDefaultPort(u.Scheme, h)) {
			return false
		}
	}
	if public := PublicURL(); public != "" {
		if pu, err := url.Parse(public); err == nil && strings.EqualFold(u.Scheme, pu.Scheme) &&
			strings.EqualFold(originHost, hostWithoutDefaultPort(pu.Scheme, pu.Host)) {
			return false
		}
	}
	return true
}

// hostWithoutDefaultPort drops :443 from https hosts and :80 from http hosts, so
// "example.com" and "example.com:443" compare equal.
func hostWithoutDefaultPort(scheme, host string) string {
	switch strings.ToLower(scheme) {
	case "https":
		return strings.TrimSuffix(host, ":443")
	case "http":
		return strings.TrimSuffix(host, ":80")
	}
	return host
}
