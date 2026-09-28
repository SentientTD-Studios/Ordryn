package domain

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"GoTodo/internal/storage"
)

var projectRoleSlugPattern = regexp.MustCompile(`^[a-z][a-z0-9_-]{1,39}$`)

func normalizeRoleSlug(raw string) (string, error) {
	slug := strings.TrimSpace(strings.ToLower(raw))
	if !projectRoleSlugPattern.MatchString(slug) {
		return "", fmt.Errorf("%w: role slug must be 2-40 characters, start with a letter, and use lowercase letters, digits, _ or -", ErrValidation)
	}
	return slug, nil
}

func normalizeRoleName(raw string) (string, error) {
	name := strings.TrimSpace(raw)
	if name == "" {
		return "", fmt.Errorf("%w: role name is required", ErrValidation)
	}
	if len(name) > storage.MaxProjectRoleNameLen {
		return "", fmt.Errorf("%w: role name must be %d characters or less", ErrValidation, storage.MaxProjectRoleNameLen)
	}
	return name, nil
}

func normalizeRoleDescription(raw string) (string, error) {
	desc := strings.TrimSpace(raw)
	if len(desc) > storage.MaxProjectRoleDescLen {
		return "", fmt.Errorf("%w: role description must be %d characters or less", ErrValidation, storage.MaxProjectRoleDescLen)
	}
	return desc, nil
}

func requireProjectManage(projectID, userID int) (*storage.ProjectWithAccess, error) {
	proj, err := storage.GetAccessibleProjectByID(projectID, userID)
	if err != nil {
		return nil, ErrNotFound
	}
	if storage.HasProjectPerm(projectID, proj.Role, storage.PermProjectManage) {
		return proj, nil
	}
	return nil, ErrForbidden
}

func denyMissingTaskPerm(projectID int, role, perm string) error {
	if projectID <= 0 {
		return nil
	}
	if storage.HasProjectPerm(projectID, role, perm) {
		return nil
	}
	return fmt.Errorf("%w: your role cannot %s", ErrForbidden, permActionLabel(perm))
}

func permActionLabel(perm string) string {
	switch perm {
	case storage.PermTasksCreate:
		return "create tasks"
	case storage.PermTasksEdit:
		return "edit task details"
	case storage.PermTasksDelete:
		return "delete tasks"
	case storage.PermTasksArchive:
		return "archive tasks"
	case storage.PermTasksRestore:
		return "restore tasks"
	case storage.PermTasksComplete:
		return "complete tasks"
	case storage.PermTasksClaim:
		return "claim tasks"
	case storage.PermTasksReorder:
		return "reorder tasks"
	case storage.PermTasksStatus:
		return "change task status"
	case storage.PermTasksSprint:
		return "change sprints"
	case storage.PermProjectTags:
		return "manage tags"
	case storage.PermTimeWrite:
		return "log time"
	case storage.PermExtensionsWrite:
		return "configure extensions"
	case storage.PermCommentsModerate:
		return "moderate comments"
	case storage.PermProjectManage:
		return "manage this project"
	default:
		return perm
	}
}

func statusMoveBypassesGates(projectID, userID int, role string) bool {
	if storage.HasProjectPerm(projectID, role, storage.PermProjectManage) {
		return true
	}
	return storage.UserHasPermission(userID, "admin")
}

// CanMoveTaskStatus checks tasks:status plus optional enter/leave gates.
func CanMoveTaskStatus(projectID, userID int, role string, fromStatusID, toStatusID int) error {
	if projectID <= 0 || fromStatusID == toStatusID {
		return nil
	}
	if err := denyMissingTaskPerm(projectID, role, storage.PermTasksStatus); err != nil {
		return err
	}
	if statusMoveBypassesGates(projectID, userID, role) {
		return nil
	}
	var fromGate, toGate *storage.ProjectStatusGate
	if fromStatusID > 0 {
		g, err := storage.GetStatusGate(fromStatusID)
		if err != nil {
			return err
		}
		fromGate = g
	}
	if toStatusID > 0 {
		g, err := storage.GetStatusGate(toStatusID)
		if err != nil {
			return err
		}
		toGate = g
	}
	leaveOK, enterOK := storage.StatusMoveAllowedByGate(fromGate, toGate, role)
	if !leaveOK {
		return fmt.Errorf("%w: your role cannot move tasks out of this status", ErrForbidden)
	}
	if !enterOK {
		return fmt.Errorf("%w: your role cannot move tasks into this status", ErrForbidden)
	}
	return nil
}

