package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
)

const (
	PermTasksCreate      = "tasks:create"
	PermTasksEdit        = "tasks:edit"
	PermTasksDelete      = "tasks:delete"
	PermTasksArchive     = "tasks:archive"
	PermTasksRestore     = "tasks:restore"
	PermTasksComplete    = "tasks:complete"
	PermTasksClaim       = "tasks:claim"
	PermTasksReorder     = "tasks:reorder"
	PermTasksStatus      = "tasks:status"
	PermTasksSprint      = "tasks:sprint"
	PermProjectManage    = "project:manage"
	PermProjectTags      = "project:tags"
	PermTimeWrite        = "time:write"
	PermExtensionsWrite  = "extensions:write"
	PermCommentsModerate = "comments:moderate"

	RoleDeveloper = "developer"
	RoleQA        = "qa"

	MaxProjectRoleSlugLen = 40
	MaxProjectRoleNameLen = 80
	MaxProjectRoleDescLen = 200
	MaxProjectCustomRoles = 20
)

// RoleOwner, RoleEditor, and RoleViewer remain the built-in slugs.

// ProjectPermInfo describes one assignable permission in the catalog.
type ProjectPermInfo struct {
	ID          string `json:"id"`
	Label       string `json:"label"`
	Description string `json:"description"`
	Group       string `json:"group"`
}

// ProjectRoleDef is a site-level or project-specific role with a permission set.
type ProjectRoleDef struct {
	ID          int
	ProjectID   *int
	Slug        string
	Name        string
	Description string
	Permissions []string
	IsSystem    bool
	SortOrder   int
	CreatedAt   time.Time
}

// ProjectStatusGate restricts which roles may enter or leave a status.
// Empty slug lists mean any role with tasks:status may move that direction.
type ProjectStatusGate struct {
	StatusID       int
	EnterRoleSlugs []string
	LeaveRoleSlugs []string
}

var projectPermCatalog = []ProjectPermInfo{
	{ID: PermTasksCreate, Label: "Create tasks", Description: "Add tasks and subtasks to the project", Group: "Tasks"},
	{ID: PermTasksEdit, Label: "Edit task details", Description: "Change title, description, due date, priority, tags, estimates, and custom fields", Group: "Tasks"},
	{ID: PermTasksDelete, Label: "Delete tasks", Description: "Permanently delete tasks", Group: "Tasks"},
	{ID: PermTasksArchive, Label: "Archive tasks", Description: "Apply the archived tag to tasks", Group: "Tasks"},
	{ID: PermTasksRestore, Label: "Restore tasks", Description: "Clear the archived tag from tasks", Group: "Tasks"},
	{ID: PermTasksComplete, Label: "Complete tasks", Description: "Mark tasks complete or incomplete", Group: "Tasks"},
	{ID: PermTasksClaim, Label: "Claim tasks", Description: "Claim or unclaim kanban cards", Group: "Tasks"},
	{ID: PermTasksReorder, Label: "Reorder tasks", Description: "Drag to change task order", Group: "Tasks"},
	{ID: PermTasksStatus, Label: "Change status", Description: "Move tasks between board columns (still subject to status gates)", Group: "Tasks"},
	{ID: PermTasksSprint, Label: "Assign sprints", Description: "Move tasks between sprints", Group: "Tasks"},
	{ID: PermProjectTags, Label: "Manage tags", Description: "Create, rename, recolor, and delete project tags", Group: "Project"},
	{ID: PermTimeWrite, Label: "Log time", Description: "Add time entries on kanban tasks", Group: "Project"},
	{ID: PermExtensionsWrite, Label: "Configure extensions", Description: "Change project extension settings and document store writes", Group: "Project"},
	{ID: PermCommentsModerate, Label: "Moderate comments", Description: "Edit or delete other members' discussion posts", Group: "Project"},
	{ID: PermProjectManage, Label: "Manage project", Description: "Members, invites, board columns, sprints, and project settings", Group: "Project"},
}

