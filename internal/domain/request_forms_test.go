package domain

import (
	"context"
	"errors"
	"strings"
	"testing"

	"GoTodo/internal/storage"
)

func TestRequestFormLifecycleAndSubmit(t *testing.T) {
	ctx := context.Background()
	proj, err := CreateProject(ctx, 1, "Requests", "")
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	if err := upsertTestMember(t, proj.ID, 2, testRoleEditor); err != nil {
		t.Fatal(err)
	}

	in := RequestFormInput{
		Title: "IT help",
		Intro: "Tell us what's broken.",
		Fields: []storage.RequestFormField{
			{ID: "kind", Label: "Kind", Type: storage.FormFieldSelect, Required: true, Options: []string{"Laptop", "Access", " Laptop "}},
			{ID: "when", Label: "Needed by", Type: storage.FormFieldDate},
			{Label: "Details", Type: storage.FormFieldTextarea},
		},
		Priority: 2,
		Enabled:  true,
	}

	// Only managers can create forms.
	if _, err := CreateRequestForm(ctx, 2, proj.ID, in); !errors.Is(err, ErrForbidden) {
		t.Fatalf("editor create err=%v, want forbidden", err)
	}
	f, err := CreateRequestForm(ctx, 1, proj.ID, in)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if len(f.Fields) != 3 || f.Fields[2].ID != "q3" || len(f.Fields[0].Options) != 2 {
		t.Fatalf("fields not normalized: %+v", f.Fields)
	}
	if f.Token == "" || f.OwnerID != 1 {
		t.Fatalf("token/owner: %+v", f)
	}

	// User 3 isn't on the project but can open and submit with the link.
	pub, err := GetRequestFormForSubmitter(ctx, 3, f.Token)
	if err != nil {
		t.Fatalf("open form: %v", err)
	}
	if pub.ProjectName != "Requests" || len(pub.Fields) != 3 {
		t.Fatalf("public form: %+v", pub)
	}
	if _, err := SubmitRequestForm(ctx, 3, f.Token, "Laptop won't boot", map[string]string{}); !errors.Is(err, ErrValidation) {
		t.Fatalf("missing required err=%v", err)
	}
	if _, err := SubmitRequestForm(ctx, 3, f.Token, "x", map[string]string{"kind": "Printer"}); !errors.Is(err, ErrValidation) {
		t.Fatalf("bad option err=%v", err)
	}
	if _, err := SubmitRequestForm(ctx, 3, f.Token, "x", map[string]string{"kind": "Access", "when": "soon"}); !errors.Is(err, ErrValidation) {
		t.Fatalf("bad date err=%v", err)
	}
	if _, err := SubmitRequestForm(ctx, 3, f.Token, "  ", map[string]string{"kind": "Access"}); !errors.Is(err, ErrValidation) {
		t.Fatalf("empty summary err=%v", err)
	}
	taskID, err := SubmitRequestForm(ctx, 3, f.Token, "Laptop won't boot", map[string]string{
		"kind": "Laptop", "when": "2031-01-02", "q3": "Black screen *after* login",
	})
	if err != nil {
		t.Fatalf("submit: %v", err)
	}
	td, err := storage.GetTaskTitleDescription(taskID)
	if err != nil {
		t.Fatal(err)
	}
	if td.Title != "Laptop won't boot" {
		t.Fatalf("title=%q", td.Title)
	}
	for _, want := range []string{"**Kind**\nLaptop", "**Needed by**\n2031-01-02", "Black screen *after* login", "via the **IT help** form"} {
		if !strings.Contains(td.Description, want) {
			t.Errorf("description missing %q:\n%s", want, td.Description)
		}
	}
	if strings.Contains(td.Description, "@example.com") {
		t.Fatalf("description leaks an email:\n%s", td.Description)
	}
	if pid, _ := storage.GetTaskProjectID(taskID); pid != proj.ID {
		t.Fatalf("task project=%d want %d", pid, proj.ID)
	}

	// Turning the form off closes it.
	in.Enabled = false
	if _, err := UpdateRequestForm(ctx, 1, proj.ID, f.ID, in); err != nil {
		t.Fatalf("update: %v", err)
	}
	if _, err := GetRequestFormForSubmitter(ctx, 3, f.Token); !errors.Is(err, ErrValidation) {
		t.Fatalf("closed form err=%v, want validation", err)
	}

	// Rotating the link retires the old token.
	in.Enabled = true
	if _, err := UpdateRequestForm(ctx, 1, proj.ID, f.ID, in); err != nil {
		t.Fatal(err)
	}
	rotated, err := RotateRequestFormLink(ctx, 1, proj.ID, f.ID)
	if err != nil {
		t.Fatalf("rotate: %v", err)
	}
	if _, err := GetRequestFormForSubmitter(ctx, 3, f.Token); !errors.Is(err, ErrNotFound) {
		t.Fatalf("old token err=%v, want not found", err)
	}
	if rotated.Submissions != 1 {
		t.Fatalf("submissions=%d want 1", rotated.Submissions)
	}

	if err := DeleteRequestFormForUser(ctx, 1, proj.ID, f.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := GetRequestFormForSubmitter(ctx, 3, rotated.Token); !errors.Is(err, ErrNotFound) {
		t.Fatalf("deleted form err=%v", err)
	}
	// The task stays.
	if _, err := storage.GetTaskTitleDescription(taskID); err != nil {
		t.Fatalf("task gone after form delete: %v", err)
	}
}

func TestRequestFormValidation(t *testing.T) {
	ctx := context.Background()
	proj, err := CreateProject(ctx, 1, "Form Rules", "")
	if err != nil {
		t.Fatal(err)
	}
	bad := []RequestFormInput{
		{Title: ""},
		{Title: "x", Priority: 4},
		{Title: "x", Fields: []storage.RequestFormField{{Label: "A", Type: "color"}}},
		{Title: "x", Fields: []storage.RequestFormField{{Label: "A", Type: storage.FormFieldSelect}}},
		{Title: "x", Fields: []storage.RequestFormField{{ID: "a", Label: "A", Type: "text"}, {ID: "a", Label: "B", Type: "text"}}},
		{Title: "x", Fields: []storage.RequestFormField{{ID: "Bad Id", Label: "A", Type: "text"}}},
		{Title: "x", Fields: []storage.RequestFormField{{ID: "summary", Label: "A", Type: "text"}}},
		{Title: "x", Fields: []storage.RequestFormField{{Label: "", Type: "text"}}},
		{Title: "x", TagIDs: []int{999999}},
	}
	for i, in := range bad {
		if _, err := CreateRequestForm(ctx, 1, proj.ID, in); !errors.Is(err, ErrValidation) {
			t.Errorf("case %d: err=%v, want validation", i, err)
		}
	}
}
