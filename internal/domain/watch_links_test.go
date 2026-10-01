package domain

import (
	"context"
	"errors"
	"strings"
	"testing"

	"GoTodo/internal/storage"
)

func sharedProject(t *testing.T, name string) int {
	t.Helper()
	proj, err := CreateProject(context.Background(), 1, name, "")
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	if err := storage.UpsertProjectMember(proj.ID, 2, "editor"); err != nil {
		t.Fatalf("add member: %v", err)
	}
	return proj.ID
}

func notificationTitles(t *testing.T, userID, taskID int) []string {
	t.Helper()
	list, _, err := storage.ListUserNotifications(userID, 200, 0)
	if err != nil {
		t.Fatalf("list notifications: %v", err)
	}
	out := []string{}
	for _, n := range list {
		if n.TaskID == taskID {
			out = append(out, n.Title)
		}
	}
	return out
}

func hasTitlePrefix(titles []string, prefix string) bool {
	for _, s := range titles {
		if strings.HasPrefix(s, prefix) {
			return true
		}
	}
	return false
}

func TestWatchersGetActivityNotifications(t *testing.T) {
	ctx := context.Background()
	pid := sharedProject(t, "Watch Proj")
	taskID := mustCreateTask(t, 1, CreateTaskInput{Title: "Watched task", ProjectID: &pid})

	// Creator auto-watches; user 2 watches explicitly.
	if st, err := GetTaskWatchState(ctx, 1, taskID); err != nil || !st.Watching {
		t.Fatalf("creator should auto-watch: %+v err=%v", st, err)
	}
	st, err := SetTaskWatching(ctx, 2, taskID, true)
	if err != nil || !st.Watching || len(st.Watchers) != 2 {
		t.Fatalf("watch: %+v err=%v", st, err)
	}

	due := "2026-10-10"
	if _, err := UpdateTask(ctx, 1, taskID, UpdateTaskInput{DueDate: &due}); err != nil {
		t.Fatalf("set due: %v", err)
	}
	if err := SetTaskCompleted(ctx, 1, taskID, true); err != nil {
		t.Fatalf("complete: %v", err)
	}
	got := notificationTitles(t, 2, taskID)
	if !hasTitlePrefix(got, "Due date changed: Watched task") || !hasTitlePrefix(got, "Completed: Watched task") {
		t.Fatalf("watcher notifications: %v", got)
	}
	if actor := notificationTitles(t, 1, taskID); len(actor) != 0 {
		t.Fatalf("actor should not be notified of own changes: %v", actor)
	}

	// Unwatching stops further notifications.
	if _, err := SetTaskWatching(ctx, 2, taskID, false); err != nil {
		t.Fatalf("unwatch: %v", err)
	}
	if err := SetTaskCompleted(ctx, 1, taskID, false); err != nil {
		t.Fatalf("reopen: %v", err)
	}
	if hasTitlePrefix(notificationTitles(t, 2, taskID), "Reopened") {
		t.Fatalf("unwatched user was notified")
	}
}

func TestProjectWatchCoversAllTasks(t *testing.T) {
	ctx := context.Background()
	pid := sharedProject(t, "Project Watch Proj")
	if _, err := SetProjectWatching(ctx, 2, pid, true); err != nil {
		t.Fatalf("watch project: %v", err)
	}
	if ok, err := IsWatchingProject(ctx, 2, pid); err != nil || !ok {
		t.Fatalf("is watching: %v %v", ok, err)
	}
	taskID := mustCreateTask(t, 1, CreateTaskInput{Title: "Via project", ProjectID: &pid})
	st, err := GetTaskWatchState(ctx, 2, taskID)
	if err != nil || !st.Watching || !st.ViaProject {
		t.Fatalf("project watch state: %+v err=%v", st, err)
	}
	if err := SetTaskCompleted(ctx, 1, taskID, true); err != nil {
		t.Fatalf("complete: %v", err)
	}
	if !hasTitlePrefix(notificationTitles(t, 2, taskID), "Completed: Via project") {
		t.Fatalf("project watcher not notified")
	}
	if _, err := SetProjectWatching(ctx, 3, pid, true); !errors.Is(err, ErrNotFound) {
		t.Fatalf("non-member project watch: err=%v want not found", err)
	}
}

func TestCommentAutoWatches(t *testing.T) {
	ctx := context.Background()
	pid := sharedProject(t, "Comment Watch Proj")
	taskID := mustCreateTask(t, 1, CreateTaskInput{Title: "Discuss", ProjectID: &pid})
	if _, err := AddCommentForUser(ctx, 2, taskID, "looking into it"); err != nil {
		t.Fatalf("comment: %v", err)
	}
	if st, err := GetTaskWatchState(ctx, 2, taskID); err != nil || !st.Watching {
		t.Fatalf("commenter should auto-watch: %+v err=%v", st, err)
	}
}

