package domain

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"GoTodo/internal/storage"
)

func TestOwnerIsOnlySiteRole(t *testing.T) {
	if err := storage.SeedDefaultProjectRoles(); err != nil {
		t.Fatalf("seed: %v", err)
	}
	site, err := storage.ListSiteProjectRoles()
	if err != nil {
		t.Fatalf("list site roles: %v", err)
	}
	if len(site) != 1 || site[0].Slug != storage.RoleOwner {
		t.Fatalf("site roles should be just owner: %+v", site)
	}
	if !storage.HasProjectPerm(0, storage.RoleOwner, storage.PermProjectManage) {
		t.Fatal("owner should manage")
	}
	for _, slug := range []string{testRoleEditor, testRoleViewer, testRoleDeveloper, testRoleQA} {
		if storage.ResolveRoleDef(0, slug) != nil {
			t.Fatalf("%s should not exist as a site role", slug)
		}
		if storage.RoleCanWriteTask(0, slug) {
			t.Fatalf("%s should not write without a project or org role", slug)
		}
	}
	if _, err := CreateSiteProjectRoleForAdmin(context.Background(), 1, CreateSiteProjectRoleInput{
		Slug: "qa-two", Name: "QA Two",
	}); err == nil {
		t.Fatal("creating a site role should be refused")
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
	if err := upsertTestMember(t, proj.ID, qaID, testRoleQA); err != nil {
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
	if err := upsertTestMember(t, proj.ID, devID, testRoleDeveloper); err != nil {
		t.Fatalf("add developer: %v", err)
	}
	if err := upsertTestMember(t, proj.ID, qaID, testRoleQA); err != nil {
		t.Fatalf("add qa: %v", err)
	}

	inQA, err := CreateProjectStatusForUser(ctx, ownerID, proj.ID, CreateProjectStatusInput{Name: "In QA"})
	if err != nil {
		t.Fatalf("create In QA: %v", err)
	}
	if _, err := UpdateStatusGatesForUser(ctx, ownerID, proj.ID, inQA.ID, []string{testRoleQA}, []string{testRoleQA}); err != nil {
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

func TestCopyAndReorderProjectRoles(t *testing.T) {
	ctx := context.Background()
	proj, err := CreateProject(ctx, 1, "Copy Reorder Roles", "")
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	first, err := CreateProjectCustomRoleForUser(ctx, 1, proj.ID, CreateSiteProjectRoleInput{
		Slug: "tester", Name: "Tester", Permissions: []string{storage.PermTasksStatus},
	})
	if err != nil {
		t.Fatalf("create role: %v", err)
	}
	copied, err := CreateProjectCustomRoleForUser(ctx, 1, proj.ID, CreateSiteProjectRoleInput{
		Slug: "tester-copy", CopyFromID: first.ID,
	})
	if err != nil {
		t.Fatalf("copy role: %v", err)
	}
	if copied.Name != "Tester (copy)" || strings.Join(copied.Permissions, ",") != storage.PermTasksStatus {
		t.Fatalf("copied role: %+v", copied)
	}
	if err := ReorderProjectCustomRolesForUser(ctx, 1, proj.ID, []int{copied.ID, first.ID}); err != nil {
		t.Fatalf("reorder: %v", err)
	}
	after, err := storage.ListProjectCustomRoles(proj.ID)
	if err != nil || len(after) != 2 || after[0].ID != copied.ID {
		t.Fatalf("reorder result: %+v err=%v", after, err)
	}
}

func TestOrgImportCopiesMembersAndAllowsProjectEdits(t *testing.T) {
	ctx := context.Background()
	setTestUsername(t, 2, "editor_user")
	setTestUsername(t, 3, "viewer_user")
	org, err := CreateOrganizationForUser(ctx, 1, "Acme Org", "team")
	if err != nil {
		t.Fatalf("create org: %v", err)
	}
	if _, err := inviteToTestOrg(t, ctx, 1, org.ID, "editor_user", testRoleEditor); err != nil {
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
	if err != nil || role != testRoleEditor {
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
		if m.UserID == 2 && !m.Inherited && m.Role == testRoleEditor {
			sawEditor = true
		}
	}
	if !sawEditor {
		t.Fatalf("expected copied editor membership, got %+v", members)
	}

	if _, err := inviteToTestProject(t, ctx, 1, proj.ID, "viewer_user", testRoleViewer); err != nil {
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

	if err := upsertTestOrgMember(t, org.ID, 3, testRoleViewer); err != nil {
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
	if _, err := inviteToTestOrg(t, ctx, 1, org.ID, "attach_editor", testRoleEditor); err != nil {
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
	if err := upsertTestMember(t, proj.ID, 2, testRoleEditor); err != nil {
		t.Fatalf("add editor: %v", err)
	}
	if err := upsertTestMember(t, proj.ID, 3, testRoleViewer); err != nil {
		t.Fatalf("add outsider: %v", err)
	}
	if _, err := storage.CreateProjectInvite(proj.ID, "pending@example.com", testRoleViewer, 1, time.Now().Add(time.Hour)); err != nil {
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
	if err != nil || role != testRoleEditor {
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
		if m.UserID == 2 && !m.Inherited && m.Role == testRoleEditor {
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
	if _, err := inviteToTestProject(t, ctx, 1, proj.ID, "attach_outsider", testRoleViewer); err != nil {
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
	inv, err := inviteToTestOrg(t, ctx, 1, org.ID, "org_invite_editor", testRoleEditor)
	if err != nil {
		t.Fatalf("invite: %v", err)
	}
	if _, err := inviteToTestOrg(t, ctx, 1, org.ID, "org_invite_editor", testRoleViewer); !errors.Is(err, ErrValidation) {
		t.Fatalf("duplicate invite: err=%v", err)
	}
	if _, err := inviteToTestOrg(t, ctx, 2, org.ID, "org_invite_viewer", testRoleViewer); !errors.Is(err, ErrForbidden) && !errors.Is(err, ErrNotFound) {
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
	if role, err := storage.GetOrganizationRole(org.ID, 2); err != nil || role != testRoleEditor {
		t.Fatalf("membership after accept: %q err=%v", role, err)
	}
	if role, err := storage.GetProjectRole(proj.ID, 2); err != nil || role != testRoleEditor {
		t.Fatalf("accepting an org invite should add the user to copy/lock projects: %q err=%v", role, err)
	}

	declined, err := inviteToTestOrg(t, ctx, 1, org.ID, "org_invite_viewer", testRoleViewer)
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
	if _, err := inviteToTestOrg(t, ctx, 1, org.ID, "lock_editor", testRoleEditor); err != nil {
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
	if _, err := inviteToTestProject(t, ctx, 1, locked.ID, "lock_viewer", testRoleViewer); !errors.Is(err, ErrForbidden) {
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
		if m.UserID == 2 && m.Inherited && m.Role == testRoleEditor {
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

	if err := UpdateOrganizationMemberRoleForUser(ctx, 1, org.ID, 2, testRoleViewer); err != nil {
		t.Fatalf("org role: %v", err)
	}
	if role, err := storage.GetProjectRole(locked.ID, 2); err != nil || role != testRoleViewer {
		t.Fatalf("locked project role after org change: %q err=%v", role, err)
	}
	if role, err := storage.GetProjectRole(unlocked.ID, 2); err != nil || role != testRoleEditor {
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
	if _, err := inviteToTestOrg(t, ctx, 1, org.ID, "select_editor", testRoleEditor); err != nil {
		t.Fatalf("invite editor: %v", err)
	}
	if _, err := inviteToTestOrg(t, ctx, 1, org.ID, "select_viewer", testRoleViewer); err != nil {
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
		Members:        []storage.OrgImportMember{{UserID: 3, Role: testRoleEditor}},
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
	if role, err := storage.GetProjectRole(proj.ID, 3); err != nil || role != testRoleEditor {
		t.Fatalf("selected member role: %q err=%v", role, err)
	}
	if _, err := inviteToTestProject(t, ctx, 1, proj.ID, "select_editor", testRoleViewer); err != nil {
		t.Fatalf("invite after select import: %v", err)
	}

	existing, err := CreateProject(ctx, 1, "Attach Select", "")
	if err != nil {
		t.Fatalf("independent: %v", err)
	}
	attached, err := AttachOrganizationToProject(ctx, 1, existing.ID, CreateProjectInput{
		OrganizationID: &org.ID,
		ImportMode:     storage.OrgImportSelect,
		Members:        []storage.OrgImportMember{{UserID: 2, Role: testRoleViewer}},
	})
	if err != nil {
		t.Fatalf("attach select: %v", err)
	}
	if attached.OrgManaged {
		t.Fatalf("attach select should not lock: %+v", attached)
	}
	if role, err := storage.GetProjectRole(existing.ID, 2); err != nil || role != testRoleViewer {
		t.Fatalf("attached selected role: %q err=%v", role, err)
	}
	if _, err := storage.GetAccessibleProjectByID(existing.ID, 3); err == nil {
		t.Fatal("unselected member should not be on attached project")
	}
}

func TestOrgRolesApplyToOrgProjects(t *testing.T) {
	ctx := context.Background()
	org, err := CreateOrganizationForUser(ctx, 1, "Org Role Scope", "")
	if err != nil {
		t.Fatalf("create org: %v", err)
	}
	if storage.ValidInviteRoleForOrg(org.ID, testRoleEditor) {
		t.Fatal("a role the org has not created should not be assignable")
	}
	created, err := CreateOrganizationRoleForUser(ctx, 1, org.ID, CreateSiteProjectRoleInput{
		Slug:        testRoleEditor,
		Name:        "Org Editor",
		Permissions: []string{storage.PermTasksCreate, storage.PermTasksEdit},
	})
	if err != nil {
		t.Fatalf("create org role: %v", err)
	}
	if created.OverridesSite {
		t.Fatal("org roles no longer override a site role")
	}
	if err := storage.UpsertOrganizationMember(org.ID, 2, testRoleEditor); err != nil {
		t.Fatalf("add editor member: %v", err)
	}

	proj, err := CreateProjectForUser(ctx, 1, CreateProjectInput{Name: "Org Role Board", OrganizationID: &org.ID})
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	assignable, err := storage.ListAssignableProjectRoles(proj.ID)
	if err != nil {
		t.Fatalf("assignable: %v", err)
	}
	if len(assignable) != 1 || assignable[0].ID != created.ID {
		t.Fatalf("assignable should be just the org role: %+v", assignable)
	}
	if !storage.HasProjectPerm(proj.ID, testRoleEditor, storage.PermTasksCreate) {
		t.Fatal("project should use org editor permissions")
	}

	perms := []string{storage.PermTasksEdit}
	if _, err := UpdateOrganizationRoleForUser(ctx, 1, org.ID, created.ID, UpdateSiteProjectRoleInput{Permissions: &perms}); err != nil {
		t.Fatalf("update org role: %v", err)
	}
	if storage.HasProjectPerm(proj.ID, testRoleEditor, storage.PermTasksCreate) {
		t.Fatal("org permission change should reach the project")
	}
	if !storage.HasOrgPerm(org.ID, testRoleEditor, storage.PermTasksEdit) {
		t.Fatal("org editor should keep edit")
	}

	owner := storage.ResolveOrgRoleDef(0, storage.RoleOwner)
	ownerPerms := []string{storage.PermTasksEdit}
	if _, err := UpdateOrganizationRoleForUser(ctx, 1, org.ID, owner.ID, UpdateSiteProjectRoleInput{Permissions: &ownerPerms}); !errors.Is(err, ErrValidation) {
		t.Fatalf("customize owner permissions: err=%v want validation", err)
	}
	if _, err := CreateOrganizationRoleForUser(ctx, 1, org.ID, CreateSiteProjectRoleInput{
		Slug: storage.RoleOwner, Name: "Not Owner", Permissions: []string{storage.PermTasksEdit},
	}); err == nil {
		t.Fatal("creating an owner-slug org role should fail")
	}
	if err := DeleteOrganizationRoleForUser(ctx, 1, org.ID, created.ID); !errors.Is(err, ErrConflict) {
		t.Fatalf("delete assigned org role: err=%v want conflict", err)
	}
}

func TestOrgAcceptAndSyncAddsMembersToCopyAndLockNotSelect(t *testing.T) {
	ctx := context.Background()
	setTestUsername(t, 2, "sync_copy_editor")
	setTestUsername(t, 3, "sync_copy_viewer")

	org, err := CreateOrganizationForUser(ctx, 1, "Sync Org", "")
	if err != nil {
		t.Fatalf("create org: %v", err)
	}
	inv2, err := inviteToTestOrg(t, ctx, 1, org.ID, "sync_copy_editor", testRoleEditor)
	if err != nil {
		t.Fatalf("invite editor: %v", err)
	}
	if err := AcceptOrganizationInviteForUser(ctx, 2, "editor@example.com", inv2.ID); err != nil {
		t.Fatalf("accept editor: %v", err)
	}

	copyProj, err := CreateProjectForUser(ctx, 1, CreateProjectInput{
		Name:           "Copy Board",
		OrganizationID: &org.ID,
		ImportMode:     storage.OrgImportCopy,
	})
	if err != nil {
		t.Fatalf("copy project: %v", err)
	}
	lockProj, err := CreateProjectForUser(ctx, 1, CreateProjectInput{
		Name:           "Lock Board",
		OrganizationID: &org.ID,
		ImportMode:     storage.OrgImportLock,
	})
	if err != nil {
		t.Fatalf("lock project: %v", err)
	}
	selectProj, err := CreateProjectForUser(ctx, 1, CreateProjectInput{
		Name:           "Select Board",
		OrganizationID: &org.ID,
		ImportMode:     storage.OrgImportSelect,
		Members:        []storage.OrgImportMember{{UserID: 2, Role: testRoleEditor}},
	})
	if err != nil {
		t.Fatalf("select project: %v", err)
	}

	inv3, err := inviteToTestOrg(t, ctx, 1, org.ID, "sync_copy_viewer", testRoleViewer)
	if err != nil {
		t.Fatalf("invite viewer: %v", err)
	}
	if err := AcceptOrganizationInviteForUser(ctx, 3, "viewer@example.com", inv3.ID); err != nil {
		t.Fatalf("accept viewer: %v", err)
	}
	if role, err := storage.GetProjectRole(copyProj.ID, 3); err != nil || role != testRoleViewer {
		t.Fatalf("copy after accept: %q err=%v", role, err)
	}
	if role, err := storage.GetProjectRole(lockProj.ID, 3); err != nil || role != testRoleViewer {
		t.Fatalf("lock after accept: %q err=%v", role, err)
	}
	if _, err := storage.GetAccessibleProjectByID(selectProj.ID, 3); err == nil {
		t.Fatal("select project should ignore accepted org members")
	}

	if err := RemoveProjectMember(ctx, 1, copyProj.ID, 3); err != nil {
		t.Fatalf("remove from copy: %v", err)
	}
	if err := upsertTestMember(t, lockProj.ID, 2, testRoleViewer); err != nil {
		t.Fatalf("stale lock role: %v", err)
	}
	if err := UpdateProjectMemberRole(ctx, 1, copyProj.ID, 2, testRoleViewer); err != nil {
		t.Fatalf("custom copy role: %v", err)
	}

	result, err := SyncOrganizationProjectsForUser(ctx, 1, org.ID)
	if err != nil {
		t.Fatalf("sync: %v", err)
	}
	if result.Added < 1 {
		t.Fatalf("sync should add missing copy membership: %+v", result)
	}
	if role, err := storage.GetProjectRole(copyProj.ID, 3); err != nil || role != testRoleViewer {
		t.Fatalf("copy after sync: %q err=%v", role, err)
	}
	if role, err := storage.GetProjectRole(copyProj.ID, 2); err != nil || role != testRoleViewer {
		t.Fatalf("copy should keep custom role: %q err=%v", role, err)
	}
	if role, err := storage.GetProjectRole(lockProj.ID, 2); err != nil || role != testRoleEditor {
		t.Fatalf("lock should realign org role: %q err=%v", role, err)
	}
	if _, err := storage.GetAccessibleProjectByID(selectProj.ID, 3); err == nil {
		t.Fatal("sync should skip select project")
	}

	rosters, err := ListOrganizationProjectRostersForUser(ctx, 1, org.ID)
	if err != nil {
		t.Fatalf("rosters: %v", err)
	}
	var sawCopy, sawLock, sawSelect bool
	for _, r := range rosters {
		switch r.ID {
		case copyProj.ID:
			sawCopy = r.OrgImport == storage.OrgImportCopy
		case lockProj.ID:
			sawLock = r.OrgImport == storage.OrgImportLock
		case selectProj.ID:
			sawSelect = r.OrgImport == storage.OrgImportSelect
		}
	}
	if !sawCopy || !sawLock || !sawSelect {
		t.Fatalf("roster import modes: %+v", rosters)
	}
}

func TestProjectRenamesInheritedRoles(t *testing.T) {
	ctx := context.Background()
	ownerID := 1
	memberID := 2
	// Editor here is an organization role the project inherits; Owner is the site role.
	org, err := CreateOrganizationForUser(ctx, ownerID, "Renamed Roles Org", "")
	if err != nil {
		t.Fatalf("create org: %v", err)
	}
	proj, err := CreateProjectForUser(ctx, ownerID, CreateProjectInput{
		Name:           "Renamed Roles",
		OrganizationID: &org.ID,
		ImportMode:     storage.OrgImportCopy,
	})
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	other, err := CreateProject(ctx, ownerID, "Untouched Roles", "")
	if err != nil {
		t.Fatalf("create other project: %v", err)
	}
	if err := upsertTestMember(t, proj.ID, memberID, testRoleEditor); err != nil {
		t.Fatalf("add editor: %v", err)
	}

	owner, err := GetProjectOwnerRoleForUser(ctx, ownerID, proj.ID)
	if err != nil || owner == nil || owner.ID == 0 {
		t.Fatalf("owner role: %+v err=%v", owner, err)
	}
	name := "Project Manager"
	renamed, err := UpdateProjectCustomRoleForUser(ctx, ownerID, proj.ID, owner.ID, UpdateSiteProjectRoleInput{Name: &name})
	if err != nil {
		t.Fatalf("rename owner: %v", err)
	}
	if renamed.Name != "Project Manager" || renamed.DefaultName != "Owner" {
		t.Fatalf("renamed owner: name=%q default=%q", renamed.Name, renamed.DefaultName)
	}
	if got := storage.RoleDisplayName(proj.ID, storage.RoleOwner); got != "Project Manager" {
		t.Fatalf("owner display name: %q", got)
	}
	if got := storage.RoleDisplayName(other.ID, storage.RoleOwner); got != "Owner" {
		t.Fatalf("other project owner display name: %q", got)
	}
	if !storage.HasProjectPerm(proj.ID, storage.RoleOwner, storage.PermProjectManage) {
		t.Fatal("renamed owner should keep permissions")
	}

	var editorID int
	roles, _, err := ListProjectRolesForUser(ctx, ownerID, proj.ID)
	if err != nil {
		t.Fatalf("list roles: %v", err)
	}
	for _, r := range roles {
		if r.Slug == testRoleEditor {
			editorID = r.ID
		}
	}
	lead := "Lead Developer"
	if _, err := UpdateProjectCustomRoleForUser(ctx, memberID, proj.ID, editorID, UpdateSiteProjectRoleInput{Name: &lead}); !errors.Is(err, ErrForbidden) {
		t.Fatalf("editor rename: err=%v want forbidden", err)
	}
	if _, err := UpdateProjectCustomRoleForUser(ctx, ownerID, proj.ID, editorID, UpdateSiteProjectRoleInput{Name: &lead}); err != nil {
		t.Fatalf("rename editor: %v", err)
	}
	perms := []string{storage.PermTasksCreate}
	if _, err := UpdateProjectCustomRoleForUser(ctx, ownerID, proj.ID, editorID, UpdateSiteProjectRoleInput{Permissions: &perms}); !errors.Is(err, ErrValidation) {
		t.Fatalf("editor perms patch: err=%v want validation", err)
	}
	roles, _, err = ListProjectRolesForUser(ctx, ownerID, proj.ID)
	if err != nil {
		t.Fatalf("list roles: %v", err)
	}
	for _, r := range roles {
		if r.Slug == testRoleEditor && (r.Name != "Lead Developer" || r.DefaultName != "Editor") {
			t.Fatalf("listed editor: name=%q default=%q", r.Name, r.DefaultName)
		}
	}
	members, err := storage.ListProjectMembers(proj.ID)
	if err != nil {
		t.Fatalf("members: %v", err)
	}
	for _, m := range members {
		if m.UserID == memberID && storage.RoleDisplayName(proj.ID, m.Role) != "Lead Developer" {
			t.Fatalf("member role label: %q", storage.RoleDisplayName(proj.ID, m.Role))
		}
	}

	reset := "Editor"
	restored, err := UpdateProjectCustomRoleForUser(ctx, ownerID, proj.ID, editorID, UpdateSiteProjectRoleInput{Name: &reset})
	if err != nil || restored.DefaultName != "" {
		t.Fatalf("reset editor: %+v err=%v", restored, err)
	}
	if got := storage.RoleDisplayName(proj.ID, testRoleEditor); got != "Editor" {
		t.Fatalf("reset display name: %q", got)
	}
}

func TestOrgRoleRenamePropagatesToLockedProjects(t *testing.T) {
	ctx := context.Background()
	org, err := CreateOrganizationForUser(ctx, 1, "Rename Sync Org", "")
	if err != nil {
		t.Fatalf("create org: %v", err)
	}
	locked, err := CreateProjectForUser(ctx, 1, CreateProjectInput{
		Name:           "Rename Sync Locked",
		OrganizationID: &org.ID,
		ImportMode:     storage.OrgImportLock,
	})
	if err != nil || !locked.OrgManaged {
		t.Fatalf("create locked: %+v err=%v", locked, err)
	}

	editor, err := CreateOrganizationRoleForUser(ctx, 1, org.ID, CreateSiteProjectRoleInput{
		Slug: testRoleEditor, Name: "Editor", Permissions: []string{storage.PermTasksEdit},
	})
	if err != nil {
		t.Fatalf("create org editor: %v", err)
	}
	editorID := editor.ID

	// A project label written before the lock must not mask the organization's name.
	if err := storage.SetProjectRoleLabel(locked.ID, testRoleEditor, "Stale Project Name"); err != nil {
		t.Fatalf("seed stale label: %v", err)
	}
	stale := "Should Not Apply"
	if _, err := UpdateProjectCustomRoleForUser(ctx, 1, locked.ID, editorID, UpdateSiteProjectRoleInput{Name: &stale}); !errors.Is(err, ErrForbidden) {
		t.Fatalf("rename on locked project: err=%v want forbidden", err)
	}

	for _, name := range []string{"Lead Developer", "Senior Developer"} {
		n := name
		if _, err := UpdateOrganizationRoleForUser(ctx, 1, org.ID, editorID, UpdateSiteProjectRoleInput{Name: &n}); err != nil {
			t.Fatalf("org rename editor to %q: %v", name, err)
		}
		if got := storage.RoleDisplayName(locked.ID, testRoleEditor); got != name {
			t.Fatalf("locked display name: got %q want %q", got, name)
		}
		roles, _, err := ListProjectRolesForUser(ctx, 1, locked.ID)
		if err != nil {
			t.Fatalf("list locked roles: %v", err)
		}
		var found bool
		for _, r := range roles {
			if r.Slug == testRoleEditor {
				found = true
				if r.Name != name || r.DefaultName != "" {
					t.Fatalf("locked listed editor: name=%q default=%q want %q", r.Name, r.DefaultName, name)
				}
			}
		}
		if !found {
			t.Fatal("locked project should list the org editor")
		}
	}
}

func TestOrgOwnerRenamePropagatesAndKeepsAllPermissions(t *testing.T) {
	ctx := context.Background()
	org, err := CreateOrganizationForUser(ctx, 1, "Owner Rename Org", "")
	if err != nil {
		t.Fatalf("create org: %v", err)
	}
	locked, err := CreateProjectForUser(ctx, 1, CreateProjectInput{
		Name:           "Owner Rename Locked",
		OrganizationID: &org.ID,
		ImportMode:     storage.OrgImportLock,
	})
	if err != nil {
		t.Fatalf("create locked: %v", err)
	}
	copied, err := CreateProjectForUser(ctx, 1, CreateProjectInput{
		Name:           "Owner Rename Copy",
		OrganizationID: &org.ID,
		ImportMode:     storage.OrgImportCopy,
	})
	if err != nil {
		t.Fatalf("create copy: %v", err)
	}
	standalone, err := CreateProject(ctx, 1, "Owner Rename Standalone", "")
	if err != nil {
		t.Fatalf("create standalone: %v", err)
	}

	owner, err := GetOrganizationOwnerRoleForUser(ctx, 1, org.ID)
	if err != nil || owner == nil || owner.ID == 0 {
		t.Fatalf("org owner role: %+v err=%v", owner, err)
	}
	pm := "Project Manager"
	renamed, err := UpdateOrganizationRoleForUser(ctx, 1, org.ID, owner.ID, UpdateSiteProjectRoleInput{Name: &pm})
	if err != nil {
		t.Fatalf("org rename owner: %v", err)
	}
	if renamed.Name != pm || renamed.DefaultName != "Owner" {
		t.Fatalf("renamed org owner: name=%q default=%q", renamed.Name, renamed.DefaultName)
	}
	if got := storage.OrgRoleDisplayName(org.ID, storage.RoleOwner); got != pm {
		t.Fatalf("org owner display: %q", got)
	}
	for _, pid := range []int{locked.ID, copied.ID} {
		if got := storage.RoleDisplayName(pid, storage.RoleOwner); got != pm {
			t.Fatalf("project %d owner display: %q", pid, got)
		}
		po, err := GetProjectOwnerRoleForUser(ctx, 1, pid)
		if err != nil || po.Name != pm {
			t.Fatalf("project %d owner role: %+v err=%v", pid, po, err)
		}
		for _, perm := range storage.AllProjectPerms() {
			if !storage.HasProjectPerm(pid, storage.RoleOwner, perm) {
				t.Fatalf("renamed owner lost %s on project %d", perm, pid)
			}
		}
		roles, _, err := ListProjectRolesForUser(ctx, 1, pid)
		if err != nil {
			t.Fatalf("list roles: %v", err)
		}
		for _, r := range roles {
			if r.Slug == storage.RoleOwner {
				t.Fatalf("owner must not be assignable on project %d", pid)
			}
		}
	}
	if got := storage.RoleDisplayName(standalone.ID, storage.RoleOwner); got != "Owner" {
		t.Fatalf("standalone owner display: %q", got)
	}

	// An unlocked project can still choose its own name on top of the organization's.
	lead := "Lead Developer"
	projRenamed, err := UpdateProjectCustomRoleForUser(ctx, 1, copied.ID, owner.ID, UpdateSiteProjectRoleInput{Name: &lead})
	if err != nil || projRenamed.Name != lead || projRenamed.DefaultName != pm {
		t.Fatalf("project rename over org name: %+v err=%v", projRenamed, err)
	}
	if got := storage.RoleDisplayName(locked.ID, storage.RoleOwner); got != pm {
		t.Fatalf("locked project should keep org name: %q", got)
	}

	site := "Owner"
	if _, err := UpdateOrganizationRoleForUser(ctx, 1, org.ID, owner.ID, UpdateSiteProjectRoleInput{Name: &site}); err != nil {
		t.Fatalf("reset org owner: %v", err)
	}
	if got := storage.RoleDisplayName(locked.ID, storage.RoleOwner); got != "Owner" {
		t.Fatalf("locked owner after reset: %q", got)
	}
	if got := storage.RoleDisplayName(copied.ID, storage.RoleOwner); got != lead {
		t.Fatalf("copied project should keep its own name: %q", got)
	}
}

func TestMigrateLegacySiteRolesKeepsAccess(t *testing.T) {
	ctx := context.Background()
	// Recreate an old site role the way earlier releases seeded them.
	const slug = "legacy-tester"
	legacyPerms := []string{storage.PermTasksEdit, storage.PermTasksStatus}
	if _, err := storage.CreateProjectRoleDef(nil, slug, "Legacy Tester", "Old site role", legacyPerms, true, 5); err != nil {
		t.Fatalf("create legacy site role: %v", err)
	}
	storage.InvalidateSiteRoleCache()

	usedOrg, err := CreateOrganizationForUser(ctx, 1, "Legacy Used Org", "")
	if err != nil {
		t.Fatalf("create org: %v", err)
	}
	if err := storage.UpsertOrganizationMember(usedOrg.ID, 2, slug); err != nil {
		t.Fatalf("org member: %v", err)
	}
	locked, err := CreateProjectForUser(ctx, 1, CreateProjectInput{
		Name: "Legacy Locked", OrganizationID: &usedOrg.ID, ImportMode: storage.OrgImportLock,
	})
	if err != nil {
		t.Fatalf("create locked: %v", err)
	}

	customOrg, err := CreateOrganizationForUser(ctx, 1, "Legacy Custom Org", "")
	if err != nil {
		t.Fatalf("create custom org: %v", err)
	}
	custom, err := storage.CreateOrganizationRoleDef(customOrg.ID, slug, "Org Tester", "", []string{storage.PermTasksClaim}, 1)
	if err != nil {
		t.Fatalf("org override: %v", err)
	}
	if err := storage.UpsertOrganizationMember(customOrg.ID, 3, slug); err != nil {
		t.Fatalf("custom org member: %v", err)
	}

	standalone, err := CreateProject(ctx, 1, "Legacy Standalone", "")
	if err != nil {
		t.Fatalf("create standalone: %v", err)
	}
	if err := storage.UpsertProjectMember(standalone.ID, 3, slug); err != nil {
		t.Fatalf("standalone member: %v", err)
	}
	if err := storage.SetProjectRoleLabel(standalone.ID, slug, "Checker"); err != nil {
		t.Fatalf("label: %v", err)
	}

	gated, err := CreateProject(ctx, 1, "Legacy Gated", "")
	if err != nil {
		t.Fatalf("create gated: %v", err)
	}
	if _, err := SetProjectWorkflowMode(ctx, 1, gated.ID, storage.WorkflowKanban); err != nil {
		t.Fatalf("kanban: %v", err)
	}
	statuses, err := storage.ListProjectStatuses(gated.ID)
	if err != nil || len(statuses) == 0 {
		t.Fatalf("statuses: %v", err)
	}
	if _, err := storage.UpsertStatusGate(statuses[0].ID, []string{slug}, []string{}); err != nil {
		t.Fatalf("gate: %v", err)
	}

	unused, err := CreateProject(ctx, 1, "Legacy Unused", "")
	if err != nil {
		t.Fatalf("create unused: %v", err)
	}

	before := map[string]bool{}
	for _, p := range storage.AllProjectPerms() {
		before[p] = storage.HasProjectPerm(standalone.ID, slug, p)
	}

	for i := 0; i < 2; i++ { // the migration runs on every start, so it must be safe to repeat
		if err := storage.MigrateLegacySiteRoles(); err != nil {
			t.Fatalf("migrate (run %d): %v", i+1, err)
		}
	}

	site, err := storage.ListSiteProjectRoles()
	if err != nil {
		t.Fatalf("list site: %v", err)
	}
	for _, d := range site {
		if d.Slug != storage.RoleOwner {
			t.Fatalf("site role %q should be gone", d.Slug)
		}
	}

	orgRole, err := storage.GetOrganizationRoleBySlug(usedOrg.ID, slug)
	if err != nil || orgRole == nil || orgRole.Name != "Legacy Tester" || orgRole.IsSystem {
		t.Fatalf("org copy: %+v err=%v", orgRole, err)
	}
	if strings.Join(orgRole.Permissions, ",") != strings.Join(legacyPerms, ",") {
		t.Fatalf("org copy perms: %v", orgRole.Permissions)
	}
	if !storage.HasProjectPerm(locked.ID, slug, storage.PermTasksEdit) || !storage.HasOrgPerm(usedOrg.ID, slug, storage.PermTasksStatus) {
		t.Fatal("org members should keep their permissions")
	}

	kept, err := storage.GetOrganizationRoleBySlug(customOrg.ID, slug)
	if err != nil || kept == nil || kept.ID != custom.ID || strings.Join(kept.Permissions, ",") != storage.PermTasksClaim {
		t.Fatalf("customized org role should be untouched: %+v err=%v", kept, err)
	}

	projRoles, err := storage.ListProjectCustomRoles(standalone.ID)
	if err != nil || len(projRoles) != 1 || projRoles[0].Slug != slug || projRoles[0].Name != "Checker" {
		t.Fatalf("standalone copy should take the project's rename: %+v err=%v", projRoles, err)
	}
	for p, had := range before {
		if storage.HasProjectPerm(standalone.ID, slug, p) != had {
			t.Fatalf("standalone permission %s changed", p)
		}
	}
	if labels, _ := storage.ListProjectRoleLabels(standalone.ID); labels[slug] != "" {
		t.Fatal("label should be cleared once the project owns the role")
	}

	if gatedRoles, _ := storage.ListProjectCustomRoles(gated.ID); len(gatedRoles) != 1 || gatedRoles[0].Slug != slug {
		t.Fatalf("status gate should keep its role: %+v", gatedRoles)
	}
	if unusedRoles, _ := storage.ListProjectCustomRoles(unused.ID); len(unusedRoles) != 0 {
		t.Fatalf("unused project should get no roles: %+v", unusedRoles)
	}
}

func TestOrgOwnersBecomeProjectOwners(t *testing.T) {
	ctx := context.Background()
	org, err := CreateOrganizationForUser(ctx, 1, "Co-owner Org", "")
	if err != nil {
		t.Fatalf("create org: %v", err)
	}
	if err := storage.UpsertOrganizationMember(org.ID, 2, storage.RoleOwner); err != nil {
		t.Fatalf("org co-owner: %v", err)
	}
	if err := upsertTestOrgMember(t, org.ID, 3, testRoleEditor); err != nil {
		t.Fatalf("org editor: %v", err)
	}

	locked, err := CreateProjectForUser(ctx, 1, CreateProjectInput{
		Name: "Co-owner Locked", OrganizationID: &org.ID, ImportMode: storage.OrgImportLock,
	})
	if err != nil {
		t.Fatalf("create locked: %v", err)
	}
	copied, err := CreateProjectForUser(ctx, 1, CreateProjectInput{
		Name: "Co-owner Copy", OrganizationID: &org.ID, ImportMode: storage.OrgImportCopy,
	})
	if err != nil {
		t.Fatalf("create copy: %v", err)
	}
	for _, pid := range []int{locked.ID, copied.ID} {
		role, err := storage.GetProjectRole(pid, 2)
		if err != nil || role != storage.RoleOwner {
			t.Fatalf("org owner in project %d: role=%q err=%v", pid, role, err)
		}
	}

	members, err := storage.ListProjectMembers(locked.ID)
	if err != nil {
		t.Fatalf("members: %v", err)
	}
	for _, m := range members {
		if m.UserID == 1 && m.Inherited {
			t.Fatal("project creator is not inherited from the org")
		}
		if m.UserID == 2 && !m.Inherited {
			t.Fatal("org owner on a locked project comes from the org")
		}
	}

	// On the copied project, only an owner may change another owner, and nobody may change the creator.
	if err := storage.UpsertProjectMember(copied.ID, 3, testRoleEditor); err != nil {
		t.Fatalf("copy editor: %v", err)
	}
	mgr, err := CreateProjectCustomRoleForUser(ctx, 1, copied.ID, CreateSiteProjectRoleInput{
		Slug: "manager", Name: "Manager", Permissions: []string{storage.PermProjectManage},
	})
	if err != nil {
		t.Fatalf("manager role: %v", err)
	}
	if err := storage.UpsertProjectMember(copied.ID, 3, mgr.Slug); err != nil {
		t.Fatalf("make manager: %v", err)
	}
	if err := UpdateProjectMemberRole(ctx, 3, copied.ID, 2, testRoleEditor); !errors.Is(err, ErrForbidden) {
		t.Fatalf("manager changing an owner: err=%v want forbidden", err)
	}
	if err := RemoveProjectMember(ctx, 3, copied.ID, 2); !errors.Is(err, ErrForbidden) {
		t.Fatalf("manager removing an owner: err=%v want forbidden", err)
	}
	if err := UpdateProjectMemberRole(ctx, 3, copied.ID, 1, testRoleEditor); !errors.Is(err, ErrValidation) {
		t.Fatalf("changing the creator: err=%v want validation", err)
	}
	if err := UpdateProjectMemberRole(ctx, 1, copied.ID, 2, testRoleEditor); err != nil {
		t.Fatalf("owner changing a co-owner: %v", err)
	}
	if role, _ := storage.GetProjectRole(copied.ID, 2); role != testRoleEditor {
		t.Fatalf("co-owner role after change: %q", role)
	}
}
