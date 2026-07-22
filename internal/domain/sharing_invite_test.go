package domain

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestInviteToProject_InvalidUsername(t *testing.T) {
	ctx := context.Background()
	_, err := InviteToProject(ctx, 1, 1, "ab", "editor")
	if !errors.Is(err, ErrValidation) {
		t.Fatalf("short username: got %v", err)
	}
	if !strings.Contains(err.Error(), "at least") {
		t.Fatalf("expected length message, got %v", err)
	}

	_, err = InviteToProject(ctx, 1, 1, "bad name!", "editor")
	if !errors.Is(err, ErrValidation) {
		t.Fatalf("invalid chars: got %v", err)
	}

	_, err = InviteToProject(ctx, 1, 1, "", "editor")
	if !errors.Is(err, ErrValidation) {
		t.Fatalf("empty: got %v", err)
	}
}

func TestInviteToProject_InvalidRole(t *testing.T) {
	ctx := context.Background()
	_, err := InviteToProject(ctx, 1, 1, "Valid_User", "owner")
	if !errors.Is(err, ErrValidation) {
		t.Fatalf("owner role: got %v", err)
	}
	if !strings.Contains(err.Error(), "editor or viewer") {
		t.Fatalf("expected role message, got %v", err)
	}

	_, err = InviteToProject(ctx, 1, 1, "Valid_User", "")
	if !errors.Is(err, ErrValidation) {
		t.Fatalf("empty role: got %v", err)
	}
}
