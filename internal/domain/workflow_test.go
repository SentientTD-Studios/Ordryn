package domain

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"GoTodo/internal/storage"

	embeddedpostgres "github.com/fergusstrange/embedded-postgres"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestMain(m *testing.M) {
	os.Setenv("SESSION_KEY", "test-session-key-for-unit-tests-32chars!!")
	port := uint32(5441)
	// Isolate RuntimePath so go test ./... can run this package in parallel with
	// internal/tasks (which also starts embedded-postgres).
	runtimePath := filepath.Join(os.TempDir(), "gotodo-embedded-pg-domain")
	db := startEmbeddedPostgres("gotodo_workflow_test", runtimePath, port)

	os.Setenv("DB_HOST", "localhost")
	os.Setenv("DB_PORT", fmt.Sprintf("%d", port))
	os.Setenv("DB_USER", "postgres")
	os.Setenv("DB_PASSWORD", "postgres")
	os.Setenv("DB_NAME", "gotodo_workflow_test")
	os.Setenv("DB_SSLMODE", "disable")

	pool, err := pgxpool.New(context.Background(),
		fmt.Sprintf("postgres://postgres:postgres@localhost:%d/gotodo_workflow_test?sslmode=disable", port))
	if err != nil {
		fmt.Fprintf(os.Stderr, "connect: %v\n", err)
		os.Exit(1)
	}

	if err := storage.CreateUsersTable(); err != nil {
		fmt.Fprintf(os.Stderr, "users: %v\n", err)
		os.Exit(1)
	}
	if err := storage.MigrateUsersAddName(); err != nil {
		fmt.Fprintf(os.Stderr, "user_name: %v\n", err)
		os.Exit(1)
	}
	if err := storage.MigrateUsersAddAvatarURL(); err != nil {
		fmt.Fprintf(os.Stderr, "avatar_url: %v\n", err)
		os.Exit(1)
	}
	if err := storage.CreateProjectsTable(); err != nil {
		fmt.Fprintf(os.Stderr, "projects: %v\n", err)
		os.Exit(1)
	}
	if err := storage.MigrateProjectsAddArchived(); err != nil {
		fmt.Fprintf(os.Stderr, "projects archived: %v\n", err)
		os.Exit(1)
	}
	if err := storage.MigrateProjectsAddWorkflowMode(); err != nil {
		fmt.Fprintf(os.Stderr, "workflow mode: %v\n", err)
		os.Exit(1)
	}
	if err := storage.CreateProjectSharingTables(); err != nil {
		fmt.Fprintf(os.Stderr, "sharing: %v\n", err)
		os.Exit(1)
	}
	if err := storage.CreateTasksTable(); err != nil {
		fmt.Fprintf(os.Stderr, "tasks: %v\n", err)
		os.Exit(1)
	}
	if err := storage.MigrateTasksAddProjectID(); err != nil {
		fmt.Fprintf(os.Stderr, "project_id: %v\n", err)
		os.Exit(1)
	}
	if err := storage.MigrateTasksAddPosition(); err != nil {
		fmt.Fprintf(os.Stderr, "position: %v\n", err)
		os.Exit(1)
	}
	if err := storage.MigrateTasksAddIsFavorite(); err != nil {
		fmt.Fprintf(os.Stderr, "favorite: %v\n", err)
		os.Exit(1)
	}
	if err := storage.MigrateTasksAddPriority(); err != nil {
		fmt.Fprintf(os.Stderr, "priority: %v\n", err)
		os.Exit(1)
	}
	if err := storage.MigrateTasksAddDateModified(); err != nil {
		fmt.Fprintf(os.Stderr, "date_modified: %v\n", err)
		os.Exit(1)
	}
	if err := storage.MigrateTasksAddDueDate(); err != nil {
		fmt.Fprintf(os.Stderr, "due_date: %v\n", err)
		os.Exit(1)
	}
	if err := storage.MigrateTasksAddParentID(); err != nil {
		fmt.Fprintf(os.Stderr, "parent_id: %v\n", err)
		os.Exit(1)
	}
	if err := storage.CreateProjectWorkflowTables(); err != nil {
		fmt.Fprintf(os.Stderr, "workflow tables: %v\n", err)
		os.Exit(1)
	}
	if err := storage.MigrateProjectStatusesAddDescription(); err != nil {
		fmt.Fprintf(os.Stderr, "status description: %v\n", err)
		os.Exit(1)
	}
	if err := storage.MigrateTasksAddWorkflowFields(); err != nil {
		fmt.Fprintf(os.Stderr, "task workflow: %v\n", err)
		os.Exit(1)
	}
	if err := storage.MigrateTasksAddClaimedBy(); err != nil {
		fmt.Fprintf(os.Stderr, "claimed_by: %v\n", err)
		os.Exit(1)
	}
	if err := storage.CreateProjectSprintsTable(); err != nil {
		fmt.Fprintf(os.Stderr, "sprints: %v\n", err)
		os.Exit(1)
	}
	if err := storage.MigrateProjectSprintsAddDescription(); err != nil {
		fmt.Fprintf(os.Stderr, "sprint description: %v\n", err)
		os.Exit(1)
	}
	if err := storage.MigrateProjectSprintsAddLockDate(); err != nil {
		fmt.Fprintf(os.Stderr, "sprint lock_date: %v\n", err)
		os.Exit(1)
	}
	if err := storage.MigrateTasksAddSprintID(); err != nil {
		fmt.Fprintf(os.Stderr, "sprint_id: %v\n", err)
		os.Exit(1)
	}
	if err := storage.CreateUserNotificationsTable(); err != nil {
		fmt.Fprintf(os.Stderr, "notifications: %v\n", err)
		os.Exit(1)
	}
	if err := storage.CreateTaskCommentsTable(); err != nil {
		fmt.Fprintf(os.Stderr, "task comments: %v\n", err)
		os.Exit(1)
	}
	if err := storage.CreateTaskEventsTable(); err != nil {
		fmt.Fprintf(os.Stderr, "task events: %v\n", err)
		os.Exit(1)
	}
	if err := storage.CreateTagsTables(); err != nil {
		fmt.Fprintf(os.Stderr, "tags: %v\n", err)
		os.Exit(1)
	}

	// Reproduce production DBs that still have UNIQUE(user_id, name) while a
	// personal tag is used on both inbox and project tasks. The migration must
	// drop that constraint before cloning.
	_, err = pool.Exec(context.Background(), `
		INSERT INTO users (id, email, password, role_id) VALUES
			(1, 'owner@example.com', 'x', 1)
		ON CONFLICT DO NOTHING;
		ALTER TABLE tags DROP CONSTRAINT IF EXISTS tags_user_id_name_key;
		ALTER TABLE tags ADD CONSTRAINT tags_user_id_name_key UNIQUE (user_id, name);
		INSERT INTO projects (id, user_id, name) VALUES (9001, 1, 'Legacy Tag Migrate Fixture')
		ON CONFLICT DO NOTHING;
		INSERT INTO project_members (project_id, user_id, role) VALUES (9001, 1, 'owner')
		ON CONFLICT DO NOTHING;
		INSERT INTO tags (id, user_id, name, color) VALUES (9001, 1, 'legacy-migrate-clone', '#0d6efd')
		ON CONFLICT DO NOTHING;
		INSERT INTO tasks (id, title, user_id, project_id) VALUES
			(9001, 'legacy inbox', 1, NULL),
			(9002, 'legacy project', 1, 9001)
		ON CONFLICT DO NOTHING;
		INSERT INTO task_tags (task_id, tag_id) VALUES (9001, 9001), (9002, 9001)
		ON CONFLICT DO NOTHING;
		SELECT setval(pg_get_serial_sequence('projects','id'), GREATEST((SELECT MAX(id) FROM projects), 1));
		SELECT setval(pg_get_serial_sequence('tasks','id'), GREATEST((SELECT MAX(id) FROM tasks), 1));
		SELECT setval(pg_get_serial_sequence('tags','id'), GREATEST((SELECT MAX(id) FROM tags), 1));
	`)
	if err != nil {
		fmt.Fprintf(os.Stderr, "legacy tag fixture: %v\n", err)
		os.Exit(1)
	}

	if err := storage.MigrateTagsAddProjectID(); err != nil {
		fmt.Fprintf(os.Stderr, "tags migrate: %v\n", err)
		os.Exit(1)
	}
	if err := storage.MigrateTagsAddProjectID(); err != nil {
		fmt.Fprintf(os.Stderr, "tags migrate retry: %v\n", err)
		os.Exit(1)
	}
	if err := storage.MigrateTagsAddProtected(); err != nil {
		fmt.Fprintf(os.Stderr, "tags protected migrate: %v\n", err)
		os.Exit(1)
	}
	if err := storage.CreateGitHubTables(); err != nil {
		fmt.Fprintf(os.Stderr, "github: %v\n", err)
		os.Exit(1)
	}
	if err := storage.MigrateUsersAddMFA(); err != nil {
		fmt.Fprintf(os.Stderr, "mfa columns: %v\n", err)
		os.Exit(1)
	}
	if err := storage.CreateMFARecoveryCodesTable(); err != nil {
		fmt.Fprintf(os.Stderr, "mfa recovery: %v\n", err)
		os.Exit(1)
	}

	_, err = pool.Exec(context.Background(), `
		INSERT INTO users (id, email, password, role_id) VALUES
			(1, 'owner@example.com', 'x', 1),
			(2, 'editor@example.com', 'x', 1),
			(3, 'viewer@example.com', 'x', 1)
		ON CONFLICT DO NOTHING;
		SELECT setval(pg_get_serial_sequence('users','id'), (SELECT MAX(id) FROM users));
	`)
	pool.Close()
	if err != nil {
		fmt.Fprintf(os.Stderr, "seed: %v\n", err)
		os.Exit(1)
	}

	code := m.Run()
	_ = db.Stop()
	os.Exit(code)
}

