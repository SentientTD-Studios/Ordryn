package domain

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestCreateProjectValidation(t *testing.T) {
	ctx := context.Background()
	_, err := CreateProject(ctx, 1, "")
	if !errors.Is(err, ErrValidation) {
		t.Fatalf("empty name: err=%v", err)
	}
	_, err = CreateProject(ctx, 1, strings.Repeat("x", MaxProjectNameLength+1))
	if !errors.Is(err, ErrValidation) {
		t.Fatalf("long name: err=%v", err)
	}
}

func TestRenameProjectValidation(t *testing.T) {
	ctx := context.Background()
	_, err := RenameProject(ctx, 1, 1, "  ")
	if !errors.Is(err, ErrValidation) {
		t.Fatalf("blank name: err=%v", err)
	}
}

func TestCreateTagValidation(t *testing.T) {
	ctx := context.Background()
	_, err := CreateTag(ctx, 1, "")
	if !errors.Is(err, ErrValidation) {
		t.Fatalf("empty name: err=%v", err)
	}
	_, err = CreateTag(ctx, 1, strings.Repeat("t", MaxTagNameLength+1))
	if !errors.Is(err, ErrValidation) {
		t.Fatalf("long name: err=%v", err)
	}
}

func TestRenameTagValidation(t *testing.T) {
	ctx := context.Background()
	_, err := RenameTag(ctx, 1, 1, "")
	if !errors.Is(err, ErrValidation) {
		t.Fatalf("empty name: err=%v", err)
	}
}
