package domain

import (
	"context"
	"fmt"

	"GoTodo/internal/mailer"
	"GoTodo/internal/storage"
)

// NotificationOption is a notification type a user may turn off. Types not in
// optionalNotifications (password reset/changed emails, invites) are always sent.
type NotificationOption struct {
	Type        string
	Label       string
	Description string
	AdminOnly   bool
	// NoEmail marks options that cannot be emailed on their own: automation is
	// an opt-out key rather than a stored type, and join requests already send
	// their own admin email.
	NoEmail bool
}

// optionalNotifications is the opt-out registry, in display order.
var optionalNotifications = []NotificationOption{
	{Type: storage.NotificationTaskCreated, Label: "New tasks", Description: "A task is posted in a kanban project you belong to."},
	{Type: storage.NotificationTaskCommented, Label: "Comments", Description: "Someone comments on a task in a project you belong to."},
	{Type: storage.NotificationTaskMentioned, Label: "Mentions", Description: "Someone @mentions you in a comment. When off, you get a regular comment notification instead if comments are on."},
	{Type: NotificationTaskActivity, Label: "Watched task activity", Description: "A task you watch is claimed, moved, blocked, completed, reopened, or has its due date changed."},
	{Type: NotificationTaskUnblocked, Label: "Unblocked tasks", Description: "A task you watch has its last open blocker completed."},
	{Type: storage.NotificationDueReminder, Label: "Due date reminders", Description: "A task is due tomorrow or today: your own tasks, tasks you claimed, and tasks in projects you own. Set the timing in the Due date reminders menu above."},
	{Type: storage.NotificationAutomation, Label: "Automation", Description: "A project automation rule changes, comments on, assigns, or flags a task you follow. When off, nothing caused by Automation reaches your inbox.", NoEmail: true},
	{Type: storage.NotificationJoinRequest, Label: "Join requests", Description: "Someone asks to join the site. Covers both the in-app notification and the email.", AdminOnly: true, NoEmail: true},
}

// NotificationPreference is one optional type and whether the user receives it
// in-app and by email.
type NotificationPreference struct {
	NotificationOption
	Enabled bool
	Email   bool
}

// NotificationEmailSettings are the user's email delivery and reminder choices,
// plus whether the site can send notification email at all.
type NotificationEmailSettings struct {
	// Available is true when an admin enabled notification email and outbound
	// mail is configured. Users may still save choices while it is off.
	Available      bool
	Mode           string
	DigestHour     int
	ReminderTiming string
}

// NotificationSettingsUpdate is a partial update; nil fields are unchanged.
type NotificationSettingsUpdate struct {
	InApp          map[string]bool
	Email          map[string]bool
	Mode           *string
	DigestHour     *int
	ReminderTiming *string
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
	emailOn, err := storage.ListUserEmailTypes(userID)
	if err != nil {
		return nil, err
	}
	opts := notificationOptionsFor(userID)
	out := make([]NotificationPreference, 0, len(opts))
	for _, o := range opts {
		out = append(out, NotificationPreference{
			NotificationOption: o,
			Enabled:            !optedOut[o.Type],
			Email:              !o.NoEmail && emailOn[o.Type],
		})
	}
	return out, nil
}

// GetNotificationEmailSettings returns the user's email and reminder settings.
func GetNotificationEmailSettings(ctx context.Context, userID int) (NotificationEmailSettings, error) {
	_ = ctx
	p, err := storage.GetUserEmailPrefs(userID)
	if err != nil {
		return NotificationEmailSettings{}, err
	}
	return NotificationEmailSettings{
		Available:      notificationEmailAvailable(),
		Mode:           p.Mode,
		DigestHour:     p.DigestHour,
		ReminderTiming: p.ReminderTiming,
	}, nil
}

func notificationEmailAvailable() bool {
	cfg, _, enabled := notificationMailConfig()
	return enabled && mailer.Configured(cfg)
}

// UpdateNotificationPreferences applies in-app enabled/disabled changes keyed by type.
// Unknown, required, or admin-only types (for non-admins) are rejected.
func UpdateNotificationPreferences(ctx context.Context, userID int, changes map[string]bool) ([]NotificationPreference, error) {
	if err := UpdateNotificationSettings(ctx, userID, NotificationSettingsUpdate{InApp: changes}); err != nil {
		return nil, err
	}
	return ListNotificationPreferences(ctx, userID)
}

// UpdateNotificationSettings validates every change first, then applies them.
func UpdateNotificationSettings(ctx context.Context, userID int, u NotificationSettingsUpdate) error {
	_ = ctx
	opts := map[string]NotificationOption{}
	for _, o := range notificationOptionsFor(userID) {
		opts[o.Type] = o
	}
	for t := range u.InApp {
		if _, ok := opts[t]; !ok {
			return fmt.Errorf("%w: unknown notification type %q", ErrValidation, t)
		}
	}
	for t := range u.Email {
		o, ok := opts[t]
		if !ok {
			return fmt.Errorf("%w: unknown notification type %q", ErrValidation, t)
		}
		if o.NoEmail {
			return fmt.Errorf("%w: %s cannot be emailed", ErrValidation, o.Label)
		}
	}
	current, err := storage.GetUserEmailPrefs(userID)
	if err != nil {
		return err
	}
	next := current
	if u.Mode != nil {
		switch *u.Mode {
		case storage.EmailModeOff, storage.EmailModeInstant, storage.EmailModeDigest:
			next.Mode = *u.Mode
		default:
			return fmt.Errorf("%w: email mode must be off, instant, or digest", ErrValidation)
		}
	}
	if u.DigestHour != nil {
		if *u.DigestHour < 0 || *u.DigestHour > 23 {
			return fmt.Errorf("%w: digest hour must be between 0 and 23", ErrValidation)
		}
		next.DigestHour = *u.DigestHour
	}
	if u.ReminderTiming != nil {
		switch *u.ReminderTiming {
		case storage.ReminderTimingOff, storage.ReminderTimingDayBefore, storage.ReminderTimingMorning, storage.ReminderTimingBoth:
			next.ReminderTiming = *u.ReminderTiming
		default:
			return fmt.Errorf("%w: reminder timing must be off, day_before, morning, or both", ErrValidation)
		}
	}

	for t, enabled := range u.InApp {
		if err := storage.SetUserNotificationOptOut(userID, t, !enabled); err != nil {
			return err
		}
	}
	for t, on := range u.Email {
		if err := storage.SetUserEmailType(userID, t, on); err != nil {
			return err
		}
	}
	if next != current {
		if err := storage.SaveUserEmailPrefs(userID, next.Mode, next.DigestHour, next.ReminderTiming); err != nil {
			return err
		}
	}
	return nil
}
