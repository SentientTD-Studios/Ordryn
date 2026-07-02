package handlers

import (
	"GoTodo/internal/server/utils"
	"net/http"
)

// resolveRequestUserID returns the logged-in user's ID from session or email lookup.
func resolveRequestUserID(r *http.Request) (int, bool) {
	if uid := utils.GetSessionUserID(r); uid != nil {
		return *uid, true
	}
	email, _, _, loggedIn := utils.GetSessionUser(r)
	if !loggedIn || email == "" {
		return 0, false
	}
	if uid := getUserIDFromEmail(email); uid != nil {
		return *uid, true
	}
	return 0, false
}