func startEmbeddedPostgres(database, runtimePath string, port uint32) *embeddedpostgres.EmbeddedPostgres {
	var last error
	for attempt := 1; attempt <= 3; attempt++ {
		db := embeddedpostgres.NewDatabase(embeddedpostgres.DefaultConfig().
			Version(embeddedpostgres.V16).
			Port(port).
			Database(database).
			RuntimePath(runtimePath))
		if err := db.Start(); err == nil {
			return db
		} else {
			last = err
			fmt.Fprintf(os.Stderr, "start postgres (attempt %d/3): %v\n", attempt, err)
			time.Sleep(time.Duration(attempt) * 2 * time.Second)
		}
	}
	fmt.Fprintf(os.Stderr, "start postgres: %v\n", last)
	os.Exit(1)
	return nil
}

func TestKanbanEnableDisableAndStatusCap(t *testing.T) {
	ctx := context.Background()
	proj, err := CreateProject(ctx, 1, "Board Proj", "")
	if err != nil {
		t.Fatalf("create project: %v", err)
	}

	updated, err := SetProjectWorkflowMode(ctx, 1, proj.ID, storage.WorkflowKanban)
	if err != nil {
		t.Fatalf("enable kanban: %v", err)
	}
	if updated.WorkflowMode != storage.WorkflowKanban {
		t.Fatalf("mode=%q", updated.WorkflowMode)
	}

	statuses, err := ListProjectStatusesForUser(ctx, 1, proj.ID)
	if err != nil {
		t.Fatalf("list statuses: %v", err)
	}
	if len(statuses) != 3 {
		t.Fatalf("want 3 default statuses, got %d", len(statuses))
	}
	wantDesc := map[string]string{
		"To Do":       "Work that hasn't been started yet",
		"In Progress": "Currently being worked on",
		"Done":        "Finished and ready to close",
	}
	for _, s := range statuses {
		if s.Description != wantDesc[s.Name] {
			t.Errorf("%s description=%q want %q", s.Name, s.Description, wantDesc[s.Name])
		}
	}

	pid := proj.ID
	taskID, err := CreateTask(ctx, 1, CreateTaskInput{
		Title:     "Kanban task",
		ProjectID: &pid,
		Priority:  1,
	})
	if err != nil {
		t.Fatalf("create task: %v", err)
	}

	_, err = SetProjectWorkflowMode(ctx, 1, proj.ID, storage.WorkflowClassic)
	if !errors.Is(err, ErrConflict) {
		t.Fatalf("disable with tasks: err=%v want conflict", err)
	}

	fields, err := storage.GetWorkflowFieldsForTasks([]int{taskID})
	if err != nil {
		t.Fatalf("workflow fields: %v", err)
	}
	f := fields[taskID]
	if f.StatusID == 0 || f.StatusName == "" {
		t.Fatalf("expected status on new task, got %+v", f)
	}

	// Fill up to max statuses (3 defaults + 5 custom = 8).
	for i := 0; i < 5; i++ {
		_, err := CreateProjectStatusForUser(ctx, 1, proj.ID, CreateProjectStatusInput{
			Name: fmt.Sprintf("Extra %d", i+1),
		})
		if err != nil {
			t.Fatalf("create status %d: %v", i+1, err)
		}
	}
	_, err = CreateProjectStatusForUser(ctx, 1, proj.ID, CreateProjectStatusInput{Name: "Over"})
	if !errors.Is(err, ErrConflict) {
		t.Fatalf("status cap: err=%v want conflict", err)
	}

	// Completed sync via status.
	var doneID int
	statuses, _ = ListProjectStatusesForUser(ctx, 1, proj.ID)
	for _, s := range statuses {
		if s.IsDone {
			doneID = s.ID
			break
		}
	}
	sid := doneID
	statusPtr := &sid
	if _, err := UpdateTask(ctx, 1, taskID, UpdateTaskInput{StatusID: &statusPtr}); err != nil {
		t.Fatalf("set done status: %v", err)
	}
	pool, _ := storage.OpenDatabase()
	defer storage.CloseDatabase(pool)
	var completed bool
	if err := pool.QueryRow(ctx, `SELECT completed FROM tasks WHERE id=$1`, taskID).Scan(&completed); err != nil {
		t.Fatal(err)
	}
	if !completed {
		t.Fatal("expected completed=true after done status")
	}

	// Time entry.
	entry, err := AddTimeEntryForUser(ctx, 1, taskID, 30, "pairing")
	if err != nil {
		t.Fatalf("add time: %v", err)
	}
	if entry.Minutes != 30 {
		t.Fatalf("minutes=%d", entry.Minutes)
	}

	// Delete task so disable is allowed.
	if err := DeleteTask(ctx, 1, taskID); err != nil {
		t.Fatalf("delete task: %v", err)
	}
	if _, err := SetProjectWorkflowMode(ctx, 1, proj.ID, storage.WorkflowClassic); err != nil {
		t.Fatalf("disable empty kanban: %v", err)
	}
}

