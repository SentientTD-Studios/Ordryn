package storage

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// Project slugs give projects readable, site-unique URLs (/p/:slug). They are
// set from the name at creation and do not follow later renames, so links stay
// stable. When a slug is changed the old one is kept in project_slug_history
// and keeps resolving to the project.

const (
	MaxProjectSlugLength = 48
	minProjectSlugLength = 2
)

// ErrProjectSlugTaken is returned when another project currently uses the slug.
var ErrProjectSlugTaken = errors.New("project slug is already in use")

var (
	projectSlugPattern = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)
	slugNonAlnum       = regexp.MustCompile(`[^a-z0-9]+`)
	slugAllDigits      = regexp.MustCompile(`^[0-9]+$`)
)

// NormalizeProjectSlug lowercases and trims raw, then validates it. Slugs are
// lowercase letters, digits and single hyphens, and may not be all digits
// (numeric /p/:ref values are project ids).
func NormalizeProjectSlug(raw string) (string, error) {
	s := strings.ToLower(strings.TrimSpace(raw))
	if len(s) < minProjectSlugLength || len(s) > MaxProjectSlugLength {
		return "", fmt.Errorf("slug must be %d-%d characters", minProjectSlugLength, MaxProjectSlugLength)
	}
	if !projectSlugPattern.MatchString(s) {
		return "", fmt.Errorf("slug may only use lowercase letters, digits and single hyphens, and cannot start or end with a hyphen")
	}
	if slugAllDigits.MatchString(s) {
		return "", fmt.Errorf("slug cannot be only digits")
	}
	return s, nil
}

// SlugifyProjectName turns a project name into a slug base. It always returns
// a valid slug, falling back to "project" when the name has nothing usable.
func SlugifyProjectName(name string) string {
	s := slugNonAlnum.ReplaceAllString(strings.ToLower(name), "-")
	s = strings.Trim(s, "-")
	if len(s) > MaxProjectSlugLength {
		s = strings.TrimRight(s[:MaxProjectSlugLength], "-")
	}
	if len(s) < minProjectSlugLength {
		return "project"
	}
	if slugAllDigits.MatchString(s) {
		s = "p-" + s
		if len(s) > MaxProjectSlugLength {
			s = s[:MaxProjectSlugLength]
		}
	}
	return s
}

// slugCandidate returns base for n == 1 and base-n otherwise, trimmed to fit.
func slugCandidate(base string, n int) string {
	if n <= 1 {
		return base
	}
	suffix := "-" + strconv.Itoa(n)
	if len(base)+len(suffix) > MaxProjectSlugLength {
		base = strings.TrimRight(base[:MaxProjectSlugLength-len(suffix)], "-")
	}
	return base + suffix
}

type slugQuerier interface {
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
	QueryRow(context.Context, string, ...any) pgx.Row
}

