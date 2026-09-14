package domain

import (
	"fmt"
	"log"
	"regexp"
	"strconv"
	"strings"
	"time"

	"GoTodo/internal/storage"
)

var trailingSprintNumber = regexp.MustCompile(`^(.*?)(\d+)$`)

func normalizeAutoSprintPatch(proj *storage.ProjectWithAccess, auto *AutoSprintPatch, workflowMode *string) (AutoSprintPatch, error) {
	if auto.empty() {
		return AutoSprintPatch{}, nil
	}

	enabled := proj.AutoCreateNextSprint
	if auto.Enabled != nil {
		enabled = *auto.Enabled
	}

	length := proj.AutoSprintLengthDays
	if auto.LengthDays != nil {
		if *auto.LengthDays == nil {
			length = nil
		} else {
			n := **auto.LengthDays
			if n < storage.MinAutoSprintLengthDays || n > storage.MaxAutoSprintLengthDays {
				return AutoSprintPatch{}, fmt.Errorf("%w: sprint length must be between %d and %d days", ErrValidation, storage.MinAutoSprintLengthDays, storage.MaxAutoSprintLengthDays)
			}
			length = &n
		}
	}

	lock := proj.AutoSprintLockDaysBefore
	if auto.LockDaysBefore != nil {
		if *auto.LockDaysBefore == nil {
			lock = nil
		} else {
			n := **auto.LockDaysBefore
			if n < 0 {
				return AutoSprintPatch{}, fmt.Errorf("%w: lock days before end cannot be negative", ErrValidation)
			}
			lock = &n
		}
	}

	if enabled {
		mode := proj.WorkflowMode
		if workflowMode != nil {
			mode = strings.TrimSpace(strings.ToLower(*workflowMode))
		}
		if mode == "" {
			mode = storage.WorkflowClassic
		}
		switchingOffKanban := workflowMode != nil && mode == storage.WorkflowClassic
		if mode != storage.WorkflowKanban && !switchingOffKanban {
			return AutoSprintPatch{}, fmt.Errorf("%w: auto-create next sprint requires a kanban project", ErrValidation)
		}
		if !switchingOffKanban && length == nil {
			return AutoSprintPatch{}, fmt.Errorf("%w: sprint length is required when auto-create next sprint is enabled", ErrValidation)
		}
	}
	if length != nil && lock != nil && *lock >= *length {
		return AutoSprintPatch{}, fmt.Errorf("%w: lock days before end must be less than sprint length", ErrValidation)
	}

	out := AutoSprintPatch{}
	if auto.Enabled != nil {
		out.Enabled = auto.Enabled
	}
	if auto.LengthDays != nil {
		out.LengthDays = auto.LengthDays
	}
	if auto.LockDaysBefore != nil {
		out.LockDaysBefore = auto.LockDaysBefore
	}
	return out, nil
}

func autoSprintSettingsChanged(proj *storage.ProjectWithAccess, patch AutoSprintPatch) bool {
	if patch.Enabled != nil && *patch.Enabled != proj.AutoCreateNextSprint {
		return true
	}
	if patch.LengthDays != nil && !intPtrsEqual(derefIntPtr(patch.LengthDays), proj.AutoSprintLengthDays) {
		return true
	}
	if patch.LockDaysBefore != nil && !intPtrsEqual(derefIntPtr(patch.LockDaysBefore), proj.AutoSprintLockDaysBefore) {
		return true
	}
	return false
}

func derefIntPtr(p **int) *int {
	if p == nil {
		return nil
	}
	return *p
}

func intPtrsEqual(a, b *int) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	return *a == *b
}

// NextAutoSprintWindow returns the next inclusive sprint range after prevEnd.
// A 30-day sprint starting the day after 2026-08-31 is 2026-09-01 through 2026-09-30.
// lockDaysBefore of 7 yields a lock date of 2026-09-23.
func NextAutoSprintWindow(prevEnd time.Time, lengthDays int, lockDaysBefore *int) (start, end time.Time, lock *time.Time, err error) {
	if lengthDays < storage.MinAutoSprintLengthDays || lengthDays > storage.MaxAutoSprintLengthDays {
		return time.Time{}, time.Time{}, nil, fmt.Errorf("%w: sprint length must be between %d and %d days", ErrValidation, storage.MinAutoSprintLengthDays, storage.MaxAutoSprintLengthDays)
	}
	start = calendarDateUTC(prevEnd).AddDate(0, 0, 1)
	end = start.AddDate(0, 0, lengthDays-1)
	if lockDaysBefore != nil {
		if *lockDaysBefore < 0 || *lockDaysBefore >= lengthDays {
			return time.Time{}, time.Time{}, nil, fmt.Errorf("%w: lock days before end must be less than sprint length", ErrValidation)
		}
		ld := end.AddDate(0, 0, -*lockDaysBefore)
		lock = &ld
	}
	return start, end, lock, nil
}

func calendarDateUTC(t time.Time) time.Time {
	y, m, d := t.UTC().Date()
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}

