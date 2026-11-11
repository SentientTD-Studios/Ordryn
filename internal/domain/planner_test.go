package domain

import (
	"context"
	"errors"
	"testing"

	"GoTodo/internal/tasks"
)

// plannerSection names the section holding taskID, or "" when absent.
func plannerSection(p *tasks.Planner, taskID int) string {
	in := func(list []tasks.PlannerTask) bool {
		for _, t := range list {
			if t.ID == taskID {
				return true
			}
		}
		return false
	}
	switch {
	case in(p.Overdue):
		return "overdue"
	case in(p.DueToday):
		return "today"
	case in(p.Woken):
		return "woken"
	case in(p.Snoozed):
		return "snoozed"
	}
	for _, d := range p.Upcoming {
		if in(d.Tasks) {
			return "upcoming:" + d.Date
		}
	}
	return ""
}

func TestPlannerSectionsAndSnooze(t *testing.T) {
	ctx := context.Background()
	const today = "2031-06-15"
	mk := func(title, due string) int {
		t.Helper()
		id, err := CreateTask(ctx, 1, CreateTaskInput{Title: title, DueDate: due})
		if err != nil {
			t.Fatalf("create %s: %v", title, err)
		}
		return id
	}
	overdue := mk("Planner overdue", "2031-06-10")
	dueToday := mk("Planner today", today)
	soon := mk("Planner soon", "2031-06-18")
	far := mk("Planner far", "2031-08-01")
	undated := mk("Planner undated", "")
	done := mk("Planner done", today)
	completed := true
	if _, err := UpdateTask(ctx, 1, done, UpdateTaskInput{Completed: &completed}); err != nil {
		t.Fatalf("complete: %v", err)
	}

	p, err := tasks.FetchPlanner(1, "UTC", today, 14)
	if err != nil {
		t.Fatalf("planner: %v", err)
	}
	for id, want := range map[int]string{
		overdue: "overdue", dueToday: "today", soon: "upcoming:2031-06-18",
		far: "", undated: "", done: "",
	} {
		if got := plannerSection(p, id); got != want {
			t.Errorf("task %d in %q, want %q", id, got, want)
		}
	}

	// Snoozing hides an overdue task until the day; then it comes back as woken.
	if err := SnoozeTaskForUser(ctx, 1, overdue, "2031-06-20", today); err != nil {
		t.Fatalf("snooze: %v", err)
	}
	if err := SnoozeTaskForUser(ctx, 1, undated, "2031-06-16", today); err != nil {
		t.Fatalf("snooze undated: %v", err)
	}
	p, _ = tasks.FetchPlanner(1, "UTC", today, 14)
	if got := plannerSection(p, overdue); got != "snoozed" {
		t.Fatalf("snoozed overdue task in %q", got)
	}
	p, _ = tasks.FetchPlanner(1, "UTC", "2031-06-16", 14)
	if got := plannerSection(p, undated); got != "woken" {
		t.Fatalf("undated task after its snooze in %q, want woken", got)
	}
	// Once the snooze ends, an overdue task is overdue again.
	p, _ = tasks.FetchPlanner(1, "UTC", "2031-06-21", 14)
	if got := plannerSection(p, overdue); got != "overdue" {
		t.Fatalf("after snooze in %q, want overdue", got)
	}
	// Dismissing a woken task clears the snooze.
	if err := ClearTaskSnoozeForUser(ctx, 1, undated); err != nil {
		t.Fatalf("clear: %v", err)
	}
	p, _ = tasks.FetchPlanner(1, "UTC", "2031-06-16", 14)
	if got := plannerSection(p, undated); got != "" {
		t.Fatalf("dismissed task in %q", got)
	}

	// Snoozes are private: another user's planner is unaffected (and they can't see this task).
	if err := SnoozeTaskForUser(ctx, 2, soon, "2031-06-20", today); !errors.Is(err, ErrNotFound) {
		t.Fatalf("other user snooze err=%v, want not found", err)
	}
}

func TestSnoozeValidation(t *testing.T) {
	ctx := context.Background()
	const today = "2031-06-15"
	id, err := CreateTask(ctx, 1, CreateTaskInput{Title: "Snooze rules"})
	if err != nil {
		t.Fatal(err)
	}
	for _, until := range []string{"", "tomorrow", today, "2031-06-01", "2032-12-31"} {
		if err := SnoozeTaskForUser(ctx, 1, id, until, today); !errors.Is(err, ErrValidation) {
			t.Errorf("until %q: err=%v, want validation", until, err)
		}
	}
	if err := SnoozeTaskForUser(ctx, 1, id, "2031-06-16", today); err != nil {
		t.Fatalf("valid snooze: %v", err)
	}
}
