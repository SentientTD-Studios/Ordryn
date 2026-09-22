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
