package tasks

import (
	"GoTodo/internal/storage"
	"context"
	"database/sql"
	"fmt"
)

const ExportMaxTasks = 10000

// FetchAllTasksForUserWithFilters returns all tasks matching filters (up to ExportMaxTasks).
// Includes favorites. When search is non-empty, matches title, description, tag names, and numeric IDs.
func FetchAllTasksForUserWithFilters(userID *int, timezone string, filters ListFilters, search string) ([]Task, error) {
	if userID == nil {
		return []Task{}, nil
	}

	pool, err := storage.OpenDatabase()
	if err != nil {
		return nil, err
	}
	defer storage.CloseDatabase(pool)

	taskSelect := `SELECT t.id, t.title, t.description, t.completed,
		TO_CHAR((t.time_stamp AT TIME ZONE 'UTC') AT TIME ZONE $2, 'YYYY/MM/DD HH:MI AM') AS date_added,
		COALESCE(CAST(t.due_date AS TEXT), '') AS due_date,
		TO_CHAR((t.time_stamp AT TIME ZONE 'UTC') AT TIME ZONE $2, 'YYYY/MM/DD HH:MI AM') AS date_created,
		COALESCE(TO_CHAR((t.date_modified AT TIME ZONE 'UTC') AT TIME ZONE $2, 'YYYY/MM/DD HH:MI AM'), '') AS date_modified,
		COALESCE(t.is_favorite,false), COALESCE(t.position,0), COALESCE(t.priority,0), t.project_id, COALESCE(p.name,'')
		FROM tasks t LEFT JOIN projects p ON t.project_id = p.id `

	var rows interface {
		Close()
		Next() bool
		Scan(...interface{}) error
	}

	if search != "" {
		args := []interface{}{searchPattern(search), timezone, *userID}
		idIdx := 0
		if searchID, ok := parseSearchTaskID(search); ok {
			args = append(args, searchID)
			idIdx = len(args)
		}
		match := searchMatchSQL("t.id", 1, idIdx)
		where := "WHERE " + match + " AND t.user_id = $3"
		where, args = appendFilterSQL(where, args, filters, timezone, "t", *userID)
		query := taskSelect + where + filters.orderByClause("t") + fmt.Sprintf(" LIMIT %d", ExportMaxTasks)
		r, err := pool.Query(context.Background(), query, args...)
		if err != nil {
			return nil, err
		}
		rows = r
	} else {
		args := []interface{}{*userID, timezone}
		where := "WHERE t.user_id = $1"
		where, args = appendFilterSQL(where, args, filters, timezone, "t", *userID)
		query := taskSelect + where + filters.orderByClause("t") + fmt.Sprintf(" LIMIT %d", ExportMaxTasks)
		r, err := pool.Query(context.Background(), query, args...)
		if err != nil {
			return nil, err
		}
		rows = r
	}
	defer rows.Close()

	tasks := make([]Task, 0)
	for rows.Next() {
		task, err := scanExportTaskRow(rows)
		if err != nil {
			return nil, err
		}
		tasks = append(tasks, task)
	}
	if err := attachTagsToTasks(tasks); err != nil {
		return nil, err
	}
	return tasks, nil
}

func scanExportTaskRow(rows interface {
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
