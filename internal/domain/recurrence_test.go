package domain

import (
	"context"
	"errors"
	"testing"
	"time"

	"GoTodo/internal/storage"
)

// withRecurrenceClock pins "today" for recurrence math (Monday 2026-09-28, UTC).
func withRecurrenceClock(t *testing.T) {
	t.Helper()
	prev := recurrenceNow
	recurrenceNow = func() time.Time { return time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC) }
	t.Cleanup(func() { recurrenceNow = prev })
}

func weeklyInput() *RecurrenceInput {
	return &RecurrenceInput{Frequency: "weekly", Interval: 1}
}

func mustCreateTask(t *testing.T, userID int, in CreateTaskInput) int {
	t.Helper()
	id, err := CreateTask(context.Background(), userID, in)
	if err != nil {
		t.Fatalf("create task %q: %v", in.Title, err)
	}
	return id
}

func mustSuccessor(t *testing.T, taskID int) int {
	t.Helper()
	succ, err := storage.FindRecurrenceSuccessor(taskID, RecurrenceUndoWindow)
	if err != nil {
		t.Fatalf("find successor of %d: %v", taskID, err)
	}
	if succ == nil {
		t.Fatalf("expected a successor for task %d", taskID)
	}
	return succ.TaskID
}

func taskRow(t *testing.T, id int) (title, due string, completed bool, priority int, parentID int) {
	t.Helper()
	pool, err := storage.OpenDatabase()
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer storage.CloseDatabase(pool)
	var parent *int
	if err := pool.QueryRow(context.Background(),
		`SELECT title, COALESCE(CAST(due_date AS TEXT), ''), COALESCE(completed,false), COALESCE(priority,0), parent_id
		 FROM tasks WHERE id = $1`, id).Scan(&title, &due, &completed, &priority, &parent); err != nil {
		t.Fatalf("load task %d: %v", id, err)
	}
	if parent != nil {
		parentID = *parent
	}
	return
}

func taskExists(t *testing.T, id int) bool {
	t.Helper()
	pool, err := storage.OpenDatabase()
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer storage.CloseDatabase(pool)
	var n int
	_ = pool.QueryRow(context.Background(), `SELECT COUNT(*) FROM tasks WHERE id = $1`, id).Scan(&n)
	return n > 0
}

func eventTypes(t *testing.T, taskID int) map[string]int {
	t.Helper()
	events, err := storage.GetEventsForTask(taskID, 1, 100)
	if err != nil {
		t.Fatalf("events for %d: %v", taskID, err)
	}
	out := map[string]int{}
	for _, e := range events {
		out[e.EventType]++
	}
	return out
}

