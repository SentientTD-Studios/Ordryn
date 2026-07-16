package handlers

import (
	"GoTodo/internal/server/utils"
	"GoTodo/internal/sessionstore"
	"GoTodo/internal/storage"
	"GoTodo/internal/tasks"
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"
)

const MaxDescriptionLength = 1000

func APIAddTask(w http.ResponseWriter, r *http.Request) {
	// fmt.Println("Request method: ", r.Method)
	if r.Method != http.MethodPost {
		http.Error(w, "Invalid request method", http.StatusMethodNotAllowed)
		return
	}

	title := strings.TrimSpace(r.FormValue("title"))
	description := strings.TrimSpace(r.FormValue("description"))
	dueDate := strings.TrimSpace(r.FormValue("due_date"))
	priority := 0
	if p, err := parsePriorityValue(r.FormValue("priority_level")); err == nil {
		priority = p
	}
	pageStr := strings.TrimSpace(r.FormValue("currentPage"))

	page, err := strconv.Atoi(pageStr)
	if err != nil || page <= 0 {
		page = 1 // Default to page 1 if no valid page is provided
	}

	// Validate description length
	if len(description) > MaxDescriptionLength {
		// On validation failure, return a 200 status with the error message
		// and use HX-Retarget and HX-Reswap to update the error div specifically
		// Tell the client this was a validation error so JS won't close the sidebar
		w.Header().Set("X-Validation-Error", "true")
		w.Header().Set("HX-Trigger", "description-error")   // Keep trigger for potential JS handling
		w.Header().Set("HX-Retarget", "#description-error") // Target the specific error div
		w.Header().Set("HX-Reswap", "innerHTML")            // Swap the content inside the error div
		w.WriteHeader(http.StatusOK)
		fmt.Fprintf(w, "Description must be %d characters or less", MaxDescriptionLength) // The content to swap
		return
	}

	if title == "" {
		// Title missing — return validation error and appropriate message
		w.Header().Set("X-Validation-Error", "true")
		w.Header().Set("HX-Trigger", "description-error")   // Keep trigger for potential JS handling
		w.Header().Set("HX-Retarget", "#description-error") // Target the specific error div
		w.Header().Set("HX-Reswap", "innerHTML")            // Swap the content inside the error div
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, "Title is required")
		return
		// http.Error(w, "Title is required", http.StatusBadRequest)
		// return
	}

	db, err := storage.OpenDatabase()
	if err != nil {
		fmt.Println("We failed to open the database.")
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	defer storage.CloseDatabase(db)

	// Get user ID from session (fallback to querying by email if not present)
	email, _, _, timezone, loggedIn, _ := utils.GetSessionUserWithTimezone(r)
	if !loggedIn {
		w.WriteHeader(http.StatusUnauthorized)
		fmt.Fprint(w, "Please log in to add tasks.")
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

	var userID int
	if uid := utils.GetSessionUserID(r); uid != nil {
		userID = *uid
	} else {
		// fallback to DB lookup if session doesn't contain user_id
		err = db.QueryRow(context.Background(), "SELECT id FROM users WHERE email = $1", email).Scan(&userID)
		if err != nil {
			fmt.Printf("Error getting user ID: %v\n", err)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
	}

	// Determine next position within non-favorite group for this user
	var nextPos int
	err = db.QueryRow(context.Background(), "SELECT COALESCE(MAX(position),0) + 1 FROM tasks WHERE user_id = $1 AND (is_favorite IS NULL OR is_favorite = false)", userID).Scan(&nextPos)
	if err != nil {
		fmt.Printf("Error determining next position: %v\n", err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	// Handle optional project association
	projectIDStr := strings.TrimSpace(r.FormValue("project_id"))
	var newTaskProject *int
	var newTaskID int
	if projectIDStr == "" {
		if dueDate != "" {
			err = db.QueryRow(context.Background(), "INSERT INTO tasks (title, description, completed, user_id, time_stamp, position, priority, due_date) VALUES ($1, $2, $3, $4, NOW() AT TIME ZONE 'UTC', $5, $6, $7) RETURNING id", title, description, false, userID, nextPos, priority, dueDate).Scan(&newTaskID)
		} else {
			err = db.QueryRow(context.Background(), "INSERT INTO tasks (title, description, completed, user_id, time_stamp, position, priority) VALUES ($1, $2, $3, $4, NOW() AT TIME ZONE 'UTC', $5, $6) RETURNING id", title, description, false, userID, nextPos, priority).Scan(&newTaskID)
		}
	} else {
		pid, errConv := strconv.Atoi(projectIDStr)
		if errConv != nil {
			http.Error(w, "Invalid project id", http.StatusBadRequest)
			return
		}
		if _, errP := storage.GetProjectByID(pid, userID); errP != nil {
			http.Error(w, "Invalid project selection", http.StatusBadRequest)
			return
		}
		if dueDate != "" {
			err = db.QueryRow(context.Background(), "INSERT INTO tasks (title, description, completed, user_id, time_stamp, position, priority, project_id, due_date) VALUES ($1, $2, $3, $4, NOW() AT TIME ZONE 'UTC', $5, $6, $7, $8) RETURNING id", title, description, false, userID, nextPos, priority, pid, dueDate).Scan(&newTaskID)
		} else {
			err = db.QueryRow(context.Background(), "INSERT INTO tasks (title, description, completed, user_id, time_stamp, position, priority, project_id) VALUES ($1, $2, $3, $4, NOW() AT TIME ZONE 'UTC', $5, $6, $7) RETURNING id", title, description, false, userID, nextPos, priority, pid).Scan(&newTaskID)
		}
		if err == nil {
			newTaskProject = &pid
		}
	}
	if err != nil {
		fmt.Println("We failed to insert into the database.")
		fmt.Println("Failed values:", title, description, false)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	if err := assignTaskTagsFromRequest(r, newTaskID, userID); err != nil {
		w.Header().Set("X-Validation-Error", "true")
		w.Header().Set("HX-Trigger", "description-error")
		w.Header().Set("HX-Retarget", "#description-error")
		w.Header().Set("HX-Reswap", "innerHTML")
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, err.Error())
		return
	}
	logTaskEvent(newTaskID, userID, "created", nil)

	if isCalendarReturn(r) {
		respondCalendarRedirect(w, r, calendarMonthFromRequest(r, timezone), timezone)
		return
	}

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
	activeProject := fc.Project
	activeStatus := fc.Status
	projectFilterPtr := parseProjectFilter(activeProject)
	listFilters := fc.ToListFilters()

	_, totalTasks, err := tasks.ReturnPaginationForUserWithFilters(1, pageSize, &userID, timezone, listFilters)
	if err != nil {
		http.Error(w, "Error counting tasks after add: "+err.Error(), http.StatusInternalServerError)
		return
	}

	lastPage := (totalTasks + pageSize - 1) / pageSize
	if lastPage < 1 {
		lastPage = 1
	}
	if page < lastPage {
		page = lastPage
	}

	shouldRefresh := false
	if projectFilterPtr == nil {
		shouldRefresh = true
	} else if *projectFilterPtr == 0 {
		if newTaskProject == nil {
			shouldRefresh = true
		}
	} else if newTaskProject != nil && *newTaskProject == *projectFilterPtr {
		shouldRefresh = true
	}
	if !taskStatusMatchesFilter(activeStatus, false) {
		shouldRefresh = false
	}

	w.Header().Set("HX-Trigger", "task-added")

	if shouldRefresh {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if err := renderFilteredTaskListPartial(w, r, page, pageSize, fc, &userID, timezone, true); err != nil {
			http.Error(w, "Error rendering tasks after add: "+err.Error(), http.StatusInternalServerError)
		}
		return
	}

	var targetFilterParam string
	if newTaskProject == nil {
		targetFilterParam = "0"
	} else {
		targetFilterParam = strconv.Itoa(*newTaskProject)
	}

	targetFC := fc
	targetFC.Project = targetFilterParam

	_, totalTasksTarget, err := tasks.ReturnPaginationForUserWithFilters(1, pageSize, &userID, timezone, targetFC.ToListFilters())
	if err != nil {
		http.Error(w, "Error counting tasks for new project: "+err.Error(), http.StatusInternalServerError)
		return
	}

	lastPageTarget := (totalTasksTarget + pageSize - 1) / pageSize
	if lastPageTarget < 1 {
		lastPageTarget = 1
	}
	page = lastPageTarget

	w.Header().Set("HX-Trigger", "task-added set-project-filter:"+targetFilterParam)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := renderFilteredTaskListPartial(w, r, page, pageSize, targetFC, &userID, timezone, true); err != nil {
		http.Error(w, "Error rendering tasks after add: "+err.Error(), http.StatusInternalServerError)
	}
}
