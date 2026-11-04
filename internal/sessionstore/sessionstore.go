package sessionstore

import (
	"crypto/hmac"
	"crypto/sha256"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"

	"GoTodo/internal/config"

	"github.com/gorilla/sessions"
)

var Store *sessions.CookieStore

func init() {
	// Earliest fail-fast: require .env + SESSION_KEY before main (skipped under go test).
	if err := config.LoadDotEnv(); err != nil {
		log.Fatal(err)
	}
	sessionKey, err := config.SessionKey()
	if err != nil {
		log.Fatal(err)
	}

	// The cookie is signed with SESSION_KEY and encrypted with a key derived from
	// it, so its contents (email, permissions, MFA setup secret) can't be read.
	Store = sessions.NewCookieStore([]byte(sessionKey), cookieEncryptionKey(sessionKey))

	const sessionMaxAge = 86400 * 30 // 30 days
	Store.MaxAge(sessionMaxAge)

	Store.Options = &sessions.Options{
		Path:     "/",
		HttpOnly: true,
		Secure:   false,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   sessionMaxAge,
	}
}

// cookieEncryptionKey derives the AES-256 key for session cookies from SESSION_KEY.
// It's purpose-specific, so it never equals a key used for anything else.
func cookieEncryptionKey(sessionKey string) []byte {
	mac := hmac.New(sha256.New, []byte(sessionKey))
	mac.Write([]byte("ordryn-session-cookie-encryption-v1"))
	return mac.Sum(nil)
}

// SecureCookies reports whether cookies get the Secure flag: when USE_HTTPS is on
// or ENV=production (which also turns on HSTS, so the site is served over HTTPS).
func SecureCookies() bool {
	return config.Cfg.UseHTTPS || strings.EqualFold(strings.TrimSpace(os.Getenv("ENV")), "production")
}

// Configure applies runtime config to the store's cookie defaults. Call it once
// config has loaded; every session saved afterwards inherits the Secure flag.
func Configure() {
	if Store != nil && Store.Options != nil {
		Store.Options.Secure = SecureCookies()
	}
}

// ApplySecureCookieOptions sets Secure on the session cookie when SecureCookies says so.
func ApplySecureCookieOptions(sess *sessions.Session) {
	if sess == nil || sess.Options == nil {
		return
	}
	if SecureCookies() {
		sess.Options.Secure = true
	}
}

// resetSessionOptions copies store defaults onto a session (e.g. after a failed decode).
func resetSessionOptions(sess *sessions.Session) {
	if sess == nil || Store == nil || Store.Options == nil {
		return
	}
	opts := *Store.Options
	sess.Options = &opts
}

// GetSession returns the request session. CookieStore always yields a session even when
// an existing cookie cannot be decoded (rotated SESSION_KEY, corruption, expiry); those
// decode errors are ignored so login/logout can proceed with a fresh session.
func GetSession(r *http.Request) (*sessions.Session, error) {
	if Store == nil {
		return nil, fmt.Errorf("session store not initialized")
	}
	sess, err := Store.Get(r, "session")
	if sess == nil {
		if err != nil {
			return nil, err
		}
		return nil, fmt.Errorf("session store returned nil session")
	}
	if err != nil {
		resetSessionOptions(sess)
		sess.Values = make(map[interface{}]interface{})
		sess.IsNew = true
	}
	return sess, nil
}

func ClearSessionCookie(w http.ResponseWriter, r *http.Request) {
	// Always emit an expired cookie so browsers drop undecodable leftovers even when
	// Store.Get returns a decode error (gorilla still hands back a fresh session).
	cookie := &http.Cookie{
		Name:     "session",
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	}
	if SecureCookies() {
		cookie.Secure = true
	}
	http.SetCookie(w, cookie)

	if Store == nil {
		return
	}
	sess, _ := Store.Get(r, "session")
	if sess == nil {
		return
	}
	resetSessionOptions(sess)
	sess.Options.MaxAge = -1
	sess.Values = make(map[interface{}]interface{})
	ApplySecureCookieOptions(sess)
	_ = sess.Save(r, w)
}
