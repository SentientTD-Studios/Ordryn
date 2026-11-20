package domain

import (
	"context"
	"errors"
	"testing"

	"GoTodo/internal/storage"
	"GoTodo/internal/tasks"
)

func TestClaimTakeoverAndUnclaim(t *testing.T) {
	ctx := context.Background()
	proj, err := CreateProject(ctx, 1, "Claim Board", "")
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	if _, err := SetProjectWorkflowMode(ctx, 1, proj.ID, storage.WorkflowKanban); err != nil {
		t.Fatalf("enable kanban: %v", err)
	}
	if err := upsertTestMember(t, proj.ID, 2, testRoleEditor); err != nil {
		t.Fatalf("add editor: %v", err)
	}

	pid := proj.ID
	taskID, err := CreateTask(ctx, 1, CreateTaskInput{Title: "Unowned", ProjectID: &pid})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	fields, err := storage.GetWorkflowFieldsForTasks([]int{taskID})
	if err != nil {
		t.Fatal(err)
	}
	if fields[taskID].ClaimedBy != 0 {
		t.Fatalf("expected no auto-claim, got %d", fields[taskID].ClaimedBy)
	}

	if err := ClaimTaskForUser(ctx, 1, taskID, ClaimOptions{}); err != nil {
		t.Fatalf("claim: %v", err)
	}
	fields, _ = storage.GetWorkflowFieldsForTasks([]int{taskID})
	if fields[taskID].ClaimedBy != 1 {
		t.Fatalf("claimed_by=%d want 1", fields[taskID].ClaimedBy)
	}

	// Claiming a task someone else holds needs an explicit takeover.
	err = ClaimTaskForUser(ctx, 2, taskID, ClaimOptions{})
	var cc *ClaimConflictError
	if !errors.As(err, &cc) || !errors.Is(err, ErrConflict) || cc.ClaimedBy != 1 {
		t.Fatalf("claim without takeover: err=%v want claim conflict naming user 1", err)
	}

	// A takeover confirmed against the wrong holder is refused.
	wrong := 3
	if err := ClaimTaskForUser(ctx, 2, taskID, ClaimOptions{TakeOver: true, ExpectedClaimedBy: &wrong}); !errors.Is(err, ErrConflict) {
		t.Fatalf("stale takeover: err=%v want conflict", err)
	}

	// Editor can take over.
	holder := 1
	if err := ClaimTaskForUser(ctx, 2, taskID, ClaimOptions{TakeOver: true, ExpectedClaimedBy: &holder}); err != nil {
		t.Fatalf("takeover: %v", err)
	}
	fields, _ = storage.GetWorkflowFieldsForTasks([]int{taskID})
	if fields[taskID].ClaimedBy != 2 {
		t.Fatalf("claimed_by=%d want 2", fields[taskID].ClaimedBy)
	}

	// A release from a view that still shows user 1 holding it is refused.
	if err := UnclaimTaskForUser(ctx, 2, taskID, UnclaimOptions{ExpectedClaimedBy: &holder}); !errors.Is(err, ErrConflict) {
		t.Fatalf("stale release: err=%v want conflict", err)
	}

	if err := UnclaimTaskForUser(ctx, 2, taskID, UnclaimOptions{}); err != nil {
		t.Fatalf("unclaim: %v", err)
	}
	fields, _ = storage.GetWorkflowFieldsForTasks([]int{taskID})
	if fields[taskID].ClaimedBy != 0 {
		t.Fatalf("expected released, got %d", fields[taskID].ClaimedBy)
	}
}

