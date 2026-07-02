package handlers

import (
	"GoTodo/internal/server/utils"
	"GoTodo/internal/sessionstore"
	"GoTodo/internal/storage"
	"GoTodo/internal/tasks"
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"
)

const MaxDescriptionLength = 1000

func APIAddTask(w http.ResponseWriter, r *http.Request) {
	// fmt.Println("Request method: ", r.Method)
	if r.Method != http.MethodPost {
		http.Error(w, "Invalid request method", http.StatusMethodNotAllowed)
		return
	}

	title := strings.TrimSpace(r.FormValue("title"))
	description := strings.TrimSpace(r.FormValue("description"))
	dueDate := strings.TrimSpace(r.FormValue("due_date"))
	priority := 0
	if p, err := parsePriorityValue(r.FormValue("priority_level")); err == nil {
		priority = p
	}
	pageStr := strings.TrimSpace(r.FormValue("currentPage"))

	page, err := strconv.Atoi(pageStr)
	if err != nil || page <= 0 {
		page = 1 // Default to page 1 if no valid page is provided
	}

	// Validate description length
	if len(description) > MaxDescriptionLength {
		// On validation failure, return a 200 status with the error message
		// and use HX-Retarget and HX-Reswap to update the error div specifically
		// Tell the client this was a validation error so JS won't close the sidebar
		w.Header().Set("X-Validation-Error", "true")
		w.Header().Set("HX-Trigger", "description-error")   // Keep trigger for potential JS handling
		w.Header().Set("HX-Retarget", "#description-error") // Target the specific error div
		w.Header().Set("HX-Reswap", "innerHTML")            // Swap the content inside the error div
		w.WriteHeader(http.StatusOK)
		fmt.Fprintf(w, "Description must be %d characters or less", MaxDescriptionLength) // The content to swap
		return
	}

	if title == "" {
		// Title missing — return validation error and appropriate message
		w.Header().Set("X-Validation-Error", "true")
		w.Header().Set("HX-Trigger", "description-error")   // Keep trigger for potential JS handling
		w.Header().Set("HX-Retarget", "#description-error") // Target the specific error div
		w.Header().Set("HX-Reswap", "innerHTML")            // Swap the content inside the error div
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, "Title is required")
		return
		// http.Error(w, "Title is required", http.StatusBadRequest)
		// return
	}

	db, err := storage.OpenDatabase()
	if err != nil {
		fmt.Println("We failed to open the database.")
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	defer db.Close()

	// Get user ID from session (fallback to querying by email if not present)
	email, _, _, timezone, loggedIn, _ := utils.GetSessionUserWithTimezone(r)
	if !loggedIn {
		w.WriteHeader(http.StatusUnauthorized)
		fmt.Fprint(w, "Please log in to add tasks.")
		return
	}

	// Prevent banned users from performing actions
	if isBanned, err := storage.IsUserBanned(email); err == nil && isBanned {
		sessionstore.ClearSessionCookie(w, r)
		basePath := utils.GetBasePath()
		w.Header().Set("HX-Redirect", basePath)
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, " ")
		return
	}

	var userID int
	if uid := utils.GetSessionUserID(r); uid != nil {
		userID = *uid
	} else {
		// fallback to DB lookup if session doesn't contain user_id
		err = db.QueryRow(context.Background(), "SELECT id FROM users WHERE email = $1", email).Scan(&userID)
		if err != nil {
			fmt.Printf("Error getting user ID: %v\n", err)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
	}

	// Determine next position within non-favorite group for this user
	var nextPos int
	err = db.QueryRow(context.Background(), "SELECT COALESCE(MAX(position),0) + 1 FROM tasks WHERE user_id = $1 AND (is_favorite IS NULL OR is_favorite = false)", userID).Scan(&nextPos)
	if err != nil {
		fmt.Printf("Error determining next position: %v\n", err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	// Handle optional project association
	projectIDStr := strings.TrimSpace(r.FormValue("project_id"))
	var newTaskProject *int
	var newTaskID int
	if projectIDStr == "" {
		if dueDate != "" {
			err = db.QueryRow(context.Background(), "INSERT INTO tasks (title, description, completed, user_id, time_stamp, position, priority, due_date) VALUES ($1, $2, $3, $4, NOW() AT TIME ZONE 'UTC', $5, $6, $7) RETURNING id", title, description, false, userID, nextPos, priority, dueDate).Scan(&newTaskID)
		} else {
			err = db.QueryRow(context.Background(), "INSERT INTO tasks (title, description, completed, user_id, time_stamp, position, priority) VALUES ($1, $2, $3, $4, NOW() AT TIME ZONE 'UTC', $5, $6) RETURNING id", title, description, false, userID, nextPos, priority).Scan(&newTaskID)
		}
	} else {
		pid, errConv := strconv.Atoi(projectIDStr)
		if errConv != nil {
			http.Error(w, "Invalid project id", http.StatusBadRequest)
			return
		}
		if _, errP := storage.GetProjectByID(pid, userID); errP != nil {
			http.Error(w, "Invalid project selection", http.StatusBadRequest)
			return
		}
		if dueDate != "" {
			err = db.QueryRow(context.Background(), "INSERT INTO tasks (title, description, completed, user_id, time_stamp, position, priority, project_id, due_date) VALUES ($1, $2, $3, $4, NOW() AT TIME ZONE 'UTC', $5, $6, $7, $8) RETURNING id", title, description, false, userID, nextPos, priority, pid, dueDate).Scan(&newTaskID)
		} else {
			err = db.QueryRow(context.Background(), "INSERT INTO tasks (title, description, completed, user_id, time_stamp, position, priority, project_id) VALUES ($1, $2, $3, $4, NOW() AT TIME ZONE 'UTC', $5, $6, $7) RETURNING id", title, description, false, userID, nextPos, priority, pid).Scan(&newTaskID)
		}
		if err == nil {
			newTaskProject = &pid
		}
	}
	if err != nil {
		fmt.Println("We failed to insert into the database.")
		fmt.Println("Failed values:", title, description, false)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	if err := assignTaskTagsFromRequest(r, newTaskID, userID); err != nil {
		w.Header().Set("X-Validation-Error", "true")
		w.Header().Set("HX-Trigger", "description-error")
		w.Header().Set("HX-Retarget", "#description-error")
		w.Header().Set("HX-Reswap", "innerHTML")
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, err.Error())
		return
	}
	logTaskEvent(newTaskID, userID, "created", nil)

	// After successful insertion, determine the correct page to display
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

	// Open a new DB connection to count total tasks (or reuse db if possible)
	// Determine active project filter (from form or query) so we can decide whether to refresh the current view
	fc := filterContextFromRequest(r)
	activeProject := fc.Project
	activeStatus := fc.Status
	projectFilterPtr := parseProjectFilter(activeProject)
	listFilters := fc.ToListFilters()

	var totalTasks int
	// Count tasks scoped to project if filter is active, otherwise count all
	statusCond := ""
	if activeStatus == "complete" {
		statusCond = " AND completed = true"
	} else if activeStatus == "incomplete" {
		statusCond = " AND (completed IS NULL OR completed = false)"
	}

	if projectFilterPtr == nil {
		err = db.QueryRow(context.Background(), "SELECT COUNT(*) FROM tasks WHERE user_id = $1"+statusCond, userID).Scan(&totalTasks)
	} else {
		projectCond := ""
		args := []interface{}{userID}
		if *projectFilterPtr == 0 {
			projectCond = " AND project_id IS NULL"
		} else {
			projectCond = " AND project_id = $2"
			args = append(args, *projectFilterPtr)
		}
		err = db.QueryRow(context.Background(), "SELECT COUNT(*) FROM tasks WHERE user_id = $1"+projectCond+statusCond, args...).Scan(&totalTasks)
	}
	if err != nil {
		http.Error(w, "Error counting tasks after add: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Calculate the new last page
	lastPage := (totalTasks + pageSize - 1) / pageSize
	if lastPage < 1 {
		lastPage = 1
	}

	// If the new task caused a new page, go to the last page
	if page < lastPage {
		page = lastPage
	}

	var taskList []tasks.Task
	taskList, totalTasks, err = tasks.ReturnPaginationForUserWithFilters(page, pageSize, &userID, timezone, listFilters)
	if err != nil {
		http.Error(w, "Error fetching tasks after add: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Calculate pagination button states based on new totalTasks
	prevDisabled := ""
	if page == 1 {
		prevDisabled = "disabled"
	}

	nextDisabled := ""
	if page*pageSize >= totalTasks {
		nextDisabled = "disabled"
	}

	prevPage := page - 1
	if prevPage < 1 {
		prevPage = 1
	}

	nextPage := page + 1

	// Split into favorites and non-favorites for rendering and allow separate sortable containers
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

	// Decide whether to refresh the current view: only refresh if we're on All projects OR
	// the new task's project matches the active project filter.
	shouldRefresh := false
	if projectFilterPtr == nil {
		// viewing All projects -> always refresh
		shouldRefresh = true
	} else {
		// viewing a specific project or 'No project'
		if *projectFilterPtr == 0 {
			// viewing No project; refresh only if new task has no project
			if newTaskProject == nil {
				shouldRefresh = true
			}
		} else if newTaskProject != nil && *newTaskProject == *projectFilterPtr {
			shouldRefresh = true
		}
	}
	if !taskStatusMatchesFilter(activeStatus, false) {
		shouldRefresh = false
	}

	// The new task belongs to a different project than the current filter.
	// Instead of returning nothing, render the view for the project the task was added to
	// and instruct the client to set the toolbar filter to that project.
	// Determine the target filter (new task's project or "No project")
	var targetFilterPtr *int
	var targetFilterParam string
	if newTaskProject == nil {
		zero := 0
		targetFilterPtr = &zero
		targetFilterParam = "0"
	} else {
		targetFilterPtr = newTaskProject
		targetFilterParam = strconv.Itoa(*newTaskProject)
	}

	// Count tasks scoped to target project
	var totalTasksTarget int
	projectCond := ""
	args := []interface{}{userID}
	if *targetFilterPtr == 0 {
		projectCond = " AND project_id IS NULL"
	} else {
		projectCond = " AND project_id = $2"
		args = append(args, *targetFilterPtr)
	}
	if err := db.QueryRow(context.Background(), "SELECT COUNT(*) FROM tasks WHERE user_id = $1"+projectCond+statusCond, args...).Scan(&totalTasksTarget); err != nil {
		http.Error(w, "Error counting tasks for new project: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Recalculate page for target list
	lastPageTarget := (totalTasksTarget + pageSize - 1) / pageSize
	if lastPageTarget < 1 {
		lastPageTarget = 1
	}
	if page > lastPageTarget {
		page = lastPageTarget
	}

	// Fetch projects and mark selected for toolbar
	projectsList := make([]map[string]interface{}, 0)
	if projs, perr := storage.GetProjectsForUser(userID); perr == nil {
		for _, p := range projs {
			sel := false
			if targetFilterPtr != nil && *targetFilterPtr == p.ID {
				sel = true
			}
			projectsList = append(projectsList, map[string]interface{}{"ID": p.ID, "Name": p.Name, "Selected": sel})
		}
	}
	tagsList := tagsListForFilter(userID, fc.Tag)

	completedCount, incompleteCount := completedIncompleteCounts(&userID, projectFilterPtr)

	// Create a context for rendering pagination.html
	tmplCtx := map[string]interface{}{
		"FavoriteTasks":    favs,
		"Tasks":            nonFavs,
		"PreviousPage":     prevPage,
		"NextPage":         nextPage,
		"CurrentPage":      page,
		"PrevDisabled":     prevDisabled,
		"NextDisabled":     nextDisabled,
		"TotalTasks":       totalTasks,
		"LoggedIn":         true,
		"TotalPages":       (totalTasks + pageSize - 1) / pageSize,
		"Pages":            utils.GetPaginationData(page, pageSize, totalTasks, userID).Pages,
		"HasRightEllipsis": utils.GetPaginationData(page, pageSize, totalTasks, userID).HasRightEllipsis,
		"CompletedTasks":   completedCount,
		"IncompleteTasks":  incompleteCount,
		"PerPage":          pageSize,
		"Projects":         projectsList,
		"Tags":             tagsList,
		"ProjectFilter":    activeProject,
		"StatusFilter":     activeStatus,
		"Timezone":         timezone,
	}
	for k, v := range fc.TemplateFields() {
		tmplCtx[k] = v
	}

	// Set headers for successful addition
	w.Header().Set("HX-Trigger", "task-added") // Signal JS to close sidebar and clear form

	if shouldRefresh {
		// Render the updated task list into the main task-container
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if err := utils.RenderTemplate(w, r, "pagination.html", tmplCtx); err != nil {
			http.Error(w, "Error rendering tasks after add: "+err.Error(), http.StatusInternalServerError)
			return
		}
		return
	}

	// Fetch tasks for the target project and page
	targetFC := fc
	targetFC.Project = targetFilterParam
	taskListTarget, totalTasksTarget, err := tasks.ReturnPaginationForUserWithFilters(page, pageSize, &userID, timezone, targetFC.ToListFilters())
	if err != nil {
		http.Error(w, "Error fetching tasks for new project: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Build pagination state for target
	prevDisabledT := ""
	if page == 1 {
		prevDisabledT = "disabled"
	}
	nextDisabledT := ""
	if page*pageSize >= totalTasksTarget {
		nextDisabledT = "disabled"
	}
	prevPageT := page - 1
	if prevPageT < 1 {
		prevPageT = 1
	}
	nextPageT := page + 1

	favsT := make([]tasks.Task, 0)
	nonFavsT := make([]tasks.Task, 0)
	for i := range taskListTarget {
		taskListTarget[i].Page = page
		if taskListTarget[i].IsFavorite {
			favsT = append(favsT, taskListTarget[i])
		} else {
			nonFavsT = append(nonFavsT, taskListTarget[i])
		}
	}

	// Compute completed/incomplete counts for target
	completedCountT := 0
	incompleteCountT := 0
	if db != nil {
		if err := db.QueryRow(context.Background(), "SELECT COUNT(*) FROM tasks WHERE user_id = $1 AND completed = true"+projectCond, args...).Scan(&completedCountT); err != nil {
			completedCountT = 0
		}
		if err := db.QueryRow(context.Background(), "SELECT COUNT(*) FROM tasks WHERE user_id = $1 AND (completed IS NULL OR completed = false)"+projectCond, args...).Scan(&incompleteCountT); err != nil {
			incompleteCountT = 0
		}
	}

	ctxT := map[string]interface{}{
		"FavoriteTasks":    favsT,
		"Tasks":            nonFavsT,
		"PreviousPage":     prevPageT,
		"NextPage":         nextPageT,
		"CurrentPage":      page,
		"PrevDisabled":     prevDisabledT,
		"NextDisabled":     nextDisabledT,
		"TotalTasks":       totalTasksTarget,
		"LoggedIn":         true,
		"TotalPages":       (totalTasksTarget + pageSize - 1) / pageSize,
		"Pages":            utils.GetPaginationData(page, pageSize, totalTasksTarget, userID).Pages,
		"HasRightEllipsis": utils.GetPaginationData(page, pageSize, totalTasksTarget, userID).HasRightEllipsis,
		"CompletedTasks":   completedCountT,
		"IncompleteTasks":  incompleteCountT,
		"PerPage":          pageSize,
		"Projects":         projectsList,
		"Tags":             tagsList,
		"ProjectFilter":    targetFilterParam,
		"StatusFilter":     activeStatus,
		"Timezone":         timezone,
	}
	for k, v := range targetFC.TemplateFields() {
		ctxT[k] = v
	}

	// Instruct client to set toolbar filter to target project
	w.Header().Set("HX-Trigger", "task-added set-project-filter:"+targetFilterParam)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := utils.RenderTemplate(w, r, "pagination.html", ctxT); err != nil {
		http.Error(w, "Error rendering tasks after add: "+err.Error(), http.StatusInternalServerError)
		return
	}
}
