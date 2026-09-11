package tasks

import (
	"GoTodo/internal/storage"
	"context"
	"database/sql"
	"fmt"
	"strconv"
	"strings"
)

const (
	RED   = "\033[31m"
	GREEN = "\033[32m"
	RESET = "\033[0m"
)

func ReturnTaskList() []Task {
	pool, _ := storage.OpenDatabase()
	defer storage.CloseDatabase(pool)

	var tasks []Task
	return tasks
}

func ReturnTaskListForUser(userID *int) []Task {
	pool, _ := storage.OpenDatabase()
	defer storage.CloseDatabase(pool)

	var tasks []Task
	if userID == nil {
		return tasks
	}

	rows, err := pool.Query(context.Background(), "SELECT id, title, description, completed FROM tasks WHERE user_id = $1 ORDER BY id", *userID)
	if err != nil {
		return tasks
	}
	defer rows.Close()

	for rows.Next() {
		var task Task
		err = rows.Scan(&task.ID, &task.Title, &task.Description, &task.Completed)
		if err != nil {
			return tasks
		}
		tasks = append(tasks, task)
	}
	return tasks
}

const nonFavoriteCond = " AND (is_favorite IS NULL OR is_favorite = false)"
const rootCond = " AND parent_id IS NULL"
const rootCondT = " AND t.parent_id IS NULL"

func taskSelectSQL() string {
	return `SELECT t.id, t.title, t.description, t.completed,
		TO_CHAR((t.time_stamp AT TIME ZONE 'UTC') AT TIME ZONE $2, 'YYYY/MM/DD HH:MI AM') AS date_added,
		COALESCE(CAST(t.due_date AS TEXT), '') AS due_date,
		TO_CHAR((t.time_stamp AT TIME ZONE 'UTC') AT TIME ZONE $2, 'YYYY/MM/DD HH:MI AM') AS date_created,
		COALESCE(TO_CHAR((t.date_modified AT TIME ZONE 'UTC') AT TIME ZONE $2, 'YYYY/MM/DD HH:MI AM'), '') AS date_modified,
		COALESCE(t.is_favorite,false), COALESCE(t.position,0), COALESCE(t.priority,0), t.project_id, COALESCE(p.name,''), t.parent_id
		FROM tasks t LEFT JOIN projects p ON t.project_id = p.id `
}

func nonFavSelectSQL() string {
	return `SELECT t.id, t.title, t.description, t.completed,
		TO_CHAR((t.time_stamp AT TIME ZONE 'UTC') AT TIME ZONE $2, 'YYYY/MM/DD HH:MI AM') AS date_added,
		COALESCE(CAST(t.due_date AS TEXT), '') AS due_date,
		TO_CHAR((t.time_stamp AT TIME ZONE 'UTC') AT TIME ZONE $2, 'YYYY/MM/DD HH:MI AM') AS date_created,
		COALESCE(TO_CHAR((t.date_modified AT TIME ZONE 'UTC') AT TIME ZONE $2, 'YYYY/MM/DD HH:MI AM'), '') AS date_modified,
		COALESCE(t.position,0), COALESCE(t.priority,0), t.project_id, COALESCE(p.name,''), t.parent_id
		FROM tasks t LEFT JOIN projects p ON t.project_id = p.id `
}

