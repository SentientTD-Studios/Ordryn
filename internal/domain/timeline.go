package domain

import (
	"context"

	"GoTodo/internal/storage"
)

// ProjectTimeline is the data for a project's timeline (Gantt) view.
type ProjectTimeline struct {
	ProjectID int
	Tasks     []storage.TimelineTask
	Links     []storage.TimelineLink
	// Undated counts open tasks with neither a start nor a due date.
	Undated   int
	Truncated bool
}

// GetProjectTimelineForUser returns the dated tasks of a project the caller can see.
func GetProjectTimelineForUser(ctx context.Context, userID, projectID int) (*ProjectTimeline, error) {
	_ = ctx
	proj, err := storage.GetAccessibleProjectByID(projectID, userID)
	if err != nil || proj == nil {
		return nil, ErrNotFound
	}
	items, links, undated, truncated, err := storage.ListProjectTimeline(proj.ID)
	if err != nil {
		return nil, err
	}
	return &ProjectTimeline{
		ProjectID: proj.ID,
		Tasks:     items,
		Links:     links,
		Undated:   undated,
		Truncated: truncated,
	}, nil
}
