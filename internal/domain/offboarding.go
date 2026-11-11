package domain

import (
	"context"
	"fmt"

	"GoTodo/internal/live"
	"GoTodo/internal/storage"
)

// Claim handling when someone is offboarded.
const (
	OffboardClaimsKeep     = "keep"
	OffboardClaimsRelease  = "release"
	OffboardClaimsReassign = "reassign"
)

// OffboardInput says what to do with a departing account's work.
type OffboardInput struct {
	// SuccessorID receives reassigned claims and owned projects; 0 for none.
	SuccessorID int
	// Claims is keep, release, or reassign (needs SuccessorID).
	Claims string
	// TransferProjects makes the successor an owner (multi mode) or gives them a
	// full share (company mode) on every project the account owns.
	TransferProjects bool
	// RevokeKeys revokes every personal and project API key the account made, so
	// they stay dead if the account is reactivated later.
	RevokeKeys bool
}

// OffboardResult reports what changed.
type OffboardResult struct {
	ClaimsReassigned    int `json:"claims_reassigned"`
	ClaimsReleased      int `json:"claims_released"`
	ProjectsTransferred int `json:"projects_transferred"`
	KeysRevoked         int `json:"keys_revoked"`
}

// OffboardingPreviewForUser returns what targetUserID still holds, for a people manager.
func OffboardingPreviewForUser(ctx context.Context, actorUserID, targetUserID int) (*storage.OffboardingSummary, error) {
	_ = ctx
	access, err := PeopleAccessFor(actorUserID)
	if err != nil {
		return nil, err
	}
	if !access.Any() {
		return nil, ErrForbidden
	}
	if err := canManageAccount(actorUserID, access, targetUserID); err != nil {
		return nil, err
	}
	return storage.GetOffboardingSummary(targetUserID)
}

// OffboardAccountForUser deactivates targetUserID and hands off their work.
// Claims the successor can't take on a project are released instead.
func OffboardAccountForUser(ctx context.Context, actorUserID, targetUserID int, in OffboardInput) (*OffboardResult, error) {
	switch in.Claims {
	case "", OffboardClaimsKeep, OffboardClaimsRelease:
	case OffboardClaimsReassign:
		if in.SuccessorID <= 0 {
			return nil, fmt.Errorf("%w: choose who takes over the claimed tasks", ErrValidation)
		}
	default:
		return nil, fmt.Errorf("%w: claims must be keep, release, or reassign", ErrValidation)
	}
	if in.TransferProjects && in.SuccessorID <= 0 {
		return nil, fmt.Errorf("%w: choose who takes over the projects", ErrValidation)
	}
	if in.SuccessorID > 0 {
		if in.SuccessorID == targetUserID {
			return nil, fmt.Errorf("%w: choose someone other than the person leaving", ErrValidation)
		}
		if storage.IsAgentUser(in.SuccessorID) || storage.IsUserBannedByID(in.SuccessorID) {
			return nil, fmt.Errorf("%w: the successor must be an active person", ErrValidation)
		}
	}
	summary, err := OffboardingPreviewForUser(ctx, actorUserID, targetUserID)
	if err != nil {
		return nil, err
	}
	// Deactivate first: it runs the remaining permission checks (last admin, etc.).
	if err := SetAccountActiveForUser(ctx, actorUserID, targetUserID, false); err != nil {
		return nil, err
	}

	res := &OffboardResult{}
	if in.TransferProjects {
		for _, pid := range summary.OwnedProjectIDs {
			if err := transferProjectTo(pid, in.SuccessorID, actorUserID); err != nil {
				return res, fmt.Errorf("transfer project %d: %w", pid, err)
			}
			_ = storage.LogProjectEvent(pid, actorUserID, "owner_transferred", map[string]interface{}{
				"from_user_id": targetUserID, "to_user_id": in.SuccessorID,
			})
			live.AfterProjectChange(actorUserID, pid, live.TypeProjectUpdated)
			res.ProjectsTransferred++
		}
	}
	if in.Claims == OffboardClaimsRelease || in.Claims == OffboardClaimsReassign {
		for _, taskID := range summary.ClaimedTaskIDs {
			var next *int
			if in.Claims == OffboardClaimsReassign && successorCanClaim(taskID, in.SuccessorID) {
				s := in.SuccessorID
				next = &s
			}
			if err := storage.SetTaskClaimedBy(taskID, next); err != nil {
				return res, err
			}
			meta := map[string]interface{}{"previous_claimed_by": targetUserID, "reason": "offboarding"}
			if next != nil {
				meta["claimed_by"] = *next
				_ = storage.LogTaskEvent(taskID, actorUserID, "claimed", meta)
				res.ClaimsReassigned++
			} else {
				_ = storage.LogTaskEvent(taskID, actorUserID, "unclaimed", meta)
				res.ClaimsReleased++
			}
			live.AfterTaskChangeLive(actorUserID, taskID, live.TypeTaskUpdated)
		}
	}
	if in.RevokeKeys {
		n, err := storage.RevokeAllAPIKeysForUser(targetUserID)
		if err != nil {
			return res, err
		}
		res.KeysRevoked = n
	}
	return res, nil
}

func successorCanClaim(taskID, userID int) bool {
	canRead, writeRole, projectID, err := storage.CanUserAccessTask(taskID, userID)
	if err != nil || !canRead || projectID <= 0 {
		return false
	}
	return denyMissingTaskPerm(projectID, writeRole, storage.PermTasksClaim) == nil
}

func transferProjectTo(projectID, successorID, actorID int) error {
	if storage.IsCompanyMode() {
		if err := storage.UpsertProjectAccessGrant(projectID, 0, successorID, storage.AccessFull, 0, actorID); err != nil {
			return err
		}
		return storage.RecomputeCompanyProjectAccess(projectID)
	}
	return storage.UpsertProjectMember(projectID, successorID, storage.RoleOwner)
}
