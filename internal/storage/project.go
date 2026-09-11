package storage

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// Project represents a user-owned project that can contain tasks.
type Project struct {
	ID           int
	UserID       int
	Name         string
	Description        string
	WorkflowMode       string
	Position           int
	Archived           bool
	BacklogName        string
	BacklogDescription string
	CreatedAt          time.Time
	UpdatedAt          time.Time
}

const projectSelectCols = `id, user_id, name, COALESCE(description, ''), COALESCE(workflow_mode, 'classic'),
		        COALESCE(position, 0), COALESCE(archived, false), COALESCE(backlog_name, 'Backlog'),
		        COALESCE(backlog_description, ''), created_at, updated_at`

// CreateProject inserts a new project for the given user and returns it.
// New projects are appended at the end of the owner's ordered list.
func CreateProject(userID int, name, description string) (*Project, error) {
	pool, err := OpenDatabase()
	if err != nil {
		return nil, err
	}
	defer CloseDatabase(pool)

	var p Project
	err = pool.QueryRow(context.Background(),
		`INSERT INTO projects (user_id, name, description, position)
		 VALUES (
		   $1, $2, $3,
		   COALESCE((SELECT MAX(position) FROM projects WHERE user_id = $1), -1) + 1
		 )
		 RETURNING `+projectSelectCols,
		userID, name, description).Scan(
		&p.ID, &p.UserID, &p.Name, &p.Description, &p.WorkflowMode, &p.Position, &p.Archived, &p.BacklogName, &p.BacklogDescription, &p.CreatedAt, &p.UpdatedAt)
	if err != nil {
		return nil, fmt.Errorf("failed to create project: %v", err)
	}
	if p.WorkflowMode == "" {
		p.WorkflowMode = WorkflowClassic
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

// UpdateProject updates name, description, backlog_name, and/or backlog_description of a project owned by the user.
// Nil pointers leave that field unchanged.
func UpdateProject(id int, userID int, name *string, description *string, backlogName *string, backlogDescription *string) error {
	if name == nil && description == nil && backlogName == nil && backlogDescription == nil {
		return nil
	}
	pool, err := OpenDatabase()
	if err != nil {
		return err
	}
	defer CloseDatabase(pool)

	setClauses := []string{"updated_at = CURRENT_TIMESTAMP"}
	args := []interface{}{}

	if name != nil {
		args = append(args, *name)
		setClauses = append(setClauses, fmt.Sprintf("name = $%d", len(args)))
	}
	if description != nil {
		args = append(args, *description)
		setClauses = append(setClauses, fmt.Sprintf("description = $%d", len(args)))
	}
	if backlogName != nil {
		args = append(args, *backlogName)
		setClauses = append(setClauses, fmt.Sprintf("backlog_name = $%d", len(args)))
	}
	if backlogDescription != nil {
		args = append(args, *backlogDescription)
		setClauses = append(setClauses, fmt.Sprintf("backlog_description = $%d", len(args)))
	}

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
		if err := rows.Scan(&p.ID, &p.UserID, &p.Name, &p.Description, &p.WorkflowMode, &p.Position, &p.Archived, &p.BacklogName, &p.BacklogDescription, &p.CreatedAt, &p.UpdatedAt); err != nil {
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
	err = pool.QueryRow(context.Background(),
		`SELECT `+projectSelectCols+`
		 FROM projects WHERE id = $1 AND user_id = $2`, id, userID).Scan(
		&p.ID, &p.UserID, &p.Name, &p.Description, &p.WorkflowMode, &p.Position, &p.Archived, &p.BacklogName, &p.BacklogDescription, &p.CreatedAt, &p.UpdatedAt)
	if err != nil {
		return nil, fmt.Errorf("failed to get project: %v", err)
	}
	return &p, nil
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
