package utils

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"time"

	"GoTodo/internal/sessionstore"
	"GoTodo/internal/storage"

	"github.com/gorilla/sessions"
)

// Session keys that tie a cookie to server-side state.
const (
	// SessionIDKey is a random id for this one sign-in, used to revoke it on logout.
	SessionIDKey = "sid"
	// SessionVersionKey is users.session_version at sign-in.
	SessionVersionKey = "session_version"
	// SessionSignedInAtKey is when this sign-in happened (unix seconds), for the
	// security policy's maximum session length.
	SessionSignedInAtKey = "signed_in_at"
	// SessionLastSeenKey is the last request time (unix seconds, refreshed at most
	// once a minute), for the idle timeout.
	SessionLastSeenKey = "last_seen_at"
	// SessionAuthMethodKey is "" for a password sign-in or AuthMethodSSO.
	SessionAuthMethodKey = "auth_method"
)

// AuthMethodSSO marks sessions started through single sign-on. The identity
// provider handled the second factor, so the local MFA requirement doesn't apply.
const AuthMethodSSO = "sso"

// AuthMethodPasskey marks sessions started with a passkey. The authenticator checked
// the person's PIN or biometric, so the local MFA requirement doesn't apply either.
const AuthMethodPasskey = "passkey"

// lastSeenRefresh limits how often an active session's cookie is rewritten.
const lastSeenRefresh = time.Minute

// sessionExpiredByPolicy applies the idle and maximum session lengths, refreshing
// the last-seen time on active sessions. Cookies from before these keys existed
// start their clocks now.
func sessionExpiredByPolicy(w http.ResponseWriter, r *http.Request, sess *sessions.Session) bool {
	p := storage.GetSecurityPolicy()
	now := time.Now()
	signedIn, ok := sessionAsInt64(sess.Values[SessionSignedInAtKey])
	if !ok || signedIn <= 0 {
		signedIn = now.Unix()
		sess.Values[SessionSignedInAtKey] = signedIn
	}
	lastSeen, ok := sessionAsInt64(sess.Values[SessionLastSeenKey])
	if !ok || lastSeen <= 0 {
		lastSeen = now.Unix()
		sess.Values[SessionLastSeenKey] = int64(0) // force a save below
	}
	if p.SessionMaxHours > 0 && now.Sub(time.Unix(signedIn, 0)) > time.Duration(p.SessionMaxHours)*time.Hour {
		return true
	}
	if p.SessionIdleMinutes > 0 && now.Sub(time.Unix(lastSeen, 0)) > time.Duration(p.SessionIdleMinutes)*time.Minute {
		return true
	}
	if seen, _ := sessionAsInt64(sess.Values[SessionLastSeenKey]); now.Sub(time.Unix(seen, 0)) >= lastSeenRefresh {
		sess.Values[SessionLastSeenKey] = now.Unix()
		if err := sess.Save(r, w); err != nil {
			fmt.Printf("session last-seen save: %v\n", err)
		}
	}
	return false
}

// SessionSignedInWithSSO reports whether this browser's session came from single sign-on.
func SessionSignedInWithSSO(r *http.Request) bool {
	sess, err := sessionstore.Store.Get(r, "session")
	if err != nil || sess == nil {
		return false
	}
	return SessionStringValue(sess, SessionAuthMethodKey) == AuthMethodSSO
}

// SessionSignedInWithPasskey reports whether this session started with a passkey.
func SessionSignedInWithPasskey(r *http.Request) bool {
	sess, err := sessionstore.Store.Get(r, "session")
	if err != nil || sess == nil {
		return false
	}
	return SessionStringValue(sess, SessionAuthMethodKey) == AuthMethodPasskey
}

// ExtractBearerTokenForHandlers returns the request's API key, or "" for a cookie session.
func ExtractBearerTokenForHandlers(r *http.Request) string {
	return extractBearerToken(r)
}

// NewSessionID returns a random id for a new sign-in.
func NewSessionID() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func revokedSessionKey(sid string) string {
	return "sess:revoked:" + sid
}

// RevokeSessionID marks one sign-in as signed out until its cookie would have
// expired anyway. Without Redis it does nothing; the cookie is still cleared.
func RevokeSessionID(ctx context.Context, sid string) error {
	if RedisClient == nil || sid == "" {
		return nil
	}
	return RedisClient.Set(ctx, revokedSessionKey(sid), 1, time.Duration(SessionMaxAge())*time.Second).Err()
}

