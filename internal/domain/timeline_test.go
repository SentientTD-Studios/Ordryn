package domain

import (
	"context"
	"errors"
	"testing"

	"GoTodo/internal/storage"
)

func ptrStr(s string) *string { return &s }

func TestStartDateCreateUpdateAndValidation(t *testing.T) {
	ctx := context.Background()
	proj, err := CreateProject(ctx, 1, "Start Dates", "")
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	pid := proj.ID

	if _, err := CreateTask(ctx, 1, CreateTaskInput{Title: "Bad", ProjectID: &pid, StartDate: "2026-13-01"}); !errors.Is(err, ErrValidation) {
		t.Fatalf("bad date err=%v, want validation", err)
	}
	if _, err := CreateTask(ctx, 1, CreateTaskInput{Title: "Backwards", ProjectID: &pid, StartDate: "2026-05-10", DueDate: "2026-05-01"}); !errors.Is(err, ErrValidation) {
		t.Fatalf("start after due err=%v, want validation", err)
	}

	id, err := CreateTask(ctx, 1, CreateTaskInput{Title: "Planned", ProjectID: &pid, StartDate: "2026-05-01", DueDate: "2026-05-10"})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if got, _ := storage.GetTaskStartDate(id); got != "2026-05-01" {
		t.Fatalf("start=%q want 2026-05-01", got)
	}

	// Moving the due date before the start is rejected; so is a start after due.
	if _, err := UpdateTask(ctx, 1, id, UpdateTaskInput{DueDate: ptrStr("2026-04-30")}); !errors.Is(err, ErrValidation) {
		t.Fatalf("due before start err=%v, want validation", err)
	}
	if _, err := UpdateTask(ctx, 1, id, UpdateTaskInput{StartDate: ptrStr("2026-05-11")}); !errors.Is(err, ErrValidation) {
		t.Fatalf("start after due err=%v, want validation", err)
	}

	if _, err := UpdateTask(ctx, 1, id, UpdateTaskInput{StartDate: ptrStr("2026-05-03")}); err != nil {
		t.Fatalf("move start: %v", err)
	}
	if got, _ := storage.GetTaskStartDate(id); got != "2026-05-03" {
		t.Fatalf("start=%q want 2026-05-03", got)
	}
	// Clearing the due date leaves the start alone.
	if _, err := UpdateTask(ctx, 1, id, UpdateTaskInput{ClearDue: true}); err != nil {
		t.Fatalf("clear due: %v", err)
	}
	if _, err := UpdateTask(ctx, 1, id, UpdateTaskInput{StartDate: ptrStr("")}); err != nil {
		t.Fatalf("clear start: %v", err)
	}
	if got, _ := storage.GetTaskStartDate(id); got != "" {
		t.Fatalf("start=%q want cleared", got)
	}
}

func TestProjectTimeline(t *testing.T) {
	ctx := context.Background()
	proj, err := CreateProject(ctx, 1, "Timeline", "")
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	pid := proj.ID
	a, err := CreateTask(ctx, 1, CreateTaskInput{Title: "Design", ProjectID: &pid, StartDate: "2026-06-01", DueDate: "2026-06-05"})
	if err != nil {
		t.Fatal(err)
	}
	b, err := CreateTask(ctx, 1, CreateTaskInput{Title: "Build", ProjectID: &pid, DueDate: "2026-06-12"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := CreateTask(ctx, 1, CreateTaskInput{Title: "Someday", ProjectID: &pid}); err != nil {
		t.Fatal(err)
	}
	if _, err := AddTaskLink(ctx, 1, a, b, "blocks"); err != nil {
		t.Fatalf("link: %v", err)
	}

	tl, err := GetProjectTimelineForUser(ctx, 1, pid)
	if err != nil {
		t.Fatalf("timeline: %v", err)
	}
	if len(tl.Tasks) != 2 || tl.Tasks[0].ID != a || tl.Tasks[1].ID != b {
		t.Fatalf("tasks=%+v, want Design then Build", tl.Tasks)
	}
	if tl.Tasks[0].StartDate != "2026-06-01" || tl.Tasks[0].DueDate != "2026-06-05" {
		t.Fatalf("dates=%+v", tl.Tasks[0])
	}
	if tl.Undated != 1 {
		t.Fatalf("undated=%d want 1", tl.Undated)
	}
	if len(tl.Links) != 1 {
		t.Fatalf("links=%+v, want one blocking link", tl.Links)
	}

	// Someone with no access gets not found.
	if _, err := GetProjectTimelineForUser(ctx, 3, pid); !errors.Is(err, ErrNotFound) {
		t.Fatalf("outsider err=%v, want not found", err)
	}
}
