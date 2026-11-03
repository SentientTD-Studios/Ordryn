package storage

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// AdminRoleName is the built-in site role that holds the admin permission.
const AdminRoleName = "admin"

// SitePermCreateInvites lets a role send site invites even when user invites are off.
const SitePermCreateInvites = "createinvites"

const (
	MaxSiteRoleNameLen = 50
	MaxSiteCustomRoles = 20
)

// ErrSiteRoleNameTaken is returned when a site role name is already in use (case-insensitive).
var ErrSiteRoleNameTaken = errors.New("site role name already in use")

// SiteRole is a row in roles with how many accounts use it.
type SiteRole struct {
	ID          int
	Name        string
	DisplayName string
	Permissions []string
	UserCount   int
}

// BuiltIn reports whether the role is one of the two roles the site depends on.
func (r SiteRole) BuiltIn() bool {
	return isBuiltInSiteRoleName(r.Name)
}

// Label is the name people see. Built-in roles keep their stored name as a fixed key,
// so renaming one sets display_name instead.
func (r SiteRole) Label() string {
	return SiteRoleLabel(r.Name, r.DisplayName)
}

func isBuiltInSiteRoleName(name string) bool {
	return name == AdminRoleName || name == MemberRoleName
}

// SiteRoleLabel returns display when set, or the default name for the stored name.
func SiteRoleLabel(name, display string) string {
	if display = strings.TrimSpace(display); display != "" {
		return display
	}
	return SiteRoleDisplayName(name)
}

// MigrateRolesAddDisplayName adds roles.display_name, which renames a built-in role
// without changing the stored name the code looks it up by.
func MigrateRolesAddDisplayName() error {
	pool, err := OpenDatabase()
	if err != nil {
		return err
	}
	defer CloseDatabase(pool)

	if _, err := pool.Exec(context.Background(),
		`ALTER TABLE roles ADD COLUMN IF NOT EXISTS display_name TEXT NOT NULL DEFAULT ''`); err != nil {
		return fmt.Errorf("add roles.display_name: %w", err)
	}
	return nil
}

// SitePermInfo describes one site permission admins can grant to a role.
type SitePermInfo struct {
	ID          string
	Label       string
	Description string
	Group       string
}

// Admin is not in the catalog: only the built-in admin role holds it.
var sitePermCatalog = []SitePermInfo{
	{ID: SitePermProjectsCreate, Label: "Create projects", Description: "Start new projects", Group: "Create"},
	{ID: SitePermOrgsCreate, Label: "Create organizations", Description: "Start new organizations", Group: "Create"},
	{ID: SitePermCreateInvites, Label: "Send site invites", Description: "Invite people to join this site, even when user invites are turned off", Group: "Invites"},
	{ID: SitePermUsersManage, Label: "Manage people", Description: "Create accounts, put people on any team, and deactivate or reactivate accounts. Accounts of admins and other people managers stay with admins", Group: "People"},
}

// companySitePermCatalog is what a company-mode site role holds beyond the project
// permissions. Nobody creates organizations in company mode, and there are no invites:
// teams come from Manage teams, accounts from Manage people.
var companySitePermCatalog = []SitePermInfo{
	{ID: SitePermProjectsCreate, Label: "Create projects", Description: "Start new projects", Group: "Site"},
	{ID: SitePermProjectsViewAll, Label: "View all projects", Description: "Open every project read-only, including ones not shared with you. A share gives you more on that project", Group: "Site"},
	{ID: SitePermAuditView, Label: "View audit logs", Description: "Read the audit log, email log, and comment history", Group: "Site"},
	{ID: SitePermRolesManage, Label: "Manage roles", Description: "Create, rename, and change roles. Unless you're an admin, only roles and permissions your own role has, and never your own role", Group: "Site"},
	{ID: SitePermSettingsManage, Label: "Manage site settings", Description: "Change site settings and extensions. The site mode and what new accounts can create stay with admins", Group: "Site"},
	{ID: SitePermTeamsManage, Label: "Manage teams", Description: "Create, rename, and delete teams, and choose who is on them", Group: "People"},
	{ID: SitePermUsersManage, Label: "Manage people", Description: "Create accounts and deactivate or reactivate anyone's account. Accounts of admins and other people managers stay with admins", Group: "People"},
	{ID: SitePermTeamPeople, Label: "Manage team members' accounts", Description: "Create accounts for the teams you're on, and deactivate or reactivate their members. The Company team doesn't count", Group: "People"},
	{ID: SitePermProfilesEdit, Label: "Edit names", Description: "Set anyone's first and last name, including your own. Admins' names stay with the site owner", Group: "People"},
}

