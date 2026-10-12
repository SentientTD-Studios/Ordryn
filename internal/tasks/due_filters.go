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
	case "overdue", "today", "week", "through_week", "none", "range":
		return strings.ToLower(strings.TrimSpace(due))
	default:
		return ""
	}
}

// appendDueRangeCondition limits due dates to [from, to] (YYYY-MM-DD, inclusive;
// either end may be empty). Tasks with no due date never match a range.
func appendDueRangeCondition(where string, args []interface{}, from, to, tablePrefix string) (string, []interface{}) {
	prefix := ""
	if tablePrefix != "" {
		prefix = tablePrefix + "."
	}
	if from != "" {
		args = append(args, from)
		where += fmt.Sprintf(" AND %sdue_date >= $%d::date", prefix, len(args))
	}
	if to != "" {
		args = append(args, to)
		where += fmt.Sprintf(" AND %sdue_date <= $%d::date", prefix, len(args))
	}
	return where, args
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
	case "through_week":
		// Incomplete tasks due from today through end of the current calendar week (Mon–Sun). Excludes overdue.
		args = append(args, timezone)
		idx := len(args)
		where += fmt.Sprintf(
			" AND %sdue_date IS NOT NULL AND %sdue_date >= (NOW() AT TIME ZONE $%d)::date AND %sdue_date <= (date_trunc('week', (NOW() AT TIME ZONE $%d)) + INTERVAL '6 days')::date AND (%scompleted IS NULL OR %scompleted = false)",
			prefix, prefix, idx, prefix, idx, prefix, prefix,
		)
	case "none":
		where += fmt.Sprintf(" AND %sdue_date IS NULL", prefix)
	}
	return where, args
}
