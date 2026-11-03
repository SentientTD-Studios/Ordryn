package domain

import (
	"context"
	"errors"
	"testing"

	"GoTodo/internal/storage"
)

// setupCompanySite runs first-time setup in company mode and returns the owner.
func setupCompanySite(t *testing.T, db string) int {
	t.Helper()
	useFreshDatabase(t, db)
	t.Cleanup(clearSetupToken)
	token, err := IssueSetupToken()
	if err != nil {
		t.Fatal(err)
	}
	owner, err := CompleteSetup(context.Background(), SetupInput{
		Token: token, Email: "owner@example.com", UserName: "owner", Password: "password1",
		ConfirmPassword: "password1", Timezone: "UTC", SiteMode: storage.SiteModeCompany,
	}, true)
	if err != nil {
		t.Fatalf("setup: %v", err)
	}
	return owner
}

func createCompanyAccount(t *testing.T, actor int, name string, roleID int) int {
	t.Helper()
	created, err := CreateAccountForUser(context.Background(), actor, CreateAccountInput{
		FirstName: "Test", LastName: name, Email: name + "@example.com", UserName: name, SiteRoleID: roleID,
	})
	if err != nil {
		t.Fatalf("create %s: %v", name, err)
	}
	return created.UserID
}

func TestCompanyDefaultRolesAreSeeded(t *testing.T) {
	setupCompanySite(t, "gotodo_company_default_roles_test")
	for _, name := range []string{"Project Administrator", "Contributor", "Reader", "Manager"} {
		mustRoleID(t, name)
	}
	reader, err := storage.GetSiteRole(mustRoleID(t, "Reader"))
	if err != nil {
		t.Fatal(err)
	}
	if len(reader.Permissions) != 0 {
		t.Fatalf("Reader permissions = %v, want none", reader.Permissions)
	}
	// Seeded roles are ordinary roles: renamable and editable.
	owner, _ := storage.SiteOwnerID()
	viewer := "Viewer"
	perms := []string{storage.PermTasksComplete}
	updated, err := UpdateSiteRoleForAdmin(context.Background(), owner, reader.ID, &viewer, &perms)
	if err != nil {
		t.Fatalf("edit Reader: %v", err)
	}
	if updated.Label() != "Viewer" || len(updated.Permissions) != 1 {
		t.Fatalf("edited Reader = %q %v", updated.Label(), updated.Permissions)
	}
}

func TestCompanyNamesAreManaged(t *testing.T) {
	ctx := context.Background()
	owner := setupCompanySite(t, "gotodo_company_names_test")

	if _, err := CreateAccountForUser(ctx, owner, CreateAccountInput{Email: "nameless@example.com", UserName: "nameless"}); !errors.Is(err, ErrValidation) {
		t.Fatalf("create without names: err=%v, want ErrValidation", err)
	}

	hr, err := CreateSiteRoleForAdmin(ctx, owner, "HR", []string{storage.SitePermProfilesEdit})
	if err != nil {
		t.Fatal(err)
	}
	userRole := mustRoleID(t, storage.MemberRoleName)
	emp := createCompanyAccount(t, owner, "emp", userRole)
	clerk := createCompanyAccount(t, owner, "clerk", hr.ID)
	adminRole := mustRoleID(t, storage.AdminRoleName)
	admin := createCompanyAccount(t, owner, "boss", adminRole)

	// Employees can't rename themselves.
	if err := SetPersonNameForUser(ctx, emp, emp, "New", "Name"); !errors.Is(err, ErrForbidden) {
		t.Fatalf("self rename: err=%v, want ErrForbidden", err)
	}
	// Edit names reaches everyone but admins, and the holder themselves.
	if err := SetPersonNameForUser(ctx, clerk, emp, "  Emma ", " Price "); err != nil {
		t.Fatalf("HR renames employee: %v", err)
	}
	p, err := storage.GetUserProfileByID(emp)
	if err != nil {
		t.Fatal(err)
	}
	if p.FirstName != "Emma" || p.LastName != "Price" {
		t.Fatalf("names = %q %q, want trimmed", p.FirstName, p.LastName)
	}
	if err := SetPersonNameForUser(ctx, clerk, clerk, "Cleo", "Clark"); err != nil {
		t.Fatalf("HR renames self: %v", err)
	}
	if err := SetPersonNameForUser(ctx, clerk, admin, "X", "Y"); !errors.Is(err, ErrForbidden) {
		t.Fatalf("HR renames admin: err=%v, want ErrForbidden", err)
	}
	if err := SetPersonNameForUser(ctx, owner, admin, "Bo", "Ss"); err != nil {
		t.Fatalf("owner renames admin: %v", err)
	}
	if err := SetPersonNameForUser(ctx, clerk, emp, "", "Price"); !errors.Is(err, ErrValidation) {
		t.Fatalf("blank first name: err=%v, want ErrValidation", err)
	}

	// The People page lists everyone for a names-only holder, who can't create accounts.
	people, access, err := ListPeopleForUser(ctx, clerk)
	if err != nil {
		t.Fatalf("names-only people list: %v", err)
	}
	if access.Any() || len(people) < 3 {
		t.Fatalf("names-only access = %+v with %d people", access, len(people))
	}
	if err := SetAccountActiveForUser(ctx, clerk, emp, false); !errors.Is(err, ErrForbidden) {
		t.Fatalf("names-only deactivating: err=%v, want ErrForbidden", err)
	}

	// Names show as "First Last" to everyone else.
	members, err := storage.ListOrganizationMembers(mustCompanyID(t))
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, m := range members {
		if m.UserID == emp {
			found = m.FullName == "Emma Price"
		}
	}
	if !found {
		t.Fatal("company roster should carry the full name")
	}
}

