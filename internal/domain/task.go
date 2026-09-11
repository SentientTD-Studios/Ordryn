package domain

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"GoTodo/internal/live"
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
	// Ignored when ParentID is set (project is copied from parent).
	ProjectID *int
	// ParentID: nil = root; &n = nest under root task n.
	ParentID       *int
	Priority       int
	Completed      bool
	Favorite       bool // Deprecated: will be removed in API v4.
	TagIDs         []int
	StatusID       *int
	EstimatePoints *int
	SprintID       *int
}

// UpdateTaskInput is a partial update. Nil pointer fields are left unchanged.
type UpdateTaskInput struct {
	Title       *string
	Description *string
	DueDate     *string
	ClearDue    bool
	// ProjectID: nil = leave; non-nil with *nil or 0 = clear; non-nil with id = set.
	ProjectID **int
	// ParentID: nil = leave; non-nil with *nil or 0 = promote to root; non-nil with id = nest.
	ParentID  **int
	Priority  *int
	Completed *bool
	Favorite  *bool // Deprecated: will be removed in API v4.
	TagIDs    *[]int
	// StatusID: nil = leave; non-nil with *nil or 0 = reject on kanban; non-nil with id = set.
	StatusID **int
	// EstimatePoints: nil = leave; non-nil with *nil = clear; non-nil with value = set.
	EstimatePoints **int
	// SprintID: nil = leave; non-nil with *nil or 0 = clear; non-nil with id = set.
	SprintID **int
}

// UpdateResult summarizes what changed for audit logging in handlers.
type UpdateResult struct {
	ChangedFields   []string
	OldPriority     int
	NewPriority     int
	OldProjectID    int
	NewProjectID    int
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
	if in.EstimatePoints != nil {
		if *in.EstimatePoints < 0 || *in.EstimatePoints > storage.MaxEstimatePoints {
			return 0, fmt.Errorf("%w: estimate_points must be 0-%d", ErrValidation, storage.MaxEstimatePoints)
		}
	}
	dueDate := strings.TrimSpace(in.DueDate)

	pool, err := storage.OpenDatabase()
	if err != nil {
		return 0, err
	}
	defer storage.CloseDatabase(pool)

	favorite := in.Favorite
	var parentArg interface{}
	var projectArg interface{}

	var dueArg interface{}
	if dueDate != "" {
		dueArg = dueDate
	}

	if in.ParentID != nil && *in.ParentID > 0 {
		parentProj, err := requireWritableRootParent(ctx, pool, userID, *in.ParentID)
		if err != nil {
			return 0, err
		}
		parentArg = *in.ParentID
		favorite = false
		if parentProj.Valid {
			projectArg = int(parentProj.Int64)
		}
		var nextPos int
		if err := pool.QueryRow(ctx,
			`SELECT COALESCE(MAX(position),0) + 1 FROM tasks WHERE parent_id = $1`,
			*in.ParentID).Scan(&nextPos); err != nil {
			return 0, err
		}
		var newID int
		err = pool.QueryRow(ctx,
			`INSERT INTO tasks (title, description, completed, user_id, time_stamp, position, priority, project_id, due_date, is_favorite, parent_id)
			 VALUES ($1, $2, $3, $4, NOW() AT TIME ZONE 'UTC', $5, $6, $7, $8, $9, $10) RETURNING id`,
			title, description, in.Completed, userID, nextPos, in.Priority, projectArg, dueArg, favorite, parentArg).Scan(&newID)
		if err != nil {
			return 0, err
		}
		if err := applyCreateWorkflow(newID, projectArg, userID, in); err != nil {
			return 0, err
		}
		if len(in.TagIDs) > 0 {
			if err := storage.SetTaskTags(newID, userID, in.TagIDs); err != nil {
				return 0, fmt.Errorf("%w: %s", ErrValidation, err.Error())
			}
		}
		_ = storage.LogTaskEvent(newID, userID, "created", map[string]interface{}{"parent_id": *in.ParentID})
		if pid, ok := projectArg.(int); ok && pid > 0 {
			NotifyProjectMembersTaskCreated(newID, userID, pid, title)
		}
		live.AfterTaskChange(userID, newID, live.TypeTaskCreated)
		return newID, nil
	}

	if in.ProjectID != nil && *in.ProjectID > 0 {
		if err := RequireProjectAcceptsNewTasks(*in.ProjectID, userID); err != nil {
			if err == ErrForbidden {
				return 0, ErrForbidden
			}
			if errors.Is(err, ErrConflict) {
				return 0, err
			}
			return 0, fmt.Errorf("%w: invalid project_id", ErrValidation)
		}
		projectArg = *in.ProjectID
	}

	var nextPos int
	if favorite {
		if err := pool.QueryRow(ctx,
			`SELECT COALESCE(MAX(position),0) + 1 FROM tasks
			 WHERE user_id = $1 AND is_favorite = true AND parent_id IS NULL`,
			userID).Scan(&nextPos); err != nil {
			return 0, err
		}
	} else {
		if err := pool.QueryRow(ctx,
			`SELECT COALESCE(MAX(position),0) + 1 FROM tasks
			 WHERE user_id = $1 AND (is_favorite IS NULL OR is_favorite = false) AND parent_id IS NULL`,
			userID).Scan(&nextPos); err != nil {
			return 0, err
		}
	}

	var newID int
	err = pool.QueryRow(ctx,
		`INSERT INTO tasks (title, description, completed, user_id, time_stamp, position, priority, project_id, due_date, is_favorite, parent_id)
		 VALUES ($1, $2, $3, $4, NOW() AT TIME ZONE 'UTC', $5, $6, $7, $8, $9, NULL) RETURNING id`,
		title, description, in.Completed, userID, nextPos, in.Priority, projectArg, dueArg, favorite).Scan(&newID)
	if err != nil {
		return 0, err
	}

	if err := applyCreateWorkflow(newID, projectArg, userID, in); err != nil {
		return 0, err
	}

	if len(in.TagIDs) > 0 {
		if err := storage.SetTaskTags(newID, userID, in.TagIDs); err != nil {
			return 0, fmt.Errorf("%w: %s", ErrValidation, err.Error())
		}
	}
	_ = storage.LogTaskEvent(newID, userID, "created", nil)
	if pid, ok := projectArg.(int); ok && pid > 0 {
		NotifyProjectMembersTaskCreated(newID, userID, pid, title)
	}
	live.AfterTaskChange(userID, newID, live.TypeTaskCreated)
	return newID, nil
}

