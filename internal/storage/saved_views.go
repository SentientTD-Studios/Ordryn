package storage

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	// MaxSavedViewsPerUser caps a user's personal saved views.
	MaxSavedViewsPerUser = 50
	// MaxSharedViewsPerOrg caps queries shared with one organization.
	MaxSharedViewsPerOrg = 100
	// MaxSiteViews caps queries shared with the whole site.
	MaxSiteViews = 100
)

// Who a saved view is shared with. Project filtering lives in the query
// definition; the scope only decides who can open the view.
const (
	SavedViewScopePersonal = "personal"
	SavedViewScopeOrg      = "organization"
	SavedViewScopeSite     = "site"
)

// ValidSavedViewScope reports whether s is a known scope.
func ValidSavedViewScope(s string) bool {
	return s == SavedViewScopePersonal || s == SavedViewScopeOrg || s == SavedViewScopeSite
}

var (
	ErrSavedViewNotFound     = errors.New("saved view not found")
	ErrSavedViewLimit        = errors.New("maximum saved views reached")
	ErrSavedViewNameConflict = errors.New("a saved view with this name already exists")
)

// SavedViewFilter mirrors FilterContext fields (excluding page). It is the
// version 1 form of a saved view; newer views carry a query definition.
type SavedViewFilter struct {
	Project   string `json:"project"`
	Status    string `json:"status"`
	Due       string `json:"due"`
	Completed string `json:"completed"`
	Priority  string `json:"priority"`
	Tag       string `json:"tag"`
	Sort      string `json:"sort"`
	Search    string `json:"search"`
	Claimed   string `json:"claimed"`
	// DueFrom and DueTo bound a custom due-date range (YYYY-MM-DD).
	DueFrom string `json:"due_from,omitempty"`
	DueTo   string `json:"due_to,omitempty"`
}

// SavedView is a named query. UserID is its owner (author). A personal view is
// seen only by its owner; an organization view by the organization's members;
// a site view by everyone. Each viewer runs it with their own access.
type SavedView struct {
	ID     int
	UserID int
	Scope  string
	// OrganizationID is set for organization views only.
	OrganizationID *int
	Name           string
	Filter         SavedViewFilter
	// Definition is the query definition JSON, or nil for a version 1 view
	// that only has Filter.
	Definition json.RawMessage
	SortOrder  int
	CreatedAt  time.Time
	UpdatedAt  time.Time
	// AuthorName and OrganizationName are filled by listings.
	AuthorName       string
	OrganizationName string
	// OnDashboard reports whether the listing user pinned it to their dashboard.
	OnDashboard bool
}

// IsShared reports whether people other than the owner can open the view.
func (v SavedView) IsShared() bool {
	return v.Scope == SavedViewScopeOrg || v.Scope == SavedViewScopeSite
}

// OrgID returns the organization of an organization view, or 0.
func (v SavedView) OrgID() int {
	if v.Scope == SavedViewScopeOrg && v.OrganizationID != nil {
		return *v.OrganizationID
	}
	return 0
}

// CreateSavedViewsTable ensures the saved_views table exists.
func CreateSavedViewsTable() error {
	pool, err := OpenDatabase()
	if err != nil {
		return err
	}
	defer CloseDatabase(pool)

	_, err = pool.Exec(context.Background(), `
		CREATE TABLE IF NOT EXISTS saved_views (
			id SERIAL PRIMARY KEY,
			user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			name TEXT NOT NULL,
			filter_json JSONB NOT NULL DEFAULT '{}',
			sort_order INTEGER NOT NULL DEFAULT 0,
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			UNIQUE (user_id, name)
		);
		ALTER TABLE saved_views
			ADD COLUMN IF NOT EXISTS updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW();
	`)
	if err != nil {
		return fmt.Errorf("failed to create saved_views table: %w", err)
	}
	_, err = pool.Exec(context.Background(),
		`CREATE INDEX IF NOT EXISTS idx_saved_views_user_id ON saved_views(user_id)`)
	if err != nil {
		return fmt.Errorf("failed to create saved_views index: %w", err)
	}
	return nil
}

