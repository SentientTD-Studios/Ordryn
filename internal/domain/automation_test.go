package domain

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"GoTodo/internal/hooks"
	"GoTodo/internal/live"
	"GoTodo/internal/storage"
)

// automationProject is a kanban project owned by user 1 (user 2 is an editor)
// with a "Ready" column added to the default To Do / In Progress / Done.
func automationProject(t *testing.T) agentFixture {
	t.Helper()
	ctx := context.Background()
	name := "Auto " + t.Name()
	if len(name) > 50 {
		name = name[:50]
	}
	pid := sharedProject(t, name)
	if _, err := SetProjectWorkflowMode(ctx, 1, pid, storage.WorkflowKanban); err != nil {
		t.Fatalf("kanban: %v", err)
	}
	if _, err := CreateProjectStatusForUser(ctx, 1, pid, CreateProjectStatusInput{Name: "Ready"}); err != nil {
		t.Fatalf("status: %v", err)
	}
	list, err := storage.ListProjectStatuses(pid)
	if err != nil {
		t.Fatal(err)
	}
	f := agentFixture{projectID: pid, statuses: map[string]int{}}
	for _, st := range list {
		f.statuses[st.Name] = st.ID
	}
	return f
}

// captureAutomation records outbound events and replays them into the rule
// engine synchronously (production runs it on a goroutine).
func captureAutomation(t *testing.T) func() []hooks.Event {
	t.Helper()
	var got []hooks.Event
	stop := live.ListenHooks(func(ev hooks.Event) { got = append(got, ev) })
	prevAsync := automationAsync
	automationAsync = func(f func()) { f() }
	t.Cleanup(func() {
		stop()
		automationAsync = prevAsync
	})
	return func() []hooks.Event {
		var all []hooks.Event
		// Replay until quiet: rule actions emit events of their own.
		for i := 0; i < 5 && len(got) > 0; i++ {
			evs := got
			got = nil
			for _, ev := range evs {
				HandleAutomationEvent(ev)
			}
			all = append(all, evs...)
		}
		return all
	}
}

func newRule(t *testing.T, projectID int, name, trigger string, cfg AutomationTriggerConfig, cond AutomationConditions, actions ...AutomationAction) *storage.AutomationRule {
	t.Helper()
	r, err := CreateAutomationRule(1, projectID, AutomationRuleInput{
		Name: &name, TriggerType: &trigger, TriggerConfig: &cfg, Conditions: &cond, Actions: &actions,
	})
	if err != nil {
		t.Fatalf("create rule %q: %v", name, err)
	}
	return r
}

func projectTag(t *testing.T, projectID int, name string) int {
	t.Helper()
	tag, err := CreateTag(context.Background(), 1, name, &projectID)
	if err != nil {
		t.Fatalf("create tag: %v", err)
	}
	return tag.ID
}

func taskSnap(t *testing.T, taskID int) *storage.HookTaskSnapshot {
	t.Helper()
	s, err := storage.GetHookTaskSnapshot(taskID)
	if err != nil || s == nil {
		t.Fatalf("snapshot %d: %v", taskID, err)
	}
	return s
}

func ruleRuns(t *testing.T, projectID, ruleID int) []storage.AutomationRuleRun {
	t.Helper()
	runs, err := storage.ListAutomationRuns(projectID, ruleID, 0, 100)
	if err != nil {
		t.Fatalf("runs: %v", err)
	}
	return runs
}

func TestAutomationSystemUserIsProtected(t *testing.T) {
	id, err := storage.EnsureSystemUser()
	if err != nil || id <= 0 {
		t.Fatalf("ensure: %d %v", id, err)
	}
	again, err := storage.EnsureSystemUser()
	if err != nil || again != id {
		t.Fatalf("ensure is not idempotent: %d vs %d (%v)", again, id, err)
	}
	if !storage.IsSystemUser(id) || storage.IsSystemUser(1) {
		t.Fatal("IsSystemUser mismatch")
	}
	if !storage.IsAgentUser(id) {
		t.Fatal("automation account should be flagged is_agent (no login, no inbox)")
	}
	users, err := storage.ListUsers()
	if err != nil {
		t.Fatal(err)
	}
	for _, u := range users {
		if u.ID == id {
			t.Fatal("automation account must not appear in the admin user list")
		}
	}

	f := automationProject(t)
	role, err := storage.GetProjectRole(f.projectID, id)
	if err != nil || role != storage.RoleAutomation {
		t.Fatalf("role = %q, %v", role, err)
	}
	if !storage.HasProjectPerm(f.projectID, role, storage.PermTasksStatus) {
		t.Fatal("automation should be able to move tasks")
	}
	for _, perm := range []string{storage.PermTasksDelete, storage.PermProjectManage, storage.PermTasksCreate} {
		if storage.HasProjectPerm(f.projectID, role, perm) {
			t.Fatalf("automation should not hold %s", perm)
		}
	}
	members, _ := storage.ListProjectMembers(f.projectID)
	for _, m := range members {
		if m.UserID == id {
			t.Fatal("automation account must not be a stored project member")
		}
	}
}