func TestKanbanEditorCannotManageStatuses(t *testing.T) {
	ctx := context.Background()
	proj, err := CreateProject(ctx, 1, "Shared Board", "")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, err := SetProjectWorkflowMode(ctx, 1, proj.ID, storage.WorkflowKanban); err != nil {
		t.Fatalf("enable: %v", err)
	}
	if err := storage.UpsertProjectMember(proj.ID, 2, storage.RoleEditor); err != nil {
		t.Fatalf("add editor: %v", err)
	}
	_, err = CreateProjectStatusForUser(ctx, 2, proj.ID, CreateProjectStatusInput{Name: "Blocked"})
	if !errors.Is(err, ErrForbidden) {
		t.Fatalf("editor create status: err=%v want forbidden", err)
	}
}

func TestProjectStatusDescription(t *testing.T) {
	ctx := context.Background()
	proj, err := CreateProject(ctx, 1, "Desc Board", "")
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	if _, err := SetProjectWorkflowMode(ctx, 1, proj.ID, storage.WorkflowKanban); err != nil {
		t.Fatalf("enable kanban: %v", err)
	}

	created, err := CreateProjectStatusForUser(ctx, 1, proj.ID, CreateProjectStatusInput{
		Name:        "Blocked",
		Description: "  Waiting on someone else  ",
	})
	if err != nil {
		t.Fatalf("create with description: %v", err)
	}
	if created.Description != "Waiting on someone else" {
		t.Fatalf("create description=%q", created.Description)
	}

	tooLong := strings.Repeat("x", storage.MaxStatusDescriptionLen+1)
	_, err = CreateProjectStatusForUser(ctx, 1, proj.ID, CreateProjectStatusInput{
		Name:        "Too Long",
		Description: tooLong,
	})
	if !errors.Is(err, ErrValidation) {
		t.Fatalf("create over-limit: err=%v want validation", err)
	}

	desc := "Needs a decision"
	updated, err := UpdateProjectStatusForUser(ctx, 1, proj.ID, created.ID, UpdateProjectStatusInput{
		Description: &desc,
	})
	if err != nil {
		t.Fatalf("update description: %v", err)
	}
	if updated.Description != desc {
		t.Fatalf("updated description=%q want %q", updated.Description, desc)
	}

	over := strings.Repeat("y", storage.MaxStatusDescriptionLen+1)
	_, err = UpdateProjectStatusForUser(ctx, 1, proj.ID, created.ID, UpdateProjectStatusInput{
		Description: &over,
	})
	if !errors.Is(err, ErrValidation) {
		t.Fatalf("update over-limit: err=%v want validation", err)
	}

	empty := ""
	cleared, err := UpdateProjectStatusForUser(ctx, 1, proj.ID, created.ID, UpdateProjectStatusInput{
		Description: &empty,
	})
	if err != nil {
		t.Fatalf("clear description: %v", err)
	}
	if cleared.Description != "" {
		t.Fatalf("cleared description=%q want empty", cleared.Description)
	}

	got, err := storage.GetProjectStatus(proj.ID, created.ID)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if got.Description != "" {
		t.Fatalf("persisted description=%q want empty", got.Description)
	}
}

