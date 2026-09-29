package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"GoTodo/internal/mailer"

	"github.com/jackc/pgx/v5"
)

const (
	MaxOrganizationNameLen     = 80
	MaxOrganizationDescLen     = 1000
	MaxOrganizationsPerUser    = 50
	MaxOrganizationCustomRoles = 20
)

// Organization is a team whose members and roles can be copied onto projects.
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

// OrganizationInvite is a pending invite to join an organization.
type OrganizationInvite struct {
	ID               int
	OrganizationID   int
	Email            string
	Role             string
	Token            string
	InvitedBy        int
	ExpiresAt        time.Time
	AcceptedAt       *time.Time
	CreatedAt        time.Time
	UserName         string
	OrganizationName string
	InviterEmail     string
	InviterUserName  string
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
		`CREATE TABLE IF NOT EXISTS organization_invites (
			id SERIAL PRIMARY KEY,
			organization_id INTEGER NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
			email VARCHAR(255) NOT NULL,
			role VARCHAR(40) NOT NULL,
			token VARCHAR(64) NOT NULL UNIQUE,
			invited_by INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			expires_at TIMESTAMPTZ NOT NULL,
			accepted_at TIMESTAMPTZ,
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)`,
		`CREATE INDEX IF NOT EXISTS idx_organization_invites_email ON organization_invites(email) WHERE accepted_at IS NULL`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_organization_invites_pending
			ON organization_invites (organization_id, LOWER(email)) WHERE accepted_at IS NULL`,
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

// MigrateProjectsAddOrgImport stores how a project imported its organization roster.
func MigrateProjectsAddOrgImport() error {
	pool, err := OpenDatabase()
	if err != nil {
		return err
	}
	defer CloseDatabase(pool)

	stmts := []string{
		`ALTER TABLE projects ADD COLUMN IF NOT EXISTS org_import VARCHAR(16)`,
		`UPDATE projects SET org_import = 'lock'
		  WHERE organization_id IS NOT NULL AND COALESCE(org_managed, false) AND COALESCE(org_import, '') = ''`,
		`UPDATE projects SET org_import = 'copy'
		  WHERE organization_id IS NOT NULL AND NOT COALESCE(org_managed, false) AND COALESCE(org_import, '') = ''`,
		`UPDATE projects SET org_import = NULL WHERE organization_id IS NULL AND org_import IS NOT NULL`,
	}
	for _, s := range stmts {
		if _, err := pool.Exec(context.Background(), s); err != nil {
			return fmt.Errorf("failed to add projects.org_import: %v", err)
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
		(SELECT COUNT(*) FROM projects p WHERE p.organization_id = o.id)
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
		`UPDATE projects SET organization_id = NULL, org_managed = FALSE, org_import = NULL, updated_at = CURRENT_TIMESTAMP
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

// ProjectOrgBinding is the organization linkage for a project (imported-from badge).
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

const (
	OrgImportCopy   = "copy"
	OrgImportLock   = "lock"
	OrgImportSelect = "select"
)

// OrgImportMember is one person copied onto a project during a selective import.
type OrgImportMember struct {
	UserID int
	Role   string
}

// OrgImportSpec describes how a project should import an organization roster.
type OrgImportSpec struct {
	OrganizationID int
	Lock           bool
	AllMembers     bool
	Mode           string
	Members        []OrgImportMember
}

func (s OrgImportSpec) storedImportMode() string {
	mode := strings.TrimSpace(strings.ToLower(s.Mode))
	switch mode {
	case OrgImportCopy, OrgImportLock, OrgImportSelect:
		return mode
	}
	if s.Lock {
		return OrgImportLock
	}
	if s.AllMembers {
		return OrgImportCopy
	}
	return OrgImportSelect
}

func orgAutoImportSQL() string {
	return `COALESCE(NULLIF(p.org_import, ''), CASE WHEN COALESCE(p.org_managed, false) THEN 'lock' ELSE 'copy' END) IN ('copy', 'lock')`
}

func normalizeImportRole(role string) string {
	role = strings.TrimSpace(strings.ToLower(role))
	if role == RoleOwner {
		return RoleEditor
	}
	return role
}

const importOrganizationMembersSQL = `
		INSERT INTO project_members (project_id, user_id, role)
		SELECT $1, om.user_id,
			CASE WHEN om.role = $4 THEN $5 ELSE om.role END
		FROM organization_members om
		WHERE om.organization_id = $2
		  AND om.user_id <> $3
		ON CONFLICT (project_id, user_id) DO UPDATE SET role = EXCLUDED.role`

func importOrganizationMembersTx(tx pgx.Tx, projectID, ownerUserID, orgID int) error {
	_, err := tx.Exec(context.Background(), importOrganizationMembersSQL,
		projectID, orgID, ownerUserID, RoleOwner, RoleEditor)
	if err != nil {
		return fmt.Errorf("failed to import organization members: %v", err)
	}
	return nil
}

func importSelectedOrganizationMembersTx(tx pgx.Tx, projectID, ownerUserID, orgID int, members []OrgImportMember) error {
	for _, m := range members {
		if m.UserID <= 0 || m.UserID == ownerUserID {
			continue
		}
		role := normalizeImportRole(m.Role)
		if role == "" {
			return fmt.Errorf("role is required for each imported member")
		}
		var exists int
		err := tx.QueryRow(context.Background(),
			`SELECT 1 FROM organization_members WHERE organization_id = $1 AND user_id = $2`,
			orgID, m.UserID).Scan(&exists)
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) || errors.Is(err, sql.ErrNoRows) {
				return fmt.Errorf("user %d is not a member of the organization", m.UserID)
			}
			return err
		}
		if _, err := tx.Exec(context.Background(), `
			INSERT INTO project_members (project_id, user_id, role) VALUES ($1, $2, $3)
			ON CONFLICT (project_id, user_id) DO UPDATE SET role = EXCLUDED.role`,
			projectID, m.UserID, role); err != nil {
			return fmt.Errorf("failed to import organization member: %v", err)
		}
	}
	return nil
}

