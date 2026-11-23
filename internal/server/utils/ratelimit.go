package utils

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"GoTodo/internal/statestore"
)

// AllowRequest takes one token from the bucket named key (capacity tokens,
// refilling at refillRate per second, forgotten after ttlSeconds idle) and
// reports whether one was available. Without a state store (unit tests) it
// allows everything.
func AllowRequest(ctx context.Context, key string, capacity int, refillRate float64, ttlSeconds int) (bool, error) {
	store := stateStore()
	if store == nil {
		return true, nil
	}
	return store.Allow(ctx, key, capacity, refillRate, time.Duration(ttlSeconds)*time.Second)
}

// RateLimitMiddleware returns a middleware that enforces a token-bucket limit.
// - capacity: bucket capacity
// - refillRate: tokens per second
// - ttlSeconds: how long an idle bucket is remembered
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

			allowed, err := AllowRequest(r.Context(), key, capacity, refillRate, ttlSeconds)
			if err != nil {
				// On a state store error, fail open but say so in a header
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
// state store calls ignore cancellation of ctx.
func CheckLogin(ctx context.Context, email, ip string) (LoginGate, error) {
	store := stateStore()
	if store == nil {
		return LoginAllowed, nil
	}
	ctx = context.WithoutCancel(ctx)
	ipOverLimit := false
	for _, c := range loginFailureCounters(email, ip) {
		raw, err := store.Get(ctx, c.key)
		if errors.Is(err, statestore.ErrNotFound) {
			continue
		}
		if err != nil {
			return LoginAllowed, err
		}
		val, err := strconv.ParseInt(string(raw), 10, 64)
		if err != nil {
			return LoginAllowed, fmt.Errorf("login counter %s: %w", c.key, err)
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
		allowed, err := AllowRequest(ctx, "rl:tb:login-slow:"+ip,
			1, 1.0/LoginSlowdownSeconds, LoginFailureWindowSeconds)
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
	store := stateStore()
	if store == nil {
		return nil
	}
	ctx = context.WithoutCancel(ctx)
	for _, c := range loginFailureCounters(email, ip) {
		if _, err := store.Incr(ctx, c.key, LoginFailureWindowSeconds*time.Second); err != nil {
			return err
		}
	}
	return nil
}

// ClearFailedLogin resets the email-and-IP counter after a successful sign-in.
// The per-email and per-IP counters run out on their own, so a successful
// sign-in doesn't reset the budget of someone guessing from elsewhere.
func ClearFailedLogin(ctx context.Context, email, ip string) error {
	store := stateStore()
	if store == nil {
		return nil
	}
	counters := loginFailureCounters(email, ip)
	if len(counters) == 0 || !strings.HasPrefix(counters[0].key, "rl:fail:email-ip:") {
		return nil
	}
	return store.Del(context.WithoutCancel(ctx), counters[0].key)
}