func sprintHasEnded(end *time.Time, now time.Time) bool {
	if end == nil {
		return false
	}
	return storage.FormatSprintDate(now) > storage.FormatSprintDate(*end)
}

func sprintWindowAlreadyEnded(end time.Time, now time.Time) bool {
	return storage.FormatSprintDate(now) > storage.FormatSprintDate(end)
}

// NextAutoSprintName increments a trailing number ("Sprint 1" -> "Sprint 2")
// or appends " 2" when the previous name has no number.
func NextAutoSprintName(previous string, existingNames []string) string {
	taken := make(map[string]struct{}, len(existingNames))
	for _, n := range existingNames {
		taken[strings.ToLower(strings.TrimSpace(n))] = struct{}{}
	}
	candidate := incrementTrailingSprintNumber(strings.TrimSpace(previous))
	if candidate == "" {
		candidate = "Sprint 1"
	}
	for i := 0; i < storage.MaxProjectSprints+10; i++ {
		if _, ok := taken[strings.ToLower(candidate)]; !ok {
			if len(candidate) > storage.MaxSprintNameLen {
				candidate = strings.TrimSpace(candidate[:storage.MaxSprintNameLen])
			}
			if _, ok := taken[strings.ToLower(candidate)]; !ok && candidate != "" {
				return candidate
			}
		}
		candidate = incrementTrailingSprintNumber(candidate)
	}
	return "Sprint " + storage.FormatSprintDate(time.Now().UTC())
}

func incrementTrailingSprintNumber(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return "Sprint 1"
	}
	m := trailingSprintNumber.FindStringSubmatch(name)
	if m == nil {
		return name + " 2"
	}
	n, err := strconv.Atoi(m[2])
	if err != nil {
		return name + " 2"
	}
	return m[1] + strconv.Itoa(n+1)
}

// StartAutoSprintWorker creates due follow-up sprints on an hourly ticker.
func StartAutoSprintWorker() {
	go func() {
		runAutoSprintPass()
		ticker := time.NewTicker(time.Hour)
		defer ticker.Stop()
		for range ticker.C {
			runAutoSprintPass()
		}
	}()
}

func runAutoSprintPass() {
	n, err := AutoCreateDueSprints(time.Now().UTC())
	if err != nil {
		log.Printf("auto-sprint: %v", err)
		return
	}
	if n > 0 {
		log.Printf("auto-sprint: created %d sprint(s)", n)
	}
}

// AutoCreateDueSprints creates the next sprint for projects whose current sprint
// has ended and still has active items.
func AutoCreateDueSprints(now time.Time) (int, error) {
	projects, err := storage.ListKanbanProjectsWithAutoSprint()
	if err != nil {
		return 0, err
	}
	created := 0
	var firstErr error
	for _, p := range projects {
		s, err := AutoCreateDueSprintsForProject(p, now)
		if err != nil {
			log.Printf("auto-sprint: project %d: %v", p.ID, err)
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		if s != nil {
			created++
		}
	}
	return created, firstErr
}

// AutoCreateDueSprintsForProject creates the next sprint when the latest dated
// sprint has ended with items. It is a no-op when settings or sprint state
// do not qualify.
func AutoCreateDueSprintsForProject(project storage.Project, now time.Time) (*storage.ProjectSprint, error) {
	if !project.AutoCreateNextSprint || project.Archived || project.WorkflowMode != storage.WorkflowKanban {
		return nil, nil
	}
	if project.AutoSprintLengthDays == nil {
		return nil, nil
	}

	current, err := storage.GetLatestDatedProjectSprint(project.ID)
	if err != nil {
		return nil, err
	}
	if current == nil || current.EndDate == nil {
		return nil, nil
	}
	if !sprintHasEnded(current.EndDate, now) {
		return nil, nil
	}

	items, err := storage.CountActiveTasksWithSprint(current.ID)
	if err != nil {
		return nil, err
	}
	if items == 0 {
		return nil, nil
	}

	start, end, lock, err := NextAutoSprintWindow(*current.EndDate, *project.AutoSprintLengthDays, project.AutoSprintLockDaysBefore)
	if err != nil {
		return nil, err
	}
	if sprintWindowAlreadyEnded(end, now) {
		return nil, nil
	}

	existing, err := storage.ListProjectSprints(project.ID)
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(existing))
	for _, s := range existing {
		names = append(names, s.Name)
	}

	lockRaw := ""
	if lock != nil {
		lockRaw = storage.FormatSprintDate(*lock)
	}
	created, err := createProjectSprintRecord(project.ID, project.UserID, CreateProjectSprintInput{
		Name:      NextAutoSprintName(current.Name, names),
		StartDate: storage.FormatSprintDate(start),
		EndDate:   storage.FormatSprintDate(end),
		LockDate:  lockRaw,
	}, map[string]interface{}{"auto": true})
	if err != nil {
		if strings.Contains(err.Error(), "overlap") {
			return nil, nil
		}
		return nil, err
	}
	return created, nil
}
