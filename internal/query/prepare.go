package query

import (
	"strings"
	"time"

	"GoTodo/internal/extensions"
	"GoTodo/internal/storage"
)

// Unavailable lists references in a saved query the caller can no longer use.
// They match nothing when the query runs.
type Unavailable struct {
	Projects      []int    `json:"projects,omitempty"`
	Organizations []int    `json:"organizations,omitempty"`
	Sprints       []int    `json:"sprints,omitempty"`
	Users         []int    `json:"users,omitempty"`
	CustomFields  []string `json:"custom_fields,omitempty"`
}

// Empty reports whether nothing is unavailable.
func (u Unavailable) Empty() bool {
	return len(u.Projects) == 0 && len(u.Organizations) == 0 && len(u.Sprints) == 0 && len(u.Users) == 0 && len(u.CustomFields) == 0
}

// ErrUnknownReference is the one error for a reference that does not exist or
// that the caller cannot see; the two cases are deliberately indistinguishable.
var ErrUnknownReference = &ValidationError{msg: "The query refers to a project, sprint, person, or field that does not exist or that you cannot access."}

// Loader fetches the caller's access. Tests can replace it.
type Loader struct {
	AccessibleProjects func(userID int) ([]int, error)
	CustomFields       func(projectIDs []int) (map[string]CustomField, error)
	VisibleSprints     func(ids, projectIDs []int) (map[int]bool, error)
	VisiblePeople      func(userID int, ids, projectIDs []int) (map[int]bool, error)
	// VisibleOrganizations returns which of ids the caller may name: those
	// they belong to. ProjectOrganizations maps projects to their
	// organization. Both are only called when a scope names organizations;
	// nil means none are visible.
	VisibleOrganizations func(userID int, ids []int) (map[int]bool, error)
	ProjectOrganizations func(projectIDs []int) (map[int]int, error)
}

// DBLoader reads access from the database.
var DBLoader = Loader{
	AccessibleProjects: func(userID int) ([]int, error) {
		projs, err := storage.GetAccessibleProjects(userID)
		if err != nil {
			return nil, err
		}
		ids := make([]int, 0, len(projs))
		for _, p := range projs {
			ids = append(ids, p.ID)
		}
		return ids, nil
	},
	CustomFields: func(projectIDs []int) (map[string]CustomField, error) {
		fields, err := storage.QueryableCustomFields(projectIDs, extensions.FieldExtensionIDs())
		if err != nil {
			return nil, err
		}
		out := make(map[string]CustomField, len(fields))
		for k, f := range fields {
			out[k] = CustomField{Type: f.Def.Type, Projects: f.Projects}
		}
		return out, nil
	},
	VisibleSprints:       storage.VisibleSprintIDs,
	VisibleOrganizations: storage.VisibleQueryOrganizations,
	ProjectOrganizations: storage.ProjectOrganizationIDs,
	VisiblePeople: func(userID int, ids, projectIDs []int) (map[int]bool, error) {
		people, err := storage.VisibleQueryPeople(userID, ids, projectIDs)
		if err != nil {
			return nil, err
		}
		out := make(map[int]bool, len(people))
		for _, p := range people {
			out[p.ID] = true
		}
		return out, nil
	},
}

// Request is one query to prepare.
type Request struct {
	UserID   int
	Timezone string
	Now      time.Time
	// ForceProject pins the query to one project: a shared query, or a
	// project-scoped API key. References outside it are unavailable.
	ForceProject int
	// Lenient is for running a saved query: references the caller has lost
	// access to match nothing and are reported instead of failing.
	Lenient bool
}

