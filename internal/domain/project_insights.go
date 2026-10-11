package domain

import (
	"context"
	"fmt"
	"sort"
	"time"

	"GoTodo/internal/storage"
)

const (
	insightsThroughputWeeks = 8
	insightsCompletedDays   = 30
	maxBurndownDays         = 120
)

// ProjectInsights is the team dashboard for one project.
type ProjectInsights struct {
	ProjectID     int
	ProjectName   string
	WorkflowMode  string
	Summary       storage.ProjectTaskSummary
	Throughput    []storage.WeekThroughput
	CompletedDays int
	// CanViewMembers is false when the viewer's role lacks insights:people; Members is then empty.
	CanViewMembers bool
	Members        []MemberInsight
	Sprints        []SprintRef
	Burndown       *SprintBurndown
}

// MemberInsight is one person's share of the project's work.
type MemberInsight struct {
	UserID        int
	Name          string
	IsAgent       bool
	IsMember      bool
	ClaimedOpen   int
	ClaimedPoints int
	Completed     int
}

// SprintRef names a dated sprint that can have a burndown.
type SprintRef struct {
	ID        int
	Name      string
	StartDate string
	EndDate   string
}

// SprintBurndown is remaining work per day across a sprint, from its current scope.
type SprintBurndown struct {
	Sprint      SprintRef
	TotalTasks  int
	TotalPoints int
	Days        []BurndownDay
}

// BurndownDay is remaining work at the end of a day. Future days have no actuals.
type BurndownDay struct {
	Date            string
	RemainingTasks  *int
	RemainingPoints *int
	IdealTasks      float64
	IdealPoints     float64
}

// GetProjectInsightsForUser builds the team dashboard. sprintID 0 picks the
// active sprint, else the most recent dated one.
func GetProjectInsightsForUser(ctx context.Context, userID, projectID, sprintID int, tz string) (*ProjectInsights, error) {
	proj, err := storage.GetAccessibleProjectByID(projectID, userID)
	if err != nil {
		return nil, ErrNotFound
	}
	loc, err := time.LoadLocation(tz)
	if err != nil {
		loc = time.UTC
		tz = "UTC"
	}
	now := time.Now().In(loc)
	today := now.Format("2006-01-02")

	out := &ProjectInsights{
		ProjectID:     proj.ID,
		ProjectName:   proj.Name,
		WorkflowMode:  proj.WorkflowMode,
		CompletedDays: insightsCompletedDays,
		Members:       []MemberInsight{},
		Sprints:       []SprintRef{},
	}
	if out.Summary, err = storage.GetProjectTaskSummary(projectID, today); err != nil {
		return nil, fmt.Errorf("summary: %w", err)
	}
	if out.Throughput, err = storage.GetProjectWeeklyThroughput(projectID, tz, insightsThroughputWeeks); err != nil {
		return nil, fmt.Errorf("throughput: %w", err)
	}
	role, _ := storage.GetProjectRole(projectID, userID)
	out.CanViewMembers = storage.HasProjectPerm(projectID, role, storage.PermInsightsPeople)
	if out.CanViewMembers {
		if out.Members, err = projectMemberInsights(projectID, now.AddDate(0, 0, -insightsCompletedDays)); err != nil {
			return nil, fmt.Errorf("members: %w", err)
		}
	}

	if proj.WorkflowMode != storage.WorkflowKanban {
		return out, nil
	}
	sprints, err := storage.ListProjectSprints(projectID)
	if err != nil {
		return nil, fmt.Errorf("sprints: %w", err)
	}
	var chosen *storage.ProjectSprint
	for i := range sprints {
		s := &sprints[i]
		if s.StartDate == nil || s.EndDate == nil {
			continue
		}
		out.Sprints = append(out.Sprints, sprintRef(s))
		if sprintID > 0 && s.ID == sprintID {
			chosen = s
		}
	}
	if sprintID > 0 && chosen == nil {
		return nil, fmt.Errorf("%w: sprint not found or has no dates", ErrValidation)
	}
	if chosen == nil {
		chosen = defaultInsightsSprint(sprints, today)
	}
	if chosen != nil {
		tasks, err := storage.ListSprintTaskStates(projectID, chosen.ID)
		if err != nil {
			return nil, fmt.Errorf("burndown: %w", err)
		}
		out.Burndown = BuildBurndown(sprintRef(chosen), tasks, loc, now)
	}
	return out, nil
}

func sprintRef(s *storage.ProjectSprint) SprintRef {
	return SprintRef{
		ID:        s.ID,
		Name:      s.Name,
		StartDate: storage.FormatSprintDatePtr(s.StartDate),
		EndDate:   storage.FormatSprintDatePtr(s.EndDate),
	}
}