// MigrateSavedViewsForQueries adds query definitions, sharing scopes, and
// dashboard pins. Names are unique per owner among personal views, per
// organization among organization views, and site-wide among site views.
// Safe to re-run.
func MigrateSavedViewsForQueries() error {
	pool, err := OpenDatabase()
	if err != nil {
		return err
	}
	defer CloseDatabase(pool)

	stmts := []string{
		`ALTER TABLE saved_views ADD COLUMN IF NOT EXISTS definition JSONB`,
		`ALTER TABLE saved_views ADD COLUMN IF NOT EXISTS scope TEXT NOT NULL DEFAULT 'personal'`,
		`ALTER TABLE saved_views ADD COLUMN IF NOT EXISTS organization_id INTEGER REFERENCES organizations(id) ON DELETE CASCADE`,
		`ALTER TABLE saved_views DROP CONSTRAINT IF EXISTS saved_views_user_id_name_key`,
		// Replaced by idx_saved_views_personal_name_v2, keyed on scope.
		`DROP INDEX IF EXISTS idx_saved_views_personal_name`,
		`DO $$ BEGIN
			ALTER TABLE saved_views ADD CONSTRAINT saved_views_scope_check CHECK (
				(scope = 'personal' AND organization_id IS NULL)
				OR (scope = 'organization' AND organization_id IS NOT NULL)
				OR (scope = 'site' AND organization_id IS NULL));
		EXCEPTION WHEN duplicate_object THEN NULL; END $$`,
		`CREATE INDEX IF NOT EXISTS idx_saved_views_org_id ON saved_views(organization_id) WHERE organization_id IS NOT NULL`,
		`CREATE TABLE IF NOT EXISTS saved_view_dashboard (
			user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			saved_view_id INTEGER NOT NULL REFERENCES saved_views(id) ON DELETE CASCADE,
			position INTEGER NOT NULL DEFAULT 0,
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			PRIMARY KEY (user_id, saved_view_id)
		)`,
	}
	for _, s := range stmts {
		if _, err := pool.Exec(context.Background(), s); err != nil {
			return fmt.Errorf("saved views for queries: %w", err)
		}
	}
	if err := migrateProjectSharedViews(pool); err != nil {
		return err
	}
	for _, s := range []string{
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_saved_views_personal_name_v2
			ON saved_views (user_id, name) WHERE scope = 'personal'`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_saved_views_org_name
			ON saved_views (organization_id, name) WHERE scope = 'organization'`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_saved_views_site_name
			ON saved_views (name) WHERE scope = 'site'`,
	} {
		if _, err := pool.Exec(context.Background(), s); err != nil {
			return fmt.Errorf("saved views for queries: %w", err)
		}
	}
	return nil
}

// migrateProjectSharedViews converts views shared on a project (the old
// saved_views.project_id) into the scope model: the project moves into the
// definition's scope, and the view is shared with the project's organization,
// or becomes its author's personal view when the project has none. Names that
// collide in their new scope get a suffix. The project_id column is dropped
// once every row is converted.
func migrateProjectSharedViews(pool *pgxpool.Pool) error {
	ctx := context.Background()
	var hasColumn bool
	if err := pool.QueryRow(ctx, `
		SELECT EXISTS (SELECT 1 FROM information_schema.columns
			WHERE table_schema = current_schema() AND table_name = 'saved_views' AND column_name = 'project_id')`).Scan(&hasColumn); err != nil {
		return fmt.Errorf("check saved_views.project_id: %w", err)
	}
	if !hasColumn {
		return nil
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	rows, err := tx.Query(ctx, `
		SELECT sv.id, sv.user_id, sv.project_id, sv.name, sv.definition, p.organization_id, COALESCE(p.name, '')
		FROM saved_views sv JOIN projects p ON p.id = sv.project_id
		WHERE sv.project_id IS NOT NULL ORDER BY sv.id`)
	if err != nil {
		return fmt.Errorf("list project shared views: %w", err)
	}
	type legacyView struct {
		id, userID, projectID int
		name, projectName     string
		def                   []byte
		orgID                 *int
	}
	var list []legacyView
	for rows.Next() {
		var v legacyView
		if err := rows.Scan(&v.id, &v.userID, &v.projectID, &v.name, &v.def, &v.orgID, &v.projectName); err != nil {
			rows.Close()
			return err
		}
		list = append(list, v)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	for _, v := range list {
		def := map[string]any{}
		if len(v.def) > 0 {
			_ = json.Unmarshal(v.def, &def)
		}
		scope, _ := def["scope"].(map[string]any)
		if scope == nil {
			scope = map[string]any{}
		}
		scope["projects"] = []int{v.projectID}
		scope["include_personal"] = false
		delete(scope, "organizations")
		def["scope"] = scope
		rawDef, err := json.Marshal(def)
		if err != nil {
			return err
		}
		newScope, nameTaken := SavedViewScopePersonal, `SELECT EXISTS (SELECT 1 FROM saved_views WHERE scope = 'personal' AND project_id IS NULL AND user_id = $1 AND name = $2 AND id <> $3)`
		var key any = v.userID
		if v.orgID != nil {
			newScope = SavedViewScopeOrg
			key = *v.orgID
			nameTaken = `SELECT EXISTS (SELECT 1 FROM saved_views WHERE scope = 'organization' AND organization_id = $1 AND name = $2 AND id <> $3)`
		}
		name := v.name
		for i := 1; ; i++ {
			var taken bool
			if err := tx.QueryRow(ctx, nameTaken, key, name, v.id).Scan(&taken); err != nil {
				return err
			}
			if !taken {
				break
			}
			label := truncateRunes(v.projectName, 30)
			suffix := " (" + label + ")"
			if i > 1 {
				suffix = fmt.Sprintf(" (%s %d)", label, i)
			}
			name = truncateRunes(v.name, 80-len([]rune(suffix))) + suffix
		}
		if _, err := tx.Exec(ctx, `
			UPDATE saved_views SET scope = $2, organization_id = $3, name = $4, definition = $5, project_id = NULL
			WHERE id = $1`, v.id, newScope, v.orgID, name, rawDef); err != nil {
			return fmt.Errorf("convert shared view %d: %w", v.id, err)
		}
	}
	for _, s := range []string{
		`DROP INDEX IF EXISTS idx_saved_views_project_name`,
		`DROP INDEX IF EXISTS idx_saved_views_project_id`,
		`ALTER TABLE saved_views DROP COLUMN project_id`,
	} {
		if _, err := tx.Exec(ctx, s); err != nil {
			return fmt.Errorf("drop saved_views.project_id: %w", err)
		}
	}
	return tx.Commit(ctx)
}

const savedViewCols = `sv.id, sv.user_id, sv.scope, sv.organization_id, sv.name, sv.filter_json, sv.definition, sv.sort_order, sv.created_at, sv.updated_at`

// savedViewVisibleSQL matches views user $1 can open: their own personal
// views, views shared with an organization they belong to, and site views.
const savedViewVisibleSQL = `(
	(sv.scope = 'personal' AND sv.user_id = $1)
	OR (sv.scope = 'organization' AND EXISTS (
		SELECT 1 FROM organization_members svm
		WHERE svm.organization_id = sv.organization_id AND svm.user_id = $1))
	OR sv.scope = 'site'
)`

// ListSavedViewsForUser returns a user's personal views ordered by sort_order then name.
func ListSavedViewsForUser(userID int) ([]SavedView, error) {
	return listSavedViews(userID, `sv.scope = 'personal' AND sv.user_id = $1`)
}

// ListVisibleSavedViews returns the user's personal views, then views shared
// with their organizations, then site views.
func ListVisibleSavedViews(userID int) ([]SavedView, error) {
	return listSavedViews(userID, savedViewVisibleSQL)
}

const savedViewJoins = `
		 FROM saved_views sv
		 LEFT JOIN users u ON u.id = sv.user_id
		 LEFT JOIN organizations o ON o.id = sv.organization_id`

const savedViewAuthorCols = `COALESCE(NULLIF(u.full_name, ''), NULLIF(u.user_name, ''), u.email, ''), COALESCE(o.name, '')`

func listSavedViews(userID int, where string) ([]SavedView, error) {
	pool, err := OpenDatabase()
	if err != nil {
		return nil, err
	}
	defer CloseDatabase(pool)

	rows, err := pool.Query(context.Background(),
		`SELECT `+savedViewCols+`, `+savedViewAuthorCols+`,
			EXISTS (SELECT 1 FROM saved_view_dashboard d WHERE d.saved_view_id = sv.id AND d.user_id = $1)
		`+savedViewJoins+`
		 WHERE `+where+`
		 ORDER BY CASE sv.scope WHEN 'personal' THEN 0 WHEN 'organization' THEN 1 ELSE 2 END,
		          LOWER(COALESCE(o.name, '')), sv.organization_id,
		          sv.sort_order ASC, LOWER(sv.name) ASC, sv.id ASC`, userID)
	if err != nil {
		return nil, fmt.Errorf("failed to list saved views: %w", err)
	}
	defer rows.Close()

	out := make([]SavedView, 0)
	for rows.Next() {
		var view SavedView
		var rawFilter, rawDef []byte
		if err := rows.Scan(&view.ID, &view.UserID, &view.Scope, &view.OrganizationID, &view.Name, &rawFilter, &rawDef,
			&view.SortOrder, &view.CreatedAt, &view.UpdatedAt,
			&view.AuthorName, &view.OrganizationName, &view.OnDashboard); err != nil {
			return nil, err
		}
		if err := finishSavedView(&view, rawFilter, rawDef); err != nil {
			return nil, err
		}
		out = append(out, view)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("failed to list saved views: %w", err)
	}
	return out, nil
}

// CountSavedViewsForUser returns how many personal saved views a user has.
func CountSavedViewsForUser(userID int) (int, error) {
	pool, err := OpenDatabase()
	if err != nil {
		return 0, err
	}
	defer CloseDatabase(pool)

	var count int
	err = pool.QueryRow(context.Background(),
		`SELECT COUNT(*) FROM saved_views WHERE user_id = $1 AND scope = 'personal'`, userID).Scan(&count)
	return count, err
}

// GetSavedViewByID returns a personal saved view owned by userID.
func GetSavedViewByID(id, userID int) (*SavedView, error) {
	view, err := GetSavedView(id)
	if err != nil {
		return nil, err
	}
	if view.Scope != SavedViewScopePersonal || view.UserID != userID {
		return nil, ErrSavedViewNotFound
	}
	return view, nil
}

// GetVisibleSavedView returns a view userID can open (personal or shared), or
// ErrSavedViewNotFound whether it is missing or hidden.
func GetVisibleSavedView(id, userID int) (*SavedView, error) {
	pool, err := OpenDatabase()
	if err != nil {
		return nil, err
	}
	defer CloseDatabase(pool)
	var ok bool
	if err := pool.QueryRow(context.Background(),
		`SELECT EXISTS (SELECT 1 FROM saved_views sv WHERE sv.id = $2 AND `+savedViewVisibleSQL+`)`,
		userID, id).Scan(&ok); err != nil {
		return nil, fmt.Errorf("failed to get saved view: %w", err)
	}
	if !ok {
		return nil, ErrSavedViewNotFound
	}
	view, err := GetSavedView(id)
	if err != nil {
		return nil, err
	}
	_ = pool.QueryRow(context.Background(),
		`SELECT EXISTS (SELECT 1 FROM saved_view_dashboard WHERE saved_view_id = $1 AND user_id = $2)`,
		id, userID).Scan(&view.OnDashboard)
	return view, nil
}

// GetSavedView returns a view by id with no access check. Callers must
// authorize it.
func GetSavedView(id int) (*SavedView, error) {
	pool, err := OpenDatabase()
	if err != nil {
		return nil, err
	}
	defer CloseDatabase(pool)

	var view SavedView
	var rawFilter, rawDef []byte
	err = pool.QueryRow(context.Background(),
		`SELECT `+savedViewCols+`, `+savedViewAuthorCols+savedViewJoins+`
		 WHERE sv.id = $1`, id).Scan(&view.ID, &view.UserID, &view.Scope, &view.OrganizationID, &view.Name, &rawFilter, &rawDef,
		&view.SortOrder, &view.CreatedAt, &view.UpdatedAt, &view.AuthorName, &view.OrganizationName)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrSavedViewNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get saved view: %w", err)
	}
	if err := finishSavedView(&view, rawFilter, rawDef); err != nil {
		return nil, err
	}
	return &view, nil
}

func finishSavedView(view *SavedView, rawFilter, rawDef []byte) error {
	if view.Scope != SavedViewScopeOrg {
		view.OrganizationID = nil
	}
	if err := json.Unmarshal(rawFilter, &view.Filter); err != nil {
		return fmt.Errorf("failed to decode saved view filter: %w", err)
	}
	if len(rawDef) > 0 && string(rawDef) != "null" {
		view.Definition = json.RawMessage(rawDef)
	}
	return nil
}

// SavedViewInput creates a view.
type SavedViewInput struct {
	UserID int
	// Scope is personal when empty; OrganizationID is required for an
	// organization view.
	Scope          string
	OrganizationID int
	Name           string
	Filter         SavedViewFilter
	Definition     json.RawMessage
	// SortOrder nil places the view after the others.
	SortOrder *int
}

// CreateSavedView inserts a new personal saved view for the user.
func CreateSavedView(userID int, name string, filter SavedViewFilter, sortOrder int) (*SavedView, error) {
	return CreateSavedViewWithSort(userID, name, filter, &sortOrder)
}

// CreateSavedViewWithSort inserts a personal view with only a version 1 filter.
func CreateSavedViewWithSort(userID int, name string, filter SavedViewFilter, sortOrder *int) (*SavedView, error) {
	return CreateSavedViewFull(SavedViewInput{UserID: userID, Name: name, Filter: filter, SortOrder: sortOrder})
}

// CreateSavedViewFull inserts a view and atomically enforces the limit of
// its scope: per owner (personal), per organization, or site-wide.
func CreateSavedViewFull(in SavedViewInput) (*SavedView, error) {
	pool, err := OpenDatabase()
	if err != nil {
		return nil, err
	}
	defer CloseDatabase(pool)

	ctx := context.Background()
	tx, err := pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to begin saved view transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	scope := in.Scope
	if scope == "" {
		scope = SavedViewScopePersonal
	}
	count, limit, err := lockSavedViewScope(ctx, tx, in.UserID, scope, in.OrganizationID)
	if err != nil {
		return nil, err
	}
	if count >= limit {
		return nil, ErrSavedViewLimit
	}
	nextSortOrder := count
	if in.SortOrder != nil {
		nextSortOrder = *in.SortOrder
	}

	rawFilter, err := json.Marshal(in.Filter)
	if err != nil {
		return nil, fmt.Errorf("failed to encode saved view filter: %w", err)
	}
	var rawDef any
	if len(in.Definition) > 0 {
		rawDef = []byte(in.Definition)
	}
	var orgID any
	if scope == SavedViewScopeOrg {
		orgID = in.OrganizationID
	}

	var id int
	err = tx.QueryRow(ctx,
		`INSERT INTO saved_views (user_id, scope, organization_id, name, filter_json, definition, sort_order)
		 VALUES ($1, $2, $3, $4, $5, $6, $7) RETURNING id`,
		in.UserID, scope, orgID, in.Name, rawFilter, rawDef, nextSortOrder).Scan(&id)
	if err != nil {
		if isSavedViewUniqueViolation(err) {
			return nil, ErrSavedViewNameConflict
		}
		return nil, fmt.Errorf("failed to create saved view: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("failed to commit saved view: %w", err)
	}
	return GetSavedView(id)
}

// savedViewSiteLockKey serializes site-view creation so the cap holds.
const savedViewSiteLockKey = 0x5157_5349 // "QWSI"

// lockSavedViewScope locks the row a scope's limit counts against (the owner,
// the organization, or a site-wide advisory lock) and returns how many views
// the scope has and its limit.
func lockSavedViewScope(ctx context.Context, tx pgx.Tx, userID int, scope string, orgID int) (int, int, error) {
	var count int
	switch scope {
	case SavedViewScopePersonal:
		var id int
		if err := tx.QueryRow(ctx, "SELECT id FROM users WHERE id = $1 FOR UPDATE", userID).Scan(&id); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return 0, 0, ErrSavedViewNotFound
			}
			return 0, 0, fmt.Errorf("failed to lock saved view owner: %w", err)
		}
		err := tx.QueryRow(ctx, "SELECT COUNT(*) FROM saved_views WHERE user_id = $1 AND scope = 'personal'", userID).Scan(&count)
		return count, MaxSavedViewsPerUser, err
	case SavedViewScopeOrg:
		var id int
		if err := tx.QueryRow(ctx, "SELECT id FROM organizations WHERE id = $1 FOR UPDATE", orgID).Scan(&id); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return 0, 0, ErrSavedViewNotFound
			}
			return 0, 0, fmt.Errorf("failed to lock saved view organization: %w", err)
		}
		err := tx.QueryRow(ctx, "SELECT COUNT(*) FROM saved_views WHERE scope = 'organization' AND organization_id = $1", orgID).Scan(&count)
		return count, MaxSharedViewsPerOrg, err
	case SavedViewScopeSite:
		if _, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock($1)", savedViewSiteLockKey); err != nil {
			return 0, 0, fmt.Errorf("failed to lock site views: %w", err)
		}
		err := tx.QueryRow(ctx, "SELECT COUNT(*) FROM saved_views WHERE scope = 'site'").Scan(&count)
		return count, MaxSiteViews, err
	}
	return 0, 0, fmt.Errorf("unknown saved view scope %q", scope)
}

// MoveSavedView changes who a view is shared with, enforcing the new scope's
// limit and name uniqueness. Callers must authorize it.
func MoveSavedView(id int, scope string, orgID int) (*SavedView, error) {
	view, err := GetSavedView(id)
	if err != nil {
		return nil, err
	}
	if view.Scope == scope && view.OrgID() == orgID {
		return view, nil
	}
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
	count, limit, err := lockSavedViewScope(ctx, tx, view.UserID, scope, orgID)
	if err != nil {
		return nil, err
	}
	if count >= limit {
		return nil, ErrSavedViewLimit
	}
	var orgArg any
	if scope == SavedViewScopeOrg {
		orgArg = orgID
	}
	if _, err := tx.Exec(ctx,
		`UPDATE saved_views SET scope = $2, organization_id = $3, updated_at = NOW() WHERE id = $1`,
		id, scope, orgArg); err != nil {
		if isSavedViewUniqueViolation(err) {
			return nil, ErrSavedViewNameConflict
		}
		return nil, fmt.Errorf("failed to move saved view: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return GetSavedView(id)
}

// UpdateSavedView updates name and/or filter for an existing personal saved view.
func UpdateSavedView(id, userID int, name string, filter *SavedViewFilter) error {
	_, err := PatchSavedView(id, userID, &name, filter, nil)
	return err
}

// PatchSavedView applies the supplied fields to a user-owned personal view.
func PatchSavedView(id, userID int, name *string, filter *SavedViewFilter, sortOrder *int) (*SavedView, error) {
	if _, err := GetSavedViewByID(id, userID); err != nil {
		return nil, err
	}
	return PatchSavedViewFields(id, SavedViewPatch{Name: name, Filter: filter, SortOrder: sortOrder})
}

// SavedViewPatch lists fields to change; nil leaves a field as it is.
type SavedViewPatch struct {
	Name       *string
	Filter     *SavedViewFilter
	Definition json.RawMessage
	SortOrder  *int
}

// PatchSavedViewFields updates a view by id. Callers must authorize it.
func PatchSavedViewFields(id int, patch SavedViewPatch) (*SavedView, error) {
	pool, err := OpenDatabase()
	if err != nil {
		return nil, err
	}
	defer CloseDatabase(pool)

	var rawFilter any
	if patch.Filter != nil {
		encoded, err := json.Marshal(patch.Filter)
		if err != nil {
			return nil, fmt.Errorf("failed to encode saved view filter: %w", err)
		}
		rawFilter = encoded
	}
	var rawDef any
	if len(patch.Definition) > 0 {
		rawDef = []byte(patch.Definition)
	}

	tag, err := pool.Exec(context.Background(), `
		UPDATE saved_views
		SET name = COALESCE($1, name),
			filter_json = COALESCE($2, filter_json),
			definition = COALESCE($3, definition),
			sort_order = COALESCE($4, sort_order),
			updated_at = NOW()
		WHERE id = $5
	`, patch.Name, rawFilter, rawDef, patch.SortOrder, id)
	if err != nil {
		if isSavedViewUniqueViolation(err) {
			return nil, ErrSavedViewNameConflict
		}
		return nil, fmt.Errorf("failed to update saved view: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return nil, ErrSavedViewNotFound
	}
	return GetSavedView(id)
}

// DeleteSavedView removes a personal saved view owned by the user.
func DeleteSavedView(id, userID int) error {
	if _, err := GetSavedViewByID(id, userID); err != nil {
		return err
	}
	return DeleteSavedViewByID(id)
}

// DeleteSavedViewByID removes a view. Callers must authorize it.
func DeleteSavedViewByID(id int) error {
	pool, err := OpenDatabase()
	if err != nil {
		return err
	}
	defer CloseDatabase(pool)

	result, err := pool.Exec(context.Background(), `DELETE FROM saved_views WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("failed to delete saved view: %w", err)
	}
	if result.RowsAffected() == 0 {
		return ErrSavedViewNotFound
	}
	return nil
}

