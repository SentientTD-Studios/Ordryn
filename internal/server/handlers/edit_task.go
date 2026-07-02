package handlers

import (
	"GoTodo/internal/server/utils"
	"GoTodo/internal/sessionstore"
	"GoTodo/internal/storage"
	"GoTodo/internal/tasks"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
)

// APIEditTaskForm renders the sidebar edit form populated with the task data
func APIEditTaskForm(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Query().Get("id")
	if id == "" {
		http.Error(w, "Task ID is required", http.StatusBadRequest)
		return
	}

	page := r.URL.Query().Get("page")
	if page == "" {
		page = "1"
	}

	email, _, _, loggedIn := utils.GetSessionUser(r)
	if !loggedIn {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}

	db, err := storage.OpenDatabase()
	if err != nil {
		http.Error(w, "Failed to open database", http.StatusInternalServerError)
		return
	}
	defer db.Close()

	var title, description string
	var completed bool
	var ownerID int
	var projectID sql.NullInt64
	var dueDate sql.NullString
	var priority int
	err = db.QueryRow(context.Background(), "SELECT title, description, completed, user_id, project_id, COALESCE(CAST(due_date AS TEXT), ''), COALESCE(priority,0) FROM tasks WHERE id = $1", id).Scan(&title, &description, &completed, &ownerID, &projectID, &dueDate, &priority)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			http.Error(w, "Task not found.", http.StatusNotFound)
			return
		}
		http.Error(w, "Error fetching task.", http.StatusInternalServerError)
		return
	}

	var userID int
	if uid := utils.GetSessionUserID(r); uid != nil {
		userID = *uid
	} else {
		err = db.QueryRow(context.Background(), "SELECT id FROM users WHERE email = $1", email).Scan(&userID)
		if err != nil {
			http.Error(w, "Error getting user ID", http.StatusInternalServerError)
			return
		}
	}
	if ownerID != userID {
		http.Error(w, "Not authorized to edit this task.", http.StatusForbidden)
		return
	}

	// Fetch projects for this user so the form can render the select
	projectsList := make([]map[string]interface{}, 0)
	if uid := utils.GetSessionUserID(r); uid != nil {
		if projs, perr := storage.GetProjectsForUser(*uid); perr == nil {
			for _, p := range projs {
				sel := false
				if projectID.Valid && int(projectID.Int64) == p.ID {
					sel = true
				}
				projectsList = append(projectsList, map[string]interface{}{"ID": p.ID, "Name": p.Name, "Selected": sel})
			}
		}
	}

	// Get the current filters from query string to pass to form
	fc := filterContextFromRequest(r)

	taskTags, _ := storage.GetTagsForTask(0)
	if idInt, err := strconv.Atoi(id); err == nil {
		taskTags, _ = storage.GetTagsForTask(idInt)
	}
	tagOptions := buildTagFormOptions(userID, selectedTagIDMap(taskTags))

	data := struct {
		FormTitle      string
		Description    string
		CurrentPage    string
		ID             string
		FormAction     string
		SubmitText     string
		SidebarTitle   string
		Error          string
		DueDate        string
		Priority       int
		Completed      bool
		Projects       []map[string]interface{}
		Tags           []map[string]interface{}
		ProjectFilter  string
		StatusFilter   string
		DueFilter      string
		SortFilter     string
		PriorityFilter string
		TagFilter      string
	}{
		FormTitle:      strings.TrimSpace(title),
		Description:    strings.TrimSpace(description),
		CurrentPage:    page,
		ID:             id,
		FormAction:     utils.GetBasePath() + "/api/edit-task",
		SubmitText:     "Save Changes",
		SidebarTitle:   "Edit Task",
		Error:          "",
		DueDate:        dueDate.String,
		Priority:       priority,
		Completed:      completed,
		Projects:       projectsList,
		Tags:           tagOptions,
		ProjectFilter:  fc.Project,
		StatusFilter:   fc.Status,
		DueFilter:      fc.Due,
		SortFilter:     fc.Sort,
		PriorityFilter: fc.Priority,
		TagFilter:      fc.Tag,
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	// Render only the form fragment so HTMX can swap it into the existing sidebar body
	if err := utils.Templates.ExecuteTemplate(w, "sidebar_form.html", data); err != nil {
		http.Error(w, "Error rendering sidebar form: "+err.Error(), http.StatusInternalServerError)
		return
	}
}

