package domain

import (
	"context"
	"fmt"
	"strings"
	"time"

	"GoTodo/internal/live"
	"GoTodo/internal/storage"
)

// MaxSnoozeDays is how far ahead a task can be snoozed.
const MaxSnoozeDays = 366

// SnoozeTaskForUser hides a task from the caller's Today list until the given
// day (YYYY-MM-DD, after today in the caller's timezone). Snoozes are private:
// any task the caller can see can be snoozed, and nobody else is affected.
func SnoozeTaskForUser(ctx context.Context, userID, taskID int, until, today string) error {
	_ = ctx
	if err := requireTaskVisible(userID, taskID); err != nil {
		return err
	}
	until = strings.TrimSpace(until)
	u, err := time.Parse("2006-01-02", until)
	if err != nil {
		return fmt.Errorf("%w: until must be YYYY-MM-DD", ErrValidation)
	}
	t, err := time.Parse("2006-01-02", today)
	if err != nil {
		return err
	}
	if !u.After(t) {
		return fmt.Errorf("%w: snooze until a day after today", ErrValidation)
	}
	if u.After(t.AddDate(0, 0, MaxSnoozeDays)) {
		return fmt.Errorf("%w: snooze for at most a year", ErrValidation)
	}
	if err := storage.SetTaskSnooze(userID, taskID, until); err != nil {
		return err
	}
	live.Push(live.Event{Type: live.TypeTaskUpdated, TaskID: taskID, ActorID: userID}, []int{userID})
	return nil
}

// ClearTaskSnoozeForUser removes the caller's snooze ("wake up now", or dismiss
// a task that came back from a snooze).
func ClearTaskSnoozeForUser(ctx context.Context, userID, taskID int) error {
	_ = ctx
	if err := requireTaskVisible(userID, taskID); err != nil {
		return err
	}
	if err := storage.ClearTaskSnooze(userID, taskID); err != nil {
		return err
	}
	live.Push(live.Event{Type: live.TypeTaskUpdated, TaskID: taskID, ActorID: userID}, []int{userID})
	return nil
}

func requireTaskVisible(userID, taskID int) error {
	canRead, _, _, err := storage.CanUserAccessTask(taskID, userID)
	if err != nil || !canRead {
		return ErrNotFound
	}
	return nil
}