func applyCreateWorkflow(taskID int, projectArg interface{}, userID int, in CreateTaskInput) error {
	projectID := 0
	switch v := projectArg.(type) {
	case int:
		projectID = v
	case int64:
		projectID = int(v)
	}
	if projectID <= 0 {
		if in.StatusID != nil || in.EstimatePoints != nil || in.SprintID != nil {
			return fmt.Errorf("%w: status, estimates, and sprints require a kanban project", ErrValidation)
		}
		return nil
	}
	mode, err := storage.GetProjectWorkflowMode(projectID)
	if err != nil {
		return err
	}
	if mode != storage.WorkflowKanban {
		if in.StatusID != nil || in.EstimatePoints != nil || in.SprintID != nil {
			return fmt.Errorf("%w: status, estimates, and sprints require a kanban project", ErrValidation)
		}
		return nil
	}
	if in.StatusID != nil && *in.StatusID > 0 {
		if err := ApplyStatusChange(taskID, projectID, *in.StatusID); err != nil {
			return err
		}
		// Status may override completed via is_done; if caller also set completed explicitly
		// and it conflicts, prefer status (already applied).
	} else {
		if err := AssignWorkflowOnProjectEnter(taskID, projectID, in.Completed); err != nil {
			return err
		}
	}
	if in.EstimatePoints != nil {
		if err := storage.SetTaskEstimatePoints(taskID, in.EstimatePoints); err != nil {
			return err
		}
	}
	explicitSprint := in.SprintID != nil
	sprintID := in.SprintID
	if sprintID == nil && in.ParentID != nil && *in.ParentID > 0 {
		if parentSprint, err := storage.GetTaskSprintID(*in.ParentID); err == nil && parentSprint > 0 {
			sprintID = &parentSprint
		}
	}
	if sprintID != nil {
		err := applyTaskSprint(taskID, projectID, userID, sprintID)
		if err != nil && !explicitSprint && errors.Is(err, ErrForbidden) {
			return nil
		}
		return err
	}
	return nil
}