// SetSavedViewOnDashboard pins or unpins a view on userID's dashboard. The
// caller must have checked the user can open the view.
func SetSavedViewOnDashboard(userID, viewID int, on bool) error {
	pool, err := OpenDatabase()
	if err != nil {
		return err
	}
	defer CloseDatabase(pool)
	ctx := context.Background()
	if !on {
		_, err = pool.Exec(ctx, `DELETE FROM saved_view_dashboard WHERE user_id = $1 AND saved_view_id = $2`, userID, viewID)
		return err
	}
	_, err = pool.Exec(ctx, `
		INSERT INTO saved_view_dashboard (user_id, saved_view_id, position)
		VALUES ($1, $2, (SELECT COALESCE(MAX(position) + 1, 0) FROM saved_view_dashboard WHERE user_id = $1))
		ON CONFLICT (user_id, saved_view_id) DO NOTHING`, userID, viewID)
	return err
}

// ListSavedViewsMissingDefinition returns views that only have a version 1
// filter, for the one-time backfill.
func ListSavedViewsMissingDefinition() ([]SavedView, error) {
	pool, err := OpenDatabase()
	if err != nil {
		return nil, err
	}
	defer CloseDatabase(pool)
	rows, err := pool.Query(context.Background(),
		`SELECT id, user_id, filter_json FROM saved_views WHERE definition IS NULL ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []SavedView{}
	for rows.Next() {
		var v SavedView
		var raw []byte
		if err := rows.Scan(&v.ID, &v.UserID, &raw); err != nil {
			return nil, err
		}
		if json.Unmarshal(raw, &v.Filter) != nil {
			continue
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

// SetSavedViewDefinitionIfMissing stores a backfilled definition unless one
// was written meanwhile.
func SetSavedViewDefinitionIfMissing(id int, def json.RawMessage) error {
	pool, err := OpenDatabase()
	if err != nil {
		return err
	}
	defer CloseDatabase(pool)
	_, err = pool.Exec(context.Background(),
		`UPDATE saved_views SET definition = $1 WHERE id = $2 AND definition IS NULL`, []byte(def), id)
	return err
}

func isSavedViewUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}
