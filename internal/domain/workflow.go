package domain

import (
	"context"
	"fmt"
	"strings"

	"GoTodo/internal/live"
	"GoTodo/internal/storage"
)

// SetProjectWorkflowMode enables or disables kanban for a project (owner only).
func SetProjectWorkflowMode(ctx context.Context, userID, projectID int, mode string) (*storage.ProjectWithAccess, error) {
	_ = ctx
	mode = strings.TrimSpace(strings.ToLower(mode))
	if mode != storage.WorkflowClassic && mode != storage.WorkflowKanban {
		return nil, fmt.Errorf("%w: workflow_mode must be classic or kanban", ErrValidation)
	}

	proj, err := storage.GetAccessibleProjectByID(projectID, userID)
	if err != nil {
		return nil, ErrNotFound
	}
	if !storage.RoleCanManage(proj.Role) {
		return nil, ErrForbidden
	}

	current := proj.WorkflowMode
	if current == "" {
		current = storage.WorkflowClassic
	}
	if current == mode {
		return proj, nil
	}

	if mode == storage.WorkflowKanban {
		if err := enableKanban(projectID); err != nil {
			return nil, err
		}
	} else {
		if err := disableKanban(projectID); err != nil {
			return nil, err
		}
	}

	_ = storage.LogProjectEvent(projectID, userID, "workflow_changed", map[string]interface{}{
		"mode": mode,
	})

	live.AfterProjectChange(userID, projectID, live.TypeProjectUpdated)
	return storage.GetAccessibleProjectByID(projectID, userID)
}

func enableKanban(projectID int) error {
	statuses, err := storage.ListProjectStatuses(projectID)
	if err != nil {
		return err
	}
	if len(statuses) == 0 {
		statuses, err = storage.SeedDefaultProjectStatuses(projectID)
		if err != nil {
			return err
		}
	}

	var defaultID, doneID int
	for _, s := range statuses {
		if s.IsDefault && defaultID == 0 {
			defaultID = s.ID
		}
		if s.IsDone && doneID == 0 {
			doneID = s.ID
		}
	}
	if defaultID == 0 || doneID == 0 {
		return fmt.Errorf("%w: project is missing default or done status", ErrValidation)
	}

	if err := storage.BackfillTaskStatusesForProject(projectID, defaultID, doneID); err != nil {
		return err
	}
	return storage.SetProjectWorkflowMode(projectID, storage.WorkflowKanban)
}

func disableKanban(projectID int) error {
	n, err := storage.CountProjectTasks(projectID)
	if err != nil {
		return err
	}
	if n > 0 {
		return fmt.Errorf("%w: cannot disable kanban while the project has tasks", ErrConflict)
	}
	if err := storage.ClearProjectStatusesAndTaskFields(projectID); err != nil {
		return err
	}
	if err := storage.ClearProjectSprints(projectID); err != nil {
		return err
	}
	return storage.SetProjectWorkflowMode(projectID, storage.WorkflowClassic)
}

// ListProjectStatusesForUser returns statuses if the user can access the project.
func ListProjectStatusesForUser(ctx context.Context, userID, projectID int) ([]storage.ProjectStatus, error) {
	_ = ctx
	proj, err := storage.GetAccessibleProjectByID(projectID, userID)
	if err != nil {
		return nil, ErrNotFound
	}
	if proj.WorkflowMode != storage.WorkflowKanban {
		return []storage.ProjectStatus{}, nil
	}
	return storage.ListProjectStatuses(projectID)
}

// CreateProjectStatusInput is the create payload for a status column.
type CreateProjectStatusInput struct {
	Name        string
	Description string
	IsDone      bool
	IsDefault   bool
}

func normalizeStatusDescription(raw string) (string, error) {
	desc := strings.TrimSpace(raw)
	if len(desc) > storage.MaxStatusDescriptionLen {
		return "", fmt.Errorf("%w: status description must be %d characters or less", ErrValidation, storage.MaxStatusDescriptionLen)
	}
	return desc, nil
}

