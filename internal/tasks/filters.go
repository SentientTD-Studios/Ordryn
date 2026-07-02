package tasks

import (
	"fmt"
	"strings"
)

func normalizeStatusFilter(status string) string {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "complete", "completed":
		return "complete"
	case "incomplete":
		return "incomplete"
	default:
		return ""
	}
}

func projectFilterSQL(projectFilter *int, tableAlias string) string {
	if projectFilter == nil {
		return ""
	}
	col := qualifyColumn("project_id", tableAlias)
	if *projectFilter == 0 {
		return fmt.Sprintf(" AND (%s IS NULL)", col)
	}
	return fmt.Sprintf(" AND (%s = %d)", col, *projectFilter)
}

func statusFilterSQL(statusFilter string, tableAlias string) string {
	status := normalizeStatusFilter(statusFilter)
	if status == "" {
		return ""
	}
	col := qualifyColumn("completed", tableAlias)
	switch status {
	case "complete":
		return fmt.Sprintf(" AND %s = true", col)
	case "incomplete":
		return fmt.Sprintf(" AND (%s IS NULL OR %s = false)", col, col)
	default:
		return ""
	}
}

func qualifyColumn(column, tableAlias string) string {
	if tableAlias == "" {
		return column
	}
	return tableAlias + "." + column
}
