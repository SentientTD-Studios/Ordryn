package domain

import (
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"GoTodo/internal/storage"
)

func TestNormalizeLocalAgentKeyInput(t *testing.T) {
	prev := localAgentKeyProjectAccess
	localAgentKeyProjectAccess = func(projectID, userID int) bool { return projectID == 10 && userID == 7 }
	t.Cleanup(func() { localAgentKeyProjectAccess = prev })
	now := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
	ptr := func(n int) *int { return &n }

	name, scopes, err := NormalizeLocalAgentKeyInput(7, CreateLocalAgentKeyInput{Name: "  laptop  "}, now)
	if err != nil || name != "laptop" || !reflect.DeepEqual(scopes, []string{storage.APIScopeTasksRead}) {
		t.Fatalf("defaults: %q %v %v", name, scopes, err)
	}

	_, scopes, err = NormalizeLocalAgentKeyInput(7, CreateLocalAgentKeyInput{Name: "x", ProjectID: ptr(10),
		Scopes: []string{"comments:write", "TASKS:READ", "tasks:read"}}, now)
	if err != nil || !reflect.DeepEqual(scopes, []string{storage.APIScopeTasksRead, storage.APIScopeCommentsWrite}) {
		t.Fatalf("scopes: %v %v", scopes, err)
	}

	future := now.Add(24 * time.Hour)
	past := now.Add(-time.Hour)
	for _, c := range []struct {
		in   CreateLocalAgentKeyInput
		want string
	}{
		{CreateLocalAgentKeyInput{Name: " "}, "name is required"},
		{CreateLocalAgentKeyInput{Name: strings.Repeat("x", 81)}, "too long"},
		{CreateLocalAgentKeyInput{Name: "x", Scopes: []string{"comments:write"}}, "tasks:read"},
		{CreateLocalAgentKeyInput{Name: "x", Scopes: []string{"admin"}}, "unknown scope"},
		{CreateLocalAgentKeyInput{Name: "x", ProjectID: ptr(20)}, "project not found"},
		{CreateLocalAgentKeyInput{Name: "x", ProjectID: ptr(0)}, "project not found"},
		{CreateLocalAgentKeyInput{Name: "x", ExpiresAt: &past}, "future"},
	} {
		_, _, err := NormalizeLocalAgentKeyInput(7, c.in, now)
		if !errors.Is(err, ErrValidation) || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%+v: err=%v, want validation containing %q", c.in, err, c.want)
		}
	}
	if _, _, err := NormalizeLocalAgentKeyInput(7, CreateLocalAgentKeyInput{Name: "x", ExpiresAt: &future}, now); err != nil {
		t.Errorf("future expiry: %v", err)
	}
}

func TestLocalAgentKeyRoundTrip(t *testing.T) {
	pid := sharedProject(t, "Local agent keys")

	// User 3 is not a member, so can't limit a key to the project.
	if _, _, err := CreateLocalAgentKey(3, CreateLocalAgentKeyInput{Name: "nope", ProjectID: &pid}); !errors.Is(err, ErrValidation) {
		t.Fatalf("non-member limit: err=%v", err)
	}

	plaintext, rec, err := CreateLocalAgentKey(2, CreateLocalAgentKeyInput{
		Name: "laptop", ProjectID: &pid, Scopes: []string{"tasks:read", "comments:write"},
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if rec.Kind != storage.APIKeyKindLocalAgent || rec.LimitProjectID == nil || *rec.LimitProjectID != pid {
		t.Fatalf("record = %+v", rec)
	}

	p, err := storage.LookupAPIKey(plaintext)
	if err != nil {
		t.Fatalf("lookup: %v", err)
	}
	if !p.IsLocalAgent || p.IsAgent || p.UserID != 2 || p.ProjectID != 0 || p.LimitProjectID != pid ||
		!p.HasScope(storage.APIScopeCommentsWrite) || p.HasScope(storage.APIScopeTasksWrite) {
		t.Fatalf("principal = %+v", p)
	}

	keys, err := storage.ListAPIKeysForUser(2)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, k := range keys {
		if k.ID == rec.ID {
			found = true
			if k.Kind != storage.APIKeyKindLocalAgent || k.LimitProjectName != "Local agent keys" || len(k.Scopes) != 2 {
				t.Errorf("listed key = %+v", k)
			}
		}
	}
	if !found {
		t.Fatal("local agent key not listed with personal keys")
	}

	// Same name rotates: the old key stops working.
	if _, _, err := CreateLocalAgentKey(2, CreateLocalAgentKeyInput{Name: "laptop"}); err != nil {
		t.Fatal(err)
	}
	if _, err := storage.LookupAPIKey(plaintext); err == nil {
		t.Fatal("rotated key still works")
	}

	// A standard personal key is not a local agent key.
	std, _, err := storage.CreateAPIKey(2, "standard "+t.Name())
	if err != nil {
		t.Fatal(err)
	}
	if p, err := storage.LookupAPIKey(std); err != nil || p.IsLocalAgent || p.LimitProjectID != 0 {
		t.Fatalf("standard key = %+v, %v", p, err)
	}
}
