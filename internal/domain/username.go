package domain

import (
	"context"
	"fmt"
	"strings"

	"GoTodo/internal/storage"
	"GoTodo/internal/username"
)

// NormalizeUsername trims whitespace and strips invisible characters.
func NormalizeUsername(s string) string {
	return username.Normalize(s)
}

// ValidateUsernameFormat checks length and charset after normalization.
func ValidateUsernameFormat(s string) error {
	msg := username.FormatError(s)
	if msg == "" {
		return nil
	}
	return fmt.Errorf("%w: %s", ErrValidation, msg)
}

// PrepareUsername normalizes and validates format (does not check uniqueness).
func PrepareUsername(raw string) (string, error) {
	name := username.Normalize(raw)
	if err := ValidateUsernameFormat(name); err != nil {
		return "", err
	}
	return name, nil
}

// UsernameAvailable reports whether the normalized username is free.
// excludeUserID skips that user (for renames); pass 0 when registering.
func UsernameAvailable(raw string, excludeUserID int) (normalized string, available bool, err error) {
	name, err := PrepareUsername(raw)
	if err != nil {
		return "", false, err
	}
	taken, err := storage.UsernameTaken(name, excludeUserID)
	if err != nil {
		return "", false, err
	}
	return name, !taken, nil
}

// ClaimUsername performs the one-time free username change for migrated users.
func ClaimUsername(ctx context.Context, userID int, raw string) (*storage.UserProfile, error) {
	_ = ctx
	name, err := PrepareUsername(raw)
	if err != nil {
		return nil, err
	}
	ok, err := storage.UserHasUsernameChangeAvailable(userID)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, fmt.Errorf("%w: username cannot be changed", ErrForbidden)
	}
	taken, err := storage.UsernameTaken(name, userID)
	if err != nil {
		return nil, err
	}
	if taken {
		return nil, fmt.Errorf("%w: username is already taken", ErrValidation)
	}
	if err := storage.SetUsername(userID, name, false); err != nil {
		return nil, err
	}
	return storage.GetUserProfileByID(userID)
}

// AdminSetUsername changes a user's username and clears the free-change flag.
func AdminSetUsername(ctx context.Context, userID int, raw string) (*storage.UserProfile, error) {
	_ = ctx
	name, err := PrepareUsername(raw)
	if err != nil {
		return nil, err
	}
	taken, err := storage.UsernameTaken(name, userID)
	if err != nil {
		return nil, err
	}
	if taken {
		return nil, fmt.Errorf("%w: username is already taken", ErrValidation)
	}
	if err := storage.SetUsername(userID, name, false); err != nil {
		return nil, err
	}
	return storage.GetUserProfileByID(userID)
}

// EnsureUsernameForRegister validates and checks availability for signup.
func EnsureUsernameForRegister(raw string) (string, error) {
	name, err := PrepareUsername(raw)
	if err != nil {
		return "", err
	}
	taken, err := storage.UsernameTaken(name, 0)
	if err != nil {
		return "", err
	}
	if taken {
		return "", fmt.Errorf("%w: username is already taken", ErrValidation)
	}
	return name, nil
}

// UpdateProfileWithoutUsername keeps profile prefs without touching user_name.
func UpdateProfileWithoutUsername(ctx context.Context, userID int, in UpdateProfileInput, timezoneOK bool, itemsPerPageOK bool) (*storage.UserProfile, error) {
	_ = ctx
	in.Timezone = strings.TrimSpace(in.Timezone)
	if !timezoneOK {
		return nil, fmt.Errorf("%w: invalid timezone", ErrValidation)
	}
	if in.ItemsPerPage <= 0 {
		in.ItemsPerPage = 15
	}
	if !itemsPerPageOK {
		return nil, fmt.Errorf("%w: invalid items per page", ErrValidation)
	}
	if in.DigestHour < 0 || in.DigestHour > 23 {
		return nil, fmt.Errorf("%w: digest_hour must be between 0 and 23", ErrValidation)
	}
	if err := storage.UpdateUserProfilePrefsByID(userID, in.Timezone, in.ItemsPerPage, in.DigestEnabled, in.DigestHour, in.AllowProjectInvites); err != nil {
		return nil, err
	}
	return storage.GetUserProfileByID(userID)
}
