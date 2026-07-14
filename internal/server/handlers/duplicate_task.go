package handlers

import (
	"GoTodo/internal/server/utils"
	"GoTodo/internal/sessionstore"
	"GoTodo/internal/storage"
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"strconv"
	"strings"
)

func APIDuplicateTask(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Invalid request method", http.StatusMethodNotAllowed)
		return
	}

	email, _, _, timezone, loggedIn, _ := utils.GetSessionUserWithTimezone(r)
	if !loggedIn {
		w.WriteHeader(http.StatusUnauthorized)
		fmt.Fprint(w, "Please log in to duplicate tasks")
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

	idStr := r.URL.Query().Get("id")
	if idStr == "" {
		idStr = r.FormValue("id")
	}
	sourceID, err := strconv.Atoi(idStr)
	if err != nil {
		http.Error(w, "Invalid task id", http.StatusBadRequest)
		return
	}

	currentPage, _ := strconv.Atoi(firstNonEmpty(r.URL.Query().Get("page"), r.FormValue("page")))
	if currentPage < 1 {
		currentPage = 1
	}

	db, err := storage.OpenDatabase()
	if err != nil {
		http.Error(w, "Error opening database", http.StatusInternalServerError)
		return
	}
	defer storage.CloseDatabase(db)

	var userID int
	if uid := utils.GetSessionUserID(r); uid != nil {
		userID = *uid
	} else {
		if err := db.QueryRow(context.Background(), "SELECT id FROM users WHERE email = $1", email).Scan(&userID); err != nil {
			http.Error(w, "Error getting user ID", http.StatusInternalServerError)
			return
		}
	}

	var title, description string
	var projectID sql.NullInt64
	var dueDate sql.NullString
	var priority int
	var ownerID int
	err = db.QueryRow(context.Background(),
		`SELECT title, COALESCE(description, ''), project_id, COALESCE(CAST(due_date AS TEXT), ''), COALESCE(priority, 0), user_id
		 FROM tasks WHERE id = $1`, sourceID).
		Scan(&title, &description, &projectID, &dueDate, &priority, &ownerID)
	if err != nil {
		http.Error(w, "Task not found", http.StatusNotFound)
		return
	}
	if ownerID != userID {
		http.Error(w, "Task not found", http.StatusNotFound)
		return
	}

	copyTitle := strings.TrimSpace(title) + " (copy)"
	if len(copyTitle) > 200 {
		copyTitle = copyTitle[:200]
	}

	var nextPos int
	if err := db.QueryRow(context.Background(),
		"SELECT COALESCE(MAX(position),0) + 1 FROM tasks WHERE user_id = $1 AND (is_favorite IS NULL OR is_favorite = false)",
		userID).Scan(&nextPos); err != nil {
		http.Error(w, "Error determining position", http.StatusInternalServerError)
		return
	}

	var newTaskID int
	if projectID.Valid && dueDate.Valid && dueDate.String != "" {
		err = db.QueryRow(context.Background(),
			`INSERT INTO tasks (title, description, completed, user_id, time_stamp, position, priority, project_id, due_date)
			 VALUES ($1, $2, false, $3, NOW() AT TIME ZONE 'UTC', $4, $5, $6, $7) RETURNING id`,
			copyTitle, description, userID, nextPos, priority, projectID.Int64, dueDate.String).Scan(&newTaskID)
	} else if projectID.Valid {
		err = db.QueryRow(context.Background(),
			`INSERT INTO tasks (title, description, completed, user_id, time_stamp, position, priority, project_id)
			 VALUES ($1, $2, false, $3, NOW() AT TIME ZONE 'UTC', $4, $5, $6) RETURNING id`,
			copyTitle, description, userID, nextPos, priority, projectID.Int64).Scan(&newTaskID)
	} else if dueDate.Valid && dueDate.String != "" {
		err = db.QueryRow(context.Background(),
			`INSERT INTO tasks (title, description, completed, user_id, time_stamp, position, priority, due_date)
			 VALUES ($1, $2, false, $3, NOW() AT TIME ZONE 'UTC', $4, $5, $6) RETURNING id`,
			copyTitle, description, userID, nextPos, priority, dueDate.String).Scan(&newTaskID)
	} else {
		err = db.QueryRow(context.Background(),
			`INSERT INTO tasks (title, description, completed, user_id, time_stamp, position, priority)
			 VALUES ($1, $2, false, $3, NOW() AT TIME ZONE 'UTC', $4, $5) RETURNING id`,
			copyTitle, description, userID, nextPos, priority).Scan(&newTaskID)
	}
	if err != nil {
		http.Error(w, "Error duplicating task", http.StatusInternalServerError)
		return
	}

	sourceTags, err := storage.GetTagsForTask(sourceID)
	if err == nil && len(sourceTags) > 0 {
		tagIDs := make([]int, 0, len(sourceTags))
		for _, tg := range sourceTags {
			tagIDs = append(tagIDs, tg.ID)
		}
		_ = storage.SetTaskTags(newTaskID, userID, tagIDs)
	}

	logTaskEvent(newTaskID, userID, "created", map[string]interface{}{"duplicated_from": sourceID})

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

	w.Header().Set("HX-Trigger", "task-added")
	if err := renderFilteredTaskListPartial(w, r, currentPage, pageSize, fc, &userID, timezone, true); err != nil {
		http.Error(w, "Error rendering tasks: "+err.Error(), http.StatusInternalServerError)
	}
}
