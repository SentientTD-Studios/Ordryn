package domain

import (
	"context"
	"testing"

	"GoTodo/internal/storage"
)

func TestUpdateTaskLogsTitleChanged(t *testing.T) {
	ctx := context.Background()
	taskID, err := CreateTask(ctx, 1, CreateTaskInput{Title: "Original title"})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	title := "Renamed title"
	if _, err := UpdateTask(ctx, 1, taskID, UpdateTaskInput{Title: &title}); err != nil {
		t.Fatalf("rename: %v", err)
	}
	changed := eventsOfType(t, taskID, 1, "title_changed")
	if len(changed) != 1 {
		t.Fatalf("title_changed count=%d want 1", len(changed))
	}
	if changed[0].Metadata["from"] != "Original title" {
		t.Errorf("from=%v want Original title", changed[0].Metadata["from"])
	}
	if changed[0].Metadata["to"] != "Renamed title" {
		t.Errorf("to=%v want Renamed title", changed[0].Metadata["to"])
	}

	if _, err := UpdateTask(ctx, 1, taskID, UpdateTaskInput{Title: &title}); err != nil {
		t.Fatalf("same title: %v", err)
	}
	if got := eventsOfType(t, taskID, 1, "title_changed"); len(got) != 1 {
		t.Fatalf("after no-op title_changed count=%d want 1", len(got))
	}
}

func TestUpdateTaskLogsPriorityChanged(t *testing.T) {
	ctx := context.Background()
	taskID, err := CreateTask(ctx, 1, CreateTaskInput{Title: "Priority activity", Priority: 1})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	high := 3
	if _, err := UpdateTask(ctx, 1, taskID, UpdateTaskInput{Priority: &high}); err != nil {
		t.Fatalf("set priority: %v", err)
	}
	changed := eventsOfType(t, taskID, 1, "priority_changed")
	if len(changed) != 1 {
		t.Fatalf("priority_changed count=%d want 1", len(changed))
	}
	if changed[0].Metadata["from"] != "Low" {
		t.Errorf("from=%v want Low", changed[0].Metadata["from"])
	}
	if changed[0].Metadata["to"] != "High" {
		t.Errorf("to=%v want High", changed[0].Metadata["to"])
	}

	if _, err := UpdateTask(ctx, 1, taskID, UpdateTaskInput{Priority: &high}); err != nil {
		t.Fatalf("same priority: %v", err)
	}
	if got := eventsOfType(t, taskID, 1, "priority_changed"); len(got) != 1 {
		t.Fatalf("after no-op priority_changed count=%d want 1", len(got))
	}
}

func TestUpdateTaskLogsDueDateChanged(t *testing.T) {
	ctx := context.Background()
	taskID, err := CreateTask(ctx, 1, CreateTaskInput{Title: "Due activity", DueDate: "2026-01-15"})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	next := "2026-09-21"
	if _, err := UpdateTask(ctx, 1, taskID, UpdateTaskInput{DueDate: &next}); err != nil {
		t.Fatalf("set due date: %v", err)
	}
	changed := eventsOfType(t, taskID, 1, "due_date_changed")
	if len(changed) != 1 {
		t.Fatalf("due_date_changed count=%d want 1", len(changed))
	}
	if changed[0].Metadata["from"] != "2026-01-15" {
		t.Errorf("from=%v want 2026-01-15", changed[0].Metadata["from"])
	}
	if changed[0].Metadata["to"] != "2026-09-21" {
		t.Errorf("to=%v want 2026-09-21", changed[0].Metadata["to"])
	}

	if _, err := UpdateTask(ctx, 1, taskID, UpdateTaskInput{DueDate: &next}); err != nil {
		t.Fatalf("same due date: %v", err)
	}
	if got := eventsOfType(t, taskID, 1, "due_date_changed"); len(got) != 1 {
		t.Fatalf("after no-op due_date_changed count=%d want 1", len(got))
	}

	if _, err := UpdateTask(ctx, 1, taskID, UpdateTaskInput{ClearDue: true}); err != nil {
		t.Fatalf("clear due date: %v", err)
	}
	cleared := eventsOfType(t, taskID, 1, "due_date_changed")
	if len(cleared) != 2 {
		t.Fatalf("after clear due_date_changed count=%d want 2", len(cleared))
	}
	if cleared[0].Metadata["from"] != "2026-09-21" {
		t.Errorf("cleared from=%v want 2026-09-21", cleared[0].Metadata["from"])
	}
	if _, ok := cleared[0].Metadata["to"]; ok {
		t.Errorf("cleared due date should omit to, got %v", cleared[0].Metadata["to"])
	}
}

