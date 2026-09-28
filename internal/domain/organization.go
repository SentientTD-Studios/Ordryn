package domain

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"GoTodo/internal/live"
	"GoTodo/internal/storage"

	"github.com/jackc/pgx/v5"
)

func normalizeOrgName(raw string) (string, error) {
	name := strings.TrimSpace(raw)
	if name == "" {
		return "", fmt.Errorf("%w: organization name is required", ErrValidation)
	}
	if len(name) > storage.MaxOrganizationNameLen {
		return "", fmt.Errorf("%w: organization name must be %d characters or less", ErrValidation, storage.MaxOrganizationNameLen)
	}
	return name, nil
}

func normalizeOrgDescription(raw string) (string, error) {
	desc := strings.TrimSpace(raw)
	if len(desc) > storage.MaxOrganizationDescLen {
		return "", fmt.Errorf("%w: organization description must be %d characters or less", ErrValidation, storage.MaxOrganizationDescLen)
	}
	return desc, nil
}

func requireOrgAccess(orgID, userID int) (*storage.Organization, error) {
	org, err := storage.GetAccessibleOrganization(orgID, userID)
	if err != nil {
		return nil, ErrNotFound
	}
	return org, nil
}

func requireOrgManage(orgID, userID int) (*storage.Organization, error) {
	org, err := requireOrgAccess(orgID, userID)
	if err != nil {
		return nil, err
	}
	if !storage.RoleCanManageOrganization(orgID, org.Role) {
		return nil, ErrForbidden
	}
	return org, nil
}

func denyOrgManagedMembershipEdits(projectID int) error {
	if storage.ProjectIsOrgManaged(projectID) {
		return fmt.Errorf("%w: membership and roles for this project are locked to the organization", ErrForbidden)
	}
	return nil
}

// ListOrganizationsForUser returns organizations the user belongs to.
func ListOrganizationsForUser(ctx context.Context, userID int) ([]storage.Organization, error) {
	_ = ctx
	if userID <= 0 {
		return nil, ErrForbidden
	}
	orgs, err := storage.ListOrganizationsForUser(userID)
	if err != nil {
		return nil, err
	}
	if orgs == nil {
		orgs = []storage.Organization{}
	}
	return orgs, nil
}

// GetOrganizationForUser returns one org if the user is a member.
func GetOrganizationForUser(ctx context.Context, userID, orgID int) (*storage.Organization, error) {
	_ = ctx
	return requireOrgAccess(orgID, userID)
}

// CreateOrganizationForUser creates an organization owned by the user.
func CreateOrganizationForUser(ctx context.Context, userID int, name, description string) (*storage.Organization, error) {
	_ = ctx
	if userID <= 0 {
		return nil, ErrForbidden
	}
	name, err := normalizeOrgName(name)
	if err != nil {
		return nil, err
	}
	desc, err := normalizeOrgDescription(description)
	if err != nil {
		return nil, err
	}
	n, err := storage.CountOrganizationsCreatedBy(userID)
	if err != nil {
		return nil, err
	}
	if n >= storage.MaxOrganizationsPerUser {
		return nil, fmt.Errorf("%w: at most %d organizations per user", ErrValidation, storage.MaxOrganizationsPerUser)
	}
	return storage.CreateOrganization(userID, name, desc)
}

// UpdateOrganizationForUser patches an organization the user can manage.
func UpdateOrganizationForUser(ctx context.Context, userID, orgID int, name, description *string) (*storage.Organization, error) {
	_ = ctx
	if _, err := requireOrgManage(orgID, userID); err != nil {
		return nil, err
	}
	if name != nil {
		n, err := normalizeOrgName(*name)
		if err != nil {
			return nil, err
		}
		name = &n
	}
	if description != nil {
		d, err := normalizeOrgDescription(*description)
		if err != nil {
			return nil, err
		}
		description = &d
	}
	if _, err := storage.UpdateOrganization(orgID, name, description); err != nil {
		return nil, err
	}
	return storage.GetAccessibleOrganization(orgID, userID)
}