func TestRecurringTaskSpawnsNextOccurrenceOnComplete(t *testing.T) {
	withRecurrenceClock(t)
	ctx := context.Background()

	tag, err := CreateTag(ctx, 1, "recurring-chores", nil)
	if err != nil {
		t.Fatalf("create tag: %v", err)
	}
	taskID := mustCreateTask(t, 1, CreateTaskInput{
		Title: "Take out trash", Description: "bins to curb", DueDate: "2026-09-28",
		Priority: 2, TagIDs: []int{tag.ID},
	})
	parent := taskID
	childID := mustCreateTask(t, 1, CreateTaskInput{Title: "Recycling too", ParentID: &parent})
	if err := SetTaskCompleted(ctx, 1, childID, true); err != nil {
		t.Fatalf("complete child: %v", err)
	}

	rec, err := SetTaskRecurrence(ctx, 1, taskID, weeklyInput())
	if err != nil {
		t.Fatalf("set recurrence: %v", err)
	}
	if rec == nil || rec.SeriesID != taskID || rec.Occurrence != 1 || rec.WeekdaysMask != 1<<1 {
		t.Fatalf("unexpected saved rule (weekday should pin to Monday): %+v", rec)
	}

	if err := SetTaskCompleted(ctx, 1, taskID, true); err != nil {
		t.Fatalf("complete: %v", err)
	}
	nextID := mustSuccessor(t, taskID)

	title, due, completed, priority, _ := taskRow(t, nextID)
	if title != "Take out trash" || due != "2026-10-05" || completed || priority != 2 {
		t.Fatalf("next occurrence: title=%q due=%q completed=%v priority=%d", title, due, completed, priority)
	}
	tags, err := storage.GetTagsForTask(nextID)
	if err != nil || len(tags) != 1 || tags[0].ID != tag.ID {
		t.Fatalf("tags not copied: %+v err=%v", tags, err)
	}
	children, err := ChildIDsOf(ctx, []int{nextID})
	if err != nil || len(children) != 1 {
		t.Fatalf("subtasks not copied: %v err=%v", children, err)
	}
	ctitle, _, ccompleted, _, cparent := taskRow(t, children[0])
	if ctitle != "Recycling too" || ccompleted || cparent != nextID {
		t.Fatalf("copied subtask: title=%q completed=%v parent=%d", ctitle, ccompleted, cparent)
	}

	if old, _ := storage.GetTaskRecurrence(taskID); old != nil {
		t.Fatalf("rule should move off the completed task, still has %+v", old)
	}
	moved, err := storage.GetTaskRecurrence(nextID)
	if err != nil || moved == nil || moved.Occurrence != 2 || moved.SeriesID != taskID {
		t.Fatalf("rule on next occurrence: %+v err=%v", moved, err)
	}
	series, prev, err := storage.GetTaskRecurrenceLinks(nextID)
	if err != nil || series != taskID || prev != taskID {
		t.Fatalf("links: series=%d prev=%d err=%v", series, prev, err)
	}
	if ev := eventTypes(t, taskID); ev["recurrence_next"] != 1 {
		t.Fatalf("expected recurrence_next on completed task, got %v", ev)
	}
	if ev := eventTypes(t, nextID); ev["recurrence_created"] != 1 {
		t.Fatalf("expected recurrence_created on new task, got %v", ev)
	}

	// Completing the old task again (no transition) must not spawn another.
	if err := SetTaskCompleted(ctx, 1, taskID, true); err != nil {
		t.Fatalf("re-complete: %v", err)
	}
	if again := mustSuccessor(t, taskID); again != nextID {
		t.Fatalf("duplicate spawn: %d then %d", nextID, again)
	}

	detail, err := GetTaskRecurrenceDetail(ctx, 1, nextID)
	if err != nil {
		t.Fatalf("detail: %v", err)
	}
	if detail.Rule == nil || detail.PrevTaskID != taskID || detail.SeriesID != taskID || len(detail.History) != 2 {
		t.Fatalf("detail: rule=%v prev=%d series=%d history=%d", detail.Rule, detail.PrevTaskID, detail.SeriesID, len(detail.History))
	}
	if detail.Rule.NextDue != "2026-10-12" || detail.Rule.Summary != "Weekly on Mon" {
		t.Fatalf("detail preview: next=%q summary=%q", detail.Rule.NextDue, detail.Rule.Summary)
	}
}

func TestReopeningRecurringTaskUndoesFreshSpawn(t *testing.T) {
	withRecurrenceClock(t)
	ctx := context.Background()
	taskID := mustCreateTask(t, 1, CreateTaskInput{Title: "Daily standup notes", DueDate: "2026-09-28"})
	if _, err := SetTaskRecurrence(ctx, 1, taskID, &RecurrenceInput{Frequency: "daily"}); err != nil {
		t.Fatalf("set: %v", err)
	}
	if _, err := ToggleTaskCompleted(ctx, 1, taskID); err != nil {
		t.Fatalf("complete: %v", err)
	}
	spawned := mustSuccessor(t, taskID)

	if _, err := ToggleTaskCompleted(ctx, 1, taskID); err != nil {
		t.Fatalf("reopen: %v", err)
	}
	if taskExists(t, spawned) {
		t.Fatalf("fresh successor %d should be removed on reopen", spawned)
	}
	rec, err := storage.GetTaskRecurrence(taskID)
	if err != nil || rec == nil || rec.Occurrence != 1 {
		t.Fatalf("rule should return to reopened task: %+v err=%v", rec, err)
	}
	if ev := eventTypes(t, taskID); ev["recurrence_undone"] != 1 {
		t.Fatalf("expected recurrence_undone, got %v", ev)
	}

	if _, err := ToggleTaskCompleted(ctx, 1, taskID); err != nil {
		t.Fatalf("complete again: %v", err)
	}
	respawned := mustSuccessor(t, taskID)
	if respawned == spawned {
		t.Fatalf("expected a new successor id")
	}
	_, due, _, _, _ := taskRow(t, respawned)
	if due != "2026-09-29" {
		t.Fatalf("respawned due=%q", due)
	}
}

