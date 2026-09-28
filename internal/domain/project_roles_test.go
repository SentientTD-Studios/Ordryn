package domain

import (
	"context"
	"errors"
	"testing"

	"GoTodo/internal/storage"
)

func TestSiteRoleDefaultsAndPermissions(t *testing.T) {
	if err := storage.SeedDefaultProjectRoles(); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if !storage.HasProjectPerm(0, storage.RoleOwner, storage.PermProjectManage) {
		t.Fatal("owner should manage")
	}
	if storage.HasProjectPerm(0, storage.RoleEditor, storage.PermProjectManage) {
		t.Fatal("editor should not manage")
	}
	if storage.HasProjectPerm(0, storage.RoleQA, storage.PermTasksCreate) {
		t.Fatal("qa should not create tasks")
	}
	if !storage.HasProjectPerm(0, storage.RoleQA, storage.PermTasksStatus) {
		t.Fatal("qa should change status")
	}
	if !storage.HasProjectPerm(0, storage.RoleDeveloper, storage.PermTasksDelete) {
		t.Fatal("developer should delete")
	}
	if storage.RoleCanWrite(storage.RoleViewer) {
		t.Fatal("viewer should not write")
	}
	if !storage.RoleCanWriteTask(0, storage.RoleQA) {
		t.Fatal("qa should be a write role")
	}
	if !storage.ValidInviteRole(storage.RoleQA) || !storage.ValidInviteRole(storage.RoleDeveloper) {
		t.Fatal("qa/developer should be inviteable")
	}
	if storage.ValidInviteRole(storage.RoleOwner) {
		t.Fatal("owner should not be inviteable")
	}
}

func TestQACannotCreateOrDeleteButCanMoveStatus(t *testing.T) {
	ctx := context.Background()
	ownerID := 1
	qaID := 2
	proj, err := CreateProject(ctx, ownerID, "QA Board", "")
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	if _, err := SetProjectWorkflowMode(ctx, ownerID, proj.ID, storage.WorkflowKanban); err != nil {
		t.Fatalf("kanban: %v", err)
	}
	if err := storage.UpsertProjectMember(proj.ID, qaID, storage.RoleQA); err != nil {
		t.Fatalf("add qa: %v", err)
	}

	pid := proj.ID
	if _, err := CreateTask(ctx, qaID, CreateTaskInput{Title: "QA created", ProjectID: &pid}); !errors.Is(err, ErrForbidden) {
		t.Fatalf("qa create: err=%v want forbidden", err)
	}

	taskID, err := CreateTask(ctx, ownerID, CreateTaskInput{Title: "Owned", ProjectID: &pid})
	if err != nil {
		t.Fatalf("owner create: %v", err)
	}
	if err := DeleteTask(ctx, qaID, taskID); !errors.Is(err, ErrForbidden) {
		t.Fatalf("qa delete: err=%v want forbidden", err)
	}

	statuses, err := ListProjectStatusesForUser(ctx, ownerID, pid)
	if err != nil || len(statuses) < 2 {
		t.Fatalf("statuses: %v n=%d", err, len(statuses))
	}
	to := statuses[len(statuses)-1].ID
	statusPtr := &to
	if _, err := UpdateTask(ctx, qaID, taskID, UpdateTaskInput{StatusID: &statusPtr}); err != nil {
		t.Fatalf("qa status move: %v", err)
	}

	title := "nope"
	if _, err := UpdateTask(ctx, qaID, taskID, UpdateTaskInput{Title: &title}); !errors.Is(err, ErrForbidden) {
		t.Fatalf("qa edit details: err=%v want forbidden", err)
	}
}

