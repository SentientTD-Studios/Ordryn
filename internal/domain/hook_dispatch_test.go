package domain

import (
	"context"
	"testing"
	"time"

	"GoTodo/internal/hooks"
	"GoTodo/internal/live"
	"GoTodo/internal/storage"
)

func listenHookTypes(t *testing.T) *[]string {
	t.Helper()
	var got []string
	stop := live.ListenHooks(func(ev hooks.Event) {
		got = append(got, ev.Type)
	})
	t.Cleanup(stop)
	return &got
}

func hasHookType(got []string, want string) bool {
	for _, typ := range got {
		if typ == want {
			return true
		}
	}
	return false
}

func TestCommentMentionHook(t *testing.T) {
	ctx := context.Background()
	setTestUsername(t, 2, "bob_editor")
	proj, err := CreateProject(ctx, 1, "Mention Hook Proj", "")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := storage.UpsertProjectMember(proj.ID, 2, storage.RoleEditor); err != nil {
		t.Fatal(err)
	}
	pid := proj.ID
	taskID, err := CreateTask(ctx, 1, CreateTaskInput{Title: "Ping Bob", ProjectID: &pid})
	if err != nil {
		t.Fatal(err)
	}

	plain := listenHookTypes(t)
	if _, err := AddCommentForUser(ctx, 1, taskID, "No one tagged"); err != nil {
		t.Fatal(err)
	}
	if hasHookType(*plain, live.TypeTaskMentioned) {
		t.Fatal("plain comment should not dispatch task.mentioned")
	}

	mentioned := listenHookTypes(t)
	if _, err := AddCommentForUser(ctx, 1, taskID, "Please look @bob_editor"); err != nil {
		t.Fatal(err)
	}
	if !hasHookType(*mentioned, live.TypeTaskMentioned) {
		t.Fatalf("expected task.mentioned, got %v", *mentioned)
	}
	if !hasHookType(*mentioned, live.TypeTaskCommented) {
		t.Fatalf("expected task.commented alongside mention, got %v", *mentioned)
	}
}

func TestCompletedReopenedHooks(t *testing.T) {
	ctx := context.Background()
	proj, err := CreateProject(ctx, 1, "Complete Hook Proj", "")
	if err != nil {
		t.Fatal(err)
	}
	pid := proj.ID
	taskID, err := CreateTask(ctx, 1, CreateTaskInput{Title: "Finish me", ProjectID: &pid})
	if err != nil {
		t.Fatal(err)
	}

	done := true
	first := listenHookTypes(t)
	if _, err := UpdateTask(ctx, 1, taskID, UpdateTaskInput{Completed: &done}); err != nil {
		t.Fatal(err)
	}
	if !hasHookType(*first, live.TypeTaskCompleted) {
		t.Fatalf("expected task.completed, got %v", *first)
	}

	noop := listenHookTypes(t)
	if err := SetTaskCompleted(ctx, 1, taskID, true); err != nil {
		t.Fatal(err)
	}
	if hasHookType(*noop, live.TypeTaskCompleted) || hasHookType(*noop, live.TypeTaskReopened) {
		t.Fatalf("no-op complete should not dispatch completed/reopened, got %v", *noop)
	}

	reopen := listenHookTypes(t)
	if err := SetTaskCompleted(ctx, 1, taskID, false); err != nil {
		t.Fatal(err)
	}
	if !hasHookType(*reopen, live.TypeTaskReopened) {
		t.Fatalf("expected task.reopened, got %v", *reopen)
	}
}

