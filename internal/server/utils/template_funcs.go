package utils

import "time"

// DueDateClass returns a CSS class for due-date styling based on user timezone.
func DueDateClass(dueDate string, completed bool, timezone string) string {
	if completed || dueDate == "" {
		return ""
	}

	loc, err := time.LoadLocation(timezone)
	if err != nil {
		loc = time.UTC
	}

	today := time.Now().In(loc).Format("2006-01-02")
	if dueDate < today {
		return "due-overdue"
	}
	if dueDate == today {
		return "due-today"
	}
	return "due-upcoming"
}
