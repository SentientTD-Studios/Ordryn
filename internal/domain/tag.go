package domain

import (
	"context"
	"fmt"
	"strings"

	"GoTodo/internal/storage"
)

// CreateTag creates or returns an existing tag by name.
func CreateTag(ctx context.Context, userID int, name string) (*storage.Tag, error) {
	_ = ctx
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, fmt.Errorf("%w: tag name is required", ErrValidation)
	}
	return storage.GetOrCreateTagByName(userID, name)
}

// RenameTag updates a tag name.
func RenameTag(ctx context.Context, userID, tagID int, name string) error {
	_ = ctx
	name = strings.TrimSpace(name)
	if name == "" {
		return fmt.Errorf("%w: tag name is required", ErrValidation)
	}
	return storage.UpdateTag(tagID, userID, name)
}

// DeleteTag removes a tag owned by the user.
func DeleteTag(ctx context.Context, userID, tagID int) error {
	_ = ctx
	return storage.DeleteTag(tagID, userID)
}
