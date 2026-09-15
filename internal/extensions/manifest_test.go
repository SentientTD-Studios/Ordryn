package extensions

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func validDiscord() Manifest {
	return Manifest{
		ID:      "discord",
		Name:    "Discord",
		Version: "1.0.0",
		HostAPI: 1,
		Hooks: []Hook{
			{On: "task.created"},
			{On: "task.updated"},
		},
		Delivery: &Delivery{Type: "discord.webhook", URLFrom: "webhook_url"},
		Settings: []Setting{
			{Key: "webhook_url", Type: "secret", Label: "Channel webhook URL", Required: true},
			{Key: "projects", Type: "project_ids", Label: "Projects"},
			{Key: "triggers", Type: "hook_select", Label: "Triggers"},
			{Key: "status_only", Type: "bool", Label: "Only notify when status changes"},
		},
		Templates: map[string]string{
			"task.updated": "Task {name} updated to {status} in project {project}",
		},
	}
}

func TestValidateManifestOK(t *testing.T) {
	if err := ValidateManifest("discord", validDiscord()); err != nil {
		t.Fatal(err)
	}
}

func TestValidateManifestIDMismatch(t *testing.T) {
	err := ValidateManifest("other", validDiscord())
	if err == nil || !strings.Contains(err.Error(), "must match folder") {
		t.Fatalf("err=%v", err)
	}
}

func TestValidateManifestUnknownHook(t *testing.T) {
	m := validDiscord()
	m.Hooks = append(m.Hooks, Hook{On: "task.exploded"})
	err := ValidateManifest("discord", m)
	if err == nil || !strings.Contains(err.Error(), "unknown hook") {
		t.Fatalf("err=%v", err)
	}
}

func TestValidateManifestHostAPITooNew(t *testing.T) {
	m := validDiscord()
	m.HostAPI = CurrentHostAPI + 1
	err := ValidateManifest("discord", m)
	if err == nil || !strings.Contains(err.Error(), "host_api") {
		t.Fatalf("err=%v", err)
	}
}

func TestValidateManifestMissingName(t *testing.T) {
	m := validDiscord()
	m.Name = "  "
	if err := ValidateManifest("discord", m); err == nil {
		t.Fatal("expected error")
	}
}

func TestValidateManifestUnknownScope(t *testing.T) {
	m := validDiscord()
	m.Settings[0].Scope = "workspace"
	err := ValidateManifest("discord", m)
	if err == nil || !strings.Contains(err.Error(), "unknown settings scope") {
		t.Fatalf("err=%v", err)
	}
}

func TestSettingScopeDefaultsToSite(t *testing.T) {
	s := Setting{Key: "x", Scope: ""}
	if s.ScopeName() != ScopeSite {
		t.Fatalf("scope=%q", s.ScopeName())
	}
	m := validDiscord()
	if m.HasProjectSettings() {
		t.Fatal("default discord fixture is site-scoped")
	}
	m.Settings[0].Scope = ScopeProject
	if !m.HasProjectSettings() {
		t.Fatal("expected project settings")
	}
	if m.HasFields() {
		t.Fatal("discord fixture should not register fields")
	}
	if got := m.SettingsForScope(ScopeProject); len(got) != 1 || got[0].Key != "webhook_url" {
		t.Fatalf("project settings=%v", got)
	}
}

func TestValidateManifestFieldsOnly(t *testing.T) {
	m := Manifest{
		ID:      "severity",
		Name:    "Severity",
		Version: "1.0.0",
		HostAPI: 1,
		Fields: []Field{{
			Key:    "level",
			Type:   "enum",
			Label:  "Severity",
			ShowOn: []string{"sidebar", "kanban"},
			Options: []FieldOption{
				{Value: "low", Label: "Low"},
				{Value: "high", Label: "High"},
			},
		}},
	}
	if err := ValidateManifest("severity", m); err != nil {
		t.Fatal(err)
	}
	if !m.HasProjectSurface() || !m.HasFields() {
		t.Fatal("fields-only extension should appear on the project tab")
	}
}

func TestValidateManifestFieldBadType(t *testing.T) {
	m := Manifest{ID: "severity", Name: "S", Version: "1", HostAPI: 1, Fields: []Field{{Key: "level", Type: "date", Label: "X"}}}
	err := ValidateManifest("severity", m)
	if err == nil || !strings.Contains(err.Error(), "unknown fields type") {
		t.Fatalf("err=%v", err)
	}
}

func TestValidateManifestEnumRequiresOptions(t *testing.T) {
	m := Manifest{ID: "severity", Name: "S", Version: "1", HostAPI: 1, Fields: []Field{{Key: "level", Type: "enum", Label: "X"}}}
	err := ValidateManifest("severity", m)
	if err == nil || !strings.Contains(err.Error(), "enum requires options") {
		t.Fatalf("err=%v", err)
	}
}