// CreateProjectStatusForUser adds a status (owner only, max 8).
func CreateProjectStatusForUser(ctx context.Context, userID, projectID int, in CreateProjectStatusInput) (*storage.ProjectStatus, error) {
	_ = ctx
	proj, err := requireKanbanOwner(projectID, userID)
	if err != nil {
		return nil, err
	}
	_ = proj

	name := strings.TrimSpace(in.Name)
	if name == "" {
		return nil, fmt.Errorf("%w: status name is required", ErrValidation)
	}
	if len(name) > storage.MaxStatusNameLen {
		return nil, fmt.Errorf("%w: status name must be %d characters or less", ErrValidation, storage.MaxStatusNameLen)
	}
	desc, err := normalizeStatusDescription(in.Description)
	if err != nil {
		return nil, err
	}
	n, err := storage.CountProjectStatuses(projectID)
	if err != nil {
		return nil, err
	}
	if n >= storage.MaxProjectStatuses {
		return nil, fmt.Errorf("%w: a maximum of %d statuses is allowed", ErrConflict, storage.MaxProjectStatuses)
	}
	s, err := storage.CreateProjectStatus(projectID, name, desc, in.IsDone, in.IsDefault)
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "unique") || strings.Contains(strings.ToLower(err.Error()), "duplicate") {
			return nil, fmt.Errorf("%w: a status with this name already exists", ErrConflict)
		}
		return nil, err
	}
	_ = storage.LogProjectEvent(projectID, userID, "status_added", map[string]interface{}{
		"status_id": s.ID, "name": s.Name,
	})
	live.AfterProjectChange(userID, projectID, live.TypeProjectUpdated)
	return s, nil
}

// UpdateProjectStatusInput is a partial status update.
type UpdateProjectStatusInput struct {
	Name        *string
	Description *string
	IsDone      *bool
	IsDefault   *bool
}

// UpdateProjectStatusForUser updates a status column (owner only).
func UpdateProjectStatusForUser(ctx context.Context, userID, projectID, statusID int, in UpdateProjectStatusInput) (*storage.ProjectStatus, error) {
	_ = ctx
	if _, err := requireKanbanOwner(projectID, userID); err != nil {
		return nil, err
	}
	if in.Name != nil {
		name := strings.TrimSpace(*in.Name)
		if name == "" {
			return nil, fmt.Errorf("%w: status name is required", ErrValidation)
		}
		if len(name) > storage.MaxStatusNameLen {
			return nil, fmt.Errorf("%w: status name must be %d characters or less", ErrValidation, storage.MaxStatusNameLen)
		}
		in.Name = &name
	}
	if in.Description != nil {
		desc, err := normalizeStatusDescription(*in.Description)
		if err != nil {
			return nil, err
		}
		in.Description = &desc
	}

	cur, err := storage.GetProjectStatus(projectID, statusID)
	if err != nil {
		return nil, ErrNotFound
	}

	// Prevent clearing the only default / only done flag.
	if in.IsDefault != nil && !*in.IsDefault && cur.IsDefault {
		return nil, fmt.Errorf("%w: at least one default status is required", ErrValidation)
	}
	if in.IsDone != nil && !*in.IsDone && cur.IsDone {
		others, err := storage.ListProjectStatuses(projectID)
		if err != nil {
			return nil, err
		}
		doneCount := 0
		for _, s := range others {
			if s.IsDone {
				doneCount++
			}
		}
		if doneCount <= 1 {
			return nil, fmt.Errorf("%w: at least one done status is required", ErrValidation)
		}
	}

	s, err := storage.UpdateProjectStatus(projectID, statusID, in.Name, in.IsDone, in.IsDefault, in.Description)
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "unique") || strings.Contains(strings.ToLower(err.Error()), "duplicate") {
			return nil, fmt.Errorf("%w: a status with this name already exists", ErrConflict)
		}
		return nil, err
	}

	// Sync completed for tasks in this column when is_done changes.
	if in.IsDone != nil && *in.IsDone != cur.IsDone {
		_ = syncCompletedForStatus(statusID, *in.IsDone)
	}
	_ = storage.LogProjectEvent(projectID, userID, "status_updated", map[string]interface{}{
		"status_id": s.ID, "name": s.Name,
	})
	live.AfterProjectChange(userID, projectID, live.TypeProjectUpdated)
	return s, nil
}

func syncCompletedForStatus(statusID int, isDone bool) error {
	pool, err := storage.OpenDatabase()
	if err != nil {
		return err
	}
	defer storage.CloseDatabase(pool)
	_, err = pool.Exec(context.Background(),
		`UPDATE tasks SET completed = $1, date_modified = NOW() AT TIME ZONE 'UTC' WHERE status_id = $2`,
		isDone, statusID)
	return err
}

