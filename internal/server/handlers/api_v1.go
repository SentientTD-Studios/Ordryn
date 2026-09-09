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
	ID        int    `json:"id"`
	Name      string `json:"name"`
	Color     string `json:"color"`
	ProjectID *int   `json:"project_id"`
	Protected bool   `json:"protected"`
}

type apiTaskJSON struct {
	ID                int                `json:"id"`
	Title             string             `json:"title"`
	Description       string             `json:"description"`
	Completed         bool               `json:"completed"`
	DueDate           string             `json:"due_date"`
	ProjectID         *int               `json:"project_id,omitempty"`
	Project           string             `json:"project,omitempty"`
	Priority          int                `json:"priority"`
	Favorite          bool               `json:"favorite"`
	Position          int                `json:"position"`
	ParentID          *int               `json:"parent_id"`
	ChildCount        int                `json:"child_count"`
	ChildrenCompleted int                `json:"children_completed"`
	Children          []apiTaskJSON      `json:"children,omitempty"`
	Tags              []apiTagJSON       `json:"tags"`
	CreatedAt         string             `json:"created_at"`
	ModifiedAt        string             `json:"modified_at"`
	StatusID          *int               `json:"status_id,omitempty"`
	StatusName        string             `json:"status_name,omitempty"`
	EstimatePoints    *int               `json:"estimate_points,omitempty"`
	TimeSpentMinutes  int                `json:"time_spent_minutes,omitempty"`
	ProjectWorkflow   string             `json:"project_workflow,omitempty"`
	ClaimedBy         *int               `json:"claimed_by"`
	ClaimedByName     string             `json:"claimed_by_name,omitempty"`
	SprintID          *int               `json:"sprint_id,omitempty"`
	SprintName        string             `json:"sprint_name,omitempty"`
	ParentTitle       string             `json:"parent_title,omitempty"`
	GitHub            *apiTaskGitHubJSON `json:"github,omitempty"`
	DeprecationNotice string             `json:"deprecation_notice,omitempty"`
}

type apiTaskListResponse struct {
	Tasks           []apiTaskJSON `json:"tasks"`
	Total           int           `json:"total"`
	Page            int           `json:"page"`
	PerPage         int           `json:"per_page"`
	TotalPages      int           `json:"total_pages"`
	CompletedCount  int           `json:"completed_count"`
	IncompleteCount int           `json:"incomplete_count"`
}

type apiTaskCreateRequest struct {
	Title          string `json:"title"`
	Description    string `json:"description"`
	DueDate        string `json:"due_date"`
	ProjectID      *int   `json:"project_id"`
	ParentID       *int   `json:"parent_id"`
	Priority       *int   `json:"priority"`
	Completed      *bool  `json:"completed"`
	Favorite       *bool  `json:"favorite"`
	TagIDs         []int  `json:"tag_ids"`
	StatusID       *int   `json:"status_id"`
	EstimatePoints *int   `json:"estimate_points"`
	SprintID       *int   `json:"sprint_id"`
}

type apiTaskPatchRequest struct {
	Title          *string     `json:"title"`
	Description    *string     `json:"description"`
	DueDate        *string     `json:"due_date"`
	ProjectID      optionalInt `json:"project_id"`
	ParentID       **int       `json:"parent_id"`
	Priority       *int        `json:"priority"`
	Completed      *bool       `json:"completed"`
	Favorite       *bool       `json:"favorite"`
	TagIDs         *[]int      `json:"tag_ids"`
	ClearDue       *bool       `json:"clear_due_date"`
	StatusID       **int       `json:"status_id"`
	EstimatePoints optionalInt `json:"estimate_points"`
	SprintID       optionalInt `json:"sprint_id"`
}

// optionalInt distinguishes omitted / null / value for JSON patch fields.
// It must be a non-pointer struct field: encoding/json sets pointer fields to
// nil on JSON null without calling UnmarshalJSON, which would make null look
// the same as an omitted field.
type optionalInt struct {
	Set   bool
	Null  bool
	Value int
}

func (o *optionalInt) UnmarshalJSON(b []byte) error {
	o.Set = true
	if string(b) == "null" {
		o.Null = true
		return nil
	}
	return json.Unmarshal(b, &o.Value)
}

func (o optionalInt) toPatchInt(zeroClears bool) **int {
	if !o.Set {
		return nil
	}
	if o.Null || (zeroClears && o.Value == 0) {
		var clear *int
		return &clear
	}
	v := o.Value
	ptr := &v
	return &ptr
}

