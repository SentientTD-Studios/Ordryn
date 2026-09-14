package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

const (
	WorkflowClassic = "classic"
	WorkflowKanban  = "kanban"

	MaxProjectStatuses      = 8
	MaxEstimatePoints       = 100
	MaxStatusNameLen        = 40
	MaxStatusDescriptionLen = 50
	MaxTimeEntryNote        = 200
	MaxTimeEntryMinutes     = 24 * 60

	projectStatusSelectCols = `id, project_id, name, description, position, is_done, is_default, created_at`
)

// ProjectStatus is a kanban column within a project.
type ProjectStatus struct {
	ID          int
	ProjectID   int
	Name        string
	Description string
	Position    int
	IsDone      bool
	IsDefault   bool
	CreatedAt   time.Time
}

type projectStatusScanner interface {
	Scan(dest ...any) error
}

func scanProjectStatus(row projectStatusScanner, s *ProjectStatus) error {
	return row.Scan(&s.ID, &s.ProjectID, &s.Name, &s.Description, &s.Position, &s.IsDone, &s.IsDefault, &s.CreatedAt)
}

// TaskTimeEntry is a time log against a kanban task.
type TaskTimeEntry struct {
	ID        int
	TaskID    int
	UserID    int
	Minutes   int
	Note      string
	CreatedAt time.Time
	UserEmail string
	UserName  string
}

// TaskWorkflowFields are kanban-related fields attached to tasks.
type TaskWorkflowFields struct {
	StatusID         int
	StatusName       string
	EstimatePoints   *int
	TimeSpentMinutes int
	ProjectWorkflow  string
	ClaimedBy        int
	ClaimedByName    string
	SprintID         int
	SprintName       string
}

// CreateProjectWorkflowTables creates statuses and time-entry tables.
func CreateProjectWorkflowTables() error {
	pool, err := OpenDatabase()
	if err != nil {
		return err
	}
	defer CloseDatabase(pool)

	stmts := []string{
		`CREATE TABLE IF NOT EXISTS project_statuses (
			id SERIAL PRIMARY KEY,
			project_id INTEGER NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
			name TEXT NOT NULL,
			description TEXT NOT NULL DEFAULT '',
			position INTEGER NOT NULL DEFAULT 0,
			is_done BOOLEAN NOT NULL DEFAULT FALSE,
			is_default BOOLEAN NOT NULL DEFAULT FALSE,
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_project_statuses_project_lower_name
			ON project_statuses (project_id, lower(name))`,
		`CREATE INDEX IF NOT EXISTS idx_project_statuses_project_position
			ON project_statuses (project_id, position)`,
		`CREATE TABLE IF NOT EXISTS task_time_entries (
			id SERIAL PRIMARY KEY,
			task_id INTEGER NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
			user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			minutes INTEGER NOT NULL CHECK (minutes > 0 AND minutes <= 1440),
			note TEXT NOT NULL DEFAULT '',
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)`,
		`CREATE INDEX IF NOT EXISTS idx_task_time_entries_task
			ON task_time_entries (task_id, created_at DESC)`,
	}
	for _, s := range stmts {
		if _, err := pool.Exec(context.Background(), s); err != nil {
			return fmt.Errorf("failed to create workflow tables: %v", err)
		}
	}
	return nil
}

// MigrateProjectsAddWorkflowMode adds workflow_mode to projects.
func MigrateProjectsAddWorkflowMode() error {
	pool, err := OpenDatabase()
	if err != nil {
		return err
	}
	defer CloseDatabase(pool)

	_, err = pool.Exec(context.Background(),
		`ALTER TABLE projects ADD COLUMN IF NOT EXISTS workflow_mode VARCHAR(16) NOT NULL DEFAULT 'classic'`)
	if err != nil {
		return fmt.Errorf("failed to add workflow_mode: %v", err)
	}

	// Ensure check constraint exists (idempotent via name).
	var exists bool
	err = pool.QueryRow(context.Background(),
		`SELECT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'projects_workflow_mode_check')`).Scan(&exists)
	if err != nil {
		return err
	}
	if !exists {
		_, err = pool.Exec(context.Background(),
			`ALTER TABLE projects ADD CONSTRAINT projects_workflow_mode_check
			 CHECK (workflow_mode IN ('classic', 'kanban'))`)
		if err != nil {
			return fmt.Errorf("failed to add workflow_mode check: %v", err)
		}
	}
	return nil
}

