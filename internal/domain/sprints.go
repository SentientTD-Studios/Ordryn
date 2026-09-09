package domain

import (
	"context"
	"fmt"
	"strings"
	"time"

	"GoTodo/internal/live"
	"GoTodo/internal/storage"
)

// CreateProjectSprintInput is the create payload for a sprint.
type CreateProjectSprintInput struct {
	Name        string
	Description string
	StartDate   string
	EndDate     string
	LockDate    string
}

// UpdateProjectSprintInput is a partial sprint update.
type UpdateProjectSprintInput struct {
	Name        *string
	Description *string
	StartDate   *string
	EndDate     *string
	// LockDate: nil = leave; pointer to "" = clear; pointer to YYYY-MM-DD = set.
	LockDate *string
}

func normalizeSprintName(raw string) (string, error) {
	name := strings.TrimSpace(raw)
	if name == "" {
		return "", fmt.Errorf("%w: sprint name is required", ErrValidation)
	}
	if len(name) > storage.MaxSprintNameLen {
		return "", fmt.Errorf("%w: sprint name must be %d characters or less", ErrValidation, storage.MaxSprintNameLen)
	}
	return name, nil
}

func normalizeSprintDescription(raw string) (string, error) {
	desc := strings.TrimSpace(raw)
	if len(desc) > storage.MaxSprintDescriptionLen {
		return "", fmt.Errorf("%w: sprint description must be %d characters or less", ErrValidation, storage.MaxSprintDescriptionLen)
	}
	return desc, nil
}

func parseSprintDateRange(startRaw, endRaw string) (time.Time, time.Time, error) {
	start, err := storage.ParseSprintDate(startRaw)
	if err != nil {
		return time.Time{}, time.Time{}, fmt.Errorf("%w: start_date %s", ErrValidation, err.Error())
	}
	end, err := storage.ParseSprintDate(endRaw)
	if err != nil {
		return time.Time{}, time.Time{}, fmt.Errorf("%w: end_date %s", ErrValidation, err.Error())
	}
	if end.Before(start) {
		return time.Time{}, time.Time{}, fmt.Errorf("%w: end_date must be on or after start_date", ErrValidation)
	}
	return start, end, nil
}

func rejectOverlappingSprint(projectID, excludeID int, start, end time.Time) error {
	hit, err := storage.FindOverlappingProjectSprint(projectID, start, end, excludeID)
	if err != nil {
		return err
	}
	if hit == nil {
		return nil
	}
	return fmt.Errorf("%w: dates overlap %s (%s – %s)", ErrValidation, hit.Name,
		storage.FormatSprintDatePtr(hit.StartDate), storage.FormatSprintDatePtr(hit.EndDate))
}

func sprintConflictError(err error) error {
	if err == nil {
		return nil
	}
	low := strings.ToLower(err.Error())
	if strings.Contains(low, "unique") || strings.Contains(low, "duplicate") {
		return fmt.Errorf("%w: a sprint with this name already exists", ErrConflict)
	}
	if strings.Contains(low, "check") {
		return fmt.Errorf("%w: end_date must be on or after start_date", ErrValidation)
	}
	return err
}

// ListProjectSprintsForUser returns sprints if the user can access the project.
func ListProjectSprintsForUser(ctx context.Context, userID, projectID int) ([]storage.ProjectSprint, error) {
	_ = ctx
	proj, err := storage.GetAccessibleProjectByID(projectID, userID)
	if err != nil {
		return nil, ErrNotFound
	}
	if proj.WorkflowMode != storage.WorkflowKanban {
		return []storage.ProjectSprint{}, nil
	}
	return storage.ListProjectSprints(projectID)
}

