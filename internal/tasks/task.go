package tasks

import (
	"fmt"

	"GoTodo/internal/storage"
)

// Tag is a label attached to tasks.
type Tag struct {
	ID        int
	Name      string
	Color     string
	ProjectID *int
	Protected bool
}

type Task struct {
	ID                  int
	Title               string
	Description         string
	Completed           bool
	DateAdded           string // time_stamp formatted for display
	DueDate             string // Due date (YYYY-MM-DD format)
	StartDate           string // Planned start (YYYY-MM-DD) or ""
	DateCreated         string // time_stamp formatted for tooltip
	DateModified        string // date_modified formatted for tooltip
	Page                int
	IsFavorite          bool
	Position            int
	ProjectID           int
	ProjectName         string
	Priority            int
	Tags                []Tag
	ParentID            int // 0 = root
	ParentTitle         string
	ChildCount          int
	ChildrenCompleted   int
	Children            []Task
	StatusID            int
	StatusName          string
	EstimateHours       *float64
	Refinement          string
	TimeSpentMinutes    int
	ProjectWorkflow     string
	ClaimedBy           int
	ClaimedByName       string
	ReviewerID          int
	ReviewerName        string
	SprintID            int
	SprintName          string
	GitHubIssueNumber   int
	GitHubIssueID       int64
	GitHubIssueURL      string
	GitHubIssueState    string
	GitHubIssueTitle    string
	GitHubLastSyncError string
	Fields              []AttachedField
	// Recurrence is the repeat rule when this task is the active occurrence of a series.
	Recurrence *storage.TaskRecurrence
	// BlockedByOpen counts incomplete tasks that block this one.
	BlockedByOpen int
	// CreatedByID, CreatedByName and CompletedDate are only filled by query runs.
	CreatedByID   int
	CreatedByName string
	CompletedDate string
	// StatusChangedDate (YYYY-MM-DD) and DaysInStatus say when the task entered
	// its workflow status; empty/nil without one. Query runs only.
	StatusChangedDate string
	DaysInStatus      *int
}

// AttachedField is a custom field value loaded for API serialization.
type AttachedField struct {
	Key    string
	Value  any
	ShowOn []string
}

func (t *Task) Validate() error {
	if t.Title == "" {
		return fmt.Errorf("title cannot be empty")
	}

	return nil
}
