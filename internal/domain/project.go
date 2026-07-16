package domain

import (
	"context"
	"fmt"
	"strings"

	"GoTodo/internal/storage"
)

// CreateProject validates and creates a project for the user.
func CreateProject(ctx context.Context, userID int, name string) (*storage.Project, error) {
	_ = ctx
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, fmt.Errorf("%w: project name is required", ErrValidation)
	}
	return storage.CreateProject(userID, name)
}

// RenameProject updates a project name.
func RenameProject(ctx context.Context, userID, projectID int, name string) error {
	_ = ctx
	name = strings.TrimSpace(name)
	if name == "" {
		return fmt.Errorf("%w: project name is required", ErrValidation)
	}
	return storage.UpdateProject(projectID, userID, name)
}

// DeleteProject removes a project owned by the user.
func DeleteProject(ctx context.Context, userID, projectID int) error {
	_ = ctx
	return storage.DeleteProject(projectID, userID)
}
