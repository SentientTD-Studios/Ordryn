package storage

import (
	"context"
	"database/sql"
)

// MaxRefinementQueue caps how many tasks one refinement session walks through.
const MaxRefinementQueue = 200

// RefinementTask is the summary a refinement session shows for each queued task.
type RefinementTask struct {
	ID            int
	ParentID      int
	ParentTitle   string
	Title         string
	Priority      int
	Completed     bool
	StatusName    string
	SprintID      int
	SprintName    string
	ClaimedByName string
	EstimateHours *float64
	Refinement    string
	Trashed       bool
}

// RefinementCandidateFilter picks which open tasks a new session queues.
type RefinementCandidateFilter struct {
	// SprintID: nil = any sprint or backlog; 0 = backlog only; n = that sprint.
	SprintID *int
	// IncludeUnflagged also queues open tasks with no refinement flag.
	IncludeUnflagged bool
}

// ListRefinementCandidates returns open, live task ids for a new session in board order:
// tasks flagged "Needs Refinement" first, then (optionally) unflagged ones.
func ListRefinementCandidates(projectID int, f RefinementCandidateFilter) ([]int, error) {
	pool, err := OpenDatabase()
	if err != nil {
		return nil, err
	}
	defer CloseDatabase(pool)

	args := []interface{}{projectID, RefinementNeeded, f.IncludeUnflagged, MaxRefinementQueue}
	sprintCond := ""
	if f.SprintID != nil {
		if *f.SprintID > 0 {
			args = append(args, *f.SprintID)
			sprintCond = " AND t.sprint_id = $5"
		} else {
			sprintCond = " AND t.sprint_id IS NULL"
		}
	}
	rows, err := pool.Query(context.Background(), `
		SELECT t.id
		FROM tasks t
		WHERE `+liveProjectTaskSQL("t")+`
		  AND NOT COALESCE(t.completed, false)
		  AND (t.refinement_state = $2 OR ($3 AND t.refinement_state IS NULL))`+sprintCond+`
		ORDER BY (t.refinement_state IS DISTINCT FROM $2), COALESCE(t.position, 0), t.id
		LIMIT $4`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	ids := []int{}
	for rows.Next() {
		var id int
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// FilterProjectTaskIDs keeps the ids that are tasks in projectID (trashed included), preserving order.
func FilterProjectTaskIDs(projectID int, ids []int) ([]int, error) {
	if len(ids) == 0 {
		return []int{}, nil
	}
	pool, err := OpenDatabase()
	if err != nil {
		return nil, err
	}
	defer CloseDatabase(pool)
	rows, err := pool.Query(context.Background(),
		`SELECT id FROM tasks WHERE project_id = $1 AND id = ANY($2)`, projectID, ids)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	ok := map[int]bool{}
	for rows.Next() {
		var id int
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ok[id] = true
	}
	out := make([]int, 0, len(ids))
	for _, id := range ids {
		if ok[id] {
			out = append(out, id)
		}
	}
	return out, rows.Err()
}

// ListRefinementTasks loads summaries for ids within projectID. Missing ids are skipped.
func ListRefinementTasks(projectID int, ids []int) (map[int]RefinementTask, error) {
	out := make(map[int]RefinementTask, len(ids))
	if len(ids) == 0 {
		return out, nil
	}
	pool, err := OpenDatabase()
	if err != nil {
		return nil, err
	}
	defer CloseDatabase(pool)
	rows, err := pool.Query(context.Background(), `
		SELECT t.id, COALESCE(t.parent_id, 0), COALESCE(pt.title, ''), t.title, COALESCE(t.priority, 0),
		       COALESCE(t.completed, false), COALESCE(s.name, ''), COALESCE(t.sprint_id, 0), COALESCE(sp.name, ''),
		       COALESCE(NULLIF(cu.full_name, ''), NULLIF(cu.user_name, ''), cu.email, ''),
		       t.estimate_hours, COALESCE(t.refinement_state, ''), t.deleted_at IS NOT NULL
		FROM tasks t
		LEFT JOIN tasks pt ON pt.id = t.parent_id
		LEFT JOIN project_statuses s ON s.id = t.status_id
		LEFT JOIN project_sprints sp ON sp.id = t.sprint_id
		LEFT JOIN users cu ON cu.id = t.claimed_by
		WHERE t.project_id = $1 AND t.id = ANY($2)`, projectID, ids)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var r RefinementTask
		var est sql.NullFloat64
		if err := rows.Scan(&r.ID, &r.ParentID, &r.ParentTitle, &r.Title, &r.Priority, &r.Completed, &r.StatusName,
			&r.SprintID, &r.SprintName, &r.ClaimedByName, &est, &r.Refinement, &r.Trashed); err != nil {
			return nil, err
		}
		if est.Valid {
			v := est.Float64
			r.EstimateHours = &v
		}
		out[r.ID] = r
	}
	return out, rows.Err()
}
