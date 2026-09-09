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
	MaxSprintNameLen        = 60
	MaxSprintDescriptionLen = 80
	MaxProjectSprints       = 50

	projectSprintSelectCols = `id, project_id, name, description, start_date, end_date, lock_date, created_at`
)

// ProjectSprint is a named date range (or dateless sprint) on a kanban project.
type ProjectSprint struct {
	ID          int
	ProjectID   int
	Name        string
	Description string
	StartDate   *time.Time
	EndDate     *time.Time
	LockDate    *time.Time
	CreatedAt   time.Time
	TaskCount   int
}

func scanProjectSprint(row projectStatusScanner, s *ProjectSprint) error {
	var start, end, lock sql.NullTime
	if err := row.Scan(&s.ID, &s.ProjectID, &s.Name, &s.Description, &start, &end, &lock, &s.CreatedAt); err != nil {
		return err
	}
	s.StartDate = nullTimePtr(start)
	s.EndDate = nullTimePtr(end)
	s.LockDate = nullTimePtr(lock)
	return nil
}

func nullTimePtr(t sql.NullTime) *time.Time {
	if !t.Valid {
		return nil
	}
	v := t.Time
	return &v
}

func sprintDateArg(t *time.Time) interface{} {
	if t == nil {
		return nil
	}
	return FormatSprintDate(*t)
}

// FormatSprintDate returns a DATE value as YYYY-MM-DD in UTC.
func FormatSprintDate(t time.Time) string {
	return t.UTC().Format("2006-01-02")
}

// FormatSprintDatePtr returns a DATE value as YYYY-MM-DD in UTC, or "" if nil.
func FormatSprintDatePtr(t *time.Time) string {
	if t == nil {
		return ""
	}
	return FormatSprintDate(*t)
}

// SprintIsActive reports whether today (UTC calendar date) falls in [start, end].
// Dateless sprints (nil start or end) are never active by date.
func SprintIsActive(start, end *time.Time, now time.Time) bool {
	if start == nil || end == nil {
		return false
	}
	today := now.UTC().Format("2006-01-02")
	s := FormatSprintDate(*start)
	e := FormatSprintDate(*end)
	return today >= s && today <= e
}

// SprintIsLocked reports whether today (UTC calendar date) is on or after lockDate.
// A nil lockDate means the sprint never locks automatically.
func SprintIsLocked(lockDate *time.Time, now time.Time) bool {
	if lockDate == nil {
		return false
	}
	today := now.UTC().Format("2006-01-02")
	return today >= FormatSprintDate(*lockDate)
}

// SprintDatesOverlap reports whether two inclusive [start, end] ranges share a day.
func SprintDatesOverlap(aStart, aEnd, bStart, bEnd time.Time) bool {
	as := FormatSprintDate(aStart)
	ae := FormatSprintDate(aEnd)
	bs := FormatSprintDate(bStart)
	be := FormatSprintDate(bEnd)
	return as <= be && bs <= ae
}

// FindOverlappingProjectSprint returns one sprint whose inclusive dates overlap
// [start, end]. excludeID skips that sprint (use 0 when creating).
func FindOverlappingProjectSprint(projectID int, start, end time.Time, excludeID int) (*ProjectSprint, error) {
	pool, err := OpenDatabase()
	if err != nil {
		return nil, err
	}
	defer CloseDatabase(pool)

	query := `SELECT ` + projectSprintSelectCols + `
		 FROM project_sprints
		 WHERE project_id = $1
		   AND start_date IS NOT NULL
		   AND end_date IS NOT NULL
		   AND start_date <= $3::date
		   AND end_date >= $2::date`
	args := []interface{}{projectID, FormatSprintDate(start), FormatSprintDate(end)}
	if excludeID > 0 {
		query += ` AND id <> $4`
		args = append(args, excludeID)
	}
	query += ` ORDER BY start_date ASC, id ASC LIMIT 1`

	var s ProjectSprint
	err = scanProjectSprint(pool.QueryRow(context.Background(), query, args...), &s)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) || errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return &s, nil
}