// DeleteProjectStatusForUser deletes a status, optionally moving tasks first.
func DeleteProjectStatusForUser(ctx context.Context, userID, projectID, statusID int, moveToStatusID *int) error {
	_ = ctx
	if _, err := requireKanbanOwner(projectID, userID); err != nil {
		return err
	}
	cur, err := storage.GetProjectStatus(projectID, statusID)
	if err != nil {
		return ErrNotFound
	}

	statuses, err := storage.ListProjectStatuses(projectID)
	if err != nil {
		return err
	}
	if len(statuses) <= 1 {
		return fmt.Errorf("%w: cannot delete the last status", ErrValidation)
	}
	if cur.IsDefault {
		return fmt.Errorf("%w: reassign the default status before deleting it", ErrValidation)
	}
	if cur.IsDone {
		doneCount := 0
		for _, s := range statuses {
			if s.IsDone {
				doneCount++
			}
		}
		if doneCount <= 1 {
			return fmt.Errorf("%w: at least one done status is required", ErrValidation)
		}
	}

	n, err := storage.CountTasksWithStatus(statusID)
	if err != nil {
		return err
	}
	if n > 0 {
		if moveToStatusID == nil || *moveToStatusID <= 0 {
			return fmt.Errorf("%w: move_to_status_id is required when the status has tasks", ErrValidation)
		}
		if *moveToStatusID == statusID {
			return fmt.Errorf("%w: move_to_status_id must be a different status", ErrValidation)
		}
		target, err := storage.GetProjectStatus(projectID, *moveToStatusID)
		if err != nil {
			return fmt.Errorf("%w: invalid move_to_status_id", ErrValidation)
		}
		if err := storage.MoveTasksFromStatus(statusID, target.ID); err != nil {
			return err
		}
		_ = syncCompletedForStatus(target.ID, target.IsDone)
	}

	if err := storage.DeleteProjectStatus(projectID, statusID); err != nil {
		return err
	}
	_ = storage.LogProjectEvent(projectID, userID, "status_deleted", map[string]interface{}{
		"name": cur.Name,
	})
	live.AfterProjectChange(userID, projectID, live.TypeProjectUpdated)
	return nil
}

// ReorderProjectStatusesForUser reorders status columns (owner only).
func ReorderProjectStatusesForUser(ctx context.Context, userID, projectID int, orderedIDs []int) error {
	_ = ctx
	if _, err := requireKanbanOwner(projectID, userID); err != nil {
		return err
	}
	if len(orderedIDs) == 0 {
		return fmt.Errorf("%w: status_ids is required", ErrValidation)
	}
	if err := storage.ReorderProjectStatuses(projectID, orderedIDs); err != nil {
		return fmt.Errorf("%w: %s", ErrValidation, err.Error())
	}
	live.AfterProjectChange(userID, projectID, live.TypeProjectUpdated)
	return nil
}

func requireKanbanOwner(projectID, userID int) (*storage.ProjectWithAccess, error) {
	proj, err := storage.GetAccessibleProjectByID(projectID, userID)
	if err != nil {
		return nil, ErrNotFound
	}
	if !storage.RoleCanManage(proj.Role) {
		return nil, ErrForbidden
	}
	if proj.WorkflowMode != storage.WorkflowKanban {
		return nil, fmt.Errorf("%w: project is not in kanban mode", ErrValidation)
	}
	return proj, nil
}

// ApplyCompletedStatusSync moves a kanban task to done/default when completed flips.
func ApplyCompletedStatusSync(taskID, projectID int, completed bool) error {
	mode, err := storage.GetProjectWorkflowMode(projectID)
	if err != nil || mode != storage.WorkflowKanban {
		return err
	}
	if completed {
		done, err := storage.GetDoneProjectStatus(projectID)
		if err != nil {
			return err
		}
		return storage.AssignTaskStatusAndCompleted(taskID, done.ID, true)
	}
	def, err := storage.GetDefaultProjectStatus(projectID)
	if err != nil {
		return err
	}
	return storage.AssignTaskStatusAndCompleted(taskID, def.ID, false)
}

// ApplyStatusChange sets status and syncs completed for a kanban task.
func ApplyStatusChange(taskID, projectID, statusID int) error {
	_, err := applyStatusChange(taskID, projectID, statusID)
	return err
}

func applyStatusChange(taskID, projectID, statusID int) (*storage.ProjectStatus, error) {
	st, err := storage.GetProjectStatus(projectID, statusID)
	if err != nil {
		return nil, fmt.Errorf("%w: invalid status_id", ErrValidation)
	}
	if err := storage.AssignTaskStatusAndCompleted(taskID, st.ID, st.IsDone); err != nil {
		return nil, err
	}
	return st, nil
}

// applyKanbanColumnMove updates status (and completed) when a card is dropped
// into a different column. No-op when the task is already in that status.
func applyKanbanColumnMove(userID, taskID, projectID, newStatusID, oldStatusID int, oldCompleted bool) error {
	if newStatusID <= 0 || oldStatusID == newStatusID {
		return nil
	}
	st, err := applyStatusChange(taskID, projectID, newStatusID)
	if err != nil {
		return err
	}
	_ = storage.LogTaskEvent(taskID, userID, "status_changed", statusChangeMetadata(projectID, oldStatusID, newStatusID))
	if st.IsDone != oldCompleted {
		if st.IsDone {
			_ = storage.LogTaskEvent(taskID, userID, "completed", nil)
		} else {
			_ = storage.LogTaskEvent(taskID, userID, "reopened", nil)
		}
		dispatchCompletedHook(userID, taskID, st.IsDone)
	}
	go SyncGitHubIssueFromOrdrynState(context.Background(), userID, taskID, st.IsDone)
	from, to := statusHookNames(projectID, oldStatusID, newStatusID)
	live.AfterTaskChangeLive(userID, taskID, live.TypeTaskUpdated)
	meta := &live.TaskHookMeta{
		StatusChanged: true,
		OldStatus:     from,
		NewStatus:     to,
		Changed:       []string{"status"},
	}
	live.DispatchHook(userID, taskID, live.TypeTaskStatusChanged, meta)
	return nil
}