func TestRepeatClaimAndReleaseAreNoOps(t *testing.T) {
	ctx := context.Background()
	proj, err := CreateProject(ctx, 1, "Repeat Claim Board", "")
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	if _, err := SetProjectWorkflowMode(ctx, 1, proj.ID, storage.WorkflowKanban); err != nil {
		t.Fatalf("enable kanban: %v", err)
	}
	pid := proj.ID
	taskID, err := CreateTask(ctx, 1, CreateTaskInput{Title: "Repeat", ProjectID: &pid})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	countEvents := func(kind string) int {
		events, err := storage.GetEventsForTask(taskID, 1, 100)
		if err != nil {
			t.Fatalf("events: %v", err)
		}
		n := 0
		for _, e := range events {
			if e.EventType == kind {
				n++
			}
		}
		return n
	}

	for i := 0; i < 2; i++ {
		if err := ClaimTaskForUser(ctx, 1, taskID, ClaimOptions{}); err != nil {
			t.Fatalf("claim %d: %v", i, err)
		}
	}
	if n := countEvents("claimed"); n != 1 {
		t.Fatalf("claimed events=%d want 1 (repeat claim must not log again)", n)
	}
	for i := 0; i < 2; i++ {
		if err := UnclaimTaskForUser(ctx, 1, taskID, UnclaimOptions{}); err != nil {
			t.Fatalf("unclaim %d: %v", i, err)
		}
	}
	if n := countEvents("unclaimed"); n != 1 {
		t.Fatalf("unclaimed events=%d want 1 (repeat release must not log again)", n)
	}
}

func TestClaimRequiresKanban(t *testing.T) {
	ctx := context.Background()
	proj, err := CreateProject(ctx, 1, "Classic Claim", "")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	pid := proj.ID
	taskID, err := CreateTask(ctx, 1, CreateTaskInput{Title: "Classic", ProjectID: &pid})
	if err != nil {
		t.Fatalf("create task: %v", err)
	}
	err = ClaimTaskForUser(ctx, 1, taskID, ClaimOptions{})
	if !errors.Is(err, ErrValidation) {
		t.Fatalf("err=%v want validation", err)
	}
}

func TestWorkflowClaimScopeMineHidesUnclaimed(t *testing.T) {
	ctx := context.Background()
	proj, err := CreateProject(ctx, 1, "Filter Board", "")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, err := SetProjectWorkflowMode(ctx, 1, proj.ID, storage.WorkflowKanban); err != nil {
		t.Fatalf("enable: %v", err)
	}
	pid := proj.ID
	unclaimedID, err := CreateTask(ctx, 1, CreateTaskInput{Title: "Unclaimed filter", ProjectID: &pid})
	if err != nil {
		t.Fatalf("create unclaimed: %v", err)
	}
	claimedID, err := CreateTask(ctx, 1, CreateTaskInput{Title: "Claimed filter", ProjectID: &pid})
	if err != nil {
		t.Fatalf("create claimed: %v", err)
	}
	if err := ClaimTaskForUser(ctx, 1, claimedID, ClaimOptions{}); err != nil {
		t.Fatalf("claim: %v", err)
	}

	uid := 1
	mine := tasks.ListFilters{
		ProjectFilter:      &pid,
		WorkflowClaimScope: "mine",
	}
	list, _, err := tasks.ReturnPaginationForUserWithFilters(1, 50, &uid, "UTC", mine)
	if err != nil {
		t.Fatalf("list mine: %v", err)
	}
	ids := map[int]bool{}
	for _, tsk := range list {
		ids[tsk.ID] = true
	}
	if ids[unclaimedID] {
		t.Fatal("unclaimed kanban task should be hidden in mine scope")
	}
	if !ids[claimedID] {
		t.Fatal("claimed task should appear in mine scope")
	}

	all := tasks.ListFilters{
		ProjectFilter:      &pid,
		WorkflowClaimScope: "all",
	}
	listAll, _, err := tasks.ReturnPaginationForUserWithFilters(1, 50, &uid, "UTC", all)
	if err != nil {
		t.Fatalf("list all: %v", err)
	}
	idsAll := map[int]bool{}
	for _, tsk := range listAll {
		idsAll[tsk.ID] = true
	}
	if !idsAll[unclaimedID] || !idsAll[claimedID] {
		t.Fatal("all scope should include claimed and unclaimed")
	}
}

