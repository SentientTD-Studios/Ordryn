package handlers

import (
	"context"
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
	return verifyTasksHavePerm(ctx, db, ids, userID, storage.PermTasksEdit)
}

func verifyTasksHavePerm(ctx context.Context, db *pgxpool.Pool, ids []int, userID int, perm string) error {
	for _, id := range ids {
		canRead, writeRole, projectID, err := storage.CanUserAccessTask(id, userID)
		if err != nil {
			return err
		}
		if !canRead {
			return fmt.Errorf("not authorized")
		}
		if projectID <= 0 {
			if !storage.RoleCanWrite(writeRole) {
				return fmt.Errorf("not authorized")
			}
			continue
		}
		if !storage.HasProjectPerm(projectID, writeRole, perm) {
			return fmt.Errorf("not authorized")
		}
	}
	_ = ctx
	_ = db
	return nil
}

// deleteTasksForAPI moves tasks (and their subtasks) to the trash and returns
// a Redis undo token; the session also keeps the undo when possible. Undo and
// the Trash view restore them with nothing lost.
func deleteTasksForAPI(ctx context.Context, r *http.Request, w http.ResponseWriter, ids []int, userID int) (string, error) {
	if _, err := domain.TrashTasksForUser(ctx, userID, ids); err != nil {
		if errors.Is(err, domain.ErrNotFound) || errors.Is(err, domain.ErrForbidden) {
			return "", fmt.Errorf("task not found or not authorized")
		}
		return "", err
	}
	snapshots := make([]DeletedTaskSnapshot, 0, len(ids))
	for _, id := range ids {
		snapshots = append(snapshots, DeletedTaskSnapshot{ID: id})
	}
	_ = savePendingUndo(r, w, snapshots)
	return utils.SaveUndoToken(ctx, userID, toRedisUndoSnapshots(snapshots))
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

func bulkSetSprint(ctx context.Context, db *pgxpool.Pool, ids []int, userID int, sprintID *int) error {
	_ = db
	var ptr *int
	if sprintID != nil && *sprintID > 0 {
		sid := *sprintID
		ptr = &sid
	}
	for _, id := range ids {
		in := domain.UpdateTaskInput{SprintID: &ptr}
		if _, err := domain.UpdateTask(ctx, userID, id, in); err != nil {
			return err
		}
	}
	return nil
}

func bulkSetPriority(ctx context.Context, db *pgxpool.Pool, ids []int, userID int, priority int) error {
	_ = db
	for _, id := range ids {
		p := priority
		in := domain.UpdateTaskInput{Priority: &p}
		if _, err := domain.UpdateTask(ctx, userID, id, in); err != nil {
			return err
		}
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
	_ = db
	for _, id := range ids {
		in := domain.UpdateTaskInput{}
		if dueDate == "" {
			in.ClearDue = true
		} else {
			d := dueDate
			in.DueDate = &d
		}
		if _, err := domain.UpdateTask(ctx, userID, id, in); err != nil {
			return err
		}
	}
	live.AfterTasksChangeLive(userID, live.TypeTaskUpdated, ids)
	for _, id := range ids {
		live.DispatchHook(userID, id, live.TypeTaskDueChanged, &live.TaskHookMeta{Changed: []string{"due_date"}})
	}
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
	live.AfterTasksChangeLive(userID, live.TypeTaskUpdated, ids)
	for _, id := range ids {
		live.DispatchHook(userID, id, live.TypeTaskTagged, &live.TaskHookMeta{Changed: []string{"tags"}})
	}
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
	live.AfterTasksChangeLive(userID, live.TypeTaskUpdated, ids)
	for _, id := range ids {
		live.DispatchHook(userID, id, live.TypeTaskTagged, &live.TaskHookMeta{Changed: []string{"tags"}})
	}
	return nil
}
