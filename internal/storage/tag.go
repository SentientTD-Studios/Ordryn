package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const MaxTagsPerTask = 5

// RemovedTagName is the reserved, protected archive tag. One exists per namespace.
const RemovedTagName = "removed"

// ArchivedTagName is the reserved, protected tag applied to tasks when a project is archived.
const ArchivedTagName = "archived"

const removedTagColor = "#6c757d"
const archivedTagColor = "#6c757d"

const tagSelectCols = `id, user_id, project_id, name, COALESCE(color, '#6c757d'), COALESCE(protected, false)`

// Tag is a label in a personal (inbox) or project namespace.
type Tag struct {
	ID        int
	UserID    int
	ProjectID *int
	Name      string
	Color     string
	Protected bool
}

// IsRemovedTagName reports whether name is the reserved archive tag (case-insensitive).
func IsRemovedTagName(name string) bool {
	return strings.EqualFold(strings.TrimSpace(name), RemovedTagName)
}

// IsArchivedTagName reports whether name is the reserved project-archive tag (case-insensitive).
func IsArchivedTagName(name string) bool {
	return strings.EqualFold(strings.TrimSpace(name), ArchivedTagName)
}

// IsSystemTagName reports whether name is a reserved protected tag.
func IsSystemTagName(name string) bool {
	return IsRemovedTagName(name) || IsArchivedTagName(name)
}

var tagPalette = []string{
	"#6c757d", "#0d6efd", "#198754", "#dc3545",
	"#fd7e14", "#6610f2", "#20c997", "#d63384",
}

func tagColorForID(id int) string {
	if len(tagPalette) == 0 {
		return "#6c757d"
	}
	return tagPalette[(id-1)%len(tagPalette)]
}

func scanTag(scanner interface{ Scan(...any) error }) (Tag, error) {
	var t Tag
	var projectID sql.NullInt64
	err := scanner.Scan(&t.ID, &t.UserID, &projectID, &t.Name, &t.Color, &t.Protected)
	if err != nil {
		return t, err
	}
	if projectID.Valid && projectID.Int64 > 0 {
		pid := int(projectID.Int64)
		t.ProjectID = &pid
	}
	return t, nil
}

func isNoRows(err error) bool {
	return err != nil && (errors.Is(err, pgx.ErrNoRows) || errors.Is(err, sql.ErrNoRows) || strings.Contains(err.Error(), "no rows"))
}

// CreateTagsTables creates tags and task_tags tables.
func CreateTagsTables() error {
	pool, err := OpenDatabase()
	if err != nil {
		return fmt.Errorf("failed to open database: %v", err)
	}
	defer CloseDatabase(pool)

	_, err = pool.Exec(context.Background(), `
		CREATE TABLE IF NOT EXISTS tags (
			id SERIAL PRIMARY KEY,
			user_id INTEGER NOT NULL,
			project_id INTEGER,
			name TEXT NOT NULL,
			color VARCHAR(7) DEFAULT '#6c757d',
			protected BOOLEAN NOT NULL DEFAULT false,
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
		);
		CREATE TABLE IF NOT EXISTS task_tags (
			task_id INTEGER NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
			tag_id INTEGER NOT NULL REFERENCES tags(id) ON DELETE CASCADE,
			PRIMARY KEY (task_id, tag_id)
		);
		CREATE INDEX IF NOT EXISTS idx_task_tags_tag_id ON task_tags(tag_id);
	`)
	if err != nil {
		return fmt.Errorf("failed to create tags tables: %v", err)
	}
	return nil
}

// MigrateTagsAddProjectID adds project-scoped tags, splits legacy user-owned
// tags onto the projects (and inbox) that use them, and replaces the old
// UNIQUE(user_id, name) constraint.
func MigrateTagsAddProjectID() error {
	pool, err := OpenDatabase()
	if err != nil {
		return err
	}
	defer CloseDatabase(pool)

	if _, err := pool.Exec(context.Background(),
		`ALTER TABLE tags ADD COLUMN IF NOT EXISTS project_id INTEGER`); err != nil {
		return fmt.Errorf("failed to add tags.project_id: %v", err)
	}

	// Cloning a personal tag onto a project inserts another row with the same
	// (user_id, name). The legacy UNIQUE(user_id, name) constraint must be
	// gone before that split, or PostgreSQL raises tags_user_id_name_key.
	if err := dropLegacyTagsUserNameUnique(pool); err != nil {
		return err
	}

	if err := migrateSplitLegacyTags(pool); err != nil {
		return err
	}
	if err := migrateMergeDuplicateTagNames(pool); err != nil {
		return err
	}

	if _, err := pool.Exec(context.Background(),
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_tags_personal_lower_name
			ON tags (user_id, lower(name)) WHERE project_id IS NULL`); err != nil {
		return fmt.Errorf("failed to create personal tag unique index: %v", err)
	}
	if _, err := pool.Exec(context.Background(),
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_tags_project_lower_name
			ON tags (project_id, lower(name)) WHERE project_id IS NOT NULL`); err != nil {
		return fmt.Errorf("failed to create project tag unique index: %v", err)
	}

	var fkExists bool
	if err := pool.QueryRow(context.Background(),
		`SELECT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'fk_tags_project')`).Scan(&fkExists); err != nil {
		return err
	}
	if !fkExists && tableExists(pool, "projects") {
		if _, err := pool.Exec(context.Background(),
			`ALTER TABLE tags ADD CONSTRAINT fk_tags_project
			 FOREIGN KEY (project_id) REFERENCES projects(id) ON DELETE CASCADE`); err != nil {
			return fmt.Errorf("failed to add tags.project_id FK: %v", err)
		}
	}
	return nil
}