func ReturnPaginationForUserWithFilters(page, pageSize int, userID *int, timezone string, filters ListFilters) ([]Task, int, error) {
	pool, err := storage.OpenDatabase()
	if err != nil {
		return nil, 0, err
	}
	defer storage.CloseDatabase(pool)

	if userID == nil {
		return []Task{}, 0, nil
	}

	taskSelect := taskSelectSQL()
	nonFavSelect := nonFavSelectSQL()

	visT := storage.TaskListVisibleCondition("t", "$1", filters.ProjectFilter)
	favArgs := []interface{}{*userID, timezone}
	favWhere := "WHERE " + visT + " AND t.is_favorite = true" + filters.rootOnlySQL("t")
	favWhere, favArgs = appendFilterSQL(favWhere, favArgs, filters, timezone, "t", *userID)
	favRows, err := pool.Query(context.Background(), taskSelect+favWhere+filters.orderByClause("t"), favArgs...)
	if err != nil {
		return nil, 0, err
	}
	defer favRows.Close()

	favs := make([]Task, 0)
	for favRows.Next() {
		task, err := scanFavoriteTaskRow(favRows)
		if err != nil {
			return nil, 0, err
		}
		favs = append(favs, task)
	}

	countArgs := []interface{}{*userID}
	var countWhere string
	if filters.SprintFilter != nil {
		countWhere = "WHERE " + storage.TaskListVisibleCondition("", "$1", filters.ProjectFilter)
	} else {
		// Include favorites in the total so a starred incomplete root is not
		// reported as 0 tasks (favorites are extra rows on page 1, but they
		// still belong in the matching-task count and empty-state check).
		countWhere = "WHERE " + storage.TaskListVisibleCondition("", "$1", filters.ProjectFilter) + filters.rootOnlySQL("")
	}
	countWhere, countArgs = appendFilterSQL(countWhere, countArgs, filters, timezone, "", *userID)
	var totalTasks int
	if err := pool.QueryRow(context.Background(), "SELECT COUNT(*) FROM tasks "+countWhere, countArgs...).Scan(&totalTasks); err != nil {
		return nil, 0, err
	}

	offset := (page - 1) * pageSize
	if offset < 0 {
		offset = 0
	}

	nonFavArgs := []interface{}{pageSize, timezone, *userID, offset}
	nonFavWhere := "WHERE " + storage.TaskListVisibleCondition("t", "$3", filters.ProjectFilter) + " AND (t.is_favorite IS NULL OR t.is_favorite = false)" + filters.rootOnlySQL("t")
	nonFavWhere, nonFavArgs = appendFilterSQL(nonFavWhere, nonFavArgs, filters, timezone, "t", *userID)
	rows, err := pool.Query(
		context.Background(),
		nonFavSelect+nonFavWhere+filters.orderByClause("t")+" LIMIT $1 OFFSET $4",
		nonFavArgs...,
	)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	nonFavs := make([]Task, 0)
	for rows.Next() {
		task, err := scanTaskRow(rows)
		if err != nil {
			return nil, 0, err
		}
		nonFavs = append(nonFavs, task)
	}

	taskList := nonFavs
	if page == 1 && len(favs) > 0 {
		taskList = append(favs, nonFavs...)
	}
	if err := attachTagsToTasks(taskList); err != nil {
		return nil, 0, err
	}
	if filters.IncludeSubtasks {
		if err := attachParentTitles(taskList); err != nil {
			return nil, 0, err
		}
		return taskList, totalTasks, nil
	}
	if err := attachChildrenToRoots(taskList, timezone, filters.SprintFilter, IsRemovedTagFilter(filters)); err != nil {
		return nil, 0, err
	}
	if page == 1 {
		before := len(taskList)
		taskList, err = appendOrphanMatchingSubtasks(taskList, *userID, timezone, filters, "")
		if err != nil {
			return nil, 0, err
		}
		if filters.SprintFilter == nil {
			totalTasks += len(taskList) - before
		}
	}
	return taskList, totalTasks, nil
}

