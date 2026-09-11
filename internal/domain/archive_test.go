package domain

import (
	"context"
	"errors"
	"strings"
	"testing"

	"GoTodo/internal/storage"
	"GoTodo/internal/tasks"
)

func TestRemovedAndArchivedTagsAreProtected(t *testing.T) {
	ctx := context.Background()
	proj, err := CreateProject(ctx, 1, "Archive Protect Proj", "")
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	pid := proj.ID

	if _, err := CreateTag(ctx, 1, "removed", &pid); !errors.Is(err, ErrValidation) {
		t.Fatalf("create reserved removed: err=%v want validation", err)
	}
	if _, err := CreateTag(ctx, 1, "archived", &pid); !errors.Is(err, ErrValidation) {
		t.Fatalf("create reserved archived: err=%v want validation", err)
	}

	tags, err := ListTags(ctx, 1, &pid)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	var archived *storage.Tag
	for i := range tags {
		if storage.IsArchivedTagName(tags[i].Name) {
			archived = &tags[i]
			break
		}
	}
	if archived == nil || !archived.Protected {
		t.Fatalf("expected protected archived tag, got %+v", tags)
	}
	if _, err := RenameTag(ctx, 1, archived.ID, "gone"); !errors.Is(err, ErrValidation) {
		t.Fatalf("rename protected: err=%v want validation", err)
	}
	color := "#dc3545"
	if _, err := UpdateTag(ctx, 1, archived.ID, nil, &color); !errors.Is(err, ErrValidation) {
		t.Fatalf("recolor protected: err=%v want validation", err)
	}
	if err := DeleteTag(ctx, 1, archived.ID); !errors.Is(err, ErrValidation) {
		t.Fatalf("delete protected: err=%v want validation", err)
	}

	custom, err := CreateTag(ctx, 1, "custom-test-tag", &pid)
	if err != nil {
		t.Fatalf("create custom tag: %v", err)
	}
	if _, err := RenameTag(ctx, 1, custom.ID, "archived"); !errors.Is(err, ErrValidation) {
		t.Fatalf("rename custom to archived: err=%v want validation", err)
	}
	if _, err := RenameTag(ctx, 1, custom.ID, "removed"); !errors.Is(err, ErrValidation) {
		t.Fatalf("rename custom to removed: err=%v want validation", err)
	}
}

func TestCannotManuallyAssignArchivedOrRemovedTag(t *testing.T) {
	ctx := context.Background()
	proj, err := CreateProject(ctx, 1, "Archive Assign Proj", "")
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	pid := proj.ID
	tags, err := ListTags(ctx, 1, &pid)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	var archivedID int
	for _, tg := range tags {
		if storage.IsArchivedTagName(tg.Name) {
			archivedID = tg.ID
			break
		}
	}
	if archivedID == 0 {
		t.Fatal("missing archived tag")
	}
	taskID, err := CreateTask(ctx, 1, CreateTaskInput{Title: "Assign archive", ProjectID: &pid, TagIDs: []int{archivedID}})
	if !errors.Is(err, ErrValidation) {
		t.Fatalf("create with archived tag_ids: err=%v want validation", err)
	}
	_ = taskID
	if _, err := storage.GetOrCreateTagByName(1, &pid, "removed"); err == nil {
		t.Fatal("GetOrCreateTagByName should reject removed name")
	}
	if _, err := storage.GetOrCreateTagByName(1, &pid, "archived"); err == nil {
		t.Fatal("GetOrCreateTagByName should reject archived name")
	}
}