func TestDueSoonHookQuery(t *testing.T) {
	ctx := context.Background()
	proj, err := CreateProject(ctx, 1, "Due Soon Proj", "")
	if err != nil {
		t.Fatal(err)
	}
	pid := proj.ID
	tomorrowID, err := CreateTask(ctx, 1, CreateTaskInput{Title: "Due tomorrow", ProjectID: &pid})
	if err != nil {
		t.Fatal(err)
	}
	todayID, err := CreateTask(ctx, 1, CreateTaskInput{Title: "Due today", ProjectID: &pid})
	if err != nil {
		t.Fatal(err)
	}
	overdueID, err := CreateTask(ctx, 1, CreateTaskInput{Title: "Already overdue", ProjectID: &pid})
	if err != nil {
		t.Fatal(err)
	}
	doneID, err := CreateTask(ctx, 1, CreateTaskInput{Title: "Done tomorrow", ProjectID: &pid, Completed: true})
	if err != nil {
		t.Fatal(err)
	}
	inboxID, err := CreateTask(ctx, 1, CreateTaskInput{Title: "Inbox tomorrow"})
	if err != nil {
		t.Fatal(err)
	}

	pool, err := storage.OpenDatabase()
	if err != nil {
		t.Fatal(err)
	}
	defer storage.CloseDatabase(pool)
	for id, days := range map[int]int{tomorrowID: 1, todayID: 0, overdueID: -1, doneID: 1, inboxID: 1} {
		if _, err := pool.Exec(ctx, `UPDATE tasks SET due_date = CURRENT_DATE + $1::integer WHERE id = $2`, days, id); err != nil {
			t.Fatalf("set due %d: %v", id, err)
		}
	}

	ids, err := storage.ListDueSoonHookTasks(200)
	if err != nil {
		t.Fatal(err)
	}
	if !containsID(ids, tomorrowID) {
		t.Fatalf("expected tomorrow task in due-soon list, got %v", ids)
	}
	for _, id := range []int{todayID, overdueID, doneID, inboxID} {
		if containsID(ids, id) {
			t.Fatalf("task %d should not be due-soon, got %v", id, ids)
		}
	}
	if err := storage.MarkDueSoonHookSent(tomorrowID); err != nil {
		t.Fatal(err)
	}
	ids, err = storage.ListDueSoonHookTasks(200)
	if err != nil {
		t.Fatal(err)
	}
	if containsID(ids, tomorrowID) {
		t.Fatal("marked due-soon task should not list again")
	}
}

func containsID(ids []int, want int) bool {
	for _, id := range ids {
		if id == want {
			return true
		}
	}
	return false
}

func TestProjectMemberJoinLeaveHooks(t *testing.T) {
	ctx := context.Background()
	setTestUsername(t, 2, "bob_editor")
	proj, err := CreateProject(ctx, 1, "Member Hook Proj", "")
	if err != nil {
		t.Fatal(err)
	}
	inv, err := storage.CreateProjectInvite(proj.ID, "editor@example.com", storage.RoleEditor, 1, time.Now().Add(24*time.Hour))
	if err != nil {
		t.Fatalf("invite: %v", err)
	}

	joined := listenHookTypes(t)
	if err := AcceptProjectInvite(ctx, 2, "editor@example.com", inv.ID); err != nil {
		t.Fatalf("accept: %v", err)
	}
	if !hasHookType(*joined, live.TypeProjectMemberJoined) {
		t.Fatalf("expected member_joined, got %v", *joined)
	}

	left := listenHookTypes(t)
	if err := RemoveProjectMember(ctx, 1, proj.ID, 2); err != nil {
		t.Fatalf("remove: %v", err)
	}
	if !hasHookType(*left, live.TypeProjectMemberLeft) {
		t.Fatalf("expected member_left, got %v", *left)
	}
}

