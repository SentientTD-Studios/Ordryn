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
)

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

	var has bool
	err = pool.QueryRow(context.Background(), `
		SELECT COALESCE($2::text = ANY(r.permissions) OR $3::text = ANY(r.permissions), FALSE)
		FROM users u
		JOIN roles r ON r.id = u.role_id
		WHERE u.id = $1`, userID, perm, SitePermAdmin).Scan(&has)
	return err == nil && has
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
