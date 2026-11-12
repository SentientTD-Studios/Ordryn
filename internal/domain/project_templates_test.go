package domain

import (
	"context"
	"errors"
	"testing"

	"GoTodo/internal/storage"
)

func TestCreateProjectFromTemplate(t *testing.T) {
	ctx := context.Background()
	f := kanbanAgentProject(t)
	src := f.projectID

	wip := 3
	wipPtr := &wip
	if _, err := UpdateProjectStatusForUser(ctx, 1, src, f.aiStatus, UpdateProjectStatusInput{WIPLimit: &wipPtr}); err != nil {
		t.Fatalf("wip: %v", err)
	}
	bug := projectTag(t, src, "bug")
	// Remappable: column and tag ids.
	newRule(t, src, "Bugs go to AI queue", AutomationOnClaimed, AutomationTriggerConfig{},
		AutomationConditions{TagsAny: []int{bug}},
		AutomationAction{Type: AutomationActSetStatus, StatusID: f.aiStatus})
	// Not remappable: names a person.
	newRule(t, src, "Assign to owner", AutomationOnClaimed, AutomationTriggerConfig{}, AutomationConditions{},
		AutomationAction{Type: AutomationActAssign, UserID: 1})

	parent := newProjectTask(t, src, "Set up repo")
	if _, err := CreateTask(ctx, 1, CreateTaskInput{Title: "Add CI", ParentID: &parent, TagIDs: []int{bug}}); err != nil {
		t.Fatal(err)
	}

	if _, err := CreateProjectFromTemplate(ctx, 1, FromTemplateInput{TemplateID: src, Name: "Nope"}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("not a template yet: %v", err)
	}
	if err := SetProjectTemplateForUser(ctx, 2, src, true); !errors.Is(err, ErrForbidden) {
		t.Fatalf("editor can't mark templates: %v", err)
	}
	if err := SetProjectTemplateForUser(ctx, 1, src, true); err != nil {
		t.Fatal(err)
	}
	list, err := ListProjectTemplatesForUser(ctx, 1)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, tpl := range list {
		found = found || tpl.ID == src
	}
	if !found {
		t.Fatal("template not listed")
	}

	res, err := CreateProjectFromTemplate(ctx, 1, FromTemplateInput{TemplateID: src, Name: "From template " + uniqueHandle("x"), IncludeTasks: true})
	if err != nil {
		t.Fatal(err)
	}
	dst := res.Project.ID
	if res.TasksCopied != 2 || res.RulesCopied != 1 || res.RulesSkipped != 1 {
		t.Fatalf("result: %+v", res)
	}

	srcStatuses, _ := storage.ListProjectStatuses(src)
	dstStatuses, _ := storage.ListProjectStatuses(dst)
	if len(srcStatuses) != len(dstStatuses) {
		t.Fatalf("columns: %d vs %d", len(srcStatuses), len(dstStatuses))
	}
	for i := range srcStatuses {
		s, d := srcStatuses[i], dstStatuses[i]
		if s.Name != d.Name || s.IsDone != d.IsDone {
			t.Fatalf("column %d: %+v vs %+v", i, s, d)
		}
		if s.ID == f.aiStatus && (d.WIPLimit == nil || *d.WIPLimit != 3) {
			t.Fatalf("WIP limit not copied: %+v", d.WIPLimit)
		}
	}

	rules, err := ListAutomationRules(1, dst)
	if err != nil || len(rules) != 1 {
		t.Fatalf("rules: %v %v", rules, err)
	}
	if string(rules[0].Conditions) == "" {
		t.Fatal("conditions lost")
	}
	tags, _ := ListTags(ctx, 1, &dst)
	hasBug := false
	for _, tg := range tags {
		hasBug = hasBug || tg.Name == "bug"
	}
	if !hasBug {
		t.Fatal("tag not copied")
	}
	if storage.ProjectIsTemplate(dst) {
		t.Fatal("the new project shouldn't be a template")
	}
}
