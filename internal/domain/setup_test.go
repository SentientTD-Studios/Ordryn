package domain

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	"GoTodo/internal/storage"
)

func TestCompleteSetupCreatesAdminAndConfiguresSite(t *testing.T) {
	useFreshDatabase(t, "gotodo_setup_test")
	t.Cleanup(clearSetupToken)
	ctx := context.Background()

	if required, err := storage.SetupRequired(); err != nil || !required {
		t.Fatalf("fresh site SetupRequired = %v, %v; want true", required, err)
	}
	token, err := IssueSetupToken()
	if err != nil || token == "" {
		t.Fatalf("IssueSetupToken = %q, %v; want a token", token, err)
	}
	if again, _ := IssueSetupToken(); again != token {
		t.Fatalf("token changed within one process: %q then %q", token, again)
	}

	in := SetupInput{
		Token:           "wrong-token",
		Email:           "owner@example.com",
		UserName:        "owner",
		Password:        "correct horse",
		ConfirmPassword: "correct horse",
		Timezone:        "Europe/Berlin",
		SiteName:        "Acme Tasks",
		SiteMode:        storage.SiteModeCompany,
		Registration:    SetupRegistrationOpen,
		EnableAPI:       true,
	}
	if _, err := CompleteSetup(ctx, in, true); !errors.Is(err, ErrForbidden) {
		t.Fatalf("wrong token: err=%v, want ErrForbidden", err)
	}

	// Dashes, spaces, and case don't matter when typing the token.
	in.Token = " " + strings.ToUpper(strings.ReplaceAll(token, "-", "")) + " "
	bad := in
	bad.SiteMode = "single"
	if _, err := CompleteSetup(ctx, bad, true); !errors.Is(err, ErrValidation) {
		t.Fatalf("bad mode: err=%v, want ErrValidation", err)
	}
	bad = in
	bad.ConfirmPassword = "something else"
	if _, err := CompleteSetup(ctx, bad, true); !errors.Is(err, ErrValidation) {
		t.Fatalf("mismatched passwords: err=%v, want ErrValidation", err)
	}
	if required, _ := storage.SetupRequired(); !required {
		t.Fatal("a rejected setup must not create an account")
	}

	adminID, err := CompleteSetup(ctx, in, true)
	if err != nil {
		t.Fatalf("CompleteSetup: %v", err)
	}
	profile, err := storage.GetUserProfileByID(adminID)
	if err != nil {
		t.Fatal(err)
	}
	if profile.UserName != "owner" || !slices.Contains(profile.Permissions, storage.SitePermAdmin) {
		t.Fatalf("admin = %q perms %v; want owner with admin permission", profile.UserName, profile.Permissions)
	}

	settings, err := storage.GetSiteSettings()
	if err != nil {
		t.Fatal(err)
	}
	// Company mode turns self-signup off whatever registration was asked for.
	if settings.SiteName != "Acme Tasks" || settings.DefaultTimezone != "Europe/Berlin" ||
		settings.EnableRegistration || !settings.EnableAPI ||
		settings.SiteMode != storage.SiteModeCompany {
		t.Fatalf("settings not applied: %+v", settings)
	}
	orgID, err := storage.CompanyOrganizationID()
	if err != nil || orgID == 0 {
		t.Fatalf("company organization = %d, %v; want one", orgID, err)
	}
	if role, err := storage.GetOrganizationRole(orgID, adminID); err != nil || role == "" {
		t.Fatalf("admin company role = %q, %v; want a member", role, err)
	}

	if required, _ := storage.SetupRequired(); required {
		t.Fatal("setup still required after it completed")
	}
	if token, _ := IssueSetupToken(); token != "" {
		t.Fatalf("IssueSetupToken after setup = %q, want empty", token)
	}
	if _, err := CompleteSetup(ctx, in, true); !errors.Is(err, ErrConflict) {
		t.Fatalf("second setup: err=%v, want ErrConflict", err)
	}
}
