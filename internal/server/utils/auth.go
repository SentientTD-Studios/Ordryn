package utils

import (
	"GoTodo/internal/sessionstore"
	"GoTodo/internal/storage"
	"fmt"
	"net/http"
	"strconv"
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
		email, _, permissions, loggedIn := GetSessionUser(r)
		if !loggedIn {
			http.Redirect(w, r, GetBasePath()+"/", http.StatusSeeOther)
			return
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