func TestArchiveRestoreAndListFilter(t *testing.T) {
	ctx := context.Background()
	proj, err := CreateProject(ctx, 1, "Archive List Proj", "")
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	pid := proj.ID
	work, err := CreateTag(ctx, 1, "work", &pid)
	if err != nil {
		t.Fatalf("work tag: %v", err)
	}
	taskID, err := CreateTask(ctx, 1, CreateTaskInput{Title: "Active then archive", ProjectID: &pid, TagIDs: []int{work.ID}})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	uid := 1
	listed, total, err := tasks.ReturnPaginationForUserWithFilters(1, 50, &uid, "UTC", tasks.ListFilters{ProjectFilter: &pid})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if !containsTaskID(listed, taskID) {
		t.Fatalf("active task missing from default list, total=%d", total)
	}

	if err := ArchiveTask(ctx, 1, taskID); err != nil {
		t.Fatalf("archive: %v", err)
	}
	listed, totalAfterArchive, err := tasks.ReturnPaginationForUserWithFilters(1, 50, &uid, "UTC", tasks.ListFilters{ProjectFilter: &pid})
	if err != nil {
		t.Fatalf("list after archive: %v", err)
	}
	if totalAfterArchive != 0 {
		t.Fatalf("expected total 0 after archive, got %d", totalAfterArchive)
	}
	if containsTaskID(listed, taskID) {
		t.Fatal("archived task should be hidden from default list")
	}

	_, searchTotalAfterArchive, err := tasks.SearchTasksForUserWithFilters(1, 50, "Active", &uid, "UTC", tasks.ListFilters{ProjectFilter: &pid})
	if err != nil {
		t.Fatalf("search after archive: %v", err)
	}
	if searchTotalAfterArchive != 0 {
		t.Fatalf("expected search total 0 after archive, got %d", searchTotalAfterArchive)
	}

	archivedTag, err := storage.FindTagByName(1, &pid, storage.ArchivedTagName)
	if err != nil {
		t.Fatalf("find archived tag: %v", err)
	}
	listed, totalByID, err := tasks.ReturnPaginationForUserWithFilters(1, 50, &uid, "UTC", tasks.ListFilters{ProjectFilter: &pid, TagFilter: &archivedTag.ID})
	if err != nil {
		t.Fatalf("list by id: %v", err)
	}
	if totalByID != 1 {
		t.Fatalf("expected total 1 for archived tag by id, got %d", totalByID)
	}
	if !containsTaskID(listed, taskID) {
		t.Fatal("filter by archived id should show archived task")
	}
	listed, totalByName, err := tasks.ReturnPaginationForUserWithFilters(1, 50, &uid, "UTC", tasks.ListFilters{ProjectFilter: &pid, TagNameFilter: "archived"})
	if err != nil {
		t.Fatalf("list by name: %v", err)
	}
	if totalByName != 1 {
		t.Fatalf("expected total 1 for archived tag by name, got %d", totalByName)
	}
	if !containsTaskID(listed, taskID) {
		t.Fatal("filter by archived name should show archived task")
	}

	// Also verify backward compatibility when filtering by "removed"
	listedRemoved, totalByRemovedName, err := tasks.ReturnPaginationForUserWithFilters(1, 50, &uid, "UTC", tasks.ListFilters{ProjectFilter: &pid, TagNameFilter: "removed"})
	if err != nil {
		t.Fatalf("list by removed name: %v", err)
	}
	if totalByRemovedName != 1 || !containsTaskID(listedRemoved, taskID) {
		t.Fatalf("filter by removed name should also show archived task, total=%d", totalByRemovedName)
	}

	_, searchArchivedTotal, err := tasks.SearchTasksForUserWithFilters(1, 50, "Active", &uid, "UTC", tasks.ListFilters{ProjectFilter: &pid, TagNameFilter: "archived"})
	if err != nil {
		t.Fatalf("search by archived tag name: %v", err)
	}
	if searchArchivedTotal != 1 {
		t.Fatalf("expected search total 1 for archived tag name, got %d", searchArchivedTotal)
	}

	got, err := storage.GetTagsForTask(taskID)
	if err != nil {
		t.Fatalf("tags: %v", err)
	}
	if !taskHasArchived(got) {
		t.Fatalf("expected archived tag after archive, got %+v", got)
	}

	ids := []int{work.ID}
	if _, err := UpdateTask(ctx, 1, taskID, UpdateTaskInput{TagIDs: &ids}); err != nil {
		t.Fatalf("update tags: %v", err)
	}
	got, err = storage.GetTagsForTask(taskID)
	if err != nil {
		t.Fatalf("tags after save: %v", err)
	}
	if !taskHasRemoved(got) {
		t.Fatal("saving other tags must keep removed")
	}

	if err := RestoreTask(ctx, 1, taskID); err != nil {
		t.Fatalf("restore: %v", err)
	}
	listed, _, err = tasks.ReturnPaginationForUserWithFilters(1, 50, &uid, "UTC", tasks.ListFilters{ProjectFilter: &pid})
	if err != nil {
		t.Fatalf("list after restore: %v", err)
	}
	if !containsTaskID(listed, taskID) {
		t.Fatal("restored task should reappear")
	}
}

