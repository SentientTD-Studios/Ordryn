package storage

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
)

// CompanyMemberRole is the organization role everyone gets when they join the company.
const CompanyMemberRole = "member"

// MigrateOrganizationsAddCompany adds organizations.is_company (at most one row) and
// stops deleting an account from cascading into the organizations it created.
func MigrateOrganizationsAddCompany() error {
	pool, err := OpenDatabase()
	if err != nil {
		return err
	}
	defer CloseDatabase(pool)

	ctx := context.Background()
	stmts := []string{
		`ALTER TABLE organizations ADD COLUMN IF NOT EXISTS is_company BOOLEAN NOT NULL DEFAULT FALSE`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_organizations_single_company ON organizations ((TRUE)) WHERE is_company`,
	}
	for _, s := range stmts {
		if _, err := pool.Exec(ctx, s); err != nil {
			return fmt.Errorf("organizations company column: %w", err)
		}
	}

	// ON DELETE CASCADE on created_by would take the whole company (or any organization)
	// with its creator. Swap it for RESTRICT once.
	var cascades bool
	if err := pool.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM pg_constraint
			WHERE conrelid = 'organizations'::regclass AND contype = 'f'
			  AND conname = 'organizations_created_by_fkey' AND confdeltype = 'c'
		)`).Scan(&cascades); err != nil {
		return err
	}
	if cascades {
		if _, err := pool.Exec(ctx, `
			ALTER TABLE organizations
			  DROP CONSTRAINT organizations_created_by_fkey,
			  ADD CONSTRAINT organizations_created_by_fkey
			    FOREIGN KEY (created_by) REFERENCES users(id) ON DELETE RESTRICT`); err != nil {
			return fmt.Errorf("organizations created_by restrict: %w", err)
		}
	}
	return nil
}

// CompanyOrganizationID returns the company organization's id, or 0 when there is none.
func CompanyOrganizationID() (int, error) {
	pool, err := OpenDatabase()
	if err != nil {
		return 0, err
	}
	defer CloseDatabase(pool)

	var id int
	err = pool.QueryRow(context.Background(), `SELECT id FROM organizations WHERE is_company`).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, nil
	}
	return id, err
}

// IsCompanyOrganization reports whether orgID is the company.
func IsCompanyOrganization(orgID int) bool {
	id, err := CompanyOrganizationID()
	return err == nil && id > 0 && id == orgID
}

// EnsureCompanyOrganization creates the company organization and its Member role if
// missing, and returns its id. It returns 0 while the site has no accounts yet,
// since an organization needs a creator.
func EnsureCompanyOrganization() (int, error) {
	if id, err := CompanyOrganizationID(); err != nil || id > 0 {
		return id, err
	}
	pool, err := OpenDatabase()
	if err != nil {
		return 0, err
	}
	defer CloseDatabase(pool)

	ctx := context.Background()
	// Prefer an admin as the recorded creator. Nobody is made owner: site admins
	// manage the company through their admin permission instead.
	var creator int
	err = pool.QueryRow(ctx, `
		SELECT u.id FROM users u
		LEFT JOIN roles r ON r.id = u.role_id
		WHERE NOT COALESCE(u.is_agent, FALSE)
		ORDER BY COALESCE($1::text = ANY(r.permissions), FALSE) DESC, u.id
		LIMIT 1`, SitePermAdmin).Scan(&creator)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}

	name := "Company"
	var siteName string
	if err := pool.QueryRow(ctx, `SELECT COALESCE(site_name, '') FROM site_settings WHERE id = 1`).Scan(&siteName); err == nil {
		if n := strings.TrimSpace(siteName); n != "" && len(n) <= MaxOrganizationNameLen {
			name = n
		}
	}

	var id int
	err = pool.QueryRow(ctx,
		`INSERT INTO organizations (name, description, created_by, is_company)
		 VALUES ($1, 'Everyone on this site.', $2, TRUE) RETURNING id`, name, creator).Scan(&id)
	if err != nil {
		if isUniqueViolation(err) {
			return CompanyOrganizationID() // created concurrently
		}
		return 0, fmt.Errorf("create company organization: %w", err)
	}
	if _, err := CreateOrganizationRoleDef(id, CompanyMemberRole, "Member", "Everyone in the company",
		append([]string(nil), writeTaskPerms...), 0); err != nil {
		return 0, fmt.Errorf("create company member role: %w", err)
	}
	return id, nil
}

// SyncCompanyMembers adds every non-agent account to the company. It returns how many joined.
func SyncCompanyMembers() (int, error) {
	pool, err := OpenDatabase()
	if err != nil {
		return 0, err
	}
	defer CloseDatabase(pool)

	tag, err := pool.Exec(context.Background(), `
		INSERT INTO organization_members (organization_id, user_id, role)
		SELECT o.id, u.id, $1 FROM organizations o
		CROSS JOIN users u
		WHERE o.is_company AND NOT COALESCE(u.is_agent, FALSE)
		ON CONFLICT DO NOTHING`, CompanyMemberRole)
	if err != nil {
		return 0, err
	}
	if tag.RowsAffected() > 0 {
		if id, err := CompanyOrganizationID(); err == nil && id > 0 {
			if err := RecomputeCompanyProjectsForTeam(id); err != nil {
				return int(tag.RowsAffected()), err
			}
		}
	}
	return int(tag.RowsAffected()), nil
}