// DeleteOrganizationForUser removes an organization (owner only).
func DeleteOrganizationForUser(ctx context.Context, userID, orgID int) error {
	_ = ctx
	org, err := requireOrgAccess(orgID, userID)
	if err != nil {
		return err
	}
	if org.Role != storage.RoleOwner {
		return ErrForbidden
	}
	return storage.DeleteOrganization(orgID)
}

// ListOrganizationMembersForUser lists org members.
func ListOrganizationMembersForUser(ctx context.Context, userID, orgID int) ([]storage.OrganizationMember, error) {
	_ = ctx
	if _, err := requireOrgAccess(orgID, userID); err != nil {
		return nil, err
	}
	members, err := storage.ListOrganizationMembers(orgID)
	if err != nil {
		return nil, err
	}
	if members == nil {
		members = []storage.OrganizationMember{}
	}
	return members, nil
}

// InviteToOrganization creates a pending invite. The user must accept before
// they become a member. Accepting does not add them to existing imported projects.
func InviteToOrganization(ctx context.Context, actorUserID, orgID int, rawUsername, role string) (*storage.OrganizationInvite, error) {
	_ = ctx
	org, err := requireOrgManage(orgID, actorUserID)
	if err != nil {
		return nil, err
	}
	name, err := PrepareUsername(rawUsername)
	if err != nil {
		return nil, err
	}
	role = strings.TrimSpace(strings.ToLower(role))
	if !storage.ValidInviteRoleForOrg(orgID, role) {
		return nil, fmt.Errorf("%w: role is not assignable on this organization", ErrValidation)
	}
	user, err := storage.GetUserByUsername(name)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) || errors.Is(err, sql.ErrNoRows) ||
			strings.Contains(strings.ToLower(err.Error()), "no rows") {
			return nil, fmt.Errorf("%w: username not found", ErrNotFound)
		}
		return nil, err
	}
	if user == nil {
		return nil, fmt.Errorf("%w: username not found", ErrNotFound)
	}
	if user.ID == org.CreatedBy {
		return nil, fmt.Errorf("%w: cannot invite the organization owner", ErrValidation)
	}
	existing, err := storage.GetOrganizationRole(orgID, user.ID)
	if err != nil {
		return nil, err
	}
	if existing != "" {
		return nil, fmt.Errorf("%w: user is already a member", ErrValidation)
	}
	pending, err := storage.PendingOrganizationInviteExists(orgID, user.Email)
	if err != nil {
		return nil, err
	}
	if pending {
		return nil, fmt.Errorf("%w: user already has a pending invite", ErrValidation)
	}
	allow, err := storage.UserAllowsProjectInvites(user.ID)
	if err != nil {
		return nil, err
	}
	if !allow {
		return nil, fmt.Errorf("%w: user does not allow invites", ErrValidation)
	}
	inv, err := storage.CreateOrganizationInvite(orgID, user.Email, role, actorUserID, time.Now().Add(defaultInviteTTL))
	if err != nil {
		return nil, err
	}
	inv.UserName = user.UserName
	return inv, nil
}

// ListOrganizationInvitesForUser lists pending invites for an organization.
func ListOrganizationInvitesForUser(ctx context.Context, userID, orgID int) ([]storage.OrganizationInvite, error) {
	_ = ctx
	if _, err := requireOrgAccess(orgID, userID); err != nil {
		return nil, err
	}
	invites, err := storage.ListOrganizationInvites(orgID)
	if err != nil {
		return nil, err
	}
	if invites == nil {
		invites = []storage.OrganizationInvite{}
	}
	return invites, nil
}

// RevokeOrganizationInviteForUser deletes a pending invite.
func RevokeOrganizationInviteForUser(ctx context.Context, userID, orgID, inviteID int) error {
	_ = ctx
	if _, err := requireOrgManage(orgID, userID); err != nil {
		return err
	}
	if err := storage.DeleteOrganizationInvite(inviteID, orgID); err != nil {
		return ErrNotFound
	}
	return nil
}

