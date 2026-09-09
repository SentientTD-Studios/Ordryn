package domain

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"GoTodo/internal/storage"
	"GoTodo/internal/tasks"
)

func TestProjectSprintsCRUDAndTaskAssignment(t *testing.T) {
	ctx := context.Background()
	proj, err := CreateProject(ctx, 1, "Sprint Board", "")
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	if _, err := SetProjectWorkflowMode(ctx, 1, proj.ID, storage.WorkflowKanban); err != nil {
		t.Fatalf("enable kanban: %v", err)
	}

	listed, err := ListProjectSprintsForUser(ctx, 1, proj.ID)
	if err != nil {
		t.Fatalf("list empty: %v", err)
	}
	if len(listed) != 0 {
		t.Fatalf("expected no sprints, got %d", len(listed))
	}

	sprint, err := CreateProjectSprintForUser(ctx, 1, proj.ID, CreateProjectSprintInput{
		Name:      "Sprint 1",
		StartDate: "2026-08-24",
		EndDate:   "2026-09-06",
	})
	if err != nil {
		t.Fatalf("create sprint: %v", err)
	}
	if sprint.Name != "Sprint 1" {
		t.Fatalf("name=%q", sprint.Name)
	}
	if sprint.Description != "" {
		t.Fatalf("description=%q want empty", sprint.Description)
	}
	if sprint.LockDate != nil {
		t.Fatalf("lock_date=%v want nil", sprint.LockDate)
	}
	if storage.FormatSprintDatePtr(sprint.StartDate) != "2026-08-24" {
		t.Fatalf("start=%q", storage.FormatSprintDatePtr(sprint.StartDate))
	}
	if storage.FormatSprintDatePtr(sprint.EndDate) != "2026-09-06" {
		t.Fatalf("end=%q", storage.FormatSprintDatePtr(sprint.EndDate))
	}

	if _, err := CreateProjectSprintForUser(ctx, 1, proj.ID, CreateProjectSprintInput{
		Name:      "Sprint 1",
		StartDate: "2026-09-07",
		EndDate:   "2026-09-20",
	}); !errors.Is(err, ErrConflict) {
		t.Fatalf("duplicate name err=%v", err)
	}

	if _, err := CreateProjectSprintForUser(ctx, 1, proj.ID, CreateProjectSprintInput{
		Name:      "Bad dates",
		StartDate: "2026-09-20",
		EndDate:   "2026-09-07",
	}); !errors.Is(err, ErrValidation) {
		t.Fatalf("inverted dates err=%v", err)
	}

	if _, err := CreateProjectSprintForUser(ctx, 2, proj.ID, CreateProjectSprintInput{
		Name:      "Editor sprint",
		StartDate: "2026-09-07",
		EndDate:   "2026-09-20",
	}); !errors.Is(err, ErrForbidden) && !errors.Is(err, ErrNotFound) {
		// editor is not a member of this project
		t.Fatalf("editor create err=%v", err)
	}

	updated, err := UpdateProjectSprintForUser(ctx, 1, proj.ID, sprint.ID, UpdateProjectSprintInput{
		Name: strPtr("Sprint One"),
	})
	if err != nil {
		t.Fatalf("rename: %v", err)
	}
	if updated.Name != "Sprint One" {
		t.Fatalf("renamed=%q", updated.Name)
	}

	pid := proj.ID
	sid := sprint.ID
	taskID, err := CreateTask(ctx, 1, CreateTaskInput{
		Title:     "In sprint",
		ProjectID: &pid,
		SprintID:  &sid,
	})
	if err != nil {
		t.Fatalf("create task: %v", err)
	}
	fields, err := storage.GetWorkflowFieldsForTasks([]int{taskID})
	if err != nil {
		t.Fatalf("workflow fields: %v", err)
	}
	if fields[taskID].SprintID != sid {
		t.Fatalf("sprint_id=%d want %d", fields[taskID].SprintID, sid)
	}
	if fields[taskID].SprintName != "Sprint One" {
		t.Fatalf("sprint_name=%q", fields[taskID].SprintName)
	}

	clear := (*int)(nil)
	if _, err := UpdateTask(ctx, 1, taskID, UpdateTaskInput{SprintID: &clear}); err != nil {
		t.Fatalf("clear sprint: %v", err)
	}
	fields, _ = storage.GetWorkflowFieldsForTasks([]int{taskID})
	if fields[taskID].SprintID != 0 {
		t.Fatalf("expected backlog, got %d", fields[taskID].SprintID)
	}

	set := &sid
	if _, err := UpdateTask(ctx, 1, taskID, UpdateTaskInput{SprintID: &set}); err != nil {
		t.Fatalf("set sprint: %v", err)
	}

	changed := eventsOfType(t, taskID, 1, "sprint_changed")
	if len(changed) < 2 {
		t.Fatalf("sprint_changed count=%d want >= 2", len(changed))
	}

	backlogID, err := CreateTask(ctx, 1, CreateTaskInput{
		Title:     "Backlog item",
		ProjectID: &pid,
	})
	if err != nil {
		t.Fatalf("create backlog: %v", err)
	}

	tz := "UTC"
	uid := 1
	sprintFilter := sid
	listedTasks, sprintTotal, err := tasks.ReturnPaginationForUserWithFilters(1, 50, &uid, tz, tasks.ListFilters{
		ProjectFilter:      &pid,
		WorkflowClaimScope: "all",
		SprintFilter:       &sprintFilter,
	})
	if err != nil {
		t.Fatalf("filter sprint: %v", err)
	}
	if sprintTotal != 1 {
		t.Fatalf("sprint total=%d want 1", sprintTotal)
	}
	if !sprintListHasID(listedTasks, taskID) || sprintListHasID(listedTasks, backlogID) {
		t.Fatalf("sprint filter mismatch: %+v", sprintTaskIDs(listedTasks))
	}

	zero := 0
	backlogTasks, backlogTotal, err := tasks.ReturnPaginationForUserWithFilters(1, 50, &uid, tz, tasks.ListFilters{
		ProjectFilter:      &pid,
		WorkflowClaimScope: "all",
		SprintFilter:       &zero,
	})
	if err != nil {
		t.Fatalf("filter backlog: %v", err)
	}
	if backlogTotal != 1 {
		t.Fatalf("backlog total=%d want 1", backlogTotal)
	}
	if !sprintListHasID(backlogTasks, backlogID) || sprintListHasID(backlogTasks, taskID) {
		t.Fatalf("backlog filter mismatch: %+v", sprintTaskIDs(backlogTasks))
	}

	sprint2, err := CreateProjectSprintForUser(ctx, 1, proj.ID, CreateProjectSprintInput{
		Name:      "Sprint Two",
		StartDate: "2026-09-07",
		EndDate:   "2026-09-20",
	})
	if err != nil {
		t.Fatalf("create sprint 2: %v", err)
	}
	sid2 := sprint2.ID
	set2 := &sid2
	if _, err := UpdateTask(ctx, 1, taskID, UpdateTaskInput{SprintID: &set2}); err != nil {
		t.Fatalf("move to sprint 2: %v", err)
	}
	fields, _ = storage.GetWorkflowFieldsForTasks([]int{taskID})
	if fields[taskID].SprintID != sid2 {
		t.Fatalf("moved sprint_id=%d want %d", fields[taskID].SprintID, sid2)
	}
	if _, err := UpdateTask(ctx, 1, taskID, UpdateTaskInput{SprintID: &set}); err != nil {
		t.Fatalf("move back to sprint 1: %v", err)
	}

	if err := DeleteProjectSprintForUser(ctx, 1, proj.ID, sprint.ID, nil); err != nil {
		t.Fatalf("delete sprint: %v", err)
	}
	fields, _ = storage.GetWorkflowFieldsForTasks([]int{taskID})
	if fields[taskID].SprintID != 0 {
		t.Fatalf("task should return to backlog after sprint delete, got %d", fields[taskID].SprintID)
	}
}

