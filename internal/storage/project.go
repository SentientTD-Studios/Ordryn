package storage

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"
)

const (
	MinAutoSprintLengthDays = 1
	MaxAutoSprintLengthDays = 365
)

// Project represents a user-owned project that can contain tasks.
type Project struct {
	ID                       int
	UserID                   int
	Name                     string
	Description              string
	WorkflowMode             string
	Position                 int
	Archived                 bool
	BacklogName              string
	BacklogDescription       string
	AutoCreateNextSprint     bool
	AutoSprintLengthDays     *int
	AutoSprintLockDaysBefore *int
	CreatedAt                time.Time
	UpdatedAt                time.Time
}

// ProjectPatch is a partial update for a project owned by the user.
type ProjectPatch struct {
	Name                     *string
	Description              *string
	BacklogName              *string
	BacklogDescription       *string
	AutoCreateNextSprint     *bool
	AutoSprintLengthDays     **int
	AutoSprintLockDaysBefore **int
}

func (p ProjectPatch) Empty() bool {
	return p.Name == nil && p.Description == nil && p.BacklogName == nil && p.BacklogDescription == nil &&
		p.AutoCreateNextSprint == nil && p.AutoSprintLengthDays == nil && p.AutoSprintLockDaysBefore == nil
}

const projectSelectCols = `id, user_id, name, COALESCE(description, ''), COALESCE(workflow_mode, 'classic'),
		        COALESCE(position, 0), COALESCE(archived, false), COALESCE(backlog_name, 'Backlog'),
		        COALESCE(backlog_description, ''), COALESCE(auto_create_next_sprint, false),
		        auto_sprint_length_days, auto_sprint_lock_days_before, created_at, updated_at`

func nullIntPtr(n sql.NullInt32) *int {
	if !n.Valid {
		return nil
	}
	v := int(n.Int32)
	return &v
}

func scanProject(row interface{ Scan(dest ...any) error }, p *Project) error {
	var length, lock sql.NullInt32
	if err := row.Scan(
		&p.ID, &p.UserID, &p.Name, &p.Description, &p.WorkflowMode, &p.Position, &p.Archived,
		&p.BacklogName, &p.BacklogDescription, &p.AutoCreateNextSprint, &length, &lock,
		&p.CreatedAt, &p.UpdatedAt,
	); err != nil {
		return err
	}
	p.AutoSprintLengthDays = nullIntPtr(length)
	p.AutoSprintLockDaysBefore = nullIntPtr(lock)
	if p.WorkflowMode == "" {
		p.WorkflowMode = WorkflowClassic
	}
	return nil
}

// CreateProject inserts a new project for the given user and returns it.
// New projects are appended at the end of the owner's ordered list.
func CreateProject(userID int, name, description string) (*Project, error) {
	pool, err := OpenDatabase()
	if err != nil {
		return nil, err
	}
	defer CloseDatabase(pool)

	var p Project
	if err := scanProject(pool.QueryRow(context.Background(),
		`INSERT INTO projects (user_id, name, description, position)
		 VALUES (
		   $1, $2, $3,
		   COALESCE((SELECT MAX(position) FROM projects WHERE user_id = $1), -1) + 1
		 )
		 RETURNING `+projectSelectCols,
		userID, name, description), &p); err != nil {
		return nil, fmt.Errorf("failed to create project: %v", err)
	}
	if err := EnsureProjectOwnerMember(p.ID, userID); err != nil {
		return nil, fmt.Errorf("failed to create project owner membership: %v", err)
	}
	pid := p.ID
	if _, err := EnsureArchivedTag(userID, &pid); err != nil {
		return nil, fmt.Errorf("failed to seed archived tag: %v", err)
	}
	return &p, nil
}

func appendNullableIntPatch(col string, p **int, setClauses *[]string, args *[]interface{}) {
	if p == nil {
		return
	}
	if *p == nil {
		*setClauses = append(*setClauses, col+" = NULL")
		return
	}
	*args = append(*args, **p)
	*setClauses = append(*setClauses, fmt.Sprintf("%s = $%d", col, len(*args)))
}