func TestArchiveAndRestoreHooks(t *testing.T) {
	ctx := context.Background()
	proj, err := CreateProject(ctx, 1, "Archive Hook Proj", "")
	if err != nil {
		t.Fatal(err)
	}
	pid := proj.ID
	taskID, err := CreateTask(ctx, 1, CreateTaskInput{Title: "Archive me", ProjectID: &pid})
	if err != nil {
		t.Fatal(err)
	}

	archived := listenHookTypes(t)
	if err := ArchiveTask(ctx, 1, taskID); err != nil {
		t.Fatal(err)
	}
	if !hasHookType(*archived, live.TypeTaskArchived) {
		t.Fatalf("expected task.archived, got %v", *archived)
	}

	restored := listenHookTypes(t)
	if err := RestoreTask(ctx, 1, taskID); err != nil {
		t.Fatal(err)
	}
	if !hasHookType(*restored, live.TypeTaskRestored) {
		t.Fatalf("expected task.restored, got %v", *restored)
	}

	projArchived := listenHookTypes(t)
	if _, err := ArchiveProject(ctx, 1, proj.ID); err != nil {
		t.Fatal(err)
	}
	if !hasHookType(*projArchived, live.TypeProjectArchived) {
		t.Fatalf("expected project.archived, got %v", *projArchived)
	}

	projRestored := listenHookTypes(t)
	if _, err := RestoreProject(ctx, 1, proj.ID); err != nil {
		t.Fatal(err)
	}
	if !hasHookType(*projRestored, live.TypeProjectRestored) {
		t.Fatalf("expected project.restored, got %v", *projRestored)
	}
}

func TestMovedSplitHooks(t *testing.T) {
	ctx := context.Background()
	src, err := CreateProject(ctx, 1, "Move Src", "")
	if err != nil {
		t.Fatal(err)
	}
	dst, err := CreateProject(ctx, 1, "Move Dst", "")
	if err != nil {
		t.Fatal(err)
	}
	pid := src.ID
	taskID, err := CreateTask(ctx, 1, CreateTaskInput{Title: "Move me", ProjectID: &pid})
	if err != nil {
		t.Fatal(err)
	}
	moved := listenHookTypes(t)
	if _, err := UpdateTask(ctx, 1, taskID, UpdateTaskInput{ProjectID: ptrToIntPtr(dst.ID)}); err != nil {
		t.Fatal(err)
	}
	if !hasHookType(*moved, live.TypeTaskMoved) || !hasHookType(*moved, live.TypeTaskProjectChanged) {
		t.Fatalf("expected moved+project_changed, got %v", *moved)
	}
	if hasHookType(*moved, live.TypeTaskSprintChanged) {
		t.Fatalf("project move should not fire sprint_changed, got %v", *moved)
	}
}

func TestSprintLifecycleHooks(t *testing.T) {
	ctx := context.Background()
	proj, err := CreateProject(ctx, 1, "Sprint Hook Proj", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := SetProjectWorkflowMode(ctx, 1, proj.ID, storage.WorkflowKanban); err != nil {
		t.Fatal(err)
	}
	today := time.Now().UTC().Format("2006-01-02")
	end := time.Now().UTC().Add(7 * 24 * time.Hour).Format("2006-01-02")
	created := listenHookTypes(t)
	s, err := CreateProjectSprintForUser(ctx, 1, proj.ID, CreateProjectSprintInput{
		Name: "Hook Sprint", StartDate: today, EndDate: end,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !hasHookType(*created, live.TypeSprintCreated) {
		t.Fatalf("expected sprint.created, got %v", *created)
	}
	if !hasHookType(*created, live.TypeSprintStarted) {
		t.Fatalf("expected sprint.started for start=today, got %v", *created)
	}

	pid := proj.ID
	taskID, err := CreateTask(ctx, 1, CreateTaskInput{Title: "Sprint task", ProjectID: &pid})
	if err != nil {
		t.Fatal(err)
	}
	sprintChanged := listenHookTypes(t)
	if _, err := UpdateTask(ctx, 1, taskID, UpdateTaskInput{SprintID: ptrToIntPtr(s.ID)}); err != nil {
		t.Fatal(err)
	}
	if !hasHookType(*sprintChanged, live.TypeTaskSprintChanged) || !hasHookType(*sprintChanged, live.TypeTaskMoved) {
		t.Fatalf("expected sprint_changed+moved, got %v", *sprintChanged)
	}
}
