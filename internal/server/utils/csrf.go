package utils

import (
	"GoTodo/internal/sessionstore"
	"crypto/rand"
	"encoding/hex"
	"net/http"
)

const csrfSessionKey = "csrf_token"

// EnsureCSRFToken returns the session CSRF token, creating one if needed.
func EnsureCSRFToken(r *http.Request, w http.ResponseWriter) (string, error) {
	sess, err := sessionstore.Store.Get(r, "session")
	if err != nil {
		return "", err
	}
	if tok, ok := sess.Values[csrfSessionKey].(string); ok && tok != "" {
		return tok, nil
	}
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	tok := hex.EncodeToString(b)
	sess.Values[csrfSessionKey] = tok
	if err := sess.Save(r, w); err != nil {
		return "", err
	}
	return tok, nil
}

// ValidateCSRF checks the token from form field csrf_token or header X-CSRF-Token.
func ValidateCSRF(r *http.Request) bool {
	sess, err := sessionstore.Store.Get(r, "session")
	if err != nil {
		return false
	}
	expected, ok := sess.Values[csrfSessionKey].(string)
	if !ok || expected == "" {
		return false
	}
	got := r.FormValue("csrf_token")
	if got == "" {
		got = r.Header.Get("X-CSRF-Token")
	}
	return got != "" && got == expected
}

// RequireCSRF wraps POST handlers that need CSRF protection.
func RequireCSRF(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			_ = r.ParseForm()
			if !ValidateCSRF(r) {
				http.Error(w, "Invalid CSRF token", http.StatusForbidden)
				return
			}
		}
		next(w, r)
	}
}
