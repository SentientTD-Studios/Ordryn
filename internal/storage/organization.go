package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

const (
	MaxOrganizationNameLen     = 80
	MaxOrganizationDescLen     = 1000
	MaxOrganizationsPerUser    = 50
	MaxOrganizationCustomRoles = 20
)

// Organization is a team whose members and roles can be inherited by projects.
type Organization struct {
	ID           int
	Name         string
	Description  string
	CreatedBy    int
	CreatedAt    time.Time
	UpdatedAt    time.Time
	Role         string
	MemberCount  int
	ProjectCount int
}

// OrganizationMember is a user assigned a role on an organization.
type OrganizationMember struct {
	UserID    int
	Email     string
	UserName  string
	Role      string
	CreatedAt time.Time
}

// CreateOrganizationTables creates organizations and membership tables.
func CreateOrganizationTables() error {
	pool, err := OpenDatabase()
	if err != nil {
		return err
	}
	defer CloseDatabase(pool)

	stmts := []string{
		`CREATE TABLE IF NOT EXISTS organizations (
			id SERIAL PRIMARY KEY,
			name VARCHAR(80) NOT NULL,
			description TEXT NOT NULL DEFAULT '',
			created_by INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)`,
		`CREATE INDEX IF NOT EXISTS idx_organizations_created_by ON organizations (created_by)`,
		`CREATE TABLE IF NOT EXISTS organization_members (
			organization_id INTEGER NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
			user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			role VARCHAR(40) NOT NULL,
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			PRIMARY KEY (organization_id, user_id)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_organization_members_user_id ON organization_members (user_id)`,
	}
	for _, s := range stmts {
		if _, err := pool.Exec(context.Background(), s); err != nil {
			return fmt.Errorf("failed to create organization tables: %v", err)
		}
	}
	return nil
}

// MigrateProjectsAddOrganization adds organization_id and org_managed to projects.
func MigrateProjectsAddOrganization() error {
	pool, err := OpenDatabase()
	if err != nil {
		return err
	}
	defer CloseDatabase(pool)

	stmts := []string{
		`ALTER TABLE projects ADD COLUMN IF NOT EXISTS organization_id INTEGER REFERENCES organizations(id) ON DELETE SET NULL`,
		`ALTER TABLE projects ADD COLUMN IF NOT EXISTS org_managed BOOLEAN NOT NULL DEFAULT FALSE`,
		`CREATE INDEX IF NOT EXISTS idx_projects_organization_id ON projects (organization_id)`,
		`UPDATE projects SET org_managed = FALSE WHERE organization_id IS NULL AND org_managed = TRUE`,
	}
	for _, s := range stmts {
		if _, err := pool.Exec(context.Background(), s); err != nil {
			return fmt.Errorf("failed to add project organization columns: %v", err)
		}
	}
	return nil
}

func scanOrganization(row interface{ Scan(dest ...any) error }, o *Organization) error {
	return row.Scan(&o.ID, &o.Name, &o.Description, &o.CreatedBy, &o.CreatedAt, &o.UpdatedAt, &o.Role, &o.MemberCount, &o.ProjectCount)
}

const organizationSelect = `SELECT o.id, o.name, COALESCE(o.description, ''), o.created_by, o.created_at, o.updated_at,
		COALESCE(om.role, ''),
		(SELECT COUNT(*) FROM organization_members m WHERE m.organization_id = o.id),
		(SELECT COUNT(*) FROM projects p WHERE p.organization_id = o.id AND COALESCE(p.org_managed, false))
	 FROM organizations o`

// CreateOrganization inserts an organization and makes createdBy the owner.
func CreateOrganization(createdBy int, name, description string) (*Organization, error) {
	pool, err := OpenDatabase()
	if err != nil {
		return nil, err
	}
	defer CloseDatabase(pool)

	tx, err := pool.Begin(context.Background())
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(context.Background())

	var id int
	err = tx.QueryRow(context.Background(),
		`INSERT INTO organizations (name, description, created_by)
		 VALUES ($1, $2, $3)
		 RETURNING id`,
		name, description, createdBy).Scan(&id)
	if err != nil {
		return nil, fmt.Errorf("failed to create organization: %v", err)
	}
	if _, err := tx.Exec(context.Background(),
		`INSERT INTO organization_members (organization_id, user_id, role) VALUES ($1, $2, $3)`,
		id, createdBy, RoleOwner); err != nil {
		return nil, fmt.Errorf("failed to add organization owner: %v", err)
	}
	if err := tx.Commit(context.Background()); err != nil {
		return nil, err
	}
	return GetAccessibleOrganization(id, createdBy)
}

