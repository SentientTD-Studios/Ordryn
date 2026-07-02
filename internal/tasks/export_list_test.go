package tasks_test

import (
	"GoTodo/internal/tasks"
	"testing"
)

func TestFetchAllTasksForUserWithFilters(t *testing.T) {
	userID := 1
	timezone := "America/New_York"
	tagID := 1

	list, err := tasks.FetchAllTasksForUserWithFilters(&userID, timezone, tasks.ListFilters{TagFilter: &tagID}, "")
	if err != nil {
		t.Fatalf("FetchAllTasksForUserWithFilters: %v", err)
	}
	if len(list) != 1 || list[0].Title != "Tagged task" {
		t.Fatalf("expected 1 tagged task, got %v", list)
	}
	if len(list[0].Tags) != 1 || list[0].Tags[0].Name != "work" {
		t.Fatalf("expected work tag attached, got %v", list[0].Tags)
	}
}
