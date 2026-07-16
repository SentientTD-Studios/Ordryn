package handlers

import (
	"net/http/httptest"
	"testing"
	"time"
)

func TestBuildCalendarMonthGridShape(t *testing.T) {
	view := buildCalendarMonth(0, "UTC", "2026-07")
	if len(view.Weeks) != 6 {
		t.Fatalf("expected 6 weeks, got %d", len(view.Weeks))
	}
	for i, week := range view.Weeks {
		if len(week) != 7 {
			t.Fatalf("week %d: expected 7 days, got %d", i, len(week))
		}
	}
	if view.MonthLabel != "July 2026" {
		t.Fatalf("MonthLabel = %q, want July 2026", view.MonthLabel)
	}
	if view.PrevMonth != "2026-06" || view.NextMonth != "2026-08" {
		t.Fatalf("prev/next = %s / %s", view.PrevMonth, view.NextMonth)
	}
}

func TestBuildCalendarMonthMarksToday(t *testing.T) {
	now := time.Now().UTC()
	yearMonth := now.Format("2006-01")
	view := buildCalendarMonth(0, "UTC", yearMonth)

	foundToday := false
	for _, week := range view.Weeks {
		for _, cell := range week {
			if cell.IsToday {
				foundToday = true
				if cell.Date != now.Format("2006-01-02") {
					t.Fatalf("today cell date = %s, want %s", cell.Date, now.Format("2006-01-02"))
				}
			}
		}
	}
	if !foundToday {
		t.Fatal("expected one cell marked as today")
	}
}

func TestSortedCalendarDates(t *testing.T) {
	byDate := map[string][]calendarTask{
		"2026-07-15": {{ID: 1}},
		"2026-07-03": {{ID: 2}},
		"2026-07-28": {{ID: 3}},
	}
	dates := sortedCalendarDates(byDate)
	want := []string{"2026-07-03", "2026-07-15", "2026-07-28"}
	for i, d := range dates {
		if d != want[i] {
			t.Fatalf("dates[%d] = %s, want %s", i, d, want[i])
		}
	}
}

func TestCalendarYearMonthFromLegacyPath(t *testing.T) {
	cases := []struct {
		path string
		want string
	}{
		{"/calendar", ""},
		{"/calendar/", ""},
		{"/calendar/2026-03", "2026-03"},
		{"/calendar/not-a-date", ""},
	}
	for _, tc := range cases {
		got := calendarYearMonthFromPathString(tc.path)
		if got != tc.want {
			t.Errorf("path %q: got %q, want %q", tc.path, got, tc.want)
		}
	}
}

func TestCalendarPageURL(t *testing.T) {
	if got := calendarPageURL(); got != "/calendar" {
		t.Fatalf("calendarPageURL() = %q", got)
	}
}

func TestCalendarMonthFromRequest(t *testing.T) {
	r := httptest.NewRequest("POST", "/api/edit-task", nil)
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.Form = map[string][]string{"calendar_month": {"2026-03"}}
	if got := calendarMonthFromRequest(r, "UTC"); got != "2026-03" {
		t.Fatalf("got %q", got)
	}
}

func TestIsCalendarReturn(t *testing.T) {
	r := httptest.NewRequest("GET", "/api/edit?from=calendar", nil)
	if !isCalendarReturn(r) {
		t.Fatal("expected calendar return from query")
	}
}

func TestIsValidYearMonth(t *testing.T) {
	if !isValidYearMonth("2026-07") {
		t.Fatal("expected valid")
	}
	if isValidYearMonth("2026-13") {
		t.Fatal("expected invalid month")
	}
	if isValidYearMonth("bad") {
		t.Fatal("expected invalid")
	}
}