func isSessionIDRevoked(ctx context.Context, sid string) (bool, error) {
	if RedisClient == nil || sid == "" {
		return false, nil
	}
	n, err := RedisClient.Exists(ctx, revokedSessionKey(sid)).Result()
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

// SessionStringValue reads a string session value, or "".
func SessionStringValue(sess *sessions.Session, key string) string {
	if sess == nil {
		return ""
	}
	s, _ := sess.Values[key].(string)
	return s
}

// sessionVersionOf returns the version recorded in the cookie. Cookies issued
// before session versions existed have none and count as version 1, the column's
// starting value, so they keep working until the account's version is first raised.
func sessionVersionOf(sess *sessions.Session) int {
	if v, ok := sessionAsInt(sess.Values[SessionVersionKey]); ok {
		return v
	}
	return 1
}

// sessionStateLookup is storage.GetSessionState; tests replace it.
var sessionStateLookup = storage.GetSessionState

// activeSessionUserID returns the session's user id, or nil when there is no
// session or it is no longer valid: the account was deactivated, its sessions
// were signed out (password change or reset, "sign out other devices"), or this
// sign-in was logged out. Invalid cookies are cleared. A database error rejects
// the request but keeps the cookie, so a brief outage doesn't sign everyone out.
func activeSessionUserID(w http.ResponseWriter, r *http.Request) *int {
	sess, err := sessionstore.Store.Get(r, "session")
	if err != nil || sess == nil {
		return nil
	}
	uid, ok := sessionAsInt(sess.Values["user_id"])
	if !ok || uid <= 0 {
		return nil
	}

	state, err := sessionStateLookup(uid)
	if err != nil {
		fmt.Printf("session check for user %d: %v\n", uid, err)
		return nil
	}
	if !state.Active || state.Version != sessionVersionOf(sess) {
		sessionstore.ClearSessionCookie(w, r)
		return nil
	}

	revoked, err := isSessionIDRevoked(r.Context(), SessionStringValue(sess, SessionIDKey))
	if err != nil {
		fmt.Printf("session revocation check: %v\n", err)
		return nil
	}
	if revoked {
		sessionstore.ClearSessionCookie(w, r)
		return nil
	}
	if sessionExpiredByPolicy(w, r, sess) {
		_ = RevokeSessionID(r.Context(), SessionStringValue(sess, SessionIDKey))
		sessionstore.ClearSessionCookie(w, r)
		return nil
	}
	return &uid
}

// mfaEnrollmentPaths stay open to people the policy requires to set up MFA, so
// they can load their profile and enroll.
var mfaEnrollmentPaths = []string{"/me", "/me/mfa", "/me/mfa/setup", "/me/mfa/enable", "/me/mfa/recovery-codes", "/me/passkeys", "/me/passkeys/register/begin", "/me/passkeys/register/finish"}

// apiPathSuffix is the path after /api/v2 or /api/v1 (and BASE_PATH), e.g. "/me/mfa".
func apiPathSuffix(r *http.Request) string {
	path := r.URL.Path
	if base := strings.TrimSuffix(GetBasePath(), "/"); base != "" && !strings.Contains(base, "://") {
		path = strings.TrimPrefix(path, base)
	}
	for _, ver := range []string{"/api/v2", "/api/v1"} {
		if strings.HasPrefix(path, ver+"/") {
			return strings.TrimSuffix(strings.TrimPrefix(path, ver), "/")
		}
	}
	return path
}

// mfaEnrollmentBlocks writes 403 mfa_enrollment_required and returns true when
// the security policy requires uid to set up MFA and this request isn't one of
// the enrollment endpoints. Single sign-on sessions are exempt.
func mfaEnrollmentBlocks(w http.ResponseWriter, r *http.Request, uid int) bool {
	if storage.GetSecurityPolicy().RequireMFA == storage.MFARequireOff {
		return false
	}
	if slices.Contains(mfaEnrollmentPaths, apiPathSuffix(r)) || SessionSignedInWithSSO(r) || SessionSignedInWithPasskey(r) {
		return false
	}
	if !storage.MFAEnrollmentRequired(uid) {
		return false
	}
	APIJSONError(w, http.StatusForbidden, "mfa_enrollment_required",
		"This site requires two-factor authentication. Set it up in your profile to continue.")
	return true
}
