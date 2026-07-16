package tasks

import (
	"fmt"
	"strings"
)

// ListFilters holds query filters for task list and search endpoints.
type ListFilters struct {
	ProjectFilter   *int
	StatusFilter    string
	DueFilter       string
	CompletedFilter string
	PriorityFilter  *int
	TagFilter       *int
	Sort            string
}

func (f ListFilters) projectCondition(tablePrefix string) string {
	prefix := ""
	if tablePrefix != "" {
		prefix = tablePrefix + "."
	}
	if f.ProjectFilter == nil {
		return ""
	}
	if *f.ProjectFilter == 0 {
		return fmt.Sprintf(" AND (%sproject_id IS NULL)", prefix)
	}
	return fmt.Sprintf(" AND (%sproject_id = %d)", prefix, *f.ProjectFilter)
}

func (f ListFilters) statusCondition(tablePrefix string) string {
	prefix := ""
	if tablePrefix != "" {
		prefix = tablePrefix + "."
	}
	switch normalizeListStatusFilter(f.StatusFilter) {
	case "complete":
		return fmt.Sprintf(" AND %scompleted = true", prefix)
	case "incomplete":
		return fmt.Sprintf(" AND (%scompleted IS NULL OR %scompleted = false)", prefix, prefix)
	default:
		return ""
	}
}

func normalizeListStatusFilter(status string) string {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "complete", "completed":
		return "complete"
	case "incomplete":
		return "incomplete"
	default:
		return ""
	}
}

func (f ListFilters) priorityCondition(tablePrefix string) string {
	prefix := ""
	if tablePrefix != "" {
		prefix = tablePrefix + "."
	}
	if f.PriorityFilter == nil {
		return ""
	}
	return fmt.Sprintf(" AND (%spriority = %d)", prefix, *f.PriorityFilter)
}

func (f ListFilters) orderByClause(tablePrefix string) string {
	prefix := ""
	if tablePrefix != "" {
		prefix = tablePrefix + "."
	}
	if f.Sort == "priority" {
		return fmt.Sprintf(" ORDER BY %spriority DESC, %sposition", prefix, prefix)
	}
	return fmt.Sprintf(" ORDER BY %sposition", prefix)
}

func (f ListFilters) appendConditions(baseWhere string, timezone string, tablePrefix string, args []interface{}) (string, []interface{}) {
	return appendFilterSQL(baseWhere, args, f, timezone, tablePrefix)
}
