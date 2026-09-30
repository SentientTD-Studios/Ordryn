package recurrence

import (
	"errors"
	"testing"
	"time"
)

func day(s string) time.Time {
	t, err := time.Parse(DateLayout, s)
	if err != nil {
		panic(err)
	}
	return t
}

func mustNext(t *testing.T, r Rule, due, today string) string {
	t.Helper()
	r.Normalize()
	if err := r.Validate(); err != nil {
		t.Fatalf("validate %+v: %v", r, err)
	}
	next, ok, err := r.Next(due, day(today))
	if err != nil {
		t.Fatalf("next %+v: %v", r, err)
	}
	if !ok {
		t.Fatalf("next %+v: series ended unexpectedly", r)
	}
	return next.Format(DateLayout)
}

func TestNextDaily(t *testing.T) {
	cases := []struct {
		name       string
		rule       Rule
		due, today string
		want       string
	}{
		{"on time", Rule{Frequency: Daily}, "2026-09-30", "2026-09-30", "2026-10-01"},
		{"completed early", Rule{Frequency: Daily}, "2026-10-05", "2026-09-30", "2026-10-06"},
		{"overdue by one day lands today", Rule{Frequency: Daily}, "2026-09-29", "2026-09-30", "2026-09-30"},
		{"long overdue rolls forward on cadence", Rule{Frequency: Daily, Interval: 3}, "2026-09-01", "2026-09-30", "2026-10-01"},
		{"long overdue can land today", Rule{Frequency: Daily, Interval: 3}, "2026-09-03", "2026-09-30", "2026-09-30"},
		{"every 3 days", Rule{Frequency: Daily, Interval: 3}, "2026-09-30", "2026-09-30", "2026-10-03"},
		{"no due date uses today", Rule{Frequency: Daily, Interval: 2}, "", "2026-09-30", "2026-10-02"},
		{"completion basis ignores due", Rule{Frequency: Daily, Interval: 3, Basis: BasisCompletion}, "2026-09-01", "2026-09-30", "2026-10-03"},
		{"month boundary", Rule{Frequency: Daily}, "2026-10-31", "2026-10-31", "2026-11-01"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := mustNext(t, tc.rule, tc.due, tc.today); got != tc.want {
				t.Fatalf("got %s want %s", got, tc.want)
			}
		})
	}
}

func TestNextWeekly(t *testing.T) {
	// 2026-09-28 is a Monday.
	cases := []struct {
		name       string
		rule       Rule
		due, today string
		want       string
	}{
		{"same weekday default", Rule{Frequency: Weekly}, "2026-09-28", "2026-09-28", "2026-10-05"},
		{"completed early keeps cadence", Rule{Frequency: Weekly}, "2026-09-28", "2026-09-25", "2026-10-05"},
		{"overdue skips missed weeks", Rule{Frequency: Weekly}, "2026-09-07", "2026-09-30", "2026-10-05"},
		{"multiple weekdays same week", Rule{Frequency: Weekly, Weekdays: []int{1, 4}}, "2026-09-28", "2026-09-28", "2026-10-01"},
		{"multiple weekdays wraps", Rule{Frequency: Weekly, Weekdays: []int{1, 4}}, "2026-10-01", "2026-10-01", "2026-10-05"},
		{"every 2 weeks wraps two weeks", Rule{Frequency: Weekly, Interval: 2, Weekdays: []int{1, 4}}, "2026-10-01", "2026-10-01", "2026-10-12"},
		{"every 2 weeks within week", Rule{Frequency: Weekly, Interval: 2, Weekdays: []int{1, 4}}, "2026-09-28", "2026-09-28", "2026-10-01"},
		{"sunday is end of week", Rule{Frequency: Weekly, Weekdays: []int{0, 1}}, "2026-09-28", "2026-09-28", "2026-10-04"},
		{"from sunday wraps to monday", Rule{Frequency: Weekly, Weekdays: []int{0, 1}}, "2026-10-04", "2026-10-04", "2026-10-05"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := mustNext(t, tc.rule, tc.due, tc.today); got != tc.want {
				t.Fatalf("got %s want %s", got, tc.want)
			}
		})
	}
}

func TestNextMonthlyAndYearly(t *testing.T) {
	cases := []struct {
		name       string
		rule       Rule
		due, today string
		want       string
	}{
		{"same day next month", Rule{Frequency: Monthly}, "2026-09-15", "2026-09-15", "2026-10-15"},
		{"clamps to short month", Rule{Frequency: Monthly, MonthDay: 31}, "2027-01-31", "2027-01-31", "2027-02-28"},
		{"anchor survives clamping", Rule{Frequency: Monthly, MonthDay: 31}, "2027-02-28", "2027-02-28", "2027-03-31"},
		{"leap year february", Rule{Frequency: Monthly, MonthDay: 30}, "2028-01-30", "2028-01-30", "2028-02-29"},
		{"every 3 months", Rule{Frequency: Monthly, Interval: 3}, "2026-11-10", "2026-11-10", "2027-02-10"},
		{"explicit month day", Rule{Frequency: Monthly, MonthDay: 1}, "2026-09-20", "2026-09-20", "2026-10-01"},
		{"yearly", Rule{Frequency: Yearly}, "2026-03-14", "2026-03-14", "2027-03-14"},
		{"yearly feb 29 clamps", Rule{Frequency: Yearly}, "2028-02-29", "2028-02-29", "2029-02-28"},
		{"yearly overdue rolls", Rule{Frequency: Yearly}, "2024-06-01", "2026-09-30", "2027-06-01"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := mustNext(t, tc.rule, tc.due, tc.today); got != tc.want {
				t.Fatalf("got %s want %s", got, tc.want)
			}
		})
	}
}

