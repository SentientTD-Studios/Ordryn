package domain

import (
	"context"
	"errors"
	"slices"
	"testing"

	"GoTodo/internal/storage"
)

func TestGuestAccounts(t *testing.T) {
	useFreshDatabase(t, "gotodo_guest_accounts_test")
	t.Cleanup(clearSetupToken)
	storage.InvalidateGuestPolicyCache()
	t.Cleanup(storage.InvalidateGuestPolicyCache)
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
	userRole := mustRoleID(t, storage.MemberRoleName)
	managerRole := mustRoleID(t, "Manager")
	role := func(projectID, userID int) string {
		t.Helper()
		r, err := storage.GetProjectRole(projectID, userID)
		if err != nil {
			t.Fatal(err)
		}
		return r
	}
	newAccount := func(email, name string, guest bool) (int, error) {
		c, err := CreateAccountForUser(ctx, admin, CreateAccountInput{
			FirstName: "T", LastName: "P", Email: email, UserName: name, Guest: guest,
		})
		if err != nil {
			return 0, err
		}
		return c.UserID, nil
	}

	// Guests are off until an admin turns them on.
	if _, err := newAccount("early@example.com", "early", true); !errors.Is(err, ErrValidation) {
		t.Fatalf("guest while disabled: err=%v", err)
	}
	if _, err := SaveGuestPolicyForAdmin(ctx, admin, GuestPolicyInput{Enabled: true, AccessLevel: "view"}); err != nil {
		t.Fatalf("enable guests: %v", err)
	}

	ann, err := newAccount("ann@example.com", "ann", false)
	if err != nil {
		t.Fatal(err)
	}
	gus, err := newAccount("gus@example.com", "gus", true)
	if err != nil {
		t.Fatalf("create guest: %v", err)
	}
	if !storage.IsGuestUser(gus) {
		t.Fatal("expected a guest")
	}
	companyID, _ := storage.CompanyOrganizationID()
	if r, _ := storage.GetOrganizationRole(companyID, gus); r != "" {
		t.Fatal("guests must not be in the company")
	}
	// A non-admin can't make guests or change the policy.
	if err := SetGuestForAdmin(ctx, ann, gus, false); !errors.Is(err, ErrForbidden) {
		t.Fatalf("non-admin toggle: err=%v", err)
	}
	if _, err := SaveGuestPolicyForAdmin(ctx, ann, GuestPolicyInput{Enabled: true}); !errors.Is(err, ErrForbidden) {
		t.Fatalf("non-admin policy: err=%v", err)
	}

	proj, err := CreateProject(ctx, admin, "Client board", "")
	if err != nil {
		t.Fatal(err)
	}
	// A share with the whole company never reaches guests.
	if err := ShareProjectForUser(ctx, admin, proj.ID, ProjectShareInput{TeamID: companyID}); err != nil {
		t.Fatal(err)
	}
	if role(proj.ID, gus) != "" || role(proj.ID, ann) == "" {
		t.Fatalf("company share: gus=%q ann=%q", role(proj.ID, gus), role(proj.ID, ann))
	}

	// Team shares don't reach guests unless the policy says so.
	pod, err := CreateOrganizationForUser(ctx, admin, "Client pod", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := storage.AddOrganizationMember(pod.ID, gus, storage.CompanyMemberRole); err != nil {
		t.Fatal(err)
	}
	teamProj, err := CreateProject(ctx, admin, "Pod board", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := ShareProjectForUser(ctx, admin, teamProj.ID, ProjectShareInput{TeamID: pod.ID}); err != nil {
		t.Fatal(err)
	}
	if role(teamProj.ID, gus) != "" {
		t.Fatal("team share reached a guest without via_teams")
	}

	// A personal full share is capped to read only under a "view" policy.
	if err := ShareProjectForUser(ctx, admin, proj.ID, ProjectShareInput{Username: "gus", RoleID: managerRole}); err != nil {
		t.Fatal(err)
	}
	if got := role(proj.ID, gus); got != storage.CompanyViewRoleSlug {
		t.Fatalf("view policy: gus role=%q", got)
	}

	// Full access uses the weaker of the share's role and the guest role.
	if _, err := SaveGuestPolicyForAdmin(ctx, admin, GuestPolicyInput{Enabled: true, AccessLevel: "full", RoleID: managerRole}); !errors.Is(err, ErrValidation) {
		t.Fatalf("manager as guest role should be refused (site permissions): err=%v", err)
	}
	if _, err := SaveGuestPolicyForAdmin(ctx, admin, GuestPolicyInput{Enabled: true, AccessLevel: "full", RoleID: userRole, ViaTeams: true}); err != nil {
		t.Fatalf("full policy: %v", err)
	}
	if got := role(proj.ID, gus); got != storage.CompanySiteRoleSlug(userRole) {
		t.Fatalf("full policy: gus role=%q, want the guest role", got)
	}
	if got := role(teamProj.ID, gus); got != storage.CompanySiteRoleSlug(userRole) {
		t.Fatalf("via_teams: gus role on team project=%q", got)
	}

	// Guests can't get a role with site permissions, and a role guests hold
	// can't gain them.
	if _, _, err := SetUserSiteRoleForAdmin(ctx, admin, gus, managerRole); !errors.Is(err, ErrValidation) {
		t.Fatalf("guest to Manager: err=%v", err)
	}
	cur, _ := storage.GetSiteRole(userRole)
	withAudit := append(slices.Clone(cur.Permissions), storage.SitePermAuditView)
	if _, err := UpdateSiteRoleForAdmin(ctx, admin, userRole, nil, &withAudit); !errors.Is(err, ErrValidation) {
		t.Fatalf("adding a site permission to a guest-held role: err=%v", err)
	}
	if storage.UserHasSitePermission(gus, storage.SitePermQueriesSite) {
		t.Fatal("guest should hold no site-wide query permission")
	}

	// Guests only find people on their projects. (A member would be on the
	// company-shared board with gus, so use another guest who shares nothing.)
	if _, err := newAccount("zed@example.com", "zed", true); err != nil {
		t.Fatal(err)
	}
	names, err := SearchUsernames(ctx, gus, "ze", 0)
	if err != nil || len(names) != 0 {
		t.Fatalf("guest directory search: %v %v", names, err)
	}
	names, _ = SearchUsernames(ctx, gus, "ow", 0)
	if !slices.Contains(names, "owner") {
		t.Fatalf("guest should find people on shared projects: %v", names)
	}

	// Back to a member: rejoins the company and its shares.
	if err := SetGuestForAdmin(ctx, admin, gus, false); err != nil {
		t.Fatalf("make member: %v", err)
	}
	if r, _ := storage.GetOrganizationRole(companyID, gus); r == "" {
		t.Fatal("member should rejoin the company")
	}
	if err := SetGuestForAdmin(ctx, admin, admin, true); !errors.Is(err, ErrValidation) {
		t.Fatalf("self toggle: err=%v", err)
	}
}