func TestAutomationRuleManagementPermissions(t *testing.T) {
	f := automationProject(t)
	name, trig := "x", AutomationOnClaimed
	acts := []AutomationAction{{Type: AutomationActSetStatus, StatusID: f.statuses["In Progress"]}}
	_, err := CreateAutomationRule(2, f.projectID, AutomationRuleInput{Name: &name, TriggerType: &trig, Actions: &acts})
	if !errors.Is(err, ErrForbidden) {
		t.Fatalf("editor create: want forbidden, got %v", err)
	}

	classic := sharedProject(t, "Auto classic rules")
	_, err = CreateAutomationRule(1, classic, AutomationRuleInput{Name: &name, TriggerType: &trig, Actions: &acts})
	if !errors.Is(err, ErrValidation) {
		t.Fatalf("kanban-only trigger on classic project: want validation, got %v", err)
	}

	many := make([]AutomationAction, MaxAutomationActions+1)
	for i := range many {
		many[i] = AutomationAction{Type: AutomationActComplete}
	}
	_, err = CreateAutomationRule(1, f.projectID, AutomationRuleInput{Name: &name, TriggerType: &trig, Actions: &many})
	if !errors.Is(err, ErrValidation) {
		t.Fatalf("too many actions: want validation, got %v", err)
	}

	bad := []AutomationAction{{Type: AutomationActAddTag, TagID: 999999}}
	_, err = CreateAutomationRule(1, f.projectID, AutomationRuleInput{Name: &name, TriggerType: &trig, Actions: &bad})
	if !errors.Is(err, ErrValidation) {
		t.Fatalf("unknown tag: want validation, got %v", err)
	}
}

func TestAutomationClaimMovesToInProgress(t *testing.T) {
	ctx := context.Background()
	f := automationProject(t)
	replay := captureAutomation(t)
	rule := newRule(t, f.projectID, "Claim starts work", AutomationOnClaimed, AutomationTriggerConfig{},
		AutomationConditions{StatusIDs: []int{f.statuses["To Do"]}},
		AutomationAction{Type: AutomationActSetStatus, StatusID: f.statuses["In Progress"]})

	task := newProjectTask(t, f.projectID, "claim me")
	replay()
	if err := ClaimTaskForUser(ctx, 2, task); err != nil {
		t.Fatal(err)
	}
	replay()

	if got := taskSnap(t, task).StatusID; got != f.statuses["In Progress"] {
		t.Fatalf("status = %d, want In Progress", got)
	}
	runs := ruleRuns(t, f.projectID, rule.ID)
	if len(runs) != 1 || runs[0].Outcome != storage.AutomationRunApplied || runs[0].TaskID != task {
		t.Fatalf("runs = %+v", runs)
	}
	var changes []AutomationChange
	_ = json.Unmarshal(runs[0].Changes, &changes)
	if len(changes) != 1 || changes[0].To != "In Progress" || changes[0].From != "To Do" {
		t.Fatalf("changes = %+v", changes)
	}

	events, err := storage.GetEventsForTask(task, 1, 50)
	if err != nil {
		t.Fatal(err)
	}
	sawRule, sawStatus := false, false
	for _, ev := range events {
		if ev.EventType == "automation_rule" && ev.ActorUserName == storage.SystemUserDisplayName {
			sawRule = true
		}
		if ev.EventType == "status_changed" && storage.IsSystemUser(ev.UserID) && ev.ActorUserName == storage.SystemUserDisplayName {
			sawStatus = true
		}
	}
	if !sawRule || !sawStatus {
		t.Fatalf("history should attribute the change to Automation: %+v", events)
	}
}