func eventsOfType(t *testing.T, taskID, userID int, eventType string) []storage.TaskEvent {
	t.Helper()
	events, err := storage.GetEventsForTask(taskID, userID, 50)
	if err != nil {
		t.Fatalf("list events: %v", err)
	}
	var out []storage.TaskEvent
	for _, ev := range events {
		if ev.EventType == eventType {
			out = append(out, ev)
		}
	}
	return out
}

func TestStatusChangedEventMetadataAndActor(t *testing.T) {
	ctx := context.Background()
	proj, err := CreateProject(ctx, 1, "Audit Board", "")
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	if _, err := SetProjectWorkflowMode(ctx, 1, proj.ID, storage.WorkflowKanban); err != nil {
		t.Fatalf("enable kanban: %v", err)
	}
	statuses, err := ListProjectStatusesForUser(ctx, 1, proj.ID)
	if err != nil {
		t.Fatalf("list statuses: %v", err)
	}
	var todoID, progressID int
	var todoName, progressName string
	for _, s := range statuses {
		switch s.Name {
		case "To Do":
			todoID, todoName = s.ID, s.Name
		case "In Progress":
			progressID, progressName = s.ID, s.Name
		}
	}
	if todoID == 0 || progressID == 0 {
		t.Fatalf("missing default statuses: %+v", statuses)
	}

	pid := proj.ID
	taskID, err := CreateTask(ctx, 1, CreateTaskInput{
		Title:     "Move me",
		ProjectID: &pid,
	})
	if err != nil {
		t.Fatalf("create task: %v", err)
	}

	sid := progressID
	statusPtr := &sid
	if _, err := UpdateTask(ctx, 1, taskID, UpdateTaskInput{StatusID: &statusPtr}); err != nil {
		t.Fatalf("set status: %v", err)
	}

	changed := eventsOfType(t, taskID, 1, "status_changed")
	if len(changed) != 1 {
		t.Fatalf("status_changed count=%d want 1", len(changed))
	}
	ev := changed[0]
	if ev.Metadata["to"] != progressName {
		t.Errorf("to=%v want %q", ev.Metadata["to"], progressName)
	}
	if ev.Metadata["from"] != todoName {
		t.Errorf("from=%v want %q", ev.Metadata["from"], todoName)
	}
	if ev.ActorEmail != "owner@example.com" && ev.ActorUserName == "" {
		t.Errorf("missing actor: email=%q name=%q", ev.ActorEmail, ev.ActorUserName)
	}
	if len(eventsOfType(t, taskID, 1, "reordered")) != 0 {
		t.Fatal("did not expect a reordered event from status change")
	}

	// Same status again should not log another status_changed.
	if _, err := UpdateTask(ctx, 1, taskID, UpdateTaskInput{StatusID: &statusPtr}); err != nil {
		t.Fatalf("set same status: %v", err)
	}
	if got := eventsOfType(t, taskID, 1, "status_changed"); len(got) != 1 {
		t.Fatalf("after no-op status_changed count=%d want 1", len(got))
	}
}

