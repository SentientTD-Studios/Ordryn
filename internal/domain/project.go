package domain

import (
	"context"
	"fmt"
	"strings"

	"GoTodo/internal/live"
	"GoTodo/internal/storage"
)

// MaxProjectNameLength is the maximum length of a project name.
const MaxProjectNameLength = 50

// MaxProjectDescriptionLength is the maximum length of a project description.
const MaxProjectDescriptionLength = 1000

// MaxBacklogNameLength is the maximum length of a project's backlog sprint name.
const MaxBacklogNameLength = storage.MaxSprintNameLen

// MaxBacklogDescriptionLength is the maximum length of a project's backlog sprint description.
const MaxBacklogDescriptionLength = storage.MaxSprintDescriptionLen

// CreateProject validates and creates a project for the user.
func CreateProject(ctx context.Context, userID int, name, description string) (*storage.Project, error) {
	_ = ctx
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, fmt.Errorf("%w: project name is required", ErrValidation)
	}
	if len(name) > MaxProjectNameLength {
		return nil, fmt.Errorf("%w: project name must be %d characters or less", ErrValidation, MaxProjectNameLength)
	}
	description = strings.TrimSpace(description)
	if len(description) > MaxProjectDescriptionLength {
		return nil, fmt.Errorf("%w: project description must be %d characters or less", ErrValidation, MaxProjectDescriptionLength)
	}
	return storage.CreateProject(userID, name, description)
}

// RenameProject updates a project name (owner only) and returns the updated project.
func RenameProject(ctx context.Context, userID, projectID int, name string) (*storage.Project, error) {
	return UpdateProject(ctx, userID, projectID, &name, nil, nil, nil, nil)
}

// RenameProjectBacklog updates a project's backlog sprint name (owner only).
func RenameProjectBacklog(ctx context.Context, userID, projectID int, name string) (*storage.Project, error) {
	return UpdateProject(ctx, userID, projectID, nil, nil, nil, &name, nil)
}

// UpdateProject patches name, description, workflow_mode, backlog_name, and/or backlog_description (owner only).
func UpdateProject(ctx context.Context, userID, projectID int, name, description, workflowMode, backlogName, backlogDescription *string) (*storage.Project, error) {
	_ = ctx
	var trimmedName string
	if name != nil {
		trimmedName = strings.TrimSpace(*name)
		if trimmedName == "" {
			return nil, fmt.Errorf("%w: project name is required", ErrValidation)
		}
		if len(trimmedName) > MaxProjectNameLength {
			return nil, fmt.Errorf("%w: project name must be %d characters or less", ErrValidation, MaxProjectNameLength)
		}
	}

	var trimmedDescription *string
	if description != nil {
		d := strings.TrimSpace(*description)
		if len(d) > MaxProjectDescriptionLength {
			return nil, fmt.Errorf("%w: project description must be %d characters or less", ErrValidation, MaxProjectDescriptionLength)
		}
		trimmedDescription = &d
	}

	var trimmedBacklogName *string
	if backlogName != nil {
		b := strings.TrimSpace(*backlogName)
		if b == "" {
			return nil, fmt.Errorf("%w: backlog name is required", ErrValidation)
		}
		if len(b) > MaxBacklogNameLength {
			return nil, fmt.Errorf("%w: backlog name must be %d characters or less", ErrValidation, MaxBacklogNameLength)
		}
		trimmedBacklogName = &b
	}

	var trimmedBacklogDesc *string
	if backlogDescription != nil {
		bd := strings.TrimSpace(*backlogDescription)
		if len(bd) > MaxBacklogDescriptionLength {
			return nil, fmt.Errorf("%w: backlog description must be %d characters or less", ErrValidation, MaxBacklogDescriptionLength)
		}
		trimmedBacklogDesc = &bd
	}

	proj, err := storage.GetAccessibleProjectByID(projectID, userID)
	if err != nil {
		return nil, ErrNotFound
	}
	if !storage.RoleCanManage(proj.Role) {
		return nil, ErrForbidden
	}

	var namePtr *string
	if name != nil {
		namePtr = &trimmedName
	}
	if namePtr != nil || trimmedDescription != nil || trimmedBacklogName != nil || trimmedBacklogDesc != nil {
		if err := storage.UpdateProject(projectID, proj.OwnerUserID, namePtr, trimmedDescription, trimmedBacklogName, trimmedBacklogDesc); err != nil {
			return nil, err
		}
		if namePtr != nil && *namePtr != proj.Name {
			_ = storage.LogProjectEvent(projectID, userID, "renamed", map[string]interface{}{
				"name": *namePtr,
			})
		}
		if trimmedDescription != nil && *trimmedDescription != proj.Description {
			_ = storage.LogProjectEvent(projectID, userID, "description_updated", nil)
		}
		if trimmedBacklogName != nil && *trimmedBacklogName != proj.BacklogName {
			_ = storage.LogProjectEvent(projectID, userID, "backlog_renamed", map[string]interface{}{
				"name": *trimmedBacklogName,
			})
		}
		if trimmedBacklogDesc != nil && *trimmedBacklogDesc != proj.BacklogDescription {
			_ = storage.LogProjectEvent(projectID, userID, "backlog_description_updated", nil)
		}
	}

	if workflowMode != nil {
		if _, err := SetProjectWorkflowMode(ctx, userID, projectID, *workflowMode); err != nil {
			return nil, err
		}
	} else if namePtr != nil || trimmedDescription != nil || trimmedBacklogName != nil || trimmedBacklogDesc != nil {
		live.AfterProjectChange(userID, projectID, live.TypeProjectUpdated)
	}

	return storage.GetProjectByID(projectID, proj.OwnerUserID)
}

