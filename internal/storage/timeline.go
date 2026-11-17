package storage

import (
	"context"
)

// TimelineMaxTasks caps how many dated tasks one timeline returns.
const TimelineMaxTasks = 2000

// TimelineTask is a dated task on a project timeline.
type TimelineTask struct {
	ID            int
	Title         string
	StartDate     string // YYYY-MM-DD or ""
	DueDate       string // YYYY-MM-DD or ""
	Completed     bool
	Priority      int
	ParentID      int
	StatusName    string
	ClaimedByName string
}

// TimelineLink is a "from blocks to" dependency between two timeline tasks.
type TimelineLink struct {
	FromTaskID int
	ToTaskID   int
}

// ListProjectTimeline returns the project's live (not trashed or archived)
// tasks that have a start or due date, the blocking links among them, how many
// live tasks have no dates, and whether the task list was cut at the cap.
func ListProjectTimeline(projectID int) ([]TimelineTask, []TimelineLink, int, bool, error) {
	pool, err := OpenDatabase()
	if err != nil {
		return nil, nil, 0, false, err
	}
	defer CloseDatabase(pool)
	ctx := context.Background()

	rows, err := pool.Query(ctx, `
		SELECT t.id, t.title,
		       COALESCE(CAST(t.start_date AS TEXT), ''), COALESCE(CAST(t.due_date AS TEXT), ''),
		       COALESCE(t.completed, false), COALESCE(t.priority, 0), COALESCE(t.parent_id, 0),
		       COALESCE(s.name, ''),
		       COALESCE(NULLIF(cu.full_name, ''), NULLIF(cu.user_name, ''), cu.email, '')
		FROM tasks t
		LEFT JOIN project_statuses s ON s.id = t.status_id
		LEFT JOIN users cu ON cu.id = t.claimed_by
		WHERE `+liveProjectTaskSQL("t")+`
		  AND (t.start_date IS NOT NULL OR t.due_date IS NOT NULL)
		ORDER BY COALESCE(t.start_date, t.due_date), t.id
		LIMIT $2`, projectID, TimelineMaxTasks+1)
	if err != nil {
		return nil, nil, 0, false, err
	}
	var items []TimelineTask
	for rows.Next() {
		var it TimelineTask
		if err := rows.Scan(&it.ID, &it.Title, &it.StartDate, &it.DueDate, &it.Completed, &it.Priority,
			&it.ParentID, &it.StatusName, &it.ClaimedByName); err != nil {
			rows.Close()
			return nil, nil, 0, false, err
		}
		items = append(items, it)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, nil, 0, false, err
	}
	truncated := len(items) > TimelineMaxTasks
	if truncated {
		items = items[:TimelineMaxTasks]
	}

	var links []TimelineLink
	if len(items) > 0 {
		ids := make([]int, len(items))
		for i, it := range items {
			ids[i] = it.ID
		}
		lrows, err := pool.Query(ctx, `
			SELECT from_task_id, to_task_id FROM task_links
			WHERE link_type = 'blocks' AND from_task_id = ANY($1) AND to_task_id = ANY($1)
			ORDER BY id`, ids)
		if err != nil {
			return nil, nil, 0, false, err
		}
		for lrows.Next() {
			var l TimelineLink
			if err := lrows.Scan(&l.FromTaskID, &l.ToTaskID); err != nil {
				lrows.Close()
				return nil, nil, 0, false, err
			}
			links = append(links, l)
		}
		lrows.Close()
		if err := lrows.Err(); err != nil {
			return nil, nil, 0, false, err
		}
	}

	var undated int
	if err := pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM tasks t
		WHERE `+liveProjectTaskSQL("t")+` AND t.start_date IS NULL AND t.due_date IS NULL
		  AND NOT COALESCE(t.completed, false)`, projectID).Scan(&undated); err != nil {
		return nil, nil, 0, false, err
	}
	return items, links, undated, truncated, nil
}
