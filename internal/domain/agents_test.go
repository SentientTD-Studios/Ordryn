package domain

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"GoTodo/internal/hooks"
	"GoTodo/internal/live"
	"GoTodo/internal/storage"
)

var agentHandleSeq atomic.Int64

func uniqueHandle(prefix string) string {
	return fmt.Sprintf("%s_%d_%d", prefix, agentHandleSeq.Add(1), testRunSuffix)
}

// testRunSuffix keeps handles unique if the test database is reused.
var testRunSuffix = time.Now().UnixNano() % 1_000_000

type agentFixture struct {
	projectID int
	statuses  map[string]int // name -> id
	aiStatus  int
}

// kanbanAgentProject is a kanban project owned by user 1 with user 2 as editor
// and an extra "AI queue" column.
func kanbanAgentProject(t *testing.T) agentFixture {
	t.Helper()
	ctx := context.Background()
	name := "Agents " + t.Name()
	if len(name) > 50 {
		name = name[:50]
	}
	pid := sharedProject(t, name)
	if _, err := SetProjectWorkflowMode(ctx, 1, pid, storage.WorkflowKanban); err != nil {
		t.Fatalf("kanban: %v", err)
	}
	ai, err := CreateProjectStatusForUser(ctx, 1, pid, CreateProjectStatusInput{Name: "AI queue"})
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	list, err := storage.ListProjectStatuses(pid)
	if err != nil {
		t.Fatal(err)
	}
	f := agentFixture{projectID: pid, statuses: map[string]int{}, aiStatus: ai.ID}
	for _, st := range list {
		f.statuses[st.Name] = st.ID
	}
	return f
}

func (f agentFixture) doneStatus(t *testing.T) int {
	t.Helper()
	st, err := storage.GetDoneProjectStatus(f.projectID)
	if err != nil || st == nil {
		t.Fatalf("done status: %v", err)
	}
	return st.ID
}

func newTestAgent(t *testing.T, projectID int, in AgentInput) *storage.ProjectAgent {
	t.Helper()
	if in.Handle == "" {
		in.Handle = uniqueHandle("bot")
	}
	if in.Role == nil {
		role := testRoleEditor
		ensureTestProjectRole(t, projectID, role)
		in.Role = &role
	}
	a, err := CreateProjectAgent(1, projectID, in)
	if err != nil {
		t.Fatalf("create agent: %v", err)
	}
	return a
}

func newProjectTask(t *testing.T, projectID int, title string) int {
	t.Helper()
	id, err := CreateTask(context.Background(), 1, CreateTaskInput{Title: title, ProjectID: &projectID})
	if err != nil {
		t.Fatalf("create task: %v", err)
	}
	return id
}

// captureHooks records outbound events and replays them into the agent
// dispatcher synchronously (production runs it on a goroutine).
func captureHooks(t *testing.T) func() []hooks.Event {
	t.Helper()
	var got []hooks.Event
	stop := live.ListenHooks(func(ev hooks.Event) { got = append(got, ev) })
	t.Cleanup(stop)
	return func() []hooks.Event {
		evs := got
		got = nil
		for _, ev := range evs {
			HandleAgentEvent(ev)
		}
		return evs
	}
}

func openRuns(t *testing.T, agentID, taskID int) []storage.AgentRun {
	t.Helper()
	runs, err := storage.ListAgentRuns(storage.AgentRunFilter{AgentID: agentID, TaskID: taskID,
		Statuses: []string{storage.AgentRunQueued, storage.AgentRunRunning}})
	if err != nil {
		t.Fatal(err)
	}
	return runs
}