func mustCompanyID(t *testing.T) int {
	t.Helper()
	id, err := storage.CompanyOrganizationID()
	if err != nil || id == 0 {
		t.Fatalf("company id = %d, %v", id, err)
	}
	return id
}

func TestCompanyShareRoles(t *testing.T) {
	ctx := context.Background()
	owner := setupCompanySite(t, "gotodo_company_share_roles_test")
	reader := mustRoleID(t, "Reader")
	contributor := mustRoleID(t, "Contributor")
	projectAdmin := mustRoleID(t, "Project Administrator")

	ann := createCompanyAccount(t, owner, "ann", reader)
	ben := createCompanyAccount(t, owner, "ben", reader)
	team, err := CreateOrganizationForUser(ctx, owner, "Builders", "")
	if err != nil {
		t.Fatal(err)
	}
	for _, u := range []int{ann, ben} {
		if _, err := storage.AddOrganizationMember(team.ID, u, storage.CompanyMemberRole); err != nil {
			t.Fatal(err)
		}
	}
	proj, err := CreateProject(ctx, owner, "Site build", "")
	if err != nil {
		t.Fatal(err)
	}
	role := func(userID int) string {
		t.Helper()
		r, err := storage.GetProjectRole(proj.ID, userID)
		if err != nil {
			t.Fatal(err)
		}
		return r
	}

	// No role on the share: each person's own site role.
	if err := ShareProjectForUser(ctx, owner, proj.ID, ProjectShareInput{TeamID: team.ID}); err != nil {
		t.Fatal(err)
	}
	if role(ann) != storage.CompanySiteRoleSlug(reader) {
		t.Fatalf("default share role = %q, want their own Reader role", role(ann))
	}
	// A role on the share overrides it for this project only.
	if err := ShareProjectForUser(ctx, owner, proj.ID, ProjectShareInput{TeamID: team.ID, RoleID: contributor}); err != nil {
		t.Fatal(err)
	}
	if role(ann) != storage.CompanySiteRoleSlug(contributor) {
		t.Fatalf("team share role = %q, want Contributor", role(ann))
	}
	if !storage.HasProjectPerm(proj.ID, role(ann), storage.PermTasksCreate) {
		t.Fatal("Contributor share should create tasks")
	}
	// The strongest role wins when someone is reached twice.
	if err := ShareProjectForUser(ctx, owner, proj.ID, ProjectShareInput{Username: "ann", RoleID: projectAdmin}); err != nil {
		t.Fatal(err)
	}
	if role(ann) != storage.CompanySiteRoleSlug(projectAdmin) || role(ben) != storage.CompanySiteRoleSlug(contributor) {
		t.Fatalf("roles = ann %q, ben %q; want Project Administrator and Contributor", role(ann), role(ben))
	}
	if !storage.HasProjectPerm(proj.ID, role(ann), storage.PermProjectManage) {
		t.Fatal("Project Administrator should manage the project")
	}
	// A personal view share doesn't lower a stronger team share.
	if err := ShareProjectForUser(ctx, owner, proj.ID, ProjectShareInput{Username: "ben", Level: storage.AccessView}); err != nil {
		t.Fatal(err)
	}
	if role(ben) != storage.CompanySiteRoleSlug(contributor) {
		t.Fatalf("ben = %q, want Contributor to beat view only", role(ben))
	}
	// The Admin role is site-wide and can't be handed out on a share.
	adminRole := mustRoleID(t, storage.AdminRoleName)
	if err := ShareProjectForUser(ctx, owner, proj.ID, ProjectShareInput{Username: "ben", RoleID: adminRole}); !errors.Is(err, ErrValidation) {
		t.Fatalf("share as Admin: err=%v, want ErrValidation", err)
	}

	// The share lists its role.
	grants, _, err := ListProjectSharesForUser(ctx, owner, proj.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, g := range grants {
		if g.OrganizationID != nil && (g.RoleID == nil || *g.RoleID != contributor || g.RoleLabel != "Contributor") {
			t.Fatalf("team grant role = %v %q", g.RoleID, g.RoleLabel)
		}
	}

	// Deleting a share's role falls back to each person's own role.
	temp, err := CreateSiteRoleForAdmin(ctx, owner, "Temp", []string{storage.PermTasksCreate, storage.PermTasksEdit,
		storage.PermTasksDelete, storage.PermTasksArchive, storage.PermTasksRestore, storage.PermTasksComplete,
		storage.PermTasksClaim, storage.PermTasksReorder, storage.PermTasksStatus, storage.PermTasksSprint, storage.PermTimeWrite})
	if err != nil {
		t.Fatal(err)
	}
	if err := ShareProjectForUser(ctx, owner, proj.ID, ProjectShareInput{TeamID: team.ID, RoleID: temp.ID}); err != nil {
		t.Fatal(err)
	}
	if role(ben) != storage.CompanySiteRoleSlug(temp.ID) {
		t.Fatalf("ben = %q, want Temp", role(ben))
	}
	if _, _, err := DeleteSiteRoleForAdmin(ctx, owner, temp.ID); err != nil {
		t.Fatal(err)
	}
	if role(ben) != storage.CompanySiteRoleSlug(reader) {
		t.Fatalf("after deleting the share's role, ben = %q, want their own Reader role", role(ben))
	}
}

func TestCompanyViewAllProjects(t *testing.T) {
	ctx := context.Background()
	owner := setupCompanySite(t, "gotodo_company_view_all_test")
	lead, err := CreateSiteRoleForAdmin(ctx, owner, "Leadership", []string{})
	if err != nil {
		t.Fatal(err)
	}
	cara := createCompanyAccount(t, owner, "cara", lead.ID)
	proj, err := CreateProject(ctx, owner, "Secret plans", "")
	if err != nil {
		t.Fatal(err)
	}
	if r, _ := storage.GetProjectRole(proj.ID, cara); r != "" {
		t.Fatalf("before view_all, role = %q", r)
	}

	perms := []string{storage.SitePermProjectsViewAll}
	if _, err := UpdateSiteRoleForAdmin(ctx, owner, lead.ID, nil, &perms); err != nil {
		t.Fatal(err)
	}
	if r, _ := storage.GetProjectRole(proj.ID, cara); r != storage.CompanyViewRoleSlug {
		t.Fatalf("with view_all, role = %q, want view", r)
	}
	members, err := storage.ListProjectMembers(proj.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range members {
		if m.UserID == cara {
			t.Fatal("view_all access should stay out of the member list")
		}
	}
	// A share still gives more.
	if err := ShareProjectForUser(ctx, owner, proj.ID, ProjectShareInput{Username: "cara", RoleID: mustRoleID(t, "Contributor")}); err != nil {
		t.Fatal(err)
	}
	if r, _ := storage.GetProjectRole(proj.ID, cara); r != storage.CompanySiteRoleSlug(mustRoleID(t, "Contributor")) {
		t.Fatalf("shared view_all holder role = %q, want Contributor", r)
	}
	// New projects pick them up too; losing the permission takes it away.
	other, err := CreateProject(ctx, owner, "Later", "")
	if err != nil {
		t.Fatal(err)
	}
	if r, _ := storage.GetProjectRole(other.ID, cara); r != storage.CompanyViewRoleSlug {
		t.Fatalf("new project role = %q, want view", r)
	}
	none := []string{}
	if _, err := UpdateSiteRoleForAdmin(ctx, owner, lead.ID, nil, &none); err != nil {
		t.Fatal(err)
	}
	if r, _ := storage.GetProjectRole(other.ID, cara); r != "" {
		t.Fatalf("after losing view_all, role = %q", r)
	}
}

func TestCompanyRoleManagersCantEscalate(t *testing.T) {
	ctx := context.Background()
	owner := setupCompanySite(t, "gotodo_company_role_managers_test")
	roleAdmin, err := CreateSiteRoleForAdmin(ctx, owner, "Role Admin",
		[]string{storage.SitePermRolesManage, storage.PermTasksCreate, storage.PermTasksEdit})
	if err != nil {
		t.Fatal(err)
	}
	rita := createCompanyAccount(t, owner, "rita", roleAdmin.ID)

	if _, _, err := ListSiteRolesForAdmin(ctx, rita); err != nil {
		t.Fatalf("role manager lists roles: %v", err)
	}
	if _, err := CreateSiteRoleForAdmin(ctx, rita, "Writers", []string{storage.PermTasksCreate}); err != nil {
		t.Fatalf("create within own permissions: %v", err)
	}
	if _, err := CreateSiteRoleForAdmin(ctx, rita, "Bosses", []string{storage.SitePermUsersManage}); !errors.Is(err, ErrForbidden) {
		t.Fatalf("create with a permission she lacks: err=%v, want ErrForbidden", err)
	}
	more := []string{storage.SitePermRolesManage, storage.PermTasksCreate, storage.PermTasksEdit, storage.SitePermSettingsManage}
	if _, err := UpdateSiteRoleForAdmin(ctx, rita, roleAdmin.ID, nil, &more); !errors.Is(err, ErrForbidden) {
		t.Fatalf("edit own role: err=%v, want ErrForbidden", err)
	}
	if _, err := UpdateSiteRoleForAdmin(ctx, rita, mustRoleID(t, "Manager"), nil, &[]string{}); !errors.Is(err, ErrForbidden) {
		t.Fatalf("edit a stronger role: err=%v, want ErrForbidden", err)
	}
	if _, _, err := DeleteSiteRoleForAdmin(ctx, rita, mustRoleID(t, "Manager")); !errors.Is(err, ErrForbidden) {
		t.Fatalf("delete a stronger role: err=%v, want ErrForbidden", err)
	}
	newName := "Watchers"
	if _, err := UpdateSiteRoleForAdmin(ctx, rita, mustRoleID(t, "Reader"), &newName, nil); err != nil {
		t.Fatalf("rename a role within reach: %v", err)
	}
	// Assigning roles stays with admins.
	emp := createCompanyAccount(t, owner, "emp", mustRoleID(t, storage.MemberRoleName))
	if _, _, err := SetUserSiteRoleForAdmin(ctx, rita, emp, roleAdmin.ID); !errors.Is(err, ErrForbidden) {
		t.Fatalf("role manager assigning roles: err=%v, want ErrForbidden", err)
	}
}

func TestCompanyProjectsHaveNoInvites(t *testing.T) {
	ctx := context.Background()
	owner := setupCompanySite(t, "gotodo_company_no_invites_test")
	createCompanyAccount(t, owner, "ivy", mustRoleID(t, storage.MemberRoleName))
	proj, err := CreateProject(ctx, owner, "Closed doors", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := InviteToProject(ctx, owner, proj.ID, "ivy", "site:1"); !errors.Is(err, ErrValidation) {
		t.Fatalf("project invite on a company site: err=%v, want ErrValidation", err)
	}
}

func TestCompanyTasksNeedAProject(t *testing.T) {
	ctx := context.Background()
	owner := setupCompanySite(t, "gotodo_company_tasks_need_project_test")
	proj, err := CreateProject(ctx, owner, "Ops", "")
	if err != nil {
		t.Fatal(err)
	}

	if _, err := CreateTask(ctx, owner, CreateTaskInput{Title: "Loose end"}); !errors.Is(err, ErrProjectRequired) {
		t.Fatalf("task without a project: err=%v, want ErrProjectRequired", err)
	}
	pid := proj.ID
	taskID, err := CreateTask(ctx, owner, CreateTaskInput{Title: "Tied up", ProjectID: &pid})
	if err != nil {
		t.Fatalf("task in a project: %v", err)
	}
	// Subtasks take their parent's project.
	if _, err := CreateTask(ctx, owner, CreateTaskInput{Title: "Step one", ParentID: &taskID}); err != nil {
		t.Fatalf("subtask: %v", err)
	}
	var none *int
	if _, err := UpdateTask(ctx, owner, taskID, UpdateTaskInput{ProjectID: &none}); !errors.Is(err, ErrProjectRequired) {
		t.Fatalf("clearing the project: err=%v, want ErrProjectRequired", err)
	}

	// A project with tasks can't be deleted (its tasks would lose their project).
	if err := DeleteProject(ctx, owner, proj.ID); !errors.Is(err, ErrConflict) {
		t.Fatalf("delete project with tasks: err=%v, want ErrConflict", err)
	}
	empty, err := CreateProject(ctx, owner, "Empty", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := DeleteProject(ctx, owner, empty.ID); err != nil {
		t.Fatalf("delete empty project: %v", err)
	}
}

func TestCompanyHomeContent(t *testing.T) {
	setupCompanySite(t, "gotodo_company_home_test")
	if got, err := storage.GetCompanyHome(); err != nil || got != "" {
		t.Fatalf("default home = %q, %v; want empty", got, err)
	}
	if err := storage.SetCompanyHome("# Welcome\nAsk **IT** for access."); err != nil {
		t.Fatal(err)
	}
	if got, _ := storage.GetCompanyHome(); got != "# Welcome\nAsk **IT** for access." {
		t.Fatalf("home = %q", got)
	}
}
