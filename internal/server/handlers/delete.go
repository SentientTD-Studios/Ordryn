package handlers

import (
	"GoTodo/internal/server/utils"
	"GoTodo/internal/sessionstore"
	"GoTodo/internal/storage"
	"GoTodo/internal/tasks"
	"context"
	"database/sql"
	"errors"
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

	// Delete the task from the database (only if it belongs to the user)
	result, err := db.Exec(context.Background(), "DELETE FROM tasks WHERE id = $1 AND user_id = $2", taskID, userID)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprintf(w, "Error deleting task")
		return
	}

	// Check if any rows were actually deleted
	rowsAffected := result.RowsAffected()
	if rowsAffected == 0 {
		w.WriteHeader(http.StatusNotFound)
		fmt.Fprintf(w, "Task not found or you don't have permission to delete it")
		return
	}

	// Determine active project filter
	projectParam := r.URL.Query().Get("project")
	statusFilter := requestStatusFilter(r)

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
	if err := renderFilteredTaskListPartial(w, r, currentPage, pageSize, "", &userID, timezone, loggedIn, projectParam, statusFilter); err != nil {
		http.Error(w, "Error rendering tasks: "+err.Error(), http.StatusInternalServerError)
	}
}

func APIGetNextItem(w http.ResponseWriter, r *http.Request) {
	currentPage, _ := strconv.Atoi(r.URL.Query().Get("page"))
	if currentPage < 1 {
		currentPage = 1
	}

	// Get user ID from session
	email, _, _, loggedIn := utils.GetSessionUser(r)
	if !loggedIn {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}

	db, err := storage.OpenDatabase()
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprintf(w, "Error opening database")
		return
	}
	defer db.Close()

	// Get user ID (prefer session value)
	var userID int
	if uid := utils.GetSessionUserID(r); uid != nil {
		userID = *uid
	} else {
		err = db.QueryRow(context.Background(), "SELECT id FROM users WHERE email = $1", email).Scan(&userID)
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
	}

	// Get total number of tasks for this user
	var totalTasks int
	err = db.QueryRow(context.Background(), "SELECT COUNT(*) FROM tasks WHERE user_id = $1", userID).Scan(&totalTasks)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	// Calculate last page
	pageSize := utils.AppConstants.PageSize
	lastPage := (totalTasks + pageSize - 1) / pageSize

	// If current page is beyond last page, trigger reload of previous page
	if currentPage > lastPage && currentPage > 1 {
		w.Header().Set("HX-Trigger", "reload-previous-page")
		w.WriteHeader(http.StatusOK)
		return
	}

	offset := (currentPage - 1) * pageSize

	// Check how many items are on the current page for this user
	var itemsOnPage int
	err = db.QueryRow(context.Background(),
		"SELECT COUNT(*) FROM (SELECT id FROM tasks WHERE user_id = $1 ORDER BY id LIMIT $2 OFFSET $3) AS page_tasks",
		userID, pageSize, offset).Scan(&itemsOnPage)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	// If there are no items on the current page and we're not on page 1, reload previous page
	if itemsOnPage == 0 && currentPage > 1 {
		w.Header().Set("HX-Trigger", "reload-previous-page")
		w.WriteHeader(http.StatusOK)
		return
	}

	// If we're on page 1 and the total tasks is less than or equal to page size,
	// we don't need to pull up any items
	if currentPage == 1 && totalTasks <= pageSize {
		w.WriteHeader(http.StatusOK)
		return
	}

	// If we're on the last page and it's full, no need to pull up
	if currentPage == lastPage && itemsOnPage == pageSize {
		w.WriteHeader(http.StatusOK)
		return
	}

	// Only pull up the next item if there is a gap on the page
	nextItemOffset := offset + itemsOnPage
	if nextItemOffset >= totalTasks {
		w.WriteHeader(http.StatusOK)
		return
	}

	row := db.QueryRow(context.Background(),
		`SELECT id, title, description, completed, TO_CHAR(time_stamp, 'YYYY/MM/DD HH:MI AM') AS date_added
		 FROM tasks WHERE user_id = $1 ORDER BY id LIMIT 1 OFFSET $2`, userID, nextItemOffset)

	var task tasks.Task
	err = row.Scan(&task.ID, &task.Title, &task.Description, &task.Completed, &task.DateAdded)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			w.WriteHeader(http.StatusOK)
			return
		} else {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
	}

	task.Page = currentPage
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := utils.RenderTemplate(w, r, "todo.html", &task); err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusOK)
}