func TestCreateAgentRequiresManagerAndValidInput(t *testing.T) {
	f := kanbanAgentProject(t)
	if _, err := CreateProjectAgent(2, f.projectID, AgentInput{Handle: uniqueHandle("nope")}); !errors.Is(err, ErrForbidden) {
		t.Fatalf("editor creating agent: err=%v want forbidden", err)
	}
	for _, bad := range []string{"", "ab", "has space", "dash-name"} {
		if _, err := CreateProjectAgent(1, f.projectID, AgentInput{Handle: bad}); !errors.Is(err, ErrValidation) {
			t.Errorf("handle %q: err=%v want validation", bad, err)
		}
	}
	taken := newTestAgent(t, f.projectID, AgentInput{})
	if _, err := CreateProjectAgent(1, f.projectID, AgentInput{Handle: taken.Handle}); !errors.Is(err, ErrConflict) {
		t.Fatalf("duplicate handle: err=%v want conflict", err)
	}
	if _, err := CreateProjectAgent(1, f.projectID, AgentInput{Handle: uniqueHandle("norole")}); !errors.Is(err, ErrValidation) {
		t.Fatalf("missing role: err=%v want validation", err)
	}
	owner := "owner"
	if _, err := CreateProjectAgent(1, f.projectID, AgentInput{Handle: uniqueHandle("own"), Role: &owner}); !errors.Is(err, ErrValidation) {
		t.Fatalf("owner role: err=%v want validation", err)
	}
	badStatus := []int{999999}
	if _, err := CreateProjectAgent(1, f.projectID, AgentInput{Handle: uniqueHandle("st"), AllowedStatusIDs: &badStatus}); !errors.Is(err, ErrValidation) {
		t.Fatalf("foreign status: err=%v want validation", err)
	}
	insecure := "http://example.com/hook"
	if _, err := CreateProjectAgent(1, f.projectID, AgentInput{Handle: uniqueHandle("wh"), WebhookURL: &insecure}); !errors.Is(err, ErrValidation) {
		t.Fatalf("http webhook: err=%v want validation", err)
	}

	// Defaults are conservative.
	if taken.CanComplete || taken.CanCreateTasks || !taken.CanComment || taken.TriggerBy != storage.AgentTriggerByManagers {
		t.Fatalf("unexpected defaults: %+v", taken)
	}
	members, err := storage.ListProjectMembers(f.projectID)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, m := range members {
		if m.UserID == taken.UserID {
			found = m.IsAgent && m.UserName == taken.Handle
		}
	}
	if !found {
		t.Fatalf("agent is not listed as an agent member")
	}
	if !storage.IsAgentUser(taken.UserID) || storage.IsAgentUser(1) {
		t.Fatalf("IsAgentUser mismatch")
	}
}

func TestAgentMemberCannotBeEditedThroughSharing(t *testing.T) {
	f := kanbanAgentProject(t)
	a := newTestAgent(t, f.projectID, AgentInput{})
	ctx := context.Background()
	if err := UpdateProjectMemberRole(ctx, 1, f.projectID, a.UserID, "viewer"); !errors.Is(err, ErrValidation) {
		t.Fatalf("role change via sharing: err=%v want validation", err)
	}
	if err := RemoveProjectMember(ctx, 1, f.projectID, a.UserID); !errors.Is(err, ErrValidation) {
		t.Fatalf("remove via sharing: err=%v want validation", err)
	}
}

func TestAgentMentionQueuesRunClaimsAndDedupes(t *testing.T) {
	f := kanbanAgentProject(t)
	a := newTestAgent(t, f.projectID, AgentInput{})
	task := newProjectTask(t, f.projectID, "Fix the login bug")
	flush := captureHooks(t)
	ctx := context.Background()

	if _, err := AddCommentForUser(ctx, 1, task, "@"+a.Handle+" please fix this; acceptance: tests pass"); err != nil {
		t.Fatal(err)
	}
	flush()
	runs := openRuns(t, a.ID, task)
	if len(runs) != 1 || runs[0].Trigger != storage.AgentTriggerMention || runs[0].TriggeredBy != 1 {
		t.Fatalf("runs after mention = %+v", runs)
	}
	if claimed, _ := storage.GetTaskClaimedBy(task); claimed != a.UserID {
		t.Fatalf("claimed_by=%d want agent %d", claimed, a.UserID)
	}

	// A second mention while the run is open does not queue another.
	if _, err := AddCommentForUser(ctx, 1, task, "@"+a.Handle+" any update?"); err != nil {
		t.Fatal(err)
	}
	flush()
	if runs := openRuns(t, a.ID, task); len(runs) != 1 {
		t.Fatalf("open runs after repeat mention = %d, want 1", len(runs))
	}

	// Editors cannot trigger an agent limited to managers.
	other := newProjectTask(t, f.projectID, "Editor task")
	if _, err := AddCommentForUser(ctx, 2, other, "@"+a.Handle+" do it"); err != nil {
		t.Fatal(err)
	}
	flush()
	if runs := openRuns(t, a.ID, other); len(runs) != 0 {
		t.Fatalf("editor mention queued %d runs, want 0", len(runs))
	}
	writers := storage.AgentTriggerByWriters
	if _, err := UpdateProjectAgent(1, f.projectID, a.ID, AgentInput{TriggerBy: &writers}); err != nil {
		t.Fatal(err)
	}
	if _, err := AddCommentForUser(ctx, 2, other, "@"+a.Handle+" now you can"); err != nil {
		t.Fatal(err)
	}
	flush()
	if runs := openRuns(t, a.ID, other); len(runs) != 1 {
		t.Fatalf("writer mention queued %d runs, want 1", len(runs))
	}
}