func TestReopenKeepsSuccessorThatHasProgress(t *testing.T) {
	withRecurrenceClock(t)
	ctx := context.Background()
	taskID := mustCreateTask(t, 1, CreateTaskInput{Title: "Weekly review", DueDate: "2026-09-28"})
	if _, err := SetTaskRecurrence(ctx, 1, taskID, weeklyInput()); err != nil {
		t.Fatalf("set: %v", err)
	}
	if err := SetTaskCompleted(ctx, 1, taskID, true); err != nil {
		t.Fatalf("complete: %v", err)
	}
	nextID := mustSuccessor(t, taskID)
	if err := SetTaskCompleted(ctx, 1, nextID, true); err != nil {
		t.Fatalf("complete successor: %v", err)
	}
	if err := SetTaskCompleted(ctx, 1, taskID, false); err != nil {
		t.Fatalf("reopen original: %v", err)
	}
	if !taskExists(t, nextID) {
		t.Fatalf("completed successor must not be deleted")
	}
	if rec, _ := storage.GetTaskRecurrence(taskID); rec != nil {
		t.Fatalf("rule must stay with the live series, found on reopened task: %+v", rec)
	}
}

func TestRecurrenceEndAfterStopsSeries(t *testing.T) {
	withRecurrenceClock(t)
	ctx := context.Background()
	taskID := mustCreateTask(t, 1, CreateTaskInput{Title: "Two-part course", DueDate: "2026-09-28"})
	if _, err := SetTaskRecurrence(ctx, 1, taskID, &RecurrenceInput{Frequency: "weekly", EndAfter: 2}); err != nil {
		t.Fatalf("set: %v", err)
	}
	if err := SetTaskCompleted(ctx, 1, taskID, true); err != nil {
		t.Fatalf("complete first: %v", err)
	}
	second := mustSuccessor(t, taskID)
	if err := SetTaskCompleted(ctx, 1, second, true); err != nil {
		t.Fatalf("complete second: %v", err)
	}
	if succ, _ := storage.FindRecurrenceSuccessor(second, RecurrenceUndoWindow); succ != nil {
		t.Fatalf("series should end after 2 occurrences, got successor %d", succ.TaskID)
	}
	if ev := eventTypes(t, second); ev["recurrence_ended"] != 1 {
		t.Fatalf("expected recurrence_ended, got %v", ev)
	}
}

func TestRecurrenceEndsOnStopsSeries(t *testing.T) {
	withRecurrenceClock(t)
	ctx := context.Background()
	taskID := mustCreateTask(t, 1, CreateTaskInput{Title: "Until October", DueDate: "2026-09-28"})
	if _, err := SetTaskRecurrence(ctx, 1, taskID, &RecurrenceInput{Frequency: "weekly", EndsOn: "2026-10-01"}); err != nil {
		t.Fatalf("set: %v", err)
	}
	if err := SetTaskCompleted(ctx, 1, taskID, true); err != nil {
		t.Fatalf("complete: %v", err)
	}
	if succ, _ := storage.FindRecurrenceSuccessor(taskID, RecurrenceUndoWindow); succ != nil {
		t.Fatalf("next date is after ends_on; expected no successor, got %d", succ.TaskID)
	}
}

func TestClearingRecurrenceStopsSpawning(t *testing.T) {
	withRecurrenceClock(t)
	ctx := context.Background()
	taskID := mustCreateTask(t, 1, CreateTaskInput{Title: "Stop repeating", DueDate: "2026-09-28"})
	if _, err := SetTaskRecurrence(ctx, 1, taskID, weeklyInput()); err != nil {
		t.Fatalf("set: %v", err)
	}
	if _, err := SetTaskRecurrence(ctx, 1, taskID, nil); err != nil {
		t.Fatalf("clear: %v", err)
	}
	if err := SetTaskCompleted(ctx, 1, taskID, true); err != nil {
		t.Fatalf("complete: %v", err)
	}
	if succ, _ := storage.FindRecurrenceSuccessor(taskID, RecurrenceUndoWindow); succ != nil {
		t.Fatalf("cleared rule should not spawn, got %d", succ.TaskID)
	}
	if ev := eventTypes(t, taskID); ev["recurrence_set"] != 1 || ev["recurrence_cleared"] != 1 {
		t.Fatalf("expected set+cleared events, got %v", ev)
	}
}