func TestAutomationRulesDoNotChain(t *testing.T) {
	ctx := context.Background()
	f := automationProject(t)
	replay := captureAutomation(t)
	tagID := projectTag(t, f.projectID, "started")
	newRule(t, f.projectID, "Claim starts work", AutomationOnClaimed, AutomationTriggerConfig{},
		AutomationConditions{}, AutomationAction{Type: AutomationActSetStatus, StatusID: f.statuses["In Progress"]})
	follow := newRule(t, f.projectID, "Tag started", AutomationOnStatusChanged,
		AutomationTriggerConfig{ToStatusIDs: []int{f.statuses["In Progress"]}},
		AutomationConditions{}, AutomationAction{Type: AutomationActAddTag, TagID: tagID})

	task := newProjectTask(t, f.projectID, "no chain")
	replay()
	if err := ClaimTaskForUser(ctx, 2, task); err != nil {
		t.Fatal(err)
	}
	replay()

	snap := taskSnap(t, task)
	if snap.StatusID != f.statuses["In Progress"] {
		t.Fatal("first rule should have run")
	}
	if containsInt(snap.TagIDs, tagID) {
		t.Fatal("a rule's own change must not trigger another rule")
	}
	if n := len(ruleRuns(t, f.projectID, follow.ID)); n != 0 {
		t.Fatalf("follow-up rule ran %d times", n)
	}

	// A person making the same move still triggers the rule.
	todo := f.statuses["To Do"]
	todoP := &todo
	if _, err := UpdateTask(ctx, 2, task, UpdateTaskInput{StatusID: &todoP}); err != nil {
		t.Fatal(err)
	}
	inProg := f.statuses["In Progress"]
	inProgP := &inProg
	if _, err := UpdateTask(ctx, 2, task, UpdateTaskInput{StatusID: &inProgP}); err != nil {
		t.Fatal(err)
	}
	replay()
	if !containsInt(taskSnap(t, task).TagIDs, tagID) {
		t.Fatal("human status change should trigger the rule")
	}
}

func TestAutomationTaggedTriggerOnlyOnAdd(t *testing.T) {
	ctx := context.Background()
	f := automationProject(t)
	replay := captureAutomation(t)
	bug := projectTag(t, f.projectID, "bug")
	other := projectTag(t, f.projectID, "ui")
	rule := newRule(t, f.projectID, "Bugs are high priority", AutomationOnTagged,
		AutomationTriggerConfig{TagIDs: []int{bug}}, AutomationConditions{},
		AutomationAction{Type: AutomationActComment, Body: "Triaged {task} as a bug"})

	task := newProjectTask(t, f.projectID, "crash")
	replay()
	tags := []int{bug}
	if _, err := UpdateTask(ctx, 2, task, UpdateTaskInput{TagIDs: &tags}); err != nil {
		t.Fatal(err)
	}
	replay()
	tags = []int{bug, other}
	if _, err := UpdateTask(ctx, 2, task, UpdateTaskInput{TagIDs: &tags}); err != nil {
		t.Fatal(err)
	}
	replay()

	runs := ruleRuns(t, f.projectID, rule.ID)
	if len(runs) != 1 {
		t.Fatalf("rule should fire once, when bug was added; ran %d times", len(runs))
	}
	comments, err := storage.ListTaskComments(task)
	if err != nil {
		t.Fatal(err)
	}
	if len(comments) != 1 || comments[0].Body != "Triaged crash as a bug" || !comments[0].AuthorIsSystem ||
		comments[0].UserName != storage.SystemUserDisplayName {
		t.Fatalf("comments = %+v", comments)
	}
}