func TestTaskLinksAndBlocking(t *testing.T) {
	ctx := context.Background()
	pid := sharedProject(t, "Links Proj")
	a := mustCreateTask(t, 1, CreateTaskInput{Title: "Design API", ProjectID: &pid})
	b := mustCreateTask(t, 1, CreateTaskInput{Title: "Build client", ProjectID: &pid})
	c := mustCreateTask(t, 1, CreateTaskInput{Title: "Write docs", ProjectID: &pid})
	if _, err := SetTaskWatching(ctx, 2, b, true); err != nil {
		t.Fatalf("watch b: %v", err)
	}

	// b is blocked by a (added from b's side); c is blocked by b (from b's side).
	link, err := AddTaskLink(ctx, 1, b, a, LinkKindBlockedBy)
	if err != nil || link.Kind != LinkKindBlockedBy || link.TaskID != a || link.Title != "Design API" {
		t.Fatalf("add blocked_by: %+v err=%v", link, err)
	}
	if _, err := AddTaskLink(ctx, 1, b, c, LinkKindBlocks); err != nil {
		t.Fatalf("add blocks: %v", err)
	}
	if !hasTitlePrefix(notificationTitles(t, 2, b), "Blocked: Build client") {
		t.Fatalf("watcher of b not told it is blocked")
	}

	// Cycle: c blocks a would close a -> b -> c -> a.
	if _, err := AddTaskLink(ctx, 1, c, a, LinkKindBlocks); !errors.Is(err, ErrConflict) {
		t.Fatalf("cycle: err=%v want conflict", err)
	}
	// Duplicate link.
	if _, err := AddTaskLink(ctx, 1, a, b, LinkKindBlocks); !errors.Is(err, ErrConflict) {
		t.Fatalf("duplicate: err=%v want conflict", err)
	}
	// Relates is symmetric and stored once.
	if _, err := AddTaskLink(ctx, 1, c, a, LinkKindRelates); err != nil {
		t.Fatalf("relates: %v", err)
	}
	if _, err := AddTaskLink(ctx, 1, a, c, LinkKindRelates); !errors.Is(err, ErrConflict) {
		t.Fatalf("reverse relates should be a duplicate: err=%v", err)
	}
	if _, err := AddTaskLink(ctx, 1, a, a, LinkKindRelates); !errors.Is(err, ErrValidation) {
		t.Fatalf("self link: err=%v", err)
	}
	if _, err := AddTaskLink(ctx, 1, a, b, "parent_of"); !errors.Is(err, ErrValidation) {
		t.Fatalf("bad kind: err=%v", err)
	}

	aLinks, err := ListTaskLinks(ctx, 1, a)
	if err != nil {
		t.Fatalf("list a: %v", err)
	}
	kinds := map[string]int{}
	for _, l := range aLinks {
		kinds[l.Kind] = l.TaskID
	}
	if kinds[LinkKindBlocks] != b || kinds[LinkKindRelates] != c || len(aLinks) != 2 {
		t.Fatalf("links from a's side: %+v", aLinks)
	}

	counts, err := storage.OpenBlockerCounts([]int{b, c})
	if err != nil || counts[b] != 1 || counts[c] != 1 {
		t.Fatalf("open blockers: %v err=%v", counts, err)
	}
	// Completing a unblocks b and notifies b's watcher.
	if err := SetTaskCompleted(ctx, 1, a, true); err != nil {
		t.Fatalf("complete a: %v", err)
	}
	counts, _ = storage.OpenBlockerCounts([]int{b})
	if counts[b] != 0 {
		t.Fatalf("b should be unblocked, counts=%v", counts)
	}
	if !hasTitlePrefix(notificationTitles(t, 2, b), "Unblocked: Build client") {
		t.Fatalf("watcher of b not told it is unblocked: %v", notificationTitles(t, 2, b))
	}

	// Removing a link: must touch the task in the URL.
	if err := RemoveTaskLink(ctx, 1, c, link.LinkID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("remove via unrelated task: err=%v", err)
	}
	if err := RemoveTaskLink(ctx, 1, a, link.LinkID); err != nil {
		t.Fatalf("remove: %v", err)
	}
	bLinks, _ := ListTaskLinks(ctx, 1, b)
	if len(bLinks) != 1 || bLinks[0].Kind != LinkKindBlocks {
		t.Fatalf("b links after removal: %+v", bLinks)
	}
	if ev := eventTypes(t, b); ev["link_added"] != 2 || ev["link_removed"] != 1 {
		t.Fatalf("link events on b: %v", ev)
	}
}

func TestTaskLinkPermissions(t *testing.T) {
	ctx := context.Background()
	pid := sharedProject(t, "Link Perm Proj")
	shared := mustCreateTask(t, 1, CreateTaskInput{Title: "Shared", ProjectID: &pid})
	private := mustCreateTask(t, 1, CreateTaskInput{Title: "Owner inbox"})
	// User 2 cannot see user 1's inbox task.
	if _, err := AddTaskLink(ctx, 2, shared, private, LinkKindRelates); !errors.Is(err, ErrValidation) {
		t.Fatalf("link to invisible task: err=%v want validation", err)
	}
	// Owner links them; user 2 must not see the hidden side.
	if _, err := AddTaskLink(ctx, 1, shared, private, LinkKindRelates); err != nil {
		t.Fatalf("owner link: %v", err)
	}
	links, err := ListTaskLinks(ctx, 2, shared)
	if err != nil || len(links) != 0 {
		t.Fatalf("hidden link leaked: %+v err=%v", links, err)
	}
}