func SearchTasksForUserWithFilters(page, pageSize int, searchQuery string, userID *int, timezone string, filters ListFilters) ([]Task, int, error) {
	pool, err := storage.OpenDatabase()
	if err != nil {
		return nil, 0, err
	}
	defer storage.CloseDatabase(pool)

	if userID == nil {
		return []Task{}, 0, nil
	}

	offset := (page - 1) * pageSize
	searchPattern := "%" + searchQuery + "%"
	searchID, hasSearchID := parseSearchTaskID(searchQuery)

	countArgs := []interface{}{searchPattern, *userID}
	countIDIdx := 0
	if hasSearchID {
		countArgs = append(countArgs, searchID)
		countIDIdx = len(countArgs)
	}
	countWhere := "WHERE " + storage.TaskListVisibleCondition("", "$2", filters.ProjectFilter)
	if filters.SprintFilter == nil {
		countWhere += filters.rootOnlySQL("")
	}
	countWhere += " AND (" + searchMatchClause("", countIDIdx) + " OR EXISTS (SELECT 1 FROM tasks c WHERE c.parent_id = id AND " + childSearchClause(countIDIdx) + "))"
	countWhere, countArgs = appendFilterSQL(countWhere, countArgs, filters, timezone, "", *userID)
	var totalTasks int
	if err := pool.QueryRow(context.Background(), "SELECT COUNT(*) FROM tasks "+countWhere, countArgs...).Scan(&totalTasks); err != nil {
		return nil, 0, err
	}

	selectArgs := []interface{}{searchPattern, timezone, pageSize, *userID, offset}
	selectIDIdx := 0
	if hasSearchID {
		selectArgs = append(selectArgs, searchID)
		selectIDIdx = len(selectArgs)
	}
	selectWhere := "WHERE " + storage.TaskListVisibleCondition("t", "$4", filters.ProjectFilter) + filters.rootOnlySQL("t") +
		" AND (" + searchMatchClause("t", selectIDIdx) + " OR EXISTS (SELECT 1 FROM tasks c WHERE c.parent_id = t.id AND " + childSearchClause(selectIDIdx) + "))"
	selectWhere, selectArgs = appendFilterSQL(selectWhere, selectArgs, filters, timezone, "t", *userID)

	query := taskSelectSQL() + selectWhere + filters.orderByClause("t") + " LIMIT $3 OFFSET $5"

	rows, err := pool.Query(context.Background(), query, selectArgs...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	taskList := make([]Task, 0)
	for rows.Next() {
		task, err := scanFavoriteTaskRow(rows)
		if err != nil {
			return nil, 0, err
		}
		taskList = append(taskList, task)
	}
	if err := attachTagsToTasks(taskList); err != nil {
		return nil, 0, err
	}
	if filters.IncludeSubtasks {
		if err := attachParentTitles(taskList); err != nil {
			return nil, 0, err
		}
		return taskList, totalTasks, nil
	}
	if err := attachChildrenToRoots(taskList, timezone, filters.SprintFilter, IsRemovedTagFilter(filters)); err != nil {
		return nil, 0, err
	}
	if page == 1 {
		before := len(taskList)
		taskList, err = appendOrphanMatchingSubtasks(taskList, *userID, timezone, filters, searchQuery)
		if err != nil {
			return nil, 0, err
		}
		if filters.SprintFilter == nil {
			totalTasks += len(taskList) - before
		}
	}
	return taskList, totalTasks, nil
}

func appendFilterSQL(where string, args []interface{}, filters ListFilters, timezone, tablePrefix string, userID int) (string, []interface{}) {
	where += filters.projectCondition(tablePrefix)
	where += filters.statusCondition(tablePrefix)
	where, args = appendDueDateCondition(where, args, filters.DueFilter, timezone, tablePrefix)
	where, args = appendCompletedWeekCondition(where, args, filters.CompletedFilter, timezone, tablePrefix)
	where += filters.priorityCondition(tablePrefix)
	where += filters.sprintCondition(tablePrefix)
	where, args = appendTagCondition(where, args, filters, userID, tablePrefix)
	where = appendArchivedExclusion(where, filters, tablePrefix)
	if strings.ToLower(strings.TrimSpace(filters.WorkflowClaimScope)) == "mine" {
		args = append(args, userID)
		where += filters.workflowClaimCondition(tablePrefix, len(args))
	}
	return where, args
}

func parseSearchTaskID(query string) (int, bool) {
	s := strings.TrimSpace(query)
	s = strings.TrimSpace(strings.TrimPrefix(s, "#"))
	if s == "" {
		return 0, false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return 0, false
		}
	}
	id, err := strconv.Atoi(s)
	if err != nil || id <= 0 {
		return 0, false
	}
	return id, true
}

