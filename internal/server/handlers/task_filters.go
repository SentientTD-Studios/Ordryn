package handlers

import (
	"GoTodo/internal/tasks"
	"net/http"
	"strconv"
	"strings"
)

// FilterContext holds active list filters for API task queries.
type FilterContext struct {
	Project   string
	Status    string
	Due       string
	Completed string
	Priority  string
	Tag       string
	Sort      string
	Search    string
	Page      int
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

func normalizeCompletedFilter(completed string) string {
	switch strings.ToLower(strings.TrimSpace(completed)) {
	case "week":
		return "week"
	default:
		return ""
	}
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

func filterContextFromRequest(r *http.Request) FilterContext {
	fc := FilterContext{
		Project:   firstNonEmpty(r.URL.Query().Get("project"), r.FormValue("project")),
		Status:    requestStatusFilter(r),
		Due:       normalizeDueFilter(firstNonEmpty(r.URL.Query().Get("due"), r.FormValue("due"))),
		Completed: normalizeCompletedFilter(firstNonEmpty(r.URL.Query().Get("completed"), r.FormValue("completed"))),
		Sort:      normalizeSortFilter(firstNonEmpty(r.URL.Query().Get("sort"), r.FormValue("sort"))),
		Priority:  normalizePriorityFilter(firstNonEmpty(r.URL.Query().Get("priority"), r.FormValue("priority"))),
		Tag:       normalizeTagFilter(firstNonEmpty(r.URL.Query().Get("tag"), r.FormValue("tag"))),
		Search:    strings.TrimSpace(firstNonEmpty(r.URL.Query().Get("search"), r.FormValue("search"))),
	}
	if pageParam := firstNonEmpty(r.URL.Query().Get("page"), r.FormValue("page"), r.FormValue("currentPage")); pageParam != "" {
		if page, err := strconv.Atoi(pageParam); err == nil && page > 0 {
			fc.Page = page
		}
	}
	return fc
}

func (fc FilterContext) ToListFilters() tasks.ListFilters {
	lf := tasks.ListFilters{
		ProjectFilter:   parseProjectFilter(fc.Project),
		StatusFilter:    fc.Status,
		DueFilter:       fc.Due,
		CompletedFilter: fc.Completed,
		Sort:            fc.Sort,
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

func fetchTasksForFilters(page, pageSize int, fc FilterContext, userID *int, timezone string) ([]tasks.Task, int, error) {
	filters := fc.ToListFilters()
	if fc.Search != "" {
		return tasks.SearchTasksForUserWithFilters(page, pageSize, fc.Search, userID, timezone, filters)
	}
	return tasks.ReturnPaginationForUserWithFilters(page, pageSize, userID, timezone, filters)
}
