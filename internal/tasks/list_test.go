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
	port := uint32(5437)
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
		CREATE TABLE tasks (
			id SERIAL PRIMARY KEY,
			title TEXT NOT NULL,
			description TEXT,
			completed BOOLEAN DEFAULT FALSE,
			time_stamp TIMESTAMP DEFAULT NOW(),
			is_favorite BOOLEAN DEFAULT FALSE,
			position INTEGER DEFAULT 0,
			user_id INTEGER,
			project_id INTEGER,
			date_modified TIMESTAMP,
			due_date DATE
		);
		INSERT INTO users (id, email) VALUES (1, 'user@example.com');
		INSERT INTO tasks (title, description, user_id, completed, is_favorite, position, project_id, due_date) VALUES
		 ('Favorite task', 'fav desc', 1, false, true, 1, NULL, CURRENT_DATE),
		 ('Open task', 'open desc', 1, false, false, 2, 1, CURRENT_DATE + 1),
		 ('Done task', 'done desc', 1, true, false, 3, 1, CURRENT_DATE - 1);
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
		name   string
		filter *int
		status string
	}{
		{"all", nil, ""},
		{"incomplete", nil, "incomplete"},
		{"complete", nil, "complete"},
		{"project incomplete", &project, "incomplete"},
		{"no project complete", &projectZero, "complete"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, total, err := tasks.ReturnPaginationForUserWithFilters(1, 10, &userID, timezone, tc.filter, tc.status)
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

	_, total, err := tasks.SearchTasksForUserWithFilters(1, 10, "task", &userID, timezone, nil, "incomplete")
	if err != nil {
		t.Fatalf("SearchTasksForUserWithFilters: %v", err)
	}
	if total != 2 {
		t.Fatalf("expected 2 incomplete search matches, got %d", total)
	}
}
