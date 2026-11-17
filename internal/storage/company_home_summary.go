package storage

import "context"

// MyWorkSummary counts a person's open work: tasks they claimed, plus tasks
// they created that nobody has claimed. today is YYYY-MM-DD in their timezone.
type MyWorkSummary struct {
	Open     int
	Overdue  int
	DueToday int
	DueWeek  int // due in the next 7 days, today included
}

// GetMyWorkSummary counts live (not trashed or archived) open tasks.
func GetMyWorkSummary(userID int, today string) (MyWorkSummary, error) {
	var s MyWorkSummary
	pool, err := OpenDatabase()
	if err != nil {
		return s, err
	}
	defer CloseDatabase(pool)
	err = pool.QueryRow(context.Background(), `
		SELECT COUNT(*),
		       COUNT(*) FILTER (WHERE t.due_date < $2::date),
		       COUNT(*) FILTER (WHERE t.due_date = $2::date),
		       COUNT(*) FILTER (WHERE t.due_date >= $2::date AND t.due_date < $2::date + 7)
		FROM tasks t
		WHERE (t.claimed_by = $1 OR (t.user_id = $1 AND t.claimed_by IS NULL))
		  AND NOT COALESCE(t.completed, false)
		  AND `+TaskNotTrashedSQL("t")+`
		  AND NOT `+ArchivedTaskExistsSQL("t.id")+`
		  AND `+TaskVisibleCondition("t", "$1"),
		userID, today).Scan(&s.Open, &s.Overdue, &s.DueToday, &s.DueWeek)
	return s, err
}

// ProjectCounts are open and overdue task counts for one project.
type ProjectCounts struct {
	Open    int
	Overdue int
}

// GetProjectCounts returns open/overdue counts for live tasks in the given projects.
func GetProjectCounts(projectIDs []int, today string) (map[int]ProjectCounts, error) {
	out := map[int]ProjectCounts{}
	if len(projectIDs) == 0 {
		return out, nil
	}
	pool, err := OpenDatabase()
	if err != nil {
		return nil, err
	}
	defer CloseDatabase(pool)
	rows, err := pool.Query(context.Background(), `
		SELECT t.project_id, COUNT(*), COUNT(*) FILTER (WHERE t.due_date < $2::date)
		FROM tasks t
		WHERE t.project_id = ANY($1) AND NOT COALESCE(t.completed, false)
		  AND `+TaskNotTrashedSQL("t")+`
		  AND NOT `+ArchivedTaskExistsSQL("t.id")+`
		GROUP BY t.project_id`, projectIDs, today)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id int
		var c ProjectCounts
		if err := rows.Scan(&id, &c.Open, &c.Overdue); err != nil {
			return nil, err
		}
		out[id] = c
	}
	return out, rows.Err()
}

// UserTeamSummary is a team the user is on, with its size.
type UserTeamSummary struct {
	ID      int
	Name    string
	Members int
}

// ListUserTeamSummaries returns the teams (not the company) the user is on.
func ListUserTeamSummaries(userID int) ([]UserTeamSummary, error) {
	pool, err := OpenDatabase()
	if err != nil {
		return nil, err
	}
	defer CloseDatabase(pool)
	rows, err := pool.Query(context.Background(), `
		SELECT o.id, o.name, (SELECT COUNT(*) FROM organization_members m2 WHERE m2.organization_id = o.id)
		FROM organization_members m
		JOIN organizations o ON o.id = m.organization_id
		WHERE m.user_id = $1 AND NOT o.is_company
		ORDER BY LOWER(o.name), o.id`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []UserTeamSummary{}
	for rows.Next() {
		var t UserTeamSummary
		if err := rows.Scan(&t.ID, &t.Name, &t.Members); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}
