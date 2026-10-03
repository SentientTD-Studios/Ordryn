package domain

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"

	"GoTodo/internal/live"
	"GoTodo/internal/storage"
)

// Notification types for watcher fan-out.
const (
	NotificationTaskActivity  = "task_activity"
	NotificationTaskUnblocked = "task_unblocked"
)

// TaskWatchState is the caller's subscription plus who else watches a task.
type TaskWatchState struct {
	Watching   bool
	ViaProject bool
	ProjectID  int
	Watchers   []storage.TaskWatcher
}

// SetTaskWatching subscribes or unsubscribes the caller from a readable task.
func SetTaskWatching(ctx context.Context, userID, taskID int, watch bool) (*TaskWatchState, error) {
	canRead, _, _, err := storage.CanUserAccessTask(taskID, userID)
	if err != nil {
		return nil, err
	}
	if !canRead {
		return nil, ErrNotFound
	}
	if watch {
		_, err = storage.AddTaskWatcher(taskID, userID)
	} else {
		_, err = storage.RemoveTaskWatcher(taskID, userID)
	}
	if err != nil {
		return nil, err
	}
	return GetTaskWatchState(ctx, userID, taskID)
}

// GetTaskWatchState reports the caller's subscription to a readable task.
func GetTaskWatchState(ctx context.Context, userID, taskID int) (*TaskWatchState, error) {
	_ = ctx
	canRead, _, projectID, err := storage.CanUserAccessTask(taskID, userID)
	if err != nil {
		return nil, err
	}
	if !canRead {
		return nil, ErrNotFound
	}
	direct, err := storage.IsTaskWatcher(taskID, userID)
	if err != nil {
		return nil, err
	}
	viaProject, err := storage.IsProjectWatcher(projectID, userID)
	if err != nil {
		return nil, err
	}
	watchers, err := storage.ListTaskWatchers(taskID)
	if err != nil {
		return nil, err
	}
	return &TaskWatchState{Watching: direct || viaProject, ViaProject: viaProject && !direct, ProjectID: projectID, Watchers: watchers}, nil
}

// SetProjectWatching subscribes or unsubscribes the caller from every task in a project.
func SetProjectWatching(ctx context.Context, userID, projectID int, watch bool) (bool, error) {
	_ = ctx
	if _, err := storage.GetAccessibleProjectByID(projectID, userID); err != nil {
		return false, ErrNotFound
	}
	var err error
	if watch {
		_, err = storage.AddProjectWatcher(projectID, userID)
	} else {
		_, err = storage.RemoveProjectWatcher(projectID, userID)
	}
	if err != nil {
		return false, err
	}
	return watch, nil
}

// IsWatchingProject reports whether the caller watches an accessible project.
func IsWatchingProject(ctx context.Context, userID, projectID int) (bool, error) {
	_ = ctx
	if _, err := storage.GetAccessibleProjectByID(projectID, userID); err != nil {
		return false, ErrNotFound
	}
	return storage.IsProjectWatcher(projectID, userID)
}

// autoWatchTask subscribes a user who created, commented on, or claimed a task.
// Only project tasks have other people to hear from, so personal tasks are skipped.
func autoWatchTask(userID, taskID int) {
	if userID <= 0 || taskID <= 0 {
		return
	}
	pid, err := storage.GetTaskProjectID(taskID)
	if err != nil || pid <= 0 {
		return
	}
	if _, err := storage.AddTaskWatcher(taskID, userID); err != nil {
		log.Printf("watch: auto-watch task %d user %d: %v", taskID, userID, err)
	}
}

// notifyTaskWatchers sends an in-app notification to everyone watching taskID
// (directly or via its project) except the actor and anyone who lost access.
func notifyTaskWatchers(actorID, taskID int, nType, title, body string) {
	recipients, err := storage.TaskWatcherRecipients(taskID)
	if err != nil {
		log.Printf("watch: recipients task %d: %v", taskID, err)
		return
	}
	projectID, _ := storage.GetTaskProjectID(taskID)
	items := make([]storage.UserNotification, 0, len(recipients))
	for _, uid := range recipients {
		if uid == actorID {
			continue
		}
		if ok, _, _, err := storage.CanUserAccessTask(taskID, uid); err != nil || !ok {
			continue
		}
		items = append(items, storage.UserNotification{
			UserID:      uid,
			ActorUserID: actorID,
			Type:        nType,
			ProjectID:   projectID,
			TaskID:      taskID,
			Title:       title,
			Body:        body,
		})
	}
	if len(items) == 0 {
		return
	}
	notified, err := storage.CreateUserNotificationsBulk(items)
	if err != nil {
		log.Printf("watch: notify task %d: %v", taskID, err)
		return
	}
	if len(notified) > 0 {
		live.Push(live.Event{Type: live.TypeNotificationCreated, TaskID: taskID}, notified)
	}
}

