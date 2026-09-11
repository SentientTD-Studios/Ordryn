package handlers

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"GoTodo/internal/domain"
	"GoTodo/internal/live"
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
		canRead, writeRole, _, err := storage.CanUserAccessTask(id, userID)
		if err != nil {
			return err
		}
		if !canRead || !storage.RoleCanWrite(writeRole) {
			return fmt.Errorf("not authorized")
		}
	}
	_ = ctx
	_ = db
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
		live.AfterTaskChange(userID, id, live.TypeTaskDeleted)
		tag, err := db.Exec(ctx, "DELETE FROM tasks WHERE id = $1", id)
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
// Children of deleted parents are snapshotted for undo; FK CASCADE removes them with the parent.
func deleteTasksForAPI(ctx context.Context, db *pgxpool.Pool, r *http.Request, w http.ResponseWriter, ids []int, userID int) (string, error) {
	childIDs, err := domain.ChildIDsOf(ctx, ids)
	if err != nil {
		return "", err
	}
	snapshotIDs := uniqueInts(append(append([]int{}, ids...), childIDs...))
	snapshots, err := snapshotTasksForUndo(ctx, db, snapshotIDs, userID)
	if err != nil {
		return "", err
	}
	// Delete parents first when both parent and child are selected; CASCADE removes children.
	// Also delete orphan selected leaves (not children of another selected id).
	childSet := make(map[int]struct{}, len(childIDs))
	for _, id := range childIDs {
		childSet[id] = struct{}{}
	}
	idSet := make(map[int]struct{}, len(ids))
	for _, id := range ids {
		idSet[id] = struct{}{}
	}
	deleteIDs := make([]int, 0, len(ids))
	for _, id := range ids {
		if _, isChildOfSelected := childSet[id]; isChildOfSelected {
			// Will be removed via parent CASCADE if parent is also selected.
			if parentAlsoSelected(ctx, db, id, idSet) {
				continue
			}
		}
		deleteIDs = append(deleteIDs, id)
	}
	if err := deleteTaskRows(ctx, db, deleteIDs, userID); err != nil {
		return "", err
	}
	_ = savePendingUndo(r, w, snapshots)
	token, err := utils.SaveUndoToken(ctx, userID, toRedisUndoSnapshots(snapshots))
	if err != nil {
		return "", err
	}
	return token, nil
}

func uniqueInts(ids []int) []int {
	seen := make(map[int]struct{}, len(ids))
	out := make([]int, 0, len(ids))
	for _, id := range ids {
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	return out
}

func parentAlsoSelected(ctx context.Context, db *pgxpool.Pool, childID int, selected map[int]struct{}) bool {
	var parentID sql.NullInt64
	if err := db.QueryRow(ctx, `SELECT parent_id FROM tasks WHERE id = $1`, childID).Scan(&parentID); err != nil {
		return false
	}
	if !parentID.Valid {
		return false
	}
	_, ok := selected[int(parentID.Int64)]
	return ok
}

func bulkSetCompleted(ctx context.Context, db *pgxpool.Pool, ids []int, userID int, completed bool) error {
	_ = db
	for _, id := range ids {
		if err := domain.SetTaskCompleted(ctx, userID, id, completed); err != nil {
			return err
		}
	}
	return nil
}

func bulkSetStatus(ctx context.Context, db *pgxpool.Pool, ids []int, userID, statusID int) error {
	_ = db
	for _, id := range ids {
		sid := statusID
		statusPtr := &sid
		in := domain.UpdateTaskInput{StatusID: &statusPtr}
		if _, err := domain.UpdateTask(ctx, userID, id, in); err != nil {
			return err
		}
	}
	return nil
}

func bulkSetPriority(ctx context.Context, db *pgxpool.Pool, ids []int, userID int, priority int) error {
	for _, id := range ids {
		if _, err := db.Exec(ctx, "UPDATE tasks SET priority = $1, date_modified = NOW() AT TIME ZONE 'UTC' WHERE id = $2", priority, id); err != nil {
			return err
		}
		logTaskEvent(id, userID, "priority_changed", map[string]interface{}{"to": priorityLabel(priority)})
	}
	live.AfterTasksChange(userID, live.TypeTaskUpdated, ids)
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
			if _, err := db.Exec(ctx, "UPDATE tasks SET due_date = NULL, date_modified = NOW() AT TIME ZONE 'UTC' WHERE id = $1", id); err != nil {
				return err
			}
		} else {
			if _, err := db.Exec(ctx, "UPDATE tasks SET due_date = $1::date, date_modified = NOW() AT TIME ZONE 'UTC' WHERE id = $2", dueDate, id); err != nil {
				return err
			}
		}
		logTaskEvent(id, userID, "edited", map[string]interface{}{"fields": []string{"due_date"}})
	}
	live.AfterTasksChange(userID, live.TypeTaskUpdated, ids)
	return nil
}