// Prepare normalizes d and loads everything Compile needs, checking every
// reference against the caller's access. In strict mode any unavailable
// reference fails with ErrUnknownReference.
func (l Loader) Prepare(req Request, d Definition) (Definition, Context, Unavailable, error) {
	var unavailable Unavailable
	ctx := Context{
		UserID:       req.UserID,
		Timezone:     req.Timezone,
		Today:        localToday(req.Now, req.Timezone),
		Projects:     map[int]bool{},
		Sprints:      map[int]bool{},
		Users:        map[int]bool{},
		CustomFields: map[string]CustomField{},
		ForceProject: req.ForceProject,
	}

	accessible, err := l.AccessibleProjects(req.UserID)
	if err != nil {
		return d, ctx, unavailable, err
	}
	for _, id := range accessible {
		if req.ForceProject > 0 && id != req.ForceProject {
			continue
		}
		ctx.Projects[id] = true
	}
	refProjects := make([]int, 0, len(ctx.Projects))
	for id := range ctx.Projects {
		refProjects = append(refProjects, id)
	}

	if mentionsCustomField(d) && len(refProjects) > 0 {
		fields, err := l.CustomFields(refProjects)
		if err != nil {
			return d, ctx, unavailable, err
		}
		ctx.CustomFields = fields
	}

	var norm Definition
	if req.Lenient {
		var lost []string
		norm, lost, err = NormalizeLenient(d, ctx)
		unavailable.CustomFields = lost
	} else {
		norm, err = Normalize(d, ctx)
		if err != nil && IsValidationError(err) && mentionsCustomField(d) && unknownCustomField(d, ctx) {
			// Same answer for a missing field and a hidden one.
			return d, ctx, unavailable, ErrUnknownReference
		}
	}
	if err != nil {
		return d, ctx, unavailable, err
	}

	refs := Refs(norm, ctx)
	for _, id := range refs.Projects {
		if !ctx.Projects[id] {
			unavailable.Projects = append(unavailable.Projects, id)
		}
	}
	if len(refs.Organizations) > 0 {
		ctx.Orgs = map[int]bool{}
		ctx.ProjectOrgs = map[int]int{}
		var ok map[int]bool
		if l.VisibleOrganizations != nil {
			if ok, err = l.VisibleOrganizations(req.UserID, refs.Organizations); err != nil {
				return norm, ctx, unavailable, err
			}
		}
		for _, id := range refs.Organizations {
			if ok[id] {
				ctx.Orgs[id] = true
			} else {
				unavailable.Organizations = append(unavailable.Organizations, id)
			}
		}
		if len(ctx.Orgs) > 0 && l.ProjectOrganizations != nil && len(refProjects) > 0 {
			if ctx.ProjectOrgs, err = l.ProjectOrganizations(refProjects); err != nil {
				return norm, ctx, unavailable, err
			}
		}
	}
	if len(refs.Sprints) > 0 {
		ok, err := l.VisibleSprints(refs.Sprints, refProjects)
		if err != nil {
			return norm, ctx, unavailable, err
		}
		for _, id := range refs.Sprints {
			if ok[id] {
				ctx.Sprints[id] = true
			} else {
				unavailable.Sprints = append(unavailable.Sprints, id)
			}
		}
	}
	others := make([]int, 0, len(refs.Users))
	for _, id := range refs.Users {
		if id != req.UserID {
			others = append(others, id)
		}
	}
	if len(others) > 0 {
		ok, err := l.VisiblePeople(req.UserID, others, refProjects)
		if err != nil {
			return norm, ctx, unavailable, err
		}
		for _, id := range others {
			if ok[id] {
				ctx.Users[id] = true
			} else {
				unavailable.Users = append(unavailable.Users, id)
			}
		}
	}
	if !req.Lenient && !unavailable.Empty() {
		return norm, ctx, unavailable, ErrUnknownReference
	}
	return norm, ctx, unavailable, nil
}

func mentionsCustomField(d Definition) bool {
	found := false
	var walk func(n Node)
	walk = func(n Node) {
		if strings.HasPrefix(strings.ToLower(strings.TrimSpace(n.Field)), "cf:") {
			found = true
		}
		for _, c := range n.Clauses {
			walk(c)
		}
	}
	walk(d.Where)
	for _, c := range d.Columns {
		if strings.HasPrefix(strings.ToLower(strings.TrimSpace(c)), "cf:") {
			found = true
		}
	}
	return found
}

func unknownCustomField(d Definition, ft FieldTypes) bool {
	unknown := false
	check := func(field string) {
		f := canonicalFieldName(field)
		if strings.HasPrefix(f, "cf:") {
			if _, ok := ft.CustomFieldType(strings.TrimPrefix(f, "cf:")); !ok {
				unknown = true
			}
		}
	}
	var walk func(n Node)
	walk = func(n Node) {
		check(n.Field)
		for _, c := range n.Clauses {
			walk(c)
		}
	}
	walk(d.Where)
	for _, c := range d.Columns {
		check(c)
	}
	return unknown
}

func localToday(now time.Time, tz string) time.Time {
	if now.IsZero() {
		now = time.Now()
	}
	loc, err := time.LoadLocation(tz)
	if err != nil || tz == "" {
		loc = time.UTC
	}
	l := now.In(loc)
	return time.Date(l.Year(), l.Month(), l.Day(), 0, 0, 0, 0, time.UTC)
}
