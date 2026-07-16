package handlers

import (
	"context"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"GoTodo/internal/server/utils"
	"GoTodo/internal/storage"

	"github.com/jackc/pgx/v5/pgxpool"
)

const maxBulkTaskIDs = 100

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

func deleteTasksForUser(ctx context.Context, db *pgxpool.Pool, r *http.Request, w http.ResponseWriter, ids []int, userID int) error {
	snapshots, err := snapshotTasksForUndo(ctx, db, ids, userID)
	if err != nil {
		return err
	}
	if err := savePendingUndo(r, w, snapshots); err != nil {
		return err
	}
	return deleteTaskRows(ctx, db, ids, userID)
}

func deleteTaskRows(ctx context.Context, db *pgxpool.Pool, ids []int, userID int) error {
	for _, id := range ids {
		logTaskEvent(id, userID, "deleted", nil)
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

// deleteTasksForAPI deletes tasks and returns a Redis undo token (session undo saved when possible).
func deleteTasksForAPI(ctx context.Context, db *pgxpool.Pool, r *http.Request, w http.ResponseWriter, ids []int, userID int) (string, error) {
	snapshots, err := snapshotTasksForUndo(ctx, db, ids, userID)
	if err != nil {
		return "", err
	}
	if err := deleteTaskRows(ctx, db, ids, userID); err != nil {
		return "", err
	}
	_ = savePendingUndo(r, w, snapshots)
	token, err := utils.SaveUndoToken(ctx, userID, toRedisUndoSnapshots(snapshots))
	if err != nil {
		return "", err
	}
	return token, nil
}

func bulkSetCompleted(ctx context.Context, db *pgxpool.Pool, ids []int, userID int, completed bool) error {
	for _, id := range ids {
		if _, err := db.Exec(ctx, "UPDATE tasks SET completed = $1, date_modified = NOW() AT TIME ZONE 'UTC' WHERE id = $2 AND user_id = $3", completed, id, userID); err != nil {
			return err
		}
		if completed {
			logTaskEvent(id, userID, "completed", nil)
		} else {
			logTaskEvent(id, userID, "reopened", nil)
		}
	}
	return nil
}

func bulkSetPriority(ctx context.Context, db *pgxpool.Pool, ids []int, userID int, priority int) error {
	for _, id := range ids {
		if _, err := db.Exec(ctx, "UPDATE tasks SET priority = $1, date_modified = NOW() AT TIME ZONE 'UTC' WHERE id = $2 AND user_id = $3", priority, id, userID); err != nil {
			return err
		}
		logTaskEvent(id, userID, "priority_changed", map[string]interface{}{"to": priorityLabel(priority)})
	}
	return nil
}

var bulkDueDatePattern = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)

// parseBulkDueDate validates YYYY-MM-DD or returns empty string to clear the due date.
func parseBulkDueDate(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", nil
	}
	if !bulkDueDatePattern.MatchString(raw) {
		return "", fmt.Errorf("invalid due date format")
	}
	return raw, nil
}

func bulkSetDueDate(ctx context.Context, db *pgxpool.Pool, ids []int, userID int, dueDate string) error {
	for _, id := range ids {
		if dueDate == "" {
			if _, err := db.Exec(ctx, "UPDATE tasks SET due_date = NULL, date_modified = NOW() AT TIME ZONE 'UTC' WHERE id = $1 AND user_id = $2", id, userID); err != nil {
				return err
			}
		} else {
			if _, err := db.Exec(ctx, "UPDATE tasks SET due_date = $1::date, date_modified = NOW() AT TIME ZONE 'UTC' WHERE id = $2 AND user_id = $3", dueDate, id, userID); err != nil {
				return err
			}
		}
		logTaskEvent(id, userID, "edited", map[string]interface{}{"fields": []string{"due_date"}})
	}
	return nil
}

func bulkMoveProject(ctx context.Context, db *pgxpool.Pool, ids []int, userID int, projectIDStr string) error {
	projectName := projectDisplayName(userID, projectIDFromForm(projectIDStr))
	if projectIDStr == "" || projectIDStr == "0" {
		for _, id := range ids {
			if _, err := db.Exec(ctx, "UPDATE tasks SET project_id = NULL, date_modified = NOW() AT TIME ZONE 'UTC' WHERE id = $1 AND user_id = $2", id, userID); err != nil {
				return err
			}
			logTaskEvent(id, userID, "moved_project", map[string]interface{}{"project": projectName})
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
		logTaskEvent(id, userID, "moved_project", map[string]interface{}{"project": projectName})
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
		var tagName string
		if t, err := storage.GetTagsForUser(userID); err == nil {
			for _, tg := range t {
				if tg.ID == tagID {
					tagName = tg.Name
					break
				}
			}
		}
		logTaskEvent(taskID, userID, "tag_added", map[string]interface{}{"tag": tagName, "tag_id": tagID})
	}
	return nil
}

func bulkRemoveTag(ctx context.Context, db *pgxpool.Pool, ids []int, userID, tagID int) error {
	var tagName string
	if tags, err := storage.GetTagsForUser(userID); err == nil {
		for _, tg := range tags {
			if tg.ID == tagID {
				tagName = tg.Name
				break
			}
		}
	}
	for _, taskID := range ids {
		if _, err := db.Exec(ctx, "DELETE FROM task_tags WHERE task_id = $1 AND tag_id = $2", taskID, tagID); err != nil {
			return err
		}
		logTaskEvent(taskID, userID, "tag_removed", map[string]interface{}{"tag": tagName, "tag_id": tagID})
	}
	return nil
}