// optionalString distinguishes omitted / null / value for JSON patch fields.
type optionalString struct {
	Set   bool
	Null  bool
	Value string
}

func (o *optionalString) UnmarshalJSON(b []byte) error {
	o.Set = true
	if string(b) == "null" {
		o.Null = true
		return nil
	}
	return json.Unmarshal(b, &o.Value)
}

func (o optionalString) toPatchString() *string {
	if !o.Set {
		return nil
	}
	if o.Null {
		empty := ""
		return &empty
	}
	v := o.Value
	return &v
}

type apiTaskReorderRequest struct {
	TaskIDs  []int   `json:"task_ids"`
	Favorite *bool   `json:"favorite"`
	ParentID *int    `json:"parent_id"`
	Page     *int    `json:"page"`
	PerPage  *int    `json:"per_page"`
	Project  *string `json:"project"`
	StatusID *int    `json:"status_id"`
}

type apiReorderOKResponse struct {
	OK                bool   `json:"ok"`
	DeprecationNotice string `json:"deprecation_notice,omitempty"`
}

type apiProjectJSON struct {
	ID            int    `json:"id"`
	Name          string `json:"name"`
	Description   string `json:"description,omitempty"`
	WorkflowMode  string `json:"workflow_mode,omitempty"`
	Archived           bool   `json:"archived"`
	BacklogName        string `json:"backlog_name,omitempty"`
	BacklogDescription string `json:"backlog_description,omitempty"`
	Role               string `json:"role,omitempty"`
	OwnerEmail    string `json:"owner_email,omitempty"`
	OwnerUserName string `json:"owner_user_name,omitempty"`
	OwnerUserID   int    `json:"owner_user_id,omitempty"`
}

type apiTagCreateRequest struct {
	Name      string `json:"name"`
	ProjectID *int   `json:"project_id"`
}

type apiTagPatchRequest struct {
	Name  *string `json:"name"`
	Color *string `json:"color"`
}

type apiProjectCreateRequest struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

type apiProjectPatchRequest struct {
	Name         *string `json:"name"`
	Description  *string `json:"description"`
	WorkflowMode       *string `json:"workflow_mode"`
	BacklogName        *string `json:"backlog_name"`
	BacklogDescription *string `json:"backlog_description"`
}

type apiProjectReorderRequest struct {
	ProjectIDs []int `json:"project_ids"`
}

type apiBulkRequest struct {
	Action    string  `json:"action"`
	TaskIDs   []int   `json:"task_ids"`
	ProjectID *int    `json:"project_id"`
	TagID     *int    `json:"tag_id"`
	Priority  *int    `json:"priority"`
	DueDate   *string `json:"due_date"`
	StatusID  *int    `json:"status_id"`
}

type apiUndoRequest struct {
	UndoToken string `json:"undo_token"`
}

type apiTaskEventJSON struct {
	ID            int                    `json:"id"`
	TaskID        int                    `json:"task_id"`
	EventType     string                 `json:"event_type"`
	Label         string                 `json:"label"`
	Metadata      map[string]interface{} `json:"metadata"`
	CreatedAt     string                 `json:"created_at"`
	ActorUserID   int                    `json:"actor_user_id,omitempty"`
	ActorUserName string                 `json:"actor_user_name,omitempty"`
	ActorEmail    string                 `json:"actor_email,omitempty"`
}

func tagToAPIJSON(t storage.Tag) apiTagJSON {
	return apiTagJSON{ID: t.ID, Name: t.Name, Color: t.Color, ProjectID: t.ProjectID, Protected: t.Protected}
}

func apiUserFromRequest(r *http.Request) (int, bool) {
	return utils.GetAPIUserID(r)
}

