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
		Settings: []extensions.Setting{
			{Key: "status_only", Type: "bool", Label: "Status only", Scope: extensions.ScopeList{extensions.ScopeProject}},
			{Key: "skip_self", Type: "bool", Label: "Skip self", Scope: extensions.ScopeList{extensions.ScopeProject}},
			{Key: "min_priority", Type: "priority", Label: "Min priority", Scope: extensions.ScopeList{extensions.ScopeProject}},
			{Key: "tag_ids", Type: "tag_ids", Label: "Tags", Scope: extensions.ScopeList{extensions.ScopeProject}},
			{Key: "claimed_only", Type: "bool", Label: "Claimed", Scope: extensions.ScopeList{extensions.ScopeProject}},
			{Key: "claimed_is_me", Type: "bool", Label: "Claimed by me", Scope: extensions.ScopeList{extensions.ScopeProject}},
		},
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
	team := destFromProject(m, storage.ExtensionProjectSettings{
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
	mem := destFromMember(m, storage.ExtensionMemberSettings{
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
	personal := destFromMember(m, storage.ExtensionMemberSettings{
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

func TestShouldDeliverIgnoresUndeclaredFilters(t *testing.T) {
	m := extensions.Manifest{
		ID:   "discord",
		Name: "Discord",
		Hooks: []extensions.Hook{
			{On: "task.updated"},
		},
	}
	site := storage.ExtensionSettings{Enabled: true}
	ev := Event{
		Type: "task.updated",
		Snapshot: &storage.HookTaskSnapshot{
			Priority: 0,
		},
	}
	team := destFromProject(m, storage.ExtensionProjectSettings{
		Enabled:     true,
		Triggers:    []string{"task.updated"},
		MinPriority: 3,
		ClaimedOnly: true,
		SkipSelf:    true,
	})
	if !shouldDeliverDest(m, site, team, ev, 3) {
		t.Fatal("undeclared min_priority/claimed_only/skip_self should not filter")
	}
}

func TestShouldDeliverMentionedAndMemberLeft(t *testing.T) {
	m := extensions.Manifest{
		ID:   "discord",
		Name: "Discord",
		Hooks: []extensions.Hook{
			{On: "task.mentioned"},
			{On: "project.member_left"},
		},
	}
	site := storage.ExtensionSettings{Enabled: true}
	mentioned := Event{Type: EventTaskMentioned, MentionedUserIDs: []int{2, 3}}

	team := destFromProject(m, storage.ExtensionProjectSettings{
		Enabled:  true,
		Triggers: []string{EventTaskMentioned},
	})
	if !shouldDeliverDest(m, site, team, mentioned, 3) {
		t.Fatal("team should receive mentions")
	}

	member := destFromMember(m, storage.ExtensionMemberSettings{
		Enabled:  true,
		Triggers: []string{EventTaskMentioned},
	}, 2, false)
	if !shouldDeliverDest(m, site, member, mentioned, 3) {
		t.Fatal("mentioned member should receive")
	}
	other := destFromMember(m, storage.ExtensionMemberSettings{
		Enabled:  true,
		Triggers: []string{EventTaskMentioned},
	}, 9, false)
	if shouldDeliverDest(m, site, other, mentioned, 3) {
		t.Fatal("unmentioned member should not receive")
	}

	left := Event{Type: EventProjectMemberLeft, MemberID: 2}
	leftTeam := destFromProject(m, storage.ExtensionProjectSettings{
		Enabled:  true,
		Triggers: []string{EventProjectMemberLeft},
	})
	if !shouldDeliverDest(m, site, leftTeam, left, 3) {
		t.Fatal("team should receive member_left")
	}
	departed := destFromMember(m, storage.ExtensionMemberSettings{
		Enabled:  true,
		Triggers: []string{EventProjectMemberLeft},
	}, 2, false)
	if shouldDeliverDest(m, site, departed, left, 3) {
		t.Fatal("departed member should not receive member_left")
	}
	stayer := destFromMember(m, storage.ExtensionMemberSettings{
		Enabled:  true,
		Triggers: []string{EventProjectMemberLeft},
	}, 3, false)
	if !shouldDeliverDest(m, site, stayer, left, 3) {
		t.Fatal("remaining member should receive member_left")
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
	s = map[string]string{"*": "catchall"}
	if got := templateFor(m, s, "task.created"); got != "catchall" {
		t.Fatalf("wildcard template got %q", got)
	}
}

func TestTriggerAllowedWildcard(t *testing.T) {
	if triggerAllowed(nil, "task.updated") {
		t.Fatal("empty triggers are none")
	}
	if !triggerAllowed([]string{"*"}, "task.status_changed") {
		t.Fatal("star should match declared-or-not at this layer")
	}
	if !triggerAllowed([]string{"task.updated", "*"}, "task.created") {
		t.Fatal("star among others")
	}
}

func TestShouldDeliverWildcardAndStatusFilters(t *testing.T) {
	m := extensions.Manifest{
		ID: "discord",
		Hooks: []extensions.Hook{
			{On: "task.updated"},
			{On: "task.status_changed"},
			{On: "task.created"},
		},
		Settings: []extensions.Setting{
			{Key: "status_ids", Type: "status_ids", Scope: extensions.ScopeList{extensions.ScopeProject}},
			{Key: "status_exclude_ids", Type: "status_exclude_ids", Scope: extensions.ScopeList{extensions.ScopeProject}},
			{Key: "status_only", Type: "bool", Scope: extensions.ScopeList{extensions.ScopeProject}},
		},
	}
	site := storage.ExtensionSettings{Enabled: true}
	star := destFromProject(m, storage.ExtensionProjectSettings{
		Enabled:  true,
		Triggers: []string{"*"},
	})
	if !shouldDeliverDest(m, site, star, Event{Type: "task.created"}, 3) {
		t.Fatal("wildcard should deliver created")
	}
	if shouldDeliverDest(m, site, star, Event{Type: "task.deleted"}, 3) {
		t.Fatal("undeclared hook still blocked")
	}

	include := destFromProject(m, storage.ExtensionProjectSettings{
		Enabled:   true,
		Triggers:  []string{"task.updated"},
		StatusIDs: []int{4},
	})
	if shouldDeliverDest(m, site, include, Event{Type: "task.updated", Snapshot: &storage.HookTaskSnapshot{StatusID: 9}}, 3) {
		t.Fatal("status include miss")
	}
	if !shouldDeliverDest(m, site, include, Event{Type: "task.updated", Snapshot: &storage.HookTaskSnapshot{StatusID: 4}}, 3) {
		t.Fatal("status include hit")
	}
	exclude := destFromProject(m, storage.ExtensionProjectSettings{
		Enabled:          true,
		Triggers:         []string{"task.updated"},
		StatusExcludeIDs: []int{4},
	})
	if shouldDeliverDest(m, site, exclude, Event{Type: "task.updated", Snapshot: &storage.HookTaskSnapshot{StatusID: 4}}, 3) {
		t.Fatal("status exclude")
	}

	legacy := destFromProject(m, storage.ExtensionProjectSettings{
		Enabled:    true,
		Triggers:   []string{"task.updated"},
		StatusOnly: true,
	})
	if !shouldDeliverDest(m, site, legacy, Event{Type: EventTaskStatusChanged, Snapshot: &storage.HookTaskSnapshot{StatusID: 1}}, 3) {
		t.Fatal("status_only configs that listed task.updated should still get status_changed")
	}
}

func TestShouldDeliverTaskUpdatedMatchesSpecializedEvents(t *testing.T) {
	m := extensions.Manifest{
		ID: "discord",
		Hooks: []extensions.Hook{
			{On: "task.created"},
			{On: "task.updated"},
			{On: "task.deleted"},
			{On: "task.commented"},
		},
		Templates: map[string]string{
			"task.updated": "Task {name} updated to {status}",
		},
		Settings: []extensions.Setting{
			{Key: "status_only", Type: "bool", Scope: extensions.ScopeList{extensions.ScopeProject}},
		},
	}
	site := storage.ExtensionSettings{Enabled: true}
	dest := storage.ExtensionProjectSettings{
		Enabled:  true,
		Triggers: []string{"task.updated"},
	}
	for _, typ := range []string{
		EventTaskStatusChanged,
		EventTaskDueChanged,
		EventTaskMoved,
		EventTaskTagged,
		EventTaskClaimed,
		EventTaskCompleted,
		EventTaskArchived,
	} {
		ev := Event{Type: typ, StatusChanged: typ == EventTaskStatusChanged || typ == EventTaskCompleted}
		if !ShouldDeliver(m, site, dest, ev, 3) {
			t.Fatalf("task.updated trigger should deliver %s", typ)
		}
		if got := templateFor(m, dest.Templates, typ); got != "Task {name} updated to {status}" {
			t.Fatalf("template for %s = %q", typ, got)
		}
	}
	if ShouldDeliver(m, site, dest, Event{Type: EventTaskCommented}, 3) {
		t.Fatal("commented is not a task.updated child")
	}
	if ShouldDeliver(m, site, dest, Event{Type: EventTaskDeleted}, 3) {
		t.Fatal("deleted is not a task.updated child")
	}

	statusOnly := dest
	statusOnly.StatusOnly = true
	if ShouldDeliver(m, site, statusOnly, Event{Type: EventTaskDueChanged}, 3) {
		t.Fatal("status_only should skip due_changed matched via task.updated")
	}
	if !ShouldDeliver(m, site, statusOnly, Event{Type: EventTaskStatusChanged, StatusChanged: true}, 3) {
		t.Fatal("status_only should still deliver status_changed")
	}

	specific := extensions.Manifest{
		ID: "discord",
		Hooks: []extensions.Hook{
			{On: "task.updated"},
			{On: "task.status_changed"},
		},
		Templates: map[string]string{
			"task.updated":        "updated",
			"task.status_changed": "status",
		},
	}
	onlyStatus := storage.ExtensionProjectSettings{Enabled: true, Triggers: []string{"task.status_changed"}}
	if !ShouldDeliver(specific, site, onlyStatus, Event{Type: EventTaskStatusChanged, StatusChanged: true}, 3) {
		t.Fatal("explicit status_changed trigger")
	}
	if ShouldDeliver(specific, site, onlyStatus, Event{Type: EventTaskDueChanged}, 3) {
		t.Fatal("due_changed should not match a status_changed-only trigger")
	}
	if got := templateFor(specific, nil, EventTaskStatusChanged); got != "status" {
		t.Fatalf("specific template=%q", got)
	}
}
