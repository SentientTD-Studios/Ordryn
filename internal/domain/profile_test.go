package domain

import (
	"context"
	"errors"
	"testing"
)

func TestUpdateProfileValidation(t *testing.T) {
	ctx := context.Background()
	_, err := UpdateProfile(ctx, 1, UpdateProfileInput{UserName: "", Timezone: "UTC", ItemsPerPage: 15}, true, true)
	if !errors.Is(err, ErrValidation) {
		t.Fatalf("empty name: %v", err)
	}
	_, err = UpdateProfile(ctx, 1, UpdateProfileInput{UserName: "Ada", Timezone: "Nope", ItemsPerPage: 15}, false, true)
	if !errors.Is(err, ErrValidation) {
		t.Fatalf("bad tz: %v", err)
	}
	_, err = UpdateProfile(ctx, 1, UpdateProfileInput{UserName: "Ada", Timezone: "UTC", ItemsPerPage: 99}, true, false)
	if !errors.Is(err, ErrValidation) {
		t.Fatalf("bad page size: %v", err)
	}
	_, err = UpdateProfile(ctx, 1, UpdateProfileInput{UserName: "Ada", Timezone: "UTC", ItemsPerPage: 15, DigestHour: 24}, true, true)
	if !errors.Is(err, ErrValidation) {
		t.Fatalf("bad digest hour: %v", err)
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
