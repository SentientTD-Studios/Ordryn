package storage

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// ProjectTaskSummary counts a project's live (not trashed, not archived) tasks.
type ProjectTaskSummary struct {
	Open       int
	Done       int
	Overdue    int
	Unclaimed  int
	OpenPoints int
}

// WeekThroughput is how many tasks (and estimate points) were completed in a week.
type WeekThroughput struct {
	WeekStart time.Time
	Tasks     int
	Points    int
}

// MemberWorkload is one person's claimed work and recent completions in a project.
type MemberWorkload struct {
	UserID        int
	ClaimedOpen   int
	ClaimedPoints int
	Completed     int
}

// SprintTaskState is a sprint task with the events needed to replay its completion history.
type SprintTaskState struct {
	ID          int
	Points      int
	Completed   bool
	ModifiedAt  *time.Time
	Transitions []CompletionTransition
}

// CompletionTransition is a "completed" or "reopened" event.
type CompletionTransition struct {
	Completed bool
	At        time.Time
}

func liveProjectTaskSQL(alias string) string {
	return fmt.Sprintf("%s.project_id = $1 AND %s AND NOT %s", alias, TaskNotTrashedSQL(alias), ArchivedTaskExistsSQL(alias+".id"))
}

// GetProjectTaskSummary counts open/done/overdue/unclaimed tasks; today is YYYY-MM-DD in the viewer's zone.
func GetProjectTaskSummary(projectID int, today string) (ProjectTaskSummary, error) {
	var s ProjectTaskSummary
	pool, err := OpenDatabase()
	if err != nil {
		return s, err
	}
	defer CloseDatabase(pool)
	err = pool.QueryRow(context.Background(), `
		SELECT
			COUNT(*) FILTER (WHERE NOT COALESCE(t.completed, false)),
			COUNT(*) FILTER (WHERE COALESCE(t.completed, false)),
			COUNT(*) FILTER (WHERE NOT COALESCE(t.completed, false) AND t.due_date < $2::date),
			COUNT(*) FILTER (WHERE NOT COALESCE(t.completed, false) AND t.claimed_by IS NULL),
			COALESCE(SUM(t.estimate_points) FILTER (WHERE NOT COALESCE(t.completed, false)), 0)
		FROM tasks t
		WHERE `+liveProjectTaskSQL("t"), projectID, today).
		Scan(&s.Open, &s.Done, &s.Overdue, &s.Unclaimed, &s.OpenPoints)
	return s, err
}

// GetProjectWeeklyThroughput returns completions per Monday-start week, oldest first,
// for the given number of weeks ending with the current week in tz.
func GetProjectWeeklyThroughput(projectID int, tz string, weeks int) ([]WeekThroughput, error) {
	pool, err := OpenDatabase()
	if err != nil {
		return nil, err
	}
	defer CloseDatabase(pool)
	rows, err := pool.Query(context.Background(), `
		WITH done AS (
			SELECT DISTINCT te.task_id,
				date_trunc('week', ((te.created_at AT TIME ZONE 'UTC') AT TIME ZONE $2))::date AS week
			FROM task_events te
			JOIN tasks t ON t.id = te.task_id
			WHERE te.event_type = 'completed' AND `+liveProjectTaskSQL("t")+`
			  AND ((te.created_at AT TIME ZONE 'UTC') AT TIME ZONE $2) >=
			      date_trunc('week', (NOW() AT TIME ZONE $2)) - make_interval(weeks => $3 - 1)
		)
		SELECT w::date, COUNT(d.task_id), COALESCE(SUM(t.estimate_points), 0)
		FROM generate_series(
			date_trunc('week', (NOW() AT TIME ZONE $2)) - make_interval(weeks => $3 - 1),
			date_trunc('week', (NOW() AT TIME ZONE $2)),
			'1 week'::interval
		) AS w
		LEFT JOIN done d ON d.week = w::date
		LEFT JOIN tasks t ON t.id = d.task_id
		GROUP BY w
		ORDER BY w`, projectID, tz, weeks)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []WeekThroughput
	for rows.Next() {
		var wt WeekThroughput
		if err := rows.Scan(&wt.WeekStart, &wt.Tasks, &wt.Points); err != nil {
			return nil, err
		}
		out = append(out, wt)
	}
	return out, rows.Err()
}