// APIEditTask handles saving edits to an existing task
func APIEditTask(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Invalid request method", http.StatusMethodNotAllowed)
		return
	}

	id := strings.TrimSpace(r.FormValue("id"))
	title := strings.TrimSpace(r.FormValue("title"))
	description := strings.TrimSpace(r.FormValue("description"))
	dueDate := strings.TrimSpace(r.FormValue("due_date"))
	priority, err := parsePriorityValue(r.FormValue("priority_level"))
	if err != nil {
		priority = 0
	}
	pageStr := strings.TrimSpace(r.FormValue("currentPage"))

	page, err := strconv.Atoi(pageStr)
	if err != nil || page <= 0 {
		page = 1
	}

	if len(description) > MaxDescriptionLength {
		w.Header().Set("X-Validation-Error", "true")
		w.Header().Set("HX-Trigger", "description-error")
		w.Header().Set("HX-Retarget", "#description-error")
		w.Header().Set("HX-Reswap", "innerHTML")
		w.WriteHeader(http.StatusOK)
		fmt.Fprintf(w, "Description must be %d characters or less", MaxDescriptionLength)
		return
	}

	if title == "" {
		w.Header().Set("X-Validation-Error", "true")
		w.Header().Set("HX-Trigger", "description-error")
		w.Header().Set("HX-Retarget", "#description-error")
		w.Header().Set("HX-Reswap", "innerHTML")
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, "Title is required")
		return
	}

	db, err := storage.OpenDatabase()
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	defer db.Close()

	email, _, _, timezone, loggedIn, _ := utils.GetSessionUserWithTimezone(r)
	if !loggedIn {
		w.WriteHeader(http.StatusUnauthorized)
		fmt.Fprint(w, "Please log in to edit tasks.")
		return
	}

	if isBanned, err := storage.IsUserBanned(email); err == nil && isBanned {
		sessionstore.ClearSessionCookie(w, r)
		basePath := utils.GetBasePath()
		w.Header().Set("HX-Redirect", basePath)
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, " ")
		return
	}

	// Verify task exists and ownership
	var ownerID int
	err = db.QueryRow(context.Background(), "SELECT user_id FROM tasks WHERE id = $1", id).Scan(&ownerID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			http.Error(w, "Task not found.", http.StatusNotFound)
			return
		}
		http.Error(w, "Error fetching task.", http.StatusInternalServerError)
		return
	}

	var userID int
	if uid := utils.GetSessionUserID(r); uid != nil {
		userID = *uid
	} else {
		err = db.QueryRow(context.Background(), "SELECT id FROM users WHERE email = $1", email).Scan(&userID)
		if err != nil {
			http.Error(w, "Error getting user ID", http.StatusInternalServerError)
			return
		}
	}
	if ownerID != userID {
		http.Error(w, "Not authorized to edit this task.", http.StatusForbidden)
		return
	}

	var oldTitle, oldDescription, oldDue string
	var oldPriority int
	var oldProjectID sql.NullInt64
	err = db.QueryRow(context.Background(),
		"SELECT title, description, COALESCE(CAST(due_date AS TEXT), ''), COALESCE(priority,0), project_id FROM tasks WHERE id = $1",
		id).Scan(&oldTitle, &oldDescription, &oldDue, &oldPriority, &oldProjectID)
	if err != nil {
		http.Error(w, "Error fetching task.", http.StatusInternalServerError)
		return
	}
	oldTags := []storage.Tag{}
	if taskIDInt, convErr := strconv.Atoi(id); convErr == nil {
		oldTags, _ = storage.GetTagsForTask(taskIDInt)
	}

	// Handle optional project association
	projectIDStr := strings.TrimSpace(r.FormValue("project_id"))
	if projectIDStr == "" {
		// Clear project association
		var err2 error
		if dueDate == "" {
			_, err2 = db.Exec(context.Background(), "UPDATE tasks SET title = $1, description = $2, project_id = NULL, due_date = NULL, priority = $4, date_modified = NOW() AT TIME ZONE 'UTC' WHERE id = $3", title, description, id, priority)
		} else {
			_, err2 = db.Exec(context.Background(), "UPDATE tasks SET title = $1, description = $2, project_id = NULL, due_date = $3, priority = $5, date_modified = NOW() AT TIME ZONE 'UTC' WHERE id = $4", title, description, dueDate, id, priority)
		}
		err = err2
		if err != nil {
			http.Error(w, "Failed to update task.", http.StatusInternalServerError)
			return
		}
	} else {
		pid, errConv := strconv.Atoi(projectIDStr)
		if errConv != nil {
			http.Error(w, "Invalid project id", http.StatusBadRequest)
			return
		}
		// Validate ownership of chosen project
		if _, perr := storage.GetProjectByID(pid, userID); perr != nil {
			http.Error(w, "Invalid project selection", http.StatusBadRequest)
			return
		}
		var err2 error
		if dueDate == "" {
			_, err2 = db.Exec(context.Background(), "UPDATE tasks SET title = $1, description = $2, project_id = $3, due_date = NULL, priority = $5, date_modified = NOW() AT TIME ZONE 'UTC' WHERE id = $4", title, description, pid, id, priority)
		} else {
			_, err2 = db.Exec(context.Background(), "UPDATE tasks SET title = $1, description = $2, project_id = $3, due_date = $4, priority = $6, date_modified = NOW() AT TIME ZONE 'UTC' WHERE id = $5", title, description, pid, dueDate, id, priority)
		}
		err = err2
		if err != nil {
			http.Error(w, "Failed to update task.", http.StatusInternalServerError)
			return
		}
	}

	taskID, err := strconv.Atoi(id)
	if err != nil {
		http.Error(w, "Invalid task id", http.StatusBadRequest)
		return
	}

	if err := assignTaskTagsFromRequest(r, taskID, userID); err != nil {
		w.Header().Set("X-Validation-Error", "true")
		w.Header().Set("HX-Trigger", "description-error")
		w.Header().Set("HX-Retarget", "#description-error")
		w.Header().Set("HX-Reswap", "innerHTML")
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, err.Error())
		return
	}

	newTags, _ := storage.GetTagsForTask(taskID)
	logTagChanges(taskID, userID, oldTags, newTags)

	changed := make([]string, 0, 4)
	if oldTitle != title {
		changed = append(changed, "title")
	}
	if oldDescription != description {
		changed = append(changed, "description")
	}
	if oldDue != dueDate {
		changed = append(changed, "due date")
	}
	if len(changed) > 0 {
		logTaskEvent(taskID, userID, "edited", map[string]interface{}{"fields": changed})
	}
	if oldPriority != priority {
		logTaskEvent(taskID, userID, "priority_changed", map[string]interface{}{"to": priorityLabel(priority)})
	}

	oldPID := projectIDFromNull(oldProjectID)
	newPID := projectIDFromForm(projectIDStr)
	if oldPID != newPID {
		logTaskEvent(taskID, userID, "moved_project", map[string]interface{}{
			"project": projectDisplayName(userID, newPID),
		})
	}

	// Re-render pagination like add_task does
	// Determine page size
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

	// Determine active filters (from form or query)
	fc := filterContextFromRequest(r)
	activeProject := fc.Project
	projectFilter := parseProjectFilter(activeProject)
	listFilters := fc.ToListFilters()

	var taskList []tasks.Task
	var totalTasks int
	taskList, totalTasks, err = tasks.ReturnPaginationForUserWithFilters(page, pageSize, &userID, timezone, listFilters)
	if err != nil {
		http.Error(w, "Error fetching tasks after edit: "+err.Error(), http.StatusInternalServerError)
		return
	}

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

	// Compute completed/incomplete counts respecting project filter
	completedCount, incompleteCount := completedIncompleteCounts(&userID, projectFilter)

	// Fetch projects and mark selected
	projectsList := make([]map[string]interface{}, 0)
	tagsList := make([]map[string]interface{}, 0)
	if projs, perr := storage.GetProjectsForUser(userID); perr == nil {
		for _, p := range projs {
			sel := false
			if projectFilter != nil && *projectFilter == p.ID {
				sel = true
			}
			projectsList = append(projectsList, map[string]interface{}{"ID": p.ID, "Name": p.Name, "Selected": sel})
		}
	}
	tagsList = tagsListForFilter(userID, fc.Tag)

	context := map[string]interface{}{
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
		"Timezone":         timezone,
	}
	for k, v := range fc.TemplateFields() {
		context[k] = v
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	// When editing, maintain the current project filter (don't follow the task to its new project)
	if activeProject != "" {
		w.Header().Set("HX-Trigger", "task-edited set-project-filter:"+activeProject)
	} else {
		w.Header().Set("HX-Trigger", "task-edited")
	}
	if err := utils.RenderTemplate(w, r, "pagination.html", context); err != nil {
		http.Error(w, "Error rendering tasks after edit: "+err.Error(), http.StatusInternalServerError)
		return
	}
}
