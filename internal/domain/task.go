package domain

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"GoTodo/internal/storage"

	"github.com/jackc/pgx/v5"
)

// MaxDescriptionLength is the shared limit for task descriptions.
const MaxDescriptionLength = 1000

// CreateTaskInput is the shared create payload for HTMX and /api/v1.
type CreateTaskInput struct {
	Title       string
	Description string
	DueDate     string
	// ProjectID: nil = omit column (no project); &0 = no project; &n = assign project n.
	ProjectID *int
	Priority  int
	Completed bool
	Favorite  bool
	TagIDs    []int
}

// UpdateTaskInput is a partial update. Nil pointer fields are left unchanged.
type UpdateTaskInput struct {
	Title       *string
	Description *string
	DueDate     *string
	ClearDue    bool
	// ProjectID: nil = leave; non-nil with *nil or 0 = clear; non-nil with id = set.
	ProjectID **int
	Priority  *int
	Completed *bool
	Favorite  *bool
	TagIDs    *[]int
}

// UpdateResult summarizes what changed for audit logging in handlers.
type UpdateResult struct {
	ChangedFields []string
	OldPriority   int
	NewPriority   int
	OldProjectID  int
	NewProjectID  int
	PriorityChanged bool
	ProjectChanged  bool
}

// CreateTask validates input, inserts a task, and optionally assigns tags.
func CreateTask(ctx context.Context, userID int, in CreateTaskInput) (int, error) {
	title := strings.TrimSpace(in.Title)
	if title == "" {
		return 0, fmt.Errorf("%w: title is required", ErrValidation)
	}
	description := strings.TrimSpace(in.Description)
	if len(description) > MaxDescriptionLength {
		return 0, fmt.Errorf("%w: description too long", ErrValidation)
	}
	if in.Priority < 0 || in.Priority > 3 {
		return 0, fmt.Errorf("%w: priority must be 0-3", ErrValidation)
	}
	dueDate := strings.TrimSpace(in.DueDate)

	pool, err := storage.OpenDatabase()
	if err != nil {
		return 0, err
	}
	defer storage.CloseDatabase(pool)

	var nextPos int
	if err := pool.QueryRow(ctx,
		`SELECT COALESCE(MAX(position),0) + 1 FROM tasks
		 WHERE user_id = $1 AND (is_favorite IS NULL OR is_favorite = false)`,
		userID).Scan(&nextPos); err != nil {
		return 0, err
	}

	var projectArg interface{}
	useProjectCol := false
	if in.ProjectID != nil {
		if *in.ProjectID == 0 {
			projectArg = nil
			useProjectCol = false
		} else {
			if _, err := storage.GetProjectByID(*in.ProjectID, userID); err != nil {
				return 0, fmt.Errorf("%w: invalid project_id", ErrValidation)
			}
			projectArg = *in.ProjectID
			useProjectCol = true
		}
	}

	var newID int
	if useProjectCol {
		if dueDate != "" {
			err = pool.QueryRow(ctx,
				`INSERT INTO tasks (title, description, completed, user_id, time_stamp, position, priority, project_id, due_date, is_favorite)
				 VALUES ($1, $2, $3, $4, NOW() AT TIME ZONE 'UTC', $5, $6, $7, $8, $9) RETURNING id`,
				title, description, in.Completed, userID, nextPos, in.Priority, projectArg, dueDate, in.Favorite).Scan(&newID)
		} else {
			err = pool.QueryRow(ctx,
				`INSERT INTO tasks (title, description, completed, user_id, time_stamp, position, priority, project_id, is_favorite)
				 VALUES ($1, $2, $3, $4, NOW() AT TIME ZONE 'UTC', $5, $6, $7, $8) RETURNING id`,
				title, description, in.Completed, userID, nextPos, in.Priority, projectArg, in.Favorite).Scan(&newID)
		}
	} else {
		if dueDate != "" {
			err = pool.QueryRow(ctx,
				`INSERT INTO tasks (title, description, completed, user_id, time_stamp, position, priority, due_date, is_favorite)
				 VALUES ($1, $2, $3, $4, NOW() AT TIME ZONE 'UTC', $5, $6, $7, $8) RETURNING id`,
				title, description, in.Completed, userID, nextPos, in.Priority, dueDate, in.Favorite).Scan(&newID)
		} else {
			err = pool.QueryRow(ctx,
				`INSERT INTO tasks (title, description, completed, user_id, time_stamp, position, priority, is_favorite)
				 VALUES ($1, $2, $3, $4, NOW() AT TIME ZONE 'UTC', $5, $6, $7) RETURNING id`,
				title, description, in.Completed, userID, nextPos, in.Priority, in.Favorite).Scan(&newID)
		}
	}
	if err != nil {
		return 0, err
	}

	if len(in.TagIDs) > 0 {
		if err := storage.SetTaskTags(newID, userID, in.TagIDs); err != nil {
			return 0, fmt.Errorf("%w: %s", ErrValidation, err.Error())
		}
	}
	_ = storage.LogTaskEvent(newID, userID, "created", nil)
	return newID, nil
}

