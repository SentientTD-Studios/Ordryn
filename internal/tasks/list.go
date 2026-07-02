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

func ReturnPagination(page, pageSize int) ([]Task, int, error) {
	return ReturnPaginationForUser(page, pageSize, nil, "America/New_York")
}

func ReturnPaginationForUser(page, pageSize int, userID *int, timezone string) ([]Task, int, error) {
	return ReturnPaginationForUserWithFilters(page, pageSize, userID, timezone, nil, "")
}

// ReturnPaginationForUserWithProject behaves like ReturnPaginationForUser but filters tasks by project.
// If projectFilter is nil, all projects are returned. If *projectFilter == 0, only tasks with no project are returned.
func ReturnPaginationForUserWithProject(page, pageSize int, userID *int, timezone string, projectFilter *int) ([]Task, int, error) {
	return ReturnPaginationForUserWithFilters(page, pageSize, userID, timezone, projectFilter, "")
}

// ReturnPaginationForUserWithFilters supports project and completion-state filtering.
// statusFilter accepts "complete" or "incomplete"; any other value returns all statuses.
func ReturnPaginationForUserWithFilters(page, pageSize int, userID *int, timezone string, projectFilter *int, statusFilter string) ([]Task, int, error) {
	pool, err := storage.OpenDatabase()
	if err != nil {
		return nil, 0, err
	}
	defer storage.CloseDatabase(pool)

	var tasks []Task
	offset := (page - 1) * pageSize

	projectCond := ""
	if projectFilter != nil {
		if *projectFilter == 0 {
			projectCond = " AND (project_id IS NULL)"
		} else {
			projectCond = fmt.Sprintf(" AND (project_id = %d)", *projectFilter)
		}
	}

	statusCond := statusCondition(statusFilter)

	// We'll fetch favorites separately so we can always show up to 5 favorites first on page 1
	query := `SELECT t.id, t.title, t.description, t.completed, 
		TO_CHAR((t.time_stamp AT TIME ZONE 'UTC') AT TIME ZONE $3, 'YYYY/MM/DD HH:MI AM') AS date_added,
		COALESCE(CAST(t.due_date AS TEXT), '') AS due_date,
		TO_CHAR((t.time_stamp AT TIME ZONE 'UTC') AT TIME ZONE $3, 'YYYY/MM/DD HH:MI AM') AS date_created,
		COALESCE(TO_CHAR((t.date_modified AT TIME ZONE 'UTC') AT TIME ZONE $3, 'YYYY/MM/DD HH:MI AM'), '') AS date_modified,
		COALESCE(t.position,0), t.project_id, COALESCE(p.name,'')
		FROM tasks t LEFT JOIN projects p ON t.project_id = p.id `

	var countQuery string
	var rows interface {
		Next() bool
		Scan(...interface{}) error
		Close()
	}

	if userID == nil {
		// Not logged in - don't show any tasks
		return tasks, 0, nil
	}

	// Logged in - show favorites first (up to 5) on page 1
	// Fetch favorite tasks
	favRows, err := pool.Query(context.Background(), `SELECT t.id, t.title, t.description, t.completed, TO_CHAR((t.time_stamp AT TIME ZONE 'UTC') AT TIME ZONE $2, 'YYYY/MM/DD HH:MI AM') AS date_added, COALESCE(CAST(t.due_date AS TEXT), '') AS due_date, TO_CHAR((t.time_stamp AT TIME ZONE 'UTC') AT TIME ZONE $2, 'YYYY/MM/DD HH:MI AM') AS date_created, COALESCE(TO_CHAR((t.date_modified AT TIME ZONE 'UTC') AT TIME ZONE $2, 'YYYY/MM/DD HH:MI AM'), '') AS date_modified, COALESCE(t.is_favorite,false), COALESCE(t.position,0), t.project_id, COALESCE(p.name,'') FROM tasks t LEFT JOIN projects p ON t.project_id = p.id WHERE t.user_id = $1 AND t.is_favorite = true`+projectCond+statusCond+` ORDER BY t.position`, *userID, timezone)
	if err != nil {
		return nil, 0, err
	}
	defer favRows.Close()
	favs := make([]Task, 0)
	for favRows.Next() {
		var t Task
		var pid sql.NullInt64
		var pname sql.NullString
		if err := favRows.Scan(&t.ID, &t.Title, &t.Description, &t.Completed, &t.DateAdded, &t.DueDate, &t.DateCreated, &t.DateModified, &t.IsFavorite, &t.Position, &pid, &pname); err != nil {
			return nil, 0, err
		}
		if pid.Valid {
			t.ProjectID = int(pid.Int64)
		} else {
			t.ProjectID = 0
		}
		t.ProjectName = pname.String
		favs = append(favs, t)
	}

	// Count total tasks
	var totalTasks int
	countQuery = "SELECT COUNT(*) FROM tasks WHERE user_id = $1" + projectCond + statusCond
	err = pool.QueryRow(context.Background(), countQuery, *userID).Scan(&totalTasks)
	if err != nil {
		return nil, 0, err
	}

	// Calculate how many favorites to show on this page
	favCount := len(favs)
	if page == 1 && favCount > 0 {
		// Need to fetch non-favorites to fill remaining slots on page 1
		remaining := pageSize - favCount
		if remaining < 0 {
			remaining = 0
		}
		rows, err = pool.Query(context.Background(), query+`WHERE t.user_id = $4 AND (t.is_favorite IS NULL OR t.is_favorite = false)`+projectCond+statusCond+` ORDER BY t.position LIMIT $1 OFFSET $2`, remaining, 0, timezone, *userID)
		if err != nil {
			return nil, 0, err
		}
		defer rows.Close()
		for rows.Next() {
			var task Task
			var pid sql.NullInt64
			var pname sql.NullString
			if err := rows.Scan(&task.ID, &task.Title, &task.Description, &task.Completed, &task.DateAdded, &task.DueDate, &task.DateCreated, &task.DateModified, &task.Position, &pid, &pname); err != nil {
				return nil, 0, err
			}
			if pid.Valid {
				task.ProjectID = int(pid.Int64)
			} else {
				task.ProjectID = 0
			}
			task.ProjectName = pname.String
			tasks = append(tasks, task)
		}
		// prepend favorites
		tasks = append(favs, tasks...)
	} else {
		// For pages >1, skip favorites in the offset calculation
		// Compute offset among non-favorite items
		offsetNonFav := offset - favCount
		if offsetNonFav < 0 {
			offsetNonFav = 0
		}
		rows, err = pool.Query(context.Background(), query+`WHERE t.user_id = $4 AND (t.is_favorite IS NULL OR t.is_favorite = false)`+projectCond+statusCond+` ORDER BY t.position LIMIT $1 OFFSET $2`, pageSize, offsetNonFav, timezone, *userID)
		if err != nil {
			return nil, 0, err
		}
		defer rows.Close()
		for rows.Next() {
			var task Task
			var pid sql.NullInt64
			var pname sql.NullString
			if err := rows.Scan(&task.ID, &task.Title, &task.Description, &task.Completed, &task.DateAdded, &task.DueDate, &task.DateCreated, &task.DateModified, &task.Position, &pid, &pname); err != nil {
				return nil, 0, err
			}
			if pid.Valid {
				task.ProjectID = int(pid.Int64)
			} else {
				task.ProjectID = 0
			}
			task.ProjectName = pname.String
			tasks = append(tasks, task)
		}
	}
	return tasks, totalTasks, nil
}

