package domain

import (
	"context"
	"testing"

	"GoTodo/internal/hooks"
	"GoTodo/internal/live"
	"GoTodo/internal/storage"
)

// Deleting a kanban project used to fail: tasks.status_id is ON DELETE RESTRICT,
// so cascading the project's statuses was blocked by its own tasks.
func TestDeleteKanbanProjectWithTasks(t *testing.T) {
	ctx := context.Background()
	proj, err := CreateProject(ctx, 1, "Delete Kanban Board", "")
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	if _, err := SetProjectWorkflowMode(ctx, 1, proj.ID, storage.WorkflowKanban); err != nil {
		t.Fatalf("enable kanban: %v", err)
	}
	pid := proj.ID
	points := 3
	taskID, err := CreateTask(ctx, 1, CreateTaskInput{Title: "Survives delete", ProjectID: &pid, EstimatePoints: &points})
	if err != nil {
		t.Fatalf("create task: %v", err)
	}
	before, err := storage.GetWorkflowFieldsForTasks([]int{taskID})
	if err != nil {
		t.Fatal(err)
	}
	if before[taskID].StatusID == 0 {
		t.Fatal("expected the kanban task to have a status before delete")
	}

	if err := DeleteProject(ctx, 1, proj.ID); err != nil {
		t.Fatalf("delete kanban project with tasks: %v", err)
	}

	_, projectID, err := storage.TaskOwnerAndProject(taskID)
	if err != nil {
		t.Fatalf("task should outlive its project: %v", err)
	}
	if projectID != 0 {
		t.Fatalf("project_id=%d, want 0 after project delete", projectID)
	}
	after, err := storage.GetWorkflowFieldsForTasks([]int{taskID})
	if err != nil {
		t.Fatal(err)
	}
	if f := after[taskID]; f.StatusID != 0 || f.EstimatePoints != nil || f.SprintID != 0 {
		t.Fatalf("workflow fields should be cleared, got status=%d estimate=%v sprint=%d", f.StatusID, f.EstimatePoints, f.SprintID)
	}
}

const projectDeletedHookManifest = `{
  "id": "delete-hook",
  "name": "Delete hook",
  "version": "1.0.0",
  "host_api": 1,
  "hooks": [{"on": "project.deleted", "label": "Project deleted"}],
  "delivery": {"type": "http.webhook", "url_from": "webhook_url", "format": "json"},
  "settings": [
    {"key": "webhook_url", "type": "secret", "label": "Webhook URL", "scope": ["project", "kanban"]},
    {"key": "triggers", "type": "hook_select", "label": "Triggers", "scope": ["project", "kanban"]}
  ],
  "templates": {"project.deleted": "Project {project} was deleted"}
}`

// project.deleted must be resolved while the project's destination exists, but
// only fire after the delete commits, and the project's extension secrets and
// callback tokens must not outlive it.
func TestDeleteProjectFiresPreparedHookAndClearsExtensionRows(t *testing.T) {
	if err := storage.CreateExtensionTables(); err != nil {
		t.Fatalf("extension tables: %v", err)
	}
	const extID = "delete-hook"
	loadTempStoreExtension(t, extID, projectDeletedHookManifest)
	if err := storage.UpsertExtensionSettings(extID, storage.ExtensionSettings{Enabled: true}); err != nil {
		t.Fatalf("enable extension: %v", err)
	}

	ctx := context.Background()
	proj, err := CreateProject(ctx, 1, "Delete Hook Board", "")
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	if err := storage.UpsertExtensionProjectSettings(extID, proj.ID, storage.ExtensionProjectSettings{
		Enabled:  true,
		Triggers: []string{"project.deleted"},
	}); err != nil {
		t.Fatalf("project settings: %v", err)
	}
	// .invalid never resolves, so the background send fails fast and offline.
	if err := storage.SetExtensionSecret(extID, proj.ID, "webhook_url", "https://receiver.invalid/hook"); err != nil {
		t.Fatalf("set secret: %v", err)
	}
	if _, err := storage.EnsureCallbackToken(extID, proj.ID, 0); err != nil {
		t.Fatalf("callback token: %v", err)
	}

	// The destination resolves while the project exists.
	if n := hooks.Prepare(hooks.Event{Type: hooks.EventProjectDeleted, ProjectID: proj.ID}).Len(); n != 1 {
		t.Fatalf("prepared deliveries=%d want 1", n)
	}

	type fired struct{ projectStillExists bool }
	firedCh := make(chan fired, 4)
	stop := live.ListenHooks(func(ev hooks.Event) {
		if ev.Type != hooks.EventProjectDeleted || ev.ProjectID != proj.ID {
			return
		}
		_, getErr := storage.GetProjectByID(proj.ID, 1)
		firedCh <- fired{projectStillExists: getErr == nil}
	})
	t.Cleanup(stop)

	if err := DeleteProject(ctx, 1, proj.ID); err != nil {
		t.Fatalf("delete project: %v", err)
	}

	select {
	case f := <-firedCh:
		if f.projectStillExists {
			t.Fatal("project.deleted fired before the delete committed")
		}
	default:
		t.Fatal("project.deleted did not fire")
	}

	if s, err := storage.GetExtensionSecret(extID, proj.ID, "webhook_url"); err != nil || s != "" {
		t.Fatalf("webhook secret should be removed with the project, got %q err=%v", s, err)
	}
	pool, err := storage.OpenDatabase()
	if err != nil {
		t.Fatal(err)
	}
	defer storage.CloseDatabase(pool)
	var tokens int
	if err := pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM extension_callback_tokens WHERE project_id = $1`, proj.ID).Scan(&tokens); err != nil {
		t.Fatal(err)
	}
	if tokens != 0 {
		t.Fatalf("callback tokens left for deleted project: %d", tokens)
	}
}

func TestDeleteProjectByNonOwnerLeavesTasksOnBoard(t *testing.T) {
	ctx := context.Background()
	proj, err := CreateProject(ctx, 1, "Delete Guard Board", "")
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	if _, err := SetProjectWorkflowMode(ctx, 1, proj.ID, storage.WorkflowKanban); err != nil {
		t.Fatalf("enable kanban: %v", err)
	}
	pid := proj.ID
	taskID, err := CreateTask(ctx, 1, CreateTaskInput{Title: "Stays on board", ProjectID: &pid})
	if err != nil {
		t.Fatalf("create task: %v", err)
	}

	// Wrong owner id: nothing is deleted, so the tasks must keep their status.
	if err := storage.DeleteProject(proj.ID, 2); err != nil {
		t.Fatalf("storage delete with wrong owner: %v", err)
	}
	fields, err := storage.GetWorkflowFieldsForTasks([]int{taskID})
	if err != nil {
		t.Fatal(err)
	}
	if fields[taskID].StatusID == 0 {
		t.Fatal("a delete that matched no project must not clear task statuses")
	}
	if err := DeleteProject(ctx, 1, proj.ID); err != nil {
		t.Fatalf("cleanup delete: %v", err)
	}
}
