package utils

import (
	"fmt"
	"strings"
	"time"
)

func dueDateLocation(timezone string) *time.Location {
	loc, err := time.LoadLocation(timezone)
	if err != nil {
		return time.UTC
	}
	return loc
}

func parseDueDate(dueDate string, loc *time.Location) (time.Time, bool) {
	t, err := time.ParseInLocation("2006-01-02", dueDate, loc)
	if err != nil {
		return time.Time{}, false
	}
	return t, true
}

// DueDateInputValue normalizes a stored due date for HTML date inputs (YYYY-MM-DD).
func DueDateInputValue(dueDate interface{}) string {
	if dueDate == nil {
		return ""
	}
	raw, ok := dueDate.(string)
	if !ok {
		return ""
	}
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	if len(raw) >= 10 {
		if t, err := time.Parse("2006-01-02", raw[:10]); err == nil {
			return t.Format("2006-01-02")
		}
	}
	return raw
}

// DueDateClass returns a CSS class for due-date styling based on user timezone.
func DueDateClass(dueDate string, completed bool, timezone string) string {
	if completed || dueDate == "" {
		return ""
	}

	loc := dueDateLocation(timezone)
	today := time.Now().In(loc).Format("2006-01-02")
	if dueDate < today {
		return "due-overdue"
	}
	if dueDate == today {
		return "due-today"
	}
	return "due-upcoming"
}

// DueDateDisplay returns a human-friendly due label with the raw date available via title attribute.
func DueDateDisplay(dueDate string, completed bool, timezone string) string {
	if dueDate == "" {
		return ""
	}
	if completed {
		return dueDate
	}

	loc := dueDateLocation(timezone)
	due, ok := parseDueDate(dueDate, loc)
	if !ok {
		return dueDate
	}

	now := time.Now().In(loc)
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc)
	dueDay := time.Date(due.Year(), due.Month(), due.Day(), 0, 0, 0, 0, loc)
	diff := int(dueDay.Sub(today).Hours() / 24)

	switch {
	case diff < -1:
		return fmt.Sprintf("%d days ago", -diff)
	case diff == -1:
		return "Yesterday"
	case diff == 0:
		return "Today"
	case diff == 1:
		return "Tomorrow"
	case diff <= 7:
		return fmt.Sprintf("In %d days", diff)
	default:
		return dueDate
	}
}
