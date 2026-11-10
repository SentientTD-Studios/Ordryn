package domain

import (
	"context"
	"errors"
	"fmt"

	"GoTodo/internal/live"
	"GoTodo/internal/storage"
)

// SetProjectSlug changes a project's URL slug (project managers only). The old
// slug keeps redirecting to the project.
func SetProjectSlug(ctx context.Context, userID, projectID int, raw string) (string, error) {
	_ = ctx
	normalized, err := storage.NormalizeProjectSlug(raw)
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrValidation, err)
	}
	proj, err := storage.GetAccessibleProjectByID(projectID, userID)
	if err != nil {
		return "", ErrNotFound
	}
	if !storage.RoleCanManageProject(proj.ID, proj.Role) {
		return "", ErrForbidden
	}
	slug, err := storage.SetProjectSlug(projectID, normalized)
	if err != nil {
		if errors.Is(err, storage.ErrProjectSlugTaken) {
			return "", fmt.Errorf("%w: that URL name is already used by another project", ErrConflict)
		}
		return "", err
	}
	if slug != proj.Slug {
		_ = storage.LogProjectEvent(projectID, userID, "slug_changed", map[string]interface{}{
			"from": proj.Slug,
			"to":   slug,
		})
		live.AfterProjectChange(userID, projectID, live.TypeProjectUpdated)
	}
	return slug, nil
}

// ProjectBySlug resolves a current or former slug to a project the user can
// access. Projects the user cannot see look the same as unknown slugs.
func ProjectBySlug(ctx context.Context, userID int, slug string) (*storage.ProjectWithAccess, error) {
	_ = ctx
	id, err := storage.ProjectIDBySlug(slug)
	if err != nil {
		return nil, err
	}
	if id == 0 {
		return nil, ErrNotFound
	}
	proj, err := storage.GetAccessibleProjectByID(id, userID)
	if err != nil {
		return nil, ErrNotFound
	}
	return proj, nil
}
