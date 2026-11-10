package domain

import (
	"context"
	"errors"
	"testing"
)

func TestProjectSlugLifecycle(t *testing.T) {
	ctx := context.Background()
	a, err := CreateProject(ctx, 1, "Slug Lifecycle", "")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if a.Slug == "" {
		t.Fatal("new project should get a slug")
	}
	b, err := CreateProject(ctx, 1, "Slug Lifecycle", "")
	if err != nil {
		t.Fatalf("create second: %v", err)
	}
	if b.Slug == a.Slug {
		t.Fatalf("duplicate names must get distinct slugs, both %q", a.Slug)
	}
	if err := upsertTestMember(t, a.ID, 2, testRoleEditor); err != nil {
		t.Fatal(err)
	}

	if _, err := SetProjectSlug(ctx, 2, a.ID, "editor-tries"); !errors.Is(err, ErrForbidden) {
		t.Fatalf("editor renaming slug: err=%v want forbidden", err)
	}
	if _, err := SetProjectSlug(ctx, 1, a.ID, "123"); !errors.Is(err, ErrValidation) {
		t.Fatalf("numeric slug: err=%v want validation", err)
	}
	if _, err := SetProjectSlug(ctx, 1, a.ID, b.Slug); !errors.Is(err, ErrConflict) {
		t.Fatalf("taken slug: err=%v want conflict", err)
	}

	old := a.Slug
	got, err := SetProjectSlug(ctx, 1, a.ID, "Slug-Renamed-"+t.Name()[:4])
	if err != nil {
		t.Fatalf("rename: %v", err)
	}
	for _, s := range []string{got, old} {
		p, err := ProjectBySlug(ctx, 2, s)
		if err != nil || p.ID != a.ID {
			t.Fatalf("resolve %q: %v (id %v)", s, err, p)
		}
	}
	if _, err := ProjectBySlug(ctx, 999999, got); !errors.Is(err, ErrNotFound) {
		t.Fatalf("outsider resolving slug: err=%v want not found", err)
	}

	// A former slug can be claimed explicitly by another project; it then resolves there.
	if _, err := SetProjectSlug(ctx, 1, b.ID, old); err != nil {
		t.Fatalf("claim former slug: %v", err)
	}
	if p, err := ProjectBySlug(ctx, 1, old); err != nil || p.ID != b.ID {
		t.Fatalf("claimed slug should resolve to new owner: %v", err)
	}
	// Renaming back to an own former slug works.
	if _, err := SetProjectSlug(ctx, 1, b.ID, b.Slug); err != nil {
		t.Fatalf("rename back: %v", err)
	}
}