func bulkMoveProject(ctx context.Context, db *pgxpool.Pool, ids []int, userID int, projectIDStr string) error {
	_ = db
	projectName := projectDisplayName(userID, projectIDFromForm(projectIDStr))
	if projectIDStr == "" || projectIDStr == "0" {
		for _, id := range ids {
			var clear *int
			projectPtr := &clear
			in := domain.UpdateTaskInput{ProjectID: projectPtr}
			if _, err := domain.UpdateTask(ctx, userID, id, in); err != nil {
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
	if err := domain.RequireProjectAcceptsNewTasks(pid, userID); err != nil {
		if errors.Is(err, domain.ErrConflict) {
			return err
		}
		return fmt.Errorf("invalid project selection")
	}
	for _, id := range ids {
		p := pid
		projectPtr := &p
		in := domain.UpdateTaskInput{ProjectID: &projectPtr}
		if _, err := domain.UpdateTask(ctx, userID, id, in); err != nil {
			return err
		}
		logTaskEvent(id, userID, "moved_project", map[string]interface{}{"project": projectName})
	}
	return nil
}

func bulkAddTag(ctx context.Context, db *pgxpool.Pool, ids []int, userID, tagID int) error {
	_ = db
	_ = ctx
	src, err := storage.GetTag(tagID)
	if err != nil {
		return fmt.Errorf("invalid tag")
	}
	ok, err := storage.UserCanAccessTag(userID, *src)
	if err != nil || !ok {
		return fmt.Errorf("invalid tag")
	}
	if src.Protected || storage.IsSystemTagName(src.Name) {
		return fmt.Errorf("cannot assign a protected tag")
	}
	for _, taskID := range ids {
		existing, err := storage.GetTagsForTask(taskID)
		if err != nil {
			return err
		}
		taskProjectID, err := storage.GetTaskProjectID(taskID)
		if err != nil {
			return err
		}
		var ns *int
		if taskProjectID > 0 {
			ns = &taskProjectID
		}
		dest, err := storage.FindTagByName(userID, ns, src.Name)
		if err != nil {
			canManage, manErr := storage.UserCanManageTagNamespace(userID, ns)
			if manErr != nil || !canManage {
				continue
			}
			dest, err = storage.GetOrCreateTagByName(userID, ns, src.Name)
			if err != nil {
				return err
			}
		}
		tagIDs := make([]int, 0, len(existing)+1)
		seen := make(map[int]bool)
		for _, t := range existing {
			if !seen[t.ID] {
				seen[t.ID] = true
				tagIDs = append(tagIDs, t.ID)
			}
		}
		if !seen[dest.ID] {
			tagIDs = append(tagIDs, dest.ID)
		}
		if len(tagIDs) > storage.MaxTagsPerTask {
			continue
		}
		if err := storage.SetTaskTags(taskID, userID, tagIDs); err != nil {
			return err
		}
		logTaskEvent(taskID, userID, "tag_added", map[string]interface{}{"tag": dest.Name, "tag_id": dest.ID})
	}
	live.AfterTasksChange(userID, live.TypeTaskUpdated, ids)
	return nil
}

func bulkRemoveTag(ctx context.Context, db *pgxpool.Pool, ids []int, userID, tagID int) error {
	src, err := storage.GetTag(tagID)
	if err != nil {
		return fmt.Errorf("invalid tag")
	}
	ok, err := storage.UserCanAccessTag(userID, *src)
	if err != nil || !ok {
		return fmt.Errorf("invalid tag")
	}
	if src.Protected || storage.IsSystemTagName(src.Name) {
		return fmt.Errorf("cannot remove a protected tag")
	}
	for _, taskID := range ids {
		if _, err := db.Exec(ctx, `
			DELETE FROM task_tags tt
			USING tags tg
			WHERE tt.task_id = $1 AND tt.tag_id = tg.id AND LOWER(tg.name) = LOWER($2)`,
			taskID, src.Name); err != nil {
			return err
		}
		logTaskEvent(taskID, userID, "tag_removed", map[string]interface{}{"tag": src.Name, "tag_id": tagID})
	}
	live.AfterTasksChange(userID, live.TypeTaskUpdated, ids)
	return nil
}