func TestArchiveCascadesToChildren(t *testing.T) {
	ctx := context.Background()
	proj, err := CreateProject(ctx, 1, "Archive Cascade Proj", "")
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	pid := proj.ID
	parentID, err := CreateTask(ctx, 1, CreateTaskInput{Title: "Parent archive", ProjectID: &pid})
	if err != nil {
		t.Fatalf("parent: %v", err)
	}
	childID, err := CreateTask(ctx, 1, CreateTaskInput{Title: "Child archive", ProjectID: &pid, ParentID: &parentID})
	if err != nil {
		t.Fatalf("child: %v", err)
	}
	if err := ArchiveTask(ctx, 1, parentID); err != nil {
		t.Fatalf("archive: %v", err)
	}
	for _, id := range []int{parentID, childID} {
		got, err := storage.GetTagsForTask(id)
		if err != nil {
			t.Fatalf("tags %d: %v", id, err)
		}
		if !taskHasRemoved(got) {
			t.Fatalf("task %d should be archived", id)
		}
	}
	if err := RestoreTask(ctx, 1, parentID); err != nil {
		t.Fatalf("restore: %v", err)
	}
	for _, id := range []int{parentID, childID} {
		got, err := storage.GetTagsForTask(id)
		if err != nil {
			t.Fatalf("tags %d: %v", id, err)
		}
		if taskHasRemoved(got) {
			t.Fatalf("task %d should be restored", id)
		}
	}
}

func TestArchiveForbiddenForViewer(t *testing.T) {
	ctx := context.Background()
	proj, err := CreateProject(ctx, 1, "Archive Viewer Proj", "")
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	pid := proj.ID
	if err := storage.UpsertProjectMember(pid, 3, storage.RoleViewer); err != nil {
		t.Fatalf("add viewer: %v", err)
	}
	if err := storage.UpsertProjectMember(pid, 2, storage.RoleEditor); err != nil {
		t.Fatalf("add editor: %v", err)
	}
	taskID, err := CreateTask(ctx, 1, CreateTaskInput{Title: "Viewer archive", ProjectID: &pid})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := ArchiveTask(ctx, 3, taskID); !errors.Is(err, ErrForbidden) {
		t.Fatalf("viewer archive: err=%v want forbidden", err)
	}
	if err := ArchiveTask(ctx, 2, taskID); err != nil {
		t.Fatalf("editor archive: %v", err)
	}
	if err := RestoreTask(ctx, 3, taskID); !errors.Is(err, ErrForbidden) {
		t.Fatalf("viewer restore: err=%v want forbidden", err)
	}
	if err := RestoreTask(ctx, 2, taskID); err != nil {
		t.Fatalf("editor restore: %v", err)
	}
}

func TestArchiveDoesNotCountAgainstTagLimit(t *testing.T) {
	ctx := context.Background()
	proj, err := CreateProject(ctx, 1, "Archive Max Tags Proj", "")
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	pid := proj.ID
	var ids []int
	for _, name := range []string{"one", "two", "three", "four", "five"} {
		tg, err := CreateTag(ctx, 1, name, &pid)
		if err != nil {
			t.Fatalf("tag %s: %v", name, err)
		}
		ids = append(ids, tg.ID)
	}
	taskID, err := CreateTask(ctx, 1, CreateTaskInput{Title: "Full tags", ProjectID: &pid, TagIDs: ids})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := ArchiveTask(ctx, 1, taskID); err != nil {
		t.Fatalf("archive with 5 user tags: %v", err)
	}
	got, err := storage.GetTagsForTask(taskID)
	if err != nil {
		t.Fatalf("tags: %v", err)
	}
	if len(got) != 6 {
		t.Fatalf("expected 5 user tags + removed, got %d", len(got))
	}
}

func containsTaskID(list []tasks.Task, id int) bool {
	for _, t := range list {
		if t.ID == id {
			return true
		}
		for _, c := range t.Children {
			if c.ID == id {
				return true
			}
		}
	}
	return false
}

func taskHasArchived(tags []storage.Tag) bool {
	for _, tg := range tags {
		if storage.IsArchivedTagName(tg.Name) || storage.IsRemovedTagName(tg.Name) {
			return true
		}
	}
	return false
}

func taskHasRemoved(tags []storage.Tag) bool {
	return taskHasArchived(tags)
}

