package domain

import (
	"context"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"GoTodo/internal/live"
	"GoTodo/internal/storage"
)

// MaxStatusUpdateLen caps a status update's text.
const MaxStatusUpdateLen = 4000

// PortfolioRow is one project on the portfolio.
type PortfolioRow struct {
	Project storage.ProjectWithAccess
	Counts  storage.PortfolioCounts
	Latest  *storage.ProjectStatusUpdate
	// CanPost is true when the viewer manages the project and may post updates.
	CanPost bool
}

// ListPortfolioForUser returns every project the user can open, with task counts
// and the latest status update. tz is the viewer's time zone.
func ListPortfolioForUser(ctx context.Context, userID int, tz string, includeArchived bool) ([]PortfolioRow, error) {
	_ = ctx
	projects, err := storage.GetAccessibleProjects(userID)
	if err != nil {
		return nil, err
	}
	loc, err := time.LoadLocation(tz)
	if err != nil {
		loc = time.UTC
	}
	today := time.Now().In(loc).Format("2006-01-02")

	kept := make([]storage.ProjectWithAccess, 0, len(projects))
	ids := make([]int, 0, len(projects))
	for _, p := range projects {
		if p.Archived && !includeArchived {
			continue
		}
		kept = append(kept, p)
		ids = append(ids, p.ID)
	}
	counts, err := storage.GetPortfolioCounts(ids, today)
	if err != nil {
		return nil, err
	}
	latest, err := storage.LatestProjectStatusUpdates(ids)
	if err != nil {
		return nil, err
	}
	out := make([]PortfolioRow, 0, len(kept))
	for _, p := range kept {
		row := PortfolioRow{Project: p, Counts: counts[p.ID], CanPost: !p.Archived && storage.RoleCanManageProject(p.ID, p.Role)}
		if u, ok := latest[p.ID]; ok {
			row.Latest = &u
		}
		out = append(out, row)
	}
	return out, nil
}

// ListProjectStatusUpdatesForUser returns a project's updates to anyone who can open it.
func ListProjectStatusUpdatesForUser(ctx context.Context, userID, projectID, limit int) ([]storage.ProjectStatusUpdate, error) {
	_ = ctx
	if _, err := storage.GetAccessibleProjectByID(projectID, userID); err != nil {
		return nil, ErrNotFound
	}
	return storage.ListProjectStatusUpdates(projectID, limit)
}

// PostProjectStatusUpdate records how a project is going. Project managers only.
func PostProjectStatusUpdate(ctx context.Context, userID, projectID int, health, body string) (*storage.ProjectStatusUpdate, error) {
	_ = ctx
	proj, err := storage.GetAccessibleProjectByID(projectID, userID)
	if err != nil {
		return nil, ErrNotFound
	}
	if !storage.RoleCanManageProject(proj.ID, proj.Role) {
		return nil, fmt.Errorf("%w: only project managers can post status updates", ErrForbidden)
	}
	if proj.Archived {
		return nil, fmt.Errorf("%w: restore the project before posting updates", ErrValidation)
	}
	health = strings.TrimSpace(health)
	if !storage.ValidHealth(health) {
		return nil, fmt.Errorf("%w: health must be on_track, at_risk, or off_track", ErrValidation)
	}
	body = strings.TrimSpace(body)
	if utf8.RuneCountInString(body) > MaxStatusUpdateLen {
		return nil, fmt.Errorf("%w: the update is too long (%d characters at most)", ErrValidation, MaxStatusUpdateLen)
	}
	u, err := storage.CreateProjectStatusUpdate(projectID, userID, health, body)
	if err != nil {
		return nil, err
	}
	_ = storage.LogProjectEvent(projectID, userID, "status_update_posted", map[string]interface{}{"health": health})
	live.AfterProjectChange(userID, projectID, live.TypeProjectUpdated)
	return u, nil
}

// DeleteProjectStatusUpdateForUser removes an update. Project managers only.
func DeleteProjectStatusUpdateForUser(ctx context.Context, userID, projectID, updateID int) error {
	_ = ctx
	proj, err := storage.GetAccessibleProjectByID(projectID, userID)
	if err != nil {
		return ErrNotFound
	}
	if !storage.RoleCanManageProject(proj.ID, proj.Role) {
		return fmt.Errorf("%w: only project managers can delete status updates", ErrForbidden)
	}
	ok, err := storage.DeleteProjectStatusUpdate(projectID, updateID)
	if err != nil {
		return err
	}
	if !ok {
		return ErrNotFound
	}
	live.AfterProjectChange(userID, projectID, live.TypeProjectUpdated)
	return nil
}
