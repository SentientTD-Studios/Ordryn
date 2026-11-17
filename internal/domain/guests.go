package domain

import (
	"context"
	"fmt"
	"strings"

	"GoTodo/internal/storage"
)

// GuestPolicyInput is what a site admin sets.
type GuestPolicyInput struct {
	Enabled     bool
	AccessLevel string
	RoleID      int
	ViaTeams    bool
	Directory   bool
}

func requireCompanyAdmin(actorUserID int) error {
	if !storage.IsCompanyMode() {
		return fmt.Errorf("%w: guest accounts are only used in company mode", ErrValidation)
	}
	if !storage.UserHasSitePermission(actorUserID, storage.SitePermAdmin) || storage.IsGuestUser(actorUserID) {
		return ErrForbidden
	}
	return nil
}

func guestRoleProblem(role *storage.SiteRole) error {
	if blocked := storage.GuestBlockedPerms(role.Permissions); len(blocked) > 0 {
		return fmt.Errorf("%w: guests can't have the %s role: it includes site permissions (%s). Guests may only create projects",
			ErrValidation, role.Label(), strings.Join(blocked, ", "))
	}
	return nil
}

// GetGuestPolicyForAdmin returns the guest policy (site admins, company mode).
func GetGuestPolicyForAdmin(ctx context.Context, actorUserID int) (storage.GuestPolicy, int, error) {
	_ = ctx
	if err := requireCompanyAdmin(actorUserID); err != nil {
		return storage.GuestPolicy{}, 0, err
	}
	n, err := storage.CountGuests()
	if err != nil {
		return storage.GuestPolicy{}, 0, err
	}
	return storage.GetGuestPolicy(), n, nil
}

// SaveGuestPolicyForAdmin validates and stores the guest policy, then recomputes
// project access so it applies at once.
func SaveGuestPolicyForAdmin(ctx context.Context, actorUserID int, in GuestPolicyInput) (storage.GuestPolicy, error) {
	_ = ctx
	if err := requireCompanyAdmin(actorUserID); err != nil {
		return storage.GuestPolicy{}, err
	}
	p := storage.GuestPolicy{
		Enabled: in.Enabled, AccessLevel: strings.TrimSpace(in.AccessLevel),
		RoleID: in.RoleID, ViaTeams: in.ViaTeams, Directory: in.Directory,
	}
	switch p.AccessLevel {
	case "", storage.AccessView:
		p.AccessLevel = storage.AccessView
		p.RoleID = 0
	case storage.AccessFull:
		if p.RoleID <= 0 {
			return p, fmt.Errorf("%w: choose the role guests get on projects shared with full access", ErrValidation)
		}
		role, err := storage.GetSiteRole(p.RoleID)
		if err != nil {
			return p, fmt.Errorf("%w: role not found", ErrValidation)
		}
		if err := guestRoleProblem(role); err != nil {
			return p, err
		}
	default:
		return p, fmt.Errorf("%w: access_level must be view or full", ErrValidation)
	}
	if err := storage.SaveGuestPolicy(p, actorUserID); err != nil {
		return p, err
	}
	_ = storage.LogAdminEvent(actorUserID, "guest_policy_updated", "site", 0, "Guest policy", map[string]interface{}{
		"enabled": p.Enabled, "access_level": p.AccessLevel, "role_id": p.RoleID,
		"via_teams": p.ViaTeams, "directory": p.Directory,
	})
	return storage.GetGuestPolicy(), nil
}

// SetGuestForAdmin turns an account into a guest or back into a member. Only
// site admins can, and only while guests are enabled (turning one back is
// always allowed). Admins, the site owner, and accounts whose role holds site
// permissions can't become guests.
func SetGuestForAdmin(ctx context.Context, actorUserID, targetUserID int, guest bool) error {
	_ = ctx
	if err := requireCompanyAdmin(actorUserID); err != nil {
		return err
	}
	if targetUserID == actorUserID {
		return fmt.Errorf("%w: you can't change your own account type", ErrValidation)
	}
	if storage.IsSystemUser(targetUserID) || storage.IsAgentUser(targetUserID) {
		return fmt.Errorf("%w: agent and automation accounts can't be guests", ErrValidation)
	}
	if guest == storage.IsGuestUser(targetUserID) {
		return nil
	}
	if guest {
		if !storage.GetGuestPolicy().Enabled {
			return fmt.Errorf("%w: turn on guest accounts in the guest policy first", ErrValidation)
		}
		if storage.IsSiteOwner(targetUserID) {
			return fmt.Errorf("%w: the site owner can't be a guest", ErrForbidden)
		}
		roleID, _, err := storage.GetUserSiteRoleID(targetUserID)
		if err != nil {
			return ErrNotFound
		}
		role, err := storage.GetSiteRole(roleID)
		if err != nil {
			return err
		}
		if err := guestRoleProblem(role); err != nil {
			return err
		}
	}
	if err := storage.SetUserGuest(targetUserID, guest); err != nil {
		return err
	}
	event := "account_made_member"
	if guest {
		event = "account_made_guest"
	}
	_ = storage.LogAdminEvent(actorUserID, event, "user", int64(targetUserID), storage.UserAuditLabel(targetUserID), nil)
	return nil
}

// checkGuestRoleChange refuses giving a guest a role with site permissions.
func checkGuestRoleChange(targetUserID int, to *storage.SiteRole) error {
	if !storage.IsGuestUser(targetUserID) {
		return nil
	}
	return guestRoleProblem(to)
}

// checkGuestRolePerms refuses adding site permissions to a role guests hold.
func checkGuestRolePerms(role *storage.SiteRole, perms []string) error {
	if len(storage.GuestBlockedPerms(perms)) == 0 || !storage.RoleHeldByGuests(role.ID) {
		return nil
	}
	return fmt.Errorf("%w: guests have the %s role, so it can't include site permissions other than creating projects",
		ErrValidation, role.Label())
}
