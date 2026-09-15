package domain

import (
	"fmt"

	"GoTodo/internal/storage"
)

// RequireProjectModOwner returns the project if the user is the owner.
func RequireProjectModOwner(userID, projectID int) (*storage.ProjectWithAccess, error) {
	proj, err := storage.GetAccessibleProjectByID(projectID, userID)
	if err != nil {
		return nil, ErrNotFound
	}
	if !storage.RoleCanManage(proj.Role) {
		return nil, fmt.Errorf("%w: only the project owner can manage mods", ErrForbidden)
	}
	return proj, nil
}
