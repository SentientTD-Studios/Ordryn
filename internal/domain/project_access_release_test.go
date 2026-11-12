package domain

import (
	"context"
	"testing"

	"GoTodo/internal/storage"
	"GoTodo/internal/tasks"
)

// taskTies reports a task's claimer and reviewer (0 when unset) and whether
// userID watches it.
func taskTies(t *testing.T, taskID, userID int) (claimed, reviewer int, watching bool) {
	t.Helper()
	pool, err := storage.OpenDatabase()
	if err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(context.Background(), `
		SELECT COALESCE(claimed_by, 0), COALESCE(reviewer_id, 0),
		       EXISTS (SELECT 1 FROM task_watchers WHERE task_id = $1 AND user_id = $2)
		FROM tasks WHERE id = $1`, taskID, userID).Scan(&claimed, &reviewer, &watching); err != nil {
		t.Fatal(err)
	}
	return claimed, reviewer, watching
}

// tieUp makes userID the claimer, reviewer and a watcher of taskID.
func tieUp(t *testing.T, taskID, userID int) {
	t.Helper()
	execSQL(t, `UPDATE tasks SET claimed_by = $2, reviewer_id = $2 WHERE id = $1`, taskID, userID)
	if _, err := storage.AddTaskWatcher(taskID, userID); err != nil {
		t.Fatal(err)
	}
}

func homeTaskIDs(t *testing.T, userID int) map[int]bool {
	t.Helper()
	list, _, err := tasks.ReturnPaginationForUserWithFilters(1, 200, &userID, "UTC", tasks.ListFilters{IncludeSubtasks: true})
	if err != nil {
		t.Fatalf("home list: %v", err)
	}
	out := map[int]bool{}
	for _, task := range list {
		out[task.ID] = true
	}
	return out
}

