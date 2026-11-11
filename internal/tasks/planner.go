package tasks

import (
	"context"
	"database/sql"
	"strconv"
	"time"

	"GoTodo/internal/storage"
)

// PlannerMaxDays caps how far ahead Upcoming looks.
const PlannerMaxDays = 60

// plannerMaxTasks caps rows loaded for one planner.
const plannerMaxTasks = 500

// PlannerTask is an open task in the caller's planner, with their snooze.
type PlannerTask struct {
	Task
	SnoozedUntil string // YYYY-MM-DD or ""
}

// PlannerDay is one day in Upcoming.
type PlannerDay struct {
	Date  string
	Tasks []PlannerTask
}

// Planner groups the caller's open tasks for a Today / Upcoming view.
type Planner struct {
	Today    string
	Overdue  []PlannerTask
	DueToday []PlannerTask
	// Woken are tasks whose snooze has run out: they come back to Today until
	// the user completes them or clears the snooze.
	Woken    []PlannerTask
	Upcoming []PlannerDay
	Snoozed  []PlannerTask
}

// FetchPlanner loads the caller's open tasks (the same set as the home list:
// personal tasks, classic project tasks, and kanban tasks they claimed) that
// are due within days of today, or snoozed. today is YYYY-MM-DD in the
// caller's timezone.
func FetchPlanner(userID int, timezone, today string, days int) (*Planner, error) {
	if days < 1 {
		days = 1
	}
	if days > PlannerMaxDays {
		days = PlannerMaxDays
	}
	day, err := time.Parse("2006-01-02", today)
	if err != nil {
		return nil, err
	}
	horizon := day.AddDate(0, 0, days).Format("2006-01-02")

	pool, err := storage.OpenDatabase()
	if err != nil {
		return nil, err
	}
	defer storage.CloseDatabase(pool)

	args := []any{timezone, userID, horizon}
	where := `WHERE ` + storage.TaskHomeVisibleCondition("t", "$2") + `
		AND ` + storage.TaskNotTrashedSQL("t") + `
		AND NOT ` + storage.ArchivedTaskExistsSQL("t.id") + `
		AND NOT COALESCE(t.completed, false)
		AND (t.due_date <= $3::date OR sz.until IS NOT NULL)`
	where, args = appendMineClaimScope(where, args, userID, "t")

	rows, err := pool.Query(context.Background(),
		`SELECT t.id, t.title, t.description, t.completed,
			TO_CHAR((t.time_stamp AT TIME ZONE 'UTC') AT TIME ZONE $1, 'YYYY/MM/DD HH:MI AM'),
			COALESCE(CAST(t.due_date AS TEXT), ''),
			TO_CHAR((t.time_stamp AT TIME ZONE 'UTC') AT TIME ZONE $1, 'YYYY/MM/DD HH:MI AM'),
			COALESCE(TO_CHAR((t.date_modified AT TIME ZONE 'UTC') AT TIME ZONE $1, 'YYYY/MM/DD HH:MI AM'), ''),
			COALESCE(t.is_favorite,false), COALESCE(t.position,0), COALESCE(t.priority,0), t.project_id, COALESCE(p.name,''), t.parent_id,
			COALESCE(CAST(sz.until AS TEXT), '')
		FROM tasks t
		LEFT JOIN projects p ON t.project_id = p.id
		LEFT JOIN task_snoozes sz ON sz.task_id = t.id AND sz.user_id = $2
		`+where+`
		ORDER BY t.due_date NULLS LAST, COALESCE(t.priority,0) DESC, t.id
		LIMIT `+strconv.Itoa(plannerMaxTasks), args...)
	if err != nil {
		return nil, err
	}
	var list []Task
	var snoozes []string
	for rows.Next() {
		var t Task
		var projectID, parentID sql.NullInt64
		var until string
		if err := rows.Scan(&t.ID, &t.Title, &t.Description, &t.Completed,
			&t.DateAdded, &t.DueDate, &t.DateCreated, &t.DateModified,
			&t.IsFavorite, &t.Position, &t.Priority, &projectID, &t.ProjectName, &parentID, &until); err != nil {
			rows.Close()
			return nil, err
		}
		if projectID.Valid {
			t.ProjectID = int(projectID.Int64)
		}
		if parentID.Valid {
			t.ParentID = int(parentID.Int64)
		}
		list = append(list, t)
		snoozes = append(snoozes, until)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if err := attachTagsToTasks(list); err != nil {
		return nil, err
	}

	items := make([]PlannerTask, len(list))
	for i := range list {
		items[i] = PlannerTask{Task: list[i], SnoozedUntil: snoozes[i]}
	}
	return groupPlanner(items, today, horizon), nil
}

// groupPlanner sorts tasks into planner sections. Dates are YYYY-MM-DD, so
// string comparison is date order.
func groupPlanner(items []PlannerTask, today, horizon string) *Planner {
	p := &Planner{
		Today:    today,
		Overdue:  []PlannerTask{},
		DueToday: []PlannerTask{},
		Woken:    []PlannerTask{},
		Upcoming: []PlannerDay{},
		Snoozed:  []PlannerTask{},
	}
	byDay := map[string]int{}
	for _, it := range items {
		due := it.DueDate
		if len(due) > 10 {
			due = due[:10]
		}
		switch {
		case it.SnoozedUntil != "" && it.SnoozedUntil > today:
			p.Snoozed = append(p.Snoozed, it)
		case due != "" && due < today:
			p.Overdue = append(p.Overdue, it)
		case due == today:
			p.DueToday = append(p.DueToday, it)
		case it.SnoozedUntil != "":
			p.Woken = append(p.Woken, it)
		case due != "" && due <= horizon:
			idx, ok := byDay[due]
			if !ok {
				idx = len(p.Upcoming)
				byDay[due] = idx
				p.Upcoming = append(p.Upcoming, PlannerDay{Date: due})
			}
			p.Upcoming[idx].Tasks = append(p.Upcoming[idx].Tasks, it)
		}
	}
	return p
}
