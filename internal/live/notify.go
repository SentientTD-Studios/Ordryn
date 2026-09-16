package live

import (
	"context"
	"sync"

	"GoTodo/internal/hooks"
	"GoTodo/internal/storage"

	"github.com/redis/go-redis/v9"
)

var (
	hubMu sync.RWMutex
	hub   *Hub
)

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
	StatusChanged bool
	OldStatus     string
	NewStatus     string
	Comment       string
	Changed       []string
	Count         int
	JoinEmail     string
	JoinMessage   string
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
		ev.Count = meta.Count
		ev.JoinEmail = meta.JoinEmail
		ev.JoinMessage = meta.JoinMessage
	}
	return ev
}

func dispatchHook(actorID, taskID, projectID int, typ string, meta *TaskHookMeta) {
	if !hooks.HasWork() {
		return
	}
	go hooks.Dispatch(hookEvent(actorID, taskID, projectID, typ, meta))
}

// DispatchHook sends an outbound extension event without an extra SSE publish.
func DispatchHook(actorID, taskID int, typ string, meta *TaskHookMeta) {
	if taskID <= 0 || !hooks.HasWork() {
		return
	}
	_, projectID, err := storage.TaskOwnerAndProject(taskID)
	if err != nil {
		return
	}
	dispatchHook(actorID, taskID, projectID, typ, meta)
}

// AfterTaskChange notifies everyone who can currently see the task.
func AfterTaskChange(actorID, taskID int, typ string, extraProjectIDs ...int) {
	AfterTaskChangeMeta(actorID, taskID, typ, nil, extraProjectIDs...)
}

// AfterTaskChangeMeta is AfterTaskChange plus optional status-change metadata for extensions.
func AfterTaskChangeMeta(actorID, taskID int, typ string, meta *TaskHookMeta, extraProjectIDs ...int) {
	if taskID <= 0 {
		return
	}
	h := currentHub()
	wantHooks := hooks.HasWork()
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
		go hooks.Dispatch(ev)
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
	wantHooks := emitHooks && hooks.HasWork()
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
		go hooks.Dispatch(hooks.Event{
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
	h := currentHub()
	wantHooks := hooks.HasWork()
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
		go hooks.Dispatch(hooks.Event{
			Type:      typ,
			ProjectID: projectID,
			ActorID:   actorID,
		})
	}
}

// AfterJoinRequest notifies admins over SSE and site-level extension hooks.
func AfterJoinRequest(email, message string) {
	if hooks.HasWork() {
		go hooks.Dispatch(hooks.Event{
			Type:        TypeJoinRequest,
			JoinEmail:   email,
			JoinMessage: message,
		})
	}
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
