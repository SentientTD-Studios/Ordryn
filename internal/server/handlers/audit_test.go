package handlers

import "testing"

func TestFormatEventLabelFieldChanges(t *testing.T) {
	cases := []struct {
		eventType string
		meta      map[string]interface{}
		want      string
	}{
		{eventType: "title_changed", want: "Title changed"},
		{eventType: "title_changed", meta: map[string]interface{}{"to": "Ship it"}, want: "Title · Ship it"},
		{eventType: "priority_changed", want: "Priority changed"},
		{eventType: "priority_changed", meta: map[string]interface{}{"to": "High"}, want: "Priority · High"},
		{eventType: "estimate_changed", want: "Estimate cleared"},
		{eventType: "estimate_changed", meta: map[string]interface{}{"to": 5}, want: "Estimate · 5"},
		{eventType: "estimate_changed", meta: map[string]interface{}{"to": float64(8)}, want: "Estimate · 8"},
		{eventType: "due_date_changed", want: "Due date cleared"},
		{eventType: "due_date_changed", meta: map[string]interface{}{"to": "2026-09-21"}, want: "Due date · 2026-09-21"},
		{eventType: "parent_changed", want: "Parent cleared"},
		{eventType: "parent_changed", meta: map[string]interface{}{"to": "Epic"}, want: "Parent · Epic"},
	}
	for _, tc := range cases {
		got := formatEventLabel(tc.eventType, tc.meta)
		if got != tc.want {
			t.Errorf("%s %v: got %q want %q", tc.eventType, tc.meta, got, tc.want)
		}
	}
}

func TestFormatEventLabelStatusChanged(t *testing.T) {
	if got := formatEventLabel("status_changed", nil); got != "Status changed" {
		t.Fatalf("legacy label=%q", got)
	}
	got := formatEventLabel("status_changed", map[string]interface{}{"to": "In Progress"})
	if got != "Status changed · In Progress" {
		t.Fatalf("label=%q", got)
	}
}

func TestFormatEventLabelSprintChanged(t *testing.T) {
	if got := formatEventLabel("sprint_changed", nil); got != "Sprint changed" {
		t.Fatalf("legacy label=%q", got)
	}
	got := formatEventLabel("sprint_changed", map[string]interface{}{"to": "Sprint 12"})
	if got != "Sprint · Sprint 12" {
		t.Fatalf("label=%q", got)
	}
}

func TestFormatEventLabelRecurrence(t *testing.T) {
	cases := []struct {
		eventType string
		meta      map[string]interface{}
		want      string
	}{
		{eventType: "recurrence_set", want: "Repeat set"},
		{eventType: "recurrence_set", meta: map[string]interface{}{"summary": "Weekly on Mon"}, want: "Repeats · Weekly on Mon"},
		{eventType: "recurrence_cleared", want: "Repeat removed"},
		{eventType: "recurrence_cleared", meta: map[string]interface{}{"reason": "nested"}, want: "Repeat removed · became a subtask"},
		{eventType: "recurrence_next", meta: map[string]interface{}{"due_date": "2026-10-05"}, want: "Next occurrence created · due 2026-10-05"},
		{eventType: "recurrence_created", meta: map[string]interface{}{"from_id": float64(12)}, want: "Repeated from #12"},
		{eventType: "recurrence_ended", want: "Repeat series ended"},
		{eventType: "recurrence_failed", want: "Could not create next occurrence"},
		{eventType: "recurrence_undone", want: "Next occurrence removed · reopened"},
	}
	for _, tc := range cases {
		if got := formatEventLabel(tc.eventType, tc.meta); got != tc.want {
			t.Errorf("%s %v: got %q want %q", tc.eventType, tc.meta, got, tc.want)
		}
	}
}

func TestFormatEventLabelLinks(t *testing.T) {
	if got := formatEventLabel("link_added", map[string]interface{}{"kind": "blocked_by", "title": "Ship API"}); got != "Link added · blocked by Ship API" {
		t.Fatalf("label=%q", got)
	}
	if got := formatEventLabel("link_removed", nil); got != "Link removed" {
		t.Fatalf("label=%q", got)
	}
}