func TestValidateManifestShowOnUnknown(t *testing.T) {
	m := Manifest{ID: "severity", Name: "S", Version: "1", HostAPI: 1, Fields: []Field{{Key: "level", Type: "string", Label: "X", ShowOn: []string{"filter"}}}}
	err := ValidateManifest("severity", m)
	if err == nil || !strings.Contains(err.Error(), "show_on") {
		t.Fatalf("err=%v", err)
	}
}

func TestShowsOnList(t *testing.T) {
	if ShowsOnList([]string{"sidebar"}) {
		t.Fatal("sidebar-only should not be on lists")
	}
	if !ShowsOnList([]string{"sidebar", "kanban"}) {
		t.Fatal("kanban should be on lists")
	}
}

func TestValidateManifestDescriptionTooLong(t *testing.T) {
	m := validDiscord()
	m.Description = strings.Repeat("x", maxDescriptionLen+1)
	if err := ValidateManifest("discord", m); err == nil {
		t.Fatal("expected description length error")
	}
	m.Description = ""
	m.Settings[0].Description = strings.Repeat("y", maxDescriptionLen+1)
	if err := ValidateManifest("discord", m); err == nil {
		t.Fatal("expected settings description length error")
	}
}

func TestLoadFailSoftAndSkipMissingManifest(t *testing.T) {
	root := t.TempDir()
	t.Setenv("EXTENSIONS_DIR", root)

	good := filepath.Join(root, "discord")
	if err := os.Mkdir(good, 0o755); err != nil {
		t.Fatal(err)
	}
	raw := `{
  "id":"discord","name":"Discord","version":"1.0.0","host_api":1,
  "hooks":[{"on":"task.updated"}],
  "delivery":{"type":"discord.webhook","url_from":"webhook_url"},
  "settings":[{"key":"webhook_url","type":"secret","label":"Webhook"}]
}`
	if err := os.WriteFile(filepath.Join(good, "manifest.json"), []byte(raw), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(root, "notes"), 0o755); err != nil {
		t.Fatal(err)
	}
	bad := filepath.Join(root, "broken")
	if err := os.Mkdir(bad, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bad, "manifest.json"), []byte(`{"id":"broken"}`), 0o644); err != nil {
		t.Fatal(err)
	}

	got := Load()
	if LoadedCount() != 1 {
		t.Fatalf("loaded=%d entries=%d", LoadedCount(), len(got))
	}
	e, ok := Get("discord")
	if !ok || !e.Loaded {
		t.Fatalf("discord not loaded: %#v", e)
	}
	b, ok := Get("broken")
	if !ok || b.Loaded || b.Error == "" {
		t.Fatalf("broken should be failed: %#v", b)
	}
	if _, ok := Get("notes"); ok {
		t.Fatal("notes without manifest should be skipped")
	}
}

func TestExampleDiscordManifest(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "examples", "extensions", "discord", "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	var m Manifest
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatal(err)
	}
	if err := ValidateManifest("discord", m); err != nil {
		t.Fatal(err)
	}
	if !m.HasProjectSettings() {
		t.Fatal("example discord should be project-scoped")
	}
	for _, s := range m.Settings {
		if s.Key == "project_ids" || s.Type == "project_ids" {
			t.Fatal("project_ids should be dropped from the example")
		}
		if s.Key == "webhook_url" && s.ScopeName() != ScopeProject {
			t.Fatalf("webhook_url scope=%q", s.ScopeName())
		}
		if s.Description == "" {
			t.Fatalf("example setting %s missing description", s.Key)
		}
	}
	if strings.TrimSpace(m.Description) == "" {
		t.Fatal("example should include an extension description")
	}
}

func TestExampleFieldsManifestsValidate(t *testing.T) {
	for _, id := range []string{"severity", "fields-demo"} {
		raw, err := os.ReadFile(filepath.Join("..", "..", "examples", "extensions", id, "manifest.json"))
		if err != nil {
			t.Fatal(err)
		}
		var m Manifest
		if err := json.Unmarshal(raw, &m); err != nil {
			t.Fatal(err)
		}
		if err := ValidateManifest(id, m); err != nil {
			t.Fatalf("%s: %v", id, err)
		}
		if !m.HasFields() || !m.HasProjectSurface() {
			t.Fatalf("%s should be fields-only project surface", id)
		}
		if m.HasProjectSettings() {
			t.Fatalf("%s should not need settings", id)
		}
	}
}

func TestLoadIDMismatchFails(t *testing.T) {
	root := t.TempDir()
	t.Setenv("EXTENSIONS_DIR", root)
	dir := filepath.Join(root, "discord")
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	raw := `{"id":"nope","name":"X","version":"1","host_api":1}`
	if err := os.WriteFile(filepath.Join(dir, "manifest.json"), []byte(raw), 0o644); err != nil {
		t.Fatal(err)
	}
	Load()
	e, ok := Get("nope")
	if !ok {
		e, ok = Get("discord")
	}
	if !ok || e.Loaded {
		t.Fatalf("expected failed entry, got %#v ok=%v", e, ok)
	}
}
