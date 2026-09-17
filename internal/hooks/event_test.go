package hooks

import (
	"testing"

	"GoTodo/internal/storage"
)

func TestEventClassification(t *testing.T) {
	if !(Event{Type: EventJoinRequest}.isSiteEvent()) || !(Event{Type: EventJoinApproved}.isSiteEvent()) || !(Event{Type: EventJoinDenied}.isSiteEvent()) {
		t.Fatal("join events should be site-level")
	}
	if (Event{Type: EventTaskMentioned}.isSiteEvent()) {
		t.Fatal("task.mentioned is not a site event")
	}
	if !(Event{Type: EventProjectMemberJoined}.isProjectLevel()) || !(Event{Type: EventProjectMemberLeft}.isProjectLevel()) {
		t.Fatal("membership events should be project-level")
	}
	if (Event{Type: EventTaskCompleted}.isProjectLevel()) {
		t.Fatal("task.completed is not project-level")
	}
	if !(Event{Type: EventSprintCreated}.isProjectLevel()) || !(Event{Type: EventProjectArchived}.isProjectLevel()) {
		t.Fatal("sprint and project archive events should be project-level")
	}
}

func TestEventVarsMentionsMemberAndJoin(t *testing.T) {
	vars := eventVars(Event{
		Type:       EventTaskMentioned,
		Mentions:   []string{"bob_editor", "carol_viewer"},
		MemberName: "ignored",
		Comment:    "Please look",
	}, &storage.HookTaskSnapshot{ID: 9, Title: "Need Bob", ProjectName: "Ordryn"}, "ada")
	if vars["mentions"] != "bob_editor, carol_viewer" {
		t.Fatalf("mentions=%q", vars["mentions"])
	}
	if vars["member"] != "ignored" {
		t.Fatalf("member=%q", vars["member"])
	}

	join := eventVars(Event{Type: EventJoinApproved, JoinEmail: "new@example.com", JoinMessage: "hi"}, nil, "")
	if join["join_email"] != "new@example.com" || join["name"] != "new@example.com" {
		t.Fatalf("join vars=%v", join)
	}
	if join["url"] == "" && publicAdminJoinURL() != "" {
		t.Fatal("approved join should use admin URL when PUBLIC_URL is set")
	}
}
