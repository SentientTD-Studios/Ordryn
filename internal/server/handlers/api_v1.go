package handlers

import (
	"GoTodo/internal/domain"
	"GoTodo/internal/server/utils"
	"GoTodo/internal/storage"
	"GoTodo/internal/tasks"
	"context"
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
	Tasks            []apiTaskJSON `json:"tasks"`
	Total            int           `json:"total"`
	Page             int           `json:"page"`
	PerPage          int           `json:"per_page"`
	TotalPages       int           `json:"total_pages"`
	CompletedCount   int           `json:"completed_count"`
	IncompleteCount  int           `json:"incomplete_count"`
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

type apiTagPatchRequest struct {
	Name string `json:"name"`
}

type apiProjectCreateRequest struct {
	Name string `json:"name"`
}

type apiProjectPatchRequest struct {
	Name string `json:"name"`
}

type apiBulkRequest struct {
	Action    string  `json:"action"`
	TaskIDs   []int   `json:"task_ids"`
	ProjectID *int    `json:"project_id"`
	TagID     *int    `json:"tag_id"`
	Priority  *int    `json:"priority"`
	DueDate   *string `json:"due_date"`
}

type apiUndoRequest struct {
	UndoToken string `json:"undo_token"`
}

type apiTaskEventJSON struct {
	ID        int                    `json:"id"`
	TaskID    int                    `json:"task_id"`
	EventType string                 `json:"event_type"`
	Label     string                 `json:"label"`
	Metadata  map[string]interface{} `json:"metadata"`
	CreatedAt string                 `json:"created_at"`
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

// APIV1TasksRouter handles /api/v1/tasks, reorder/bulk/undo, /api/v1/tasks/{id}, and events.
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
	switch sub {
	case "reorder":
		if r.Method != http.MethodPost {
			utils.APIJSONError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed.")
			return
		}
		apiV1ReorderTasks(w, r)
		return
	case "bulk":
		if r.Method != http.MethodPost {
			utils.APIJSONError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed.")
			return
		}
		apiV1BulkTasks(w, r)
		return
	case "undo":
		if r.Method != http.MethodPost {
			utils.APIJSONError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed.")
			return
		}
		apiV1UndoTasks(w, r)
		return
	}
	if strings.HasSuffix(sub, "/events") {
		idStr := strings.TrimSuffix(sub, "/events")
		idStr = strings.TrimSuffix(idStr, "/")
		id, err := strconv.Atoi(idStr)
		if err != nil || id <= 0 {
			utils.APIJSONError(w, http.StatusBadRequest, "invalid_request", "Invalid task id.")
			return
		}
		if r.Method != http.MethodGet {
			utils.APIJSONError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed.")
			return
		}
		apiV1TaskEvents(w, r, id)
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
	projectFilter := parseProjectFilter(fc.Project)
	completedCount, incompleteCount := completedIncompleteCounts(&userID, projectFilter)
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	json.NewEncoder(w).Encode(apiTaskListResponse{
		Tasks:           out,
		Total:           total,
		Page:            page,
		PerPage:         perPage,
		TotalPages:      totalPages,
		CompletedCount:  completedCount,
		IncompleteCount: incompleteCount,
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
	priority := 0
	if req.Priority != nil {
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
	in := domain.CreateTaskInput{
		Title:       req.Title,
		Description: req.Description,
		DueDate:     req.DueDate,
		ProjectID:   req.ProjectID,
		Priority:    priority,
		Completed:   completed,
		Favorite:    favorite,
		TagIDs:      req.TagIDs,
	}
	newID, err := domain.CreateTask(r.Context(), userID, in)
	if err != nil {
		if errors.Is(err, domain.ErrValidation) {
			utils.APIJSONError(w, http.StatusBadRequest, "invalid_request", err.Error())
			return
		}
		utils.APIJSONError(w, http.StatusInternalServerError, "internal_error", "Failed to create task.")
		return
	}

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

	in := domain.UpdateTaskInput{
		Title:       req.Title,
		Description: req.Description,
		DueDate:     req.DueDate,
		Priority:    req.Priority,
		Completed:   req.Completed,
		Favorite:    req.Favorite,
		TagIDs:      req.TagIDs,
		ProjectID:   req.ProjectID,
	}
	if req.ClearDue != nil && *req.ClearDue {
		in.ClearDue = true
	}

	if _, err := domain.UpdateTask(r.Context(), userID, taskID, in); err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			utils.APIJSONError(w, http.StatusNotFound, "not_found", "Task not found.")
			return
		}
		if errors.Is(err, domain.ErrValidation) {
			utils.APIJSONError(w, http.StatusBadRequest, "invalid_request", err.Error())
			return
		}
		utils.APIJSONError(w, http.StatusInternalServerError, "internal_error", "Failed to update task.")
		return
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

	token, err := deleteTasksForAPI(r.Context(), db, r, w, []int{taskID}, userID)
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			utils.APIJSONError(w, http.StatusNotFound, "not_found", "Task not found.")
			return
		}
		utils.APIJSONError(w, http.StatusInternalServerError, "internal_error", "Failed to delete task.")
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"ok":          true,
		"undo_token":  token,
		"expires_in":  utils.UndoTTLSeconds,
	})
}

func apiV1BulkTasks(w http.ResponseWriter, r *http.Request) {
	userID, ok := apiUserFromRequest(r)
	if !ok {
		utils.APIJSONError(w, http.StatusUnauthorized, "unauthorized", "Not authenticated.")
		return
	}
	var req apiBulkRequest
	if err := decodeJSONBody(r, &req); err != nil {
		utils.APIJSONError(w, http.StatusBadRequest, "invalid_request", "Invalid JSON body.")
		return
	}
	action := strings.TrimSpace(req.Action)
	if action == "" {
		utils.APIJSONError(w, http.StatusBadRequest, "invalid_request", "action is required.")
		return
	}
	if len(req.TaskIDs) == 0 {
		utils.APIJSONError(w, http.StatusBadRequest, "invalid_request", "task_ids is required.")
		return
	}
	if len(req.TaskIDs) > maxBulkTaskIDs {
		utils.APIJSONError(w, http.StatusBadRequest, "invalid_request",
			"Maximum "+strconv.Itoa(maxBulkTaskIDs)+" tasks per bulk action.")
		return
	}

	db, err := storage.OpenDatabase()
	if err != nil {
		utils.APIJSONError(w, http.StatusInternalServerError, "internal_error", "Database error.")
		return
	}
	defer storage.CloseDatabase(db)

	ctx := r.Context()
	if ctx == nil {
		ctx = context.Background()
	}
	if err := verifyTasksOwnedByUser(ctx, db, req.TaskIDs, userID); err != nil {
		utils.APIJSONError(w, http.StatusForbidden, "forbidden", err.Error())
		return
	}

	var undoToken string
	switch action {
	case "complete":
		err = bulkSetCompleted(ctx, db, req.TaskIDs, userID, true)
	case "incomplete":
		err = bulkSetCompleted(ctx, db, req.TaskIDs, userID, false)
	case "move_project":
		projectIDStr := ""
		if req.ProjectID != nil {
			projectIDStr = strconv.Itoa(*req.ProjectID)
		}
		err = bulkMoveProject(ctx, db, req.TaskIDs, userID, projectIDStr)
	case "add_tag":
		if req.TagID == nil || *req.TagID <= 0 {
			utils.APIJSONError(w, http.StatusBadRequest, "invalid_request", "tag_id is required.")
			return
		}
		err = bulkAddTag(ctx, db, req.TaskIDs, userID, *req.TagID)
	case "remove_tag":
		if req.TagID == nil || *req.TagID <= 0 {
			utils.APIJSONError(w, http.StatusBadRequest, "invalid_request", "tag_id is required.")
			return
		}
		err = bulkRemoveTag(ctx, db, req.TaskIDs, userID, *req.TagID)
	case "set_priority":
		if req.Priority == nil || *req.Priority < 0 || *req.Priority > 3 {
			utils.APIJSONError(w, http.StatusBadRequest, "invalid_request", "priority must be 0-3.")
			return
		}
		err = bulkSetPriority(ctx, db, req.TaskIDs, userID, *req.Priority)
	case "set_due_date":
		raw := ""
		if req.DueDate != nil {
			raw = *req.DueDate
		}
		dueDate, derr := parseBulkDueDate(raw)
		if derr != nil {
			utils.APIJSONError(w, http.StatusBadRequest, "invalid_request", derr.Error())
			return
		}
		err = bulkSetDueDate(ctx, db, req.TaskIDs, userID, dueDate)
	case "delete":
		undoToken, err = deleteTasksForAPI(ctx, db, r, w, req.TaskIDs, userID)
	default:
		utils.APIJSONError(w, http.StatusBadRequest, "invalid_request", "Unknown bulk action.")
		return
	}
	if err != nil {
		utils.APIJSONError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}

	resp := map[string]interface{}{
		"ok":       true,
		"affected": len(req.TaskIDs),
		"action":   action,
	}
	if undoToken != "" {
		resp["undo_token"] = undoToken
		resp["expires_in"] = utils.UndoTTLSeconds
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(resp)
}

func apiV1UndoTasks(w http.ResponseWriter, r *http.Request) {
	userID, ok := apiUserFromRequest(r)
	if !ok {
		utils.APIJSONError(w, http.StatusUnauthorized, "unauthorized", "Not authenticated.")
		return
	}
	var req apiUndoRequest
	// Empty body is allowed when restoring from session pending_undo.
	_ = decodeJSONBody(r, &req)

	var snaps []DeletedTaskSnapshot
	if strings.TrimSpace(req.UndoToken) != "" {
		redisSnaps, err := utils.LoadUndoToken(r.Context(), userID, req.UndoToken)
		if err != nil {
			utils.APIJSONError(w, http.StatusBadRequest, "invalid_request", err.Error())
			return
		}
		snaps = fromRedisUndoSnapshots(redisSnaps)
	} else {
		pu, err := loadPendingUndo(r)
		if err != nil {
			utils.APIJSONError(w, http.StatusBadRequest, "invalid_request", err.Error())
			return
		}
		snaps = pu.Tasks
	}

	db, err := storage.OpenDatabase()
	if err != nil {
		utils.APIJSONError(w, http.StatusInternalServerError, "internal_error", "Database error.")
		return
	}
	defer storage.CloseDatabase(db)

	if err := restoreDeletedTasks(r.Context(), db, userID, snaps); err != nil {
		utils.APIJSONError(w, http.StatusInternalServerError, "internal_error", "Failed to restore tasks.")
		return
	}
	_ = clearPendingUndo(r, w)

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"ok":        true,
		"restored":  len(snaps),
	})
}

