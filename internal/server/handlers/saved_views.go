package handlers

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"GoTodo/internal/query"
	"GoTodo/internal/server/utils"
	"GoTodo/internal/storage"
	"GoTodo/internal/tasks"
)

const (
	maxSavedViewNameLen      = 80
	maxSavedViewSearchLength = 500
	maxSavedViewRequestBytes = 1 << 20
	maxSavedViewSortOrder    = 2147483647
)

type savedViewAPIResponse struct {
	ID          int                     `json:"id"`
	Name        string                  `json:"name"`
	Filter      storage.SavedViewFilter `json:"filter"`
	Definition  query.Definition        `json:"definition"`
	ProjectID   *int                    `json:"project_id"`
	ProjectName string                  `json:"project_name,omitempty"`
	Shared      bool                    `json:"shared"`
	AuthorName  string                  `json:"author_name,omitempty"`
	CanEdit     bool                    `json:"can_edit"`
	OnDashboard bool                    `json:"on_dashboard"`
	SortOrder   int                     `json:"sort_order"`
	CreatedAt   time.Time               `json:"created_at"`
	UpdatedAt   time.Time               `json:"updated_at"`
}

type savedViewAPIRequest struct {
	Name       *string                  `json:"name"`
	Filter     *storage.SavedViewFilter `json:"filter"`
	Definition *query.Definition        `json:"definition"`
	ProjectID  *int                     `json:"project_id"`
	SortOrder  *int                     `json:"sort_order"`
}

// queryLoader is the access loader for saved queries; tests may swap it.
var queryLoader = query.DBLoader

// APIV1SavedViewsRouter handles /api/v2/saved-views and its sub-resources:
//
//	GET, POST            /saved-views
//	GET, PUT, PATCH, DEL /saved-views/{id}
//	GET                  /saved-views/{id}/results
//	GET                  /saved-views/{id}/export
//	PUT, DELETE          /saved-views/{id}/dashboard
func APIV1SavedViewsRouter(w http.ResponseWriter, r *http.Request) {
	userID, ok := apiUserFromRequest(r)
	if !ok {
		utils.APIJSONError(w, http.StatusUnauthorized, "unauthorized", "Not authenticated.")
		return
	}

	subpath := utils.ParseAPIV1Subpath(r, "saved-views")
	if subpath == "" {
		switch r.Method {
		case http.MethodGet:
			apiV1ListSavedViews(w, userID)
		case http.MethodPost:
			apiV1CreateSavedView(w, r, userID)
		default:
			w.Header().Set("Allow", "GET, POST")
			utils.APIJSONError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed.")
		}
		return
	}

	parts := strings.Split(subpath, "/")
	id, err := strconv.Atoi(parts[0])
	if err != nil || id <= 0 || len(parts) > 2 {
		utils.APIJSONError(w, http.StatusBadRequest, "invalid_request", "Invalid saved view id.")
		return
	}

	if len(parts) == 2 {
		switch parts[1] {
		case "results":
			if r.Method != http.MethodGet {
				w.Header().Set("Allow", "GET")
				utils.APIJSONError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed.")
				return
			}
			apiV1SavedViewResults(w, r, userID, id)
		case "export":
			if r.Method != http.MethodGet {
				w.Header().Set("Allow", "GET")
				utils.APIJSONError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed.")
				return
			}
			apiV1SavedViewExport(w, userID, id)
		case "dashboard":
			switch r.Method {
			case http.MethodPut:
				apiV1SavedViewDashboard(w, userID, id, true)
			case http.MethodDelete:
				apiV1SavedViewDashboard(w, userID, id, false)
			default:
				w.Header().Set("Allow", "PUT, DELETE")
				utils.APIJSONError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed.")
			}
		default:
			utils.APIJSONError(w, http.StatusNotFound, "not_found", "Not found.")
		}
		return
	}

	switch r.Method {
	case http.MethodGet:
		apiV1GetSavedView(w, userID, id)
	case http.MethodPut, http.MethodPatch:
		apiV1UpdateSavedView(w, r, userID, id)
	case http.MethodDelete:
		apiV1DeleteSavedView(w, userID, id)
	default:
		w.Header().Set("Allow", "GET, PUT, PATCH, DELETE")
		utils.APIJSONError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed.")
	}
}

