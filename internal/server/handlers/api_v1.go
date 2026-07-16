package handlers

import (
	"GoTodo/internal/server/utils"
	"GoTodo/internal/storage"
	"GoTodo/internal/tasks"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
)

type apiTagJSON struct {
	ID    int    `json:"id"`
	Name  string `json:"name"`
	Color string `json:"color"`
}

type apiTaskJSON struct {
	ID          int          `json:"id"`
	Title       string       `json:"title"`
	Description string       `json:"description"`
	Completed   bool         `json:"completed"`
	DueDate     string       `json:"due_date"`
	ProjectID   *int         `json:"project_id,omitempty"`
	Project     string       `json:"project,omitempty"`
	Priority    int          `json:"priority"`
	Favorite    bool         `json:"favorite"`
	Position    int          `json:"position"`
	Tags        []apiTagJSON `json:"tags"`
	CreatedAt   string       `json:"created_at"`
	ModifiedAt  string       `json:"modified_at"`
}

type apiTaskListResponse struct {
	Tasks      []apiTaskJSON `json:"tasks"`
	Total      int           `json:"total"`
	Page       int           `json:"page"`
	PerPage    int           `json:"per_page"`
	TotalPages int           `json:"total_pages"`
}

type apiTaskCreateRequest struct {
	Title       string `json:"title"`
	Description string `json:"description"`
	DueDate     string `json:"due_date"`
	ProjectID   *int   `json:"project_id"`
	Priority    *int   `json:"priority"`
	Completed   *bool  `json:"completed"`
	Favorite    *bool  `json:"favorite"`
	TagIDs      []int  `json:"tag_ids"`
}

type apiTaskPatchRequest struct {
	Title       *string `json:"title"`
	Description *string `json:"description"`
	DueDate     *string `json:"due_date"`
	ProjectID   **int   `json:"project_id"`
	Priority    *int    `json:"priority"`
	Completed   *bool   `json:"completed"`
	Favorite    *bool   `json:"favorite"`
	TagIDs      *[]int  `json:"tag_ids"`
	ClearDue    *bool   `json:"clear_due_date"`
}

type apiTaskReorderRequest struct {
	TaskIDs  []int   `json:"task_ids"`
	Favorite *bool   `json:"favorite"`
	Page     *int    `json:"page"`
	PerPage  *int    `json:"per_page"`
	Project  *string `json:"project"`
}

type apiReorderOKResponse struct {
	OK bool `json:"ok"`
}

type apiProjectJSON struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

type apiTagCreateRequest struct {
	Name string `json:"name"`
}

func tagToAPIJSON(t storage.Tag) apiTagJSON {
	return apiTagJSON{ID: t.ID, Name: t.Name, Color: t.Color}
}

func apiUserFromRequest(r *http.Request) (int, bool) {
	return utils.GetAPIUserID(r)
}

func taskToAPIJSON(t tasks.Task) apiTaskJSON {
	tags := make([]apiTagJSON, 0, len(t.Tags))
	for _, tg := range t.Tags {
		tags = append(tags, apiTagJSON{ID: tg.ID, Name: tg.Name, Color: tg.Color})
	}
	out := apiTaskJSON{
		ID:          t.ID,
		Title:       t.Title,
		Description: t.Description,
		Completed:   t.Completed,
		DueDate:     t.DueDate,
		Priority:    t.Priority,
		Favorite:    t.IsFavorite,
		Position:    t.Position,
		Tags:        tags,
		CreatedAt:   t.DateCreated,
		ModifiedAt:  t.DateModified,
	}
	if t.ProjectID > 0 {
		pid := t.ProjectID
		out.ProjectID = &pid
		out.Project = t.ProjectName
	} else if t.ProjectName != "" {
		out.Project = t.ProjectName
	}
	return out
}

func decodeJSONBody(r *http.Request, dest interface{}) error {
	defer r.Body.Close()
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		return err
	}
	if len(body) == 0 {
		return errors.New("empty body")
	}
	return json.Unmarshal(body, dest)
}