func attachStatusGates(projectID int, statuses []storage.ProjectStatus) error {
	gates, err := storage.ListStatusGatesForProject(projectID)
	if err != nil {
		return err
	}
	for i := range statuses {
		if g, ok := gates[statuses[i].ID]; ok {
			statuses[i].EnterRoleSlugs = g.EnterRoleSlugs
			statuses[i].LeaveRoleSlugs = g.LeaveRoleSlugs
		} else {
			statuses[i].EnterRoleSlugs = []string{}
			statuses[i].LeaveRoleSlugs = []string{}
		}
	}
	return nil
}

func attachCommentRoles(projectID int, comments []storage.TaskComment) {
	if projectID <= 0 || len(comments) == 0 {
		return
	}
	members, err := storage.ListProjectMembers(projectID)
	if err != nil {
		return
	}
	roleByUser := make(map[int]string, len(members))
	for _, m := range members {
		roleByUser[m.UserID] = m.Role
	}
	for i := range comments {
		slug := roleByUser[comments[i].UserID]
		comments[i].AuthorRole = slug
		comments[i].AuthorRoleName = storage.RoleDisplayName(projectID, slug)
	}
}

func attachCommentRole(projectID int, comment *storage.TaskComment) {
	if comment == nil || projectID <= 0 {
		return
	}
	role, _ := storage.GetProjectRole(projectID, comment.UserID)
	comment.AuthorRole = role
	comment.AuthorRoleName = storage.RoleDisplayName(projectID, role)
}

// ListSiteProjectRolesForUser returns the permission catalog and site roles.
func ListSiteProjectRolesForUser(ctx context.Context, userID int) ([]storage.ProjectPermInfo, []storage.ProjectRoleDef, error) {
	_ = ctx
	if userID <= 0 {
		return nil, nil, ErrForbidden
	}
	roles, err := storage.ListSiteProjectRoles()
	if err != nil {
		return nil, nil, err
	}
	if roles == nil {
		roles = []storage.ProjectRoleDef{}
	}
	return storage.ProjectPermissionCatalog(), roles, nil
}

// CreateSiteProjectRoleInput is the admin create payload.
type CreateSiteProjectRoleInput struct {
	Slug        string
	Name        string
	Description string
	Permissions []string
	SortOrder   int
}

// CreateSiteProjectRoleForAdmin adds a site-level assignable role.
func CreateSiteProjectRoleForAdmin(ctx context.Context, userID int, in CreateSiteProjectRoleInput) (*storage.ProjectRoleDef, error) {
	_ = ctx
	if !storage.UserHasPermission(userID, "admin") {
		return nil, ErrForbidden
	}
	slug, err := normalizeRoleSlug(in.Slug)
	if err != nil {
		return nil, err
	}
	name, err := normalizeRoleName(in.Name)
	if err != nil {
		return nil, err
	}
	desc, err := normalizeRoleDescription(in.Description)
	if err != nil {
		return nil, err
	}
	taken, err := storage.SiteRoleSlugTaken(slug, 0)
	if err != nil {
		return nil, err
	}
	if taken {
		return nil, fmt.Errorf("%w: role slug already exists", ErrConflict)
	}
	return storage.CreateProjectRoleDef(nil, slug, name, desc, in.Permissions, false, in.SortOrder)
}

// UpdateSiteProjectRoleInput is a partial admin patch.
type UpdateSiteProjectRoleInput struct {
	Name        *string
	Description *string
	Permissions *[]string
	SortOrder   *int
}

