package storage

import (
	"context"
	"encoding/json"
	"fmt"
)

// Lookups for saved queries: what a caller may reference in a query. Each one
// only answers for things inside projects the caller can open, so a query can
// never be used to learn about anything else.

// VisibleSprintIDs returns which of ids are sprints in one of projectIDs.
func VisibleSprintIDs(ids, projectIDs []int) (map[int]bool, error) {
	out := map[int]bool{}
	if len(ids) == 0 || len(projectIDs) == 0 {
		return out, nil
	}
	pool, err := OpenDatabase()
	if err != nil {
		return nil, err
	}
	defer CloseDatabase(pool)
	rows, err := pool.Query(context.Background(),
		`SELECT id FROM project_sprints WHERE id = ANY($1) AND project_id = ANY($2)`, ids, projectIDs)
	if err != nil {
		return nil, fmt.Errorf("visible sprints: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var id int
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out[id] = true
	}
	return out, rows.Err()
}

// VisibleQueryOrganizations returns which of ids are organizations callerID
// belongs to. The company organization is left out: a site-wide query covers it.
func VisibleQueryOrganizations(callerID int, ids []int) (map[int]bool, error) {
	out := map[int]bool{}
	if len(ids) == 0 {
		return out, nil
	}
	pool, err := OpenDatabase()
	if err != nil {
		return nil, err
	}
	defer CloseDatabase(pool)
	rows, err := pool.Query(context.Background(), `
		SELECT o.id FROM organizations o
		JOIN organization_members om ON om.organization_id = o.id AND om.user_id = $1
		WHERE o.id = ANY($2) AND NOT COALESCE(o.is_company, FALSE)`, callerID, ids)
	if err != nil {
		return nil, fmt.Errorf("visible organizations: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var id int
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out[id] = true
	}
	return out, rows.Err()
}

// ProjectOrganizationIDs maps each of projectIDs that belongs to an
// organization to that organization's id.
func ProjectOrganizationIDs(projectIDs []int) (map[int]int, error) {
	out := map[int]int{}
	if len(projectIDs) == 0 {
		return out, nil
	}
	pool, err := OpenDatabase()
	if err != nil {
		return nil, err
	}
	defer CloseDatabase(pool)
	rows, err := pool.Query(context.Background(),
		`SELECT id, organization_id FROM projects WHERE id = ANY($1) AND organization_id IS NOT NULL`, projectIDs)
	if err != nil {
		return nil, fmt.Errorf("project organizations: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var pid, oid int
		if err := rows.Scan(&pid, &oid); err != nil {
			return nil, err
		}
		out[pid] = oid
	}
	return out, rows.Err()
}

// QueryPerson is someone a caller may name in a query.
type QueryPerson struct {
	ID   int
	Name string
}

// visiblePeopleSQL selects users ($2 = candidate ids or NULL for all) who are
// the caller ($1) or belong to (own or are members of) one of the projects in
// $3. The caller passes only projects they can open.
const visiblePeopleSQL = `
	SELECT u.id, COALESCE(NULLIF(u.full_name, ''), NULLIF(u.user_name, ''), u.email, '')
	FROM users u
	WHERE ($2::int[] IS NULL OR u.id = ANY($2))
	  AND (u.id = $1 OR EXISTS (
		SELECT 1 FROM projects vp
		WHERE vp.id = ANY($3)
		  AND (vp.user_id = u.id OR EXISTS (
			SELECT 1 FROM project_members vm WHERE vm.project_id = vp.id AND vm.user_id = u.id))
	  ))
	ORDER BY LOWER(COALESCE(NULLIF(u.full_name, ''), NULLIF(u.user_name, ''), u.email, '')), u.id`

// VisibleQueryPeople lists the people callerID may reference: themselves and
// everyone on the given projects (which must be projects the caller can open).
// With ids non-nil, only those candidates are checked.
func VisibleQueryPeople(callerID int, ids []int, projectIDs []int) ([]QueryPerson, error) {
	if ids != nil && len(ids) == 0 {
		return []QueryPerson{}, nil
	}
	if projectIDs == nil {
		projectIDs = []int{}
	}
	pool, err := OpenDatabase()
	if err != nil {
		return nil, err
	}
	defer CloseDatabase(pool)
	var idArg any
	if ids != nil {
		idArg = ids
	}
	rows, err := pool.Query(context.Background(), visiblePeopleSQL, callerID, idArg, projectIDs)
	if err != nil {
		return nil, fmt.Errorf("visible people: %w", err)
	}
	defer rows.Close()
	out := []QueryPerson{}
	for rows.Next() {
		var p QueryPerson
		if err := rows.Scan(&p.ID, &p.Name); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// QueryableField is a custom field with the projects it applies to.
type QueryableField struct {
	Def      CustomFieldDef
	Projects []int
}

// QueryableCustomFields returns the active custom fields of the given
// extensions that apply to at least one of projectIDs, keyed by field key.
// A field applies to a project when its extension is enabled site-wide and on
// that project, the same rule the task sidebar uses.
func QueryableCustomFields(projectIDs []int, extensionIDs []string) (map[string]QueryableField, error) {
	out := map[string]QueryableField{}
	if len(projectIDs) == 0 || len(extensionIDs) == 0 {
		return out, nil
	}
	siteEnabled := make([]string, 0, len(extensionIDs))
	for _, id := range extensionIDs {
		s, err := GetExtensionSettings(id)
		if err != nil {
			return nil, err
		}
		if s.Enabled {
			siteEnabled = append(siteEnabled, id)
		}
	}
	if len(siteEnabled) == 0 {
		return out, nil
	}

	pool, err := OpenDatabase()
	if err != nil {
		return nil, err
	}
	defer CloseDatabase(pool)
	rows, err := pool.Query(context.Background(),
		`SELECT extension_id, project_id, data FROM extension_project_settings
		 WHERE extension_id = ANY($1) AND project_id = ANY($2)
		 ORDER BY project_id`, siteEnabled, projectIDs)
	if err != nil {
		return nil, fmt.Errorf("queryable custom fields: %w", err)
	}
	projectsByExt := map[string][]int{}
	for rows.Next() {
		var ext string
		var pid int
		var raw []byte
		if err := rows.Scan(&ext, &pid, &raw); err != nil {
			rows.Close()
			return nil, err
		}
		settings := emptyExtensionProjectSettings()
		if len(raw) > 0 && json.Unmarshal(raw, &settings) != nil {
			continue
		}
		if settings.Enabled {
			projectsByExt[ext] = append(projectsByExt[ext], pid)
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	exts := make([]string, 0, len(projectsByExt))
	for ext := range projectsByExt {
		exts = append(exts, ext)
	}
	defs, err := ListCustomFieldDefsForExtensions(exts)
	if err != nil {
		return nil, err
	}
	for _, d := range defs {
		out[d.FieldKey] = QueryableField{Def: d, Projects: projectsByExt[d.ExtensionID]}
	}
	return out, nil
}

// QueryOptionNames lists distinct workflow status names, tag names, and
// sprints across the given projects (which must be projects the caller can
// open), plus the caller's own tags without a project.
type QueryOptionNames struct {
	Statuses []string
	Tags     []string
	Sprints  []QuerySprint
}

// QuerySprint is one sprint a query can name.
type QuerySprint struct {
	ID        int
	ProjectID int
	Name      string
	Active    bool
}

// LoadQueryOptionNames loads the picker values for a query editor.
func LoadQueryOptionNames(callerID int, projectIDs []int) (QueryOptionNames, error) {
	out := QueryOptionNames{Statuses: []string{}, Tags: []string{}, Sprints: []QuerySprint{}}
	if projectIDs == nil {
		projectIDs = []int{}
	}
	pool, err := OpenDatabase()
	if err != nil {
		return out, err
	}
	defer CloseDatabase(pool)
	ctx := context.Background()

	strs := func(q string, args ...any) ([]string, error) {
		rows, err := pool.Query(ctx, q, args...)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		list := []string{}
		for rows.Next() {
			var s string
			if err := rows.Scan(&s); err != nil {
				return nil, err
			}
			list = append(list, s)
		}
		return list, rows.Err()
	}
	if out.Statuses, err = strs(`
		SELECT MIN(name) FROM project_statuses WHERE project_id = ANY($1)
		GROUP BY LOWER(name) ORDER BY LOWER(MIN(name))`, projectIDs); err != nil {
		return out, fmt.Errorf("query statuses: %w", err)
	}
	if out.Tags, err = strs(`
		SELECT MIN(name) FROM tags
		WHERE project_id = ANY($1) OR (project_id IS NULL AND user_id = $2)
		GROUP BY LOWER(name) ORDER BY LOWER(MIN(name))`, projectIDs, callerID); err != nil {
		return out, fmt.Errorf("query tags: %w", err)
	}
	rows, err := pool.Query(ctx, `
		SELECT id, project_id, name,
		       COALESCE(start_date <= CURRENT_DATE AND end_date >= CURRENT_DATE, false)
		FROM project_sprints WHERE project_id = ANY($1)
		ORDER BY project_id, start_date NULLS LAST, id`, projectIDs)
	if err != nil {
		return out, fmt.Errorf("query sprints: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var s QuerySprint
		if err := rows.Scan(&s.ID, &s.ProjectID, &s.Name, &s.Active); err != nil {
			return out, err
		}
		out.Sprints = append(out.Sprints, s)
	}
	return out, rows.Err()
}
