package utils

import (
	"GoTodo/internal/sessionstore"
	"GoTodo/internal/storage"
	"fmt"
	"net/http"
	"strconv"
	"time"
)

const (
	mfaPendingUserIDKey = "mfa_pending_user_id"
	mfaPendingEmailKey  = "mfa_pending_email"
	mfaPendingUntilKey  = "mfa_pending_until"
	mfaSetupSecretKey   = "mfa_setup_secret"
	MFAPendingTTL       = 10 * time.Minute
	sessionMaxAge       = 86400 * 30
)

// GetSessionUserID returns the user_id stored in the session as a pointer to int.
// Returns nil if not present or on error.
func GetSessionUserID(r *http.Request) *int {
	session, err := sessionstore.Store.Get(r, "session")
	if err != nil {
		fmt.Printf("GetSessionUserID error getting session: %v\n", err)
		return nil
	}

	idVal, ok := session.Values["user_id"]
	if !ok {
		return nil
	}

	switch v := idVal.(type) {
	case int:
		return &v
	case int64:
		i := int(v)
		return &i
	case float64:
		i := int(v)
		return &i
	case string:
		if n, err := strconv.Atoi(v); err == nil {
			return &n
		}
	default:
		return nil
	}
	return nil
}

func GetSessionUser(r *http.Request) (email string, roleID int, permissions []string, loggedIn bool) {
	session, err := sessionstore.Store.Get(r, "session")
	if err != nil {
		fmt.Printf("GetSessionUser error getting session: %v\n", err)
		return "", 0, nil, false
	}

	emailVal, ok := session.Values["email"]
	if !ok {
		return "", 0, nil, false
	}

	email, ok = emailVal.(string)
	if !ok {
		return "", 0, nil, false
	}

	roleIDVal, ok := session.Values["role_id"]
	if !ok {
		return email, 0, nil, true
	}

	roleID, ok = roleIDVal.(int)
	if !ok {
		return email, 0, nil, true
	}

	permissionsVal, ok := session.Values["permissions"]
	if !ok {
		return email, roleID, []string{}, true
	}

	permissions, ok = permissionsVal.([]string)
	if !ok {
		if permsInterface, ok := permissionsVal.([]interface{}); ok {
			permissions = make([]string, len(permsInterface))
			for i, v := range permsInterface {
				if str, ok := v.(string); ok {
					permissions[i] = str
				}
			}
		} else {
			permissions = []string{}
		}
	}

	return email, roleID, permissions, true
}

// GetSessionUserWithTimezone retrieves session user data including timezone
func GetSessionUserWithTimezone(r *http.Request) (email string, roleID int, permissions []string, timezone string, loggedIn bool, user_name string) {
	session, err := sessionstore.Store.Get(r, "session")
	if err != nil {
		fmt.Printf("GetSessionUserWithTimezone error getting session: %v\n", err)
		return "", 0, nil, "America/New_York", false, ""
	}

	emailVal, ok := session.Values["email"]
	if !ok {
		return "", 0, nil, "America/New_York", false, ""
	}

	email, ok = emailVal.(string)
	if !ok {
		return "", 0, nil, "America/New_York", false, ""
	}

	roleIDVal, ok := session.Values["role_id"]
	if !ok {
		return email, 0, nil, "America/New_York", true, ""
	}

	roleID, ok = roleIDVal.(int)
	if !ok {
		return email, 0, nil, "America/New_York", true, ""
	}

	permissionsVal, ok := session.Values["permissions"]
	if !ok {
		return email, roleID, []string{}, "America/New_York", true, ""
	}

	permissions, ok = permissionsVal.([]string)
	if !ok {
		if permsInterface, ok := permissionsVal.([]interface{}); ok {
			permissions = make([]string, len(permsInterface))
			for i, v := range permsInterface {
				if str, ok := v.(string); ok {
					permissions[i] = str
				}
			}
		} else {
			permissions = []string{}
		}
	}

	timezoneVal, ok := session.Values["timezone"]
	if !ok {
		return email, roleID, permissions, "America/New_York", true, ""
	}

	timezone, ok = timezoneVal.(string)
	if !ok {
		timezone = "America/New_York"
	}

	userNameVal, ok := session.Values["user_name"]
	if !ok {
		return email, roleID, permissions, timezone, true, ""
	}
	userName, ok := userNameVal.(string)
	if !ok {
		userName = ""
	}

	return email, roleID, permissions, timezone, true, userName
}

// RequireAuth is a middleware that checks if a user is logged in
func RequireAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		basePath := GetBasePath()
		email, _, _, loggedIn := GetSessionUser(r)
		if !loggedIn {
			http.Redirect(w, r, basePath+"/", http.StatusSeeOther)
			return
		}

		if email != "" {
			if isBanned, err := storage.IsUserBanned(email); err == nil && isBanned {
				sessionstore.ClearSessionCookie(w, r)
				http.Redirect(w, r, basePath+"/", http.StatusSeeOther)
				return
			}
		}
		next(w, r)
	}
}