// UpdateTask applies a partial update for an owned task.
func UpdateTask(ctx context.Context, userID, taskID int, in UpdateTaskInput) (*UpdateResult, error) {
	pool, err := storage.OpenDatabase()
	if err != nil {
		return nil, err
	}
	defer storage.CloseDatabase(pool)

	var ownerID int
	err = pool.QueryRow(ctx, `SELECT user_id FROM tasks WHERE id = $1`, taskID).Scan(&ownerID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) || errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	if ownerID != userID {
		return nil, ErrNotFound
	}

	var title, description, dueDate string
	var completed, favorite bool
	var priority int
	var projectID sql.NullInt64
	err = pool.QueryRow(ctx,
		`SELECT title, description, COALESCE(CAST(due_date AS TEXT), ''), completed,
		 COALESCE(priority,0), COALESCE(is_favorite,false), project_id FROM tasks WHERE id = $1`,
		taskID).Scan(&title, &description, &dueDate, &completed, &priority, &favorite, &projectID)
	if err != nil {
		return nil, err
	}

	result := &UpdateResult{
		OldPriority:  priority,
		OldProjectID: nullInt(projectID),
	}

	if in.Title != nil {
		title = strings.TrimSpace(*in.Title)
		if title == "" {
			return nil, fmt.Errorf("%w: title cannot be empty", ErrValidation)
		}
	}
	if in.Description != nil {
		description = strings.TrimSpace(*in.Description)
		if len(description) > MaxDescriptionLength {
			return nil, fmt.Errorf("%w: description too long", ErrValidation)
		}
	}
	if in.Priority != nil {
		if *in.Priority < 0 || *in.Priority > 3 {
			return nil, fmt.Errorf("%w: priority must be 0-3", ErrValidation)
		}
		priority = *in.Priority
	}
	if in.Completed != nil {
		completed = *in.Completed
	}
	if in.Favorite != nil {
		favorite = *in.Favorite
	}
	if in.ClearDue {
		dueDate = ""
	} else if in.DueDate != nil {
		dueDate = strings.TrimSpace(*in.DueDate)
	}

	newProjectID := projectID
	if in.ProjectID != nil {
		if *in.ProjectID == nil || **in.ProjectID == 0 {
			newProjectID = sql.NullInt64{Valid: false}
		} else {
			if _, err := storage.GetProjectByID(**in.ProjectID, userID); err != nil {
				return nil, fmt.Errorf("%w: invalid project_id", ErrValidation)
			}
			newProjectID = sql.NullInt64{Int64: int64(**in.ProjectID), Valid: true}
		}
	}

	if dueDate == "" {
		_, err = pool.Exec(ctx,
			`UPDATE tasks SET title=$1, description=$2, completed=$3, priority=$4, is_favorite=$5,
			 project_id=$6, due_date=NULL, date_modified=NOW() AT TIME ZONE 'UTC' WHERE id=$7`,
			title, description, completed, priority, favorite, newProjectID, taskID)
	} else {
		_, err = pool.Exec(ctx,
			`UPDATE tasks SET title=$1, description=$2, completed=$3, priority=$4, is_favorite=$5,
			 project_id=$6, due_date=$7, date_modified=NOW() AT TIME ZONE 'UTC' WHERE id=$8`,
			title, description, completed, priority, favorite, newProjectID, dueDate, taskID)
	}
	if err != nil {
		return nil, err
	}

	if in.TagIDs != nil {
		if err := storage.SetTaskTags(taskID, userID, *in.TagIDs); err != nil {
			return nil, fmt.Errorf("%w: %s", ErrValidation, err.Error())
		}
	}

	result.NewPriority = priority
	result.NewProjectID = nullInt(newProjectID)
	result.PriorityChanged = result.OldPriority != result.NewPriority
	result.ProjectChanged = result.OldProjectID != result.NewProjectID
	return result, nil
}

