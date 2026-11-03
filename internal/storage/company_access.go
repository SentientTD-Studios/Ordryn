package storage

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
)

// Company mode has no separate project roles: every role is a site role. A project is
// shared with whole teams or with individual people. Each share is either view only
// or full; a full share names the site role people get on this project, or none to
// use each person's own site role.
//
// project_members stays the list of who can open a project, so every existing
// permission check keeps working: RecomputeCompanyProjectAccess rewrites it from the
// shares, giving each person the role slug "site:<role id>" (full) or "view". Site
// admins get full access to every project, and holders of projects:view_all view
// access to the rest; rows nothing else explains are marked implicit_admin so member
// lists can leave them out.

const (
	AccessFull = "full"
	AccessView = "view"

	// CompanyViewRoleSlug is the project role of a view-only share.
	CompanyViewRoleSlug   = "view"
	companySiteRolePrefix = "site:"
)

// ErrNotCompanyMode is returned by company-only operations on a multi-user site.
var ErrNotCompanyMode = errors.New("only available in company mode")

// MigrateCompanyProjectAccess creates the share table and the implicit-admin marker.
func MigrateCompanyProjectAccess() error {
	pool, err := OpenDatabase()
	if err != nil {
		return err
	}
	defer CloseDatabase(pool)

	stmts := []string{
		`CREATE TABLE IF NOT EXISTS project_access_grants (
			id SERIAL PRIMARY KEY,
			project_id INTEGER NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
			organization_id INTEGER REFERENCES organizations(id) ON DELETE CASCADE,
			user_id INTEGER REFERENCES users(id) ON DELETE CASCADE,
			level VARCHAR(8) NOT NULL DEFAULT 'full',
			granted_by INTEGER REFERENCES users(id) ON DELETE SET NULL,
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			CHECK ((organization_id IS NULL) <> (user_id IS NULL)),
			CHECK (level IN ('full', 'view'))
		)`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_project_access_team ON project_access_grants (project_id, organization_id) WHERE organization_id IS NOT NULL`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_project_access_user ON project_access_grants (project_id, user_id) WHERE user_id IS NOT NULL`,
		`ALTER TABLE project_members ADD COLUMN IF NOT EXISTS implicit_admin BOOLEAN NOT NULL DEFAULT FALSE`,
		// NULL means each person's own site role. A deleted role falls back to that.
		`ALTER TABLE project_access_grants ADD COLUMN IF NOT EXISTS role_id INTEGER REFERENCES roles(id) ON DELETE SET NULL`,
	}
	for _, s := range stmts {
		if _, err := pool.Exec(context.Background(), s); err != nil {
			return fmt.Errorf("company project access: %w", err)
		}
	}
	return nil
}

// IsCompanyMode reports whether the site runs in company mode.
func IsCompanyMode() bool {
	mode, err := GetSiteMode()
	return err == nil && mode == SiteModeCompany
}

// CompanySiteRoleSlug is the project role slug for full access under site role roleID.
func CompanySiteRoleSlug(roleID int) string {
	return companySiteRolePrefix + strconv.Itoa(roleID)
}

var (
	companyRoleCacheMu sync.RWMutex
	companyRoleCache   = map[int]SiteRole{}
)

// InvalidateCompanyRoleCache drops cached site roles after one is created, changed, or deleted.
func InvalidateCompanyRoleCache() {
	companyRoleCacheMu.Lock()
	companyRoleCache = map[int]SiteRole{}
	companyRoleCacheMu.Unlock()
}

func cachedSiteRole(id int) (*SiteRole, error) {
	companyRoleCacheMu.RLock()
	r, ok := companyRoleCache[id]
	companyRoleCacheMu.RUnlock()
	if ok {
		return &r, nil
	}
	role, err := GetSiteRole(id)
	if err != nil {
		return nil, err
	}
	companyRoleCacheMu.Lock()
	companyRoleCache[id] = *role
	companyRoleCacheMu.Unlock()
	return role, nil
}