func searchMatchClause(tablePrefix string, idArgIndex int) string {
	prefix := ""
	idCol := "id"
	if tablePrefix != "" {
		prefix = tablePrefix + "."
		idCol = tablePrefix + ".id"
	}
	clause := fmt.Sprintf(`(%stitle ILIKE $1 OR %sdescription ILIKE $1 OR EXISTS (
		SELECT 1 FROM task_tags tt JOIN tags tg ON tt.tag_id = tg.id
		WHERE tt.task_id = %s AND tg.name ILIKE $1))`, prefix, prefix, idCol)
	if idArgIndex <= 0 {
		return clause
	}
	return fmt.Sprintf(`(%s OR %s = $%d)`, clause, idCol, idArgIndex)
}

func childSearchClause(idArgIndex int) string {
	clause := `(c.title ILIKE $1 OR c.description ILIKE $1 OR EXISTS (
		SELECT 1 FROM task_tags tt JOIN tags tg ON tt.tag_id = tg.id
		WHERE tt.task_id = c.id AND tg.name ILIKE $1))`
	if idArgIndex <= 0 {
		return clause
	}
	return fmt.Sprintf(`(%s OR c.id = $%d)`, clause, idArgIndex)
}

func appendTagCondition(where string, args []interface{}, filters ListFilters, userID int, tablePrefix string) (string, []interface{}) {
	idCol := "id"
	if tablePrefix != "" {
		idCol = tablePrefix + ".id"
	}
	if name := strings.TrimSpace(filters.TagNameFilter); name != "" {
		args = append(args, name, userID)
		nameIdx := len(args) - 1
		userIdx := len(args)
		tagMatchClause := fmt.Sprintf("LOWER(tg.name) = LOWER($%d)", nameIdx)
		if storage.IsSystemTagName(name) {
			tagMatchClause = fmt.Sprintf("(LOWER(tg.name) = LOWER($%d) OR LOWER(tg.name) IN ('archived', 'removed'))", nameIdx)
		}
		where += fmt.Sprintf(` AND %s IN (
			SELECT tt.task_id FROM task_tags tt
			JOIN tags tg ON tg.id = tt.tag_id
			WHERE %s
			  AND (
				(tg.project_id IS NULL AND tg.user_id = $%d)
				OR EXISTS (
					SELECT 1 FROM projects p
					LEFT JOIN project_members pm ON pm.project_id = p.id AND pm.user_id = $%d
					WHERE p.id = tg.project_id AND (p.user_id = $%d OR pm.user_id IS NOT NULL)
				)
			  )
		)`, idCol, tagMatchClause, userIdx, userIdx, userIdx)
		return where, args
	}
	if filters.TagFilter == nil {
		return where, args
	}
	args = append(args, *filters.TagFilter)
	idx := len(args)
	where += fmt.Sprintf(" AND %s IN (SELECT task_id FROM task_tags WHERE tag_id = $%d)", idCol, idx)
	return where, args
}

// IsArchivedTagFilter reports whether the filters specify the protected archived (or legacy removed) tag.
func IsArchivedTagFilter(filters ListFilters) bool {
	name := strings.TrimSpace(filters.TagNameFilter)
	if storage.IsArchivedTagName(name) || storage.IsRemovedTagName(name) {
		return true
	}
	if filters.TagFilter == nil {
		return false
	}
	tag, err := storage.GetTag(*filters.TagFilter)
	return err == nil && tag != nil && (storage.IsArchivedTagName(tag.Name) || storage.IsRemovedTagName(tag.Name))
}

// IsRemovedTagFilter delegates to IsArchivedTagFilter for backward compatibility.
func IsRemovedTagFilter(filters ListFilters) bool {
	return IsArchivedTagFilter(filters)
}

func appendArchivedExclusion(where string, filters ListFilters, tablePrefix string) string {
	if IsArchivedTagFilter(filters) {
		return where
	}
	idCol := "tasks.id"
	if tablePrefix != "" {
		idCol = tablePrefix + ".id"
	}
	return where + " AND NOT " + storage.ArchivedTaskExistsSQL(idCol)
}