// MigrateProjectStatusesAddDescription adds an optional short description on status columns.
func MigrateProjectStatusesAddDescription() error {
	pool, err := OpenDatabase()
	if err != nil {
		return err
	}
	defer CloseDatabase(pool)

	_, err = pool.Exec(context.Background(),
		`ALTER TABLE project_statuses ADD COLUMN IF NOT EXISTS description TEXT NOT NULL DEFAULT ''`)
	if err != nil {
		return fmt.Errorf("failed to add status description: %v", err)
	}
	return nil
}

// MigrateTasksAddWorkflowFields adds status_id and estimate_points to tasks.
func MigrateTasksAddWorkflowFields() error {
	pool, err := OpenDatabase()
	if err != nil {
		return err
	}
	defer CloseDatabase(pool)

	stmts := []string{
		`ALTER TABLE tasks ADD COLUMN IF NOT EXISTS status_id INTEGER`,
		`ALTER TABLE tasks ADD COLUMN IF NOT EXISTS estimate_points INTEGER`,
	}
	for _, s := range stmts {
		if _, err := pool.Exec(context.Background(), s); err != nil {
			return fmt.Errorf("failed to add task workflow columns: %v", err)
		}
	}

	var exists bool
	err = pool.QueryRow(context.Background(),
		`SELECT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'fk_tasks_status')`).Scan(&exists)
	if err != nil {
		return err
	}
	if !exists {
		_, err = pool.Exec(context.Background(),
			`ALTER TABLE tasks ADD CONSTRAINT fk_tasks_status
			 FOREIGN KEY (status_id) REFERENCES project_statuses(id) ON DELETE RESTRICT`)
		if err != nil {
			return fmt.Errorf("failed to add status FK: %v", err)
		}
	}
	return nil
}

// MigrateTasksAddClaimedBy adds claimed_by for kanban task ownership.
func MigrateTasksAddClaimedBy() error {
	pool, err := OpenDatabase()
	if err != nil {
		return err
	}
	defer CloseDatabase(pool)

	if _, err := pool.Exec(context.Background(),
		`ALTER TABLE tasks ADD COLUMN IF NOT EXISTS claimed_by INTEGER`); err != nil {
		return fmt.Errorf("failed to add claimed_by: %v", err)
	}

	var exists bool
	err = pool.QueryRow(context.Background(),
		`SELECT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'fk_tasks_claimed_by')`).Scan(&exists)
	if err != nil {
		return err
	}
	if !exists {
		_, err = pool.Exec(context.Background(),
			`ALTER TABLE tasks ADD CONSTRAINT fk_tasks_claimed_by
			 FOREIGN KEY (claimed_by) REFERENCES users(id) ON DELETE SET NULL`)
		if err != nil {
			return fmt.Errorf("failed to add claimed_by FK: %v", err)
		}
	}

	if _, err := pool.Exec(context.Background(),
		`CREATE INDEX IF NOT EXISTS idx_tasks_claimed_by ON tasks(claimed_by)`); err != nil {
		return fmt.Errorf("failed to index claimed_by: %v", err)
	}
	return nil
}

// CountProjectTasks returns the number of tasks in a project (including subtasks).
func CountProjectTasks(projectID int) (int, error) {
	pool, err := OpenDatabase()
	if err != nil {
		return 0, err
	}
	defer CloseDatabase(pool)

	var n int
	err = pool.QueryRow(context.Background(),
		`SELECT COUNT(*) FROM tasks WHERE project_id = $1`, projectID).Scan(&n)
	return n, err
}

// GetProjectWorkflowMode returns classic/kanban for a project.
func GetProjectWorkflowMode(projectID int) (string, error) {
	pool, err := OpenDatabase()
	if err != nil {
		return "", err
	}
	defer CloseDatabase(pool)

	var mode string
	err = pool.QueryRow(context.Background(),
		`SELECT COALESCE(workflow_mode, 'classic') FROM projects WHERE id = $1`, projectID).Scan(&mode)
	if err != nil {
		return "", err
	}
	return mode, nil
}