// UpdateSiteProjectRoleForAdmin updates a site-level role.
func UpdateSiteProjectRoleForAdmin(ctx context.Context, userID, roleID int, in UpdateSiteProjectRoleInput) (*storage.ProjectRoleDef, error) {
	_ = ctx
	if !storage.UserHasPermission(userID, "admin") {
		return nil, ErrForbidden
	}
	cur, err := storage.GetProjectRoleDef(roleID)
	if err != nil || cur == nil || cur.ProjectID != nil {
		return nil, ErrNotFound
	}
	if in.Name != nil {
		name, err := normalizeRoleName(*in.Name)
		if err != nil {
			return nil, err
		}
		in.Name = &name
	}
	if in.Description != nil {
		desc, err := normalizeRoleDescription(*in.Description)
		if err != nil {
			return nil, err
		}
		in.Description = &desc
	}
	if cur.Slug == storage.RoleOwner && in.Permissions != nil {
		all := storage.AllProjectPerms()
		in.Permissions = &all
	}
	updated, err := storage.UpdateProjectRoleDef(roleID, in.Name, in.Description, in.Permissions, in.SortOrder)
	if err != nil {
		if strings.Contains(err.Error(), "unknown permission") {
			return nil, fmt.Errorf("%w: %s", ErrValidation, err.Error())
		}
		return nil, err
	}
	return updated, nil
}

// DeleteSiteProjectRoleForAdmin removes a non-system site role that is unused.
func DeleteSiteProjectRoleForAdmin(ctx context.Context, userID, roleID int) error {
	_ = ctx
	if !storage.UserHasPermission(userID, "admin") {
		return ErrForbidden
	}
	cur, err := storage.GetProjectRoleDef(roleID)
	if err != nil || cur == nil || cur.ProjectID != nil {
		return ErrNotFound
	}
	if cur.IsSystem {
		return fmt.Errorf("%w: cannot delete a built-in role", ErrValidation)
	}
	members, err := storage.CountMembersWithRole(cur.Slug, 0)
	if err != nil {
		return err
	}
	invites, err := storage.CountInvitesWithRole(cur.Slug, 0)
	if err != nil {
		return err
	}
	if members > 0 || invites > 0 {
		return fmt.Errorf("%w: role is still assigned to members or pending invites", ErrConflict)
	}
	return storage.DeleteProjectRoleDef(roleID)
}

// ListProjectRolesForUser returns assignable roles for a project the user can access.
func ListProjectRolesForUser(ctx context.Context, userID, projectID int) ([]storage.ProjectRoleDef, []storage.ProjectPermInfo, error) {
	_ = ctx
	if _, err := storage.GetAccessibleProjectByID(projectID, userID); err != nil {
		return nil, nil, ErrNotFound
	}
	roles, err := storage.ListAssignableProjectRoles(projectID)
	if err != nil {
		return nil, nil, err
	}
	if roles == nil {
		roles = []storage.ProjectRoleDef{}
	}
	return roles, storage.ProjectPermissionCatalog(), nil
}

// CreateProjectCustomRoleForUser adds a project-only role built from the catalog.
func CreateProjectCustomRoleForUser(ctx context.Context, userID, projectID int, in CreateSiteProjectRoleInput) (*storage.ProjectRoleDef, error) {
	_ = ctx
	if _, err := requireProjectManage(projectID, userID); err != nil {
		return nil, err
	}
	slug, err := normalizeRoleSlug(in.Slug)
	if err != nil {
		return nil, err
	}
	name, err := normalizeRoleName(in.Name)
	if err != nil {
		return nil, err
	}
	desc, err := normalizeRoleDescription(in.Description)
	if err != nil {
		return nil, err
	}
	n, err := storage.CountProjectCustomRoles(projectID)
	if err != nil {
		return nil, err
	}
	if n >= storage.MaxProjectCustomRoles {
		return nil, fmt.Errorf("%w: at most %d custom roles per project", ErrValidation, storage.MaxProjectCustomRoles)
	}
	taken, err := storage.ProjectRoleSlugTaken(projectID, slug, 0)
	if err != nil {
		return nil, err
	}
	if taken {
		return nil, fmt.Errorf("%w: role slug already exists on this site or project", ErrConflict)
	}
	pid := projectID
	return storage.CreateProjectRoleDef(&pid, slug, name, desc, in.Permissions, false, in.SortOrder)
}

