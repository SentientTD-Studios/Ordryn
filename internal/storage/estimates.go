package storage

import (
	"context"
	"fmt"
	"math"
)

const (
	// MaxEstimateHours caps a task estimate.
	MaxEstimateHours = 999.75
	// MaxHoursPerPoint caps the project points-to-hours rate.
	MaxHoursPerPoint = 100.0
	// DefaultHoursPerPoint is the rate legacy point estimates convert at until a manager changes it.
	DefaultHoursPerPoint = 1.0

	RefinementNeeded  = "needs_refinement"
	RefinementRefined = "refined"
)

// IsQuarterHour reports whether h is a non-negative multiple of 0.25.
func IsQuarterHour(h float64) bool {
	if h < 0 || math.IsNaN(h) || math.IsInf(h, 0) {
		return false
	}
	q := h * 4
	return math.Abs(q-math.Round(q)) < 1e-9
}

// ValidRefinementState reports whether s is a stored refinement state ("" means unset).
func ValidRefinementState(s string) bool {
	return s == "" || s == RefinementNeeded || s == RefinementRefined
}

// MigrateTasksAddEstimateHours moves estimates from points to hours and adds the refinement flag.
//
// estimate_points is kept as a legacy column: a non-NULL value means the hours were derived from
// points at the project's hours_per_point rate, so changing the rate re-derives them. Setting hours
// directly clears the legacy points. The first run also imports values from the Estimate extension
// (custom field estimate.points) into the legacy column.
func MigrateTasksAddEstimateHours() error {
	pool, err := OpenDatabase()
	if err != nil {
		return err
	}
	defer CloseDatabase(pool)
	ctx := context.Background()

	var firstRun bool
	if err := pool.QueryRow(ctx,
		`SELECT NOT EXISTS (SELECT 1 FROM information_schema.columns
		  WHERE table_name = 'tasks' AND column_name = 'estimate_hours')`).Scan(&firstRun); err != nil {
		return err
	}

	stmts := []string{
		fmt.Sprintf(`ALTER TABLE projects ADD COLUMN IF NOT EXISTS hours_per_point DOUBLE PRECISION NOT NULL DEFAULT %g`, DefaultHoursPerPoint),
		`ALTER TABLE projects ADD COLUMN IF NOT EXISTS new_tasks_need_refinement BOOLEAN NOT NULL DEFAULT FALSE`,
		`ALTER TABLE tasks ADD COLUMN IF NOT EXISTS estimate_hours DOUBLE PRECISION`,
		`ALTER TABLE tasks ADD COLUMN IF NOT EXISTS refinement_state VARCHAR(20)`,
		`ALTER TABLE task_templates ADD COLUMN IF NOT EXISTS estimate_hours DOUBLE PRECISION`,
		`CREATE INDEX IF NOT EXISTS idx_tasks_project_refinement ON tasks (project_id, refinement_state) WHERE refinement_state IS NOT NULL`,
	}
	for _, s := range stmts {
		if _, err := pool.Exec(ctx, s); err != nil {
			return fmt.Errorf("estimate hours migration %q: %w", s, err)
		}
	}
	if !firstRun {
		return nil
	}

	importStmts := []string{
		// Estimate extension values fill tasks that have no core estimate.
		`UPDATE tasks t SET estimate_points = LEAST((v.value #>> '{}')::int, 1000)
		 FROM custom_field_values v
		 WHERE v.task_id = t.id AND v.field_key = 'estimate.points'
		   AND t.estimate_points IS NULL
		   AND (v.value #>> '{}') ~ '^[0-9]{1,6}$'`,
		`UPDATE tasks t SET estimate_hours = LEAST(t.estimate_points * p.hours_per_point, $1)
		 FROM projects p
		 WHERE p.id = t.project_id AND t.estimate_points IS NOT NULL`,
		`UPDATE task_templates tt SET estimate_hours = LEAST(tt.estimate_points * COALESCE(p.hours_per_point, $2::double precision), $1)
		 FROM task_templates src LEFT JOIN projects p ON p.id = src.project_id
		 WHERE src.id = tt.id AND tt.estimate_points IS NOT NULL`,
	}
	if _, err := pool.Exec(ctx, importStmts[0]); err != nil {
		return fmt.Errorf("import estimate extension values: %w", err)
	}
	if _, err := pool.Exec(ctx, importStmts[1], MaxEstimateHours); err != nil {
		return fmt.Errorf("convert task estimates to hours: %w", err)
	}
	if _, err := pool.Exec(ctx, importStmts[2], MaxEstimateHours, DefaultHoursPerPoint); err != nil {
		return fmt.Errorf("convert template estimates to hours: %w", err)
	}
	return nil
}

