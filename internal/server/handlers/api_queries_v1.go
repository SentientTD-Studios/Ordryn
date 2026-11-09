package handlers

import (
	"encoding/csv"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"GoTodo/internal/extensions"
	"GoTodo/internal/query"
	"GoTodo/internal/server/utils"
	"GoTodo/internal/storage"
	"GoTodo/internal/tasks"
)

type queryRunRequest struct {
	Definition *query.Definition `json:"definition"`
	// ProjectID previews a shared query: the run is pinned to that project.
	ProjectID *int `json:"project_id"`
	Page      int  `json:"page"`
	PageSize  int  `json:"page_size"`
}

type queryTaskJSON struct {
	apiTaskJSON
	Children      []queryTaskJSON `json:"children,omitempty"`
	CreatedBy     *int            `json:"created_by"`
	CreatedByName string          `json:"created_by_name,omitempty"`
	CompletedDate string          `json:"completed_date,omitempty"`
	IsFavorite    bool            `json:"is_favorite,omitempty"`
}

type queryResultsJSON struct {
	Tasks       []queryTaskJSON    `json:"tasks"`
	Total       int                `json:"total"`
	Page        int                `json:"page"`
	PageSize    int                `json:"page_size"`
	TotalPages  int                `json:"total_pages"`
	Mode        string             `json:"mode"`
	Columns     []string           `json:"columns"`
	Unavailable *query.Unavailable `json:"unavailable,omitempty"`
}

// APIV1QueriesRouter handles /api/v2/queries/run, /queries/export and /queries/options.
func APIV1QueriesRouter(w http.ResponseWriter, r *http.Request) {
	userID, ok := apiUserFromRequest(r)
	if !ok {
		utils.APIJSONError(w, http.StatusUnauthorized, "unauthorized", "Not authenticated.")
		return
	}
	switch utils.ParseAPIV1Subpath(r, "queries") {
	case "run":
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", "POST")
			utils.APIJSONError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed.")
			return
		}
		apiV1QueryRun(w, r, userID, false)
	case "export":
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", "POST")
			utils.APIJSONError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed.")
			return
		}
		apiV1QueryRun(w, r, userID, true)
	case "options":
		if r.Method != http.MethodGet {
			w.Header().Set("Allow", "GET")
			utils.APIJSONError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed.")
			return
		}
		apiV1QueryOptions(w, r, userID)
	default:
		utils.APIJSONError(w, http.StatusNotFound, "not_found", "Not found.")
	}
}

// queryForceProject decides the project a run is pinned to. A project-scoped
// API key always pins to its project; otherwise an explicit project_id (a
// shared-query preview) must be a project the caller can open.
func queryForceProject(r *http.Request, userID int, requested *int) (int, int, string, string) {
	if p, ok := utils.GetAPIKeyPrincipal(r); ok && p.IsProjectScoped() {
		if requested != nil && *requested != 0 && *requested != p.ProjectID {
			return 0, http.StatusForbidden, "forbidden", "This API key is limited to project " + strconv.Itoa(p.ProjectID) + "."
		}
		return p.ProjectID, 0, "", ""
	}
	if requested == nil || *requested == 0 {
		return 0, 0, "", ""
	}
	if *requested < 0 {
		return 0, http.StatusBadRequest, "invalid_request", "project_id must be a positive project id."
	}
	if proj, err := storage.GetAccessibleProjectByID(*requested, userID); err != nil || proj == nil {
		return 0, http.StatusNotFound, "not_found", "Project not found."
	}
	return *requested, 0, "", ""
}

func apiV1QueryRun(w http.ResponseWriter, r *http.Request, userID int, export bool) {
	var req queryRunRequest
	if err := decodeSavedViewJSON(w, r, &req); err != nil {
		utils.APIJSONError(w, http.StatusBadRequest, "invalid_request", "Request body must be a single valid JSON object.")
		return
	}
	if req.Definition == nil {
		utils.APIJSONError(w, http.StatusBadRequest, "invalid_request", "definition is required.")
		return
	}
	force, status, code, msg := queryForceProject(r, userID, req.ProjectID)
	if status != 0 {
		utils.APIJSONError(w, status, code, msg)
		return
	}
	def, qctx, _, err := queryLoader.Prepare(query.Request{
		UserID:       userID,
		Timezone:     GetUserTimezoneByID(userID),
		Now:          time.Now(),
		ForceProject: force,
	}, *req.Definition)
	if err != nil {
		writeQueryError(w, err)
		return
	}
	if export {
		list, err := tasks.RunQueryExport(def, qctx)
		if err != nil {
			writeQueryError(w, err)
			return
		}
		writeQueryCSV(w, "query", def, list)
		return
	}
	page, pageSize, ok := queryPaging(w, strconv.Itoa(req.Page), strconv.Itoa(req.PageSize))
	if !ok {
		return
	}
	res, err := tasks.RunQuery(def, qctx, page, pageSize)
	if err != nil {
		writeQueryError(w, err)
		return
	}
	writeSavedViewAPIJSON(w, http.StatusOK, queryResultsResponse(def, res, page, pageSize, query.Unavailable{}))
}

