package domain

import (
	"context"
	"testing"
	"time"

	"GoTodo/internal/storage"
)

func TestReminderRecipients(t *testing.T) {
	cases := []struct {
		name string
		task storage.ReminderTask
		want []int
	}{
		{"board task claimed by someone else", storage.ReminderTask{OwnerID: 7, ProjectID: 1, ProjectOwnerID: 1, ClaimedBy: 2}, []int{1, 2}},
		{"owner claimed their own task", storage.ReminderTask{OwnerID: 7, ProjectID: 1, ProjectOwnerID: 1, ClaimedBy: 1}, []int{1}},
		{"unclaimed project task", storage.ReminderTask{OwnerID: 7, ProjectID: 1, ProjectOwnerID: 1}, []int{1}},
		{"personal task", storage.ReminderTask{OwnerID: 7}, []int{7}},
	}
	for _, tc := range cases {
		got := reminderRecipients(tc.task)
		if len(got) != len(tc.want) {
			t.Fatalf("%s: got %v want %v", tc.name, got, tc.want)
		}
		for i := range got {
			if got[i] != tc.want[i] {
				t.Fatalf("%s: got %v want %v", tc.name, got, tc.want)
			}
		}
	}
}

func TestReminderKindAt(t *testing.T) {
	due := time.Date(2026, 10, 15, 0, 0, 0, 0, time.UTC)
	loc, _ := time.LoadLocation("America/New_York")
	at := func(day, hour int) time.Time { return time.Date(2026, 10, day, hour, 30, 0, 0, loc) }
	cases := []struct {
		name   string
		local  time.Time
		timing string
		want   string
	}{
		{"day before, before 8", at(14, 7), storage.ReminderTimingBoth, ""},
		{"day before, after 8", at(14, 9), storage.ReminderTimingBoth, storage.ReminderKindDayBefore},
		{"day before, morning-only", at(14, 9), storage.ReminderTimingMorning, ""},
		{"due day, after 8", at(15, 8), storage.ReminderTimingBoth, storage.ReminderKindMorning},
		{"due day, day-before-only", at(15, 9), storage.ReminderTimingDayBefore, ""},
		{"two days before", at(13, 9), storage.ReminderTimingBoth, ""},
		{"overdue", at(16, 9), storage.ReminderTimingBoth, ""},
		{"off", at(14, 9), storage.ReminderTimingOff, ""},
	}
	for _, tc := range cases {
		if got := reminderKindAt(due, tc.local, tc.timing); got != tc.want {
			t.Fatalf("%s: got %q want %q", tc.name, got, tc.want)
		}
	}
}

func remindersFor(t *testing.T, userID, taskID int) []string {
	t.Helper()
	pool, err := storage.OpenDatabase()
	if err != nil {
		t.Fatal(err)
	}
	defer storage.CloseDatabase(pool)
	rows, err := pool.Query(context.Background(), `
		SELECT title FROM user_notifications WHERE user_id = $1 AND task_id = $2 AND type = $3 ORDER BY id`,
		userID, taskID, storage.NotificationDueReminder)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			t.Fatal(err)
		}
		out = append(out, s)
	}
	return out
}

func TestDueReminderPassRemindsOwnerAndClaimerOnce(t *testing.T) {
	ctx := context.Background()
	const owner, claimer, solo = 911, 912, 913
	emailTestUser(t, owner, "UTC")
	emailTestUser(t, claimer, "UTC")
	emailTestUser(t, solo, "UTC")

	proj, err := CreateProject(ctx, owner, "Reminder Board", "")
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	if _, err := SetProjectWorkflowMode(ctx, owner, proj.ID, storage.WorkflowKanban); err != nil {
		t.Fatalf("kanban: %v", err)
	}
	if err := upsertTestMember(t, proj.ID, claimer, testRoleEditor); err != nil {
		t.Fatalf("member: %v", err)
	}

	// "Today" for the pass is fixed; due dates are relative to it.
	day := time.Now().UTC().Truncate(24 * time.Hour).AddDate(0, 0, 3)
	tomorrow := day.AddDate(0, 0, 1).Format("2006-01-02")
	pid := proj.ID
	boardTask, err := CreateTask(ctx, owner, CreateTaskInput{Title: "Board deliverable", ProjectID: &pid, DueDate: tomorrow})
	if err != nil {
		t.Fatalf("board task: %v", err)
	}
	if err := ClaimTaskForUser(ctx, claimer, boardTask); err != nil {
		t.Fatalf("claim: %v", err)
	}
	soloTask, err := CreateTask(ctx, solo, CreateTaskInput{Title: "Personal errand", DueDate: tomorrow})
	if err != nil {
		t.Fatalf("personal task: %v", err)
	}
	t.Cleanup(func() { _ = DeleteProject(ctx, owner, proj.ID) })

	at := func(d time.Time, hour int) time.Time { return d.Add(time.Duration(hour) * time.Hour) }

	if RunDueReminderPass(at(day, 7)); len(remindersFor(t, owner, boardTask)) != 0 {
		t.Fatal("no reminder before 08:00 local")
	}

	RunDueReminderPass(at(day, 9))
	for _, uid := range []int{owner, claimer} {
		if got := remindersFor(t, uid, boardTask); len(got) != 1 || got[0] != "Due tomorrow: Board deliverable" {
			t.Fatalf("user %d day-before reminders = %v", uid, got)
		}
	}
	if got := remindersFor(t, solo, soloTask); len(got) != 1 || got[0] != "Due tomorrow: Personal errand" {
		t.Fatalf("personal task owner reminders = %v", got)
	}

	// A second pass the same day must not repeat anything.
	RunDueReminderPass(at(day, 10))
	if got := remindersFor(t, owner, boardTask); len(got) != 1 {
		t.Fatalf("reminder repeated: %v", got)
	}

	// The claimer only wants the day-before reminder.
	timing := storage.ReminderTimingDayBefore
	if err := UpdateNotificationSettings(ctx, claimer, NotificationSettingsUpdate{ReminderTiming: &timing}); err != nil {
		t.Fatal(err)
	}
	RunDueReminderPass(at(day.AddDate(0, 0, 1), 8))
	if got := remindersFor(t, owner, boardTask); len(got) != 2 || got[1] != "Due today: Board deliverable" {
		t.Fatalf("owner morning reminder = %v", got)
	}
	if got := remindersFor(t, claimer, boardTask); len(got) != 1 {
		t.Fatalf("claimer opted out of the morning reminder but got %v", got)
	}

	// Completed tasks are never reminded, even with their sent records cleared.
	before := len(remindersFor(t, solo, soloTask))
	execSQL(t, `UPDATE tasks SET completed = TRUE WHERE id = $1`, soloTask)
	execSQL(t, `DELETE FROM task_reminder_sent WHERE task_id = $1`, soloTask)
	RunDueReminderPass(at(day.AddDate(0, 0, 1), 9))
	if got := remindersFor(t, solo, soloTask); len(got) != before {
		t.Fatalf("completed task reminded again: %v (had %d)", got, before)
	}
}
