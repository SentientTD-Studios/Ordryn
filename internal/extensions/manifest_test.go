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
	if !m.HasProjectSettings() || m.HasMemberSettings() {
		t.Fatal("expected project settings only")
	}
	m.Settings = append(m.Settings, Setting{Key: "claimed_is_me", Type: "bool", Label: "Mine", Scope: ScopeMember})
	if err := ValidateManifest("discord", m); err != nil {
		t.Fatal(err)
	}
	if !m.HasMemberSettings() {
		t.Fatal("expected member settings")
	}
	if m.HasFields() {
		t.Fatal("discord fixture should not register fields")
	}
	if got := m.SettingsForScope(ScopeProject); len(got) != 1 || got[0].Key != "webhook_url" {
		t.Fatalf("project settings=%v", got)
	}
	if got := m.SettingsForScope(ScopeMember); len(got) != 1 || got[0].Key != "claimed_is_me" {
		t.Fatalf("member settings=%v", got)
	}
	if !m.HasProjectSurface() {
		t.Fatal("member settings should still appear on the project tab")
	}
}

func TestHasSettingAndFilterTypes(t *testing.T) {
	m := Manifest{
		ID:      "discord",
		Name:    "Discord",
		Version: "1.0.0",
		HostAPI: 1,
		Settings: []Setting{
			{Key: "webhook_url", Type: "secret", Label: "URL", Scope: ScopeProject},
			{Key: "skip_self", Type: "bool", Label: "Skip", Scope: ScopeProject},
			{Key: "min_priority", Type: "priority", Label: "Min", Scope: ScopeProject},
			{Key: "tag_ids", Type: "tag_ids", Label: "Tags", Scope: ScopeProject},
			{Key: "quiet_hours_start", Type: "time", Label: "Start", Scope: ScopeProject},
			{Key: "digest", Type: "digest", Label: "Digest", Scope: ScopeProject},
			{Key: "field_filter", Type: "field_filter", Label: "Field", Scope: ScopeProject},
			{Key: "mention_map", Type: "mention_map", Label: "Mentions", Scope: ScopeProject},
		},
	}
	if err := ValidateManifest("discord", m); err != nil {
		t.Fatal(err)
	}
	if !m.HasSetting("skip_self") || !m.HasSetting("min_priority") || !m.HasSetting("field_key") {
		t.Fatal("expected declared filter settings")
	}
	if m.HasSetting("claimed_only") {
		t.Fatal("undeclared setting")
	}
	m.Settings = append(m.Settings, Setting{Key: "wrong", Type: "priority", Label: "X", Scope: ScopeProject})
	err := ValidateManifest("discord", m)
	if err == nil || !strings.Contains(err.Error(), "requires key min_priority") {
		t.Fatalf("err=%v", err)
	}
}

