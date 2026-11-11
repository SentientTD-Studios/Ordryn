package domain

import (
	"context"
	"fmt"
	"time"

	"GoTodo/internal/storage"
)

// MaxReportRangeDays caps a timesheet's date range.
const MaxReportRangeDays = 366

// reportScope is what a viewer may report on: projects they can open, and the
// subset where they may see everyone's work (View team workload).
type reportScope struct {
	visible []int
	people  []int
	names   map[int]string
}

func reportScopeFor(userID int) (*reportScope, error) {
	projects, err := storage.GetAccessibleProjects(userID)
	if err != nil {
		return nil, err
	}
	s := &reportScope{visible: []int{}, people: []int{}, names: map[int]string{}}
	for _, p := range projects {
		s.visible = append(s.visible, p.ID)
		s.names[p.ID] = p.Name
		if storage.HasProjectPerm(p.ID, p.Role, storage.PermInsightsPeople) {
			s.people = append(s.people, p.ID)
		}
	}
	return s, nil
}

// TimesheetInput filters a timesheet. Dates are YYYY-MM-DD in tz, inclusive.
type TimesheetInput struct {
	From, To  string
	UserID    int
	ProjectID int
	TZ        string
}

// Timesheet is the entries in range plus totals.
type Timesheet struct {
	Entries      []storage.TimeEntryRow
	TotalMinutes int
	ByPerson     map[int]int
	ByProject    map[int]int
	// Truncated is true when the range had more entries than were returned.
	Truncated bool
}

// TimesheetForUser returns logged time the viewer may see: their own on any
// project they can open, and everyone's on projects where they hold View team workload.
func TimesheetForUser(ctx context.Context, userID int, in TimesheetInput) (*Timesheet, error) {
	_ = ctx
	loc, err := time.LoadLocation(in.TZ)
	if err != nil {
		loc = time.UTC
	}
	from, err := time.ParseInLocation("2006-01-02", in.From, loc)
	if err != nil {
		return nil, fmt.Errorf("%w: from must be YYYY-MM-DD", ErrValidation)
	}
	to, err := time.ParseInLocation("2006-01-02", in.To, loc)
	if err != nil {
		return nil, fmt.Errorf("%w: to must be YYYY-MM-DD", ErrValidation)
	}
	if to.Before(from) {
		return nil, fmt.Errorf("%w: to is before from", ErrValidation)
	}
	if to.Sub(from) > MaxReportRangeDays*24*time.Hour {
		return nil, fmt.Errorf("%w: choose a range of at most %d days", ErrValidation, MaxReportRangeDays)
	}
	scope, err := reportScopeFor(userID)
	if err != nil {
		return nil, err
	}
	if in.ProjectID > 0 {
		if _, ok := scope.names[in.ProjectID]; !ok {
			return nil, ErrNotFound
		}
	}
	const limit = 5000
	entries, err := storage.QueryTimeEntries(storage.TimeEntryQuery{
		ViewerID: userID, VisibleProjects: scope.visible, PeopleProjects: scope.people,
		From: from, To: to.AddDate(0, 0, 1), UserID: in.UserID, ProjectID: in.ProjectID, Limit: limit + 1,
	})
	if err != nil {
		return nil, err
	}
	ts := &Timesheet{ByPerson: map[int]int{}, ByProject: map[int]int{}}
	if len(entries) > limit {
		entries = entries[:limit]
		ts.Truncated = true
	}
	ts.Entries = entries
	for _, e := range entries {
		ts.TotalMinutes += e.Minutes
		ts.ByPerson[e.UserID] += e.Minutes
		ts.ByProject[e.ProjectID] += e.Minutes
	}
	return ts, nil
}

// WorkloadForUser returns per-person claimed work across every project where
// the viewer holds View team workload, and how many projects that covers.
func WorkloadForUser(ctx context.Context, userID int, tz string) ([]storage.WorkloadRow, int, error) {
	_ = ctx
	scope, err := reportScopeFor(userID)
	if err != nil {
		return nil, 0, err
	}
	loc, err := time.LoadLocation(tz)
	if err != nil {
		loc = time.UTC
	}
	rows, err := storage.GetCrossProjectWorkload(scope.people, time.Now().In(loc).Format("2006-01-02"))
	return rows, len(scope.people), err
}