func applyOrgImportTx(tx pgx.Tx, projectID, ownerUserID int, spec OrgImportSpec) error {
	if spec.AllMembers {
		return importOrganizationMembersTx(tx, projectID, ownerUserID, spec.OrganizationID)
	}
	return importSelectedOrganizationMembersTx(tx, projectID, ownerUserID, spec.OrganizationID, spec.Members)
}

// ImportOrganizationMembersToProject copies current org members onto project_members.
func ImportOrganizationMembersToProject(projectID, ownerUserID, orgID int) error {
	if projectID <= 0 || ownerUserID <= 0 || orgID <= 0 {
		return fmt.Errorf("invalid project or organization")
	}
	pool, err := OpenDatabase()
	if err != nil {
		return err
	}
	defer CloseDatabase(pool)
	_, err = pool.Exec(context.Background(), importOrganizationMembersSQL,
		projectID, orgID, ownerUserID, RoleOwner, RoleEditor)
	if err != nil {
		return fmt.Errorf("failed to import organization members: %v", err)
	}
	return nil
}

// ProjectIsOrgManaged reports whether membership/roles are locked to the organization.
func ProjectIsOrgManaged(projectID int) bool {
	b, err := GetProjectOrgBinding(projectID)
	return err == nil && b != nil && b.OrgManaged && b.OrganizationID != nil
}

// OrgMemberProjectRef is an org-linked project the member currently belongs to.
type OrgMemberProjectRef struct {
	ID     int
	Name   string
	Role   string
	Locked bool
}

// OrgProjectRoster is an org-linked project plus its current project_members.
type OrgProjectRoster struct {
	ID         int
	Name       string
	OrgManaged bool
	OrgImport  string
	CanManage  bool
	Members    []ProjectMember
}

