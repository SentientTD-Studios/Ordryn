package domain

import (
	"context"
	"fmt"
	"strings"

	"GoTodo/internal/storage"
)

// MaxTagNameLength is the maximum length of a tag name.
const MaxTagNameLength = 50

// MaxTagColorLength is the maximum length of a tag color value.
const MaxTagColorLength = 20

// DefaultTagColor is used when a tag color is cleared.
const DefaultTagColor = "#6c757d"

func normalizeTagName(name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", fmt.Errorf("%w: tag name is required", ErrValidation)
	}
	if len(name) > MaxTagNameLength {
		return "", fmt.Errorf("%w: tag name must be %d characters or less", ErrValidation, MaxTagNameLength)
	}
	return name, nil
}

func normalizeTagColor(color string) (string, error) {
	color = strings.TrimSpace(color)
	if color == "" {
		return DefaultTagColor, nil
	}
	if len(color) > MaxTagColorLength {
		return "", fmt.Errorf("%w: tag color must be %d characters or less", ErrValidation, MaxTagColorLength)
	}
	return color, nil
}

func normalizeProjectIDArg(projectID *int) *int {
	if projectID == nil || *projectID <= 0 {
		return nil
	}
	return projectID
}

// ListTags returns tags for a namespace.
// projectID nil means all accessible tags; *0 means personal; *N means that project.
func ListTags(ctx context.Context, userID int, projectID *int) ([]storage.Tag, error) {
	_ = ctx
	if projectID == nil {
		_, _ = storage.EnsureArchivedTag(userID, nil)
		return storage.GetAccessibleTags(userID)
	}
	if *projectID <= 0 {
		_, _ = storage.EnsureArchivedTag(userID, nil)
		return storage.GetPersonalTags(userID)
	}
	if _, err := storage.GetAccessibleProjectByID(*projectID, userID); err != nil {
		return nil, ErrNotFound
	}
	_, _ = storage.EnsureArchivedTag(userID, projectID)
	return storage.GetProjectTags(*projectID)
}

// CreateTag creates or returns an existing tag by name in a namespace.
func CreateTag(ctx context.Context, userID int, name string, projectID *int) (*storage.Tag, error) {
	_ = ctx
	name, err := normalizeTagName(name)
	if err != nil {
		return nil, err
	}
	if storage.IsSystemTagName(name) {
		return nil, fmt.Errorf("%w: tag name is reserved", ErrValidation)
	}
	projectID = normalizeProjectIDArg(projectID)
	ok, err := storage.UserCanManageTagNamespace(userID, projectID)
	if err != nil {
		return nil, ErrNotFound
	}
	if !ok {
		return nil, ErrForbidden
	}
	return storage.GetOrCreateTagByName(userID, projectID, name)
}

// UpdateTag updates a tag's name and/or color and returns the updated tag.
// Omitted fields keep their current values.
func UpdateTag(ctx context.Context, userID, tagID int, name, color *string) (*storage.Tag, error) {
	_ = ctx
	if name == nil && color == nil {
		return nil, fmt.Errorf("%w: name or color is required", ErrValidation)
	}
	var nextName string
	if name != nil {
		var err error
		nextName, err = normalizeTagName(*name)
		if err != nil {
			return nil, err
		}
	}
	var nextColor string
	if color != nil {
		var err error
		nextColor, err = normalizeTagColor(*color)
		if err != nil {
			return nil, err
		}
	}
	tag, err := storage.GetTag(tagID)
	if err != nil {
		return nil, ErrNotFound
	}
	ok, err := storage.UserCanManageTag(userID, *tag)
	if err != nil || !ok {
		if access, aerr := storage.UserCanAccessTag(userID, *tag); aerr == nil && access {
			return nil, ErrForbidden
		}
		return nil, ErrNotFound
	}
	if tag.Protected || storage.IsSystemTagName(tag.Name) {
		return nil, fmt.Errorf("%w: cannot update a protected tag", ErrValidation)
	}
	if name == nil {
		nextName = tag.Name
	} else if storage.IsSystemTagName(nextName) {
		return nil, fmt.Errorf("%w: tag name is reserved", ErrValidation)
	}
	if color == nil {
		nextColor = tag.Color
	}
	if err := storage.UpdateTag(tagID, nextName, nextColor); err != nil {
		msg := err.Error()
		if strings.Contains(msg, "already exists") || strings.Contains(msg, "required") || strings.Contains(msg, "characters or less") {
			return nil, fmt.Errorf("%w: %s", ErrValidation, msg)
		}
		if strings.Contains(msg, "not found") {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return storage.GetTag(tagID)
}

// RenameTag updates a tag name and returns the updated tag.
func RenameTag(ctx context.Context, userID, tagID int, name string) (*storage.Tag, error) {
	return UpdateTag(ctx, userID, tagID, &name, nil)
}

// DeleteTag removes a tag the user is allowed to manage.
func DeleteTag(ctx context.Context, userID, tagID int) error {
	_ = ctx
	tag, err := storage.GetTag(tagID)
	if err != nil {
		return ErrNotFound
	}
	ok, err := storage.UserCanManageTag(userID, *tag)
	if err != nil || !ok {
		if access, aerr := storage.UserCanAccessTag(userID, *tag); aerr == nil && access {
			return ErrForbidden
		}
		return ErrNotFound
	}
	if tag.Protected || storage.IsSystemTagName(tag.Name) {
		return fmt.Errorf("%w: cannot delete a protected tag", ErrValidation)
	}
	return storage.DeleteTag(tagID)
}
