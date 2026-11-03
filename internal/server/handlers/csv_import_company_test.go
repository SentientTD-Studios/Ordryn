package handlers

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"GoTodo/internal/domain"
	"GoTodo/internal/storage"

	embeddedpostgres "github.com/fergusstrange/embedded-postgres"
)

// startImportTestDB runs a throwaway Postgres for this file's tests only, so the rest
// of the handlers package stays database-free.
func startImportTestDB(t *testing.T) {
	t.Helper()
	const port = 5444
	db := embeddedpostgres.NewDatabase(embeddedpostgres.DefaultConfig().
		Version(embeddedpostgres.V16).
		Port(port).
		Database("gotodo_import_test").
		RuntimePath(filepath.Join(os.TempDir(), "gotodo-embedded-pg-handlers-import")))
	if err := db.Start(); err != nil {
		t.Skipf("embedded postgres unavailable: %v", err)
	}
	t.Cleanup(func() { _ = db.Stop() })
	t.Setenv("SESSION_KEY", "test-session-key-for-unit-tests-32chars!!")
	t.Setenv("DB_HOST", "localhost")
	t.Setenv("DB_PORT", fmt.Sprintf("%d", port))
	t.Setenv("DB_USER", "postgres")
	t.Setenv("DB_PASSWORD", "postgres")
	t.Setenv("DB_NAME", "gotodo_import_test")
	t.Setenv("DB_SSLMODE", "disable")
	storage.ResetSharedPoolForTests()
	t.Cleanup(storage.ResetSharedPoolForTests)
	if err := storage.RunMigrations(); err != nil {
		t.Fatalf("migrate: %v", err)
	}
}

// runImport fails the test instead of hanging if the import ever blocks.
func runImport(t *testing.T, userID int, cols importColumnMap, rows [][]string) (int, error) {
	t.Helper()
	type result struct {
		n   int
		err error
	}
	done := make(chan result, 1)
	go func() {
		n, _, err := importTasksFromCSV(userID, cols, rows)
		done <- result{n, err}
	}()
	select {
	case r := <-done:
		return r.n, r.err
	case <-time.After(30 * time.Second):
		t.Fatal("import hung")
		return 0, nil
	}
}

func TestCompanyCSVImportUsesSharedProjects(t *testing.T) {
	startImportTestDB(t)
	ctx := context.Background()

	token, err := domain.IssueSetupToken()
	if err != nil {
		t.Fatal(err)
	}
	owner, err := domain.CompleteSetup(ctx, domain.SetupInput{
		Token: token, Email: "owner@example.com", UserName: "owner", Password: "password1",
		ConfirmPassword: "password1", Timezone: "UTC", SiteMode: storage.SiteModeCompany,
	}, true)
	if err != nil {
		t.Fatalf("setup: %v", err)
	}
	roleID := func(name string) int {
		id, err := storage.GetRoleIDByName(name)
		if err != nil {
			t.Fatalf("role %s: %v", name, err)
		}
		return id
	}
	created, err := domain.CreateAccountForUser(ctx, owner, domain.CreateAccountInput{
		FirstName: "Ann", LastName: "Lee", Email: "ann@example.com", UserName: "ann", SiteRoleID: roleID("Reader"),
	})
	if err != nil {
		t.Fatal(err)
	}
	ann := created.UserID

	shared, err := domain.CreateProject(ctx, owner, "Shared", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := domain.ShareProjectForUser(ctx, owner, shared.ID, domain.ProjectShareInput{Username: "ann", RoleID: roleID("Contributor")}); err != nil {
		t.Fatal(err)
	}
	if _, err := storage.GetOrCreateTagByName(owner, &shared.ID, "existing"); err != nil {
		t.Fatal(err)
	}
	peek, err := domain.CreateProject(ctx, owner, "Peek", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := domain.ShareProjectForUser(ctx, owner, peek.ID, domain.ProjectShareInput{Username: "ann", Level: storage.AccessView}); err != nil {
		t.Fatal(err)
	}

	cols := importColumnMap{title: 0, description: -1, completed: -1, dueDate: -1, project: 1, priority: -1, favorite: -1, tags: 2}

	// A project shared with Ann is found by name; no duplicate is created. She can use
	// its existing tags but, without Manage tags, not add new ones.
	if n, err := runImport(t, ann, cols, [][]string{{"Imported", "shared", "existing;brandnew"}}); err != nil || n != 1 {
		t.Fatalf("import into shared project: n=%d err=%v", n, err)
	}
	projects, err := storage.GetAccessibleProjects(ann)
	if err != nil {
		t.Fatal(err)
	}
	named := 0
	for _, p := range projects {
		if p.Name == "Shared" {
			named++
		}
	}
	if named != 1 {
		t.Fatalf("Ann sees %d projects named Shared, want 1", named)
	}
	if tag, _ := storage.FindTagByName(owner, &shared.ID, "brandnew"); tag != nil {
		t.Fatal("import created a project tag without Manage tags")
	}

	// A project she can only view stops the import instead of being duplicated.
	if _, err := runImport(t, ann, cols, [][]string{{"Nope", "Peek", ""}}); !errors.Is(err, errImportProjectReadOnly) {
		t.Fatalf("import into view-only project: err=%v, want errImportProjectReadOnly", err)
	}
	// Every company task needs a project.
	if _, err := runImport(t, ann, cols, [][]string{{"Loose", "", ""}}); !errors.Is(err, errImportProjectRequired) {
		t.Fatalf("import without a project: err=%v, want errImportProjectRequired", err)
	}

	// A project the import creates can get new tags in the same import. This used to
	// fail: tags were created on another connection, which couldn't see the uncommitted
	// project, so the foreign key rejected them.
	if n, err := runImport(t, owner, cols, [][]string{{"Fresh task", "Fresh", "alpha;beta"}}); err != nil || n != 1 {
		t.Fatalf("import creating a project with tags: n=%d err=%v", n, err)
	}
}