// DeleteTask removes an owned task. Returns ErrNotFound if missing.
func DeleteTask(ctx context.Context, userID, taskID int) error {
	pool, err := storage.OpenDatabase()
	if err != nil {
		return err
	}
	defer storage.CloseDatabase(pool)

	_ = storage.LogTaskEvent(taskID, userID, "deleted", nil)
	tag, err := pool.Exec(ctx, `DELETE FROM tasks WHERE id = $1 AND user_id = $2`, taskID, userID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// SetTaskCompleted sets completed and logs completed/reopened.
func SetTaskCompleted(ctx context.Context, userID, taskID int, completed bool) error {
	pool, err := storage.OpenDatabase()
	if err != nil {
		return err
	}
	defer storage.CloseDatabase(pool)

	tag, err := pool.Exec(ctx,
		`UPDATE tasks SET completed = $1, date_modified = NOW() AT TIME ZONE 'UTC'
		 WHERE id = $2 AND user_id = $3`, completed, taskID, userID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	if completed {
		_ = storage.LogTaskEvent(taskID, userID, "completed", nil)
	} else {
		_ = storage.LogTaskEvent(taskID, userID, "reopened", nil)
	}
	return nil
}

// ToggleTaskCompleted flips completed for an owned task.
func ToggleTaskCompleted(ctx context.Context, userID, taskID int) (bool, error) {
	pool, err := storage.OpenDatabase()
	if err != nil {
		return false, err
	}
	defer storage.CloseDatabase(pool)

	var completed bool
	var ownerID int
	err = pool.QueryRow(ctx, `SELECT completed, user_id FROM tasks WHERE id = $1`, taskID).Scan(&completed, &ownerID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) || errors.Is(err, sql.ErrNoRows) {
			return false, ErrNotFound
		}
		return false, err
	}
	if ownerID != userID {
		return false, ErrNotFound
	}
	newVal := !completed
	if err := SetTaskCompleted(ctx, userID, taskID, newVal); err != nil {
		return false, err
	}
	return newVal, nil
}

// SetTaskFavorite sets is_favorite for an owned task.
func SetTaskFavorite(ctx context.Context, userID, taskID int, favorite bool) error {
	pool, err := storage.OpenDatabase()
	if err != nil {
		return err
	}
	defer storage.CloseDatabase(pool)

	tag, err := pool.Exec(ctx,
		`UPDATE tasks SET is_favorite = $1, date_modified = NOW() AT TIME ZONE 'UTC'
		 WHERE id = $2 AND user_id = $3`, favorite, taskID, userID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// ToggleTaskFavorite flips is_favorite for an owned task.
func ToggleTaskFavorite(ctx context.Context, userID, taskID int) (bool, error) {
	pool, err := storage.OpenDatabase()
	if err != nil {
		return false, err
	}
	defer storage.CloseDatabase(pool)

	var isFav bool
	err = pool.QueryRow(ctx,
		`SELECT COALESCE(is_favorite,false) FROM tasks WHERE id = $1 AND user_id = $2`,
		taskID, userID).Scan(&isFav)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) || errors.Is(err, sql.ErrNoRows) {
			return false, ErrNotFound
		}
		return false, err
	}
	newVal := !isFav
	if err := SetTaskFavorite(ctx, userID, taskID, newVal); err != nil {
		return false, err
	}
	return newVal, nil
}

func nullInt(v sql.NullInt64) int {
	if v.Valid {
		return int(v.Int64)
	}
	return 0
}
