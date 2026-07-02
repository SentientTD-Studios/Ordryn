package handlers

import (
	"GoTodo/internal/server/utils"
	"GoTodo/internal/storage"
	"GoTodo/internal/tasks"
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

// FilterContext holds active list filters for HTMX task views.
type FilterContext struct {
	Project  string
	Status   string
	Due      string
	Priority string
	Tag      string
	Sort     string
	Search   string
	Page     int
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

func normalizeDueFilter(due string) string {
	return tasks.NormalizeDueFilter(due)
}

func normalizeSortFilter(sort string) string {
	switch strings.ToLower(strings.TrimSpace(sort)) {
	case "priority":
		return "priority"
	default:
		return ""
	}
}

func normalizePriorityFilter(priority string) string {
	priority = strings.TrimSpace(priority)
	if priority == "" {
		return ""
	}
	if p, err := strconv.Atoi(priority); err == nil && p >= 0 && p <= 3 {
		return strconv.Itoa(p)
	}
	return ""
}

func normalizeTagFilter(tag string) string {
	tag = strings.TrimSpace(tag)
	if tag == "" {
		return ""
	}
	if id, err := strconv.Atoi(tag); err == nil && id > 0 {
		return strconv.Itoa(id)
	}
	return ""
}

func filterContextFromRequest(r *http.Request) FilterContext {
	fc := FilterContext{
		Project:  firstNonEmpty(r.URL.Query().Get("project"), r.FormValue("project")),
		Status:   requestStatusFilter(r),
		Due:      normalizeDueFilter(firstNonEmpty(r.URL.Query().Get("due"), r.FormValue("due"))),
		Sort:     normalizeSortFilter(firstNonEmpty(r.URL.Query().Get("sort"), r.FormValue("sort"))),
		Priority: normalizePriorityFilter(firstNonEmpty(r.URL.Query().Get("priority"), r.FormValue("priority"))),
		Tag:      normalizeTagFilter(firstNonEmpty(r.URL.Query().Get("tag"), r.FormValue("tag"))),
		Search:   strings.TrimSpace(firstNonEmpty(r.URL.Query().Get("search"), r.FormValue("search"))),
	}
	if pageParam := firstNonEmpty(r.URL.Query().Get("page"), r.FormValue("page"), r.FormValue("currentPage")); pageParam != "" {
		if page, err := strconv.Atoi(pageParam); err == nil && page > 0 {
			fc.Page = page
		}
	}
	return fc
}

func (fc FilterContext) queryValues() url.Values {
	values := url.Values{}
	if fc.Search != "" {
		values.Set("search", fc.Search)
	}
	if fc.Project != "" {
		values.Set("project", fc.Project)
	}
	if fc.Status != "" {
		values.Set("status", fc.Status)
	}
	if fc.Due != "" {
		values.Set("due", fc.Due)
	}
	if fc.Sort != "" {
		values.Set("sort", fc.Sort)
	}
	if fc.Priority != "" {
		values.Set("priority", fc.Priority)
	}
	if fc.Tag != "" {
		values.Set("tag", fc.Tag)
	}
	return values
}

func (fc FilterContext) QuerySuffix() string {
	encoded := fc.queryValues().Encode()
	if encoded == "" {
		return ""
	}
	return "&" + encoded
}

func (fc FilterContext) QuerySuffixWithout(keys ...string) string {
	values := fc.queryValues()
	for _, key := range keys {
		values.Del(key)
	}
	encoded := values.Encode()
	if encoded == "" {
		return ""
	}
	return "&" + encoded
}

func (fc FilterContext) ToListFilters() tasks.ListFilters {
	lf := tasks.ListFilters{
		ProjectFilter: parseProjectFilter(fc.Project),
		StatusFilter:  fc.Status,
		DueFilter:     fc.Due,
		Sort:          fc.Sort,
	}
	if fc.Priority != "" {
		if p, err := strconv.Atoi(fc.Priority); err == nil {
			lf.PriorityFilter = &p
		}
	}
	if fc.Tag != "" {
		if tid, err := strconv.Atoi(fc.Tag); err == nil {
			lf.TagFilter = &tid
		}
	}
	return lf
}

func (fc FilterContext) TemplateFields() map[string]interface{} {
	return map[string]interface{}{
		"ProjectFilter":       fc.Project,
		"StatusFilter":        fc.Status,
		"DueFilter":           fc.Due,
		"SortFilter":          fc.Sort,
		"PriorityFilter":      fc.Priority,
		"TagFilter":           fc.Tag,
		"SearchQuery":         fc.Search,
		"FilterQuery":         fc.QuerySuffix(),
		"FilterQueryNoStatus": fc.QuerySuffixWithout("status"),
		"FilterQueryNoDue":    fc.QuerySuffixWithout("due"),
	}
}

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

func fetchTasksForFilters(page, pageSize int, fc FilterContext, userID *int, timezone string) ([]tasks.Task, int, error) {
	filters := fc.ToListFilters()
	if fc.Search != "" {
		return tasks.SearchTasksForUserWithFilters(page, pageSize, fc.Search, userID, timezone, filters)
	}
	return tasks.ReturnPaginationForUserWithFilters(page, pageSize, userID, timezone, filters)
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

func renderFilteredTaskListPartial(w http.ResponseWriter, r *http.Request, page, pageSize int, fc FilterContext, userID *int, timezone string, loggedIn bool) error {
	if page <= 0 {
		if fc.Page > 0 {
			page = fc.Page
		} else {
			page = 1
		}
	}

	taskList, totalTasks, err := fetchTasksForFilters(page, pageSize, fc, userID, timezone)
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
		refetched, refetchedTotal, err := fetchTasksForFilters(page, pageSize, fc, userID, timezone)
		if err != nil {
			return err
		}
		taskList = refetched
		totalTasks = refetchedTotal
	}

	if fc.Search != "" {
		for i := range taskList {
			taskList[i].Title = highlightMatches(taskList[i].Title, fc.Search)
			taskList[i].Description = highlightMatches(taskList[i].Description, fc.Search)
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
	projectFilter := parseProjectFilter(fc.Project)
	completedCount, incompleteCount := completedIncompleteCounts(userID, projectFilter)

	projectsList := make([]map[string]interface{}, 0)
	tagsList := make([]map[string]interface{}, 0)
	if userID != nil {
		if projs, perr := storage.GetProjectsForUser(*userID); perr == nil {
			for _, p := range projs {
				sel := projectFilter != nil && *projectFilter == p.ID
				projectsList = append(projectsList, map[string]interface{}{"ID": p.ID, "Name": p.Name, "Selected": sel})
			}
		}
		if tags, terr := storage.GetTagsForUser(*userID); terr == nil {
			for _, tg := range tags {
				sel := fc.Tag == strconv.Itoa(tg.ID)
				tagsList = append(tagsList, map[string]interface{}{"ID": tg.ID, "Name": tg.Name, "Color": tg.Color, "Selected": sel})
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
		"Tags":             tagsList,
		"IsSearching":      fc.Search != "",
	}
	for k, v := range fc.TemplateFields() {
		context[k] = v
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	return utils.RenderTemplate(w, r, "pagination.html", context)
}

func parsePriorityValue(raw string) (int, error) {
	priority, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil || priority < 0 || priority > 3 {
		return 0, fmt.Errorf("invalid priority")
	}
	return priority, nil
}
