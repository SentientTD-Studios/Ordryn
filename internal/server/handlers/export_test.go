package handlers

import (
	"GoTodo/internal/tasks"
	"testing"
)

func TestTaskToCSVRow(t *testing.T) {
	row := taskToCSVRow(tasks.Task{
		ID:          1,
		Title:       "Test",
		Description: "Desc",
		Completed:   true,
		DueDate:     "2026-07-01",
		ProjectName: "Work",
		Priority:    2,
		IsFavorite:  false,
		Position:    3,
		Tags:        []tasks.Tag{{Name: "a"}, {Name: "b"}},
		DateCreated: "2026/01/01",
		DateModified: "2026/01/02",
	})
	if len(row) != 12 {
		t.Fatalf("expected 12 columns, got %d", len(row))
	}
	if row[0] != "1" || row[1] != "Test" || row[9] != "a;b" {
		t.Fatalf("unexpected row: %#v", row)
	}
}

func TestParseBulkTaskIDs(t *testing.T) {
	ids, err := parseBulkTaskIDs("1, 2,2, 3")
	if err != nil {
		t.Fatal(err)
	}
	if len(ids) != 3 {
		t.Fatalf("expected 3 unique ids, got %v", ids)
	}
}

func TestParseImportCSV(t *testing.T) {
	csv := "title,description,tags\nTask one,Hello,work\nTask two,,"
	cols, rows, err := parseImportCSV([]byte(csv))
	if err != nil {
		t.Fatal(err)
	}
	if cols.title != 0 || len(rows) != 2 {
		t.Fatalf("unexpected parse result cols=%+v rows=%d", cols, len(rows))
	}
}

func TestParseImportCSVRequiresTitle(t *testing.T) {
	_, _, err := parseImportCSV([]byte("name,description\nx,y"))
	if err == nil {
		t.Fatal("expected error for missing title column")
	}
}
