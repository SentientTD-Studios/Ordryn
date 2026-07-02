package handlers

import (
	"GoTodo/internal/server/utils"
	"GoTodo/internal/sessionstore"
	"GoTodo/internal/storage"
	"context"
	"fmt"
	"net/http"
	"strconv"
)

func APIDeleteTask(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		http.Error(w, "Invalid request method", http.StatusMethodNotAllowed)
		return
	}

	taskID := r.URL.Query().Get("id")
	currentPage, _ := strconv.Atoi(r.URL.Query().Get("page"))
	if currentPage < 1 {
		currentPage = 1
	}

	// Get user ID from session (fallback to querying by email)
	email, _, _, timezone, loggedIn, _ := utils.GetSessionUserWithTimezone(r)
	if !loggedIn {
		w.WriteHeader(http.StatusUnauthorized)
		fmt.Fprintf(w, "Please log in to delete tasks")
		return
	}

	// Prevent banned users from performing actions
	if isBanned, err := storage.IsUserBanned(email); err == nil && isBanned {
		sessionstore.ClearSessionCookie(w, r)
		basePath := utils.GetBasePath()
		w.Header().Set("HX-Redirect", basePath)
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, " ")
		return
	}

	db, err := storage.OpenDatabase()
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprintf(w, "Error opening database")
		return
	}
	defer db.Close()

	// Determine user ID (prefer session value)
	var userID int
	if uid := utils.GetSessionUserID(r); uid != nil {
		userID = *uid
	} else {
		err = db.QueryRow(context.Background(), "SELECT id FROM users WHERE email = $1", email).Scan(&userID)
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			fmt.Fprintf(w, "Error getting user ID")
			return
		}
	}

	tid, err := strconv.Atoi(taskID)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		fmt.Fprintf(w, "Invalid task id")
		return
	}

	if err := deleteTasksForUser(context.Background(), db, []int{tid}, userID); err != nil {
		w.WriteHeader(http.StatusNotFound)
		fmt.Fprintf(w, "Task not found or you don't have permission to delete it")
		return
	}

	// Determine active project filter
	// Determine active filters from the request
	fc := filterContextFromRequest(r)
	fc.Page = currentPage

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
	if err := renderFilteredTaskListPartial(w, r, currentPage, pageSize, fc, &userID, timezone, loggedIn); err != nil {
		http.Error(w, "Error rendering tasks: "+err.Error(), http.StatusInternalServerError)
	}
}