// AssignWorkflowOnProjectEnter sets default/done status when a task enters a kanban project.
func AssignWorkflowOnProjectEnter(taskID, projectID int, completed bool) error {
	mode, err := storage.GetProjectWorkflowMode(projectID)
	if err != nil {
		return err
	}
	if mode != storage.WorkflowKanban {
		return storage.ClearTaskWorkflowFields(taskID)
	}
	if completed {
		done, err := storage.GetDoneProjectStatus(projectID)
		if err != nil {
			return err
		}
		return storage.AssignTaskStatusAndCompleted(taskID, done.ID, true)
	}
	def, err := storage.GetDefaultProjectStatus(projectID)
	if err != nil {
		return err
	}
	return storage.AssignTaskStatusAndCompleted(taskID, def.ID, false)
}

// ListTimeEntriesForUser lists time entries if the user can read the task.
func ListTimeEntriesForUser(ctx context.Context, userID, taskID int) ([]storage.TaskTimeEntry, error) {
	_ = ctx
	canRead, _, projectID, err := storage.CanUserAccessTask(taskID, userID)
	if err != nil {
		return nil, err
	}
	if !canRead {
		return nil, ErrNotFound
	}
	if projectID == 0 {
		return nil, fmt.Errorf("%w: time tracking is only available on kanban project tasks", ErrValidation)
	}
	mode, err := storage.GetProjectWorkflowMode(projectID)
	if err != nil {
		return nil, err
	}
	if mode != storage.WorkflowKanban {
		return nil, fmt.Errorf("%w: time tracking is only available on kanban project tasks", ErrValidation)
	}
	return storage.ListTaskTimeEntries(taskID)
}

// AddTimeEntryForUser adds minutes to a writable kanban task.
func AddTimeEntryForUser(ctx context.Context, userID, taskID, minutes int, note string) (*storage.TaskTimeEntry, error) {
	_ = ctx
	canRead, writeRole, projectID, err := storage.CanUserAccessTask(taskID, userID)
	if err != nil {
		return nil, err
	}
	if !canRead || !storage.RoleCanWrite(writeRole) {
		return nil, ErrNotFound
	}
	if projectID == 0 {
		return nil, fmt.Errorf("%w: time tracking is only available on kanban project tasks", ErrValidation)
	}
	mode, err := storage.GetProjectWorkflowMode(projectID)
	if err != nil {
		return nil, err
	}
	if mode != storage.WorkflowKanban {
		return nil, fmt.Errorf("%w: time tracking is only available on kanban project tasks", ErrValidation)
	}
	if minutes <= 0 || minutes > storage.MaxTimeEntryMinutes {
		return nil, fmt.Errorf("%w: minutes must be between 1 and %d", ErrValidation, storage.MaxTimeEntryMinutes)
	}
	note = strings.TrimSpace(note)
	if len(note) > storage.MaxTimeEntryNote {
		return nil, fmt.Errorf("%w: note must be %d characters or less", ErrValidation, storage.MaxTimeEntryNote)
	}
	entry, err := storage.CreateTaskTimeEntry(taskID, userID, minutes, note)
	if err != nil {
		return nil, err
	}
	live.AfterTaskChange(userID, taskID, live.TypeTaskUpdated)
	return entry, nil
}

// DeleteTimeEntryForUser deletes a time entry (author or project owner).
func DeleteTimeEntryForUser(ctx context.Context, userID, taskID, entryID int) error {
	_ = ctx
	canRead, writeRole, projectID, err := storage.CanUserAccessTask(taskID, userID)
	if err != nil {
		return err
	}
	if !canRead {
		return ErrNotFound
	}
	entry, err := storage.GetTaskTimeEntry(entryID)
	if err != nil || entry.TaskID != taskID {
		return ErrNotFound
	}
	isOwner := false
	if projectID > 0 {
		role, _ := storage.GetProjectRole(projectID, userID)
		isOwner = storage.RoleCanManage(role)
	}
	if entry.UserID != userID && !isOwner {
		return ErrForbidden
	}
	if !storage.RoleCanWrite(writeRole) && !isOwner {
		return ErrForbidden
	}
	if err := storage.DeleteTaskTimeEntry(entryID); err != nil {
		return err
	}
	live.AfterTaskChange(userID, taskID, live.TypeTaskUpdated)
	return nil
}
