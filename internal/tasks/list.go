package tasks

import (
	"GoTodo/internal/storage"
	"context"
	"database/sql"
	"fmt"
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
		fmt.Println("Error in ListTasks (query):", err)
		return tasks
	}
	defer rows.Close()

	for rows.Next() {
		var task Task
		err = rows.Scan(&task.ID, &task.Title, &task.Description, &task.Completed)
		if err != nil {
			fmt.Println("Error in ListTasks (scan):", err)
			return tasks
		}
		tasks = append(tasks, task)
	}
	return tasks
}

const nonFavoriteCond = " AND (is_favorite IS NULL OR is_favorite = false)"

func ReturnPaginationForUserWithFilters(page, pageSize int, userID *int, timezone string, filters ListFilters) ([]Task, int, error) {
	pool, err := storage.OpenDatabase()
	if err != nil {
		return nil, 0, err
	}
	defer storage.CloseDatabase(pool)

	if userID == nil {
		return []Task{}, 0, nil
	}

	taskSelect := `SELECT t.id, t.title, t.description, t.completed,
		TO_CHAR((t.time_stamp AT TIME ZONE 'UTC') AT TIME ZONE $2, 'YYYY/MM/DD HH:MI AM') AS date_added,
		COALESCE(CAST(t.due_date AS TEXT), '') AS due_date,
		TO_CHAR((t.time_stamp AT TIME ZONE 'UTC') AT TIME ZONE $2, 'YYYY/MM/DD HH:MI AM') AS date_created,
		COALESCE(TO_CHAR((t.date_modified AT TIME ZONE 'UTC') AT TIME ZONE $2, 'YYYY/MM/DD HH:MI AM'), '') AS date_modified,
		COALESCE(t.is_favorite,false), COALESCE(t.position,0), COALESCE(t.priority,0), t.project_id, COALESCE(p.name,'')
		FROM tasks t LEFT JOIN projects p ON t.project_id = p.id `

	nonFavSelect := `SELECT t.id, t.title, t.description, t.completed,
		TO_CHAR((t.time_stamp AT TIME ZONE 'UTC') AT TIME ZONE $2, 'YYYY/MM/DD HH:MI AM') AS date_added,
		COALESCE(CAST(t.due_date AS TEXT), '') AS due_date,
		TO_CHAR((t.time_stamp AT TIME ZONE 'UTC') AT TIME ZONE $2, 'YYYY/MM/DD HH:MI AM') AS date_created,
		COALESCE(TO_CHAR((t.date_modified AT TIME ZONE 'UTC') AT TIME ZONE $2, 'YYYY/MM/DD HH:MI AM'), '') AS date_modified,
		COALESCE(t.position,0), COALESCE(t.priority,0), t.project_id, COALESCE(p.name,'')
		FROM tasks t LEFT JOIN projects p ON t.project_id = p.id `

	favArgs := []interface{}{*userID, timezone}
	favWhere := "WHERE t.user_id = $1 AND t.is_favorite = true"
	favWhere, favArgs = appendFilterSQL(favWhere, favArgs, filters, timezone, "t")
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
	countWhere := "WHERE user_id = $1" + nonFavoriteCond
	countWhere, countArgs = appendFilterSQL(countWhere, countArgs, filters, timezone, "")
	var totalTasks int
	if err := pool.QueryRow(context.Background(), "SELECT COUNT(*) FROM tasks "+countWhere, countArgs...).Scan(&totalTasks); err != nil {
		return nil, 0, err
	}

	offset := (page - 1) * pageSize
	if offset < 0 {
		offset = 0
	}

	nonFavArgs := []interface{}{pageSize, timezone, *userID, offset}
	nonFavWhere := "WHERE t.user_id = $3 AND (t.is_favorite IS NULL OR t.is_favorite = false)"
	nonFavWhere, nonFavArgs = appendFilterSQL(nonFavWhere, nonFavArgs, filters, timezone, "t")
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

	tasks := nonFavs
	if page == 1 && len(favs) > 0 {
		tasks = append(favs, nonFavs...)
	}
	return tasks, totalTasks, nil
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

	countArgs := []interface{}{searchPattern, *userID}
	countWhere := "WHERE (title ILIKE $1 OR description ILIKE $1) AND user_id = $2"
	countWhere, countArgs = appendFilterSQL(countWhere, countArgs, filters, timezone, "")
	var totalTasks int
	if err := pool.QueryRow(context.Background(), "SELECT COUNT(*) FROM tasks "+countWhere, countArgs...).Scan(&totalTasks); err != nil {
		return nil, 0, err
	}

	selectArgs := []interface{}{searchPattern, timezone, pageSize, *userID, offset}
	selectWhere := "WHERE (t.title ILIKE $1 OR t.description ILIKE $1) AND t.user_id = $4"
	selectWhere, selectArgs = appendFilterSQL(selectWhere, selectArgs, filters, timezone, "t")

	query := `SELECT t.id, t.title, t.description, t.completed,
		TO_CHAR((t.time_stamp AT TIME ZONE 'UTC') AT TIME ZONE $2, 'YYYY/MM/DD HH:MM AM') as date_added,
		COALESCE(CAST(t.due_date AS TEXT), '') AS due_date,
		TO_CHAR((t.time_stamp AT TIME ZONE 'UTC') AT TIME ZONE $2, 'YYYY/MM/DD HH:MM AM') AS date_created,
		COALESCE(TO_CHAR((t.date_modified AT TIME ZONE 'UTC') AT TIME ZONE $2, 'YYYY/MM/DD HH:MM AM'), '') AS date_modified,
		COALESCE(t.position,0), COALESCE(t.priority,0), t.project_id, COALESCE(p.name,'')
		FROM tasks t LEFT JOIN projects p ON t.project_id = p.id ` + selectWhere + filters.orderByClause("t") + " LIMIT $3 OFFSET $5"

	rows, err := pool.Query(context.Background(), query, selectArgs...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	tasks := make([]Task, 0)
	for rows.Next() {
		task, err := scanTaskRow(rows)
		if err != nil {
			return nil, 0, err
		}
		tasks = append(tasks, task)
	}
	return tasks, totalTasks, nil
}

func appendFilterSQL(where string, args []interface{}, filters ListFilters, timezone, tablePrefix string) (string, []interface{}) {
	where += filters.projectCondition(tablePrefix)
	where += filters.statusCondition(tablePrefix)
	where, args = appendDueDateCondition(where, args, filters.DueFilter, timezone, tablePrefix)
	where += filters.priorityCondition(tablePrefix)
	return where, args
}

func scanFavoriteTaskRow(rows interface {
	Scan(...interface{}) error
}) (Task, error) {
	var task Task
	var pid sql.NullInt64
	var pname sql.NullString
	if err := rows.Scan(
		&task.ID, &task.Title, &task.Description, &task.Completed,
		&task.DateAdded, &task.DueDate, &task.DateCreated, &task.DateModified,
		&task.IsFavorite, &task.Position, &task.Priority, &pid, &pname,
	); err != nil {
		return task, err
	}
	if pid.Valid {
		task.ProjectID = int(pid.Int64)
	}
	task.ProjectName = pname.String
	return task, nil
}

func scanTaskRow(rows interface {
	Scan(...interface{}) error
}) (Task, error) {
	var task Task
	var pid sql.NullInt64
	var pname sql.NullString
	if err := rows.Scan(
		&task.ID, &task.Title, &task.Description, &task.Completed,
		&task.DateAdded, &task.DueDate, &task.DateCreated, &task.DateModified,
		&task.Position, &task.Priority, &pid, &pname,
	); err != nil {
		return task, err
	}
	if pid.Valid {
		task.ProjectID = int(pid.Int64)
	}
	task.ProjectName = pname.String
	return task, nil
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
