package tasks

import "fmt"

// appendCompletedWeekCondition filters to tasks completed during the current week (user timezone).
func appendCompletedWeekCondition(where string, args []interface{}, completedFilter, timezone, tablePrefix string) (string, []interface{}) {
	if completedFilter != "week" {
		return where, args
	}
	prefix := ""
	if tablePrefix != "" {
		prefix = tablePrefix + "."
	}
	args = append(args, timezone)
	tzIdx := len(args)
	where += fmt.Sprintf(` AND %scompleted = true AND (
		EXISTS (
			SELECT 1 FROM task_events te
			WHERE te.task_id = %sid AND te.event_type = 'completed'
			  AND (((te.created_at AT TIME ZONE 'UTC') AT TIME ZONE $%d))::date >= date_trunc('week', (NOW() AT TIME ZONE $%d))::date
		)
		OR (
			NOT EXISTS (SELECT 1 FROM task_events te2 WHERE te2.task_id = %sid AND te2.event_type = 'completed')
			AND ((COALESCE(%sdate_modified, %stime_stamp) AT TIME ZONE 'UTC') AT TIME ZONE $%d)::date >=
			    date_trunc('week', (NOW() AT TIME ZONE $%d))::date
		)
	)`, prefix, prefix, tzIdx, tzIdx, prefix, prefix, prefix, tzIdx, tzIdx)
	return where, args
}
