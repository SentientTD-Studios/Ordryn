package hooks

import (
	"testing"

	"GoTodo/internal/extensions"
	"GoTodo/internal/storage"
)

func TestHasWorkWithoutExtensions(t *testing.T) {
	if HasWork() {
		t.Fatal("no extensions loaded")
	}
}

func TestDispatchNoExtensionsIsNoop(t *testing.T) {
	Dispatch(Event{Type: "task.updated", TaskID: 1})
}

func TestDeliverSkipsWithoutProject(t *testing.T) {
	m := extensions.Manifest{
		ID:       "discord",
		Delivery: &extensions.Delivery{Type: "discord.webhook", URLFrom: "webhook_url"},
	}
	sent, err := deliver(m, "hello", 0, "task.updated", nil)
	if err != nil || sent {
		t.Fatalf("sent=%v err=%v", sent, err)
	}
}

func TestMemberDestinationsAllowed(t *testing.T) {
	if !MemberDestinationsAllowed(0, storage.WorkflowKanban) {
		t.Fatal("inbox tasks may use member destinations")
	}
	if MemberDestinationsAllowed(7, storage.WorkflowKanban) {
		t.Fatal("kanban projects must not fan out to member destinations")
	}
	if !MemberDestinationsAllowed(7, storage.WorkflowClassic) {
		t.Fatal("classic projects may use member destinations")
	}
}