func TestSprintRequiresKanbanAndValidDates(t *testing.T) {
	ctx := context.Background()
	classic, err := CreateProject(ctx, 1, "Classic No Sprint", "")
	if err != nil {
		t.Fatalf("create classic: %v", err)
	}
	if _, err := CreateProjectSprintForUser(ctx, 1, classic.ID, CreateProjectSprintInput{
		Name:      "Nope",
		StartDate: "2026-08-24",
		EndDate:   "2026-09-06",
	}); !errors.Is(err, ErrValidation) {
		t.Fatalf("classic sprint err=%v", err)
	}

	kanban, err := CreateProject(ctx, 1, "Kanban Sprint Dates", "")
	if err != nil {
		t.Fatalf("create kanban: %v", err)
	}
	if _, err := SetProjectWorkflowMode(ctx, 1, kanban.ID, storage.WorkflowKanban); err != nil {
		t.Fatalf("enable kanban: %v", err)
	}
	if _, err := CreateProjectSprintForUser(ctx, 1, kanban.ID, CreateProjectSprintInput{
		Name:      "Missing dates",
		StartDate: "",
		EndDate:   "2026-09-06",
	}); !errors.Is(err, ErrValidation) {
		t.Fatalf("missing start err=%v", err)
	}

	pid := kanban.ID
	taskID, err := CreateTask(ctx, 1, CreateTaskInput{Title: "No sprint on classic move", ProjectID: &pid})
	if err != nil {
		t.Fatalf("create task: %v", err)
	}
	bad := 999999
	if _, err := UpdateTask(ctx, 1, taskID, UpdateTaskInput{SprintID: ptrToIntPtr(bad)}); !errors.Is(err, ErrValidation) {
		t.Fatalf("invalid sprint_id err=%v", err)
	}
}

