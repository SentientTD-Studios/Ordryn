package domain

import (
	"context"
	"errors"
	"slices"
	"testing"

	"GoTodo/internal/storage"
)

func TestCompanyProjectSharing(t *testing.T) {
	useFreshDatabase(t, "gotodo_company_access_test")
	t.Cleanup(clearSetupToken)
	ctx := context.Background()

	token, err := IssueSetupToken()
	if err != nil {
		t.Fatal(err)
	}
	admin, err := CompleteSetup(ctx, SetupInput{
		Token: token, Email: "owner@example.com", UserName: "owner", Password: "password1",
		ConfirmPassword: "password1", Timezone: "UTC", SiteMode: storage.SiteModeCompany,
	}, true)
	if err != nil {
		t.Fatalf("setup: %v", err)
	}

	// Site roles are the only roles: the catalog carries project permissions, and
	// nobody can create organizations.
	catalog := map[string]bool{}
	for _, p := range storage.SitePermissionCatalog() {
		catalog[p.ID] = true
	}
	if !catalog[storage.PermTasksCreate] || !catalog[storage.PermProjectManage] || !catalog[storage.SitePermTeamsManage] || catalog[storage.SitePermOrgsCreate] {
		t.Fatalf("company site role catalog = %v", catalog)
	}

	userRole := mustRoleID(t, storage.MemberRoleName)
	managerRole := mustRoleID(t, "Manager")
	mk := func(email, name string, roleID int) int {
		t.Helper()
		created, err := CreateAccountForUser(ctx, admin, CreateAccountInput{FirstName: "Test", LastName: "Person", Email: email, UserName: name, SiteRoleID: roleID})
		if err != nil {
			t.Fatalf("create %s: %v", name, err)
		}
		return created.UserID
	}
	mgr := mk("mgr@example.com", "mgr", managerRole)
	ann := mk("ann@example.com", "ann", userRole)
	ben := mk("ben@example.com", "ben", userRole)
	vic := mk("vic@example.com", "vic", userRole)

	role := func(projectID, userID int) string {
		t.Helper()
		r, err := storage.GetProjectRole(projectID, userID)
		if err != nil {
			t.Fatal(err)
		}
		return r
	}
	can := func(projectID, userID int, perm string) bool {
		return storage.HasProjectPerm(projectID, role(projectID, userID), perm)
	}

	pod, err := CreateOrganizationForUser(ctx, admin, "Pod One", "")
	if err != nil {
		t.Fatal(err)
	}
	for _, u := range []int{mgr, ann} {
		if _, err := storage.AddOrganizationMember(pod.ID, u, storage.CompanyMemberRole); err != nil {
			t.Fatal(err)
		}
	}
	if r, _ := storage.GetOrganizationRole(pod.ID, admin); r != "" {
		t.Fatal("creating a company team must not put its creator on it")
	}

	proj, err := CreateProject(ctx, admin, "Pod board", "")
	if err != nil {
		t.Fatal(err)
	}
	if role(proj.ID, mgr) != "" {
		t.Fatal("a new project is closed until shared")
	}

	// Sharing with a team gives each member their site role on the project.
	if err := ShareProjectForUser(ctx, admin, proj.ID, ProjectShareInput{TeamID: pod.ID}); err != nil {
		t.Fatalf("share with team: %v", err)
	}
	if !can(proj.ID, ann, storage.PermTasksCreate) || can(proj.ID, ann, storage.PermProjectManage) {
		t.Fatal("a User on the team should create tasks but not manage the project")
	}
	if !can(proj.ID, mgr, storage.PermProjectManage) {
		t.Fatal("a Manager on the team should manage the project")
	}

	// Manage project lets the manager share further; a User can't.
	if err := ShareProjectForUser(ctx, mgr, proj.ID, ProjectShareInput{Username: "vic", Level: storage.AccessView}); err != nil {
		t.Fatalf("manager shares view-only: %v", err)
	}
	if role(proj.ID, vic) != storage.CompanyViewRoleSlug || can(proj.ID, vic, storage.PermTasksCreate) {
		t.Fatal("a view-only share should open the project without task permissions")
	}
	if err := ShareProjectForUser(ctx, ann, proj.ID, ProjectShareInput{Username: "ben"}); !errors.Is(err, ErrForbidden) {
		t.Fatalf("user sharing: err=%v, want ErrForbidden", err)
	}

	// The higher level wins when someone is reached twice.
	companyID, _ := storage.CompanyOrganizationID()
	if err := ShareProjectForUser(ctx, admin, proj.ID, ProjectShareInput{TeamID: companyID, Level: storage.AccessView}); err != nil {
		t.Fatal(err)
	}
	if role(proj.ID, ben) != storage.CompanyViewRoleSlug {
		t.Fatal("sharing with the Company team should reach everyone")
	}
	if !can(proj.ID, ann, storage.PermTasksCreate) {
		t.Fatal("full access through a team should beat view-only through the company")
	}

	// Site role changes follow people onto their projects.
	if _, _, err := SetUserSiteRoleForAdmin(ctx, admin, ann, managerRole); err != nil {
		t.Fatal(err)
	}
	if !can(proj.ID, ann, storage.PermProjectManage) {
		t.Fatal("promotion to Manager should apply on the project")
	}
	if err := RemoveOrganizationMemberForUser(ctx, admin, pod.ID, ann); err != nil {
		t.Fatal(err)
	}
	if role(proj.ID, ann) != storage.CompanyViewRoleSlug {
		t.Fatalf("leaving the team should fall back to the company's view share, got %q", role(proj.ID, ann))
	}

	// Editing a site role changes what it allows everywhere, at once.
	if _, err := storage.AddOrganizationMember(pod.ID, ben, storage.CompanyMemberRole); err != nil {
		t.Fatal(err)
	}
	if !can(proj.ID, ben, storage.PermTasksEdit) {
		t.Fatal("User should edit tasks by default")
	}
	user, _ := storage.GetSiteRole(userRole)
	// The admin UI sends only catalog permissions; legacy ones stay on the role.
	perms := slices.DeleteFunc(slices.Clone(user.Permissions), func(p string) bool {
		return p == storage.PermTasksEdit || !storage.IsSitePermInCatalog(p)
	})
	if _, err := UpdateSiteRoleForAdmin(ctx, admin, userRole, nil, &perms); err != nil {
		t.Fatal(err)
	}
	if can(proj.ID, ben, storage.PermTasksEdit) {
		t.Fatal("removing a permission from the site role should apply on the project")
	}

	// Removing a share takes away the access it gave.
	grants, _, err := ListProjectSharesForUser(ctx, admin, proj.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, g := range grants {
		if g.OrganizationID != nil && *g.OrganizationID == companyID {
			if err := UnshareProjectForUser(ctx, mgr, proj.ID, g.ID); err != nil {
				t.Fatal(err)
			}
		}
	}
	if role(proj.ID, ann) != "" {
		t.Fatal("unsharing the company should close the project to people with no other share")
	}

	// Admins reach every project without showing up as members.
	mine, err := CreateProject(ctx, mgr, "Manager's board", "")
	if err != nil {
		t.Fatal(err)
	}
	if !can(mine.ID, admin, storage.PermProjectManage) {
		t.Fatal("admins should manage every project")
	}
	members, err := storage.ListProjectMembers(mine.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range members {
		if m.UserID == admin {
			t.Fatal("an admin reached only as an admin should not be listed as a member")
		}
	}

	// Per-project and per-team roles and invites are gone.
	if _, err := InviteToProject(ctx, admin, proj.ID, "ben", storage.CompanyViewRoleSlug); !errors.Is(err, ErrValidation) {
		t.Fatalf("project invite: err=%v, want ErrValidation", err)
	}
	if _, err := CreateProjectCustomRoleForUser(ctx, admin, proj.ID, CreateSiteProjectRoleInput{Slug: "lead", Name: "Lead"}); !errors.Is(err, ErrValidation) {
		t.Fatalf("project role: err=%v, want ErrValidation", err)
	}
	if _, err := CreateOrganizationRoleForUser(ctx, admin, pod.ID, CreateSiteProjectRoleInput{Slug: "lead", Name: "Lead"}); !errors.Is(err, ErrValidation) {
		t.Fatalf("team role: err=%v, want ErrValidation", err)
	}
	if _, err := CreateOrganizationForUser(ctx, ben, "Ben's team", ""); !errors.Is(err, ErrForbidden) {
		t.Fatalf("user creating a team: err=%v, want ErrForbidden", err)
	}
}
