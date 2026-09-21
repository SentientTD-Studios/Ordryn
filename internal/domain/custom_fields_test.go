package domain

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"GoTodo/internal/extensions"
	"GoTodo/internal/storage"
	"GoTodo/internal/tasks"
)

func loadTempFieldsExtension(t *testing.T, id, manifest string) {
	t.Helper()
	root := t.TempDir()
	dir := filepath.Join(root, id)
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "manifest.json"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("EXTENSIONS_DIR", root)
	extensions.Load()
	empty := t.TempDir()
	t.Cleanup(func() {
		_ = os.Setenv("EXTENSIONS_DIR", empty)
		extensions.Load()
	})
	if err := SyncCustomFieldDefs(); err != nil {
		t.Fatalf("sync defs: %v", err)
	}
}

func enableFieldsExtension(t *testing.T, id string, projectID int) {
	t.Helper()
	if err := storage.UpsertExtensionSettings(id, storage.ExtensionSettings{Enabled: true}); err != nil {
		t.Fatal(err)
	}
	if err := storage.UpsertExtensionProjectSettings(id, projectID, storage.ExtensionProjectSettings{
		Enabled:   true,
		Triggers:  []string{},
		Templates: map[string]string{},
	}); err != nil {
		t.Fatal(err)
	}
}

const severityManifest = `{
  "id": "severity",
  "name": "Severity",
  "version": "1.0.0",
  "host_api": 1,
  "fields": [
    {
      "key": "level",
      "type": "enum",
      "label": "Severity",
      "show_on": ["sidebar", "kanban"],
      "options": [
        {"value": "low", "label": "Low"},
        {"value": "high", "label": "High"}
      ]
    },
    {
      "key": "ticket",
      "type": "string",
      "label": "Ticket",
      "show_on": ["sidebar"]
    }
  ]
}`

func TestCustomFieldsMergeClearAndListFilter(t *testing.T) {
	ctx := context.Background()
	loadTempFieldsExtension(t, "severity", severityManifest)
	proj, err := CreateProject(ctx, 1, "Fields Proj", "")
	if err != nil {
		t.Fatalf("project: %v", err)
	}
	enableFieldsExtension(t, "severity", proj.ID)
	pid := proj.ID
	taskID, err := CreateTask(ctx, 1, CreateTaskInput{Title: "Has fields", ProjectID: &pid})
	if err != nil {
		t.Fatalf("task: %v", err)
	}

	patch := map[string]json.RawMessage{
		"severity.level":  json.RawMessage(`"high"`),
		"severity.ticket": json.RawMessage(`"ABC-1"`),
	}
	if _, err := ApplyTaskFields(taskID, pid, 1, patch); err != nil {
		t.Fatalf("apply: %v", err)
	}

	got, err := tasks.FetchTaskByIDForUser(taskID, 1, "UTC", 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Fields) != 2 {
		t.Fatalf("detail fields=%d %+v", len(got.Fields), got.Fields)
	}

	listOnly := 0
	sidebarOnly := 0
	for _, f := range got.Fields {
		if extensions.ShowsOnList(f.ShowOn) {
			listOnly++
		} else {
			sidebarOnly++
		}
		if f.Key == "severity.level" && f.Value != "high" {
			t.Fatalf("level=%v", f.Value)
		}
	}
	if listOnly != 1 || sidebarOnly != 1 {
		t.Fatalf("list=%d sidebar=%d", listOnly, sidebarOnly)
	}

	clear := map[string]json.RawMessage{"severity.ticket": json.RawMessage(`null`)}
	if _, err := ApplyTaskFields(taskID, pid, 1, clear); err != nil {
		t.Fatal(err)
	}
	keep := map[string]json.RawMessage{"severity.level": json.RawMessage(`"low"`)}
	if _, err := ApplyTaskFields(taskID, pid, 1, keep); err != nil {
		t.Fatal(err)
	}
	got, err = tasks.FetchTaskByIDForUser(taskID, 1, "UTC", 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Fields) != 1 || got.Fields[0].Key != "severity.level" || got.Fields[0].Value != "low" {
		t.Fatalf("after merge/clear %+v", got.Fields)
	}
}