func TestProjectSprintDescription(t *testing.T) {
	ctx := context.Background()
	proj, err := CreateProject(ctx, 1, "Sprint Desc Board", "")
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	if _, err := SetProjectWorkflowMode(ctx, 1, proj.ID, storage.WorkflowKanban); err != nil {
		t.Fatalf("enable kanban: %v", err)
	}

	created, err := CreateProjectSprintForUser(ctx, 1, proj.ID, CreateProjectSprintInput{
		Name:        "3.0.0",
		Description: "  features required for v3.0.0 release  ",
		StartDate:   "2026-08-24",
		EndDate:     "2026-09-06",
	})
	if err != nil {
		t.Fatalf("create with description: %v", err)
	}
	if created.Description != "features required for v3.0.0 release" {
		t.Fatalf("create description=%q", created.Description)
	}

	listed, err := ListProjectSprintsForUser(ctx, 1, proj.ID)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(listed) != 1 || listed[0].Description != "features required for v3.0.0 release" {
		t.Fatalf("list description=%v", listed)
	}

	tooLong := strings.Repeat("x", storage.MaxSprintDescriptionLen+1)
	_, err = CreateProjectSprintForUser(ctx, 1, proj.ID, CreateProjectSprintInput{
		Name:        "Too Long",
		Description: tooLong,
		StartDate:   "2026-09-07",
		EndDate:     "2026-09-20",
	})
	if !errors.Is(err, ErrValidation) {
		t.Fatalf("create over-limit: err=%v want validation", err)
	}

	desc := "ship the public API"
	updated, err := UpdateProjectSprintForUser(ctx, 1, proj.ID, created.ID, UpdateProjectSprintInput{
		Description: &desc,
	})
	if err != nil {
		t.Fatalf("update description: %v", err)
	}
	if updated.Description != desc {
		t.Fatalf("updated description=%q want %q", updated.Description, desc)
	}
	if updated.Name != "3.0.0" {
		t.Fatalf("name should be unchanged, got %q", updated.Name)
	}

	over := strings.Repeat("y", storage.MaxSprintDescriptionLen+1)
	_, err = UpdateProjectSprintForUser(ctx, 1, proj.ID, created.ID, UpdateProjectSprintInput{
		Description: &over,
	})
	if !errors.Is(err, ErrValidation) {
		t.Fatalf("update over-limit: err=%v want validation", err)
	}

	empty := ""
	cleared, err := UpdateProjectSprintForUser(ctx, 1, proj.ID, created.ID, UpdateProjectSprintInput{
		Description: &empty,
	})
	if err != nil {
		t.Fatalf("clear description: %v", err)
	}
	if cleared.Description != "" {
		t.Fatalf("cleared description=%q want empty", cleared.Description)
	}

	got, err := storage.GetProjectSprint(proj.ID, created.ID)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if got.Description != "" {
		t.Fatalf("persisted description=%q want empty", got.Description)
	}
}

func utcDay(offset int) string {
	return time.Now().UTC().AddDate(0, 0, offset).Format("2006-01-02")
}

func TestProjectSprintLockDate(t *testing.T) {
	ctx := context.Background()
	proj, err := CreateProject(ctx, 1, "Sprint Lock Board", "")
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	if _, err := SetProjectWorkflowMode(ctx, 1, proj.ID, storage.WorkflowKanban); err != nil {
		t.Fatalf("enable kanban: %v", err)
	}

	created, err := CreateProjectSprintForUser(ctx, 1, proj.ID, CreateProjectSprintInput{
		Name:      "Lock Me",
		StartDate: "2026-08-24",
		EndDate:   "2026-09-06",
		LockDate:  "2026-08-24",
	})
	if err != nil {
		t.Fatalf("create with lock: %v", err)
	}
	if created.LockDate == nil || storage.FormatSprintDate(*created.LockDate) != "2026-08-24" {
		t.Fatalf("create lock_date=%v", created.LockDate)
	}

	listed, err := ListProjectSprintsForUser(ctx, 1, proj.ID)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(listed) != 1 || listed[0].LockDate == nil || storage.FormatSprintDate(*listed[0].LockDate) != "2026-08-24" {
		t.Fatalf("list lock_date=%v", listed)
	}

	if _, err := CreateProjectSprintForUser(ctx, 1, proj.ID, CreateProjectSprintInput{
		Name:      "Bad lock",
		StartDate: "2026-09-07",
		EndDate:   "2026-09-20",
		LockDate:  "not-a-date",
	}); !errors.Is(err, ErrValidation) {
		t.Fatalf("invalid lock_date err=%v", err)
	}

	next := "2026-08-31"
	updated, err := UpdateProjectSprintForUser(ctx, 1, proj.ID, created.ID, UpdateProjectSprintInput{
		LockDate: &next,
	})
	if err != nil {
		t.Fatalf("update lock: %v", err)
	}
	if updated.LockDate == nil || storage.FormatSprintDate(*updated.LockDate) != "2026-08-31" {
		t.Fatalf("updated lock_date=%v", updated.LockDate)
	}
	if updated.Name != "Lock Me" {
		t.Fatalf("name should be unchanged, got %q", updated.Name)
	}

	empty := ""
	cleared, err := UpdateProjectSprintForUser(ctx, 1, proj.ID, created.ID, UpdateProjectSprintInput{
		LockDate: &empty,
	})
	if err != nil {
		t.Fatalf("clear lock: %v", err)
	}
	if cleared.LockDate != nil {
		t.Fatalf("cleared lock_date=%v want nil", cleared.LockDate)
	}

	got, err := storage.GetProjectSprint(proj.ID, created.ID)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if got.LockDate != nil {
		t.Fatalf("persisted lock_date=%v want nil", got.LockDate)
	}
}

