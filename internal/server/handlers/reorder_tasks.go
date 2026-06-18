package handlers

import (
	"GoTodo/internal/server/utils"
	"GoTodo/internal/sessionstore"
	"GoTodo/internal/storage"
	"GoTodo/internal/tasks"
	"context"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
)

// isXHR reports whether the request came from HTMX or a classic XHR/fetch client.
func isXHR(r *http.Request) bool {
	if r.Header.Get("HX-Request") == "true" {
		return true
	}
	if strings.ToLower(r.Header.Get("X-Requested-With")) == "xmlhttprequest" {
		return true
	}
	return false
}

// APIReorderTasks updates positions for tasks within a favorite/non-favorite group
func APIReorderTasks(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Invalid request method", http.StatusMethodNotAllowed)
		return
	}

	order := r.FormValue("order") // comma-separated IDs
	isFavStr := r.FormValue("is_favorite")
	pageStr := r.FormValue("page")

	page, _ := strconv.Atoi(pageStr)
	if page < 1 {
		page = 1
	}

	isFav := false
	if isFavStr == "true" || isFavStr == "1" {
		isFav = true
	}

	if order == "" {
		http.Error(w, "Missing order", http.StatusBadRequest)
		return
	}

	idStrs := strings.Split(order, ",")
	ids := make([]int, 0, len(idStrs))
	for _, s := range idStrs {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		v, err := strconv.Atoi(s)
		if err != nil {
			http.Error(w, "Invalid id in order", http.StatusBadRequest)
			return
		}
		ids = append(ids, v)
	}

	email, _, _, timezone, loggedIn, _ := utils.GetSessionUserWithTimezone(r)

	// Optional project filter (read early so validation and selection respect it)
	projectParam := r.FormValue("project")
	if projectParam == "" {
		projectParam = r.URL.Query().Get("project")
	}
	statusFilter := requestStatusFilter(r)

	// Log the request for diagnostics
	log.Printf("APIReorderTasks called: remote=%s user=%s loggedIn=%v order=%q page=%d project=%q",
		r.RemoteAddr, email, loggedIn, order, page, projectParam)

	// For XHR/HTMX clients, return a clean 401 instead of a redirect when not logged in.
	if !loggedIn {
		log.Printf("APIReorderTasks: unauthorized request from %s (user=%s); returning 401 for XHR=%v",
			r.RemoteAddr, email, isXHR(r))
		if isXHR(r) {
			w.Header().Set("Content-Type", "text/plain; charset=utf-8")
			w.WriteHeader(http.StatusUnauthorized)
			fmt.Fprint(w, "unauthorized")
			return
		}
		// Non-XHR fallback: redirect to base path (preserve previous UX for browser navigations)
		http.Redirect(w, r, utils.GetBasePath(), http.StatusSeeOther)
		return
	}

	// Prevent banned users from performing actions
	if isBanned, err := storage.IsUserBanned(email); err == nil && isBanned {
		sessionstore.ClearSessionCookie(w, r)
		basePath := utils.GetBasePath()
		log.Printf("APIReorderTasks: banned user %s from %s - clearing session and responding with HX-Redirect=%s",
			email, r.RemoteAddr, basePath)

		// For HTMX clients we communicate navigation via HX-Redirect header; do not issue a 3xx.
		w.Header().Set("HX-Redirect", basePath)
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, " ")
		return
	}

	db, err := storage.OpenDatabase()
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	defer db.Close()

	var userID int
	if uid := utils.GetSessionUserID(r); uid != nil {
		userID = *uid
	} else {
		err = db.QueryRow(context.Background(), "SELECT id FROM users WHERE email = $1", email).Scan(&userID)
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
	}

	projectFilter := parseProjectFilter(projectParam)

	// Validate that all provided IDs belong to the user and match is_favorite and project (if provided)
	for _, id := range ids {
		var exists bool
		projectCond := ""
		args := []interface{}{id, userID, isFav}
		if projectFilter != nil {
			if *projectFilter == 0 {
				projectCond = " AND project_id IS NULL"
			} else {
				projectCond = " AND project_id = $4"
				args = append(args, *projectFilter)
			}
		}
		query := "SELECT EXISTS(SELECT 1 FROM tasks WHERE id = $1 AND user_id = $2 AND COALESCE(is_favorite,false) = $3" + projectCond + ")"
		err = db.QueryRow(context.Background(), query, args...).Scan(&exists)
		if err != nil {
			http.Error(w, "Error validating tasks", http.StatusInternalServerError)
			return
		}
		if !exists {
			http.Error(w, "Task does not belong to user or mismatched favorite group/project", http.StatusBadRequest)
			return
		}
	}

	// Fetch all task IDs in this user's group ordered by position so we can renumber globally
	projectCondAll := ""
	argsAll := []interface{}{userID, isFav}
	q := "SELECT id FROM tasks WHERE user_id = $1 AND COALESCE(is_favorite,false) = $2"
	if projectFilter != nil {
		if *projectFilter == 0 {
			projectCondAll = " AND project_id IS NULL"
			q += projectCondAll
		} else {
			projectCondAll = " AND project_id = $3"
			q += projectCondAll
			argsAll = append(argsAll, *projectFilter)
		}
	}
	q += " ORDER BY position ASC, id ASC"
	rowsAll, err := db.Query(context.Background(), q, argsAll...)
	if err != nil {
		http.Error(w, "Error fetching task list for reorder", http.StatusInternalServerError)
		return
	}
	defer rowsAll.Close()

	allIDs := make([]int, 0)
	for rowsAll.Next() {
		var tid int
		if err := rowsAll.Scan(&tid); err != nil {
			http.Error(w, "Error reading task ids", http.StatusInternalServerError)
			return
		}
		allIDs = append(allIDs, tid)
	}

	if len(allIDs) == 0 {
		// Nothing to reorder
	} else {
		// Compute page window start index and clamp
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

		start := (page - 1) * pageSize
		if start < 0 {
			start = 0
		}
		// Ensure the replacement window fits in the allIDs slice
		if start > len(allIDs)-len(ids) {
			start = len(allIDs) - len(ids)
			if start < 0 {
				start = 0
			}
		}

		// Replace the slice segment with the new ordering provided by the client
		for i, id := range ids {
			if start+i < len(allIDs) {
				allIDs[start+i] = id
			} else {
				// Append if somehow beyond end
				allIDs = append(allIDs, id)
			}
		}

		// Update positions for allIDs inside a transaction
		tx, err := db.Begin(context.Background())
		if err != nil {
			http.Error(w, "Error starting transaction", http.StatusInternalServerError)
			return
		}
		defer tx.Rollback(context.Background())

		for idx, id := range allIDs {
			pos := idx + 1
			_, err := tx.Exec(context.Background(), "UPDATE tasks SET position = $1 WHERE id = $2 AND user_id = $3", pos, id, userID)
			if err != nil {
				http.Error(w, "Error updating positions", http.StatusInternalServerError)
				return
			}
		}

		if err := tx.Commit(context.Background()); err != nil {
			http.Error(w, "Error committing position updates", http.StatusInternalServerError)
			return
		}
	}

	// Determine page size from session
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

	// Optional project filter
	// (projectParam and projectFilter were parsed earlier)

	userPtr := &userID
	var taskList []tasks.Task
	var totalTasks int
	taskList, totalTasks, err = tasks.ReturnPaginationForUserWithFilters(page, pageSize, userPtr, timezone, projectFilter, statusFilter)
	if err != nil {
		http.Error(w, "Error fetching tasks: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Split into favorites and non-favorites
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

	uid := userID
	pagination := utils.GetPaginationData(page, pageSize, totalTasks, uid)

	// Compute completed/incomplete counts respecting project filter
	completedCount, incompleteCount := completedIncompleteCounts(userPtr, projectFilter)

	// Fetch projects for user and mark selected
	projectsList := make([]map[string]interface{}, 0)
	if projs, perr := storage.GetProjectsForUser(uid); perr == nil {
		for _, p := range projs {
			sel := false
			if projectFilter != nil && *projectFilter == p.ID {
				sel = true
			}
			projectsList = append(projectsList, map[string]interface{}{"ID": p.ID, "Name": p.Name, "Selected": sel})
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
		"SearchQuery":      "",
		"TotalTasks":       totalTasks,
		"LoggedIn":         true,
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
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := utils.RenderTemplate(w, r, "pagination.html", context); err != nil {
		http.Error(w, "Error rendering template: "+err.Error(), http.StatusInternalServerError)
		return
	}
}