var writeTaskPerms = []string{
	PermTasksCreate, PermTasksEdit, PermTasksDelete, PermTasksArchive, PermTasksRestore,
	PermTasksComplete, PermTasksClaim, PermTasksReorder, PermTasksStatus, PermTasksSprint,
	PermTimeWrite,
}

var (
	siteRoleCacheMu sync.RWMutex
	siteRoleCache   map[string]ProjectRoleDef
)

// ProjectPermissionCatalog returns the core permission list roles can be built from.
func ProjectPermissionCatalog() []ProjectPermInfo {
	out := make([]ProjectPermInfo, len(projectPermCatalog))
	copy(out, projectPermCatalog)
	return out
}

// AllProjectPerms returns every permission id.
func AllProjectPerms() []string {
	out := make([]string, 0, len(projectPermCatalog))
	for _, p := range projectPermCatalog {
		out = append(out, p.ID)
	}
	return out
}

// ValidProjectPerm reports whether id is in the catalog.
func ValidProjectPerm(id string) bool {
	for _, p := range projectPermCatalog {
		if p.ID == id {
			return true
		}
	}
	return false
}

func editorDefaultPerms() []string {
	return []string{
		PermTasksCreate, PermTasksEdit, PermTasksDelete, PermTasksArchive, PermTasksRestore,
		PermTasksComplete, PermTasksClaim, PermTasksReorder, PermTasksStatus, PermTasksSprint,
		PermProjectTags, PermTimeWrite, PermExtensionsWrite,
	}
}

func developerDefaultPerms() []string {
	return editorDefaultPerms()
}

func qaDefaultPerms() []string {
	return []string{
		PermTasksComplete, PermTasksClaim, PermTasksReorder, PermTasksStatus, PermTimeWrite,
	}
}

func normalizePermList(perms []string) ([]string, error) {
	seen := make(map[string]bool, len(perms))
	out := make([]string, 0, len(perms))
	for _, raw := range perms {
		id := strings.TrimSpace(strings.ToLower(raw))
		if id == "" {
			continue
		}
		if !ValidProjectPerm(id) {
			return nil, fmt.Errorf("unknown permission %q", id)
		}
		if seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, id)
	}
	return out, nil
}

func hasAnyWritePerm(perms []string) bool {
	want := make(map[string]bool, len(writeTaskPerms))
	for _, p := range writeTaskPerms {
		want[p] = true
	}
	for _, p := range perms {
		if want[p] {
			return true
		}
	}
	return false
}

func permListContains(perms []string, perm string) bool {
	for _, p := range perms {
		if p == perm {
			return true
		}
	}
	return false
}

// InvalidateSiteRoleCache drops cached site-level role definitions.
func InvalidateSiteRoleCache() {
	siteRoleCacheMu.Lock()
	siteRoleCache = nil
	siteRoleCacheMu.Unlock()
}

func loadSiteRoleCache() map[string]ProjectRoleDef {
	siteRoleCacheMu.RLock()
	if siteRoleCache != nil {
		out := siteRoleCache
		siteRoleCacheMu.RUnlock()
		return out
	}
	siteRoleCacheMu.RUnlock()

	defs, err := ListSiteProjectRoles()
	if err != nil {
		return map[string]ProjectRoleDef{}
	}
	next := make(map[string]ProjectRoleDef, len(defs))
	for _, d := range defs {
		next[d.Slug] = d
	}
	siteRoleCacheMu.Lock()
	siteRoleCache = next
	siteRoleCacheMu.Unlock()
	return next
}

