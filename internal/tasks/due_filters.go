package tasks

import (
	"fmt"
	"strings"
)

func NormalizeDueFilter(due string) string {
	return normalizeDueFilter(due)
}

func normalizeDueFilter(due string) string {
	switch strings.ToLower(strings.TrimSpace(due)) {
	case "overdue", "today", "week", "none":
		return strings.ToLower(strings.TrimSpace(due))
	default:
		return ""
	}
}

func appendDueDateCondition(where string, args []interface{}, dueFilter, timezone, tablePrefix string) (string, []interface{}) {
	dueFilter = normalizeDueFilter(dueFilter)
	if dueFilter == "" {
		return where, args
	}

	prefix := ""
	if tablePrefix != "" {
		prefix = tablePrefix + "."
	}

	switch dueFilter {
	case "overdue":
		args = append(args, timezone)
		idx := len(args)
		where += fmt.Sprintf(
			" AND %sdue_date IS NOT NULL AND %sdue_date < (NOW() AT TIME ZONE $%d)::date AND (%scompleted IS NULL OR %scompleted = false)",
			prefix, prefix, idx, prefix, prefix,
		)
	case "today":
		args = append(args, timezone)
		idx := len(args)
		where += fmt.Sprintf(" AND %sdue_date = (NOW() AT TIME ZONE $%d)::date", prefix, idx)
	case "week":
		args = append(args, timezone)
		idx := len(args)
		where += fmt.Sprintf(
			" AND %sdue_date IS NOT NULL AND %sdue_date >= (NOW() AT TIME ZONE $%d)::date AND %sdue_date <= ((NOW() AT TIME ZONE $%d)::date + INTERVAL '7 days')::date",
			prefix, prefix, idx, prefix, idx,
		)
	case "none":
		where += fmt.Sprintf(" AND %sdue_date IS NULL", prefix)
	}
	return where, args
}
