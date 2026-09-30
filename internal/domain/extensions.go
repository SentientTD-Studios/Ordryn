package domain

import (
	"fmt"

	"GoTodo/internal/extensions"
	"GoTodo/internal/live"
	"GoTodo/internal/storage"
)

// RequireProjectExtensionMember returns the project if the user can access it.
func RequireProjectExtensionMember(userID, projectID int) (*storage.ProjectWithAccess, error) {
	proj, err := storage.GetAccessibleProjectByID(projectID, userID)
	if err != nil {
		return nil, ErrNotFound
	}
	return proj, nil
}

// RequireProjectExtensionOwner returns the project if the user is the owner.
func RequireProjectExtensionOwner(userID, projectID int) (*storage.ProjectWithAccess, error) {
	proj, err := storage.GetAccessibleProjectByID(projectID, userID)
	if err != nil {
		return nil, ErrNotFound
	}
	if !storage.RoleCanManageProject(proj.ID, proj.Role) {
		return nil, fmt.Errorf("%w: only the project owner can manage extensions", ErrForbidden)
	}
	return proj, nil
}

// ReloadExtensions rescans data/extensions, updates the in-memory registry,
// syncs custom field definitions in PostgreSQL, and notifies connected clients via SSE.
func ReloadExtensions(actorID int) ([]extensions.Entry, error) {
	entries := extensions.Load()
	if err := SyncCustomFieldDefs(); err != nil {
		return entries, fmt.Errorf("sync custom field defs: %w", err)
	}
	live.AfterExtensionsReload(actorID)
	return entries, nil
}