// UpdateProjectCustomRoleForUser patches a project-created role.
func UpdateProjectCustomRoleForUser(ctx context.Context, userID, projectID, roleID int, in UpdateSiteProjectRoleInput) (*storage.ProjectRoleDef, error) {
	_ = ctx
	if _, err := requireProjectManage(projectID, userID); err != nil {
		return nil, err
	}
	cur, err := storage.GetProjectRoleDef(roleID)
	if err != nil || cur == nil || cur.ProjectID == nil || *cur.ProjectID != projectID {
		return nil, ErrNotFound
	}
	if in.Name != nil {
		name, err := normalizeRoleName(*in.Name)
		if err != nil {
			return nil, err
		}
		in.Name = &name
	}
	if in.Description != nil {
		desc, err := normalizeRoleDescription(*in.Description)
		if err != nil {
			return nil, err
		}
		in.Description = &desc
	}
	updated, err := storage.UpdateProjectRoleDef(roleID, in.Name, in.Description, in.Permissions, in.SortOrder)
	if err != nil {
		if strings.Contains(err.Error(), "unknown permission") {
			return nil, fmt.Errorf("%w: %s", ErrValidation, err.Error())
		}
		return nil, err
	}
	return updated, nil
}

// DeleteProjectCustomRoleForUser removes an unused project-created role.
func DeleteProjectCustomRoleForUser(ctx context.Context, userID, projectID, roleID int) error {
	_ = ctx
	if _, err := requireProjectManage(projectID, userID); err != nil {
		return err
	}
	cur, err := storage.GetProjectRoleDef(roleID)
	if err != nil || cur == nil || cur.ProjectID == nil || *cur.ProjectID != projectID {
		return ErrNotFound
	}
	members, err := storage.CountMembersWithRole(cur.Slug, projectID)
	if err != nil {
		return err
	}
	invites, err := storage.CountInvitesWithRole(cur.Slug, projectID)
	if err != nil {
		return err
	}
	if members > 0 || invites > 0 {
		return fmt.Errorf("%w: role is still assigned to members or pending invites", ErrConflict)
	}
	return storage.DeleteProjectRoleDef(roleID)
}

func normalizeGateSlugs(projectID int, slugs []string) ([]string, error) {
	if slugs == nil {
		return []string{}, nil
	}
	seen := make(map[string]bool, len(slugs))
	out := make([]string, 0, len(slugs))
	for _, raw := range slugs {
		slug := strings.TrimSpace(strings.ToLower(raw))
		if slug == "" {
			continue
		}
		if slug == storage.RoleOwner {
			continue
		}
		if storage.ResolveRoleDef(projectID, slug) == nil {
			return nil, fmt.Errorf("%w: unknown role %q", ErrValidation, slug)
		}
		if seen[slug] {
			continue
		}
		seen[slug] = true
		out = append(out, slug)
	}
	return out, nil
}

// UpdateStatusGatesForUser sets which roles may enter and leave a status.
func UpdateStatusGatesForUser(ctx context.Context, userID, projectID, statusID int, enter, leave []string) (*storage.ProjectStatusGate, error) {
	_ = ctx
	if _, err := requireProjectManage(projectID, userID); err != nil {
		return nil, err
	}
	st, err := storage.GetProjectStatus(projectID, statusID)
	if err != nil || st == nil {
		return nil, ErrNotFound
	}
	enterSlugs, err := normalizeGateSlugs(projectID, enter)
	if err != nil {
		return nil, err
	}
	leaveSlugs, err := normalizeGateSlugs(projectID, leave)
	if err != nil {
		return nil, err
	}
	return storage.UpsertStatusGate(statusID, enterSlugs, leaveSlugs)
}
