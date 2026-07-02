package tasks_test

import (
	"GoTodo/internal/tasks"
	"context"
	"fmt"
	"os"
	"testing"

	embeddedpostgres "github.com/fergusstrange/embedded-postgres"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestMain(m *testing.M) {
	os.Setenv("SESSION_KEY", "test-session-key-for-unit-tests-32chars!!")
	port := uint32(5438)
	db := embeddedpostgres.NewDatabase(embeddedpostgres.DefaultConfig().Port(port).Database("gotodo_test"))
	if err := db.Start(); err != nil {
		fmt.Fprintf(os.Stderr, "start postgres: %v\n", err)
		os.Exit(1)
	}

	os.Setenv("DB_HOST", "localhost")
	os.Setenv("DB_PORT", fmt.Sprintf("%d", port))
	os.Setenv("DB_USER", "postgres")
	os.Setenv("DB_PASSWORD", "postgres")
	os.Setenv("DB_NAME", "gotodo_test")

	pool, err := pgxpool.New(context.Background(), fmt.Sprintf("postgres://postgres:postgres@localhost:%d/gotodo_test?sslmode=disable", port))
	if err != nil {
		fmt.Fprintf(os.Stderr, "connect: %v\n", err)
		os.Exit(1)
	}

	_, err = pool.Exec(context.Background(), `
		CREATE TABLE users (id SERIAL PRIMARY KEY, email TEXT);
		CREATE TABLE projects (id SERIAL PRIMARY KEY, user_id INT, name TEXT);
		CREATE TABLE tags (
			id SERIAL PRIMARY KEY,
			user_id INTEGER NOT NULL,
			name TEXT NOT NULL,
			color VARCHAR(7) DEFAULT '#6c757d',
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			UNIQUE(user_id, name)
		);
		CREATE TABLE tasks (
			id SERIAL PRIMARY KEY,
			title TEXT NOT NULL,
			description TEXT,
			completed BOOLEAN DEFAULT FALSE,
			time_stamp TIMESTAMP DEFAULT NOW(),
			is_favorite BOOLEAN DEFAULT FALSE,
			position INTEGER DEFAULT 0,
			priority SMALLINT DEFAULT 0,
			user_id INTEGER,
			project_id INTEGER,
			date_modified TIMESTAMP,
			due_date DATE
		);
		CREATE TABLE task_tags (
			task_id INTEGER NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
			tag_id INTEGER NOT NULL REFERENCES tags(id) ON DELETE CASCADE,
			PRIMARY KEY (task_id, tag_id)
		);
		INSERT INTO users (id, email) VALUES (1, 'user@example.com');
		INSERT INTO tags (id, user_id, name, color) VALUES (1, 1, 'work', '#0d6efd'), (2, 1, 'personal', '#198754');
		INSERT INTO tasks (title, description, user_id, completed, is_favorite, position, priority, project_id, due_date) VALUES
		 ('Favorite task', 'fav desc', 1, false, true, 1, 2, NULL, CURRENT_DATE),
		 ('Open task', 'open desc', 1, false, false, 2, 1, 1, CURRENT_DATE + 1),
		 ('Done task', 'done desc', 1, true, false, 3, 0, 1, CURRENT_DATE - 1),
		 ('Tagged task', 'has work tag', 1, false, false, 4, 0, NULL, NULL);
		INSERT INTO task_tags (task_id, tag_id) VALUES (4, 1);
	`)
	pool.Close()
	if err != nil {
		fmt.Fprintf(os.Stderr, "schema: %v\n", err)
		os.Exit(1)
	}

	code := m.Run()
	_ = db.Stop()
	os.Exit(code)
}

func TestReturnPaginationForUserWithFilters(t *testing.T) {
	userID := 1
	timezone := "America/New_York"
	project := 1
	projectZero := 0

	cases := []struct {
		name    string
		filters tasks.ListFilters
	}{
		{"all", tasks.ListFilters{}},
		{"incomplete", tasks.ListFilters{StatusFilter: "incomplete"}},
		{"complete", tasks.ListFilters{StatusFilter: "complete"}},
		{"completed alias", tasks.ListFilters{StatusFilter: "Completed"}},
		{"project incomplete", tasks.ListFilters{ProjectFilter: &project, StatusFilter: "incomplete"}},
		{"no project complete", tasks.ListFilters{ProjectFilter: &projectZero, StatusFilter: "complete"}},
		{"due today", tasks.ListFilters{DueFilter: "today"}},
		{"due overdue", tasks.ListFilters{DueFilter: "overdue"}},
		{"tag filter", tasks.ListFilters{TagFilter: intPtr(1)}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, total, err := tasks.ReturnPaginationForUserWithFilters(1, 10, &userID, timezone, tc.filters)
			if err != nil {
				t.Fatalf("ReturnPaginationForUserWithFilters: %v", err)
			}
			if total < 0 {
				t.Fatalf("expected non-negative total, got %d", total)
			}
		})
	}
}

func TestSearchTasksForUserWithFilters(t *testing.T) {
	userID := 1
	timezone := "America/New_York"

	_, total, err := tasks.SearchTasksForUserWithFilters(1, 10, "task", &userID, timezone, tasks.ListFilters{StatusFilter: "incomplete"})
	if err != nil {
		t.Fatalf("SearchTasksForUserWithFilters: %v", err)
	}
	if total != 3 {
		t.Fatalf("expected 3 incomplete search matches, got %d", total)
	}

	tagID := 1
	tasksList, tagTotal, err := tasks.ReturnPaginationForUserWithFilters(1, 10, &userID, timezone, tasks.ListFilters{TagFilter: &tagID})
	if err != nil {
		t.Fatalf("tag filter list: %v", err)
	}
	if tagTotal != 1 {
		t.Fatalf("expected 1 tagged task, got total %d", tagTotal)
	}
	if len(tasksList) != 1 || tasksList[0].Title != "Tagged task" {
		t.Fatalf("expected tagged task on page, got %v", tasksList)
	}

	_, tagSearchTotal, err := tasks.SearchTasksForUserWithFilters(1, 10, "work", &userID, timezone, tasks.ListFilters{})
	if err != nil {
		t.Fatalf("tag name search: %v", err)
	}
	if tagSearchTotal != 1 {
		t.Fatalf("expected 1 task matching tag name search, got %d", tagSearchTotal)
	}
}

func intPtr(n int) *int {
	return &n
}
