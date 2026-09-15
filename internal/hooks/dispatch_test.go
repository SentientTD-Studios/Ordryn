package hooks

import (
	"testing"

	"GoTodo/internal/mods"
)

func TestHasWorkWithoutMods(t *testing.T) {
	if HasWork() {
		t.Fatal("no mods loaded")
	}
}

func TestDispatchNoModsIsNoop(t *testing.T) {
	Dispatch(Event{Type: "task.updated", TaskID: 1})
}

func TestDeliverSkipsWithoutProject(t *testing.T) {
	m := mods.Manifest{
		ID:       "discord",
		Delivery: &mods.Delivery{Type: "discord.webhook", URLFrom: "webhook_url"},
	}
	sent, err := deliver(m, "hello", 0)
	if err != nil || sent {
		t.Fatalf("sent=%v err=%v", sent, err)
	}
}
