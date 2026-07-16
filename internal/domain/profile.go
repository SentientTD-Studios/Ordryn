package domain

import (
	"context"
	"fmt"
	"strings"

	"GoTodo/internal/storage"

	"golang.org/x/crypto/bcrypt"
)

// UpdateProfileInput is the shared profile update payload.
type UpdateProfileInput struct {
	UserName      string
	Timezone      string
	ItemsPerPage  int
	DigestEnabled bool
	DigestHour    int
}

// UpdateProfile validates and persists profile fields. timezoneOK should come from utils.IsValidTimezone.
func UpdateProfile(ctx context.Context, userID int, in UpdateProfileInput, timezoneOK bool, itemsPerPageOK bool) (*storage.UserProfile, error) {
	_ = ctx
	in.UserName = strings.TrimSpace(in.UserName)
	in.Timezone = strings.TrimSpace(in.Timezone)
	if in.UserName == "" {
		return nil, fmt.Errorf("%w: name is required", ErrValidation)
	}
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
	if err := storage.UpdateUserProfileByID(userID, in.UserName, in.Timezone, in.ItemsPerPage, in.DigestEnabled, in.DigestHour); err != nil {
		return nil, err
	}
	return storage.GetUserProfileByID(userID)
}

// ChangePassword verifies the current password and sets a new one.
func ChangePassword(ctx context.Context, userID int, currentPassword, newPassword, confirmPassword string) error {
	_ = ctx
	if currentPassword == "" || newPassword == "" || confirmPassword == "" {
		return fmt.Errorf("%w: all password fields are required", ErrValidation)
	}
	if newPassword != confirmPassword {
		return fmt.Errorf("%w: new passwords do not match", ErrValidation)
	}
	if len(newPassword) < 8 {
		return fmt.Errorf("%w: new password must be at least 8 characters long", ErrValidation)
	}
	hash, err := storage.GetPasswordHashByID(userID)
	if err != nil {
		return err
	}
	if err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(currentPassword)); err != nil {
		return fmt.Errorf("%w: current password is incorrect", ErrValidation)
	}
	hashed, err := bcrypt.GenerateFromPassword([]byte(newPassword), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	return storage.UpdatePasswordByID(userID, string(hashed))
}
