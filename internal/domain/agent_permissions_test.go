package domain

import (
	"context"
	"errors"
	"testing"

	"GoTodo/internal/storage"
)

// withoutMemberPerm takes perm off the User site role for the rest of the test.
func withoutMemberPerm(t *testing.T, perm string) {
	t.Helper()
	if err := storage.SetRolePermission(storage.MemberRoleName, perm, false); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = storage.SetRolePermission(storage.MemberRoleName, perm, true) })
}

func TestAgentPermissionsAreGrantedOnUpgrade(t *testing.T) {
	for _, perm := range storage.AgentSitePerms {
		has, err := storage.RoleHasPermission(storage.MemberRoleName, perm)
		if err != nil {
			t.Fatal(err)
		}
		if !has {
			t.Fatalf("User role should start with %s", perm)
		}
	}
}

func TestProjectAgentsNeedSitePermission(t *testing.T) {
	ctx := context.Background()
	proj, err := CreateProject(ctx, 2, "Agent perms "+uniqueHandle("p"), "")
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	ensureTestProjectRole(t, proj.ID, testRoleEditor)
	role := testRoleEditor
	in := AgentInput{Handle: uniqueHandle("perm"), Role: &role}

	a, err := CreateProjectAgent(2, proj.ID, in)
	if err != nil {
		t.Fatalf("create with permission: %v", err)
	}

	withoutMemberPerm(t, storage.SitePermAgentsProject)

	in.Handle = uniqueHandle("perm")
	if _, err := CreateProjectAgent(2, proj.ID, in); !errors.Is(err, ErrForbidden) {
		t.Fatalf("create without permission: want ErrForbidden, got %v", err)
	}
	name := "Renamed"
	if _, err := UpdateProjectAgent(2, proj.ID, a.ID, AgentInput{Name: &name}); !errors.Is(err, ErrForbidden) {
		t.Fatalf("configure without permission: want ErrForbidden, got %v", err)
	}
	if _, _, err := CreateAgentAPIKey(2, proj.ID, a.ID, "", nil); !errors.Is(err, ErrForbidden) {
		t.Fatalf("key without permission: want ErrForbidden, got %v", err)
	}
	off := false
	if _, err := UpdateProjectAgent(2, proj.ID, a.ID, AgentInput{Enabled: &off}); err != nil {
		t.Fatalf("pausing should not need the permission: %v", err)
	}
	if err := RemoveProjectAgent(2, proj.ID, a.ID); err != nil {
		t.Fatalf("removing should not need the permission: %v", err)
	}
}

func TestLocalAgentKeysNeedSitePermission(t *testing.T) {
	withoutMemberPerm(t, storage.SitePermAgentsLocal)
	if _, _, err := CreateLocalAgentKey(2, CreateLocalAgentKeyInput{Name: "blocked " + uniqueHandle("k")}); !errors.Is(err, ErrForbidden) {
		t.Fatalf("want ErrForbidden, got %v", err)
	}
}

func TestAgentInputOnlyPauses(t *testing.T) {
	off, on := false, true
	name := "x"
	cases := []struct {
		in   AgentInput
		want bool
	}{
		{AgentInput{Enabled: &off}, true},
		{AgentInput{Enabled: &on}, false},
		{AgentInput{Enabled: &off, Name: &name}, false},
		{AgentInput{}, false},
	}
	for i, c := range cases {
		if got := c.in.onlyPauses(); got != c.want {
			t.Errorf("case %d: got %v, want %v", i, got, c.want)
		}
	}
}
