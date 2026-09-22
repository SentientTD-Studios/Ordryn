package handlers

import (
	"GoTodo/internal/storage"
	"database/sql"
	"fmt"
	"strconv"
	"strings"
)

func logTaskEvent(taskID, userID int, eventType string, metadata map[string]interface{}) {
	if err := storage.LogTaskEvent(taskID, userID, eventType, metadata); err != nil {
		fmt.Printf("audit: failed to log %s for task %d: %v\n", eventType, taskID, err)
	}
}

func priorityLabel(p int) string {
	switch p {
	case 1:
		return "Low"
	case 2:
		return "Medium"
	case 3:
		return "High"
	default:
		return "None"
	}
}

func projectIDFromNull(v sql.NullInt64) int {
	if v.Valid {
		return int(v.Int64)
	}
	return 0
}

func projectIDFromForm(projectIDStr string) int {
	projectIDStr = strings.TrimSpace(projectIDStr)
	if projectIDStr == "" {
		return 0
	}
	pid, err := strconv.Atoi(projectIDStr)
	if err != nil {
		return 0
	}
	return pid
}

func projectDisplayName(userID, projectID int) string {
	if projectID == 0 {
		return "No project"
	}
	if p, err := storage.GetAccessibleProjectByID(projectID, userID); err == nil {
		return p.Name
	}
	return "Project"
}

func formatEventLabel(eventType string, meta map[string]interface{}) string {
	switch eventType {
	case "created":
		return "Created"
	case "edited":
		if fields, ok := meta["fields"].([]interface{}); ok && len(fields) > 0 {
			parts := make([]string, 0, len(fields))
			for _, f := range fields {
				parts = append(parts, fmt.Sprintf("%v", f))
			}
			return "Edited · " + strings.Join(parts, ", ")
		}
		return "Edited"
	case "completed":
		return "Completed"
	case "reopened":
		return "Reopened"
	case "deleted":
		return "Deleted"
	case "moved_project":
		if name, ok := meta["project"].(string); ok && name != "" {
			return "Moved to · " + name
		}
		return "Moved project"
	case "tag_added":
		if name, ok := meta["tag"].(string); ok {
			return "Tag added · " + name
		}
		return "Tag added"
	case "tag_removed":
		if name, ok := meta["tag"].(string); ok {
			return "Tag removed · " + name
		}
		return "Tag removed"
	case "reordered":
		return "Reordered"
	case "priority_changed":
		if to, ok := meta["to"].(string); ok {
			return "Priority · " + to
		}
		return "Priority changed"
	case "title_changed":
		if to, ok := meta["to"].(string); ok && to != "" {
			return "Title · " + to
		}
		return "Title changed"
	case "estimate_changed":
		if to, ok := meta["to"]; ok && to != nil {
			return fmt.Sprintf("Estimate · %v", to)
		}
		return "Estimate cleared"
	case "due_date_changed":
		if to, ok := meta["to"].(string); ok && to != "" {
			return "Due date · " + to
		}
		return "Due date cleared"
	case "parent_changed":
		if to, ok := meta["to"].(string); ok && to != "" {
			return "Parent · " + to
		}
		return "Parent cleared"
	case "claimed":
		return "Claimed"
	case "unclaimed":
		return "Unclaimed"
	case "status_changed":
		if name, ok := meta["to"].(string); ok && name != "" {
			return "Status changed · " + name
		}
		return "Status changed"
	case "sprint_changed":
		if name, ok := meta["to"].(string); ok && name != "" {
			return "Sprint · " + name
		}
		return "Sprint changed"
	case "github_issue_created":
		if n, ok := meta["issue_number"]; ok {
			return fmt.Sprintf("GitHub issue created · #%v", n)
		}
		return "GitHub issue created"
	case "github_issue_linked":
		if n, ok := meta["issue_number"]; ok {
			return fmt.Sprintf("GitHub issue linked · #%v", n)
		}
		return "GitHub issue linked"
	case "github_issue_unlinked":
		if n, ok := meta["issue_number"]; ok {
			return fmt.Sprintf("GitHub issue unlinked · #%v", n)
		}
		return "GitHub issue unlinked"
	case "github_issue_synced":
		if state, ok := meta["issue_state"].(string); ok && state != "" {
			return "GitHub issue synced · " + state
		}
		return "GitHub issue synced"
	default:
		return eventType
	}
}