// SetProjectWorkflowMode updates workflow_mode.
func SetProjectWorkflowMode(projectID int, mode string) error {
	pool, err := OpenDatabase()
	if err != nil {
		return err
	}
	defer CloseDatabase(pool)

	query := `UPDATE projects SET workflow_mode = $1, updated_at = CURRENT_TIMESTAMP WHERE id = $2`
	args := []interface{}{mode, projectID}
	if mode == WorkflowClassic {
		query = `UPDATE projects SET workflow_mode = $1, auto_create_next_sprint = false, updated_at = CURRENT_TIMESTAMP WHERE id = $2`
	}
	_, err = pool.Exec(context.Background(), query, args...)
	return err
}

// SeedDefaultProjectStatuses inserts To Do / In Progress / Done and returns them.
func SeedDefaultProjectStatuses(projectID int) ([]ProjectStatus, error) {
	pool, err := OpenDatabase()
	if err != nil {
		return nil, err
	}
	defer CloseDatabase(pool)

	defaults := []struct {
		name        string
		description string
		position    int
		isDone      bool
		isDefault   bool
	}{
		{"To Do", "Work that hasn't been started yet", 0, false, true},
		{"In Progress", "Currently being worked on", 1, false, false},
		{"Done", "Finished and ready to close", 2, true, false},
	}
	out := make([]ProjectStatus, 0, len(defaults))
	for _, d := range defaults {
		var s ProjectStatus
		err = scanProjectStatus(pool.QueryRow(context.Background(),
			`INSERT INTO project_statuses (project_id, name, description, position, is_done, is_default)
			 VALUES ($1, $2, $3, $4, $5, $6)
			 RETURNING `+projectStatusSelectCols,
			projectID, d.name, d.description, d.position, d.isDone, d.isDefault), &s)
		if err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, nil
}

// BackfillTaskStatusesForProject assigns statuses from completed and syncs completed.
func BackfillTaskStatusesForProject(projectID, defaultStatusID, doneStatusID int) error {
	pool, err := OpenDatabase()
	if err != nil {
		return err
	}
	defer CloseDatabase(pool)

	_, err = pool.Exec(context.Background(),
		`UPDATE tasks SET status_id = CASE WHEN COALESCE(completed,false) THEN $1::integer ELSE $2::integer END,
		 date_modified = NOW() AT TIME ZONE 'UTC'
		 WHERE project_id = $3`,
		doneStatusID, defaultStatusID, projectID)
	return err
}

// ClearProjectStatusesAndTaskFields removes statuses after clearing task FKs (empty project).
func ClearProjectStatusesAndTaskFields(projectID int) error {
	pool, err := OpenDatabase()
	if err != nil {
		return err
	}
	defer CloseDatabase(pool)

	_, err = pool.Exec(context.Background(),
		`UPDATE tasks SET status_id = NULL, estimate_points = NULL, sprint_id = NULL WHERE project_id = $1`, projectID)
	if err != nil {
		return err
	}
	_, err = pool.Exec(context.Background(),
		`DELETE FROM project_statuses WHERE project_id = $1`, projectID)
	return err
}

// ListProjectStatuses returns statuses ordered by position.
func ListProjectStatuses(projectID int) ([]ProjectStatus, error) {
	pool, err := OpenDatabase()
	if err != nil {
		return nil, err
	}
	defer CloseDatabase(pool)

	rows, err := pool.Query(context.Background(),
		`SELECT `+projectStatusSelectCols+`
		 FROM project_statuses WHERE project_id = $1 ORDER BY position ASC, id ASC`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []ProjectStatus
	for rows.Next() {
		var s ProjectStatus
		if err := scanProjectStatus(rows, &s); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// CountProjectStatuses returns how many statuses a project has.
func CountProjectStatuses(projectID int) (int, error) {
	pool, err := OpenDatabase()
	if err != nil {
		return 0, err
	}
	defer CloseDatabase(pool)
	var n int
	err = pool.QueryRow(context.Background(),
		`SELECT COUNT(*) FROM project_statuses WHERE project_id = $1`, projectID).Scan(&n)
	return n, err
}

// GetProjectStatus returns a status by id if it belongs to the project.
func GetProjectStatus(projectID, statusID int) (*ProjectStatus, error) {
	pool, err := OpenDatabase()
	if err != nil {
		return nil, err
	}
	defer CloseDatabase(pool)

	var s ProjectStatus
	err = scanProjectStatus(pool.QueryRow(context.Background(),
		`SELECT `+projectStatusSelectCols+`
		 FROM project_statuses WHERE id = $1 AND project_id = $2`,
		statusID, projectID), &s)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) || errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("status not found")
		}
		return nil, err
	}
	return &s, nil
}

// GetTaskProjectStatus returns the kanban status currently assigned to a task.
// Returns nil, nil when the task has no status_id.
func GetTaskProjectStatus(taskID int) (*ProjectStatus, error) {
	pool, err := OpenDatabase()
	if err != nil {
		return nil, err
	}
	defer CloseDatabase(pool)

	var s ProjectStatus
	err = scanProjectStatus(pool.QueryRow(context.Background(),
		`SELECT s.id, s.project_id, s.name, s.description, s.position, s.is_done, s.is_default, s.created_at
		 FROM project_statuses s
		 INNER JOIN tasks t ON t.status_id = s.id
		 WHERE t.id = $1`, taskID), &s)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) || errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return &s, nil
}

