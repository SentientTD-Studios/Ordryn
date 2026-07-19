package utils

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
)

// tokenBucketScript implements a simple token-bucket using Redis hashes.
const tokenBucketScript = `local key = KEYS[1]
local now = tonumber(ARGV[1])
local refill_rate = tonumber(ARGV[2])
local capacity = tonumber(ARGV[3])
local requested = tonumber(ARGV[4])
local ttl = tonumber(ARGV[5])

local data = redis.call("HMGET", key, "tokens", "last")
local tokens = data[1]
local last = data[2]
if tokens == false or tokens == nil then
  tokens = capacity
  last = now
else
  tokens = tonumber(tokens)
  last = tonumber(last)
end
local delta = math.max(0, now - last)
local add = delta * refill_rate
tokens = math.min(capacity, tokens + add)
if tokens < requested then
  redis.call("HMSET", key, "tokens", tokens, "last", now)
  redis.call("EXPIRE", key, ttl)
  return 0
else
  tokens = tokens - requested
  redis.call("HMSET", key, "tokens", tokens, "last", now)
  redis.call("EXPIRE", key, ttl)
  return 1
end`

// AllowRequest uses the Redis-backed token bucket to decide whether the request
// identified by key is allowed. Returns true if allowed.
func AllowRequest(ctx context.Context, client *redis.Client, key string, capacity int, refillRate float64, requested int, ttlSeconds int) (bool, error) {
	if client == nil {
		// No Redis configured; allow (fail-open)
		return true, nil
	}

	now := time.Now().Unix()
	res, err := client.Eval(ctx, tokenBucketScript, []string{key}, now, refillRate, capacity, requested, ttlSeconds).Result()
	if err != nil {
		return false, fmt.Errorf("redis eval error: %w", err)
	}
	allowed, ok := res.(int64)
	if !ok {
		return false, fmt.Errorf("unexpected redis response: %T", res)
	}
	return allowed == 1, nil
}

// RateLimitMiddleware returns a middleware that enforces a token-bucket limit.
// - capacity: bucket capacity
// - refillRate: tokens per second
// - ttlSeconds: Redis key TTL
// - keyFunc: returns a stable key for the request (e.g. per-IP or per-account)
func RateLimitMiddleware(capacity int, refillRate float64, ttlSeconds int, keyFunc func(*http.Request) string) func(http.HandlerFunc) http.HandlerFunc {
	return func(next http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			key := keyFunc(r)
			if key == "" {
				next(w, r)
				return
			}

			// Prefix to keep rate-limit keys separate
			key = "rl:tb:" + key

			allowed, err := AllowRequest(r.Context(), RedisClient, key, capacity, refillRate, 1, ttlSeconds)
			if err != nil {
				// On Redis error, fail-open but log via header
				w.Header().Set("X-RateLimit-Error", err.Error())
				next(w, r)
				return
			}
			if !allowed {
				if strings.Contains(r.URL.Path, "/api/v1/") {
					msg := "Too many requests; please try again later."
					if strings.Contains(r.URL.Path, "/auth/login") || strings.Contains(r.URL.Path, "/auth/register") {
						msg = "Too many login attempts; please try again later."
					}
					APIJSONError(w, http.StatusTooManyRequests, "rate_limit_exceeded", msg)
					return
				}

				// Legacy HTMX: return 200 so HTMX will swap the response.
				if r.Header.Get("HX-Request") == "true" {
					msg := "Too many requests"
					if strings.HasSuffix(r.URL.Path, "/api/login") {
						msg = "Too many login attempts; please try again later"
						w.Header().Set("HX-Retarget", "#login-error")
						w.Header().Set("HX-Reswap", "innerHTML")
					}
					w.WriteHeader(http.StatusOK)
					fmt.Fprint(w, msg)
					return
				}

				http.Error(w, "Too many requests", http.StatusTooManyRequests)
				return
			}
			next(w, r)
		}
	}
}

// KeyByIP extracts a client IP for rate limiting. Uses X-Forwarded-For when present.
func KeyByIP(r *http.Request) string {
	xff := r.Header.Get("X-Forwarded-For")
	if xff != "" {
		// X-Forwarded-For may be a comma-separated list
		parts := strings.Split(xff, ",")
		ip := strings.TrimSpace(parts[0])
		return ip
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// KeyByEmail reads form value `email` if present and returns a normalized key.
func KeyByEmail(r *http.Request) string {
	// ParseForm is idempotent; handlers may also call it.
	_ = r.ParseForm()
	email := strings.TrimSpace(r.FormValue("email"))
	if email == "" {
		return ""
	}
	return "email:" + strings.ToLower(email)
}

// KeyByUser uses session user id/email when available.
func KeyByUser(r *http.Request) string {
	email, _, _, loggedIn := GetSessionUser(r)
	if !loggedIn {
		// Fall back to IP
		return KeyByIP(r)
	}
	return "user:" + strings.ToLower(email)
}

// IncrementFailedLogin increments a per-email failed-login counter with TTL (windowSeconds).
// Returns the current count after incrementing.
func IncrementFailedLogin(ctx context.Context, email string, windowSeconds int) (int64, error) {
	if RedisClient == nil {
		return 0, nil
	}
	if email == "" {
		return 0, nil
	}
	key := "rl:fail:email:" + strings.ToLower(email)
	val, err := RedisClient.Incr(ctx, key).Result()
	if err != nil {
		return 0, err
	}
	if val == 1 {
		// first failure, set TTL
		_, _ = RedisClient.Expire(ctx, key, time.Duration(windowSeconds)*time.Second).Result()
	}
	return val, nil
}

// IsLoginBlocked checks whether the failed-login counter for email has reached threshold.
func IsLoginBlocked(ctx context.Context, email string, threshold int64) (bool, error) {
	if RedisClient == nil {
		return false, nil
	}
	if email == "" {
		return false, nil
	}
	key := "rl:fail:email:" + strings.ToLower(email)
	val, err := RedisClient.Get(ctx, key).Int64()
	if err != nil {
		if err == redis.Nil {
			return false, nil
		}
		return false, err
	}
	return val >= threshold, nil
}

// ClearFailedLogin removes the failed-login counter for the given email.
func ClearFailedLogin(ctx context.Context, email string) error {
	if RedisClient == nil {
		return nil
	}
	if email == "" {
		return nil
	}
	key := "rl:fail:email:" + strings.ToLower(email)
	_, err := RedisClient.Del(ctx, key).Result()
	return err
}
