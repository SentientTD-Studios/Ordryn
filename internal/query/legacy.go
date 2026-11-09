package query

import (
	"encoding/json"
	"strconv"
	"strings"

	"GoTodo/internal/storage"
)

// FromLegacyFilter converts a version 1 saved view (a flat task-list filter)
// into a definition that selects the same tasks. tagName resolves a tag id to
// its name; ids it cannot resolve are dropped.
func FromLegacyFilter(f storage.SavedViewFilter, tagName func(id int) (string, bool)) Definition {
	d := DefaultDefinition()
	add := func(field, op string, value any) {
		n := Node{Field: field, Operator: op}
		if value != nil {
			n.Value, _ = json.Marshal(value)
		}
		d.Where.Clauses = append(d.Where.Clauses, n)
	}

	switch p := strings.ToLower(strings.TrimSpace(f.Project)); p {
	case "":
	case "none", "0":
		add("project", OpEmpty, nil)
	default:
		if id, err := strconv.Atoi(p); err == nil && id > 0 {
			no := false
			d.Scope.Projects = []int{id}
			d.Scope.IncludePersonal = &no
		}
	}

	switch strings.ToLower(strings.TrimSpace(f.Status)) {
	case "complete", "completed":
		add("completed", OpEq, true)
	case "incomplete":
		add("completed", OpEq, false)
	}

	switch strings.ToLower(strings.TrimSpace(f.Due)) {
	case "overdue":
		add("due_date", OpLt, "@today")
		add("completed", OpEq, false)
	case "today":
		add("due_date", OpEq, "@today")
	case "week":
		add("due_date", OpBetween, []string{"@today", "@today+7"})
	case "through_week":
		add("due_date", OpBetween, []string{"@today", "@end_of_week"})
		add("completed", OpEq, false)
	case "none":
		add("due_date", OpEmpty, nil)
	}
	if s := strings.TrimSpace(f.DueFrom); s != "" {
		add("due_date", OpGe, s)
	}
	if s := strings.TrimSpace(f.DueTo); s != "" {
		add("due_date", OpLe, s)
	}

	if strings.ToLower(strings.TrimSpace(f.Completed)) == "week" {
		add("completed", OpEq, true)
		add("completed_date", OpGe, "@start_of_week")
	}

	if p := strings.TrimSpace(f.Priority); p != "" {
		var vals []int
		for _, part := range strings.Split(p, ",") {
			if n, err := strconv.Atoi(strings.TrimSpace(part)); err == nil && n >= 0 && n <= 3 {
				vals = append(vals, n)
			}
		}
		if len(vals) == 1 {
			add("priority", OpEq, vals[0])
		} else if len(vals) > 1 {
			add("priority", OpIn, vals)
		}
	}

	if t := strings.TrimSpace(f.Tag); t != "" {
		names := []string{}
		for _, part := range strings.Split(t, ",") {
			part = strings.TrimSpace(part)
			if part == "" {
				continue
			}
			if id, err := strconv.Atoi(part); err == nil {
				if tagName != nil {
					if name, ok := tagName(id); ok {
						names = append(names, name)
					}
				}
				continue
			}
			names = append(names, part)
		}
		if len(names) > 0 {
			add("tags", OpIn, names)
		}
	}

	switch strings.ToLower(strings.TrimSpace(f.Claimed)) {
	case "me", "mine":
		add("claimed_by", OpEq, MacroMe)
	case "none", "unclaimed":
		add("claimed_by", OpEmpty, nil)
	}

	if s := strings.TrimSpace(f.Search); s != "" {
		add("text", OpContains, s)
	}
	if strings.ToLower(strings.TrimSpace(f.Sort)) == "priority" {
		d.Sort = []SortKey{{Field: "priority", Dir: "desc"}}
	}
	if norm, err := Normalize(d, nil); err == nil {
		return norm
	}
	return d
}

// BackfillSavedViews gives every version 1 saved view a definition built from
// its filter. Views keep their filter, so older clients still work. Safe to
// re-run; it only touches views without a definition.
func BackfillSavedViews() error {
	views, err := storage.ListSavedViewsMissingDefinition()
	if err != nil {
		return err
	}
	tagName := func(id int) (string, bool) {
		t, err := storage.GetTag(id)
		if err != nil || t == nil {
			return "", false
		}
		return t.Name, true
	}
	for _, v := range views {
		def := FromLegacyFilter(v.Filter, tagName)
		raw, err := json.Marshal(def)
		if err != nil {
			return err
		}
		if err := storage.SetSavedViewDefinitionIfMissing(v.ID, raw); err != nil {
			return err
		}
	}
	return nil
}
