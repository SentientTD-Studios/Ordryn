package domain

import (
	"context"
	"errors"
	"testing"

	"GoTodo/internal/storage"
)

func TestStatusWIPLimit(t *testing.T) {
	ctx := context.Background()
	proj, err := CreateProject(ctx, 1, "WIP Board", "")
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	if _, err := SetProjectWorkflowMode(ctx, 1, proj.ID, storage.WorkflowKanban); err != nil {
		t.Fatalf("kanban: %v", err)
	}
	statuses, err := storage.ListProjectStatuses(proj.ID)
	if err != nil {
		t.Fatal(err)
	}
	var open, done storage.ProjectStatus
	for _, s := range statuses {
		if s.IsDone {
			done = s
		} else if open.ID == 0 {
			open = s
		}
	}

	limit := func(n int) **int { v := &n; return &v }
	clear := func() **int { var v *int; return &v }

	s, err := UpdateProjectStatusForUser(ctx, 1, proj.ID, open.ID, UpdateProjectStatusInput{WIPLimit: limit(3)})
	if err != nil || s.WIPLimit == nil || *s.WIPLimit != 3 {
		t.Fatalf("set limit: status=%+v err=%v", s, err)
	}
	for _, bad := range []int{0, 1000} {
		if _, err := UpdateProjectStatusForUser(ctx, 1, proj.ID, open.ID, UpdateProjectStatusInput{WIPLimit: limit(bad)}); !errors.Is(err, ErrValidation) {
			t.Fatalf("limit %d accepted: err=%v", bad, err)
		}
	}
	if _, err := UpdateProjectStatusForUser(ctx, 1, proj.ID, done.ID, UpdateProjectStatusInput{WIPLimit: limit(2)}); !errors.Is(err, ErrValidation) {
		t.Fatalf("done column accepted a limit: err=%v", err)
	}

	// Leaving the field out keeps the limit.
	name := "In flight"
	s, err = UpdateProjectStatusForUser(ctx, 1, proj.ID, open.ID, UpdateProjectStatusInput{Name: &name})
	if err != nil || s.WIPLimit == nil || *s.WIPLimit != 3 {
		t.Fatalf("rename dropped the limit: status=%+v err=%v", s, err)
	}

	// Turning the column into a done column clears its limit.
	isDone := true
	s, err = UpdateProjectStatusForUser(ctx, 1, proj.ID, open.ID, UpdateProjectStatusInput{IsDone: &isDone})
	if err != nil || s.WIPLimit != nil {
		t.Fatalf("becoming done should clear the limit: status=%+v err=%v", s, err)
	}

	notDone := false
	if _, err := UpdateProjectStatusForUser(ctx, 1, proj.ID, open.ID, UpdateProjectStatusInput{IsDone: &notDone, WIPLimit: limit(5)}); err != nil {
		t.Fatalf("reset to open with a limit: %v", err)
	}
	s, err = UpdateProjectStatusForUser(ctx, 1, proj.ID, open.ID, UpdateProjectStatusInput{WIPLimit: clear()})
	if err != nil || s.WIPLimit != nil {
		t.Fatalf("null should clear the limit: status=%+v err=%v", s, err)
	}

	if err := upsertTestMember(t, proj.ID, 2, testRoleEditor); err != nil {
		t.Fatal(err)
	}
	if _, err := UpdateProjectStatusForUser(ctx, 2, proj.ID, open.ID, UpdateProjectStatusInput{WIPLimit: limit(2)}); err == nil {
		t.Fatal("an editor should not be able to change column settings")
	}
}