func TestCustomFieldsValidation(t *testing.T) {
	ctx := context.Background()
	loadTempFieldsExtension(t, "severity", severityManifest)
	proj, err := CreateProject(ctx, 1, "Fields Validate", "")
	if err != nil {
		t.Fatal(err)
	}
	enableFieldsExtension(t, "severity", proj.ID)
	pid := proj.ID
	taskID, err := CreateTask(ctx, 1, CreateTaskInput{Title: "Validate fields", ProjectID: &pid})
	if err != nil {
		t.Fatal(err)
	}

	_, err = ApplyTaskFields(taskID, pid, 1, map[string]json.RawMessage{"severity.level": json.RawMessage(`"nope"`)})
	if err == nil || !errors.Is(err, ErrValidation) || !strings.Contains(err.Error(), "invalid option") {
		t.Fatalf("enum err=%v", err)
	}
	_, err = ApplyTaskFields(taskID, pid, 1, map[string]json.RawMessage{"missing.key": json.RawMessage(`"x"`)})
	if err == nil || !errors.Is(err, ErrValidation) || !strings.Contains(err.Error(), "unknown field") {
		t.Fatalf("unknown err=%v", err)
	}
	_, err = ApplyTaskFields(taskID, pid, 1, map[string]json.RawMessage{"severity.level": json.RawMessage(`"high"`)})
	if err != nil {
		t.Fatal(err)
	}

	other, err := CreateProject(ctx, 1, "Fields Off", "")
	if err != nil {
		t.Fatal(err)
	}
	defs, err := ApplicableFieldDefsForProject(other.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(defs) != 0 {
		t.Fatalf("disabled project should have no defs, got %d", len(defs))
	}
}

func TestCustomFieldsUserMustBeMember(t *testing.T) {
	ctx := context.Background()
	loadTempFieldsExtension(t, "fields-demo", `{
	  "id": "fields-demo",
	  "name": "Demo",
	  "version": "1.0.0",
	  "host_api": 1,
	  "fields": [{"key": "owner", "type": "user", "label": "Owner", "show_on": ["sidebar", "list"]}]
	}`)
	proj, err := CreateProject(ctx, 1, "Fields User", "")
	if err != nil {
		t.Fatal(err)
	}
	enableFieldsExtension(t, "fields-demo", proj.ID)
	pid := proj.ID
	taskID, err := CreateTask(ctx, 1, CreateTaskInput{Title: "User field", ProjectID: &pid})
	if err != nil {
		t.Fatal(err)
	}
	_, err = ApplyTaskFields(taskID, pid, 1, map[string]json.RawMessage{"fields-demo.owner": json.RawMessage(`2`)})
	if err == nil || !errors.Is(err, ErrValidation) {
		t.Fatalf("non-member err=%v", err)
	}
	if _, err := ApplyTaskFields(taskID, pid, 1, map[string]json.RawMessage{"fields-demo.owner": json.RawMessage(`1`)}); err != nil {
		t.Fatal(err)
	}
}

func TestSyncCustomFieldDefsDeactivatesRemoved(t *testing.T) {
	loadTempFieldsExtension(t, "severity", severityManifest)
	defs, err := storage.ListCustomFieldDefsForExtensions([]string{"severity"})
	if err != nil {
		t.Fatal(err)
	}
	if len(defs) != 2 {
		t.Fatalf("defs=%d", len(defs))
	}
	loadTempFieldsExtension(t, "severity", `{
	  "id": "severity",
	  "name": "Severity",
	  "version": "1.0.1",
	  "host_api": 1,
	  "fields": [{"key": "level", "type": "string", "label": "Severity"}]
	}`)
	defs, err = storage.ListCustomFieldDefsForExtensions([]string{"severity"})
	if err != nil {
		t.Fatal(err)
	}
	if len(defs) != 1 || defs[0].LocalKey != "level" {
		t.Fatalf("after deactivate %+v", defs)
	}
}
