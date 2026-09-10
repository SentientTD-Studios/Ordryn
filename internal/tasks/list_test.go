package tasks_test

import (
	"GoTodo/internal/storage"
	"GoTodo/internal/tasks"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	embeddedpostgres "github.com/fergusstrange/embedded-postgres"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestMain(m *testing.M) {
	os.Setenv("SESSION_KEY", "test-session-key-for-unit-tests-32chars!!")
	port := uint32(5438)
	// Pin a Maven-published binary version (DefaultConfig alone can drift and 404).
	// Isolate RuntimePath for parallel go test ./... against other packages.
	runtimePath := filepath.Join(os.TempDir(), "gotodo-embedded-pg-tasks")
	db := startEmbeddedPostgres("gotodo_test", runtimePath, port)

	os.Setenv("DB_HOST", "localhost")
	os.Setenv("DB_PORT", fmt.Sprintf("%d", port))
	os.Setenv("DB_USER", "postgres")
	os.Setenv("DB_PASSWORD", "postgres")
	os.Setenv("DB_NAME", "gotodo_test")
	os.Setenv("DB_SSLMODE", "disable")

	pool, err := pgxpool.New(context.Background(), fmt.Sprintf("postgres://postgres:postgres@localhost:%d/gotodo_test?sslmode=disable", port))
	if err != nil {
		fmt.Fprintf(os.Stderr, "connect: %v\n", err)
		os.Exit(1)
	}

	_, err = pool.Exec(context.Background(), `
		CREATE TABLE users (id SERIAL PRIMARY KEY, email TEXT, user_name TEXT, avatar_url TEXT DEFAULT '');
		CREATE TABLE saved_views (
			id SERIAL PRIMARY KEY,
			user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			name TEXT NOT NULL,
			filter_json JSONB NOT NULL DEFAULT '{}',
			sort_order INTEGER NOT NULL DEFAULT 0,
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			UNIQUE (user_id, name)
		);
		CREATE TABLE projects (
			id SERIAL PRIMARY KEY,
			user_id INT,
			name TEXT,
			description TEXT NOT NULL DEFAULT '',
			workflow_mode VARCHAR(16) NOT NULL DEFAULT 'classic'
		);
		CREATE TABLE project_statuses (
			id SERIAL PRIMARY KEY,
			project_id INTEGER NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
			name TEXT NOT NULL,
			description TEXT NOT NULL DEFAULT '',
			position INTEGER NOT NULL DEFAULT 0,
			is_done BOOLEAN NOT NULL DEFAULT FALSE,
			is_default BOOLEAN NOT NULL DEFAULT FALSE,
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		);
		CREATE TABLE project_members (
			project_id INTEGER NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
			user_id INTEGER NOT NULL,
			role VARCHAR(16) NOT NULL,
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			PRIMARY KEY (project_id, user_id)
		);
		CREATE TABLE tags (
			id SERIAL PRIMARY KEY,
			user_id INTEGER NOT NULL,
			project_id INTEGER,
			name TEXT NOT NULL,
			color VARCHAR(7) DEFAULT '#6c757d',
			protected BOOLEAN NOT NULL DEFAULT false,
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
		);
		CREATE TABLE project_sprints (
			id SERIAL PRIMARY KEY,
			project_id INTEGER NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
			name TEXT NOT NULL,
			start_date DATE NOT NULL,
			end_date DATE NOT NULL,
			lock_date DATE,
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
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
			parent_id INTEGER REFERENCES tasks(id) ON DELETE CASCADE,
			date_modified TIMESTAMP,
			due_date DATE,
			status_id INTEGER,
			estimate_points INTEGER,
			claimed_by INTEGER,
			sprint_id INTEGER
		);
		CREATE TABLE task_tags (
			task_id INTEGER NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
			tag_id INTEGER NOT NULL REFERENCES tags(id) ON DELETE CASCADE,
			PRIMARY KEY (task_id, tag_id)
		);
		CREATE TABLE task_time_entries (
			id SERIAL PRIMARY KEY,
			task_id INTEGER NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
			user_id INTEGER NOT NULL,
			minutes INTEGER NOT NULL,
			note TEXT NOT NULL DEFAULT '',
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		);
		CREATE TABLE task_events (
			id SERIAL PRIMARY KEY,
			task_id INTEGER NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
			user_id INTEGER NOT NULL,
			event_type VARCHAR(32) NOT NULL,
			metadata JSONB DEFAULT '{}',
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
		);
		CREATE TABLE task_github_issues (
			task_id INTEGER PRIMARY KEY REFERENCES tasks(id) ON DELETE CASCADE,
			issue_id BIGINT NOT NULL DEFAULT 0,
			issue_number INTEGER NOT NULL,
			issue_url TEXT NOT NULL DEFAULT '',
			issue_state VARCHAR(16) NOT NULL DEFAULT 'open',
			issue_title TEXT NOT NULL DEFAULT '',
			last_synced_at TIMESTAMPTZ,
			last_sync_error TEXT NOT NULL DEFAULT '',
			UNIQUE (issue_id),
			CHECK (issue_number > 0)
		);
		INSERT INTO users (id, email) VALUES
			(1, 'user@example.com'),
			(2, 'other@example.com'),
			(3, 'viewer@example.com'),
			(4, 'editor@example.com');
		INSERT INTO projects (id, user_id, name) VALUES
			(1, 1, 'Owned project'),
			(2, 2, 'Shared project');
		INSERT INTO project_members (project_id, user_id, role) VALUES
			(1, 1, 'owner'),
			(2, 2, 'owner'),
			(2, 3, 'viewer'),
			(2, 4, 'editor');
		INSERT INTO tags (id, user_id, name, color) VALUES (1, 1, 'work', '#0d6efd'), (2, 1, 'personal', '#198754');
		INSERT INTO tasks (title, description, user_id, completed, is_favorite, position, priority, project_id, due_date) VALUES
		 ('Favorite task', 'fav desc', 1, false, true, 1, 2, NULL, CURRENT_DATE),
		 ('Open task', 'open desc', 1, false, false, 2, 1, 1, CURRENT_DATE + 1),
		 ('Done task', 'done desc', 1, true, false, 3, 0, 1, CURRENT_DATE - 1),
		 ('Tagged task', 'has work tag', 1, false, false, 4, 0, NULL, NULL),
		 ('Shared owner task', 'owned by project owner', 2, false, false, 5, 0, 2, NULL);
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

func startEmbeddedPostgres(database, runtimePath string, port uint32) *embeddedpostgres.EmbeddedPostgres {
	var last error
	for attempt := 1; attempt <= 3; attempt++ {
		db := embeddedpostgres.NewDatabase(embeddedpostgres.DefaultConfig().
			Version(embeddedpostgres.V16).
			Port(port).
			Database(database).
			RuntimePath(runtimePath))
		if err := db.Start(); err == nil {
			return db
		} else {
			last = err
			fmt.Fprintf(os.Stderr, "start postgres (attempt %d/3): %v\n", attempt, err)
			time.Sleep(time.Duration(attempt) * 2 * time.Second)
		}
	}
	fmt.Fprintf(os.Stderr, "start postgres: %v\n", last)
	os.Exit(1)
	return nil
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
		{"due overdue with subtasks", tasks.ListFilters{DueFilter: "overdue", IncludeSubtasks: true}},
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

func TestIncompleteListTotalIncludesFavoriteRoot(t *testing.T) {
	userID := 1
	timezone := "America/New_York"
	list, total, err := tasks.ReturnPaginationForUserWithFilters(1, 50, &userID, timezone, tasks.ListFilters{
		StatusFilter:       "incomplete",
		WorkflowClaimScope: "mine",
	})
	if err != nil {
		t.Fatalf("incomplete list: %v", err)
	}
	var found bool
	for _, task := range list {
		if task.Title == "Favorite task" && task.IsFavorite && !task.Completed {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected starred incomplete root in incomplete list, got %v", titles(list))
	}
	if total < 1 {
		t.Fatalf("incomplete total should include starred roots, got %d", total)
	}
}

func TestSearchTasksForUserWithFilters(t *testing.T) {
	userID := 1
	timezone := "America/New_York"

	favoriteResults, favoriteTotal, err := tasks.SearchTasksForUserWithFilters(1, 10, "Favorite", &userID, timezone, tasks.ListFilters{})
	if err != nil {
		t.Fatalf("favorite search: %v", err)
	}
	if favoriteTotal != 1 || len(favoriteResults) != 1 {
		t.Fatalf("expected one favorite search result, got total %d and %d tasks", favoriteTotal, len(favoriteResults))
	}
	if !favoriteResults[0].IsFavorite {
		t.Fatalf("expected search result %q to preserve favorite status", favoriteResults[0].Title)
	}

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

	_, nameTotal, err := tasks.ReturnPaginationForUserWithFilters(1, 10, &userID, timezone, tasks.ListFilters{TagNameFilter: "WORK"})
	if err != nil {
		t.Fatalf("tag name filter list: %v", err)
	}
	if nameTotal != 1 {
		t.Fatalf("expected 1 task matching tag name, got total %d", nameTotal)
	}

	_, tagSearchTotal, err := tasks.SearchTasksForUserWithFilters(1, 10, "work", &userID, timezone, tasks.ListFilters{})
	if err != nil {
		t.Fatalf("tag name search: %v", err)
	}
	if tagSearchTotal != 1 {
		t.Fatalf("expected 1 task matching tag name search, got %d", tagSearchTotal)
	}

	idResults, idTotal, err := tasks.SearchTasksForUserWithFilters(1, 10, "4", &userID, timezone, tasks.ListFilters{})
	if err != nil {
		t.Fatalf("numeric id search: %v", err)
	}
	if idTotal != 1 || len(idResults) != 1 || idResults[0].Title != "Tagged task" {
		t.Fatalf("expected Tagged task for id 4, got total %d tasks %v", idTotal, idResults)
	}

	hashResults, hashTotal, err := tasks.SearchTasksForUserWithFilters(1, 10, "#4", &userID, timezone, tasks.ListFilters{})
	if err != nil {
		t.Fatalf("hash id search: %v", err)
	}
	if hashTotal != 1 || len(hashResults) != 1 || hashResults[0].ID != 4 {
		t.Fatalf("expected task 4 for #4, got total %d tasks %v", hashTotal, hashResults)
	}
}

func TestSharedProjectVisibilityByRole(t *testing.T) {
	timezone := "America/New_York"
	sharedProject := 2
	viewerID := 3
	editorID := 4

	viewerHome, viewerHomeTotal, err := tasks.ReturnPaginationForUserWithFilters(1, 10, &viewerID, timezone, tasks.ListFilters{})
	if err != nil {
		t.Fatalf("viewer home list: %v", err)
	}
	if viewerHomeTotal != 0 || len(viewerHome) != 0 {
		t.Fatalf("viewer should not see shared tasks on home list, got total %d tasks %v", viewerHomeTotal, titles(viewerHome))
	}

	viewerScoped, viewerScopedTotal, err := tasks.ReturnPaginationForUserWithFilters(1, 10, &viewerID, timezone, tasks.ListFilters{ProjectFilter: &sharedProject})
	if err != nil {
		t.Fatalf("viewer project list: %v", err)
	}
	if viewerScopedTotal != 1 || len(viewerScoped) != 1 || viewerScoped[0].Title != "Shared owner task" {
		t.Fatalf("viewer should see shared task in project view, got total %d tasks %v", viewerScopedTotal, titles(viewerScoped))
	}

	matchHome, err := tasks.TaskMatchesFilters(5, viewerID, timezone, tasks.ListFilters{}, "")
	if err != nil {
		t.Fatalf("viewer home TaskMatchesFilters: %v", err)
	}
	if matchHome {
		t.Fatal("viewer TaskMatchesFilters should be false on home list")
	}
	matchScoped, err := tasks.TaskMatchesFilters(5, viewerID, timezone, tasks.ListFilters{ProjectFilter: &sharedProject}, "")
	if err != nil {
		t.Fatalf("viewer scoped TaskMatchesFilters: %v", err)
	}
	if !matchScoped {
		t.Fatal("viewer TaskMatchesFilters should be true for project filter")
	}

	editorHome, editorHomeTotal, err := tasks.ReturnPaginationForUserWithFilters(1, 10, &editorID, timezone, tasks.ListFilters{})
	if err != nil {
		t.Fatalf("editor home list: %v", err)
	}
	if editorHomeTotal != 1 || len(editorHome) != 1 || editorHome[0].Title != "Shared owner task" {
		t.Fatalf("editor should see shared tasks on home list, got total %d tasks %v", editorHomeTotal, titles(editorHome))
	}

	editorScoped, editorScopedTotal, err := tasks.ReturnPaginationForUserWithFilters(1, 10, &editorID, timezone, tasks.ListFilters{ProjectFilter: &sharedProject})
	if err != nil {
		t.Fatalf("editor project list: %v", err)
	}
	if editorScopedTotal != 1 || len(editorScoped) != 1 || editorScoped[0].Title != "Shared owner task" {
		t.Fatalf("editor should see shared task in project view, got total %d tasks %v", editorScopedTotal, titles(editorScoped))
	}

	_, searchHomeTotal, err := tasks.SearchTasksForUserWithFilters(1, 10, "Shared", &viewerID, timezone, tasks.ListFilters{})
	if err != nil {
		t.Fatalf("viewer home search: %v", err)
	}
	if searchHomeTotal != 0 {
		t.Fatalf("viewer home search should hide shared tasks, got %d", searchHomeTotal)
	}
	_, searchScopedTotal, err := tasks.SearchTasksForUserWithFilters(1, 10, "Shared", &viewerID, timezone, tasks.ListFilters{ProjectFilter: &sharedProject})
	if err != nil {
		t.Fatalf("viewer project search: %v", err)
	}
	if searchScopedTotal != 1 {
		t.Fatalf("viewer project search should find shared task, got %d", searchScopedTotal)
	}
}

func TestFetchTaskByIDAttachesChildrenAndParent(t *testing.T) {
	pool, err := storage.OpenDatabase()
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	var parentID, childID int
	err = pool.QueryRow(context.Background(),
		`INSERT INTO tasks (title, description, user_id, completed, is_favorite, position, priority, project_id)
		 VALUES ('Parent with kids', 'root', 1, false, false, 50, 0, 1)
		 RETURNING id`).Scan(&parentID)
	if err != nil {
		t.Fatalf("insert parent: %v", err)
	}
	err = pool.QueryRow(context.Background(),
		`INSERT INTO tasks (title, description, user_id, completed, is_favorite, position, priority, project_id, parent_id)
		 VALUES ('Nested child', 'child', 1, false, false, 1, 0, 1, $1)
		 RETURNING id`, parentID).Scan(&childID)
	if err != nil {
		t.Fatalf("insert child: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), "DELETE FROM tasks WHERE id = $1", parentID)
	})

	parent, err := tasks.FetchTaskByIDForUser(parentID, 1, "UTC", 1)
	if err != nil {
		t.Fatalf("fetch parent: %v", err)
	}
	if parent.ChildCount != 1 || len(parent.Children) != 1 {
		t.Fatalf("expected parent to include 1 child, got count=%d children=%d", parent.ChildCount, len(parent.Children))
	}
	if parent.Children[0].ID != childID || parent.Children[0].Title != "Nested child" {
		t.Fatalf("unexpected child on parent: %+v", parent.Children[0])
	}

	child, err := tasks.FetchTaskByIDForUser(childID, 1, "UTC", 1)
	if err != nil {
		t.Fatalf("fetch child: %v", err)
	}
	if child.ParentID != parentID {
		t.Fatalf("expected child parent_id %d, got %d", parentID, child.ParentID)
	}
	if child.ChildCount != 0 || len(child.Children) != 0 {
		t.Fatalf("expected no nested children on subtask, got count=%d children=%d", child.ChildCount, len(child.Children))
	}
}

func TestIncludeSubtasksReturnsOverdueChildOfCompletedParent(t *testing.T) {
	pool, err := storage.OpenDatabase()
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer storage.CloseDatabase(pool)

	ctx := context.Background()
	userID := 1
	timezone := "America/New_York"

	var parentID, childID int
	err = pool.QueryRow(ctx,
		`INSERT INTO tasks (title, description, user_id, completed, is_favorite, position, priority, due_date)
		 VALUES ('Completed parent', 'root', 1, true, false, 80, 0, CURRENT_DATE - 3)
		 RETURNING id`).Scan(&parentID)
	if err != nil {
		t.Fatalf("insert parent: %v", err)
	}
	err = pool.QueryRow(ctx,
		`INSERT INTO tasks (title, description, user_id, completed, is_favorite, position, priority, parent_id, due_date)
		 VALUES ('Leftover overdue child', 'child', 1, false, false, 1, 0, $1, CURRENT_DATE - 2)
		 RETURNING id`, parentID).Scan(&childID)
	if err != nil {
		t.Fatalf("insert child: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, "DELETE FROM tasks WHERE id IN ($1, $2)", childID, parentID)
	})

	orphans, _, err := tasks.ReturnPaginationForUserWithFilters(1, 50, &userID, timezone, tasks.ListFilters{DueFilter: "overdue"})
	if err != nil {
		t.Fatalf("overdue list: %v", err)
	}
	var orphan *tasks.Task
	for i := range orphans {
		if orphans[i].ID == childID {
			orphan = &orphans[i]
			break
		}
	}
	if orphan == nil {
		t.Fatalf("expected leftover overdue child as an orphan row, got %v", titles(orphans))
	}
	if orphan.ParentTitle != "Completed parent" {
		t.Fatalf("orphan parent_title=%q want Completed parent", orphan.ParentTitle)
	}

	withChildren, _, err := tasks.ReturnPaginationForUserWithFilters(1, 50, &userID, timezone, tasks.ListFilters{
		DueFilter:       "overdue",
		IncludeSubtasks: true,
	})
	if err != nil {
		t.Fatalf("include_subtasks overdue list: %v", err)
	}
	var found *tasks.Task
	for i := range withChildren {
		if withChildren[i].ID == childID {
			found = &withChildren[i]
			break
		}
	}
	if found == nil {
		t.Fatalf("expected leftover overdue child in include_subtasks list, got %v", titles(withChildren))
	}
	if found.ParentID != parentID {
		t.Fatalf("child parent_id=%d want %d", found.ParentID, parentID)
	}
	if found.ParentTitle != "Completed parent" {
		t.Fatalf("child parent_title=%q want Completed parent", found.ParentTitle)
	}
	if len(found.Children) != 0 {
		t.Fatalf("flattened child should not nest further children, got %d", len(found.Children))
	}
}

func TestIncompleteFilterReturnsOrphanChildOfCompletedParent(t *testing.T) {
	pool, err := storage.OpenDatabase()
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer storage.CloseDatabase(pool)

	ctx := context.Background()
	userID := 1
	timezone := "America/New_York"

	var parentID, childID int
	err = pool.QueryRow(ctx,
		`INSERT INTO tasks (title, description, user_id, completed, is_favorite, position, priority)
		 VALUES ('Done parent hiding child', 'root', 1, true, false, 81, 0)
		 RETURNING id`).Scan(&parentID)
	if err != nil {
		t.Fatalf("insert parent: %v", err)
	}
	err = pool.QueryRow(ctx,
		`INSERT INTO tasks (title, description, user_id, completed, is_favorite, position, priority, parent_id)
		 VALUES ('Nonced Title', 'child', 1, false, false, 1, 0, $1)
		 RETURNING id`, parentID).Scan(&childID)
	if err != nil {
		t.Fatalf("insert child: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, "DELETE FROM tasks WHERE id IN ($1, $2)", childID, parentID)
	})

	allStatus, _, err := tasks.ReturnPaginationForUserWithFilters(1, 50, &userID, timezone, tasks.ListFilters{})
	if err != nil {
		t.Fatalf("all-status list: %v", err)
	}
	var nested bool
	for _, task := range allStatus {
		if task.ID == childID {
			t.Fatal("all-status list should keep the child nested, not top-level")
		}
		for _, c := range task.Children {
			if c.ID == childID {
				nested = true
			}
		}
	}
	if !nested {
		t.Fatal("expected incomplete child nested under completed parent when status is all")
	}

	incomplete, total, err := tasks.ReturnPaginationForUserWithFilters(1, 50, &userID, timezone, tasks.ListFilters{StatusFilter: "incomplete"})
	if err != nil {
		t.Fatalf("incomplete list: %v", err)
	}
	var found *tasks.Task
	for i := range incomplete {
		if incomplete[i].ID == childID {
			found = &incomplete[i]
			break
		}
	}
	if found == nil {
		t.Fatalf("expected leftover incomplete child in incomplete list, got %v", titles(incomplete))
	}
	if found.ParentID != parentID {
		t.Fatalf("child parent_id=%d want %d", found.ParentID, parentID)
	}
	if found.ParentTitle != "Done parent hiding child" {
		t.Fatalf("child parent_title=%q", found.ParentTitle)
	}
	if total < 1 {
		t.Fatalf("incomplete total should include orphan child, got %d", total)
	}
}

func TestSprintFilterTotalIncludesOrphanSubtasksAndSearch(t *testing.T) {
	pool, err := storage.OpenDatabase()
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer storage.CloseDatabase(pool)

	ctx := context.Background()
	var sprintID int
	err = pool.QueryRow(ctx,
		`INSERT INTO project_sprints (project_id, name, start_date, end_date)
		 VALUES (1, 'Sprint Alpha', CURRENT_DATE, CURRENT_DATE + 14)
		 RETURNING id`).Scan(&sprintID)
	if err != nil {
		t.Fatalf("insert sprint: %v", err)
	}

	var sprintRootID, backlogParentID, orphanChildID int
	err = pool.QueryRow(ctx,
		`INSERT INTO tasks (title, description, user_id, project_id, sprint_id, position)
		 VALUES ('Sprint Root Task', 'root in sprint', 1, 1, $1, 10)
		 RETURNING id`, sprintID).Scan(&sprintRootID)
	if err != nil {
		t.Fatalf("insert sprint root: %v", err)
	}

	err = pool.QueryRow(ctx,
		`INSERT INTO tasks (title, description, user_id, project_id, sprint_id, position)
		 VALUES ('Backlog Parent Task', 'parent in backlog', 1, 1, NULL, 20)
		 RETURNING id`).Scan(&backlogParentID)
	if err != nil {
		t.Fatalf("insert backlog parent: %v", err)
	}

	err = pool.QueryRow(ctx,
		`INSERT INTO tasks (title, description, user_id, project_id, parent_id, sprint_id, position)
		 VALUES ('Sprint Orphan Subtask', 'child in sprint but parent in backlog', 1, 1, $1, $2, 30)
		 RETURNING id`, backlogParentID, sprintID).Scan(&orphanChildID)
	if err != nil {
		t.Fatalf("insert orphan child: %v", err)
	}

	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, "DELETE FROM tasks WHERE id IN ($1, $2, $3)", orphanChildID, backlogParentID, sprintRootID)
		_, _ = pool.Exec(ctx, "DELETE FROM project_sprints WHERE id = $1", sprintID)
	})

	userID := 1
	projectID := 1
	tz := "UTC"

	// 1. Test sprint filter listing: should return root task + orphan child, with total = 2
	sprintList, sprintTotal, err := tasks.ReturnPaginationForUserWithFilters(1, 50, &userID, tz, tasks.ListFilters{
		ProjectFilter: &projectID,
		SprintFilter:  &sprintID,
	})
	if err != nil {
		t.Fatalf("sprint list: %v", err)
	}
	if sprintTotal != 2 {
		t.Fatalf("expected sprintTotal=2 (1 root + 1 orphan subtask), got %d", sprintTotal)
	}
	if len(sprintList) != 2 {
		t.Fatalf("expected 2 tasks in sprint list, got %d", len(sprintList))
	}
	var orphanFound bool
	for _, task := range sprintList {
		if task.ID == orphanChildID {
			orphanFound = true
			if task.ParentID != backlogParentID {
				t.Fatalf("expected orphan child parent_id=%d, got %d", backlogParentID, task.ParentID)
			}
			if task.ParentTitle != "Backlog Parent Task" {
				t.Fatalf("expected orphan child parent_title='Backlog Parent Task', got %q", task.ParentTitle)
			}
		}
	}
	if !orphanFound {
		t.Fatalf("orphan child %d was not found in sprint list: %+v", orphanChildID, titles(sprintList))
	}

	// 2. Test search with sprint filter matching orphan subtask: should return orphan child with total = 1
	searchedList, searchTotal, err := tasks.SearchTasksForUserWithFilters(1, 50, "Orphan", &userID, tz, tasks.ListFilters{
		ProjectFilter: &projectID,
		SprintFilter:  &sprintID,
	})
	if err != nil {
		t.Fatalf("search with sprint filter: %v", err)
	}
	if searchTotal != 1 {
		t.Fatalf("expected searchTotal=1 for orphan child search, got %d", searchTotal)
	}
	if len(searchedList) != 1 || searchedList[0].ID != orphanChildID {
		t.Fatalf("expected orphan child in search results, got %+v", searchedList)
	}

	// 3. Test backlog filter: should include Backlog Parent Task and exclude Sprint Root Task
	zero := 0
	backlogList, _, err := tasks.ReturnPaginationForUserWithFilters(1, 50, &userID, tz, tasks.ListFilters{
		ProjectFilter: &projectID,
		SprintFilter:  &zero,
	})
	if err != nil {
		t.Fatalf("backlog list: %v", err)
	}
	var parentFound, rootFound bool
	for _, task := range backlogList {
		if task.ID == backlogParentID {
			parentFound = true
		}
		if task.ID == sprintRootID {
			rootFound = true
		}
	}
	if !parentFound {
		t.Fatalf("expected backlog parent %d in backlog list", backlogParentID)
	}
	if rootFound {
		t.Fatalf("did not expect sprint root %d in backlog list", sprintRootID)
	}
}

func titles(list []tasks.Task) []string {
	out := make([]string, len(list))
	for i, task := range list {
		out[i] = task.Title
	}
	return out
}

func intPtr(n int) *int {
	return &n
}