func TestSprintLockBlocksNonOwnerAdds(t *testing.T) {
	ctx := context.Background()
	proj, err := CreateProject(ctx, 1, "Locked Sprint Access", "")
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	if _, err := SetProjectWorkflowMode(ctx, 1, proj.ID, storage.WorkflowKanban); err != nil {
		t.Fatalf("enable kanban: %v", err)
	}
	if err := storage.UpsertProjectMember(proj.ID, 2, storage.RoleEditor); err != nil {
		t.Fatalf("add editor: %v", err)
	}

	locked, err := CreateProjectSprintForUser(ctx, 1, proj.ID, CreateProjectSprintInput{
		Name:      "Already Locked",
		StartDate: utcDay(-7),
		EndDate:   utcDay(7),
		LockDate:  utcDay(-1),
	})
	if err != nil {
		t.Fatalf("create locked sprint: %v", err)
	}
	future, err := CreateProjectSprintForUser(ctx, 1, proj.ID, CreateProjectSprintInput{
		Name:      "Locks Later",
		StartDate: utcDay(10),
		EndDate:   utcDay(20),
		LockDate:  utcDay(10),
	})
	if err != nil {
		t.Fatalf("create future-lock sprint: %v", err)
	}
	open, err := CreateProjectSprintForUser(ctx, 1, proj.ID, CreateProjectSprintInput{
		Name:      "Never Locks",
		StartDate: utcDay(21),
		EndDate:   utcDay(30),
	})
	if err != nil {
		t.Fatalf("create unlocked sprint: %v", err)
	}

	pid := proj.ID
	lockedID := locked.ID
	futureID := future.ID
	openID := open.ID

	ownerTask, err := CreateTask(ctx, 1, CreateTaskInput{Title: "Owner in locked", ProjectID: &pid, SprintID: &lockedID})
	if err != nil {
		t.Fatalf("owner add to locked: %v", err)
	}

	_, err = CreateTask(ctx, 2, CreateTaskInput{Title: "Editor in locked", ProjectID: &pid, SprintID: &lockedID})
	if !errors.Is(err, ErrForbidden) {
		t.Fatalf("editor add to locked err=%v", err)
	}
	if err == nil || !strings.Contains(err.Error(), "locked") {
		t.Fatalf("editor add error should mention lock, got %v", err)
	}

	if _, err := CreateTask(ctx, 2, CreateTaskInput{Title: "Editor future lock", ProjectID: &pid, SprintID: &futureID}); err != nil {
		t.Fatalf("editor add before lock date: %v", err)
	}
	openTask, err := CreateTask(ctx, 2, CreateTaskInput{Title: "Editor open", ProjectID: &pid, SprintID: &openID})
	if err != nil {
		t.Fatalf("editor add to unlocked: %v", err)
	}

	if _, err := UpdateTask(ctx, 2, openTask, UpdateTaskInput{SprintID: ptrToIntPtr(lockedID)}); !errors.Is(err, ErrForbidden) {
		t.Fatalf("editor move into locked err=%v", err)
	}

	if _, err := UpdateTask(ctx, 2, ownerTask, UpdateTaskInput{Title: strPtr("still in locked")}); err != nil {
		t.Fatalf("editor edit fields on locked-sprint task: %v", err)
	}
	if _, err := UpdateTask(ctx, 2, ownerTask, UpdateTaskInput{SprintID: ptrToIntPtr(lockedID)}); err != nil {
		t.Fatalf("editor keep current locked sprint: %v", err)
	}

	clear := (*int)(nil)
	if _, err := UpdateTask(ctx, 2, ownerTask, UpdateTaskInput{SprintID: &clear}); err != nil {
		t.Fatalf("editor remove from locked: %v", err)
	}

	parentID, err := CreateTask(ctx, 1, CreateTaskInput{Title: "Locked parent", ProjectID: &pid, SprintID: &lockedID})
	if err != nil {
		t.Fatalf("owner parent: %v", err)
	}
	childID, err := CreateTask(ctx, 2, CreateTaskInput{Title: "Editor child", ParentID: &parentID})
	if err != nil {
		t.Fatalf("editor child of locked parent: %v", err)
	}
	fields, err := storage.GetWorkflowFieldsForTasks([]int{childID})
	if err != nil {
		t.Fatalf("child fields: %v", err)
	}
	if fields[childID].SprintID != 0 {
		t.Fatalf("inherited locked sprint should be skipped for editor, got %d", fields[childID].SprintID)
	}

	openParent, err := CreateTask(ctx, 1, CreateTaskInput{Title: "Open parent", ProjectID: &pid, SprintID: &openID})
	if err != nil {
		t.Fatalf("open parent: %v", err)
	}
	openChild, err := CreateTask(ctx, 2, CreateTaskInput{Title: "Editor open child", ParentID: &openParent})
	if err != nil {
		t.Fatalf("editor child of open parent: %v", err)
	}
	fields, err = storage.GetWorkflowFieldsForTasks([]int{openChild})
	if err != nil {
		t.Fatalf("open child fields: %v", err)
	}
	if fields[openChild].SprintID != openID {
		t.Fatalf("open parent sprint should inherit, got %d want %d", fields[openChild].SprintID, openID)
	}
}

func TestSprintIsActiveWindow(t *testing.T) {
	start, _ := time.Parse("2006-01-02", "2026-08-24")
	end, _ := time.Parse("2006-01-02", "2026-09-06")
	now, _ := time.Parse(time.RFC3339, "2026-08-24T18:00:00Z")
	if !storage.SprintIsActive(&start, &end, now) {
		t.Fatal("expected active on start date")
	}
	before, _ := time.Parse(time.RFC3339, "2026-08-23T23:00:00Z")
	if storage.SprintIsActive(&start, &end, before) {
		t.Fatal("expected inactive before start")
	}
	after, _ := time.Parse(time.RFC3339, "2026-09-07T00:00:00Z")
	if storage.SprintIsActive(&start, &end, after) {
		t.Fatal("expected inactive after end")
	}
	if storage.SprintIsActive(nil, nil, now) {
		t.Fatal("dateless sprint should never be active by date")
	}
}

