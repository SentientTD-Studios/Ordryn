package domain

import (
	"fmt"
	"strings"
	"time"

	"GoTodo/internal/storage"
)

// normalizeStartDate trims and checks a YYYY-MM-DD start date ("" = none).
func normalizeStartDate(raw string) (string, error) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return "", nil
	}
	if _, err := time.Parse("2006-01-02", s); err != nil {
		return "", fmt.Errorf("%w: start_date must be YYYY-MM-DD", ErrValidation)
	}
	return s, nil
}

// checkStartBeforeDue rejects a start date after the due date. Either may be "".
// Dates are YYYY-MM-DD, so string order is date order.
func checkStartBeforeDue(start, due string) error {
	due = strings.TrimSpace(due)
	if len(due) > 10 {
		due = due[:10]
	}
	if start != "" && due != "" && start > due {
		return fmt.Errorf("%w: start date must be on or before the due date", ErrValidation)
	}
	return nil
}

func logStartDateChange(taskID, userID int, from, to string) {
	if from == to {
		return
	}
	_ = storage.LogTaskEvent(taskID, userID, "start_date_changed", dueDateChangeMetadata(from, to))
}