// SiteRoleDisplayName is the default label for a stored site role name.
func SiteRoleDisplayName(name string) string {
	switch name {
	case AdminRoleName:
		return "Admin"
	case MemberRoleName:
		return "User"
	}
	return name
}

// companyRoleDef resolves the project role slugs company mode writes, or returns nil
// for any other slug.
func companyRoleDef(slug string) *ProjectRoleDef {
	if slug == CompanyViewRoleSlug {
		return &ProjectRoleDef{Slug: CompanyViewRoleSlug, Name: "View only", Permissions: []string{}, IsSystem: true}
	}
	if !strings.HasPrefix(slug, companySiteRolePrefix) {
		return nil
	}
	id, err := strconv.Atoi(strings.TrimPrefix(slug, companySiteRolePrefix))
	if err != nil {
		return nil
	}
	role, err := cachedSiteRole(id)
	if err != nil {
		// A deleted role grants nothing until the next recompute moves its people.
		return &ProjectRoleDef{Slug: slug, Name: "Removed role", Permissions: []string{}, IsSystem: true}
	}
	perms := []string{}
	if permListContains(role.Permissions, SitePermAdmin) {
		perms = AllProjectPerms()
	} else {
		for _, p := range role.Permissions {
			if ValidProjectPerm(p) {
				perms = append(perms, p)
			}
		}
	}
	return &ProjectRoleDef{Slug: slug, Name: role.Label(), Permissions: perms, IsSystem: true}
}

// CompanyAssignableRoles lists the project role slugs company mode uses: one per site
// role, plus view only. Status gates and AI agents pick from these.
func CompanyAssignableRoles() ([]ProjectRoleDef, error) {
	roles, err := ListSiteRoles()
	if err != nil {
		return nil, err
	}
	out := make([]ProjectRoleDef, 0, len(roles)+1)
	for _, r := range roles {
		if def := companyRoleDef(CompanySiteRoleSlug(r.ID)); def != nil {
			out = append(out, *def)
		}
	}
	out = append(out, *companyRoleDef(CompanyViewRoleSlug))
	return out, nil
}

// ProjectAccessGrant is one share of a project with a team or a person.
type ProjectAccessGrant struct {
	ID             int
	ProjectID      int
	OrganizationID *int
	UserID         *int
	Level          string
	// RoleID is the site role a full share gives; nil means each person's own.
	RoleID *int
	// RoleLabel is RoleID's label, empty when RoleID is nil.
	RoleLabel string
	// Name is the team name or how the person reads (full name, else username); Email is set for people.
	Name        string
	Email       string
	UserName    string
	IsCompany   bool
	MemberCount int
	CreatedAt   time.Time
}