// APIV1TasksRouter handles /api/v1/tasks, /api/v1/tasks/reorder, and /api/v1/tasks/{id}.
func APIV1TasksRouter(w http.ResponseWriter, r *http.Request) {
	sub := utils.ParseAPIV1Subpath(r, "tasks")
	if sub == "" {
		switch r.Method {
		case http.MethodGet:
			apiV1ListTasks(w, r)
		case http.MethodPost:
			apiV1CreateTask(w, r)
		default:
			utils.APIJSONError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed.")
		}
		return
	}
	if sub == "reorder" {
		if r.Method != http.MethodPost {
			utils.APIJSONError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed.")
			return
		}
		apiV1ReorderTasks(w, r)
		return
	}
	id, err := strconv.Atoi(sub)
	if err != nil || id <= 0 {
		utils.APIJSONError(w, http.StatusBadRequest, "invalid_request", "Invalid task id.")
		return
	}
	switch r.Method {
	case http.MethodGet:
		apiV1GetTask(w, r, id)
	case http.MethodPatch:
		apiV1PatchTask(w, r, id)
	case http.MethodDelete:
		apiV1DeleteTask(w, r, id)
	default:
		utils.APIJSONError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed.")
	}
}

func apiV1ListTasks(w http.ResponseWriter, r *http.Request) {
	userID, ok := apiUserFromRequest(r)
	if !ok {
		utils.APIJSONError(w, http.StatusUnauthorized, "unauthorized", "Not authenticated.")
		return
	}
	tz := GetUserTimezoneByID(userID)
	fc := filterContextFromRequest(r)
	page := fc.Page
	if page <= 0 {
		page = 1
	}
	perPage := 50
	if p := r.URL.Query().Get("per_page"); p != "" {
		if v, err := strconv.Atoi(p); err == nil && v > 0 && v <= 100 {
			perPage = v
		}
	}

	taskList, total, err := fetchTasksForFilters(page, perPage, fc, &userID, tz)
	if err != nil {
		utils.APIJSONError(w, http.StatusInternalServerError, "internal_error", "Failed to list tasks.")
		return
	}
	totalPages := (total + perPage - 1) / perPage
	if totalPages < 1 {
		totalPages = 1
	}
	out := make([]apiTaskJSON, 0, len(taskList))
	for _, t := range taskList {
		out = append(out, taskToAPIJSON(t))
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	json.NewEncoder(w).Encode(apiTaskListResponse{
		Tasks:      out,
		Total:      total,
		Page:       page,
		PerPage:    perPage,
		TotalPages: totalPages,
	})
}

func apiV1GetTask(w http.ResponseWriter, r *http.Request, taskID int) {
	userID, ok := apiUserFromRequest(r)
	if !ok {
		utils.APIJSONError(w, http.StatusUnauthorized, "unauthorized", "Not authenticated.")
		return
	}
	tz := GetUserTimezoneByID(userID)
	task, err := tasks.FetchTaskByIDForUser(taskID, userID, tz, 1)
	if err != nil {
		utils.APIJSONError(w, http.StatusNotFound, "not_found", "Task not found.")
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	json.NewEncoder(w).Encode(taskToAPIJSON(task))
}

func apiV1CreateTask(w http.ResponseWriter, r *http.Request) {
	userID, ok := apiUserFromRequest(r)
	if !ok {
		utils.APIJSONError(w, http.StatusUnauthorized, "unauthorized", "Not authenticated.")
		return
	}
	var req apiTaskCreateRequest
	if err := decodeJSONBody(r, &req); err != nil {
		utils.APIJSONError(w, http.StatusBadRequest, "invalid_request", "Invalid JSON body.")
		return
	}
	title := strings.TrimSpace(req.Title)
	if title == "" {
		utils.APIJSONError(w, http.StatusBadRequest, "invalid_request", "Title is required.")
		return
	}
	description := strings.TrimSpace(req.Description)
	if len(description) > MaxDescriptionLength {
		utils.APIJSONError(w, http.StatusBadRequest, "invalid_request", "Description too long.")
		return
	}
	priority := 0
	if req.Priority != nil {
		if *req.Priority < 0 || *req.Priority > 3 {
			utils.APIJSONError(w, http.StatusBadRequest, "invalid_request", "Priority must be 0-3.")
			return
		}
		priority = *req.Priority
	}
	completed := false
	if req.Completed != nil {
		completed = *req.Completed
	}
	favorite := false
	if req.Favorite != nil {
		favorite = *req.Favorite
	}
	dueDate := strings.TrimSpace(req.DueDate)

	db, err := storage.OpenDatabase()
	if err != nil {
		utils.APIJSONError(w, http.StatusInternalServerError, "internal_error", "Database error.")
		return
	}
	defer storage.CloseDatabase(db)

	var nextPos int
	if err := db.QueryRow(context.Background(),
		`SELECT COALESCE(MAX(position),0) + 1 FROM tasks WHERE user_id = $1 AND (is_favorite IS NULL OR is_favorite = false)`,
		userID).Scan(&nextPos); err != nil {
		utils.APIJSONError(w, http.StatusInternalServerError, "internal_error", "Failed to create task.")
		return
	}

	var projectID *int
	if req.ProjectID != nil {
		if *req.ProjectID == 0 {
			zero := 0
			projectID = &zero
		} else {
			if _, err := storage.GetProjectByID(*req.ProjectID, userID); err != nil {
				utils.APIJSONError(w, http.StatusBadRequest, "invalid_request", "Invalid project_id.")
				return
			}
			projectID = req.ProjectID
		}
	}

	var newID int
	if projectID == nil {
		if dueDate != "" {
			err = db.QueryRow(context.Background(),
				`INSERT INTO tasks (title, description, completed, user_id, time_stamp, position, priority, due_date, is_favorite)
				 VALUES ($1, $2, $3, $4, NOW() AT TIME ZONE 'UTC', $5, $6, $7, $8) RETURNING id`,
				title, description, completed, userID, nextPos, priority, dueDate, favorite).Scan(&newID)
		} else {
			err = db.QueryRow(context.Background(),
				`INSERT INTO tasks (title, description, completed, user_id, time_stamp, position, priority, is_favorite)
				 VALUES ($1, $2, $3, $4, NOW() AT TIME ZONE 'UTC', $5, $6, $7) RETURNING id`,
				title, description, completed, userID, nextPos, priority, favorite).Scan(&newID)
		}
	} else if *projectID == 0 {
		if dueDate != "" {
			err = db.QueryRow(context.Background(),
				`INSERT INTO tasks (title, description, completed, user_id, time_stamp, position, priority, due_date, is_favorite)
				 VALUES ($1, $2, $3, $4, NOW() AT TIME ZONE 'UTC', $5, $6, $7, $8) RETURNING id`,
				title, description, completed, userID, nextPos, priority, dueDate, favorite).Scan(&newID)
		} else {
			err = db.QueryRow(context.Background(),
				`INSERT INTO tasks (title, description, completed, user_id, time_stamp, position, priority, is_favorite)
				 VALUES ($1, $2, $3, $4, NOW() AT TIME ZONE 'UTC', $5, $6, $7) RETURNING id`,
				title, description, completed, userID, nextPos, priority, favorite).Scan(&newID)
		}
	} else {
		if dueDate != "" {
			err = db.QueryRow(context.Background(),
				`INSERT INTO tasks (title, description, completed, user_id, time_stamp, position, priority, project_id, due_date, is_favorite)
				 VALUES ($1, $2, $3, $4, NOW() AT TIME ZONE 'UTC', $5, $6, $7, $8, $9) RETURNING id`,
				title, description, completed, userID, nextPos, priority, *projectID, dueDate, favorite).Scan(&newID)
		} else {
			err = db.QueryRow(context.Background(),
				`INSERT INTO tasks (title, description, completed, user_id, time_stamp, position, priority, project_id, is_favorite)
				 VALUES ($1, $2, $3, $4, NOW() AT TIME ZONE 'UTC', $5, $6, $7, $8) RETURNING id`,
				title, description, completed, userID, nextPos, priority, *projectID, favorite).Scan(&newID)
		}
	}
	if err != nil {
		utils.APIJSONError(w, http.StatusInternalServerError, "internal_error", "Failed to create task.")
		return
	}

	if len(req.TagIDs) > 0 {
		if err := storage.SetTaskTags(newID, userID, req.TagIDs); err != nil {
			utils.APIJSONError(w, http.StatusBadRequest, "invalid_request", err.Error())
			return
		}
	}
	logTaskEvent(newID, userID, "created", nil)

	tz := GetUserTimezoneByID(userID)
	task, err := tasks.FetchTaskByIDForUser(newID, userID, tz, 1)
	if err != nil {
		utils.APIJSONError(w, http.StatusInternalServerError, "internal_error", "Task created but failed to load.")
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(taskToAPIJSON(task))
}

func apiV1PatchTask(w http.ResponseWriter, r *http.Request, taskID int) {
	userID, ok := apiUserFromRequest(r)
	if !ok {
		utils.APIJSONError(w, http.StatusUnauthorized, "unauthorized", "Not authenticated.")
		return
	}
	var req apiTaskPatchRequest
	if err := decodeJSONBody(r, &req); err != nil {
		utils.APIJSONError(w, http.StatusBadRequest, "invalid_request", "Invalid JSON body.")
		return
	}

	db, err := storage.OpenDatabase()
	if err != nil {
		utils.APIJSONError(w, http.StatusInternalServerError, "internal_error", "Database error.")
		return
	}
	defer storage.CloseDatabase(db)

	var ownerID int
	err = db.QueryRow(context.Background(), `SELECT user_id FROM tasks WHERE id = $1`, taskID).Scan(&ownerID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			utils.APIJSONError(w, http.StatusNotFound, "not_found", "Task not found.")
			return
		}
		utils.APIJSONError(w, http.StatusInternalServerError, "internal_error", "Database error.")
		return
	}
	if ownerID != userID {
		utils.APIJSONError(w, http.StatusNotFound, "not_found", "Task not found.")
		return
	}

	var title, description, dueDate string
	var completed, favorite bool
	var priority int
	var projectID sql.NullInt64
	err = db.QueryRow(context.Background(),
		`SELECT title, description, COALESCE(CAST(due_date AS TEXT), ''), completed,
		 COALESCE(priority,0), COALESCE(is_favorite,false), project_id FROM tasks WHERE id = $1`,
		taskID).Scan(&title, &description, &dueDate, &completed, &priority, &favorite, &projectID)
	if err != nil {
		utils.APIJSONError(w, http.StatusInternalServerError, "internal_error", "Database error.")
		return
	}

	if req.Title != nil {
		title = strings.TrimSpace(*req.Title)
		if title == "" {
			utils.APIJSONError(w, http.StatusBadRequest, "invalid_request", "Title cannot be empty.")
			return
		}
	}
	if req.Description != nil {
		description = strings.TrimSpace(*req.Description)
		if len(description) > MaxDescriptionLength {
			utils.APIJSONError(w, http.StatusBadRequest, "invalid_request", "Description too long.")
			return
		}
	}
	if req.Priority != nil {
		if *req.Priority < 0 || *req.Priority > 3 {
			utils.APIJSONError(w, http.StatusBadRequest, "invalid_request", "Priority must be 0-3.")
			return
		}
		priority = *req.Priority
	}
	if req.Completed != nil {
		completed = *req.Completed
	}
	if req.Favorite != nil {
		favorite = *req.Favorite
	}
	if req.ClearDue != nil && *req.ClearDue {
		dueDate = ""
	} else if req.DueDate != nil {
		dueDate = strings.TrimSpace(*req.DueDate)
	}

	var newProjectID sql.NullInt64
	if req.ProjectID != nil {
		if *req.ProjectID == nil {
			newProjectID = sql.NullInt64{Valid: false}
		} else if **req.ProjectID == 0 {
			newProjectID = sql.NullInt64{Valid: false}
		} else {
			if _, err := storage.GetProjectByID(**req.ProjectID, userID); err != nil {
				utils.APIJSONError(w, http.StatusBadRequest, "invalid_request", "Invalid project_id.")
				return
			}
			newProjectID = sql.NullInt64{Int64: int64(**req.ProjectID), Valid: true}
		}
	} else {
		newProjectID = projectID
	}

	if dueDate == "" {
		_, err = db.Exec(context.Background(),
			`UPDATE tasks SET title=$1, description=$2, completed=$3, priority=$4, is_favorite=$5,
			 project_id=$6, due_date=NULL, date_modified=NOW() AT TIME ZONE 'UTC' WHERE id=$7`,
			title, description, completed, priority, favorite, newProjectID, taskID)
	} else {
		_, err = db.Exec(context.Background(),
			`UPDATE tasks SET title=$1, description=$2, completed=$3, priority=$4, is_favorite=$5,
			 project_id=$6, due_date=$7, date_modified=NOW() AT TIME ZONE 'UTC' WHERE id=$8`,
			title, description, completed, priority, favorite, newProjectID, dueDate, taskID)
	}
	if err != nil {
		utils.APIJSONError(w, http.StatusInternalServerError, "internal_error", "Failed to update task.")
		return
	}

	if req.TagIDs != nil {
		if err := storage.SetTaskTags(taskID, userID, *req.TagIDs); err != nil {
			utils.APIJSONError(w, http.StatusBadRequest, "invalid_request", err.Error())
			return
		}
	}

	tz := GetUserTimezoneByID(userID)
	task, err := tasks.FetchTaskByIDForUser(taskID, userID, tz, 1)
	if err != nil {
		utils.APIJSONError(w, http.StatusInternalServerError, "internal_error", "Task updated but failed to load.")
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	json.NewEncoder(w).Encode(taskToAPIJSON(task))
}

func apiV1DeleteTask(w http.ResponseWriter, r *http.Request, taskID int) {
	userID, ok := apiUserFromRequest(r)
	if !ok {
		utils.APIJSONError(w, http.StatusUnauthorized, "unauthorized", "Not authenticated.")
		return
	}
	db, err := storage.OpenDatabase()
	if err != nil {
		utils.APIJSONError(w, http.StatusInternalServerError, "internal_error", "Database error.")
		return
	}
	defer storage.CloseDatabase(db)

	logTaskEvent(taskID, userID, "deleted", nil)
	tag, err := db.Exec(context.Background(),
		`DELETE FROM tasks WHERE id = $1 AND user_id = $2`, taskID, userID)
	if err != nil {
		utils.APIJSONError(w, http.StatusInternalServerError, "internal_error", "Failed to delete task.")
		return
	}
	if tag.RowsAffected() == 0 {
		utils.APIJSONError(w, http.StatusNotFound, "not_found", "Task not found.")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func apiV1ReorderTasks(w http.ResponseWriter, r *http.Request) {
	userID, ok := apiUserFromRequest(r)
	if !ok {
		utils.APIJSONError(w, http.StatusUnauthorized, "unauthorized", "Not authenticated.")
		return
	}

	var req apiTaskReorderRequest
	if err := decodeJSONBody(r, &req); err != nil {
		utils.APIJSONError(w, http.StatusBadRequest, "invalid_request", "Invalid JSON body.")
		return
	}
	if len(req.TaskIDs) == 0 {
		utils.APIJSONError(w, http.StatusBadRequest, "invalid_request", "task_ids is required.")
		return
	}
	if req.Favorite == nil {
		utils.APIJSONError(w, http.StatusBadRequest, "invalid_request", "favorite is required.")
		return
	}

	page := 1
	if req.Page != nil && *req.Page > 0 {
		page = *req.Page
	}
	perPage := 50
	if req.PerPage != nil && *req.PerPage > 0 && *req.PerPage <= 100 {
		perPage = *req.PerPage
	}

	var projectFilter *int
	if req.Project != nil {
		projectFilter = parseProjectFilter(*req.Project)
	}

	db, err := storage.OpenDatabase()
	if err != nil {
		utils.APIJSONError(w, http.StatusInternalServerError, "internal_error", "Database error.")
		return
	}
	defer storage.CloseDatabase(db)

	if err := reorderTaskPositions(context.Background(), db, userID, req.TaskIDs, *req.Favorite, page, perPage, projectFilter); err != nil {
		if errors.Is(err, ErrReorderValidation) {
			utils.APIJSONError(w, http.StatusBadRequest, "invalid_request", "Task does not belong to user or mismatched favorite group/project.")
			return
		}
		utils.APIJSONError(w, http.StatusInternalServerError, "internal_error", "Failed to reorder tasks.")
		return
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	json.NewEncoder(w).Encode(apiReorderOKResponse{OK: true})
}

// APIV1Projects returns the user's projects.
func APIV1Projects(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		utils.APIJSONError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed.")
		return
	}
	userID, ok := apiUserFromRequest(r)
	if !ok {
		utils.APIJSONError(w, http.StatusUnauthorized, "unauthorized", "Not authenticated.")
		return
	}
	projects, err := storage.GetProjectsForUser(userID)
	if err != nil {
		utils.APIJSONError(w, http.StatusInternalServerError, "internal_error", "Failed to list projects.")
		return
	}
	out := make([]apiProjectJSON, 0, len(projects))
	for _, p := range projects {
		out = append(out, apiProjectJSON{ID: p.ID, Name: p.Name})
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	json.NewEncoder(w).Encode(out)
}

// APIV1TagsRouter handles /api/v1/tags and /api/v1/tags/{id}.
func APIV1TagsRouter(w http.ResponseWriter, r *http.Request) {
	sub := utils.ParseAPIV1Subpath(r, "tags")
	if sub == "" {
		switch r.Method {
		case http.MethodGet:
			apiV1ListTags(w, r)
		case http.MethodPost:
			apiV1CreateTag(w, r)
		default:
			utils.APIJSONError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed.")
		}
		return
	}
	id, err := strconv.Atoi(sub)
	if err != nil || id <= 0 {
		utils.APIJSONError(w, http.StatusBadRequest, "invalid_request", "Invalid tag id.")
		return
	}
	switch r.Method {
	case http.MethodDelete:
		apiV1DeleteTag(w, r, id)
	default:
		utils.APIJSONError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed.")
	}
}

func apiV1ListTags(w http.ResponseWriter, r *http.Request) {
	userID, ok := apiUserFromRequest(r)
	if !ok {
		utils.APIJSONError(w, http.StatusUnauthorized, "unauthorized", "Not authenticated.")
		return
	}
	tags, err := storage.GetTagsForUser(userID)
	if err != nil {
		utils.APIJSONError(w, http.StatusInternalServerError, "internal_error", "Failed to list tags.")
		return
	}
	out := make([]apiTagJSON, 0, len(tags))
	for _, t := range tags {
		out = append(out, tagToAPIJSON(t))
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	json.NewEncoder(w).Encode(out)
}

func apiV1CreateTag(w http.ResponseWriter, r *http.Request) {
	userID, ok := apiUserFromRequest(r)
	if !ok {
		utils.APIJSONError(w, http.StatusUnauthorized, "unauthorized", "Not authenticated.")
		return
	}
	var req apiTagCreateRequest
	if err := decodeJSONBody(r, &req); err != nil {
		utils.APIJSONError(w, http.StatusBadRequest, "invalid_request", "Invalid JSON body.")
		return
	}
	name := strings.TrimSpace(req.Name)
	if name == "" {
		utils.APIJSONError(w, http.StatusBadRequest, "invalid_request", "Tag name is required.")
		return
	}
	tag, err := storage.GetOrCreateTagByName(userID, name)
	if err != nil {
		utils.APIJSONError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(tagToAPIJSON(*tag))
}

func apiV1DeleteTag(w http.ResponseWriter, r *http.Request, tagID int) {
	userID, ok := apiUserFromRequest(r)
	if !ok {
		utils.APIJSONError(w, http.StatusUnauthorized, "unauthorized", "Not authenticated.")
		return
	}
	db, err := storage.OpenDatabase()
	if err != nil {
		utils.APIJSONError(w, http.StatusInternalServerError, "internal_error", "Database error.")
		return
	}
	defer storage.CloseDatabase(db)

	var ownerID int
	err = db.QueryRow(context.Background(), `SELECT user_id FROM tags WHERE id = $1`, tagID).Scan(&ownerID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			utils.APIJSONError(w, http.StatusNotFound, "not_found", "Tag not found.")
			return
		}
		utils.APIJSONError(w, http.StatusInternalServerError, "internal_error", "Database error.")
		return
	}
	if ownerID != userID {
		utils.APIJSONError(w, http.StatusNotFound, "not_found", "Tag not found.")
		return
	}
	if err := storage.DeleteTag(tagID, userID); err != nil {
		utils.APIJSONError(w, http.StatusInternalServerError, "internal_error", "Failed to delete tag.")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