// CreateProjectSprintsTable creates the project_sprints table.
func CreateProjectSprintsTable() error {
	pool, err := OpenDatabase()
	if err != nil {
		return err
	}
	defer CloseDatabase(pool)

	stmts := []string{
		`CREATE TABLE IF NOT EXISTS project_sprints (
			id SERIAL PRIMARY KEY,
			project_id INTEGER NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
			name TEXT NOT NULL,
			description TEXT NOT NULL DEFAULT '',
			start_date DATE,
			end_date DATE,
			lock_date DATE,
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			CHECK (end_date IS NULL OR start_date IS NULL OR end_date >= start_date)
		)`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_project_sprints_project_lower_name
			ON project_sprints (project_id, lower(name))`,
		`CREATE INDEX IF NOT EXISTS idx_project_sprints_project_dates
			ON project_sprints (project_id, start_date, end_date)`,
	}
	for _, s := range stmts {
		if _, err := pool.Exec(context.Background(), s); err != nil {
			return fmt.Errorf("failed to create project_sprints: %v", err)
		}
	}
	return nil
}

// MigrateProjectSprintsAllowDateless alters start_date and end_date columns to allow NULL.
func MigrateProjectSprintsAllowDateless() error {
	pool, err := OpenDatabase()
	if err != nil {
		return err
	}
	defer CloseDatabase(pool)

	_, err = pool.Exec(context.Background(),
		`ALTER TABLE project_sprints ALTER COLUMN start_date DROP NOT NULL, ALTER COLUMN end_date DROP NOT NULL`)
	if err != nil {
		return fmt.Errorf("failed to alter project_sprints dates to nullable: %v", err)
	}
	return nil
}

// MigrateProjectSprintsAddDescription adds an optional short description on sprints.
func MigrateProjectSprintsAddDescription() error {
	pool, err := OpenDatabase()
	if err != nil {
		return err
	}
	defer CloseDatabase(pool)

	_, err = pool.Exec(context.Background(),
		`ALTER TABLE project_sprints ADD COLUMN IF NOT EXISTS description TEXT NOT NULL DEFAULT ''`)
	if err != nil {
		return fmt.Errorf("failed to add sprint description: %v", err)
	}
	return nil
}

// MigrateProjectSprintsAddLockDate adds an optional lock date on sprints.
func MigrateProjectSprintsAddLockDate() error {
	pool, err := OpenDatabase()
	if err != nil {
		return err
	}
	defer CloseDatabase(pool)

	_, err = pool.Exec(context.Background(),
		`ALTER TABLE project_sprints ADD COLUMN IF NOT EXISTS lock_date DATE`)
	if err != nil {
		return fmt.Errorf("failed to add sprint lock_date: %v", err)
	}
	return nil
}

// MigrateTasksAddSprintID adds tasks.sprint_id referencing project_sprints.
func MigrateTasksAddSprintID() error {
	pool, err := OpenDatabase()
	if err != nil {
		return err
	}
	defer CloseDatabase(pool)

	if _, err := pool.Exec(context.Background(),
		`ALTER TABLE tasks ADD COLUMN IF NOT EXISTS sprint_id INTEGER`); err != nil {
		return fmt.Errorf("failed to add sprint_id: %v", err)
	}

	var exists bool
	err = pool.QueryRow(context.Background(),
		`SELECT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'fk_tasks_sprint')`).Scan(&exists)
	if err != nil {
		return err
	}
	if !exists {
		_, err = pool.Exec(context.Background(),
			`ALTER TABLE tasks ADD CONSTRAINT fk_tasks_sprint
			 FOREIGN KEY (sprint_id) REFERENCES project_sprints(id) ON DELETE SET NULL`)
		if err != nil {
			return fmt.Errorf("failed to add sprint FK: %v", err)
		}
	}

	if _, err := pool.Exec(context.Background(),
		`CREATE INDEX IF NOT EXISTS idx_tasks_sprint_id ON tasks(sprint_id)`); err != nil {
		return fmt.Errorf("failed to index sprint_id: %v", err)
	}
	return nil
}

// ListProjectSprints returns sprints for a project, dateless sprints first, then newest start date first.
func ListProjectSprints(projectID int) ([]ProjectSprint, error) {
	pool, err := OpenDatabase()
	if err != nil {
		return nil, err
	}
	defer CloseDatabase(pool)

	rows, err := pool.Query(context.Background(),
		`SELECT s.id, s.project_id, s.name, s.description, s.start_date, s.end_date, s.lock_date, s.created_at,
		        COALESCE((SELECT COUNT(*) FROM tasks t WHERE t.sprint_id = s.id AND NOT `+ArchivedTaskExistsSQL("t.id")+`), 0)
		 FROM project_sprints s
		 WHERE s.project_id = $1
		 ORDER BY (s.start_date IS NULL) DESC, s.start_date DESC, s.id DESC`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []ProjectSprint
	for rows.Next() {
		var s ProjectSprint
		var start, end, lock sql.NullTime
		if err := rows.Scan(&s.ID, &s.ProjectID, &s.Name, &s.Description, &start, &end, &lock, &s.CreatedAt, &s.TaskCount); err != nil {
			return nil, err
		}
		s.StartDate = nullTimePtr(start)
		s.EndDate = nullTimePtr(end)
		s.LockDate = nullTimePtr(lock)
		out = append(out, s)
	}
	return out, rows.Err()
}

// CountProjectSprints returns how many sprints a project has.
func CountProjectSprints(projectID int) (int, error) {
	pool, err := OpenDatabase()
	if err != nil {
		return 0, err
	}
	defer CloseDatabase(pool)
	var n int
	err = pool.QueryRow(context.Background(),
		`SELECT COUNT(*) FROM project_sprints WHERE project_id = $1`, projectID).Scan(&n)
	return n, err
}

// GetProjectSprint returns a sprint by id if it belongs to the project.
func GetProjectSprint(projectID, sprintID int) (*ProjectSprint, error) {
	pool, err := OpenDatabase()
	if err != nil {
		return nil, err
	}
	defer CloseDatabase(pool)

	var s ProjectSprint
	err = scanProjectSprint(pool.QueryRow(context.Background(),
		`SELECT `+projectSprintSelectCols+`
		 FROM project_sprints WHERE id = $1 AND project_id = $2`,
		sprintID, projectID), &s)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) || errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("sprint not found")
		}
		return nil, err
	}
	return &s, nil
}

// CreateProjectSprint inserts a sprint.
func CreateProjectSprint(projectID int, name, description string, startDate, endDate, lockDate *time.Time) (*ProjectSprint, error) {
	pool, err := OpenDatabase()
	if err != nil {
		return nil, err
	}
	defer CloseDatabase(pool)

	var s ProjectSprint
	err = scanProjectSprint(pool.QueryRow(context.Background(),
		`INSERT INTO project_sprints (project_id, name, description, start_date, end_date, lock_date)
		 VALUES ($1, $2, $3, $4::date, $5::date, $6::date)
		 RETURNING `+projectSprintSelectCols,
		projectID, name, description, sprintDateArg(startDate), sprintDateArg(endDate), sprintDateArg(lockDate)), &s)
	if err != nil {
		return nil, err
	}
	return &s, nil
}

// UpdateProjectSprint updates mutable sprint fields.
func UpdateProjectSprint(projectID, sprintID int, name, description *string, startDate, endDate, lockDate *time.Time) (*ProjectSprint, error) {
	cur, err := GetProjectSprint(projectID, sprintID)
	if err != nil {
		return nil, err
	}
	newName := cur.Name
	if name != nil {
		newName = *name
	}
	newDescription := cur.Description
	if description != nil {
		newDescription = *description
	}

	pool, err := OpenDatabase()
	if err != nil {
		return nil, err
	}
	defer CloseDatabase(pool)

	_, err = pool.Exec(context.Background(),
		`UPDATE project_sprints SET name = $1, description = $2, start_date = $3::date, end_date = $4::date, lock_date = $5::date
		 WHERE id = $6 AND project_id = $7`,
		newName, newDescription, sprintDateArg(startDate), sprintDateArg(endDate), sprintDateArg(lockDate), sprintID, projectID)
	if err != nil {
		return nil, err
	}
	return GetProjectSprint(projectID, sprintID)
}

// CountTasksWithSprint returns tasks currently assigned to a sprint.
func CountTasksWithSprint(sprintID int) (int, error) {
	pool, err := OpenDatabase()
	if err != nil {
		return 0, err
	}
	defer CloseDatabase(pool)
	var n int
	err = pool.QueryRow(context.Background(),
		`SELECT COUNT(*) FROM tasks WHERE sprint_id = $1`, sprintID).Scan(&n)
	return n, err
}

// CountTasksInBacklog returns active tasks currently in a project's backlog (sprint_id IS NULL).
func CountTasksInBacklog(projectID int) (int, error) {
	pool, err := OpenDatabase()
	if err != nil {
		return 0, err
	}
	defer CloseDatabase(pool)
	var n int
	err = pool.QueryRow(context.Background(),
		`SELECT COUNT(*) FROM tasks t WHERE t.project_id = $1 AND t.sprint_id IS NULL AND NOT `+ArchivedTaskExistsSQL("t.id"),
		projectID).Scan(&n)
	return n, err
}

// MoveTasksFromSprint reassigns all tasks from one sprint to another (or NULL).
func MoveTasksFromSprint(fromSprintID int, toSprintID *int) error {
	pool, err := OpenDatabase()
	if err != nil {
		return err
	}
	defer CloseDatabase(pool)
	var dest interface{}
	if toSprintID != nil && *toSprintID > 0 {
		dest = *toSprintID
	}
	_, err = pool.Exec(context.Background(),
		`UPDATE tasks SET sprint_id = $1, date_modified = NOW() AT TIME ZONE 'UTC' WHERE sprint_id = $2`,
		dest, fromSprintID)
	return err
}

// DeleteProjectSprint removes a sprint row. Tasks are detached via ON DELETE SET NULL
// unless moved first.
func DeleteProjectSprint(projectID, sprintID int) error {
	pool, err := OpenDatabase()
	if err != nil {
		return err
	}
	defer CloseDatabase(pool)
	tag, err := pool.Exec(context.Background(),
		`DELETE FROM project_sprints WHERE id = $1 AND project_id = $2`, sprintID, projectID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("sprint not found")
	}
	return nil
}

// SetTaskSprintID sets or clears a task's sprint assignment.
func SetTaskSprintID(taskID int, sprintID *int) error {
	pool, err := OpenDatabase()
	if err != nil {
		return err
	}
	defer CloseDatabase(pool)
	var arg interface{}
	if sprintID != nil && *sprintID > 0 {
		arg = *sprintID
	}
	_, err = pool.Exec(context.Background(),
		`UPDATE tasks SET sprint_id = $1, date_modified = NOW() AT TIME ZONE 'UTC' WHERE id = $2`,
		arg, taskID)
	return err
}

// GetTaskSprintID returns the current sprint id (0 if none).
func GetTaskSprintID(taskID int) (int, error) {
	pool, err := OpenDatabase()
	if err != nil {
		return 0, err
	}
	defer CloseDatabase(pool)
	var sprint sql.NullInt64
	err = pool.QueryRow(context.Background(),
		`SELECT sprint_id FROM tasks WHERE id = $1`, taskID).Scan(&sprint)
	if err != nil {
		return 0, err
	}
	if !sprint.Valid {
		return 0, nil
	}
	return int(sprint.Int64), nil
}

// ClearProjectSprints removes all sprints for a project (task FKs set null).
func ClearProjectSprints(projectID int) error {
	pool, err := OpenDatabase()
	if err != nil {
		return err
	}
	defer CloseDatabase(pool)
	_, err = pool.Exec(context.Background(),
		`UPDATE tasks SET sprint_id = NULL WHERE project_id = $1`, projectID)
	if err != nil {
		return err
	}
	_, err = pool.Exec(context.Background(),
		`DELETE FROM project_sprints WHERE project_id = $1`, projectID)
	return err
}

// ParseSprintDate parses a YYYY-MM-DD calendar date.
func ParseSprintDate(raw string) (time.Time, error) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return time.Time{}, fmt.Errorf("date is required")
	}
	t, err := time.Parse("2006-01-02", s)
	if err != nil {
		return time.Time{}, fmt.Errorf("date must be YYYY-MM-DD")
	}
	return t, nil
}
