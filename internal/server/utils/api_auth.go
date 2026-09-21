package utils

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"GoTodo/internal/storage"
)

type apiUserContextKey struct{}
type apiAuthKindKey struct{}

const (
	// AuthKindSession is a first-party SPA session cookie.
	AuthKindSession = "session"
	// AuthKindAPIKey is an external Bearer API key.
	AuthKindAPIKey = "apikey"

	// External API token-bucket (per user, read and write are independent).
	APIReadCapacity  = 120
	APIReadRefill    = 2.0
	APIWriteCapacity = 60
	APIWriteRefill   = 1.0
	APITTLSeconds    = 120

	// SPA session token-bucket: same shape, higher ceilings so the UI can
	// move cards and edit boards without sharing the external API budget.
	SPAReadCapacity  = 600
	SPAReadRefill    = 10.0
	SPAWriteCapacity = 240
	SPAWriteRefill   = 4.0
	SPATTLSeconds    = 300
)

// RateLimitSpec is the Redis token-bucket configuration for one request.
type RateLimitSpec struct {
	Key      string
	Capacity int
	Refill   float64
	TTL      int
}

// APIJSONError writes a consistent JSON error response.
func APIJSONError(w http.ResponseWriter, status int, code, message string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{
		"error":   code,
		"message": message,
	})
}

// IsAPIEnabled returns whether the REST API is enabled in site settings.
func IsAPIEnabled() bool {
	s, err := storage.GetSiteSettings()
	if err != nil || s == nil {
		return false
	}
	return s.EnableAPI
}

// RedisAvailable reports whether a Redis client is connected.
func RedisAvailable() bool {
	return RedisClient != nil
}

// SetAPIUserID stores the authenticated API user on the request context.
func SetAPIUserID(r *http.Request, userID int) *http.Request {
	ctx := context.WithValue(r.Context(), apiUserContextKey{}, userID)
	return r.WithContext(ctx)
}

// SetAPIAuthKind records how the request was authenticated (session vs API key).
func SetAPIAuthKind(r *http.Request, kind string) *http.Request {
	ctx := context.WithValue(r.Context(), apiAuthKindKey{}, kind)
	return r.WithContext(ctx)
}

// GetAPIAuthKind returns the auth kind, defaulting to API key when unknown.
func GetAPIAuthKind(r *http.Request) string {
	v := r.Context().Value(apiAuthKindKey{})
	if kind, ok := v.(string); ok && kind != "" {
		return kind
	}
	return AuthKindAPIKey
}

// GetAPIUserID returns the authenticated API user from context.
func GetAPIUserID(r *http.Request) (int, bool) {
	v := r.Context().Value(apiUserContextKey{})
	if v == nil {
		return 0, false
	}
	id, ok := v.(int)
	return id, ok
}

func extractBearerToken(r *http.Request) string {
	auth := strings.TrimSpace(r.Header.Get("Authorization"))
	if auth == "" {
		return ""
	}
	const prefix = "Bearer "
	if !strings.HasPrefix(auth, prefix) {
		return ""
	}
	return strings.TrimSpace(strings.TrimPrefix(auth, prefix))
}

// RequireAPIEnabled ensures external REST API access is enabled (Bearer clients).
func RequireAPIEnabled(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if rejectExternalAPIIfDisabled(w) {
			return
		}
		next(w, r)
	}
}

func rejectExternalAPIIfDisabled(w http.ResponseWriter) bool {
	if IsAPIEnabled() {
		return false
	}
	APIJSONError(w, http.StatusForbidden, "api_disabled",
		"The REST API is disabled. An administrator can enable it in site settings.")
	return true
}

// RequireAPIRedis ensures Redis is available (fail closed for API).
func RequireAPIRedis(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !RedisAvailable() {
			APIJSONError(w, http.StatusServiceUnavailable, "api_unavailable",
				"The REST API requires Redis for authentication and rate limiting.")
			return
		}
		next(w, r)
	}
}

// RequireAPIKey validates Bearer token and attaches user ID to context.
func RequireAPIKey(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if rejectExternalAPIIfDisabled(w) {
			return
		}
		token := extractBearerToken(r)
		if token == "" {
			APIJSONError(w, http.StatusUnauthorized, "unauthorized",
				"Missing or invalid Authorization header. Use: Bearer <api_key>")
			return
		}
		userID, err := storage.LookupAPIKeyUserID(token)
		if err != nil {
			APIJSONError(w, http.StatusUnauthorized, "unauthorized",
				"Invalid or revoked API key.")
			return
		}
		*r = *SetAPIAuthKind(SetAPIUserID(r, userID), AuthKindAPIKey)
		next(w, r)
	}
}

// RateLimitSpecFor returns independent read/write buckets for SPA sessions vs API keys.
func RateLimitSpecFor(userID int, kind string, method string) RateLimitSpec {
	write := isWriteMethod(method)
	id := strconv.Itoa(userID)
	if kind == AuthKindSession {
		if write {
			return RateLimitSpec{
				Key: "rl:tb:spa:write:user:" + id, Capacity: SPAWriteCapacity,
				Refill: SPAWriteRefill, TTL: SPATTLSeconds,
			}
		}
		return RateLimitSpec{
			Key: "rl:tb:spa:read:user:" + id, Capacity: SPAReadCapacity,
			Refill: SPAReadRefill, TTL: SPATTLSeconds,
		}
	}
	if write {
		return RateLimitSpec{
			Key: "rl:tb:api:write:user:" + id, Capacity: APIWriteCapacity,
			Refill: APIWriteRefill, TTL: APITTLSeconds,
		}
	}
	return RateLimitSpec{
		Key: "rl:tb:api:read:user:" + id, Capacity: APIReadCapacity,
		Refill: APIReadRefill, TTL: APITTLSeconds,
	}
}