// defaultInsightsSprint picks the sprint running today, else the one that ended
// most recently, else the next to start.
func defaultInsightsSprint(sprints []storage.ProjectSprint, today string) *storage.ProjectSprint {
	var lastEnded, nextUp *storage.ProjectSprint
	for i := range sprints {
		s := &sprints[i]
		if s.StartDate == nil || s.EndDate == nil {
			continue
		}
		start, end := storage.FormatSprintDate(*s.StartDate), storage.FormatSprintDate(*s.EndDate)
		switch {
		case start <= today && today <= end:
			return s
		case end < today:
			if lastEnded == nil || end > storage.FormatSprintDate(*lastEnded.EndDate) {
				lastEnded = s
			}
		default:
			if nextUp == nil || start < storage.FormatSprintDate(*nextUp.StartDate) {
				nextUp = s
			}
		}
	}
	if lastEnded != nil {
		return lastEnded
	}
	return nextUp
}

func projectMemberInsights(projectID int, since time.Time) ([]MemberInsight, error) {
	members, err := storage.ListProjectMembers(projectID)
	if err != nil {
		return nil, err
	}
	work, err := storage.GetProjectMemberWorkload(projectID, since)
	if err != nil {
		return nil, err
	}
	out := make([]MemberInsight, 0, len(members))
	seen := map[int]bool{}
	for _, m := range members {
		name := m.UserName
		if name == "" {
			name = m.Email
		}
		mi := MemberInsight{UserID: m.UserID, Name: name, IsAgent: m.IsAgent, IsMember: true}
		if w := work[m.UserID]; w != nil {
			mi.ClaimedOpen, mi.ClaimedPoints, mi.Completed = w.ClaimedOpen, w.ClaimedPoints, w.Completed
		}
		seen[m.UserID] = true
		out = append(out, mi)
	}
	// People who left the project can still hold claims or recent completions.
	var former []int
	for id := range work {
		if !seen[id] {
			former = append(former, id)
		}
	}
	if len(former) > 0 {
		names, err := storage.UserDisplayNames(former)
		if err != nil {
			return nil, err
		}
		for _, id := range former {
			w := work[id]
			name := names[id]
			if name == "" {
				name = "Removed user"
			}
			out = append(out, MemberInsight{
				UserID: id, Name: name,
				ClaimedOpen: w.ClaimedOpen, ClaimedPoints: w.ClaimedPoints, Completed: w.Completed,
			})
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if a.ClaimedOpen+a.Completed != b.ClaimedOpen+b.Completed {
			return a.ClaimedOpen+a.Completed > b.ClaimedOpen+b.Completed
		}
		return a.Name < b.Name
	})
	return out, nil
}

// completedAt reports whether a task was complete at instant t, replaying its
// completed/reopened events. Tasks with no events fall back to their current
// state, dated by their last modification.
func completedAt(task storage.SprintTaskState, t time.Time) bool {
	if len(task.Transitions) == 0 {
		if !task.Completed {
			return false
		}
		return task.ModifiedAt == nil || !task.ModifiedAt.After(t)
	}
	state := !task.Transitions[0].Completed
	for _, tr := range task.Transitions {
		if tr.At.After(t) {
			break
		}
		state = tr.Completed
	}
	return state
}

// BuildBurndown computes remaining work at the end of each sprint day in loc.
// It uses the sprint's current tasks for the whole range, so scope added mid-sprint
// counts from day one.
func BuildBurndown(sprint SprintRef, tasks []storage.SprintTaskState, loc *time.Location, now time.Time) *SprintBurndown {
	start, err1 := time.ParseInLocation("2006-01-02", sprint.StartDate, loc)
	end, err2 := time.ParseInLocation("2006-01-02", sprint.EndDate, loc)
	if err1 != nil || err2 != nil || end.Before(start) {
		return nil
	}
	bd := &SprintBurndown{Sprint: sprint, Days: []BurndownDay{}}
	for _, t := range tasks {
		bd.TotalTasks++
		bd.TotalPoints += t.Points
	}
	days := int(end.Sub(start).Hours()/24+0.5) + 1
	if days > maxBurndownDays {
		days = maxBurndownDays
	}
	for i := 0; i < days; i++ {
		day := start.AddDate(0, 0, i)
		frac := 1.0
		if days > 1 {
			frac = 1 - float64(i)/float64(days-1)
		}
		d := BurndownDay{
			Date:        day.Format("2006-01-02"),
			IdealTasks:  float64(bd.TotalTasks) * frac,
			IdealPoints: float64(bd.TotalPoints) * frac,
		}
		if !day.After(now) {
			endOfDay := day.AddDate(0, 0, 1)
			if endOfDay.After(now) {
				endOfDay = now
			}
			remTasks, remPoints := 0, 0
			for _, t := range tasks {
				if !completedAt(t, endOfDay) {
					remTasks++
					remPoints += t.Points
				}
			}
			d.RemainingTasks, d.RemainingPoints = &remTasks, &remPoints
		}
		bd.Days = append(bd.Days, d)
	}
	return bd
}