func TestWorkflowClaimScopeMineHidesCompletedKanban(t *testing.T) {
	ctx := context.Background()
	proj, err := CreateProject(ctx, 1, "Done List Board", "")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, err := SetProjectWorkflowMode(ctx, 1, proj.ID, storage.WorkflowKanban); err != nil {
		t.Fatalf("enable: %v", err)
	}
	pid := proj.ID
	openID, err := CreateTask(ctx, 1, CreateTaskInput{Title: "Open claimed", ProjectID: &pid})
	if err != nil {
		t.Fatalf("create open: %v", err)
	}
	doneID, err := CreateTask(ctx, 1, CreateTaskInput{Title: "Done claimed", ProjectID: &pid})
	if err != nil {
		t.Fatalf("create done: %v", err)
	}
	if err := ClaimTaskForUser(ctx, 1, openID, ClaimOptions{}); err != nil {
		t.Fatalf("claim open: %v", err)
	}
	if err := ClaimTaskForUser(ctx, 1, doneID, ClaimOptions{}); err != nil {
		t.Fatalf("claim done: %v", err)
	}
	if err := SetTaskCompleted(ctx, 1, doneID, true); err != nil {
		t.Fatalf("complete: %v", err)
	}

	uid := 1
	mine := tasks.ListFilters{
		ProjectFilter:      &pid,
		WorkflowClaimScope: "mine",
	}
	list, _, err := tasks.ReturnPaginationForUserWithFilters(1, 50, &uid, "UTC", mine)
	if err != nil {
		t.Fatalf("list mine: %v", err)
	}
	ids := map[int]bool{}
	for _, tsk := range list {
		ids[tsk.ID] = true
	}
	if !ids[openID] {
		t.Fatal("open claimed task should appear in mine scope")
	}
	if ids[doneID] {
		t.Fatal("completed kanban task should be hidden from mine list by default")
	}

	mineDone := tasks.ListFilters{
		ProjectFilter:      &pid,
		WorkflowClaimScope: "mine",
		StatusFilter:       "complete",
	}
	listDone, _, err := tasks.ReturnPaginationForUserWithFilters(1, 50, &uid, "UTC", mineDone)
	if err != nil {
		t.Fatalf("list mine complete: %v", err)
	}
	idsDone := map[int]bool{}
	for _, tsk := range listDone {
		idsDone[tsk.ID] = true
	}
	if !idsDone[doneID] {
		t.Fatal("completed claimed task should appear when status=complete")
	}
	if idsDone[openID] {
		t.Fatal("open task should not appear in status=complete")
	}

	// Board/all scope still includes completed work.
	all := tasks.ListFilters{
		ProjectFilter:      &pid,
		WorkflowClaimScope: "all",
	}
	listAll, _, err := tasks.ReturnPaginationForUserWithFilters(1, 50, &uid, "UTC", all)
	if err != nil {
		t.Fatalf("list all: %v", err)
	}
	idsAll := map[int]bool{}
	for _, tsk := range listAll {
		idsAll[tsk.ID] = true
	}
	if !idsAll[doneID] || !idsAll[openID] {
		t.Fatal("all scope should keep completed kanban tasks for the board")
	}
}

func TestKanbanCreateNotifiesOtherMembers(t *testing.T) {
	ctx := context.Background()
	proj, err := CreateProject(ctx, 1, "Notify Board", "")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, err := SetProjectWorkflowMode(ctx, 1, proj.ID, storage.WorkflowKanban); err != nil {
		t.Fatalf("enable: %v", err)
	}
	if err := upsertTestMember(t, proj.ID, 2, testRoleEditor); err != nil {
		t.Fatalf("add editor: %v", err)
	}

	pid := proj.ID
	taskID, err := CreateTask(ctx, 1, CreateTaskInput{Title: "Fresh work", ProjectID: &pid})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	forOwner, totalOwner, err := storage.ListUserNotifications(1, 20, 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, n := range forOwner {
		if n.TaskID == taskID {
			t.Fatal("creator should not receive own task_created notification")
		}
	}
	_ = totalOwner

	forEditor, _, err := storage.ListUserNotifications(2, 20, 0)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, n := range forEditor {
		if n.TaskID == taskID && n.Type == storage.NotificationTaskCreated {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("editor should receive task_created notification")
	}

	unread, err := UnreadNotificationCountForUser(ctx, 2)
	if err != nil {
		t.Fatal(err)
	}
	if unread < 1 {
		t.Fatalf("unread=%d", unread)
	}
}
