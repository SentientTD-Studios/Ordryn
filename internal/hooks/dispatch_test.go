package hooks

import (
	"testing"

	"GoTodo/internal/extensions"
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
	sent, err := deliver(m, "hello", 0)
	if err != nil || sent {
		t.Fatalf("sent=%v err=%v", sent, err)
	}
}
