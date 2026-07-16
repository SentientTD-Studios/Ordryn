package tasks_test

import (
	"GoTodo/internal/storage"
	"GoTodo/internal/tasks"
	"context"
	"testing"
)

func TestGetDashboardStats(t *testing.T) {
	stats, err := tasks.GetDashboardStats(1, "America/New_York")
	if err != nil {
		t.Fatalf("GetDashboardStats: %v", err)
	}
	if stats == nil {
		t.Fatal("expected stats")
	}
	if len(stats.ByProject) == 0 {
		t.Error("expected open tasks grouped by project")
	}
	if len(stats.CompletionsLast7Days) != 7 {
		t.Fatalf("expected 7 chart days, got %d", len(stats.CompletionsLast7Days))
	}
}

func TestGetDashboardStatsCompletionNotDoubleCounted(t *testing.T) {
	before, err := tasks.GetDashboardStats(1, "America/New_York")
	if err != nil {
		t.Fatalf("GetDashboardStats baseline: %v", err)
	}
	weekBefore := before.CompletedThisWeek
	todayBefore := before.CompletionsLast7Days[len(before.CompletionsLast7Days)-1].Count

	pool, err := storage.OpenDatabase()
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer storage.CloseDatabase(pool)

	ctx := context.Background()
	var taskID int
	err = pool.QueryRow(ctx, `
		INSERT INTO tasks (title, user_id, completed, time_stamp, date_modified)
		VALUES ('count-once', 1, true, NOW() AT TIME ZONE 'UTC', NOW() AT TIME ZONE 'UTC')
		RETURNING id`).Scan(&taskID)
	if err != nil {
		t.Fatalf("insert task: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, "DELETE FROM task_events WHERE task_id = $1", taskID)
		_, _ = pool.Exec(ctx, "DELETE FROM tasks WHERE id = $1", taskID)
	})

	_, err = pool.Exec(ctx,
		`INSERT INTO task_events (task_id, user_id, event_type, created_at) VALUES ($1, 1, 'completed', NOW() AT TIME ZONE 'UTC')`,
		taskID,
	)
	if err != nil {
		t.Fatalf("insert event: %v", err)
	}

	after, err := tasks.GetDashboardStats(1, "America/New_York")
	if err != nil {
		t.Fatalf("GetDashboardStats after completion: %v", err)
	}
	todayAfter := after.CompletionsLast7Days[len(after.CompletionsLast7Days)-1].Count

	if after.CompletedThisWeek != weekBefore+1 {
		t.Fatalf("week completions: want %d, got %d", weekBefore+1, after.CompletedThisWeek)
	}
	if todayAfter != todayBefore+1 {
		t.Fatalf("today chart completions: want %d, got %d", todayBefore+1, todayAfter)
	}

	_, err = pool.Exec(ctx,
		`INSERT INTO task_events (task_id, user_id, event_type, created_at) VALUES ($1, 1, 'completed', NOW() AT TIME ZONE 'UTC')`,
		taskID,
	)
	if err != nil {
		t.Fatalf("insert duplicate completion event: %v", err)
	}

	afterToggle, err := tasks.GetDashboardStats(1, "America/New_York")
	if err != nil {
		t.Fatalf("GetDashboardStats after duplicate event: %v", err)
	}
	if afterToggle.CompletedThisWeek != after.CompletedThisWeek {
		t.Fatalf("week completions after toggle spam: want %d, got %d", after.CompletedThisWeek, afterToggle.CompletedThisWeek)
	}
	todayAfterToggle := afterToggle.CompletionsLast7Days[len(afterToggle.CompletionsLast7Days)-1].Count
	if todayAfterToggle != todayAfter {
		t.Fatalf("today chart after toggle spam: want %d, got %d", todayAfter, todayAfterToggle)
	}
}
