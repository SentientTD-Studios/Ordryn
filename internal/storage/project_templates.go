package storage

import (
	"context"
	"fmt"
)

// MigrateProjectsAddIsTemplate adds projects.is_template: a project people can
// start new projects from.
func MigrateProjectsAddIsTemplate() error {
	pool, err := OpenDatabase()
	if err != nil {
		return err
	}
	defer CloseDatabase(pool)
	if _, err := pool.Exec(context.Background(),
		`ALTER TABLE projects ADD COLUMN IF NOT EXISTS is_template BOOLEAN NOT NULL DEFAULT FALSE`); err != nil {
		return fmt.Errorf("projects.is_template: %w", err)
	}
	return nil
}

// SetProjectIsTemplate marks or unmarks a project as a template.
func SetProjectIsTemplate(projectID int, on bool) error {
	pool, err := OpenDatabase()
	if err != nil {
		return err
	}
	defer CloseDatabase(pool)
	_, err = pool.Exec(context.Background(), `UPDATE projects SET is_template = $2 WHERE id = $1`, projectID, on)
	return err
}

// ProjectIsTemplate reports whether a project is marked as a template.
func ProjectIsTemplate(projectID int) bool {
	pool, err := OpenDatabase()
	if err != nil {
		return false
	}
	defer CloseDatabase(pool)
	var on bool
	_ = pool.QueryRow(context.Background(), `SELECT COALESCE(is_template, FALSE) FROM projects WHERE id = $1`, projectID).Scan(&on)
	return on
}

// TemplateProjectIDs returns which of ids are templates.
func TemplateProjectIDs(ids []int) (map[int]bool, error) {
	out := map[int]bool{}
	if len(ids) == 0 {
		return out, nil
	}
	pool, err := OpenDatabase()
	if err != nil {
		return nil, err
	}
	defer CloseDatabase(pool)
	rows, err := pool.Query(context.Background(), `SELECT id FROM projects WHERE id = ANY($1) AND is_template`, ids)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id int
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out[id] = true
	}
	return out, rows.Err()
}

// TemplateTask is an open task copied when a project starts from a template.
type TemplateTask struct {
	ID             int
	ParentID       int
	Title          string
	Description    string
	Priority       int
	StatusID       int
	EstimatePoints *int
	TagIDs         []int
}

// ListTemplateTasks returns a project's live open tasks, parents before
// subtasks, in board order.
func ListTemplateTasks(projectID int) ([]TemplateTask, error) {
	pool, err := OpenDatabase()
	if err != nil {
		return nil, err
	}
	defer CloseDatabase(pool)

	rows, err := pool.Query(context.Background(), `
		SELECT t.id, COALESCE(t.parent_id, 0), COALESCE(t.title, ''), COALESCE(t.description, ''),
		       COALESCE(t.priority, 0), COALESCE(t.status_id, 0), t.estimate_points,
		       COALESCE((SELECT array_agg(tt.tag_id ORDER BY tt.tag_id) FROM task_tags tt WHERE tt.task_id = t.id), '{}')
		FROM tasks t
		WHERE t.project_id = $1 AND NOT COALESCE(t.completed, FALSE)
		  AND `+TaskNotTrashedSQL("t")+` AND NOT `+ArchivedTaskExistsSQL("t.id")+`
		ORDER BY (t.parent_id IS NOT NULL), COALESCE(t.position, 0), t.id`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []TemplateTask{}
	for rows.Next() {
		var t TemplateTask
		if err := rows.Scan(&t.ID, &t.ParentID, &t.Title, &t.Description, &t.Priority, &t.StatusID,
			&t.EstimatePoints, &t.TagIDs); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}
