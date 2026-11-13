package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// Ceremony kinds: built-in team ceremonies a site enables and kanban projects opt into.
const (
	CeremonyRefinement = "refinement"
	CeremonyRetro      = "retro"
	CeremonyStandup    = "standup"
)

// CeremonyKinds lists every built-in ceremony, in display order.
var CeremonyKinds = []string{CeremonyRefinement, CeremonyRetro, CeremonyStandup}

// ValidCeremonyKind reports whether kind is a built-in ceremony.
func ValidCeremonyKind(kind string) bool {
	for _, k := range CeremonyKinds {
		if k == kind {
			return true
		}
	}
	return false
}

// Ceremony session statuses.
const (
	CeremonySessionActive = "active"
	CeremonySessionEnded  = "ended"
)

// ErrCeremonySessionNotFound is returned when a session id does not exist in the project.
var ErrCeremonySessionNotFound = errors.New("ceremony session not found")

// ErrCeremonyRevisionConflict is returned when a session changed since the caller read it.
var ErrCeremonyRevisionConflict = errors.New("ceremony session changed")

// ErrCeremonySessionActive is returned when a project already has an active session of that kind.
var ErrCeremonySessionActive = errors.New("a session is already running")

// CreateCeremonyTables creates the site feature switches, per-project opt-ins, and sessions.
func CreateCeremonyTables() error {
	pool, err := OpenDatabase()
	if err != nil {
		return err
	}
	defer CloseDatabase(pool)

	stmts := []string{
		`CREATE TABLE IF NOT EXISTS ceremony_site_features (
			kind TEXT PRIMARY KEY,
			enabled BOOLEAN NOT NULL DEFAULT FALSE,
			updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)`,
		`CREATE TABLE IF NOT EXISTS project_ceremonies (
			project_id INTEGER NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
			kind TEXT NOT NULL,
			enabled BOOLEAN NOT NULL DEFAULT FALSE,
			updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			PRIMARY KEY (project_id, kind)
		)`,
		`CREATE TABLE IF NOT EXISTS ceremony_sessions (
			id SERIAL PRIMARY KEY,
			project_id INTEGER NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
			kind TEXT NOT NULL,
			title TEXT NOT NULL DEFAULT '',
			status TEXT NOT NULL DEFAULT 'active',
			facilitator_id INTEGER REFERENCES users(id) ON DELETE SET NULL,
			sprint_id INTEGER REFERENCES project_sprints(id) ON DELETE SET NULL,
			state JSONB NOT NULL DEFAULT '{}'::jsonb,
			revision INTEGER NOT NULL DEFAULT 1,
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			ended_at TIMESTAMPTZ
		)`,
		`CREATE INDEX IF NOT EXISTS idx_ceremony_sessions_project_kind
			ON ceremony_sessions (project_id, kind, created_at DESC)`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_ceremony_sessions_one_active
			ON ceremony_sessions (project_id, kind) WHERE status = 'active'`,
	}
	for _, s := range stmts {
		if _, err := pool.Exec(context.Background(), s); err != nil {
			return fmt.Errorf("create ceremony tables: %w", err)
		}
	}
	return nil
}

// GetSiteCeremonyFeatures returns which ceremonies a site admin has turned on. Missing rows are off.
func GetSiteCeremonyFeatures() (map[string]bool, error) {
	out := make(map[string]bool, len(CeremonyKinds))
	for _, k := range CeremonyKinds {
		out[k] = false
	}
	pool, err := OpenDatabase()
	if err != nil {
		return out, err
	}
	defer CloseDatabase(pool)
	rows, err := pool.Query(context.Background(), `SELECT kind, enabled FROM ceremony_site_features`)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		var kind string
		var on bool
		if err := rows.Scan(&kind, &on); err != nil {
			return out, err
		}
		if ValidCeremonyKind(kind) {
			out[kind] = on
		}
	}
	return out, rows.Err()
}

// SetSiteCeremonyFeature turns a ceremony on or off for the whole site.
func SetSiteCeremonyFeature(kind string, enabled bool) error {
	if !ValidCeremonyKind(kind) {
		return fmt.Errorf("unknown ceremony %q", kind)
	}
	pool, err := OpenDatabase()
	if err != nil {
		return err
	}
	defer CloseDatabase(pool)
	_, err = pool.Exec(context.Background(), `
		INSERT INTO ceremony_site_features (kind, enabled, updated_at) VALUES ($1, $2, NOW())
		ON CONFLICT (kind) DO UPDATE SET enabled = EXCLUDED.enabled, updated_at = NOW()`, kind, enabled)
	return err
}

// GetProjectCeremonies returns a project's opt-ins. Missing rows are off.
func GetProjectCeremonies(projectID int) (map[string]bool, error) {
	out := make(map[string]bool, len(CeremonyKinds))
	for _, k := range CeremonyKinds {
		out[k] = false
	}
	pool, err := OpenDatabase()
	if err != nil {
		return out, err
	}
	defer CloseDatabase(pool)
	rows, err := pool.Query(context.Background(),
		`SELECT kind, enabled FROM project_ceremonies WHERE project_id = $1`, projectID)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		var kind string
		var on bool
		if err := rows.Scan(&kind, &on); err != nil {
			return out, err
		}
		if ValidCeremonyKind(kind) {
			out[kind] = on
		}
	}
	return out, rows.Err()
}

// SetProjectCeremony opts a project in or out of a ceremony.
func SetProjectCeremony(projectID int, kind string, enabled bool) error {
	if !ValidCeremonyKind(kind) {
		return fmt.Errorf("unknown ceremony %q", kind)
	}
	pool, err := OpenDatabase()
	if err != nil {
		return err
	}
	defer CloseDatabase(pool)
	_, err = pool.Exec(context.Background(), `
		INSERT INTO project_ceremonies (project_id, kind, enabled, updated_at) VALUES ($1, $2, $3, NOW())
		ON CONFLICT (project_id, kind) DO UPDATE SET enabled = EXCLUDED.enabled, updated_at = NOW()`,
		projectID, kind, enabled)
	return err
}

// CeremonyEnabledForProject reports whether kind is on site-wide, opted into by the project,
// and the project is a kanban board.
func CeremonyEnabledForProject(projectID int, kind string) (bool, error) {
	pool, err := OpenDatabase()
	if err != nil {
		return false, err
	}
	defer CloseDatabase(pool)
	var on bool
	err = pool.QueryRow(context.Background(), `
		SELECT EXISTS (
			SELECT 1 FROM projects p
			JOIN project_ceremonies pc ON pc.project_id = p.id AND pc.kind = $2 AND pc.enabled
			JOIN ceremony_site_features sf ON sf.kind = $2 AND sf.enabled
			WHERE p.id = $1 AND p.workflow_mode = 'kanban'
		)`, projectID, kind).Scan(&on)
	return on, err
}

// CeremonySession is one run of a ceremony. State is kind-specific JSON owned by the domain layer.
type CeremonySession struct {
	ID              int
	ProjectID       int
	Kind            string
	Title           string
	Status          string
	FacilitatorID   int
	FacilitatorName string
	SprintID        int
	SprintName      string
	State           json.RawMessage
	Revision        int
	CreatedAt       time.Time
	UpdatedAt       time.Time
	EndedAt         *time.Time
}

const ceremonySessionSelect = `
	SELECT s.id, s.project_id, s.kind, s.title, s.status, COALESCE(s.facilitator_id, 0),
	       COALESCE(NULLIF(u.full_name, ''), NULLIF(u.user_name, ''), u.email, ''),
	       COALESCE(s.sprint_id, 0), COALESCE(sp.name, ''),
	       s.state, s.revision, s.created_at, s.updated_at, s.ended_at
	FROM ceremony_sessions s
	LEFT JOIN users u ON u.id = s.facilitator_id
	LEFT JOIN project_sprints sp ON sp.id = s.sprint_id`

func scanCeremonySession(row pgx.Row) (*CeremonySession, error) {
	var s CeremonySession
	var ended sql.NullTime
	var state []byte
	err := row.Scan(&s.ID, &s.ProjectID, &s.Kind, &s.Title, &s.Status, &s.FacilitatorID, &s.FacilitatorName,
		&s.SprintID, &s.SprintName, &state, &s.Revision, &s.CreatedAt, &s.UpdatedAt, &ended)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrCeremonySessionNotFound
		}
		return nil, err
	}
	s.State = json.RawMessage(state)
	if ended.Valid {
		t := ended.Time
		s.EndedAt = &t
	}
	return &s, nil
}

// CreateCeremonySession starts a session. It fails with ErrCeremonySessionActive when one is running.
func CreateCeremonySession(projectID int, kind, title string, facilitatorID, sprintID int, state json.RawMessage) (*CeremonySession, error) {
	pool, err := OpenDatabase()
	if err != nil {
		return nil, err
	}
	defer CloseDatabase(pool)
	var sprintArg interface{}
	if sprintID > 0 {
		sprintArg = sprintID
	}
	var id int
	err = pool.QueryRow(context.Background(), `
		INSERT INTO ceremony_sessions (project_id, kind, title, status, facilitator_id, sprint_id, state)
		VALUES ($1, $2, $3, 'active', $4, $5, $6) RETURNING id`,
		projectID, kind, title, facilitatorID, sprintArg, []byte(state)).Scan(&id)
	if err != nil {
		if isUniqueViolation(err) {
			return nil, ErrCeremonySessionActive
		}
		return nil, err
	}
	return GetCeremonySession(projectID, id)
}

// GetCeremonySession loads a session within a project.
func GetCeremonySession(projectID, sessionID int) (*CeremonySession, error) {
	pool, err := OpenDatabase()
	if err != nil {
		return nil, err
	}
	defer CloseDatabase(pool)
	return scanCeremonySession(pool.QueryRow(context.Background(),
		ceremonySessionSelect+` WHERE s.id = $1 AND s.project_id = $2`, sessionID, projectID))
}

// GetActiveCeremonySession returns the running session of kind, or ErrCeremonySessionNotFound.
func GetActiveCeremonySession(projectID int, kind string) (*CeremonySession, error) {
	pool, err := OpenDatabase()
	if err != nil {
		return nil, err
	}
	defer CloseDatabase(pool)
	return scanCeremonySession(pool.QueryRow(context.Background(),
		ceremonySessionSelect+` WHERE s.project_id = $1 AND s.kind = $2 AND s.status = 'active'`, projectID, kind))
}

// ListCeremonySessions returns recent sessions of kind, newest first.
func ListCeremonySessions(projectID int, kind string, limit int) ([]CeremonySession, error) {
	if limit <= 0 || limit > 100 {
		limit = 25
	}
	pool, err := OpenDatabase()
	if err != nil {
		return nil, err
	}
	defer CloseDatabase(pool)
	rows, err := pool.Query(context.Background(),
		ceremonySessionSelect+` WHERE s.project_id = $1 AND s.kind = $2 ORDER BY s.created_at DESC, s.id DESC LIMIT $3`,
		projectID, kind, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []CeremonySession{}
	for rows.Next() {
		s, err := scanCeremonySession(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *s)
	}
	return out, rows.Err()
}

// UpdateCeremonySession writes new state (and optionally ends the session) if revision still matches.
// It returns the stored session with its bumped revision.
func UpdateCeremonySession(projectID, sessionID, revision int, state json.RawMessage, facilitatorID int, end bool) (*CeremonySession, error) {
	pool, err := OpenDatabase()
	if err != nil {
		return nil, err
	}
	defer CloseDatabase(pool)
	status := CeremonySessionActive
	if end {
		status = CeremonySessionEnded
	}
	tag, err := pool.Exec(context.Background(), `
		UPDATE ceremony_sessions
		SET state = $4, revision = revision + 1, updated_at = NOW(),
		    facilitator_id = CASE WHEN $5 > 0 THEN $5 ELSE facilitator_id END,
		    status = $6, ended_at = CASE WHEN $6 = 'ended' THEN NOW() ELSE ended_at END
		WHERE id = $1 AND project_id = $2 AND revision = $3 AND status = 'active'`,
		sessionID, projectID, revision, []byte(state), facilitatorID, status)
	if err != nil {
		return nil, err
	}
	if tag.RowsAffected() == 0 {
		if _, err := GetCeremonySession(projectID, sessionID); err != nil {
			return nil, err
		}
		return nil, ErrCeremonyRevisionConflict
	}
	return GetCeremonySession(projectID, sessionID)
}

