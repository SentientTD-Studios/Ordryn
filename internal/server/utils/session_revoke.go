package utils

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net/http"
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
)

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
	return &uid
}