func SearchTasks(page, pageSize int, searchQuery string) ([]Task, int, error) {
	return SearchTasksForUser(page, pageSize, searchQuery, nil, "America/New_York")
}

func SearchTasksForUser(page, pageSize int, searchQuery string, userID *int, timezone string) ([]Task, int, error) {
	return SearchTasksForUserWithStatus(page, pageSize, searchQuery, userID, timezone, "")
}

func SearchTasksForUserWithStatus(page, pageSize int, searchQuery string, userID *int, timezone string, statusFilter string) ([]Task, int, error) {
	return SearchTasksForUserWithFilters(page, pageSize, searchQuery, userID, timezone, nil, statusFilter)
}

func SearchTasksForUserWithFilters(page, pageSize int, searchQuery string, userID *int, timezone string, projectFilter *int, statusFilter string) ([]Task, int, error) {
	pool, err := storage.OpenDatabase()
	if err != nil {
		return nil, 0, err
	}

	defer storage.CloseDatabase(pool)

	var tasks []Task
	offset := (page - 1) * pageSize
	searchPattern := "%" + searchQuery + "%"
	projectCond := ""
	if projectFilter != nil {
		if *projectFilter == 0 {
			projectCond = " AND (project_id IS NULL)"
		} else {
			projectCond = fmt.Sprintf(" AND (project_id = %d)", *projectFilter)
		}
	}

	// If not logged in, return empty results
	if userID == nil {
		return tasks, 0, nil
	}

	rows, err := pool.Query(context.Background(),
		`SELECT id,
		    title, 
		    description,
		    completed, 
		    TO_CHAR((time_stamp AT TIME ZONE 'UTC') AT TIME ZONE $2, 'YYYY/MM/DD HH:MM AM') as date_added,
		    COALESCE(CAST(due_date AS TEXT), '') AS due_date,
		    TO_CHAR((time_stamp AT TIME ZONE 'UTC') AT TIME ZONE $2, 'YYYY/MM/DD HH:MM AM') AS date_created,
		    COALESCE(TO_CHAR((date_modified AT TIME ZONE 'UTC') AT TIME ZONE $2, 'YYYY/MM/DD HH:MM AM'), '') AS date_modified,
		    COALESCE(position,0)
		 FROM tasks 
		 WHERE (title ILIKE $1 OR description ILIKE $1) AND user_id = $4`+projectCond+statusCondition(statusFilter)+`
		 ORDER BY position 
		 LIMIT $3 OFFSET $5`,
		searchPattern, timezone, pageSize, *userID, offset)

	if err != nil {
		return nil, 0, err
	}

	defer rows.Close()
	var totalTasks int = 0

	for rows.Next() {
		var task Task
		if err := rows.Scan(&task.ID, &task.Title, &task.Description, &task.Completed, &task.DateAdded, &task.DueDate, &task.DateCreated, &task.DateModified, &task.Position); err != nil {
			return nil, 0, err
		}
		totalTasks++
		tasks = append(tasks, task)
	}

	return tasks, totalTasks, nil

}

func statusCondition(statusFilter string) string {
	switch statusFilter {
	case "complete", "completed":
		return " AND completed = true"
	case "incomplete":
		return " AND (completed IS NULL OR completed = false)"
	default:
		return ""
	}
}
