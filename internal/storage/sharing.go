package storage

import (
	"GoTodo/internal/mailer"
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

const (
	RoleOwner  = "owner"
	RoleEditor = "editor"
	RoleViewer = "viewer"

	ShareScopeProject = "project"
)

// ProjectWithAccess is a project plus the caller's role and owner info.
type ProjectWithAccess struct {
	ID            int
	UserID        int
	Name          string
	Description   string
	WorkflowMode  string
	Position      int
	Archived      bool
	BacklogName   string
	Role          string
	OwnerEmail    string
	OwnerUserName string
	OwnerUserID   int
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

// ProjectMember is a member of a shared project.
type ProjectMember struct {
	UserID    int
	Email     string
	UserName  string
	Role      string
	CreatedAt time.Time
}

// ProjectInvite is a pending invite to join a project.
type ProjectInvite struct {
	ID         int
	ProjectID  int
	Email      string
	Role       string
	Token      string
	InvitedBy  int
	ExpiresAt  time.Time
	AcceptedAt *time.Time
	CreatedAt  time.Time
	// Optional join fields for listing.
	UserName        string
	ProjectName     string
	InviterEmail    string
	InviterUserName string
}

// ShareLink is a read-only public view token.
type ShareLink struct {
	ID        int
	Token     string
	CreatedBy int
	ScopeType string
	ScopeID   int
	ExpiresAt *time.Time
	RevokedAt *time.Time
	CreatedAt time.Time
}

// ProjectEvent is an audit entry for project settings and sharing actions.
type ProjectEvent struct {
	ID            int
	ProjectID     int
	ActorUserID   int
	EventType     string
	Metadata      map[string]interface{}
	CreatedAt     time.Time
	ActorEmail    string
	ActorUserName string
}

// CreateProjectSharingTables creates membership, invite, share-link, and event tables.
func CreateProjectSharingTables() error {
	pool, err := OpenDatabase()
	if err != nil {
		return err
	}
	defer CloseDatabase(pool)

	stmts := []string{
		`CREATE TABLE IF NOT EXISTS project_members (
			project_id INTEGER NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
			user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			role VARCHAR(16) NOT NULL,
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			PRIMARY KEY (project_id, user_id),
			CHECK (role IN ('owner', 'editor', 'viewer'))
		)`,
		`CREATE INDEX IF NOT EXISTS idx_project_members_user_id ON project_members(user_id)`,
		`CREATE TABLE IF NOT EXISTS project_invites (
			id SERIAL PRIMARY KEY,
			project_id INTEGER NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
			email VARCHAR(255) NOT NULL,
			role VARCHAR(16) NOT NULL,
			token VARCHAR(64) NOT NULL UNIQUE,
			invited_by INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			expires_at TIMESTAMPTZ NOT NULL,
			accepted_at TIMESTAMPTZ,
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			CHECK (role IN ('editor', 'viewer'))
		)`,
		`CREATE INDEX IF NOT EXISTS idx_project_invites_email ON project_invites(email) WHERE accepted_at IS NULL`,
		`CREATE TABLE IF NOT EXISTS share_links (
			id SERIAL PRIMARY KEY,
			token VARCHAR(64) NOT NULL UNIQUE,
			created_by INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			scope_type VARCHAR(16) NOT NULL,
			scope_id INTEGER NOT NULL,
			expires_at TIMESTAMPTZ,
			revoked_at TIMESTAMPTZ,
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			CHECK (scope_type IN ('project'))
		)`,
		`CREATE INDEX IF NOT EXISTS idx_share_links_scope ON share_links(scope_type, scope_id)`,
		`CREATE TABLE IF NOT EXISTS project_events (
			id SERIAL PRIMARY KEY,
			project_id INTEGER NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
			actor_user_id INTEGER NOT NULL,
			event_type VARCHAR(32) NOT NULL,
			metadata JSONB DEFAULT '{}',
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)`,
		`CREATE INDEX IF NOT EXISTS idx_project_events_project ON project_events(project_id, created_at DESC)`,
	}
	for _, s := range stmts {
		if _, err := pool.Exec(context.Background(), s); err != nil {
			return fmt.Errorf("failed to create sharing tables: %v", err)
		}
	}
	return nil
}

// MigrateProjectOwnersToMembers ensures every project owner has an owner membership row.
func MigrateProjectOwnersToMembers() error {
	pool, err := OpenDatabase()
	if err != nil {
		return err
	}
	defer CloseDatabase(pool)

	_, err = pool.Exec(context.Background(), `
		INSERT INTO project_members (project_id, user_id, role)
		SELECT p.id, p.user_id, 'owner'
		FROM projects p
		ON CONFLICT (project_id, user_id) DO NOTHING`)
	return err
}

// MigrateShareLinksDropTagScope removes deprecated tag share links and
// restricts share_links.scope_type to project.
func MigrateShareLinksDropTagScope() error {
	pool, err := OpenDatabase()
	if err != nil {
		return err
	}
	defer CloseDatabase(pool)

	if _, err := pool.Exec(context.Background(), `DELETE FROM share_links WHERE scope_type = 'tag'`); err != nil {
		return fmt.Errorf("failed to delete tag share links: %v", err)
	}

	rows, err := pool.Query(context.Background(), `
		SELECT conname
		FROM pg_constraint
		WHERE conrelid = 'share_links'::regclass
		  AND contype = 'c'
		  AND pg_get_constraintdef(oid) ILIKE '%scope_type%'`)
	if err != nil {
		return err
	}
	defer rows.Close()

	var names []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return err
		}
		names = append(names, name)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	rows.Close()

	for _, name := range names {
		if !pgIdentSafe(name) {
			return fmt.Errorf("unexpected share_links constraint name %q", name)
		}
		if _, err := pool.Exec(context.Background(),
			fmt.Sprintf(`ALTER TABLE share_links DROP CONSTRAINT IF EXISTS %s`, name)); err != nil {
			return fmt.Errorf("failed to drop share_links check %s: %v", name, err)
		}
	}

	var exists bool
	err = pool.QueryRow(context.Background(),
		`SELECT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'share_links_scope_type_check')`).Scan(&exists)
	if err != nil {
		return err
	}
	if !exists {
		_, err = pool.Exec(context.Background(),
			`ALTER TABLE share_links ADD CONSTRAINT share_links_scope_type_check
			 CHECK (scope_type IN ('project'))`)
		if err != nil {
			return fmt.Errorf("failed to add share_links scope check: %v", err)
		}
	}
	return nil
}

func pgIdentSafe(name string) bool {
	if name == "" {
		return false
	}
	for i, r := range name {
		if r == '_' || (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') {
			continue
		}
		if i > 0 && r >= '0' && r <= '9' {
			continue
		}
		return false
	}
	return true
}

func newShareToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func ValidMemberRole(role string) bool {
	switch role {
	case RoleOwner, RoleEditor, RoleViewer:
		return true
	default:
		return false
	}
}

func ValidInviteRole(role string) bool {
	return role == RoleEditor || role == RoleViewer
}

func RoleCanWrite(role string) bool {
	return role == RoleOwner || role == RoleEditor
}

func RoleCanManage(role string) bool {
	return role == RoleOwner
}

// EnsureProjectOwnerMember inserts the owner membership row (idempotent).
func EnsureProjectOwnerMember(projectID, ownerUserID int) error {
	pool, err := OpenDatabase()
	if err != nil {
		return err
	}
	defer CloseDatabase(pool)

	_, err = pool.Exec(context.Background(),
		`INSERT INTO project_members (project_id, user_id, role) VALUES ($1, $2, $3)
		 ON CONFLICT (project_id, user_id) DO NOTHING`,
		projectID, ownerUserID, RoleOwner)
	return err
}

// GetProjectRole returns the caller's role for a project, or empty if no access.
func GetProjectRole(projectID, userID int) (string, error) {
	pool, err := OpenDatabase()
	if err != nil {
		return "", err
	}
	defer CloseDatabase(pool)

	var role string
	err = pool.QueryRow(context.Background(),
		`SELECT role FROM project_members WHERE project_id = $1 AND user_id = $2`,
		projectID, userID).Scan(&role)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) || errors.Is(err, sql.ErrNoRows) {
			// Fallback: project owner column (pre-migration or missing member row).
			var ownerID int
			err2 := pool.QueryRow(context.Background(),
				`SELECT user_id FROM projects WHERE id = $1`, projectID).Scan(&ownerID)
			if err2 != nil {
				if errors.Is(err2, pgx.ErrNoRows) || errors.Is(err2, sql.ErrNoRows) {
					return "", nil
				}
				return "", err2
			}
			if ownerID == userID {
				_ = EnsureProjectOwnerMember(projectID, userID)
				return RoleOwner, nil
			}
			return "", nil
		}
		return "", err
	}
	return role, nil
}

