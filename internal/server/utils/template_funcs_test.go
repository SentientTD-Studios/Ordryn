package utils

import (
	"testing"
	"time"
)

func TestDueDateDisplay(t *testing.T) {
	tz := "UTC"
	loc := dueDateLocation(tz)
	now := time.Now().In(loc)
	todayStr := now.Format("2006-01-02")
	yesterday := now.AddDate(0, 0, -1).Format("2006-01-02")
	tomorrow := now.AddDate(0, 0, 1).Format("2006-01-02")
	in3 := now.AddDate(0, 0, 3).Format("2006-01-02")
	in10 := now.AddDate(0, 0, 10).Format("2006-01-02")

	tests := []struct {
		due       string
		completed bool
		want      string
	}{
		{"", false, ""},
		{todayStr, false, "Today"},
		{yesterday, false, "Yesterday"},
		{tomorrow, false, "Tomorrow"},
		{in3, false, "In 3 days"},
		{in10, false, in10},
		{yesterday, true, yesterday},
	}

	for _, tc := range tests {
		got := DueDateDisplay(tc.due, tc.completed, tz)
		if got != tc.want {
			t.Errorf("DueDateDisplay(%q, %v) = %q, want %q", tc.due, tc.completed, got, tc.want)
		}
	}
}

func TestDueDateInputValue(t *testing.T) {
	tests := []struct {
		in   interface{}
		want string
	}{
		{nil, ""},
		{"", ""},
		{"2026-07-17", "2026-07-17"},
		{"2026-07-17 00:00:00+00", "2026-07-17"},
		{"  2026-07-17  ", "2026-07-17"},
	}

	for _, tc := range tests {
		got := DueDateInputValue(tc.in)
		if got != tc.want {
			t.Errorf("DueDateInputValue(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