// ListProjectAccessGrants returns a project's shares, teams first.
func ListProjectAccessGrants(projectID int) ([]ProjectAccessGrant, error) {
	pool, err := OpenDatabase()
	if err != nil {
		return nil, err
	}
	defer CloseDatabase(pool)

	rows, err := pool.Query(context.Background(), `
		SELECT g.id, g.project_id, g.organization_id, g.user_id, g.level, g.role_id,
		       COALESCE(r.name, ''), COALESCE(r.display_name, ''), g.created_at,
		       COALESCE(o.name, NULLIF(u.full_name, ''), u.user_name, ''), COALESCE(u.email, ''), COALESCE(u.user_name, ''),
		       COALESCE(o.is_company, FALSE),
		       COALESCE((SELECT COUNT(*) FROM organization_members m WHERE m.organization_id = g.organization_id), 0)
		FROM project_access_grants g
		LEFT JOIN organizations o ON o.id = g.organization_id
		LEFT JOIN users u ON u.id = g.user_id
		LEFT JOIN roles r ON r.id = g.role_id
		WHERE g.project_id = $1
		ORDER BY g.organization_id IS NULL, LOWER(COALESCE(o.name, NULLIF(u.full_name, ''), u.user_name, '')), g.id`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ProjectAccessGrant{}
	for rows.Next() {
		var g ProjectAccessGrant
		var roleName, roleDisplay string
		if err := rows.Scan(&g.ID, &g.ProjectID, &g.OrganizationID, &g.UserID, &g.Level, &g.RoleID,
			&roleName, &roleDisplay, &g.CreatedAt, &g.Name, &g.Email, &g.UserName, &g.IsCompany, &g.MemberCount); err != nil {
			return nil, err
		}
		if g.RoleID != nil {
			g.RoleLabel = SiteRoleLabel(roleName, roleDisplay)
		}
		out = append(out, g)
	}
	return out, rows.Err()
}

// UpsertProjectAccessGrant shares a project with a team (orgID) or a person (userID),
// or changes an existing share. Exactly one of orgID and userID is set. roleID is the
// site role a full share gives, or 0 for each person's own; view shares ignore it.
func UpsertProjectAccessGrant(projectID, orgID, userID int, level string, roleID int, grantedBy int) error {
	if level != AccessFull && level != AccessView {
		return fmt.Errorf("unknown access level %q", level)
	}
	var role *int
	if level == AccessFull && roleID > 0 {
		role = &roleID
	}
	if (orgID > 0) == (userID > 0) {
		return fmt.Errorf("share with a team or a person")
	}
	pool, err := OpenDatabase()
	if err != nil {
		return err
	}
	defer CloseDatabase(pool)

	ctx := context.Background()
	if orgID > 0 {
		_, err = pool.Exec(ctx, `
			INSERT INTO project_access_grants (project_id, organization_id, level, role_id, granted_by) VALUES ($1, $2, $3, $4, $5)
			ON CONFLICT (project_id, organization_id) WHERE organization_id IS NOT NULL
			DO UPDATE SET level = EXCLUDED.level, role_id = EXCLUDED.role_id`,
			projectID, orgID, level, role, grantedBy)
	} else {
		_, err = pool.Exec(ctx, `
			INSERT INTO project_access_grants (project_id, user_id, level, role_id, granted_by) VALUES ($1, $2, $3, $4, $5)
			ON CONFLICT (project_id, user_id) WHERE user_id IS NOT NULL
			DO UPDATE SET level = EXCLUDED.level, role_id = EXCLUDED.role_id`,
			projectID, userID, level, role, grantedBy)
	}
	if err != nil {
		return err
	}
	return RecomputeCompanyProjectAccess(projectID)
}

// DeleteProjectAccessGrant removes one share of a project.
func DeleteProjectAccessGrant(projectID, grantID int) error {
	pool, err := OpenDatabase()
	if err != nil {
		return err
	}
	defer CloseDatabase(pool)

	tag, err := pool.Exec(context.Background(),
		`DELETE FROM project_access_grants WHERE id = $1 AND project_id = $2`, grantID, projectID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return pgx.ErrNoRows
	}
	return RecomputeCompanyProjectAccess(projectID)
}

// companyAccess is what one person gets on a project: a site role (full) or view only.
type companyAccess struct {
	roleID   int // 0 means view only
	implicit bool
}

// companyRoleStrength ranks site roles by how many project permissions they give, so
// someone reached by several shares gets the strongest. View only ranks below every role.
func companyRoleStrength(perms []string) int {
	if permListContains(perms, SitePermAdmin) {
		return len(projectPermCatalog) + 1
	}
	n := 0
	for _, p := range perms {
		if ValidProjectPerm(p) {
			n++
		}
	}
	return n
}

// RecomputeCompanyProjectAccess rewrites a project's people from its shares. The
// creator gets their own site role; each team and personal share adds its people with
// the share's role (or their own) or view only. Someone reached more than once gets
// the strongest role, ties going to the lower role id. Site admins always get their
// own role, and holders of projects:view_all at least view access. AI agents are
// left alone; they are managed per project. Does nothing outside company mode.
func RecomputeCompanyProjectAccess(projectID int) error {
	if !IsCompanyMode() {
		return nil
	}
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

	// Serialize recomputes of the same project.
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtext('company_project_access'), $1)`, projectID); err != nil {
		return err
	}

	strength := map[int]int{}
	var admins, viewers []int
	roleRows, err := tx.Query(ctx, `SELECT id, permissions FROM roles`)
	if err != nil {
		return err
	}
	for roleRows.Next() {
		var id int
		var perms []string
		if err := roleRows.Scan(&id, &perms); err != nil {
			roleRows.Close()
			return err
		}
		strength[id] = companyRoleStrength(perms)
	}
	roleRows.Close()
	if err := roleRows.Err(); err != nil {
		return err
	}

	const human = `NOT COALESCE(u.is_agent, FALSE)`
	siteRole := map[int]int{}
	userRows, err := tx.Query(ctx, `
		SELECT u.id, COALESCE(u.role_id, 0),
		       COALESCE($1::text = ANY(r.permissions), FALSE),
		       COALESCE($2::text = ANY(r.permissions), FALSE)
		FROM users u LEFT JOIN roles r ON r.id = u.role_id
		WHERE `+human, SitePermAdmin, SitePermProjectsViewAll)
	if err != nil {
		return err
	}
	for userRows.Next() {
		var id, roleID int
		var admin, viewAll bool
		if err := userRows.Scan(&id, &roleID, &admin, &viewAll); err != nil {
			userRows.Close()
			return err
		}
		siteRole[id] = roleID
		if admin {
			admins = append(admins, id)
		} else if viewAll {
			viewers = append(viewers, id)
		}
	}
	userRows.Close()
	if err := userRows.Err(); err != nil {
		return err
	}

	rank := func(a companyAccess) (int, int) {
		if a.roleID == 0 {
			return -1, 0
		}
		return strength[a.roleID], -a.roleID
	}
	stronger := func(a, b companyAccess) bool {
		as, at := rank(a)
		bs, bt := rank(b)
		return as > bs || (as == bs && at > bt)
	}

	want := map[int]companyAccess{}
	collect := func(sql string, args ...any) error {
		rows, err := tx.Query(ctx, sql, args...)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var id int
			var level string
			var roleID *int
			if err := rows.Scan(&id, &level, &roleID); err != nil {
				return err
			}
			a := companyAccess{}
			if level == AccessFull {
				a.roleID = siteRole[id]
				if roleID != nil {
					a.roleID = *roleID
				}
			}
			if cur, ok := want[id]; !ok || stronger(a, cur) {
				want[id] = a
			}
		}
		return rows.Err()
	}
	if err := collect(`SELECT p.user_id, 'full', NULL::int FROM projects p JOIN users u ON u.id = p.user_id
		WHERE p.id = $1 AND `+human, projectID); err != nil {
		return err
	}
	if err := collect(`SELECT g.user_id, g.level, g.role_id FROM project_access_grants g JOIN users u ON u.id = g.user_id
		WHERE g.project_id = $1 AND `+human, projectID); err != nil {
		return err
	}
	if err := collect(`SELECT m.user_id, g.level, g.role_id FROM project_access_grants g
		JOIN organization_members m ON m.organization_id = g.organization_id
		JOIN users u ON u.id = m.user_id
		WHERE g.project_id = $1 AND `+human, projectID); err != nil {
		return err
	}
	// Admins always get their own role, which grants everything, whatever a share
	// says. Someone nothing else reaches is marked implicit.
	for _, id := range admins {
		_, shared := want[id]
		want[id] = companyAccess{roleID: siteRole[id], implicit: !shared}
	}
	for _, id := range viewers {
		if _, ok := want[id]; !ok {
			want[id] = companyAccess{implicit: true}
		}
	}

	if _, err := tx.Exec(ctx, `
		DELETE FROM project_members pm USING users u
		WHERE pm.project_id = $1 AND u.id = pm.user_id AND NOT COALESCE(u.is_agent, FALSE)
		  AND NOT (pm.user_id = ANY($2))`, projectID, keys(want)); err != nil {
		return err
	}
	for userID, a := range want {
		role := CompanyViewRoleSlug
		if a.roleID > 0 {
			role = CompanySiteRoleSlug(a.roleID)
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO project_members (project_id, user_id, role, implicit_admin) VALUES ($1, $2, $3, $4)
			ON CONFLICT (project_id, user_id) DO UPDATE SET role = EXCLUDED.role, implicit_admin = EXCLUDED.implicit_admin
			WHERE project_members.role IS DISTINCT FROM EXCLUDED.role
			   OR project_members.implicit_admin IS DISTINCT FROM EXCLUDED.implicit_admin`,
			projectID, userID, role, a.implicit); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

