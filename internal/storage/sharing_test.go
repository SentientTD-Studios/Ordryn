package storage

import (
	"strings"
	"testing"
)

func TestRoleCanWrite(t *testing.T) {
	if !RoleCanWrite(RoleOwner) || !RoleCanWrite(RoleAutomation) {
		t.Fatal("owner/automation should write")
	}
	if RoleCanWrite("") {
		t.Fatal("no role should not write")
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
	if !strings.Contains(cond, "project_members") {
		t.Fatalf("visible condition should include project membership: %s", cond)
	}
	if strings.Contains(cond, "organization_members") {
		t.Fatalf("visible condition should not live-join organization membership: %s", cond)
	}

	unaliased := TaskVisibleCondition("", "$1")
	if !strings.Contains(unaliased, "pm.project_id = tasks.project_id") {
		t.Fatalf("unaliased condition should correlate to tasks.project_id: %s", unaliased)
	}
}

func TestTaskHomeVisibleCondition(t *testing.T) {
	cond := TaskHomeVisibleCondition("t", "$1")
	if !strings.Contains(cond, "pm.role <> 'viewer'") {
		t.Fatalf("home visible condition should exclude viewers: %s", cond)
	}
}

func TestTaskListVisibleCondition(t *testing.T) {
	project := 7
	projectZero := 0

	home := TaskListVisibleCondition("t", "$1", nil)
	if !strings.Contains(home, "pm.role <> 'viewer'") {
		t.Fatalf("unscoped list should use home visibility: %s", home)
	}
	inbox := TaskListVisibleCondition("t", "$1", &projectZero)
	if !strings.Contains(inbox, "pm.role <> 'viewer'") {
		t.Fatalf("no-project list should use home visibility: %s", inbox)
	}
	scoped := TaskListVisibleCondition("t", "$1", &project)
	if strings.Contains(scoped, "pm.role IN ('owner', 'editor')") {
		t.Fatalf("project-scoped list should include viewers: %s", scoped)
	}
}