// GetDefaultProjectStatus returns the default column for a project.
func GetDefaultProjectStatus(projectID int) (*ProjectStatus, error) {
	pool, err := OpenDatabase()
	if err != nil {
		return nil, err
	}
	defer CloseDatabase(pool)

	var s ProjectStatus
	err = scanProjectStatus(pool.QueryRow(context.Background(),
		`SELECT `+projectStatusSelectCols+`
		 FROM project_statuses WHERE project_id = $1 AND is_default = true
		 ORDER BY position ASC, id ASC LIMIT 1`, projectID), &s)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) || errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("default status not found")
		}
		return nil, err
	}
	return &s, nil
}

// GetDoneProjectStatus returns the first done status for a project.
func GetDoneProjectStatus(projectID int) (*ProjectStatus, error) {
	pool, err := OpenDatabase()
	if err != nil {
		return nil, err
	}
	defer CloseDatabase(pool)

	var s ProjectStatus
	err = scanProjectStatus(pool.QueryRow(context.Background(),
		`SELECT `+projectStatusSelectCols+`
		 FROM project_statuses WHERE project_id = $1 AND is_done = true
		 ORDER BY position ASC, id ASC LIMIT 1`, projectID), &s)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) || errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("done status not found")
		}
		return nil, err
	}
	return &s, nil
}

// CreateProjectStatus inserts a status at the end of the board.
func CreateProjectStatus(projectID int, name, description string, isDone, isDefault bool) (*ProjectStatus, error) {
	pool, err := OpenDatabase()
	if err != nil {
		return nil, err
	}
	defer CloseDatabase(pool)

	var nextPos int
	if err := pool.QueryRow(context.Background(),
		`SELECT COALESCE(MAX(position), -1) + 1 FROM project_statuses WHERE project_id = $1`,
		projectID).Scan(&nextPos); err != nil {
		return nil, err
	}

	if isDefault {
		if _, err := pool.Exec(context.Background(),
			`UPDATE project_statuses SET is_default = false WHERE project_id = $1`, projectID); err != nil {
			return nil, err
		}
	}

	var s ProjectStatus
	err = scanProjectStatus(pool.QueryRow(context.Background(),
		`INSERT INTO project_statuses (project_id, name, description, position, is_done, is_default)
		 VALUES ($1, $2, $3, $4, $5, $6)
		 RETURNING `+projectStatusSelectCols,
		projectID, name, description, nextPos, isDone, isDefault), &s)
	if err != nil {
		return nil, err
	}
	return &s, nil
}

