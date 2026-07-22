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
	DigestEnabled       bool
	DigestHour          int
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
