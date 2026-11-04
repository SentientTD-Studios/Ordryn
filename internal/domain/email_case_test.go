package domain

import (
	"context"
	"testing"

	"GoTodo/internal/storage"
)

func insertUserWithPassword(t *testing.T, email, password string) int {
	t.Helper()
	pool, err := storage.OpenDatabase()
	if err != nil {
		t.Fatal(err)
	}
	var id int
	if err := pool.QueryRow(context.Background(), `
		INSERT INTO users (email, password, role_id, user_name)
		VALUES ($1, $2, (SELECT id FROM roles WHERE name = 'user'), $1) RETURNING id`,
		email, password).Scan(&id); err != nil {
		t.Fatalf("insert %s: %v", email, err)
	}
	return id
}

func deleteUser(t *testing.T, id int) {
	t.Helper()
	pool, err := storage.OpenDatabase()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(context.Background(), `DELETE FROM users WHERE id = $1`, id); err != nil {
		t.Fatal(err)
	}
}

func TestEmailLookupsIgnoreCase(t *testing.T) {
	id := insertUserWithPassword(t, "casey@example.com", "hash-casey")

	for _, input := range []string{"casey@example.com", "Casey@Example.COM", "  CASEY@example.com "} {
		u, err := storage.GetUserByEmail(input)
		if err != nil || u.ID != id {
			t.Fatalf("GetUserByEmail(%q) = %v, %v; want id %d", input, u, err, id)
		}
		hash, _, _, err := storage.GetAuthCredentials(input)
		if err != nil || hash != "hash-casey" {
			t.Fatalf("GetAuthCredentials(%q) = %q, %v", input, hash, err)
		}
		exists, err := storage.UserExistsByEmail(input)
		if err != nil || !exists {
			t.Fatalf("UserExistsByEmail(%q) = %v, %v", input, exists, err)
		}
	}
}

func TestNormalizeEmail(t *testing.T) {
	if got := storage.NormalizeEmail("  New.Person@Example.COM "); got != "new.person@example.com" {
		t.Fatalf("NormalizeEmail = %q", got)
	}
}

// Accounts that differ only by case can exist from before this change. Every
// lookup must pick the same one, or login could check one account's password
// and then sign in as the other.
func TestDuplicateCaseEmailsResolveToOneAccount(t *testing.T) {
	older := insertUserWithPassword(t, "Dupe@Example.com", "hash-older")
	newer := insertUserWithPassword(t, "dupe@example.com", "hash-newer")
	t.Cleanup(func() {
		deleteUser(t, older)
		deleteUser(t, newer)
	})

	// An exact-case match wins.
	if u, _ := storage.GetUserByEmail("dupe@example.com"); u == nil || u.ID != newer {
		t.Fatalf("exact match should pick %d, got %+v", newer, u)
	}
	if hash, _, _, _ := storage.GetAuthCredentials("dupe@example.com"); hash != "hash-newer" {
		t.Fatalf("credentials for exact match = %q, want hash-newer", hash)
	}
	// No exact match: the oldest account, consistently.
	if u, _ := storage.GetUserByEmail("DUPE@EXAMPLE.COM"); u == nil || u.ID != older {
		t.Fatalf("no exact match should pick oldest %d, got %+v", older, u)
	}
	if hash, _, _, _ := storage.GetAuthCredentials("DUPE@EXAMPLE.COM"); hash != "hash-older" {
		t.Fatalf("credentials without exact match = %q, want hash-older", hash)
	}

	dups, err := storage.FindEmailCaseDuplicates()
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, d := range dups {
		if d.Email == "dupe@example.com" && len(d.UserIDs) == 2 && d.UserIDs[0] == older && d.UserIDs[1] == newer {
			found = true
		}
	}
	if !found {
		t.Fatalf("duplicates not reported: %+v", dups)
	}

	// With duplicates present the migration warns and skips the index.
	if err := storage.MigrateUsersEmailCaseInsensitive(); err != nil {
		t.Fatalf("migration should not fail on duplicates: %v", err)
	}
	if indexExists(t) {
		t.Fatal("index must not be created while duplicates exist")
	}
}

func TestEmailCaseIndexBlocksNewDuplicates(t *testing.T) {
	if dups, _ := storage.FindEmailCaseDuplicates(); len(dups) > 0 {
		t.Skipf("other tests left duplicates: %+v", dups)
	}
	if err := storage.MigrateUsersEmailCaseInsensitive(); err != nil {
		t.Fatal(err)
	}
	if !indexExists(t) {
		t.Fatal("index should exist once there are no duplicates")
	}
	insertUserWithPassword(t, "unique-case@example.com", "x")
	pool, err := storage.OpenDatabase()
	if err != nil {
		t.Fatal(err)
	}
	_, err = pool.Exec(context.Background(), `
		INSERT INTO users (email, password, role_id, user_name)
		VALUES ('Unique-Case@Example.com', 'x', (SELECT id FROM roles WHERE name = 'user'), 'unique-case-2')`)
	if err == nil {
		t.Fatal("inserting an email that differs only by case should fail")
	}
}

func indexExists(t *testing.T) bool {
	t.Helper()
	pool, err := storage.OpenDatabase()
	if err != nil {
		t.Fatal(err)
	}
	var ok bool
	if err := pool.QueryRow(context.Background(),
		`SELECT EXISTS (SELECT 1 FROM pg_indexes WHERE indexname = 'users_email_lower_key')`).Scan(&ok); err != nil {
		t.Fatal(err)
	}
	return ok
}