func TestValidateManifestControls(t *testing.T) {
	m := validDiscord()
	m.Controls = []string{"send_test", "rotate_signing"}
	if err := ValidateManifest("discord", m); err != nil {
		t.Fatal(err)
	}
	if !m.HasControl(ControlSendTest) || !m.HasControl(ControlRotateSigning) {
		t.Fatal("expected declared controls")
	}
	if m.HasControl(ControlSampleJSON) {
		t.Fatal("undeclared sample_json")
	}
	m.Controls = []string{"send_test", "nope"}
	err := ValidateManifest("discord", m)
	if err == nil || !strings.Contains(err.Error(), "unknown control") {
		t.Fatalf("err=%v", err)
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

func TestValidateManifestEmailDeliveryRejected(t *testing.T) {
	m := validDiscord()
	m.Delivery.Type = "email"
	err := ValidateManifest("discord", m)
	if err == nil || !strings.Contains(err.Error(), "site email is not available to extensions") {
		t.Fatalf("err=%v", err)
	}
}

func TestDueDatesRelayManifestValidates(t *testing.T) {
	m := Manifest{
		ID:          "due-dates",
		Name:        "Due dates",
		Version:     "1.2.0",
		HostAPI:     1,
		Description: "POST due-date events to your own HTTPS relay. Does not use Admin email.",
		Hooks: []Hook{
			{On: "task.due_changed"},
			{On: "task.overdue"},
		},
		Delivery: &Delivery{Type: DeliveryHTTPWebhook, URLFrom: "webhook_url", Format: DeliveryFormatJSON},
		Settings: []Setting{
			{Key: "webhook_url", Type: "secret", Label: "Relay URL", Description: "Public HTTPS URL of your email relay.", Required: true, Scope: ScopeProject},
			{Key: "triggers", Type: "hook_select", Label: "Triggers", Description: "Choose due-date events.", Scope: ScopeProject},
		},
		Templates: map[string]string{
			"task.due_changed": "Due date for {name} is now {due_date}",
			"task.overdue":     "{name} is overdue ({due_date})",
		},
	}
	if err := ValidateManifest("due-dates", m); err != nil {
		t.Fatal(err)
	}
	if m.Delivery.Type != DeliveryHTTPWebhook || m.Delivery.Format != DeliveryFormatJSON {
		t.Fatalf("due-dates should post JSON to a custom relay, got %#v", m.Delivery)
	}
	if !m.HasProjectSettings() || !m.HasProjectSurface() {
		t.Fatal("due-dates should appear on the project tab")
	}
}

func TestValidateManifestUnknownDelivery(t *testing.T) {
	m := validDiscord()
	m.Delivery.Type = "smtp"
	err := ValidateManifest("discord", m)
	if err == nil || !strings.Contains(err.Error(), "unknown delivery type") {
		t.Fatalf("err=%v", err)
	}
}

func TestValidateManifestHTTPWebhookFormat(t *testing.T) {
	m := Manifest{
		ID:       "webhook",
		Name:     "Webhook",
		Version:  "1.0.0",
		HostAPI:  1,
		Delivery: &Delivery{Type: DeliveryHTTPWebhook, URLFrom: "webhook_url", Format: DeliveryFormatJSON},
		Settings: []Setting{{Key: "webhook_url", Type: "secret", Label: "Webhook URL", Required: true, Scope: ScopeProject}},
	}
	if err := ValidateManifest("webhook", m); err != nil {
		t.Fatal(err)
	}
	m.Delivery.Format = "xml"
	err := ValidateManifest("webhook", m)
	if err == nil || !strings.Contains(err.Error(), "unknown delivery format") {
		t.Fatalf("err=%v", err)
	}
	m.Delivery.Type = DeliverySlackWebhook
	m.Delivery.Format = DeliveryFormatJSON
	err = ValidateManifest("webhook", m)
	if err == nil || !strings.Contains(err.Error(), "delivery.format is only allowed") {
		t.Fatalf("err=%v", err)
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

func TestExampleNotificationManifestsValidate(t *testing.T) {
	ids := []string{"discord", "slack", "teams", "google-chat", "webhook", "ntfy"}
	wantType := map[string]string{
		"discord":     DeliveryDiscordWebhook,
		"slack":       DeliverySlackWebhook,
		"teams":       DeliveryTeamsWebhook,
		"google-chat": DeliveryGoogleChatWebhook,
		"webhook":     DeliveryHTTPWebhook,
		"ntfy":        DeliveryNtfyWebhook,
	}
	for _, id := range ids {
		t.Run(id, func(t *testing.T) {
			raw, err := os.ReadFile(filepath.Join("..", "..", "examples", "extensions", id, "manifest.json"))
			if err != nil {
				raw, err = os.ReadFile(filepath.Join("..", "..", "data", "extensions", id, "manifest.json"))
			}
			if err != nil {
				t.Skipf("examples/extensions/%s not present (separate repo): %v", id, err)
			}
			var m Manifest
			if err := json.Unmarshal(raw, &m); err != nil {
				t.Fatalf("%s: %v", id, err)
			}
			if err := ValidateManifest(id, m); err != nil {
				t.Fatalf("%s: %v", id, err)
			}
			if m.Delivery == nil {
				t.Fatalf("%s missing delivery", id)
			}
			if m.Delivery.Type != wantType[id] {
				t.Fatalf("%s delivery type=%q want %q", id, m.Delivery.Type, wantType[id])
			}
			if id == "webhook" && m.Delivery.Format != DeliveryFormatJSON {
				t.Fatalf("generic webhook should use json format, got %q", m.Delivery.Format)
			}
			if !m.HasProjectSettings() {
				t.Fatalf("%s should be project-scoped", id)
			}
			if strings.TrimSpace(m.Description) == "" {
				t.Fatalf("%s should include an extension description", id)
			}
			for _, s := range m.Settings {
				if s.Key == "project_ids" || s.Type == "project_ids" {
					t.Fatalf("%s: project_ids should not be in the example", id)
				}
				if s.Description == "" {
					t.Fatalf("%s setting %s missing description", id, s.Key)
				}
				if s.Key == "webhook_url" && s.ScopeName() != ScopeProject {
					t.Fatalf("%s webhook_url scope=%q", id, s.ScopeName())
				}
			}
		})
	}
}

func TestExampleFocusedHookManifestsValidate(t *testing.T) {
	type want struct {
		delivery string
		siteOnly bool
	}
	cases := map[string]want{
		"due-dates":     {delivery: DeliveryHTTPWebhook},
		"email":         {delivery: DeliveryHTTPWebhook},
		"comments":      {delivery: DeliveryHTTPWebhook},
		"claimed":       {delivery: DeliveryNtfyWebhook},
		"activity":      {delivery: DeliveryHTTPWebhook},
		"join-requests": {delivery: DeliveryHTTPWebhook, siteOnly: true},
	}
	for id, w := range cases {
		t.Run(id, func(t *testing.T) {
			raw, err := os.ReadFile(filepath.Join("..", "..", "examples", "extensions", id, "manifest.json"))
			if err != nil {
				raw, err = os.ReadFile(filepath.Join("..", "..", "data", "extensions", id, "manifest.json"))
			}
			if err != nil {
				t.Skipf("examples/extensions/%s not present (separate repo): %v", id, err)
			}
			var m Manifest
			if err := json.Unmarshal(raw, &m); err != nil {
				t.Fatalf("%s: %v", id, err)
			}
			if err := ValidateManifest(id, m); err != nil {
				t.Fatalf("%s: %v", id, err)
			}
			if m.Delivery == nil || m.Delivery.Type != w.delivery {
				t.Fatalf("%s delivery=%v want %s", id, m.Delivery, w.delivery)
			}
			if id == "email" || id == "due-dates" {
				if m.Delivery.Format != DeliveryFormatJSON {
					t.Fatalf("%s relay should use json format, got %q", id, m.Delivery.Format)
				}
				if !strings.Contains(strings.ToLower(m.Description), "admin") {
					t.Fatalf("%s relay description should say it does not use Admin email", id)
				}
			}
			if w.siteOnly {
				if m.HasProjectSurface() || m.HasProjectSettings() {
					t.Fatalf("%s should stay in Admin (no project surface)", id)
				}
				if !m.DeclaresHook("join.request") {
					t.Fatalf("%s should declare join.request", id)
				}
				return
			}
			if !m.HasProjectSettings() || !m.HasProjectSurface() {
				t.Fatalf("%s should appear on the project tab", id)
			}
			for _, s := range m.Settings {
				if s.Description == "" {
					t.Fatalf("%s setting %s missing description", id, s.Key)
				}
			}
		})
	}
}

func TestExampleFieldsManifestsValidate(t *testing.T) {
	for _, id := range []string{"severity", "fields-demo", "estimate"} {
		raw, err := os.ReadFile(filepath.Join("..", "..", "examples", "extensions", id, "manifest.json"))
		if err != nil {
			t.Skipf("examples/extensions/%s not present (separate repo): %v", id, err)
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
