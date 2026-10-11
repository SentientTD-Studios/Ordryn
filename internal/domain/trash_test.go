package domain

import (
	"context"
	"errors"
	"testing"

	"GoTodo/internal/storage"
	"GoTodo/internal/tasks"
)

func trashEntryIDs(t *testing.T, userID int) map[int]storage.TrashedTask {
	t.Helper()
	entries, err := ListTrashForUser(context.Background(), userID)
	if err != nil {
		t.Fatalf("list trash: %v", err)
	}
	out := make(map[int]storage.TrashedTask, len(entries))
	for _, e := range entries {
		out[e.ID] = e.TrashedTask
	}
	return out
}

func listedTaskIDs(t *testing.T, userID, projectID int) map[int]bool {
	t.Helper()
	pid := projectID
	list, _, err := tasks.ReturnPaginationForUserWithFilters(1, 200, &userID, "UTC", tasks.ListFilters{ProjectFilter: &pid})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	out := map[int]bool{}
	for _, task := range list {
		out[task.ID] = true
		for _, c := range task.Children {
			out[c.ID] = true
		}
	}
	return out
}

func commentCount(t *testing.T, taskID int) int {
	t.Helper()
	pool, err := storage.OpenDatabase()
	if err != nil {
		t.Fatal(err)
	}
	defer storage.CloseDatabase(pool)
	var n int
	if err := pool.QueryRow(context.Background(),
		`SELECT COUNT(*) FROM task_comments WHERE task_id = $1 AND deleted_at IS NULL`, taskID).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestTrashHidesTasksAndRestoresLosslessly(t *testing.T) {
	ctx := context.Background()
	pid := sharedProject(t, "Trash Board")
	parent := mustCreateTask(t, 1, CreateTaskInput{Title: "Trash parent", ProjectID: &pid})
	childA := mustCreateTask(t, 1, CreateTaskInput{Title: "Trash child A", ProjectID: &pid, ParentID: &parent})
	childB := mustCreateTask(t, 1, CreateTaskInput{Title: "Trash child B", ProjectID: &pid, ParentID: &parent})
	if _, err := AddCommentForUser(ctx, 1, parent, "keep this comment"); err != nil {
		t.Fatalf("comment: %v", err)
	}

	// Child B goes to the trash on its own first, then the parent (with A).
	if err := DeleteTask(ctx, 1, childB); err != nil {
		t.Fatalf("trash child B: %v", err)
	}
	trashed, err := TrashTasksForUser(ctx, 1, []int{parent})
	if err != nil {
		t.Fatalf("trash parent: %v", err)
	}
	if len(trashed) != 2 {
		t.Fatalf("trashing the parent should take its live child too, got %v", trashed)
	}

	listed := listedTaskIDs(t, 1, pid)
	for _, id := range []int{parent, childA, childB} {
		if listed[id] {
			t.Fatalf("trashed task %d still listed", id)
		}
		if ok, _, _, _ := storage.CanUserAccessTask(id, 1); ok {
			t.Fatalf("trashed task %d still accessible", id)
		}
	}

	entries := trashEntryIDs(t, 1)
	if e, ok := entries[parent]; !ok || e.SubtaskCount != 1 {
		t.Fatalf("parent entry=%+v ok=%v, want it listed with 1 subtask", e, ok)
	}
	if _, ok := entries[childA]; ok {
		t.Fatal("child A was trashed with its parent and should not be listed on its own")
	}
	if e, ok := entries[childB]; !ok || e.ParentID != parent {
		t.Fatalf("child B (trashed separately) should be listed on its own, got %+v ok=%v", e, ok)
	}

	// Child B can't come back while its parent is in the trash.
	if _, err := RestoreTrashedTaskForUser(ctx, 1, childB); !errors.Is(err, ErrConflict) {
		t.Fatalf("restore child under trashed parent: err=%v, want conflict", err)
	}

	restored, err := RestoreTrashedTaskForUser(ctx, 1, parent)
	if err != nil {
		t.Fatalf("restore parent: %v", err)
	}
	if len(restored) != 2 {
		t.Fatalf("restore should bring back the parent and child A only, got %v", restored)
	}
	listed = listedTaskIDs(t, 1, pid)
	if !listed[parent] || !listed[childA] || listed[childB] {
		t.Fatalf("after restore listed=%v; want parent and A, not B", listed)
	}
	if n := commentCount(t, parent); n != 1 {
		t.Fatalf("comments after restore=%d, want 1 (restore must be lossless)", n)
	}

	// Now that the parent is back, child B can be restored too.
	if _, err := RestoreTrashedTaskForUser(ctx, 1, childB); err != nil {
		t.Fatalf("restore child B after parent: %v", err)
	}
	if _, err := RestoreTrashedTaskForUser(ctx, 1, childB); !errors.Is(err, ErrNotFound) {
		t.Fatalf("restoring a live task: err=%v, want not found", err)
	}
}

func TestTrashPermissionsAndPurge(t *testing.T) {
	ctx := context.Background()
	pid := sharedProject(t, "Trash Perms Board")
	if err := upsertTestMember(t, pid, 3, testRoleViewer); err != nil {
		t.Fatalf("viewer: %v", err)
	}
	taskID := mustCreateTask(t, 1, CreateTaskInput{Title: "Guarded", ProjectID: &pid})

	if err := DeleteTask(ctx, 3, taskID); !errors.Is(err, ErrForbidden) {
		t.Fatalf("viewer delete: err=%v, want forbidden", err)
	}
	if err := DeleteTask(ctx, 1, taskID); err != nil {
		t.Fatalf("owner delete: %v", err)
	}
	if _, ok := trashEntryIDs(t, 3)[taskID]; ok {
		t.Fatal("a viewer without tasks:delete should not see the project's trash")
	}
	if _, err := RestoreTrashedTaskForUser(ctx, 3, taskID); !errors.Is(err, ErrForbidden) {
		t.Fatalf("viewer restore: err=%v, want forbidden", err)
	}
	if err := PurgeTrashedTaskForUser(ctx, 3, taskID); !errors.Is(err, ErrForbidden) {
		t.Fatalf("viewer purge: err=%v, want forbidden", err)
	}

	// Permanent delete only works on trashed tasks.
	live := mustCreateTask(t, 1, CreateTaskInput{Title: "Still live", ProjectID: &pid})
	if err := PurgeTrashedTaskForUser(ctx, 1, live); !errors.Is(err, ErrNotFound) {
		t.Fatalf("purging a live task: err=%v, want not found", err)
	}
	if err := PurgeTrashedTaskForUser(ctx, 1, taskID); err != nil {
		t.Fatalf("owner purge: %v", err)
	}
	if trashed, _ := storage.IsTaskTrashed(taskID); trashed {
		t.Fatal("purged task still in the trash")
	}

	// Expiry: only tasks trashed longer ago than the retention window go.
	old := mustCreateTask(t, 1, CreateTaskInput{Title: "Old trash", ProjectID: &pid})
	recent := mustCreateTask(t, 1, CreateTaskInput{Title: "Recent trash", ProjectID: &pid})
	if _, err := TrashTasksForUser(ctx, 1, []int{old, recent}); err != nil {
		t.Fatalf("trash: %v", err)
	}
	execSQL(t, `UPDATE tasks SET deleted_at = NOW() - INTERVAL '31 days' WHERE id = $1`, old)
	if _, err := storage.PurgeExpiredTrash(30); err != nil {
		t.Fatalf("purge expired: %v", err)
	}
	if trashed, _ := storage.IsTaskTrashed(old); trashed {
		t.Fatal("31-day-old trash should be purged")
	}
	if trashed, _ := storage.IsTaskTrashed(recent); !trashed {
		t.Fatal("recent trash should be kept")
	}
}