func taskToAPIJSON(t tasks.Task) apiTaskJSON {
	tags := make([]apiTagJSON, 0, len(t.Tags))
	for _, tg := range t.Tags {
		tags = append(tags, apiTagJSON{ID: tg.ID, Name: tg.Name, Color: tg.Color, ProjectID: tg.ProjectID, Protected: tg.Protected})
	}
	out := apiTaskJSON{
		ID:                t.ID,
		Title:             t.Title,
		Description:       t.Description,
		Completed:         t.Completed,
		DueDate:           t.DueDate,
		Priority:          t.Priority,
		Favorite:          t.IsFavorite,
		Position:          t.Position,
		ChildCount:        t.ChildCount,
		ChildrenCompleted: t.ChildrenCompleted,
		Tags:              tags,
		CreatedAt:         t.DateCreated,
		ModifiedAt:        t.DateModified,
		StatusName:        t.StatusName,
		EstimatePoints:    t.EstimatePoints,
		TimeSpentMinutes:  t.TimeSpentMinutes,
		ProjectWorkflow:   t.ProjectWorkflow,
		ClaimedByName:     t.ClaimedByName,
		SprintName:        t.SprintName,
		ParentTitle:       t.ParentTitle,
	}
	if t.ParentID > 0 {
		pid := t.ParentID
		out.ParentID = &pid
	}
	if t.ProjectID > 0 {
		pid := t.ProjectID
		out.ProjectID = &pid
		out.Project = t.ProjectName
	} else if t.ProjectName != "" {
		out.Project = t.ProjectName
	}
	if t.StatusID > 0 {
		sid := t.StatusID
		out.StatusID = &sid
	}
	if t.ClaimedBy > 0 {
		cid := t.ClaimedBy
		out.ClaimedBy = &cid
	}
	if t.SprintID > 0 {
		sid := t.SprintID
		out.SprintID = &sid
	}
	if t.GitHubIssueNumber > 0 || t.GitHubIssueURL != "" {
		out.GitHub = &apiTaskGitHubJSON{
			IssueNumber:   t.GitHubIssueNumber,
			IssueID:       t.GitHubIssueID,
			IssueURL:      t.GitHubIssueURL,
			IssueState:    t.GitHubIssueState,
			IssueTitle:    t.GitHubIssueTitle,
			LastSyncError: t.GitHubLastSyncError,
		}
	}
	if len(t.Children) > 0 {
		out.Children = make([]apiTaskJSON, 0, len(t.Children))
		for _, c := range t.Children {
			out.Children = append(out.Children, taskToAPIJSON(c))
		}
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
	if strings.Contains(sub, "/") {
		parts := strings.Split(sub, "/")
		id, err := strconv.Atoi(parts[0])
		if err != nil || id <= 0 {
			utils.APIJSONError(w, http.StatusBadRequest, "invalid_request", "Invalid task id.")
			return
		}
		switch parts[1] {
		case "events":
			if len(parts) != 2 || r.Method != http.MethodGet {
				utils.APIJSONError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed.")
				return
			}
			apiV1TaskEvents(w, r, id)
			return
		case "time-entries":
			handleTaskTimeEntries(w, r, id, parts[2:])
			return
		case "claim":
			if len(parts) != 2 {
				utils.APIJSONError(w, http.StatusBadRequest, "invalid_request", "Invalid task path.")
				return
			}
			apiV1TaskClaim(w, r, id)
			return
		case "archive":
			if len(parts) != 2 || r.Method != http.MethodPost {
				utils.APIJSONError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed.")
				return
			}
			apiV1TaskArchive(w, r, id)
			return
		case "restore":
			if len(parts) != 2 || r.Method != http.MethodPost {
				utils.APIJSONError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed.")
				return
			}
			apiV1TaskRestore(w, r, id)
			return
		case "github-issue":
			if len(parts) != 2 {
				utils.APIJSONError(w, http.StatusBadRequest, "invalid_request", "Invalid task path.")
				return
			}
			apiV1TaskGitHubIssue(w, r, id)
			return
		case "comments":
			handleTaskComments(w, r, id, parts[2:])
			return
		}
		utils.APIJSONError(w, http.StatusBadRequest, "invalid_request", "Invalid task path.")
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
	sprintFilter := parseSprintFilter(fc.Sprint)
	filters := fc.ToListFilters()
	includeRemoved := tasks.IsRemovedTagFilter(filters)
	completedCount, incompleteCount := completedIncompleteCounts(&userID, projectFilter, sprintFilter, includeRemoved)
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
	notice := favoriteDeprecationNoticeIfUsed(w, req.Favorite != nil)
	in := domain.CreateTaskInput{
		Title:          req.Title,
		Description:    req.Description,
		DueDate:        req.DueDate,
		ProjectID:      req.ProjectID,
		ParentID:       req.ParentID,
		Priority:       priority,
		Completed:      completed,
		Favorite:       favorite,
		TagIDs:         req.TagIDs,
		StatusID:       req.StatusID,
		EstimatePoints: req.EstimatePoints,
		SprintID:       req.SprintID,
	}
	newID, err := domain.CreateTask(r.Context(), userID, in)
	if err != nil {
		if errors.Is(err, domain.ErrValidation) {
			utils.APIJSONError(w, http.StatusBadRequest, "invalid_request", err.Error())
			return
		}
		if errors.Is(err, domain.ErrForbidden) {
			utils.APIJSONError(w, http.StatusForbidden, "forbidden", sharingClientMessage(err, "Forbidden."))
			return
		}
		if errors.Is(err, domain.ErrConflict) {
			utils.APIJSONError(w, http.StatusConflict, "conflict", sharingClientMessage(err, "Conflict."))
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
	out := taskToAPIJSON(task)
	out.DeprecationNotice = notice
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(out)
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
	notice := favoriteDeprecationNoticeIfUsed(w, req.Favorite != nil)

	in := domain.UpdateTaskInput{
		Title:       req.Title,
		Description: req.Description,
		DueDate:     req.DueDate,
		Priority:    req.Priority,
		Completed:   req.Completed,
		Favorite:    req.Favorite,
		TagIDs:      req.TagIDs,
		ParentID:    req.ParentID,
		StatusID:    req.StatusID,
	}
	if req.ClearDue != nil && *req.ClearDue {
		in.ClearDue = true
	}
	in.ProjectID = req.ProjectID.toPatchInt(true)
	in.EstimatePoints = req.EstimatePoints.toPatchInt(false)
	in.SprintID = req.SprintID.toPatchInt(true)

	if _, err := domain.UpdateTask(r.Context(), userID, taskID, in); err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			utils.APIJSONError(w, http.StatusNotFound, "not_found", "Task not found.")
			return
		}
		if errors.Is(err, domain.ErrValidation) {
			utils.APIJSONError(w, http.StatusBadRequest, "invalid_request", err.Error())
			return
		}
		if errors.Is(err, domain.ErrForbidden) {
			utils.APIJSONError(w, http.StatusForbidden, "forbidden", sharingClientMessage(err, "Forbidden."))
			return
		}
		if errors.Is(err, domain.ErrConflict) {
			utils.APIJSONError(w, http.StatusConflict, "conflict", err.Error())
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
	out := taskToAPIJSON(task)
	out.DeprecationNotice = notice
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	json.NewEncoder(w).Encode(out)
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

	mode := strings.TrimSpace(r.URL.Query().Get("mode"))
	if mode == "" {
		mode = "cascade"
	}
	if mode != "cascade" && mode != "reparent" {
		utils.APIJSONError(w, http.StatusBadRequest, "invalid_request", "mode must be cascade or reparent.")
		return
	}

	if mode == "reparent" {
		var newParent *int
		if raw := strings.TrimSpace(r.URL.Query().Get("new_parent_id")); raw != "" {
			v, err := strconv.Atoi(raw)
			if err != nil || v < 0 {
				utils.APIJSONError(w, http.StatusBadRequest, "invalid_request", "invalid new_parent_id.")
				return
			}
			if v > 0 {
				newParent = &v
			}
		}
		if err := domain.ReparentChildren(r.Context(), userID, taskID, newParent); err != nil {
			if errors.Is(err, domain.ErrNotFound) {
				utils.APIJSONError(w, http.StatusNotFound, "not_found", "Task not found.")
				return
			}
			if errors.Is(err, domain.ErrValidation) {
				utils.APIJSONError(w, http.StatusBadRequest, "invalid_request", err.Error())
				return
			}
			utils.APIJSONError(w, http.StatusInternalServerError, "internal_error", "Failed to reparent subtasks.")
			return
		}
	}

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
		"ok":         true,
		"undo_token": token,
		"expires_in": utils.UndoTTLSeconds,
	})
}

func apiV1TaskArchive(w http.ResponseWriter, r *http.Request, taskID int) {
	userID, ok := apiUserFromRequest(r)
	if !ok {
		utils.APIJSONError(w, http.StatusUnauthorized, "unauthorized", "Not authenticated.")
		return
	}
	if err := domain.ArchiveTask(r.Context(), userID, taskID); err != nil {
		writeWorkflowDomainError(w, err)
		return
	}
	tz := GetUserTimezoneByID(userID)
	task, err := tasks.FetchTaskByIDForUser(taskID, userID, tz, 1)
	if err != nil {
		utils.APIJSONError(w, http.StatusInternalServerError, "internal_error", "Failed to load task.")
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(taskToAPIJSON(task))
}

func apiV1TaskRestore(w http.ResponseWriter, r *http.Request, taskID int) {
	userID, ok := apiUserFromRequest(r)
	if !ok {
		utils.APIJSONError(w, http.StatusUnauthorized, "unauthorized", "Not authenticated.")
		return
	}
	if err := domain.RestoreTask(r.Context(), userID, taskID); err != nil {
		writeWorkflowDomainError(w, err)
		return
	}
	tz := GetUserTimezoneByID(userID)
	task, err := tasks.FetchTaskByIDForUser(taskID, userID, tz, 1)
	if err != nil {
		utils.APIJSONError(w, http.StatusInternalServerError, "internal_error", "Failed to load task.")
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(taskToAPIJSON(task))
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
	case "set_status":
		if req.StatusID == nil || *req.StatusID <= 0 {
			utils.APIJSONError(w, http.StatusBadRequest, "invalid_request", "status_id is required.")
			return
		}
		err = bulkSetStatus(ctx, db, req.TaskIDs, userID, *req.StatusID)
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
		"ok":       true,
		"restored": len(snaps),
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
			ID:            ev.ID,
			TaskID:        ev.TaskID,
			EventType:     ev.EventType,
			Label:         formatEventLabel(ev.EventType, meta),
			Metadata:      meta,
			CreatedAt:     ev.CreatedAt.UTC().Format("2006-01-02T15:04:05Z07:00"),
			ActorUserID:   ev.UserID,
			ActorUserName: ev.ActorUserName,
			ActorEmail:    ev.ActorEmail,
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
	notice := favoriteDeprecationNoticeIfUsed(w, *req.Favorite)

	var projectFilter *int
	if req.Project != nil {
		projectFilter = parseProjectFilter(*req.Project)
	}

	if err := domain.ReorderTasks(r.Context(), userID, req.TaskIDs, *req.Favorite, projectFilter, req.ParentID, req.StatusID); err != nil {
		if errors.Is(err, domain.ErrValidation) {
			utils.APIJSONError(w, http.StatusBadRequest, "invalid_request", "Task does not belong to user or mismatched favorite group/project.")
			return
		}
		utils.APIJSONError(w, http.StatusInternalServerError, "internal_error", "Failed to reorder tasks.")
		return
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	json.NewEncoder(w).Encode(apiReorderOKResponse{OK: true, DeprecationNotice: notice})
}

// APIV1ProjectsRouter handles /api/v1/projects and /api/v1/projects/{id}[/members|invites|events].
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
	if sub == "reorder" {
		if r.Method != http.MethodPost {
			utils.APIJSONError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed.")
			return
		}
		apiV1ReorderProjects(w, r)
		return
	}
	if handleProjectSubResource(w, r, sub) {
		return
	}
	id, err := strconv.Atoi(sub)
	if err != nil || id <= 0 {
		utils.APIJSONError(w, http.StatusBadRequest, "invalid_request", "Invalid project id.")
		return
	}
	switch r.Method {
	case http.MethodGet:
		apiV1GetProject(w, r, id)
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
	projects, err := storage.GetAccessibleProjects(userID)
	if err != nil {
		utils.APIJSONError(w, http.StatusInternalServerError, "internal_error", "Failed to list projects.")
		return
	}
	out := make([]apiProjectJSON, 0, len(projects))
	for i := range projects {
		out = append(out, projectToAPIJSON(&projects[i]))
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	json.NewEncoder(w).Encode(out)
}

func apiV1GetProject(w http.ResponseWriter, r *http.Request, projectID int) {
	userID, ok := apiUserFromRequest(r)
	if !ok {
		utils.APIJSONError(w, http.StatusUnauthorized, "unauthorized", "Not authenticated.")
		return
	}
	p, err := storage.GetAccessibleProjectByID(projectID, userID)
	if err != nil {
		utils.APIJSONError(w, http.StatusNotFound, "not_found", "Project not found.")
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	json.NewEncoder(w).Encode(projectToAPIJSON(p))
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
	project, err := domain.CreateProject(r.Context(), userID, req.Name, req.Description)
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
	json.NewEncoder(w).Encode(projectStorageToAPIJSON(project, storage.RoleOwner))
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
	if req.Name == nil && req.Description == nil && req.WorkflowMode == nil && req.BacklogName == nil && req.BacklogDescription == nil {
		utils.APIJSONError(w, http.StatusBadRequest, "invalid_request", "Nothing to update.")
		return
	}
	project, err := domain.UpdateProject(r.Context(), userID, projectID, req.Name, req.Description, req.WorkflowMode, req.BacklogName, req.BacklogDescription)
	if err != nil {
		if errors.Is(err, domain.ErrValidation) {
			utils.APIJSONError(w, http.StatusBadRequest, "invalid_request", err.Error())
			return
		}
		if errors.Is(err, domain.ErrForbidden) {
			utils.APIJSONError(w, http.StatusForbidden, "forbidden", "Only the owner can update this project.")
			return
		}
		if errors.Is(err, domain.ErrNotFound) {
			utils.APIJSONError(w, http.StatusNotFound, "not_found", "Project not found.")
			return
		}
		if errors.Is(err, domain.ErrConflict) {
			utils.APIJSONError(w, http.StatusConflict, "conflict", err.Error())
			return
		}
		utils.APIJSONError(w, http.StatusInternalServerError, "internal_error", "Failed to update project.")
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	json.NewEncoder(w).Encode(projectStorageToAPIJSON(project, storage.RoleOwner))
}

func apiV1ReorderProjects(w http.ResponseWriter, r *http.Request) {
	userID, ok := apiUserFromRequest(r)
	if !ok {
		utils.APIJSONError(w, http.StatusUnauthorized, "unauthorized", "Not authenticated.")
		return
	}
	var req apiProjectReorderRequest
	if err := decodeJSONBody(r, &req); err != nil {
		utils.APIJSONError(w, http.StatusBadRequest, "invalid_request", "Invalid JSON body.")
		return
	}
	if err := domain.ReorderProjectsForUser(r.Context(), userID, req.ProjectIDs); err != nil {
		if errors.Is(err, domain.ErrValidation) {
			utils.APIJSONError(w, http.StatusBadRequest, "invalid_request", err.Error())
			return
		}
		utils.APIJSONError(w, http.StatusInternalServerError, "internal_error", "Failed to reorder projects.")
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	json.NewEncoder(w).Encode(apiReorderOKResponse{OK: true})
}

func apiV1DeleteProject(w http.ResponseWriter, r *http.Request, projectID int) {
	userID, ok := apiUserFromRequest(r)
	if !ok {
		utils.APIJSONError(w, http.StatusUnauthorized, "unauthorized", "Not authenticated.")
		return
	}
	if err := domain.DeleteProject(r.Context(), userID, projectID); err != nil {
		if errors.Is(err, domain.ErrForbidden) {
			utils.APIJSONError(w, http.StatusForbidden, "forbidden", "Only the owner can delete this project.")
			return
		}
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
	var projectID *int
	if raw := strings.TrimSpace(r.URL.Query().Get("project_id")); raw != "" {
		pid, err := strconv.Atoi(raw)
		if err != nil || pid < 0 {
			utils.APIJSONError(w, http.StatusBadRequest, "invalid_request", "Invalid project_id.")
			return
		}
		projectID = &pid
	}
	tags, err := domain.ListTags(r.Context(), userID, projectID)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			utils.APIJSONError(w, http.StatusNotFound, "not_found", "Project not found.")
			return
		}
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
	tag, err := domain.CreateTag(r.Context(), userID, req.Name, req.ProjectID)
	if err != nil {
		if errors.Is(err, domain.ErrValidation) {
			utils.APIJSONError(w, http.StatusBadRequest, "invalid_request", err.Error())
			return
		}
		if errors.Is(err, domain.ErrForbidden) {
			utils.APIJSONError(w, http.StatusForbidden, "forbidden", "Not authorized to manage tags.")
			return
		}
		if errors.Is(err, domain.ErrNotFound) {
			utils.APIJSONError(w, http.StatusNotFound, "not_found", "Project not found.")
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
	if req.Name == nil && req.Color == nil {
		utils.APIJSONError(w, http.StatusBadRequest, "invalid_request", "name or color is required.")
		return
	}
	tag, err := domain.UpdateTag(r.Context(), userID, tagID, req.Name, req.Color)
	if err != nil {
		if errors.Is(err, domain.ErrValidation) {
			utils.APIJSONError(w, http.StatusBadRequest, "invalid_request", err.Error())
			return
		}
		if errors.Is(err, domain.ErrForbidden) {
			utils.APIJSONError(w, http.StatusForbidden, "forbidden", "Not authorized to manage tags.")
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
		if errors.Is(err, domain.ErrForbidden) {
			utils.APIJSONError(w, http.StatusForbidden, "forbidden", "Not authorized to manage tags.")
			return
		}
		utils.APIJSONError(w, http.StatusInternalServerError, "internal_error", "Failed to delete tag.")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