// CountOrganizationsCreatedBy returns how many orgs a user created.
func CountOrganizationsCreatedBy(userID int) (int, error) {
	pool, err := OpenDatabase()
	if err != nil {
		return 0, err
	}
	defer CloseDatabase(pool)
	var n int
	err = pool.QueryRow(context.Background(),
		`SELECT COUNT(*) FROM organizations WHERE created_by = $1`, userID).Scan(&n)
	return n, err
}

// ListOrganizationsForUser returns orgs the user belongs to.
func ListOrganizationsForUser(userID int) ([]Organization, error) {
	pool, err := OpenDatabase()
	if err != nil {
		return nil, err
	}
	defer CloseDatabase(pool)

	rows, err := pool.Query(context.Background(),
		organizationSelect+`
		 JOIN organization_members om ON om.organization_id = o.id AND om.user_id = $1
		 ORDER BY LOWER(o.name) ASC, o.id ASC`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []Organization
	for rows.Next() {
		var o Organization
		if err := scanOrganization(rows, &o); err != nil {
			return nil, err
		}
		out = append(out, o)
	}
	return out, rows.Err()
}

// GetAccessibleOrganization returns an org if the user is a member.
func GetAccessibleOrganization(orgID, userID int) (*Organization, error) {
	pool, err := OpenDatabase()
	if err != nil {
		return nil, err
	}
	defer CloseDatabase(pool)

	var o Organization
	err = scanOrganization(pool.QueryRow(context.Background(),
		organizationSelect+`
		 JOIN organization_members om ON om.organization_id = o.id AND om.user_id = $2
		 WHERE o.id = $1`, orgID, userID), &o)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) || errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("organization not found")
		}
		return nil, err
	}
	return &o, nil
}

// GetOrganizationByID loads an organization without membership checks.
func GetOrganizationByID(orgID int) (*Organization, error) {
	pool, err := OpenDatabase()
	if err != nil {
		return nil, err
	}
	defer CloseDatabase(pool)

	var o Organization
	err = scanOrganization(pool.QueryRow(context.Background(),
		organizationSelect+`
		 LEFT JOIN organization_members om ON om.organization_id = o.id AND om.user_id = o.created_by
		 WHERE o.id = $1`, orgID), &o)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) || errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return &o, nil
}

// UpdateOrganization patches name and/or description.
func UpdateOrganization(orgID int, name, description *string) (*Organization, error) {
	pool, err := OpenDatabase()
	if err != nil {
		return nil, err
	}
	defer CloseDatabase(pool)

	cur, err := GetOrganizationByID(orgID)
	if err != nil || cur == nil {
		return nil, fmt.Errorf("organization not found")
	}
	newName := cur.Name
	newDesc := cur.Description
	if name != nil {
		newName = strings.TrimSpace(*name)
	}
	if description != nil {
		newDesc = strings.TrimSpace(*description)
	}
	_, err = pool.Exec(context.Background(),
		`UPDATE organizations SET name = $2, description = $3, updated_at = NOW() WHERE id = $1`,
		orgID, newName, newDesc)
	if err != nil {
		return nil, err
	}
	return GetOrganizationByID(orgID)
}

// DeleteOrganization removes an org after detaching projects.
func DeleteOrganization(orgID int) error {
	pool, err := OpenDatabase()
	if err != nil {
		return err
	}
	defer CloseDatabase(pool)

	tx, err := pool.Begin(context.Background())
	if err != nil {
		return err
	}
	defer tx.Rollback(context.Background())

	if _, err := tx.Exec(context.Background(),
		`UPDATE projects SET organization_id = NULL, org_managed = FALSE, updated_at = CURRENT_TIMESTAMP
		 WHERE organization_id = $1`, orgID); err != nil {
		return err
	}
	tag, err := tx.Exec(context.Background(), `DELETE FROM organizations WHERE id = $1`, orgID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("organization not found")
	}
	return tx.Commit(context.Background())
}