func taskTitleOrID(taskID int) string {
	if td, err := storage.GetTaskTitleDescription(taskID); err == nil && td != nil && strings.TrimSpace(td.Title) != "" {
		return td.Title
	}
	return fmt.Sprintf("Task #%d", taskID)
}

// notifyWatchersCompletion tells watchers a task was completed or reopened and,
// on completion, tells watchers of tasks it blocked when they become unblocked.
func notifyWatchersCompletion(actorID, taskID int, completed bool) {
	title := taskTitleOrID(taskID)
	if completed {
		notifyTaskWatchers(actorID, taskID, NotificationTaskActivity, "Completed: "+title, "")
	} else {
		notifyTaskWatchers(actorID, taskID, NotificationTaskActivity, "Reopened: "+title, "")
		return
	}
	blocked, err := storage.ListTasksBlockedBy(taskID)
	if err != nil || len(blocked) == 0 {
		return
	}
	counts, err := storage.OpenBlockerCounts(blocked)
	if err != nil {
		return
	}
	for _, id := range blocked {
		if counts[id] > 0 {
			continue
		}
		notifyTaskWatchers(actorID, id, NotificationTaskUnblocked, "Unblocked: "+taskTitleOrID(id), "Blocker completed: "+title)
		live.AfterTaskChangeLive(actorID, id, live.TypeTaskUpdated)
	}
}

// --- links ---

// Link kinds accepted from clients, relative to the task in the URL.
const (
	LinkKindBlocks       = "blocks"
	LinkKindBlockedBy    = "blocked_by"
	LinkKindRelates      = "relates"
	LinkKindDuplicates   = "duplicates"
	LinkKindDuplicatedBy = "duplicated_by"
)

// TaskLinkView is a link as seen from one task.
type TaskLinkView struct {
	LinkID    int
	Kind      string // one of the LinkKind* constants, relative to the viewing task
	TaskID    int
	Title     string
	Completed bool
	ProjectID int
}

// normalizeLink maps a kind relative to taskID into a stored (from, to, type).
func normalizeLink(taskID, otherID int, kind string) (from, to int, linkType string, err error) {
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case LinkKindBlocks:
		return taskID, otherID, storage.TaskLinkBlocks, nil
	case LinkKindBlockedBy:
		return otherID, taskID, storage.TaskLinkBlocks, nil
	case LinkKindDuplicates:
		return taskID, otherID, storage.TaskLinkDuplicates, nil
	case LinkKindDuplicatedBy:
		return otherID, taskID, storage.TaskLinkDuplicates, nil
	case LinkKindRelates:
		if taskID < otherID {
			return taskID, otherID, storage.TaskLinkRelates, nil
		}
		return otherID, taskID, storage.TaskLinkRelates, nil
	}
	return 0, 0, "", fmt.Errorf("%w: type must be blocks, blocked_by, relates, duplicates, or duplicated_by", ErrValidation)
}

// kindFor describes a stored link from viewerID's side.
func kindFor(l storage.TaskLink, viewerID int) (kind string, otherID int) {
	outgoing := l.FromTaskID == viewerID
	otherID = l.ToTaskID
	if !outgoing {
		otherID = l.FromTaskID
	}
	switch l.Type {
	case storage.TaskLinkBlocks:
		if outgoing {
			return LinkKindBlocks, otherID
		}
		return LinkKindBlockedBy, otherID
	case storage.TaskLinkDuplicates:
		if outgoing {
			return LinkKindDuplicates, otherID
		}
		return LinkKindDuplicatedBy, otherID
	}
	return LinkKindRelates, otherID
}

func linkEventMeta(kind string, otherID int) map[string]interface{} {
	return map[string]interface{}{"kind": kind, "task_id": otherID, "title": taskTitleOrID(otherID)}
}