func TestNextEndsOn(t *testing.T) {
	r := Rule{Frequency: Weekly, EndsOn: "2026-10-10"}
	r.Normalize()
	next, ok, err := r.Next("2026-09-28", day("2026-09-28"))
	if err != nil || !ok || next.Format(DateLayout) != "2026-10-05" {
		t.Fatalf("before end: next=%v ok=%v err=%v", next, ok, err)
	}
	_, ok, err = r.Next("2026-10-05", day("2026-10-05"))
	if err != nil || ok {
		t.Fatalf("after end should stop: ok=%v err=%v", ok, err)
	}
	// Landing exactly on the end date is still allowed.
	r2 := Rule{Frequency: Daily, EndsOn: "2026-10-01"}
	r2.Normalize()
	next, ok, err = r2.Next("2026-09-30", day("2026-09-30"))
	if err != nil || !ok || next.Format(DateLayout) != "2026-10-01" {
		t.Fatalf("on end date: next=%v ok=%v err=%v", next, ok, err)
	}
}

func TestValidate(t *testing.T) {
	bad := []Rule{
		{Frequency: "hourly"},
		{Frequency: ""},
		{Frequency: Daily, Interval: MaxInterval + 1},
		{Frequency: Weekly, Weekdays: []int{7}},
		{Frequency: Monthly, MonthDay: 32},
		{Frequency: Daily, Basis: "whenever"},
		{Frequency: Daily, EndsOn: "10/01/2026"},
		{Frequency: Daily, EndAfter: -1},
		{Frequency: Daily, EndAfter: MaxEndAfter + 1},
	}
	for _, r := range bad {
		r.Normalize()
		if err := r.Validate(); err == nil || !errors.Is(err, ErrInvalid) {
			t.Errorf("expected invalid for %+v, got %v", r, err)
		}
	}
	good := Rule{Frequency: " Weekly ", Weekdays: []int{4, 1, 4}, MonthDay: 12}
	good.Normalize()
	if err := good.Validate(); err != nil {
		t.Fatalf("good rule: %v", err)
	}
	if good.Frequency != Weekly || good.Interval != 1 || good.Basis != BasisDue || good.MonthDay != 0 {
		t.Fatalf("normalize: %+v", good)
	}
	if len(good.Weekdays) != 2 || good.Weekdays[0] != 1 || good.Weekdays[1] != 4 {
		t.Fatalf("weekdays not deduped/sorted: %v", good.Weekdays)
	}
}

func TestWeekdayMaskRoundTrip(t *testing.T) {
	r := Rule{Weekdays: []int{0, 3, 6}}
	mask := r.WeekdayMask()
	if mask != 1|8|64 {
		t.Fatalf("mask=%d", mask)
	}
	got := WeekdaysFromMask(mask)
	if len(got) != 3 || got[0] != 0 || got[1] != 3 || got[2] != 6 {
		t.Fatalf("round trip=%v", got)
	}
}

func TestSummary(t *testing.T) {
	cases := []struct {
		rule Rule
		want string
	}{
		{Rule{Frequency: Daily, Interval: 1}, "Daily"},
		{Rule{Frequency: Daily, Interval: 3, Basis: BasisCompletion}, "Every 3 days after completion"},
		{Rule{Frequency: Weekly, Interval: 2, Weekdays: []int{4, 1}}, "Every 2 weeks on Mon, Thu"},
		{Rule{Frequency: Monthly, Interval: 1, MonthDay: 1}, "Monthly on the 1st"},
		{Rule{Frequency: Monthly, Interval: 1, MonthDay: 22}, "Monthly on the 22nd"},
		{Rule{Frequency: Monthly, Interval: 1, MonthDay: 13}, "Monthly on the 13th"},
		{Rule{Frequency: Monthly, Interval: 1, MonthDay: 31}, "Monthly on the last day"},
		{Rule{Frequency: Yearly, Interval: 1, EndsOn: "2030-01-01"}, "Yearly until 2030-01-01"},
		{Rule{Frequency: Weekly, Interval: 1, EndAfter: 5}, "Weekly, 5 times"},
	}
	for _, tc := range cases {
		if got := tc.rule.Summary(); got != tc.want {
			t.Errorf("%+v: got %q want %q", tc.rule, got, tc.want)
		}
	}
}

func TestTodayIn(t *testing.T) {
	now := time.Date(2026, 10, 1, 2, 30, 0, 0, time.UTC) // 22:30 on Sep 30 in New York
	if got := TodayIn("America/New_York", now).Format(DateLayout); got != "2026-09-30" {
		t.Fatalf("new york today=%s", got)
	}
	if got := TodayIn("", now).Format(DateLayout); got != "2026-10-01" {
		t.Fatalf("utc today=%s", got)
	}
	if got := TodayIn("Not/AZone", now).Format(DateLayout); got != "2026-10-01" {
		t.Fatalf("bad tz today=%s", got)
	}
}
