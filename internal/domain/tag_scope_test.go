package domain

import (
	"context"
	"errors"
	"strings"
	"testing"

	"GoTodo/internal/storage"
)

func TestProjectTagsAreSharedAndScoped(t *testing.T) {
	ctx := context.Background()
	proj, err := CreateProject(ctx, 1, "Tag Scope Proj", "")
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	if err := storage.UpsertProjectMember(proj.ID, 2, storage.RoleEditor); err != nil {
		t.Fatalf("add editor: %v", err)
	}
	if err := storage.UpsertProjectMember(proj.ID, 3, storage.RoleViewer); err != nil {
		t.Fatalf("add viewer: %v", err)
	}

	pid := proj.ID
	ownerTag, err := CreateTag(ctx, 1, "urgent", &pid)
	if err != nil {
		t.Fatalf("owner create: %v", err)
	}
	if ownerTag.ProjectID == nil || *ownerTag.ProjectID != pid {
		t.Fatalf("expected project tag, got %+v", ownerTag)
	}

	editorTag, err := CreateTag(ctx, 2, "urgent", &pid)
	if err != nil {
		t.Fatalf("editor get-or-create: %v", err)
	}
	if editorTag.ID != ownerTag.ID {
		t.Fatalf("editor should reuse project tag %d, got %d", ownerTag.ID, editorTag.ID)
	}

	_, err = CreateTag(ctx, 3, "blocked", &pid)
	if !errors.Is(err, ErrForbidden) {
		t.Fatalf("viewer create: err=%v want forbidden", err)
	}

	listed, err := ListTags(ctx, 3, &pid)
	if err != nil {
		t.Fatalf("viewer list: %v", err)
	}
	if !containsTagID(listed, ownerTag.ID) || !containsArchivedTag(listed) {
		t.Fatalf("viewer should see project tags including archived, got %+v", listed)
	}

	if err := DeleteTag(ctx, 3, ownerTag.ID); !errors.Is(err, ErrForbidden) {
		t.Fatalf("viewer delete: err=%v want forbidden", err)
	}

	personal, err := CreateTag(ctx, 2, "urgent", nil)
	if err != nil {
		t.Fatalf("editor personal tag: %v", err)
	}
	if personal.ProjectID != nil {
		t.Fatalf("personal tag should have nil project_id")
	}
	if personal.ID == ownerTag.ID {
		t.Fatal("personal and project tags must be distinct")
	}
}

func TestSetTaskTagsRejectsCrossNamespace(t *testing.T) {
	ctx := context.Background()
	proj, err := CreateProject(ctx, 1, "Tag Assign Proj", "")
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	pid := proj.ID
	projectTag, err := CreateTag(ctx, 1, "proj-only", &pid)
	if err != nil {
		t.Fatalf("project tag: %v", err)
	}
	personalTag, err := CreateTag(ctx, 1, "inbox-only", nil)
	if err != nil {
		t.Fatalf("personal tag: %v", err)
	}

	inboxID, err := CreateTask(ctx, 1, CreateTaskInput{Title: "Inbox tagged", TagIDs: []int{personalTag.ID}})
	if err != nil {
		t.Fatalf("inbox task: %v", err)
	}
	if err := storage.SetTaskTags(inboxID, 1, []int{projectTag.ID}); err == nil {
		t.Fatal("expected invalid tag selection for project tag on inbox task")
	}

	projTaskID, err := CreateTask(ctx, 1, CreateTaskInput{Title: "Project tagged", ProjectID: &pid, TagIDs: []int{projectTag.ID}})
	if err != nil {
		t.Fatalf("project task: %v", err)
	}
	if err := storage.SetTaskTags(projTaskID, 1, []int{personalTag.ID}); err == nil {
		t.Fatal("expected invalid tag selection for personal tag on project task")
	}
}

