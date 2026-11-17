package domain

import (
	"context"
	"errors"
	"testing"

	"GoTodo/internal/storage"
)

// refinementBoard makes a kanban project owned by user 1 with refinement switched on site-wide and
// for the project, plus an editor member (no facilitate permission) and the editor's user id.
func refinementBoard(t *testing.T, name string) (projectID, editorID int) {
	t.Helper()
	ctx := context.Background()
	proj, err := CreateProject(ctx, 1, name, "")
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	if _, err := SetProjectWorkflowMode(ctx, 1, proj.ID, storage.WorkflowKanban); err != nil {
		t.Fatalf("kanban: %v", err)
	}
	if err := SetSiteCeremonies(map[string]bool{storage.CeremonyRefinement: true}); err != nil {
		t.Fatalf("site on: %v", err)
	}
	t.Cleanup(func() { _ = SetSiteCeremonies(map[string]bool{storage.CeremonyRefinement: false}) })
	on := true
	if _, err := UpdateCeremonySettings(ctx, 1, proj.ID, UpdateCeremonySettingsInput{
		Ceremonies: map[string]bool{storage.CeremonyRefinement: on},
	}); err != nil {
		t.Fatalf("project opt-in: %v", err)
	}
	ensureTestProjectRole(t, proj.ID, testRoleEditor)
	editorID = insertUserWithRole(t, name+"-editor@example.com", storage.MemberRoleName)
	pool, err := storage.OpenDatabase()
	if err != nil {
		t.Fatal(err)
	}
	defer storage.CloseDatabase(pool)
	if _, err := pool.Exec(ctx, `INSERT INTO project_members (project_id, user_id, role) VALUES ($1, $2, $3)`,
		proj.ID, editorID, testRoleEditor); err != nil {
		t.Fatalf("add editor: %v", err)
	}
	return proj.ID, editorID
}

func TestCeremonySettingsGating(t *testing.T) {
	ctx := context.Background()
	proj, err := CreateProject(ctx, 1, "Refinement gating", "")
	if err != nil {
		t.Fatal(err)
	}
	pid := proj.ID
	on := map[string]bool{storage.CeremonyRefinement: true}

	if _, err := UpdateCeremonySettings(ctx, 1, pid, UpdateCeremonySettingsInput{Ceremonies: on}); !errors.Is(err, ErrValidation) {
		t.Fatalf("classic board opt-in err=%v want validation", err)
	}
	if _, err := SetProjectWorkflowMode(ctx, 1, pid, storage.WorkflowKanban); err != nil {
		t.Fatal(err)
	}
	if _, err := UpdateCeremonySettings(ctx, 1, pid, UpdateCeremonySettingsInput{Ceremonies: on}); !errors.Is(err, ErrValidation) {
		t.Fatalf("site-disabled opt-in err=%v want validation", err)
	}
	needs := storage.RefinementNeeded
	if _, err := CreateTask(ctx, 1, CreateTaskInput{Title: "Too early", ProjectID: &pid, Refinement: &needs}); !errors.Is(err, ErrValidation) {
		t.Fatalf("flag before opt-in err=%v want validation", err)
	}

	if err := SetSiteCeremonies(on); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = SetSiteCeremonies(map[string]bool{storage.CeremonyRefinement: false}) })
	yes := true
	got, err := UpdateCeremonySettings(ctx, 1, pid, UpdateCeremonySettingsInput{Ceremonies: on, NewTasksNeedRefinement: &yes})
	if err != nil {
		t.Fatalf("opt-in: %v", err)
	}
	if !got.Active(storage.CeremonyRefinement) || !got.NewTasksNeedRefinement || !got.CanFacilitate[storage.CeremonyRefinement] {
		t.Fatalf("settings after opt-in: %+v", got)
	}

	// New tasks pick up the project default; an explicit value wins.
	defaulted := mustCreateTask(t, 1, CreateTaskInput{Title: "Defaulted", ProjectID: &pid})
	empty := ""
	explicit := mustCreateTask(t, 1, CreateTaskInput{Title: "Explicit", ProjectID: &pid, Refinement: &empty})
	if s, _ := storage.GetTaskRefinementState(defaulted); s != storage.RefinementNeeded {
		t.Fatalf("defaulted refinement=%q", s)
	}
	if s, _ := storage.GetTaskRefinementState(explicit); s != "" {
		t.Fatalf("explicit refinement=%q", s)
	}

	refined := storage.RefinementRefined
	if _, err := UpdateTask(ctx, 1, defaulted, UpdateTaskInput{Refinement: &refined}); err != nil {
		t.Fatalf("set refined: %v", err)
	}
	if ev := eventsOfType(t, defaulted, 1, "refinement_changed"); len(ev) != 1 || ev[0].Metadata["to"] != refined {
		t.Fatalf("refinement_changed events=%+v", ev)
	}
	bogus := "maybe"
	if _, err := UpdateTask(ctx, 1, defaulted, UpdateTaskInput{Refinement: &bogus}); !errors.Is(err, ErrValidation) {
		t.Fatalf("bogus refinement err=%v", err)
	}
}

