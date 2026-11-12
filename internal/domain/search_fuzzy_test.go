package domain

import (
	"context"
	"slices"
	"strconv"
	"testing"

	"GoTodo/internal/storage"
	"GoTodo/internal/tasks"
)

func TestSiteSearchToleratesTypos(t *testing.T) {
	useFreshDatabase(t, "gotodo_search_fuzzy_test")
	ctx := context.Background()

	owner := insertUserWithRole(t, "owner@example.com", storage.AdminRoleName)
	proj, err := CreateProject(ctx, owner, "Search Board", "")
	if err != nil {
		t.Fatal(err)
	}
	receive := mustCreateTask(t, owner, CreateTaskInput{Title: "Receive vendor invoices", ProjectID: &proj.ID})
	dashboard := mustCreateTask(t, owner, CreateTaskInput{Title: "Redesign the dashboard", ProjectID: &proj.ID})
	exact := mustCreateTask(t, owner, CreateTaskInput{Title: "Dashbord typo kept on purpose", ProjectID: &proj.ID})
	login := mustCreateTask(t, owner, CreateTaskInput{Title: "Fix login page", ProjectID: &proj.ID})
	tagged := mustCreateTask(t, owner, CreateTaskInput{Title: "Unrelated chores", Description: "Count the quarterly numbers", ProjectID: &proj.ID})
	tag, err := storage.GetOrCreateTagByName(owner, &proj.ID, "quarterly")
	if err != nil {
		t.Fatal(err)
	}
	if err := storage.SetTaskTags(tagged, owner, []int{tag.ID}); err != nil {
		t.Fatal(err)
	}

	if _, err := storage.CreateTaskComment(login, owner, "Blocked on the staging certificate"); err != nil {
		t.Fatal(err)
	}
	gone, err := storage.CreateTaskComment(login, owner, "Ask about the warehouse")
	if err != nil {
		t.Fatal(err)
	}
	if err := storage.SoftDeleteTaskComment(gone.ID, owner, "user"); err != nil {
		t.Fatal(err)
	}

	titlesOnly := false
	search := func(raw string) []int {
		t.Helper()
		q := tasks.ParseSearchQuery(raw)
		q.TitlesOnly = titlesOnly
		projects, err := storage.GetAccessibleProjects(owner)
		if err != nil {
			t.Fatal(err)
		}
		found, total, err := tasks.SiteSearchTasks(owner, "UTC", q, tasks.ResolveSearchScope(projects, q, 0), 1, 50)
		if err != nil {
			t.Fatalf("search %q: %v", raw, err)
		}
		if total != len(found) {
			t.Fatalf("search %q: total %d but %d rows", raw, total, len(found))
		}
		ids := make([]int, len(found))
		for i, f := range found {
			ids[i] = f.ID
		}
		return ids
	}
	expect := func(raw string, want ...int) {
		t.Helper()
		if got := search(raw); !slices.Equal(got, want) {
			t.Errorf("search %q = %v, want %v", raw, got, want)
		}
	}

	expect("invoice", receive)
	expect("recieve", receive)           // swapped letters
	expect("RECIEVE vendr", receive)     // every term may have a typo, any case
	expect("lgoin", login)               // swap at the start
	expect("quartely")                   // typos are forgiven in titles only
	expect("quarterly", tagged)          // tag name and description
	expect("numbers", tagged)            // description
	expect("certificate", login)         // comment
	expect("warehouse")                  // deleted comments don't count
	expect("dashbord", exact, dashboard) // the spelling as typed ranks first
	expect("xylophone")
	expect("fxi") // terms under five letters must match as typed

	// Renaming a task teaches the dictionary its new words.
	execSQL(t, `UPDATE tasks SET title = 'Archive spreadsheet' WHERE id = $1`, login)
	expect("spredsheet", login)

	// Words written before the dictionary existed are filled in on migration.
	execSQL(t, `DROP TRIGGER gotodo_task_search_words ON tasks`)
	execSQL(t, `UPDATE tasks SET title = 'Onboarding checklist' WHERE id = $1`, login)
	expect("onbaording")
	if err := storage.MigrateSearchWords(); err != nil {
		t.Fatal(err)
	}
	expect("onbaording", login)

	// Live results look at titles and ids only.
	titlesOnly = true
	expect("onbaording", login)
	expect("quarterly")
	expect("numbers")
	expect("certificate")
	expect("chores", tagged)
	expect("#"+strconv.Itoa(receive), receive)
}
