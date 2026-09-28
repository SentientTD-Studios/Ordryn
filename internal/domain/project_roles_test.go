package domain

import (
	"context"
	"errors"
	"testing"
	"time"

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

func TestCopyAndReorderSiteRoles(t *testing.T) {
	ctx := context.Background()
	listed, err := storage.ListSiteProjectRoles()
	if err != nil || len(listed) < 2 {
		t.Fatalf("list site roles: %v n=%d", err, len(listed))
	}
	qa := listed[0]
	for _, d := range listed {
		if d.Slug == storage.RoleQA {
			qa = d
			break
		}
	}
	copied, err := storage.CreateProjectRoleDef(nil, "qa-copy-test", qa.Name+" (copy)", qa.Description, qa.Permissions, false, 50)
	if err != nil {
		t.Fatalf("copy role: %v", err)
	}
	ids := []int{copied.ID}
	for _, d := range listed {
		ids = append(ids, d.ID)
	}
	if err := storage.ReorderProjectRoleDefs(ids, true, 0, 0); err != nil {
		t.Fatalf("reorder: %v", err)
	}
	after, err := storage.ListSiteProjectRoles()
	if err != nil || len(after) == 0 || after[0].ID != copied.ID {
		t.Fatalf("reorder result first=%v err=%v", after, err)
	}
	if err := storage.DeleteProjectRoleDef(copied.ID); err != nil {
		t.Fatalf("cleanup copy: %v", err)
	}
	_ = ctx
}

func TestOrgImportCopiesMembersAndAllowsProjectEdits(t *testing.T) {
	ctx := context.Background()
	setTestUsername(t, 2, "editor_user")
	setTestUsername(t, 3, "viewer_user")
	org, err := CreateOrganizationForUser(ctx, 1, "Acme Org", "team")
	if err != nil {
		t.Fatalf("create org: %v", err)
	}
	if _, err := InviteToOrganization(ctx, 1, org.ID, "editor_user", storage.RoleEditor); err != nil {
		t.Fatalf("invite org member: %v", err)
	}
	if role, err := storage.GetOrganizationRole(org.ID, 2); err != nil || role != "" {
		t.Fatalf("pending invite should not add membership yet: %q err=%v", role, err)
	}
	invites, err := storage.ListPendingOrganizationInvitesForEmail("editor@example.com")
	if err != nil || len(invites) != 1 {
		t.Fatalf("pending invites: %+v err=%v", invites, err)
	}
	if err := AcceptOrganizationInviteForUser(ctx, 2, "editor@example.com", invites[0].ID); err != nil {
		t.Fatalf("accept org invite: %v", err)
	}
	orgRole, err := CreateOrganizationRoleForUser(ctx, 1, org.ID, CreateSiteProjectRoleInput{
		Slug:        "org-qa",
		Name:        "Org QA",
		Permissions: []string{storage.PermTasksStatus, storage.PermTasksComplete},
	})
	if err != nil {
		t.Fatalf("org role: %v", err)
	}
	if orgRole.OrganizationID == nil || *orgRole.OrganizationID != org.ID {
		t.Fatalf("org role scope: %+v", orgRole)
	}

	copied, err := CreateOrganizationRoleForUser(ctx, 1, org.ID, CreateSiteProjectRoleInput{
		Slug:       "org-qa-copy",
		CopyFromID: orgRole.ID,
	})
	if err != nil {
		t.Fatalf("copy org role: %v", err)
	}
	if len(copied.Permissions) != len(orgRole.Permissions) {
		t.Fatalf("copied org perms")
	}

	proj, err := CreateProjectForUser(ctx, 1, CreateProjectInput{
		Name:           "Org Board",
		OrganizationID: &org.ID,
	})
	if err != nil {
		t.Fatalf("create org project: %v", err)
	}
	if proj.OrgManaged || proj.OrganizationID == nil || *proj.OrganizationID != org.ID {
		t.Fatalf("expected unlocked org import: %+v", proj)
	}

	role, err := storage.GetProjectRole(proj.ID, 2)
	if err != nil || role != storage.RoleEditor {
		t.Fatalf("imported role: %q err=%v", role, err)
	}
	if _, err := storage.GetAccessibleProjectByID(proj.ID, 2); err != nil {
		t.Fatalf("editor should access imported project: %v", err)
	}
	members, err := storage.ListProjectMembers(proj.ID)
	if err != nil {
		t.Fatalf("list members: %v", err)
	}
	var sawEditor bool
	for _, m := range members {
		if m.UserID == 2 && !m.Inherited && m.Role == storage.RoleEditor {
			sawEditor = true
		}
	}
	if !sawEditor {
		t.Fatalf("expected copied editor membership, got %+v", members)
	}

	if _, err := InviteToProject(ctx, 1, proj.ID, "viewer_user", storage.RoleViewer); err != nil {
		t.Fatalf("invite on imported project: %v", err)
	}
	custom, err := CreateProjectCustomRoleForUser(ctx, 1, proj.ID, CreateSiteProjectRoleInput{
		Slug: "project-only", Name: "Board Only", Permissions: []string{storage.PermTasksEdit},
	})
	if err != nil {
		t.Fatalf("custom role on imported project: %v", err)
	}
	if custom.ProjectID == nil || *custom.ProjectID != proj.ID {
		t.Fatalf("project custom role scope: %+v", custom)
	}
	if err := UpdateProjectMemberRole(ctx, 1, proj.ID, 2, "project-only"); err != nil {
		t.Fatalf("change imported member role: %v", err)
	}

	pid := proj.ID
	if _, err := CreateTask(ctx, 2, CreateTaskInput{Title: "From project-only", ProjectID: &pid}); !errors.Is(err, ErrForbidden) {
		t.Fatalf("project-only create: err=%v want forbidden", err)
	}

	if err := storage.UpsertOrganizationMember(org.ID, 2, "org-qa"); err != nil {
		t.Fatalf("change org role: %v", err)
	}
	got, err := storage.GetProjectRole(proj.ID, 2)
	if err != nil || got != "project-only" {
		t.Fatalf("org role change should not overwrite project role: %q err=%v", got, err)
	}

	if err := storage.UpsertOrganizationMember(org.ID, 3, storage.RoleViewer); err != nil {
		t.Fatalf("add later org member: %v", err)
	}
	if _, err := storage.GetAccessibleProjectByID(proj.ID, 3); err == nil {
		t.Fatal("later org member should not auto-join existing imported project")
	}
}

func TestAttachOrganizationToExistingProjectRemovesNonOrgMembers(t *testing.T) {
	ctx := context.Background()
	setTestUsername(t, 2, "attach_editor")
	setTestUsername(t, 3, "attach_outsider")

	org, err := CreateOrganizationForUser(ctx, 1, "Attach Later Org", "")
	if err != nil {
		t.Fatalf("create org: %v", err)
	}
	if _, err := InviteToOrganization(ctx, 1, org.ID, "attach_editor", storage.RoleEditor); err != nil {
		t.Fatalf("invite org member: %v", err)
	}
	invites, err := storage.ListPendingOrganizationInvitesForEmail("editor@example.com")
	if err != nil || len(invites) != 1 {
		t.Fatalf("pending invites: %+v err=%v", invites, err)
	}
	if err := AcceptOrganizationInviteForUser(ctx, 2, "editor@example.com", invites[0].ID); err != nil {
		t.Fatalf("accept org invite: %v", err)
	}

	proj, err := CreateProject(ctx, 1, "Independent Board", "")
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	if err := storage.UpsertProjectMember(proj.ID, 2, storage.RoleEditor); err != nil {
		t.Fatalf("add editor: %v", err)
	}
	if err := storage.UpsertProjectMember(proj.ID, 3, storage.RoleViewer); err != nil {
		t.Fatalf("add outsider: %v", err)
	}
	if _, err := storage.CreateProjectInvite(proj.ID, "pending@example.com", storage.RoleViewer, 1, time.Now().Add(time.Hour)); err != nil {
		t.Fatalf("pending invite: %v", err)
	}

	if _, err := AttachOrganizationToProject(ctx, 2, proj.ID, CreateProjectInput{OrganizationID: &org.ID}); !errors.Is(err, ErrForbidden) {
		t.Fatalf("editor attach: err=%v want forbidden", err)
	}

	updated, err := AttachOrganizationToProject(ctx, 1, proj.ID, CreateProjectInput{OrganizationID: &org.ID})
	if err != nil {
		t.Fatalf("attach: %v", err)
	}
	if updated.OrgManaged || updated.OrganizationID == nil || *updated.OrganizationID != org.ID {
		t.Fatalf("attached unlocked project: %+v", updated)
	}

	again, err := AttachOrganizationToProject(ctx, 1, proj.ID, CreateProjectInput{OrganizationID: &org.ID})
	if err != nil {
		t.Fatalf("idempotent attach: %v", err)
	}
	if again.OrganizationID == nil || *again.OrganizationID != org.ID {
		t.Fatalf("idempotent org: %+v", again)
	}

	if _, err := storage.GetAccessibleProjectByID(proj.ID, 3); err == nil {
		t.Fatal("non-org member should lose access")
	}
	role, err := storage.GetProjectRole(proj.ID, 2)
	if err != nil || role != storage.RoleEditor {
		t.Fatalf("org editor role: %q err=%v", role, err)
	}
	if _, err := storage.GetAccessibleProjectByID(proj.ID, 2); err != nil {
		t.Fatalf("org editor should keep access: %v", err)
	}

	members, err := storage.ListProjectMembers(proj.ID)
	if err != nil {
		t.Fatalf("list members: %v", err)
	}
	var sawOutsider, sawCopiedEditor bool
	for _, m := range members {
		if m.UserID == 3 {
			sawOutsider = true
		}
		if m.UserID == 2 && !m.Inherited && m.Role == storage.RoleEditor {
			sawCopiedEditor = true
		}
	}
	if sawOutsider {
		t.Fatalf("outsider still listed: %+v", members)
	}
	if !sawCopiedEditor {
		t.Fatalf("expected copied editor membership, got %+v", members)
	}

	pendingProjectInvites, err := storage.ListProjectInvites(proj.ID)
	if err != nil {
		t.Fatalf("list invites: %v", err)
	}
	if len(pendingProjectInvites) != 0 {
		t.Fatalf("pending invites should be cancelled, got %+v", pendingProjectInvites)
	}
	if _, err := InviteToProject(ctx, 1, proj.ID, "attach_outsider", storage.RoleViewer); err != nil {
		t.Fatalf("invite after attach: %v", err)
	}

	other, err := CreateOrganizationForUser(ctx, 1, "Second Attach Org", "")
	if err != nil {
		t.Fatalf("second org: %v", err)
	}
	switched, err := AttachOrganizationToProject(ctx, 1, proj.ID, CreateProjectInput{OrganizationID: &other.ID})
	if err != nil {
		t.Fatalf("switch org: %v", err)
	}
	if switched.OrganizationID == nil || *switched.OrganizationID != other.ID {
		t.Fatalf("switched org: %+v", switched)
	}
	if _, err := storage.GetAccessibleProjectByID(proj.ID, 2); err == nil {
		t.Fatal("editor from previous org should lose access after switch")
	}
}

func TestInviteToOrganizationRequiresAccept(t *testing.T) {
	ctx := context.Background()
	setTestUsername(t, 2, "org_invite_editor")
	setTestUsername(t, 3, "org_invite_viewer")

	org, err := CreateOrganizationForUser(ctx, 1, "Invite Org", "")
	if err != nil {
		t.Fatalf("create org: %v", err)
	}
	inv, err := InviteToOrganization(ctx, 1, org.ID, "org_invite_editor", storage.RoleEditor)
	if err != nil {
		t.Fatalf("invite: %v", err)
	}
	if _, err := InviteToOrganization(ctx, 1, org.ID, "org_invite_editor", storage.RoleViewer); !errors.Is(err, ErrValidation) {
		t.Fatalf("duplicate invite: err=%v", err)
	}
	if _, err := InviteToOrganization(ctx, 2, org.ID, "org_invite_viewer", storage.RoleViewer); !errors.Is(err, ErrForbidden) && !errors.Is(err, ErrNotFound) {
		t.Fatalf("non-member invite: err=%v", err)
	}

	if role, err := storage.GetOrganizationRole(org.ID, 2); err != nil || role != "" {
		t.Fatalf("no membership before accept: %q err=%v", role, err)
	}
	proj, err := CreateProjectForUser(ctx, 1, CreateProjectInput{Name: "Invite Board", OrganizationID: &org.ID})
	if err != nil {
		t.Fatalf("create org project: %v", err)
	}
	if _, err := storage.GetAccessibleProjectByID(proj.ID, 2); err == nil {
		t.Fatal("pending invitee should not access org project")
	}

	if err := AcceptOrganizationInviteForUser(ctx, 2, "editor@example.com", inv.ID); err != nil {
		t.Fatalf("accept: %v", err)
	}
	if role, err := storage.GetOrganizationRole(org.ID, 2); err != nil || role != storage.RoleEditor {
		t.Fatalf("membership after accept: %q err=%v", role, err)
	}
	if _, err := storage.GetAccessibleProjectByID(proj.ID, 2); err == nil {
		t.Fatal("accepting an org invite should not add the user to existing imported projects")
	}

	declined, err := InviteToOrganization(ctx, 1, org.ID, "org_invite_viewer", storage.RoleViewer)
	if err != nil {
		t.Fatalf("invite viewer: %v", err)
	}
	if err := DeclineOrganizationInviteForUser(ctx, "viewer@example.com", declined.ID); err != nil {
		t.Fatalf("decline: %v", err)
	}
	if role, err := storage.GetOrganizationRole(org.ID, 3); err != nil || role != "" {
		t.Fatalf("declined user should not be a member: %q err=%v", role, err)
	}
	if _, err := storage.GetAccessibleProjectByID(proj.ID, 3); err == nil {
		t.Fatal("declined invitee should not access org project")
	}
}

func TestOrgImportLockBlocksEditsAndAppliesOrgRoleChanges(t *testing.T) {
	ctx := context.Background()
	setTestUsername(t, 2, "lock_editor")
	setTestUsername(t, 3, "lock_viewer")
	org, err := CreateOrganizationForUser(ctx, 1, "Lock Org", "")
	if err != nil {
		t.Fatalf("create org: %v", err)
	}
	if _, err := InviteToOrganization(ctx, 1, org.ID, "lock_editor", storage.RoleEditor); err != nil {
		t.Fatalf("invite: %v", err)
	}
	invites, err := storage.ListPendingOrganizationInvitesForEmail("editor@example.com")
	if err != nil || len(invites) != 1 {
		t.Fatalf("pending: %+v err=%v", invites, err)
	}
	if err := AcceptOrganizationInviteForUser(ctx, 2, "editor@example.com", invites[0].ID); err != nil {
		t.Fatalf("accept: %v", err)
	}

	locked, err := CreateProjectForUser(ctx, 1, CreateProjectInput{
		Name:           "Locked Board",
		OrganizationID: &org.ID,
		ImportMode:     storage.OrgImportLock,
	})
	if err != nil {
		t.Fatalf("create locked: %v", err)
	}
	if !locked.OrgManaged {
		t.Fatalf("expected lock: %+v", locked)
	}
	if _, err := InviteToProject(ctx, 1, locked.ID, "lock_viewer", storage.RoleViewer); !errors.Is(err, ErrForbidden) {
		t.Fatalf("invite on locked project: err=%v", err)
	}
	if _, err := CreateProjectCustomRoleForUser(ctx, 1, locked.ID, CreateSiteProjectRoleInput{
		Slug: "nope", Name: "Nope", Permissions: []string{storage.PermTasksEdit},
	}); !errors.Is(err, ErrForbidden) {
		t.Fatalf("custom role on locked project: err=%v", err)
	}

	unlocked, err := CreateProjectForUser(ctx, 1, CreateProjectInput{
		Name:           "Unlocked Board",
		OrganizationID: &org.ID,
		ImportMode:     storage.OrgImportCopy,
	})
	if err != nil {
		t.Fatalf("create unlocked: %v", err)
	}
	if unlocked.OrgManaged {
		t.Fatalf("copy should not lock: %+v", unlocked)
	}

	lockedMembers, err := storage.ListProjectMembers(locked.ID)
	if err != nil {
		t.Fatalf("list locked members: %v", err)
	}
	var sawLockedEditor bool
	for _, m := range lockedMembers {
		if m.UserID == 2 && m.Inherited && m.Role == storage.RoleEditor {
			sawLockedEditor = true
		}
	}
	if !sawLockedEditor {
		t.Fatalf("locked project should still list imported members: %+v", lockedMembers)
	}
	rosters, err := ListOrganizationProjectRostersForUser(ctx, 1, org.ID)
	if err != nil {
		t.Fatalf("org project rosters: %v", err)
	}
	if len(rosters) != 2 {
		t.Fatalf("expected 2 attached projects, got %+v", rosters)
	}
	var sawLockedRoster, sawUnlockedRoster bool
	for _, r := range rosters {
		var hasEditor bool
		for _, m := range r.Members {
			if m.UserID == 2 {
				hasEditor = true
			}
		}
		if !hasEditor {
			t.Fatalf("roster %s missing imported member: %+v", r.Name, r.Members)
		}
		if r.ID == locked.ID && r.OrgManaged {
			sawLockedRoster = true
		}
		if r.ID == unlocked.ID && !r.OrgManaged {
			sawUnlockedRoster = true
			if !r.CanManage {
				t.Fatal("owner should be able to edit unlocked project roles")
			}
		}
	}
	if !sawLockedRoster || !sawUnlockedRoster {
		t.Fatalf("rosters: %+v", rosters)
	}

	impact, err := OrganizationMemberRoleImpactForUser(ctx, 1, org.ID, 2)
	if err != nil {
		t.Fatalf("impact: %v", err)
	}
	if len(impact.Locked) != 1 || impact.Locked[0].ID != locked.ID {
		t.Fatalf("locked impact: %+v", impact.Locked)
	}
	if len(impact.Unlocked) != 1 || impact.Unlocked[0].ID != unlocked.ID {
		t.Fatalf("unlocked impact: %+v", impact.Unlocked)
	}

	if err := UpdateOrganizationMemberRoleForUser(ctx, 1, org.ID, 2, storage.RoleViewer); err != nil {
		t.Fatalf("org role: %v", err)
	}
	if role, err := storage.GetProjectRole(locked.ID, 2); err != nil || role != storage.RoleViewer {
		t.Fatalf("locked project role after org change: %q err=%v", role, err)
	}
	if role, err := storage.GetProjectRole(unlocked.ID, 2); err != nil || role != storage.RoleEditor {
		t.Fatalf("unlocked project role should stay editor: %q err=%v", role, err)
	}
}

func TestOrgImportSelectCopiesChosenMembers(t *testing.T) {
	ctx := context.Background()
	setTestUsername(t, 2, "select_editor")
	setTestUsername(t, 3, "select_viewer")
	org, err := CreateOrganizationForUser(ctx, 1, "Select Org", "")
	if err != nil {
		t.Fatalf("create org: %v", err)
	}
	if _, err := InviteToOrganization(ctx, 1, org.ID, "select_editor", storage.RoleEditor); err != nil {
		t.Fatalf("invite editor: %v", err)
	}
	if _, err := InviteToOrganization(ctx, 1, org.ID, "select_viewer", storage.RoleViewer); err != nil {
		t.Fatalf("invite viewer: %v", err)
	}
	for _, email := range []string{"editor@example.com", "viewer@example.com"} {
		invites, err := storage.ListPendingOrganizationInvitesForEmail(email)
		if err != nil || len(invites) != 1 {
			t.Fatalf("pending %s: %+v err=%v", email, invites, err)
		}
		uid := 2
		if email == "viewer@example.com" {
			uid = 3
		}
		if err := AcceptOrganizationInviteForUser(ctx, uid, email, invites[0].ID); err != nil {
			t.Fatalf("accept %s: %v", email, err)
		}
	}

	if _, err := CreateProjectForUser(ctx, 1, CreateProjectInput{
		Name:           "Need Role",
		OrganizationID: &org.ID,
		ImportMode:     storage.OrgImportSelect,
		Members:        []storage.OrgImportMember{{UserID: 2}},
	}); !errors.Is(err, ErrValidation) {
		t.Fatalf("select without role: err=%v", err)
	}

	proj, err := CreateProjectForUser(ctx, 1, CreateProjectInput{
		Name:           "Select Board",
		OrganizationID: &org.ID,
		ImportMode:     storage.OrgImportSelect,
		Members:        []storage.OrgImportMember{{UserID: 3, Role: storage.RoleEditor}},
	})
	if err != nil {
		t.Fatalf("create select: %v", err)
	}
	if proj.OrgManaged {
		t.Fatalf("select should not lock: %+v", proj)
	}
	if _, err := storage.GetAccessibleProjectByID(proj.ID, 2); err == nil {
		t.Fatal("unselected org member should not be imported")
	}
	if role, err := storage.GetProjectRole(proj.ID, 3); err != nil || role != storage.RoleEditor {
		t.Fatalf("selected member role: %q err=%v", role, err)
	}
	if _, err := InviteToProject(ctx, 1, proj.ID, "select_editor", storage.RoleViewer); err != nil {
		t.Fatalf("invite after select import: %v", err)
	}

	existing, err := CreateProject(ctx, 1, "Attach Select", "")
	if err != nil {
		t.Fatalf("independent: %v", err)
	}
	attached, err := AttachOrganizationToProject(ctx, 1, existing.ID, CreateProjectInput{
		OrganizationID: &org.ID,
		ImportMode:     storage.OrgImportSelect,
		Members:        []storage.OrgImportMember{{UserID: 2, Role: storage.RoleViewer}},
	})
	if err != nil {
		t.Fatalf("attach select: %v", err)
	}
	if attached.OrgManaged {
		t.Fatalf("attach select should not lock: %+v", attached)
	}
	if role, err := storage.GetProjectRole(existing.ID, 2); err != nil || role != storage.RoleViewer {
		t.Fatalf("attached selected role: %q err=%v", role, err)
	}
	if _, err := storage.GetAccessibleProjectByID(existing.ID, 3); err == nil {
		t.Fatal("unselected member should not be on attached project")
	}
}

func TestOrgCustomizeDefaultRoles(t *testing.T) {
	ctx := context.Background()
	org, err := CreateOrganizationForUser(ctx, 1, "Override Org", "")
	if err != nil {
		t.Fatalf("create org: %v", err)
	}
	if err := storage.UpsertOrganizationMember(org.ID, 2, storage.RoleEditor); err != nil {
		t.Fatalf("add editor member: %v", err)
	}

	site, err := storage.ListSiteProjectRoles()
	if err != nil {
		t.Fatalf("list site roles: %v", err)
	}
	var owner, editor storage.ProjectRoleDef
	for _, d := range site {
		switch d.Slug {
		case storage.RoleOwner:
			owner = d
		case storage.RoleEditor:
			editor = d
		}
	}
	if owner.ID == 0 || editor.ID == 0 {
		t.Fatalf("missing built-in roles: owner=%+v editor=%+v", owner, editor)
	}
	if !storage.HasOrgPerm(org.ID, storage.RoleEditor, storage.PermTasksCreate) {
		t.Fatal("site editor should create tasks before override")
	}

	name := "Org Editor"
	desc := "Trimmed for this org"
	perms := []string{storage.PermTasksEdit, storage.PermTasksStatus}
	overridden, err := UpdateOrganizationRoleForUser(ctx, 1, org.ID, editor.ID, UpdateSiteProjectRoleInput{
		Name:        &name,
		Description: &desc,
		Permissions: &perms,
	})
	if err != nil {
		t.Fatalf("customize editor: %v", err)
	}
	if overridden.OrganizationID == nil || *overridden.OrganizationID != org.ID || overridden.Slug != storage.RoleEditor {
		t.Fatalf("override scope: %+v", overridden)
	}
	if !overridden.OverridesSite || overridden.Name != name {
		t.Fatalf("override flags: %+v", overridden)
	}

	again, err := UpdateOrganizationRoleForUser(ctx, 1, org.ID, editor.ID, UpdateSiteProjectRoleInput{Name: &name})
	if err != nil || again.ID != overridden.ID {
		t.Fatalf("second customize should update existing override: %+v err=%v", again, err)
	}

	listed, _, err := ListOrganizationRolesForUser(ctx, 1, org.ID)
	if err != nil {
		t.Fatalf("list org roles: %v", err)
	}
	var sawSiteEditor, sawOverride bool
	for _, d := range listed {
		if d.Slug == storage.RoleEditor && d.OrganizationID == nil {
			sawSiteEditor = true
		}
		if d.ID == overridden.ID && d.OverridesSite {
			sawOverride = true
		}
	}
	if sawSiteEditor || !sawOverride {
		t.Fatalf("list should hide site editor and show override: %+v", listed)
	}

	if storage.HasOrgPerm(org.ID, storage.RoleEditor, storage.PermTasksCreate) {
		t.Fatal("org editor override should drop create")
	}
	if !storage.HasOrgPerm(org.ID, storage.RoleEditor, storage.PermTasksEdit) {
		t.Fatal("org editor override should keep edit")
	}
	if !storage.HasProjectPerm(0, storage.RoleEditor, storage.PermTasksCreate) {
		t.Fatal("site editor template must stay unchanged")
	}
	if storage.OrgRoleDisplayName(org.ID, storage.RoleEditor) != name {
		t.Fatalf("display name: %q", storage.OrgRoleDisplayName(org.ID, storage.RoleEditor))
	}

	proj, err := CreateProjectForUser(ctx, 1, CreateProjectInput{
		Name:           "Override Board",
		OrganizationID: &org.ID,
	})
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	assignable, err := storage.ListAssignableProjectRoles(proj.ID)
	if err != nil {
		t.Fatalf("assignable: %v", err)
	}
	var sawAssignableSiteEditor, sawAssignableOverride bool
	for _, d := range assignable {
		if d.Slug == storage.RoleEditor && d.OrganizationID == nil {
			sawAssignableSiteEditor = true
		}
		if d.ID == overridden.ID {
			sawAssignableOverride = true
		}
	}
	if sawAssignableSiteEditor || !sawAssignableOverride {
		t.Fatalf("assignable should prefer org editor: %+v", assignable)
	}
	if storage.HasProjectPerm(proj.ID, storage.RoleEditor, storage.PermTasksCreate) {
		t.Fatal("project should use org editor override")
	}

	if _, err := UpdateOrganizationRoleForUser(ctx, 1, org.ID, owner.ID, UpdateSiteProjectRoleInput{Name: &name}); !errors.Is(err, ErrValidation) {
		t.Fatalf("customize owner: err=%v want validation", err)
	}
	if _, err := CreateOrganizationRoleForUser(ctx, 1, org.ID, CreateSiteProjectRoleInput{
		Slug: storage.RoleOwner, Name: "Not Owner", Permissions: []string{storage.PermTasksEdit},
	}); !errors.Is(err, ErrValidation) {
		t.Fatalf("create owner slug: err=%v want validation", err)
	}

	if err := DeleteOrganizationRoleForUser(ctx, 1, org.ID, overridden.ID); err != nil {
		t.Fatalf("reset override while assigned: %v", err)
	}
	if !storage.HasOrgPerm(org.ID, storage.RoleEditor, storage.PermTasksCreate) {
		t.Fatal("reset should restore site editor permissions")
	}
	if storage.OrgRoleDisplayName(org.ID, storage.RoleEditor) == name {
		t.Fatal("reset should restore site editor name")
	}
	if role, err := storage.GetOrganizationRole(org.ID, 2); err != nil || role != storage.RoleEditor {
		t.Fatalf("member should keep editor slug after reset: %q err=%v", role, err)
	}
}