func TestHoursPerPointRederivesLegacyEstimates(t *testing.T) {
	ctx := context.Background()
	pid, _ := refinementBoard(t, "Legacy points")
	legacy := mustCreateTask(t, 1, CreateTaskInput{Title: "Old estimate", ProjectID: &pid})
	edited := mustCreateTask(t, 1, CreateTaskInput{Title: "Re-estimated", ProjectID: &pid})

	pool, err := storage.OpenDatabase()
	if err != nil {
		t.Fatal(err)
	}
	defer storage.CloseDatabase(pool)
	// Simulate pre-upgrade rows: points with hours derived at the default rate.
	if _, err := pool.Exec(ctx, `UPDATE tasks SET estimate_points = 3, estimate_hours = 3 WHERE id = ANY($1)`,
		[]int{legacy, edited}); err != nil {
		t.Fatal(err)
	}
	h := 2.5
	hp := &h
	if _, err := UpdateTask(ctx, 1, edited, UpdateTaskInput{EstimateHours: &hp}); err != nil {
		t.Fatalf("re-estimate: %v", err)
	}

	rate := 4.0
	if _, err := UpdateCeremonySettings(ctx, 1, pid, UpdateCeremonySettingsInput{HoursPerPoint: &rate}); err != nil {
		t.Fatalf("set rate: %v", err)
	}
	fields, err := storage.GetWorkflowFieldsForTasks([]int{legacy, edited})
	if err != nil {
		t.Fatal(err)
	}
	if got := fields[legacy].EstimateHours; got == nil || *got != 12 {
		t.Fatalf("legacy hours=%v want 12 (3 pts x 4h)", got)
	}
	if got := fields[edited].EstimateHours; got == nil || *got != 2.5 {
		t.Fatalf("edited hours=%v want 2.5 (no longer derived from points)", got)
	}

	bad := 0.1
	if _, err := UpdateCeremonySettings(ctx, 1, pid, UpdateCeremonySettingsInput{HoursPerPoint: &bad}); !errors.Is(err, ErrValidation) {
		t.Fatalf("bad rate err=%v", err)
	}
}

