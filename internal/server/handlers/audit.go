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

func logTagChanges(taskID, userID int, before, after []storage.Tag) {
	beforeMap := make(map[int]string, len(before))
	for _, t := range before {
		beforeMap[t.ID] = t.Name
	}
	afterMap := make(map[int]string, len(after))
	for _, t := range after {
		afterMap[t.ID] = t.Name
	}
	for id, name := range afterMap {
		if _, ok := beforeMap[id]; !ok {
			logTaskEvent(taskID, userID, "tag_added", map[string]interface{}{"tag": name, "tag_id": id})
		}
	}
	for id, name := range beforeMap {
		if _, ok := afterMap[id]; !ok {
			logTaskEvent(taskID, userID, "tag_removed", map[string]interface{}{"tag": name, "tag_id": id})
		}
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
	if p, err := storage.GetProjectByID(projectID, userID); err == nil {
		return p.Name
	}
	return "Project"
}
