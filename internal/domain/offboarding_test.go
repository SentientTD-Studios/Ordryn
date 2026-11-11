package domain

import (
	"context"
	"errors"
	"testing"

	"GoTodo/internal/storage"
)

func siteRoleIDByName(t *testing.T, name string) int {
	t.Helper()
	roles, err := storage.ListSiteRoles()
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range roles {
		if r.Name == name {
			return r.ID
		}
	}
	t.Fatalf("no site role %q", name)
	return 0
}

func TestOffboardHandsOffWork(t *testing.T) {
	ctx := context.Background()
	owner := setupCompanySite(t, "gotodo_offboarding_test")
	manager := siteRoleIDByName(t, "Manager")
	leaving := createCompanyAccount(t, owner, "leaving", manager)
	successor := createCompanyAccount(t, owner, "successor", 0)
	bystander := createCompanyAccount(t, owner, "bystander", 0)

	proj, err := CreateProject(ctx, leaving, "Leaving's board", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := SetProjectWorkflowMode(ctx, leaving, proj.ID, storage.WorkflowKanban); err != nil {
		t.Fatal(err)
	}
	pid := proj.ID
	task, err := CreateTask(ctx, leaving, CreateTaskInput{Title: "Ship it", ProjectID: &pid})
	if err != nil {
		t.Fatal(err)
	}
	if err := ClaimTaskForUser(ctx, leaving, task); err != nil {
		t.Fatal(err)
	}
	if _, _, err := storage.CreateAPIKey(leaving, "laptop"); err != nil {
		t.Fatal(err)
	}

	preview, err := OffboardingPreviewForUser(ctx, owner, leaving)
	if err != nil {
		t.Fatal(err)
	}
	if len(preview.ClaimedTaskIDs) != 1 || len(preview.OwnedProjectIDs) != 1 || preview.PersonalKeys != 1 {
		t.Fatalf("preview: %+v", preview)
	}
	if _, err := OffboardingPreviewForUser(ctx, bystander, leaving); !errors.Is(err, ErrForbidden) {
		t.Fatalf("non-manager preview: %v", err)
	}
	if _, err := OffboardAccountForUser(ctx, owner, leaving, OffboardInput{Claims: OffboardClaimsReassign}); !errors.Is(err, ErrValidation) {
		t.Fatalf("reassign without successor: %v", err)
	}
	if storage.IsUserBannedByID(leaving) {
		t.Fatal("a refused offboard must not deactivate")
	}

	// Projects transfer first, so the successor can take over the claim.
	res, err := OffboardAccountForUser(ctx, owner, leaving, OffboardInput{
		SuccessorID: successor, Claims: OffboardClaimsReassign, TransferProjects: true, RevokeKeys: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.ProjectsTransferred != 1 || res.ClaimsReassigned != 1 || res.KeysRevoked != 1 {
		t.Fatalf("result: %+v", res)
	}
	if !storage.IsUserBannedByID(leaving) {
		t.Fatal("account should be deactivated")
	}
	if by, _ := storage.GetTaskClaimedBy(task); by != successor {
		t.Fatalf("claimed by %d, want %d", by, successor)
	}
}

func TestOffboardReleasesClaimsTheSuccessorCantTake(t *testing.T) {
	ctx := context.Background()
	owner := setupCompanySite(t, "gotodo_offboarding_release_test")
	manager := siteRoleIDByName(t, "Manager")
	leaving := createCompanyAccount(t, owner, "leaver", manager)
	successor := createCompanyAccount(t, owner, "heir", 0)

	proj, err := CreateProject(ctx, leaving, "Private board", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := SetProjectWorkflowMode(ctx, leaving, proj.ID, storage.WorkflowKanban); err != nil {
		t.Fatal(err)
	}
	pid := proj.ID
	task, err := CreateTask(ctx, leaving, CreateTaskInput{Title: "Half done", ProjectID: &pid})
	if err != nil {
		t.Fatal(err)
	}
	if err := ClaimTaskForUser(ctx, leaving, task); err != nil {
		t.Fatal(err)
	}
	res, err := OffboardAccountForUser(ctx, owner, leaving, OffboardInput{SuccessorID: successor, Claims: OffboardClaimsReassign})
	if err != nil {
		t.Fatal(err)
	}
	if res.ClaimsReleased != 1 || res.ClaimsReassigned != 0 {
		t.Fatalf("result: %+v", res)
	}
	if by, _ := storage.GetTaskClaimedBy(task); by != 0 {
		t.Fatalf("claim should be released, got %d", by)
	}
}