func dropLegacyTagsUserNameUnique(pool *pgxpool.Pool) error {
	ctx := context.Background()
	if _, err := pool.Exec(ctx, `ALTER TABLE tags DROP CONSTRAINT IF EXISTS tags_user_id_name_key`); err != nil {
		return fmt.Errorf("failed to drop tags_user_id_name_key: %v", err)
	}
	// Some installs created this as a unique index rather than a table constraint.
	if _, err := pool.Exec(ctx, `DROP INDEX IF EXISTS tags_user_id_name_key`); err != nil {
		return fmt.Errorf("failed to drop tags_user_id_name_key index: %v", err)
	}

	// Catch leftover UNIQUE(user_id, name) objects that were renamed or created
	// as an index instead of a table constraint.
	constraintNames, err := listTagNames(ctx, pool, `
		SELECT conname
		FROM pg_constraint
		WHERE conrelid = 'tags'::regclass
		  AND contype = 'u'
		  AND pg_get_constraintdef(oid) ~* 'UNIQUE\s*\(\s*user_id\s*,\s*name\s*\)'
		  AND pg_get_constraintdef(oid) !~* 'project_id'`)
	if err != nil {
		return fmt.Errorf("failed to list leftover tag unique constraints: %v", err)
	}
	for _, name := range constraintNames {
		if !pgIdentSafe(name) {
			return fmt.Errorf("unexpected tags unique constraint name %q", name)
		}
		if _, err := pool.Exec(ctx, fmt.Sprintf(`ALTER TABLE tags DROP CONSTRAINT IF EXISTS %s`, name)); err != nil {
			return fmt.Errorf("failed to drop leftover tag unique constraint %s: %v", name, err)
		}
	}

	indexNames, err := listTagNames(ctx, pool, `
		SELECT indexname
		FROM pg_indexes
		WHERE tablename = 'tags'
		  AND indexdef ILIKE '%UNIQUE%'
		  AND indexdef ~* '\(\s*user_id\s*,\s*name\s*\)'
		  AND indexdef !~* 'project_id'
		  AND indexname NOT IN ('tags_pkey', 'idx_tags_personal_lower_name', 'idx_tags_project_lower_name')`)
	if err != nil {
		return fmt.Errorf("failed to list leftover tag unique indexes: %v", err)
	}
	for _, name := range indexNames {
		if !pgIdentSafe(name) {
			return fmt.Errorf("unexpected tags unique index name %q", name)
		}
		if _, err := pool.Exec(ctx, fmt.Sprintf(`DROP INDEX IF EXISTS %s`, name)); err != nil {
			return fmt.Errorf("failed to drop leftover tag unique index %s: %v", name, err)
		}
	}
	return nil
}

func listTagNames(ctx context.Context, pool *pgxpool.Pool, query string) ([]string, error) {
	rows, err := pool.Query(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var names []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		names = append(names, name)
	}
	return names, rows.Err()
}

func migrateSplitLegacyTags(pool *pgxpool.Pool) error {
	if !tableExists(pool, "tasks") || !tableExists(pool, "tags") {
		return nil
	}
	ctx := context.Background()

	if _, err := pool.Exec(ctx, `
		DELETE FROM task_tags a
		WHERE EXISTS (
			SELECT 1
			FROM task_tags b
			JOIN tags ta ON ta.id = a.tag_id
			JOIN tags tb ON tb.id = b.tag_id
			WHERE a.task_id = b.task_id
			  AND lower(ta.name) = lower(tb.name)
			  AND a.tag_id > b.tag_id
		)`); err != nil {
		return fmt.Errorf("failed to collapse duplicate task tags: %v", err)
	}

	rows, err := pool.Query(ctx, `SELECT `+tagSelectCols+` FROM tags WHERE project_id IS NULL`)
	if err != nil {
		return fmt.Errorf("failed to list personal tags for migration: %v", err)
	}
	defer rows.Close()

	var tags []Tag
	for rows.Next() {
		t, err := scanTag(rows)
		if err != nil {
			return err
		}
		tags = append(tags, t)
	}
	if err := rows.Err(); err != nil {
		return err
	}

	for _, t := range tags {
		projRows, err := pool.Query(ctx, `
			SELECT DISTINCT t.project_id
			FROM task_tags tt
			JOIN tasks t ON t.id = tt.task_id
			WHERE tt.tag_id = $1 AND t.project_id IS NOT NULL
			ORDER BY t.project_id`, t.ID)
		if err != nil {
			return fmt.Errorf("failed to list tag projects: %v", err)
		}
		var projectIDs []int
		for projRows.Next() {
			var pid int
			if err := projRows.Scan(&pid); err != nil {
				projRows.Close()
				return err
			}
			projectIDs = append(projectIDs, pid)
		}
		projRows.Close()
		if err := projRows.Err(); err != nil {
			return err
		}

		var inboxUsed bool
		if err := pool.QueryRow(ctx, `
			SELECT EXISTS (
				SELECT 1 FROM task_tags tt
				JOIN tasks t ON t.id = tt.task_id
				WHERE tt.tag_id = $1 AND t.project_id IS NULL
			)`, t.ID).Scan(&inboxUsed); err != nil {
			return err
		}

		if len(projectIDs) == 0 {
			continue
		}
		if len(projectIDs) == 1 && !inboxUsed {
			if _, err := pool.Exec(ctx, `UPDATE tags SET project_id = $1 WHERE id = $2 AND project_id IS NULL`,
				projectIDs[0], t.ID); err != nil {
				return fmt.Errorf("failed to convert tag to project scope: %v", err)
			}
			continue
		}

		for _, pid := range projectIDs {
			destID, err := findOrCloneProjectTag(pool, t, pid)
			if err != nil {
				return err
			}
			if destID == t.ID {
				continue
			}
			if _, err := pool.Exec(ctx, `
				UPDATE task_tags tt
				SET tag_id = $1
				FROM tasks tsk
				WHERE tt.task_id = tsk.id
				  AND tt.tag_id = $2
				  AND tsk.project_id = $3
				  AND NOT EXISTS (
					SELECT 1 FROM task_tags existing
					WHERE existing.task_id = tt.task_id AND existing.tag_id = $1
				  )`, destID, t.ID, pid); err != nil {
				return fmt.Errorf("failed to re-point task tags: %v", err)
			}
			if _, err := pool.Exec(ctx, `
				DELETE FROM task_tags tt
				USING tasks tsk
				WHERE tt.task_id = tsk.id AND tt.tag_id = $1 AND tsk.project_id = $2`,
				t.ID, pid); err != nil {
				return err
			}
		}
	}
	return nil
}