// ReorderProjectsForUser sets the display order of active (non-archived) owned projects.
func ReorderProjectsForUser(ctx context.Context, userID int, orderedIDs []int) error {
	_ = ctx
	if len(orderedIDs) == 0 {
		return fmt.Errorf("%w: project_ids is required", ErrValidation)
	}
	if err := storage.ReorderProjects(userID, orderedIDs); err != nil {
		return fmt.Errorf("%w: %s", ErrValidation, err.Error())
	}
	return nil
}

// DeleteProject removes a project (owner only).
func DeleteProject(ctx context.Context, userID, projectID int) error {
	_ = ctx
	proj, err := storage.GetAccessibleProjectByID(projectID, userID)
	if err != nil {
		return ErrNotFound
	}
	if !storage.RoleCanManage(proj.Role) {
		return ErrForbidden
	}
	live.AfterProjectChange(userID, projectID, live.TypeProjectUpdated)
	return storage.DeleteProject(projectID, proj.OwnerUserID)
}

// ArchiveProject marks a project archived (owner only), tags its tasks, and blocks new work.
func ArchiveProject(ctx context.Context, userID, projectID int) (*storage.Project, error) {
	_ = ctx
	proj, err := storage.GetAccessibleProjectByID(projectID, userID)
	if err != nil {
		return nil, ErrNotFound
	}
	if !storage.RoleCanManage(proj.Role) {
		return nil, ErrForbidden
	}
	if proj.Archived {
		return storage.GetProjectByID(projectID, proj.OwnerUserID)
	}
	if err := storage.SetProjectArchived(projectID, proj.OwnerUserID, true); err != nil {
		return nil, err
	}
	if err := storage.ApplyArchivedTagToProjectTasks(projectID, proj.OwnerUserID); err != nil {
		return nil, err
	}
	_ = storage.LogProjectEvent(projectID, userID, "archived", nil)
	live.AfterProjectChange(userID, projectID, live.TypeProjectUpdated)
	return storage.GetProjectByID(projectID, proj.OwnerUserID)
}

// RestoreProject unarchives a project (owner only) and clears the archived tag from its tasks.
func RestoreProject(ctx context.Context, userID, projectID int) (*storage.Project, error) {
	_ = ctx
	proj, err := storage.GetAccessibleProjectByID(projectID, userID)
	if err != nil {
		return nil, ErrNotFound
	}
	if !storage.RoleCanManage(proj.Role) {
		return nil, ErrForbidden
	}
	if !proj.Archived {
		return storage.GetProjectByID(projectID, proj.OwnerUserID)
	}
	if err := storage.SetProjectArchived(projectID, proj.OwnerUserID, false); err != nil {
		return nil, err
	}
	if err := storage.ClearArchivedTagFromProjectTasks(projectID, proj.OwnerUserID); err != nil {
		return nil, err
	}
	_ = storage.LogProjectEvent(projectID, userID, "restored", nil)
	live.AfterProjectChange(userID, projectID, live.TypeProjectUpdated)
	return storage.GetProjectByID(projectID, proj.OwnerUserID)
}

// RequireProjectWriteAccess ensures the user can create/edit tasks in the project.
func RequireProjectWriteAccess(projectID, userID int) error {
	if projectID <= 0 {
		return fmt.Errorf("%w: invalid project_id", ErrValidation)
	}
	proj, err := storage.GetAccessibleProjectByID(projectID, userID)
	if err != nil {
		return fmt.Errorf("%w: invalid project_id", ErrValidation)
	}
	if !storage.RoleCanWrite(proj.Role) {
		return ErrForbidden
	}
	return nil
}

// RequireProjectAcceptsNewTasks ensures the user can add a task to the project.
func RequireProjectAcceptsNewTasks(projectID, userID int) error {
	if err := RequireProjectWriteAccess(projectID, userID); err != nil {
		return err
	}
	proj, err := storage.GetAccessibleProjectByID(projectID, userID)
	if err != nil {
		return fmt.Errorf("%w: invalid project_id", ErrValidation)
	}
	if proj.Archived {
		return fmt.Errorf("%w: cannot add tasks to an archived project", ErrConflict)
	}
	return nil
}
