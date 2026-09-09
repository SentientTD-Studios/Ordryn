package domain

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestCreateProjectValidation(t *testing.T) {
	ctx := context.Background()
	_, err := CreateProject(ctx, 1, "", "")
	if !errors.Is(err, ErrValidation) {
		t.Fatalf("empty name: err=%v", err)
	}
	_, err = CreateProject(ctx, 1, strings.Repeat("x", MaxProjectNameLength+1), "")
	if !errors.Is(err, ErrValidation) {
		t.Fatalf("long name: err=%v", err)
	}
	_, err = CreateProject(ctx, 1, "ok", strings.Repeat("d", MaxProjectDescriptionLength+1))
	if !errors.Is(err, ErrValidation) {
		t.Fatalf("long description: err=%v", err)
	}
}

func TestRenameProjectValidation(t *testing.T) {
	ctx := context.Background()
	_, err := RenameProject(ctx, 1, 1, "  ")
	if !errors.Is(err, ErrValidation) {
		t.Fatalf("blank name: err=%v", err)
	}
}

func TestUpdateProjectDescriptionValidation(t *testing.T) {
	ctx := context.Background()
	long := strings.Repeat("d", MaxProjectDescriptionLength+1)
	_, err := UpdateProject(ctx, 1, 1, nil, &long, nil, nil, nil)
	if !errors.Is(err, ErrValidation) {
		t.Fatalf("long description: err=%v", err)
	}

	longBacklogDesc := strings.Repeat("b", MaxBacklogDescriptionLength+1)
	_, err = UpdateProject(ctx, 1, 1, nil, nil, nil, nil, &longBacklogDesc)
	if !errors.Is(err, ErrValidation) {
		t.Fatalf("long backlog description: err=%v", err)
	}
}

func TestReorderProjectsForUserValidation(t *testing.T) {
	ctx := context.Background()
	err := ReorderProjectsForUser(ctx, 1, nil)
	if !errors.Is(err, ErrValidation) {
		t.Fatalf("empty ids: err=%v", err)
	}
}

func TestCreateTagValidation(t *testing.T) {
	ctx := context.Background()
	_, err := CreateTag(ctx, 1, "", nil)
	if !errors.Is(err, ErrValidation) {
		t.Fatalf("empty name: err=%v", err)
	}
	_, err = CreateTag(ctx, 1, strings.Repeat("t", MaxTagNameLength+1), nil)
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

func TestUpdateTagValidation(t *testing.T) {
	ctx := context.Background()
	empty := ""
	_, err := UpdateTag(ctx, 1, 1, &empty, nil)
	if !errors.Is(err, ErrValidation) {
		t.Fatalf("empty name: err=%v", err)
	}
	longColor := strings.Repeat("c", MaxTagColorLength+1)
	_, err = UpdateTag(ctx, 1, 1, nil, &longColor)
	if !errors.Is(err, ErrValidation) {
		t.Fatalf("long color: err=%v", err)
	}
	_, err = UpdateTag(ctx, 1, 1, nil, nil)
	if !errors.Is(err, ErrValidation) {
		t.Fatalf("missing fields: err=%v", err)
	}
}

func TestRenameProjectBacklogValidation(t *testing.T) {
	ctx := context.Background()
	_, err := RenameProjectBacklog(ctx, 1, 1, "   ")
	if !errors.Is(err, ErrValidation) {
		t.Fatalf("blank backlog name: err=%v", err)
	}
	long := strings.Repeat("b", MaxBacklogNameLength+1)
	_, err = RenameProjectBacklog(ctx, 1, 1, long)
	if !errors.Is(err, ErrValidation) {
		t.Fatalf("long backlog name: err=%v", err)
	}
}