// UpdateProjectStatus updates mutable status fields.
func UpdateProjectStatus(projectID, statusID int, name *string, isDone, isDefault *bool, description *string) (*ProjectStatus, error) {
	pool, err := OpenDatabase()
	if err != nil {
		return nil, err
	}
	defer CloseDatabase(pool)

	cur, err := GetProjectStatus(projectID, statusID)
	if err != nil {
		return nil, err
	}
	newName := cur.Name
	if name != nil {
		newName = *name
	}
	newDone := cur.IsDone
	if isDone != nil {
		newDone = *isDone
	}
	newDefault := cur.IsDefault
	if isDefault != nil {
		newDefault = *isDefault
	}
	newDescription := cur.Description
	if description != nil {
		newDescription = *description
	}

	if newDefault {
		if _, err := pool.Exec(context.Background(),
			`UPDATE project_statuses SET is_default = false WHERE project_id = $1 AND id <> $2`,
			projectID, statusID); err != nil {
			return nil, err
		}
	}

	_, err = pool.Exec(context.Background(),
		`UPDATE project_statuses SET name = $1, description = $2, is_done = $3, is_default = $4 WHERE id = $5 AND project_id = $6`,
		newName, newDescription, newDone, newDefault, statusID, projectID)
	if err != nil {
		return nil, err
	}
	return GetProjectStatus(projectID, statusID)
}

// CountTasksWithStatus returns tasks currently in a status column.
func CountTasksWithStatus(statusID int) (int, error) {
	pool, err := OpenDatabase()
	if err != nil {
		return 0, err
	}
	defer CloseDatabase(pool)
	var n int
	err = pool.QueryRow(context.Background(),
		`SELECT COUNT(*) FROM tasks WHERE status_id = $1`, statusID).Scan(&n)
	return n, err
}

// MoveTasksFromStatus reassigns all tasks from one status to another.
func MoveTasksFromStatus(fromStatusID, toStatusID int) error {
	pool, err := OpenDatabase()
	if err != nil {
		return err
	}
	defer CloseDatabase(pool)
	_, err = pool.Exec(context.Background(),
		`UPDATE tasks SET status_id = $1, date_modified = NOW() AT TIME ZONE 'UTC' WHERE status_id = $2`,
		toStatusID, fromStatusID)
	return err
}

// DeleteProjectStatus removes a status row.
func DeleteProjectStatus(projectID, statusID int) error {
	pool, err := OpenDatabase()
	if err != nil {
		return err
	}
	defer CloseDatabase(pool)
	tag, err := pool.Exec(context.Background(),
		`DELETE FROM project_statuses WHERE id = $1 AND project_id = $2`, statusID, projectID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("status not found")
	}
	return nil
}

// ReorderProjectStatuses sets positions from ordered status IDs.
func ReorderProjectStatuses(projectID int, orderedIDs []int) error {
	pool, err := OpenDatabase()
	if err != nil {
		return err
	}
	defer CloseDatabase(pool)

	existing, err := ListProjectStatuses(projectID)
	if err != nil {
		return err
	}
	if len(orderedIDs) != len(existing) {
		return fmt.Errorf("status list mismatch")
	}
	have := make(map[int]struct{}, len(existing))
	for _, s := range existing {
		have[s.ID] = struct{}{}
	}
	for _, id := range orderedIDs {
		if _, ok := have[id]; !ok {
			return fmt.Errorf("status %d not in project", id)
		}
	}

	tx, err := pool.Begin(context.Background())
	if err != nil {
		return err
	}
	defer tx.Rollback(context.Background())

	for i, id := range orderedIDs {
		if _, err := tx.Exec(context.Background(),
			`UPDATE project_statuses SET position = $1 WHERE id = $2 AND project_id = $3`,
			i, id, projectID); err != nil {
			return err
		}
	}
	return tx.Commit(context.Background())
}

// AssignTaskStatusAndCompleted sets status_id and completed together.
func AssignTaskStatusAndCompleted(taskID, statusID int, completed bool) error {
	pool, err := OpenDatabase()
	if err != nil {
		return err
	}
	defer CloseDatabase(pool)
	_, err = pool.Exec(context.Background(),
		`UPDATE tasks SET status_id = $1, completed = $2, date_modified = NOW() AT TIME ZONE 'UTC' WHERE id = $3`,
		statusID, completed, taskID)
	return err
}

