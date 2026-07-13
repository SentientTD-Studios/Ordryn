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

// RequireAPIEnabled ensures the site has the REST API enabled.
func RequireAPIEnabled(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !IsAPIEnabled() {
			APIJSONError(w, http.StatusForbidden, "api_disabled",
				"The REST API is disabled. An administrator can enable it in site settings.")
			return
		}
		next(w, r)
	}
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
		*r = *SetAPIUserID(r, userID)
		next(w, r)
	}
}

// APIRateLimitMiddleware enforces per-user token bucket limits for the REST API.
// Fails closed when Redis is unavailable.
func APIRateLimitMiddleware(readCapacity int, readRefill float64, writeCapacity int, writeRefill float64, ttlSeconds int) func(http.HandlerFunc) http.HandlerFunc {
	return func(next http.HandlerFunc) http.HandlerFunc {
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

			capacity := readCapacity
			refill := readRefill
			if isWriteMethod(r.Method) {
				capacity = writeCapacity
				refill = writeRefill
			}

			key := "rl:tb:api:user:" + strconv.Itoa(userID)
			allowed, err := AllowRequest(r.Context(), RedisClient, key, capacity, refill, 1, ttlSeconds)
			if err != nil {
				APIJSONError(w, http.StatusServiceUnavailable, "api_unavailable",
					"Rate limiting is temporarily unavailable.")
				return
			}
			if !allowed {
				retryAfter := int(float64(capacity) / refill)
				if retryAfter < 1 {
					retryAfter = 1
				}
				w.Header().Set("Retry-After", strconv.Itoa(retryAfter))
				APIJSONError(w, http.StatusTooManyRequests, "rate_limit_exceeded",
					"API rate limit exceeded. Try again later.")
				return
			}
			next(w, r)
		}
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

// APIChain composes the standard /api/v1 middleware chain.
func APIChain(handler http.HandlerFunc) http.HandlerFunc {
	chain := RequireAPIEnabled(
		RequireAPIRedis(
			RequireAPIKey(
				APIRateLimitMiddleware(120, 2.0, 60, 1.0, 120)(handler),
			),
		),
	)
	return chain
}

// ParseAPIV1Subpath returns the path segment after /api/v1/<resource>/.
func ParseAPIV1Subpath(r *http.Request, resource string) string {
	base := strings.TrimSuffix(GetBasePath(), "/")
	path := r.URL.Path
	if base != "" && base != "/" {
		path = strings.TrimPrefix(path, base)
	}
	prefix := "/api/v1/" + resource + "/"
	if strings.HasPrefix(path, prefix) {
		return strings.Trim(strings.TrimPrefix(path, prefix), "/")
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
