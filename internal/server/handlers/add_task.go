package handlers

import (
	"GoTodo/internal/domain"
	"GoTodo/internal/server/utils"
	"GoTodo/internal/sessionstore"
	"GoTodo/internal/storage"
	"GoTodo/internal/tasks"
	"fmt"
	"net/http"
	"strconv"
	"strings"
)

// MaxDescriptionLength aliases the shared domain limit for HTMX handlers/validators.
const MaxDescriptionLength = domain.MaxDescriptionLength

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

	email, _, _, timezone, loggedIn, _ := utils.GetSessionUserWithTimezone(r)
	if !loggedIn {
		w.WriteHeader(http.StatusUnauthorized)
		fmt.Fprint(w, "Please log in to add tasks.")
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
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	projectIDStr := strings.TrimSpace(r.FormValue("project_id"))
	var projectID *int
	var newTaskProject *int
	if projectIDStr != "" {
		pid, errConv := strconv.Atoi(projectIDStr)
		if errConv != nil {
			http.Error(w, "Invalid project id", http.StatusBadRequest)
			return
		}
		projectID = &pid
		newTaskProject = &pid
	}

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

	if _, err := domain.CreateTask(r.Context(), userID, domain.CreateTaskInput{
		Title:       title,
		Description: description,
		DueDate:     dueDate,
		ProjectID:   projectID,
		Priority:    priority,
		TagIDs:      tagIDs,
	}); err != nil {
		if strings.Contains(err.Error(), "validation") {
			w.Header().Set("X-Validation-Error", "true")
			w.Header().Set("HX-Trigger", "description-error")
			w.Header().Set("HX-Retarget", "#description-error")
			w.Header().Set("HX-Reswap", "innerHTML")
			w.WriteHeader(http.StatusOK)
			fmt.Fprint(w, err.Error())
			return
		}
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	if isCalendarReturn(r) {
		respondCalendarRedirect(w, r, calendarMonthFromRequest(r, timezone), timezone)
		return
	}

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

	fc := filterContextFromRequest(r)
	activeProject := fc.Project
	activeStatus := fc.Status
	projectFilterPtr := parseProjectFilter(activeProject)
	listFilters := fc.ToListFilters()

	_, totalTasks, err := tasks.ReturnPaginationForUserWithFilters(1, pageSize, &userID, timezone, listFilters)
	if err != nil {
		http.Error(w, "Error counting tasks after add: "+err.Error(), http.StatusInternalServerError)
		return
	}

	lastPage := (totalTasks + pageSize - 1) / pageSize
	if lastPage < 1 {
		lastPage = 1
	}
	if page < lastPage {
		page = lastPage
	}

	shouldRefresh := false
	if projectFilterPtr == nil {
		shouldRefresh = true
	} else if *projectFilterPtr == 0 {
		if newTaskProject == nil {
			shouldRefresh = true
		}
	} else if newTaskProject != nil && *newTaskProject == *projectFilterPtr {
		shouldRefresh = true
	}
	if !taskStatusMatchesFilter(activeStatus, false) {
		shouldRefresh = false
	}

	w.Header().Set("HX-Trigger", "task-added")

	if shouldRefresh {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if err := renderFilteredTaskListPartial(w, r, page, pageSize, fc, &userID, timezone, true); err != nil {
			http.Error(w, "Error rendering tasks after add: "+err.Error(), http.StatusInternalServerError)
		}
		return
	}

	var targetFilterParam string
	if newTaskProject == nil {
		targetFilterParam = "0"
	} else {
		targetFilterParam = strconv.Itoa(*newTaskProject)
	}

	targetFC := fc
	targetFC.Project = targetFilterParam

	_, totalTasksTarget, err := tasks.ReturnPaginationForUserWithFilters(1, pageSize, &userID, timezone, targetFC.ToListFilters())
	if err != nil {
		http.Error(w, "Error counting tasks for new project: "+err.Error(), http.StatusInternalServerError)
		return
	}

	lastPageTarget := (totalTasksTarget + pageSize - 1) / pageSize
	if lastPageTarget < 1 {
		lastPageTarget = 1
	}
	page = lastPageTarget

	w.Header().Set("HX-Trigger", "task-added set-project-filter:"+targetFilterParam)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := renderFilteredTaskListPartial(w, r, page, pageSize, targetFC, &userID, timezone, true); err != nil {
		http.Error(w, "Error rendering tasks after add: "+err.Error(), http.StatusInternalServerError)
	}
}