// JoinCompanyIfEnabled adds a newly created account to the company in company mode,
// creating the company first if this is the site's first account.
func JoinCompanyIfEnabled(userID int) error {
	mode, err := GetSiteMode()
	if err != nil || mode != SiteModeCompany {
		return err
	}
	orgID, err := EnsureCompanyOrganization()
	if err != nil || orgID == 0 {
		return err
	}
	pool, err := OpenDatabase()
	if err != nil {
		return err
	}
	defer CloseDatabase(pool)

	_, err = pool.Exec(context.Background(), `
		INSERT INTO organization_members (organization_id, user_id, role)
		SELECT $1, u.id, $3 FROM users u WHERE u.id = $2 AND NOT COALESCE(u.is_agent, FALSE)
		ON CONFLICT DO NOTHING`, orgID, userID, CompanyMemberRole)
	if err != nil {
		return err
	}
	// Projects shared with the whole company now include them.
	return RecomputeCompanyProjectsForTeam(orgID)
}

// deleteCompanyOrganization removes the company. Only valid while the mode is unlocked,
// when no project can belong to it.
func deleteCompanyOrganization() error {
	pool, err := OpenDatabase()
	if err != nil {
		return err
	}
	defer CloseDatabase(pool)

	_, err = pool.Exec(context.Background(),
		`DELETE FROM organizations WHERE is_company AND NOT EXISTS (SELECT 1 FROM projects)`)
	return err
}

// ChangeSiteMode sets the mode (see SetSiteMode for the lock) and applies its effects:
// entering company mode creates the company, enrolls everyone, and stops regular users
// from creating teams; leaving it removes the still-empty company.
func ChangeSiteMode(mode string) error {
	before, err := GetSiteMode()
	if err != nil {
		return err
	}
	if err := SetSiteMode(mode); err != nil {
		return err
	}
	after := NormalizeSiteMode(mode)
	switch {
	case after == SiteModeCompany:
		if before != SiteModeCompany {
			if err := seedCompanySiteRoles(); err != nil {
				return err
			}
		}
		if err := EnsureCompanyReady(); err != nil {
			return err
		}
		return RecomputeAllCompanyProjects()
	case before == SiteModeCompany:
		return deleteCompanyOrganization()
	}
	return nil
}

// companyMemberPerms is what the default User role can do on the projects shared
// with them, once the site is a company.
var companyMemberPerms = []string{
	PermTasksCreate, PermTasksEdit, PermTasksComplete, PermTasksClaim,
	PermTasksReorder, PermTasksStatus, PermTasksSprint, PermTimeWrite,
}

// seedCompanySiteRoles prepares the site roles for company mode, where they are the
// only roles: nobody creates organizations, the User role gets everyday task
// permissions, and the default company roles are added unless a role already has the
// name. Admins can rename them or change their permissions afterwards.
func seedCompanySiteRoles() error {
	pool, err := OpenDatabase()
	if err != nil {
		return err
	}
	defer CloseDatabase(pool)

	ctx := context.Background()
	if _, err := pool.Exec(ctx, `UPDATE roles SET permissions = array_remove(permissions, $1::text)`, SitePermOrgsCreate); err != nil {
		return err
	}
	for _, perm := range companyMemberPerms {
		if err := SetRolePermission(MemberRoleName, perm, true); err != nil {
			return err
		}
	}
	manager := append([]string{
		SitePermProjectsCreate, SitePermTeamPeople,
		PermTasksDelete, PermTasksArchive, PermTasksRestore, PermProjectManage, PermProjectTags,
		PermCommentsModerate, PermInsightsPeople,
	}, companyMemberPerms...)
	defaults := []struct {
		name  string
		perms []string
	}{
		// Runs projects it's given on a share; no site-wide powers.
		{"Project Administrator", AllProjectPerms()},
		// Does the work on the projects shared with them.
		{"Contributor", companyMemberPerms},
		// Follows along without changing anything.
		{"Reader", []string{}},
		// Leads a team: hires onto it and runs its projects.
		{"Manager", manager},
	}
	for _, d := range defaults {
		if _, err := CreateSiteRole(d.name, d.perms); err != nil && !errors.Is(err, ErrSiteRoleNameTaken) {
			return err
		}
	}
	InvalidateCompanyRoleCache()
	return nil
}

// EnsureCompanyReady creates the company if needed and enrolls any account that is
// missing. Safe to call on every startup; does nothing outside company mode.
func EnsureCompanyReady() error {
	mode, err := GetSiteMode()
	if err != nil || mode != SiteModeCompany {
		return err
	}
	if _, err := EnsureCompanyOrganization(); err != nil {
		return err
	}
	_, err = SyncCompanyMembers()
	return err
}
