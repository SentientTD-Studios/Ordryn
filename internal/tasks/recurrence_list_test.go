package tasks_test

import (
	"context"
	"testing"

	"GoTodo/internal/storage"
	"GoTodo/internal/tasks"
)

func TestTaskListsAttachRecurrence(t *testing.T) {
	pool, err := storage.OpenDatabase()
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	var recurringID, plainID int
	if err := pool.QueryRow(context.Background(),
		`INSERT INTO tasks (title, description, user_id, completed, is_favorite, position, priority, project_id, due_date)
		 VALUES ('Water plants', '', 1, false, false, 90, 0, 1, DATE '2026-09-28') RETURNING id`).Scan(&recurringID); err != nil {
		t.Fatalf("insert recurring: %v", err)
	}
	if err := pool.QueryRow(context.Background(),
		`INSERT INTO tasks (title, description, user_id, completed, is_favorite, position, priority, project_id)
		 VALUES ('One-off', '', 1, false, false, 91, 0, 1) RETURNING id`).Scan(&plainID); err != nil {
		t.Fatalf("insert plain: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), "DELETE FROM tasks WHERE id = ANY($1)", []int{recurringID, plainID})
	})
	if _, err := pool.Exec(context.Background(),
		`INSERT INTO task_recurrence (task_id, frequency, interval_count, weekdays_mask, series_id, occurrence)
		 VALUES ($1, 'weekly', 2, 2, $1, 3)`, recurringID); err != nil {
		t.Fatalf("insert rule: %v", err)
	}

	got, err := tasks.FetchTaskByIDForUser(recurringID, 1, "UTC", 1)
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	if got.Recurrence == nil {
		t.Fatal("expected recurrence attached on single fetch")
	}
	if got.Recurrence.Frequency != "weekly" || got.Recurrence.Interval != 2 || got.Recurrence.WeekdaysMask != 2 || got.Recurrence.Occurrence != 3 {
		t.Fatalf("unexpected rule: %+v", got.Recurrence)
	}

	userID := 1
	project := 1
	list, _, err := tasks.ReturnPaginationForUserWithFilters(1, 100, &userID, "UTC", tasks.ListFilters{ProjectFilter: &project})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	seenRecurring, seenPlain := false, false
	for _, task := range list {
		switch task.ID {
		case recurringID:
			seenRecurring = true
			if task.Recurrence == nil || task.Recurrence.SeriesID != recurringID {
				t.Fatalf("list task missing recurrence: %+v", task.Recurrence)
			}
		case plainID:
			seenPlain = true
			if task.Recurrence != nil {
				t.Fatalf("plain task should not recur: %+v", task.Recurrence)
			}
		}
	}
	if !seenRecurring || !seenPlain {
		t.Fatalf("expected both tasks in list (recurring=%v plain=%v)", seenRecurring, seenPlain)
	}
}