// queryPaging reads page and page_size; "0" or "" means the default. A
// page_size of -1 asks for the count only.
func queryPaging(w http.ResponseWriter, rawPage, rawSize string) (int, int, bool) {
	page, size := 1, tasks.QueryDefaultPageSize
	if s := strings.TrimSpace(rawPage); s != "" && s != "0" {
		n, err := strconv.Atoi(s)
		if err != nil || n < 1 || n > 100000 {
			utils.APIJSONError(w, http.StatusBadRequest, "invalid_request", "page must be a positive number.")
			return 0, 0, false
		}
		page = n
	}
	if s := strings.TrimSpace(rawSize); s != "" && s != "0" {
		n, err := strconv.Atoi(s)
		if err != nil || n < -1 || n > tasks.QueryMaxPageSize {
			utils.APIJSONError(w, http.StatusBadRequest, "invalid_request",
				"page_size must be between 1 and "+strconv.Itoa(tasks.QueryMaxPageSize)+", or -1 for a count only.")
			return 0, 0, false
		}
		size = n
		if n == -1 {
			size = 0
		}
	}
	return page, size, true
}

func queryResultsResponse(def query.Definition, res tasks.QueryResult, page, pageSize int, unavailable query.Unavailable) queryResultsJSON {
	out := queryResultsJSON{
		Tasks:    make([]queryTaskJSON, 0, len(res.Tasks)),
		Total:    res.Total,
		Page:     page,
		PageSize: pageSize,
		Mode:     def.Mode,
		Columns:  def.Columns,
	}
	if pageSize > 0 {
		out.TotalPages = (res.Total + pageSize - 1) / pageSize
	}
	for _, t := range res.Tasks {
		out.Tasks = append(out.Tasks, queryTaskToJSON(t))
	}
	if !unavailable.Empty() {
		u := unavailable
		out.Unavailable = &u
	}
	return out
}

func queryTaskToJSON(t tasks.Task) queryTaskJSON {
	children := t.Children
	t.Children = nil
	out := queryTaskJSON{apiTaskJSON: taskToAPIJSONOpts(t, false), CompletedDate: t.CompletedDate, IsFavorite: t.IsFavorite}
	if t.CreatedByID > 0 {
		id := t.CreatedByID
		out.CreatedBy = &id
		out.CreatedByName = t.CreatedByName
	}
	for _, c := range children {
		out.Children = append(out.Children, queryTaskToJSON(c))
	}
	return out
}

func writeQueryError(w http.ResponseWriter, err error) {
	if query.IsValidationError(err) {
		utils.APIJSONError(w, http.StatusBadRequest, "invalid_query", err.Error())
		return
	}
	if strings.Contains(err.Error(), "statement timeout") || strings.Contains(err.Error(), "canceling statement") {
		utils.APIJSONError(w, http.StatusUnprocessableEntity, "query_timeout", "The query took too long. Narrow it with more clauses or fewer projects.")
		return
	}
	utils.APIJSONError(w, http.StatusInternalServerError, "internal_error", "Query failed.")
}

var unsafeFilenameRe = regexp.MustCompile(`[^A-Za-z0-9._-]+`)

// csvSafe stops spreadsheet apps from reading a cell as a formula.
func csvSafe(s string) string {
	if s != "" && strings.ContainsRune("=+-@\t\r", rune(s[0])) {
		return "'" + s
	}
	return s
}

func writeQueryCSV(w http.ResponseWriter, name string, def query.Definition, list []tasks.Task) {
	cols := def.Columns
	stamp := time.Now().UTC().Format("20060102")
	file := strings.Trim(unsafeFilenameRe.ReplaceAllString(name, "-"), "-")
	if file == "" {
		file = "query"
	}
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s-%s.csv"`, file, stamp))
	cw := csv.NewWriter(w)
	_ = cw.Write(cols)
	for _, t := range list {
		row := make([]string, 0, len(cols))
		for _, c := range cols {
			row = append(row, csvSafe(queryColumnText(t, c)))
		}
		_ = cw.Write(row)
	}
	cw.Flush()
}