// GetAccessibleProjects returns owned and member projects with role metadata.
func GetAccessibleProjects(userID int) ([]ProjectWithAccess, error) {
	pool, err := OpenDatabase()
	if err != nil {
		return nil, err
	}
	defer CloseDatabase(pool)

	rows, err := pool.Query(context.Background(), `
		SELECT p.id, p.user_id, p.name, COALESCE(p.description, ''), COALESCE(p.workflow_mode, 'classic'),
		       COALESCE(p.position, 0), COALESCE(p.archived, false), COALESCE(p.backlog_name, 'Backlog'), p.created_at, p.updated_at,
		       COALESCE(pm.role, CASE WHEN p.user_id = $1 THEN 'owner' END),
		       u.email, COALESCE(u.user_name, ''), p.user_id
		FROM projects p
		LEFT JOIN project_members pm ON pm.project_id = p.id AND pm.user_id = $1
		JOIN users u ON u.id = p.user_id
		WHERE p.user_id = $1 OR pm.user_id = $1
		ORDER BY
		  CASE WHEN COALESCE(p.archived, false) THEN 1 ELSE 0 END,
		  CASE WHEN p.user_id = $1 THEN 0 ELSE 1 END,
		  p.position ASC,
		  LOWER(p.name) ASC,
		  p.id ASC`, userID)
	if err != nil {
		return nil, fmt.Errorf("failed to query accessible projects: %v", err)
	}
	defer rows.Close()

	var out []ProjectWithAccess
	for rows.Next() {
		var p ProjectWithAccess
		if err := rows.Scan(&p.ID, &p.UserID, &p.Name, &p.Description, &p.WorkflowMode, &p.Position,
			&p.Archived, &p.BacklogName, &p.CreatedAt, &p.UpdatedAt, &p.Role, &p.OwnerEmail, &p.OwnerUserName, &p.OwnerUserID); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, nil
}

// GetAccessibleProjectByID returns a project if the user is owner or member.
func GetAccessibleProjectByID(projectID, userID int) (*ProjectWithAccess, error) {
	pool, err := OpenDatabase()
	if err != nil {
		return nil, err
	}
	defer CloseDatabase(pool)

	var p ProjectWithAccess
	err = pool.QueryRow(context.Background(), `
		SELECT p.id, p.user_id, p.name, COALESCE(p.description, ''), COALESCE(p.workflow_mode, 'classic'),
		       COALESCE(p.position, 0), COALESCE(p.archived, false), COALESCE(p.backlog_name, 'Backlog'), p.created_at, p.updated_at,
		       COALESCE(pm.role, CASE WHEN p.user_id = $2 THEN 'owner' END),
		       u.email, COALESCE(u.user_name, ''), p.user_id
		FROM projects p
		LEFT JOIN project_members pm ON pm.project_id = p.id AND pm.user_id = $2
		JOIN users u ON u.id = p.user_id
		WHERE p.id = $1 AND (p.user_id = $2 OR pm.user_id = $2)`,
		projectID, userID).Scan(
		&p.ID, &p.UserID, &p.Name, &p.Description, &p.WorkflowMode, &p.Position,
		&p.Archived, &p.BacklogName, &p.CreatedAt, &p.UpdatedAt, &p.Role, &p.OwnerEmail, &p.OwnerUserName, &p.OwnerUserID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) || errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("project not found")
		}
		return nil, err
	}
	return &p, nil
}

