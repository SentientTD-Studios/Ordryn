package domain

import (
	"context"
	"errors"
	"testing"

	"GoTodo/internal/storage"
)

func reviewerOf(t *testing.T, taskID int) (int, string) {
	t.Helper()
	fields, err := storage.GetWorkflowFieldsForTasks([]int{taskID})
	if err != nil {
		t.Fatal(err)
	}
	return fields[taskID].ReviewerID, fields[taskID].ReviewerName
}

func TestTaskReviewerSetClearAndPermissions(t *testing.T) {
	ctx := context.Background()
	proj, err := CreateProject(ctx, 1, "Review Board", "")
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	if err := upsertTestMember(t, proj.ID, 2, testRoleEditor); err != nil {
		t.Fatalf("add editor: %v", err)
	}
	if err := upsertTestMember(t, proj.ID, 3, testRoleViewer); err != nil {
		t.Fatalf("add viewer: %v", err)
	}
	pid := proj.ID
	taskID, err := CreateTask(ctx, 1, CreateTaskInput{Title: "Needs review", ProjectID: &pid})
	if err != nil {
		t.Fatalf("create task: %v", err)
	}

	// Works on classic projects (no kanban needed).
	if err := SetTaskReviewerForUser(ctx, 1, taskID, 2, ""); err != nil {
		t.Fatalf("set reviewer: %v", err)
	}
	if id, name := reviewerOf(t, taskID); id != 2 || name == "" {
		t.Fatalf("reviewer=%d %q, want 2 with a name", id, name)
	}

	// A viewer (no tasks:edit) cannot change it.
	if err := SetTaskReviewerForUser(ctx, 3, taskID, 3, ""); !errors.Is(err, ErrForbidden) {
		t.Fatalf("viewer set err=%v, want forbidden", err)
	}
	if err := ClearTaskReviewerForUser(ctx, 3, taskID); !errors.Is(err, ErrForbidden) {
		t.Fatalf("viewer clear err=%v, want forbidden", err)
	}

	// The viewer can be the reviewer, and can step down without edit rights.
	if err := SetTaskReviewerForUser(ctx, 1, taskID, 3, ""); err != nil {
		t.Fatalf("set viewer as reviewer: %v", err)
	}
	if err := ClearTaskReviewerForUser(ctx, 3, taskID); err != nil {
		t.Fatalf("reviewer steps down: %v", err)
	}
	if id, _ := reviewerOf(t, taskID); id != 0 {
		t.Fatalf("reviewer=%d after clear, want 0", id)
	}

	// Clearing when none is set is a no-op.
	if err := ClearTaskReviewerForUser(ctx, 1, taskID); err != nil {
		t.Fatalf("clear empty: %v", err)
	}
}

func TestTaskReviewerMustBeProjectMember(t *testing.T) {
	ctx := context.Background()
	proj, err := CreateProject(ctx, 1, "Review Members", "")
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	pid := proj.ID
	taskID, err := CreateTask(ctx, 1, CreateTaskInput{Title: "Members only", ProjectID: &pid})
	if err != nil {
		t.Fatalf("create task: %v", err)
	}
	// User 2 is not on this project.
	if err := SetTaskReviewerForUser(ctx, 1, taskID, 2, ""); !errors.Is(err, ErrValidation) {
		t.Fatalf("non-member err=%v, want validation", err)
	}
	if err := SetTaskReviewerForUser(ctx, 1, taskID, 0, "no-such-user-xyz"); !errors.Is(err, ErrValidation) {
		t.Fatalf("unknown username err=%v, want validation", err)
	}
	if err := SetTaskReviewerForUser(ctx, 1, taskID, 0, ""); !errors.Is(err, ErrValidation) {
		t.Fatalf("missing reviewer err=%v, want validation", err)
	}
	// Outsiders can't see the task at all.
	if err := SetTaskReviewerForUser(ctx, 2, taskID, 1, ""); !errors.Is(err, ErrNotFound) {
		t.Fatalf("outsider err=%v, want not found", err)
	}
}

func TestTaskReviewerNeedsProject(t *testing.T) {
	ctx := context.Background()
	taskID, err := CreateTask(ctx, 1, CreateTaskInput{Title: "Personal"})
	if err != nil {
		t.Skipf("personal tasks unavailable here: %v", err)
	}
	if err := SetTaskReviewerForUser(ctx, 1, taskID, 1, ""); !errors.Is(err, ErrValidation) {
		t.Fatalf("personal task err=%v, want validation", err)
	}
}
