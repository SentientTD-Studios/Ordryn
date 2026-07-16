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

func APIUpdateTaskStatus(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Query().Get("id")
	if id == "" {
		http.Error(w, "Invalid ID", http.StatusBadRequest)
		return
	}
	page := r.URL.Query().Get("page")
	projectParam := r.URL.Query().Get("project")
	statusFilter := requestStatusFilter(r)

	// Require logged-in user and enforce ban check + ownership
	email, _, _, loggedIn := utils.GetSessionUser(r)
	if !loggedIn {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
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
		http.Error(w, "Failed to connect to database", http.StatusInternalServerError)
		return
	}

	defer storage.CloseDatabase(db)

	var completed bool

	// Ensure task exists and belongs to the current user
	var ownerID int
	err = db.QueryRow(context.Background(), "SELECT completed, user_id FROM tasks WHERE id = $1", id).Scan(&completed, &ownerID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			http.Error(w, "Task not found.", http.StatusNotFound)
			return
		}
		http.Error(w, "Task not found.", http.StatusInternalServerError)
		return
	}

	// Verify ownership
	var userID int
	if uid := utils.GetSessionUserID(r); uid != nil {
		userID = *uid
	} else {
		err = db.QueryRow(context.Background(), "SELECT id FROM users WHERE email = $1", email).Scan(&userID)
		if err != nil {
			http.Error(w, "Error getting user ID", http.StatusInternalServerError)
			return
		}
	}
	if ownerID != userID {
		http.Error(w, "Not authorized to update this task.", http.StatusForbidden)
		return
	}

	// Optional project filter for counts + row links
	projectFilter := parseProjectFilter(projectParam)

	updatedStatus := !completed

	_, err = db.Exec(context.Background(), "UPDATE tasks SET completed = $1, date_modified = NOW() AT TIME ZONE 'UTC' WHERE id = $2", updatedStatus, id)

	if err != nil {
		http.Error(w, "Failed to update task status.", http.StatusInternalServerError)
		return
	}

	if taskID, convErr := strconv.Atoi(id); convErr == nil {
		if updatedStatus {
			logTaskEvent(taskID, userID, "completed", nil)
		} else {
			logTaskEvent(taskID, userID, "reopened", nil)
		}
	}

	// Fetch updated task data to render the complete row with updated timestamps
	email, _, _, timezone, _, _ := utils.GetSessionUserWithTimezone(r)
	var task tasks.Task
	var projectID sql.NullInt64
	var projectName sql.NullString
	err = db.QueryRow(context.Background(),
		`SELECT t.id, t.title, t.description, t.completed, 
			TO_CHAR((t.time_stamp AT TIME ZONE 'UTC') AT TIME ZONE $2, 'YYYY/MM/DD HH:MI AM') AS date_added,
			COALESCE(CAST(t.due_date AS TEXT), '') AS due_date,
			TO_CHAR((t.time_stamp AT TIME ZONE 'UTC') AT TIME ZONE $2, 'YYYY/MM/DD HH:MI AM') AS date_created,
			COALESCE(TO_CHAR((t.date_modified AT TIME ZONE 'UTC') AT TIME ZONE $2, 'YYYY/MM/DD HH:MI AM'), '') AS date_modified,
			COALESCE(t.is_favorite,false), COALESCE(t.position,0), COALESCE(t.priority,0), t.project_id, COALESCE(p.name,'')
		FROM tasks t LEFT JOIN projects p ON t.project_id = p.id 
		WHERE t.id = $1`, id, timezone).Scan(
		&task.ID, &task.Title, &task.Description, &task.Completed,
		&task.DateAdded, &task.DueDate, &task.DateCreated, &task.DateModified,
		&task.IsFavorite, &task.Position, &task.Priority, &projectID, &projectName)

	if err != nil {
		http.Error(w, "Failed to fetch updated task", http.StatusInternalServerError)
		return
	}

	if projectID.Valid {
		task.ProjectID = int(projectID.Int64)
	}
	task.ProjectName = projectName.String
	pageNum, _ := strconv.Atoi(page)
	task.Page = pageNum

	if err := db.QueryRow(context.Background(), "SELECT user_id FROM tasks WHERE id = $1", id).Scan(&ownerID); err == nil {
		completedCount, incompleteCount := completedIncompleteCounts(&ownerID, projectFilter)
		// Emit HTMX trigger with counts payload so client can update badges
		w.Header().Set("HX-Trigger", fmt.Sprintf(`{"taskCountsChanged":{"completed":%d,"incomplete":%d}}`, completedCount, incompleteCount))
	}

	if isCalendarReturn(r) {
		respondCalendarRedirect(w, r, calendarMonthFromRequest(r, timezone), timezone)
		return
	}

	if statusFilter != "" {
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
		userPtr := &userID
		fc := filterContextFromRequest(r)
		fc.Page = pageNum
		if err := renderFilteredTaskListPartial(w, r, pageNum, pageSize, fc, userPtr, timezone, true); err != nil {
			http.Error(w, "Error rendering filtered tasks: "+err.Error(), http.StatusInternalServerError)
		}
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	// Render the complete task row with updated data
	fc := filterContextFromRequest(r)
	if err := renderSingleTaskRow(w, task, fc, timezone); err != nil {
		http.Error(w, "Error rendering task row: "+err.Error(), http.StatusInternalServerError)
		return
	}
}
