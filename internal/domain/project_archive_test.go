package domain

import (
	"context"
	"errors"
	"testing"

	"GoTodo/internal/storage"
)

func TestArchiveProjectOwnerOnlyAndTagsTasks(t *testing.T) {
	ctx := context.Background()
	proj, err := CreateProject(ctx, 1, "Archive Me", "")
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	if err := upsertTestMember(t, proj.ID, 2, testRoleEditor); err != nil {
		t.Fatalf("add editor: %v", err)
	}
	if err := upsertTestMember(t, proj.ID, 3, testRoleViewer); err != nil {
		t.Fatalf("add viewer: %v", err)
	}

	pid := proj.ID
	taskID, err := CreateTask(ctx, 1, CreateTaskInput{Title: "Stay put", ProjectID: &pid})
	if err != nil {
		t.Fatalf("create task: %v", err)
	}

	if _, err := ArchiveProject(ctx, 2, pid); !errors.Is(err, ErrForbidden) {
		t.Fatalf("editor archive: err=%v want forbidden", err)
	}
	if _, err := ArchiveProject(ctx, 3, pid); !errors.Is(err, ErrForbidden) {
		t.Fatalf("viewer archive: err=%v want forbidden", err)
	}

	archived, err := ArchiveProject(ctx, 1, pid)
	if err != nil {
		t.Fatalf("owner archive: %v", err)
	}
	if archived == nil || !archived.Archived {
		t.Fatalf("archived=%+v want archived true", archived)
	}

	got, err := storage.GetAccessibleProjectByID(pid, 1)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if !got.Archived {
		t.Fatal("project should be archived")
	}

	tags, err := storage.GetTagsForTask(taskID)
	if err != nil {
		t.Fatalf("task tags: %v", err)
	}
	found := false
	for _, tg := range tags {
		if storage.IsArchivedTagName(tg.Name) && tg.Protected {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected protected archived tag on task, got %+v", tags)
	}

	if _, err := CreateTag(ctx, 1, "archived", &pid); !errors.Is(err, ErrValidation) {
		t.Fatalf("create reserved archived tag: err=%v want validation", err)
	}
}

func TestArchivedProjectRejectsNewTasks(t *testing.T) {
	ctx := context.Background()
	proj, err := CreateProject(ctx, 1, "No New Work", "")
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	pid := proj.ID
	archivedParent, err := CreateTask(ctx, 1, CreateTaskInput{Title: "Before archive", ProjectID: &pid})
	if err != nil {
		t.Fatalf("pre-archive task: %v", err)
	}
	inboxTask, err := CreateTask(ctx, 1, CreateTaskInput{Title: "Inbox parent"})
	if err != nil {
		t.Fatalf("inbox parent: %v", err)
	}
	if _, err := ArchiveProject(ctx, 1, pid); err != nil {
		t.Fatalf("archive: %v", err)
	}

	if _, err := CreateTask(ctx, 1, CreateTaskInput{Title: "Too late", ProjectID: &pid}); !errors.Is(err, ErrConflict) {
		t.Fatalf("create on archived: err=%v want conflict", err)
	}
	if _, err := CreateTask(ctx, 1, CreateTaskInput{Title: "Child", ParentID: &archivedParent}); !errors.Is(err, ErrConflict) {
		t.Fatalf("subtask on archived: err=%v want conflict", err)
	}

	dst, err := CreateProject(ctx, 1, "Still Active Then Archive", "")
	if err != nil {
		t.Fatalf("active dest: %v", err)
	}
	if _, err := ArchiveProject(ctx, 1, dst.ID); err != nil {
		t.Fatalf("archive dest: %v", err)
	}
	destID := dst.ID
	projectPtr := &destID
	if _, err := UpdateTask(ctx, 1, inboxTask, UpdateTaskInput{ProjectID: &projectPtr}); !errors.Is(err, ErrConflict) {
		t.Fatalf("move into archived: err=%v want conflict", err)
	}
}

func TestRestoreProjectAllowsNewTasksAndClearsTag(t *testing.T) {
	ctx := context.Background()
	proj, err := CreateProject(ctx, 1, "Bring Back", "")
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	pid := proj.ID
	taskID, err := CreateTask(ctx, 1, CreateTaskInput{Title: "Existing", ProjectID: &pid})
	if err != nil {
		t.Fatalf("create task: %v", err)
	}
	if _, err := ArchiveProject(ctx, 1, pid); err != nil {
		t.Fatalf("archive: %v", err)
	}
	if err := upsertTestMember(t, pid, 2, testRoleEditor); err != nil {
		t.Fatalf("add editor: %v", err)
	}
	if _, err := RestoreProject(ctx, 2, pid); !errors.Is(err, ErrForbidden) {
		t.Fatalf("editor restore: err=%v want forbidden", err)
	}

	restored, err := RestoreProject(ctx, 1, pid)
	if err != nil {
		t.Fatalf("owner restore: %v", err)
	}
	if restored == nil || restored.Archived {
		t.Fatalf("restored=%+v want archived false", restored)
	}

	tags, err := storage.GetTagsForTask(taskID)
	if err != nil {
		t.Fatalf("task tags: %v", err)
	}
	for _, tg := range tags {
		if storage.IsArchivedTagName(tg.Name) {
			t.Fatalf("archived tag should be cleared, got %+v", tags)
		}
	}

	if _, err := CreateTask(ctx, 1, CreateTaskInput{Title: "After restore", ProjectID: &pid}); err != nil {
		t.Fatalf("create after restore: %v", err)
	}
}

func TestReorderProjectsIgnoresArchived(t *testing.T) {
	ctx := context.Background()
	c, err := CreateProject(ctx, 1, "Reorder Archived", "")
	if err != nil {
		t.Fatalf("create archived: %v", err)
	}
	if _, err := ArchiveProject(ctx, 1, c.ID); err != nil {
		t.Fatalf("archive: %v", err)
	}

	active, err := storage.GetActiveOwnedProjectsForUser(1)
	if err != nil {
		t.Fatalf("list active: %v", err)
	}
	if len(active) < 2 {
		a, err := CreateProject(ctx, 1, "Reorder Extra A", "")
		if err != nil {
			t.Fatalf("extra a: %v", err)
		}
		b, err := CreateProject(ctx, 1, "Reorder Extra B", "")
		if err != nil {
			t.Fatalf("extra b: %v", err)
		}
		_ = a
		_ = b
		active, err = storage.GetActiveOwnedProjectsForUser(1)
		if err != nil {
			t.Fatalf("list active retry: %v", err)
		}
	}

	ids := make([]int, len(active))
	for i, p := range active {
		ids[len(active)-1-i] = p.ID
	}
	if err := ReorderProjectsForUser(ctx, 1, ids); err != nil {
		t.Fatalf("reorder active only: %v", err)
	}

	withArchived := append(append([]int{}, ids...), c.ID)
	if err := ReorderProjectsForUser(ctx, 1, withArchived); !errors.Is(err, ErrValidation) {
		t.Fatalf("reorder including archived: err=%v want validation", err)
	}
}

func TestArchiveProjectNotFound(t *testing.T) {
	ctx := context.Background()
	if _, err := ArchiveProject(ctx, 1, 999999); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing project: err=%v want not found", err)
	}
}
