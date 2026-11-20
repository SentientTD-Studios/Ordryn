package domain

import (
	"context"
	"errors"
	"testing"

	"GoTodo/internal/storage"
)

func taskVersion(t *testing.T, taskID int) int {
	t.Helper()
	v, err := storage.GetTaskVersion(taskID)
	if err != nil {
		t.Fatalf("version: %v", err)
	}
	return v
}

func TestTaskVersionTracksContentChanges(t *testing.T) {
	ctx := context.Background()
	taskID, err := CreateTask(ctx, 1, CreateTaskInput{Title: "Versioned"})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	pool, err := storage.OpenDatabase()
	if err != nil {
		t.Fatal(err)
	}
	v0 := taskVersion(t, taskID)

	// Ordering and bookkeeping are not content.
	if _, err := pool.Exec(ctx, `UPDATE tasks SET position = position + 5, date_modified = NOW() WHERE id = $1`, taskID); err != nil {
		t.Fatal(err)
	}
	if v := taskVersion(t, taskID); v != v0 {
		t.Fatalf("position change bumped version %d -> %d", v0, v)
	}

	// Saving the same value changes nothing either.
	same := "Versioned"
	if _, err := UpdateTask(ctx, 1, taskID, UpdateTaskInput{Title: &same}); err != nil {
		t.Fatalf("same title: %v", err)
	}
	if v := taskVersion(t, taskID); v != v0 {
		t.Fatalf("no-op save bumped version %d -> %d", v0, v)
	}

	title := "Renamed"
	if _, err := UpdateTask(ctx, 1, taskID, UpdateTaskInput{Title: &title}); err != nil {
		t.Fatalf("rename: %v", err)
	}
	v1 := taskVersion(t, taskID)
	if v1 <= v0 {
		t.Fatalf("rename did not bump version (%d -> %d)", v0, v1)
	}

	// Tags live in their own table but are part of the task.
	tag, err := CreateTag(ctx, 1, "versioned-tag", nil)
	if err != nil {
		t.Fatalf("tag: %v", err)
	}
	tagIDs := []int{tag.ID}
	if _, err := UpdateTask(ctx, 1, taskID, UpdateTaskInput{TagIDs: &tagIDs}); err != nil {
		t.Fatalf("tag task: %v", err)
	}
	if v := taskVersion(t, taskID); v <= v1 {
		t.Fatalf("tagging did not bump version (%d -> %d)", v1, v)
	}

	// Hard-deleting a tagged task must not trip over the tag trigger.
	if _, err := pool.Exec(ctx, `DELETE FROM tasks WHERE id = $1`, taskID); err != nil {
		t.Fatalf("hard delete tagged task: %v", err)
	}
}

func TestUpdateTaskRefusesStaleVersion(t *testing.T) {
	ctx := context.Background()
	taskID, err := CreateTask(ctx, 1, CreateTaskInput{Title: "Shared"})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	seen := taskVersion(t, taskID)

	// Device A saves first.
	a := "From device A"
	if _, err := UpdateTask(ctx, 1, taskID, UpdateTaskInput{Title: &a, ExpectedVersion: &seen}); err != nil {
		t.Fatalf("first save: %v", err)
	}

	// Device B saves from the same, now old, version.
	b := "From device B"
	_, err = UpdateTask(ctx, 1, taskID, UpdateTaskInput{Title: &b, ExpectedVersion: &seen})
	if !errors.Is(err, ErrStaleTask) || !errors.Is(err, ErrConflict) {
		t.Fatalf("stale save: err=%v want ErrStaleTask", err)
	}
	pool, _ := storage.OpenDatabase()
	var got string
	if err := pool.QueryRow(ctx, `SELECT title FROM tasks WHERE id = $1`, taskID).Scan(&got); err != nil {
		t.Fatal(err)
	}
	if got != a {
		t.Fatalf("title=%q; the stale save must not overwrite %q", got, a)
	}

	// Saving from the fresh version works.
	fresh := taskVersion(t, taskID)
	if _, err := UpdateTask(ctx, 1, taskID, UpdateTaskInput{Title: &b, ExpectedVersion: &fresh}); err != nil {
		t.Fatalf("fresh save: %v", err)
	}
}
