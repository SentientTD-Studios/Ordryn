package domain

import (
	"context"
	"errors"
	"testing"

	"GoTodo/internal/storage"
)

func TestRequireProjectExtensionOwnerVsEditor(t *testing.T) {
	ctx := context.Background()
	proj, err := CreateProject(ctx, 1, "Extension Access Proj", "")
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	if err := storage.UpsertProjectMember(proj.ID, 3, storage.RoleEditor); err != nil {
		t.Fatalf("add editor: %v", err)
	}
	if _, err := RequireProjectExtensionOwner(1, proj.ID); err != nil {
		t.Fatalf("owner: %v", err)
	}
	if _, err := RequireProjectExtensionOwner(3, proj.ID); !errors.Is(err, ErrForbidden) {
		t.Fatalf("editor err=%v, want forbidden", err)
	}
}

func TestExtensionSecretProjectIsolation(t *testing.T) {
	if err := storage.CreateExtensionTables(); err != nil {
		t.Fatalf("extension tables: %v", err)
	}
	ctx := context.Background()
	p1, err := CreateProject(ctx, 1, "Extension Secret A", "")
	if err != nil {
		t.Fatalf("create p1: %v", err)
	}
	p2, err := CreateProject(ctx, 1, "Extension Secret B", "")
	if err != nil {
		t.Fatalf("create p2: %v", err)
	}
	const key = "webhook_url"
	if err := storage.SetExtensionSecret("discord", p1.ID, key, "https://discord.com/api/webhooks/1/aaa"); err != nil {
		t.Fatalf("set p1: %v", err)
	}
	if err := storage.SetExtensionSecret("discord", p2.ID, key, "https://discord.com/api/webhooks/2/bbb"); err != nil {
		t.Fatalf("set p2: %v", err)
	}
	got1, err := storage.GetExtensionSecret("discord", p1.ID, key)
	if err != nil {
		t.Fatal(err)
	}
	got2, err := storage.GetExtensionSecret("discord", p2.ID, key)
	if err != nil {
		t.Fatal(err)
	}
	if got1 == got2 {
		t.Fatal("project secrets should be isolated")
	}
	if got1 != "https://discord.com/api/webhooks/1/aaa" || got2 != "https://discord.com/api/webhooks/2/bbb" {
		t.Fatalf("got %q and %q", got1, got2)
	}
	site, err := storage.GetExtensionSecret("discord", 0, key)
	if err != nil {
		t.Fatal(err)
	}
	if site != "" {
		t.Fatalf("site secret should be unused, got %q", site)
	}
}
