package domain

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"GoTodo/internal/hooks"
	"GoTodo/internal/live"
	"GoTodo/internal/storage"
)

func TestCreateTaskAfterInsert(t *testing.T) {
	ctx := context.Background()
	proj, err := CreateProject(ctx, 1, "After Insert Proj", "")
	if err != nil {
		t.Fatal(err)
	}
	pid := proj.ID

	// Runs before anyone hears about the task.
	types := listenHookTypes(t)
	var sawCreatedFirst bool
	id, err := CreateTask(ctx, 1, CreateTaskInput{Title: "Hooked", ProjectID: &pid, AfterInsert: func(int) error {
		sawCreatedFirst = hasHookType(*types, live.TypeTaskCreated)
		return nil
	}})
	if err != nil || id <= 0 {
		t.Fatalf("create: %d %v", id, err)
	}
	if sawCreatedFirst {
		t.Fatal("AfterInsert ran after task.created went out")
	}

	// A failing hook removes the task and reports the error.
	boom := errors.New("boom")
	var made int
	if _, err := CreateTask(ctx, 1, CreateTaskInput{Title: "Doomed", ProjectID: &pid, AfterInsert: func(id int) error {
		made = id
		return boom
	}}); !errors.Is(err, boom) {
		t.Fatalf("err = %v, want boom", err)
	}
	if _, _, err := storage.TaskOwnerAndProject(made); err == nil {
		t.Fatal("task should have been removed")
	}
}

// The storage and event pieces behind POST /api/v2/chat/links/{id}/tasks.
func TestChatTaskSources(t *testing.T) {
	ctx := context.Background()
	cs, _, err := storage.CreateChatServer("Tasks chat", "https://chat-tasks.example.com", false, true, true, false, 1)
	if err != nil {
		t.Fatal(err)
	}
	proj, err := CreateProject(ctx, 1, "Chat Tasks Proj", "")
	if err != nil {
		t.Fatal(err)
	}
	pid := proj.ID
	if err := upsertTestMember(t, pid, 3, testRoleViewer); err != nil {
		t.Fatal(err)
	}
	if err := storage.UpsertChatLink(cs.ID, "lnk_tasks", pid, 1); err != nil {
		t.Fatal(err)
	}
	l, err := storage.GetChatLink(cs.ID, "lnk_tasks")
	if err != nil || l == nil || l.ProjectID != pid || !l.Active {
		t.Fatalf("link = %+v %v", l, err)
	}
	if l, _ := storage.GetChatLink(cs.ID, "nope"); l != nil {
		t.Fatal("unknown link found")
	}

	create := func(userID int, key string) (int, error) {
		existing, err := storage.ReserveChatTaskSource(cs.ID, key, "lnk_tasks")
		if err != nil || existing > 0 {
			return existing, err
		}
		id, err := CreateTask(ctx, userID, CreateTaskInput{Title: "From chat", ProjectID: &pid,
			AfterInsert: func(id int) error { return storage.SetChatTaskSourceTask(cs.ID, key, id) }})
		if err != nil {
			_ = storage.ReleaseChatTaskSource(cs.ID, key)
		}
		return id, err
	}

	// A viewer can't add tasks; the key is left free.
	if _, err := create(3, "chatmsg_1"); !errors.Is(err, ErrForbidden) {
		t.Fatalf("viewer: %v", err)
	}
	id, err := create(1, "chatmsg_1")
	if err != nil || id <= 0 {
		t.Fatalf("create: %d %v", id, err)
	}
	if key, _ := storage.ChatTaskSourceKey(cs.ID, id); key != "chatmsg_1" {
		t.Fatalf("source key = %q", key)
	}

	// Same key: same task.
	if again, err := create(1, "chatmsg_1"); err != nil || again != id {
		t.Fatalf("repeat = %d %v, want %d", again, err, id)
	}

	// A key mid-creation blocks a second request.
	if _, err := storage.ReserveChatTaskSource(cs.ID, "chatmsg_2", "lnk_tasks"); err != nil {
		t.Fatal(err)
	}
	if _, err := storage.ReserveChatTaskSource(cs.ID, "chatmsg_2", "lnk_tasks"); !errors.Is(err, storage.ErrChatTaskInProgress) {
		t.Fatalf("in-flight key: %v", err)
	}
	_ = storage.ReleaseChatTaskSource(cs.ID, "chatmsg_2")

	// The task.created event carries the key, only for this chat server.
	other, _, err := storage.CreateChatServer("Other chat", "https://chat-other.example.com", false, true, false, false, 1)
	if err != nil {
		t.Fatal(err)
	}
	if err := storage.UpsertChatLink(other.ID, "lnk_other", pid, 1); err != nil {
		t.Fatal(err)
	}
	hooks.HandleChatEvent(hooks.Event{Type: hooks.EventTaskCreated, TaskID: id, ProjectID: pid, ActorID: 1})
	sourceOf := func(serverID int) string {
		t.Helper()
		evs, err := storage.TakeChatEvents(serverID, 0, 50)
		if err != nil {
			t.Fatal(err)
		}
		for _, e := range evs {
			var p hooks.ChatEventPayload
			if err := json.Unmarshal(e.Payload, &p); err != nil {
				t.Fatal(err)
			}
			if p.Type == hooks.EventTaskCreated && p.TaskID == id {
				return "seen:" + p.SourceKey
			}
		}
		return ""
	}
	if got := sourceOf(cs.ID); got != "seen:chatmsg_1" {
		t.Fatalf("this server's event = %q", got)
	}
	if got := sourceOf(other.ID); got != "seen:" {
		t.Fatalf("other server's event = %q", got)
	}

	// Once the task is deleted, the key makes a new task.
	if err := DeleteTask(ctx, 1, id); err != nil {
		t.Fatal(err)
	}
	fresh, err := create(1, "chatmsg_1")
	if err != nil || fresh <= 0 || fresh == id {
		t.Fatalf("after delete = %d %v (old %d)", fresh, err, id)
	}

	// Banned users can't act through chat.
	if ok, _ := storage.ChatUserCanAct(1); !ok {
		t.Fatal("user 1 should be able to act")
	}
	if ok, _ := storage.ChatUserCanAct(0); ok {
		t.Fatal("user 0 can't act")
	}
}
