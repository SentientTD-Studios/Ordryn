package tasks

import (
	"fmt"
)

type Task struct {
	ID           int
	Title        string
	Description  string
	Completed    bool
	DateAdded    string // time_stamp formatted for display
	DueDate      string // Due date (YYYY-MM-DD format)
	DateCreated  string // time_stamp formatted for tooltip
	DateModified string // date_modified formatted for tooltip
	Page         int
	IsFavorite   bool
	Position     int
	ProjectID    int
	ProjectName  string
	Priority     int
}

func (t *Task) Validate() error {
	if t.Title == "" {
		return fmt.Errorf("title cannot be empty")
	}

	return nil
}
