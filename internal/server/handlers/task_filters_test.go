package handlers

import (
	"net/http/httptest"
	"testing"
)

func TestParseSprintFilter(t *testing.T) {
	zero := 0
	val42 := 42
	val1 := 1

	cases := []struct {
		input string
		want  *int
	}{
		{"", nil},
		{"none", &zero},
		{"backlog", &zero},
		{"0", &zero},
		{"42", &val42},
		{"1", &val1},
		{"-1", nil},
		{"-5", nil},
		{"invalid", nil},
		{"abc", nil},
		{"123abc", nil},
	}

	for _, tc := range cases {
		t.Run("input_"+tc.input, func(t *testing.T) {
			got := parseSprintFilter(tc.input)
			if tc.want == nil {
				if got != nil {
					t.Fatalf("parseSprintFilter(%q) = %v, want nil", tc.input, *got)
				}
			} else {
				if got == nil {
					t.Fatalf("parseSprintFilter(%q) = nil, want %d", tc.input, *tc.want)
				} else if *got != *tc.want {
					t.Fatalf("parseSprintFilter(%q) = %d, want %d", tc.input, *got, *tc.want)
				}
			}
		})
	}
}

func TestParseProjectFilter(t *testing.T) {
	zero := 0
	val5 := 5

	cases := []struct {
		input string
		want  *int
	}{
		{"", nil},
		{"none", &zero},
		{"0", &zero},
		{"5", &val5},
		{"invalid", nil},
	}

	for _, tc := range cases {
		t.Run("input_"+tc.input, func(t *testing.T) {
			got := parseProjectFilter(tc.input)
			if tc.want == nil {
				if got != nil {
					t.Fatalf("parseProjectFilter(%q) = %v, want nil", tc.input, *got)
				}
			} else {
				if got == nil {
					t.Fatalf("parseProjectFilter(%q) = nil, want %d", tc.input, *tc.want)
				} else if *got != *tc.want {
					t.Fatalf("parseProjectFilter(%q) = %d, want %d", tc.input, *got, *tc.want)
				}
			}
		})
	}
}

func TestFilterContextFromRequest(t *testing.T) {
	req := httptest.NewRequest("GET", "/api/v2/tasks?project=3&sprint_id=7&status=incomplete&due=today&priority=2&tag=urgent&workflow_claim_scope=mine&include_subtasks=1&page=3", nil)
	fc := filterContextFromRequest(req)

	if fc.Project != "3" {
		t.Fatalf("fc.Project = %q, want '3'", fc.Project)
	}
	if fc.Sprint != "7" {
		t.Fatalf("fc.Sprint = %q, want '7'", fc.Sprint)
	}
	if fc.Status != "incomplete" {
		t.Fatalf("fc.Status = %q, want 'incomplete'", fc.Status)
	}
	if fc.Due != "today" {
		t.Fatalf("fc.Due = %q, want 'today'", fc.Due)
	}
	if fc.Priority != "2" {
		t.Fatalf("fc.Priority = %q, want '2'", fc.Priority)
	}
	if fc.Tag != "urgent" {
		t.Fatalf("fc.Tag = %q, want 'urgent'", fc.Tag)
	}
	if fc.WorkflowClaimScope != "mine" {
		t.Fatalf("fc.WorkflowClaimScope = %q, want 'mine'", fc.WorkflowClaimScope)
	}
	if !fc.IncludeSubtasks {
		t.Fatal("fc.IncludeSubtasks = false, want true")
	}
	if fc.Page != 3 {
		t.Fatalf("fc.Page = %d, want 3", fc.Page)
	}
}

func TestFilterContextToListFiltersWithSprint(t *testing.T) {
	fc := FilterContext{
		Project:            "5",
		Sprint:             "12",
		Status:             "complete",
		Priority:           "1",
		WorkflowClaimScope: "all",
	}
	lf := fc.ToListFilters()

	if lf.ProjectFilter == nil || *lf.ProjectFilter != 5 {
		t.Fatalf("lf.ProjectFilter = %v, want 5", lf.ProjectFilter)
	}
	if lf.SprintFilter == nil || *lf.SprintFilter != 12 {
		t.Fatalf("lf.SprintFilter = %v, want 12", lf.SprintFilter)
	}
	if lf.StatusFilter != "complete" {
		t.Fatalf("lf.StatusFilter = %q, want 'complete'", lf.StatusFilter)
	}
	if lf.PriorityFilter == nil || *lf.PriorityFilter != 1 {
		t.Fatalf("lf.PriorityFilter = %v, want 1", lf.PriorityFilter)
	}
	if lf.WorkflowClaimScope != "all" {
		t.Fatalf("lf.WorkflowClaimScope = %q, want 'all'", lf.WorkflowClaimScope)
	}
	if lf.IncludeSubtasks {
		t.Fatal("lf.IncludeSubtasks = true, want false by default")
	}
}

func TestFilterContextToListFiltersIncludeSubtasks(t *testing.T) {
	fc := FilterContext{
		Due:             "overdue",
		IncludeSubtasks: true,
	}
	lf := fc.ToListFilters()
	if !lf.IncludeSubtasks {
		t.Fatal("lf.IncludeSubtasks = false, want true")
	}
	if lf.DueFilter != "overdue" {
		t.Fatalf("lf.DueFilter = %q, want overdue", lf.DueFilter)
	}
}

func TestFilterContextToListFiltersBacklogSprint(t *testing.T) {
	fc := FilterContext{
		Project: "5",
		Sprint:  "backlog",
	}
	lf := fc.ToListFilters()

	if lf.SprintFilter == nil || *lf.SprintFilter != 0 {
		t.Fatalf("lf.SprintFilter = %v, want 0 (backlog)", lf.SprintFilter)
	}
}

func TestCompletedIncompleteCountsNilUserID(t *testing.T) {
	c, inc := completedIncompleteCounts(nil, nil, nil)
	if c != 0 || inc != 0 {
		t.Fatalf("expected (0, 0) for nil user, got (%d, %d)", c, inc)
	}
	c, inc = completedIncompleteCounts(nil, nil, nil, true)
	if c != 0 || inc != 0 {
		t.Fatalf("expected (0, 0) for nil user with includeRemoved, got (%d, %d)", c, inc)
	}
}
