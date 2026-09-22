package live

import (
	"context"
	"strings"
	"sync"

	"GoTodo/internal/hooks"
	"GoTodo/internal/storage"

	"github.com/redis/go-redis/v9"
)

var (
	hubMu sync.RWMutex
	hub   *Hub

	hookListenMu     sync.Mutex
	hookListeners    []hookListener
	nextHookListenID int
)

type hookListener struct {
	id int
	fn func(hooks.Event)
}

// Init installs the process-wide hub. A nil Redis client keeps fan-out in-process
// (unit tests). Production always has Redis from server startup.
func Init(client *redis.Client) {
	hubMu.Lock()
	defer hubMu.Unlock()
	hub = NewHub(client)
}

func Ready() bool {
	hubMu.RLock()
	defer hubMu.RUnlock()
	return hub != nil
}

func currentHub() *Hub {
	hubMu.RLock()
	defer hubMu.RUnlock()
	return hub
}

// Push broadcasts ev to the given users. No-op when Init has not been called.
func Push(ev Event, userIDs []int) {
	if h := currentHub(); h != nil {
		h.Publish(ev, userIDs)
	}
}

// SubscribeUser streams events for userID until ctx is cancelled.
func SubscribeUser(ctx context.Context, userID int) <-chan []byte {
	h := currentHub()
	if h == nil {
		ch := make(chan []byte)
		close(ch)
		return ch
	}
	return h.Subscribe(ctx, UserChannelKey(userID))
}

// TaskHookMeta is extra detail for outbound event hooks (not sent over SSE).
type TaskHookMeta struct {
	StatusChanged    bool
	OldStatus        string
	NewStatus        string
	Comment          string
	Changed          []string
	FieldChanges     []hooks.FieldChange
	Count            int
	JoinEmail        string
	JoinMessage      string
	MentionedUserIDs []int
	Mentions         []string
	MemberID         int
	MemberName       string
	SprintID         int
	SprintName       string
}

func hookEvent(actorID, taskID, projectID int, typ string, meta *TaskHookMeta) hooks.Event {
	ev := hooks.Event{
		Type:      typ,
		TaskID:    taskID,
		ProjectID: projectID,
		ActorID:   actorID,
	}
	if meta != nil {
		ev.StatusChanged = meta.StatusChanged
		ev.OldStatus = meta.OldStatus
		ev.NewStatus = meta.NewStatus
		ev.Comment = meta.Comment
		ev.Changed = meta.Changed
		ev.FieldChanges = meta.FieldChanges
		ev.Count = meta.Count
		ev.JoinEmail = meta.JoinEmail
		ev.JoinMessage = meta.JoinMessage
		ev.MentionedUserIDs = meta.MentionedUserIDs
		ev.Mentions = meta.Mentions
		ev.MemberID = meta.MemberID
		ev.MemberName = meta.MemberName
		ev.SprintID = meta.SprintID
		ev.SprintName = meta.SprintName
	}
	return ev
}

func hasHookListeners() bool {
	hookListenMu.Lock()
	defer hookListenMu.Unlock()
	return len(hookListeners) > 0
}

func notifyHookListeners(ev hooks.Event) {
	hookListenMu.Lock()
	listeners := append([]hookListener{}, hookListeners...)
	hookListenMu.Unlock()
	for _, l := range listeners {
		l.fn(ev)
	}
}

// ListenHooks records outbound extension events for tests. The returned stop
// function removes the listener.
func ListenHooks(fn func(hooks.Event)) func() {
	if fn == nil {
		return func() {}
	}
	hookListenMu.Lock()
	nextHookListenID++
	id := nextHookListenID
	hookListeners = append(hookListeners, hookListener{id: id, fn: fn})
	hookListenMu.Unlock()
	return func() {
		hookListenMu.Lock()
		defer hookListenMu.Unlock()
		kept := hookListeners[:0]
		for _, l := range hookListeners {
			if l.id != id {
				kept = append(kept, l)
			}
		}
		hookListeners = kept
	}
}

func emitHook(ev hooks.Event) {
	notifyHookListeners(ev)
	if hooks.HasWork() {
		go hooks.Dispatch(ev)
	}
}

func wantOutboundHooks() bool {
	return hooks.HasWork() || hasHookListeners()
}