func TestArchivedAndRemovedTagNameHelpers(t *testing.T) {
	if !storage.IsRemovedTagName("Removed") || storage.IsRemovedTagName("work") {
		t.Fatal("IsRemovedTagName mismatch")
	}
	if !storage.IsArchivedTagName("Archived") || storage.IsArchivedTagName("work") {
		t.Fatal("IsArchivedTagName mismatch")
	}
	if !storage.IsSystemTagName("archived") || !storage.IsSystemTagName("removed") || storage.IsSystemTagName("work") {
		t.Fatal("IsSystemTagName mismatch")
	}
	if !strings.EqualFold(storage.RemovedTagName, "removed") {
		t.Fatalf("canonical name %q", storage.RemovedTagName)
	}
	if !strings.EqualFold(storage.ArchivedTagName, "archived") {
		t.Fatalf("canonical name %q", storage.ArchivedTagName)
	}
}

func TestRemovedTaskCountsAndChildExclusion(t *testing.T) {
	ctx := context.Background()
	proj, err := CreateProject(ctx, 1, "Child Exclusion Proj", "")
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	pid := proj.ID

	if _, err := SetProjectWorkflowMode(ctx, 1, pid, storage.WorkflowKanban); err != nil {
		t.Fatalf("enable kanban: %v", err)
	}

	sprint, err := CreateProjectSprintForUser(ctx, 1, pid, CreateProjectSprintInput{
		Name:      "Sprint With Removed",
		StartDate: "2026-08-24",
		EndDate:   "2026-09-06",
	})
	if err != nil {
		t.Fatalf("create sprint: %v", err)
	}
	sid := sprint.ID

	rootID, err := CreateTask(ctx, 1, CreateTaskInput{Title: "Root Task", ProjectID: &pid})
	if err != nil {
		t.Fatalf("create root: %v", err)
	}

	child1ID, err := CreateTask(ctx, 1, CreateTaskInput{
		Title:     "Active Completed Child",
		ParentID:  &rootID,
		SprintID:  &sid,
		Completed: true,
	})
	if err != nil {
		t.Fatalf("create child 1: %v", err)
	}

	child2ID, err := CreateTask(ctx, 1, CreateTaskInput{
		Title:     "Archive Completed Child",
		ParentID:  &rootID,
		SprintID:  &sid,
		Completed: true,
	})
	if err != nil {
		t.Fatalf("create child 2: %v", err)
	}

	sprints, err := storage.ListProjectSprints(pid)
	if err != nil {
		t.Fatalf("list project sprints: %v", err)
	}
	if len(sprints) == 0 || sprints[0].TaskCount != 2 {
		t.Fatalf("expected sprint task count 2 before archive, got %+v", sprints)
	}

	uid := 1
	listed, _, err := tasks.ReturnPaginationForUserWithFilters(1, 50, &uid, "UTC", tasks.ListFilters{ProjectFilter: &pid})
	if err != nil {
		t.Fatalf("list roots: %v", err)
	}
	var rootFound *tasks.Task
	for i := range listed {
		if listed[i].ID == rootID {
			rootFound = &listed[i]
			break
		}
	}
	if rootFound == nil {
		t.Fatalf("root task not found in list")
	}
	if rootFound.ChildCount != 2 || rootFound.ChildrenCompleted != 2 {
		t.Fatalf("expected child count 2 and completed 2, got (%d, %d)", rootFound.ChildCount, rootFound.ChildrenCompleted)
	}

	// Archive child 2
	if err := ArchiveTask(ctx, 1, child2ID); err != nil {
		t.Fatalf("archive child 2: %v", err)
	}

	// Sprints task count should now exclude the archived child
	sprintsAfter, err := storage.ListProjectSprints(pid)
	if err != nil {
		t.Fatalf("list project sprints after archive: %v", err)
	}
	if len(sprintsAfter) == 0 || sprintsAfter[0].TaskCount != 1 {
		t.Fatalf("expected sprint task count 1 after archive, got %+v", sprintsAfter)
	}

	// Root task should now report child count 1 and completed 1
	listedAfter, _, err := tasks.ReturnPaginationForUserWithFilters(1, 50, &uid, "UTC", tasks.ListFilters{ProjectFilter: &pid})
	if err != nil {
		t.Fatalf("list roots after archive: %v", err)
	}
	rootFound = nil
	for i := range listedAfter {
		if listedAfter[i].ID == rootID {
			rootFound = &listedAfter[i]
			break
		}
	}
	if rootFound == nil {
		t.Fatalf("root task not found in list after archive")
	}
	if rootFound.ChildCount != 1 || rootFound.ChildrenCompleted != 1 {
		t.Fatalf("expected child count 1 and completed 1 after archive, got (%d, %d)", rootFound.ChildCount, rootFound.ChildrenCompleted)
	}

	_ = child1ID
}

