package sessionstore

import (
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"GoTodo/internal/config"

	"github.com/gorilla/securecookie"
)

func sessionCookieFor(t *testing.T, values map[interface{}]interface{}) *http.Cookie {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	sess, err := GetSession(req)
	if err != nil {
		t.Fatal(err)
	}
	sess.Values = values
	rec := httptest.NewRecorder()
	if err := sess.Save(req, rec); err != nil {
		t.Fatal(err)
	}
	for _, c := range rec.Result().Cookies() {
		if c.Name == "session" {
			return c
		}
	}
	t.Fatal("no session cookie written")
	return nil
}

// decodeLayers undoes the base64 layers gorilla wraps around the cookie value.
func decodeLayers(v string) string {
	out := v
	for i := 0; i < 3; i++ {
		b, err := base64.URLEncoding.DecodeString(out)
		if err != nil {
			b, err = base64.RawURLEncoding.DecodeString(out)
		}
		if err != nil {
			break
		}
		out = string(b)
	}
	return out
}

func TestSessionCookieContentsAreEncrypted(t *testing.T) {
	const secret = "alice-secret@example.com"
	c := sessionCookieFor(t, map[interface{}]interface{}{"email": secret})
	if strings.Contains(c.Value, secret) || strings.Contains(decodeLayers(c.Value), secret) {
		t.Fatal("session cookie exposes its contents; it should be encrypted")
	}

	// And it still round-trips.
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.AddCookie(c)
	sess, err := GetSession(req)
	if err != nil || sess.Values["email"] != secret {
		t.Fatalf("round trip failed: %v %v", sess.Values, err)
	}
}

// Cookies from before encryption was turned on were only signed. They must be
// treated as signed out, not as an error.
func TestSignedOnlyLegacyCookieStartsFreshSession(t *testing.T) {
	key, err := config.SessionKey()
	if err != nil {
		t.Fatal(err)
	}
	legacy := securecookie.New([]byte(key), nil)
	value, err := legacy.Encode("session", map[interface{}]interface{}{"user_id": 7})
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.AddCookie(&http.Cookie{Name: "session", Value: value})
	sess, err := GetSession(req)
	if err != nil {
		t.Fatalf("GetSession should not fail on a legacy cookie: %v", err)
	}
	if _, ok := sess.Values["user_id"]; ok {
		t.Fatal("legacy signed-only cookie must not be accepted")
	}
}

func TestSecureCookiesFollowsConfig(t *testing.T) {
	orig := config.Cfg.UseHTTPS
	t.Cleanup(func() { config.Cfg.UseHTTPS = orig })

	config.Cfg.UseHTTPS = false
	t.Setenv("ENV", "")
	if SecureCookies() {
		t.Fatal("no HTTPS and not production: Secure should be off")
	}
	t.Setenv("ENV", "production")
	if !SecureCookies() {
		t.Fatal("ENV=production should turn Secure on")
	}
	t.Setenv("ENV", "")
	config.Cfg.UseHTTPS = true
	if !SecureCookies() {
		t.Fatal("USE_HTTPS should turn Secure on")
	}
}

func TestCookieEncryptionKeyIsDerived(t *testing.T) {
	k := cookieEncryptionKey("a-session-key-of-at-least-32-chars!!")
	if len(k) != 32 {
		t.Fatalf("key length = %d, want 32 for AES-256", len(k))
	}
	if string(k) == "a-session-key-of-at-least-32-chars!!"[:32] {
		t.Fatal("encryption key must not be the raw SESSION_KEY")
	}
}
