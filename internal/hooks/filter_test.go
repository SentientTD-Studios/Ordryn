package hooks

import (
	"testing"

	"GoTodo/internal/extensions"
	"GoTodo/internal/storage"
)

func testManifest() extensions.Manifest {
	return extensions.Manifest{
		ID:   "discord",
		Name: "Discord",
		Hooks: []extensions.Hook{
			{On: "task.created"},
			{On: "task.updated"},
		},
		Templates: map[string]string{"task.updated": "default"},
	}
}

func TestShouldDeliverFilters(t *testing.T) {
	m := testManifest()
	site := storage.ExtensionSettings{Enabled: true}
	base := storage.ExtensionProjectSettings{
		Enabled:  true,
		Triggers: []string{"task.updated"},
	}
	ev := Event{Type: "task.updated", StatusChanged: true}

	if !ShouldDeliver(m, site, base, ev, 3) {
		t.Fatal("expected deliver")
	}

	siteOff := site
	siteOff.Enabled = false
	if ShouldDeliver(m, siteOff, base, ev, 3) {
		t.Fatal("site disabled")
	}

	projOff := base
	projOff.Enabled = false
	if ShouldDeliver(m, site, projOff, ev, 3) {
		t.Fatal("project disabled")
	}

	if ShouldDeliver(m, site, base, ev, 0) {
		t.Fatal("personal task")
	}

	wrongTrigger := base
	wrongTrigger.Triggers = []string{"task.created"}
	if ShouldDeliver(m, site, wrongTrigger, ev, 3) {
		t.Fatal("wrong trigger")
	}

	statusOnly := base
	statusOnly.StatusOnly = true
	if ShouldDeliver(m, site, statusOnly, Event{Type: "task.updated"}, 3) {
		t.Fatal("status_only without change")
	}
	if !ShouldDeliver(m, site, statusOnly, ev, 3) {
		t.Fatal("status_only with change")
	}

	if ShouldDeliver(m, site, base, Event{Type: "task.deleted", StatusChanged: true}, 3) {
		t.Fatal("undeclared hook")
	}
}

func TestTemplateForOverrideAndBlank(t *testing.T) {
	m := testManifest()
	s := map[string]string{"task.updated": "custom"}
	if got := templateFor(m, s, "task.updated"); got != "custom" {
		t.Fatalf("got %q", got)
	}
	s["task.updated"] = ""
	if got := templateFor(m, s, "task.updated"); got != "" {
		t.Fatalf("blank override should skip, got %q", got)
	}
	if got := templateFor(m, nil, "task.updated"); got != "default" {
		t.Fatalf("got %q", got)
	}
}
