package sessionstore

import (
	"fmt"
	"log"
	"net/http"

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

	Store = sessions.NewCookieStore([]byte(sessionKey))

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

// ApplySecureCookieOptions sets Secure on the session cookie when USE_HTTPS is on.
func ApplySecureCookieOptions(sess *sessions.Session) {
	if sess == nil || sess.Options == nil {
		return
	}
	if config.Cfg.UseHTTPS {
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
	if config.Cfg.UseHTTPS {
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
