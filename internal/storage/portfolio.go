package storage

import (
	"context"
	"fmt"
	"time"
)

// Project health on a status update.
const (
	HealthOnTrack  = "on_track"
	HealthAtRisk   = "at_risk"
	HealthOffTrack = "off_track"
)

// ValidHealth reports whether h is a known health value.
func ValidHealth(h string) bool {
	return h == HealthOnTrack || h == HealthAtRisk || h == HealthOffTrack
}

// ProjectStatusUpdate is a project lead's short "how is it going" post.
type ProjectStatusUpdate struct {
	ID         int
	ProjectID  int
	AuthorID   int
	AuthorName string
	Health     string
	Body       string
	CreatedAt  time.Time
}

// MigrateProjectStatusUpdates creates project_status_updates.
func MigrateProjectStatusUpdates() error {
	pool, err := OpenDatabase()
	if err != nil {
		return err
	}
	defer CloseDatabase(pool)

	for _, s := range []string{
		`CREATE TABLE IF NOT EXISTS project_status_updates (
			id SERIAL PRIMARY KEY,
			project_id INTEGER NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
			author_id INTEGER REFERENCES users(id) ON DELETE SET NULL,
			health TEXT NOT NULL,
			body TEXT NOT NULL DEFAULT '',
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)`,
		`CREATE INDEX IF NOT EXISTS idx_project_status_updates_project ON project_status_updates (project_id, created_at DESC)`,
	} {
		if _, err := pool.Exec(context.Background(), s); err != nil {
			return fmt.Errorf("project_status_updates: %w", err)
		}
	}
	return nil
}

const statusUpdateSelect = `
	SELECT s.id, s.project_id, COALESCE(s.author_id, 0),
	       COALESCE(NULLIF(TRIM(CONCAT_WS(' ', u.first_name, u.last_name)), ''), u.user_name, u.email, ''),
	       s.health, s.body, s.created_at
	FROM project_status_updates s
	LEFT JOIN users u ON u.id = s.author_id`

// CreateProjectStatusUpdate stores an update.
func CreateProjectStatusUpdate(projectID, authorID int, health, body string) (*ProjectStatusUpdate, error) {
	pool, err := OpenDatabase()
	if err != nil {
		return nil, err
	}
	defer CloseDatabase(pool)

	var id int
	if err := pool.QueryRow(context.Background(),
		`INSERT INTO project_status_updates (project_id, author_id, health, body) VALUES ($1, $2, $3, $4) RETURNING id`,
		projectID, authorID, health, body).Scan(&id); err != nil {
		return nil, err
	}
	list, err := queryStatusUpdates(statusUpdateSelect+` WHERE s.id = $1`, id)
	if err != nil || len(list) == 0 {
		return nil, err
	}
	return &list[0], nil
}

// ListProjectStatusUpdates returns a project's updates, newest first.
func ListProjectStatusUpdates(projectID, limit int) ([]ProjectStatusUpdate, error) {
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	return queryStatusUpdates(statusUpdateSelect+` WHERE s.project_id = $1 ORDER BY s.created_at DESC, s.id DESC LIMIT $2`, projectID, limit)
}

// LatestProjectStatusUpdates returns each project's newest update, by project id.
func LatestProjectStatusUpdates(projectIDs []int) (map[int]ProjectStatusUpdate, error) {
	out := map[int]ProjectStatusUpdate{}
	if len(projectIDs) == 0 {
		return out, nil
	}
	list, err := queryStatusUpdates(`
		SELECT DISTINCT ON (s.project_id) s.id, s.project_id, COALESCE(s.author_id, 0),
		       COALESCE(NULLIF(TRIM(CONCAT_WS(' ', u.first_name, u.last_name)), ''), u.user_name, u.email, ''),
		       s.health, s.body, s.created_at
		FROM project_status_updates s
		LEFT JOIN users u ON u.id = s.author_id
		WHERE s.project_id = ANY($1)
		ORDER BY s.project_id, s.created_at DESC, s.id DESC`, projectIDs)
	if err != nil {
		return nil, err
	}
	for _, u := range list {
		out[u.ProjectID] = u
	}
	return out, nil
}

// DeleteProjectStatusUpdate removes one update from a project.
func DeleteProjectStatusUpdate(projectID, updateID int) (bool, error) {
	pool, err := OpenDatabase()
	if err != nil {
		return false, err
	}
	defer CloseDatabase(pool)
	tag, err := pool.Exec(context.Background(),
		`DELETE FROM project_status_updates WHERE id = $1 AND project_id = $2`, updateID, projectID)
	return tag.RowsAffected() > 0, err
}

func queryStatusUpdates(q string, args ...any) ([]ProjectStatusUpdate, error) {
	pool, err := OpenDatabase()
	if err != nil {
		return nil, err
	}
	defer CloseDatabase(pool)

	rows, err := pool.Query(context.Background(), q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ProjectStatusUpdate{}
	for rows.Next() {
		var u ProjectStatusUpdate
		if err := rows.Scan(&u.ID, &u.ProjectID, &u.AuthorID, &u.AuthorName, &u.Health, &u.Body, &u.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

// PortfolioCounts is one project's numbers on the portfolio.
type PortfolioCounts struct {
	Open         int
	Overdue      int
	DueThisWeek  int
	Unclaimed    int
	DoneLastWeek int
	Done         int
}

// GetPortfolioCounts counts live tasks for each project. today is YYYY-MM-DD in
// the viewer's zone.
func GetPortfolioCounts(projectIDs []int, today string) (map[int]PortfolioCounts, error) {
	out := map[int]PortfolioCounts{}
	if len(projectIDs) == 0 {
		return out, nil
	}
	pool, err := OpenDatabase()
	if err != nil {
		return nil, err
	}
	defer CloseDatabase(pool)

	rows, err := pool.Query(context.Background(), `
		SELECT t.project_id,
			COUNT(*) FILTER (WHERE NOT COALESCE(t.completed, false)),
			COUNT(*) FILTER (WHERE NOT COALESCE(t.completed, false) AND t.due_date < $2::date),
			COUNT(*) FILTER (WHERE NOT COALESCE(t.completed, false) AND t.due_date >= $2::date AND t.due_date < $2::date + 7),
			COUNT(*) FILTER (WHERE NOT COALESCE(t.completed, false) AND t.claimed_by IS NULL),
			COUNT(*) FILTER (WHERE COALESCE(t.completed, false) AND EXISTS (
				SELECT 1 FROM task_events te WHERE te.task_id = t.id AND te.event_type = 'completed'
				  AND te.created_at >= NOW() - INTERVAL '7 days')),
			COUNT(*) FILTER (WHERE COALESCE(t.completed, false))
		FROM tasks t
		WHERE t.project_id = ANY($1) AND `+TaskNotTrashedSQL("t")+` AND NOT `+ArchivedTaskExistsSQL("t.id")+`
		GROUP BY t.project_id`, projectIDs, today)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id int
		var c PortfolioCounts
		if err := rows.Scan(&id, &c.Open, &c.Overdue, &c.DueThisWeek, &c.Unclaimed, &c.DoneLastWeek, &c.Done); err != nil {
			return nil, err
		}
		out[id] = c
	}
	return out, rows.Err()
}