func keys(m map[int]companyAccess) []int {
	out := make([]int, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

// RecomputeAllCompanyProjects recomputes every project. Used after changes that can
// reach any project: a site role change, an admin added or removed, a role deleted.
func RecomputeAllCompanyProjects() error {
	if !IsCompanyMode() {
		return nil
	}
	return recomputeProjects(`SELECT id FROM projects`)
}

// RecomputeCompanyProjectsForTeam recomputes the projects shared with a team, after
// someone joins or leaves it.
func RecomputeCompanyProjectsForTeam(orgID int) error {
	if !IsCompanyMode() {
		return nil
	}
	return recomputeProjects(`SELECT DISTINCT project_id FROM project_access_grants WHERE organization_id = $1`, orgID)
}

// RecomputeCompanyProjectsForUser recomputes the projects a person can reach through
// a personal share or a team, after their site role changes.
func RecomputeCompanyProjectsForUser(userID int) error {
	if !IsCompanyMode() {
		return nil
	}
	return recomputeProjects(`
		SELECT project_id FROM project_members WHERE user_id = $1
		UNION SELECT project_id FROM project_access_grants WHERE user_id = $1
		UNION SELECT g.project_id FROM project_access_grants g
		      JOIN organization_members m ON m.organization_id = g.organization_id WHERE m.user_id = $1`, userID)
}

func recomputeProjects(query string, args ...any) error {
	pool, err := OpenDatabase()
	if err != nil {
		return err
	}
	defer CloseDatabase(pool)

	rows, err := pool.Query(context.Background(), query, args...)
	if err != nil {
		return err
	}
	var ids []int
	for rows.Next() {
		var id int
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		ids = append(ids, id)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	for _, id := range ids {
		if err := RecomputeCompanyProjectAccess(id); err != nil {
			return fmt.Errorf("recompute project %d: %w", id, err)
		}
	}
	return nil
}

// RecomputeAfterAccountChange keeps project access current after a person's site
// role changes. Gaining or losing admin or projects:view_all reaches every project
// (admin implies view_all).
func RecomputeAfterAccountChange(userID int) {
	if !IsCompanyMode() {
		return
	}
	var err error
	if UserHasSitePermission(userID, SitePermProjectsViewAll) || userIsImplicitAdminAnywhere(userID) {
		err = RecomputeAllCompanyProjects()
	} else {
		err = RecomputeCompanyProjectsForUser(userID)
	}
	if err != nil {
		fmt.Printf("company: project access for user %d: %v\n", userID, err)
	}
}

func userIsImplicitAdminAnywhere(userID int) bool {
	pool, err := OpenDatabase()
	if err != nil {
		return false
	}
	defer CloseDatabase(pool)
	var found bool
	_ = pool.QueryRow(context.Background(),
		`SELECT EXISTS (SELECT 1 FROM project_members WHERE user_id = $1 AND implicit_admin)`, userID).Scan(&found)
	return found
}