func TestAgentStatusTriggerIgnoresAgentActors(t *testing.T) {
	f := kanbanAgentProject(t)
	trigger := []int{f.aiStatus}
	a := newTestAgent(t, f.projectID, AgentInput{TriggerStatusIDs: &trigger})
	task := newProjectTask(t, f.projectID, "Write release notes")
	flush := captureHooks(t)

	sid := &f.aiStatus
	if _, err := UpdateTask(context.Background(), 1, task, UpdateTaskInput{StatusID: &sid}); err != nil {
		t.Fatal(err)
	}
	flush()
	runs := openRuns(t, a.ID, task)
	if len(runs) != 1 || runs[0].Trigger != storage.AgentTriggerStatus {
		t.Fatalf("runs after move = %+v", runs)
	}

	// Events caused by an agent never start runs (no agent loops).
	if _, err := FinishAgentRun(a.UserID, runs[0].ID, storage.AgentRunSucceeded, "done"); err != nil {
		t.Fatal(err)
	}
	HandleAgentEvent(hooks.Event{Type: hooks.EventTaskStatusChanged, TaskID: task, ProjectID: f.projectID, ActorID: a.UserID})
	if runs := openRuns(t, a.ID, task); len(runs) != 0 {
		t.Fatalf("agent-caused move queued %d runs, want 0", len(runs))
	}
}

