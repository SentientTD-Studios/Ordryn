package storage

import (
	"context"
	"fmt"
)

// Site-level permissions stored on roles.permissions. Admins always have both.
const (
	SitePermAdmin          = "admin"
	SitePermProjectsCreate = "projects:create"
	SitePermOrgsCreate     = "orgs:create"
	// SitePermUsersManage creates accounts and deactivates or reactivates any account
	// that is not an admin or another people manager.
	SitePermUsersManage = "users:manage"
	// SitePermTeamsManage creates and deletes company-mode teams and sets who is on them.
	SitePermTeamsManage = "teams:manage"
	// SitePermTeamPeople manages the accounts of people on the company-mode teams
	// the holder is on (the Company team excluded).
	SitePermTeamPeople = "team:people"

	// Company-mode permissions split out of admin.

	// SitePermProfilesEdit sets people's first and last names within the holder's
	// people reach, and the holder's own.
	SitePermProfilesEdit = "people:profile"
	// SitePermProjectsViewAll opens every project read-only.
	SitePermProjectsViewAll = "projects:view_all"
	// SitePermAuditView reads the audit, comment, and email logs.
	SitePermAuditView = "audit:view"
	// SitePermRolesManage creates and edits site roles.
	SitePermRolesManage = "roles:manage"
	// SitePermSettingsManage changes site settings and extensions.
	SitePermSettingsManage = "settings:manage"

	// SitePermQueriesSite shares queries with everyone on the site and edits
	// or deletes the ones shared there.
	SitePermQueriesSite = "queries:site"

	// SitePermAgentsLocal creates and uses local agent keys: an AI agent on the
	// holder's own machine that works on their tasks as them.
	SitePermAgentsLocal = "agents:local"
	// SitePermAgentsProject adds and configures AI agent members on projects the
	// holder manages. Pausing or removing an agent never needs it.
	SitePermAgentsProject = "agents:project"
)

// AgentSitePerms are the AI agent permissions. Existing roles get both on upgrade.
var AgentSitePerms = []string{SitePermAgentsLocal, SitePermAgentsProject}

// MigrateGrantAgentPermissions gives the AI agent permissions to every existing
// site role so upgrading changes nothing. It runs once (recorded in
// data_migrations) so an admin who later revokes them does not see them return.
func MigrateGrantAgentPermissions() error {
	return grantSitePermsOnce("grant_site_agent_permissions", AgentSitePerms)
}

