package handlers

import (
	"GoTodo/internal/server/utils"
	"GoTodo/internal/sessionstore"
	"GoTodo/internal/storage"
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
)

const maxBulkTaskIDs = 100

// APIBulkUpdate applies a bulk action to multiple tasks owned by the user.
func APIBulkUpdate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Invalid request method", http.StatusMethodNotAllowed)
		return
	}

	email, _, _, timezone, loggedIn, _ := utils.GetSessionUserWithTimezone(r)
	if !loggedIn {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	if isBanned, err := storage.IsUserBanned(email); err == nil && isBanned {
		sessionstore.ClearSessionCookie(w, r)
		w.Header().Set("HX-Redirect", utils.GetBasePath())
		w.WriteHeader(http.StatusOK)
		return
	}

	userID := utils.GetSessionUserID(r)
	if userID == nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	if err := r.ParseForm(); err != nil {
		http.Error(w, "Invalid form data", http.StatusBadRequest)
		return
	}

	action := strings.TrimSpace(r.FormValue("action"))
	ids, err := parseBulkTaskIDs(r.FormValue("ids"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if len(ids) == 0 {
		http.Error(w, "No tasks selected", http.StatusBadRequest)
		return
	}
	if len(ids) > maxBulkTaskIDs {
		http.Error(w, fmt.Sprintf("Maximum %d tasks per bulk action", maxBulkTaskIDs), http.StatusBadRequest)
		return
	}

	currentPage, _ := strconv.Atoi(r.FormValue("page"))
	if currentPage < 1 {
		currentPage = 1
	}

	db, err := storage.OpenDatabase()
	if err != nil {
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	defer db.Close()

	ctx := context.Background()
	if err := verifyTasksOwnedByUser(ctx, db, ids, *userID); err != nil {
		http.Error(w, err.Error(), http.StatusForbidden)
		return
	}

	switch action {
	case "complete":
		err = bulkSetCompleted(ctx, db, ids, *userID, true)
	case "incomplete":
		err = bulkSetCompleted(ctx, db, ids, *userID, false)
	case "delete":
		err = deleteTasksForUser(ctx, db, ids, *userID)
	case "move_project":
		err = bulkMoveProject(ctx, db, ids, *userID, strings.TrimSpace(r.FormValue("project_id")))
	case "add_tag":
		tagID, perr := strconv.Atoi(strings.TrimSpace(r.FormValue("tag_id")))
		if perr != nil || tagID <= 0 {
			http.Error(w, "Invalid tag", http.StatusBadRequest)
			return
		}
		err = bulkAddTag(ctx, db, ids, *userID, tagID)
	case "remove_tag":
		tagID, perr := strconv.Atoi(strings.TrimSpace(r.FormValue("tag_id")))
		if perr != nil || tagID <= 0 {
			http.Error(w, "Invalid tag", http.StatusBadRequest)
			return
		}
		err = bulkRemoveTag(ctx, db, ids, *userID, tagID)
	case "set_priority":
		priority, perr := strconv.Atoi(strings.TrimSpace(r.FormValue("priority")))
		if perr != nil || priority < 0 || priority > 3 {
			http.Error(w, "Invalid priority", http.StatusBadRequest)
			return
		}
		err = bulkSetPriority(ctx, db, ids, *userID, priority)
	default:
		http.Error(w, "Unknown bulk action", http.StatusBadRequest)
		return
	}
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	pageSize := utils.AppConstants.PageSize
	if sess, serr := sessionstore.Store.Get(r, "session"); serr == nil && sess != nil {
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
				if v, aerr := strconv.Atoi(tv); aerr == nil && v > 0 {
					pageSize = v
				}
			}
		}
	}

	fc := filterContextFromRequest(r)
	fc.Page = currentPage
	if err := renderFilteredTaskListPartial(w, r, currentPage, pageSize, fc, userID, timezone, loggedIn); err != nil {
		http.Error(w, "Error rendering tasks: "+err.Error(), http.StatusInternalServerError)
	}
}

func parseBulkTaskIDs(raw string) ([]int, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	parts := strings.Split(raw, ",")
	ids := make([]int, 0, len(parts))
	seen := make(map[int]bool)
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		id, err := strconv.Atoi(part)
		if err != nil || id <= 0 {
			return nil, fmt.Errorf("invalid task id in selection")
		}
		if !seen[id] {
			seen[id] = true
			ids = append(ids, id)
		}
	}
	return ids, nil
}