func TestMoveTaskRemapsTagsByName(t *testing.T) {
	ctx := context.Background()
	src, err := CreateProject(ctx, 1, "Tag Move Src", "")
	if err != nil {
		t.Fatalf("src project: %v", err)
	}
	dst, err := CreateProject(ctx, 1, "Tag Move Dst", "")
	if err != nil {
		t.Fatalf("dst project: %v", err)
	}
	srcID, dstID := src.ID, dst.ID
	srcTag, err := CreateTag(ctx, 1, "shared-name", &srcID)
	if err != nil {
		t.Fatalf("src tag: %v", err)
	}
	dstTag, err := CreateTag(ctx, 1, "shared-name", &dstID)
	if err != nil {
		t.Fatalf("dst tag: %v", err)
	}
	onlySrc, err := CreateTag(ctx, 1, "src-only", &srcID)
	if err != nil {
		t.Fatalf("src-only tag: %v", err)
	}

	taskID, err := CreateTask(ctx, 1, CreateTaskInput{
		Title:     "Moving",
		ProjectID: &srcID,
		TagIDs:    []int{srcTag.ID, onlySrc.ID},
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	dstPtr := &dstID
	if _, err := UpdateTask(ctx, 1, taskID, UpdateTaskInput{ProjectID: &dstPtr}); err != nil {
		t.Fatalf("move: %v", err)
	}
	got, err := storage.GetTagsForTask(taskID)
	if err != nil {
		t.Fatalf("get tags: %v", err)
	}
	if len(got) != 1 || got[0].ID != dstTag.ID {
		t.Fatalf("after move want dest shared-name tag %d, got %+v", dstTag.ID, got)
	}
}

func TestSameTagNameAllowedAcrossProjectsAndInbox(t *testing.T) {
	ctx := context.Background()
	a, err := CreateProject(ctx, 1, "Tag Dup A", "")
	if err != nil {
		t.Fatalf("project a: %v", err)
	}
	b, err := CreateProject(ctx, 1, "Tag Dup B", "")
	if err != nil {
		t.Fatalf("project b: %v", err)
	}
	aID, bID := a.ID, b.ID

	aTag, err := CreateTag(ctx, 1, "shared-across", &aID)
	if err != nil {
		t.Fatalf("project a tag: %v", err)
	}
	bTag, err := CreateTag(ctx, 1, "shared-across", &bID)
	if err != nil {
		t.Fatalf("project b tag: %v", err)
	}
	inbox, err := CreateTag(ctx, 1, "shared-across", nil)
	if err != nil {
		t.Fatalf("inbox tag: %v", err)
	}

	if aTag.ID == bTag.ID || aTag.ID == inbox.ID || bTag.ID == inbox.ID {
		t.Fatalf("expected distinct tags, got a=%d b=%d inbox=%d", aTag.ID, bTag.ID, inbox.ID)
	}
	if aTag.ProjectID == nil || *aTag.ProjectID != aID {
		t.Fatalf("project a tag scope: %+v", aTag)
	}
	if bTag.ProjectID == nil || *bTag.ProjectID != bID {
		t.Fatalf("project b tag scope: %+v", bTag)
	}
	if inbox.ProjectID != nil {
		t.Fatalf("inbox tag should be personal, got %+v", inbox)
	}
}

func TestListTagsQueryScopes(t *testing.T) {
	ctx := context.Background()
	proj, err := CreateProject(ctx, 1, "Tag List Proj", "")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	pid := proj.ID
	if _, err := CreateTag(ctx, 1, "personal-a", nil); err != nil {
		t.Fatalf("personal: %v", err)
	}
	if _, err := CreateTag(ctx, 1, "project-a", &pid); err != nil {
		t.Fatalf("project: %v", err)
	}

	zero := 0
	personal, err := ListTags(ctx, 1, &zero)
	if err != nil {
		t.Fatalf("personal list: %v", err)
	}
	for _, tg := range personal {
		if tg.ProjectID != nil {
			t.Fatalf("personal list included project tag %+v", tg)
		}
	}

	projectTags, err := ListTags(ctx, 1, &pid)
	if err != nil {
		t.Fatalf("project list: %v", err)
	}
	if !containsTagName(projectTags, "project-a") || !containsArchivedTag(projectTags) {
		t.Fatalf("project list: %+v", projectTags)
	}

	all, err := ListTags(ctx, 1, nil)
	if err != nil {
		t.Fatalf("all list: %v", err)
	}
	if len(all) < 2 {
		t.Fatalf("all list should include personal and project tags, got %d", len(all))
	}
}

func TestMigrateTagsSplitsLegacySharedTag(t *testing.T) {
	personal, err := storage.FindTagByName(1, nil, "legacy-migrate-clone")
	if err != nil {
		t.Fatalf("personal tag after migrate: %v", err)
	}
	projID := 9001
	project, err := storage.FindTagByName(1, &projID, "legacy-migrate-clone")
	if err != nil {
		t.Fatalf("project tag after migrate: %v", err)
	}
	if personal.ID == project.ID {
		t.Fatal("legacy shared tag should be cloned into the project namespace")
	}

	inboxTags, err := storage.GetTagsForTask(9001)
	if err != nil {
		t.Fatalf("inbox tags: %v", err)
	}
	if len(inboxTags) != 1 || inboxTags[0].ID != personal.ID {
		t.Fatalf("inbox task should keep personal tag, got %+v", inboxTags)
	}

	projTags, err := storage.GetTagsForTask(9002)
	if err != nil {
		t.Fatalf("project tags: %v", err)
	}
	if len(projTags) != 1 || projTags[0].ID != project.ID {
		t.Fatalf("project task should use cloned tag, got %+v", projTags)
	}
}

func TestUpdateTaskLogsTagAddedAndRemoved(t *testing.T) {
	ctx := context.Background()
	proj, err := CreateProject(ctx, 1, "Tag Activity Proj", "")
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	pid := proj.ID
	alpha, err := CreateTag(ctx, 1, "alpha", &pid)
	if err != nil {
		t.Fatalf("alpha tag: %v", err)
	}
	bravo, err := CreateTag(ctx, 1, "bravo", &pid)
	if err != nil {
		t.Fatalf("bravo tag: %v", err)
	}

	taskID, err := CreateTask(ctx, 1, CreateTaskInput{Title: "Tag activity", ProjectID: &pid})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if n := len(eventsOfType(t, taskID, 1, "tag_added")); n != 0 {
		t.Fatalf("create tag_added count=%d want 0", n)
	}

	ids := []int{alpha.ID}
	if _, err := UpdateTask(ctx, 1, taskID, UpdateTaskInput{TagIDs: &ids}); err != nil {
		t.Fatalf("add tag: %v", err)
	}
	added := eventsOfType(t, taskID, 1, "tag_added")
	if len(added) != 1 {
		t.Fatalf("tag_added count=%d want 1", len(added))
	}
	if added[0].Metadata["tag"] != "alpha" {
		t.Errorf("tag_added name=%v want alpha", added[0].Metadata["tag"])
	}
	if eventTagID(added[0]) != alpha.ID {
		t.Errorf("tag_added tag_id=%d want %d", eventTagID(added[0]), alpha.ID)
	}
	if n := len(eventsOfType(t, taskID, 1, "tag_removed")); n != 0 {
		t.Fatalf("tag_removed count=%d want 0 after add", n)
	}

	if _, err := UpdateTask(ctx, 1, taskID, UpdateTaskInput{TagIDs: &ids}); err != nil {
		t.Fatalf("same tags: %v", err)
	}
	if got := eventsOfType(t, taskID, 1, "tag_added"); len(got) != 1 {
		t.Fatalf("after no-op tag_added count=%d want 1", len(got))
	}
	if got := eventsOfType(t, taskID, 1, "tag_removed"); len(got) != 0 {
		t.Fatalf("after no-op tag_removed count=%d want 0", len(got))
	}

	swap := []int{bravo.ID}
	if _, err := UpdateTask(ctx, 1, taskID, UpdateTaskInput{TagIDs: &swap}); err != nil {
		t.Fatalf("swap tags: %v", err)
	}
	added = eventsOfType(t, taskID, 1, "tag_added")
	removed := eventsOfType(t, taskID, 1, "tag_removed")
	if len(added) != 2 {
		t.Fatalf("after swap tag_added count=%d want 2", len(added))
	}
	if added[0].Metadata["tag"] != "bravo" {
		t.Errorf("newest tag_added=%v want bravo", added[0].Metadata["tag"])
	}
	if eventTagID(added[0]) != bravo.ID {
		t.Errorf("newest tag_added tag_id=%d want %d", eventTagID(added[0]), bravo.ID)
	}
	if len(removed) != 1 {
		t.Fatalf("after swap tag_removed count=%d want 1", len(removed))
	}
	if removed[0].Metadata["tag"] != "alpha" {
		t.Errorf("tag_removed name=%v want alpha", removed[0].Metadata["tag"])
	}
	if eventTagID(removed[0]) != alpha.ID {
		t.Errorf("tag_removed tag_id=%d want %d", eventTagID(removed[0]), alpha.ID)
	}

	empty := []int{}
	if _, err := UpdateTask(ctx, 1, taskID, UpdateTaskInput{TagIDs: &empty}); err != nil {
		t.Fatalf("remove tags: %v", err)
	}
	removed = eventsOfType(t, taskID, 1, "tag_removed")
	if len(removed) != 2 {
		t.Fatalf("after clear tag_removed count=%d want 2", len(removed))
	}
	if removed[0].Metadata["tag"] != "bravo" {
		t.Errorf("newest tag_removed=%v want bravo", removed[0].Metadata["tag"])
	}
	if eventTagID(removed[0]) != bravo.ID {
		t.Errorf("newest tag_removed tag_id=%d want %d", eventTagID(removed[0]), bravo.ID)
	}
}

func eventTagID(ev storage.TaskEvent) int {
	switch v := ev.Metadata["tag_id"].(type) {
	case float64:
		return int(v)
	case int:
		return v
	case int64:
		return int(v)
	default:
		return 0
	}
}

func TestTagShareLinksRejected(t *testing.T) {
	ctx := context.Background()
	_, err := CreateShareLinkForScope(ctx, 1, "tag", 1, nil)
	if !errors.Is(err, ErrValidation) {
		t.Fatalf("create tag share: err=%v want validation", err)
	}
	_, err = ListShareLinksForUser(ctx, 1, "tag", 1)
	if !errors.Is(err, ErrValidation) {
		t.Fatalf("list tag share: err=%v want validation", err)
	}
}

func TestUpdateTagRecolorPreservesNameAndRenamePreservesColor(t *testing.T) {
	ctx := context.Background()
	proj, err := CreateProject(ctx, 1, "Tag Recolor Proj", "")
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	pid := proj.ID
	tag, err := CreateTag(ctx, 1, "paintable", &pid)
	if err != nil {
		t.Fatalf("create tag: %v", err)
	}
	if tag.Color == "" {
		t.Fatal("expected created tag to have a color")
	}

	newColor := "#dc3545"
	if strings.EqualFold(tag.Color, newColor) {
		newColor = "#0d6efd"
	}
	recolored, err := UpdateTag(ctx, 1, tag.ID, nil, &newColor)
	if err != nil {
		t.Fatalf("recolor: %v", err)
	}
	if recolored.Name != tag.Name {
		t.Fatalf("recolor changed name: got %q want %q", recolored.Name, tag.Name)
	}
	if !strings.EqualFold(recolored.Color, newColor) {
		t.Fatalf("color=%q want %q", recolored.Color, newColor)
	}

	renamed, err := RenameTag(ctx, 1, tag.ID, "painted")
	if err != nil {
		t.Fatalf("rename: %v", err)
	}
	if renamed.Name != "painted" {
		t.Fatalf("name=%q want painted", renamed.Name)
	}
	if !strings.EqualFold(renamed.Color, newColor) {
		t.Fatalf("rename wiped color: got %q want %q", renamed.Color, newColor)
	}

	if err := storage.UpsertProjectMember(pid, 3, storage.RoleViewer); err != nil {
		t.Fatalf("add viewer: %v", err)
	}
	_, err = UpdateTag(ctx, 3, tag.ID, nil, &newColor)
	if !errors.Is(err, ErrForbidden) {
		t.Fatalf("viewer recolor: err=%v want forbidden", err)
	}
}

func containsTagID(tags []storage.Tag, id int) bool {
	for _, tg := range tags {
		if tg.ID == id {
			return true
		}
	}
	return false
}

func containsTagName(tags []storage.Tag, name string) bool {
	for _, tg := range tags {
		if strings.EqualFold(tg.Name, name) {
			return true
		}
	}
	return false
}

func containsArchivedTag(tags []storage.Tag) bool {
	for _, tg := range tags {
		if storage.IsArchivedTagName(tg.Name) && tg.Protected {
			return true
		}
	}
	return false
}