func TestSprintIsLockedWindow(t *testing.T) {
	lock, _ := time.Parse("2006-01-02", "2026-08-25")
	if storage.SprintIsLocked(nil, lock) {
		t.Fatal("nil lock date should not lock")
	}
	on, _ := time.Parse(time.RFC3339, "2026-08-25T00:00:00Z")
	if !storage.SprintIsLocked(&lock, on) {
		t.Fatal("expected locked on lock date")
	}
	before, _ := time.Parse(time.RFC3339, "2026-08-24T23:59:59Z")
	if storage.SprintIsLocked(&lock, before) {
		t.Fatal("expected unlocked before lock date")
	}
	after, _ := time.Parse(time.RFC3339, "2026-08-26T00:00:00Z")
	if !storage.SprintIsLocked(&lock, after) {
		t.Fatal("expected locked after lock date")
	}
}

func TestSprintDatesOverlap(t *testing.T) {
	parse := func(s string) time.Time {
		d, err := time.Parse("2006-01-02", s)
		if err != nil {
			t.Fatal(err)
		}
		return d
	}
	if !storage.SprintDatesOverlap(parse("2026-08-01"), parse("2026-08-15"), parse("2026-08-15"), parse("2026-08-31")) {
		t.Fatal("sharing an endpoint day should overlap")
	}
	if storage.SprintDatesOverlap(parse("2026-08-01"), parse("2026-08-14"), parse("2026-08-15"), parse("2026-08-31")) {
		t.Fatal("adjacent ranges should not overlap")
	}
	if !storage.SprintDatesOverlap(parse("2026-08-01"), parse("2026-08-31"), parse("2026-08-10"), parse("2026-08-12")) {
		t.Fatal("contained range should overlap")
	}
}

func TestSprintRejectsOverlappingDates(t *testing.T) {
	ctx := context.Background()
	proj, err := CreateProject(ctx, 1, "No Overlap Board", "")
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	if _, err := SetProjectWorkflowMode(ctx, 1, proj.ID, storage.WorkflowKanban); err != nil {
		t.Fatalf("enable kanban: %v", err)
	}
	first, err := CreateProjectSprintForUser(ctx, 1, proj.ID, CreateProjectSprintInput{
		Name:      "Sprint A",
		StartDate: "2026-08-01",
		EndDate:   "2026-08-14",
	})
	if err != nil {
		t.Fatalf("create first: %v", err)
	}
	if _, err := CreateProjectSprintForUser(ctx, 1, proj.ID, CreateProjectSprintInput{
		Name:      "Sprint B",
		StartDate: "2026-08-15",
		EndDate:   "2026-08-31",
	}); err != nil {
		t.Fatalf("adjacent sprint should be allowed: %v", err)
	}
	_, err = CreateProjectSprintForUser(ctx, 1, proj.ID, CreateProjectSprintInput{
		Name:      "Overlap",
		StartDate: "2026-08-10",
		EndDate:   "2026-08-20",
	})
	if !errors.Is(err, ErrValidation) {
		t.Fatalf("overlapping create err=%v", err)
	}
	if err == nil || !strings.Contains(err.Error(), "Sprint A") {
		t.Fatalf("overlap error should name the other sprint, got %v", err)
	}
	_, err = UpdateProjectSprintForUser(ctx, 1, proj.ID, first.ID, UpdateProjectSprintInput{
		EndDate: strPtr("2026-08-20"),
	})
	if !errors.Is(err, ErrValidation) {
		t.Fatalf("overlapping update err=%v", err)
	}
	if _, err := UpdateProjectSprintForUser(ctx, 1, proj.ID, first.ID, UpdateProjectSprintInput{
		EndDate: strPtr("2026-08-14"),
	}); err != nil {
		t.Fatalf("updating a sprint to its own range should be allowed: %v", err)
	}
}

func TestSubtaskInheritsParentSprint(t *testing.T) {
	ctx := context.Background()
	proj, err := CreateProject(ctx, 1, "Inherit Sprint", "")
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	if _, err := SetProjectWorkflowMode(ctx, 1, proj.ID, storage.WorkflowKanban); err != nil {
		t.Fatalf("enable kanban: %v", err)
	}
	sprint, err := CreateProjectSprintForUser(ctx, 1, proj.ID, CreateProjectSprintInput{
		Name:      "Inherit Me",
		StartDate: "2026-08-01",
		EndDate:   "2026-08-31",
	})
	if err != nil {
		t.Fatalf("create sprint: %v", err)
	}
	pid := proj.ID
	sid := sprint.ID
	parentID, err := CreateTask(ctx, 1, CreateTaskInput{Title: "Parent", ProjectID: &pid, SprintID: &sid})
	if err != nil {
		t.Fatalf("create parent: %v", err)
	}
	childID, err := CreateTask(ctx, 1, CreateTaskInput{Title: "Child", ParentID: &parentID})
	if err != nil {
		t.Fatalf("create child: %v", err)
	}
	fields, err := storage.GetWorkflowFieldsForTasks([]int{childID})
	if err != nil {
		t.Fatalf("fields: %v", err)
	}
	if fields[childID].SprintID != sid {
		t.Fatalf("child sprint_id=%d want %d", fields[childID].SprintID, sid)
	}
}

