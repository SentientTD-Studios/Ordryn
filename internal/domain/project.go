package domain

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

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
	return CreateProjectForUser(ctx, userID, CreateProjectInput{Name: name, Description: description})
}

// CreateProjectInput is the create payload, including optional organization import.
type CreateProjectInput struct {
	Name           string
	Description    string
	OrganizationID *int
	ImportMode     string
	Members        []storage.OrgImportMember
}

func parseOrgImportSpec(orgID int, mode string, members []storage.OrgImportMember) (*storage.OrgImportSpec, error) {
	if orgID <= 0 {
		return nil, nil
	}
	mode = strings.TrimSpace(strings.ToLower(mode))
	if mode == "" {
		mode = storage.OrgImportCopy
	}
	spec := &storage.OrgImportSpec{OrganizationID: orgID}
	switch mode {
	case storage.OrgImportCopy:
		spec.AllMembers = true
		spec.Mode = storage.OrgImportCopy
	case storage.OrgImportLock:
		spec.AllMembers = true
		spec.Lock = true
		spec.Mode = storage.OrgImportLock
	case storage.OrgImportSelect:
		spec.Mode = storage.OrgImportSelect
		seen := map[int]bool{}
		var picked []storage.OrgImportMember
		for _, m := range members {
			if m.UserID <= 0 {
				continue
			}
			role := strings.TrimSpace(strings.ToLower(m.Role))
			if role == "" {
				return nil, fmt.Errorf("%w: each selected member needs a role", ErrValidation)
			}
			if !storage.ValidInviteRoleForOrg(orgID, role) {
				return nil, fmt.Errorf("%w: role is not assignable on this organization", ErrValidation)
			}
			if seen[m.UserID] {
				continue
			}
			seen[m.UserID] = true
			picked = append(picked, storage.OrgImportMember{UserID: m.UserID, Role: role})
		}
		if len(picked) == 0 {
			return nil, fmt.Errorf("%w: select at least one organization member and a role", ErrValidation)
		}
		spec.Members = picked
	default:
		return nil, fmt.Errorf("%w: org_import must be copy, lock, or select", ErrValidation)
	}
	return spec, nil
}

// CreateProjectForUser validates and creates a project, optionally importing an organization roster.
func CreateProjectForUser(ctx context.Context, userID int, in CreateProjectInput) (*storage.Project, error) {
	_ = ctx
	name := strings.TrimSpace(in.Name)
	if name == "" {
		return nil, fmt.Errorf("%w: project name is required", ErrValidation)
	}
	if len(name) > MaxProjectNameLength {
		return nil, fmt.Errorf("%w: project name must be %d characters or less", ErrValidation, MaxProjectNameLength)
	}
	description := strings.TrimSpace(in.Description)
	if len(description) > MaxProjectDescriptionLength {
		return nil, fmt.Errorf("%w: project description must be %d characters or less", ErrValidation, MaxProjectDescriptionLength)
	}
	var spec *storage.OrgImportSpec
	if in.OrganizationID != nil && *in.OrganizationID > 0 {
		org, err := requireOrgManage(*in.OrganizationID, userID)
		if err != nil {
			return nil, err
		}
		parsed, err := parseOrgImportSpec(org.ID, in.ImportMode, in.Members)
		if err != nil {
			return nil, err
		}
		spec = parsed
	}
	proj, err := storage.CreateProjectWithOrg(userID, name, description, spec)
	if err != nil {
		return nil, err
	}
	live.AfterProjectChange(userID, proj.ID, live.TypeProjectCreated)
	return proj, nil
}

// AttachOrganizationToProject binds an existing project to an organization.
// Non-owner members who are not imported lose access. Pending invites are cancelled.
func AttachOrganizationToProject(ctx context.Context, userID, projectID int, in CreateProjectInput) (*storage.Project, error) {
	_ = ctx
	if in.OrganizationID == nil || *in.OrganizationID <= 0 {
		return nil, fmt.Errorf("%w: organization_id is required", ErrValidation)
	}
	proj, err := storage.GetAccessibleProjectByID(projectID, userID)
	if err != nil {
		return nil, ErrNotFound
	}
	if !storage.RoleCanManageProject(proj.ID, proj.Role) {
		return nil, ErrForbidden
	}
	org, err := requireOrgManage(*in.OrganizationID, userID)
	if err != nil {
		return nil, err
	}
	spec, err := parseOrgImportSpec(org.ID, in.ImportMode, in.Members)
	if err != nil {
		return nil, err
	}
	if spec == nil {
		return nil, fmt.Errorf("%w: organization_id is required", ErrValidation)
	}
	if proj.OrganizationID != nil && *proj.OrganizationID == spec.OrganizationID {
		return storage.GetProjectByID(projectID, proj.OwnerUserID)
	}

	removed, err := storage.AttachProjectOrganization(projectID, proj.OwnerUserID, *spec)
	if err != nil {
		return nil, err
	}
	_ = storage.LogProjectEvent(projectID, userID, "organization_attached", map[string]interface{}{
		"organization_id": spec.OrganizationID,
		"org_import":      in.ImportMode,
		"lock":            spec.Lock,
	})
	live.AfterProjectChangeLive(userID, projectID, live.TypeProjectUpdated, removed...)
	return storage.GetProjectByID(projectID, proj.OwnerUserID)
}

