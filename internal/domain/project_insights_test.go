package domain

import (
	"context"
	"errors"
	"testing"
	"time"

	"GoTodo/internal/storage"
)

func at(s string) time.Time {
	t, err := time.Parse("2006-01-02 15:04", s)
	if err != nil {
		panic(err)
	}
	return t
}

func TestBuildBurndownReplaysCompletionHistory(t *testing.T) {
	modified := at("2026-10-03 08:00")
	tasks := []storage.SprintTaskState{
		// Done on day 2.
		{ID: 1, Points: 3, Completed: true, Transitions: []storage.CompletionTransition{{Completed: true, At: at("2026-10-02 10:00")}}},
		// Done on day 1, reopened on day 2.
		{ID: 2, Points: 2, Transitions: []storage.CompletionTransition{
			{Completed: true, At: at("2026-10-01 09:00")},
			{Completed: false, At: at("2026-10-02 09:00")},
		}},
		// Never touched.
		{ID: 3, Points: 5},
		// Completed before events were recorded; dated by its last change.
		{ID: 4, Points: 1, Completed: true, ModifiedAt: &modified},
	}
	sprint := SprintRef{ID: 9, Name: "S", StartDate: "2026-10-01", EndDate: "2026-10-05"}
	bd := BuildBurndown(sprint, tasks, time.UTC, at("2026-10-03 12:00"))
	if bd == nil || bd.TotalTasks != 4 || bd.TotalPoints != 11 || len(bd.Days) != 5 {
		t.Fatalf("burndown = %+v", bd)
	}
	want := []struct{ tasks, points int }{{3, 9}, {3, 8}, {2, 7}}
	for i, w := range want {
		d := bd.Days[i]
		if d.RemainingTasks == nil || *d.RemainingTasks != w.tasks || *d.RemainingPoints != w.points {
			t.Fatalf("day %s remaining = %v/%v, want %d/%d", d.Date, d.RemainingTasks, d.RemainingPoints, w.tasks, w.points)
		}
	}
	for _, d := range bd.Days[3:] {
		if d.RemainingTasks != nil {
			t.Fatalf("future day %s has actuals", d.Date)
		}
	}
	if bd.Days[0].IdealTasks != 4 || bd.Days[4].IdealTasks != 0 || bd.Days[2].IdealPoints != 5.5 {
		t.Fatalf("ideal line = %+v", bd.Days)
	}
}

func TestDefaultInsightsSprint(t *testing.T) {
	d := func(s string) *time.Time { v, _ := time.Parse("2006-01-02", s); return &v }
	past := storage.ProjectSprint{ID: 1, StartDate: d("2026-09-01"), EndDate: d("2026-09-14")}
	later := storage.ProjectSprint{ID: 2, StartDate: d("2026-09-15"), EndDate: d("2026-09-28")}
	next := storage.ProjectSprint{ID: 3, StartDate: d("2026-11-01"), EndDate: d("2026-11-14")}
	dateless := storage.ProjectSprint{ID: 4}
	if s := defaultInsightsSprint([]storage.ProjectSprint{past, later, next, dateless}, "2026-09-20"); s == nil || s.ID != 2 {
		t.Fatalf("running sprint not chosen: %+v", s)
	}
	if s := defaultInsightsSprint([]storage.ProjectSprint{next, past, later}, "2026-10-10"); s == nil || s.ID != 2 {
		t.Fatalf("latest ended sprint not chosen: %+v", s)
	}
	if s := defaultInsightsSprint([]storage.ProjectSprint{next, dateless}, "2026-10-10"); s == nil || s.ID != 3 {
		t.Fatalf("upcoming sprint not chosen: %+v", s)
	}
	if s := defaultInsightsSprint([]storage.ProjectSprint{dateless}, "2026-10-10"); s != nil {
		t.Fatalf("dateless sprint chosen: %+v", s)
	}
}

