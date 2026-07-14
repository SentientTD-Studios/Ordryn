package handlers

import (
	"GoTodo/internal/server/utils"
	"GoTodo/internal/sessionstore"
	"GoTodo/internal/storage"
	"GoTodo/internal/tasks"
	"context"
	"errors"
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
	defer storage.CloseDatabase(db)

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

	if err := reorderTaskPositions(context.Background(), db, userID, ids, isFav, page, pageSize, projectFilter); err != nil {
		if errors.Is(err, ErrReorderValidation) {
			http.Error(w, "Task does not belong to user or mismatched favorite group/project", http.StatusBadRequest)
			return
		}
		http.Error(w, "Error updating positions", http.StatusInternalServerError)
		return
	}

	// Optional project filter
	// (projectParam and projectFilter were parsed earlier)

	userPtr := &userID
	var taskList []tasks.Task
	var totalTasks int
	fc := filterContextFromRequest(r)
	listFilters := fc.ToListFilters()
	taskList, totalTasks, err = tasks.ReturnPaginationForUserWithFilters(page, pageSize, userPtr, timezone, listFilters)
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
	}
	for k, v := range fc.TemplateFields() {
		context[k] = v
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := utils.RenderTemplate(w, r, "pagination.html", context); err != nil {
		http.Error(w, "Error rendering template: "+err.Error(), http.StatusInternalServerError)
		return
	}
}
