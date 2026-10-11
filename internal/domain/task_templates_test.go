package domain

import (
	"context"
	"errors"
	"testing"
	"time"

	"GoTodo/internal/storage"
	"GoTodo/internal/tasks"
)

func TestTaskTemplatesApplyAndAccess(t *testing.T) {
	ctx := context.Background()
	proj, err := CreateProject(ctx, 1, "Template Project", "")
	if err != nil {
		t.Fatal(err)
	}
	pid := proj.ID
	if err := upsertTestMember(t, pid, 2, testRoleEditor); err != nil {
		t.Fatal(err)
	}
	bug, err := storage.GetOrCreateTagByName(1, &pid, "Bug")
	if err != nil {
		t.Fatal(err)
	}

	three, offset := 3, 2
	personal, err := CreateTaskTemplateForUser(ctx, 1, nil, TaskTemplateInput{
		Name: "  Bug report ", Title: "Investigate bug", Description: "Steps:",
		Priority: 3, TagNames: []string{"bug", "BUG", "missing-tag", "archived"},
		EstimatePoints: &three, DueOffsetDays: &offset,
		Subtasks: []string{"Reproduce", " ", "Fix", "Add test"},
	})
	if err != nil {
		t.Fatalf("create personal: %v", err)
	}
	if personal.Name != "Bug report" || len(personal.TagNames) != 2 || len(personal.Subtasks) != 3 {
		t.Fatalf("template not normalized: %+v", personal.TaskTemplate)
	}

	// Applied into a project, the tag name resolves to that project's tag.
	taskID, err := ApplyTaskTemplateForUser(ctx, 1, personal.ID, ApplyTaskTemplateInput{ProjectID: &pid})
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	got, err := tasks.FetchTaskByIDForUser(taskID, 1, "UTC", 1)
	if err != nil {
		t.Fatal(err)
	}
	wantDue := time.Now().In(mustLoc(t, storage.UserTimezone(1))).AddDate(0, 0, 2).Format("2006-01-02")
	if got.Title != "Investigate bug" || got.Priority != 3 || got.DueDate != wantDue || got.ProjectID != pid {
		t.Fatalf("applied task = %+v (want due %s)", got, wantDue)
	}
	if len(got.Tags) != 1 || got.Tags[0].ID != bug.ID {
		t.Fatalf("tags = %+v, want the project's Bug tag", got.Tags)
	}
	if got.ChildCount != 3 {
		t.Fatalf("subtasks = %d, want 3", got.ChildCount)
	}

	// Personal templates are private; project templates are shared with members.
	if _, err := ListTaskTemplatesForUser(ctx, 2, 0); err != nil {
		t.Fatal(err)
	}
	if err := DeleteTaskTemplateForUser(ctx, 2, personal.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("other user deleting a personal template: %v", err)
	}
	shared, err := CreateTaskTemplateForUser(ctx, 1, &pid, TaskTemplateInput{Name: "Release", Title: "Cut release"})
	if err != nil {
		t.Fatal(err)
	}
	list, err := ListTaskTemplatesForUser(ctx, 2, pid)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 || list[0].ID != shared.ID || list[0].CanManage {
		t.Fatalf("editor's view of project templates = %+v", list)
	}
	if err := DeleteTaskTemplateForUser(ctx, 2, shared.ID); !errors.Is(err, ErrForbidden) {
		t.Fatalf("editor deleting the owner's project template: %v", err)
	}
	if _, err := ApplyTaskTemplateForUser(ctx, 2, shared.ID, ApplyTaskTemplateInput{}); err != nil {
		t.Fatalf("editor applying a project template: %v", err)
	}
	if _, err := ListTaskTemplatesForUser(ctx, 3, pid); !errors.Is(err, ErrNotFound) {
		t.Fatalf("non-member listing project templates: %v", err)
	}
	if err := DeleteTaskTemplateForUser(ctx, 1, shared.ID); err != nil {
		t.Fatal(err)
	}

	if _, err := CreateTaskTemplateForUser(ctx, 1, nil, TaskTemplateInput{Name: "x", Title: ""}); !errors.Is(err, ErrValidation) {
		t.Fatalf("empty title: %v", err)
	}
}

func mustLoc(t *testing.T, name string) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation(name)
	if err != nil {
		return time.UTC
	}
	return loc
}
