package utils

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
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
				if strings.Contains(r.URL.Path, "/api/v2/") || strings.Contains(r.URL.Path, "/api/v1/") {
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

// KeyByIP keys rate limits by client IP. See ClientIP for how proxies are handled.
func KeyByIP(r *http.Request) string {
	return ClientIP(r)
}

// KeyByIPAndBearer rate-limits public callback endpoints per IP plus token hash.
func KeyByIPAndBearer(r *http.Request) string {
	ip := KeyByIP(r)
	auth := strings.TrimSpace(r.Header.Get("Authorization"))
	if len(auth) < 8 || !strings.EqualFold(auth[:7], "Bearer ") {
		return ip
	}
	tok := strings.TrimSpace(auth[7:])
	if tok == "" {
		return ip
	}
	sum := sha256.Sum256([]byte(tok))
	return ip + ":" + hex.EncodeToString(sum[:8])
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

// Failed sign-in limits. Each counts failures over LoginFailureWindowSeconds.
const (
	// LoginFailuresPerEmailAndIP locks one email out from one IP. Someone guessing
	// from elsewhere can't lock the real owner out this way.
	LoginFailuresPerEmailAndIP = 5
	// LoginFailuresPerEmail locks the email out everywhere: the backstop for
	// guesses spread over many IPs.
	LoginFailuresPerEmail = 10
	// LoginFailuresPerIP slows down (doesn't block) an IP that fails this often
	// across any accounts. Many people can share one IP (an office, a carrier NAT),
	// so a hard block would lock them all out over someone else's typos.
	LoginFailuresPerIP = 20
	// LoginFailureWindowSeconds is how long failures are remembered.
	LoginFailureWindowSeconds = 900
	// LoginSlowdownSeconds is the gap between attempts from a slowed-down IP.
	LoginSlowdownSeconds = 5
)

// LoginGate is the result of checking whether a sign-in attempt may proceed.
type LoginGate int

const (
	// LoginAllowed means go ahead and check the password.
	LoginAllowed LoginGate = iota
	// LoginLocked means the email is locked out for now.
	LoginLocked
	// LoginSlowedDown means the IP has to wait a few seconds between attempts.
	LoginSlowedDown
)

type failureCounter struct {
	key   string
	limit int64
}

// loginFailureCounters returns the counters a sign-in attempt for email from ip
// is checked against. Counters with an empty part are skipped.
func loginFailureCounters(email, ip string) []failureCounter {
	email = strings.ToLower(strings.TrimSpace(email))
	var out []failureCounter
	if email != "" && ip != "" {
		out = append(out, failureCounter{"rl:fail:email-ip:" + email + "|" + ip, LoginFailuresPerEmailAndIP})
	}
	if email != "" {
		out = append(out, failureCounter{"rl:fail:email:" + email, LoginFailuresPerEmail})
	}
	if ip != "" {
		out = append(out, failureCounter{"rl:fail:ip:" + ip, LoginFailuresPerIP})
	}
	return out
}

// CheckLogin decides whether a sign-in attempt for email from ip may proceed.
// Lockout state must not depend on whether the client stays connected, so the
// Redis calls ignore cancellation of ctx.
func CheckLogin(ctx context.Context, email, ip string) (LoginGate, error) {
	if RedisClient == nil {
		return LoginAllowed, nil
	}
	ctx = context.WithoutCancel(ctx)
	ipOverLimit := false
	for _, c := range loginFailureCounters(email, ip) {
		val, err := RedisClient.Get(ctx, c.key).Int64()
		if err == redis.Nil {
			continue
		}
		if err != nil {
			return LoginAllowed, err
		}
		if val < c.limit {
			continue
		}
		if strings.HasPrefix(c.key, "rl:fail:ip:") {
			ipOverLimit = true
			continue
		}
		return LoginLocked, nil
	}
	if ipOverLimit {
		allowed, err := AllowRequest(ctx, RedisClient, "rl:tb:login-slow:"+ip,
			1, 1.0/LoginSlowdownSeconds, 1, LoginFailureWindowSeconds)
		if err != nil {
			return LoginAllowed, err
		}
		if !allowed {
			return LoginSlowedDown, nil
		}
	}
	return LoginAllowed, nil
}

// RecordFailedLogin counts a failed password or MFA code for email from ip.
func RecordFailedLogin(ctx context.Context, email, ip string) error {
	if RedisClient == nil {
		return nil
	}
	ctx = context.WithoutCancel(ctx)
	for _, c := range loginFailureCounters(email, ip) {
		val, err := RedisClient.Incr(ctx, c.key).Result()
		if err != nil {
			return err
		}
		if val == 1 {
			_, _ = RedisClient.Expire(ctx, c.key, LoginFailureWindowSeconds*time.Second).Result()
		}
	}
	return nil
}

// ClearFailedLogin resets the email-and-IP counter after a successful sign-in.
// The per-email and per-IP counters run out on their own, so a successful
// sign-in doesn't reset the budget of someone guessing from elsewhere.
func ClearFailedLogin(ctx context.Context, email, ip string) error {
	if RedisClient == nil {
		return nil
	}
	counters := loginFailureCounters(email, ip)
	if len(counters) == 0 || !strings.HasPrefix(counters[0].key, "rl:fail:email-ip:") {
		return nil
	}
	return RedisClient.Del(context.WithoutCancel(ctx), counters[0].key).Err()
}
