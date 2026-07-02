package handlers

import (
	"GoTodo/internal/server/utils"
	"GoTodo/internal/sessionstore"
	"net/http"
	"strconv"
)

func APIReturnTasks(w http.ResponseWriter, r *http.Request) {
	pageSize := utils.AppConstants.PageSize
	if sess, err := sessionstore.Store.Get(r, "session"); err == nil && sess != nil {
		if val, ok := sess.Values["items_per_page"]; ok {
			switch tv := val.(type) {
			case int:
				if tv > 0 {
					pageSize = tv
				}
			case int64:
				if int(tv) > 0 {
					pageSize = int(tv)
				}
			case float64:
				if int(tv) > 0 {
					pageSize = int(tv)
				}
			case string:
				if v, err := strconv.Atoi(tv); err == nil && v > 0 {
					pageSize = v
				}
			}
		}
	}

	fc := filterContextFromRequest(r)
	page := fc.Page
	if page < 1 {
		page = 1
	}

	email, _, _, timezone, loggedIn, _ := utils.GetSessionUserWithTimezone(r)
	var userID *int
	if loggedIn {
		userID = getUserIDFromEmail(email)
	}

	if err := renderFilteredTaskListPartial(w, r, page, pageSize, fc, userID, timezone, loggedIn); err != nil {
		http.Error(w, "Error rendering template: "+err.Error(), http.StatusInternalServerError)
	}
}