func apiV1TaskEvents(w http.ResponseWriter, r *http.Request, taskID int) {
	userID, ok := apiUserFromRequest(r)
	if !ok {
		utils.APIJSONError(w, http.StatusUnauthorized, "unauthorized", "Not authenticated.")
		return
	}
	tz := GetUserTimezoneByID(userID)
	if _, err := tasks.FetchTaskByIDForUser(taskID, userID, tz, 1); err != nil {
		utils.APIJSONError(w, http.StatusNotFound, "not_found", "Task not found.")
		return
	}
	events, err := storage.GetEventsForTask(taskID, userID, 50)
	if err != nil {
		utils.APIJSONError(w, http.StatusInternalServerError, "internal_error", "Failed to load events.")
		return
	}
	out := make([]apiTaskEventJSON, 0, len(events))
	for _, ev := range events {
		meta := ev.Metadata
		if meta == nil {
			meta = map[string]interface{}{}
		}
		out = append(out, apiTaskEventJSON{
			ID:        ev.ID,
			TaskID:    ev.TaskID,
			EventType: ev.EventType,
			Label:     formatEventLabel(ev.EventType, meta),
			Metadata:  meta,
			CreatedAt: ev.CreatedAt.UTC().Format("2006-01-02T15:04:05Z07:00"),
		})
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(out)
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

	var projectFilter *int
	if req.Project != nil {
		projectFilter = parseProjectFilter(*req.Project)
	}

	if err := domain.ReorderTasks(r.Context(), userID, req.TaskIDs, *req.Favorite, projectFilter); err != nil {
		if errors.Is(err, domain.ErrValidation) {
			utils.APIJSONError(w, http.StatusBadRequest, "invalid_request", "Task does not belong to user or mismatched favorite group/project.")
			return
		}
		utils.APIJSONError(w, http.StatusInternalServerError, "internal_error", "Failed to reorder tasks.")
		return
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	json.NewEncoder(w).Encode(apiReorderOKResponse{OK: true})
}

// APIV1ProjectsRouter handles /api/v1/projects and /api/v1/projects/{id}.
func APIV1ProjectsRouter(w http.ResponseWriter, r *http.Request) {
	sub := utils.ParseAPIV1Subpath(r, "projects")
	if sub == "" {
		switch r.Method {
		case http.MethodGet:
			apiV1ListProjects(w, r)
		case http.MethodPost:
			apiV1CreateProject(w, r)
		default:
			utils.APIJSONError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed.")
		}
		return
	}
	id, err := strconv.Atoi(sub)
	if err != nil || id <= 0 {
		utils.APIJSONError(w, http.StatusBadRequest, "invalid_request", "Invalid project id.")
		return
	}
	switch r.Method {
	case http.MethodPatch:
		apiV1PatchProject(w, r, id)
	case http.MethodDelete:
		apiV1DeleteProject(w, r, id)
	default:
		utils.APIJSONError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed.")
	}
}

// APIV1Projects is kept as an alias for list-only callers/tests.
func APIV1Projects(w http.ResponseWriter, r *http.Request) {
	APIV1ProjectsRouter(w, r)
}

func apiV1ListProjects(w http.ResponseWriter, r *http.Request) {
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

func apiV1CreateProject(w http.ResponseWriter, r *http.Request) {
	userID, ok := apiUserFromRequest(r)
	if !ok {
		utils.APIJSONError(w, http.StatusUnauthorized, "unauthorized", "Not authenticated.")
		return
	}
	var req apiProjectCreateRequest
	if err := decodeJSONBody(r, &req); err != nil {
		utils.APIJSONError(w, http.StatusBadRequest, "invalid_request", "Invalid JSON body.")
		return
	}
	project, err := domain.CreateProject(r.Context(), userID, req.Name)
	if err != nil {
		if errors.Is(err, domain.ErrValidation) {
			utils.APIJSONError(w, http.StatusBadRequest, "invalid_request", err.Error())
			return
		}
		utils.APIJSONError(w, http.StatusInternalServerError, "internal_error", "Failed to create project.")
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(apiProjectJSON{ID: project.ID, Name: project.Name})
}

func apiV1PatchProject(w http.ResponseWriter, r *http.Request, projectID int) {
	userID, ok := apiUserFromRequest(r)
	if !ok {
		utils.APIJSONError(w, http.StatusUnauthorized, "unauthorized", "Not authenticated.")
		return
	}
	var req apiProjectPatchRequest
	if err := decodeJSONBody(r, &req); err != nil {
		utils.APIJSONError(w, http.StatusBadRequest, "invalid_request", "Invalid JSON body.")
		return
	}
	project, err := domain.RenameProject(r.Context(), userID, projectID, req.Name)
	if err != nil {
		if errors.Is(err, domain.ErrValidation) {
			utils.APIJSONError(w, http.StatusBadRequest, "invalid_request", err.Error())
			return
		}
		if errors.Is(err, domain.ErrNotFound) {
			utils.APIJSONError(w, http.StatusNotFound, "not_found", "Project not found.")
			return
		}
		utils.APIJSONError(w, http.StatusInternalServerError, "internal_error", "Failed to update project.")
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	json.NewEncoder(w).Encode(apiProjectJSON{ID: project.ID, Name: project.Name})
}

func apiV1DeleteProject(w http.ResponseWriter, r *http.Request, projectID int) {
	userID, ok := apiUserFromRequest(r)
	if !ok {
		utils.APIJSONError(w, http.StatusUnauthorized, "unauthorized", "Not authenticated.")
		return
	}
	if err := domain.DeleteProject(r.Context(), userID, projectID); err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			utils.APIJSONError(w, http.StatusNotFound, "not_found", "Project not found.")
			return
		}
		utils.APIJSONError(w, http.StatusInternalServerError, "internal_error", "Failed to delete project.")
		return
	}
	w.WriteHeader(http.StatusNoContent)
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
	case http.MethodPatch:
		apiV1PatchTag(w, r, id)
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
	tag, err := domain.CreateTag(r.Context(), userID, req.Name)
	if err != nil {
		if errors.Is(err, domain.ErrValidation) {
			utils.APIJSONError(w, http.StatusBadRequest, "invalid_request", err.Error())
			return
		}
		utils.APIJSONError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(tagToAPIJSON(*tag))
}

func apiV1PatchTag(w http.ResponseWriter, r *http.Request, tagID int) {
	userID, ok := apiUserFromRequest(r)
	if !ok {
		utils.APIJSONError(w, http.StatusUnauthorized, "unauthorized", "Not authenticated.")
		return
	}
	var req apiTagPatchRequest
	if err := decodeJSONBody(r, &req); err != nil {
		utils.APIJSONError(w, http.StatusBadRequest, "invalid_request", "Invalid JSON body.")
		return
	}
	tag, err := domain.RenameTag(r.Context(), userID, tagID, req.Name)
	if err != nil {
		if errors.Is(err, domain.ErrValidation) {
			utils.APIJSONError(w, http.StatusBadRequest, "invalid_request", err.Error())
			return
		}
		if errors.Is(err, domain.ErrNotFound) {
			utils.APIJSONError(w, http.StatusNotFound, "not_found", "Tag not found.")
			return
		}
		utils.APIJSONError(w, http.StatusInternalServerError, "internal_error", "Failed to update tag.")
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	json.NewEncoder(w).Encode(tagToAPIJSON(*tag))
}

func apiV1DeleteTag(w http.ResponseWriter, r *http.Request, tagID int) {
	userID, ok := apiUserFromRequest(r)
	if !ok {
		utils.APIJSONError(w, http.StatusUnauthorized, "unauthorized", "Not authenticated.")
		return
	}
	if err := domain.DeleteTag(r.Context(), userID, tagID); err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			utils.APIJSONError(w, http.StatusNotFound, "not_found", "Tag not found.")
			return
		}
		utils.APIJSONError(w, http.StatusInternalServerError, "internal_error", "Failed to delete tag.")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
