package sessionstore

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestGetSessionIgnoresUndecodableCookie(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.AddCookie(&http.Cookie{
		Name:  "session",
		Value: "not-a-valid-securecookie-value",
	})

	sess, err := GetSession(req)
	if err != nil {
		t.Fatalf("GetSession: %v", err)
	}
	if sess == nil {
		t.Fatal("expected session")
	}
	if !sess.IsNew {
		t.Fatal("expected new session after decode failure")
	}
	if sess.Options == nil || sess.Options.MaxAge <= 0 {
		t.Fatalf("expected store MaxAge on fresh session, got %+v", sess.Options)
	}

	sess.Values["user_id"] = 42
	rec := httptest.NewRecorder()
	if err := sess.Save(req, rec); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if len(rec.Result().Cookies()) == 0 {
		t.Fatal("expected Set-Cookie after Save")
	}
}

func TestEstablishSessionPatternWithBadCookie(t *testing.T) {
	// Mirrors login: GetSession + write values + Save must succeed with junk cookie.
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", nil)
	req.AddCookie(&http.Cookie{Name: "session", Value: "garbage"})

	sess, err := GetSession(req)
	if err != nil {
		t.Fatal(err)
	}
	sess.Values["email"] = "a@b.com"
	ApplySecureCookieOptions(sess)
	rec := httptest.NewRecorder()
	if err := sess.Save(req, rec); err != nil {
		t.Fatalf("Save: %v", err)
	}
	var found *http.Cookie
	for _, c := range rec.Result().Cookies() {
		if c.Name == "session" {
			found = c
			break
		}
	}
	if found == nil || found.MaxAge < 0 {
		t.Fatalf("expected persistent session cookie, got %+v", found)
	}

	// Round-trip the new cookie.
	req2 := httptest.NewRequest(http.MethodGet, "/", nil)
	req2.AddCookie(found)
	sess2, err := Store.Get(req2, "session")
	if err != nil {
		t.Fatalf("decode new cookie: %v", err)
	}
	if sess2.Values["email"] != "a@b.com" {
		t.Fatalf("email = %v", sess2.Values["email"])
	}
}

func TestClearSessionCookieAlwaysExpires(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.AddCookie(&http.Cookie{Name: "session", Value: "garbage"})
	rec := httptest.NewRecorder()
	ClearSessionCookie(rec, req)

	var found *http.Cookie
	for _, c := range rec.Result().Cookies() {
		if c.Name == "session" {
			found = c
			break
		}
	}
	if found == nil {
		t.Fatal("expected expired session cookie")
	}
	if found.MaxAge >= 0 {
		t.Fatalf("MaxAge = %d, want < 0", found.MaxAge)
	}
}

func TestGetSessionWithoutCookie(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	sess, err := GetSession(req)
	if err != nil {
		t.Fatal(err)
	}
	if !sess.IsNew {
		t.Fatal("expected new session")
	}
}
