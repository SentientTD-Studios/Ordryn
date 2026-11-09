package storage

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

const (
	// MaxSavedViewsPerUser caps a user's personal saved views.
	MaxSavedViewsPerUser = 50
	// MaxSharedViewsPerProject caps shared queries on one project.
	MaxSharedViewsPerProject = 100
)

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

// SavedView is a named query. A personal view (ProjectID nil) belongs to
// UserID alone; a shared view belongs to ProjectID, and UserID is its author.
type SavedView struct {
	ID        int
	UserID    int
	ProjectID *int
	Name      string
	Filter    SavedViewFilter
	// Definition is the query definition JSON, or nil for a version 1 view
	// that only has Filter.
	Definition json.RawMessage
	SortOrder  int
	CreatedAt  time.Time
	UpdatedAt  time.Time
	// AuthorName and ProjectName are filled by listings.
	AuthorName  string
	ProjectName string
	// OnDashboard reports whether the listing user pinned it to their dashboard.
	OnDashboard bool
}

// IsShared reports whether the view belongs to a project.
func (v SavedView) IsShared() bool { return v.ProjectID != nil && *v.ProjectID > 0 }

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
	return MigrateSavedViewsForQueries()
}

// MigrateSavedViewsForQueries adds query definitions, shared (project) views,
// and dashboard pins. Names are unique per user among personal views and per
// project among shared ones. Safe to re-run.
func MigrateSavedViewsForQueries() error {
	pool, err := OpenDatabase()
	if err != nil {
		return err
	}
	defer CloseDatabase(pool)

	stmts := []string{
		`ALTER TABLE saved_views ADD COLUMN IF NOT EXISTS definition JSONB`,
		`ALTER TABLE saved_views ADD COLUMN IF NOT EXISTS project_id INTEGER REFERENCES projects(id) ON DELETE CASCADE`,
		`ALTER TABLE saved_views DROP CONSTRAINT IF EXISTS saved_views_user_id_name_key`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_saved_views_personal_name
			ON saved_views (user_id, name) WHERE project_id IS NULL`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_saved_views_project_name
			ON saved_views (project_id, name) WHERE project_id IS NOT NULL`,
		`CREATE INDEX IF NOT EXISTS idx_saved_views_project_id ON saved_views(project_id) WHERE project_id IS NOT NULL`,
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
	return nil
}

const savedViewCols = `sv.id, sv.user_id, sv.project_id, sv.name, sv.filter_json, sv.definition, sv.sort_order, sv.created_at, sv.updated_at`

// savedViewVisibleSQL matches views user $1 can open: their own personal
// views, and shared views of projects they own or are a member of.
const savedViewVisibleSQL = `(
	(sv.project_id IS NULL AND sv.user_id = $1)
	OR (sv.project_id IS NOT NULL AND EXISTS (
		SELECT 1 FROM projects svp
		WHERE svp.id = sv.project_id AND (svp.user_id = $1 OR EXISTS (
			SELECT 1 FROM project_members svm WHERE svm.project_id = svp.id AND svm.user_id = $1))
	))
)`

// ListSavedViewsForUser returns a user's personal views ordered by sort_order then name.
func ListSavedViewsForUser(userID int) ([]SavedView, error) {
	return listSavedViews(userID, `sv.project_id IS NULL AND sv.user_id = $1`)
}

// ListVisibleSavedViews returns the user's personal views, then shared views
// of every project they can open.
func ListVisibleSavedViews(userID int) ([]SavedView, error) {
	return listSavedViews(userID, savedViewVisibleSQL)
}

func listSavedViews(userID int, where string) ([]SavedView, error) {
	pool, err := OpenDatabase()
	if err != nil {
		return nil, err
	}
	defer CloseDatabase(pool)

	rows, err := pool.Query(context.Background(),
		`SELECT `+savedViewCols+`,
			COALESCE(NULLIF(u.full_name, ''), NULLIF(u.user_name, ''), u.email, ''),
			COALESCE(p.name, ''),
			EXISTS (SELECT 1 FROM saved_view_dashboard d WHERE d.saved_view_id = sv.id AND d.user_id = $1)
		 FROM saved_views sv
		 LEFT JOIN users u ON u.id = sv.user_id
		 LEFT JOIN projects p ON p.id = sv.project_id
		 WHERE `+where+`
		 ORDER BY (sv.project_id IS NOT NULL), LOWER(COALESCE(p.name, '')), sv.project_id,
		          sv.sort_order ASC, LOWER(sv.name) ASC, sv.id ASC`, userID)
	if err != nil {
		return nil, fmt.Errorf("failed to list saved views: %w", err)
	}
	defer rows.Close()

	out := make([]SavedView, 0)
	for rows.Next() {
		var view SavedView
		var rawFilter, rawDef []byte
		var pid *int
		if err := rows.Scan(&view.ID, &view.UserID, &pid, &view.Name, &rawFilter, &rawDef,
			&view.SortOrder, &view.CreatedAt, &view.UpdatedAt,
			&view.AuthorName, &view.ProjectName, &view.OnDashboard); err != nil {
			return nil, err
		}
		if err := finishSavedView(&view, pid, rawFilter, rawDef); err != nil {
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
		`SELECT COUNT(*) FROM saved_views WHERE user_id = $1 AND project_id IS NULL`, userID).Scan(&count)
	return count, err
}

// GetSavedViewByID returns a personal saved view owned by userID.
func GetSavedViewByID(id, userID int) (*SavedView, error) {
	view, err := GetSavedView(id)
	if err != nil {
		return nil, err
	}
	if view.IsShared() || view.UserID != userID {
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
	var pid *int
	err = pool.QueryRow(context.Background(),
		`SELECT `+savedViewCols+`,
			COALESCE(NULLIF(u.full_name, ''), NULLIF(u.user_name, ''), u.email, ''), COALESCE(p.name, '')
		 FROM saved_views sv
		 LEFT JOIN users u ON u.id = sv.user_id
		 LEFT JOIN projects p ON p.id = sv.project_id
		 WHERE sv.id = $1`, id).Scan(&view.ID, &view.UserID, &pid, &view.Name, &rawFilter, &rawDef,
		&view.SortOrder, &view.CreatedAt, &view.UpdatedAt, &view.AuthorName, &view.ProjectName)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrSavedViewNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get saved view: %w", err)
	}
	if err := finishSavedView(&view, pid, rawFilter, rawDef); err != nil {
		return nil, err
	}
	return &view, nil
}

func finishSavedView(view *SavedView, pid *int, rawFilter, rawDef []byte) error {
	view.ProjectID = pid
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
	UserID     int
	ProjectID  *int // nil for a personal view
	Name       string
	Filter     SavedViewFilter
	Definition json.RawMessage
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

// CreateSavedViewFull inserts a view and atomically enforces the per-user
// (personal) or per-project (shared) limit.
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

	var count, limit int
	if in.ProjectID != nil {
		var pid int
		if err := tx.QueryRow(ctx, "SELECT id FROM projects WHERE id = $1 FOR UPDATE", *in.ProjectID).Scan(&pid); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return nil, ErrSavedViewNotFound
			}
			return nil, fmt.Errorf("failed to lock saved view project: %w", err)
		}
		if err := tx.QueryRow(ctx,
			"SELECT COUNT(*) FROM saved_views WHERE project_id = $1", pid).Scan(&count); err != nil {
			return nil, fmt.Errorf("failed to count saved views: %w", err)
		}
		limit = MaxSharedViewsPerProject
	} else {
		var ownerID int
		if err := tx.QueryRow(ctx,
			"SELECT id FROM users WHERE id = $1 FOR UPDATE", in.UserID).Scan(&ownerID); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return nil, ErrSavedViewNotFound
			}
			return nil, fmt.Errorf("failed to lock saved view owner: %w", err)
		}
		if err := tx.QueryRow(ctx,
			"SELECT COUNT(*) FROM saved_views WHERE user_id = $1 AND project_id IS NULL", in.UserID).Scan(&count); err != nil {
			return nil, fmt.Errorf("failed to count saved views: %w", err)
		}
		limit = MaxSavedViewsPerUser
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

	var id int
	err = tx.QueryRow(ctx,
		`INSERT INTO saved_views (user_id, project_id, name, filter_json, definition, sort_order)
		 VALUES ($1, $2, $3, $4, $5, $6) RETURNING id`,
		in.UserID, in.ProjectID, in.Name, rawFilter, rawDef, nextSortOrder).Scan(&id)
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