// ListOrganizationProjectRosters returns projects attached to the organization
// and the people currently on each board.
func ListOrganizationProjectRosters(orgID int) ([]OrgProjectRoster, error) {
	if orgID <= 0 {
		return nil, fmt.Errorf("invalid organization")
	}
	pool, err := OpenDatabase()
	if err != nil {
		return nil, err
	}
	defer CloseDatabase(pool)

	rows, err := pool.Query(context.Background(), `
		SELECT p.id, p.name, COALESCE(p.org_managed, false),
		       COALESCE(NULLIF(p.org_import, ''), CASE WHEN COALESCE(p.org_managed, false) THEN 'lock' ELSE 'copy' END),
		       pm.user_id, COALESCE(u.email, ''), COALESCE(u.user_name, ''),
		       COALESCE(pm.role, ''), pm.created_at
		FROM projects p
		LEFT JOIN project_members pm ON pm.project_id = p.id
		LEFT JOIN users u ON u.id = pm.user_id
		WHERE p.organization_id = $1
		ORDER BY LOWER(p.name) ASC, p.id ASC,
		         CASE pm.role WHEN 'owner' THEN 0 ELSE 1 END, u.email`, orgID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	byID := map[int]*OrgProjectRoster{}
	var order []int
	for rows.Next() {
		var (
			id         int
			name       string
			orgManaged bool
			orgImport  string
			userID     *int
			email      string
			userName   string
			role       string
			createdAt  *time.Time
		)
		if err := rows.Scan(&id, &name, &orgManaged, &orgImport, &userID, &email, &userName, &role, &createdAt); err != nil {
			return nil, err
		}
		r, ok := byID[id]
		if !ok {
			r = &OrgProjectRoster{ID: id, Name: name, OrgManaged: orgManaged, OrgImport: orgImport, Members: []ProjectMember{}}
			byID[id] = r
			order = append(order, id)
		}
		if userID == nil || *userID <= 0 {
			continue
		}
		m := ProjectMember{UserID: *userID, Email: email, UserName: userName, Role: role, Inherited: orgManaged && role != RoleOwner}
		if createdAt != nil {
			m.CreatedAt = *createdAt
		}
		r.Members = append(r.Members, m)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	out := make([]OrgProjectRoster, 0, len(order))
	for _, id := range order {
		out = append(out, *byID[id])
	}
	return out, nil
}

// ListOrgProjectsForMember returns org-linked projects the user is a member of.
func ListOrgProjectsForMember(orgID, userID int) ([]OrgMemberProjectRef, error) {
	pool, err := OpenDatabase()
	if err != nil {
		return nil, err
	}
	defer CloseDatabase(pool)

	rows, err := pool.Query(context.Background(), `
		SELECT p.id, p.name, COALESCE(p.org_managed, false),
		       COALESCE(pm.role, CASE WHEN p.user_id = $2 THEN 'owner' END)
		FROM projects p
		LEFT JOIN project_members pm ON pm.project_id = p.id AND pm.user_id = $2
		WHERE p.organization_id = $1
		  AND (pm.user_id IS NOT NULL OR p.user_id = $2)
		ORDER BY LOWER(p.name) ASC, p.id ASC`, orgID, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []OrgMemberProjectRef
	for rows.Next() {
		var r OrgMemberProjectRef
		if err := rows.Scan(&r.ID, &r.Name, &r.Locked, &r.Role); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// UpdateLockedOrgProjectMemberRole overwrites the member's role on imported-and-locked
// projects for this organization. The project owner row is never changed.
func UpdateLockedOrgProjectMemberRole(orgID, userID int, role string) ([]int, error) {
	role = normalizeImportRole(role)
	if orgID <= 0 || userID <= 0 || role == "" {
		return nil, fmt.Errorf("invalid organization member role update")
	}
	pool, err := OpenDatabase()
	if err != nil {
		return nil, err
	}
	defer CloseDatabase(pool)

	rows, err := pool.Query(context.Background(), `
		UPDATE project_members pm
		SET role = $3
		FROM projects p
		WHERE pm.project_id = p.id
		  AND p.organization_id = $1
		  AND COALESCE(p.org_managed, false)
		  AND pm.user_id = $2
		  AND pm.role <> 'owner'
		RETURNING p.id`, orgID, userID, role)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []int
	for rows.Next() {
		var id int
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// AttachProjectOrganization binds a project to an organization, drops non-owner
// project_members rows, imports members according to spec, and cancels pending
// invites. Returns user IDs that were removed from project_members.
func AttachProjectOrganization(projectID, ownerUserID int, spec OrgImportSpec) ([]int, error) {
	if projectID <= 0 || ownerUserID <= 0 || spec.OrganizationID <= 0 {
		return nil, fmt.Errorf("invalid project or organization")
	}
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

	var owner int
	err = tx.QueryRow(context.Background(),
		`SELECT user_id FROM projects WHERE id = $1`, projectID).Scan(&owner)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) || errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("project not found")
		}
		return nil, err
	}
	if owner != ownerUserID {
		return nil, fmt.Errorf("project not found")
	}

	rows, err := tx.Query(context.Background(),
		`DELETE FROM project_members
		 WHERE project_id = $1 AND role <> 'owner'
		 RETURNING user_id`, projectID)
	if err != nil {
		return nil, fmt.Errorf("failed to remove project members: %v", err)
	}
	var removed []int
	for rows.Next() {
		var uid int
		if err := rows.Scan(&uid); err != nil {
			rows.Close()
			return nil, err
		}
		removed = append(removed, uid)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}

	if _, err := tx.Exec(context.Background(),
		`DELETE FROM project_invites WHERE project_id = $1 AND accepted_at IS NULL`, projectID); err != nil {
		return nil, fmt.Errorf("failed to cancel project invites: %v", err)
	}

	tag, err := tx.Exec(context.Background(),
		`UPDATE projects SET organization_id = $1, org_managed = $2, org_import = $3, updated_at = CURRENT_TIMESTAMP
		 WHERE id = $4 AND user_id = $5`,
		spec.OrganizationID, spec.Lock, spec.storedImportMode(), projectID, ownerUserID)
	if err != nil {
		return nil, fmt.Errorf("failed to attach organization: %v", err)
	}
	if tag.RowsAffected() == 0 {
		return nil, fmt.Errorf("project not found")
	}
	if err := applyOrgImportTx(tx, projectID, ownerUserID, spec); err != nil {
		return nil, err
	}
	if err := tx.Commit(context.Background()); err != nil {
		return nil, err
	}
	return removed, nil
}

const organizationInviteCols = `id, organization_id, email, role, token, invited_by, expires_at, accepted_at, created_at`
const organizationInviteSelect = `i.id, i.organization_id, i.email, i.role, i.token, i.invited_by, i.expires_at, i.accepted_at, i.created_at`

func scanOrganizationInvite(row interface{ Scan(dest ...any) error }, inv *OrganizationInvite, extra ...any) error {
	dest := []any{
		&inv.ID, &inv.OrganizationID, &inv.Email, &inv.Role, &inv.Token, &inv.InvitedBy,
		&inv.ExpiresAt, &inv.AcceptedAt, &inv.CreatedAt,
	}
	dest = append(dest, extra...)
	return row.Scan(dest...)
}

// PendingOrganizationInviteExists reports whether email already has a pending invite.
func PendingOrganizationInviteExists(orgID int, email string) (bool, error) {
	email = strings.TrimSpace(strings.ToLower(email))
	pool, err := OpenDatabase()
	if err != nil {
		return false, err
	}
	defer CloseDatabase(pool)
	var n int
	err = pool.QueryRow(context.Background(),
		`SELECT COUNT(*) FROM organization_invites
		 WHERE organization_id = $1 AND LOWER(email) = $2 AND accepted_at IS NULL AND expires_at > NOW()`,
		orgID, email).Scan(&n)
	return n > 0, err
}

// CreateOrganizationInvite creates a pending invite.
func CreateOrganizationInvite(orgID int, email, role string, invitedBy int, expiresAt time.Time) (*OrganizationInvite, error) {
	email = strings.TrimSpace(strings.ToLower(email))
	token, err := newShareToken()
	if err != nil {
		return nil, err
	}
	pool, err := OpenDatabase()
	if err != nil {
		return nil, err
	}
	defer CloseDatabase(pool)

	var inv OrganizationInvite
	err = pool.QueryRow(context.Background(), `
		WITH ins AS (
			INSERT INTO organization_invites (organization_id, email, role, token, invited_by, expires_at)
			VALUES ($1, $2, $3, $4, $5, $6)
			RETURNING `+organizationInviteCols+`
		)
		SELECT ins.id, ins.organization_id, ins.email, ins.role, ins.token, ins.invited_by,
		       ins.expires_at, ins.accepted_at, ins.created_at,
		       COALESCE(o.name, ''), COALESCE(u.user_name, '')
		FROM ins
		LEFT JOIN organizations o ON o.id = ins.organization_id
		LEFT JOIN users u ON u.id = ins.invited_by`,
		orgID, email, role, token, invitedBy, expiresAt).Scan(
		&inv.ID, &inv.OrganizationID, &inv.Email, &inv.Role, &inv.Token, &inv.InvitedBy,
		&inv.ExpiresAt, &inv.AcceptedAt, &inv.CreatedAt, &inv.OrganizationName, &inv.InviterUserName)
	if err != nil {
		return nil, err
	}
	if settings, err := GetSiteSettings(); err == nil && settings != nil {
		subject := "Organization Invite"
		body := fmt.Sprintf("You have been invited to join organization %s by %s.", inv.OrganizationName, inv.InviterUserName)
		if err := mailer.SendEmail(settings.Email, mailer.TriggerOrganizationInvite, subject, body, inv.Email); err != nil {
			fmt.Printf("Warning: Failed to send organization invite email to %s: %v\n", inv.Email, err)
		}
	}
	return &inv, nil
}

// ListOrganizationInvites returns pending invites for an organization.
func ListOrganizationInvites(orgID int) ([]OrganizationInvite, error) {
	pool, err := OpenDatabase()
	if err != nil {
		return nil, err
	}
	defer CloseDatabase(pool)

	rows, err := pool.Query(context.Background(), `
		SELECT `+organizationInviteSelect+`, COALESCE(u.user_name, '')
		FROM organization_invites i
		LEFT JOIN users u ON LOWER(u.email) = LOWER(i.email)
		WHERE i.organization_id = $1 AND i.accepted_at IS NULL
		ORDER BY i.created_at DESC`, orgID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []OrganizationInvite
	for rows.Next() {
		var inv OrganizationInvite
		if err := scanOrganizationInvite(rows, &inv, &inv.UserName); err != nil {
			return nil, err
		}
		out = append(out, inv)
	}
	return out, nil
}

// DeleteOrganizationInvite removes a pending invite.
func DeleteOrganizationInvite(inviteID, orgID int) error {
	pool, err := OpenDatabase()
	if err != nil {
		return err
	}
	defer CloseDatabase(pool)

	tag, err := pool.Exec(context.Background(),
		`DELETE FROM organization_invites WHERE id = $1 AND organization_id = $2 AND accepted_at IS NULL`,
		inviteID, orgID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("invite not found")
	}
	return nil
}

// ListPendingOrganizationInvitesForEmail returns pending org invites for an email.
func ListPendingOrganizationInvitesForEmail(email string) ([]OrganizationInvite, error) {
	email = strings.TrimSpace(strings.ToLower(email))
	pool, err := OpenDatabase()
	if err != nil {
		return nil, err
	}
	defer CloseDatabase(pool)

	rows, err := pool.Query(context.Background(), `
		SELECT `+organizationInviteSelect+`,
		       COALESCE(invitee.user_name, ''), o.name, COALESCE(u.email, ''), COALESCE(u.user_name, '')
		FROM organization_invites i
		JOIN organizations o ON o.id = i.organization_id
		LEFT JOIN users u ON u.id = i.invited_by
		LEFT JOIN users invitee ON LOWER(invitee.email) = LOWER(i.email)
		WHERE i.email = $1 AND i.accepted_at IS NULL AND i.expires_at > NOW()
		ORDER BY i.created_at DESC`, email)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []OrganizationInvite
	for rows.Next() {
		var inv OrganizationInvite
		if err := scanOrganizationInvite(rows, &inv, &inv.UserName, &inv.OrganizationName, &inv.InviterEmail, &inv.InviterUserName); err != nil {
			return nil, err
		}
		out = append(out, inv)
	}
	return out, nil
}

// GetOrganizationInviteByID loads an invite by id.
func GetOrganizationInviteByID(inviteID int) (*OrganizationInvite, error) {
	pool, err := OpenDatabase()
	if err != nil {
		return nil, err
	}
	defer CloseDatabase(pool)

	var inv OrganizationInvite
	err = scanOrganizationInvite(pool.QueryRow(context.Background(), `
		SELECT `+organizationInviteSelect+`,
		       COALESCE(o.name, ''), COALESCE(u.email, ''), COALESCE(u.user_name, '')
		FROM organization_invites i
		LEFT JOIN organizations o ON o.id = i.organization_id
		LEFT JOIN users u ON u.id = i.invited_by
		WHERE i.id = $1`, inviteID), &inv, &inv.OrganizationName, &inv.InviterEmail, &inv.InviterUserName)
	if err != nil {
		return nil, err
	}
	return &inv, nil
}

// AcceptOrganizationInvite marks invite accepted, adds membership, and copies the
// new member onto organization projects that imported everyone (copy/lock).
func AcceptOrganizationInvite(inviteID, userID int, userEmail string) ([]int, error) {
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

	var inv OrganizationInvite
	err = tx.QueryRow(context.Background(), `
		SELECT id, organization_id, email, role, expires_at, accepted_at
		FROM organization_invites WHERE id = $1 FOR UPDATE`, inviteID).Scan(
		&inv.ID, &inv.OrganizationID, &inv.Email, &inv.Role, &inv.ExpiresAt, &inv.AcceptedAt)
	if err != nil {
		return nil, err
	}
	if inv.AcceptedAt != nil {
		return nil, fmt.Errorf("invite already accepted")
	}
	if time.Now().After(inv.ExpiresAt) {
		return nil, fmt.Errorf("invite expired")
	}
	if !strings.EqualFold(inv.Email, strings.TrimSpace(userEmail)) {
		return nil, fmt.Errorf("invite email mismatch")
	}

	_, err = tx.Exec(context.Background(), `
		INSERT INTO organization_members (organization_id, user_id, role) VALUES ($1, $2, $3)
		ON CONFLICT (organization_id, user_id) DO UPDATE SET role = EXCLUDED.role
		WHERE organization_members.role <> 'owner'`,
		inv.OrganizationID, userID, inv.Role)
	if err != nil {
		return nil, err
	}
	_, err = tx.Exec(context.Background(),
		`UPDATE organization_invites SET accepted_at = NOW() WHERE id = $1`, inviteID)
	if err != nil {
		return nil, err
	}
	projectIDs, err := addUserToAutoImportOrgProjectsTx(tx, inv.OrganizationID, userID, inv.Role)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(context.Background()); err != nil {
		return nil, err
	}
	return projectIDs, nil
}

func addUserToAutoImportOrgProjectsTx(tx pgx.Tx, orgID, userID int, role string) ([]int, error) {
	role = normalizeImportRole(role)
	if orgID <= 0 || userID <= 0 || role == "" {
		return nil, nil
	}
	rows, err := tx.Query(context.Background(), `
		INSERT INTO project_members (project_id, user_id, role)
		SELECT p.id, $2, $3
		FROM projects p
		WHERE p.organization_id = $1
		  AND p.user_id <> $2
		  AND `+orgAutoImportSQL()+`
		ON CONFLICT (project_id, user_id) DO NOTHING
		RETURNING project_id`, orgID, userID, role)
	if err != nil {
		return nil, fmt.Errorf("failed to add member to organization projects: %v", err)
	}
	defer rows.Close()
	var ids []int
	for rows.Next() {
		var id int
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// OrgProjectSyncResult reports which auto-import projects received missing members.
type OrgProjectSyncResult struct {
	ProjectIDs []int
	Added      int
}

// SyncOrganizationMembersToAutoImportProjects adds current org members to copy/lock
// projects. Select-mode projects are left unchanged. Existing copy-project roles are
// kept; locked project roles are aligned with the organization.
func SyncOrganizationMembersToAutoImportProjects(orgID int) (*OrgProjectSyncResult, error) {
	if orgID <= 0 {
		return nil, fmt.Errorf("invalid organization")
	}
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

	rows, err := tx.Query(context.Background(), `
		INSERT INTO project_members (project_id, user_id, role)
		SELECT p.id, om.user_id,
			CASE WHEN om.role = $2 THEN $3 ELSE om.role END
		FROM projects p
		JOIN organization_members om ON om.organization_id = p.organization_id
		WHERE p.organization_id = $1
		  AND om.user_id <> p.user_id
		  AND `+orgAutoImportSQL()+`
		ON CONFLICT (project_id, user_id) DO NOTHING
		RETURNING project_id`, orgID, RoleOwner, RoleEditor)
	if err != nil {
		return nil, fmt.Errorf("failed to sync organization members: %v", err)
	}
	seen := map[int]bool{}
	var ids []int
	added := 0
	for rows.Next() {
		var id int
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return nil, err
		}
		added++
		if !seen[id] {
			seen[id] = true
			ids = append(ids, id)
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}

	lockRows, err := tx.Query(context.Background(), `
		UPDATE project_members pm
		SET role = CASE WHEN om.role = $2 THEN $3 ELSE om.role END
		FROM projects p
		JOIN organization_members om ON om.organization_id = p.organization_id AND om.user_id = pm.user_id
		WHERE pm.project_id = p.id
		  AND p.organization_id = $1
		  AND COALESCE(p.org_import, CASE WHEN COALESCE(p.org_managed, false) THEN 'lock' ELSE 'copy' END) = 'lock'
		  AND pm.role <> 'owner'
		  AND pm.role <> CASE WHEN om.role = $2 THEN $3 ELSE om.role END
		RETURNING p.id`, orgID, RoleOwner, RoleEditor)
	if err != nil {
		return nil, fmt.Errorf("failed to sync locked project roles: %v", err)
	}
	for lockRows.Next() {
		var id int
		if err := lockRows.Scan(&id); err != nil {
			lockRows.Close()
			return nil, err
		}
		if !seen[id] {
			seen[id] = true
			ids = append(ids, id)
		}
	}
	err = lockRows.Err()
	lockRows.Close()
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(context.Background()); err != nil {
		return nil, err
	}
	if ids == nil {
		ids = []int{}
	}
	return &OrgProjectSyncResult{ProjectIDs: ids, Added: added}, nil
}

// DeclineOrganizationInvite deletes a pending invite for the user's email.
func DeclineOrganizationInvite(inviteID int, userEmail string) error {
	pool, err := OpenDatabase()
	if err != nil {
		return err
	}
	defer CloseDatabase(pool)

	tag, err := pool.Exec(context.Background(), `
		DELETE FROM organization_invites
		WHERE id = $1 AND accepted_at IS NULL AND LOWER(email) = LOWER($2)`,
		inviteID, strings.TrimSpace(userEmail))
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("invite not found")
	}
	return nil
}

// CountOrganizationInvitesWithRole counts pending org invites using slug.
func CountOrganizationInvitesWithRole(slug string, orgID int) (int, error) {
	pool, err := OpenDatabase()
	if err != nil {
		return 0, err
	}
	defer CloseDatabase(pool)
	var n int
	if orgID > 0 {
		err = pool.QueryRow(context.Background(),
			`SELECT COUNT(*) FROM organization_invites WHERE role = $1 AND organization_id = $2 AND accepted_at IS NULL`,
			slug, orgID).Scan(&n)
	} else {
		err = pool.QueryRow(context.Background(),
			`SELECT COUNT(*) FROM organization_invites WHERE role = $1 AND accepted_at IS NULL`, slug).Scan(&n)
	}
	return n, err
}