// CreateProjectSlugTables adds projects.slug and the slug history table.
func CreateProjectSlugTables() error {
	pool, err := OpenDatabase()
	if err != nil {
		return err
	}
	defer CloseDatabase(pool)
	queries := []string{
		`ALTER TABLE projects ADD COLUMN IF NOT EXISTS slug TEXT`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_projects_slug ON projects (slug) WHERE slug IS NOT NULL`,
		`CREATE TABLE IF NOT EXISTS project_slug_history (
			slug TEXT PRIMARY KEY,
			project_id INTEGER NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)`,
		`CREATE INDEX IF NOT EXISTS idx_project_slug_history_project ON project_slug_history (project_id)`,
	}
	for _, q := range queries {
		if _, err := pool.Exec(context.Background(), q); err != nil {
			return fmt.Errorf("project slug migration failed on %q: %w", q, err)
		}
	}
	return nil
}

// MigrateBackfillProjectSlugs gives every project without a slug one from its name.
func MigrateBackfillProjectSlugs() error {
	pool, err := OpenDatabase()
	if err != nil {
		return err
	}
	defer CloseDatabase(pool)
	ctx := context.Background()
	rows, err := pool.Query(ctx, `SELECT id, name FROM projects WHERE slug IS NULL ORDER BY id`)
	if err != nil {
		return err
	}
	type pending struct {
		id   int
		name string
	}
	var todo []pending
	for rows.Next() {
		var p pending
		if err := rows.Scan(&p.id, &p.name); err != nil {
			rows.Close()
			return err
		}
		todo = append(todo, p)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	for _, p := range todo {
		if _, err := AssignProjectSlug(ctx, pool, p.id, p.name); err != nil {
			return fmt.Errorf("project %d: %w", p.id, err)
		}
	}
	return nil
}

// AssignProjectSlug sets a generated slug on a project that has none and
// returns the project's slug. Generated slugs avoid slugs in history so old
// links never start pointing at a different project. q may be a pool or a tx.
func AssignProjectSlug(ctx context.Context, q slugQuerier, projectID int, name string) (string, error) {
	base := SlugifyProjectName(name)
	for n := 1; n <= 1000; n++ {
		cand := slugCandidate(base, n)
		var taken bool
		if err := q.QueryRow(ctx,
			`SELECT EXISTS (SELECT 1 FROM projects WHERE slug = $1)
			     OR EXISTS (SELECT 1 FROM project_slug_history WHERE slug = $1)`, cand).Scan(&taken); err != nil {
			return "", err
		}
		if taken {
			continue
		}
		var slug string
		err := q.QueryRow(ctx,
			`UPDATE projects SET slug = COALESCE(slug, $2) WHERE id = $1 RETURNING slug`,
			projectID, cand).Scan(&slug)
		if err != nil {
			if isUniqueViolation(err) {
				continue // lost a race for this candidate
			}
			return "", err
		}
		return slug, nil
	}
	return "", fmt.Errorf("could not find a free slug for %q", name)
}

// SetProjectSlug changes a project's slug. The previous slug moves to history
// and keeps resolving to the project. A slug another project currently uses is
// rejected with ErrProjectSlugTaken; one only held in another project's history
// is taken over, since explicitly claiming it is a deliberate choice.
func SetProjectSlug(projectID int, raw string) (string, error) {
	slug, err := NormalizeProjectSlug(raw)
	if err != nil {
		return "", err
	}
	pool, err := OpenDatabase()
	if err != nil {
		return "", err
	}
	defer CloseDatabase(pool)
	ctx := context.Background()
	tx, err := pool.Begin(ctx)
	if err != nil {
		return "", err
	}
	defer tx.Rollback(ctx)

	var current *string
	if err := tx.QueryRow(ctx, `SELECT slug FROM projects WHERE id = $1 FOR UPDATE`, projectID).Scan(&current); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", fmt.Errorf("project not found")
		}
		return "", err
	}
	if current != nil && *current == slug {
		return slug, nil
	}
	var takenBy int
	err = tx.QueryRow(ctx, `SELECT id FROM projects WHERE slug = $1 AND id <> $2`, slug, projectID).Scan(&takenBy)
	if err == nil {
		return "", ErrProjectSlugTaken
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return "", err
	}
	if _, err := tx.Exec(ctx, `DELETE FROM project_slug_history WHERE slug = $1`, slug); err != nil {
		return "", err
	}
	if current != nil && *current != "" {
		if _, err := tx.Exec(ctx,
			`INSERT INTO project_slug_history (slug, project_id) VALUES ($1, $2)
			 ON CONFLICT (slug) DO UPDATE SET project_id = EXCLUDED.project_id, created_at = NOW()`,
			*current, projectID); err != nil {
			return "", err
		}
	}
	if _, err := tx.Exec(ctx, `UPDATE projects SET slug = $2, updated_at = CURRENT_TIMESTAMP WHERE id = $1`, projectID, slug); err != nil {
		if isUniqueViolation(err) {
			return "", ErrProjectSlugTaken
		}
		return "", err
	}
	if err := tx.Commit(ctx); err != nil {
		return "", err
	}
	return slug, nil
}

// ProjectIDBySlug resolves a current or historical slug to a project id.
// It returns 0 when nothing matches. Callers must check access themselves.
func ProjectIDBySlug(raw string) (int, error) {
	slug := strings.ToLower(strings.TrimSpace(raw))
	if slug == "" {
		return 0, nil
	}
	pool, err := OpenDatabase()
	if err != nil {
		return 0, err
	}
	defer CloseDatabase(pool)
	var id int
	err = pool.QueryRow(context.Background(),
		`SELECT id FROM projects WHERE slug = $1
		 UNION ALL
		 SELECT project_id FROM project_slug_history WHERE slug = $1
		 LIMIT 1`, slug).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, nil
	}
	return id, err
}