// AcceptOrganizationInviteForUser accepts a pending invite for the current user.
func AcceptOrganizationInviteForUser(ctx context.Context, userID int, userEmail string, inviteID int) error {
	_ = ctx
	if _, err := storage.GetOrganizationInviteByID(inviteID); err != nil {
		return ErrNotFound
	}
	if err := storage.AcceptOrganizationInvite(inviteID, userID, userEmail); err != nil {
		if strings.Contains(err.Error(), "mismatch") || strings.Contains(err.Error(), "expired") || strings.Contains(err.Error(), "accepted") {
			return fmt.Errorf("%w: %s", ErrValidation, err.Error())
		}
		return err
	}
	return nil
}

// DeclineOrganizationInviteForUser declines a pending invite.
func DeclineOrganizationInviteForUser(ctx context.Context, userEmail string, inviteID int) error {
	_ = ctx
	if err := storage.DeclineOrganizationInvite(inviteID, userEmail); err != nil {
		return ErrNotFound
	}
	return nil
}

// ListMyOrganizationInvitesForUser returns pending org invites for the user's email.
func ListMyOrganizationInvitesForUser(ctx context.Context, userEmail string) ([]storage.OrganizationInvite, error) {
	_ = ctx
	invites, err := storage.ListPendingOrganizationInvitesForEmail(userEmail)
	if err != nil {
		return nil, err
	}
	if invites == nil {
		invites = []storage.OrganizationInvite{}
	}
	return invites, nil
}

// UpdateOrganizationMemberRoleForUser changes a non-owner member's role.
func UpdateOrganizationMemberRoleForUser(ctx context.Context, actorUserID, orgID, memberUserID int, role string) error {
	_ = ctx
	if _, err := requireOrgManage(orgID, actorUserID); err != nil {
		return err
	}
	role = strings.TrimSpace(strings.ToLower(role))
	if !storage.ValidInviteRoleForOrg(orgID, role) {
		return fmt.Errorf("%w: role is not assignable on this organization", ErrValidation)
	}
	current, err := storage.GetOrganizationRole(orgID, memberUserID)
	if err != nil {
		return err
	}
	if current == "" {
		return ErrNotFound
	}
	if current == storage.RoleOwner {
		return fmt.Errorf("%w: cannot change owner role", ErrValidation)
	}
	if err := storage.UpsertOrganizationMember(orgID, memberUserID, role); err != nil {
		return err
	}
	ids, err := storage.UpdateLockedOrgProjectMemberRole(orgID, memberUserID, role)
	if err != nil {
		return err
	}
	for _, pid := range ids {
		live.AfterProjectChangeLive(actorUserID, pid, live.TypeProjectUpdated)
		live.DispatchProjectHook(actorUserID, pid, live.TypeProjectMemberRoleChanged, &live.TaskHookMeta{
			MemberID:   memberUserID,
			MemberName: hookDisplayName(memberUserID),
		})
	}
	return nil
}

// OrgMemberRoleImpact lists org-linked projects a member is on, split by lock.
type OrgMemberRoleImpact struct {
	Locked   []storage.OrgMemberProjectRef
	Unlocked []storage.OrgMemberProjectRef
}

// OrganizationMemberRoleImpactForUser returns which projects would change if this
// member's organization role is updated.
func OrganizationMemberRoleImpactForUser(ctx context.Context, actorUserID, orgID, memberUserID int) (*OrgMemberRoleImpact, error) {
	_ = ctx
	if _, err := requireOrgManage(orgID, actorUserID); err != nil {
		return nil, err
	}
	role, err := storage.GetOrganizationRole(orgID, memberUserID)
	if err != nil {
		return nil, err
	}
	if role == "" {
		return nil, ErrNotFound
	}
	refs, err := storage.ListOrgProjectsForMember(orgID, memberUserID)
	if err != nil {
		return nil, err
	}
	out := &OrgMemberRoleImpact{
		Locked:   []storage.OrgMemberProjectRef{},
		Unlocked: []storage.OrgMemberProjectRef{},
	}
	for _, r := range refs {
		if r.Locked {
			out.Locked = append(out.Locked, r)
		} else {
			out.Unlocked = append(out.Unlocked, r)
		}
	}
	return out, nil
}