func findOrCloneProjectTag(pool *pgxpool.Pool, src Tag, projectID int) (int, error) {
	ctx := context.Background()
	var existingID int
	err := pool.QueryRow(ctx, `
		SELECT id FROM tags
		WHERE project_id = $1 AND lower(name) = lower($2)
		ORDER BY id ASC LIMIT 1`, projectID, src.Name).Scan(&existingID)
	if err == nil {
		return existingID, nil
	}
	if !isNoRows(err) {
		return 0, err
	}

	var newID int
	err = pool.QueryRow(ctx, `
		INSERT INTO tags (user_id, project_id, name, color)
		VALUES ($1, $2, $3, $4)
		RETURNING id`, src.UserID, projectID, src.Name, src.Color).Scan(&newID)
	if err != nil {
		if lookupErr := pool.QueryRow(ctx, `
			SELECT id FROM tags
			WHERE project_id = $1 AND lower(name) = lower($2)
			ORDER BY id ASC LIMIT 1`, projectID, src.Name).Scan(&existingID); lookupErr == nil {
			return existingID, nil
		}
		return 0, fmt.Errorf("failed to clone project tag: %v", err)
	}
	return newID, nil
}

func migrateMergeDuplicateTagNames(pool *pgxpool.Pool) error {
	ctx := context.Background()

	personalRows, err := pool.Query(ctx, `
		SELECT user_id, lower(name)
		FROM tags
		WHERE project_id IS NULL
		GROUP BY user_id, lower(name)
		HAVING COUNT(*) > 1`)
	if err != nil {
		return fmt.Errorf("failed to list personal tag duplicates: %v", err)
	}
	type pair struct {
		scopeID int
		name    string
	}
	var personal []pair
	for personalRows.Next() {
		var p pair
		if err := personalRows.Scan(&p.scopeID, &p.name); err != nil {
			personalRows.Close()
			return err
		}
		personal = append(personal, p)
	}
	personalRows.Close()
	if err := personalRows.Err(); err != nil {
		return err
	}
	for _, p := range personal {
		if err := mergeTagsMatching(pool, `
			SELECT id FROM tags
			WHERE project_id IS NULL AND user_id = $1 AND lower(name) = $2
			ORDER BY id ASC`, p.scopeID, p.name); err != nil {
			return err
		}
	}

	projectRows, err := pool.Query(ctx, `
		SELECT project_id, lower(name)
		FROM tags
		WHERE project_id IS NOT NULL
		GROUP BY project_id, lower(name)
		HAVING COUNT(*) > 1`)
	if err != nil {
		return fmt.Errorf("failed to list project tag duplicates: %v", err)
	}
	var projects []pair
	for projectRows.Next() {
		var p pair
		if err := projectRows.Scan(&p.scopeID, &p.name); err != nil {
			projectRows.Close()
			return err
		}
		projects = append(projects, p)
	}
	projectRows.Close()
	if err := projectRows.Err(); err != nil {
		return err
	}
	for _, p := range projects {
		if err := mergeTagsMatching(pool, `
			SELECT id FROM tags
			WHERE project_id = $1 AND lower(name) = $2
			ORDER BY id ASC`, p.scopeID, p.name); err != nil {
			return err
		}
	}
	return nil
}

func tableExists(pool *pgxpool.Pool, name string) bool {
	var exists bool
	_ = pool.QueryRow(context.Background(),
		`SELECT EXISTS (SELECT 1 FROM information_schema.tables WHERE table_schema = 'public' AND table_name = $1)`,
		name).Scan(&exists)
	return exists
}