// SitePermissionCatalog returns the site permissions admins can assign. In company
// mode a site role is the only role anyone has, so it also carries every project permission.
func SitePermissionCatalog() []SitePermInfo {
	if !IsCompanyMode() {
		out := make([]SitePermInfo, len(sitePermCatalog))
		copy(out, sitePermCatalog)
		return out
	}
	out := make([]SitePermInfo, 0, len(companySitePermCatalog)+len(projectPermCatalog))
	out = append(out, companySitePermCatalog...)
	for _, p := range ProjectPermissionCatalog() {
		out = append(out, SitePermInfo{ID: p.ID, Label: p.Label, Description: p.Description, Group: p.Group})
	}
	return out
}

// IsSitePermInCatalog reports whether perm can be assigned from the admin UI.
func IsSitePermInCatalog(perm string) bool {
	for _, p := range SitePermissionCatalog() {
		if p.ID == perm {
			return true
		}
	}
	return false
}

const siteRoleSelect = `
	SELECT r.id, r.name, r.display_name, r.permissions,
	       (SELECT COUNT(*) FROM users u WHERE u.role_id = r.id AND NOT COALESCE(u.is_agent, FALSE))
	FROM roles r`

func scanSiteRole(row pgx.Row, r *SiteRole) error {
	if err := row.Scan(&r.ID, &r.Name, &r.DisplayName, &r.Permissions, &r.UserCount); err != nil {
		return err
	}
	if r.Permissions == nil {
		r.Permissions = []string{}
	}
	return nil
}