// RequirePermission is a middleware that checks if a user has a specific permission
func RequirePermission(permission string, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		email, _, _, loggedIn := GetSessionUser(r)
		if !loggedIn {
			http.Redirect(w, r, GetBasePath()+"/", http.StatusSeeOther)
			return
		}
		// The session's permission list is a login-time snapshot; read the role live.
		var permissions []string
		if uid := GetSessionUserID(r); uid != nil {
			permissions, _ = storage.GetUserPermissions(*uid)
		}

		if email != "" {
			if isBanned, err := storage.IsUserBanned(email); err == nil && isBanned {
				sessionstore.ClearSessionCookie(w, r)
				http.Redirect(w, r, GetBasePath()+"/", http.StatusSeeOther)
				return
			}
		}

		hasPermission := false
		for _, p := range permissions {
			if p == permission {
				hasPermission = true
				break
			}
		}

		if !hasPermission {
			SetFlash(w, r, "You don't have permission to access this.")
			http.Redirect(w, r, GetBasePath()+"/", http.StatusSeeOther)
			return
		}

		next(w, r)
	}
}

func sessionAsInt(v interface{}) (int, bool) {
	switch n := v.(type) {
	case int:
		return n, true
	case int64:
		return int(n), true
	case float64:
		return int(n), true
	case string:
		if parsed, err := strconv.Atoi(n); err == nil {
			return parsed, true
		}
	}
	return 0, false
}

func sessionAsInt64(v interface{}) (int64, bool) {
	switch n := v.(type) {
	case int64:
		return n, true
	case int:
		return int64(n), true
	case float64:
		return int64(n), true
	}
	return 0, false
}

// EstablishPendingMFASession stores a short-lived MFA challenge without granting login.
func EstablishPendingMFASession(w http.ResponseWriter, r *http.Request, userID int, email string) error {
	session, err := sessionstore.GetSession(r)
	if err != nil {
		return err
	}
	session.Values = map[interface{}]interface{}{
		mfaPendingUserIDKey: userID,
		mfaPendingEmailKey:  email,
		mfaPendingUntilKey:  time.Now().Add(MFAPendingTTL).Unix(),
	}
	if session.Options == nil && sessionstore.Store != nil && sessionstore.Store.Options != nil {
		opts := *sessionstore.Store.Options
		session.Options = &opts
	}
	if session.Options != nil {
		session.Options.MaxAge = int(MFAPendingTTL.Seconds())
		session.Options.Path = "/"
		session.Options.HttpOnly = true
		session.Options.SameSite = http.SameSiteLaxMode
	}
	sessionstore.ApplySecureCookieOptions(session)
	return session.Save(r, w)
}

// GetPendingMFA returns the pending MFA user if the challenge is still valid.
func GetPendingMFA(r *http.Request) (userID int, email string, ok bool) {
	session, err := sessionstore.GetSession(r)
	if err != nil {
		return 0, "", false
	}
	untilVal, hasUntil := session.Values[mfaPendingUntilKey]
	until, untilOK := sessionAsInt64(untilVal)
	if !hasUntil || !untilOK || until < time.Now().Unix() {
		return 0, "", false
	}
	idVal, hasID := session.Values[mfaPendingUserIDKey]
	userID, idOK := sessionAsInt(idVal)
	if !hasID || !idOK || userID <= 0 {
		return 0, "", false
	}
	emailVal, _ := session.Values[mfaPendingEmailKey].(string)
	return userID, emailVal, true
}

// SetMFASetupSecret stores a pending TOTP secret on the authenticated session.
func SetMFASetupSecret(w http.ResponseWriter, r *http.Request, totpSecret string) error {
	session, err := sessionstore.GetSession(r)
	if err != nil {
		return err
	}
	session.Values[mfaSetupSecretKey] = totpSecret
	sessionstore.ApplySecureCookieOptions(session)
	return session.Save(r, w)
}

// GetMFASetupSecret returns the pending TOTP setup secret, if any.
func GetMFASetupSecret(r *http.Request) string {
	session, err := sessionstore.GetSession(r)
	if err != nil {
		return ""
	}
	s, _ := session.Values[mfaSetupSecretKey].(string)
	return s
}

// ClearMFASetupSecret removes the pending TOTP setup secret.
func ClearMFASetupSecret(w http.ResponseWriter, r *http.Request) error {
	session, err := sessionstore.GetSession(r)
	if err != nil {
		return err
	}
	delete(session.Values, mfaSetupSecretKey)
	sessionstore.ApplySecureCookieOptions(session)
	return session.Save(r, w)
}

// SessionMaxAge is the persistent cookie lifetime in seconds.
func SessionMaxAge() int {
	return sessionMaxAge
}