// CreateProjectSprintForUser adds a sprint (owner only).
func CreateProjectSprintForUser(ctx context.Context, userID, projectID int, in CreateProjectSprintInput) (*storage.ProjectSprint, error) {
	_ = ctx
	if _, err := requireKanbanOwner(projectID, userID); err != nil {
		return nil, err
	}
	name, err := normalizeSprintName(in.Name)
	if err != nil {
		return nil, err
	}
	desc, err := normalizeSprintDescription(in.Description)
	if err != nil {
		return nil, err
	}

	startRaw := strings.TrimSpace(in.StartDate)
	endRaw := strings.TrimSpace(in.EndDate)
	if (startRaw == "" && endRaw != "") || (startRaw != "" && endRaw == "") {
		return nil, fmt.Errorf("%w: both start_date and end_date are required for a dated sprint, or leave both blank for a dateless sprint", ErrValidation)
	}

	var startPtr, endPtr *time.Time
	if startRaw != "" && endRaw != "" {
		start, end, err := parseSprintDateRange(startRaw, endRaw)
		if err != nil {
			return nil, err
		}
		if err := rejectOverlappingSprint(projectID, 0, start, end); err != nil {
			return nil, err
		}
		startPtr = &start
		endPtr = &end
	}

	if startPtr == nil && strings.TrimSpace(in.LockDate) != "" {
		return nil, fmt.Errorf("%w: lock_date cannot be set on a dateless sprint", ErrValidation)
	}

	lockDate, err := parseOptionalLockDate(in.LockDate)
	if err != nil {
		return nil, err
	}
	n, err := storage.CountProjectSprints(projectID)
	if err != nil {
		return nil, err
	}
	if n >= storage.MaxProjectSprints {
		return nil, fmt.Errorf("%w: a maximum of %d sprints is allowed", ErrConflict, storage.MaxProjectSprints)
	}
	s, err := storage.CreateProjectSprint(projectID, name, desc, startPtr, endPtr, lockDate)
	if err != nil {
		return nil, sprintConflictError(err)
	}
	_ = storage.LogProjectEvent(projectID, userID, "sprint_added", map[string]interface{}{
		"sprint_id": s.ID, "name": s.Name,
	})
	live.AfterProjectChange(userID, projectID, live.TypeProjectUpdated)
	return s, nil
}

// UpdateProjectSprintForUser updates a sprint (owner only).
func UpdateProjectSprintForUser(ctx context.Context, userID, projectID, sprintID int, in UpdateProjectSprintInput) (*storage.ProjectSprint, error) {
	_ = ctx
	if _, err := requireKanbanOwner(projectID, userID); err != nil {
		return nil, err
	}
	cur, err := storage.GetProjectSprint(projectID, sprintID)
	if err != nil {
		return nil, ErrNotFound
	}
	var name *string
	if in.Name != nil {
		n, err := normalizeSprintName(*in.Name)
		if err != nil {
			return nil, err
		}
		name = &n
	}
	var description *string
	if in.Description != nil {
		d, err := normalizeSprintDescription(*in.Description)
		if err != nil {
			return nil, err
		}
		description = &d
	}

	var finalStart, finalEnd *time.Time
	if in.StartDate == nil && in.EndDate == nil {
		finalStart = cur.StartDate
		finalEnd = cur.EndDate
	} else {
		startRaw := ""
		if in.StartDate != nil {
			startRaw = strings.TrimSpace(*in.StartDate)
		} else if cur.StartDate != nil {
			startRaw = storage.FormatSprintDate(*cur.StartDate)
		}
		endRaw := ""
		if in.EndDate != nil {
			endRaw = strings.TrimSpace(*in.EndDate)
		} else if cur.EndDate != nil {
			endRaw = storage.FormatSprintDate(*cur.EndDate)
		}

		if (startRaw == "" && endRaw != "") || (startRaw != "" && endRaw == "") {
			return nil, fmt.Errorf("%w: both start_date and end_date are required for a dated sprint, or leave both blank for a dateless sprint", ErrValidation)
		}

		if startRaw != "" && endRaw != "" {
			start, end, err := parseSprintDateRange(startRaw, endRaw)
			if err != nil {
				return nil, err
			}
			if err := rejectOverlappingSprint(projectID, sprintID, start, end); err != nil {
				return nil, err
			}
			finalStart = &start
			finalEnd = &end
		}
	}

	lockDate := cur.LockDate
	if in.LockDate != nil {
		parsedLock, err := parseOptionalLockDate(*in.LockDate)
		if err != nil {
			return nil, err
		}
		lockDate = parsedLock
	}

	if finalStart == nil && lockDate != nil {
		if in.LockDate != nil && strings.TrimSpace(*in.LockDate) != "" {
			return nil, fmt.Errorf("%w: lock_date cannot be set on a dateless sprint", ErrValidation)
		}
		lockDate = nil
	}

	s, err := storage.UpdateProjectSprint(projectID, sprintID, name, description, finalStart, finalEnd, lockDate)
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "sprint not found") {
			return nil, ErrNotFound
		}
		return nil, sprintConflictError(err)
	}
	_ = storage.LogProjectEvent(projectID, userID, "sprint_updated", map[string]interface{}{
		"sprint_id": s.ID, "name": s.Name,
	})
	live.AfterProjectChange(userID, projectID, live.TypeProjectUpdated)
	return s, nil
}