// grantSitePermsOnce appends perms to every site role the first time name runs.
func grantSitePermsOnce(name string, perms []string) error {
	pool, err := OpenDatabase()
	if err != nil {
		return err
	}
	defer CloseDatabase(pool)

	ctx := context.Background()
	if _, err := pool.Exec(ctx, `CREATE TABLE IF NOT EXISTS data_migrations (
		name TEXT PRIMARY KEY,
		applied_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
	)`); err != nil {
		return err
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	tag, err := tx.Exec(ctx, `INSERT INTO data_migrations (name) VALUES ($1) ON CONFLICT DO NOTHING`, name)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return nil
	}
	for _, perm := range perms {
		if _, err := tx.Exec(ctx, `
			UPDATE roles SET permissions = array_append(permissions, $1::text)
			WHERE NOT ($1::text = ANY(permissions))`, perm); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

// MemberRoleName is the site role every non-admin account gets.
const MemberRoleName = "user"

// UserHasSitePermission reports whether the user's site role grants perm. The
// admin permission implies every other one.
func UserHasSitePermission(userID int, perm string) bool {
	if userID <= 0 || perm == "" {
		return false
	}
	pool, err := OpenDatabase()
	if err != nil {
		return false
	}
	defer CloseDatabase(pool)

	var has, guest bool
	err = pool.QueryRow(context.Background(), `
		SELECT COALESCE($2::text = ANY(r.permissions) OR $3::text = ANY(r.permissions), FALSE),
		       COALESCE(u.is_guest, FALSE)
		FROM users u
		JOIN roles r ON r.id = u.role_id
		WHERE u.id = $1`, userID, perm, SitePermAdmin).Scan(&has, &guest)
	if err != nil || !has {
		return false
	}
	// Guests hold no site permission but creating projects, whatever their role
	// says (roles are also checked when they're given to guests).
	if guest {
		return guestAllowedSitePerms[perm] && roleHasExact(pool, userID, perm)
	}
	return true
}

// GetUserPermissions returns the live permissions of the user's site role. Sessions
// cache permissions at login, so authorization checks must use this instead.
func GetUserPermissions(userID int) ([]string, error) {
	pool, err := OpenDatabase()
	if err != nil {
		return nil, err
	}
	defer CloseDatabase(pool)

	var perms []string
	err = pool.QueryRow(context.Background(), `
		SELECT COALESCE(r.permissions, '{}')
		FROM users u
		LEFT JOIN roles r ON r.id = u.role_id
		WHERE u.id = $1`, userID).Scan(&perms)
	if err != nil {
		return nil, err
	}
	if perms == nil {
		perms = []string{}
	}
	if IsGuestUser(userID) {
		blocked := map[string]bool{}
		for _, p := range GuestBlockedPerms(perms) {
			blocked[p] = true
		}
		kept := make([]string, 0, len(perms))
		for _, p := range perms {
			if !blocked[p] {
				kept = append(kept, p)
			}
		}
		perms = kept
	}
	return perms, nil
}

// RoleHasPermission reports whether the named site role grants perm. A missing role grants nothing.
func RoleHasPermission(roleName, perm string) (bool, error) {
	pool, err := OpenDatabase()
	if err != nil {
		return false, err
	}
	defer CloseDatabase(pool)

	var has bool
	err = pool.QueryRow(context.Background(),
		`SELECT COALESCE(bool_or($2::text = ANY(permissions)), FALSE) FROM roles WHERE name = $1`,
		roleName, perm).Scan(&has)
	if err != nil {
		return false, err
	}
	return has, nil
}

// SetRolePermission grants or revokes perm on the named site role.
func SetRolePermission(roleName, perm string, grant bool) error {
	pool, err := OpenDatabase()
	if err != nil {
		return err
	}
	defer CloseDatabase(pool)

	var q string
	if grant {
		q = `UPDATE roles SET permissions = array_append(permissions, $2::text)
		     WHERE name = $1 AND NOT ($2::text = ANY(permissions))`
	} else {
		q = `UPDATE roles SET permissions = array_remove(permissions, $2::text) WHERE name = $1`
	}
	if _, err := pool.Exec(context.Background(), q, roleName, perm); err != nil {
		return fmt.Errorf("update role %q permission %q: %w", roleName, perm, err)
	}
	return nil
}

// MigrateGrantSiteCreatePermissions gives projects:create and orgs:create to every
// existing site role so upgrading changes nothing. It runs once (recorded in
// data_migrations) so an admin who later revokes them does not see them return.
func MigrateGrantSiteCreatePermissions() error {
	pool, err := OpenDatabase()
	if err != nil {
		return err
	}
	defer CloseDatabase(pool)

	ctx := context.Background()
	if _, err := pool.Exec(ctx, `CREATE TABLE IF NOT EXISTS data_migrations (
		name TEXT PRIMARY KEY,
		applied_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
	)`); err != nil {
		return err
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	tag, err := tx.Exec(ctx, `INSERT INTO data_migrations (name) VALUES ('grant_site_create_permissions') ON CONFLICT DO NOTHING`)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return nil
	}
	for _, perm := range []string{SitePermProjectsCreate, SitePermOrgsCreate} {
		if _, err := tx.Exec(ctx, `
			UPDATE roles SET permissions = array_append(permissions, $1::text)
			WHERE NOT ($1::text = ANY(permissions))`, perm); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}