func TestAutomationUnblockedMovesToReady(t *testing.T) {
	ctx := context.Background()
	f := automationProject(t)
	captureAutomation(t)
	newRule(t, f.projectID, "Unblocked to Ready", AutomationOnUnblocked, AutomationTriggerConfig{},
		AutomationConditions{}, AutomationAction{Type: AutomationActSetStatus, StatusID: f.statuses["Ready"]})

	blocker := newProjectTask(t, f.projectID, "blocker")
	blocked := newProjectTask(t, f.projectID, "blocked")
	if _, err := AddTaskLink(ctx, 1, blocked, blocker, LinkKindBlockedBy); err != nil {
		t.Fatal(err)
	}
	if err := SetTaskCompleted(ctx, 1, blocker, true); err != nil {
		t.Fatal(err)
	}
	if got := taskSnap(t, blocked).StatusID; got != f.statuses["Ready"] {
		t.Fatalf("blocked task status = %d, want Ready", got)
	}
}

func TestAutomationTimedOverdueTagsOncePerDueDate(t *testing.T) {
	f := automationProject(t)
	captureAutomation(t)
	slipping := projectTag(t, f.projectID, "slipping")
	rule := newRule(t, f.projectID, "Slipping", storage.AutomationTimedOverdue, AutomationTriggerConfig{Days: 3},
		AutomationConditions{}, AutomationAction{Type: AutomationActAddTag, TagID: slipping})

	late := newProjectTask(t, f.projectID, "late")
	recent := newProjectTask(t, f.projectID, "recently due")
	setDue(t, late, -5)
	setDue(t, recent, -1)

	RunTimedAutomation()
	if !containsInt(taskSnap(t, late).TagIDs, slipping) {
		t.Fatal("task 5 days overdue should be tagged")
	}
	if containsInt(taskSnap(t, recent).TagIDs, slipping) {
		t.Fatal("task 1 day overdue should not be tagged yet")
	}

	// Someone removes the tag; the same due date does not re-trigger.
	none := []int{}
	if _, err := UpdateTask(context.Background(), 1, late, UpdateTaskInput{TagIDs: &none}); err != nil {
		t.Fatal(err)
	}
	RunTimedAutomation()
	if containsInt(taskSnap(t, late).TagIDs, slipping) {
		t.Fatal("rule should act once per due date")
	}
	if n := len(ruleRuns(t, f.projectID, rule.ID)); n != 1 {
		t.Fatalf("runs = %d, want 1", n)
	}

	// A new (still overdue) due date is a new episode.
	setDue(t, late, -4)
	RunTimedAutomation()
	if !containsInt(taskSnap(t, late).TagIDs, slipping) {
		t.Fatal("new due date should re-trigger")
	}
}

func setDue(t *testing.T, taskID, days int) {
	t.Helper()
	pool, err := storage.OpenDatabase()
	if err != nil {
		t.Fatal(err)
	}
	defer storage.CloseDatabase(pool)
	if _, err := pool.Exec(context.Background(),
		`UPDATE tasks SET due_date = CURRENT_DATE + ($2 * INTERVAL '1 day') WHERE id = $1`, taskID, days); err != nil {
		t.Fatal(err)
	}
}

func TestAutomationTimedArchiveCompleted(t *testing.T) {
	ctx := context.Background()
	f := automationProject(t)
	captureAutomation(t)
	newRule(t, f.projectID, "Archive done", storage.AutomationTimedCompleted, AutomationTriggerConfig{Days: 30},
		AutomationConditions{}, AutomationAction{Type: AutomationActArchive})

	old := newProjectTask(t, f.projectID, "old done")
	fresh := newProjectTask(t, f.projectID, "fresh done")
	for _, id := range []int{old, fresh} {
		if err := SetTaskCompleted(ctx, 1, id, true); err != nil {
			t.Fatal(err)
		}
	}
	pool, err := storage.OpenDatabase()
	if err != nil {
		t.Fatal(err)
	}
	_, err = pool.Exec(ctx, `UPDATE task_events SET created_at = NOW() - INTERVAL '40 days'
		WHERE task_id = $1 AND event_type = 'completed'`, old)
	storage.CloseDatabase(pool)
	if err != nil {
		t.Fatal(err)
	}

	RunTimedAutomation()
	if !storage.TaskIsArchived(old) {
		t.Fatal("task completed 40 days ago should be archived")
	}
	if storage.TaskIsArchived(fresh) {
		t.Fatal("task completed today should not be archived")
	}
}