// DeleteProjectSprintForUser deletes a sprint, optionally moving tasks first.
func DeleteProjectSprintForUser(ctx context.Context, userID, projectID, sprintID int, moveToSprintID *int) error {
	_ = ctx
	if _, err := requireKanbanOwner(projectID, userID); err != nil {
		return err
	}
	cur, err := storage.GetProjectSprint(projectID, sprintID)
	if err != nil {
		return ErrNotFound
	}

	n, err := storage.CountTasksWithSprint(sprintID)
	if err != nil {
		return err
	}
	if n > 0 && moveToSprintID != nil {
		if *moveToSprintID == sprintID {
			return fmt.Errorf("%w: move_to_sprint_id must be a different sprint", ErrValidation)
		}
		if *moveToSprintID > 0 {
			if _, err := storage.GetProjectSprint(projectID, *moveToSprintID); err != nil {
				return fmt.Errorf("%w: invalid move_to_sprint_id", ErrValidation)
			}
		}
		if err := storage.MoveTasksFromSprint(sprintID, moveToSprintID); err != nil {
			return err
		}
	}

	if err := storage.DeleteProjectSprint(projectID, sprintID); err != nil {
		return err
	}
	_ = storage.LogProjectEvent(projectID, userID, "sprint_deleted", map[string]interface{}{
		"name": cur.Name,
	})
	live.AfterProjectChange(userID, projectID, live.TypeProjectUpdated)
	return nil
}

func parseOptionalLockDate(raw string) (*time.Time, error) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return nil, nil
	}
	t, err := storage.ParseSprintDate(s)
	if err != nil {
		return nil, fmt.Errorf("%w: lock_date %s", ErrValidation, err.Error())
	}
	return &t, nil
}

func rejectLockedSprintAssignment(userID, projectID int, sprint *storage.ProjectSprint) error {
	if sprint == nil || !storage.SprintIsLocked(sprint.LockDate, time.Now()) {
		return nil
	}
	proj, err := storage.GetAccessibleProjectByID(projectID, userID)
	if err != nil {
		return ErrNotFound
	}
	if storage.RoleCanManage(proj.Role) {
		return nil
	}
	return fmt.Errorf("%w: sprint is locked; only the project owner can add items", ErrForbidden)
}

func applyTaskSprint(taskID, projectID, userID int, sprintID *int) error {
	if sprintID == nil || *sprintID <= 0 {
		return storage.SetTaskSprintID(taskID, nil)
	}
	if projectID <= 0 {
		return fmt.Errorf("%w: sprint requires a kanban project", ErrValidation)
	}
	mode, err := storage.GetProjectWorkflowMode(projectID)
	if err != nil {
		return err
	}
	if mode != storage.WorkflowKanban {
		return fmt.Errorf("%w: sprint requires a kanban project", ErrValidation)
	}
	sprint, err := storage.GetProjectSprint(projectID, *sprintID)
	if err != nil {
		return fmt.Errorf("%w: invalid sprint_id", ErrValidation)
	}
	current, err := storage.GetTaskSprintID(taskID)
	if err != nil {
		return err
	}
	if current != *sprintID {
		if err := rejectLockedSprintAssignment(userID, projectID, sprint); err != nil {
			return err
		}
	}
	return storage.SetTaskSprintID(taskID, sprintID)
}