func TestSprintBoardSeparatesParentAndChildAssignments(t *testing.T) {
	ctx := context.Background()
	proj, err := CreateProject(ctx, 1, "Mixed Sprint Board", "")
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	if _, err := SetProjectWorkflowMode(ctx, 1, proj.ID, storage.WorkflowKanban); err != nil {
		t.Fatalf("enable kanban: %v", err)
	}
	sprint, err := CreateProjectSprintForUser(ctx, 1, proj.ID, CreateProjectSprintInput{
		Name:      "Sprint 1",
		StartDate: "2026-08-01",
		EndDate:   "2026-08-31",
	})
	if err != nil {
		t.Fatalf("create sprint: %v", err)
	}
	pid := proj.ID
	sid := sprint.ID
	parentID, err := CreateTask(ctx, 1, CreateTaskInput{Title: "Kanban Project 2", ProjectID: &pid})
	if err != nil {
		t.Fatalf("create parent: %v", err)
	}
	childID, err := CreateTask(ctx, 1, CreateTaskInput{Title: "Sub task", ProjectID: &pid, ParentID: &parentID, SprintID: &sid})
	if err != nil {
		t.Fatalf("create child: %v", err)
	}

	tz := "UTC"
	uid := 1
	zero := 0
	backlog, backlogTotal, err := tasks.ReturnPaginationForUserWithFilters(1, 50, &uid, tz, tasks.ListFilters{
		ProjectFilter:      &pid,
		WorkflowClaimScope: "all",
		SprintFilter:       &zero,
	})
	if err != nil {
		t.Fatalf("backlog list: %v", err)
	}
	if backlogTotal != 1 {
		t.Fatalf("backlog total=%d want 1", backlogTotal)
	}
	parent := sprintTopLevel(backlog, parentID)
	if parent == nil {
		t.Fatalf("backlog missing parent: %v", sprintTaskIDs(backlog))
	}
	if sprintNestedHasID(*parent, childID) {
		t.Fatal("sprint-1 subtask should not nest under a backlog parent")
	}
	if sprintTopLevel(backlog, childID) != nil {
		t.Fatal("sprint-1 subtask should not appear as a backlog card")
	}

	sprintTasks, sprintTotal, err := tasks.ReturnPaginationForUserWithFilters(1, 50, &uid, tz, tasks.ListFilters{
		ProjectFilter:      &pid,
		WorkflowClaimScope: "all",
		SprintFilter:       &sid,
	})
	if err != nil {
		t.Fatalf("sprint list: %v", err)
	}
	if sprintTotal != 1 {
		t.Fatalf("sprint total=%d want 1 (orphan child included)", sprintTotal)
	}
	if sprintTopLevel(sprintTasks, parentID) != nil {
		t.Fatal("backlog parent should not appear on sprint 1")
	}
	child := sprintTopLevel(sprintTasks, childID)
	if child == nil {
		t.Fatalf("sprint-1 missing subtask card: %v", sprintTaskIDs(sprintTasks))
	}
	if child.ParentID != parentID {
		t.Fatalf("subtask parent_id=%d want %d", child.ParentID, parentID)
	}
	if child.ParentTitle != "Kanban Project 2" {
		t.Fatalf("subtask parent_title=%q", child.ParentTitle)
	}

	searchedSprint, searchedSprintTotal, err := tasks.SearchTasksForUserWithFilters(1, 50, "Sub task", &uid, tz, tasks.ListFilters{
		ProjectFilter:      &pid,
		WorkflowClaimScope: "all",
		SprintFilter:       &sid,
	})
	if err != nil {
		t.Fatalf("search sprint: %v", err)
	}
	if searchedSprintTotal != 1 || len(searchedSprint) != 1 {
		t.Fatalf("search sprint total=%d (tasks=%d) want 1", searchedSprintTotal, len(searchedSprint))
	}
}

func strPtr(s string) *string { return &s }

func ptrToIntPtr(v int) **int {
	p := &v
	return &p
}

func sprintListHasID(list []tasks.Task, id int) bool {
	for _, t := range list {
		if t.ID == id {
			return true
		}
		for _, c := range t.Children {
			if c.ID == id {
				return true
			}
		}
	}
	return false
}

func sprintTopLevel(list []tasks.Task, id int) *tasks.Task {
	for i := range list {
		if list[i].ID == id {
			return &list[i]
		}
	}
	return nil
}

func sprintNestedHasID(parent tasks.Task, id int) bool {
	for _, c := range parent.Children {
		if c.ID == id {
			return true
		}
	}
	return false
}

func sprintTaskIDs(list []tasks.Task) []int {
	ids := make([]int, 0, len(list))
	for _, t := range list {
		ids = append(ids, t.ID)
	}
	return ids
}

