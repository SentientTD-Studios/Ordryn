// Package recurrence holds the pure date math for recurring tasks: rule
// validation, computing the next due date, and a human-readable summary.
// It has no database or HTTP dependencies so it can be unit tested directly.
package recurrence

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
)

// Frequencies supported by a Rule.
const (
	Daily   = "daily"
	Weekly  = "weekly"
	Monthly = "monthly"
	Yearly  = "yearly"
)

// Bases for computing the next due date.
const (
	// BasisDue schedules from the previous due date ("every Monday"), rolling
	// forward past any missed occurrences so the next task is never overdue.
	BasisDue = "due"
	// BasisCompletion schedules from the day the task was completed
	// ("3 days after I last did it").
	BasisCompletion = "completion"
)

// DateLayout is the YYYY-MM-DD format used for due dates.
const DateLayout = "2006-01-02"

// MaxInterval caps "every N <unit>" to keep rules sane.
const MaxInterval = 365

// MaxEndAfter caps the "end after N occurrences" setting.
const MaxEndAfter = 1000

// ErrInvalid is wrapped by every validation error.
var ErrInvalid = errors.New("invalid recurrence")

// Rule describes how a task repeats.
type Rule struct {
	Frequency string
	// Interval is the step size: every Interval days/weeks/months/years. Minimum 1.
	Interval int
	// Weekdays applies to Weekly rules: 0=Sunday … 6=Saturday. Empty means
	// "the same weekday as the base date".
	Weekdays []int
	// MonthDay applies to Monthly rules: 1-31. Days past the end of a month
	// clamp to the last day (31 => last day of every month). 0 means "the same
	// day of month as the base date".
	MonthDay int
	Basis    string
	// EndsOn stops the series after this date (YYYY-MM-DD). Empty = never.
	EndsOn string
	// EndAfter stops the series after this many occurrences in total
	// (including the first task). 0 = unlimited.
	EndAfter int
}

// Normalize fills defaults and canonicalizes fields in place.
func (r *Rule) Normalize() {
	r.Frequency = strings.ToLower(strings.TrimSpace(r.Frequency))
	r.Basis = strings.ToLower(strings.TrimSpace(r.Basis))
	r.EndsOn = strings.TrimSpace(r.EndsOn)
	if r.Interval <= 0 {
		r.Interval = 1
	}
	if r.Basis == "" {
		r.Basis = BasisDue
	}
	if r.Frequency != Weekly {
		r.Weekdays = nil
	} else {
		r.Weekdays = uniqueSortedWeekdays(r.Weekdays)
	}
	if r.Frequency != Monthly {
		r.MonthDay = 0
	}
}

// Validate checks a normalized rule.
func (r Rule) Validate() error {
	switch r.Frequency {
	case Daily, Weekly, Monthly, Yearly:
	default:
		return fmt.Errorf("%w: frequency must be daily, weekly, monthly, or yearly", ErrInvalid)
	}
	if r.Interval < 1 || r.Interval > MaxInterval {
		return fmt.Errorf("%w: interval must be 1-%d", ErrInvalid, MaxInterval)
	}
	for _, d := range r.Weekdays {
		if d < 0 || d > 6 {
			return fmt.Errorf("%w: weekdays must be 0 (Sunday) through 6 (Saturday)", ErrInvalid)
		}
	}
	if r.MonthDay < 0 || r.MonthDay > 31 {
		return fmt.Errorf("%w: month_day must be 1-31", ErrInvalid)
	}
	if r.Basis != BasisDue && r.Basis != BasisCompletion {
		return fmt.Errorf("%w: basis must be due or completion", ErrInvalid)
	}
	if r.EndsOn != "" {
		if _, err := time.Parse(DateLayout, r.EndsOn); err != nil {
			return fmt.Errorf("%w: ends_on must be YYYY-MM-DD", ErrInvalid)
		}
	}
	if r.EndAfter < 0 || r.EndAfter > MaxEndAfter {
		return fmt.Errorf("%w: end_after must be 0-%d", ErrInvalid, MaxEndAfter)
	}
	return nil
}

// WeekdayMask encodes Weekdays as a bitmask (bit i = weekday i).
func (r Rule) WeekdayMask() int {
	mask := 0
	for _, d := range r.Weekdays {
		if d >= 0 && d <= 6 {
			mask |= 1 << d
		}
	}
	return mask
}

// WeekdaysFromMask decodes a bitmask produced by WeekdayMask.
func WeekdaysFromMask(mask int) []int {
	out := []int{}
	for d := 0; d <= 6; d++ {
		if mask&(1<<d) != 0 {
			out = append(out, d)
		}
	}
	return out
}

// Next computes the due date for the next occurrence.
//
// due is the completed task's due date ("" when it had none), today is the
// completion day in the owner's timezone. The result is never before today:
// for BasisDue, missed occurrences are skipped. ok is false when the series
// has ended because of EndsOn.
func (r Rule) Next(due string, today time.Time) (next time.Time, ok bool, err error) {
	today = dateOnly(today)
	base := today
	if r.Basis == BasisDue && strings.TrimSpace(due) != "" {
		d, perr := time.Parse(DateLayout, strings.TrimSpace(due))
		if perr != nil {
			return time.Time{}, false, fmt.Errorf("%w: due date %q", ErrInvalid, due)
		}
		base = dateOnly(d)
	}
	anchorDay := r.MonthDay
	if anchorDay == 0 {
		anchorDay = base.Day()
	}
	weekdays := r.Weekdays
	if r.Frequency == Weekly && len(weekdays) == 0 {
		weekdays = []int{int(base.Weekday())}
	}

	next = r.step(base, anchorDay, weekdays)
	// Roll forward past missed occurrences so the new task starts current.
	// Bounded to avoid pathological loops on very old due dates.
	for i := 0; next.Before(today) && i < 10000; i++ {
		next = r.step(next, anchorDay, weekdays)
	}
	if next.Before(today) {
		return time.Time{}, false, fmt.Errorf("%w: could not schedule next occurrence", ErrInvalid)
	}
	if r.EndsOn != "" {
		end, perr := time.Parse(DateLayout, r.EndsOn)
		if perr == nil && next.After(dateOnly(end)) {
			return time.Time{}, false, nil
		}
	}
	return next, true, nil
}

