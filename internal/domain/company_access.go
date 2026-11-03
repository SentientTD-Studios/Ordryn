package domain

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"GoTodo/internal/live"
	"GoTodo/internal/storage"

	"github.com/jackc/pgx/v5"
)

// ProjectShareInput shares a company project with one team or one person.
type ProjectShareInput struct {
	TeamID   int
	Username string
	// Level is storage.AccessFull or storage.AccessView.
	Level string
	// RoleID is the site role a full share gives on this project; 0 means each
	// person's own site role.
	RoleID int
}

// requireCompanyProject loads a project the actor can open, in company mode only.
func requireCompanyProject(projectID, actorUserID int) (*storage.ProjectWithAccess, error) {
	if !storage.IsCompanyMode() {
		return nil, fmt.Errorf("%w: sharing with teams is only used in company mode", ErrValidation)
	}
	proj, err := storage.GetAccessibleProjectByID(projectID, actorUserID)
	if err != nil {
		return nil, ErrNotFound
	}
	return proj, nil
}

// ListProjectSharesForUser returns who a company project is shared with. Anyone who
// can open the project can see this.
func ListProjectSharesForUser(ctx context.Context, actorUserID, projectID int) ([]storage.ProjectAccessGrant, bool, error) {
	_ = ctx
	proj, err := requireCompanyProject(projectID, actorUserID)
	if err != nil {
		return nil, false, err
	}
	grants, err := storage.ListProjectAccessGrants(projectID)
	if err != nil {
		return nil, false, err
	}
	return grants, storage.RoleCanManageProject(proj.ID, proj.Role), nil
}

// ShareProjectForUser shares a company project with a team or a person, or changes
// the level of an existing share. It needs Manage project on that project; site
// admins have it on every project.
func ShareProjectForUser(ctx context.Context, actorUserID, projectID int, in ProjectShareInput) error {
	_ = ctx
	proj, err := requireCompanyProject(projectID, actorUserID)
	if err != nil {
		return err
	}
	if !storage.RoleCanManageProject(proj.ID, proj.Role) {
		return fmt.Errorf("%w: you need Manage project to share this project", ErrForbidden)
	}
	level := strings.TrimSpace(strings.ToLower(in.Level))
	if level == "" {
		level = storage.AccessFull
	}
	if level != storage.AccessFull && level != storage.AccessView {
		return fmt.Errorf("%w: access must be full or view", ErrValidation)
	}
	roleID := 0
	if level == storage.AccessFull && in.RoleID > 0 {
		role, err := storage.GetSiteRole(in.RoleID)
		if err != nil {
			return fmt.Errorf("%w: role not found", ErrValidation)
		}
		if storage.RoleGrantsAdmin(role.Permissions) {
			return fmt.Errorf("%w: %s is a site-wide role; choose a project role such as Project Administrator", ErrValidation, role.Label())
		}
		roleID = role.ID
	}
	username := strings.TrimSpace(in.Username)
	if (in.TeamID > 0) == (username != "") {
		return fmt.Errorf("%w: choose a team or a person", ErrValidation)
	}
	userID := 0
	if in.TeamID > 0 {
		if org, err := storage.GetOrganizationByID(in.TeamID); err != nil || org == nil {
			return fmt.Errorf("%w: team not found", ErrNotFound)
		}
	} else {
		name, err := PrepareUsername(username)
		if err != nil {
			return err
		}
		user, err := storage.GetUserByUsername(name)
		if err != nil || user == nil {
			return fmt.Errorf("%w: username not found", ErrNotFound)
		}
		if storage.IsAgentUser(user.ID) {
			return fmt.Errorf("%w: add AI agents on the AI agents tab", ErrValidation)
		}
		userID = user.ID
	}
	if err := storage.UpsertProjectAccessGrant(projectID, in.TeamID, userID, level, roleID, actorUserID); err != nil {
		return err
	}
	_ = storage.LogProjectEvent(projectID, actorUserID, "shared", map[string]interface{}{
		"team_id": in.TeamID, "user_id": userID, "level": level, "role_id": roleID,
	})
	live.AfterProjectChangeLive(actorUserID, projectID, live.TypeProjectUpdated)
	return nil
}

// UnshareProjectForUser removes one share. People reached only through it lose access.
func UnshareProjectForUser(ctx context.Context, actorUserID, projectID, grantID int) error {
	_ = ctx
	proj, err := requireCompanyProject(projectID, actorUserID)
	if err != nil {
		return err
	}
	if !storage.RoleCanManageProject(proj.ID, proj.Role) {
		return fmt.Errorf("%w: you need Manage project to change who this project is shared with", ErrForbidden)
	}
	before, err := storage.ListProjectMembers(projectID)
	if err != nil {
		return err
	}
	if err := storage.DeleteProjectAccessGrant(projectID, grantID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		return err
	}
	_ = storage.LogProjectEvent(projectID, actorUserID, "unshared", map[string]interface{}{"grant_id": grantID})
	// Tell anyone who just lost access so their open views drop the project.
	var removed []int
	for _, m := range before {
		if role, _ := storage.GetProjectRole(projectID, m.UserID); role == "" {
			removed = append(removed, m.UserID)
		}
	}
	live.AfterProjectChangeLive(actorUserID, projectID, live.TypeProjectUpdated, removed...)
	return nil
}