func TestRecurrenceValidationAndNesting(t *testing.T) {
	withRecurrenceClock(t)
	ctx := context.Background()
	rootID := mustCreateTask(t, 1, CreateTaskInput{Title: "Root"})
	parent := rootID
	childID := mustCreateTask(t, 1, CreateTaskInput{Title: "Child", ParentID: &parent})

	if _, err := SetTaskRecurrence(ctx, 1, childID, weeklyInput()); !errors.Is(err, ErrValidation) {
		t.Fatalf("subtask recurrence: err=%v want validation", err)
	}
	if _, err := SetTaskRecurrence(ctx, 1, rootID, &RecurrenceInput{Frequency: "hourly"}); !errors.Is(err, ErrValidation) {
		t.Fatalf("bad frequency: err=%v want validation", err)
	}
	if _, err := SetTaskRecurrence(ctx, 1, rootID, &RecurrenceInput{Frequency: "monthly", MonthDay: 40}); !errors.Is(err, ErrValidation) {
		t.Fatalf("bad month day: err=%v want validation", err)
	}

	// Monthly without a due date anchors to "today".
	monthly, err := SetTaskRecurrence(ctx, 1, rootID, &RecurrenceInput{Frequency: "monthly"})
	if err != nil || monthly.MonthDay != 28 {
		t.Fatalf("monthly anchor: %+v err=%v", monthly, err)
	}

	otherID := mustCreateTask(t, 1, CreateTaskInput{Title: "Will be nested", DueDate: "2026-09-30"})
	if _, err := SetTaskRecurrence(ctx, 1, otherID, weeklyInput()); err != nil {
		t.Fatalf("set on other: %v", err)
	}
	newParent := &rootID
	if _, err := UpdateTask(ctx, 1, otherID, UpdateTaskInput{ParentID: &newParent}); err != nil {
		t.Fatalf("nest: %v", err)
	}
	if rec, _ := storage.GetTaskRecurrence(otherID); rec != nil {
		t.Fatalf("nesting should drop the rule, found %+v", rec)
	}
}

func TestRecurrenceSpawnsFromKanbanDoneStatus(t *testing.T) {
	withRecurrenceClock(t)
	ctx := context.Background()
	proj, err := CreateProject(ctx, 1, "Recurring Board", "")
	if err != nil {
		t.Fatalf("project: %v", err)
	}
	if _, err := SetProjectWorkflowMode(ctx, 1, proj.ID, storage.WorkflowKanban); err != nil {
		t.Fatalf("kanban: %v", err)
	}
	pid := proj.ID
	estimate := 3
	taskID := mustCreateTask(t, 1, CreateTaskInput{Title: "Sprint hygiene", DueDate: "2026-09-28", ProjectID: &pid, EstimatePoints: &estimate})
	if _, err := SetTaskRecurrence(ctx, 1, taskID, &RecurrenceInput{Frequency: "daily", Interval: 2}); err != nil {
		t.Fatalf("set: %v", err)
	}
	done, err := storage.GetDoneProjectStatus(pid)
	if err != nil || done == nil {
		t.Fatalf("done status: %v", err)
	}
	doneID := done.ID
	donePtr := &doneID
	if _, err := UpdateTask(ctx, 1, taskID, UpdateTaskInput{StatusID: &donePtr}); err != nil {
		t.Fatalf("move to done: %v", err)
	}
	nextID := mustSuccessor(t, taskID)
	_, due, completed, _, _ := taskRow(t, nextID)
	if due != "2026-09-30" || completed {
		t.Fatalf("kanban successor: due=%q completed=%v", due, completed)
	}
	pool, err := storage.OpenDatabase()
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer storage.CloseDatabase(pool)
	var statusID, projectID int
	var points *int
	if err := pool.QueryRow(ctx, `SELECT COALESCE(status_id,0), COALESCE(project_id,0), estimate_points FROM tasks WHERE id = $1`, nextID).
		Scan(&statusID, &projectID, &points); err != nil {
		t.Fatalf("load successor: %v", err)
	}
	def, err := storage.GetDefaultProjectStatus(pid)
	if err != nil || def == nil {
		t.Fatalf("default status: %v", err)
	}
	if projectID != pid || statusID != def.ID || points == nil || *points != 3 {
		t.Fatalf("successor workflow: project=%d status=%d (default %d) points=%v", projectID, statusID, def.ID, points)
	}
}
