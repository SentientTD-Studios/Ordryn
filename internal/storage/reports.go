package storage

import (
	"context"
	"time"
)

// personNameSQL is how reports name a person: "First Last", else username, else email.
func personNameSQL(alias string) string {
	return `COALESCE(NULLIF(TRIM(CONCAT_WS(' ', ` + alias + `.first_name, ` + alias + `.last_name)), ''), ` +
		alias + `.user_name, ` + alias + `.email, '')`
}

// TimeEntryRow is one logged time entry with its task, project, and person.
type TimeEntryRow struct {
	ID          int
	LoggedAt    time.Time
	UserID      int
	UserName    string
	ProjectID   int
	ProjectName string
	TaskID      int
	TaskTitle   string
	Minutes     int
	Note        string
}

// TimeEntryQuery selects entries for a report. Entries come from VisibleProjects
// and are either the viewer's own or on a project in PeopleProjects (where the
// viewer may see everyone's work).
type TimeEntryQuery struct {
	ViewerID        int
	VisibleProjects []int
	PeopleProjects  []int
	From, To        time.Time // [From, To)
	UserID          int       // 0 = anyone the viewer may see
	ProjectID       int       // 0 = all visible projects
	Limit           int
}

// QueryTimeEntries returns matching entries, newest first.
func QueryTimeEntries(q TimeEntryQuery) ([]TimeEntryRow, error) {
	out := []TimeEntryRow{}
	if len(q.VisibleProjects) == 0 {
		return out, nil
	}
	if q.Limit <= 0 || q.Limit > 10000 {
		q.Limit = 5000
	}
	pool, err := OpenDatabase()
	if err != nil {
		return nil, err
	}
	defer CloseDatabase(pool)

	rows, err := pool.Query(context.Background(), `
		SELECT e.id, e.created_at, e.user_id, `+personNameSQL("u")+`, t.project_id, COALESCE(p.name, ''),
		       t.id, COALESCE(t.title, ''), e.minutes, e.note
		FROM task_time_entries e
		JOIN tasks t ON t.id = e.task_id
		JOIN projects p ON p.id = t.project_id
		JOIN users u ON u.id = e.user_id
		WHERE t.project_id = ANY($1)
		  AND (e.user_id = $2 OR t.project_id = ANY($3))
		  AND e.created_at >= $4 AND e.created_at < $5
		  AND ($6 = 0 OR e.user_id = $6)
		  AND ($7 = 0 OR t.project_id = $7)
		  AND `+TaskNotTrashedSQL("t")+`
		ORDER BY e.created_at DESC, e.id DESC
		LIMIT $8`,
		q.VisibleProjects, q.ViewerID, q.PeopleProjects, q.From, q.To, q.UserID, q.ProjectID, q.Limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var r TimeEntryRow
		if err := rows.Scan(&r.ID, &r.LoggedAt, &r.UserID, &r.UserName, &r.ProjectID, &r.ProjectName,
			&r.TaskID, &r.TaskTitle, &r.Minutes, &r.Note); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// WorkloadRow is one person's open and recent work across projects.
type WorkloadRow struct {
	UserID        int
	UserName      string
	ClaimedOpen   int
	ClaimedPoints int
	ClaimedOver   int // claimed and overdue
	DoneLastWeek  int
	MinutesWeek   int // time logged in the last 7 days
	Projects      int // projects they have open claims on
}

// GetCrossProjectWorkload sums claimed work per person over projectIDs. today is
// YYYY-MM-DD in the viewer's zone.
func GetCrossProjectWorkload(projectIDs []int, today string) ([]WorkloadRow, error) {
	out := []WorkloadRow{}
	if len(projectIDs) == 0 {
		return out, nil
	}
	pool, err := OpenDatabase()
	if err != nil {
		return nil, err
	}
	defer CloseDatabase(pool)

	live := `t.project_id = ANY($1) AND ` + TaskNotTrashedSQL("t") + ` AND NOT ` + ArchivedTaskExistsSQL("t.id")
	rows, err := pool.Query(context.Background(), `
		WITH claimed AS (
			SELECT t.claimed_by AS uid,
			       COUNT(*) AS open,
			       COALESCE(SUM(t.estimate_points), 0) AS points,
			       COUNT(*) FILTER (WHERE t.due_date < $2::date) AS overdue,
			       COUNT(DISTINCT t.project_id) AS projects
			FROM tasks t
			WHERE `+live+` AND t.claimed_by IS NOT NULL AND NOT COALESCE(t.completed, false)
			GROUP BY t.claimed_by
		), done AS (
			SELECT te.user_id AS uid, COUNT(DISTINCT te.task_id) AS n
			FROM task_events te JOIN tasks t ON t.id = te.task_id
			WHERE `+live+` AND te.event_type = 'completed' AND te.created_at >= NOW() - INTERVAL '7 days'
			  AND COALESCE(t.completed, false)
			GROUP BY te.user_id
		), logged AS (
			SELECT e.user_id AS uid, SUM(e.minutes) AS minutes
			FROM task_time_entries e JOIN tasks t ON t.id = e.task_id
			WHERE `+live+` AND e.created_at >= NOW() - INTERVAL '7 days'
			GROUP BY e.user_id
		), people AS (
			SELECT uid FROM claimed UNION SELECT uid FROM done UNION SELECT uid FROM logged
		)
		SELECT u.id, `+personNameSQL("u")+`,
		       COALESCE(c.open, 0), COALESCE(c.points, 0), COALESCE(c.overdue, 0),
		       COALESCE(d.n, 0), COALESCE(l.minutes, 0), COALESCE(c.projects, 0)
		FROM people x
		JOIN users u ON u.id = x.uid
		LEFT JOIN claimed c ON c.uid = x.uid
		LEFT JOIN done d ON d.uid = x.uid
		LEFT JOIN logged l ON l.uid = x.uid
		WHERE NOT COALESCE(u.is_agent, FALSE) AND NOT COALESCE(u.is_system, FALSE)
		ORDER BY COALESCE(c.open, 0) DESC, 2`, projectIDs, today)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var r WorkloadRow
		if err := rows.Scan(&r.UserID, &r.UserName, &r.ClaimedOpen, &r.ClaimedPoints, &r.ClaimedOver,
			&r.DoneLastWeek, &r.MinutesWeek, &r.Projects); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}
