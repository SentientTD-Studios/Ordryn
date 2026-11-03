package domain

import (
	"context"
	"fmt"

	"GoTodo/internal/storage"

	"golang.org/x/crypto/bcrypt"
)

// UpdateProfileInput is the shared profile update payload (username is not mutable here).
type UpdateProfileInput struct {
	Timezone            string
	ItemsPerPage        int
	AllowProjectInvites bool
}

// UpdateProfile validates and persists profile preference fields. timezoneOK should come from utils.IsValidTimezone.
func UpdateProfile(ctx context.Context, userID int, in UpdateProfileInput, timezoneOK bool, itemsPerPageOK bool) (*storage.UserProfile, error) {
	return UpdateProfileWithoutUsername(ctx, userID, in, timezoneOK, itemsPerPageOK)
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
	if err := ValidatePassword(newPassword); err != nil {
		return err
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

// UpdateAvatarStorage is overridable in tests.
var UpdateAvatarStorage = storage.UpdateUserAvatarURL

// GetUserProfileStorage is overridable in tests.
var GetUserProfileStorage = storage.GetUserProfileByID

// UpdateAvatarURL updates the user's avatar image URL and returns the updated profile.
func UpdateAvatarURL(ctx context.Context, userID int, avatarURL string) (*storage.UserProfile, error) {
	_ = ctx
	if err := UpdateAvatarStorage(userID, avatarURL); err != nil {
		return nil, err
	}
	return GetUserProfileStorage(userID)
}

// RemoveAvatar clears the user's avatar image URL and returns the updated profile.
func RemoveAvatar(ctx context.Context, userID int) (*storage.UserProfile, error) {
	return UpdateAvatarURL(ctx, userID, "")
}

