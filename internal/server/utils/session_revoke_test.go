package utils

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"GoTodo/internal/sessionstore"
	"GoTodo/internal/storage"
)

// requestWithSession returns a request carrying a signed session cookie with values.
func requestWithSession(t *testing.T, values map[interface{}]interface{}) *http.Request {
	t.Helper()
	seed := httptest.NewRequest(http.MethodGet, "/api/v2/me", nil)
	sess, err := sessionstore.GetSession(seed)
	if err != nil {
		t.Fatal(err)
	}
	sess.Values = values
	rec := httptest.NewRecorder()
	if err := sess.Save(seed, rec); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodGet, "/api/v2/me", nil)
	for _, c := range rec.Result().Cookies() {
		req.AddCookie(c)
	}
	return req
}

func stubSessionState(t *testing.T, state storage.SessionState, err error) {
	t.Helper()
	orig := sessionStateLookup
	sessionStateLookup = func(int) (storage.SessionState, error) { return state, err }
	t.Cleanup(func() { sessionStateLookup = orig })
}

func clearsCookie(rec *httptest.ResponseRecorder) bool {
	for _, h := range rec.Header().Values("Set-Cookie") {
		if strings.HasPrefix(h, "session=") && strings.Contains(h, "Max-Age=0") {
			return true
		}
	}
	return false
}

func TestActiveSessionUserID(t *testing.T) {
	origRedis := RedisClient
	RedisClient = nil
	t.Cleanup(func() { RedisClient = origRedis })

	current := map[interface{}]interface{}{"user_id": 7, SessionVersionKey: 3, SessionIDKey: "abc"}
	legacy := map[interface{}]interface{}{"user_id": 7}

	tests := []struct {
		name        string
		values      map[interface{}]interface{}
		state       storage.SessionState
		lookupErr   error
		wantUser    bool
		wantCleared bool
	}{
		{"matching version", current, storage.SessionState{Version: 3, Active: true}, nil, true, false},
		{"version raised after sign-in", current, storage.SessionState{Version: 4, Active: true}, nil, false, true},
		{"deactivated account", current, storage.SessionState{Version: 3, Active: false}, nil, false, true},
		{"account missing", current, storage.SessionState{}, nil, false, true},
		{"legacy cookie before any bump", legacy, storage.SessionState{Version: 1, Active: true}, nil, true, false},
		{"legacy cookie after a bump", legacy, storage.SessionState{Version: 2, Active: true}, nil, false, true},
		{"database error keeps cookie", current, storage.SessionState{}, errors.New("db down"), false, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stubSessionState(t, tt.state, tt.lookupErr)
			req := requestWithSession(t, tt.values)
			rec := httptest.NewRecorder()
			uid := activeSessionUserID(rec, req)
			if (uid != nil) != tt.wantUser {
				t.Fatalf("got user %v, want signed in = %v", uid, tt.wantUser)
			}
			if uid != nil && *uid != 7 {
				t.Fatalf("user id = %d, want 7", *uid)
			}
			if got := clearsCookie(rec); got != tt.wantCleared {
				t.Fatalf("cookie cleared = %v, want %v", got, tt.wantCleared)
			}
		})
	}
}

func TestActiveSessionUserIDWithoutSession(t *testing.T) {
	stubSessionState(t, storage.SessionState{Version: 1, Active: true}, nil)
	req := httptest.NewRequest(http.MethodGet, "/api/v2/me", nil)
	if uid := activeSessionUserID(httptest.NewRecorder(), req); uid != nil {
		t.Fatalf("expected no user without a cookie, got %d", *uid)
	}
}

func TestNewSessionIDIsRandom(t *testing.T) {
	a, err := NewSessionID()
	if err != nil {
		t.Fatal(err)
	}
	b, _ := NewSessionID()
	if len(a) != 32 || a == b {
		t.Fatalf("session ids should be 32 hex chars and distinct: %q %q", a, b)
	}
}