func dispatchHook(actorID, taskID, projectID int, typ string, meta *TaskHookMeta) {
	if !wantOutboundHooks() {
		return
	}
	emitHook(hookEvent(actorID, taskID, projectID, typ, meta))
}

// DispatchHook sends an outbound extension event without an extra SSE publish.
func DispatchHook(actorID, taskID int, typ string, meta *TaskHookMeta) {
	if taskID <= 0 || !wantOutboundHooks() {
		return
	}
	_, projectID, err := storage.TaskOwnerAndProject(taskID)
	if err != nil {
		return
	}
	dispatchHook(actorID, taskID, projectID, typ, meta)
}

// DispatchOwnerHook sends a personal (no project) outbound extension event.
func DispatchOwnerHook(actorID, ownerID int, typ string, meta *TaskHookMeta) {
	if ownerID <= 0 || !wantOutboundHooks() {
		return
	}
	ev := hookEvent(actorID, 0, 0, typ, meta)
	ev.OwnerID = ownerID
	ev.Snapshot = &storage.HookTaskSnapshot{OwnerID: ownerID}
	emitHook(ev)
}

// DispatchProjectHook sends a project-level outbound event without an extra SSE publish.
func DispatchProjectHook(actorID, projectID int, typ string, meta *TaskHookMeta) {
	if projectID <= 0 || !wantOutboundHooks() {
		return
	}
	ev := hookEvent(actorID, 0, projectID, typ, meta)
	if snap, err := storage.GetHookProjectSnapshot(projectID); err == nil {
		ev.Snapshot = snap
	}
	emitHook(ev)
}

// AfterTaskChange notifies everyone who can currently see the task.
func AfterTaskChange(actorID, taskID int, typ string, extraProjectIDs ...int) {
	AfterTaskChangeMeta(actorID, taskID, typ, nil, extraProjectIDs...)
}

// AfterTaskChangeMeta is AfterTaskChange plus optional status-change metadata for extensions.
func AfterTaskChangeMeta(actorID, taskID int, typ string, meta *TaskHookMeta, extraProjectIDs ...int) {
	afterTaskChange(actorID, taskID, typ, meta, true, extraProjectIDs...)
}

// AfterTaskChangeLive is SSE-only (no outbound extension hooks).
func AfterTaskChangeLive(actorID, taskID int, typ string, extraProjectIDs ...int) {
	afterTaskChange(actorID, taskID, typ, nil, false, extraProjectIDs...)
}

func afterTaskChange(actorID, taskID int, typ string, meta *TaskHookMeta, emitHooks bool, extraProjectIDs ...int) {
	if taskID <= 0 {
		return
	}
	h := currentHub()
	wantHooks := emitHooks && wantOutboundHooks()
	if h == nil && !wantHooks {
		return
	}
	ownerID, projectID, err := storage.TaskOwnerAndProject(taskID)
	if err != nil {
		return
	}
	if h != nil {
		h.Publish(Event{
			Type:      typ,
			TaskID:    taskID,
			ProjectID: projectID,
			ActorID:   actorID,
		}, audience(ownerID, projectID, extraProjectIDs...))
	}
	if wantHooks {
		ev := hookEvent(actorID, taskID, projectID, typ, meta)
		ev.OwnerID = ownerID
		emitHook(ev)
	}
}

// AfterTasksChange notifies the union of audiences for many tasks (one SSE event).
// Outbound hooks fire per task except task.reordered, which is one project-level event.
func AfterTasksChange(actorID int, typ string, taskIDs []int, extraProjectIDs ...int) {
	publishTasksChange(actorID, typ, taskIDs, true, extraProjectIDs...)
}

// AfterTasksChangeLive is SSE-only (no outbound extension hooks).
func AfterTasksChangeLive(actorID int, typ string, taskIDs []int) {
	publishTasksChange(actorID, typ, taskIDs, false)
}

