package storage

import (
	"strings"
	"testing"
)

func TestValidInviteRole(t *testing.T) {
	if !ValidInviteRole(RoleEditor) || !ValidInviteRole(RoleViewer) {
		t.Fatal("editor/viewer should be valid invite roles")
	}
	if ValidInviteRole(RoleOwner) || ValidInviteRole("admin") {
		t.Fatal("owner/admin should not be invite roles")
	}
}

func TestRoleCanWrite(t *testing.T) {
	if !RoleCanWrite(RoleOwner) || !RoleCanWrite(RoleEditor) {
		t.Fatal("owner/editor should write")
	}
	if RoleCanWrite(RoleViewer) {
		t.Fatal("viewer should not write")
	}
}

func TestTaskVisibleCondition(t *testing.T) {
	cond := TaskVisibleCondition("t", "$1")
	if cond == "" || len(cond) < 20 {
		t.Fatalf("unexpected condition: %s", cond)
	}
	if strings.Contains(cond, "pm.role IN ('owner', 'editor')") {
		t.Fatalf("full visible condition should not restrict membership roles: %s", cond)
	}
	if !strings.Contains(cond, "pm.project_id = t.project_id") {
		t.Fatalf("aliased condition should correlate to outer alias: %s", cond)
	}

	unaliased := TaskVisibleCondition("", "$1")
	if !strings.Contains(unaliased, "pm.project_id = tasks.project_id") {
		t.Fatalf("unaliased condition should correlate to tasks.project_id: %s", unaliased)
	}
}

func TestTaskHomeVisibleCondition(t *testing.T) {
	cond := TaskHomeVisibleCondition("t", "$1")
	if !strings.Contains(cond, "pm.role IN ('owner', 'editor')") {
		t.Fatalf("home visible condition should restrict membership to owner/editor: %s", cond)
	}
}

func TestTaskListVisibleCondition(t *testing.T) {
	project := 7
	projectZero := 0

	home := TaskListVisibleCondition("t", "$1", nil)
	if !strings.Contains(home, "pm.role IN ('owner', 'editor')") {
		t.Fatalf("unscoped list should use home visibility: %s", home)
	}
	inbox := TaskListVisibleCondition("t", "$1", &projectZero)
	if !strings.Contains(inbox, "pm.role IN ('owner', 'editor')") {
		t.Fatalf("no-project list should use home visibility: %s", inbox)
	}
	scoped := TaskListVisibleCondition("t", "$1", &project)
	if strings.Contains(scoped, "pm.role IN ('owner', 'editor')") {
		t.Fatalf("project-scoped list should include viewers: %s", scoped)
	}
}
