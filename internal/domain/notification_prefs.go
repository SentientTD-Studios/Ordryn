package domain

import (
	"context"
	"fmt"

	"GoTodo/internal/storage"
)

// NotificationOption is a notification type a user may turn off. Types not in
// optionalNotifications (password reset/changed emails, invites) are always sent.
type NotificationOption struct {
	Type        string
	Label       string
	Description string
	AdminOnly   bool
}

// optionalNotifications is the opt-out registry, in display order.
var optionalNotifications = []NotificationOption{
	{Type: storage.NotificationTaskCreated, Label: "New tasks", Description: "A task is posted in a kanban project you belong to."},
	{Type: storage.NotificationTaskCommented, Label: "Comments", Description: "Someone comments on a task in a project you belong to."},
	{Type: storage.NotificationTaskMentioned, Label: "Mentions", Description: "Someone @mentions you in a comment. When off, you get a regular comment notification instead if comments are on."},
	{Type: NotificationTaskActivity, Label: "Watched task activity", Description: "A task you watch is claimed, moved, blocked, completed, reopened, or has its due date changed."},
	{Type: NotificationTaskUnblocked, Label: "Unblocked tasks", Description: "A task you watch has its last open blocker completed."},
	{Type: storage.NotificationJoinRequest, Label: "Join requests", Description: "Someone asks to join the site. Covers both the in-app notification and the email.", AdminOnly: true},
}

// NotificationPreference is one optional type and whether the user receives it.
type NotificationPreference struct {
	NotificationOption
	Enabled bool
}

func notificationOptionsFor(userID int) []NotificationOption {
	// AI agents have no inbox, so there is nothing to opt out of.
	if storage.IsAgentUser(userID) {
		return nil
	}
	isAdmin := storage.UserHasPermission(userID, "admin")
	out := make([]NotificationOption, 0, len(optionalNotifications))
	for _, o := range optionalNotifications {
		if o.AdminOnly && !isAdmin {
			continue
		}
		out = append(out, o)
	}
	return out
}

// ListNotificationPreferences returns the optional notification types visible to the user.
func ListNotificationPreferences(ctx context.Context, userID int) ([]NotificationPreference, error) {
	_ = ctx
	optedOut, err := storage.ListUserNotificationOptOuts(userID)
	if err != nil {
		return nil, err
	}
	opts := notificationOptionsFor(userID)
	out := make([]NotificationPreference, 0, len(opts))
	for _, o := range opts {
		out = append(out, NotificationPreference{NotificationOption: o, Enabled: !optedOut[o.Type]})
	}
	return out, nil
}

// UpdateNotificationPreferences applies enabled/disabled changes keyed by type.
// Unknown, required, or admin-only types (for non-admins) are rejected.
func UpdateNotificationPreferences(ctx context.Context, userID int, changes map[string]bool) ([]NotificationPreference, error) {
	allowed := map[string]bool{}
	for _, o := range notificationOptionsFor(userID) {
		allowed[o.Type] = true
	}
	for t := range changes {
		if !allowed[t] {
			return nil, fmt.Errorf("%w: unknown notification type %q", ErrValidation, t)
		}
	}
	for t, enabled := range changes {
		if err := storage.SetUserNotificationOptOut(userID, t, !enabled); err != nil {
			return nil, err
		}
	}
	return ListNotificationPreferences(ctx, userID)
}
