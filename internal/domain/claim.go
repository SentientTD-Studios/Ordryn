package domain

import (
	"context"
	"fmt"

	"GoTodo/internal/live"
	"GoTodo/internal/storage"
)

// ClaimOptions guards a claim against a view that is out of date.
type ClaimOptions struct {
	// TakeOver lets the caller replace another member's claim. Without it a
	// claim held by someone else is a conflict, so nobody takes work from a
	// teammate without being asked first.
	TakeOver bool
	// ExpectedClaimedBy is who the caller saw holding the task (0 = nobody).
	// When set, a takeover only proceeds while that is still true.
	ExpectedClaimedBy *int
}

// UnclaimOptions guards a release against a view that is out of date.
type UnclaimOptions struct {
	// ExpectedClaimedBy is who the caller saw holding the task. When set and
	// someone else holds it now, the release is a conflict instead of
	// dropping their claim.
	ExpectedClaimedBy *int
}

// ClaimConflictError is ErrConflict with the claimer actually stored, so
// clients can tell the person who holds the task now.
type ClaimConflictError struct {
	ClaimedBy     int
	ClaimedByName string
	Message       string
}

func (e *ClaimConflictError) Error() string { return e.Message }
func (e *ClaimConflictError) Unwrap() error { return ErrConflict }

func claimConflict(current int, format string) error {
	name := claimerName(current)
	return &ClaimConflictError{
		ClaimedBy:     current,
		ClaimedByName: name,
		Message:       fmt.Sprintf(format, name),
	}
}

func claimerName(userID int) string {
	if userID <= 0 {
		return ""
	}
	name, email := storage.UserNameAndEmail(userID)
	if name != "" {
		return name
	}
	if email != "" {
		return email
	}
	return "another member"
}

func checkClaimAccess(userID, taskID int, verb string) error {
	canRead, writeRole, projectID, err := storage.CanUserAccessTask(taskID, userID)
	if err != nil {
		return err
	}
	if !canRead {
		return ErrNotFound
	}
	if err := denyMissingTaskPerm(projectID, writeRole, storage.PermTasksClaim); err != nil {
		return err
	}
	if projectID <= 0 {
		return fmt.Errorf("%w: only kanban project tasks can be %s", ErrValidation, verb)
	}
	mode, err := storage.GetProjectWorkflowMode(projectID)
	if err != nil {
		return err
	}
	if mode != storage.WorkflowKanban {
		return fmt.Errorf("%w: only kanban project tasks can be %s", ErrValidation, verb)
	}
	return nil
}

// ClaimTaskForUser sets claimed_by to the caller. Claiming a task the caller
// already holds changes nothing; taking one from someone else needs
// opts.TakeOver.
func ClaimTaskForUser(ctx context.Context, userID, taskID int, opts ClaimOptions) error {
	_ = ctx
	if err := checkClaimAccess(userID, taskID, "claimed"); err != nil {
		return err
	}

	prev, err := storage.GetTaskClaimedBy(taskID)
	if err != nil {
		return err
	}
	if prev == userID {
		return nil
	}
	if prev > 0 {
		if !opts.TakeOver {
			return claimConflict(prev, "%s has already claimed this task.")
		}
		if opts.ExpectedClaimedBy != nil && *opts.ExpectedClaimedBy != prev {
			return claimConflict(prev, "This task is now claimed by %s.")
		}
	}
	claimer := userID
	ok, current, err := storage.CompareAndSetTaskClaimedBy(taskID, prev, &claimer)
	if err != nil {
		return err
	}
	if !ok {
		// Someone else changed the claim between our read and write.
		if current == userID {
			return nil
		}
		if current == 0 {
			return fmt.Errorf("%w: this task's claim just changed; refresh and try again", ErrConflict)
		}
		return claimConflict(current, "This task was just claimed by %s.")
	}
	meta := map[string]interface{}{"claimed_by": userID}
	if prev > 0 {
		meta["previous_claimed_by"] = prev
	}
	_ = storage.LogTaskEvent(taskID, userID, "claimed", meta)
	live.AfterTaskChangeLive(userID, taskID, live.TypeTaskUpdated)
	live.DispatchHook(userID, taskID, live.TypeTaskClaimed, &live.TaskHookMeta{Changed: []string{"claimed_by"}})
	autoWatchTask(userID, taskID)
	notifyTaskWatchers(userID, taskID, NotificationTaskActivity, "Claimed: "+taskTitleOrID(taskID), "")
	return nil
}

// UnclaimTaskForUser clears claimed_by when the caller can write. Releasing
// a task nobody holds changes nothing.
func UnclaimTaskForUser(ctx context.Context, userID, taskID int, opts UnclaimOptions) error {
	_ = ctx
	if err := checkClaimAccess(userID, taskID, "unclaimed"); err != nil {
		return err
	}

	prev, err := storage.GetTaskClaimedBy(taskID)
	if err != nil {
		return err
	}
	if prev == 0 {
		return nil
	}
	if opts.ExpectedClaimedBy != nil && *opts.ExpectedClaimedBy != prev {
		return claimConflict(prev, "This task is now claimed by %s.")
	}
	ok, current, err := storage.CompareAndSetTaskClaimedBy(taskID, prev, nil)
	if err != nil {
		return err
	}
	if !ok {
		if current == 0 {
			return nil
		}
		return claimConflict(current, "This task was just claimed by %s.")
	}
	_ = storage.LogTaskEvent(taskID, userID, "unclaimed", map[string]interface{}{"previous_claimed_by": prev})
	live.AfterTaskChangeLive(userID, taskID, live.TypeTaskUpdated)
	live.DispatchHook(userID, taskID, live.TypeTaskUnclaimed, &live.TaskHookMeta{Changed: []string{"claimed_by"}})
	notifyTaskWatchers(userID, taskID, NotificationTaskActivity, "Unclaimed: "+taskTitleOrID(taskID), "")
	return nil
}