// step advances one occurrence from d.
func (r Rule) step(d time.Time, anchorDay int, weekdays []int) time.Time {
	switch r.Frequency {
	case Daily:
		return d.AddDate(0, 0, r.Interval)
	case Weekly:
		return nextWeekly(d, r.Interval, weekdays)
	case Monthly:
		return addMonthsClamped(d, r.Interval, anchorDay)
	case Yearly:
		return addMonthsClamped(d, 12*r.Interval, anchorDay)
	}
	return d.AddDate(0, 0, 1)
}

// nextWeekly returns the next selected weekday after d. Later weekdays in the
// same (Monday-start) week come first; otherwise it jumps interval weeks ahead
// and takes the earliest selected weekday of that week.
func nextWeekly(d time.Time, interval int, weekdays []int) time.Time {
	selected := map[int]bool{}
	for _, w := range weekdays {
		selected[w] = true
	}
	weekStart := startOfWeek(d)
	for c := d.AddDate(0, 0, 1); c.Before(weekStart.AddDate(0, 0, 7)); c = c.AddDate(0, 0, 1) {
		if selected[int(c.Weekday())] {
			return c
		}
	}
	target := weekStart.AddDate(0, 0, 7*interval)
	for i := 0; i < 7; i++ {
		c := target.AddDate(0, 0, i)
		if selected[int(c.Weekday())] {
			return c
		}
	}
	return d.AddDate(0, 0, 7*interval)
}

// startOfWeek returns the Monday on or before d.
func startOfWeek(d time.Time) time.Time {
	offset := (int(d.Weekday()) + 6) % 7 // Monday=0 … Sunday=6
	return d.AddDate(0, 0, -offset)
}

// addMonthsClamped moves d forward by months and lands on anchorDay, clamped
// to the length of the resulting month (Jan 31 + 1 month => Feb 28/29).
func addMonthsClamped(d time.Time, months, anchorDay int) time.Time {
	first := time.Date(d.Year(), d.Month(), 1, 0, 0, 0, 0, time.UTC).AddDate(0, months, 0)
	last := first.AddDate(0, 1, -1).Day()
	day := anchorDay
	if day > last {
		day = last
	}
	if day < 1 {
		day = 1
	}
	return time.Date(first.Year(), first.Month(), day, 0, 0, 0, 0, time.UTC)
}

func dateOnly(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
}

// TodayIn returns the current calendar date in the named IANA timezone
// (UTC when the name is empty or unknown).
func TodayIn(tz string, now time.Time) time.Time {
	loc, err := time.LoadLocation(strings.TrimSpace(tz))
	if err != nil || strings.TrimSpace(tz) == "" {
		loc = time.UTC
	}
	return dateOnly(now.In(loc))
}

var weekdayShort = []string{"Sun", "Mon", "Tue", "Wed", "Thu", "Fri", "Sat"}

// Summary renders a short human description, e.g. "Every 2 weeks on Mon, Thu".
func (r Rule) Summary() string {
	unit := map[string]string{Daily: "day", Weekly: "week", Monthly: "month", Yearly: "year"}[r.Frequency]
	if unit == "" {
		return ""
	}
	var b strings.Builder
	if r.Interval <= 1 {
		switch r.Frequency {
		case Daily:
			b.WriteString("Daily")
		case Weekly:
			b.WriteString("Weekly")
		case Monthly:
			b.WriteString("Monthly")
		case Yearly:
			b.WriteString("Yearly")
		}
	} else {
		fmt.Fprintf(&b, "Every %d %ss", r.Interval, unit)
	}
	if r.Frequency == Weekly && len(r.Weekdays) > 0 {
		names := make([]string, 0, len(r.Weekdays))
		for _, d := range uniqueSortedWeekdays(r.Weekdays) {
			names = append(names, weekdayShort[d])
		}
		b.WriteString(" on " + strings.Join(names, ", "))
	}
	if r.Frequency == Monthly && r.MonthDay > 0 {
		if r.MonthDay >= 31 {
			b.WriteString(" on the last day")
		} else {
			b.WriteString(" on the " + ordinal(r.MonthDay))
		}
	}
	if r.Basis == BasisCompletion {
		b.WriteString(" after completion")
	}
	if r.EndsOn != "" {
		b.WriteString(" until " + r.EndsOn)
	} else if r.EndAfter > 0 {
		fmt.Fprintf(&b, ", %d times", r.EndAfter)
	}
	return b.String()
}

func ordinal(n int) string {
	suffix := "th"
	if n%100 < 11 || n%100 > 13 {
		switch n % 10 {
		case 1:
			suffix = "st"
		case 2:
			suffix = "nd"
		case 3:
			suffix = "rd"
		}
	}
	return fmt.Sprintf("%d%s", n, suffix)
}

func uniqueSortedWeekdays(in []int) []int {
	seen := map[int]bool{}
	out := make([]int, 0, len(in))
	for _, d := range in {
		if !seen[d] {
			seen[d] = true
			out = append(out, d)
		}
	}
	sort.Ints(out)
	return out
}