// ClearTaskWorkflowFields clears status/estimate when leaving a kanban project.
func ClearTaskWorkflowFields(taskID int) error {
	pool, err := OpenDatabase()
	if err != nil {
		return err
	}
	defer CloseDatabase(pool)
	_, err = pool.Exec(context.Background(),
		`UPDATE tasks SET status_id = NULL, estimate_points = NULL, sprint_id = NULL, date_modified = NOW() AT TIME ZONE 'UTC' WHERE id = $1`,
		taskID)
	return err
}

// ClearTaskWorkflowFieldsForProjectANDChildren clears workflow fields for a root and its children.
func ClearTaskWorkflowFieldsForTasks(taskIDs []int) error {
	if len(taskIDs) == 0 {
		return nil
	}
	pool, err := OpenDatabase()
	if err != nil {
		return err
	}
	defer CloseDatabase(pool)
	_, err = pool.Exec(context.Background(),
		`UPDATE tasks SET status_id = NULL, estimate_points = NULL, sprint_id = NULL, date_modified = NOW() AT TIME ZONE 'UTC'
		 WHERE id = ANY($1)`, taskIDs)
	return err
}

// SetTaskEstimatePoints sets or clears estimate_points.
func SetTaskEstimatePoints(taskID int, points *int) error {
	pool, err := OpenDatabase()
	if err != nil {
		return err
	}
	defer CloseDatabase(pool)
	_, err = pool.Exec(context.Background(),
		`UPDATE tasks SET estimate_points = $1, date_modified = NOW() AT TIME ZONE 'UTC' WHERE id = $2`,
		points, taskID)
	return err
}

// GetWorkflowFieldsForTasks loads kanban fields for a set of task IDs.
func GetWorkflowFieldsForTasks(taskIDs []int) (map[int]TaskWorkflowFields, error) {
	out := make(map[int]TaskWorkflowFields)
	if len(taskIDs) == 0 {
		return out, nil
	}
	pool, err := OpenDatabase()
	if err != nil {
		return nil, err
	}
	defer CloseDatabase(pool)

	rows, err := pool.Query(context.Background(),
		`SELECT t.id, t.status_id, COALESCE(s.name, ''), t.estimate_points,
		        COALESCE(p.workflow_mode, 'classic'),
		        COALESCE((SELECT SUM(minutes) FROM task_time_entries e WHERE e.task_id = t.id), 0),
		        t.claimed_by, COALESCE(cu.user_name, cu.email, ''),
		        t.sprint_id, COALESCE(sp.name, '')
		 FROM tasks t
		 LEFT JOIN project_statuses s ON s.id = t.status_id
		 LEFT JOIN projects p ON p.id = t.project_id
		 LEFT JOIN users cu ON cu.id = t.claimed_by
		 LEFT JOIN project_sprints sp ON sp.id = t.sprint_id
		 WHERE t.id = ANY($1)`, taskIDs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var id int
		var statusID sql.NullInt64
		var statusName string
		var estimate sql.NullInt64
		var mode string
		var spent int
		var claimedBy sql.NullInt64
		var claimedByName string
		var sprintID sql.NullInt64
		var sprintName string
		if err := rows.Scan(&id, &statusID, &statusName, &estimate, &mode, &spent, &claimedBy, &claimedByName, &sprintID, &sprintName); err != nil {
			return nil, err
		}
		f := TaskWorkflowFields{
			StatusName:       statusName,
			TimeSpentMinutes: spent,
			ProjectWorkflow:  mode,
			ClaimedByName:    claimedByName,
			SprintName:       sprintName,
		}
		if statusID.Valid {
			f.StatusID = int(statusID.Int64)
		}
		if estimate.Valid {
			v := int(estimate.Int64)
			f.EstimatePoints = &v
		}
		if claimedBy.Valid {
			f.ClaimedBy = int(claimedBy.Int64)
		}
		if sprintID.Valid {
			f.SprintID = int(sprintID.Int64)
		}
		out[id] = f
	}
	return out, rows.Err()
}

// SetTaskClaimedBy sets or clears the claimer for a task.
func SetTaskClaimedBy(taskID int, claimedBy *int) error {
	pool, err := OpenDatabase()
	if err != nil {
		return err
	}
	defer CloseDatabase(pool)

	var arg interface{}
	if claimedBy != nil && *claimedBy > 0 {
		arg = *claimedBy
	}
	_, err = pool.Exec(context.Background(),
		`UPDATE tasks SET claimed_by = $1, date_modified = NOW() AT TIME ZONE 'UTC' WHERE id = $2`,
		arg, taskID)
	return err
}