// GetOrganizationRole returns the caller's org role, or empty if not a member.
func GetOrganizationRole(orgID, userID int) (string, error) {
	pool, err := OpenDatabase()
	if err != nil {
		return "", err
	}
	defer CloseDatabase(pool)

	var role string
	err = pool.QueryRow(context.Background(),
		`SELECT role FROM organization_members WHERE organization_id = $1 AND user_id = $2`,
		orgID, userID).Scan(&role)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) || errors.Is(err, sql.ErrNoRows) {
			return "", nil
		}
		return "", err
	}
	return role, nil
}

// ListOrganizationMembers returns members of an organization.
func ListOrganizationMembers(orgID int) ([]OrganizationMember, error) {
	pool, err := OpenDatabase()
	if err != nil {
		return nil, err
	}
	defer CloseDatabase(pool)

	rows, err := pool.Query(context.Background(), `
		SELECT om.user_id, u.email, COALESCE(u.user_name, ''), om.role, om.created_at
		FROM organization_members om
		JOIN users u ON u.id = om.user_id
		WHERE om.organization_id = $1
		ORDER BY CASE om.role WHEN 'owner' THEN 0 ELSE 1 END, u.email`,
		orgID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []OrganizationMember
	for rows.Next() {
		var m OrganizationMember
		if err := rows.Scan(&m.UserID, &m.Email, &m.UserName, &m.Role, &m.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// UpsertOrganizationMember sets or updates an org member role.
func UpsertOrganizationMember(orgID, userID int, role string) error {
	pool, err := OpenDatabase()
	if err != nil {
		return err
	}
	defer CloseDatabase(pool)

	_, err = pool.Exec(context.Background(), `
		INSERT INTO organization_members (organization_id, user_id, role) VALUES ($1, $2, $3)
		ON CONFLICT (organization_id, user_id) DO UPDATE SET role = EXCLUDED.role`,
		orgID, userID, role)
	return err
}

// RemoveOrganizationMember deletes a non-owner membership row.
func RemoveOrganizationMember(orgID, userID int) error {
	pool, err := OpenDatabase()
	if err != nil {
		return err
	}
	defer CloseDatabase(pool)

	tag, err := pool.Exec(context.Background(),
		`DELETE FROM organization_members WHERE organization_id = $1 AND user_id = $2 AND role <> 'owner'`,
		orgID, userID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("member not found or cannot remove owner")
	}
	return nil
}

// CountOrgMembersWithRole counts org memberships using slug, optionally limited to one org.
func CountOrgMembersWithRole(slug string, orgID int) (int, error) {
	pool, err := OpenDatabase()
	if err != nil {
		return 0, err
	}
	defer CloseDatabase(pool)
	var n int
	if orgID > 0 {
		err = pool.QueryRow(context.Background(),
			`SELECT COUNT(*) FROM organization_members WHERE role = $1 AND organization_id = $2`,
			slug, orgID).Scan(&n)
	} else {
		err = pool.QueryRow(context.Background(),
			`SELECT COUNT(*) FROM organization_members WHERE role = $1`, slug).Scan(&n)
	}
	return n, err
}

// ProjectOrgBinding is the org inheritance state for a project.
type ProjectOrgBinding struct {
	OrganizationID *int
	OrgManaged     bool
}

// GetProjectOrgBinding returns organization linkage for a project.
func GetProjectOrgBinding(projectID int) (*ProjectOrgBinding, error) {
	pool, err := OpenDatabase()
	if err != nil {
		return nil, err
	}
	defer CloseDatabase(pool)

	var orgID sql.NullInt64
	var managed bool
	err = pool.QueryRow(context.Background(),
		`SELECT organization_id, COALESCE(org_managed, false) FROM projects WHERE id = $1`,
		projectID).Scan(&orgID, &managed)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) || errors.Is(err, sql.ErrNoRows) {
			return &ProjectOrgBinding{}, nil
		}
		return nil, err
	}
	out := &ProjectOrgBinding{OrgManaged: managed}
	if orgID.Valid {
		id := int(orgID.Int64)
		out.OrganizationID = &id
	}
	if out.OrganizationID == nil {
		out.OrgManaged = false
	}
	return out, nil
}

// ProjectIsOrgManaged reports whether membership/roles are inherited from an org.
func ProjectIsOrgManaged(projectID int) bool {
	b, err := GetProjectOrgBinding(projectID)
	return err == nil && b != nil && b.OrgManaged && b.OrganizationID != nil
}