func queryColumnText(t tasks.Task, col string) string {
	switch col {
	case "id":
		return strconv.Itoa(t.ID)
	case "title":
		return t.Title
	case "description":
		return t.Description
	case "project":
		return t.ProjectName
	case "completed":
		return strconv.FormatBool(t.Completed)
	case "workflow_status":
		return t.StatusName
	case "priority":
		return []string{"None", "Low", "Medium", "High"}[clampPriority(t.Priority)]
	case "tags":
		names := make([]string, 0, len(t.Tags))
		for _, tg := range t.Tags {
			names = append(names, tg.Name)
		}
		return strings.Join(names, ";")
	case "due_date":
		return t.DueDate
	case "created_date":
		return t.DateCreated
	case "modified_date":
		return t.DateModified
	case "completed_date":
		return t.CompletedDate
	case "claimed_by":
		return t.ClaimedByName
	case "created_by":
		return t.CreatedByName
	case "sprint":
		return t.SprintName
	case "estimate":
		if t.EstimatePoints != nil {
			return strconv.Itoa(*t.EstimatePoints)
		}
		return ""
	case "parent":
		return t.ParentTitle
	case "is_subtask":
		return strconv.FormatBool(t.ParentID > 0)
	case "has_subtasks":
		return strconv.FormatBool(t.ChildCount > 0)
	case "favorite":
		return strconv.FormatBool(t.IsFavorite)
	}
	if strings.HasPrefix(col, "cf:") {
		key := strings.TrimPrefix(col, "cf:")
		for _, f := range t.Fields {
			if f.Key != key {
				continue
			}
			if m, ok := f.Value.(map[string]any); ok {
				if name, ok := m["user_name"].(string); ok {
					return name
				}
			}
			return fmt.Sprint(f.Value)
		}
	}
	return ""
}

func clampPriority(p int) int {
	if p < 0 {
		return 0
	}
	if p > 3 {
		return 3
	}
	return p
}

// ---- options ----

type queryFieldJSON struct {
	ID        string            `json:"id"`
	Label     string            `json:"label"`
	Type      string            `json:"type"`
	Operators []string          `json:"operators"`
	Sortable  bool              `json:"sortable"`
	Options   []queryOptionJSON `json:"options,omitempty"`
}

type queryOptionJSON struct {
	Value string `json:"value"`
	Label string `json:"label"`
}

type queryProjectJSON struct {
	ID               int    `json:"id"`
	Name             string `json:"name"`
	Archived         bool   `json:"archived"`
	ViewOnly         bool   `json:"view_only"`
	CanManageQueries bool   `json:"can_manage_queries"`
}

type queryPersonJSON struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

type querySprintJSON struct {
	ID        int    `json:"id"`
	ProjectID int    `json:"project_id"`
	Name      string `json:"name"`
	Active    bool   `json:"active"`
}

type queryOptionsJSON struct {
	Fields     []queryFieldJSON   `json:"fields"`
	Columns    []queryOptionJSON  `json:"columns"`
	Projects   []queryProjectJSON `json:"projects"`
	People     []queryPersonJSON  `json:"people"`
	Statuses   []string           `json:"statuses"`
	Tags       []string           `json:"tags"`
	Sprints    []querySprintJSON  `json:"sprints"`
	DateMacros []string           `json:"date_macros"`
	Limits     map[string]int     `json:"limits"`
}

var queryFieldLabels = map[string]string{
	"id": "ID", "title": "Title", "description": "Description", "text": "Title, description or tag",
	"project": "Project", "completed": "Completed", "workflow_status": "Status", "priority": "Priority",
	"tags": "Tags", "due_date": "Due date", "created_date": "Created date", "modified_date": "Changed date",
	"completed_date": "Completed date", "claimed_by": "Claimed by", "created_by": "Created by",
	"sprint": "Sprint", "estimate": "Estimate", "is_subtask": "Is a subtask", "has_subtasks": "Has subtasks",
	"favorite": "Favorite", "parent": "Parent",
}

