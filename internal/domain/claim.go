package domain

import (
	"context"
	"fmt"

	"GoTodo/internal/live"
	"GoTodo/internal/storage"
)

// ClaimTaskForUser sets claimed_by to the current user (allows takeover).
func ClaimTaskForUser(ctx context.Context, userID, taskID int) error {
	_ = ctx
	canRead, writeRole, projectID, err := storage.CanUserAccessTask(taskID, userID)
	if err != nil {
		return err
	}
	if !canRead {
		return ErrNotFound
	}
	if !storage.RoleCanWrite(writeRole) {
		return ErrForbidden
	}
	if projectID <= 0 {
		return fmt.Errorf("%w: only kanban project tasks can be claimed", ErrValidation)
	}
	mode, err := storage.GetProjectWorkflowMode(projectID)
	if err != nil {
		return err
	}
	if mode != storage.WorkflowKanban {
		return fmt.Errorf("%w: only kanban project tasks can be claimed", ErrValidation)
	}

	prev, err := storage.GetTaskClaimedBy(taskID)
	if err != nil {
		return err
	}
	claimer := userID
	if err := storage.SetTaskClaimedBy(taskID, &claimer); err != nil {
		return err
	}
	meta := map[string]interface{}{"claimed_by": userID}
	if prev > 0 {
		meta["previous_claimed_by"] = prev
	}
	_ = storage.LogTaskEvent(taskID, userID, "claimed", meta)
	live.AfterTaskChangeLive(userID, taskID, live.TypeTaskUpdated)
	live.DispatchHook(userID, taskID, live.TypeTaskClaimed, &live.TaskHookMeta{Changed: []string{"claimed_by"}})
	return nil
}

// UnclaimTaskForUser clears claimed_by when the caller can write.
func UnclaimTaskForUser(ctx context.Context, userID, taskID int) error {
	_ = ctx
	canRead, writeRole, projectID, err := storage.CanUserAccessTask(taskID, userID)
	if err != nil {
		return err
	}
	if !canRead {
		return ErrNotFound
	}
	if !storage.RoleCanWrite(writeRole) {
		return ErrForbidden
	}
	if projectID <= 0 {
		return fmt.Errorf("%w: only kanban project tasks can be unclaimed", ErrValidation)
	}
	mode, err := storage.GetProjectWorkflowMode(projectID)
	if err != nil {
		return err
	}
	if mode != storage.WorkflowKanban {
		return fmt.Errorf("%w: only kanban project tasks can be unclaimed", ErrValidation)
	}

	prev, err := storage.GetTaskClaimedBy(taskID)
	if err != nil {
		return err
	}
	if err := storage.SetTaskClaimedBy(taskID, nil); err != nil {
		return err
	}
	meta := map[string]interface{}{}
	if prev > 0 {
		meta["previous_claimed_by"] = prev
	}
	_ = storage.LogTaskEvent(taskID, userID, "unclaimed", meta)
	live.AfterTaskChangeLive(userID, taskID, live.TypeTaskUpdated)
	live.DispatchHook(userID, taskID, live.TypeTaskUnclaimed, &live.TaskHookMeta{Changed: []string{"claimed_by"}})
	return nil
}