func attachTagsToTasks(taskList []Task) error {
	if len(taskList) == 0 {
		return nil
	}
	ids := make([]int, len(taskList))
	for i, t := range taskList {
		ids[i] = t.ID
	}
	tagMap, err := storage.GetTagsForTasks(ids)
	if err != nil {
		return err
	}
	for i := range taskList {
		if tags, ok := tagMap[taskList[i].ID]; ok {
			taskList[i].Tags = make([]Tag, len(tags))
			for j, tg := range tags {
				taskList[i].Tags[j] = Tag{ID: tg.ID, Name: tg.Name, Color: tg.Color, ProjectID: tg.ProjectID, Protected: tg.Protected}
			}
		}
	}
	return attachWorkflowFieldsToTasks(taskList)
}

func attachWorkflowFieldsToTasks(taskList []Task) error {
	if len(taskList) == 0 {
		return nil
	}
	ids := make([]int, 0, len(taskList))
	for _, t := range taskList {
		ids = append(ids, t.ID)
		for _, c := range t.Children {
			ids = append(ids, c.ID)
		}
	}
	fields, err := storage.GetWorkflowFieldsForTasks(ids)
	if err != nil {
		return err
	}
	applyWorkflow := func(t *Task) {
		if f, ok := fields[t.ID]; ok {
			t.StatusID = f.StatusID
			t.StatusName = f.StatusName
			t.EstimatePoints = f.EstimatePoints
			t.TimeSpentMinutes = f.TimeSpentMinutes
			t.ProjectWorkflow = f.ProjectWorkflow
			t.ClaimedBy = f.ClaimedBy
			t.ClaimedByName = f.ClaimedByName
			t.SprintID = f.SprintID
			t.SprintName = f.SprintName
		}
	}
	for i := range taskList {
		applyWorkflow(&taskList[i])
		for j := range taskList[i].Children {
			applyWorkflow(&taskList[i].Children[j])
		}
	}
	return attachGitHubFieldsToTasks(taskList)
}

func attachGitHubFieldsToTasks(taskList []Task) error {
	if len(taskList) == 0 {
		return nil
	}
	ids := make([]int, 0, len(taskList))
	for _, t := range taskList {
		ids = append(ids, t.ID)
		for _, c := range t.Children {
			ids = append(ids, c.ID)
		}
	}
	issues, err := storage.GetGitHubIssuesForTasks(ids)
	if err != nil {
		return err
	}
	applyGitHub := func(t *Task) {
		if issue, ok := issues[t.ID]; ok {
			t.GitHubIssueNumber = issue.IssueNumber
			t.GitHubIssueID = issue.IssueID
			t.GitHubIssueURL = issue.IssueURL
			t.GitHubIssueState = issue.IssueState
			t.GitHubIssueTitle = issue.IssueTitle
			t.GitHubLastSyncError = issue.LastSyncError
		}
	}
	for i := range taskList {
		applyGitHub(&taskList[i])
		for j := range taskList[i].Children {
			applyGitHub(&taskList[i].Children[j])
		}
	}
	return nil
}

func scanFavoriteTaskRow(rows interface {
	Scan(...interface{}) error
}) (Task, error) {
	var task Task
	var pid sql.NullInt64
	var pname sql.NullString
	var parentID sql.NullInt64
	if err := rows.Scan(
		&task.ID, &task.Title, &task.Description, &task.Completed,
		&task.DateAdded, &task.DueDate, &task.DateCreated, &task.DateModified,
		&task.IsFavorite, &task.Position, &task.Priority, &pid, &pname, &parentID,
	); err != nil {
		return task, err
	}
	if pid.Valid {
		task.ProjectID = int(pid.Int64)
	}
	task.ProjectName = pname.String
	if parentID.Valid {
		task.ParentID = int(parentID.Int64)
	}
	return task, nil
}

func scanTaskRow(rows interface {
	Scan(...interface{}) error
}) (Task, error) {
	var task Task
	var pid sql.NullInt64
	var pname sql.NullString
	var parentID sql.NullInt64
	if err := rows.Scan(
		&task.ID, &task.Title, &task.Description, &task.Completed,
		&task.DateAdded, &task.DueDate, &task.DateCreated, &task.DateModified,
		&task.Position, &task.Priority, &pid, &pname, &parentID,
	); err != nil {
		return task, err
	}
	if pid.Valid {
		task.ProjectID = int(pid.Int64)
	}
	task.ProjectName = pname.String
	if parentID.Valid {
		task.ParentID = int(parentID.Int64)
	}
	return task, nil
}