func TestUpdateTaskLogsEstimateChanged(t *testing.T) {
	ctx := context.Background()
	proj, err := CreateProject(ctx, 1, "Estimate Activity Board", "")
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	if _, err := SetProjectWorkflowMode(ctx, 1, proj.ID, storage.WorkflowKanban); err != nil {
		t.Fatalf("enable kanban: %v", err)
	}

	pid := proj.ID
	taskID, err := CreateTask(ctx, 1, CreateTaskInput{Title: "Estimate activity", ProjectID: &pid})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	five := 5
	if _, err := UpdateTask(ctx, 1, taskID, UpdateTaskInput{EstimatePoints: ptrToIntPtr(five)}); err != nil {
		t.Fatalf("set estimate: %v", err)
	}
	changed := eventsOfType(t, taskID, 1, "estimate_changed")
	if len(changed) != 1 {
		t.Fatalf("estimate_changed count=%d want 1", len(changed))
	}
	if _, ok := changed[0].Metadata["from"]; ok {
		t.Errorf("initial estimate should omit from, got %v", changed[0].Metadata["from"])
	}
	if eventMetaInt(changed[0], "to") != 5 {
		t.Errorf("to=%v want 5", changed[0].Metadata["to"])
	}

	if _, err := UpdateTask(ctx, 1, taskID, UpdateTaskInput{EstimatePoints: ptrToIntPtr(five)}); err != nil {
		t.Fatalf("same estimate: %v", err)
	}
	if got := eventsOfType(t, taskID, 1, "estimate_changed"); len(got) != 1 {
		t.Fatalf("after no-op estimate_changed count=%d want 1", len(got))
	}

	var clear *int
	if _, err := UpdateTask(ctx, 1, taskID, UpdateTaskInput{EstimatePoints: &clear}); err != nil {
		t.Fatalf("clear estimate: %v", err)
	}
	cleared := eventsOfType(t, taskID, 1, "estimate_changed")
	if len(cleared) != 2 {
		t.Fatalf("after clear estimate_changed count=%d want 2", len(cleared))
	}
	if eventMetaInt(cleared[0], "from") != 5 {
		t.Errorf("cleared from=%v want 5", cleared[0].Metadata["from"])
	}
	if _, ok := cleared[0].Metadata["to"]; ok {
		t.Errorf("cleared estimate should omit to, got %v", cleared[0].Metadata["to"])
	}
}