// UpdateProject updates mutable project fields owned by the user.
// Nil pointers leave that field unchanged.
func UpdateProject(id int, userID int, patch ProjectPatch) error {
	if patch.Empty() {
		return nil
	}
	pool, err := OpenDatabase()
	if err != nil {
		return err
	}
	defer CloseDatabase(pool)

	setClauses := []string{"updated_at = CURRENT_TIMESTAMP"}
	args := []interface{}{}

	if patch.Name != nil {
		args = append(args, *patch.Name)
		setClauses = append(setClauses, fmt.Sprintf("name = $%d", len(args)))
	}
	if patch.Description != nil {
		args = append(args, *patch.Description)
		setClauses = append(setClauses, fmt.Sprintf("description = $%d", len(args)))
	}
	if patch.BacklogName != nil {
		args = append(args, *patch.BacklogName)
		setClauses = append(setClauses, fmt.Sprintf("backlog_name = $%d", len(args)))
	}
	if patch.BacklogDescription != nil {
		args = append(args, *patch.BacklogDescription)
		setClauses = append(setClauses, fmt.Sprintf("backlog_description = $%d", len(args)))
	}
	if patch.AutoCreateNextSprint != nil {
		args = append(args, *patch.AutoCreateNextSprint)
		setClauses = append(setClauses, fmt.Sprintf("auto_create_next_sprint = $%d", len(args)))
	}
	appendNullableIntPatch("auto_sprint_length_days", patch.AutoSprintLengthDays, &setClauses, &args)
	appendNullableIntPatch("auto_sprint_lock_days_before", patch.AutoSprintLockDaysBefore, &setClauses, &args)

	args = append(args, id, userID)
	query := fmt.Sprintf("UPDATE projects SET %s WHERE id = $%d AND user_id = $%d",
		strings.Join(setClauses, ", "), len(args)-1, len(args))

	_, err = pool.Exec(context.Background(), query, args...)
	if err != nil {
		return fmt.Errorf("failed to update project: %v", err)
	}
	return nil
}

// GetProjectBacklogName returns the custom backlog sprint name for a project (or "Backlog").
func GetProjectBacklogName(projectID int) (string, error) {
	pool, err := OpenDatabase()
	if err != nil {
		return "Backlog", err
	}
	defer CloseDatabase(pool)

	var name string
	err = pool.QueryRow(context.Background(),
		`SELECT COALESCE(backlog_name, 'Backlog') FROM projects WHERE id = $1`, projectID).Scan(&name)
	if err != nil {
		return "Backlog", err
	}
	if strings.TrimSpace(name) == "" {
		return "Backlog", nil
	}
	return name, nil
}

// GetProjectBacklogDescription returns the custom backlog sprint description for a project.
func GetProjectBacklogDescription(projectID int) (string, error) {
	pool, err := OpenDatabase()
	if err != nil {
		return "", err
	}
	defer CloseDatabase(pool)

	var desc string
	err = pool.QueryRow(context.Background(),
		`SELECT COALESCE(backlog_description, '') FROM projects WHERE id = $1`, projectID).Scan(&desc)
	if err != nil {
		return "", err
	}
	return desc, nil
}

// DeleteProject removes a project owned by the user.
func DeleteProject(id int, userID int) error {
	pool, err := OpenDatabase()
	if err != nil {
		return err
	}
	defer CloseDatabase(pool)

	_, err = pool.Exec(context.Background(), "DELETE FROM projects WHERE id = $1 AND user_id = $2", id, userID)
	if err != nil {
		return fmt.Errorf("failed to delete project: %v", err)
	}
	return nil
}

// GetProjectsForUser returns all projects owned by a user, ordered by position.
func GetProjectsForUser(userID int) ([]Project, error) {
	pool, err := OpenDatabase()
	if err != nil {
		return nil, err
	}
	defer CloseDatabase(pool)

	rows, err := pool.Query(context.Background(),
		`SELECT `+projectSelectCols+`
		 FROM projects WHERE user_id = $1
		 ORDER BY archived ASC, position ASC, LOWER(name) ASC, id ASC`, userID)
	if err != nil {
		return nil, fmt.Errorf("failed to query projects: %v", err)
	}
	defer rows.Close()

	var out []Project
	for rows.Next() {
		var p Project
		if err := scanProject(rows, &p); err != nil {
			return nil, fmt.Errorf("failed to scan project row: %v", err)
		}
		out = append(out, p)
	}
	return out, nil
}