// APIRateLimitMiddleware enforces per-user token bucket limits.
// Session (SPA) traffic uses a higher independent budget than Bearer API keys.
// Read and write methods use separate Redis keys so writes cannot starve reads.
// Fails closed when Redis is unavailable.
func APIRateLimitMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID, ok := GetAPIUserID(r)
		if !ok {
			APIJSONError(w, http.StatusUnauthorized, "unauthorized", "Not authenticated.")
			return
		}
		if RedisClient == nil {
			APIJSONError(w, http.StatusServiceUnavailable, "api_unavailable",
				"The REST API requires Redis for rate limiting.")
			return
		}

		spec := RateLimitSpecFor(userID, GetAPIAuthKind(r), r.Method)
		allowed, err := AllowRequest(r.Context(), RedisClient, spec.Key, spec.Capacity, spec.Refill, 1, spec.TTL)
		if err != nil {
			APIJSONError(w, http.StatusServiceUnavailable, "api_unavailable",
				"Rate limiting is temporarily unavailable.")
			return
		}
		if !allowed {
			retryAfter := RetryAfterSeconds(spec.Capacity, spec.Refill)
			w.Header().Set("Retry-After", strconv.Itoa(retryAfter))
			msg := "API rate limit exceeded. Try again later."
			if GetAPIAuthKind(r) == AuthKindSession {
				msg = "Too many requests. Please wait a moment and try again."
			}
			APIJSONError(w, http.StatusTooManyRequests, "rate_limit_exceeded", msg)
			return
		}
		next(w, r)
	}
}

func isWriteMethod(method string) bool {
	switch method {
	case http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
		return true
	default:
		return false
	}
}

// APIChain composes the standard /api/v2 middleware chain.
// Accepts a session cookie (SPA) or Bearer API key; Redis is required for rate limiting.
func APIChain(handler http.HandlerFunc) http.HandlerFunc {
	return RequireAPIRedis(
		RequireSessionOrAPIKey(
			APIRateLimitMiddleware(handler),
		),
	)
}

// AuthPublicChain wraps JSON login/register (Redis + IP rate limit; no API key).
// SPA auth is not gated by site_settings.enable_api.
func AuthPublicChain(handler http.HandlerFunc) http.HandlerFunc {
	return RequireAPIRedis(
		RateLimitMiddleware(10, 1.0, 60, KeyByIP)(handler),
	)
}

// AuthMeChain allows unauthenticated GET /me (SPA session probe → 200 null).
// PATCH and other methods still require a session or API key.
func AuthMeChain(handler http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			if token := extractBearerToken(r); token != "" {
				if rejectExternalAPIIfDisabled(w) {
					return
				}
				if !RedisAvailable() {
					APIJSONError(w, http.StatusServiceUnavailable, "api_unavailable",
						"The REST API requires Redis for authentication and rate limiting.")
					return
				}
				userID, err := storage.LookupAPIKeyUserID(token)
				if err != nil {
					APIJSONError(w, http.StatusUnauthorized, "unauthorized",
						"Invalid or revoked API key.")
					return
				}
				*r = *SetAPIAuthKind(SetAPIUserID(r, userID), AuthKindAPIKey)
			} else if uid := GetSessionUserID(r); uid != nil {
				*r = *SetAPIAuthKind(SetAPIUserID(r, *uid), AuthKindSession)
			}
			handler(w, r)
			return
		}
		AuthSessionChain(handler)(w, r)
	}
}

// RequireSessionOrAPIKey accepts either a session cookie or Bearer API key and sets API user id.
func RequireSessionOrAPIKey(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		token := extractBearerToken(r)
		if token != "" {
			if rejectExternalAPIIfDisabled(w) {
				return
			}
			if !RedisAvailable() {
				APIJSONError(w, http.StatusServiceUnavailable, "api_unavailable",
					"The REST API requires Redis for authentication and rate limiting.")
				return
			}
			userID, err := storage.LookupAPIKeyUserID(token)
			if err != nil {
				APIJSONError(w, http.StatusUnauthorized, "unauthorized",
					"Invalid or revoked API key.")
				return
			}
			*r = *SetAPIAuthKind(SetAPIUserID(r, userID), AuthKindAPIKey)
			next(w, r)
			return
		}

		if uid := GetSessionUserID(r); uid != nil {
			*r = *SetAPIAuthKind(SetAPIUserID(r, *uid), AuthKindSession)
			next(w, r)
			return
		}

		APIJSONError(w, http.StatusUnauthorized, "unauthorized",
			"Not authenticated. Send a session cookie or Authorization: Bearer <api_key>.")
	}
}

// AuthSessionChain wraps endpoints that need a logged-in SPA session or API key.
func AuthSessionChain(handler http.HandlerFunc) http.HandlerFunc {
	return RequireSessionOrAPIKey(handler)
}

// ParseAPIV1Subpath returns the path segment after /api/v2/<resource>/ or
// the /api/v1 compatibility alias.
func ParseAPIV1Subpath(r *http.Request, resource string) string {
	base := strings.TrimSuffix(GetBasePath(), "/")
	path := r.URL.Path
	if base != "" && base != "/" {
		path = strings.TrimPrefix(path, base)
	}
	for _, ver := range []string{"/api/v2/", "/api/v1/"} {
		prefix := ver + resource + "/"
		if strings.HasPrefix(path, prefix) {
			return strings.Trim(strings.TrimPrefix(path, prefix), "/")
		}
	}
	return ""
}

// RetryAfterSeconds is a helper for tests.
func RetryAfterSeconds(capacity int, refill float64) int {
	sec := int(float64(capacity) / refill)
	if sec < 1 {
		return 1
	}
	return sec
}
