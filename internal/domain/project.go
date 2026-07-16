package domain

import (
	"context"
	"fmt"
	"strings"

	"GoTodo/internal/storage"
)

// MaxProjectNameLength is the maximum length of a project name.
const MaxProjectNameLength = 50

// CreateProject validates and creates a project for the user.
func CreateProject(ctx context.Context, userID int, name string) (*storage.Project, error) {
	_ = ctx
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, fmt.Errorf("%w: project name is required", ErrValidation)
	}
	if len(name) > MaxProjectNameLength {
		return nil, fmt.Errorf("%w: project name must be %d characters or less", ErrValidation, MaxProjectNameLength)
	}
	return storage.CreateProject(userID, name)
}

// RenameProject updates a project name and returns the updated project.
func RenameProject(ctx context.Context, userID, projectID int, name string) (*storage.Project, error) {
	_ = ctx
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, fmt.Errorf("%w: project name is required", ErrValidation)
	}
	if len(name) > MaxProjectNameLength {
		return nil, fmt.Errorf("%w: project name must be %d characters or less", ErrValidation, MaxProjectNameLength)
	}
	if _, err := storage.GetProjectByID(projectID, userID); err != nil {
		return nil, ErrNotFound
	}
	if err := storage.UpdateProject(projectID, userID, name); err != nil {
		return nil, err
	}
	return storage.GetProjectByID(projectID, userID)
}

// DeleteProject removes a project owned by the user.
func DeleteProject(ctx context.Context, userID, projectID int) error {
	_ = ctx
	if _, err := storage.GetProjectByID(projectID, userID); err != nil {
		return ErrNotFound
	}
	return storage.DeleteProject(projectID, userID)
}
