package tasks

import (
	"GoTodo/internal/storage"
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// DashboardStats holds aggregate metrics for the dashboard page.
type DashboardStats struct {
	OverdueCount         int
	DueTodayCount        int
	CompletedThisWeek    int
	CompletedThisMonth   int
	StreakDays           int
	ByProject            []NameCount
	ByPriority           []PriorityCount
	CompletionsLast7Days []DayCount
}

type NameCount struct {
	Name  string
	Count int
}

type PriorityCount struct {
	Priority int
	Label    string
	Count    int
}

type DayCount struct {
	Date  string
	Count int
}

// GetDashboardStats computes dashboard metrics for a user in their timezone.
func GetDashboardStats(userID int, timezone string) (*DashboardStats, error) {
	pool, err := storage.OpenDatabase()
	if err != nil {
		return nil, err
	}
	defer storage.CloseDatabase(pool)

	ctx := context.Background()
	stats := &DashboardStats{}

	where := "user_id = $1"
	args := []interface{}{userID}

	overdueWhere, overdueArgs := appendDueDateCondition(where, args, "overdue", timezone, "")
	if err := pool.QueryRow(ctx, "SELECT COUNT(*) FROM tasks WHERE "+overdueWhere, overdueArgs...).Scan(&stats.OverdueCount); err != nil {
		return nil, fmt.Errorf("overdue count: %w", err)
	}

	todayWhere, todayArgs := appendDueDateCondition(where, args, "today", timezone, "")
	if err := pool.QueryRow(ctx, "SELECT COUNT(*) FROM tasks WHERE "+todayWhere, todayArgs...).Scan(&stats.DueTodayCount); err != nil {
		return nil, fmt.Errorf("due today count: %w", err)
	}

	weekQ := `SELECT COUNT(*) FROM tasks WHERE user_id = $1 AND completed = true
		AND ((date_modified AT TIME ZONE 'UTC') AT TIME ZONE $2)::date >=
		    date_trunc('week', (NOW() AT TIME ZONE $2))::date`
	if err := pool.QueryRow(ctx, weekQ, userID, timezone).Scan(&stats.CompletedThisWeek); err != nil {
		return nil, fmt.Errorf("completed week: %w", err)
	}

	monthQ := `SELECT COUNT(*) FROM tasks WHERE user_id = $1 AND completed = true
		AND ((date_modified AT TIME ZONE 'UTC') AT TIME ZONE $2)::date >=
		    date_trunc('month', (NOW() AT TIME ZONE $2))::date`
	if err := pool.QueryRow(ctx, monthQ, userID, timezone).Scan(&stats.CompletedThisMonth); err != nil {
		return nil, fmt.Errorf("completed month: %w", err)
	}

	projRows, err := pool.Query(ctx, `
		SELECT COALESCE(p.name, 'No project') AS name, COUNT(*) AS cnt
		FROM tasks t
		LEFT JOIN projects p ON t.project_id = p.id
		WHERE t.user_id = $1 AND (t.completed IS NULL OR t.completed = false)
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
		WHERE user_id = $1 AND (completed IS NULL OR completed = false)
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
			SELECT ((date_modified AT TIME ZONE 'UTC') AT TIME ZONE $2)::date AS day, COUNT(*) AS cnt
			FROM tasks
			WHERE user_id = $1 AND completed = true
			  AND date_modified IS NOT NULL
			  AND ((date_modified AT TIME ZONE 'UTC') AT TIME ZONE $2)::date >= ((NOW() AT TIME ZONE $2)::date - INTERVAL '6 days')::date
			GROUP BY 1
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
		SELECT DISTINCT ((date_modified AT TIME ZONE 'UTC') AT TIME ZONE $2)::date AS day
		FROM tasks
		WHERE user_id = $1 AND completed = true AND date_modified IS NOT NULL
		  AND ((date_modified AT TIME ZONE 'UTC') AT TIME ZONE $2)::date >= ((NOW() AT TIME ZONE $2)::date - INTERVAL '90 days')::date
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
