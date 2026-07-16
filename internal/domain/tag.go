package domain

import (
	"context"
	"fmt"
	"strings"

	"GoTodo/internal/storage"
)

// MaxTagNameLength is the maximum length of a tag name.
const MaxTagNameLength = 50

// CreateTag creates or returns an existing tag by name.
func CreateTag(ctx context.Context, userID int, name string) (*storage.Tag, error) {
	_ = ctx
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, fmt.Errorf("%w: tag name is required", ErrValidation)
	}
	if len(name) > MaxTagNameLength {
		return nil, fmt.Errorf("%w: tag name must be %d characters or less", ErrValidation, MaxTagNameLength)
	}
	return storage.GetOrCreateTagByName(userID, name)
}

// RenameTag updates a tag name and returns the updated tag.
func RenameTag(ctx context.Context, userID, tagID int, name string) (*storage.Tag, error) {
	_ = ctx
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, fmt.Errorf("%w: tag name is required", ErrValidation)
	}
	if len(name) > MaxTagNameLength {
		return nil, fmt.Errorf("%w: tag name must be %d characters or less", ErrValidation, MaxTagNameLength)
	}
	if _, err := storage.GetTagByID(tagID, userID); err != nil {
		return nil, ErrNotFound
	}
	if err := storage.UpdateTag(tagID, userID, name); err != nil {
		msg := err.Error()
		if strings.Contains(msg, "already exists") || strings.Contains(msg, "required") || strings.Contains(msg, "characters or less") {
			return nil, fmt.Errorf("%w: %s", ErrValidation, msg)
		}
		if strings.Contains(msg, "not found") {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return storage.GetTagByID(tagID, userID)
}

// DeleteTag removes a tag owned by the user.
func DeleteTag(ctx context.Context, userID, tagID int) error {
	_ = ctx
	if _, err := storage.GetTagByID(tagID, userID); err != nil {
		return ErrNotFound
	}
	return storage.DeleteTag(tagID, userID)
}
