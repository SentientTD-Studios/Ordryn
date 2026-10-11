package domain

import (
	"context"
	"errors"
	"fmt"
	"time"

	"GoTodo/internal/live"
	"GoTodo/internal/storage"
)

// trashListLimit bounds one trash listing.
const trashListLimit = 500

// TrashEntry is a trashed task the user can restore or delete for good.
type TrashEntry struct {
	storage.TrashedTask
	// PurgeAt is when the task is deleted permanently.
	PurgeAt time.Time
}

// TrashTasksForUser moves tasks (and their subtasks) to the trash. Every task
// must be live, readable, and deletable by the user, or nothing is trashed.
// Hooks and live updates fire after the rows are trashed.
func TrashTasksForUser(ctx context.Context, userID int, ids []int) ([]int, error) {
	_ = ctx
	for _, id := range ids {
		canRead, writeRole, projectID, err := storage.CanUserAccessTask(id, userID)
		if err != nil {
			return nil, err
		}
		if !canRead {
			return nil, ErrNotFound
		}
		if err := denyMissingTaskPerm(projectID, writeRole, storage.PermTasksDelete); err != nil {
			return nil, err
		}
	}
	trashed, err := storage.TrashTasks(ids, userID)
	if err != nil {
		return nil, err
	}
	if len(trashed) == 0 {
		return nil, ErrNotFound
	}
	for _, id := range ids {
		_ = storage.LogTaskEvent(id, userID, "deleted", nil)
		live.AfterTaskChange(userID, id, live.TypeTaskDeleted)
	}
	return trashed, nil
}

// DeleteTask moves a task the user can delete to the trash.
func DeleteTask(ctx context.Context, userID, taskID int) error {
	_, err := TrashTasksForUser(ctx, userID, []int{taskID})
	return err
}

func requireTrashedTaskDelete(taskID, userID int) error {
	canRead, writeRole, projectID, err := storage.CanUserAccessTrashedTask(taskID, userID)
	if err != nil {
		return err
	}
	if !canRead {
		return ErrNotFound
	}
	return denyMissingTaskPerm(projectID, writeRole, storage.PermTasksDelete)
}

// RestoreTrashedTaskForUser brings a task (and the subtasks trashed with it)
// back from the trash. Anyone who could delete the task can restore it.
func RestoreTrashedTaskForUser(ctx context.Context, userID, taskID int) ([]int, error) {
	_ = ctx
	if err := requireTrashedTaskDelete(taskID, userID); err != nil {
		return nil, err
	}
	restored, err := storage.RestoreTrashedTask(taskID)
	if errors.Is(err, storage.ErrTrashedParent) {
		return nil, fmt.Errorf("%w: restore its parent task first", ErrConflict)
	}
	if err != nil {
		return nil, err
	}
	if len(restored) == 0 {
		return nil, ErrNotFound
	}
	_ = storage.LogTaskEvent(taskID, userID, "restored_from_trash", nil)
	live.AfterTaskChange(userID, taskID, live.TypeTaskRestored)
	return restored, nil
}

// PurgeTrashedTaskForUser permanently deletes a trashed task and its subtasks.
func PurgeTrashedTaskForUser(ctx context.Context, userID, taskID int) error {
	_ = ctx
	if err := requireTrashedTaskDelete(taskID, userID); err != nil {
		return err
	}
	ok, err := storage.PurgeTrashedTask(taskID)
	if err != nil {
		return err
	}
	if !ok {
		return ErrNotFound
	}
	return nil
}

// ListTrashForUser returns trashed tasks the user may restore: their own
// personal tasks, and project tasks where their role can delete tasks.
func ListTrashForUser(ctx context.Context, userID int) ([]TrashEntry, error) {
	_ = ctx
	candidates, err := storage.ListTrashCandidates(userID, trashListLimit)
	if err != nil {
		return nil, err
	}
	days := storage.DefaultTrashRetentionDays
	if s, err := storage.GetSiteSettings(); err == nil && s != nil {
		days = s.TrashRetentionDays
	}
	keep := time.Duration(storage.ClampTrashRetentionDays(days)) * 24 * time.Hour

	canDelete := map[int]bool{}
	out := make([]TrashEntry, 0, len(candidates))
	for _, c := range candidates {
		if c.ProjectID <= 0 {
			if c.OwnerID != userID {
				continue
			}
		} else {
			allowed, seen := canDelete[c.ProjectID]
			if !seen {
				role, err := storage.GetProjectRole(c.ProjectID, userID)
				allowed = err == nil && role != "" && storage.HasProjectPerm(c.ProjectID, role, storage.PermTasksDelete)
				canDelete[c.ProjectID] = allowed
			}
			if !allowed {
				continue
			}
		}
		out = append(out, TrashEntry{TrashedTask: c, PurgeAt: c.DeletedAt.Add(keep)})
	}
	return out, nil
}

// EmptyTrashForUser permanently deletes everything in the user's trash view
// and returns how many entries it removed.
func EmptyTrashForUser(ctx context.Context, userID int) (int, error) {
	entries, err := ListTrashForUser(ctx, userID)
	if err != nil {
		return 0, err
	}
	n := 0
	for _, e := range entries {
		ok, err := storage.PurgeTrashedTask(e.ID)
		if err != nil {
			return n, err
		}
		if ok {
			n++
		}
	}
	return n, nil
}