// ListSiteRoles returns admin, then user, then custom roles by name.
func ListSiteRoles() ([]SiteRole, error) {
	pool, err := OpenDatabase()
	if err != nil {
		return nil, err
	}
	defer CloseDatabase(pool)

	rows, err := pool.Query(context.Background(), siteRoleSelect+`
		ORDER BY CASE r.name WHEN 'admin' THEN 0 WHEN 'user' THEN 1 ELSE 2 END, LOWER(COALESCE(NULLIF(r.display_name, ''), r.name)), r.id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []SiteRole{}
	for rows.Next() {
		var r SiteRole
		if err := scanSiteRole(rows, &r); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// GetSiteRole loads one role by id.
func GetSiteRole(id int) (*SiteRole, error) {
	pool, err := OpenDatabase()
	if err != nil {
		return nil, err
	}
	defer CloseDatabase(pool)

	var r SiteRole
	if err := scanSiteRole(pool.QueryRow(context.Background(), siteRoleSelect+` WHERE r.id = $1`, id), &r); err != nil {
		return nil, err
	}
	return &r, nil
}

// CountCustomSiteRoles counts roles other than admin and user.
func CountCustomSiteRoles() (int, error) {
	pool, err := OpenDatabase()
	if err != nil {
		return 0, err
	}
	defer CloseDatabase(pool)

	var n int
	err = pool.QueryRow(context.Background(),
		`SELECT COUNT(*) FROM roles WHERE name NOT IN ($1, $2)`, AdminRoleName, MemberRoleName).Scan(&n)
	return n, err
}

func siteRoleNameTaken(ctx context.Context, q interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}, name string, exceptID int) (bool, error) {
	// A name clashes with another role's stored name or its label. The built-in
	// defaults (Admin, User) are their stored names, so they stay reserved.
	var taken bool
	err := q.QueryRow(ctx,
		`SELECT EXISTS (SELECT 1 FROM roles
		  WHERE (LOWER(name) = LOWER($1) OR LOWER(NULLIF(display_name, '')) = LOWER($1)) AND id <> $2)`,
		name, exceptID).Scan(&taken)
	return taken, err
}

func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}

// CreateSiteRole inserts a custom role.
func CreateSiteRole(name string, perms []string) (*SiteRole, error) {
	pool, err := OpenDatabase()
	if err != nil {
		return nil, err
	}
	defer CloseDatabase(pool)

	ctx := context.Background()
	if taken, err := siteRoleNameTaken(ctx, pool, name, 0); err != nil {
		return nil, err
	} else if taken {
		return nil, ErrSiteRoleNameTaken
	}
	if perms == nil {
		perms = []string{}
	}
	var id int
	if err := pool.QueryRow(ctx,
		`INSERT INTO roles (name, permissions) VALUES ($1, $2) RETURNING id`, name, perms).Scan(&id); err != nil {
		if isUniqueViolation(err) {
			return nil, ErrSiteRoleNameTaken
		}
		return nil, fmt.Errorf("create site role: %w", err)
	}
	InvalidateCompanyRoleCache()
	return GetSiteRole(id)
}

// UpdateSiteRole renames a role and/or sets its catalog permissions. Permissions
// outside the catalog (admin, legacy ones) are kept as they are. Renaming a built-in
// role sets its display name; its stored name never changes.
func UpdateSiteRole(id int, name *string, perms *[]string) (*SiteRole, error) {
	pool, err := OpenDatabase()
	if err != nil {
		return nil, err
	}
	defer CloseDatabase(pool)

	ctx := context.Background()
	tx, err := pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	var current []string
	var storedName string
	if err := tx.QueryRow(ctx, `SELECT name, permissions FROM roles WHERE id = $1 FOR UPDATE`, id).Scan(&storedName, &current); err != nil {
		return nil, err
	}
	if name != nil {
		if taken, err := siteRoleNameTaken(ctx, tx, *name, id); err != nil {
			return nil, err
		} else if taken {
			return nil, ErrSiteRoleNameTaken
		}
		query := `UPDATE roles SET name = $2 WHERE id = $1`
		next := *name
		if isBuiltInSiteRoleName(storedName) {
			query = `UPDATE roles SET display_name = $2 WHERE id = $1`
			if next == SiteRoleDisplayName(storedName) {
				next = ""
			}
		}
		if _, err := tx.Exec(ctx, query, id, next); err != nil {
			if isUniqueViolation(err) {
				return nil, ErrSiteRoleNameTaken
			}
			return nil, err
		}
	}
	if perms != nil {
		next := []string{}
		for _, p := range current {
			if !IsSitePermInCatalog(p) {
				next = append(next, p)
			}
		}
		next = append(next, *perms...)
		if _, err := tx.Exec(ctx, `UPDATE roles SET permissions = $2 WHERE id = $1`, id, next); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	// Project access reads the role by id, so new permissions apply at once. A
	// recompute is still needed: who holds projects:view_all, and which role wins for
	// someone reached by several shares, can change.
	InvalidateCompanyRoleCache()
	if perms != nil {
		if err := RecomputeAllCompanyProjects(); err != nil {
			return nil, err
		}
	}
	return GetSiteRole(id)
}

// DeleteSiteRole moves the role's accounts to the member role, then deletes it.
// It returns how many accounts were moved.
func DeleteSiteRole(id int) (int, error) {
	pool, err := OpenDatabase()
	if err != nil {
		return 0, err
	}
	defer CloseDatabase(pool)

	ctx := context.Background()
	tx, err := pool.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(ctx)

	var memberID int
	if err := tx.QueryRow(ctx, `SELECT id FROM roles WHERE name = $1`, MemberRoleName).Scan(&memberID); err != nil {
		return 0, fmt.Errorf("member role: %w", err)
	}
	if memberID == id {
		return 0, fmt.Errorf("cannot delete the member role")
	}
	tag, err := tx.Exec(ctx, `UPDATE users SET role_id = $2 WHERE role_id = $1`, id, memberID)
	if err != nil {
		return 0, err
	}
	del, err := tx.Exec(ctx, `DELETE FROM roles WHERE id = $1`, id)
	if err != nil {
		return 0, err
	}
	if del.RowsAffected() == 0 {
		return 0, pgx.ErrNoRows
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, err
	}
	InvalidateCompanyRoleCache()
	// The role's people moved to the User role; their projects need the new slug.
	if err := RecomputeAllCompanyProjects(); err != nil {
		return int(tag.RowsAffected()), err
	}
	return int(tag.RowsAffected()), nil
}

// GetUserSiteRoleID returns the user's role id, and whether the account is an agent.
func GetUserSiteRoleID(userID int) (roleID int, isAgent bool, err error) {
	pool, err := OpenDatabase()
	if err != nil {
		return 0, false, err
	}
	defer CloseDatabase(pool)

	err = pool.QueryRow(context.Background(),
		`SELECT role_id, COALESCE(is_agent, FALSE) FROM users WHERE id = $1`, userID).Scan(&roleID, &isAgent)
	return roleID, isAgent, err
}

// SetUserSiteRole assigns a site role to an account.
func SetUserSiteRole(userID, roleID int) error {
	pool, err := OpenDatabase()
	if err != nil {
		return err
	}
	defer CloseDatabase(pool)

	tag, err := pool.Exec(context.Background(), `UPDATE users SET role_id = $2 WHERE id = $1`, userID, roleID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return pgx.ErrNoRows
	}
	RecomputeAfterAccountChange(userID)
	return nil
}

// CountActiveAdmins counts unbanned, non-agent accounts whose role grants admin.
func CountActiveAdmins() (int, error) {
	pool, err := OpenDatabase()
	if err != nil {
		return 0, err
	}
	defer CloseDatabase(pool)

	var n int
	err = pool.QueryRow(context.Background(), `
		SELECT COUNT(*) FROM users u
		JOIN roles r ON r.id = u.role_id
		WHERE $1::text = ANY(r.permissions)
		  AND NOT COALESCE(u.is_agent, FALSE)
		  AND NOT COALESCE(u.is_banned, FALSE)`, SitePermAdmin).Scan(&n)
	return n, err
}

// RoleGrantsAdmin reports whether the permission list includes admin.
func RoleGrantsAdmin(perms []string) bool {
	for _, p := range perms {
		if strings.EqualFold(p, SitePermAdmin) {
			return true
		}
	}
	return false
}
