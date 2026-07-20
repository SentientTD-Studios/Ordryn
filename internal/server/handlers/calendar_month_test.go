package handlers

import "testing"

func TestIsValidYearMonth(t *testing.T) {
	if !isValidYearMonth("2026-07") {
		t.Fatal("expected 2026-07 valid")
	}
	if isValidYearMonth("2026-13") {
		t.Fatal("expected 2026-13 invalid")
	}
	if isValidYearMonth("july") {
		t.Fatal("expected july invalid")
	}
}

func TestBuildCalendarMonthJSONShape(t *testing.T) {
	view := buildCalendarMonthJSON(0, "UTC", "2026-07")
	if view.YearMonth != "2026-07" {
		t.Fatalf("YearMonth = %q", view.YearMonth)
	}
	if view.MonthLabel != "July 2026" {
		t.Fatalf("MonthLabel = %q", view.MonthLabel)
	}
	if view.PrevMonth != "2026-06" || view.NextMonth != "2026-08" {
		t.Fatalf("prev/next = %q / %q", view.PrevMonth, view.NextMonth)
	}
	if len(view.Weeks) != 6 {
		t.Fatalf("weeks = %d, want 6", len(view.Weeks))
	}
	for i, week := range view.Weeks {
		if len(week) != 7 {
			t.Fatalf("week %d len = %d", i, len(week))
		}
	}
	if view.Weeks[0][0].Tasks == nil {
		t.Fatal("expected non-nil tasks slice")
	}
}
