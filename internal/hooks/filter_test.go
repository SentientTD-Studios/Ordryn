package hooks

import (
	"testing"
	"time"

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

func TestShouldDeliverPriorityTagsClaimedAndSkipSelf(t *testing.T) {
	m := testManifest()
	site := storage.ExtensionSettings{Enabled: true}
	ev := Event{
		Type:    "task.updated",
		ActorID: 5,
		Snapshot: &storage.HookTaskSnapshot{
			Priority:  1,
			TagIDs:    []int{2},
			ClaimedBy: 9,
		},
	}
	team := destFromProject(storage.ExtensionProjectSettings{
		Enabled:     true,
		Triggers:    []string{"task.updated"},
		MinPriority: 2,
	})
	if shouldDeliverDest(m, site, team, ev, 3) {
		t.Fatal("min_priority")
	}
	team.MinPriority = 1
	team.TagIDs = []int{8}
	if shouldDeliverDest(m, site, team, ev, 3) {
		t.Fatal("tag filter")
	}
	team.TagIDs = []int{2}
	team.ClaimedOnly = true
	ev.Snapshot.ClaimedBy = 0
	if shouldDeliverDest(m, site, team, ev, 3) {
		t.Fatal("claimed_only")
	}
	ev.Snapshot.ClaimedBy = 5
	skipOff := false
	mem := destFromMember(storage.ExtensionMemberSettings{
		Enabled:     true,
		Triggers:    []string{"task.updated"},
		ClaimedIsMe: true,
		SkipSelf:    &skipOff,
	}, 5, false)
	if !shouldDeliverDest(m, site, mem, ev, 3) {
		t.Fatal("claimed_is_me match")
	}
	mem.SkipSelf = true
	if shouldDeliverDest(m, site, mem, ev, 3) {
		t.Fatal("skip-self")
	}
	personal := destFromMember(storage.ExtensionMemberSettings{
		Enabled:  true,
		Triggers: []string{"task.updated"},
		SkipSelf: &skipOff,
	}, 5, true)
	if shouldDeliverDest(m, site, personal, ev, 3) {
		t.Fatal("personal dest must not receive project events")
	}
	if !shouldDeliverDest(m, site, personal, ev, 0) {
		t.Fatal("personal dest should receive inbox events")
	}
}

func TestApplyMentions(t *testing.T) {
	if got := applyMentions("ada", map[string]string{"ada": "<@U1>"}); got != "<@U1>" {
		t.Fatalf("got %q", got)
	}
	if got := applyMentions("ada", nil); got != "ada" {
		t.Fatalf("got %q", got)
	}
}

func TestInQuietHours(t *testing.T) {
	now := time.Date(2026, 9, 15, 22, 0, 0, 0, time.UTC)
	if !inQuietHours("21:00", "07:00", "UTC", now) {
		t.Fatal("expected quiet")
	}
	if inQuietHours("08:00", "17:00", "UTC", now) {
		t.Fatal("not quiet")
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