// GetProjectMemberWorkload returns claimed open work per claimer and tasks each
// person completed since the given time, keyed by user id.
func GetProjectMemberWorkload(projectID int, since time.Time) (map[int]*MemberWorkload, error) {
	pool, err := OpenDatabase()
	if err != nil {
		return nil, err
	}
	defer CloseDatabase(pool)
	ctx := context.Background()
	out := map[int]*MemberWorkload{}
	get := func(id int) *MemberWorkload {
		if m, ok := out[id]; ok {
			return m
		}
		m := &MemberWorkload{UserID: id}
		out[id] = m
		return m
	}

	rows, err := pool.Query(ctx, `
		SELECT t.claimed_by, COUNT(*), COALESCE(SUM(t.estimate_points), 0)
		FROM tasks t
		WHERE `+liveProjectTaskSQL("t")+` AND t.claimed_by IS NOT NULL AND NOT COALESCE(t.completed, false)
		GROUP BY t.claimed_by`, projectID)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var id, n, pts int
		if err := rows.Scan(&id, &n, &pts); err != nil {
			rows.Close()
			return nil, err
		}
		m := get(id)
		m.ClaimedOpen, m.ClaimedPoints = n, pts
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// Only count a completion that still stands: the task is done now.
	rows, err = pool.Query(ctx, `
		SELECT te.user_id, COUNT(DISTINCT te.task_id)
		FROM task_events te
		JOIN tasks t ON t.id = te.task_id
		WHERE te.event_type = 'completed' AND te.created_at >= $2
		  AND COALESCE(t.completed, false) AND `+liveProjectTaskSQL("t")+`
		GROUP BY te.user_id`, projectID, since.UTC())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id, n int
		if err := rows.Scan(&id, &n); err != nil {
			return nil, err
		}
		get(id).Completed = n
	}
	return out, rows.Err()
}

// ListSprintTaskStates returns the live tasks currently in a sprint with their
// completed/reopened history, oldest event first.
func ListSprintTaskStates(projectID, sprintID int) ([]SprintTaskState, error) {
	pool, err := OpenDatabase()
	if err != nil {
		return nil, err
	}
	defer CloseDatabase(pool)
	ctx := context.Background()
	rows, err := pool.Query(ctx, `
		SELECT t.id, COALESCE(t.estimate_points, 0), COALESCE(t.completed, false), COALESCE(t.date_modified, t.time_stamp)
		FROM tasks t
		WHERE `+liveProjectTaskSQL("t")+` AND t.sprint_id = $2
		ORDER BY t.id`, projectID, sprintID)
	if err != nil {
		return nil, err
	}
	var tasks []SprintTaskState
	index := map[int]int{}
	var ids []int
	for rows.Next() {
		var st SprintTaskState
		var mod sql.NullTime
		if err := rows.Scan(&st.ID, &st.Points, &st.Completed, &mod); err != nil {
			rows.Close()
			return nil, err
		}
		st.ModifiedAt = nullTimePtr(mod)
		index[st.ID] = len(tasks)
		ids = append(ids, st.ID)
		tasks = append(tasks, st)
	}
	rows.Close()
	if err := rows.Err(); err != nil || len(ids) == 0 {
		return tasks, err
	}

	ev, err := pool.Query(ctx, `
		SELECT task_id, event_type = 'completed', created_at
		FROM task_events
		WHERE task_id = ANY($1) AND event_type IN ('completed', 'reopened')
		ORDER BY created_at, id`, ids)
	if err != nil {
		return nil, err
	}
	defer ev.Close()
	for ev.Next() {
		var id int
		var tr CompletionTransition
		if err := ev.Scan(&id, &tr.Completed, &tr.At); err != nil {
			return nil, err
		}
		if i, ok := index[id]; ok {
			tasks[i].Transitions = append(tasks[i].Transitions, tr)
		}
	}
	return tasks, ev.Err()
}
