package handlers

import (
	"GoTodo/internal/domain"
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

	email, _, _, timezone, loggedIn, _ := utils.GetSessionUserWithTimezone(r)
	if !loggedIn {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}

	db, err := storage.OpenDatabase()
	if err != nil {
		http.Error(w, "Failed to open database", http.StatusInternalServerError)
		return
	}
	defer storage.CloseDatabase(db)

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

	returnTo := ""
	calendarMonth := ""
	if r.URL.Query().Get("from") == "calendar" {
		returnTo = "calendar"
		calendarMonth = calendarMonthFromRequest(r, timezone)
	}

	data := struct {
		FormTitle       string
		Description     string
		CurrentPage     string
		ID              string
		FormAction      string
		SubmitText      string
		SidebarTitle    string
		Error           string
		DueDate         string
		Priority        int
		Completed       bool
		Projects        []map[string]interface{}
		Tags            []map[string]interface{}
		ProjectFilter   string
		StatusFilter    string
		DueFilter       string
		SortFilter      string
		PriorityFilter  string
		TagFilter       string
		CompletedFilter string
		ReturnTo        string
		CalendarMonth   string
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
		TagFilter:       fc.Tag,
		CompletedFilter: fc.Completed,
		ReturnTo:        returnTo,
		CalendarMonth:   calendarMonth,
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

	userID, ok := resolveRequestUserID(r)
	if !ok {
		http.Error(w, "Error getting user ID", http.StatusInternalServerError)
		return
	}

	taskID, err := strconv.Atoi(id)
	if err != nil {
		http.Error(w, "Invalid task id", http.StatusBadRequest)
		return
	}

	oldTags, _ := storage.GetTagsForTask(taskID)
	tagIDs, tagErr := resolveTaskTagIDsFromRequest(r, userID)
	if tagErr != nil {
		w.Header().Set("X-Validation-Error", "true")
		w.Header().Set("HX-Trigger", "description-error")
		w.Header().Set("HX-Retarget", "#description-error")
		w.Header().Set("HX-Reswap", "innerHTML")
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, tagErr.Error())
		return
	}

	projectIDStr := strings.TrimSpace(r.FormValue("project_id"))
	var projectPtr *int
	if projectIDStr == "" {
		zero := 0
		projectPtr = &zero
	} else {
		pid, errConv := strconv.Atoi(projectIDStr)
		if errConv != nil {
			http.Error(w, "Invalid project id", http.StatusBadRequest)
			return
		}
		projectPtr = &pid
	}
	projectField := &projectPtr

	titleCopy, descCopy, dueCopy := title, description, dueDate
	prioCopy := priority
	in := domain.UpdateTaskInput{
		Title:       &titleCopy,
		Description: &descCopy,
		DueDate:     &dueCopy,
		Priority:    &prioCopy,
		ProjectID:   projectField,
		TagIDs:      &tagIDs,
	}
	if dueDate == "" {
		in.ClearDue = true
	}

	result, err := domain.UpdateTask(r.Context(), userID, taskID, in)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			http.Error(w, "Task not found.", http.StatusNotFound)
			return
		}
		if errors.Is(err, domain.ErrValidation) {
			w.Header().Set("X-Validation-Error", "true")
			w.Header().Set("HX-Trigger", "description-error")
			w.Header().Set("HX-Retarget", "#description-error")
			w.Header().Set("HX-Reswap", "innerHTML")
			w.WriteHeader(http.StatusOK)
			fmt.Fprint(w, err.Error())
			return
		}
		http.Error(w, "Failed to update task.", http.StatusInternalServerError)
		return
	}

	newTags, _ := storage.GetTagsForTask(taskID)
	logTagChanges(taskID, userID, oldTags, newTags)
	if result.PriorityChanged {
		logTaskEvent(taskID, userID, "priority_changed", map[string]interface{}{"to": priorityLabel(result.NewPriority)})
	}
	if result.ProjectChanged {
		logTaskEvent(taskID, userID, "moved_project", map[string]interface{}{
			"project": projectDisplayName(userID, result.NewProjectID),
		})
	}
	logTaskEvent(taskID, userID, "edited", map[string]interface{}{"fields": []string{"title", "description", "due date"}})

	// Re-render the task row in place when it still matches the active filters.
	if isCalendarReturn(r) {
		respondCalendarRedirect(w, r, calendarMonthFromRequest(r, timezone), timezone)
		return
	}

	fc := filterContextFromRequest(r)
	activeProject := fc.Project
	listFilters := fc.ToListFilters()

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

	task, err := tasks.FetchTaskByIDForUser(taskID, userID, timezone, page)
	if err != nil {
		http.Error(w, "Error fetching updated task.", http.StatusInternalServerError)
		return
	}

	matches, err := tasks.TaskMatchesFilters(taskID, userID, timezone, listFilters, fc.Search)
	if err != nil {
		http.Error(w, "Error checking task filters.", http.StatusInternalServerError)
		return
	}

	if activeProject != "" {
		w.Header().Set("HX-Trigger", "task-edited set-project-filter:"+activeProject)
	} else {
		w.Header().Set("HX-Trigger", "task-edited")
	}

	if matches {
		if err := renderSingleTaskRow(w, task, fc, timezone); err != nil {
			http.Error(w, "Error rendering task row after edit: "+err.Error(), http.StatusInternalServerError)
		}
		return
	}

	w.Header().Set("HX-Retarget", "#task-container")
	w.Header().Set("HX-Reswap", "innerHTML")
	userPtr := &userID
	if err := renderFilteredTaskListPartial(w, r, page, pageSize, fc, userPtr, timezone, true); err != nil {
		http.Error(w, "Error rendering tasks after edit: "+err.Error(), http.StatusInternalServerError)
	}
}
