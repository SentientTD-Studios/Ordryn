package domain

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"strings"

	"GoTodo/internal/live"
	"GoTodo/internal/storage"
)

const joinRequestNotifyPreviewMax = 120

// NotifyAdminsOfJoinRequest fans out in-app notifications when a visitor
// asks to join. Best-effort: failures are logged and do not fail the create.
func NotifyAdminsOfJoinRequest(email, message string) {
	ids, err := storage.ListAdminUserIDs()
	if err != nil {
		log.Printf("notify join_request: list admins: %v", err)
	}
	if len(ids) > 0 {
		title := "New join request"
		body := joinRequestNotificationBody(email, message)
		items := make([]storage.UserNotification, 0, len(ids))
		for _, id := range ids {
			items = append(items, storage.UserNotification{
				UserID: id,
				Type:   storage.NotificationJoinRequest,
				Title:  title,
				Body:   body,
			})
		}
		if err := storage.CreateUserNotificationsBulk(items); err != nil {
			log.Printf("notify join_request: insert: %v", err)
		} else {
			live.Push(live.Event{Type: live.TypeJoinRequest}, ids)
		}
	}
	live.AfterJoinRequest(email, message)
}

func joinRequestNotificationBody(email, message string) string {
	email = strings.TrimSpace(email)
	message = strings.TrimSpace(message)
	if message == "" {
		return email
	}
	if len(message) > joinRequestNotifyPreviewMax {
		message = message[:joinRequestNotifyPreviewMax-3] + "..."
	}
	return email + "\n" + message
}

// NotifyProjectMembersTaskCreated fans out in-app notifications for a new kanban task.
// Best-effort: failures are logged and do not fail task creation.
func NotifyProjectMembersTaskCreated(taskID, actorUserID, projectID int, taskTitle string) {
	if projectID <= 0 || taskID <= 0 {
		return
	}
	mode, err := storage.GetProjectWorkflowMode(projectID)
	if err != nil || mode != storage.WorkflowKanban {
		return
	}

	members, err := storage.ListProjectMembers(projectID)
	if err != nil {
		log.Printf("notify task_created: list members project=%d: %v", projectID, err)
		return
	}

	proj, err := storage.GetAccessibleProjectByID(projectID, actorUserID)
	projectName := ""
	if err == nil && proj != nil {
		projectName = proj.Name
	}

	title := "New task in project"
	if projectName != "" {
		title = fmt.Sprintf("New task in %s", projectName)
	}
	body := taskTitle
	if body == "" {
		body = "A new task was posted"
	}

	items := make([]storage.UserNotification, 0, len(members))
	for _, m := range members {
		if m.UserID == actorUserID {
			continue
		}
		items = append(items, storage.UserNotification{
			UserID:      m.UserID,
			ActorUserID: actorUserID,
			Type:        storage.NotificationTaskCreated,
			ProjectID:   projectID,
			TaskID:      taskID,
			Title:       title,
			Body:        body,
		})
	}
	if err := storage.CreateUserNotificationsBulk(items); err != nil {
		log.Printf("notify task_created: insert task=%d: %v", taskID, err)
	}
}

// NotifyProjectMembersTaskCommented fans out in-app notifications for a new discussion post.
// Best-effort: failures are logged and do not fail the comment write.
func NotifyProjectMembersTaskCommented(taskID, actorUserID, projectID int, commentBody string) {
	if projectID <= 0 || taskID <= 0 {
		return
	}

	members, err := storage.ListProjectMembers(projectID)
	if err != nil {
		log.Printf("notify task_commented: list members project=%d: %v", projectID, err)
		return
	}

	proj, err := storage.GetAccessibleProjectByID(projectID, actorUserID)
	projectName := ""
	if err == nil && proj != nil {
		projectName = proj.Name
	}

	taskTitle := ""
	if td, err := storage.GetTaskTitleDescription(taskID); err == nil && td != nil {
		taskTitle = td.Title
	}

	commentTitle := "New comment on a task"
	if taskTitle != "" {
		commentTitle = fmt.Sprintf("New comment on %s", taskTitle)
	} else if projectName != "" {
		commentTitle = fmt.Sprintf("New comment in %s", projectName)
	}
	mentionTitle := "You were mentioned on a task"
	if taskTitle != "" {
		mentionTitle = fmt.Sprintf("You were mentioned on %s", taskTitle)
	} else if projectName != "" {
		mentionTitle = fmt.Sprintf("You were mentioned in %s", projectName)
	}
	body := commentPreview(commentBody)

	mentioned := make(map[int]struct{})
	for _, m := range ResolveCommentMentions(projectID, commentBody) {
		if m.UserID == actorUserID {
			continue
		}
		mentioned[m.UserID] = struct{}{}
	}

	items := make([]storage.UserNotification, 0, len(members))
	for _, m := range members {
		if m.UserID == actorUserID {
			continue
		}
		nType := storage.NotificationTaskCommented
		title := commentTitle
		if _, ok := mentioned[m.UserID]; ok {
			nType = storage.NotificationTaskMentioned
			title = mentionTitle
		}
		items = append(items, storage.UserNotification{
			UserID:      m.UserID,
			ActorUserID: actorUserID,
			Type:        nType,
			ProjectID:   projectID,
			TaskID:      taskID,
			Title:       title,
			Body:        body,
		})
	}
	if err := storage.CreateUserNotificationsBulk(items); err != nil {
		log.Printf("notify task_commented: insert task=%d: %v", taskID, err)
	}
}

// ListNotificationsForUser returns paginated notifications.
func ListNotificationsForUser(ctx context.Context, userID, page, perPage int) ([]storage.UserNotification, int, error) {
	_ = ctx
	if page <= 0 {
		page = 1
	}
	if perPage <= 0 {
		perPage = 20
	}
	offset := (page - 1) * perPage
	return storage.ListUserNotifications(userID, perPage, offset)
}

// UnreadNotificationCountForUser returns unread count.
func UnreadNotificationCountForUser(ctx context.Context, userID int) (int, error) {
	_ = ctx
	return storage.CountUnreadUserNotifications(userID)
}

// MarkNotificationReadForUser marks one notification read.
func MarkNotificationReadForUser(ctx context.Context, userID, notificationID int) error {
	_ = ctx
	err := storage.MarkUserNotificationRead(userID, notificationID)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	return err
}

// MarkAllNotificationsReadForUser marks all unread for the user.
func MarkAllNotificationsReadForUser(ctx context.Context, userID int) error {
	_ = ctx
	return storage.MarkAllUserNotificationsRead(userID)
}
