package domain

import (
	"context"
	"fmt"

	"GoTodo/internal/storage"
)

// RequireCanEditAdminAccount allows edits to targetUserID's account details (such
// as the username) only when the actor outranks it: nobody edits the owner, and
// only the owner edits other admins.
func RequireCanEditAdminAccount(actorUserID, targetUserID int) error {
	if storage.IsSiteOwner(targetUserID) && actorUserID != targetUserID {
		return fmt.Errorf("%w: nobody can change the site owner's account", ErrForbidden)
	}
	if storage.UserHasSitePermission(targetUserID, storage.SitePermAdmin) &&
		actorUserID != targetUserID && !storage.IsSiteOwner(actorUserID) {
		return fmt.Errorf("%w: only the site owner can change an admin's account", ErrForbidden)
	}
	return nil
}

// TransferSiteOwnership hands ownership to another active admin. The previous owner
// stays an admin.
func TransferSiteOwnership(ctx context.Context, actorUserID, targetUserID int) error {
	_ = ctx
	if !storage.IsSiteOwner(actorUserID) {
		return fmt.Errorf("%w: only the site owner can transfer ownership", ErrForbidden)
	}
	if targetUserID == actorUserID {
		return fmt.Errorf("%w: you already own this site", ErrValidation)
	}
	if storage.IsSystemUser(targetUserID) || storage.IsAgentUser(targetUserID) {
		return fmt.Errorf("%w: agent and automation accounts can't own the site", ErrValidation)
	}
	if storage.IsUserBannedByID(targetUserID) {
		return fmt.Errorf("%w: reactivate the account before making it the owner", ErrValidation)
	}
	if !storage.UserHasSitePermission(targetUserID, storage.SitePermAdmin) {
		return fmt.Errorf("%w: make them an admin first; the owner is always an admin", ErrValidation)
	}
	return storage.SetSiteOwner(targetUserID)
}