func TestRefinementSessionFlow(t *testing.T) {
	ctx := context.Background()
	pid, editor := refinementBoard(t, "Refinement flow")
	needs := storage.RefinementNeeded
	a := mustCreateTask(t, 1, CreateTaskInput{Title: "A", ProjectID: &pid, Refinement: &needs})
	b := mustCreateTask(t, 1, CreateTaskInput{Title: "B", ProjectID: &pid, Refinement: &needs})
	unflagged := mustCreateTask(t, 1, CreateTaskInput{Title: "Unflagged", ProjectID: &pid})

	if _, err := StartRefinementSession(ctx, editor, pid, CreateRefinementSessionInput{}); !errors.Is(err, ErrForbidden) {
		t.Fatalf("editor start err=%v want forbidden", err)
	}
	s, err := StartRefinementSession(ctx, 1, pid, CreateRefinementSessionInput{Title: "Sprint 12 grooming"})
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	if len(s.State.Queue) != 2 || s.State.Queue[0] != a || s.State.Queue[1] != b || !s.IsFacilitator {
		t.Fatalf("queue=%v facilitator=%v", s.State.Queue, s.IsFacilitator)
	}
	if _, err := StartRefinementSession(ctx, 1, pid, CreateRefinementSessionInput{}); !errors.Is(err, ErrConflict) {
		t.Fatalf("second session err=%v want conflict", err)
	}

	// Members without the permission can watch but not drive.
	view, err := GetActiveRefinementSession(ctx, editor, pid)
	if err != nil || view == nil || view.CanFacilitate || view.ID != s.ID {
		t.Fatalf("editor view=%+v err=%v", view, err)
	}
	if _, err := ApplyRefinementAction(ctx, editor, pid, s.ID, RefinementActionInput{Action: RefinementActionNext, Revision: s.Revision}); !errors.Is(err, ErrForbidden) {
		t.Fatalf("editor action err=%v", err)
	}

	s, err = ApplyRefinementAction(ctx, 1, pid, s.ID, RefinementActionInput{Action: RefinementActionRefine, Revision: s.Revision})
	if err != nil {
		t.Fatalf("refine: %v", err)
	}
	if st, _ := storage.GetTaskRefinementState(a); st != storage.RefinementRefined {
		t.Fatalf("task a refinement=%q", st)
	}
	if s.State.Current != 1 || s.State.Outcomes[a] != RefinementOutcomeRefined {
		t.Fatalf("after refine state=%+v", s.State)
	}
	if _, err := ApplyRefinementAction(ctx, 1, pid, s.ID, RefinementActionInput{Action: RefinementActionNext, Revision: s.Revision - 1}); !errors.Is(err, ErrConflict) {
		t.Fatalf("stale revision err=%v", err)
	}

	s, err = ApplyRefinementAction(ctx, 1, pid, s.ID, RefinementActionInput{Action: RefinementActionAdd, Revision: s.Revision, TaskIDs: []int{unflagged, a, 999999}})
	if err != nil {
		t.Fatalf("add: %v", err)
	}
	if len(s.State.Queue) != 3 || s.State.Queue[2] != unflagged || len(s.Tasks) != 3 {
		t.Fatalf("after add queue=%v tasks=%d", s.State.Queue, len(s.Tasks))
	}
	s, err = ApplyRefinementAction(ctx, 1, pid, s.ID, RefinementActionInput{Action: RefinementActionSkip, Revision: s.Revision})
	if err != nil {
		t.Fatalf("skip: %v", err)
	}
	if st, _ := storage.GetTaskRefinementState(b); st != storage.RefinementNeeded {
		t.Fatalf("skipped task b should keep its flag, got %q", st)
	}

	// Anyone with the permission can take over; then the old facilitator is locked out.
	def := storage.ResolveRoleDef(pid, testRoleEditor)
	if def == nil {
		t.Fatal("editor role missing")
	}
	perms := append(append([]string{}, def.Permissions...), storage.PermRefinementRun)
	if _, err := storage.UpdateProjectRoleDef(def.ID, nil, nil, &perms, nil); err != nil {
		t.Fatalf("grant: %v", err)
	}
	s, err = ApplyRefinementAction(ctx, editor, pid, s.ID, RefinementActionInput{Action: RefinementActionTakeOver, Revision: s.Revision})
	if err != nil || s.FacilitatorID != editor {
		t.Fatalf("take over: facilitator=%d err=%v", s.FacilitatorID, err)
	}
	if _, err := ApplyRefinementAction(ctx, 1, pid, s.ID, RefinementActionInput{Action: RefinementActionNext, Revision: s.Revision}); !errors.Is(err, ErrForbidden) {
		t.Fatalf("former facilitator err=%v", err)
	}

	s, err = ApplyRefinementAction(ctx, editor, pid, s.ID, RefinementActionInput{Action: RefinementActionEnd, Revision: s.Revision})
	if err != nil || s.Status != storage.CeremonySessionEnded {
		t.Fatalf("end: status=%v err=%v", s.Status, err)
	}
	if active, err := GetActiveRefinementSession(ctx, 1, pid); err != nil || active != nil {
		t.Fatalf("active after end=%v err=%v", active, err)
	}
	hist, err := ListRefinementSessions(ctx, 1, pid, 10)
	if err != nil || len(hist) != 1 || hist[0].Refined != 1 || hist[0].Skipped != 1 || hist[0].Queued != 3 {
		t.Fatalf("history=%+v err=%v", hist, err)
	}
}
