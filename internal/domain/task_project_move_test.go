package domain

import (
	"context"
	"errors"
	"testing"
)

func TestSharedProjectMoveRequiresOwner(t *testing.T) {
	ctx := context.Background()
	shared, err := CreateProject(ctx, 1, "Shared Move Board", "")
	if err != nil {
		t.Fatalf("create shared project: %v", err)
	}
	if err := upsertTestMember(t, shared.ID, 2, testRoleEditor); err != nil {
		t.Fatalf("add editor: %v", err)
	}
	dest, err := CreateProject(ctx, 1, "Owner Dest Board", "")
	if err != nil {
		t.Fatalf("create dest project: %v", err)
	}
	if err := upsertTestMember(t, dest.ID, 2, testRoleEditor); err != nil {
		t.Fatalf("add editor to dest: %v", err)
	}

	sharedID := shared.ID
	destID := dest.ID
	taskID, err := CreateTask(ctx, 1, CreateTaskInput{Title: "Stay on board", ProjectID: &sharedID})
	if err != nil {
		t.Fatalf("owner create: %v", err)
	}

	createdByEditor, err := CreateTask(ctx, 2, CreateTaskInput{Title: "Editor created", ProjectID: &sharedID})
	if err != nil {
		t.Fatalf("editor create into shared project: %v", err)
	}

	if _, err := UpdateTask(ctx, 2, taskID, UpdateTaskInput{ProjectID: ptrToIntPtr(destID)}); !errors.Is(err, ErrForbidden) {
		t.Fatalf("editor move to other project: err=%v want forbidden", err)
	}
	clear := (*int)(nil)
	if _, err := UpdateTask(ctx, 2, taskID, UpdateTaskInput{ProjectID: &clear}); !errors.Is(err, ErrForbidden) {
		t.Fatalf("editor move to inbox: err=%v want forbidden", err)
	}

	got, err := UpdateTask(ctx, 2, taskID, UpdateTaskInput{
		Title:     strPtr("editor kept project"),
		ProjectID: ptrToIntPtr(sharedID),
	})
	if err != nil {
		t.Fatalf("editor same-project patch: %v", err)
	}
	if got.ProjectChanged {
		t.Fatal("same-project patch should not change project")
	}
	if got.NewProjectID != sharedID {
		t.Fatalf("project_id=%d want %d", got.NewProjectID, sharedID)
	}

	moved, err := UpdateTask(ctx, 1, createdByEditor, UpdateTaskInput{ProjectID: ptrToIntPtr(destID)})
	if err != nil {
		t.Fatalf("owner move: %v", err)
	}
	if !moved.ProjectChanged || moved.NewProjectID != destID {
		t.Fatalf("owner move result=%+v want dest %d", moved, destID)
	}
}