func TestReorderSkipsEventForKanbanColumn(t *testing.T) {
	ctx := context.Background()
	proj, err := CreateProject(ctx, 1, "Reorder Board", "")
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	if _, err := SetProjectWorkflowMode(ctx, 1, proj.ID, storage.WorkflowKanban); err != nil {
		t.Fatalf("enable kanban: %v", err)
	}
	statuses, err := ListProjectStatusesForUser(ctx, 1, proj.ID)
	if err != nil {
		t.Fatalf("list statuses: %v", err)
	}
	var todoID int
	for _, s := range statuses {
		if s.Name == "To Do" {
			todoID = s.ID
			break
		}
	}
	if todoID == 0 {
		t.Fatal("missing To Do status")
	}

	pid := proj.ID
	id1, err := CreateTask(ctx, 1, CreateTaskInput{Title: "A", ProjectID: &pid})
	if err != nil {
		t.Fatalf("create A: %v", err)
	}
	id2, err := CreateTask(ctx, 1, CreateTaskInput{Title: "B", ProjectID: &pid})
	if err != nil {
		t.Fatalf("create B: %v", err)
	}

	statusFilter := todoID
	if err := ReorderTasks(ctx, 1, []int{id2, id1}, false, &pid, nil, &statusFilter); err != nil {
		t.Fatalf("kanban reorder: %v", err)
	}
	if n := len(eventsOfType(t, id1, 1, "reordered")); n != 0 {
		t.Fatalf("task A reordered events=%d want 0", n)
	}
	if n := len(eventsOfType(t, id2, 1, "reordered")); n != 0 {
		t.Fatalf("task B reordered events=%d want 0", n)
	}
}