func mergeTagsMatching(pool *pgxpool.Pool, idSQL string, scopeID int, name string) error {
	ctx := context.Background()
	rows, err := pool.Query(ctx, idSQL, scopeID, name)
	if err != nil {
		return err
	}
	defer rows.Close()
	var ids []int
	for rows.Next() {
		var id int
		if err := rows.Scan(&id); err != nil {
			return err
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if len(ids) < 2 {
		return nil
	}
	canonical := ids[0]
	for _, dupe := range ids[1:] {
		if _, err := pool.Exec(ctx, `
			UPDATE task_tags tt SET tag_id = $1
			WHERE tt.tag_id = $2
			  AND NOT EXISTS (
				SELECT 1 FROM task_tags existing
				WHERE existing.task_id = tt.task_id AND existing.tag_id = $1
			  )`, canonical, dupe); err != nil {
			return fmt.Errorf("failed to merge task_tags: %v", err)
		}
		if _, err := pool.Exec(ctx, `DELETE FROM task_tags WHERE tag_id = $1`, dupe); err != nil {
			return err
		}
		if tableExists(pool, "saved_views") {
			if _, err := pool.Exec(ctx, `
				UPDATE saved_views
				SET filter_json = jsonb_set(filter_json, '{tag}', to_jsonb($1::text), true)
				WHERE filter_json->>'tag' = $2`, fmt.Sprintf("%d", canonical), fmt.Sprintf("%d", dupe)); err != nil {
				return err
			}
		}
		if _, err := pool.Exec(ctx, `DELETE FROM tags WHERE id = $1`, dupe); err != nil {
			return err
		}
	}
	return nil
}

const accessibleTagCondition = `
	(
		(tg.project_id IS NULL AND tg.user_id = $%[1]d)
		OR EXISTS (
			SELECT 1 FROM projects p
			LEFT JOIN project_members pm ON pm.project_id = p.id AND pm.user_id = $%[1]d
			WHERE p.id = tg.project_id AND (p.user_id = $%[1]d OR pm.user_id IS NOT NULL)
		)
	)`

// GetTag returns a tag by id.
func GetTag(id int) (*Tag, error) {
	pool, err := OpenDatabase()
	if err != nil {
		return nil, err
	}
	defer CloseDatabase(pool)

	t, err := scanTag(pool.QueryRow(context.Background(),
		`SELECT `+tagSelectCols+` FROM tags WHERE id = $1`, id))
	if err != nil {
		if isNoRows(err) {
			return nil, fmt.Errorf("tag not found")
		}
		return nil, fmt.Errorf("tag not found")
	}
	return &t, nil
}

// GetTagByID returns a tag by id if the user can access it.
func GetTagByID(id, userID int) (*Tag, error) {
	t, err := GetTag(id)
	if err != nil {
		return nil, err
	}
	ok, err := UserCanAccessTag(userID, *t)
	if err != nil || !ok {
		return nil, fmt.Errorf("tag not found")
	}
	return t, nil
}

// UserCanAccessTag reports whether the user can see the tag.
func UserCanAccessTag(userID int, tag Tag) (bool, error) {
	if tag.ProjectID == nil {
		return tag.UserID == userID, nil
	}
	_, err := GetAccessibleProjectByID(*tag.ProjectID, userID)
	if err != nil {
		return false, nil
	}
	return true, nil
}

// UserCanManageTag reports whether the user can create/update/delete the tag.
func UserCanManageTag(userID int, tag Tag) (bool, error) {
	if tag.ProjectID == nil {
		return tag.UserID == userID, nil
	}
	proj, err := GetAccessibleProjectByID(*tag.ProjectID, userID)
	if err != nil {
		return false, nil
	}
	return RoleCanWrite(proj.Role), nil
}

// UserCanManageTagNamespace reports whether the user can add/delete tags in a namespace.
// projectID nil means personal tags.
func UserCanManageTagNamespace(userID int, projectID *int) (bool, error) {
	if projectID == nil || *projectID <= 0 {
		return true, nil
	}
	proj, err := GetAccessibleProjectByID(*projectID, userID)
	if err != nil {
		return false, fmt.Errorf("project not found")
	}
	return RoleCanWrite(proj.Role), nil
}

// GetAccessibleTags returns personal tags plus tags on projects the user can access.
func GetAccessibleTags(userID int) ([]Tag, error) {
	pool, err := OpenDatabase()
	if err != nil {
		return nil, err
	}
	defer CloseDatabase(pool)

	cond := fmt.Sprintf(accessibleTagCondition, 1)
	rows, err := pool.Query(context.Background(),
		`SELECT `+tagSelectCols+` FROM tags tg WHERE `+cond+` ORDER BY LOWER(tg.name), tg.id`, userID)
	if err != nil {
		return nil, fmt.Errorf("failed to query tags: %v", err)
	}
	return collectTags(rows)
}

// GetTagsForUser returns all tags accessible to the user.
func GetTagsForUser(userID int) ([]Tag, error) {
	return GetAccessibleTags(userID)
}

// GetPersonalTags returns inbox tags owned by the user.
func GetPersonalTags(userID int) ([]Tag, error) {
	pool, err := OpenDatabase()
	if err != nil {
		return nil, err
	}
	defer CloseDatabase(pool)

	rows, err := pool.Query(context.Background(),
		`SELECT `+tagSelectCols+` FROM tags WHERE project_id IS NULL AND user_id = $1 ORDER BY LOWER(name)`, userID)
	if err != nil {
		return nil, fmt.Errorf("failed to query tags: %v", err)
	}
	return collectTags(rows)
}

// GetProjectTags returns tags belonging to a project.
func GetProjectTags(projectID int) ([]Tag, error) {
	pool, err := OpenDatabase()
	if err != nil {
		return nil, err
	}
	defer CloseDatabase(pool)

	rows, err := pool.Query(context.Background(),
		`SELECT `+tagSelectCols+` FROM tags WHERE project_id = $1 ORDER BY LOWER(name)`, projectID)
	if err != nil {
		return nil, fmt.Errorf("failed to query tags: %v", err)
	}
	return collectTags(rows)
}

func collectTags(rows pgx.Rows) ([]Tag, error) {
	defer rows.Close()
	var out []Tag
	for rows.Next() {
		t, err := scanTag(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if out == nil {
		out = []Tag{}
	}
	return out, nil
}

// FindTagByName returns a tag in the given namespace (case-insensitive).
// projectID nil means personal tags for userID.
func FindTagByName(userID int, projectID *int, name string) (*Tag, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, fmt.Errorf("tag name is required")
	}
	pool, err := OpenDatabase()
	if err != nil {
		return nil, err
	}
	defer CloseDatabase(pool)

	var t Tag
	if projectID == nil || *projectID <= 0 {
		t, err = scanTag(pool.QueryRow(context.Background(),
			`SELECT `+tagSelectCols+` FROM tags
			 WHERE project_id IS NULL AND user_id = $1 AND LOWER(name) = LOWER($2)`,
			userID, name))
	} else {
		t, err = scanTag(pool.QueryRow(context.Background(),
			`SELECT `+tagSelectCols+` FROM tags
			 WHERE project_id = $1 AND LOWER(name) = LOWER($2)`,
			*projectID, name))
	}
	if err != nil {
		if isNoRows(err) {
			return nil, fmt.Errorf("tag not found")
		}
		return nil, err
	}
	return &t, nil
}

// GetOrCreateTagByName returns an existing tag or creates one in the namespace.
func GetOrCreateTagByName(userID int, projectID *int, name string) (*Tag, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, fmt.Errorf("tag name is required")
	}
	if len(name) > 50 {
		return nil, fmt.Errorf("tag name must be 50 characters or less")
	}
	if IsSystemTagName(name) {
		return nil, fmt.Errorf("tag name is reserved")
	}
	if existing, err := FindTagByName(userID, projectID, name); err == nil {
		if existing.Protected || IsSystemTagName(existing.Name) {
			return nil, fmt.Errorf("tag name is reserved")
		}
		return existing, nil
	}

	pool, err := OpenDatabase()
	if err != nil {
		return nil, err
	}
	defer CloseDatabase(pool)

	var t Tag
	if projectID == nil || *projectID <= 0 {
		t, err = scanTag(pool.QueryRow(context.Background(),
			`INSERT INTO tags (user_id, name) VALUES ($1, $2)
			 RETURNING `+tagSelectCols, userID, name))
	} else {
		t, err = scanTag(pool.QueryRow(context.Background(),
			`INSERT INTO tags (user_id, project_id, name) VALUES ($1, $2, $3)
			 RETURNING `+tagSelectCols, userID, *projectID, name))
	}
	if err != nil {
		if existing, findErr := FindTagByName(userID, projectID, name); findErr == nil {
			if existing.Protected || IsSystemTagName(existing.Name) {
				return nil, fmt.Errorf("tag name is reserved")
			}
			return existing, nil
		}
		return nil, fmt.Errorf("failed to create tag: %v", err)
	}
	t.Color = tagColorForID(t.ID)
	_, _ = pool.Exec(context.Background(), "UPDATE tags SET color = $1 WHERE id = $2", t.Color, t.ID)
	return &t, nil
}

// DeleteTag removes a tag by id.
func DeleteTag(id int) error {
	pool, err := OpenDatabase()
	if err != nil {
		return err
	}
	defer CloseDatabase(pool)

	existing, err := GetTag(id)
	if err != nil {
		return err
	}
	if existing.Protected || IsSystemTagName(existing.Name) {
		return fmt.Errorf("cannot delete a protected tag")
	}

	tag, err := pool.Exec(context.Background(), "DELETE FROM tags WHERE id = $1", id)
	if err != nil {
		return fmt.Errorf("failed to delete tag: %v", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("tag not found")
	}
	return nil
}

// UpdateTag updates a tag's name and color.
func UpdateTag(id int, name string, color string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return fmt.Errorf("tag name is required")
	}
	if len(name) > 50 {
		return fmt.Errorf("tag name must be 50 characters or less")
	}
	color = strings.TrimSpace(color)
	if len(color) > 20 {
		return fmt.Errorf("tag color must be 20 characters or less")
	}

	existing, err := GetTag(id)
	if err != nil {
		return err
	}
	if existing.Protected || IsSystemTagName(existing.Name) {
		return fmt.Errorf("cannot update a protected tag")
	}
	if IsSystemTagName(name) {
		return fmt.Errorf("tag name is reserved")
	}

	pool, err := OpenDatabase()
	if err != nil {
		return err
	}
	defer CloseDatabase(pool)

	var exists bool
	if existing.ProjectID == nil {
		if err := pool.QueryRow(context.Background(),
			`SELECT EXISTS(SELECT 1 FROM tags
			 WHERE project_id IS NULL AND user_id = $1 AND LOWER(name) = LOWER($2) AND id != $3)`,
			existing.UserID, name, id).Scan(&exists); err != nil {
			return fmt.Errorf("failed to check tag name: %v", err)
		}
	} else {
		if err := pool.QueryRow(context.Background(),
			`SELECT EXISTS(SELECT 1 FROM tags
			 WHERE project_id = $1 AND LOWER(name) = LOWER($2) AND id != $3)`,
			*existing.ProjectID, name, id).Scan(&exists); err != nil {
			return fmt.Errorf("failed to check tag name: %v", err)
		}
	}
	if exists {
		return fmt.Errorf("a tag with that name already exists")
	}

	tag, err := pool.Exec(context.Background(), "UPDATE tags SET name = $1, color = $2 WHERE id = $3", name, color, id)
	if err != nil {
		return fmt.Errorf("failed to update tag: %v", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("tag not found")
	}
	return nil
}

func taskTagNamespace(taskID int) (*int, error) {
	pid, err := GetTaskProjectID(taskID)
	if err != nil {
		return nil, err
	}
	if pid <= 0 {
		return nil, nil
	}
	return &pid, nil
}

func tagMatchesTaskNamespace(tag Tag, taskProjectID *int, userID int) bool {
	if taskProjectID == nil {
		return tag.ProjectID == nil && tag.UserID == userID
	}
	return tag.ProjectID != nil && *tag.ProjectID == *taskProjectID
}

// SetTaskTags replaces user-assigned tags on a task (max MaxTagsPerTask).
// Protected tags cannot be assigned here; an existing protected tag is kept.
func SetTaskTags(taskID, userID int, tagIDs []int) error {
	current, err := GetTagsForTask(taskID)
	if err != nil {
		return err
	}
	var preserved []int
	for _, t := range current {
		if t.Protected {
			preserved = append(preserved, t.ID)
		}
	}

	userTagIDs := make([]int, 0, len(tagIDs))
	seen := make(map[int]bool)
	for _, tagID := range tagIDs {
		if seen[tagID] {
			continue
		}
		seen[tagID] = true
		tag, err := GetTag(tagID)
		if err != nil {
			return fmt.Errorf("invalid tag selection")
		}
		if tag.Protected || IsSystemTagName(tag.Name) {
			return fmt.Errorf("cannot assign a protected tag")
		}
		userTagIDs = append(userTagIDs, tagID)
	}
	if len(userTagIDs) > MaxTagsPerTask {
		return fmt.Errorf("maximum %d tags per task", MaxTagsPerTask)
	}

	pool, err := OpenDatabase()
	if err != nil {
		return err
	}
	defer CloseDatabase(pool)

	canRead, writeRole, _, accessErr := CanUserAccessTask(taskID, userID)
	if accessErr != nil {
		return accessErr
	}
	if !canRead || !RoleCanWrite(writeRole) {
		return fmt.Errorf("not authorized")
	}

	taskProjectID, err := taskTagNamespace(taskID)
	if err != nil {
		return err
	}

	for _, tagID := range userTagIDs {
		tag, err := GetTag(tagID)
		if err != nil || !tagMatchesTaskNamespace(*tag, taskProjectID, userID) {
			return fmt.Errorf("invalid tag selection")
		}
	}

	_, err = pool.Exec(context.Background(), "DELETE FROM task_tags WHERE task_id = $1", taskID)
	if err != nil {
		return fmt.Errorf("failed to clear task tags: %v", err)
	}

	finalIDs := append(append([]int{}, userTagIDs...), preserved...)
	for _, tagID := range finalIDs {
		_, err = pool.Exec(context.Background(), "INSERT INTO task_tags (task_id, tag_id) VALUES ($1, $2)", taskID, tagID)
		if err != nil {
			return fmt.Errorf("failed to assign tag: %v", err)
		}
	}
	return nil
}

// RemapTaskTagsForProjectChange keeps tags whose names exist in the destination namespace and drops the rest.
func RemapTaskTagsForProjectChange(taskID, userID int, newProjectID *int) error {
	existing, err := GetTagsForTask(taskID)
	if err != nil {
		return err
	}
	if len(existing) == 0 {
		return nil
	}
	var dest []Tag
	if newProjectID == nil || *newProjectID <= 0 {
		dest, err = GetPersonalTags(userID)
	} else {
		dest, err = GetProjectTags(*newProjectID)
	}
	if err != nil {
		return err
	}
	byName := make(map[string]int, len(dest))
	for _, t := range dest {
		byName[strings.ToLower(t.Name)] = t.ID
	}
	wasArchived := false
	var ids []int
	seen := make(map[int]bool)
	for _, t := range existing {
		if t.Protected || IsSystemTagName(t.Name) {
			if IsArchivedTagName(t.Name) || IsRemovedTagName(t.Name) {
				wasArchived = true
			}
			continue
		}
		id, ok := byName[strings.ToLower(t.Name)]
		if !ok || seen[id] {
			continue
		}
		seen[id] = true
		ids = append(ids, id)
	}
	if err := SetTaskTags(taskID, userID, ids); err != nil {
		return err
	}
	if wasArchived {
		return ApplyArchivedTag(taskID, userID)
	}
	return nil
}

// GetTagsForTask returns tags assigned to a single task.
func GetTagsForTask(taskID int) ([]Tag, error) {
	pool, err := OpenDatabase()
	if err != nil {
		return nil, err
	}
	defer CloseDatabase(pool)

	rows, err := pool.Query(context.Background(), `
		SELECT `+tagSelectCols+`
		FROM tags tg
		JOIN task_tags tt ON tt.tag_id = tg.id
		WHERE tt.task_id = $1
		ORDER BY LOWER(tg.name)`, taskID)
	if err != nil {
		return nil, err
	}
	return collectTags(rows)
}

// GetTagsForTasks batch-loads tags for multiple tasks.
func GetTagsForTasks(taskIDs []int) (map[int][]Tag, error) {
	result := make(map[int][]Tag)
	if len(taskIDs) == 0 {
		return result, nil
	}

	pool, err := OpenDatabase()
	if err != nil {
		return nil, err
	}
	defer CloseDatabase(pool)

	rows, err := pool.Query(context.Background(), `
		SELECT tt.task_id, `+qualifyTagSelect("tg")+`
		FROM task_tags tt
		JOIN tags tg ON tg.id = tt.tag_id
		WHERE tt.task_id = ANY($1)
		ORDER BY tt.task_id, LOWER(tg.name)`, taskIDs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var taskID int
		var projectID sql.NullInt64
		var t Tag
		if err := rows.Scan(&taskID, &t.ID, &t.UserID, &projectID, &t.Name, &t.Color, &t.Protected); err != nil {
			return nil, err
		}
		if projectID.Valid && projectID.Int64 > 0 {
			pid := int(projectID.Int64)
			t.ProjectID = &pid
		}
		result[taskID] = append(result[taskID], t)
	}
	return result, nil
}

func qualifyTagSelect(alias string) string {
	return fmt.Sprintf(`%s.id, %s.user_id, %s.project_id, %s.name, COALESCE(%s.color, '#6c757d'), COALESCE(%s.protected, false)`,
		alias, alias, alias, alias, alias, alias)
}

// ResolveTaskTagIDs parses tag_ids from form and creates new tags from comma-separated names.
func ResolveTaskTagIDs(userID int, projectID *int, tagIDStrs []string, newTagsCSV string) ([]int, error) {
	seen := make(map[int]bool)
	var ids []int

	for _, s := range tagIDStrs {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		id, err := parseInt(s)
		if err != nil {
			continue
		}
		if seen[id] {
			continue
		}
		tag, err := GetTag(id)
		if err != nil || tag.Protected || IsSystemTagName(tag.Name) {
			continue
		}
		seen[id] = true
		ids = append(ids, id)
	}

	for _, part := range strings.Split(newTagsCSV, ",") {
		name := strings.TrimSpace(part)
		if name == "" || IsSystemTagName(name) {
			continue
		}
		t, err := GetOrCreateTagByName(userID, projectID, name)
		if err != nil {
			return nil, err
		}
		if t.Protected || seen[t.ID] {
			continue
		}
		seen[t.ID] = true
		ids = append(ids, t.ID)
	}

	if len(ids) > MaxTagsPerTask {
		return nil, fmt.Errorf("maximum %d tags per task", MaxTagsPerTask)
	}
	return ids, nil
}

// EnsureRemovedTag delegates to EnsureArchivedTag as the system-level archive tag is now "archived".
func EnsureRemovedTag(userID int, projectID *int) (*Tag, error) {
	return EnsureArchivedTag(userID, projectID)
}

// ApplyRemovedTag delegates to ApplyArchivedTag as the system-level archive tag is now "archived".
func ApplyRemovedTag(taskID, userID int) error {
	return ApplyArchivedTag(taskID, userID)
}

// EnsureArchivedTag returns the protected "archived" tag for a namespace, creating or converting it.
func EnsureArchivedTag(userID int, projectID *int) (*Tag, error) {
	if existing, err := FindTagByName(userID, projectID, ArchivedTagName); err == nil {
		if !existing.Protected {
			pool, err := OpenDatabase()
			if err != nil {
				return nil, err
			}
			defer CloseDatabase(pool)
			_, err = pool.Exec(context.Background(),
				`UPDATE tags SET protected = true, name = $1, color = COALESCE(NULLIF(color, ''), $2) WHERE id = $3`,
				ArchivedTagName, archivedTagColor, existing.ID)
			if err != nil {
				return nil, fmt.Errorf("failed to protect archived tag: %v", err)
			}
			existing.Protected = true
			existing.Name = ArchivedTagName
		}
		return existing, nil
	}

	pool, err := OpenDatabase()
	if err != nil {
		return nil, err
	}
	defer CloseDatabase(pool)

	var t Tag
	if projectID == nil || *projectID <= 0 {
		t, err = scanTag(pool.QueryRow(context.Background(),
			`INSERT INTO tags (user_id, name, color, protected) VALUES ($1, $2, $3, true)
			 RETURNING `+tagSelectCols, userID, ArchivedTagName, archivedTagColor))
	} else {
		t, err = scanTag(pool.QueryRow(context.Background(),
			`INSERT INTO tags (user_id, project_id, name, color, protected) VALUES ($1, $2, $3, $4, true)
			 RETURNING `+tagSelectCols, userID, *projectID, ArchivedTagName, archivedTagColor))
	}
	if err != nil {
		if existing, findErr := FindTagByName(userID, projectID, ArchivedTagName); findErr == nil {
			return existing, nil
		}
		return nil, fmt.Errorf("failed to create archived tag: %v", err)
	}
	return &t, nil
}

// ApplyArchivedTag attaches the namespace's protected archived tag to a task.
func ApplyArchivedTag(taskID, userID int) error {
	ns, err := taskTagNamespace(taskID)
	if err != nil {
		return err
	}
	tag, err := EnsureArchivedTag(userID, ns)
	if err != nil {
		return err
	}
	pool, err := OpenDatabase()
	if err != nil {
		return err
	}
	defer CloseDatabase(pool)
	_, err = pool.Exec(context.Background(),
		`INSERT INTO task_tags (task_id, tag_id) VALUES ($1, $2)
		 ON CONFLICT (task_id, tag_id) DO NOTHING`, taskID, tag.ID)
	if err != nil {
		return fmt.Errorf("failed to tag archived task: %v", err)
	}
	return nil
}

// ApplyArchivedTagToProjectTasks tags every task in the project with the protected archived tag.
func ApplyArchivedTagToProjectTasks(projectID, userID int) error {
	if projectID <= 0 {
		return fmt.Errorf("invalid project_id")
	}
	pid := projectID
	tag, err := EnsureArchivedTag(userID, &pid)
	if err != nil {
		return err
	}
	pool, err := OpenDatabase()
	if err != nil {
		return err
	}
	defer CloseDatabase(pool)
	_, err = pool.Exec(context.Background(),
		`INSERT INTO task_tags (task_id, tag_id)
		 SELECT t.id, $1 FROM tasks t WHERE t.project_id = $2
		 ON CONFLICT (task_id, tag_id) DO NOTHING`, tag.ID, projectID)
	if err != nil {
		return fmt.Errorf("failed to tag archived project tasks: %v", err)
	}
	return nil
}

// ClearArchivedTagFromProjectTasks removes the protected archived tag from every task in the project.
func ClearArchivedTagFromProjectTasks(projectID, userID int) error {
	if projectID <= 0 {
		return nil
	}
	pid := projectID
	tag, err := FindTagByName(userID, &pid, ArchivedTagName)
	if err != nil {
		return nil
	}
	pool, err := OpenDatabase()
	if err != nil {
		return err
	}
	defer CloseDatabase(pool)
	_, err = pool.Exec(context.Background(),
		`DELETE FROM task_tags tt
		 USING tasks t
		 WHERE tt.task_id = t.id AND tt.tag_id = $1 AND t.project_id = $2`,
		tag.ID, projectID)
	if err != nil {
		return fmt.Errorf("failed to clear archived project tags: %v", err)
	}
	return nil
}

// ClearArchivedTag restores a task by removing the protected archived tag (and legacy removed tag).
func ClearArchivedTag(taskID, userID int) error {
	pool, err := OpenDatabase()
	if err != nil {
		return err
	}
	defer CloseDatabase(pool)
	_, err = pool.Exec(context.Background(),
		`DELETE FROM task_tags tt
		 USING tags tg
		 WHERE tt.task_id = $1 AND tt.tag_id = tg.id AND (LOWER(tg.name) = LOWER($2) OR LOWER(tg.name) = LOWER($3))`,
		taskID, ArchivedTagName, RemovedTagName)
	if err != nil {
		return fmt.Errorf("failed to restore task: %v", err)
	}
	return nil
}

// ClearRemovedTag restores a task by removing the protected archive tag.
func ClearRemovedTag(taskID, userID int) error {
	return ClearArchivedTag(taskID, userID)
}

// TaskHasArchivedTag reports whether the task currently has the protected archived (or legacy removed) tag.
func TaskHasArchivedTag(taskID int) (bool, error) {
	tags, err := GetTagsForTask(taskID)
	if err != nil {
		return false, err
	}
	for _, t := range tags {
		if IsArchivedTagName(t.Name) || IsRemovedTagName(t.Name) {
			return true, nil
		}
	}
	return false, nil
}

// TaskHasRemovedTag reports whether the task currently has the protected archive tag.
func TaskHasRemovedTag(taskID int) (bool, error) {
	return TaskHasArchivedTag(taskID)
}

// ArchivedTaskExistsSQL is a SQL EXISTS clause for tasks with the protected archived tag.
func ArchivedTaskExistsSQL(taskIDExpr string) string {
	if taskIDExpr == "" || taskIDExpr == "id" {
		taskIDExpr = "tasks.id"
	}
	return fmt.Sprintf(`EXISTS (
		SELECT 1 FROM task_tags tt
		JOIN tags tg ON tg.id = tt.tag_id
		WHERE tt.task_id = %s AND tg.protected = true AND (LOWER(tg.name) = LOWER('%s') OR LOWER(tg.name) = LOWER('%s'))
	)`, taskIDExpr, ArchivedTagName, RemovedTagName)
}

func migrateRenameRemovedTagsToArchived(pool *pgxpool.Pool) error {
	ctx := context.Background()

	// Handle namespaces that have both a 'removed' tag and an 'archived' tag
	rows, err := pool.Query(ctx, `
		SELECT r.id, a.id
		FROM tags r
		JOIN tags a ON (
			(r.project_id IS NULL AND a.project_id IS NULL AND r.user_id = a.user_id)
			OR (r.project_id IS NOT NULL AND a.project_id IS NOT NULL AND r.project_id = a.project_id)
		)
		WHERE LOWER(r.name) = LOWER($1) AND LOWER(a.name) = LOWER($2)`,
		RemovedTagName, ArchivedTagName)
	if err == nil {
		type idPair struct{ removedID, archivedID int }
		var pairs []idPair
		for rows.Next() {
			var p idPair
			if scanErr := rows.Scan(&p.removedID, &p.archivedID); scanErr == nil {
				pairs = append(pairs, p)
			}
		}
		rows.Close()

		for _, p := range pairs {
			_, _ = pool.Exec(ctx, `
				INSERT INTO task_tags (task_id, tag_id)
				SELECT task_id, $1 FROM task_tags WHERE tag_id = $2
				ON CONFLICT (task_id, tag_id) DO NOTHING`, p.archivedID, p.removedID)
			_, _ = pool.Exec(ctx, `DELETE FROM task_tags WHERE tag_id = $1`, p.removedID)
			_, _ = pool.Exec(ctx, `DELETE FROM tags WHERE id = $1`, p.removedID)
		}
	}

	// Rename any remaining 'removed' tags to 'archived' and protect them
	if _, err := pool.Exec(ctx,
		`UPDATE tags SET protected = true, name = $1 WHERE LOWER(name) = LOWER($2)`,
		ArchivedTagName, RemovedTagName); err != nil {
		return fmt.Errorf("failed to rename removed tags to archived: %v", err)
	}

	// Ensure all 'archived' tags are protected
	if _, err := pool.Exec(ctx,
		`UPDATE tags SET protected = true WHERE LOWER(name) = LOWER($1)`,
		ArchivedTagName); err != nil {
		return fmt.Errorf("failed to protect archived tags: %v", err)
	}

	return nil
}

// MigrateTagsAddProtected adds the protected column, renames removed tags to archived, and seeds archived tags per namespace.
func MigrateTagsAddProtected() error {
	pool, err := OpenDatabase()
	if err != nil {
		return err
	}
	defer CloseDatabase(pool)

	if _, err := pool.Exec(context.Background(),
		`ALTER TABLE tags ADD COLUMN IF NOT EXISTS protected BOOLEAN NOT NULL DEFAULT false`); err != nil {
		return fmt.Errorf("failed to add tags.protected: %v", err)
	}

	// Drop leftover UNIQUE(user_id, name) first or PostgreSQL raises 23505.
	if err := dropLegacyTagsUserNameUnique(pool); err != nil {
		return err
	}

	if err := migrateRenameRemovedTagsToArchived(pool); err != nil {
		return err
	}

	if tableExists(pool, "users") {
		if _, err := pool.Exec(context.Background(), `
			INSERT INTO tags (user_id, name, color, protected)
			SELECT u.id, $1, $2, true
			FROM users u
			WHERE NOT EXISTS (
				SELECT 1 FROM tags t
				WHERE t.project_id IS NULL AND t.user_id = u.id AND LOWER(t.name) = LOWER($1)
			)`, ArchivedTagName, archivedTagColor); err != nil {
			return fmt.Errorf("failed to seed personal archived tags: %v", err)
		}
	}
	if tableExists(pool, "projects") {
		if _, err := pool.Exec(context.Background(), `
			INSERT INTO tags (user_id, project_id, name, color, protected)
			SELECT p.user_id, p.id, $1, $2, true
			FROM projects p
			WHERE NOT EXISTS (
				SELECT 1 FROM tags t
				WHERE t.project_id = p.id AND LOWER(t.name) = LOWER($1)
			)`, ArchivedTagName, archivedTagColor); err != nil {
			return fmt.Errorf("failed to seed project archived tags: %v", err)
		}
	}
	return nil
}

func parseInt(s string) (int, error) {
	var n int
	_, err := fmt.Sscanf(s, "%d", &n)
	return n, err
}