func TestAutomationNotifyRespectsOptOut(t *testing.T) {
	ctx := context.Background()
	f := automationProject(t)
	replay := captureAutomation(t)
	newRule(t, f.projectID, "Ping", AutomationOnCommented, AutomationTriggerConfig{}, AutomationConditions{},
		AutomationAction{Type: AutomationActNotify, Target: "watchers", Body: "Rule saw a comment on {task}"})

	task := newProjectTask(t, f.projectID, "watched")
	if _, err := SetTaskWatching(ctx, 2, task, true); err != nil {
		t.Fatal(err)
	}
	replay()
	if _, err := AddCommentForUser(ctx, 1, task, "first"); err != nil {
		t.Fatal(err)
	}
	replay()
	if !hasTitlePrefix(notificationTitles(t, 2, task), "Rule saw a comment on watched") {
		t.Fatal("watcher should get the automation notice")
	}

	if _, err := UpdateNotificationPreferences(ctx, 2, map[string]bool{storage.NotificationAutomation: false}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = UpdateNotificationPreferences(ctx, 2, map[string]bool{storage.NotificationAutomation: true})
	})
	before := len(notificationTitles(t, 2, task))
	if _, err := AddCommentForUser(ctx, 1, task, "second"); err != nil {
		t.Fatal(err)
	}
	replay()
	titles := notificationTitles(t, 2, task)
	automationNotices := 0
	for _, s := range titles {
		if s == "Rule saw a comment on watched" {
			automationNotices++
		}
	}
	if automationNotices != 1 {
		t.Fatalf("opted-out watcher got another automation notice: %v (before %d)", titles, before)
	}
}

func TestAutomationPausesAfterErrorStreak(t *testing.T) {
	ctx := context.Background()
	f := automationProject(t)
	replay := captureAutomation(t)
	// No sprint is running, so "move to current sprint" fails every time.
	rule := newRule(t, f.projectID, "Pull into sprint", AutomationOnCommented, AutomationTriggerConfig{},
		AutomationConditions{}, AutomationAction{Type: AutomationActSetSprint, Sprint: "current"})

	task := newProjectTask(t, f.projectID, "noisy")
	replay()
	for i := 0; i < AutomationErrorStreakLimit+2; i++ {
		if _, err := AddCommentForUser(ctx, 1, task, "ping"); err != nil {
			t.Fatal(err)
		}
		replay()
	}
	got, err := storage.GetAutomationRule(f.projectID, rule.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Enabled || got.PausedReason == "" {
		t.Fatalf("rule should be paused: %+v", got)
	}
	if n := len(ruleRuns(t, f.projectID, rule.ID)); n != AutomationErrorStreakLimit {
		t.Fatalf("runs = %d, want %d", n, AutomationErrorStreakLimit)
	}

	// Re-enabling clears the streak and reason.
	on := true
	again, err := UpdateAutomationRule(1, f.projectID, rule.ID, AutomationRuleInput{Enabled: &on})
	if err != nil {
		t.Fatal(err)
	}
	if !again.Enabled || again.PausedReason != "" || again.ConsecutiveErrors != 0 {
		t.Fatalf("re-enabled rule = %+v", again)
	}
}

func TestAutomationPreview(t *testing.T) {
	f := automationProject(t)
	rule := "time.overdue"
	name := "p"
	cfg := AutomationTriggerConfig{Days: 0}
	late := newProjectTask(t, f.projectID, "late one")
	newProjectTask(t, f.projectID, "not due")
	setDue(t, late, -2)
	tasks, err := PreviewAutomationRule(1, f.projectID, AutomationRuleInput{Name: &name, TriggerType: &rule, TriggerConfig: &cfg})
	if err != nil {
		t.Fatal(err)
	}
	if len(tasks) != 1 || tasks[0].ID != late {
		t.Fatalf("preview = %+v", tasks)
	}

	ev := AutomationOnCreated
	cond := AutomationConditions{MinPriority: 3}
	high, err := CreateTask(context.Background(), 1, CreateTaskInput{Title: "urgent", ProjectID: &f.projectID, Priority: 3})
	if err != nil {
		t.Fatal(err)
	}
	tasks, err = PreviewAutomationRule(1, f.projectID, AutomationRuleInput{Name: &name, TriggerType: &ev, Conditions: &cond})
	if err != nil {
		t.Fatal(err)
	}
	if len(tasks) != 1 || tasks[0].ID != high {
		t.Fatalf("event preview = %+v", tasks)
	}
}