func apiV1ListSavedViews(w http.ResponseWriter, userID int) {
	views, err := storage.ListVisibleSavedViews(userID)
	if err != nil {
		utils.APIJSONError(w, http.StatusInternalServerError, "internal_error", "Failed to list saved views.")
		return
	}
	perms := newSharedQueryPerms(userID)
	response := make([]savedViewAPIResponse, 0, len(views))
	for _, view := range views {
		response = append(response, savedViewToAPIResponse(view, userID, perms))
	}
	writeSavedViewAPIJSON(w, http.StatusOK, response)
}

func apiV1GetSavedView(w http.ResponseWriter, userID, id int) {
	view, err := storage.GetVisibleSavedView(id, userID)
	if err != nil {
		writeSavedViewStorageError(w, err)
		return
	}
	writeSavedViewAPIJSON(w, http.StatusOK, savedViewToAPIResponse(*view, userID, newSharedQueryPerms(userID)))
}

func apiV1CreateSavedView(w http.ResponseWriter, r *http.Request, userID int) {
	var request savedViewAPIRequest
	if err := decodeSavedViewJSON(w, r, &request); err != nil {
		utils.APIJSONError(w, http.StatusBadRequest, "invalid_request", "Request body must be a single valid JSON object.")
		return
	}

	name, err := validateSavedViewName(request.Name)
	if err != nil {
		utils.APIJSONError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	filter, err := validateSavedViewFilter(request.Filter)
	if err != nil {
		utils.APIJSONError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	if err := validateSavedViewSortOrder(request.SortOrder); err != nil {
		utils.APIJSONError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}

	var projectID *int
	if request.ProjectID != nil && *request.ProjectID > 0 {
		if status, code, msg := authorizeSharedQueryEdit(userID, *request.ProjectID); status != 0 {
			utils.APIJSONError(w, status, code, msg)
			return
		}
		pid := *request.ProjectID
		projectID = &pid
	} else if request.ProjectID != nil && *request.ProjectID != 0 {
		utils.APIJSONError(w, http.StatusBadRequest, "invalid_request", "project_id must be a positive project id.")
		return
	}

	def, ok := savedViewDefinitionFromRequest(w, userID, projectID, request.Definition, filter)
	if !ok {
		return
	}

	view, err := storage.CreateSavedViewFull(storage.SavedViewInput{
		UserID:     userID,
		ProjectID:  projectID,
		Name:       name,
		Filter:     filter,
		Definition: def,
		SortOrder:  request.SortOrder,
	})
	if err != nil {
		writeSavedViewStorageError(w, err)
		return
	}

	basePath := strings.TrimSuffix(utils.GetBasePath(), "/")
	w.Header().Set("Location", basePath+"/api/v2/saved-views/"+strconv.Itoa(view.ID))
	writeSavedViewAPIJSON(w, http.StatusCreated, savedViewToAPIResponse(*view, userID, newSharedQueryPerms(userID)))
}

func apiV1UpdateSavedView(w http.ResponseWriter, r *http.Request, userID, id int) {
	var request savedViewAPIRequest
	if err := decodeSavedViewJSON(w, r, &request); err != nil {
		utils.APIJSONError(w, http.StatusBadRequest, "invalid_request", "Request body must be a single valid JSON object.")
		return
	}
	if request.Name == nil && request.Filter == nil && request.Definition == nil && request.SortOrder == nil {
		utils.APIJSONError(w, http.StatusBadRequest, "invalid_request", "At least one field must be provided.")
		return
	}
	if r.Method == http.MethodPut && (request.Name == nil || (request.Filter == nil && request.Definition == nil)) {
		utils.APIJSONError(w, http.StatusBadRequest, "invalid_request", "PUT requires name and either filter or definition.")
		return
	}

	view, err := storage.GetVisibleSavedView(id, userID)
	if err != nil {
		writeSavedViewStorageError(w, err)
		return
	}
	if !canEditSavedView(*view, userID, newSharedQueryPerms(userID)) {
		utils.APIJSONError(w, http.StatusForbidden, "forbidden", "You need the Manage shared queries permission on this project.")
		return
	}
	if request.ProjectID != nil {
		current := 0
		if view.ProjectID != nil {
			current = *view.ProjectID
		}
		if *request.ProjectID != current {
			utils.APIJSONError(w, http.StatusBadRequest, "invalid_request", "project_id cannot be changed; save a copy instead.")
			return
		}
	}

	patch := storage.SavedViewPatch{SortOrder: request.SortOrder}
	if request.Name != nil {
		validated, err := validateSavedViewName(request.Name)
		if err != nil {
			utils.APIJSONError(w, http.StatusBadRequest, "invalid_request", err.Error())
			return
		}
		patch.Name = &validated
	}
	if request.Filter != nil {
		validated, err := validateSavedViewFilter(request.Filter)
		if err != nil {
			utils.APIJSONError(w, http.StatusBadRequest, "invalid_request", err.Error())
			return
		}
		patch.Filter = &validated
	}
	if err := validateSavedViewSortOrder(request.SortOrder); err != nil {
		utils.APIJSONError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	// A new definition, or a new filter from an older client, rewrites the
	// definition; either way it is checked against the caller's access.
	if request.Definition != nil || patch.Filter != nil {
		filter := view.Filter
		if patch.Filter != nil {
			filter = *patch.Filter
		}
		def, ok := savedViewDefinitionFromRequest(w, userID, view.ProjectID, request.Definition, filter)
		if !ok {
			return
		}
		patch.Definition = def
	}

	updated, err := storage.PatchSavedViewFields(id, patch)
	if err != nil {
		writeSavedViewStorageError(w, err)
		return
	}
	updated.OnDashboard = view.OnDashboard
	writeSavedViewAPIJSON(w, http.StatusOK, savedViewToAPIResponse(*updated, userID, newSharedQueryPerms(userID)))
}

func apiV1DeleteSavedView(w http.ResponseWriter, userID, id int) {
	view, err := storage.GetVisibleSavedView(id, userID)
	if err != nil {
		writeSavedViewStorageError(w, err)
		return
	}
	if !canEditSavedView(*view, userID, newSharedQueryPerms(userID)) {
		utils.APIJSONError(w, http.StatusForbidden, "forbidden", "You need the Manage shared queries permission on this project.")
		return
	}
	if err := storage.DeleteSavedViewByID(id); err != nil {
		writeSavedViewStorageError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func apiV1SavedViewDashboard(w http.ResponseWriter, userID, id int, on bool) {
	if _, err := storage.GetVisibleSavedView(id, userID); err != nil {
		writeSavedViewStorageError(w, err)
		return
	}
	if err := storage.SetSavedViewOnDashboard(userID, id, on); err != nil {
		utils.APIJSONError(w, http.StatusInternalServerError, "internal_error", "Failed to update dashboard.")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func apiV1SavedViewResults(w http.ResponseWriter, r *http.Request, userID, id int) {
	view, err := storage.GetVisibleSavedView(id, userID)
	if err != nil {
		writeSavedViewStorageError(w, err)
		return
	}
	page, pageSize, ok := queryPaging(w, r.URL.Query().Get("page"), r.URL.Query().Get("page_size"))
	if !ok {
		return
	}
	def, qctx, unavailable, err := prepareSavedViewRun(userID, *view)
	if err != nil {
		writeQueryError(w, err)
		return
	}
	res, err := tasks.RunQuery(def, qctx, page, pageSize)
	if err != nil {
		writeQueryError(w, err)
		return
	}
	writeSavedViewAPIJSON(w, http.StatusOK, queryResultsResponse(def, res, page, pageSize, unavailable))
}

func apiV1SavedViewExport(w http.ResponseWriter, userID, id int) {
	view, err := storage.GetVisibleSavedView(id, userID)
	if err != nil {
		writeSavedViewStorageError(w, err)
		return
	}
	def, qctx, _, err := prepareSavedViewRun(userID, *view)
	if err != nil {
		writeQueryError(w, err)
		return
	}
	list, err := tasks.RunQueryExport(def, qctx)
	if err != nil {
		writeQueryError(w, err)
		return
	}
	writeQueryCSV(w, view.Name, def, list)
}

// prepareSavedViewRun loads a saved view's definition for running. References
// the caller has lost access to match nothing. A shared view only ever runs
// against its own project.
func prepareSavedViewRun(userID int, view storage.SavedView) (query.Definition, query.Context, query.Unavailable, error) {
	def := storedDefinition(view)
	force := 0
	if view.ProjectID != nil {
		force = *view.ProjectID
	}
	return queryLoader.Prepare(query.Request{
		UserID:       userID,
		Timezone:     GetUserTimezoneByID(userID),
		Now:          time.Now(),
		ForceProject: force,
		Lenient:      true,
	}, def)
}

// storedDefinition returns a view's definition, converting a version 1
// filter when the view predates definitions.
func storedDefinition(view storage.SavedView) query.Definition {
	if len(view.Definition) > 0 {
		if def, err := query.Parse(view.Definition); err == nil {
			return def
		}
	}
	return query.FromLegacyFilter(view.Filter, legacyTagName)
}

func legacyTagName(id int) (string, bool) {
	t, err := storage.GetTag(id)
	if err != nil || t == nil {
		return "", false
	}
	return t.Name, true
}

// savedViewDefinitionFromRequest validates the definition to store: the one
// sent, or one converted from the filter. Every reference is checked against
// the caller's access; a shared view may only reference its own project.
func savedViewDefinitionFromRequest(w http.ResponseWriter, userID int, projectID *int, sent *query.Definition, filter storage.SavedViewFilter) (json.RawMessage, bool) {
	var def query.Definition
	if sent != nil {
		def = *sent
	} else {
		def = query.FromLegacyFilter(filter, legacyTagName)
	}
	force := 0
	if projectID != nil {
		force = *projectID
	}
	norm, _, _, err := queryLoader.Prepare(query.Request{
		UserID:       userID,
		Timezone:     GetUserTimezoneByID(userID),
		Now:          time.Now(),
		ForceProject: force,
	}, def)
	if err != nil {
		writeQueryError(w, err)
		return nil, false
	}
	raw, err := json.Marshal(norm)
	if err != nil {
		utils.APIJSONError(w, http.StatusInternalServerError, "internal_error", "Failed to encode query.")
		return nil, false
	}
	return raw, true
}

// sharedQueryPerms caches, per request, whether the user may edit each
// project's shared queries.
type sharedQueryPerms struct {
	userID int
	cache  map[int]bool
}

func newSharedQueryPerms(userID int) *sharedQueryPerms {
	return &sharedQueryPerms{userID: userID, cache: map[int]bool{}}
}

func (p *sharedQueryPerms) canManage(projectID int) bool {
	if v, ok := p.cache[projectID]; ok {
		return v
	}
	v := false
	if proj, err := storage.GetAccessibleProjectByID(projectID, p.userID); err == nil && proj != nil {
		v = storage.HasProjectPerm(projectID, proj.Role, storage.PermQueriesManage)
	}
	p.cache[projectID] = v
	return v
}

func canEditSavedView(view storage.SavedView, userID int, perms *sharedQueryPerms) bool {
	if view.IsShared() {
		return perms.canManage(*view.ProjectID)
	}
	return view.UserID == userID
}

// authorizeSharedQueryEdit checks the user may create shared queries on
// projectID. A project they cannot open reads as not found.
func authorizeSharedQueryEdit(userID, projectID int) (int, string, string) {
	proj, err := storage.GetAccessibleProjectByID(projectID, userID)
	if err != nil || proj == nil {
		return http.StatusNotFound, "not_found", "Project not found."
	}
	if !storage.HasProjectPerm(projectID, proj.Role, storage.PermQueriesManage) {
		return http.StatusForbidden, "forbidden", "You need the Manage shared queries permission on this project."
	}
	return 0, "", ""
}

func validateSavedViewName(name *string) (string, error) {
	if name == nil {
		return "", errors.New("name is required.")
	}
	trimmed := strings.TrimSpace(*name)
	if trimmed == "" {
		return "", errors.New("name is required.")
	}
	if utf8.RuneCountInString(trimmed) > maxSavedViewNameLen {
		return "", errors.New("name must be 80 characters or less.")
	}
	return trimmed, nil
}

func validateSavedViewFilter(filter *storage.SavedViewFilter) (storage.SavedViewFilter, error) {
	if filter == nil {
		return storage.SavedViewFilter{}, nil
	}

	normalized := storage.SavedViewFilter{
		Search: strings.TrimSpace(filter.Search),
	}
	if utf8.RuneCountInString(normalized.Search) > maxSavedViewSearchLength {
		return storage.SavedViewFilter{}, errors.New("filter.search must be 500 characters or less.")
	}

	project := strings.ToLower(strings.TrimSpace(filter.Project))
	switch project {
	case "":
	case "none", "0":
		normalized.Project = project
	default:
		id, err := strconv.Atoi(project)
		if err != nil || id <= 0 {
			return storage.SavedViewFilter{}, errors.New("filter.project must be a positive id, 0, or \"none\".")
		}
		normalized.Project = strconv.Itoa(id)
	}

	rawStatus := strings.TrimSpace(filter.Status)
	normalized.Status = normalizeStatusFilter(rawStatus)
	if rawStatus != "" && normalized.Status == "" {
		return storage.SavedViewFilter{}, errors.New("filter.status must be \"complete\" or \"incomplete\".")
	}

	rawDue := strings.TrimSpace(filter.Due)
	normalized.Due = normalizeDueFilter(rawDue)
	if rawDue != "" && normalized.Due == "" {
		return storage.SavedViewFilter{}, errors.New("filter.due must be \"overdue\", \"today\", \"week\", \"through_week\", \"none\", or \"range\".")
	}

	rawCompleted := strings.TrimSpace(filter.Completed)
	normalized.Completed = normalizeCompletedFilter(rawCompleted)
	if rawCompleted != "" && normalized.Completed == "" {
		return storage.SavedViewFilter{}, errors.New("filter.completed must be \"week\".")
	}

	rawPriority := strings.TrimSpace(filter.Priority)
	normalized.Priority = normalizePriorityFilter(rawPriority)
	if rawPriority != "" && normalized.Priority == "" {
		return storage.SavedViewFilter{}, errors.New("filter.priority must be 0-3, or a comma-separated list of them.")
	}

	rawTag := strings.TrimSpace(filter.Tag)
	normalized.Tag = normalizeTagFilter(rawTag)
	if rawTag != "" && normalized.Tag == "" {
		return storage.SavedViewFilter{}, errors.New("filter.tag must be tag ids or names (max 50 characters each, up to 10, comma-separated).")
	}

	rawSort := strings.TrimSpace(filter.Sort)
	normalized.Sort = normalizeSortFilter(rawSort)
	if rawSort != "" && normalized.Sort == "" {
		return storage.SavedViewFilter{}, errors.New("filter.sort must be \"priority\".")
	}

	for _, d := range []struct {
		raw  string
		dest *string
		name string
	}{{filter.DueFrom, &normalized.DueFrom, "due_from"}, {filter.DueTo, &normalized.DueTo, "due_to"}} {
		if strings.TrimSpace(d.raw) == "" {
			continue
		}
		if *d.dest = normalizeDateFilter(d.raw); *d.dest == "" {
			return storage.SavedViewFilter{}, errors.New("filter." + d.name + " must be a date (YYYY-MM-DD).")
		}
	}

	rawClaimed := strings.TrimSpace(filter.Claimed)
	normalized.Claimed = tasks.NormalizeClaimFilter(rawClaimed)
	if rawClaimed != "" && normalized.Claimed == "" {
		return storage.SavedViewFilter{}, errors.New("filter.claimed must be \"me\" or \"none\".")
	}
	return normalized, nil
}

func validateSavedViewSortOrder(sortOrder *int) error {
	if sortOrder != nil && (*sortOrder < 0 || *sortOrder > maxSavedViewSortOrder) {
		return errors.New("sort_order must be between 0 and 2147483647.")
	}
	return nil
}

func decodeSavedViewJSON(w http.ResponseWriter, r *http.Request, destination any) error {
	r.Body = http.MaxBytesReader(w, r.Body, maxSavedViewRequestBytes)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return errors.New("request body must contain one JSON object")
	}
	return nil
}

func savedViewToAPIResponse(view storage.SavedView, userID int, perms *sharedQueryPerms) savedViewAPIResponse {
	def := storedDefinition(view)
	if norm, err := query.Normalize(def, nil); err == nil {
		def = norm
	} else if def.Columns == nil {
		// Custom-field clauses fail Normalize without field types; keep the
		// stored form, which was normalized when saved.
		def.Columns = query.DefaultColumns()
	}
	out := savedViewAPIResponse{
		ID:          view.ID,
		Name:        view.Name,
		Filter:      view.Filter,
		Definition:  def,
		ProjectID:   view.ProjectID,
		Shared:      view.IsShared(),
		CanEdit:     canEditSavedView(view, userID, perms),
		OnDashboard: view.OnDashboard,
		SortOrder:   view.SortOrder,
		CreatedAt:   view.CreatedAt,
		UpdatedAt:   view.UpdatedAt,
	}
	if view.IsShared() {
		out.ProjectName = view.ProjectName
		out.AuthorName = view.AuthorName
	}
	return out
}

func writeSavedViewStorageError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, storage.ErrSavedViewNotFound):
		utils.APIJSONError(w, http.StatusNotFound, "not_found", "Saved view not found.")
	case errors.Is(err, storage.ErrSavedViewLimit):
		utils.APIJSONError(w, http.StatusConflict, "limit_reached",
			"A maximum of "+strconv.Itoa(storage.MaxSavedViewsPerUser)+" personal saved views (or "+
				strconv.Itoa(storage.MaxSharedViewsPerProject)+" shared queries per project) is allowed.")
	case errors.Is(err, storage.ErrSavedViewNameConflict):
		utils.APIJSONError(w, http.StatusConflict, "name_conflict", "A saved view with this name already exists.")
	default:
		utils.APIJSONError(w, http.StatusInternalServerError, "internal_error", "Saved view operation failed.")
	}
}

func writeSavedViewAPIJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
