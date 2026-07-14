package handlers

import (
	"GoTodo/internal/server/utils"
	"GoTodo/internal/sessionstore"
	"GoTodo/internal/storage"
	"GoTodo/internal/tasks"
	"context"
	"net/http"
	"regexp"
	"strconv"
	"strings"
)

func getUserIDFromEmail(email string) *int {
	// First try to read user_id from the session (avoid extra DB lookup)
	// Note: we don't have *http.Request here, so callers may prefer using
	// utils.GetSessionUserID directly. This function remains for backward
	// compatibility and will perform a DB lookup by email if needed.
	pool, err := storage.OpenDatabase()
	if err != nil {
		return nil
	}
	defer storage.CloseDatabase(pool)

	var userID int
	err = pool.QueryRow(context.Background(), "SELECT id FROM users WHERE email = $1", email).Scan(&userID)
	if err != nil {
		return nil
	}

	return &userID
}

func HomeHandler(w http.ResponseWriter, r *http.Request) {
	if project := strings.TrimSpace(r.URL.Query().Get("project")); project != "" && parseProjectFilter(project) != nil {
		target := projectFilterPageURL(utils.GetBasePath(), project, r.URL.Query())
		http.Redirect(w, r, target, http.StatusMovedPermanently)
		return
	}
	renderHome(w, r, filterContextFromRequest(r))
}

// ProjectFilterHandler serves the home task list filtered by project via /p/{id}.
func ProjectFilterHandler(w http.ResponseWriter, r *http.Request) {
	project := parseProjectFromPath(r.URL.Path)
	if project == "" {
		http.Redirect(w, r, homeURLWithQuery(utils.GetBasePath(), r.URL.Query()), http.StatusSeeOther)
		return
	}
	fc := filterContextFromRequest(r)
	if project == "none" {
		fc.Project = "0"
	} else {
		fc.Project = project
	}
	renderHome(w, r, fc)
}

func renderHome(w http.ResponseWriter, r *http.Request, fc FilterContext) {
	page := 1
	// determine page size from session if present
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
	if fc.Search == "" {
		fc.Search = searchQuery
	}
	if fc.Page == 0 {
		fc.Page = page
	}
	projectFilter := parseProjectFilter(fc.Project)

	loggedOut := r.URL.Query().Get("logged_out") == "true"
	accountCreated := r.URL.Query().Get("account_created") == "true"

	email, _, permissions, timezone, loggedIn, _ := utils.GetSessionUserWithTimezone(r)

	var taskList []tasks.Task
	var totalTasks int
	var err error
	var userID *int

	isSearching := false

	// Get user ID if logged in (prefer session-stored ID)
	if loggedIn {
		if uid := utils.GetSessionUserID(r); uid != nil {
			userID = uid
		} else {
			userID = getUserIDFromEmail(email)
		}
	}

	taskList, totalTasks, err = fetchTasksForFilters(page, pageSize, fc, userID, timezone)

	if err != nil {
		if w.Header().Get("Content-Type") == "" {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
		}
		http.Error(w, "Error fetching tasks: "+err.Error(), http.StatusInternalServerError)
		return
	}

	if searchQuery != "" {
		isSearching = true
		for i, task := range taskList {
			taskList[i].Title = highlightMatches(task.Title, searchQuery)
			taskList[i].Description = highlightMatches(task.Description, searchQuery)
		}
	}

	// Split into favorite and non-favorite lists and set page number
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

	// Compute completed/incomplete counts (may be scoped to project below)
	completedCount, incompleteCount := completedIncompleteCounts(userID, projectFilter)

	// Check for password reset success parameter
	passwordResetSuccess := r.URL.Query().Get("password_reset") == "success"

	// Create a context for the tasks and pagination
	tplContext := map[string]interface{}{
		"FavoriteTasks":        favs,
		"Tasks":                nonFavs,
		"CurrentPage":          page,
		"PreviousPage":         pagination.PreviousPage,
		"NextPage":             pagination.NextPage,
		"PrevDisabled":         pagination.PrevDisabled,
		"NextDisabled":         pagination.NextDisabled,
		"Pages":                pagination.Pages,
		"HasRightEllipsis":     pagination.HasRightEllipsis,
		"PerPage":              pageSize,
		"LoggedIn":             loggedIn,
		"UserEmail":            email,
		"Permissions":          permissions,
		"LoggedOut":            loggedOut,
		"AccountCreated":       accountCreated,
		"TotalTasks":           totalTasks,
		"TotalPages":           pagination.TotalPages,
		"IsSearching":          isSearching,
		"Title":                "GoTodo - Home",
		"CompletedTasks":       completedCount,
		"IncompleteTasks":      incompleteCount,
		"PasswordResetSuccess": passwordResetSuccess,
		"Timezone":             timezone,
	}

	// Include user's projects for the sidebar project select and mark selected project
	if loggedIn && userID != nil {
		if projs, err := storage.GetProjectsForUser(*userID); err == nil {
			projList := make([]map[string]interface{}, 0)
			for _, p := range projs {
				sel := false
				if projectFilter != nil {
					if *projectFilter == p.ID {
						sel = true
					}
				}
				projList = append(projList, map[string]interface{}{"ID": p.ID, "Name": p.Name, "Selected": sel})
			}
			tplContext["Projects"] = projList
		}
		tplContext["Tags"] = tagsListForFilter(*userID, fc.Tag)
	}

	for k, v := range fc.TemplateFields() {
		tplContext[k] = v
	}

	// Expose the active project filter to the template so the toolbar select can reflect it
	tplContext["ProjectFilter"] = fc.Project

	// Render the tasks and pagination controls
	if err := utils.RenderTemplate(w, r, "index.html", tplContext); err != nil {
		if w.Header().Get("Content-Type") == "" {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
		}
		http.Error(w, "Error rendering template: "+err.Error(), http.StatusInternalServerError)
		return
	}
}

