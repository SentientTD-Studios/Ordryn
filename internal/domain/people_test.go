package domain

import (
	"context"
	"errors"
	"testing"

	"GoTodo/internal/storage"
)

func TestPeopleManagementInCompanyMode(t *testing.T) {
	useFreshDatabase(t, "gotodo_people_test")
	t.Cleanup(clearSetupToken)
	ctx := context.Background()

	token, err := IssueSetupToken()
	if err != nil {
		t.Fatal(err)
	}
	adminID, err := CompleteSetup(ctx, SetupInput{
		Token: token, Email: "admin@example.com", UserName: "admin", Password: "password1",
		ConfirmPassword: "password1", Timezone: "UTC", SiteMode: storage.SiteModeCompany,
		Registration: SetupRegistrationOpen,
	}, true)
	if err != nil {
		t.Fatalf("setup: %v", err)
	}
	if s, _ := storage.GetSiteSettings(); s.EnableRegistration {
		t.Fatal("company setup must turn self-signup off")
	}

	sales, err := CreateOrganizationForUser(ctx, adminID, "Sales", "")
	if err != nil {
		t.Fatal(err)
	}
	support, err := CreateOrganizationForUser(ctx, adminID, "Support", "")
	if err != nil {
		t.Fatal(err)
	}
	managerRole := mustRoleID(t, "Manager")

	create := func(actor int, in CreateAccountInput) (int, error) {
		t.Helper()
		created, err := CreateAccountForUser(ctx, actor, in)
		if err != nil {
			return 0, err
		}
		if created.Reset == nil {
			t.Fatalf("account %s has no set-password token", in.Email)
		}
		return created.UserID, nil
	}
	mustCreate := func(actor int, in CreateAccountInput) int {
		t.Helper()
		id, err := create(actor, in)
		if err != nil {
			t.Fatalf("create %s: %v", in.Email, err)
		}
		return id
	}

	// The admin picks the managers: the Manager site role, on the team they run.
	mia := mustCreate(adminID, CreateAccountInput{FirstName: "Test", LastName: "Person", Email: "mia@example.com", UserName: "mia", TeamID: sales.ID, SiteRoleID: managerRole})
	olga := mustCreate(adminID, CreateAccountInput{FirstName: "Test", LastName: "Person", Email: "olga@example.com", UserName: "olga", TeamID: sales.ID, SiteRoleID: managerRole})
	companyID, err := storage.CompanyOrganizationID()
	if err != nil {
		t.Fatal(err)
	}
	if role, _ := storage.GetOrganizationRole(companyID, mia); role == "" {
		t.Fatal("new accounts must join the company")
	}

	// A manager hires onto their own team, with the default site role only.
	noah := mustCreate(mia, CreateAccountInput{FirstName: "Test", LastName: "Person", Email: "noah@example.com", UserName: "noah", TeamID: sales.ID})
	for name, tc := range map[string]struct {
		in   CreateAccountInput
		want error
	}{
		"manager role":   {CreateAccountInput{FirstName: "Test", LastName: "Person", Email: "x1@example.com", UserName: "extra1", TeamID: sales.ID, SiteRoleID: managerRole}, ErrForbidden},
		"other team":     {CreateAccountInput{FirstName: "Test", LastName: "Person", Email: "x2@example.com", UserName: "extra2", TeamID: support.ID}, ErrForbidden},
		"no team":        {CreateAccountInput{FirstName: "Test", LastName: "Person", Email: "x3@example.com", UserName: "extra3"}, ErrValidation},
		"admin role":     {CreateAccountInput{FirstName: "Test", LastName: "Person", Email: "x4@example.com", UserName: "extra4", TeamID: sales.ID, SiteRoleID: mustRoleID(t, storage.AdminRoleName)}, ErrForbidden},
		"existing email": {CreateAccountInput{FirstName: "Test", LastName: "Person", Email: "noah@example.com", UserName: "extra5", TeamID: sales.ID}, ErrConflict},
	} {
		if _, err := create(mia, tc.in); !errors.Is(err, tc.want) {
			t.Errorf("manager create with %s: err=%v, want %v", name, err, tc.want)
		}
	}

	// A manager deactivates and reactivates their own members, and nobody else.
	if err := SetAccountActiveForUser(ctx, mia, noah, false); err != nil {
		t.Fatalf("manager deactivates member: %v", err)
	}
	if !storage.IsUserBannedByID(noah) {
		t.Fatal("deactivated account should be banned")
	}
	if err := SetAccountActiveForUser(ctx, mia, noah, true); err != nil || storage.IsUserBannedByID(noah) {
		t.Fatalf("manager reactivates member: %v", err)
	}
	for name, target := range map[string]int{"fellow manager": olga, "admin": adminID, "self": mia} {
		if err := SetAccountActiveForUser(ctx, mia, target, false); err == nil {
			t.Errorf("manager deactivating %s: want an error", name)
		}
	}
	paul := mustCreate(adminID, CreateAccountInput{FirstName: "Test", LastName: "Person", Email: "paul@example.com", UserName: "paul", TeamID: support.ID})
	if err := SetAccountActiveForUser(ctx, mia, paul, false); !errors.Is(err, ErrForbidden) {
		t.Fatalf("manager deactivating another team's member: err=%v, want ErrForbidden", err)
	}

	// A company-wide people manager reaches every team, but not admins.
	hr, err := CreateSiteRoleForAdmin(ctx, adminID, "HR", []string{storage.SitePermUsersManage})
	if err != nil {
		t.Fatal(err)
	}
	hank := mustCreate(adminID, CreateAccountInput{FirstName: "Test", LastName: "Person", Email: "hank@example.com", UserName: "hank", SiteRoleID: hr.ID})
	if err := SetAccountActiveForUser(ctx, hank, paul, false); err != nil {
		t.Fatalf("people manager deactivates any member: %v", err)
	}
	if err := SetAccountActiveForUser(ctx, hank, mia, false); err != nil {
		t.Fatalf("people manager deactivates a team manager: %v", err)
	}
	if err := SetAccountActiveForUser(ctx, hank, adminID, false); !errors.Is(err, ErrForbidden) {
		t.Fatalf("people manager deactivating an admin: err=%v, want ErrForbidden", err)
	}
	if _, err := create(hank, CreateAccountInput{FirstName: "Test", LastName: "Person", Email: "x6@example.com", UserName: "extra6", SiteRoleID: hr.ID}); !errors.Is(err, ErrForbidden) {
		t.Fatalf("people manager creating another people manager: err=%v, want ErrForbidden", err)
	}

	// A deactivated manager loses their reach.
	if access, _ := PeopleAccessFor(mia); access.Any() {
		t.Fatal("deactivated manager should manage nobody")
	}

	// Team-managed people show up for their manager only.
	people, _, err := ListPeopleForUser(ctx, olga)
	if err != nil {
		t.Fatal(err)
	}
	seen := map[int]bool{}
	for _, p := range people {
		seen[p.ID] = true
	}
	if !seen[noah] || seen[paul] || seen[hank] {
		t.Fatalf("olga sees %v; want Sales members only", seen)
	}
	if _, _, err := ListPeopleForUser(ctx, noah); !errors.Is(err, ErrForbidden) {
		t.Fatalf("regular member listing people: err=%v, want ErrForbidden", err)
	}
}