// AddTaskLink links taskID to otherID. kind is relative to taskID.
// The caller needs edit rights on taskID and read access to otherID.
func AddTaskLink(ctx context.Context, userID, taskID, otherID int, kind string) (*TaskLinkView, error) {
	_ = ctx
	if otherID <= 0 || otherID == taskID {
		return nil, fmt.Errorf("%w: choose a different task to link", ErrValidation)
	}
	if err := requireTaskPermOnTask(taskID, userID, storage.PermTasksEdit); err != nil {
		return nil, err
	}
	if ok, _, _, err := storage.CanUserAccessTask(otherID, userID); err != nil || !ok {
		return nil, fmt.Errorf("%w: linked task not found", ErrValidation)
	}
	from, to, linkType, err := normalizeLink(taskID, otherID, kind)
	if err != nil {
		return nil, err
	}
	if linkType == storage.TaskLinkBlocks {
		cycle, err := storage.BlockingPathExists(to, from)
		if err != nil {
			return nil, err
		}
		if cycle {
			return nil, fmt.Errorf("%w: that would create a blocking cycle", ErrConflict)
		}
	}
	id, err := storage.InsertTaskLink(from, to, linkType, userID)
	if err != nil {
		if errors.Is(err, storage.ErrTaskLinkExists) {
			return nil, fmt.Errorf("%w: those tasks are already linked that way", ErrConflict)
		}
		return nil, err
	}
	link := storage.TaskLink{ID: id, FromTaskID: from, ToTaskID: to, Type: linkType}
	myKind, _ := kindFor(link, taskID)
	theirKind, _ := kindFor(link, otherID)
	_ = storage.LogTaskEvent(taskID, userID, "link_added", linkEventMeta(myKind, otherID))
	_ = storage.LogTaskEvent(otherID, userID, "link_added", linkEventMeta(theirKind, taskID))
	live.AfterTaskChangeLive(userID, taskID, live.TypeTaskUpdated)
	live.AfterTaskChangeLive(userID, otherID, live.TypeTaskUpdated)
	if linkType == storage.TaskLinkBlocks {
		notifyTaskWatchers(userID, to, NotificationTaskActivity,
			"Blocked: "+taskTitleOrID(to), "Now blocked by "+taskTitleOrID(from))
	}
	view := &TaskLinkView{LinkID: id, Kind: myKind, TaskID: otherID}
	fillLinkTarget(view)
	return view, nil
}

// RemoveTaskLink deletes a link that touches taskID.
func RemoveTaskLink(ctx context.Context, userID, taskID, linkID int) error {
	_ = ctx
	link, err := storage.GetTaskLink(linkID)
	if err != nil {
		return err
	}
	if link == nil || (link.FromTaskID != taskID && link.ToTaskID != taskID) {
		return ErrNotFound
	}
	if err := requireTaskPermOnTask(taskID, userID, storage.PermTasksEdit); err != nil {
		return err
	}
	if _, err := storage.DeleteTaskLink(linkID); err != nil {
		return err
	}
	myKind, otherID := kindFor(*link, taskID)
	theirKind, _ := kindFor(*link, otherID)
	_ = storage.LogTaskEvent(taskID, userID, "link_removed", linkEventMeta(myKind, otherID))
	_ = storage.LogTaskEvent(otherID, userID, "link_removed", linkEventMeta(theirKind, taskID))
	live.AfterTaskChangeLive(userID, taskID, live.TypeTaskUpdated)
	live.AfterTaskChangeLive(userID, otherID, live.TypeTaskUpdated)
	return nil
}

// ListTaskLinks returns links on a readable task, hiding tasks the caller cannot see.
func ListTaskLinks(ctx context.Context, userID, taskID int) ([]TaskLinkView, error) {
	_ = ctx
	canRead, _, _, err := storage.CanUserAccessTask(taskID, userID)
	if err != nil {
		return nil, err
	}
	if !canRead {
		return nil, ErrNotFound
	}
	links, err := storage.ListTaskLinks(taskID)
	if err != nil {
		return nil, err
	}
	out := make([]TaskLinkView, 0, len(links))
	for _, l := range links {
		kind, otherID := kindFor(l, taskID)
		if ok, _, _, err := storage.CanUserAccessTask(otherID, userID); err != nil || !ok {
			continue
		}
		v := TaskLinkView{LinkID: l.ID, Kind: kind, TaskID: otherID}
		fillLinkTarget(&v)
		out = append(out, v)
	}
	return out, nil
}

func fillLinkTarget(v *TaskLinkView) {
	src, err := storage.GetTaskRecurrenceSource(v.TaskID)
	if err == nil && src != nil {
		v.Title = src.Title
		v.ProjectID = src.ProjectID
	}
	if completed, err := lookupTaskCompleted(v.TaskID); err == nil {
		v.Completed = completed
	}
}
