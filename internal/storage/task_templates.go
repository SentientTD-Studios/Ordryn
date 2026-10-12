package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// TaskTemplate is a reusable task blueprint. ProjectID nil means a personal
// template only its owner sees; otherwise every project member can use it.
type TaskTemplate struct {
	ID             int
	OwnerID        int
	OwnerName      string
	ProjectID      *int
	Name           string
	Title          string
	Description    string
	Priority       int
	TagNames       []string
	EstimatePoints *int
	// DueOffsetDays sets the new task's due date to today plus this many days.
	DueOffsetDays *int
	Subtasks      []string
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

// ErrTaskTemplateNotFound is returned when a template id does not exist.
var ErrTaskTemplateNotFound = errors.New("task template not found")

// CreateTaskTemplatesTable creates task_templates.
func CreateTaskTemplatesTable() error {
	pool, err := OpenDatabase()
	if err != nil {
		return err
	}
	defer CloseDatabase(pool)
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS task_templates (
			id SERIAL PRIMARY KEY,
			owner_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			project_id INTEGER REFERENCES projects(id) ON DELETE CASCADE,
			name VARCHAR(80) NOT NULL,
			title TEXT NOT NULL,
			description TEXT NOT NULL DEFAULT '',
			priority INTEGER NOT NULL DEFAULT 0,
			tag_names TEXT[] NOT NULL DEFAULT '{}',
			estimate_points INTEGER,
			due_offset_days INTEGER,
			subtasks TEXT[] NOT NULL DEFAULT '{}',
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)`,
		`CREATE INDEX IF NOT EXISTS idx_task_templates_owner ON task_templates (owner_id) WHERE project_id IS NULL`,
		`CREATE INDEX IF NOT EXISTS idx_task_templates_project ON task_templates (project_id)`,
	}
	for _, s := range stmts {
		if _, err := pool.Exec(context.Background(), s); err != nil {
			return fmt.Errorf("create task_templates: %w", err)
		}
	}
	return nil
}

const taskTemplateSelect = `
	SELECT tt.id, tt.owner_id, COALESCE(NULLIF(u.user_name, ''), u.email, ''), tt.project_id, tt.name, tt.title,
		tt.description, tt.priority, tt.tag_names, tt.estimate_points, tt.due_offset_days, tt.subtasks,
		tt.created_at, tt.updated_at
	FROM task_templates tt
	LEFT JOIN users u ON u.id = tt.owner_id`

func scanTaskTemplate(row pgx.Row) (TaskTemplate, error) {
	var t TaskTemplate
	var projectID, estimate, offset sql.NullInt64
	err := row.Scan(&t.ID, &t.OwnerID, &t.OwnerName, &projectID, &t.Name, &t.Title, &t.Description, &t.Priority,
		&t.TagNames, &estimate, &offset, &t.Subtasks, &t.CreatedAt, &t.UpdatedAt)
	if err != nil {
		return t, err
	}
	if projectID.Valid {
		v := int(projectID.Int64)
		t.ProjectID = &v
	}
	if estimate.Valid {
		v := int(estimate.Int64)
		t.EstimatePoints = &v
	}
	if offset.Valid {
		v := int(offset.Int64)
		t.DueOffsetDays = &v
	}
	if t.TagNames == nil {
		t.TagNames = []string{}
	}
	if t.Subtasks == nil {
		t.Subtasks = []string{}
	}
	return t, nil
}

// ListTaskTemplates returns the user's personal templates plus the templates of
// the given projects, personal first, then by name.
func ListTaskTemplates(ownerID int, projectIDs []int) ([]TaskTemplate, error) {
	pool, err := OpenDatabase()
	if err != nil {
		return nil, err
	}
	defer CloseDatabase(pool)
	if projectIDs == nil {
		projectIDs = []int{}
	}
	rows, err := pool.Query(context.Background(), taskTemplateSelect+`
		WHERE (tt.project_id IS NULL AND tt.owner_id = $1) OR tt.project_id = ANY($2)
		ORDER BY tt.project_id NULLS FIRST, LOWER(tt.name), tt.id`, ownerID, projectIDs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []TaskTemplate{}
	for rows.Next() {
		t, err := scanTaskTemplate(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// GetTaskTemplate loads one template by id.
func GetTaskTemplate(id int) (*TaskTemplate, error) {
	pool, err := OpenDatabase()
	if err != nil {
		return nil, err
	}
	defer CloseDatabase(pool)
	t, err := scanTaskTemplate(pool.QueryRow(context.Background(), taskTemplateSelect+` WHERE tt.id = $1`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrTaskTemplateNotFound
	}
	if err != nil {
		return nil, err
	}
	return &t, nil
}

// CountTaskTemplates counts templates in one scope: a project, or an owner's personal set.
func CountTaskTemplates(ownerID int, projectID *int) (int, error) {
	pool, err := OpenDatabase()
	if err != nil {
		return 0, err
	}
	defer CloseDatabase(pool)
	var n int
	if projectID != nil {
		err = pool.QueryRow(context.Background(), `SELECT COUNT(*) FROM task_templates WHERE project_id = $1`, *projectID).Scan(&n)
	} else {
		err = pool.QueryRow(context.Background(),
			`SELECT COUNT(*) FROM task_templates WHERE project_id IS NULL AND owner_id = $1`, ownerID).Scan(&n)
	}
	return n, err
}

// SaveTaskTemplate inserts t when ID is 0, else updates its editable fields.
func SaveTaskTemplate(t *TaskTemplate) error {
	pool, err := OpenDatabase()
	if err != nil {
		return err
	}
	defer CloseDatabase(pool)
	ctx := context.Background()
	if t.TagNames == nil {
		t.TagNames = []string{}
	}
	if t.Subtasks == nil {
		t.Subtasks = []string{}
	}
	if t.ID == 0 {
		return pool.QueryRow(ctx, `
			INSERT INTO task_templates (owner_id, project_id, name, title, description, priority, tag_names,
				estimate_points, due_offset_days, subtasks)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
			RETURNING id, created_at, updated_at`,
			t.OwnerID, t.ProjectID, t.Name, t.Title, t.Description, t.Priority, t.TagNames,
			t.EstimatePoints, t.DueOffsetDays, t.Subtasks).Scan(&t.ID, &t.CreatedAt, &t.UpdatedAt)
	}
	tag, err := pool.Exec(ctx, `
		UPDATE task_templates SET name = $2, title = $3, description = $4, priority = $5, tag_names = $6,
			estimate_points = $7, due_offset_days = $8, subtasks = $9, updated_at = NOW()
		WHERE id = $1`,
		t.ID, t.Name, t.Title, t.Description, t.Priority, t.TagNames, t.EstimatePoints, t.DueOffsetDays, t.Subtasks)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrTaskTemplateNotFound
	}
	return nil
}

// DeleteTaskTemplate removes a template.
func DeleteTaskTemplate(id int) error {
	pool, err := OpenDatabase()
	if err != nil {
		return err
	}
	defer CloseDatabase(pool)
	tag, err := pool.Exec(context.Background(), `DELETE FROM task_templates WHERE id = $1`, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrTaskTemplateNotFound
	}
	return nil
}
