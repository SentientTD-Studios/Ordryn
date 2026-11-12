package domain

import (
	"context"
	"fmt"
	"log"
	"strings"

	"GoTodo/internal/live"
	"GoTodo/internal/storage"
)

// NotificationReviewRequested tells a member they were asked to review a task.
const NotificationReviewRequested = "review_requested"

// SetTaskReviewerForUser asks reviewerID to review a project task. The caller
// needs tasks:edit. The reviewer is given by id or, when reviewerID is 0, by
// username. They must be a person (not an agent or the system account) who can
// see the task.
func SetTaskReviewerForUser(ctx context.Context, userID, taskID, reviewerID int, reviewerName string) error {
	_ = ctx
	projectID, err := reviewerTaskAccess(userID, taskID, false)
	if err != nil {
		return err
	}
	if reviewerID <= 0 {
		name := strings.TrimPrefix(strings.TrimSpace(reviewerName), "@")
		if name == "" {
			return fmt.Errorf("%w: user_id or user_name is required", ErrValidation)
		}
		u, err := storage.GetUserByUsername(name)
		if err != nil || u == nil {
			return fmt.Errorf("%w: the reviewer must be a member of this project", ErrValidation)
		}
		reviewerID = u.ID
	}
	if err := validateReviewer(taskID, reviewerID); err != nil {
		return err
	}
	prev, err := storage.GetTaskReviewer(taskID)
	if err != nil {
		return err
	}
	if prev == reviewerID {
		return nil
	}
	rid := reviewerID
	if err := storage.SetTaskReviewer(taskID, &rid); err != nil {
		return err
	}
	meta := map[string]any{"reviewer_id": reviewerID}
	if prev > 0 {
		meta["previous_reviewer_id"] = prev
	}
	_ = storage.LogTaskEvent(taskID, userID, "reviewer_set", meta)
	live.AfterTaskChangeLive(userID, taskID, live.TypeTaskUpdated)
	live.DispatchHook(userID, taskID, live.TypeTaskUpdated, &live.TaskHookMeta{Changed: []string{"reviewer_id"}})
	autoWatchTask(reviewerID, taskID)
	if reviewerID != userID {
		notifyReviewer(userID, taskID, projectID, reviewerID)
	}
	return nil
}

// ClearTaskReviewerForUser removes the reviewer. Editors can always clear it;
// the reviewer can also step down without edit rights.
func ClearTaskReviewerForUser(ctx context.Context, userID, taskID int) error {
	_ = ctx
	if _, err := reviewerTaskAccess(userID, taskID, true); err != nil {
		return err
	}
	prev, err := storage.GetTaskReviewer(taskID)
	if err != nil {
		return err
	}
	if prev != userID {
		if _, err := reviewerTaskAccess(userID, taskID, false); err != nil {
			return err
		}
	}
	if prev == 0 {
		return nil
	}
	if err := storage.SetTaskReviewer(taskID, nil); err != nil {
		return err
	}
	_ = storage.LogTaskEvent(taskID, userID, "reviewer_cleared", map[string]any{"previous_reviewer_id": prev})
	live.AfterTaskChangeLive(userID, taskID, live.TypeTaskUpdated)
	live.DispatchHook(userID, taskID, live.TypeTaskUpdated, &live.TaskHookMeta{Changed: []string{"reviewer_id"}})
	return nil
}

// reviewerTaskAccess checks the caller can see the task, that it belongs to a
// project, and (unless selfOnly) that the caller has tasks:edit.
func reviewerTaskAccess(userID, taskID int, selfOnly bool) (int, error) {
	canRead, writeRole, projectID, err := storage.CanUserAccessTask(taskID, userID)
	if err != nil {
		return 0, err
	}
	if !canRead {
		return 0, ErrNotFound
	}
	if projectID <= 0 {
		return 0, fmt.Errorf("%w: only project tasks can have a reviewer", ErrValidation)
	}
	if selfOnly {
		return projectID, nil
	}
	if writeRole == "" {
		return 0, fmt.Errorf("%w: your role cannot edit task details", ErrForbidden)
	}
	if err := denyMissingTaskPerm(projectID, writeRole, storage.PermTasksEdit); err != nil {
		return 0, err
	}
	return projectID, nil
}

func validateReviewer(taskID, reviewerID int) error {
	if storage.IsSystemUser(reviewerID) || storage.IsAgentUser(reviewerID) || storage.IsUserBannedByID(reviewerID) {
		return fmt.Errorf("%w: the reviewer must be a person on this project", ErrValidation)
	}
	ok, _, _, err := storage.CanUserAccessTask(taskID, reviewerID)
	if err != nil || !ok {
		return fmt.Errorf("%w: the reviewer must be a member of this project", ErrValidation)
	}
	return nil
}

func notifyReviewer(actorID, taskID, projectID, reviewerID int) {
	delivered, err := storage.CreateUserNotificationsBulk([]storage.UserNotification{{
		UserID:      reviewerID,
		ActorUserID: actorID,
		Type:        NotificationReviewRequested,
		ProjectID:   projectID,
		TaskID:      taskID,
		Title:       "Review requested: " + taskTitleOrID(taskID),
	}})
	if err != nil {
		log.Printf("reviewer: notify task %d: %v", taskID, err)
		return
	}
	if len(delivered) > 0 {
		live.Push(live.Event{Type: live.TypeNotificationCreated, TaskID: taskID}, delivered)
	}
}
