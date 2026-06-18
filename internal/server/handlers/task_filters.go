package handlers

import (
	"GoTodo/internal/server/utils"
	"GoTodo/internal/storage"
	"GoTodo/internal/tasks"
	"context"
	"net/http"
	"strconv"
	"strings"
)

func parseProjectFilter(projectParam string) *int {
	if projectParam == "" {
		return nil
	}
	if projectParam == "none" || projectParam == "0" {
		zero := 0
		return &zero
	}
	if pid, err := strconv.Atoi(projectParam); err == nil {
		return &pid
	}
	return nil
}

func normalizeStatusFilter(status string) string {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "complete", "completed":
		return "complete"
	case "incomplete":
		return "incomplete"
	default:
		return ""
	}
}

func requestStatusFilter(r *http.Request) string {
	if status := normalizeStatusFilter(r.URL.Query().Get("status")); status != "" {
		return status
	}
	return normalizeStatusFilter(r.FormValue("status"))
}

func taskStatusMatchesFilter(statusFilter string, completed bool) bool {
	switch normalizeStatusFilter(statusFilter) {
	case "complete":
		return completed
	case "incomplete":
		return !completed
	default:
		return true
	}
}

func fetchTasksForFilters(page, pageSize int, searchQuery string, userID *int, timezone string, projectFilter *int, statusFilter string) ([]tasks.Task, int, error) {
	if searchQuery != "" {
		return tasks.SearchTasksForUserWithFilters(page, pageSize, searchQuery, userID, timezone, projectFilter, statusFilter)
	}
	return tasks.ReturnPaginationForUserWithFilters(page, pageSize, userID, timezone, projectFilter, statusFilter)
}

func completedIncompleteCounts(userID *int, projectFilter *int) (int, int) {
	if userID == nil {
		return 0, 0
	}
	if projectFilter == nil {
		return utils.GetCompletedTasksCount(userID), utils.GetIncompleteTasksCount(userID)
	}

	pool, err := storage.OpenDatabase()
	if err != nil {
		return 0, 0
	}
	defer storage.CloseDatabase(pool)

	projectCond := ""
	args := []interface{}{*userID}
	if *projectFilter == 0 {
		projectCond = " AND project_id IS NULL"
	} else {
		projectCond = " AND project_id = $2"
		args = append(args, *projectFilter)
	}

	completedCount := 0
	incompleteCount := 0
	if err := pool.QueryRow(context.Background(), "SELECT COUNT(*) FROM tasks WHERE user_id = $1 AND completed = true"+projectCond, args...).Scan(&completedCount); err != nil {
		completedCount = 0
	}
	if err := pool.QueryRow(context.Background(), "SELECT COUNT(*) FROM tasks WHERE user_id = $1 AND (completed IS NULL OR completed = false)"+projectCond, args...).Scan(&incompleteCount); err != nil {
		incompleteCount = 0
	}

	return completedCount, incompleteCount
}

func renderFilteredTaskListPartial(w http.ResponseWriter, r *http.Request, page, pageSize int, searchQuery string, userID *int, timezone string, loggedIn bool, projectParam string, statusFilter string) error {
	projectFilter := parseProjectFilter(projectParam)
	taskList, totalTasks, err := fetchTasksForFilters(page, pageSize, searchQuery, userID, timezone, projectFilter, statusFilter)
	if err != nil {
		return err
	}

	lastPage := (totalTasks + pageSize - 1) / pageSize
	if lastPage < 1 {
		lastPage = 1
	}
	if page > lastPage {
		page = lastPage
	}
	if page < 1 {
		page = 1
	}

	if page > 0 && totalTasks > 0 {
		refetched, refetchedTotal, err := fetchTasksForFilters(page, pageSize, searchQuery, userID, timezone, projectFilter, statusFilter)
		if err != nil {
			return err
		}
		taskList = refetched
		totalTasks = refetchedTotal
	}

	if searchQuery != "" {
		for i := range taskList {
			taskList[i].Title = highlightMatches(taskList[i].Title, searchQuery)
			taskList[i].Description = highlightMatches(taskList[i].Description, searchQuery)
		}
	}

	favs := make([]tasks.Task, 0)
	nonFavs := make([]tasks.Task, 0)
	for i := range taskList {
		taskList[i].Page = page
		if taskList[i].IsFavorite {
			favs = append(favs, taskList[i])
		} else {
			nonFavs = append(nonFavs, taskList[i])
		}
	}

	uid := 0
	if userID != nil {
		uid = *userID
	}
	pagination := utils.GetPaginationData(page, pageSize, totalTasks, uid)
	completedCount, incompleteCount := completedIncompleteCounts(userID, projectFilter)

	projectsList := make([]map[string]interface{}, 0)
	if userID != nil {
		if projs, perr := storage.GetProjectsForUser(*userID); perr == nil {
			for _, p := range projs {
				sel := projectFilter != nil && *projectFilter == p.ID
				projectsList = append(projectsList, map[string]interface{}{"ID": p.ID, "Name": p.Name, "Selected": sel})
			}
		}
	}

	context := map[string]interface{}{
		"FavoriteTasks":    favs,
		"Tasks":            nonFavs,
		"PreviousPage":     pagination.PreviousPage,
		"NextPage":         pagination.NextPage,
		"CurrentPage":      pagination.CurrentPage,
		"PrevDisabled":     pagination.PrevDisabled,
		"NextDisabled":     pagination.NextDisabled,
		"SearchQuery":      searchQuery,
		"TotalTasks":       totalTasks,
		"LoggedIn":         loggedIn,
		"Timezone":         timezone,
		"TotalPages":       pagination.TotalPages,
		"Pages":            pagination.Pages,
		"HasRightEllipsis": pagination.HasRightEllipsis,
		"PerPage":          pageSize,
		"CompletedTasks":   completedCount,
		"IncompleteTasks":  incompleteCount,
		"Projects":         projectsList,
		"ProjectFilter":    projectParam,
		"StatusFilter":     statusFilter,
		"IsSearching":      searchQuery != "",
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	return utils.RenderTemplate(w, r, "pagination.html", context)
}
