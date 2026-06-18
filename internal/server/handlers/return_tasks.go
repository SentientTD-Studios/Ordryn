package handlers

import (
	"GoTodo/internal/server/utils"
	"GoTodo/internal/sessionstore"
	"GoTodo/internal/storage"
	"GoTodo/internal/tasks"
	"net/http"
	"strconv"
)

func APIReturnTasks(w http.ResponseWriter, r *http.Request) {
	// Determine page size from session (set on login/profile). Do NOT accept per_page query param.
	pageSize := utils.AppConstants.PageSize
	if sess, err := sessionstore.Store.Get(r, "session"); err == nil && sess != nil {
		if val, ok := sess.Values["items_per_page"]; ok {
			switch tv := val.(type) {
			case int:
				if tv > 0 {
					pageSize = tv
				}
			case int64:
				if int(tv) > 0 {
					pageSize = int(tv)
				}
			case float64:
				if int(tv) > 0 {
					pageSize = int(tv)
				}
			case string:
				if v, err := strconv.Atoi(tv); err == nil && v > 0 {
					pageSize = v
				}
			}
		}
	}

	searchQuery := r.URL.Query().Get("search")
	// Optional project filter: empty = all, "0" or "none" = no project, numeric id = specific project
	projectParam := r.URL.Query().Get("project")
	projectFilter := parseProjectFilter(projectParam)
	statusFilter := requestStatusFilter(r)

	// Parse "page" query parameter
	var currentPage int
	if pageParam := r.URL.Query().Get("page"); pageParam != "" {
		var err error
		currentPage, err = strconv.Atoi(pageParam)
		if err != nil || currentPage < 1 {
			currentPage = 1
		}
	} else {
		currentPage = 1
	}
	page := currentPage

	// Get user ID if logged in
	email, _, _, timezone, loggedIn, _ := utils.GetSessionUserWithTimezone(r)
	var userID *int
	if loggedIn {
		userID = getUserIDFromEmail(email)
	}

	// Fetch tasks for the current page
	var taskList []tasks.Task
	var totalTasks int
	var err error

	if searchQuery != "" {
		taskList, totalTasks, err = tasks.SearchTasksForUserWithFilters(page, pageSize, searchQuery, userID, timezone, projectFilter, statusFilter)
		if err != nil {
			http.Error(w, "Error fetching tasks: "+err.Error(), http.StatusInternalServerError)
			return
		}

		// Highlight search matches
		for i, task := range taskList {
			taskList[i].Title = highlightMatches(task.Title, searchQuery)
			taskList[i].Description = highlightMatches(task.Description, searchQuery)
		}
	} else {
		taskList, totalTasks, err = tasks.ReturnPaginationForUserWithFilters(page, pageSize, userID, timezone, projectFilter, statusFilter)
		if err != nil {
			http.Error(w, "Error fetching tasks: "+err.Error(), http.StatusInternalServerError)
			return
		}
	}

	// Validate and clamp page number to valid range
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

	// If page was adjusted, we need to refetch with the correct page
	if page != currentPage {
		if searchQuery != "" {
			taskList, totalTasks, err = tasks.SearchTasksForUserWithFilters(page, pageSize, searchQuery, userID, timezone, projectFilter, statusFilter)
			if err != nil {
				http.Error(w, "Error fetching tasks: "+err.Error(), http.StatusInternalServerError)
				return
			}
			// Highlight search matches
			for i, task := range taskList {
				taskList[i].Title = highlightMatches(task.Title, searchQuery)
				taskList[i].Description = highlightMatches(task.Description, searchQuery)
			}
		} else {
			taskList, totalTasks, err = tasks.ReturnPaginationForUserWithFilters(page, pageSize, userID, timezone, projectFilter, statusFilter)
			if err != nil {
				http.Error(w, "Error fetching tasks: "+err.Error(), http.StatusInternalServerError)
				return
			}
		}
	}

	// Split into favorites and non-favorites for separate sortable lists
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

	// Avoid dereferencing nil userID; use 0 for anonymous users
	uid := 0
	if userID != nil {
		uid = *userID
	}
	pagination := utils.GetPaginationData(page, pageSize, totalTasks, uid)

	// Set response header
	w.Header().Set("Content-Type", "text/html; charset=utf-8")

	// Compute completed/incomplete counts — respect project filter when present
	completedCount, incompleteCount := completedIncompleteCounts(userID, projectFilter)

	// Create a context for the tasks and pagination
	// Fetch projects for this user so the sidebar form can render the select
	projectsList := make([]map[string]interface{}, 0)
	if userID != nil {
		if projs, perr := storage.GetProjectsForUser(*userID); perr == nil {
			for _, p := range projs {
				sel := false
				if projectFilter != nil {
					switch *projectFilter {
					case 0:
						// nothing selected (no project)
					case p.ID:
						sel = true
					}
				}
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
		"ProjectFilter":    projectParam,
		"StatusFilter":     statusFilter,
		"Projects":         projectsList,
	}

	if err := utils.RenderTemplate(w, r, "pagination.html", context); err != nil {
		http.Error(w, "Error rendering template: "+err.Error(), http.StatusInternalServerError)
	}
}
