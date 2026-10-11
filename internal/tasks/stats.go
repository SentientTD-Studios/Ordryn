package tasks

import (
	"GoTodo/internal/storage"
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// DashboardStats holds aggregate metrics for the dashboard page.
type DashboardStats struct {
	OverdueCount         int             `json:"overdue_count"`
	DueTodayCount        int             `json:"due_today_count"`
	DueThisWeekCount     int             `json:"due_this_week_count"`
	CompletedThisWeek    int             `json:"completed_this_week"`
	CompletedThisMonth   int             `json:"completed_this_month"`
	StreakDays           int             `json:"streak_days"`
	ByProject            []NameCount     `json:"by_project"`
	ByPriority           []PriorityCount `json:"by_priority"`
	CompletionsLast7Days []DayCount      `json:"completions_last_7_days"`
}

type NameCount struct {
	Name  string `json:"name"`
	Count int    `json:"count"`
}

type PriorityCount struct {
	Priority int    `json:"priority"`
	Label    string `json:"label"`
	Count    int    `json:"count"`
}

type DayCount struct {
	Date  string `json:"date"`
	Count int    `json:"count"`
}

// GetOverdueCount returns the number of incomplete overdue tasks for a user.
func GetOverdueCount(userID int, timezone string) (int, error) {
	timezone = normalizeTimezone(timezone)

	pool, err := storage.OpenDatabase()
	if err != nil {
		return 0, err
	}
	defer storage.CloseDatabase(pool)

	where, args := appendDueDateCondition("user_id = $1 AND deleted_at IS NULL", []interface{}{userID}, "overdue", timezone, "")
	where += " AND NOT " + storage.ArchivedTaskExistsSQL("id")
	var count int
	if err := pool.QueryRow(context.Background(), "SELECT COUNT(*) FROM tasks WHERE "+where, args...).Scan(&count); err != nil {
		return 0, fmt.Errorf("overdue count: %w", err)
	}
	return count, nil
}

// GetDashboardStats computes dashboard metrics for a user in their timezone.
func GetDashboardStats(userID int, timezone string) (*DashboardStats, error) {
	timezone = normalizeTimezone(timezone)

	pool, err := storage.OpenDatabase()
	if err != nil {
		return nil, err
	}
	defer storage.CloseDatabase(pool)

	ctx := context.Background()
	stats := &DashboardStats{
		ByProject:            []NameCount{},
		ByPriority:           []PriorityCount{},
		CompletionsLast7Days: []DayCount{},
	}

	where := "user_id = $1 AND deleted_at IS NULL"
	args := []interface{}{userID}

	overdueWhere, overdueArgs := appendDueDateCondition(where, args, "overdue", timezone, "")
	overdueWhere += " AND NOT " + storage.ArchivedTaskExistsSQL("id")
	overdueWhere, overdueArgs = appendMineClaimScope(overdueWhere, overdueArgs, userID, "")
	if err := pool.QueryRow(ctx, "SELECT COUNT(*) FROM tasks WHERE "+overdueWhere, overdueArgs...).Scan(&stats.OverdueCount); err != nil {
		return nil, fmt.Errorf("overdue count: %w", err)
	}

	todayWhere, todayArgs := appendDueDateCondition(where, args, "today", timezone, "")
	todayWhere += " AND (completed IS NULL OR completed = false) AND NOT " + storage.ArchivedTaskExistsSQL("id")
	todayWhere, todayArgs = appendMineClaimScope(todayWhere, todayArgs, userID, "")
	if err := pool.QueryRow(ctx, "SELECT COUNT(*) FROM tasks WHERE "+todayWhere, todayArgs...).Scan(&stats.DueTodayCount); err != nil {
		return nil, fmt.Errorf("due today count: %w", err)
	}

	throughWeekWhere, throughWeekArgs := appendDueDateCondition(where, args, "through_week", timezone, "")
	throughWeekWhere += " AND NOT " + storage.ArchivedTaskExistsSQL("id")
	throughWeekWhere, throughWeekArgs = appendMineClaimScope(throughWeekWhere, throughWeekArgs, userID, "")
	if err := pool.QueryRow(ctx, "SELECT COUNT(*) FROM tasks WHERE "+throughWeekWhere, throughWeekArgs...).Scan(&stats.DueThisWeekCount); err != nil {
		return nil, fmt.Errorf("due this week count: %w", err)
	}

	weekQ := `
		SELECT COUNT(*) FROM (
			SELECT DISTINCT te.task_id
			FROM task_events te
			WHERE te.user_id = $1 AND te.event_type = 'completed'
			  AND (((te.created_at AT TIME ZONE 'UTC') AT TIME ZONE $2))::date >= date_trunc('week', (NOW() AT TIME ZONE $2))::date
			UNION
			SELECT DISTINCT t.id
			FROM tasks t
			WHERE t.user_id = $1 AND t.completed = true
			  AND NOT EXISTS (
			    SELECT 1 FROM task_events te2
			    WHERE te2.task_id = t.id AND te2.event_type = 'completed'
			  )
			  AND ((COALESCE(t.date_modified, t.time_stamp) AT TIME ZONE 'UTC') AT TIME ZONE $2)::date >=
			      date_trunc('week', (NOW() AT TIME ZONE $2))::date
		) completed`
	if err := pool.QueryRow(ctx, weekQ, userID, timezone).Scan(&stats.CompletedThisWeek); err != nil {
		return nil, fmt.Errorf("completed week: %w", err)
	}

	monthQ := `
		SELECT COUNT(*) FROM (
			SELECT DISTINCT te.task_id
			FROM task_events te
			WHERE te.user_id = $1 AND te.event_type = 'completed'
			  AND (((te.created_at AT TIME ZONE 'UTC') AT TIME ZONE $2))::date >= date_trunc('month', (NOW() AT TIME ZONE $2))::date
			UNION
			SELECT DISTINCT t.id
			FROM tasks t
			WHERE t.user_id = $1 AND t.completed = true
			  AND NOT EXISTS (
			    SELECT 1 FROM task_events te2
			    WHERE te2.task_id = t.id AND te2.event_type = 'completed'
			  )
			  AND ((COALESCE(t.date_modified, t.time_stamp) AT TIME ZONE 'UTC') AT TIME ZONE $2)::date >=
			      date_trunc('month', (NOW() AT TIME ZONE $2))::date
		) completed`
	if err := pool.QueryRow(ctx, monthQ, userID, timezone).Scan(&stats.CompletedThisMonth); err != nil {
		return nil, fmt.Errorf("completed month: %w", err)
	}

	projRows, err := pool.Query(ctx, `
		SELECT COALESCE(p.name, 'No project') AS name, COUNT(*) AS cnt
		FROM tasks t
		LEFT JOIN projects p ON t.project_id = p.id
		WHERE t.user_id = $1 AND t.deleted_at IS NULL AND (t.completed IS NULL OR t.completed = false)
		  AND NOT `+storage.ArchivedTaskExistsSQL("t.id")+`
		GROUP BY COALESCE(p.name, 'No project')
		ORDER BY cnt DESC`, userID)
	if err != nil {
		return nil, fmt.Errorf("by project: %w", err)
	}
	defer projRows.Close()
	for projRows.Next() {
		var nc NameCount
		if err := projRows.Scan(&nc.Name, &nc.Count); err != nil {
			return nil, err
		}
		stats.ByProject = append(stats.ByProject, nc)
	}

	priorityLabels := map[int]string{0: "None", 1: "Low", 2: "Medium", 3: "High"}
	priRows, err := pool.Query(ctx, `
		SELECT COALESCE(priority, 0), COUNT(*)
		FROM tasks
		WHERE user_id = $1 AND deleted_at IS NULL AND (completed IS NULL OR completed = false)
		  AND NOT `+storage.ArchivedTaskExistsSQL("id")+`
		GROUP BY COALESCE(priority, 0)
		ORDER BY COALESCE(priority, 0) DESC`, userID)
	if err != nil {
		return nil, fmt.Errorf("by priority: %w", err)
	}
	defer priRows.Close()
	for priRows.Next() {
		var pc PriorityCount
		if err := priRows.Scan(&pc.Priority, &pc.Count); err != nil {
			return nil, err
		}
		pc.Label = priorityLabels[pc.Priority]
		stats.ByPriority = append(stats.ByPriority, pc)
	}

	chartQ := `
		SELECT d::date, COALESCE(c.cnt, 0)
		FROM generate_series(
			((NOW() AT TIME ZONE $2)::date - INTERVAL '6 days')::date,
			(NOW() AT TIME ZONE $2)::date,
			'1 day'::interval
		) AS d
		LEFT JOIN (
			SELECT day, COUNT(*) AS cnt FROM (
				SELECT DISTINCT te.task_id, (((te.created_at AT TIME ZONE 'UTC') AT TIME ZONE $2))::date AS day
				FROM task_events te
				WHERE te.user_id = $1 AND te.event_type = 'completed'
				  AND (((te.created_at AT TIME ZONE 'UTC') AT TIME ZONE $2))::date >= ((NOW() AT TIME ZONE $2)::date - INTERVAL '6 days')::date
				UNION
				SELECT DISTINCT t.id AS task_id, ((COALESCE(t.date_modified, t.time_stamp) AT TIME ZONE 'UTC') AT TIME ZONE $2)::date AS day
				FROM tasks t
				WHERE t.user_id = $1 AND t.completed = true
				  AND NOT EXISTS (
				    SELECT 1 FROM task_events te2
				    WHERE te2.task_id = t.id AND te2.event_type = 'completed'
				  )
				  AND ((COALESCE(t.date_modified, t.time_stamp) AT TIME ZONE 'UTC') AT TIME ZONE $2)::date >= ((NOW() AT TIME ZONE $2)::date - INTERVAL '6 days')::date
			) completions
			GROUP BY day
		) c ON c.day = d::date
		ORDER BY d`
	chartRows, err := pool.Query(ctx, chartQ, userID, timezone)
	if err != nil {
		return nil, fmt.Errorf("completions chart: %w", err)
	}
	defer chartRows.Close()
	for chartRows.Next() {
		var dc DayCount
		var day time.Time
		if err := chartRows.Scan(&day, &dc.Count); err != nil {
			return nil, err
		}
		dc.Date = day.Format("Mon 1/2")
		stats.CompletionsLast7Days = append(stats.CompletionsLast7Days, dc)
	}

	stats.StreakDays = completionStreak(ctx, pool, userID, timezone)
	return stats, nil
}

func completionStreak(ctx context.Context, pool *pgxpool.Pool, userID int, timezone string) int {
	rows, err := pool.Query(ctx, `
		SELECT DISTINCT day FROM (
			SELECT (((te.created_at AT TIME ZONE 'UTC') AT TIME ZONE $2))::date AS day
			FROM task_events te
			WHERE te.user_id = $1 AND te.event_type = 'completed'
			  AND (((te.created_at AT TIME ZONE 'UTC') AT TIME ZONE $2))::date >= ((NOW() AT TIME ZONE $2)::date - INTERVAL '90 days')::date
			UNION
			SELECT ((COALESCE(t.date_modified, t.time_stamp) AT TIME ZONE 'UTC') AT TIME ZONE $2)::date AS day
			FROM tasks t
			WHERE t.user_id = $1 AND t.completed = true
			  AND NOT EXISTS (
			    SELECT 1 FROM task_events te2
			    WHERE te2.task_id = t.id AND te2.event_type = 'completed'
			  )
			  AND ((COALESCE(t.date_modified, t.time_stamp) AT TIME ZONE 'UTC') AT TIME ZONE $2)::date >= ((NOW() AT TIME ZONE $2)::date - INTERVAL '90 days')::date
		) days
		ORDER BY day DESC`, userID, timezone)
	if err != nil {
		return 0
	}
	defer rows.Close()

	loc, err := time.LoadLocation(timezone)
	if err != nil {
		loc = time.UTC
	}
	today := time.Now().In(loc).Truncate(24 * time.Hour)
	streak := 0
	expect := today

	for rows.Next() {
		var day time.Time
		if err := rows.Scan(&day); err != nil {
			break
		}
		day = day.In(loc).Truncate(24 * time.Hour)
		if day.Equal(expect) {
			streak++
			expect = expect.AddDate(0, 0, -1)
		} else if day.Before(expect) {
			break
		}
	}
	return streak
}

func normalizeTimezone(timezone string) string {
	timezone = strings.TrimSpace(timezone)
	if timezone == "" {
		return "America/New_York"
	}
	if _, err := time.LoadLocation(timezone); err != nil {
		return "America/New_York"
	}
	return timezone
}
