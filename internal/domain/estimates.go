package domain

import (
	"database/sql"
	"fmt"
	"math"
	"strconv"

	"GoTodo/internal/storage"
)

// ErrInvalidRefinement rejects an unknown refinement state.
var ErrInvalidRefinement = fmt.Errorf("%w: refinement must be %q, %q, or empty",
	ErrValidation, storage.RefinementNeeded, storage.RefinementRefined)

// ValidateEstimateHours enforces quarter-hour estimates between 0 and storage.MaxEstimateHours.
func ValidateEstimateHours(h float64) error {
	if !storage.IsQuarterHour(h) || h > storage.MaxEstimateHours {
		return fmt.Errorf("%w: estimate_hours must be 0-%g in quarter hours", ErrValidation, storage.MaxEstimateHours)
	}
	return nil
}

// ValidateHoursPerPoint enforces a positive quarter-hour points-to-hours rate.
func ValidateHoursPerPoint(h float64) error {
	if h <= 0 || !storage.IsQuarterHour(h) || h > storage.MaxHoursPerPoint {
		return fmt.Errorf("%w: hours_per_point must be 0.25-%g in quarter hours", ErrValidation, storage.MaxHoursPerPoint)
	}
	return nil
}

func requireKanbanProject(projectID int, what string) error {
	if projectID <= 0 {
		return fmt.Errorf("%w: %s require a kanban project", ErrValidation, what)
	}
	mode, err := storage.GetProjectWorkflowMode(projectID)
	if err != nil {
		return err
	}
	if mode != storage.WorkflowKanban {
		return fmt.Errorf("%w: %s require a kanban project", ErrValidation, what)
	}
	return nil
}

// requireRefinementEnabled fails unless refinement is on for the site and this kanban project.
func requireRefinementEnabled(projectID int) error {
	if err := requireKanbanProject(projectID, "refinement flags"); err != nil {
		return err
	}
	on, err := storage.CeremonyEnabledForProject(projectID, storage.CeremonyRefinement)
	if err != nil {
		return err
	}
	if !on {
		return fmt.Errorf("%w: refinement is not enabled for this project", ErrValidation)
	}
	return nil
}

// applyCreateRefinement sets the flag on a new kanban task: the explicit value when given,
// otherwise "needs refinement" when the project asks for it.
func applyCreateRefinement(taskID, projectID int, explicit *string) error {
	if explicit != nil {
		if *explicit == "" {
			return nil
		}
		if err := requireRefinementEnabled(projectID); err != nil {
			return err
		}
		return storage.SetTaskRefinementState(taskID, *explicit)
	}
	on, err := storage.CeremonyEnabledForProject(projectID, storage.CeremonyRefinement)
	if err != nil || !on {
		return err
	}
	settings, err := storage.GetProjectEstimateSettings(projectID)
	if err != nil {
		return err
	}
	if !settings.NewTasksNeedRefinement {
		return nil
	}
	return storage.SetTaskRefinementState(taskID, storage.RefinementNeeded)
}

func sameNullFloat64(a, b sql.NullFloat64) bool {
	if a.Valid != b.Valid {
		return false
	}
	return !a.Valid || math.Abs(a.Float64-b.Float64) < 1e-9
}

func formatNullHours(v sql.NullFloat64) string {
	if !v.Valid {
		return ""
	}
	return strconv.FormatFloat(v.Float64, 'f', -1, 64)
}