func TestStatusGatesRestrictEnterAndLeave(t *testing.T) {
	ctx := context.Background()
	ownerID := 1
	devID := 2
	qaID := 3
	proj, err := CreateProject(ctx, ownerID, "Gated Board", "")
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	if _, err := SetProjectWorkflowMode(ctx, ownerID, proj.ID, storage.WorkflowKanban); err != nil {
		t.Fatalf("kanban: %v", err)
	}
	if err := storage.UpsertProjectMember(proj.ID, devID, storage.RoleDeveloper); err != nil {
		t.Fatalf("add developer: %v", err)
	}
	if err := storage.UpsertProjectMember(proj.ID, qaID, storage.RoleQA); err != nil {
		t.Fatalf("add qa: %v", err)
	}

	inQA, err := CreateProjectStatusForUser(ctx, ownerID, proj.ID, CreateProjectStatusInput{Name: "In QA"})
	if err != nil {
		t.Fatalf("create In QA: %v", err)
	}
	if _, err := UpdateStatusGatesForUser(ctx, ownerID, proj.ID, inQA.ID, []string{storage.RoleQA}, []string{storage.RoleQA}); err != nil {
		t.Fatalf("set gates: %v", err)
	}

	pid := proj.ID
	taskID, err := CreateTask(ctx, ownerID, CreateTaskInput{Title: "Needs QA", ProjectID: &pid})
	if err != nil {
		t.Fatalf("create task: %v", err)
	}
	qaStatus := inQA.ID
	qaPtr := &qaStatus
	if _, err := UpdateTask(ctx, devID, taskID, UpdateTaskInput{StatusID: &qaPtr}); !errors.Is(err, ErrForbidden) {
		t.Fatalf("developer enter In QA: err=%v want forbidden", err)
	}
	if _, err := UpdateTask(ctx, qaID, taskID, UpdateTaskInput{StatusID: &qaPtr}); err != nil {
		t.Fatalf("qa enter In QA: %v", err)
	}

	statuses, err := ListProjectStatusesForUser(ctx, ownerID, pid)
	if err != nil {
		t.Fatalf("list statuses: %v", err)
	}
	var otherID int
	for _, s := range statuses {
		if s.ID != inQA.ID {
			otherID = s.ID
			break
		}
	}
	if otherID == 0 {
		t.Fatal("expected another status")
	}
	otherPtr := &otherID
	if _, err := UpdateTask(ctx, devID, taskID, UpdateTaskInput{StatusID: &otherPtr}); !errors.Is(err, ErrForbidden) {
		t.Fatalf("developer leave In QA: err=%v want forbidden", err)
	}
	if _, err := UpdateTask(ctx, ownerID, taskID, UpdateTaskInput{StatusID: &otherPtr}); err != nil {
		t.Fatalf("owner leave In QA: %v", err)
	}
}

func TestProjectCustomRoleAndDiscussionLabel(t *testing.T) {
	ctx := context.Background()
	ownerID := 1
	memberID := 2
	proj, err := CreateProject(ctx, ownerID, "Custom Roles", "")
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	created, err := CreateProjectCustomRoleForUser(ctx, ownerID, proj.ID, CreateSiteProjectRoleInput{
		Slug:        "developer-ii",
		Name:        "Developer II",
		Permissions: []string{storage.PermTasksCreate, storage.PermTasksEdit, storage.PermTasksStatus},
	})
	if err != nil {
		t.Fatalf("create custom role: %v", err)
	}
	if created.Slug != "developer-ii" || created.ProjectID == nil || *created.ProjectID != proj.ID {
		t.Fatalf("custom role: %+v", created)
	}
	if err := storage.UpsertProjectMember(proj.ID, memberID, "developer-ii"); err != nil {
		t.Fatalf("assign custom role: %v", err)
	}

	pid := proj.ID
	taskID, err := CreateTask(ctx, memberID, CreateTaskInput{Title: "From Dev II", ProjectID: &pid})
	if err != nil {
		t.Fatalf("custom role create: %v", err)
	}
	setTestUsername(t, memberID, "Dev")
	if err := DeleteTask(ctx, memberID, taskID); !errors.Is(err, ErrForbidden) {
		t.Fatalf("custom role delete: err=%v want forbidden", err)
	}

	comment, err := AddCommentForUser(ctx, memberID, taskID, "Checking the build")
	if err != nil {
		t.Fatalf("comment: %v", err)
	}
	if comment.AuthorRole != "developer-ii" || comment.AuthorRoleName != "Developer II" {
		t.Fatalf("comment role label: role=%q name=%q", comment.AuthorRole, comment.AuthorRoleName)
	}

	listed, err := ListCommentsForUser(ctx, ownerID, taskID)
	if err != nil || len(listed) != 1 {
		t.Fatalf("list comments: %v n=%d", err, len(listed))
	}
	if listed[0].AuthorRoleName != "Developer II" {
		t.Fatalf("listed role name: %q", listed[0].AuthorRoleName)
	}
}