func publishTasksChange(actorID int, typ string, taskIDs []int, emitHooks bool, extraProjectIDs ...int) {
	h := currentHub()
	wantHooks := emitHooks && wantOutboundHooks()
	if (h == nil && !wantHooks) || len(taskIDs) == 0 {
		return
	}
	seen := make(map[int]struct{})
	users := make([]int, 0)
	projectID := 0
	for _, id := range taskIDs {
		ownerID, pid, err := storage.TaskOwnerAndProject(id)
		if err != nil {
			continue
		}
		if pid > 0 && projectID == 0 {
			projectID = pid
		}
		for _, u := range audience(ownerID, pid, extraProjectIDs...) {
			if _, ok := seen[u]; ok {
				continue
			}
			seen[u] = struct{}{}
			users = append(users, u)
		}
	}
	taskID := 0
	if len(taskIDs) == 1 {
		taskID = taskIDs[0]
	}
	if h != nil {
		h.Publish(Event{
			Type:      typ,
			TaskID:    taskID,
			ProjectID: projectID,
			ActorID:   actorID,
		}, users)
	}
	if !wantHooks {
		return
	}
	if typ == TypeTaskReordered {
		emitHook(hooks.Event{
			Type:      typ,
			ProjectID: projectID,
			ActorID:   actorID,
			Count:     len(taskIDs),
		})
		return
	}
	for _, id := range taskIDs {
		DispatchHook(actorID, id, typ, nil)
	}
}

// AfterProjectChange notifies current project members, plus any extra user IDs
// (for example a member who was just removed).
func AfterProjectChange(actorID, projectID int, typ string, extraUserIDs ...int) {
	afterProjectChange(actorID, projectID, typ, true, extraUserIDs...)
}

// AfterProjectChangeLive is SSE-only (no outbound extension hooks).
func AfterProjectChangeLive(actorID, projectID int, typ string, extraUserIDs ...int) {
	afterProjectChange(actorID, projectID, typ, false, extraUserIDs...)
}

// AfterExtensionStoreChange notifies project members that an extension document changed.
func AfterExtensionStoreChange(actorID, projectID int, extensionID, key string) {
	h := currentHub()
	if h == nil || projectID <= 0 {
		return
	}
	users, err := storage.ProjectMemberUserIDs(projectID)
	if err != nil {
		return
	}
	h.Publish(Event{
		Type:        TypeExtensionStore,
		ProjectID:   projectID,
		ActorID:     actorID,
		ExtensionID: strings.TrimSpace(extensionID),
		Key:         strings.TrimSpace(key),
	}, users)
}

func afterProjectChange(actorID, projectID int, typ string, emitHooks bool, extraUserIDs ...int) {
	h := currentHub()
	wantHooks := emitHooks && wantOutboundHooks()
	if projectID <= 0 || (h == nil && !wantHooks) {
		return
	}
	users, err := storage.ProjectMemberUserIDs(projectID)
	if err != nil {
		return
	}
	users = append(users, extraUserIDs...)
	if h != nil {
		h.Publish(Event{
			Type:      typ,
			ProjectID: projectID,
			ActorID:   actorID,
		}, users)
	}
	if wantHooks {
		emitHook(hooks.Event{
			Type:      typ,
			ProjectID: projectID,
			ActorID:   actorID,
		})
	}
}

// AfterJoinRequest notifies admins over SSE and site-level extension hooks.
func AfterJoinRequest(email, message string) {
	if !wantOutboundHooks() {
		return
	}
	emitHook(hooks.Event{
		Type:        TypeJoinRequest,
		JoinEmail:   email,
		JoinMessage: message,
	})
}

// AfterJoinReviewed sends a site-level hook after an admin approves or denies a join request.
func AfterJoinReviewed(email, message string, approved bool) {
	if !wantOutboundHooks() {
		return
	}
	typ := TypeJoinDenied
	if approved {
		typ = TypeJoinApproved
	}
	emitHook(hooks.Event{
		Type:        typ,
		JoinEmail:   email,
		JoinMessage: message,
	})
}

func audience(ownerID, projectID int, extraProjectIDs ...int) []int {
	users := make([]int, 0, 8)
	if ownerID > 0 {
		users = append(users, ownerID)
	}
	if projectID > 0 {
		if ids, err := storage.ProjectMemberUserIDs(projectID); err == nil {
			users = append(users, ids...)
		}
	}
	for _, pid := range extraProjectIDs {
		if pid <= 0 || pid == projectID {
			continue
		}
		if ids, err := storage.ProjectMemberUserIDs(pid); err == nil {
			users = append(users, ids...)
		}
	}
	return users
}
