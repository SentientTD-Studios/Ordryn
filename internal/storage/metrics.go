package storage

import "context"

// MetricsCounts are site totals for the Prometheus endpoint.
type MetricsCounts struct {
	ActiveUsers, DeactivatedUsers int
	Projects, ArchivedProjects   int
	OpenTasks, DoneTasks         int
	// Deliveries by status (pending, retry, dead, delivered, …) for extension hooks.
	Deliveries map[string]int
	// AgentRuns by status (queued, running, succeeded, failed, cancelled).
	AgentRuns map[string]int
}

// GetMetricsCounts reads the totals. Each query is a plain COUNT on indexed or
// small tables, cheap enough for a scrape every 15–60 seconds.
func GetMetricsCounts() (*MetricsCounts, error) {
	pool, err := OpenDatabase()
	if err != nil {
		return nil, err
	}
	defer CloseDatabase(pool)

	ctx := context.Background()
	c := &MetricsCounts{Deliveries: map[string]int{}, AgentRuns: map[string]int{}}
	if err := pool.QueryRow(ctx, `
		SELECT COUNT(*) FILTER (WHERE NOT COALESCE(is_banned, FALSE)), COUNT(*) FILTER (WHERE COALESCE(is_banned, FALSE))
		FROM users WHERE NOT COALESCE(is_agent, FALSE) AND NOT COALESCE(is_system, FALSE)`).
		Scan(&c.ActiveUsers, &c.DeactivatedUsers); err != nil {
		return nil, err
	}
	if err := pool.QueryRow(ctx, `
		SELECT COUNT(*) FILTER (WHERE NOT COALESCE(archived, FALSE)), COUNT(*) FILTER (WHERE COALESCE(archived, FALSE))
		FROM projects`).Scan(&c.Projects, &c.ArchivedProjects); err != nil {
		return nil, err
	}
	if err := pool.QueryRow(ctx, `
		SELECT COUNT(*) FILTER (WHERE NOT COALESCE(completed, FALSE)), COUNT(*) FILTER (WHERE COALESCE(completed, FALSE))
		FROM tasks WHERE deleted_at IS NULL`).Scan(&c.OpenTasks, &c.DoneTasks); err != nil {
		return nil, err
	}
	for _, q := range []struct {
		sql string
		out map[string]int
	}{
		{`SELECT status, COUNT(*) FROM extension_deliveries GROUP BY status`, c.Deliveries},
		{`SELECT status, COUNT(*) FROM agent_runs GROUP BY status`, c.AgentRuns},
	} {
		rows, err := pool.Query(ctx, q.sql)
		if err != nil {
			continue // optional tables on very old schemas
		}
		for rows.Next() {
			var s string
			var n int
			if rows.Scan(&s, &n) == nil {
				q.out[s] = n
			}
		}
		rows.Close()
	}
	return c, nil
}