func TestAgentRunLifecycle(t *testing.T) {
	f := kanbanAgentProject(t)
	a := newTestAgent(t, f.projectID, AgentInput{})
	task := newProjectTask(t, f.projectID, "Lifecycle")

	if _, err := DispatchAgentRun(2, task, a.ID, "go"); !errors.Is(err, ErrForbidden) {
		t.Fatalf("editor dispatch: err=%v want forbidden", err)
	}
	run, err := DispatchAgentRun(1, task, a.ID, "Please triage")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := DispatchAgentRun(1, task, a.ID, "again"); !errors.Is(err, ErrConflict) {
		t.Fatalf("duplicate dispatch: err=%v want conflict", err)
	}

	// Another agent cannot touch this run.
	b := newTestAgent(t, f.projectID, AgentInput{})
	if _, err := StartAgentRun(b.UserID, run.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("foreign agent start: err=%v want not found", err)
	}

	queue, err := ListAgentQueue(a.UserID, nil, 0)
	if err != nil || len(queue) != 1 || queue[0].ID != run.ID || queue[0].Note != "Please triage" {
		t.Fatalf("queue=%+v err=%v", queue, err)
	}
	if _, err := StartAgentRun(a.UserID, run.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := StartAgentRun(a.UserID, run.ID); !errors.Is(err, ErrConflict) {
		t.Fatalf("double start: err=%v want conflict", err)
	}
	if _, err := FinishAgentRun(a.UserID, run.ID, "maybe", ""); !errors.Is(err, ErrValidation) {
		t.Fatalf("bad finish status: err=%v want validation", err)
	}
	done, err := FinishAgentRun(a.UserID, run.ID, storage.AgentRunSucceeded, "Triaged as P2")
	if err != nil {
		t.Fatal(err)
	}
	if done.Status != storage.AgentRunSucceeded || done.Summary != "Triaged as P2" || done.FinishedAt == nil {
		t.Fatalf("finished run = %+v", done)
	}
	if claimed, _ := storage.GetTaskClaimedBy(task); claimed != 0 {
		t.Fatalf("claim not released: %d", claimed)
	}
	if _, err := FinishAgentRun(a.UserID, run.ID, storage.AgentRunFailed, ""); !errors.Is(err, ErrConflict) {
		t.Fatalf("finish twice: err=%v want conflict", err)
	}

	// Whoever started a run (or a manager) can cancel it.
	run2, err := DispatchAgentRun(1, task, a.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := CancelAgentRun(2, run2.ID); !errors.Is(err, ErrForbidden) {
		t.Fatalf("editor cancel: err=%v want forbidden", err)
	}
	if c, err := CancelAgentRun(1, run2.ID); err != nil || c.Status != storage.AgentRunCancelled {
		t.Fatalf("cancel: %+v %v", c, err)
	}
}

func TestAgentHourlyLimitAndPause(t *testing.T) {
	f := kanbanAgentProject(t)
	one := 1
	a := newTestAgent(t, f.projectID, AgentInput{MaxRunsPerHour: &one})
	t1 := newProjectTask(t, f.projectID, "One")
	t2 := newProjectTask(t, f.projectID, "Two")
	if _, err := DispatchAgentRun(1, t1, a.ID, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := DispatchAgentRun(1, t2, a.ID, ""); !errors.Is(err, ErrConflict) {
		t.Fatalf("over hourly limit: err=%v want conflict", err)
	}
	off := false
	ten := 10
	if _, err := UpdateProjectAgent(1, f.projectID, a.ID, AgentInput{Enabled: &off, MaxRunsPerHour: &ten}); err != nil {
		t.Fatal(err)
	}
	if _, err := DispatchAgentRun(1, t2, a.ID, ""); !errors.Is(err, ErrConflict) {
		t.Fatalf("paused agent: err=%v want conflict", err)
	}
}

func TestRemoveAgentRevokesEverything(t *testing.T) {
	f := kanbanAgentProject(t)
	a := newTestAgent(t, f.projectID, AgentInput{})
	task := newProjectTask(t, f.projectID, "Remove me")
	key, _, err := CreateAgentAPIKey(1, f.projectID, a.ID, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	p, err := storage.LookupAPIKey(key)
	if err != nil || !p.IsAgent || p.UserID != a.UserID || p.ProjectID != f.projectID {
		t.Fatalf("agent key principal = %+v err=%v", p, err)
	}
	// Agent keys stay out of the regular project key list.
	if keys, _ := storage.ListProjectAPIKeys(f.projectID); len(keys) != 0 {
		t.Fatalf("agent key leaked into project keys: %+v", keys)
	}
	run, err := DispatchAgentRun(1, task, a.ID, "")
	if err != nil {
		t.Fatal(err)
	}

	if err := RemoveProjectAgent(2, f.projectID, a.ID); !errors.Is(err, ErrForbidden) {
		t.Fatalf("editor remove: err=%v want forbidden", err)
	}
	if err := RemoveProjectAgent(1, f.projectID, a.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := storage.LookupAPIKey(key); err == nil {
		t.Fatal("key still works after removal")
	}
	if role, _ := storage.GetProjectRole(f.projectID, a.UserID); role != "" {
		t.Fatalf("agent still a member with role %q", role)
	}
	if got, _ := storage.GetAgentRun(run.ID); got == nil || got.Status != storage.AgentRunCancelled {
		t.Fatalf("open run not cancelled: %+v", got)
	}
	if claimed, _ := storage.GetTaskClaimedBy(task); claimed != 0 {
		t.Fatalf("claim not cleared: %d", claimed)
	}
	if _, err := GetProjectAgent(1, f.projectID, a.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("removed agent still loads: %v", err)
	}
}

func TestAgentsGetNoNotifications(t *testing.T) {
	f := kanbanAgentProject(t)
	a := newTestAgent(t, f.projectID, AgentInput{})
	task := newProjectTask(t, f.projectID, "Quiet")
	NotifyProjectMembersTaskCommented(task, 1, f.projectID, "hello everyone")
	if titles := notificationTitles(t, a.UserID, task); len(titles) != 0 {
		t.Fatalf("agent got notifications: %v", titles)
	}
	if titles := notificationTitles(t, 2, task); len(titles) == 0 {
		t.Fatal("human member got no notification")
	}
}

func TestAgentRunPayloadCarriesTaskAndGuardrailsButNoKey(t *testing.T) {
	f := kanbanAgentProject(t)
	instr := "Fix it, then move to review."
	allowed := []int{f.aiStatus}
	a := newTestAgent(t, f.projectID, AgentInput{Instructions: &instr, AllowedStatusIDs: &allowed})
	task := newProjectTask(t, f.projectID, "Payload task")
	key, _, err := CreateAgentAPIKey(1, f.projectID, a.ID, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	run, err := DispatchAgentRun(1, task, a.ID, "acceptance: green CI")
	if err != nil {
		t.Fatal(err)
	}
	snap, err := storage.GetHookTaskSnapshot(task)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(agentRunPayload(a, snap.ProjectName, run, snap))
	if err != nil {
		t.Fatal(err)
	}
	body := string(raw)
	for _, want := range []string{`"event":"agent.run"`, `"title":"Payload task"`, instr, "acceptance: green CI",
		fmt.Sprintf(`"allowed_status_ids":[%d]`, f.aiStatus), `"can_complete":false`} {
		if !strings.Contains(body, want) {
			t.Errorf("payload missing %s: %s", want, body)
		}
	}
	if strings.Contains(body, key) || strings.Contains(body, "gotodo_") {
		t.Fatalf("payload leaks an API key: %s", body)
	}
}

func TestAgentSelectedCallersOnly(t *testing.T) {
	f := kanbanAgentProject(t) // user 1 owner (manager), user 2 editor
	ctx := context.Background()
	if err := upsertTestMember(t, f.projectID, 3, testRoleViewer); err != nil {
		t.Fatal(err)
	}
	selected := storage.AgentTriggerBySelected

	// Selected mode needs at least one role or member.
	if _, err := CreateProjectAgent(1, f.projectID, AgentInput{Handle: uniqueHandle("sel"), TriggerBy: &selected}); !errors.Is(err, ErrValidation) {
		t.Fatalf("empty selection: err=%v want validation", err)
	}
	outsider := []int{99999}
	if _, err := CreateProjectAgent(1, f.projectID, AgentInput{Handle: uniqueHandle("sel"), TriggerBy: &selected, TriggerUserIDs: &outsider}); !errors.Is(err, ErrValidation) {
		t.Fatalf("non-member caller: err=%v want validation", err)
	}
	badRole := []string{"no_such_role"}
	if _, err := CreateProjectAgent(1, f.projectID, AgentInput{Handle: uniqueHandle("sel"), TriggerBy: &selected, TriggerRoleSlugs: &badRole}); !errors.Is(err, ErrValidation) {
		t.Fatalf("unknown role: err=%v want validation", err)
	}

	// Only the viewer (by user) may call it; the owner/manager is not implied.
	users := []int{3}
	a := newTestAgent(t, f.projectID, AgentInput{TriggerBy: &selected, TriggerUserIDs: &users})
	flush := captureHooks(t)
	task := newProjectTask(t, f.projectID, "Selected callers")

	for _, uid := range []int{1, 2} {
		if _, err := AddCommentForUser(ctx, uid, task, "@"+a.Handle+" go"); err != nil {
			t.Fatal(err)
		}
	}
	flush()
	if runs := openRuns(t, a.ID, task); len(runs) != 0 {
		t.Fatalf("unlisted callers queued %d runs, want 0 (ignored)", len(runs))
	}
	if _, err := DispatchAgentRun(1, task, a.ID, ""); !errors.Is(err, ErrForbidden) {
		t.Fatalf("unlisted manager dispatch: err=%v want forbidden", err)
	}
	if res, err := ListTaskAgentRuns(2, task); err != nil || len(res.Agents) != 0 {
		t.Fatalf("unlisted editor offered agents: %+v %v", res, err)
	}
	if _, err := AddCommentForUser(ctx, 3, task, "@"+a.Handle+" go"); err != nil {
		t.Fatal(err)
	}
	flush()
	if runs := openRuns(t, a.ID, task); len(runs) != 1 || runs[0].TriggeredBy != 3 {
		t.Fatalf("listed member's mention: %+v", runs)
	}

	// Allow the editor role too; then editors may call it.
	roles := []string{"editor"}
	if _, err := UpdateProjectAgent(1, f.projectID, a.ID, AgentInput{TriggerRoleSlugs: &roles}); err != nil {
		t.Fatal(err)
	}
	other := newProjectTask(t, f.projectID, "Editor role")
	if _, err := DispatchAgentRun(2, other, a.ID, ""); err != nil {
		t.Fatalf("listed role dispatch: %v", err)
	}
	if res, _ := ListTaskAgentRuns(2, other); len(res.Agents) != 1 {
		t.Fatalf("listed editor not offered the agent: %+v", res)
	}

	// Removing the member from the project revokes their ability to call it.
	if err := storage.RemoveProjectMember(f.projectID, 3); err != nil {
		t.Fatal(err)
	}
	third := newProjectTask(t, f.projectID, "After leaving")
	HandleAgentEvent(hooks.Event{Type: hooks.EventTaskMentioned, TaskID: third, ProjectID: f.projectID, ActorID: 3,
		MentionedUserIDs: []int{a.UserID}})
	if runs := openRuns(t, a.ID, third); len(runs) != 0 {
		t.Fatalf("former member still triggered a run")
	}
}

func TestAgentCommentsAreFlagged(t *testing.T) {
	f := kanbanAgentProject(t)
	a := newTestAgent(t, f.projectID, AgentInput{})
	task := newProjectTask(t, f.projectID, "Flagged comments")
	ctx := context.Background()
	if _, err := AddCommentForUser(ctx, 1, task, "from a person"); err != nil {
		t.Fatal(err)
	}
	if _, err := AddCommentForUser(ctx, a.UserID, task, "from the agent"); err != nil {
		t.Fatal(err)
	}
	list, err := ListCommentsForUser(ctx, 1, task)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]bool{}
	for _, c := range list {
		got[c.Body] = c.AuthorIsAgent
	}
	if got["from a person"] || !got["from the agent"] {
		t.Fatalf("author_is_agent flags = %v", got)
	}
}
