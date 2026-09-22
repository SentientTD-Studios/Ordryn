package domain

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"GoTodo/internal/extensions"
	"GoTodo/internal/storage"
)

const retroStoreManifest = `{
  "id": "retro",
  "name": "Retro",
  "version": "1.0.0",
  "host_api": 2,
  "ui": "panel.html",
  "surfaces": [{"id":"board","file":"board.html","at":"kanban.tab","label":"Retro"}],
  "permissions": ["store:read", "store:write"]
}`

func loadTempStoreExtension(t *testing.T, id, manifest string) {
	t.Helper()
	root := t.TempDir()
	dir := filepath.Join(root, id)
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "manifest.json"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "panel.html"), []byte("<html></html>"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "board.html"), []byte("<html></html>"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("EXTENSIONS_DIR", root)
	extensions.Load()
	empty := t.TempDir()
	t.Cleanup(func() {
		_ = os.Setenv("EXTENSIONS_DIR", empty)
		extensions.Load()
	})
}

func TestExtensionStoreGetPutConflictAndViewer(t *testing.T) {
	if err := storage.CreateExtensionTables(); err != nil {
		t.Fatal(err)
	}
	loadTempStoreExtension(t, "retro", retroStoreManifest)
	proj, err := CreateProject(context.Background(), 1, "Store Proj", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := storage.UpsertProjectMember(proj.ID, 3, storage.RoleViewer); err != nil {
		t.Fatal(err)
	}
	if err := storage.UpsertProjectMember(proj.ID, 2, storage.RoleEditor); err != nil {
		t.Fatal(err)
	}

	if _, err := GetExtensionStoreDoc(1, proj.ID, "retro", "sprint:backlog"); !errors.Is(err, ErrForbidden) {
		t.Fatalf("disabled get err=%v", err)
	}

	if err := storage.UpsertExtensionSettings("retro", storage.ExtensionSettings{Enabled: true}); err != nil {
		t.Fatal(err)
	}
	if err := storage.UpsertExtensionProjectSettings("retro", proj.ID, storage.ExtensionProjectSettings{
		Enabled: true, Triggers: []string{}, Templates: map[string]string{},
	}); err != nil {
		t.Fatal(err)
	}

	if _, err := GetExtensionStoreDoc(1, proj.ID, "retro", "sprint:backlog"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing get err=%v", err)
	}

	raw := json.RawMessage(`{"columns":["went-well"],"cards":[]}`)
	doc, err := PutExtensionStoreDoc(1, proj.ID, "retro", "sprint:backlog", 0, raw)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if doc.Revision != 1 {
		t.Fatalf("rev=%d", doc.Revision)
	}

	_, err = PutExtensionStoreDoc(1, proj.ID, "retro", "sprint:backlog", 0, raw)
	var conflict *StoreConflictError
	if !errors.As(err, &conflict) || conflict.Current == nil || conflict.Current.Revision != 1 {
		t.Fatalf("conflict err=%v", err)
	}

	next := json.RawMessage(`{"columns":["went-well"],"cards":[{"id":"c1","column":"went-well","text":"ok","author_id":1,"votes":[]}]}`)
	doc, err = PutExtensionStoreDoc(2, proj.ID, "retro", "sprint:backlog", 1, next)
	if err != nil {
		t.Fatalf("editor put: %v", err)
	}
	if doc.Revision != 2 {
		t.Fatalf("rev=%d", doc.Revision)
	}

	got, err := GetExtensionStoreDoc(3, proj.ID, "retro", "sprint:backlog")
	if err != nil {
		t.Fatalf("viewer get: %v", err)
	}
	if got.Revision != 2 {
		t.Fatalf("viewer rev=%d", got.Revision)
	}
	if _, err := PutExtensionStoreDoc(3, proj.ID, "retro", "sprint:backlog", 2, next); !errors.Is(err, ErrForbidden) {
		t.Fatalf("viewer put err=%v", err)
	}

	keys, err := ListExtensionStoreKeys(1, proj.ID, "retro")
	if err != nil {
		t.Fatal(err)
	}
	if len(keys) != 1 || keys[0] != "sprint:backlog" {
		t.Fatalf("keys=%v", keys)
	}
}
