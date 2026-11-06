package domain

import (
	"context"
	"errors"
	"testing"

	"GoTodo/internal/storage"
)

func TestSetProjectPinsForUser(t *testing.T) {
	ctx := context.Background()
	owned, err := CreateProject(ctx, 1, "Pin Owned", "")
	if err != nil {
		t.Fatalf("create owned: %v", err)
	}
	shared, err := CreateProject(ctx, 2, "Pin Shared", "")
	if err != nil {
		t.Fatalf("create shared: %v", err)
	}
	if err := upsertTestMember(t, shared.ID, 1, testRoleViewer); err != nil {
		t.Fatalf("add member: %v", err)
	}
	private, err := CreateProject(ctx, 3, "Pin Private", "")
	if err != nil {
		t.Fatalf("create private: %v", err)
	}
	t.Cleanup(func() { _ = storage.SetProjectPins(1, nil) })

	// Shared project first, then owned: order is preserved.
	if err := SetProjectPinsForUser(ctx, 1, []int{shared.ID, owned.ID}); err != nil {
		t.Fatalf("pin: %v", err)
	}
	pins, err := storage.GetProjectPinPositions(1)
	if err != nil {
		t.Fatalf("read pins: %v", err)
	}
	if pins[shared.ID] != 0 || pins[owned.ID] != 1 || len(pins) != 2 {
		t.Fatalf("pins = %v, want shared=0 owned=1", pins)
	}

	// A project the user cannot access is rejected and existing pins are kept.
	if err := SetProjectPinsForUser(ctx, 1, []int{owned.ID, private.ID}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("pin inaccessible: err=%v want not found", err)
	}
	if pins, _ = storage.GetProjectPinPositions(1); len(pins) != 2 {
		t.Fatalf("pins after rejected update = %v, want unchanged", pins)
	}

	if err := SetProjectPinsForUser(ctx, 1, []int{owned.ID, owned.ID}); !errors.Is(err, ErrValidation) {
		t.Fatalf("duplicate: err=%v want validation", err)
	}

	// Losing access hides the pin without deleting the row.
	if err := storage.RemoveProjectMember(shared.ID, 1); err != nil {
		t.Fatalf("remove member: %v", err)
	}
	if pins, _ = storage.GetProjectPinPositions(1); len(pins) != 1 || pins[owned.ID] != 1 {
		t.Fatalf("pins after removal = %v, want only owned", pins)
	}

	if err := SetProjectPinsForUser(ctx, 1, []int{}); err != nil {
		t.Fatalf("clear: %v", err)
	}
	if pins, _ = storage.GetProjectPinPositions(1); len(pins) != 0 {
		t.Fatalf("pins after clear = %v, want none", pins)
	}
}
