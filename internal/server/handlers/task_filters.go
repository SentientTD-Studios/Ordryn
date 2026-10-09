package handlers

import (
	"GoTodo/internal/server/utils"
	"GoTodo/internal/storage"
	"GoTodo/internal/tasks"
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"
)

// FilterContext holds active list filters for API task queries.
type FilterContext struct {
	Project            string
	Status             string
	Due                string
	Completed          string
	Priority           string
	Tag                string
	Sort               string
	Search             string
	Page               int
	WorkflowClaimScope string
	Claimed            string
	Sprint             string
	IncludeSubtasks    bool
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
	if len(tag) > 50 {
		return ""
	}
	return tag
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

func normalizeWorkflowClaimScope(scope string) string {
	switch strings.ToLower(strings.TrimSpace(scope)) {
	case "mine", "all":
		return strings.ToLower(strings.TrimSpace(scope))
	default:
		return ""
	}
}

func filterContextFromRequest(r *http.Request) FilterContext {
	fc := FilterContext{
		Project:            firstNonEmpty(r.URL.Query().Get("project"), r.FormValue("project")),
		Status:             requestStatusFilter(r),
		Due:                normalizeDueFilter(firstNonEmpty(r.URL.Query().Get("due"), r.FormValue("due"))),
		Completed:          normalizeCompletedFilter(firstNonEmpty(r.URL.Query().Get("completed"), r.FormValue("completed"))),
		Sort:               normalizeSortFilter(firstNonEmpty(r.URL.Query().Get("sort"), r.FormValue("sort"))),
		Priority:           normalizePriorityFilter(firstNonEmpty(r.URL.Query().Get("priority"), r.FormValue("priority"))),
		Tag:                normalizeTagFilter(firstNonEmpty(r.URL.Query().Get("tag"), r.FormValue("tag"))),
		Search:             strings.TrimSpace(firstNonEmpty(r.URL.Query().Get("search"), r.FormValue("search"))),
		WorkflowClaimScope: normalizeWorkflowClaimScope(firstNonEmpty(r.URL.Query().Get("workflow_claim_scope"), r.FormValue("workflow_claim_scope"))),
		Sprint:             firstNonEmpty(r.URL.Query().Get("sprint_id"), r.FormValue("sprint_id")),
		Claimed:            tasks.NormalizeClaimFilter(firstNonEmpty(r.URL.Query().Get("claimed"), r.FormValue("claimed"))),
		IncludeSubtasks:    parseIncludeSubtasks(firstNonEmpty(r.URL.Query().Get("include_subtasks"), r.FormValue("include_subtasks"))),
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
		ProjectFilter:      parseProjectFilter(fc.Project),
		StatusFilter:       fc.Status,
		DueFilter:          fc.Due,
		CompletedFilter:    fc.Completed,
		Sort:               fc.Sort,
		WorkflowClaimScope: fc.WorkflowClaimScope,
		ClaimFilter:        fc.Claimed,
		IncludeSubtasks:    fc.IncludeSubtasks,
	}
	if sid := parseSprintFilter(fc.Sprint); sid != nil {
		lf.SprintFilter = sid
	}
	if fc.Priority != "" {
		if p, err := strconv.Atoi(fc.Priority); err == nil {
			lf.PriorityFilter = &p
		}
	}
	if fc.Tag != "" {
		if tid, err := strconv.Atoi(fc.Tag); err == nil && tid > 0 {
			if lf.ProjectFilter != nil && *lf.ProjectFilter > 0 {
				lf.TagFilter = &tid
			} else if tag, err := storage.GetTag(tid); err == nil {
				lf.TagNameFilter = tag.Name
			} else {
				lf.TagFilter = &tid
			}
		} else {
			lf.TagNameFilter = fc.Tag
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

func parseIncludeSubtasks(v string) bool {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "1", "true", "yes":
		return true
	default:
		return false
	}
}

func parseSprintFilter(sprintParam string) *int {
	if sprintParam == "" {
		return nil
	}
	if sprintParam == "none" || sprintParam == "backlog" || sprintParam == "0" {
		zero := 0
		return &zero
	}
	if sid, err := strconv.Atoi(sprintParam); err == nil && sid > 0 {
		return &sid
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

func completedIncompleteCounts(userID *int, projectFilter *int, sprintFilter *int, includeRemoved ...bool) (int, int) {
	if userID == nil {
		return 0, 0
	}
	incRemoved := len(includeRemoved) > 0 && includeRemoved[0]
	if !incRemoved && projectFilter == nil && sprintFilter == nil {
		return utils.GetCompletedTasksCount(userID), utils.GetIncompleteTasksCount(userID)
	}

	pool, err := storage.OpenDatabase()
	if err != nil {
		return 0, 0
	}
	defer storage.CloseDatabase(pool)

	filterCond := ""
	args := []interface{}{*userID}
	if projectFilter != nil {
		if *projectFilter == 0 {
			filterCond += " AND project_id IS NULL"
		} else {
			args = append(args, *projectFilter)
			filterCond += fmt.Sprintf(" AND project_id = $%d", len(args))
		}
	}

	if sprintFilter != nil {
		if *sprintFilter > 0 {
			args = append(args, *sprintFilter)
			filterCond += fmt.Sprintf(" AND sprint_id = $%d", len(args))
		} else if *sprintFilter == 0 {
			filterCond += " AND sprint_id IS NULL"
		}
	}

	completedCount := 0
	incompleteCount := 0
	notArchived := ""
	if !incRemoved {
		notArchived = " AND NOT " + storage.ArchivedTaskExistsSQL("id")
	}
	if err := pool.QueryRow(context.Background(), "SELECT COUNT(*) FROM tasks WHERE user_id = $1 AND completed = true"+filterCond+notArchived, args...).Scan(&completedCount); err != nil {
		completedCount = 0
	}
	if err := pool.QueryRow(context.Background(), "SELECT COUNT(*) FROM tasks WHERE user_id = $1 AND (completed IS NULL OR completed = false)"+filterCond+notArchived, args...).Scan(&incompleteCount); err != nil {
		incompleteCount = 0
	}
	return completedCount, incompleteCount
}