// GetTaskClaimedBy returns the current claimer user id (0 if none).
func GetTaskClaimedBy(taskID int) (int, error) {
	pool, err := OpenDatabase()
	if err != nil {
		return 0, err
	}
	defer CloseDatabase(pool)

	var claimed sql.NullInt64
	err = pool.QueryRow(context.Background(),
		`SELECT claimed_by FROM tasks WHERE id = $1`, taskID).Scan(&claimed)
	if err != nil {
		return 0, err
	}
	if !claimed.Valid {
		return 0, nil
	}
	return int(claimed.Int64), nil
}

// ListTaskTimeEntries returns time entries for a task newest first.
func ListTaskTimeEntries(taskID int) ([]TaskTimeEntry, error) {
	pool, err := OpenDatabase()
	if err != nil {
		return nil, err
	}
	defer CloseDatabase(pool)

	rows, err := pool.Query(context.Background(),
		`SELECT e.id, e.task_id, e.user_id, e.minutes, COALESCE(e.note,''), e.created_at,
		        COALESCE(u.email,''), COALESCE(u.user_name,'')
		 FROM task_time_entries e
		 JOIN users u ON u.id = e.user_id
		 WHERE e.task_id = $1
		 ORDER BY e.created_at DESC, e.id DESC`, taskID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []TaskTimeEntry
	for rows.Next() {
		var e TaskTimeEntry
		if err := rows.Scan(&e.ID, &e.TaskID, &e.UserID, &e.Minutes, &e.Note, &e.CreatedAt,
			&e.UserEmail, &e.UserName); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// CreateTaskTimeEntry inserts a time entry.
func CreateTaskTimeEntry(taskID, userID, minutes int, note string) (*TaskTimeEntry, error) {
	pool, err := OpenDatabase()
	if err != nil {
		return nil, err
	}
	defer CloseDatabase(pool)

	note = strings.TrimSpace(note)
	var e TaskTimeEntry
	err = pool.QueryRow(context.Background(),
		`INSERT INTO task_time_entries (task_id, user_id, minutes, note)
		 VALUES ($1, $2, $3, $4)
		 RETURNING id, task_id, user_id, minutes, COALESCE(note,''), created_at`,
		taskID, userID, minutes, note).Scan(
		&e.ID, &e.TaskID, &e.UserID, &e.Minutes, &e.Note, &e.CreatedAt)
	if err != nil {
		return nil, err
	}
	return &e, nil
}

// GetTaskTimeEntry loads a single entry.
func GetTaskTimeEntry(entryID int) (*TaskTimeEntry, error) {
	pool, err := OpenDatabase()
	if err != nil {
		return nil, err
	}
	defer CloseDatabase(pool)

	var e TaskTimeEntry
	err = pool.QueryRow(context.Background(),
		`SELECT id, task_id, user_id, minutes, COALESCE(note,''), created_at
		 FROM task_time_entries WHERE id = $1`, entryID).Scan(
		&e.ID, &e.TaskID, &e.UserID, &e.Minutes, &e.Note, &e.CreatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) || errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("time entry not found")
		}
		return nil, err
	}
	return &e, nil
}

// DeleteTaskTimeEntry deletes an entry by id.
func DeleteTaskTimeEntry(entryID int) error {
	pool, err := OpenDatabase()
	if err != nil {
		return err
	}
	defer CloseDatabase(pool)
	tag, err := pool.Exec(context.Background(),
		`DELETE FROM task_time_entries WHERE id = $1`, entryID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("time entry not found")
	}
	return nil
}

// SumTaskTimeMinutes returns total minutes logged on a task.
func SumTaskTimeMinutes(taskID int) (int, error) {
	pool, err := OpenDatabase()
	if err != nil {
		return 0, err
	}
	defer CloseDatabase(pool)
	var n int
	err = pool.QueryRow(context.Background(),
		`SELECT COALESCE(SUM(minutes),0) FROM task_time_entries WHERE task_id = $1`, taskID).Scan(&n)
	return n, err
}