// ListOrganizationProjectRostersForUser returns attached projects and their members.
func ListOrganizationProjectRostersForUser(ctx context.Context, actorUserID, orgID int) ([]storage.OrgProjectRoster, error) {
	_ = ctx
	if _, err := requireOrgAccess(orgID, actorUserID); err != nil {
		return nil, err
	}
	rosters, err := storage.ListOrganizationProjectRosters(orgID)
	if err != nil {
		return nil, err
	}
	if rosters == nil {
		rosters = []storage.OrgProjectRoster{}
	}
	for i := range rosters {
		proj, err := storage.GetAccessibleProjectByID(rosters[i].ID, actorUserID)
		if err != nil {
			continue
		}
		rosters[i].CanManage = storage.RoleCanManageProject(proj.ID, proj.Role)
	}
	return rosters, nil
}

// RemoveOrganizationMemberForUser removes a non-owner, or allows self-leave.
func RemoveOrganizationMemberForUser(ctx context.Context, actorUserID, orgID, memberUserID int) error {
	_ = ctx
	org, err := requireOrgAccess(orgID, actorUserID)
	if err != nil {
		return err
	}
	selfLeave := actorUserID == memberUserID
	if !selfLeave && !storage.RoleCanManageOrganization(orgID, org.Role) {
		return ErrForbidden
	}
	targetRole, err := storage.GetOrganizationRole(orgID, memberUserID)
	if err != nil {
		return err
	}
	if targetRole == "" {
		return ErrNotFound
	}
	if targetRole == storage.RoleOwner {
		return fmt.Errorf("%w: cannot remove the organization owner", ErrValidation)
	}
	if err := storage.RemoveOrganizationMember(orgID, memberUserID); err != nil {
		return ErrNotFound
	}
	return nil
}

// ListOrganizationRolesForUser returns site roles plus org custom roles and the catalog.
func ListOrganizationRolesForUser(ctx context.Context, userID, orgID int) ([]storage.ProjectRoleDef, []storage.ProjectPermInfo, error) {
	_ = ctx
	if _, err := requireOrgAccess(orgID, userID); err != nil {
		return nil, nil, err
	}
	site, err := storage.ListSiteProjectRoles()
	if err != nil {
		return nil, nil, err
	}
	custom, err := storage.ListOrganizationRoles(orgID)
	if err != nil {
		return nil, nil, err
	}
	siteSlugs := make(map[string]bool, len(site))
	for _, d := range site {
		siteSlugs[d.Slug] = true
	}
	overridden := make(map[string]bool, len(custom))
	for i := range custom {
		if siteSlugs[custom[i].Slug] {
			custom[i].OverridesSite = true
			overridden[custom[i].Slug] = true
		}
	}
	out := make([]storage.ProjectRoleDef, 0, len(site)+len(custom))
	for _, d := range site {
		if d.Slug == storage.RoleOwner || overridden[d.Slug] {
			continue
		}
		out = append(out, d)
	}
	out = append(out, custom...)
	return out, storage.ProjectPermissionCatalog(), nil
}

func applyRoleCopy(in *CreateSiteProjectRoleInput, allowed func(*storage.ProjectRoleDef) bool) error {
	if in == nil || in.CopyFromID <= 0 {
		return nil
	}
	src, err := storage.GetProjectRoleDef(in.CopyFromID)
	if err != nil || src == nil {
		return fmt.Errorf("%w: copy source role not found", ErrNotFound)
	}
	if allowed != nil && !allowed(src) {
		return fmt.Errorf("%w: cannot copy that role into this list", ErrValidation)
	}
	if strings.TrimSpace(in.Name) == "" {
		in.Name = strings.TrimSpace(src.Name) + " (copy)"
	}
	if strings.TrimSpace(in.Description) == "" {
		in.Description = src.Description
	}
	if len(in.Permissions) == 0 {
		in.Permissions = append([]string{}, src.Permissions...)
	}
	if in.SortOrder == 0 {
		in.SortOrder = src.SortOrder + 1
	}
	return nil
}

func isSiteRoleDef(d *storage.ProjectRoleDef) bool {
	return d != nil && d.ProjectID == nil && d.OrganizationID == nil
}