func TestProjectInsights(t *testing.T) {
	ctx := context.Background()
	proj, err := CreateProject(ctx, 1, "Insights Board", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := SetProjectWorkflowMode(ctx, 1, proj.ID, storage.WorkflowKanban); err != nil {
		t.Fatal(err)
	}
	if err := upsertTestMember(t, proj.ID, 2, testRoleEditor); err != nil {
		t.Fatal(err)
	}
	today := time.Now().UTC()
	sprint, err := CreateProjectSprintForUser(ctx, 1, proj.ID, CreateProjectSprintInput{
		Name:      "Now",
		StartDate: today.AddDate(0, 0, -2).Format("2006-01-02"),
		EndDate:   today.AddDate(0, 0, 5).Format("2006-01-02"),
	})
	if err != nil {
		t.Fatal(err)
	}
	pid, sid := proj.ID, sprint.ID
	three, five := 3, 5
	done := mustCreateTask(t, 1, CreateTaskInput{Title: "Done", ProjectID: &pid, SprintID: &sid, EstimatePoints: &three})
	claimed := mustCreateTask(t, 1, CreateTaskInput{Title: "Claimed", ProjectID: &pid, SprintID: &sid, EstimatePoints: &five})
	mustCreateTask(t, 1, CreateTaskInput{Title: "Overdue", ProjectID: &pid, DueDate: "2001-01-01"})
	if err := SetTaskCompleted(ctx, 1, done, true); err != nil {
		t.Fatal(err)
	}
	if err := ClaimTaskForUser(ctx, 2, claimed); err != nil {
		t.Fatal(err)
	}

	// The editor role lacks insights:people: it sees everything except the per-person list.
	ins, err := GetProjectInsightsForUser(ctx, 2, proj.ID, 0, "UTC")
	if err != nil {
		t.Fatalf("member insights: %v", err)
	}
	if ins.CanViewMembers || len(ins.Members) != 0 {
		t.Fatalf("editor without insights:people got members: %+v", ins.Members)
	}
	if ins.Summary.Open != 2 || ins.Burndown == nil {
		t.Fatalf("editor should still see summary and burndown: %+v", ins.Summary)
	}

	ins, err = GetProjectInsightsForUser(ctx, 1, proj.ID, 0, "UTC")
	if err != nil {
		t.Fatalf("owner insights: %v", err)
	}
	if !ins.CanViewMembers {
		t.Fatal("owner should see members")
	}
	if s := ins.Summary; s.Open != 2 || s.Done != 1 || s.Overdue != 1 || s.Unclaimed != 1 || s.OpenPoints != 5 {
		t.Fatalf("summary = %+v", s)
	}
	if n := len(ins.Throughput); n != insightsThroughputWeeks {
		t.Fatalf("throughput weeks = %d", n)
	}
	if last := ins.Throughput[len(ins.Throughput)-1]; last.Tasks != 1 || last.Points != 3 {
		t.Fatalf("this week = %+v", last)
	}
	byID := map[int]MemberInsight{}
	for _, m := range ins.Members {
		byID[m.UserID] = m
	}
	if m := byID[1]; m.Completed != 1 || m.ClaimedOpen != 0 {
		t.Fatalf("owner = %+v", m)
	}
	if m := byID[2]; m.ClaimedOpen != 1 || m.ClaimedPoints != 5 || !m.IsMember {
		t.Fatalf("editor = %+v", m)
	}
	bd := ins.Burndown
	if bd == nil || bd.Sprint.ID != sprint.ID || bd.TotalTasks != 2 || bd.TotalPoints != 8 {
		t.Fatalf("burndown = %+v", bd)
	}
	todayRow := bd.Days[2]
	if todayRow.RemainingTasks == nil || *todayRow.RemainingTasks != 1 || *todayRow.RemainingPoints != 5 {
		t.Fatalf("today remaining = %+v", todayRow)
	}
	if bd.Days[3].RemainingTasks != nil {
		t.Fatal("tomorrow should have no actuals")
	}
	if len(ins.Sprints) != 1 {
		t.Fatalf("sprints = %+v", ins.Sprints)
	}

	// Trashed work drops out of every number.
	if err := DeleteTask(ctx, 1, claimed); err != nil {
		t.Fatal(err)
	}
	ins, err = GetProjectInsightsForUser(ctx, 1, proj.ID, sprint.ID, "UTC")
	if err != nil {
		t.Fatal(err)
	}
	if ins.Summary.Open != 1 || ins.Burndown.TotalTasks != 1 {
		t.Fatalf("trashed task still counted: %+v / %+v", ins.Summary, ins.Burndown)
	}

	if _, err := GetProjectInsightsForUser(ctx, 3, proj.ID, 0, "UTC"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("non-member err = %v", err)
	}
	if _, err := GetProjectInsightsForUser(ctx, 1, proj.ID, 999999, "UTC"); !errors.Is(err, ErrValidation) {
		t.Fatalf("unknown sprint err = %v", err)
	}
}

func TestMigrateGrantInsightsPeopleToManagers(t *testing.T) {
	ctx := context.Background()
	proj, err := CreateProject(ctx, 1, "Insights Roles", "")
	if err != nil {
		t.Fatal(err)
	}
	pid := proj.ID
	lead, err := storage.CreateProjectRoleDef(&pid, "lead", "Lead", "", []string{storage.PermProjectManage}, false, 10)
	if err != nil {
		t.Fatal(err)
	}
	dev, err := storage.CreateProjectRoleDef(&pid, "dev", "Dev", "", []string{storage.PermTasksEdit}, false, 11)
	if err != nil {
		t.Fatal(err)
	}
	// Forget any earlier run (which also creates the marker table) so the backfill applies again.
	rerun := func() {
		t.Helper()
		if err := storage.MigrateGrantInsightsPeopleToManagers(); err != nil {
			t.Fatal(err)
		}
		pool, err := storage.OpenDatabase()
		if err != nil {
			t.Fatal(err)
		}
		_, err = pool.Exec(ctx, `DELETE FROM data_migrations WHERE name = 'grant_insights_people_to_managers'`)
		storage.CloseDatabase(pool)
		if err != nil {
			t.Fatal(err)
		}
		if err := storage.MigrateGrantInsightsPeopleToManagers(); err != nil {
			t.Fatal(err)
		}
	}
	rerun()
	if !storage.HasProjectPerm(pid, lead.Slug, storage.PermInsightsPeople) {
		t.Fatal("manager role was not granted insights:people")
	}
	if storage.HasProjectPerm(pid, dev.Slug, storage.PermInsightsPeople) {
		t.Fatal("non-manager role was granted insights:people")
	}

	// Once applied, removing the permission sticks across later startups.
	perms := []string{storage.PermProjectManage}
	if _, err := storage.UpdateProjectRoleDef(lead.ID, nil, nil, &perms, nil); err != nil {
		t.Fatal(err)
	}
	if err := storage.MigrateGrantInsightsPeopleToManagers(); err != nil {
		t.Fatal(err)
	}
	if storage.HasProjectPerm(pid, lead.Slug, storage.PermInsightsPeople) {
		t.Fatal("migration re-granted insights:people after it was removed")
	}
}

func TestProjectStatusCountsIgnoreFiltersAndTrash(t *testing.T) {
	ctx := context.Background()
	proj, err := CreateProject(ctx, 1, "WIP Board", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := SetProjectWorkflowMode(ctx, 1, proj.ID, storage.WorkflowKanban); err != nil {
		t.Fatal(err)
	}
	pid := proj.ID
	a := mustCreateTask(t, 1, CreateTaskInput{Title: "A", ProjectID: &pid})
	mustCreateTask(t, 1, CreateTaskInput{Title: "B", ProjectID: &pid, Priority: 3})
	mustCreateTask(t, 1, CreateTaskInput{Title: "A child", ParentID: &a})
	gone := mustCreateTask(t, 1, CreateTaskInput{Title: "Trashed", ProjectID: &pid})
	if err := DeleteTask(ctx, 1, gone); err != nil {
		t.Fatal(err)
	}

	counts, err := ProjectStatusCountsForUser(ctx, 1, pid, 0)
	if err != nil {
		t.Fatal(err)
	}
	total := 0
	for _, n := range counts {
		total += n
	}
	if total != 3 {
		t.Fatalf("backlog counts = %v, want 3 live tasks (subtask included, trash excluded)", counts)
	}
	if _, err := ProjectStatusCountsForUser(ctx, 3, pid, 0); !errors.Is(err, ErrNotFound) {
		t.Fatalf("non-member: %v", err)
	}
}