// apiV1QueryOptions lists what the query editor can offer the caller: only
// projects they can open and the people, statuses, tags, sprints and custom
// fields inside them. ?project_id= narrows everything to one project (the
// editor for that project's shared queries).
func apiV1QueryOptions(w http.ResponseWriter, r *http.Request, userID int) {
	projs, err := storage.GetAccessibleProjects(userID)
	if err != nil {
		utils.APIJSONError(w, http.StatusInternalServerError, "internal_error", "Failed to load projects.")
		return
	}
	var requested *int
	if s := strings.TrimSpace(r.URL.Query().Get("project_id")); s != "" {
		n, err := strconv.Atoi(s)
		if err != nil || n <= 0 {
			utils.APIJSONError(w, http.StatusBadRequest, "invalid_request", "project_id must be a positive project id.")
			return
		}
		requested = &n
	}
	force, status, code, msg := queryForceProject(r, userID, requested)
	if status != 0 {
		utils.APIJSONError(w, status, code, msg)
		return
	}

	out := queryOptionsJSON{
		Fields:     []queryFieldJSON{},
		Columns:    []queryOptionJSON{},
		Projects:   []queryProjectJSON{},
		People:     []queryPersonJSON{},
		DateMacros: []string{"@today", "@today-7", "@today+7", "@start_of_week", "@start_of_month", "@start_of_year"},
		Limits: map[string]int{
			"clauses": query.MaxClauses, "depth": query.MaxDepth, "list_values": query.MaxListValues,
			"columns": query.MaxColumns, "sort_keys": query.MaxSortKeys, "page_size": tasks.QueryMaxPageSize,
			"export_rows": tasks.QueryExportLimit,
		},
	}
	ids := make([]int, 0, len(projs))
	for _, p := range projs {
		if force > 0 && p.ID != force {
			continue
		}
		ids = append(ids, p.ID)
		out.Projects = append(out.Projects, queryProjectJSON{
			ID:               p.ID,
			Name:             p.Name,
			Archived:         p.Archived,
			ViewOnly:         p.Role == "viewer" || p.Role == storage.CompanyViewRoleSlug,
			CanManageQueries: storage.HasProjectPerm(p.ID, p.Role, storage.PermQueriesManage),
		})
	}

	people, err := storage.VisibleQueryPeople(userID, nil, ids)
	if err != nil {
		utils.APIJSONError(w, http.StatusInternalServerError, "internal_error", "Failed to load people.")
		return
	}
	for _, p := range people {
		out.People = append(out.People, queryPersonJSON{ID: p.ID, Name: p.Name})
	}
	names, err := storage.LoadQueryOptionNames(userID, ids)
	if err != nil {
		utils.APIJSONError(w, http.StatusInternalServerError, "internal_error", "Failed to load query options.")
		return
	}
	out.Statuses, out.Tags = names.Statuses, names.Tags
	out.Sprints = make([]querySprintJSON, 0, len(names.Sprints))
	for _, s := range names.Sprints {
		out.Sprints = append(out.Sprints, querySprintJSON{ID: s.ID, ProjectID: s.ProjectID, Name: s.Name, Active: s.Active})
	}

	fields, err := storage.QueryableCustomFields(ids, extensions.FieldExtensionIDs())
	if err != nil {
		utils.APIJSONError(w, http.StatusInternalServerError, "internal_error", "Failed to load custom fields.")
		return
	}
	ctx := query.Context{CustomFields: map[string]query.CustomField{}}
	for k, f := range fields {
		ctx.CustomFields[k] = query.CustomField{Type: f.Def.Type, Projects: f.Projects}
	}

	sortable := map[string]bool{}
	for _, f := range query.SortableFields() {
		sortable[f] = true
	}
	for _, f := range query.BuiltinFieldNames() {
		out.Fields = append(out.Fields, queryFieldJSON{
			ID: f, Label: queryFieldLabels[f], Type: query.FieldType(f, ctx),
			Operators: query.OperatorsFor(f, ctx), Sortable: sortable[f],
		})
		if f != "text" {
			out.Columns = append(out.Columns, queryOptionJSON{Value: f, Label: queryFieldLabels[f]})
		}
	}
	out.Columns = append(out.Columns, queryOptionJSON{Value: "parent", Label: queryFieldLabels["parent"]})
	for _, k := range sortedKeys(fields) {
		f := fields[k]
		id := "cf:" + k
		typ := query.FieldType(id, ctx)
		if typ == "" {
			continue
		}
		qf := queryFieldJSON{ID: id, Label: f.Def.Label, Type: typ, Operators: query.OperatorsFor(id, ctx)}
		for _, o := range f.Def.Options {
			label := o.Label
			if label == "" {
				label = o.Value
			}
			qf.Options = append(qf.Options, queryOptionJSON{Value: o.Value, Label: label})
		}
		out.Fields = append(out.Fields, qf)
		out.Columns = append(out.Columns, queryOptionJSON{Value: id, Label: f.Def.Label})
	}
	writeSavedViewAPIJSON(w, http.StatusOK, out)
}

func sortedKeys(m map[string]storage.QueryableField) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	for i := 1; i < len(keys); i++ {
		for j := i; j > 0 && strings.ToLower(m[keys[j]].Def.Label) < strings.ToLower(m[keys[j-1]].Def.Label); j-- {
			keys[j], keys[j-1] = keys[j-1], keys[j]
		}
	}
	return keys
}
