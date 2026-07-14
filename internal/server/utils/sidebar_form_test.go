package utils

import (
	"bytes"
	"testing"
)

func TestSidebarFormRendersWithoutDueDateField(t *testing.T) {
	t.Chdir("../../..")
	if err := InitializeTemplates(); err != nil {
		t.Fatalf("InitializeTemplates: %v", err)
	}

	ctx := map[string]interface{}{
		"Title":    "GoTodo - Home",
		"Projects": []map[string]interface{}{},
	}

	var buf bytes.Buffer
	if err := Templates.ExecuteTemplate(&buf, "sidebar_form.html", ctx); err != nil {
		t.Fatalf("ExecuteTemplate sidebar_form.html: %v", err)
	}
	if buf.Len() == 0 {
		t.Fatal("expected sidebar form HTML")
	}
}