// GetActiveOwnedProjectsForUser returns non-archived projects owned by a user, ordered by position.
func GetActiveOwnedProjectsForUser(userID int) ([]Project, error) {
	all, err := GetProjectsForUser(userID)
	if err != nil {
		return nil, err
	}
	out := make([]Project, 0, len(all))
	for _, p := range all {
		if !p.Archived {
			out = append(out, p)
		}
	}
	return out, nil
}

// GetProjectByID returns a project by id for the given user.
func GetProjectByID(id int, userID int) (*Project, error) {
	pool, err := OpenDatabase()
	if err != nil {
		return nil, err
	}
	defer CloseDatabase(pool)

	var p Project
	err = scanProject(pool.QueryRow(context.Background(),
		`SELECT `+projectSelectCols+`
		 FROM projects WHERE id = $1 AND user_id = $2`, id, userID), &p)
	if err != nil {
		return nil, fmt.Errorf("failed to get project: %v", err)
	}
	return &p, nil
}

// ListKanbanProjectsWithAutoSprint returns non-archived kanban projects that
// automatically create the next sprint when the current one ends.
func ListKanbanProjectsWithAutoSprint() ([]Project, error) {
	pool, err := OpenDatabase()
	if err != nil {
		return nil, err
	}
	defer CloseDatabase(pool)

	rows, err := pool.Query(context.Background(),
		`SELECT `+projectSelectCols+`
		 FROM projects
		 WHERE COALESCE(auto_create_next_sprint, false) = true
		   AND COALESCE(archived, false) = false
		   AND COALESCE(workflow_mode, 'classic') = $1
		 ORDER BY id ASC`, WorkflowKanban)
	if err != nil {
		return nil, fmt.Errorf("failed to list auto-sprint projects: %v", err)
	}
	defer rows.Close()

	var out []Project
	for rows.Next() {
		var p Project
		if err := scanProject(rows, &p); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// SetProjectArchived sets the archived flag for a project owned by userID.
func SetProjectArchived(id, userID int, archived bool) error {
	pool, err := OpenDatabase()
	if err != nil {
		return err
	}
	defer CloseDatabase(pool)

	res, err := pool.Exec(context.Background(),
		`UPDATE projects SET archived = $1, updated_at = CURRENT_TIMESTAMP WHERE id = $2 AND user_id = $3`,
		archived, id, userID)
	if err != nil {
		return fmt.Errorf("failed to update project archive state: %v", err)
	}
	if res.RowsAffected() == 0 {
		return fmt.Errorf("project not found")
	}
	return nil
}

// ReorderProjects sets positions from ordered owned project IDs for a user.
// orderedIDs must be the full set of active (non-archived) projects owned by userID.
func ReorderProjects(userID int, orderedIDs []int) error {
	pool, err := OpenDatabase()
	if err != nil {
		return err
	}
	defer CloseDatabase(pool)

	existing, err := GetActiveOwnedProjectsForUser(userID)
	if err != nil {
		return err
	}
	if len(orderedIDs) != len(existing) {
		return fmt.Errorf("project list mismatch")
	}
	have := make(map[int]struct{}, len(existing))
	for _, p := range existing {
		have[p.ID] = struct{}{}
	}
	for _, id := range orderedIDs {
		if _, ok := have[id]; !ok {
			return fmt.Errorf("project %d not owned by user", id)
		}
	}

	tx, err := pool.Begin(context.Background())
	if err != nil {
		return err
	}
	defer tx.Rollback(context.Background())

	for i, id := range orderedIDs {
		if _, err := tx.Exec(context.Background(),
			`UPDATE projects SET position = $1, updated_at = CURRENT_TIMESTAMP WHERE id = $2 AND user_id = $3`,
			i, id, userID); err != nil {
			return err
		}
	}
	return tx.Commit(context.Background())
}