func SearchHandler(w http.ResponseWriter, r *http.Request) {
	// determine page size from session if present
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

	var page int
	var userID *int
	var taskList []tasks.Task
	var totalTasks int
	var err error

	isSearching := false

	if pageParam := r.URL.Query().Get("page"); pageParam != "" {
		var err error
		page, err = strconv.Atoi(pageParam)
		if err != nil || page < 1 {
			page = 1
		}
	} else {
		page = 1
	}

	loggedOut := r.URL.Query().Get("logged_out") == "true"

	email, _, permissions, timezone, loggedIn, _ := utils.GetSessionUserWithTimezone(r)

	searchQuery := r.FormValue("search")
	fc := filterContextFromRequest(r)
	if fc.Search == "" {
		fc.Search = searchQuery
	}
	projectFilter := parseProjectFilter(fc.Project)

	if loggedIn {
		if uid := utils.GetSessionUserID(r); uid != nil {
			userID = uid
		} else {
			userID = getUserIDFromEmail(email)
		}
	}

	if searchQuery != "" {
		isSearching = true
	}
	taskList, totalTasks, err = fetchTasksForFilters(page, pageSize, fc, userID, timezone)

	if err != nil {
		if w.Header().Get("Content-Type") == "" {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
		}
		http.Error(w, "Error fetching tasks: "+err.Error(), http.StatusInternalServerError)
		return
	}

	if searchQuery != "" {
		for i, task := range taskList {
			taskList[i].Title = highlightMatches(task.Title, searchQuery)
			taskList[i].Description = highlightMatches(task.Description, searchQuery)
		}
	}

	// Set the page number for each task and split into favorites/non-favorites
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

	completedCount, incompleteCount := completedIncompleteCounts(userID, projectFilter)
	projectsList := make([]map[string]interface{}, 0)
	if loggedIn && userID != nil {
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
		"TotalResults":     totalTasks,
		"CurrentPage":      page,
		"PreviousPage":     pagination.PreviousPage,
		"NextPage":         pagination.NextPage,
		"PrevDisabled":     pagination.PrevDisabled,
		"NextDisabled":     pagination.NextDisabled,
		"TotalPages":       pagination.TotalPages,
		"Pages":            pagination.Pages,
		"HasRightEllipsis": pagination.HasRightEllipsis,
		"LoggedIn":         loggedIn,
		"UserEmail":        email,
		"Permissions":      permissions,
		"LoggedOut":        loggedOut,
		"IsSearching":      isSearching,
		"TotalTasks":       totalTasks,
		"CompletedTasks":   completedCount,
		"IncompleteTasks":  incompleteCount,
		"Projects":         projectsList,
		"Timezone":         timezone,
	}
	for k, v := range fc.TemplateFields() {
		context[k] = v
	}

	if err := utils.RenderTemplate(w, r, "pagination.html", context); err != nil {
		if w.Header().Get("Content-Type") == "" {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
		}
		http.Error(w, "Error rendering template: "+err.Error(), http.StatusInternalServerError)
		return
	}
}

func highlightMatches(text, searchQuery string) string {
	re := regexp.MustCompile(`(?i)` + regexp.QuoteMeta(searchQuery))
	text = re.ReplaceAllString(text, "<mark>$0</mark>")
	return text
}
