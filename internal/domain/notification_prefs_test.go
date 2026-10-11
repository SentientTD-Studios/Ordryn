package domain

import (
	"context"
	"errors"
	"testing"

	"GoTodo/internal/storage"
)

func setPrefs(t *testing.T, userID int, changes map[string]bool) {
	t.Helper()
	if _, err := UpdateNotificationPreferences(context.Background(), userID, changes); err != nil {
		t.Fatalf("update prefs: %v", err)
	}
	t.Cleanup(func() {
		reset := map[string]bool{}
		for k := range changes {
			reset[k] = true
		}
		_, _ = UpdateNotificationPreferences(context.Background(), userID, reset)
	})
}

func notificationTypes(t *testing.T, userID, taskID int) map[string]int {
	t.Helper()
	list, _, err := storage.ListUserNotifications(userID, 200, 0)
	if err != nil {
		t.Fatalf("list notifications: %v", err)
	}
	out := map[string]int{}
	for _, n := range list {
		if n.TaskID == taskID {
			out[n.Type]++
		}
	}
	return out
}

func TestNotificationPreferencesListAndValidate(t *testing.T) {
	ctx := context.Background()
	prefs, err := ListNotificationPreferences(ctx, 2)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	for _, p := range prefs {
		if !p.Enabled {
			t.Fatalf("%s should default to enabled", p.Type)
		}
		if p.Type == storage.NotificationJoinRequest {
			t.Fatal("join_request should be hidden from non-admins")
		}
	}
	if len(prefs) != 7 {
		t.Fatalf("want 7 optional types for non-admin, got %d", len(prefs))
	}

	for _, bad := range []string{"password_reset", "nope", storage.NotificationJoinRequest} {
		if _, err := UpdateNotificationPreferences(ctx, 2, map[string]bool{bad: false}); !errors.Is(err, ErrValidation) {
			t.Fatalf("type %q: want ErrValidation, got %v", bad, err)
		}
	}

	setPrefs(t, 2, map[string]bool{storage.NotificationTaskCreated: false})
	prefs, _ = ListNotificationPreferences(ctx, 2)
	for _, p := range prefs {
		if want := p.Type != storage.NotificationTaskCreated; p.Enabled != want {
			t.Fatalf("%s enabled=%v want %v", p.Type, p.Enabled, want)
		}
	}
}

func TestOptOutStopsWatcherActivity(t *testing.T) {
	ctx := context.Background()
	pid := sharedProject(t, "Optout Watch Proj")
	taskID := mustCreateTask(t, 1, CreateTaskInput{Title: "Muted task", ProjectID: &pid})
	if _, err := SetTaskWatching(ctx, 2, taskID, true); err != nil {
		t.Fatalf("watch: %v", err)
	}

	setPrefs(t, 2, map[string]bool{NotificationTaskActivity: false})
	if err := SetTaskCompleted(ctx, 1, taskID, true); err != nil {
		t.Fatalf("complete: %v", err)
	}
	if got := notificationTypes(t, 2, taskID)[NotificationTaskActivity]; got != 0 {
		t.Fatalf("opted-out watcher got %d activity notifications", got)
	}

	if _, err := UpdateNotificationPreferences(ctx, 2, map[string]bool{NotificationTaskActivity: true}); err != nil {
		t.Fatalf("re-enable: %v", err)
	}
	if err := SetTaskCompleted(ctx, 1, taskID, false); err != nil {
		t.Fatalf("reopen: %v", err)
	}
	if got := notificationTypes(t, 2, taskID)[NotificationTaskActivity]; got != 1 {
		t.Fatalf("re-enabled watcher: want 1 activity notification, got %d", got)
	}
}

func TestMutedMentionFallsBackToComment(t *testing.T) {
	ctx := context.Background()
	setTestUsername(t, 1, "alice")
	setTestUsername(t, 2, "bob_editor")
	pid := sharedProject(t, "Optout Mention Proj")
	taskID := mustCreateTask(t, 1, CreateTaskInput{Title: "Mention me", ProjectID: &pid})

	setPrefs(t, 2, map[string]bool{storage.NotificationTaskMentioned: false})
	if _, err := AddCommentForUser(ctx, 1, taskID, "ping @bob_editor"); err != nil {
		t.Fatalf("comment: %v", err)
	}
	got := notificationTypes(t, 2, taskID)
	if got[storage.NotificationTaskMentioned] != 0 || got[storage.NotificationTaskCommented] != 1 {
		t.Fatalf("muted mention should fall back to one comment notification: %v", got)
	}

	setPrefs(t, 2, map[string]bool{storage.NotificationTaskCommented: false})
	if _, err := AddCommentForUser(ctx, 1, taskID, "again @bob_editor"); err != nil {
		t.Fatalf("comment: %v", err)
	}
	got = notificationTypes(t, 2, taskID)
	if got[storage.NotificationTaskMentioned] != 0 || got[storage.NotificationTaskCommented] != 1 {
		t.Fatalf("both muted should add nothing: %v", got)
	}
}
