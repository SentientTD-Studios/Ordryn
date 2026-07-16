package handlers

import (
	"GoTodo/internal/tasks"
	"testing"
)

func TestTaskToCSVRow(t *testing.T) {
	row := taskToCSVRow(tasks.Task{
		ID:           1,
		Title:        "Test",
		Description:  "Desc",
		Completed:    true,
		DueDate:      "2026-07-01",
		ProjectName:  "Work",
		Priority:     2,
		IsFavorite:   false,
		Position:     3,
		Tags:         []tasks.Tag{{Name: "a"}, {Name: "b"}},
		DateCreated:  "2026/01/01",
		DateModified: "2026/01/02",
	})
	if len(row) != 12 {
		t.Fatalf("expected 12 columns, got %d", len(row))
	}
	if row[0] != "1" || row[1] != "Test" || row[9] != "a;b" {
		t.Fatalf("unexpected row: %#v", row)
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

func TestDryRunImportCountsSkipsEmptyTitles(t *testing.T) {
	cols := importColumnMap{title: 0, dueDate: 1}
	rows := [][]string{
		{""},
		{"   ", "2026-07-01"},
	}
	wouldImport, wouldSkip, err := dryRunImportCounts(1, cols, rows)
	if err != nil {
		t.Fatalf("empty titles should not hit database: %v", err)
	}
	if wouldImport != 0 || wouldSkip != 2 {
		t.Fatalf("expected 2 skipped, got import=%d skip=%d", wouldImport, wouldSkip)
	}
}

func TestIcsEscape(t *testing.T) {
	got := icsEscape("a;b,c")
	want := `a\;b\,c`
	if got != want {
		t.Fatalf("unexpected escape: %q want %q", got, want)
	}
}