func TestLosingProjectAccessReleasesAndHidesTasks(t *testing.T) {
	useFreshDatabase(t, "gotodo_access_release_test")
	ctx := context.Background()
	owner := insertUserWithRole(t, "owner@example.com", storage.AdminRoleName)
	member := insertUserWithRole(t, "member@example.com", storage.MemberRoleName)

	proj, err := CreateProject(ctx, owner, "Release Board", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := upsertTestMember(t, proj.ID, member, testRoleEditor); err != nil {
		t.Fatal(err)
	}
	mine := mustCreateTask(t, member, CreateTaskInput{Title: "Created by member", ProjectID: &proj.ID})
	assigned := mustCreateTask(t, owner, CreateTaskInput{Title: "Assigned to member", ProjectID: &proj.ID})
	personal := mustCreateTask(t, member, CreateTaskInput{Title: "Personal"})
	tieUp(t, assigned, member)
	if _, err := storage.AddProjectWatcher(proj.ID, member); err != nil {
		t.Fatal(err)
	}

	if home := homeTaskIDs(t, member); !home[mine] || !home[personal] {
		t.Fatalf("member's home before removal = %v", home)
	}

	if err := storage.RemoveProjectMember(proj.ID, member); err != nil {
		t.Fatal(err)
	}

	if claimed, reviewer, watching := taskTies(t, assigned, member); claimed != 0 || reviewer != 0 || watching {
		t.Errorf("after removal: claimed=%d reviewer=%d watching=%v, want all cleared", claimed, reviewer, watching)
	}
	if w, _ := storage.IsProjectWatcher(proj.ID, member); w {
		t.Error("removed member still watches the project")
	}
	home := homeTaskIDs(t, member)
	if home[mine] {
		t.Error("a task created in a project the member left still shows on their home list")
	}
	if !home[personal] {
		t.Error("the member's personal task disappeared")
	}
	q := tasks.ParseSearchQuery("member")
	projects, _ := storage.GetAccessibleProjects(member)
	found, _, err := tasks.SiteSearchTasks(member, "UTC", q, tasks.ResolveSearchScope(projects, q, 0), 1, 50)
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range found {
		if f.ID == mine || f.ID == assigned {
			t.Errorf("search still finds task %d in a project the member left", f.ID)
		}
	}

	// The owner keeps everything; the member coming back doesn't reclaim anything.
	if claimed, _, _ := taskTies(t, mine, owner); claimed != 0 {
		t.Errorf("unrelated task claim = %d", claimed)
	}
}

func TestReimportingInOneTransactionKeepsAssignments(t *testing.T) {
	useFreshDatabase(t, "gotodo_access_reimport_test")
	ctx := context.Background()
	owner := insertUserWithRole(t, "owner@example.com", storage.AdminRoleName)
	member := insertUserWithRole(t, "member@example.com", storage.MemberRoleName)

	org, err := storage.CreateOrganization(owner, "Org", "")
	if err != nil {
		t.Fatal(err)
	}
	ensureTestOrgRole(t, org.ID, testRoleEditor)
	if err := storage.UpsertOrganizationMember(org.ID, member, testRoleEditor); err != nil {
		t.Fatal(err)
	}
	proj, err := CreateProject(ctx, owner, "Reimport Board", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := storage.UpsertProjectMember(proj.ID, member, testRoleEditor); err != nil {
		t.Fatal(err)
	}
	task := mustCreateTask(t, owner, CreateTaskInput{Title: "Claimed", ProjectID: &proj.ID})
	tieUp(t, task, member)

	// Attaching deletes every member and re-imports the organization's people in
	// the same transaction: the member never really loses access.
	if _, err := storage.AttachProjectOrganization(proj.ID, owner, storage.OrgImportSpec{
		OrganizationID: org.ID, Lock: true, AllMembers: true, Mode: storage.OrgImportLock,
	}); err != nil {
		t.Fatal(err)
	}
	if claimed, reviewer, watching := taskTies(t, task, member); claimed != member || reviewer != member || !watching {
		t.Errorf("re-imported member lost ties: claimed=%d reviewer=%d watching=%v", claimed, reviewer, watching)
	}
}

func TestRemovingOrgMemberLeavesLockedAndChosenProjects(t *testing.T) {
	useFreshDatabase(t, "gotodo_access_org_removal_test")
	ctx := context.Background()
	owner := insertUserWithRole(t, "owner@example.com", storage.AdminRoleName)
	member := insertUserWithRole(t, "member@example.com", storage.MemberRoleName)

	org, err := storage.CreateOrganization(owner, "Org", "")
	if err != nil {
		t.Fatal(err)
	}
	ensureTestOrgRole(t, org.ID, testRoleEditor)
	if err := storage.UpsertOrganizationMember(org.ID, member, testRoleEditor); err != nil {
		t.Fatal(err)
	}
	attach := func(name, mode string) (int, int) {
		t.Helper()
		proj, err := CreateProject(ctx, owner, name, "")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := storage.AttachProjectOrganization(proj.ID, owner, storage.OrgImportSpec{
			OrganizationID: org.ID, Lock: mode == storage.OrgImportLock, AllMembers: true, Mode: mode,
		}); err != nil {
			t.Fatal(err)
		}
		task := mustCreateTask(t, owner, CreateTaskInput{Title: name + " task", ProjectID: &proj.ID})
		tieUp(t, task, member)
		return proj.ID, task
	}
	locked, lockedTask := attach("Locked", storage.OrgImportLock)
	copied, copiedTask := attach("Copied", storage.OrgImportCopy)
	kept, keptTask := attach("Kept", storage.OrgImportCopy)

	impact, err := OrganizationMemberRemovalImpactForUser(ctx, owner, org.ID, member)
	if err != nil {
		t.Fatal(err)
	}
	auto := map[int]bool{}
	for _, p := range impact {
		auto[p.ID] = p.Automatic
	}
	if len(impact) != 3 || !auto[locked] || auto[copied] || auto[kept] {
		t.Fatalf("removal impact = %+v", impact)
	}

	if err := RemoveOrganizationMemberForUser(ctx, owner, org.ID, member, []int{copied}); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name    string
		project int
		task    int
		stays   bool
	}{{"locked", locked, lockedTask, false}, {"chosen", copied, copiedTask, false}, {"not chosen", kept, keptTask, true}} {
		role, _ := storage.GetProjectRole(tc.project, member)
		if (role != "") != tc.stays {
			t.Errorf("%s project: member role = %q, want stays=%v", tc.name, role, tc.stays)
		}
		claimed, _, _ := taskTies(t, tc.task, member)
		if (claimed == member) != tc.stays {
			t.Errorf("%s project: claimed_by = %d, want kept=%v", tc.name, claimed, tc.stays)
		}
	}
	if role, _ := storage.GetOrganizationRole(org.ID, member); role != "" {
		t.Errorf("still in the organization as %q", role)
	}
}

func TestStaleAssignmentsAreReleasedOnMigrate(t *testing.T) {
	useFreshDatabase(t, "gotodo_access_stale_test")
	ctx := context.Background()
	owner := insertUserWithRole(t, "owner@example.com", storage.AdminRoleName)
	gone := insertUserWithRole(t, "gone@example.com", storage.MemberRoleName)

	proj, err := CreateProject(ctx, owner, "Stale Board", "")
	if err != nil {
		t.Fatal(err)
	}
	task := mustCreateTask(t, owner, CreateTaskInput{Title: "Left behind", ProjectID: &proj.ID})
	// A claim from before the trigger existed: never a member, no delete to fire on.
	tieUp(t, task, gone)
	if err := storage.ReleaseStaleProjectAssignments(); err != nil {
		t.Fatal(err)
	}
	if claimed, reviewer, watching := taskTies(t, task, gone); claimed != 0 || reviewer != 0 || watching {
		t.Errorf("stale ties kept: claimed=%d reviewer=%d watching=%v", claimed, reviewer, watching)
	}
}

func TestRemovingTeamMemberLeavesTeamSharesAndChosenPersonalShares(t *testing.T) {
	useFreshDatabase(t, "gotodo_access_team_removal_test")
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
	created, err := CreateAccountForUser(ctx, admin, CreateAccountInput{
		FirstName: "Ann", LastName: "Lee", Email: "ann@example.com", UserName: "ann", SiteRoleID: mustRoleID(t, storage.MemberRoleName),
	})
	if err != nil {
		t.Fatal(err)
	}
	ann := created.UserID

	pod, err := CreateOrganizationForUser(ctx, admin, "Pod", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := storage.AddOrganizationMember(pod.ID, ann, storage.CompanyMemberRole); err != nil {
		t.Fatal(err)
	}
	board := func(name string, share ProjectShareInput, onTeam bool) (int, int) {
		t.Helper()
		proj, err := CreateProject(ctx, admin, name, "")
		if err != nil {
			t.Fatal(err)
		}
		if onTeam {
			execSQL(t, `UPDATE projects SET organization_id = $1 WHERE id = $2`, pod.ID, proj.ID)
		}
		if err := ShareProjectForUser(ctx, admin, proj.ID, share); err != nil {
			t.Fatalf("share %s: %v", name, err)
		}
		task := mustCreateTask(t, admin, CreateTaskInput{Title: name + " task", ProjectID: &proj.ID})
		tieUp(t, task, ann)
		return proj.ID, task
	}
	viaTeam, viaTeamTask := board("Via team", ProjectShareInput{TeamID: pod.ID}, false)
	chosen, chosenTask := board("Chosen", ProjectShareInput{Username: "ann"}, true)
	kept, keptTask := board("Kept", ProjectShareInput{Username: "ann"}, true)

	impact, err := OrganizationMemberRemovalImpactForUser(ctx, admin, pod.ID, ann)
	if err != nil {
		t.Fatal(err)
	}
	modes := map[int]string{}
	for _, p := range impact {
		modes[p.ID] = p.Mode
		if p.Automatic != (p.Mode == "share") {
			t.Errorf("project %d: automatic=%v for mode %q", p.ID, p.Automatic, p.Mode)
		}
	}
	if modes[viaTeam] != "share" || modes[chosen] != "personal" || modes[kept] != "personal" {
		t.Fatalf("removal impact = %+v", impact)
	}

	if err := RemoveOrganizationMemberForUser(ctx, admin, pod.ID, ann, []int{chosen}); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name    string
		project int
		task    int
		stays   bool
	}{{"team share", viaTeam, viaTeamTask, false}, {"chosen personal share", chosen, chosenTask, false}, {"kept personal share", kept, keptTask, true}} {
		role, _ := storage.GetProjectRole(tc.project, ann)
		if (role != "") != tc.stays {
			t.Errorf("%s: role = %q, want stays=%v", tc.name, role, tc.stays)
		}
		claimed, _, _ := taskTies(t, tc.task, ann)
		if (claimed == ann) != tc.stays {
			t.Errorf("%s: claimed_by = %d, want kept=%v", tc.name, claimed, tc.stays)
		}
	}
}