// CreateOrganizationRoleForUser adds an org-only role, optionally copied from another role.
func CreateOrganizationRoleForUser(ctx context.Context, userID, orgID int, in CreateSiteProjectRoleInput) (*storage.ProjectRoleDef, error) {
	_ = ctx
	if _, err := requireOrgManage(orgID, userID); err != nil {
		return nil, err
	}
	if err := applyRoleCopy(&in, func(src *storage.ProjectRoleDef) bool {
		if isSiteRoleDef(src) {
			return true
		}
		return src.OrganizationID != nil && *src.OrganizationID == orgID && src.ProjectID == nil
	}); err != nil {
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
	n, err := storage.CountOrganizationCustomRoles(orgID)
	if err != nil {
		return nil, err
	}
	if n >= storage.MaxOrganizationCustomRoles {
		return nil, fmt.Errorf("%w: at most %d custom roles per organization", ErrValidation, storage.MaxOrganizationCustomRoles)
	}
	taken, err := storage.OrgRoleSlugTaken(orgID, slug, 0)
	if err != nil {
		return nil, err
	}
	if taken {
		return nil, fmt.Errorf("%w: role slug already exists on this organization", ErrConflict)
	}
	if slug == storage.RoleOwner {
		return nil, fmt.Errorf("%w: cannot override the owner role", ErrValidation)
	}
	created, err := storage.CreateOrganizationRoleDef(orgID, slug, name, desc, in.Permissions, in.SortOrder)
	if err != nil {
		if strings.Contains(err.Error(), "unknown permission") {
			return nil, fmt.Errorf("%w: %s", ErrValidation, err.Error())
		}
		return nil, err
	}
	markOrgRoleSiteOverride(created)
	return created, nil
}

// UpdateOrganizationRoleForUser patches an org-created role, or creates an org
// override when roleID is a site default (except owner).
func UpdateOrganizationRoleForUser(ctx context.Context, userID, orgID, roleID int, in UpdateSiteProjectRoleInput) (*storage.ProjectRoleDef, error) {
	_ = ctx
	if _, err := requireOrgManage(orgID, userID); err != nil {
		return nil, err
	}
	cur, err := storage.GetProjectRoleDef(roleID)
	if err != nil || cur == nil || cur.ProjectID != nil {
		return nil, ErrNotFound
	}
	if cur.Slug == storage.RoleOwner {
		return nil, fmt.Errorf("%w: cannot change owner role", ErrValidation)
	}
	if isSiteRoleDef(cur) {
		return upsertOrganizationRoleOverride(orgID, cur, in)
	}
	if cur.OrganizationID == nil || *cur.OrganizationID != orgID {
		return nil, ErrNotFound
	}
	if err := normalizeRolePatch(&in); err != nil {
		return nil, err
	}
	updated, err := storage.UpdateProjectRoleDef(roleID, in.Name, in.Description, in.Permissions, in.SortOrder)
	if err != nil {
		if strings.Contains(err.Error(), "unknown permission") {
			return nil, fmt.Errorf("%w: %s", ErrValidation, err.Error())
		}
		return nil, err
	}
	markOrgRoleSiteOverride(updated)
	return updated, nil
}

func normalizeRolePatch(in *UpdateSiteProjectRoleInput) error {
	if in.Name != nil {
		name, err := normalizeRoleName(*in.Name)
		if err != nil {
			return err
		}
		in.Name = &name
	}
	if in.Description != nil {
		desc, err := normalizeRoleDescription(*in.Description)
		if err != nil {
			return err
		}
		in.Description = &desc
	}
	return nil
}

func upsertOrganizationRoleOverride(orgID int, src *storage.ProjectRoleDef, in UpdateSiteProjectRoleInput) (*storage.ProjectRoleDef, error) {
	if err := normalizeRolePatch(&in); err != nil {
		return nil, err
	}
	existing, err := storage.GetOrganizationRoleBySlug(orgID, src.Slug)
	if err != nil {
		return nil, err
	}
	if existing != nil {
		updated, err := storage.UpdateProjectRoleDef(existing.ID, in.Name, in.Description, in.Permissions, in.SortOrder)
		if err != nil {
			if strings.Contains(err.Error(), "unknown permission") {
				return nil, fmt.Errorf("%w: %s", ErrValidation, err.Error())
			}
			return nil, err
		}
		markOrgRoleSiteOverride(updated)
		return updated, nil
	}
	n, err := storage.CountOrganizationCustomRoles(orgID)
	if err != nil {
		return nil, err
	}
	if n >= storage.MaxOrganizationCustomRoles {
		return nil, fmt.Errorf("%w: at most %d custom roles per organization", ErrValidation, storage.MaxOrganizationCustomRoles)
	}
	name := src.Name
	if in.Name != nil {
		name = *in.Name
	}
	desc := src.Description
	if in.Description != nil {
		desc = *in.Description
	}
	perms := src.Permissions
	if in.Permissions != nil {
		perms = *in.Permissions
	}
	created, err := storage.CreateOrganizationRoleDef(orgID, src.Slug, name, desc, perms, src.SortOrder)
	if err != nil {
		if strings.Contains(err.Error(), "unknown permission") {
			return nil, fmt.Errorf("%w: %s", ErrValidation, err.Error())
		}
		return nil, err
	}
	markOrgRoleSiteOverride(created)
	return created, nil
}

func markOrgRoleSiteOverride(d *storage.ProjectRoleDef) {
	if d == nil || d.OrganizationID == nil {
		return
	}
	ok, err := storage.SiteRoleSlugExists(d.Slug)
	if err == nil && ok {
		d.OverridesSite = true
	}
}

// DeleteOrganizationRoleForUser removes an unused org-created role.
func DeleteOrganizationRoleForUser(ctx context.Context, userID, orgID, roleID int) error {
	_ = ctx
	if _, err := requireOrgManage(orgID, userID); err != nil {
		return err
	}
	cur, err := storage.GetProjectRoleDef(roleID)
	if err != nil || cur == nil || cur.OrganizationID == nil || *cur.OrganizationID != orgID || cur.ProjectID != nil {
		return ErrNotFound
	}
	members, err := storage.CountOrgMembersWithRole(cur.Slug, orgID)
	if err != nil {
		return err
	}
	siteSlug, err := storage.SiteRoleSlugExists(cur.Slug)
	if err != nil {
		return err
	}
	if !siteSlug {
		if members > 0 {
			return fmt.Errorf("%w: role is still assigned to members", ErrConflict)
		}
		invites, err := storage.CountOrganizationInvitesWithRole(cur.Slug, orgID)
		if err != nil {
			return err
		}
		if invites > 0 {
			return fmt.Errorf("%w: role is still assigned to members or pending invites", ErrConflict)
		}
	}
	return storage.DeleteProjectRoleDef(roleID)
}

// ReorderOrganizationRolesForUser persists drag-drop order of org custom roles.
func ReorderOrganizationRolesForUser(ctx context.Context, userID, orgID int, roleIDs []int) error {
	_ = ctx
	if _, err := requireOrgManage(orgID, userID); err != nil {
		return err
	}
	if err := storage.ReorderProjectRoleDefs(roleIDs, false, 0, orgID); err != nil {
		return fmt.Errorf("%w: %s", ErrValidation, err.Error())
	}
	return nil
}

// ReorderSiteProjectRolesForAdmin persists drag-drop order of site roles.
func ReorderSiteProjectRolesForAdmin(ctx context.Context, userID int, roleIDs []int) error {
	_ = ctx
	if !storage.UserHasPermission(userID, "admin") {
		return ErrForbidden
	}
	if err := storage.ReorderProjectRoleDefs(roleIDs, true, 0, 0); err != nil {
		return fmt.Errorf("%w: %s", ErrValidation, err.Error())
	}
	return nil
}

// ReorderProjectCustomRolesForUser persists drag-drop order of project custom roles.
func ReorderProjectCustomRolesForUser(ctx context.Context, userID, projectID int, roleIDs []int) error {
	_ = ctx
	if _, err := requireProjectManage(projectID, userID); err != nil {
		return err
	}
	if err := denyOrgManagedMembershipEdits(projectID); err != nil {
		return err
	}
	if err := storage.ReorderProjectRoleDefs(roleIDs, false, projectID, 0); err != nil {
		return fmt.Errorf("%w: %s", ErrValidation, err.Error())
	}
	return nil
}
