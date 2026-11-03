package domain

import (
	"context"
	"errors"
	"testing"

	"GoTodo/internal/storage"
)

func TestSiteOwnerOutranksAdmins(t *testing.T) {
	useFreshDatabase(t, "gotodo_site_owner_test")
	t.Cleanup(clearSetupToken)
	ctx := context.Background()

	token, err := IssueSetupToken()
	if err != nil {
		t.Fatal(err)
	}
	owner, err := CompleteSetup(ctx, SetupInput{
		Token: token, Email: "owner@example.com", UserName: "owner", Password: "password1",
		ConfirmPassword: "password1", Timezone: "UTC", SiteMode: storage.SiteModeCompany,
	}, true)
	if err != nil {
		t.Fatalf("setup: %v", err)
	}
	if !storage.IsSiteOwner(owner) {
		t.Fatal("whoever runs setup should own the site")
	}

	adminRole := mustRoleID(t, storage.AdminRoleName)
	userRole := mustRoleID(t, storage.MemberRoleName)
	mustCreate := func(actor int, in CreateAccountInput) int {
		t.Helper()
		created, err := CreateAccountForUser(ctx, actor, in)
		if err != nil {
			t.Fatalf("create %s: %v", in.Email, err)
		}
		return created.UserID
	}
	ada := mustCreate(owner, CreateAccountInput{FirstName: "Test", LastName: "Person", Email: "ada@example.com", UserName: "ada", SiteRoleID: adminRole})
	ben := mustCreate(owner, CreateAccountInput{FirstName: "Test", LastName: "Person", Email: "ben@example.com", UserName: "ben", SiteRoleID: adminRole})
	cal := mustCreate(owner, CreateAccountInput{FirstName: "Test", LastName: "Person", Email: "cal@example.com", UserName: "cal"})

	// An admin who isn't the owner can't touch admin-level accounts.
	if _, err := CreateAccountForUser(ctx, ada, CreateAccountInput{FirstName: "Test", LastName: "Person", Email: "dee@example.com", UserName: "dee", SiteRoleID: adminRole}); !errors.Is(err, ErrForbidden) {
		t.Fatalf("admin creating an admin: err=%v, want ErrForbidden", err)
	}
	for name, target := range map[string]int{"another admin": ben, "the owner": owner} {
		if err := SetAccountActiveForUser(ctx, ada, target, false); !errors.Is(err, ErrForbidden) {
			t.Errorf("admin deactivating %s: err=%v, want ErrForbidden", name, err)
		}
		if err := RequireCanEditAdminAccount(ada, target); !errors.Is(err, ErrForbidden) {
			t.Errorf("admin renaming %s: err=%v, want ErrForbidden", name, err)
		}
	}
	if _, _, err := SetUserSiteRoleForAdmin(ctx, ada, ben, userRole); !errors.Is(err, ErrForbidden) {
		t.Fatalf("admin demoting an admin: err=%v, want ErrForbidden", err)
	}
	if _, _, err := SetUserSiteRoleForAdmin(ctx, ada, cal, adminRole); !errors.Is(err, ErrForbidden) {
		t.Fatalf("admin promoting someone to admin: err=%v, want ErrForbidden", err)
	}
	// ...but still manages everyone else.
	if err := SetAccountActiveForUser(ctx, ada, cal, false); err != nil {
		t.Fatalf("admin deactivating a regular account: %v", err)
	}
	if err := SetAccountActiveForUser(ctx, ada, cal, true); err != nil {
		t.Fatal(err)
	}

	// The owner manages admins, and nobody changes the owner.
	if err := SetAccountActiveForUser(ctx, owner, ada, false); err != nil {
		t.Fatalf("owner deactivating an admin: %v", err)
	}
	if err := SetAccountActiveForUser(ctx, owner, ada, true); err != nil {
		t.Fatal(err)
	}
	if _, _, err := SetUserSiteRoleForAdmin(ctx, ben, owner, userRole); !errors.Is(err, ErrForbidden) {
		t.Fatalf("demoting the owner: err=%v, want ErrForbidden", err)
	}

	// In company mode every admin sees and manages every team.
	team, err := CreateOrganizationForUser(ctx, owner, "Ops", "")
	if err != nil {
		t.Fatal(err)
	}
	orgs, err := ListOrganizationsForUser(ctx, ada)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, o := range orgs {
		found = found || o.ID == team.ID
	}
	if !found {
		t.Fatal("an admin should see teams they didn't create")
	}
	if _, err := requireOrgManage(team.ID, ada); err != nil {
		t.Fatalf("admin managing a team they aren't on: %v", err)
	}
	if _, err := requireOrgAccess(team.ID, cal); !errors.Is(err, ErrNotFound) {
		t.Fatalf("regular account opening a team they aren't on: err=%v, want ErrNotFound", err)
	}

	// Ownership moves only by transfer, and only to an admin.
	if err := TransferSiteOwnership(ctx, ada, ben); !errors.Is(err, ErrForbidden) {
		t.Fatalf("admin transferring ownership: err=%v, want ErrForbidden", err)
	}
	if err := TransferSiteOwnership(ctx, owner, cal); !errors.Is(err, ErrValidation) {
		t.Fatalf("transferring to a non-admin: err=%v, want ErrValidation", err)
	}
	if err := TransferSiteOwnership(ctx, owner, ben); err != nil {
		t.Fatalf("transfer: %v", err)
	}
	if !storage.IsSiteOwner(ben) || storage.IsSiteOwner(owner) {
		t.Fatal("ownership should now be ben's")
	}
	if !storage.UserHasSitePermission(owner, storage.SitePermAdmin) {
		t.Fatal("the previous owner stays an admin")
	}
	if err := SetAccountActiveForUser(ctx, owner, ada, false); !errors.Is(err, ErrForbidden) {
		t.Fatalf("former owner deactivating an admin: err=%v, want ErrForbidden", err)
	}

	// Sites from before ownership get their earliest active admin as owner.
	pool, err := storage.OpenDatabase()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE site_settings SET owner_user_id = NULL`); err != nil {
		t.Fatal(err)
	}
	if err := storage.EnsureSiteOwner(); err != nil {
		t.Fatal(err)
	}
	if !storage.IsSiteOwner(owner) {
		t.Fatal("backfill should pick the earliest active admin")
	}
}