func TestUpdateTaskLogsParentChanged(t *testing.T) {
	ctx := context.Background()
	parentID, err := CreateTask(ctx, 1, CreateTaskInput{Title: "Parent task"})
	if err != nil {
		t.Fatalf("create parent: %v", err)
	}
	otherID, err := CreateTask(ctx, 1, CreateTaskInput{Title: "Other parent"})
	if err != nil {
		t.Fatalf("create other parent: %v", err)
	}
	childID, err := CreateTask(ctx, 1, CreateTaskInput{Title: "Child task"})
	if err != nil {
		t.Fatalf("create child: %v", err)
	}

	if _, err := UpdateTask(ctx, 1, childID, UpdateTaskInput{ParentID: ptrToIntPtr(parentID)}); err != nil {
		t.Fatalf("nest under parent: %v", err)
	}
	changed := eventsOfType(t, childID, 1, "parent_changed")
	if len(changed) != 1 {
		t.Fatalf("parent_changed count=%d want 1", len(changed))
	}
	if changed[0].Metadata["to"] != "Parent task" {
		t.Errorf("to=%v want Parent task", changed[0].Metadata["to"])
	}
	if eventMetaInt(changed[0], "to_id") != parentID {
		t.Errorf("to_id=%d want %d", eventMetaInt(changed[0], "to_id"), parentID)
	}
	if _, ok := changed[0].Metadata["from"]; ok {
		t.Errorf("initial parent should omit from, got %v", changed[0].Metadata["from"])
	}

	if _, err := UpdateTask(ctx, 1, childID, UpdateTaskInput{ParentID: ptrToIntPtr(parentID)}); err != nil {
		t.Fatalf("same parent: %v", err)
	}
	if got := eventsOfType(t, childID, 1, "parent_changed"); len(got) != 1 {
		t.Fatalf("after no-op parent_changed count=%d want 1", len(got))
	}

	if _, err := UpdateTask(ctx, 1, childID, UpdateTaskInput{ParentID: ptrToIntPtr(otherID)}); err != nil {
		t.Fatalf("move parent: %v", err)
	}
	moved := eventsOfType(t, childID, 1, "parent_changed")
	if len(moved) != 2 {
		t.Fatalf("after move parent_changed count=%d want 2", len(moved))
	}
	if moved[0].Metadata["from"] != "Parent task" {
		t.Errorf("from=%v want Parent task", moved[0].Metadata["from"])
	}
	if eventMetaInt(moved[0], "from_id") != parentID {
		t.Errorf("from_id=%d want %d", eventMetaInt(moved[0], "from_id"), parentID)
	}
	if moved[0].Metadata["to"] != "Other parent" {
		t.Errorf("to=%v want Other parent", moved[0].Metadata["to"])
	}
	if eventMetaInt(moved[0], "to_id") != otherID {
		t.Errorf("to_id=%d want %d", eventMetaInt(moved[0], "to_id"), otherID)
	}

	var clear *int
	if _, err := UpdateTask(ctx, 1, childID, UpdateTaskInput{ParentID: &clear}); err != nil {
		t.Fatalf("clear parent: %v", err)
	}
	cleared := eventsOfType(t, childID, 1, "parent_changed")
	if len(cleared) != 3 {
		t.Fatalf("after clear parent_changed count=%d want 3", len(cleared))
	}
	if cleared[0].Metadata["from"] != "Other parent" {
		t.Errorf("cleared from=%v want Other parent", cleared[0].Metadata["from"])
	}
	if _, ok := cleared[0].Metadata["to"]; ok {
		t.Errorf("cleared parent should omit to, got %v", cleared[0].Metadata["to"])
	}
}

func TestUpdateTaskUnrelatedFieldDoesNotLogTrackedChanges(t *testing.T) {
	ctx := context.Background()
	taskID, err := CreateTask(ctx, 1, CreateTaskInput{Title: "Description only", Priority: 2, DueDate: "2026-03-01"})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	desc := "updated notes"
	if _, err := UpdateTask(ctx, 1, taskID, UpdateTaskInput{Description: &desc}); err != nil {
		t.Fatalf("description: %v", err)
	}
	for _, eventType := range []string{"title_changed", "priority_changed", "due_date_changed", "estimate_changed", "parent_changed"} {
		if n := len(eventsOfType(t, taskID, 1, eventType)); n != 0 {
			t.Errorf("%s count=%d want 0 after description-only edit", eventType, n)
		}
	}
}

func eventMetaInt(ev storage.TaskEvent, key string) int {
	switch v := ev.Metadata[key].(type) {
	case float64:
		return int(v)
	case int:
		return v
	case int64:
		return int(v)
	default:
		return 0
	}
}
