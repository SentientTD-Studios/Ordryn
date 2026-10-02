package handlers

import (
	"GoTodo/internal/server/utils"
	"GoTodo/internal/storage"
	"database/sql"
	"fmt"
	"net/http"
	"strconv"
	"strings"
)

func logTaskEvent(taskID, userID int, eventType string, metadata map[string]interface{}) {
	if err := storage.LogTaskEvent(taskID, userID, eventType, metadata); err != nil {
		fmt.Printf("audit: failed to log %s for task %d: %v\n", eventType, taskID, err)
	}
}

// logAdminEvent records a site-admin action attributed to the request's user.
func logAdminEvent(r *http.Request, eventType, targetType string, targetID int64, targetLabel string, metadata map[string]interface{}) {
	actorID, _ := utils.GetAPIUserID(r)
	if err := storage.LogAdminEvent(actorID, eventType, targetType, targetID, targetLabel, metadata); err != nil {
		fmt.Printf("audit: failed to log admin %s: %v\n", eventType, err)
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
	case "link_added", "link_removed":
		verb := "Link added"
		if eventType == "link_removed" {
			verb = "Link removed"
		}
		kind, _ := meta["kind"].(string)
		label := map[string]string{
			"blocks": "blocks", "blocked_by": "blocked by", "relates": "relates to",
			"duplicates": "duplicates", "duplicated_by": "duplicated by",
		}[kind]
		title, _ := meta["title"].(string)
		if label != "" && title != "" {
			return verb + " · " + label + " " + title
		}
		return verb
	case "recurrence_set":
		if summary, ok := meta["summary"].(string); ok && summary != "" {
			return "Repeats · " + summary
		}
		return "Repeat set"
	case "recurrence_cleared":
		if reason, ok := meta["reason"].(string); ok && reason == "nested" {
			return "Repeat removed · became a subtask"
		}
		return "Repeat removed"
	case "recurrence_next":
		if due, ok := meta["due_date"].(string); ok && due != "" {
			return "Next occurrence created · due " + due
		}
		return "Next occurrence created"
	case "recurrence_created":
		if from, ok := meta["from_id"]; ok && from != nil {
			return fmt.Sprintf("Repeated from #%v", from)
		}
		return "Repeated from a recurring task"
	case "recurrence_ended":
		return "Repeat series ended"
	case "recurrence_failed":
		return "Could not create next occurrence"
	case "recurrence_undone":
		return "Next occurrence removed · reopened"
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
	case "agent_run_queued":
		name := agentEventName(meta)
		switch meta["trigger"] {
		case "mention":
			return "Sent to " + name + " · @mention"
		case "status":
			return "Sent to " + name + " · column move"
		}
		return "Sent to " + name
	case "agent_run_finished":
		if s, ok := meta["status"].(string); ok && s == "failed" {
			return agentEventName(meta) + " could not finish"
		}
		return agentEventName(meta) + " finished"
	case "agent_run_cancelled":
		return "Cancelled " + agentEventName(meta) + "'s run"
	default:
		return eventType
	}
}

func agentEventName(meta map[string]interface{}) string {
	if name, ok := meta["agent_name"].(string); ok && name != "" {
		return name
	}
	return "AI agent"
}