func verifyTasksOwnedByUser(ctx context.Context, db *pgxpool.Pool, ids []int, userID int) error {
	for _, id := range ids {
		var owner int
		if err := db.QueryRow(ctx, "SELECT user_id FROM tasks WHERE id = $1", id).Scan(&owner); err != nil {
			return fmt.Errorf("task not found")
		}
		if owner != userID {
			return fmt.Errorf("not authorized")
		}
	}
	return nil
}

func deleteTasksForUser(ctx context.Context, db *pgxpool.Pool, ids []int, userID int) error {
	for _, id := range ids {
		tag, err := db.Exec(ctx, "DELETE FROM tasks WHERE id = $1 AND user_id = $2", id, userID)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return fmt.Errorf("task not found or not authorized")
		}
	}
	return nil
}

func bulkSetCompleted(ctx context.Context, db *pgxpool.Pool, ids []int, userID int, completed bool) error {
	for _, id := range ids {
		if _, err := db.Exec(ctx, "UPDATE tasks SET completed = $1, date_modified = NOW() AT TIME ZONE 'UTC' WHERE id = $2 AND user_id = $3", completed, id, userID); err != nil {
			return err
		}
	}
	return nil
}

func bulkSetPriority(ctx context.Context, db *pgxpool.Pool, ids []int, userID int, priority int) error {
	for _, id := range ids {
		if _, err := db.Exec(ctx, "UPDATE tasks SET priority = $1, date_modified = NOW() AT TIME ZONE 'UTC' WHERE id = $2 AND user_id = $3", priority, id, userID); err != nil {
			return err
		}
	}
	return nil
}

func bulkMoveProject(ctx context.Context, db *pgxpool.Pool, ids []int, userID int, projectIDStr string) error {
	if projectIDStr == "" || projectIDStr == "0" {
		for _, id := range ids {
			if _, err := db.Exec(ctx, "UPDATE tasks SET project_id = NULL, date_modified = NOW() AT TIME ZONE 'UTC' WHERE id = $1 AND user_id = $2", id, userID); err != nil {
				return err
			}
		}
		return nil
	}
	pid, err := strconv.Atoi(projectIDStr)
	if err != nil {
		return fmt.Errorf("invalid project")
	}
	if _, err := storage.GetProjectByID(pid, userID); err != nil {
		return fmt.Errorf("invalid project selection")
	}
	for _, id := range ids {
		if _, err := db.Exec(ctx, "UPDATE tasks SET project_id = $1, date_modified = NOW() AT TIME ZONE 'UTC' WHERE id = $2 AND user_id = $3", pid, id, userID); err != nil {
			return err
		}
	}
	return nil
}

func bulkAddTag(ctx context.Context, db *pgxpool.Pool, ids []int, userID, tagID int) error {
	var tagExists bool
	if err := db.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM tags WHERE id = $1 AND user_id = $2)", tagID, userID).Scan(&tagExists); err != nil || !tagExists {
		return fmt.Errorf("invalid tag")
	}
	for _, taskID := range ids {
		existing, err := storage.GetTagsForTask(taskID)
		if err != nil {
			return err
		}
		tagIDs := make([]int, 0, len(existing)+1)
		seen := make(map[int]bool)
		for _, t := range existing {
			if !seen[t.ID] {
				seen[t.ID] = true
				tagIDs = append(tagIDs, t.ID)
			}
		}
		if !seen[tagID] {
			tagIDs = append(tagIDs, tagID)
		}
		if len(tagIDs) > storage.MaxTagsPerTask {
			continue
		}
		if err := storage.SetTaskTags(taskID, userID, tagIDs); err != nil {
			return err
		}
	}
	return nil
}

func bulkRemoveTag(ctx context.Context, db *pgxpool.Pool, ids []int, userID, tagID int) error {
	for _, taskID := range ids {
		if _, err := db.Exec(ctx, "DELETE FROM task_tags WHERE task_id = $1 AND tag_id = $2", taskID, tagID); err != nil {
			return err
		}
	}
	return nil
}