// ListProjectMembers returns members of a project.
func ListProjectMembers(projectID int) ([]ProjectMember, error) {
	pool, err := OpenDatabase()
	if err != nil {
		return nil, err
	}
	defer CloseDatabase(pool)

	rows, err := pool.Query(context.Background(), `
		SELECT pm.user_id, u.email, COALESCE(u.user_name, ''), pm.role, pm.created_at
		FROM project_members pm
		JOIN users u ON u.id = pm.user_id
		WHERE pm.project_id = $1
		ORDER BY CASE pm.role WHEN 'owner' THEN 0 WHEN 'editor' THEN 1 ELSE 2 END, u.email`,
		projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []ProjectMember
	for rows.Next() {
		var m ProjectMember
		if err := rows.Scan(&m.UserID, &m.Email, &m.UserName, &m.Role, &m.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, nil
}

// UpsertProjectMember sets or updates a member role.
func UpsertProjectMember(projectID, userID int, role string) error {
	pool, err := OpenDatabase()
	if err != nil {
		return err
	}
	defer CloseDatabase(pool)

	_, err = pool.Exec(context.Background(), `
		INSERT INTO project_members (project_id, user_id, role) VALUES ($1, $2, $3)
		ON CONFLICT (project_id, user_id) DO UPDATE SET role = EXCLUDED.role`,
		projectID, userID, role)
	return err
}

// RemoveProjectMember deletes a membership row.
func RemoveProjectMember(projectID, userID int) error {
	pool, err := OpenDatabase()
	if err != nil {
		return err
	}
	defer CloseDatabase(pool)

	tag, err := pool.Exec(context.Background(),
		`DELETE FROM project_members WHERE project_id = $1 AND user_id = $2 AND role <> 'owner'`,
		projectID, userID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("member not found or cannot remove owner")
	}
	return nil
}

// CreateProjectInvite creates a pending invite.
func CreateProjectInvite(projectID int, email, role string, invitedBy int, expiresAt time.Time) (*ProjectInvite, error) {
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

	var inv ProjectInvite
	err = pool.QueryRow(context.Background(), `
		WITH ins AS (
			INSERT INTO project_invites (project_id, email, role, token, invited_by, expires_at)
			VALUES ($1, $2, $3, $4, $5, $6)
			RETURNING id, project_id, email, role, token, invited_by, expires_at, accepted_at, created_at
		)
		SELECT ins.id, ins.project_id, ins.email, ins.role, ins.token, ins.invited_by,
		       ins.expires_at, ins.accepted_at, ins.created_at,
		       COALESCE(p.name, ''), COALESCE(u.user_name, '')
		FROM ins
		LEFT JOIN projects p ON p.id = ins.project_id
		LEFT JOIN users u ON u.id = ins.invited_by`, projectID, email, role, token, invitedBy, expiresAt).Scan(
		&inv.ID, &inv.ProjectID, &inv.Email, &inv.Role, &inv.Token, &inv.InvitedBy,
		&inv.ExpiresAt, &inv.AcceptedAt, &inv.CreatedAt, &inv.ProjectName, &inv.InviterUserName)
	if err != nil {
		return nil, err
	}
	if settings, err := GetSiteSettings(); err == nil && settings != nil {
		subject := "Project Invite"
		body := fmt.Sprintf("You have been invited to join project %s by %s.", inv.ProjectName, inv.InviterUserName)
		if err := mailer.SendEmail(settings.Email, mailer.TriggerProjectInvite, subject, body, inv.Email); err != nil {
			fmt.Printf("Warning: Failed to send project invite email to %s: %v\n", inv.Email, err)
		}
	}
	return &inv, nil
}

// ListProjectInvites returns pending invites for a project.
func ListProjectInvites(projectID int) ([]ProjectInvite, error) {
	pool, err := OpenDatabase()
	if err != nil {
		return nil, err
	}
	defer CloseDatabase(pool)

	rows, err := pool.Query(context.Background(), `
		SELECT i.id, i.project_id, i.email, i.role, i.token, i.invited_by, i.expires_at, i.accepted_at, i.created_at,
		       COALESCE(u.user_name, '')
		FROM project_invites i
		LEFT JOIN users u ON LOWER(u.email) = LOWER(i.email)
		WHERE i.project_id = $1 AND i.accepted_at IS NULL
		ORDER BY i.created_at DESC`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []ProjectInvite
	for rows.Next() {
		var inv ProjectInvite
		if err := rows.Scan(&inv.ID, &inv.ProjectID, &inv.Email, &inv.Role, &inv.Token,
			&inv.InvitedBy, &inv.ExpiresAt, &inv.AcceptedAt, &inv.CreatedAt, &inv.UserName); err != nil {
			return nil, err
		}
		out = append(out, inv)
	}
	return out, nil
}

// DeleteProjectInvite removes a pending invite.
func DeleteProjectInvite(inviteID, projectID int) error {
	pool, err := OpenDatabase()
	if err != nil {
		return err
	}
	defer CloseDatabase(pool)

	tag, err := pool.Exec(context.Background(),
		`DELETE FROM project_invites WHERE id = $1 AND project_id = $2 AND accepted_at IS NULL`,
		inviteID, projectID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("invite not found")
	}
	return nil
}

// ListPendingInvitesForEmail returns pending invites for an email address.
func ListPendingInvitesForEmail(email string) ([]ProjectInvite, error) {
	email = strings.TrimSpace(strings.ToLower(email))
	pool, err := OpenDatabase()
	if err != nil {
		return nil, err
	}
	defer CloseDatabase(pool)

	rows, err := pool.Query(context.Background(), `
		SELECT i.id, i.project_id, i.email, i.role, i.token, i.invited_by, i.expires_at, i.accepted_at, i.created_at,
		       COALESCE(invitee.user_name, ''), p.name, COALESCE(u.email, ''), COALESCE(u.user_name, '')
		FROM project_invites i
		JOIN projects p ON p.id = i.project_id
		LEFT JOIN users u ON u.id = i.invited_by
		LEFT JOIN users invitee ON LOWER(invitee.email) = LOWER(i.email)
		WHERE i.email = $1 AND i.accepted_at IS NULL AND i.expires_at > NOW()
		ORDER BY i.created_at DESC`, email)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []ProjectInvite
	for rows.Next() {
		var inv ProjectInvite
		if err := rows.Scan(&inv.ID, &inv.ProjectID, &inv.Email, &inv.Role, &inv.Token,
			&inv.InvitedBy, &inv.ExpiresAt, &inv.AcceptedAt, &inv.CreatedAt,
			&inv.UserName, &inv.ProjectName, &inv.InviterEmail, &inv.InviterUserName); err != nil {
			return nil, err
		}
		out = append(out, inv)
	}
	return out, nil
}

// GetProjectInviteByID loads an invite by id.
func GetProjectInviteByID(inviteID int) (*ProjectInvite, error) {
	pool, err := OpenDatabase()
	if err != nil {
		return nil, err
	}
	defer CloseDatabase(pool)

	var inv ProjectInvite
	err = pool.QueryRow(context.Background(), `
		SELECT i.id, i.project_id, i.email, i.role, i.token, i.invited_by, i.expires_at, i.accepted_at, i.created_at,
		       COALESCE(p.name, ''), COALESCE(u.email, ''), COALESCE(u.user_name, '')
		FROM project_invites i
		LEFT JOIN projects p ON p.id = i.project_id
		LEFT JOIN users u ON u.id = i.invited_by
		WHERE i.id = $1`, inviteID).Scan(
		&inv.ID, &inv.ProjectID, &inv.Email, &inv.Role, &inv.Token,
		&inv.InvitedBy, &inv.ExpiresAt, &inv.AcceptedAt, &inv.CreatedAt,
		&inv.ProjectName, &inv.InviterEmail, &inv.InviterUserName)
	if err != nil {
		return nil, err
	}
	return &inv, nil
}

// AcceptProjectInvite marks invite accepted and adds membership.
func AcceptProjectInvite(inviteID, userID int, userEmail string) error {
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

	var inv ProjectInvite
	err = tx.QueryRow(context.Background(), `
		SELECT id, project_id, email, role, expires_at, accepted_at
		FROM project_invites WHERE id = $1 FOR UPDATE`, inviteID).Scan(
		&inv.ID, &inv.ProjectID, &inv.Email, &inv.Role, &inv.ExpiresAt, &inv.AcceptedAt)
	if err != nil {
		return err
	}
	if inv.AcceptedAt != nil {
		return fmt.Errorf("invite already accepted")
	}
	if time.Now().After(inv.ExpiresAt) {
		return fmt.Errorf("invite expired")
	}
	if !strings.EqualFold(inv.Email, strings.TrimSpace(userEmail)) {
		return fmt.Errorf("invite email mismatch")
	}

	_, err = tx.Exec(context.Background(), `
		INSERT INTO project_members (project_id, user_id, role) VALUES ($1, $2, $3)
		ON CONFLICT (project_id, user_id) DO UPDATE SET role = EXCLUDED.role
		WHERE project_members.role <> 'owner'`,
		inv.ProjectID, userID, inv.Role)
	if err != nil {
		return err
	}
	_, err = tx.Exec(context.Background(),
		`UPDATE project_invites SET accepted_at = NOW() WHERE id = $1`, inviteID)
	if err != nil {
		return err
	}
	return tx.Commit(context.Background())
}

// DeclineProjectInvite deletes a pending invite for the user's email.
func DeclineProjectInvite(inviteID int, userEmail string) error {
	pool, err := OpenDatabase()
	if err != nil {
		return err
	}
	defer CloseDatabase(pool)

	tag, err := pool.Exec(context.Background(), `
		DELETE FROM project_invites
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

// CreateShareLink creates a read-only share link.
func CreateShareLink(createdBy int, scopeType string, scopeID int, expiresAt *time.Time) (*ShareLink, error) {
	token, err := newShareToken()
	if err != nil {
		return nil, err
	}
	pool, err := OpenDatabase()
	if err != nil {
		return nil, err
	}
	defer CloseDatabase(pool)

	var link ShareLink
	err = pool.QueryRow(context.Background(), `
		INSERT INTO share_links (token, created_by, scope_type, scope_id, expires_at)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING id, token, created_by, scope_type, scope_id, expires_at, revoked_at, created_at`,
		token, createdBy, scopeType, scopeID, expiresAt).Scan(
		&link.ID, &link.Token, &link.CreatedBy, &link.ScopeType, &link.ScopeID,
		&link.ExpiresAt, &link.RevokedAt, &link.CreatedAt)
	if err != nil {
		return nil, err
	}
	return &link, nil
}

// ListShareLinks returns share links for a scope that the user created.
func ListShareLinks(userID int, scopeType string, scopeID int) ([]ShareLink, error) {
	pool, err := OpenDatabase()
	if err != nil {
		return nil, err
	}
	defer CloseDatabase(pool)

	rows, err := pool.Query(context.Background(), `
		SELECT id, token, created_by, scope_type, scope_id, expires_at, revoked_at, created_at
		FROM share_links
		WHERE created_by = $1 AND scope_type = $2 AND scope_id = $3 AND revoked_at IS NULL
		ORDER BY created_at DESC`, userID, scopeType, scopeID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []ShareLink
	for rows.Next() {
		var link ShareLink
		if err := rows.Scan(&link.ID, &link.Token, &link.CreatedBy, &link.ScopeType, &link.ScopeID,
			&link.ExpiresAt, &link.RevokedAt, &link.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, link)
	}
	return out, nil
}

// ListShareLinksForScope returns all active share links for a scope.
func ListShareLinksForScope(scopeType string, scopeID int) ([]ShareLink, error) {
	pool, err := OpenDatabase()
	if err != nil {
		return nil, err
	}
	defer CloseDatabase(pool)

	rows, err := pool.Query(context.Background(), `
		SELECT id, token, created_by, scope_type, scope_id, expires_at, revoked_at, created_at
		FROM share_links
		WHERE scope_type = $1 AND scope_id = $2 AND revoked_at IS NULL
		ORDER BY created_at DESC`, scopeType, scopeID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []ShareLink
	for rows.Next() {
		var link ShareLink
		if err := rows.Scan(&link.ID, &link.Token, &link.CreatedBy, &link.ScopeType, &link.ScopeID,
			&link.ExpiresAt, &link.RevokedAt, &link.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, link)
	}
	return out, nil
}

// GetLatestActiveShareLinkForScope returns the newest non-revoked, non-expired share link for a scope.
func GetLatestActiveShareLinkForScope(scopeType string, scopeID int) (*ShareLink, error) {
	pool, err := OpenDatabase()
	if err != nil {
		return nil, err
	}
	defer CloseDatabase(pool)

	var link ShareLink
	err = pool.QueryRow(context.Background(), `
		SELECT id, token, created_by, scope_type, scope_id, expires_at, revoked_at, created_at
		FROM share_links
		WHERE scope_type = $1 AND scope_id = $2 AND revoked_at IS NULL
		  AND (expires_at IS NULL OR expires_at > NOW())
		ORDER BY created_at DESC, id DESC
		LIMIT 1`, scopeType, scopeID).Scan(
		&link.ID, &link.Token, &link.CreatedBy, &link.ScopeType, &link.ScopeID,
		&link.ExpiresAt, &link.RevokedAt, &link.CreatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) || errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return &link, nil
}

// GetShareLinkByID loads a share link by id.
func GetShareLinkByID(id int) (*ShareLink, error) {
	pool, err := OpenDatabase()
	if err != nil {
		return nil, err
	}
	defer CloseDatabase(pool)

	var link ShareLink
	err = pool.QueryRow(context.Background(), `
		SELECT id, token, created_by, scope_type, scope_id, expires_at, revoked_at, created_at
		FROM share_links WHERE id = $1`, id).Scan(
		&link.ID, &link.Token, &link.CreatedBy, &link.ScopeType, &link.ScopeID,
		&link.ExpiresAt, &link.RevokedAt, &link.CreatedAt)
	if err != nil {
		return nil, err
	}
	return &link, nil
}

// RevokeShareLink sets revoked_at.
func RevokeShareLink(id, userID int) error {
	pool, err := OpenDatabase()
	if err != nil {
		return err
	}
	defer CloseDatabase(pool)

	tag, err := pool.Exec(context.Background(), `
		UPDATE share_links SET revoked_at = NOW()
		WHERE id = $1 AND created_by = $2 AND revoked_at IS NULL`, id, userID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("share link not found")
	}
	return nil
}

// GetActiveShareLinkByToken returns a non-revoked, non-expired share link.
func GetActiveShareLinkByToken(token string) (*ShareLink, error) {
	pool, err := OpenDatabase()
	if err != nil {
		return nil, err
	}
	defer CloseDatabase(pool)

	var link ShareLink
	err = pool.QueryRow(context.Background(), `
		SELECT id, token, created_by, scope_type, scope_id, expires_at, revoked_at, created_at
		FROM share_links
		WHERE token = $1 AND revoked_at IS NULL
		  AND (expires_at IS NULL OR expires_at > NOW())`, token).Scan(
		&link.ID, &link.Token, &link.CreatedBy, &link.ScopeType, &link.ScopeID,
		&link.ExpiresAt, &link.RevokedAt, &link.CreatedAt)
	if err != nil {
		return nil, err
	}
	return &link, nil
}

// LogProjectEvent appends a project audit event.
func LogProjectEvent(projectID, actorUserID int, eventType string, metadata map[string]interface{}) error {
	pool, err := OpenDatabase()
	if err != nil {
		return err
	}
	defer CloseDatabase(pool)

	metaJSON := []byte("{}")
	if metadata != nil {
		if b, err := json.Marshal(metadata); err == nil {
			metaJSON = b
		}
	}
	_, err = pool.Exec(context.Background(),
		`INSERT INTO project_events (project_id, actor_user_id, event_type, metadata) VALUES ($1, $2, $3, $4)`,
		projectID, actorUserID, eventType, metaJSON)
	return err
}

// GetProjectEvents returns recent project events.
func GetProjectEvents(projectID, limit int) ([]ProjectEvent, error) {
	if limit <= 0 {
		limit = 50
	}
	pool, err := OpenDatabase()
	if err != nil {
		return nil, err
	}
	defer CloseDatabase(pool)

	rows, err := pool.Query(context.Background(), `
		SELECT pe.id, pe.project_id, pe.actor_user_id, pe.event_type, COALESCE(pe.metadata, '{}'), pe.created_at,
		       COALESCE(u.email, ''), COALESCE(u.user_name, '')
		FROM project_events pe
		LEFT JOIN users u ON u.id = pe.actor_user_id
		WHERE pe.project_id = $1
		ORDER BY pe.created_at DESC
		LIMIT $2`, projectID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []ProjectEvent
	for rows.Next() {
		var ev ProjectEvent
		var metaRaw []byte
		if err := rows.Scan(&ev.ID, &ev.ProjectID, &ev.ActorUserID, &ev.EventType, &metaRaw, &ev.CreatedAt, &ev.ActorEmail, &ev.ActorUserName); err != nil {
			return nil, err
		}
		ev.Metadata = map[string]interface{}{}
		if len(metaRaw) > 0 {
			_ = json.Unmarshal(metaRaw, &ev.Metadata)
		}
		out = append(out, ev)
	}
	return out, nil
}

// TaskVisibleCondition returns a SQL fragment for tasks readable by userID,
// including viewer membership on shared projects.
// Uses placeholder $N for userID; caller must append userID to args at that index.
func TaskVisibleCondition(alias string, userParam string) string {
	return taskVisibleCondition(alias, userParam, false)
}

// TaskHomeVisibleCondition returns a SQL fragment for the unscoped/home task list.
// Shared-project membership only counts when the caller is owner or editor;
// viewers see those tasks only when listing a specific project.
func TaskHomeVisibleCondition(alias string, userParam string) string {
	return taskVisibleCondition(alias, userParam, true)
}

// TaskListVisibleCondition picks home vs full visibility from whether the list
// is scoped to a specific project (projectFilter > 0).
func TaskListVisibleCondition(alias, userParam string, projectFilter *int) string {
	if projectFilter != nil && *projectFilter > 0 {
		return TaskVisibleCondition(alias, userParam)
	}
	return TaskHomeVisibleCondition(alias, userParam)
}

func taskVisibleCondition(alias, userParam string, writeRolesOnly bool) string {
	// Default to tasks. when unaliased so EXISTS subqueries correlate to the
	// outer tasks row (bare project_id would resolve to pm.project_id).
	prefix := "tasks."
	if alias != "" {
		prefix = alias + "."
	}
	memberRoleFilter := ""
	if writeRolesOnly {
		memberRoleFilter = " AND pm.role IN ('owner', 'editor')"
	}
	return fmt.Sprintf(`(%suser_id = %s OR (%sproject_id IS NOT NULL AND EXISTS (
		SELECT 1 FROM project_members pm WHERE pm.project_id = %sproject_id AND pm.user_id = %s%s
	)) OR (%sproject_id IS NOT NULL AND EXISTS (
		SELECT 1 FROM projects p_own WHERE p_own.id = %sproject_id AND p_own.user_id = %s
	)))`, prefix, userParam, prefix, prefix, userParam, memberRoleFilter, prefix, prefix, userParam)
}

// CanUserAccessTask reports whether userID can read the task and their effective write role.
// writeRole is owner/editor when writes are allowed, viewer when read-only, or empty if no access.
func CanUserAccessTask(taskID, userID int) (canRead bool, writeRole string, projectID int, err error) {
	pool, err := OpenDatabase()
	if err != nil {
		return false, "", 0, err
	}
	defer CloseDatabase(pool)

	var ownerID int
	var proj sql.NullInt64
	err = pool.QueryRow(context.Background(),
		`SELECT user_id, project_id FROM tasks WHERE id = $1`, taskID).Scan(&ownerID, &proj)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) || errors.Is(err, sql.ErrNoRows) {
			return false, "", 0, nil
		}
		return false, "", 0, err
	}
	if !proj.Valid {
		if ownerID == userID {
			return true, RoleOwner, 0, nil
		}
		return false, "", 0, nil
	}
	pid := int(proj.Int64)
	role, err := GetProjectRole(pid, userID)
	if err != nil {
		return false, "", pid, err
	}
	if role == "" {
		return false, "", pid, nil
	}
	if RoleCanWrite(role) {
		return true, role, pid, nil
	}
	return true, RoleViewer, pid, nil
}

// ListTasksForShareLink returns a slim task list for a share scope.
func ListTasksForShareLink(scopeType string, scopeID, createdBy int) ([]map[string]interface{}, error) {
	pool, err := OpenDatabase()
	if err != nil {
		return nil, err
	}
	defer CloseDatabase(pool)

	if scopeType != ShareScopeProject {
		return nil, fmt.Errorf("invalid scope")
	}
	_ = createdBy
	rows, err := pool.Query(context.Background(), `
			SELECT t.id, t.title, t.completed, COALESCE(CAST(t.due_date AS TEXT), ''), COALESCE(t.priority,0),
			       COALESCE(p.name, ''), COALESCE(t.description, ''), COALESCE(s.name, '')
			FROM tasks t
			LEFT JOIN projects p ON p.id = t.project_id
			LEFT JOIN project_statuses s ON s.id = t.status_id
			WHERE t.project_id = $1
			ORDER BY t.completed ASC, t.position ASC, t.id ASC`, scopeID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []map[string]interface{}
	for rows.Next() {
		var id, priority int
		var title, due, projectName, description, statusName string
		var completed bool
		if err := rows.Scan(&id, &title, &completed, &due, &priority, &projectName, &description, &statusName); err != nil {
			return nil, err
		}
		item := map[string]interface{}{
			"id":          id,
			"title":       title,
			"description": description,
			"completed":   completed,
			"due_date":    due,
			"priority":    priority,
		}
		if projectName != "" {
			item["project"] = projectName
		}
		if statusName != "" {
			item["status_name"] = statusName
		}
		// Attach tags for this task
		tagRows, err := pool.Query(context.Background(), `
			SELECT tg.id, tg.name, COALESCE(tg.color, '')
			FROM tags tg JOIN task_tags tt ON tt.tag_id = tg.id WHERE tt.task_id = $1 ORDER BY tg.name`, id)
		if err == nil {
			tags := []map[string]interface{}{}
			for tagRows.Next() {
				var tid int
				var name, color string
				if tagRows.Scan(&tid, &name, &color) == nil {
					tags = append(tags, map[string]interface{}{"id": tid, "name": name, "color": color})
				}
			}
			tagRows.Close()
			item["tags"] = tags
		}
		out = append(out, item)
	}
	if out == nil {
		out = []map[string]interface{}{}
	}
	return out, nil
}

// GetUserEmailByID returns the email for a user id.
func GetUserEmailByID(userID int) (string, error) {
	pool, err := OpenDatabase()
	if err != nil {
		return "", err
	}
	defer CloseDatabase(pool)

	var email string
	err = pool.QueryRow(context.Background(), `SELECT email FROM users WHERE id = $1`, userID).Scan(&email)
	return email, err
}