// requireWritableRootParent ensures parentID is a writable root task and returns its project_id.
func requireWritableRootParent(ctx context.Context, pool interface {
	QueryRow(context.Context, string, ...interface{}) pgx.Row
}, userID, parentID int) (sql.NullInt64, error) {
	canRead, writeRole, _, accessErr := storage.CanUserAccessTask(parentID, userID)
	if accessErr != nil {
		return sql.NullInt64{}, accessErr
	}
	if !canRead || !storage.RoleCanWrite(writeRole) {
		return sql.NullInt64{}, fmt.Errorf("%w: parent task not found", ErrValidation)
	}
	var parentParent sql.NullInt64
	var projectID sql.NullInt64
	err := pool.QueryRow(ctx,
		`SELECT parent_id, project_id FROM tasks WHERE id = $1`, parentID).Scan(&parentParent, &projectID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) || errors.Is(err, sql.ErrNoRows) {
			return sql.NullInt64{}, fmt.Errorf("%w: parent task not found", ErrValidation)
		}
		return sql.NullInt64{}, err
	}
	if parentParent.Valid {
		return sql.NullInt64{}, fmt.Errorf("%w: cannot nest under a subtask", ErrValidation)
	}
	if projectID.Valid && projectID.Int64 > 0 {
		if err := RequireProjectAcceptsNewTasks(int(projectID.Int64), userID); err != nil {
			return sql.NullInt64{}, err
		}
	}
	return projectID, nil
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
	canRead, writeRole, _, accessErr := storage.CanUserAccessTask(taskID, userID)
	if accessErr != nil {
		return nil, accessErr
	}
	if !canRead || !storage.RoleCanWrite(writeRole) {
		return nil, ErrNotFound
	}
	_ = ownerID

	var title, description, dueDate string
	var completed, favorite bool
	var priority int
	var projectID, parentID, statusID sql.NullInt64
	var estimatePoints sql.NullInt64
	var sprintID sql.NullInt64
	err = pool.QueryRow(ctx,
		`SELECT title, description, COALESCE(CAST(due_date AS TEXT), ''), completed,
		 COALESCE(priority,0), COALESCE(is_favorite,false), project_id, parent_id, status_id, estimate_points, sprint_id
		 FROM tasks WHERE id = $1`,
		taskID).Scan(&title, &description, &dueDate, &completed, &priority, &favorite, &projectID, &parentID, &statusID, &estimatePoints, &sprintID)
	if err != nil {
		return nil, err
	}

	oldCompleted := completed
	statusTouched := false
	completedTouched := in.Completed != nil
	originalProjectID := projectID

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

	newParentID := parentID
	if in.ParentID != nil {
		if *in.ParentID == nil || **in.ParentID == 0 {
			newParentID = sql.NullInt64{Valid: false}
		} else {
			if **in.ParentID == taskID {
				return nil, fmt.Errorf("%w: cannot nest a task under itself", ErrValidation)
			}
			var childCount int
			if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM tasks WHERE parent_id = $1`, taskID).Scan(&childCount); err != nil {
				return nil, err
			}
			if childCount > 0 {
				return nil, fmt.Errorf("%w: cannot nest a task that has subtasks", ErrValidation)
			}
			parentProj, err := requireWritableRootParent(ctx, pool, userID, **in.ParentID)
			if err != nil {
				return nil, err
			}
			newParentID = sql.NullInt64{Int64: int64(**in.ParentID), Valid: true}
			projectID = parentProj
			favorite = false
		}
	}

	newProjectID := projectID
	if in.ProjectID != nil {
		if newParentID.Valid {
			return nil, fmt.Errorf("%w: subtasks inherit project from parent", ErrValidation)
		}
		if *in.ProjectID == nil || **in.ProjectID == 0 {
			newProjectID = sql.NullInt64{Valid: false}
		} else {
			if err := RequireProjectAcceptsNewTasks(**in.ProjectID, userID); err != nil {
				if err == ErrForbidden {
					return nil, ErrForbidden
				}
				if errors.Is(err, ErrConflict) {
					return nil, err
				}
				return nil, fmt.Errorf("%w: invalid project_id", ErrValidation)
			}
			newProjectID = sql.NullInt64{Int64: int64(**in.ProjectID), Valid: true}
		}
	}

	if !sameNullInt64(originalProjectID, newProjectID) && !storage.RoleCanManage(writeRole) {
		return nil, fmt.Errorf("%w: only the project owner can move a task to another project", ErrForbidden)
	}

	if newParentID.Valid {
		favorite = false
	}
	if favorite && newParentID.Valid {
		return nil, fmt.Errorf("%w: subtasks cannot be favorites", ErrValidation)
	}

	if dueDate == "" {
		_, err = pool.Exec(ctx,
			`UPDATE tasks SET title=$1, description=$2, completed=$3, priority=$4, is_favorite=$5,
			 project_id=$6, parent_id=$7, due_date=NULL, date_modified=NOW() AT TIME ZONE 'UTC' WHERE id=$8`,
			title, description, completed, priority, favorite, newProjectID, newParentID, taskID)
	} else {
		_, err = pool.Exec(ctx,
			`UPDATE tasks SET title=$1, description=$2, completed=$3, priority=$4, is_favorite=$5,
			 project_id=$6, parent_id=$7, due_date=$8, date_modified=NOW() AT TIME ZONE 'UTC' WHERE id=$9`,
			title, description, completed, priority, favorite, newProjectID, newParentID, dueDate, taskID)
	}
	if err != nil {
		return nil, err
	}

	projectChanged := result.OldProjectID != nullInt(newProjectID)

	// Keep children project in sync when a root's project changes.
	if !newParentID.Valid && projectChanged {
		_, _ = pool.Exec(ctx,
			`UPDATE tasks SET project_id = $1, date_modified = NOW() AT TIME ZONE 'UTC' WHERE parent_id = $2`,
			newProjectID, taskID)
	}

	if projectChanged {
		childIDs, _ := ChildIDsOf(ctx, []int{taskID})
		allIDs := append([]int{taskID}, childIDs...)
		if !newProjectID.Valid {
			_ = storage.ClearTaskWorkflowFieldsForTasks(allIDs)
		} else {
			for _, id := range allIDs {
				var childCompleted bool
				_ = pool.QueryRow(ctx, `SELECT COALESCE(completed,false) FROM tasks WHERE id = $1`, id).Scan(&childCompleted)
				if err := AssignWorkflowOnProjectEnter(id, int(newProjectID.Int64), childCompleted); err != nil {
					return nil, err
				}
			}
		}
		var destProject *int
		if newProjectID.Valid {
			pid := int(newProjectID.Int64)
			destProject = &pid
		}
		for _, id := range allIDs {
			if in.TagIDs != nil && id == taskID {
				continue
			}
			if err := storage.RemapTaskTagsForProjectChange(id, userID, destProject); err != nil {
				return nil, fmt.Errorf("%w: %s", ErrValidation, err.Error())
			}
		}
	}

	oldStatusID := 0
	if statusID.Valid {
		oldStatusID = int(statusID.Int64)
	}
	newStatusID := oldStatusID

	effectiveProjectID := nullInt(newProjectID)
	if in.StatusID != nil {
		if *in.StatusID == nil || **in.StatusID == 0 {
			return nil, fmt.Errorf("%w: status_id is required", ErrValidation)
		}
		if effectiveProjectID <= 0 {
			return nil, fmt.Errorf("%w: status requires a kanban project", ErrValidation)
		}
		mode, err := storage.GetProjectWorkflowMode(effectiveProjectID)
		if err != nil {
			return nil, err
		}
		if mode != storage.WorkflowKanban {
			return nil, fmt.Errorf("%w: status requires a kanban project", ErrValidation)
		}
		if err := ApplyStatusChange(taskID, effectiveProjectID, **in.StatusID); err != nil {
			return nil, err
		}
		statusTouched = true
		st, _ := storage.GetProjectStatus(effectiveProjectID, **in.StatusID)
		if st != nil {
			completed = st.IsDone
			newStatusID = st.ID
		}
	} else if completedTouched && oldCompleted != completed && effectiveProjectID > 0 {
		mode, err := storage.GetProjectWorkflowMode(effectiveProjectID)
		if err != nil {
			return nil, err
		}
		if mode == storage.WorkflowKanban {
			if err := ApplyCompletedStatusSync(taskID, effectiveProjectID, completed); err != nil {
				return nil, err
			}
			statusTouched = true
			if completed {
				if done, err := storage.GetDoneProjectStatus(effectiveProjectID); err == nil {
					newStatusID = done.ID
				}
			} else if def, err := storage.GetDefaultProjectStatus(effectiveProjectID); err == nil {
				newStatusID = def.ID
			}
		}
	}

	if in.EstimatePoints != nil {
		if effectiveProjectID <= 0 {
			return nil, fmt.Errorf("%w: estimates require a kanban project", ErrValidation)
		}
		mode, err := storage.GetProjectWorkflowMode(effectiveProjectID)
		if err != nil {
			return nil, err
		}
		if mode != storage.WorkflowKanban {
			return nil, fmt.Errorf("%w: estimates require a kanban project", ErrValidation)
		}
		if *in.EstimatePoints == nil {
			if err := storage.SetTaskEstimatePoints(taskID, nil); err != nil {
				return nil, err
			}
		} else {
			pts := **in.EstimatePoints
			if pts < 0 || pts > storage.MaxEstimatePoints {
				return nil, fmt.Errorf("%w: estimate_points must be 0-%d", ErrValidation, storage.MaxEstimatePoints)
			}
			if err := storage.SetTaskEstimatePoints(taskID, &pts); err != nil {
				return nil, err
			}
		}
	}

	oldSprintID := 0
	if sprintID.Valid {
		oldSprintID = int(sprintID.Int64)
	}
	newSprintID := oldSprintID
	if in.SprintID != nil {
		if *in.SprintID == nil || **in.SprintID == 0 {
			if err := applyTaskSprint(taskID, effectiveProjectID, userID, nil); err != nil {
				return nil, err
			}
			newSprintID = 0
		} else {
			sid := **in.SprintID
			if err := applyTaskSprint(taskID, effectiveProjectID, userID, &sid); err != nil {
				return nil, err
			}
			newSprintID = sid
		}
	} else if projectChanged {
		newSprintID = 0
	}

	if in.TagIDs != nil {
		beforeTags, err := storage.GetTagsForTask(taskID)
		if err != nil {
			return nil, err
		}
		if err := storage.SetTaskTags(taskID, userID, *in.TagIDs); err != nil {
			return nil, fmt.Errorf("%w: %s", ErrValidation, err.Error())
		}
		if afterTags, err := storage.GetTagsForTask(taskID); err == nil {
			logTagChanges(taskID, userID, beforeTags, afterTags)
		}
	}

	if (completedTouched || statusTouched) && oldCompleted != completed {
		if completed {
			_ = storage.LogTaskEvent(taskID, userID, "completed", nil)
		} else {
			_ = storage.LogTaskEvent(taskID, userID, "reopened", nil)
		}
		go SyncGitHubIssueFromOrdrynState(context.Background(), userID, taskID, completed)
	}
	if statusTouched && newStatusID != oldStatusID {
		_ = storage.LogTaskEvent(taskID, userID, "status_changed", statusChangeMetadata(effectiveProjectID, oldStatusID, newStatusID))
		if oldCompleted == completed {
			go SyncGitHubIssueFromOrdrynState(context.Background(), userID, taskID, completed)
		}
	}
	if newSprintID != oldSprintID {
		_ = storage.LogTaskEvent(taskID, userID, "sprint_changed", sprintChangeMetadata(effectiveProjectID, oldSprintID, newSprintID))
	}

	result.NewPriority = priority
	result.NewProjectID = effectiveProjectID
	result.PriorityChanged = result.OldPriority != result.NewPriority
	result.ProjectChanged = projectChanged
	if projectChanged {
		live.AfterTaskChange(userID, taskID, live.TypeTaskUpdated, result.OldProjectID)
	} else {
		live.AfterTaskChange(userID, taskID, live.TypeTaskUpdated)
	}
	return result, nil
}

func logTagChanges(taskID, userID int, before, after []storage.Tag) {
	beforeMap := tagActivityMap(before)
	afterMap := tagActivityMap(after)
	for key, t := range afterMap {
		if _, ok := beforeMap[key]; !ok {
			_ = storage.LogTaskEvent(taskID, userID, "tag_added", map[string]interface{}{"tag": t.Name, "tag_id": t.ID})
		}
	}
	for key, t := range beforeMap {
		if _, ok := afterMap[key]; !ok {
			_ = storage.LogTaskEvent(taskID, userID, "tag_removed", map[string]interface{}{"tag": t.Name, "tag_id": t.ID})
		}
	}
}

func tagActivityMap(tags []storage.Tag) map[string]storage.Tag {
	m := make(map[string]storage.Tag, len(tags))
	for _, t := range tags {
		if t.Protected || storage.IsSystemTagName(t.Name) {
			continue
		}
		key := strings.ToLower(strings.TrimSpace(t.Name))
		if key == "" {
			continue
		}
		m[key] = t
	}
	return m
}

func statusChangeMetadata(projectID, oldStatusID, newStatusID int) map[string]interface{} {
	meta := map[string]interface{}{}
	if newStatusID > 0 {
		if st, err := storage.GetProjectStatus(projectID, newStatusID); err == nil && st != nil {
			meta["to"] = st.Name
			meta["to_id"] = st.ID
		}
	}
	if oldStatusID > 0 {
		if st, err := storage.GetProjectStatus(projectID, oldStatusID); err == nil && st != nil {
			meta["from"] = st.Name
			meta["from_id"] = st.ID
		}
	}
	return meta
}

func sprintChangeMetadata(projectID, oldSprintID, newSprintID int) map[string]interface{} {
	meta := map[string]interface{}{}
	backlogName := "Backlog"
	if projectID > 0 {
		if bn, err := storage.GetProjectBacklogName(projectID); err == nil && bn != "" {
			backlogName = bn
		}
	}
	if newSprintID > 0 {
		if s, err := storage.GetProjectSprint(projectID, newSprintID); err == nil && s != nil {
			meta["to"] = s.Name
			meta["to_id"] = s.ID
		}
	} else {
		meta["to"] = backlogName
	}
	if oldSprintID > 0 {
		if s, err := storage.GetProjectSprint(projectID, oldSprintID); err == nil && s != nil {
			meta["from"] = s.Name
			meta["from_id"] = s.ID
		}
	} else {
		meta["from"] = backlogName
	}
	return meta
}

// DeleteTask removes a task the user can write. Returns ErrNotFound if missing.
func DeleteTask(ctx context.Context, userID, taskID int) error {
	pool, err := storage.OpenDatabase()
	if err != nil {
		return err
	}
	defer storage.CloseDatabase(pool)

	canRead, writeRole, _, accessErr := storage.CanUserAccessTask(taskID, userID)
	if accessErr != nil {
		return accessErr
	}
	if !canRead || !storage.RoleCanWrite(writeRole) {
		return ErrNotFound
	}

	_ = storage.LogTaskEvent(taskID, userID, "deleted", nil)
	live.AfterTaskChange(userID, taskID, live.TypeTaskDeleted)
	tag, err := pool.Exec(ctx, `DELETE FROM tasks WHERE id = $1`, taskID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func descendantTaskIDs(ctx context.Context, rootID int) ([]int, error) {
	ids := []int{rootID}
	frontier := []int{rootID}
	for len(frontier) > 0 {
		children, err := ChildIDsOf(ctx, frontier)
		if err != nil {
			return nil, err
		}
		if len(children) == 0 {
			break
		}
		ids = append(ids, children...)
		frontier = children
	}
	return ids, nil
}

func requireTaskWrite(taskID, userID int) error {
	canRead, writeRole, _, err := storage.CanUserAccessTask(taskID, userID)
	if err != nil {
		return err
	}
	if !canRead {
		return ErrNotFound
	}
	if !storage.RoleCanWrite(writeRole) {
		return ErrForbidden
	}
	return nil
}

// ArchiveTask applies the protected archived tag to a task and its descendants.
func ArchiveTask(ctx context.Context, userID, taskID int) error {
	if err := requireTaskWrite(taskID, userID); err != nil {
		return err
	}
	ids, err := descendantTaskIDs(ctx, taskID)
	if err != nil {
		return err
	}
	for _, id := range ids {
		if err := storage.ApplyArchivedTag(id, userID); err != nil {
			return err
		}
		_ = storage.LogTaskEvent(id, userID, "archived", nil)
		live.AfterTaskChange(userID, id, live.TypeTaskUpdated)
	}
	return nil
}

// RestoreTask removes the protected archived tag from a task and its descendants.
func RestoreTask(ctx context.Context, userID, taskID int) error {
	if err := requireTaskWrite(taskID, userID); err != nil {
		return err
	}
	ids, err := descendantTaskIDs(ctx, taskID)
	if err != nil {
		return err
	}
	for _, id := range ids {
		if err := storage.ClearArchivedTag(id, userID); err != nil {
			return err
		}
		_ = storage.LogTaskEvent(id, userID, "restored", nil)
		live.AfterTaskChange(userID, id, live.TypeTaskUpdated)
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

	canRead, writeRole, projectID, accessErr := storage.CanUserAccessTask(taskID, userID)
	if accessErr != nil {
		return accessErr
	}
	if !canRead || !storage.RoleCanWrite(writeRole) {
		return ErrNotFound
	}

	if projectID > 0 {
		mode, err := storage.GetProjectWorkflowMode(projectID)
		if err != nil {
			return err
		}
		if mode == storage.WorkflowKanban {
			if err := ApplyCompletedStatusSync(taskID, projectID, completed); err != nil {
				return err
			}
			if completed {
				_ = storage.LogTaskEvent(taskID, userID, "completed", nil)
			} else {
				_ = storage.LogTaskEvent(taskID, userID, "reopened", nil)
			}
			go SyncGitHubIssueFromOrdrynState(context.Background(), userID, taskID, completed)
			live.AfterTaskChange(userID, taskID, live.TypeTaskUpdated)
			return nil
		}
	}

	tag, err := pool.Exec(ctx,
		`UPDATE tasks SET completed = $1, date_modified = NOW() AT TIME ZONE 'UTC'
		 WHERE id = $2`, completed, taskID)
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
	go SyncGitHubIssueFromOrdrynState(context.Background(), userID, taskID, completed)
	live.AfterTaskChange(userID, taskID, live.TypeTaskUpdated)
	return nil
}

// ToggleTaskCompleted flips completed for a writable task.
func ToggleTaskCompleted(ctx context.Context, userID, taskID int) (bool, error) {
	pool, err := storage.OpenDatabase()
	if err != nil {
		return false, err
	}
	defer storage.CloseDatabase(pool)

	canRead, writeRole, _, accessErr := storage.CanUserAccessTask(taskID, userID)
	if accessErr != nil {
		return false, accessErr
	}
	if !canRead || !storage.RoleCanWrite(writeRole) {
		return false, ErrNotFound
	}

	var completed bool
	err = pool.QueryRow(ctx, `SELECT completed FROM tasks WHERE id = $1`, taskID).Scan(&completed)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) || errors.Is(err, sql.ErrNoRows) {
			return false, ErrNotFound
		}
		return false, err
	}
	newVal := !completed
	if err := SetTaskCompleted(ctx, userID, taskID, newVal); err != nil {
		return false, err
	}
	return newVal, nil
}

// SetTaskFavorite sets is_favorite for a writable task.
// Deprecated: task favoriting will be removed in API v4.
func SetTaskFavorite(ctx context.Context, userID, taskID int, favorite bool) error {
	pool, err := storage.OpenDatabase()
	if err != nil {
		return err
	}
	defer storage.CloseDatabase(pool)

	canRead, writeRole, _, accessErr := storage.CanUserAccessTask(taskID, userID)
	if accessErr != nil {
		return accessErr
	}
	if !canRead || !storage.RoleCanWrite(writeRole) {
		return ErrNotFound
	}

	if favorite {
		var parentID sql.NullInt64
		if err := pool.QueryRow(ctx, `SELECT parent_id FROM tasks WHERE id = $1`, taskID).Scan(&parentID); err != nil {
			return err
		}
		if parentID.Valid {
			return fmt.Errorf("%w: subtasks cannot be favorites", ErrValidation)
		}
	}

	tag, err := pool.Exec(ctx,
		`UPDATE tasks SET is_favorite = $1, date_modified = NOW() AT TIME ZONE 'UTC'
		 WHERE id = $2`, favorite, taskID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	live.AfterTaskChange(userID, taskID, live.TypeTaskUpdated)
	return nil
}

// ToggleTaskFavorite flips is_favorite for a writable task.
// Deprecated: task favoriting will be removed in API v4.
func ToggleTaskFavorite(ctx context.Context, userID, taskID int) (bool, error) {
	pool, err := storage.OpenDatabase()
	if err != nil {
		return false, err
	}
	defer storage.CloseDatabase(pool)

	canRead, writeRole, _, accessErr := storage.CanUserAccessTask(taskID, userID)
	if accessErr != nil {
		return false, accessErr
	}
	if !canRead || !storage.RoleCanWrite(writeRole) {
		return false, ErrNotFound
	}

	var isFav bool
	err = pool.QueryRow(ctx,
		`SELECT COALESCE(is_favorite,false) FROM tasks WHERE id = $1`,
		taskID).Scan(&isFav)
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

func sameNullInt64(a, b sql.NullInt64) bool {
	if !a.Valid && !b.Valid {
		return true
	}
	return a.Valid && b.Valid && a.Int64 == b.Int64
}

// ChildIDsOf returns direct child task ids for the given parents.
func ChildIDsOf(ctx context.Context, parentIDs []int) ([]int, error) {
	if len(parentIDs) == 0 {
		return nil, nil
	}
	pool, err := storage.OpenDatabase()
	if err != nil {
		return nil, err
	}
	defer storage.CloseDatabase(pool)
	rows, err := pool.Query(ctx, `SELECT id FROM tasks WHERE parent_id = ANY($1)`, parentIDs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]int, 0)
	for rows.Next() {
		var id int
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

// ReparentChildren moves children of fromParent to toParent (nil/0 = promote to root).
func ReparentChildren(ctx context.Context, userID, fromParent int, toParent *int) error {
	pool, err := storage.OpenDatabase()
	if err != nil {
		return err
	}
	defer storage.CloseDatabase(pool)

	canRead, writeRole, _, accessErr := storage.CanUserAccessTask(fromParent, userID)
	if accessErr != nil {
		return accessErr
	}
	if !canRead || !storage.RoleCanWrite(writeRole) {
		return ErrNotFound
	}

	if toParent != nil && *toParent > 0 {
		if *toParent == fromParent {
			return fmt.Errorf("%w: cannot reparent onto the task being deleted", ErrValidation)
		}
		parentProj, err := requireWritableRootParent(ctx, pool, userID, *toParent)
		if err != nil {
			return err
		}
		_, err = pool.Exec(ctx,
			`UPDATE tasks SET parent_id = $1, project_id = $2, is_favorite = false,
			 date_modified = NOW() AT TIME ZONE 'UTC' WHERE parent_id = $3`,
			*toParent, parentProj, fromParent)
		if err != nil {
			return err
		}
		live.AfterTaskChange(userID, fromParent, live.TypeTaskUpdated)
		live.AfterTaskChange(userID, *toParent, live.TypeTaskUpdated)
		return nil
	}

	_, err = pool.Exec(ctx,
		`UPDATE tasks SET parent_id = NULL, date_modified = NOW() AT TIME ZONE 'UTC' WHERE parent_id = $1`,
		fromParent)
	if err != nil {
		return err
	}
	live.AfterTaskChange(userID, fromParent, live.TypeTaskUpdated)
	return nil
}