// CreateProjectRoleTables creates role definition and status-gate tables.
func CreateProjectRoleTables() error {
	pool, err := OpenDatabase()
	if err != nil {
		return err
	}
	defer CloseDatabase(pool)

	stmts := []string{
		`CREATE TABLE IF NOT EXISTS project_role_defs (
			id SERIAL PRIMARY KEY,
			project_id INTEGER REFERENCES projects(id) ON DELETE CASCADE,
			slug VARCHAR(40) NOT NULL,
			name VARCHAR(80) NOT NULL,
			description TEXT NOT NULL DEFAULT '',
			permissions TEXT[] NOT NULL DEFAULT '{}',
			is_system BOOLEAN NOT NULL DEFAULT FALSE,
			sort_order INTEGER NOT NULL DEFAULT 0,
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_project_role_defs_site_slug
			ON project_role_defs (slug) WHERE project_id IS NULL`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_project_role_defs_project_slug
			ON project_role_defs (project_id, slug) WHERE project_id IS NOT NULL`,
		`CREATE INDEX IF NOT EXISTS idx_project_role_defs_project
			ON project_role_defs (project_id)`,
		`CREATE TABLE IF NOT EXISTS project_status_gates (
			status_id INTEGER PRIMARY KEY REFERENCES project_statuses(id) ON DELETE CASCADE,
			enter_role_slugs TEXT[] NOT NULL DEFAULT '{}',
			leave_role_slugs TEXT[] NOT NULL DEFAULT '{}'
		)`,
	}
	for _, s := range stmts {
		if _, err := pool.Exec(context.Background(), s); err != nil {
			return fmt.Errorf("failed to create project role tables: %v", err)
		}
	}
	return nil
}

func dropRoleCheckConstraints(table string) error {
	pool, err := OpenDatabase()
	if err != nil {
		return err
	}
	defer CloseDatabase(pool)

	if !pgIdentSafe(table) {
		return fmt.Errorf("unexpected table name %q", table)
	}

	rows, err := pool.Query(context.Background(), `
		SELECT conname
		FROM pg_constraint
		WHERE conrelid = $1::regclass
		  AND contype = 'c'
		  AND pg_get_constraintdef(oid) ILIKE '%role%'`, table)
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
			return fmt.Errorf("unexpected constraint name %q", name)
		}
		if _, err := pool.Exec(context.Background(),
			fmt.Sprintf(`ALTER TABLE %s DROP CONSTRAINT IF EXISTS %s`, table, name)); err != nil {
			return fmt.Errorf("failed to drop %s check %s: %v", table, name, err)
		}
	}
	if _, err := pool.Exec(context.Background(),
		fmt.Sprintf(`ALTER TABLE %s ALTER COLUMN role TYPE VARCHAR(40)`, table)); err != nil {
		return fmt.Errorf("failed to widen %s.role: %v", table, err)
	}
	return nil
}

// MigrateProjectMemberRoleConstraints removes owner/editor/viewer CHECKs so
// site and project roles can use other slugs.
func MigrateProjectMemberRoleConstraints() error {
	if err := dropRoleCheckConstraints("project_members"); err != nil {
		return err
	}
	return dropRoleCheckConstraints("project_invites")
}

func seedSiteRole(slug, name, description string, perms []string, system bool, sortOrder int) error {
	pool, err := OpenDatabase()
	if err != nil {
		return err
	}
	defer CloseDatabase(pool)

	if perms == nil {
		perms = []string{}
	}
	_, err = pool.Exec(context.Background(), `
		INSERT INTO project_role_defs (project_id, slug, name, description, permissions, is_system, sort_order)
		VALUES (NULL, $1, $2, $3, $4, $5, $6)
		ON CONFLICT (slug) WHERE project_id IS NULL DO NOTHING`,
		slug, name, description, perms, system, sortOrder)
	return err
}

// SeedDefaultProjectRoles inserts built-in site roles if they are missing.
func SeedDefaultProjectRoles() error {
	seeds := []struct {
		slug, name, description string
		perms                   []string
		system                  bool
		order                   int
	}{
		{RoleOwner, "Owner", "Full control of the project, members, and workflow", AllProjectPerms(), true, 0},
		{RoleEditor, "Editor", "Create and edit work; cannot manage members or project settings", editorDefaultPerms(), true, 1},
		{RoleViewer, "Viewer", "Read the project and join discussion", []string{}, true, 2},
		{RoleDeveloper, "Developer", "Implement work across the board, including creating and deleting tasks", developerDefaultPerms(), false, 3},
		{RoleQA, "QA", "Move and complete tasks during testing; cannot create or delete tasks", qaDefaultPerms(), false, 4},
	}
	for _, s := range seeds {
		if err := seedSiteRole(s.slug, s.name, s.description, s.perms, s.system, s.order); err != nil {
			return fmt.Errorf("seed role %s: %w", s.slug, err)
		}
	}
	InvalidateSiteRoleCache()
	return nil
}

func scanProjectRoleDef(row interface{ Scan(dest ...any) error }, d *ProjectRoleDef) error {
	var projectID sql.NullInt64
	var perms []string
	if err := row.Scan(&d.ID, &projectID, &d.Slug, &d.Name, &d.Description, &perms, &d.IsSystem, &d.SortOrder, &d.CreatedAt); err != nil {
		return err
	}
	if projectID.Valid {
		id := int(projectID.Int64)
		d.ProjectID = &id
	}
	if perms == nil {
		perms = []string{}
	}
	d.Permissions = perms
	return nil
}

const projectRoleSelect = `SELECT id, project_id, slug, name, COALESCE(description, ''), COALESCE(permissions, '{}'),
		COALESCE(is_system, false), COALESCE(sort_order, 0), created_at
	 FROM project_role_defs`

// ListSiteProjectRoles returns global role templates.
func ListSiteProjectRoles() ([]ProjectRoleDef, error) {
	pool, err := OpenDatabase()
	if err != nil {
		return nil, err
	}
	defer CloseDatabase(pool)

	rows, err := pool.Query(context.Background(),
		projectRoleSelect+` WHERE project_id IS NULL ORDER BY sort_order ASC, id ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []ProjectRoleDef
	for rows.Next() {
		var d ProjectRoleDef
		if err := scanProjectRoleDef(rows, &d); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

// ListProjectCustomRoles returns roles created for one project.
func ListProjectCustomRoles(projectID int) ([]ProjectRoleDef, error) {
	pool, err := OpenDatabase()
	if err != nil {
		return nil, err
	}
	defer CloseDatabase(pool)

	rows, err := pool.Query(context.Background(),
		projectRoleSelect+` WHERE project_id = $1 ORDER BY sort_order ASC, id ASC`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []ProjectRoleDef
	for rows.Next() {
		var d ProjectRoleDef
		if err := scanProjectRoleDef(rows, &d); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

// ListAssignableProjectRoles returns site roles (except owner) plus project custom roles.
func ListAssignableProjectRoles(projectID int) ([]ProjectRoleDef, error) {
	site, err := ListSiteProjectRoles()
	if err != nil {
		return nil, err
	}
	custom, err := ListProjectCustomRoles(projectID)
	if err != nil {
		return nil, err
	}
	out := make([]ProjectRoleDef, 0, len(site)+len(custom))
	for _, d := range site {
		if d.Slug == RoleOwner {
			continue
		}
		out = append(out, d)
	}
	out = append(out, custom...)
	return out, nil
}

// GetProjectRoleDef loads a role by id.
func GetProjectRoleDef(id int) (*ProjectRoleDef, error) {
	pool, err := OpenDatabase()
	if err != nil {
		return nil, err
	}
	defer CloseDatabase(pool)

	var d ProjectRoleDef
	err = scanProjectRoleDef(pool.QueryRow(context.Background(),
		projectRoleSelect+` WHERE id = $1`, id), &d)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) || errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return &d, nil
}

// ResolveRoleDef finds the project-specific role, then the site template.
func ResolveRoleDef(projectID int, slug string) *ProjectRoleDef {
	slug = strings.TrimSpace(strings.ToLower(slug))
	if slug == "" {
		return nil
	}
	if projectID > 0 {
		if d, err := getRoleDefBySlug(projectID, slug, false); err == nil && d != nil {
			return d
		}
	}
	if cached := loadSiteRoleCache(); cached != nil {
		if d, ok := cached[slug]; ok {
			cp := d
			return &cp
		}
	}
	if d, err := getRoleDefBySlug(0, slug, true); err == nil && d != nil {
		return d
	}
	return builtinRoleFallback(slug)
}

func builtinRoleFallback(slug string) *ProjectRoleDef {
	switch slug {
	case RoleOwner:
		return &ProjectRoleDef{Slug: RoleOwner, Name: "Owner", Permissions: AllProjectPerms(), IsSystem: true}
	case RoleEditor:
		return &ProjectRoleDef{Slug: RoleEditor, Name: "Editor", Permissions: editorDefaultPerms(), IsSystem: true}
	case RoleViewer:
		return &ProjectRoleDef{Slug: RoleViewer, Name: "Viewer", Permissions: []string{}, IsSystem: true}
	default:
		return nil
	}
}

func getRoleDefBySlug(projectID int, slug string, siteOnly bool) (*ProjectRoleDef, error) {
	pool, err := OpenDatabase()
	if err != nil {
		return nil, err
	}
	defer CloseDatabase(pool)

	var d ProjectRoleDef
	var q string
	var args []any
	if siteOnly || projectID <= 0 {
		q = projectRoleSelect + ` WHERE project_id IS NULL AND slug = $1`
		args = []any{slug}
	} else {
		q = projectRoleSelect + ` WHERE project_id = $1 AND slug = $2`
		args = []any{projectID, slug}
	}
	err = scanProjectRoleDef(pool.QueryRow(context.Background(), q, args...), &d)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) || errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return &d, nil
}

// RolePermissionList returns the effective permission set for a membership slug.
func RolePermissionList(projectID int, role string) []string {
	role = strings.TrimSpace(strings.ToLower(role))
	if role == RoleOwner {
		return AllProjectPerms()
	}
	def := ResolveRoleDef(projectID, role)
	if def == nil {
		return nil
	}
	if def.Permissions == nil {
		return []string{}
	}
	return def.Permissions
}

// RoleDisplayName returns the human label for a membership slug.
func RoleDisplayName(projectID int, role string) string {
	def := ResolveRoleDef(projectID, role)
	if def == nil || strings.TrimSpace(def.Name) == "" {
		return role
	}
	return def.Name
}

// HasProjectPerm reports whether a membership role includes perm.
func HasProjectPerm(projectID int, role, perm string) bool {
	role = strings.TrimSpace(strings.ToLower(role))
	perm = strings.TrimSpace(strings.ToLower(perm))
	if role == "" || perm == "" {
		return false
	}
	if role == RoleOwner {
		return ValidProjectPerm(perm)
	}
	return permListContains(RolePermissionList(projectID, role), perm)
}

// RoleCanWriteTask is the project-aware write check. Personal tasks use projectID 0.
func RoleCanWriteTask(projectID int, role string) bool {
	if role == RoleOwner || role == RoleEditor {
		return true
	}
	if role == "" || role == RoleViewer {
		return false
	}
	return hasAnyWritePerm(RolePermissionList(projectID, role))
}

// SiteRoleSlugTaken reports whether a site-level slug already exists.
func SiteRoleSlugTaken(slug string, exceptID int) (bool, error) {
	pool, err := OpenDatabase()
	if err != nil {
		return false, err
	}
	defer CloseDatabase(pool)
	var n int
	err = pool.QueryRow(context.Background(),
		`SELECT COUNT(*) FROM project_role_defs WHERE project_id IS NULL AND slug = $1 AND id <> $2`,
		slug, exceptID).Scan(&n)
	return n > 0, err
}

// ProjectRoleSlugTaken reports whether slug exists as a site role or this project's custom role.
func ProjectRoleSlugTaken(projectID int, slug string, exceptID int) (bool, error) {
	pool, err := OpenDatabase()
	if err != nil {
		return false, err
	}
	defer CloseDatabase(pool)
	var n int
	err = pool.QueryRow(context.Background(), `
		SELECT COUNT(*) FROM project_role_defs
		WHERE slug = $1 AND id <> $2 AND (project_id IS NULL OR project_id = $3)`,
		slug, exceptID, projectID).Scan(&n)
	return n > 0, err
}

// CountProjectCustomRoles returns how many project-defined roles exist.
func CountProjectCustomRoles(projectID int) (int, error) {
	pool, err := OpenDatabase()
	if err != nil {
		return 0, err
	}
	defer CloseDatabase(pool)
	var n int
	err = pool.QueryRow(context.Background(),
		`SELECT COUNT(*) FROM project_role_defs WHERE project_id = $1`, projectID).Scan(&n)
	return n, err
}

// CountMembersWithRole counts memberships using slug, optionally limited to one project.
func CountMembersWithRole(slug string, projectID int) (int, error) {
	pool, err := OpenDatabase()
	if err != nil {
		return 0, err
	}
	defer CloseDatabase(pool)
	var n int
	if projectID > 0 {
		err = pool.QueryRow(context.Background(),
			`SELECT COUNT(*) FROM project_members WHERE role = $1 AND project_id = $2`,
			slug, projectID).Scan(&n)
	} else {
		err = pool.QueryRow(context.Background(),
			`SELECT COUNT(*) FROM project_members WHERE role = $1`, slug).Scan(&n)
	}
	return n, err
}

// CountInvitesWithRole counts pending invites using slug.
func CountInvitesWithRole(slug string, projectID int) (int, error) {
	pool, err := OpenDatabase()
	if err != nil {
		return 0, err
	}
	defer CloseDatabase(pool)
	var n int
	if projectID > 0 {
		err = pool.QueryRow(context.Background(),
			`SELECT COUNT(*) FROM project_invites WHERE role = $1 AND project_id = $2 AND accepted_at IS NULL`,
			slug, projectID).Scan(&n)
	} else {
		err = pool.QueryRow(context.Background(),
			`SELECT COUNT(*) FROM project_invites WHERE role = $1 AND accepted_at IS NULL`, slug).Scan(&n)
	}
	return n, err
}

// CreateProjectRoleDef inserts a site or project role.
func CreateProjectRoleDef(projectID *int, slug, name, description string, perms []string, system bool, sortOrder int) (*ProjectRoleDef, error) {
	perms, err := normalizePermList(perms)
	if err != nil {
		return nil, err
	}
	if perms == nil {
		perms = []string{}
	}
	pool, err := OpenDatabase()
	if err != nil {
		return nil, err
	}
	defer CloseDatabase(pool)

	var d ProjectRoleDef
	err = scanProjectRoleDef(pool.QueryRow(context.Background(), `
		INSERT INTO project_role_defs (project_id, slug, name, description, permissions, is_system, sort_order)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		RETURNING id, project_id, slug, name, COALESCE(description, ''), COALESCE(permissions, '{}'),
		          COALESCE(is_system, false), COALESCE(sort_order, 0), created_at`,
		projectID, slug, name, description, perms, system, sortOrder), &d)
	if err != nil {
		return nil, err
	}
	if projectID == nil {
		InvalidateSiteRoleCache()
	}
	return &d, nil
}

// UpdateProjectRoleDef patches name, description, permissions, and sort order.
func UpdateProjectRoleDef(id int, name, description *string, perms *[]string, sortOrder *int) (*ProjectRoleDef, error) {
	cur, err := GetProjectRoleDef(id)
	if err != nil || cur == nil {
		return nil, fmt.Errorf("role not found")
	}
	newName := cur.Name
	newDesc := cur.Description
	newPerms := cur.Permissions
	newOrder := cur.SortOrder
	if name != nil {
		newName = strings.TrimSpace(*name)
	}
	if description != nil {
		newDesc = strings.TrimSpace(*description)
	}
	if perms != nil {
		normalized, err := normalizePermList(*perms)
		if err != nil {
			return nil, err
		}
		newPerms = normalized
	}
	if sortOrder != nil {
		newOrder = *sortOrder
	}

	pool, err := OpenDatabase()
	if err != nil {
		return nil, err
	}
	defer CloseDatabase(pool)

	var d ProjectRoleDef
	err = scanProjectRoleDef(pool.QueryRow(context.Background(), `
		UPDATE project_role_defs
		SET name = $2, description = $3, permissions = $4, sort_order = $5
		WHERE id = $1
		RETURNING id, project_id, slug, name, COALESCE(description, ''), COALESCE(permissions, '{}'),
		          COALESCE(is_system, false), COALESCE(sort_order, 0), created_at`,
		id, newName, newDesc, newPerms, newOrder), &d)
	if err != nil {
		return nil, err
	}
	if cur.ProjectID == nil {
		InvalidateSiteRoleCache()
	}
	return &d, nil
}

// DeleteProjectRoleDef removes a non-system role.
func DeleteProjectRoleDef(id int) error {
	cur, err := GetProjectRoleDef(id)
	if err != nil || cur == nil {
		return fmt.Errorf("role not found")
	}
	if cur.IsSystem {
		return fmt.Errorf("cannot delete a built-in role")
	}
	pool, err := OpenDatabase()
	if err != nil {
		return err
	}
	defer CloseDatabase(pool)
	tag, err := pool.Exec(context.Background(),
		`DELETE FROM project_role_defs WHERE id = $1 AND is_system = FALSE`, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("role not found")
	}
	if cur.ProjectID == nil {
		InvalidateSiteRoleCache()
	}
	return nil
}

// GetStatusGate returns gates for a status, or empty lists when unset.
func GetStatusGate(statusID int) (*ProjectStatusGate, error) {
	pool, err := OpenDatabase()
	if err != nil {
		return nil, err
	}
	defer CloseDatabase(pool)

	var g ProjectStatusGate
	err = pool.QueryRow(context.Background(),
		`SELECT status_id, COALESCE(enter_role_slugs, '{}'), COALESCE(leave_role_slugs, '{}')
		 FROM project_status_gates WHERE status_id = $1`, statusID).Scan(
		&g.StatusID, &g.EnterRoleSlugs, &g.LeaveRoleSlugs)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) || errors.Is(err, sql.ErrNoRows) {
			return &ProjectStatusGate{StatusID: statusID, EnterRoleSlugs: []string{}, LeaveRoleSlugs: []string{}}, nil
		}
		return nil, err
	}
	if g.EnterRoleSlugs == nil {
		g.EnterRoleSlugs = []string{}
	}
	if g.LeaveRoleSlugs == nil {
		g.LeaveRoleSlugs = []string{}
	}
	return &g, nil
}

// ListStatusGatesForProject maps status id to gate.
func ListStatusGatesForProject(projectID int) (map[int]ProjectStatusGate, error) {
	pool, err := OpenDatabase()
	if err != nil {
		return nil, err
	}
	defer CloseDatabase(pool)

	rows, err := pool.Query(context.Background(), `
		SELECT g.status_id, COALESCE(g.enter_role_slugs, '{}'), COALESCE(g.leave_role_slugs, '{}')
		FROM project_status_gates g
		JOIN project_statuses s ON s.id = g.status_id
		WHERE s.project_id = $1`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make(map[int]ProjectStatusGate)
	for rows.Next() {
		var g ProjectStatusGate
		if err := rows.Scan(&g.StatusID, &g.EnterRoleSlugs, &g.LeaveRoleSlugs); err != nil {
			return nil, err
		}
		if g.EnterRoleSlugs == nil {
			g.EnterRoleSlugs = []string{}
		}
		if g.LeaveRoleSlugs == nil {
			g.LeaveRoleSlugs = []string{}
		}
		out[g.StatusID] = g
	}
	return out, rows.Err()
}

// UpsertStatusGate stores enter/leave role lists for a status.
func UpsertStatusGate(statusID int, enter, leave []string) (*ProjectStatusGate, error) {
	if enter == nil {
		enter = []string{}
	}
	if leave == nil {
		leave = []string{}
	}
	pool, err := OpenDatabase()
	if err != nil {
		return nil, err
	}
	defer CloseDatabase(pool)

	var g ProjectStatusGate
	err = pool.QueryRow(context.Background(), `
		INSERT INTO project_status_gates (status_id, enter_role_slugs, leave_role_slugs)
		VALUES ($1, $2, $3)
		ON CONFLICT (status_id) DO UPDATE
		SET enter_role_slugs = EXCLUDED.enter_role_slugs,
		    leave_role_slugs = EXCLUDED.leave_role_slugs
		RETURNING status_id, COALESCE(enter_role_slugs, '{}'), COALESCE(leave_role_slugs, '{}')`,
		statusID, enter, leave).Scan(&g.StatusID, &g.EnterRoleSlugs, &g.LeaveRoleSlugs)
	if err != nil {
		return nil, err
	}
	if g.EnterRoleSlugs == nil {
		g.EnterRoleSlugs = []string{}
	}
	if g.LeaveRoleSlugs == nil {
		g.LeaveRoleSlugs = []string{}
	}
	return &g, nil
}

func slugInList(slugs []string, slug string) bool {
	for _, s := range slugs {
		if strings.EqualFold(strings.TrimSpace(s), slug) {
			return true
		}
	}
	return false
}

// StatusMoveAllowedByGate reports whether role may leave fromID and enter toID.
// Empty lists are unrestricted. Owner / project:manage is not applied here.
func StatusMoveAllowedByGate(fromGate, toGate *ProjectStatusGate, role string) (leaveOK, enterOK bool) {
	leaveOK = true
	enterOK = true
	if fromGate != nil && len(fromGate.LeaveRoleSlugs) > 0 {
		leaveOK = slugInList(fromGate.LeaveRoleSlugs, role)
	}
	if toGate != nil && len(toGate.EnterRoleSlugs) > 0 {
		enterOK = slugInList(toGate.EnterRoleSlugs, role)
	}
	return leaveOK, enterOK
}

// ValidInviteRole reports whether a slug can be used on an invite without a project context.
func ValidInviteRole(role string) bool {
	role = strings.TrimSpace(strings.ToLower(role))
	if role == "" || role == RoleOwner {
		return false
	}
	if role == RoleEditor || role == RoleViewer {
		return true
	}
	def := ResolveRoleDef(0, role)
	return def != nil && def.Slug != RoleOwner
}

// ValidInviteRoleForProject reports whether a project can assign slug to a member.
func ValidInviteRoleForProject(projectID int, role string) bool {
	role = strings.TrimSpace(strings.ToLower(role))
	if role == "" || role == RoleOwner {
		return false
	}
	return ResolveRoleDef(projectID, role) != nil
}

// ValidMemberRole reports whether a slug is a known membership role.
func ValidMemberRole(role string) bool {
	role = strings.TrimSpace(strings.ToLower(role))
	if role == RoleOwner || role == RoleEditor || role == RoleViewer {
		return true
	}
	return ResolveRoleDef(0, role) != nil
}

// RoleCanWrite reports coarse write access. Prefer RoleCanWriteTask when a project id is known.
func RoleCanWrite(role string) bool {
	return RoleCanWriteTask(0, role)
}

// RoleCanManageProject reports whether the role may change settings for this project.
func RoleCanManageProject(projectID int, role string) bool {
	if role == RoleOwner {
		return true
	}
	return HasProjectPerm(projectID, role, PermProjectManage)
}

// RoleCanManage reports whether the role may change project settings and membership.
func RoleCanManage(role string) bool {
	return RoleCanManageProject(0, role)
}