// RenameProject updates a project name (owner only) and returns the updated project.
func RenameProject(ctx context.Context, userID, projectID int, name string) (*storage.Project, error) {
	return UpdateProject(ctx, userID, projectID, &name, nil, nil, nil, nil, nil)
}

// RenameProjectBacklog updates a project's backlog sprint name (owner only).
func RenameProjectBacklog(ctx context.Context, userID, projectID int, name string) (*storage.Project, error) {
	return UpdateProject(ctx, userID, projectID, nil, nil, nil, &name, nil, nil)
}

// AutoSprintPatch is a partial update for automatic next-sprint settings.
type AutoSprintPatch struct {
	Enabled        *bool
	LengthDays     **int
	LockDaysBefore **int
}

func (p *AutoSprintPatch) empty() bool {
	return p == nil || (p.Enabled == nil && p.LengthDays == nil && p.LockDaysBefore == nil)
}

// UpdateProject patches name, description, workflow_mode, backlog_name, backlog_description,
// and optional auto-sprint settings (owner only).
func UpdateProject(ctx context.Context, userID, projectID int, name, description, workflowMode, backlogName, backlogDescription *string, auto *AutoSprintPatch) (*storage.Project, error) {
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
	if !storage.RoleCanManageProject(proj.ID, proj.Role) {
		return nil, ErrForbidden
	}

	var namePtr *string
	if name != nil {
		namePtr = &trimmedName
	}

	autoPatch, err := normalizeAutoSprintPatch(proj, auto, workflowMode)
	if err != nil {
		return nil, err
	}

	storagePatch := storage.ProjectPatch{
		Name:                     namePtr,
		Description:              trimmedDescription,
		BacklogName:              trimmedBacklogName,
		BacklogDescription:       trimmedBacklogDesc,
		AutoCreateNextSprint:     autoPatch.Enabled,
		AutoSprintLengthDays:     autoPatch.LengthDays,
		AutoSprintLockDaysBefore: autoPatch.LockDaysBefore,
	}
	if !storagePatch.Empty() {
		if err := storage.UpdateProject(projectID, proj.OwnerUserID, storagePatch); err != nil {
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
		if autoSprintSettingsChanged(proj, autoPatch) {
			_ = storage.LogProjectEvent(projectID, userID, "auto_sprint_settings_updated", nil)
		}
	}

	if workflowMode != nil {
		if _, err := SetProjectWorkflowMode(ctx, userID, projectID, *workflowMode); err != nil {
			return nil, err
		}
	} else if !storagePatch.Empty() {
		live.AfterProjectChange(userID, projectID, live.TypeProjectUpdated)
	}

	updated, err := storage.GetProjectByID(projectID, proj.OwnerUserID)
	if err != nil {
		return nil, err
	}
	if updated.AutoCreateNextSprint && autoSprintSettingsChanged(proj, autoPatch) {
		if _, err := AutoCreateDueSprintsForProject(*updated, time.Now().UTC()); err != nil {
			log.Printf("auto-sprint after settings save: project %d: %v", projectID, err)
		} else {
			return storage.GetProjectByID(projectID, proj.OwnerUserID)
		}
	}
	return updated, nil
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
	if !storage.RoleCanManageProject(proj.ID, proj.Role) {
		return ErrForbidden
	}
	// Membership cascades with the project, so capture the live audience first.
	members, _ := storage.ProjectMemberUserIDs(projectID)
	// Destination settings and secrets are deleted with the project, so resolve
	// the outbound hook first and fire it only once the delete has committed.
	fireDeleted := live.PrepareProjectHook(userID, projectID, live.TypeProjectDeleted, nil)
	if err := storage.DeleteProject(projectID, proj.OwnerUserID); err != nil {
		return err
	}
	fireDeleted()
	live.AfterProjectChangeLive(userID, projectID, live.TypeProjectDeleted, members...)
	return nil
}

// ArchiveProject marks a project archived (owner only), tags its tasks, and blocks new work.
func ArchiveProject(ctx context.Context, userID, projectID int) (*storage.Project, error) {
	_ = ctx
	proj, err := storage.GetAccessibleProjectByID(projectID, userID)
	if err != nil {
		return nil, ErrNotFound
	}
	if !storage.RoleCanManageProject(proj.ID, proj.Role) {
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
	live.AfterProjectChangeLive(userID, projectID, live.TypeProjectUpdated)
	live.DispatchProjectHook(userID, projectID, live.TypeProjectArchived, nil)
	return storage.GetProjectByID(projectID, proj.OwnerUserID)
}

// RestoreProject unarchives a project (owner only) and clears the archived tag from its tasks.
func RestoreProject(ctx context.Context, userID, projectID int) (*storage.Project, error) {
	_ = ctx
	proj, err := storage.GetAccessibleProjectByID(projectID, userID)
	if err != nil {
		return nil, ErrNotFound
	}
	if !storage.RoleCanManageProject(proj.ID, proj.Role) {
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
	live.AfterProjectChangeLive(userID, projectID, live.TypeProjectUpdated)
	live.DispatchProjectHook(userID, projectID, live.TypeProjectRestored, nil)
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
	if !storage.HasProjectPerm(proj.ID, proj.Role, storage.PermTasksCreate) {
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