// attachChildrenToRoots loads direct children for the given root tasks.
// When sprintFilter is set, every child still counts toward child_count, but
// only children on that sprint (or the backlog when filter is 0) are nested.
func attachChildrenToRoots(roots []Task, timezone string, sprintFilter *int, includeRemoved ...bool) error {
	if len(roots) == 0 {
		return nil
	}
	pool, err := storage.OpenDatabase()
	if err != nil {
		return err
	}
	defer storage.CloseDatabase(pool)

	ids := make([]int, len(roots))
	indexByID := make(map[int]int, len(roots))
	for i, t := range roots {
		ids[i] = t.ID
		indexByID[t.ID] = i
		roots[i].Children = []Task{}
	}

	notArchived := ""
	if len(includeRemoved) == 0 || !includeRemoved[0] {
		notArchived = " AND NOT " + storage.ArchivedTaskExistsSQL("t.id")
	}

	rows, err := pool.Query(context.Background(),
		`SELECT t.id, t.title, t.description, t.completed,
			TO_CHAR((t.time_stamp AT TIME ZONE 'UTC') AT TIME ZONE $2, 'YYYY/MM/DD HH:MI AM') AS date_added,
			COALESCE(CAST(t.due_date AS TEXT), '') AS due_date,
			TO_CHAR((t.time_stamp AT TIME ZONE 'UTC') AT TIME ZONE $2, 'YYYY/MM/DD HH:MI AM') AS date_created,
			COALESCE(TO_CHAR((t.date_modified AT TIME ZONE 'UTC') AT TIME ZONE $2, 'YYYY/MM/DD HH:MI AM'), '') AS date_modified,
			COALESCE(t.is_favorite,false), COALESCE(t.position,0), COALESCE(t.priority,0), t.project_id, COALESCE(p.name,''), t.parent_id
		 FROM tasks t LEFT JOIN projects p ON t.project_id = p.id
		 WHERE t.parent_id = ANY($1)`+notArchived+`
		 ORDER BY t.position ASC, t.id ASC`,
		ids, timezone)
	if err != nil {
		return err
	}
	defer rows.Close()

	children := make([]Task, 0)
	for rows.Next() {
		child, err := scanFavoriteTaskRow(rows)
		if err != nil {
			return err
		}
		children = append(children, child)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if err := attachTagsToTasks(children); err != nil {
		return err
	}
	for _, child := range children {
		idx, ok := indexByID[child.ParentID]
		if !ok {
			continue
		}
		child.ParentTitle = roots[idx].Title
		roots[idx].ChildCount++
		if child.Completed {
			roots[idx].ChildrenCompleted++
		}
		if !matchesSprintFilter(child.SprintID, sprintFilter) {
			continue
		}
		roots[idx].Children = append(roots[idx].Children, child)
	}
	return nil
}

// appendOrphanMatchingSubtasks adds subtasks that match the current filters
// whose parent does not, so Home/dashboard lists can show leftover children
// after a parent is completed or otherwise excluded.
func appendOrphanMatchingSubtasks(roots []Task, userID int, timezone string, filters ListFilters, searchQuery string) ([]Task, error) {
	if !filters.shouldAppendOrphanSubtasks() {
		return roots, nil
	}
	pool, err := storage.OpenDatabase()
	if err != nil {
		return nil, err
	}
	defer storage.CloseDatabase(pool)

	seen := make(map[int]struct{}, len(roots))
	exclude := make([]int, 0, len(roots))
	for _, t := range roots {
		if _, ok := seen[t.ID]; !ok {
			seen[t.ID] = struct{}{}
			exclude = append(exclude, t.ID)
		}
		for _, c := range t.Children {
			if _, ok := seen[c.ID]; !ok {
				seen[c.ID] = struct{}{}
				exclude = append(exclude, c.ID)
			}
		}
	}

	args := []interface{}{userID, timezone}
	where := "WHERE " + storage.TaskListVisibleCondition("t", "$1", filters.ProjectFilter) + " AND t.parent_id IS NOT NULL"
	where, args = appendFilterSQL(where, args, filters, timezone, "t", userID)
	if strings.TrimSpace(searchQuery) != "" {
		searchPattern := "%" + strings.TrimSpace(searchQuery) + "%"
		searchID, hasSearchID := parseSearchTaskID(searchQuery)
		args = append(args, searchPattern)
		searchIdx := len(args)
		searchIDIdx := 0
		if hasSearchID {
			args = append(args, searchID)
			searchIDIdx = len(args)
		}
		matchClause := fmt.Sprintf(`(t.title ILIKE $%d OR t.description ILIKE $%d OR EXISTS (
			SELECT 1 FROM task_tags tt JOIN tags tg ON tt.tag_id = tg.id
			WHERE tt.task_id = t.id AND tg.name ILIKE $%d))`, searchIdx, searchIdx, searchIdx)
		if searchIDIdx > 0 {
			matchClause = fmt.Sprintf(`(%s OR t.id = $%d)`, matchClause, searchIDIdx)
		}
		where += " AND " + matchClause
	}
	if len(exclude) > 0 {
		args = append(args, exclude)
		where += fmt.Sprintf(" AND NOT (t.id = ANY($%d))", len(args))
	}

	rows, err := pool.Query(context.Background(), taskSelectSQL()+where+filters.orderByClause("t"), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	extras := make([]Task, 0)
	for rows.Next() {
		child, err := scanFavoriteTaskRow(rows)
		if err != nil {
			return nil, err
		}
		if _, ok := seen[child.ID]; ok {
			continue
		}
		extras = append(extras, child)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(extras) == 0 {
		return roots, nil
	}
	if err := attachTagsToTasks(extras); err != nil {
		return nil, err
	}
	if err := attachParentTitles(extras); err != nil {
		return nil, err
	}
	return append(roots, extras...), nil
}

func attachParentTitles(list []Task) error {
	parentIDs := make([]int, 0)
	seen := make(map[int]struct{})
	for _, t := range list {
		if t.ParentID <= 0 {
			continue
		}
		if _, ok := seen[t.ParentID]; ok {
			continue
		}
		seen[t.ParentID] = struct{}{}
		parentIDs = append(parentIDs, t.ParentID)
	}
	if len(parentIDs) == 0 {
		return nil
	}

	pool, err := storage.OpenDatabase()
	if err != nil {
		return err
	}
	defer storage.CloseDatabase(pool)

	titles := map[int]string{}
	titleRows, err := pool.Query(context.Background(), `SELECT id, title FROM tasks WHERE id = ANY($1)`, parentIDs)
	if err != nil {
		return err
	}
	defer titleRows.Close()
	for titleRows.Next() {
		var id int
		var title string
		if err := titleRows.Scan(&id, &title); err != nil {
			return err
		}
		titles[id] = title
	}
	if err := titleRows.Err(); err != nil {
		return err
	}
	for i := range list {
		if title, ok := titles[list[i].ParentID]; ok {
			list[i].ParentTitle = title
		}
	}
	return nil
}

func ReturnPaginationForUserWithProject(page, pageSize int, userID *int, timezone string, projectFilter *int) ([]Task, int, error) {
	return ReturnPaginationForUserWithFilters(page, pageSize, userID, timezone, ListFilters{ProjectFilter: projectFilter})
}

func ReturnPaginationForUser(page, pageSize int, userID *int, timezone string) ([]Task, int, error) {
	return ReturnPaginationForUserWithFilters(page, pageSize, userID, timezone, ListFilters{})
}

func SearchTasksForUserWithStatus(page, pageSize int, searchQuery string, userID *int, timezone string, statusFilter string) ([]Task, int, error) {
	return SearchTasksForUserWithFilters(page, pageSize, searchQuery, userID, timezone, ListFilters{StatusFilter: statusFilter})
}

func SearchTasksForUser(page, pageSize int, searchQuery string, userID *int, timezone string) ([]Task, int, error) {
	return SearchTasksForUserWithFilters(page, pageSize, searchQuery, userID, timezone, ListFilters{})
}

func SearchTasks(page, pageSize int, searchQuery string) ([]Task, int, error) {
	return SearchTasksForUser(page, pageSize, searchQuery, nil, "America/New_York")
}

func ReturnPagination(page, pageSize int) ([]Task, int, error) {
	return ReturnPaginationForUser(page, pageSize, nil, "America/New_York")
}

func statusCondition(statusFilter string) string {
	return ListFilters{StatusFilter: statusFilter}.statusCondition("")
}

// FetchTaskByIDForUser loads a single task row for display in the task list.
func FetchTaskByIDForUser(taskID, userID int, timezone string, page int) (Task, error) {
	pool, err := storage.OpenDatabase()
	if err != nil {
		return Task{}, err
	}
	defer storage.CloseDatabase(pool)

	var task Task
	var projectID sql.NullInt64
	var projectName sql.NullString
	var parentID sql.NullInt64
	err = pool.QueryRow(context.Background(),
		`SELECT t.id, t.title, t.description, t.completed,
			TO_CHAR((t.time_stamp AT TIME ZONE 'UTC') AT TIME ZONE $3, 'YYYY/MM/DD HH:MI AM') AS date_added,
			COALESCE(CAST(t.due_date AS TEXT), '') AS due_date,
			TO_CHAR((t.time_stamp AT TIME ZONE 'UTC') AT TIME ZONE $3, 'YYYY/MM/DD HH:MI AM') AS date_created,
			COALESCE(TO_CHAR((t.date_modified AT TIME ZONE 'UTC') AT TIME ZONE $3, 'YYYY/MM/DD HH:MI AM'), '') AS date_modified,
			COALESCE(t.is_favorite,false), COALESCE(t.position,0), COALESCE(t.priority,0), t.project_id, COALESCE(p.name,''), t.parent_id
		FROM tasks t LEFT JOIN projects p ON t.project_id = p.id
		WHERE t.id = $1 AND `+storage.TaskVisibleCondition("t", "$2"), taskID, userID, timezone).Scan(
		&task.ID, &task.Title, &task.Description, &task.Completed,
		&task.DateAdded, &task.DueDate, &task.DateCreated, &task.DateModified,
		&task.IsFavorite, &task.Position, &task.Priority, &projectID, &projectName, &parentID)
	if err != nil {
		return Task{}, err
	}
	if projectID.Valid {
		task.ProjectID = int(projectID.Int64)
	}
	task.ProjectName = projectName.String
	if parentID.Valid {
		task.ParentID = int(parentID.Int64)
	}
	task.Page = page
	taskList := []Task{task}
	if err := attachTagsToTasks(taskList); err != nil {
		return Task{}, err
	}
	if err := attachChildrenToRoots(taskList, timezone, nil); err != nil {
		return Task{}, err
	}
	return taskList[0], nil
}

// TaskMatchesFilters reports whether a task satisfies the active list filters.
func TaskMatchesFilters(taskID, userID int, timezone string, filters ListFilters, search string) (bool, error) {
	pool, err := storage.OpenDatabase()
	if err != nil {
		return false, err
	}
	defer storage.CloseDatabase(pool)

	var countWhere string
	var args []interface{}
	vis := storage.TaskListVisibleCondition("", "$3", filters.ProjectFilter)
	visNoSearch := storage.TaskListVisibleCondition("", "$2", filters.ProjectFilter)
	if search != "" {
		searchPattern := "%" + search + "%"
		args = []interface{}{searchPattern, taskID, userID}
		idArgIndex := 0
		if searchID, ok := parseSearchTaskID(search); ok {
			args = append(args, searchID)
			idArgIndex = len(args)
		}
		countWhere = "WHERE id = $2 AND " + vis + " AND " + searchMatchClause("", idArgIndex)
	} else {
		args = []interface{}{taskID, userID}
		countWhere = "WHERE id = $1 AND " + visNoSearch
	}
	countWhere, args = appendFilterSQL(countWhere, args, filters, timezone, "", userID)

	var count int
	err = pool.QueryRow(context.Background(), "SELECT COUNT(*) FROM tasks "+countWhere, args...).Scan(&count)
	if err != nil {
		return false, err
	}
	return count > 0, nil
}