// SetTaskEstimateHours sets or clears a task estimate in hours and drops any legacy point value.
func SetTaskEstimateHours(taskID int, hours *float64) error {
	pool, err := OpenDatabase()
	if err != nil {
		return err
	}
	defer CloseDatabase(pool)
	_, err = pool.Exec(context.Background(),
		`UPDATE tasks SET estimate_hours = $1, estimate_points = NULL, date_modified = NOW() AT TIME ZONE 'UTC' WHERE id = $2`,
		hours, taskID)
	return err
}

// SetTaskRefinementState sets ("needs_refinement" / "refined") or clears ("") a task's refinement flag.
func SetTaskRefinementState(taskID int, state string) error {
	if !ValidRefinementState(state) {
		return fmt.Errorf("invalid refinement state %q", state)
	}
	pool, err := OpenDatabase()
	if err != nil {
		return err
	}
	defer CloseDatabase(pool)
	var arg interface{}
	if state != "" {
		arg = state
	}
	_, err = pool.Exec(context.Background(),
		`UPDATE tasks SET refinement_state = $1, date_modified = NOW() AT TIME ZONE 'UTC' WHERE id = $2`,
		arg, taskID)
	return err
}

// GetTaskRefinementState returns a task's refinement flag ("" when unset).
func GetTaskRefinementState(taskID int) (string, error) {
	pool, err := OpenDatabase()
	if err != nil {
		return "", err
	}
	defer CloseDatabase(pool)
	var state string
	err = pool.QueryRow(context.Background(),
		`SELECT COALESCE(refinement_state, '') FROM tasks WHERE id = $1`, taskID).Scan(&state)
	return state, err
}

// ProjectEstimateSettings are the per-project estimate and refinement defaults.
type ProjectEstimateSettings struct {
	HoursPerPoint          float64
	NewTasksNeedRefinement bool
}

// GetProjectEstimateSettings loads a project's estimate settings.
func GetProjectEstimateSettings(projectID int) (ProjectEstimateSettings, error) {
	out := ProjectEstimateSettings{HoursPerPoint: DefaultHoursPerPoint}
	pool, err := OpenDatabase()
	if err != nil {
		return out, err
	}
	defer CloseDatabase(pool)
	err = pool.QueryRow(context.Background(),
		`SELECT hours_per_point, new_tasks_need_refinement FROM projects WHERE id = $1`, projectID).
		Scan(&out.HoursPerPoint, &out.NewTasksNeedRefinement)
	return out, err
}

// SetProjectHoursPerPoint changes the points-to-hours rate and re-derives every estimate that
// still comes from legacy points (tasks and project-scoped templates).
func SetProjectHoursPerPoint(projectID int, rate float64) error {
	pool, err := OpenDatabase()
	if err != nil {
		return err
	}
	defer CloseDatabase(pool)
	ctx := context.Background()
	tx, err := pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `UPDATE projects SET hours_per_point = $1 WHERE id = $2`, rate, projectID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx,
		`UPDATE tasks SET estimate_hours = LEAST(estimate_points * $1, $3)
		 WHERE project_id = $2 AND estimate_points IS NOT NULL`, rate, projectID, MaxEstimateHours); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx,
		`UPDATE task_templates SET estimate_hours = LEAST(estimate_points * $1, $3)
		 WHERE project_id = $2 AND estimate_points IS NOT NULL`, rate, projectID, MaxEstimateHours); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// SetProjectNewTasksNeedRefinement toggles whether new kanban tasks start flagged "Needs Refinement".
func SetProjectNewTasksNeedRefinement(projectID int, on bool) error {
	pool, err := OpenDatabase()
	if err != nil {
		return err
	}
	defer CloseDatabase(pool)
	_, err = pool.Exec(context.Background(),
		`UPDATE projects SET new_tasks_need_refinement = $1 WHERE id = $2`, on, projectID)
	return err
}