func TestReorderMovesTaskIntoKanbanColumn(t *testing.T) {
	ctx := context.Background()
	proj, err := CreateProject(ctx, 1, "Move Column Board", "")
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	if _, err := SetProjectWorkflowMode(ctx, 1, proj.ID, storage.WorkflowKanban); err != nil {
		t.Fatalf("enable kanban: %v", err)
	}
	statuses, err := ListProjectStatusesForUser(ctx, 1, proj.ID)
	if err != nil {
		t.Fatalf("list statuses: %v", err)
	}
	var todoID, doneID int
	for _, s := range statuses {
		switch s.Name {
		case "To Do":
			todoID = s.ID
		case "Done":
			doneID = s.ID
		}
	}
	if todoID == 0 || doneID == 0 {
		t.Fatalf("missing default statuses: %+v", statuses)
	}

	pid := proj.ID
	taskID, err := CreateTask(ctx, 1, CreateTaskInput{Title: "Card", ProjectID: &pid})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	if err := ReorderTasks(ctx, 1, []int{taskID}, false, &pid, nil, &doneID); err != nil {
		t.Fatalf("move via reorder: %v", err)
	}

	pool, err := storage.OpenDatabase()
	if err != nil {
		t.Fatalf("db: %v", err)
	}
	defer storage.CloseDatabase(pool)
	var statusID int
	var completed bool
	if err := pool.QueryRow(ctx,
		`SELECT status_id, COALESCE(completed,false) FROM tasks WHERE id = $1`, taskID,
	).Scan(&statusID, &completed); err != nil {
		t.Fatalf("load task: %v", err)
	}
	if statusID != doneID {
		t.Fatalf("status_id=%d want %d (Done)", statusID, doneID)
	}
	if !completed {
		t.Fatal("expected completed=true after move to Done")
	}
	if n := len(eventsOfType(t, taskID, 1, "status_changed")); n != 1 {
		t.Fatalf("status_changed events=%d want 1", n)
	}
	if n := len(eventsOfType(t, taskID, 1, "completed")); n != 1 {
		t.Fatalf("completed events=%d want 1", n)
	}

	if err := ReorderTasks(ctx, 1, []int{taskID}, false, &pid, nil, &todoID); err != nil {
		t.Fatalf("move back: %v", err)
	}
	if err := pool.QueryRow(ctx,
		`SELECT status_id, COALESCE(completed,false) FROM tasks WHERE id = $1`, taskID,
	).Scan(&statusID, &completed); err != nil {
		t.Fatalf("reload task: %v", err)
	}
	if statusID != todoID {
		t.Fatalf("status_id=%d want %d (To Do)", statusID, todoID)
	}
	if completed {
		t.Fatal("expected completed=false after move to To Do")
	}
}