func TestProjectBacklogSprintRename(t *testing.T) {
	ctx := context.Background()
	proj, err := CreateProject(ctx, 1, "Backlog Rename Board", "")
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	if _, err := SetProjectWorkflowMode(ctx, 1, proj.ID, storage.WorkflowKanban); err != nil {
		t.Fatalf("enable kanban: %v", err)
	}

	// Initial backlog name should be default "Backlog"
	gotProj, err := storage.GetProjectByID(proj.ID, 1)
	if err != nil {
		t.Fatalf("get project: %v", err)
	}
	if gotProj.BacklogName != "Backlog" {
		t.Fatalf("initial BacklogName = %q, want 'Backlog'", gotProj.BacklogName)
	}

	bn, err := storage.GetProjectBacklogName(proj.ID)
	if err != nil {
		t.Fatalf("GetProjectBacklogName: %v", err)
	}
	if bn != "Backlog" {
		t.Fatalf("initial GetProjectBacklogName = %q, want 'Backlog'", bn)
	}

	// Rename backlog to "Icebox"
	updatedProj, err := RenameProjectBacklog(ctx, 1, proj.ID, "Icebox")
	if err != nil {
		t.Fatalf("RenameProjectBacklog: %v", err)
	}
	if updatedProj.BacklogName != "Icebox" {
		t.Fatalf("updated BacklogName = %q, want 'Icebox'", updatedProj.BacklogName)
	}

	bn, err = storage.GetProjectBacklogName(proj.ID)
	if err != nil {
		t.Fatalf("GetProjectBacklogName after rename: %v", err)
	}
	if bn != "Icebox" {
		t.Fatalf("bn = %q, want 'Icebox'", bn)
	}

	// Create a sprint
	sprint, err := CreateProjectSprintForUser(ctx, 1, proj.ID, CreateProjectSprintInput{
		Name:      "Sprint Beta",
		StartDate: "2026-10-01",
		EndDate:   "2026-10-14",
	})
	if err != nil {
		t.Fatalf("create sprint: %v", err)
	}

	// Create a task in the project (defaults to backlog, sprint_id=0)
	taskID, err := CreateTask(ctx, 1, CreateTaskInput{
		Title:     "Item to move",
		ProjectID: &proj.ID,
	})
	if err != nil {
		t.Fatalf("create task: %v", err)
	}

	// Move task from backlog to Sprint Beta
	spID := sprint.ID
	if _, err := UpdateTask(ctx, 1, taskID, UpdateTaskInput{SprintID: ptrToIntPtr(spID)}); err != nil {
		t.Fatalf("move to sprint: %v", err)
	}

	// Verify events show "from": "Icebox"
	evs, err := storage.GetEventsForTask(taskID, 1, 50)
	if err != nil {
		t.Fatalf("list events: %v", err)
	}
	var foundSprintChange bool
	for _, ev := range evs {
		if ev.EventType == "sprint_changed" {
			foundSprintChange = true
			if ev.Metadata["from"] != "Icebox" {
				t.Fatalf("sprint_changed from = %v, want 'Icebox'", ev.Metadata["from"])
			}
			if ev.Metadata["to"] != "Sprint Beta" {
				t.Fatalf("sprint_changed to = %v, want 'Sprint Beta'", ev.Metadata["to"])
			}
		}
	}
	if !foundSprintChange {
		t.Fatal("expected sprint_changed event")
	}

	// Move task back to backlog (0)
	zero := 0
	if _, err := UpdateTask(ctx, 1, taskID, UpdateTaskInput{SprintID: ptrToIntPtr(zero)}); err != nil {
		t.Fatalf("move back to backlog: %v", err)
	}

	evs, err = storage.GetEventsForTask(taskID, 1, 50)
	if err != nil {
		t.Fatalf("list events: %v", err)
	}
	if evs[0].EventType != "sprint_changed" {
		t.Fatalf("latest event = %s, want sprint_changed", evs[0].EventType)
	}
	if evs[0].Metadata["to"] != "Icebox" {
		t.Fatalf("sprint_changed to = %v, want 'Icebox'", evs[0].Metadata["to"])
	}
}

