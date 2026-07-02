package handlers

import (
	"GoTodo/internal/server/utils"
	"GoTodo/internal/storage"
	"GoTodo/internal/tasks"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"
)

// APITaskEvents returns an HTML partial timeline for a task.
func APITaskEvents(w http.ResponseWriter, r *http.Request) {
	idStr := r.URL.Query().Get("id")
	if idStr == "" {
		http.Error(w, "Task ID required", http.StatusBadRequest)
		return
	}
	taskID, err := strconv.Atoi(idStr)
	if err != nil {
		http.Error(w, "Invalid task ID", http.StatusBadRequest)
		return
	}

	_, _, _, timezone, loggedIn, _ := utils.GetSessionUserWithTimezone(r)
	if !loggedIn {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	userID := utils.GetSessionUserID(r)
	if userID == nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	events, err := storage.GetEventsForTask(taskID, *userID, 50)
	if err != nil {
		http.Error(w, "Error loading activity", http.StatusInternalServerError)
		return
	}

	loc, err := time.LoadLocation(timezone)
	if err != nil {
		loc = time.UTC
	}

	type timelineEntry struct {
		Label   string
		TimeStr string
	}

	entries := make([]timelineEntry, 0, len(events))
	for _, ev := range events {
		entries = append(entries, timelineEntry{
			Label:   formatEventLabel(ev.EventType, ev.Metadata),
			TimeStr: ev.CreatedAt.In(loc).Format("Jan 2, 2006 3:04 PM"),
		})
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := utils.Templates.ExecuteTemplate(w, "task_timeline.html", map[string]interface{}{
		"Events": entries,
	}); err != nil {
		http.Error(w, "Error rendering timeline", http.StatusInternalServerError)
	}
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
			return "Edited · " + joinStrings(parts, ", ")
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
	default:
		return eventType
	}
}

func joinStrings(parts []string, sep string) string {
	if len(parts) == 0 {
		return ""
	}
	out := parts[0]
	for i := 1; i < len(parts); i++ {
		out += sep + parts[i]
	}
	return out
}

// DashboardPageHandler renders the insights dashboard.
func DashboardPageHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Invalid request method", http.StatusMethodNotAllowed)
		return
	}

	email, _, permissions, timezone, loggedIn, _ := utils.GetSessionUserWithTimezone(r)
	if !loggedIn {
		http.Redirect(w, r, utils.GetBasePath()+"/", http.StatusSeeOther)
		return
	}

	userID := utils.GetSessionUserID(r)
	if userID == nil {
		http.Redirect(w, r, utils.GetBasePath()+"/", http.StatusSeeOther)
		return
	}

	stats, err := tasks.GetDashboardStats(*userID, timezone)
	if err != nil {
		http.Error(w, "Error loading dashboard", http.StatusInternalServerError)
		return
	}

	projectLabels, _ := json.Marshal(projectNames(stats.ByProject))
	projectCounts, _ := json.Marshal(projectCountsOnly(stats.ByProject))
	chartLabels, _ := json.Marshal(dayLabels(stats.CompletionsLast7Days))
	chartData, _ := json.Marshal(dayCountsOnly(stats.CompletionsLast7Days))

	ctx := map[string]interface{}{
		"LoggedIn":           true,
		"UserEmail":          email,
		"Permissions":        permissions,
		"Title":              "Dashboard",
		"Stats":              stats,
		"ProjectChartLabels": string(projectLabels),
		"ProjectChartData":   string(projectCounts),
		"CompletionLabels":   string(chartLabels),
		"CompletionData":     string(chartData),
	}
	utils.RenderTemplate(w, r, "dashboard.html", ctx)
}

func projectNames(items []tasks.NameCount) []string {
	out := make([]string, len(items))
	for i, it := range items {
		out[i] = it.Name
	}
	return out
}

func projectCountsOnly(items []tasks.NameCount) []int {
	out := make([]int, len(items))
	for i, it := range items {
		out[i] = it.Count
	}
	return out
}

func dayLabels(items []tasks.DayCount) []string {
	out := make([]string, len(items))
	for i, it := range items {
		out[i] = it.Date
	}
	return out
}

func dayCountsOnly(items []tasks.DayCount) []int {
	out := make([]int, len(items))
	for i, it := range items {
		out[i] = it.Count
	}
	return out
}

// APIGetUsers returns registered users as an HTML table fragment (admin only).
func APIGetUsers(w http.ResponseWriter, r *http.Request) {
	users, err := storage.ListUsers()
	if err != nil {
		http.Error(w, "Error loading users", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := utils.Templates.ExecuteTemplate(w, "users_table.html", map[string]interface{}{
		"Users": users,
	}); err != nil {
		http.Error(w, "Error rendering users", http.StatusInternalServerError)
	}
}
