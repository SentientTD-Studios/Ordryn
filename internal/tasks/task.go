package tasks

import (
	"fmt"
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
	EstimatePoints      *int
	TimeSpentMinutes    int
	ProjectWorkflow     string
	ClaimedBy           int
	ClaimedByName       string
	SprintID            int
	SprintName          string
	GitHubIssueNumber   int
	GitHubIssueID       int64
	GitHubIssueURL      string
	GitHubIssueState    string
	GitHubIssueTitle    string
	GitHubLastSyncError string
	Fields              []AttachedField
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