func TestDatelessSprintsAndDescriptions(t *testing.T) {
	ctx := context.Background()
	proj, err := CreateProject(ctx, 1, "Kanban Board With Icebox", "")
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	if _, err := SetProjectWorkflowMode(ctx, 1, proj.ID, storage.WorkflowKanban); err != nil {
		t.Fatalf("enable kanban: %v", err)
	}

	// 1. Backlog description can be set on project
	backlogDesc := "Items waiting for triage"
	updatedProj, err := UpdateProject(ctx, 1, proj.ID, nil, nil, nil, nil, &backlogDesc)
	if err != nil {
		t.Fatalf("set backlog description: %v", err)
	}
	if updatedProj.BacklogDescription != backlogDesc {
		t.Fatalf("backlog description = %q, want %q", updatedProj.BacklogDescription, backlogDesc)
	}

	// 2. Reject incomplete dates (only start_date or only end_date)
	if _, err := CreateProjectSprintForUser(ctx, 1, proj.ID, CreateProjectSprintInput{
		Name:      "Incomplete Sprint 1",
		StartDate: "2026-10-01",
	}); !errors.Is(err, ErrValidation) {
		t.Fatalf("expected ErrValidation for start_date only, got %v", err)
	}
	if _, err := CreateProjectSprintForUser(ctx, 1, proj.ID, CreateProjectSprintInput{
		Name:    "Incomplete Sprint 2",
		EndDate: "2026-10-15",
	}); !errors.Is(err, ErrValidation) {
		t.Fatalf("expected ErrValidation for end_date only, got %v", err)
	}

	// 3. Reject lock_date on dateless sprint
	if _, err := CreateProjectSprintForUser(ctx, 1, proj.ID, CreateProjectSprintInput{
		Name:     "Bad Dateless Sprint",
		LockDate: "2026-10-01",
	}); !errors.Is(err, ErrValidation) {
		t.Fatalf("expected ErrValidation for lock_date on dateless sprint, got %v", err)
	}

	// 4. Reject sprint description exceeding 80 chars
	tooLongDesc := strings.Repeat("x", storage.MaxSprintDescriptionLen+1)
	if _, err := CreateProjectSprintForUser(ctx, 1, proj.ID, CreateProjectSprintInput{
		Name:        "Long Desc Sprint",
		Description: tooLongDesc,
	}); !errors.Is(err, ErrValidation) {
		t.Fatalf("expected ErrValidation for description > 80 chars, got %v", err)
	}

	// 5. Create a dateless sprint (e.g. Icebox)
	icebox, err := CreateProjectSprintForUser(ctx, 1, proj.ID, CreateProjectSprintInput{
		Name:        "Icebox",
		Description: "Ideas and deprioritized work",
	})
	if err != nil {
		t.Fatalf("create dateless sprint: %v", err)
	}
	if icebox.Name != "Icebox" {
		t.Fatalf("name = %q, want 'Icebox'", icebox.Name)
	}
	if icebox.Description != "Ideas and deprioritized work" {
		t.Fatalf("description = %q", icebox.Description)
	}
	if icebox.StartDate != nil || icebox.EndDate != nil {
		t.Fatalf("expected nil dates for dateless sprint, got %v, %v", icebox.StartDate, icebox.EndDate)
	}
	if icebox.LockDate != nil {
		t.Fatalf("expected nil lock_date for dateless sprint, got %v", icebox.LockDate)
	}

	// 6. Create another dateless sprint (e.g. Someday/Maybe) - multiple dateless sprints coexist
	someday, err := CreateProjectSprintForUser(ctx, 1, proj.ID, CreateProjectSprintInput{
		Name:        "Someday",
		Description: "Blue sky research",
	})
	if err != nil {
		t.Fatalf("create second dateless sprint: %v", err)
	}
	if someday.StartDate != nil || someday.EndDate != nil {
		t.Fatalf("expected nil dates, got %v, %v", someday.StartDate, someday.EndDate)
	}

	// 7. Create a dated sprint
	dated, err := CreateProjectSprintForUser(ctx, 1, proj.ID, CreateProjectSprintInput{
		Name:      "Sprint 1",
		StartDate: "2026-10-01",
		EndDate:   "2026-10-14",
	})
	if err != nil {
		t.Fatalf("create dated sprint: %v", err)
	}

	// 8. List sprints: dateless sprints appear first, then dated sprints
	sprints, err := ListProjectSprintsForUser(ctx, 1, proj.ID)
	if err != nil {
		t.Fatalf("list sprints: %v", err)
	}
	if len(sprints) != 3 {
		t.Fatalf("expected 3 sprints, got %d", len(sprints))
	}
	// First two should be dateless (Someday id > Icebox id), third should be dated
	if sprints[0].StartDate != nil || sprints[1].StartDate != nil {
		t.Fatalf("first two sprints should be dateless, got %v, %v", sprints[0].StartDate, sprints[1].StartDate)
	}
	if sprints[2].StartDate == nil {
		t.Fatal("third sprint should be dated")
	}

	// 9. Assign a task to the dateless sprint
	pid := proj.ID
	iceboxID := icebox.ID
	taskID, err := CreateTask(ctx, 1, CreateTaskInput{
		Title:     "Icebox item",
		ProjectID: &pid,
		SprintID:  &iceboxID,
	})
	if err != nil {
		t.Fatalf("create task in icebox: %v", err)
	}
	assignedSprintID, err := storage.GetTaskSprintID(taskID)
	if err != nil || assignedSprintID != iceboxID {
		t.Fatalf("task sprint_id = %d, want %d", assignedSprintID, iceboxID)
	}

	// 10. Update dateless sprint description and name
	newDesc := "Updated icebox description"
	newName := "Deep Freeze"
	updated, err := UpdateProjectSprintForUser(ctx, 1, proj.ID, iceboxID, UpdateProjectSprintInput{
		Name:        &newName,
		Description: &newDesc,
	})
	if err != nil {
		t.Fatalf("update dateless sprint: %v", err)
	}
	if updated.Name != "Deep Freeze" || updated.Description != newDesc {
		t.Fatalf("updated = (%q, %q)", updated.Name, updated.Description)
	}
	if updated.StartDate != nil || updated.EndDate != nil {
		t.Fatalf("expected dates to remain nil, got %v, %v", updated.StartDate, updated.EndDate)
	}

	// 11. Reject setting lock_date when updating a dateless sprint
	badLock := "2026-10-15"
	if _, err := UpdateProjectSprintForUser(ctx, 1, proj.ID, iceboxID, UpdateProjectSprintInput{
		LockDate: &badLock,
	}); !errors.Is(err, ErrValidation) {
		t.Fatalf("expected ErrValidation when adding lock_date to dateless sprint, got %v", err)
	}

	// 12. Convert dateless sprint to dated sprint
	newStart := "2026-10-15"
	newEnd := "2026-10-28"
	datedConverted, err := UpdateProjectSprintForUser(ctx, 1, proj.ID, iceboxID, UpdateProjectSprintInput{
		StartDate: &newStart,
		EndDate:   &newEnd,
	})
	if err != nil {
		t.Fatalf("convert to dated: %v", err)
	}
	if datedConverted.StartDate == nil || storage.FormatSprintDatePtr(datedConverted.StartDate) != "2026-10-15" {
		t.Fatalf("converted start_date = %v", datedConverted.StartDate)
	}

	// 13. Convert dated sprint to dateless sprint
	emptyStart := ""
	emptyEnd := ""
	datelessConverted, err := UpdateProjectSprintForUser(ctx, 1, proj.ID, dated.ID, UpdateProjectSprintInput{
		StartDate: &emptyStart,
		EndDate:   &emptyEnd,
	})
	if err != nil {
		t.Fatalf("convert dated to dateless: %v", err)
	}
	if datelessConverted.StartDate != nil || datelessConverted.EndDate != nil {
		t.Fatalf("expected nil dates after converting to dateless, got %v, %v", datelessConverted.StartDate, datelessConverted.EndDate)
	}
}

