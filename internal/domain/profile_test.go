package domain

import (
	"context"
	"errors"
	"testing"

	"GoTodo/internal/storage"
)

func TestUpdateProfileValidation(t *testing.T) {
	ctx := context.Background()
	_, err := UpdateProfile(ctx, 1, UpdateProfileInput{Timezone: "Nope", ItemsPerPage: 15}, false, true)
	if !errors.Is(err, ErrValidation) {
		t.Fatalf("bad tz: %v", err)
	}
	_, err = UpdateProfile(ctx, 1, UpdateProfileInput{Timezone: "UTC", ItemsPerPage: 99}, true, false)
	if !errors.Is(err, ErrValidation) {
		t.Fatalf("bad page size: %v", err)
	}
}

// The domain test DB has a trimmed users table, so this reads the column directly
// instead of going through GetUserProfileByID.
func TestUserPrefsHomeSplitView(t *testing.T) {
	pool, err := storage.OpenDatabase()
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer storage.CloseDatabase(pool)
	read := func() (split, invites bool, perPage int) {
		t.Helper()
		if err := pool.QueryRow(context.Background(),
			`SELECT home_split_view, COALESCE(allow_project_invites, true), COALESCE(items_per_page, 15) FROM users WHERE id = 1`,
		).Scan(&split, &invites, &perPage); err != nil {
			t.Fatalf("read prefs: %v", err)
		}
		return
	}

	split, invites, perPage := read()
	if split {
		t.Fatal("home_split_view should default to false")
	}
	t.Cleanup(func() { _ = storage.UpdateUserProfilePrefsByID(1, "UTC", perPage, invites, false) })

	if err := storage.UpdateUserProfilePrefsByID(1, "UTC", perPage, invites, true); err != nil {
		t.Fatalf("update: %v", err)
	}
	gotSplit, gotInvites, gotPerPage := read()
	if !gotSplit {
		t.Fatal("home_split_view was not saved")
	}
	if gotInvites != invites || gotPerPage != perPage {
		t.Fatalf("other prefs changed: invites=%v perPage=%d", gotInvites, gotPerPage)
	}
}

func TestChangePasswordValidation(t *testing.T) {
	ctx := context.Background()
	err := ChangePassword(ctx, 1, "", "abcdefgh", "abcdefgh")
	if !errors.Is(err, ErrValidation) {
		t.Fatalf("missing current: %v", err)
	}
	err = ChangePassword(ctx, 1, "old", "short", "short")
	if !errors.Is(err, ErrValidation) {
		t.Fatalf("short password: %v", err)
	}
	err = ChangePassword(ctx, 1, "old", "abcdefgh", "zzzzzzzz")
	if !errors.Is(err, ErrValidation) {
		t.Fatalf("mismatch: %v", err)
	}
}