func TestSubtaskKanbanColumnMoveUsesUpdateNotReorder(t *testing.T) {
	ctx := context.Background()
	proj, err := CreateProject(ctx, 1, "Subtask Column Board", "")
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	if _, err := SetProjectWorkflowMode(ctx, 1, proj.ID, storage.WorkflowKanban); err != nil {
		t.Fatalf("enable kanban: %v", err)
	}
	statuses, err := ListProjectStatusesForUser(ctx, 1, proj.ID)
	if err != nil {
		t.Fatalf("list statuses: %v", err)
	}
	var doneID int
	for _, s := range statuses {
		if s.Name == "Done" {
			doneID = s.ID
			break
		}
	}
	if doneID == 0 {
		t.Fatalf("missing Done status: %+v", statuses)
	}

	pid := proj.ID
	parentID, err := CreateTask(ctx, 1, CreateTaskInput{Title: "Parent", ProjectID: &pid})
	if err != nil {
		t.Fatalf("create parent: %v", err)
	}
	childID, err := CreateTask(ctx, 1, CreateTaskInput{Title: "Child", ParentID: &parentID})
	if err != nil {
		t.Fatalf("create child: %v", err)
	}

	if err := ReorderTasks(ctx, 1, []int{childID}, false, &pid, nil, &doneID); err == nil {
		t.Fatal("expected reorder of subtask to fail")
	} else if !errors.Is(err, ErrValidation) {
		t.Fatalf("reorder subtask: err=%v want validation", err)
	}

	statusPtr := &doneID
	if _, err := UpdateTask(ctx, 1, childID, UpdateTaskInput{StatusID: &statusPtr}); err != nil {
		t.Fatalf("update subtask status: %v", err)
	}

	pool, err := storage.OpenDatabase()
	if err != nil {
		t.Fatalf("db: %v", err)
	}
	defer storage.CloseDatabase(pool)
	var statusID int
	var completed bool
	if err := pool.QueryRow(ctx,
		`SELECT status_id, COALESCE(completed,false) FROM tasks WHERE id = $1`, childID,
	).Scan(&statusID, &completed); err != nil {
		t.Fatalf("load subtask: %v", err)
	}
	if statusID != doneID {
		t.Fatalf("status_id=%d want %d (Done)", statusID, doneID)
	}
	if !completed {
		t.Fatal("expected completed=true after subtask move to Done")
	}
}

func TestReorderLogsEventWithoutStatusFilter(t *testing.T) {
	ctx := context.Background()
	proj, err := CreateProject(ctx, 1, "List Reorder", "")
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	pid := proj.ID
	id1, err := CreateTask(ctx, 1, CreateTaskInput{Title: "One", ProjectID: &pid})
	if err != nil {
		t.Fatalf("create One: %v", err)
	}
	id2, err := CreateTask(ctx, 1, CreateTaskInput{Title: "Two", ProjectID: &pid})
	if err != nil {
		t.Fatalf("create Two: %v", err)
	}

	if err := ReorderTasks(ctx, 1, []int{id2, id1}, false, &pid, nil, nil); err != nil {
		t.Fatalf("list reorder: %v", err)
	}
	if n := len(eventsOfType(t, id2, 1, "reordered")); n != 1 {
		t.Fatalf("reordered events on ids[0]=%d want 1", n)
	}
}
