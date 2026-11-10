package tasks_test

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"GoTodo/internal/query"
	"GoTodo/internal/server/handlers"
	"GoTodo/internal/server/utils"
	"GoTodo/internal/storage"
	"GoTodo/internal/tasks"
)

// Query fixtures live in their own users and project so the shared list
// fixtures stay untouched:
//
//	user 50 owns project 30; user 51 is an editor on it; user 52 is a viewer.
//	user 53 has no access to project 30 and owns project 31.
//	project 30: tasks "Q parent" (open, claimed by 51) with subtasks "Q child done" (completed)
//	            and "Q child open" (priority 3), plus "Q other" (due yesterday).
//	project 31: "Q secret" (user 53 only).
var queryFixtureOnce sync.Once
var queryIDs = map[string]int{}

func setupQueryFixtures(t *testing.T) {
	t.Helper()
	queryFixtureOnce.Do(func() {
		pool, err := storage.OpenDatabase()
		if err != nil {
			t.Fatalf("db: %v", err)
		}
		defer storage.CloseDatabase(pool)
		ctx := context.Background()
		_, err = pool.Exec(ctx, `
			INSERT INTO users (id, email, full_name) VALUES
				(50, 'owner50@example.com', 'Olive Owner'),
				(51, 'editor51@example.com', 'Eddie Editor'),
				(52, 'viewer52@example.com', 'Vera Viewer'),
				(53, 'outsider53@example.com', 'Otto Outsider');
			INSERT INTO projects (id, user_id, name) VALUES (30, 50, 'Query project'), (31, 53, 'Secret project');
			INSERT INTO project_members (project_id, user_id, role) VALUES
				(30, 50, 'owner'), (30, 51, 'editor'), (30, 52, 'viewer'), (31, 53, 'owner');
			INSERT INTO project_statuses (id, project_id, name, position) VALUES
				(300, 30, 'Doing', 1), (301, 31, 'Doing', 1);
			INSERT INTO project_sprints (id, project_id, name, start_date, end_date) VALUES
				(400, 30, 'Now', (NOW() AT TIME ZONE 'UTC')::date - 3, (NOW() AT TIME ZONE 'UTC')::date + 3),
				(401, 31, 'Secret sprint', (NOW() AT TIME ZONE 'UTC')::date - 3, (NOW() AT TIME ZONE 'UTC')::date + 3);
		`)
		if err != nil {
			t.Fatalf("fixture: %v", err)
		}
		insert := func(key, title string, owner, project int, extra string) {
			var id int
			q := fmt.Sprintf(`INSERT INTO tasks (title, description, user_id, project_id%s) VALUES ($1, '', $2, $3%s) RETURNING id`,
				colsOf(extra), valsOf(extra))
			if err := pool.QueryRow(ctx, q, title, owner, project).Scan(&id); err != nil {
				t.Fatalf("insert %s: %v", key, err)
			}
			queryIDs[key] = id
		}
		insert("parent", "Q parent", 50, 30, "claimed_by=51,status_id=300,sprint_id=400")
		insert("other", "Q other", 50, 30, "due_date=(NOW() AT TIME ZONE 'UTC')::date - 1")
		insert("secret", "Q secret", 53, 31, "status_id=301,sprint_id=401")
		if _, err := pool.Exec(ctx, `
			INSERT INTO tasks (title, description, user_id, project_id, parent_id, completed) VALUES
				('Q child done', '', 50, 30, $1, true);
			`, queryIDs["parent"]); err != nil {
			t.Fatalf("child: %v", err)
		}
		var childOpen int
		if err := pool.QueryRow(ctx, `
			INSERT INTO tasks (title, description, user_id, project_id, parent_id, priority) VALUES
				('Q child open', '', 50, 30, $1, 3) RETURNING id`, queryIDs["parent"]).Scan(&childOpen); err != nil {
			t.Fatalf("child: %v", err)
		}
		queryIDs["child_open"] = childOpen
	})
}

// colsOf / valsOf turn "a=1,b=2" into extra column and value lists.
func colsOf(extra string) string {
	if extra == "" {
		return ""
	}
	var b strings.Builder
	for _, kv := range strings.Split(extra, ",") {
		b.WriteString(", " + strings.SplitN(kv, "=", 2)[0])
	}
	return b.String()
}

func valsOf(extra string) string {
	if extra == "" {
		return ""
	}
	var b strings.Builder
	for _, kv := range strings.Split(extra, ",") {
		b.WriteString(", " + strings.SplitN(kv, "=", 2)[1])
	}
	return b.String()
}

func runQueryFor(t *testing.T, userID int, raw string, req query.Request) (tasks.QueryResult, query.Unavailable, error) {
	t.Helper()
	def, err := query.Parse([]byte(raw))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	req.UserID = userID
	req.Timezone = "UTC"
	req.Now = time.Now()
	norm, ctx, un, err := query.DBLoader.Prepare(req, def)
	if err != nil {
		return tasks.QueryResult{}, un, err
	}
	res, err := tasks.RunQuery(norm, ctx, 1, 100)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	return res, un, nil
}

func qTitles(list []tasks.Task) []string {
	out := []string{}
	for _, t := range list {
		if strings.HasPrefix(t.Title, "Q ") {
			out = append(out, t.Title)
		}
	}
	sort.Strings(out)
	return out
}

func expectTitles(t *testing.T, got []tasks.Task, want ...string) {
	t.Helper()
	sort.Strings(want)
	g := qTitles(got)
	if strings.Join(g, "|") != strings.Join(want, "|") {
		t.Fatalf("got %v, want %v", g, want)
	}
}

func TestQueryOnlyReturnsVisibleTasks(t *testing.T) {
	setupQueryFixtures(t)
	// Every task on the site, as far as the clauses go; visibility still applies.
	all := `{"mode":"flat","where":{"op":"or","clauses":[
		{"field":"id","operator":">","value":0},
		{"field":"completed","operator":"=","value":true},
		{"field":"title","operator":"contains","value":"Q"}]}}`

	res, _, err := runQueryFor(t, 52, all, query.Request{})
	if err != nil {
		t.Fatal(err)
	}
	expectTitles(t, res.Tasks, "Q parent", "Q other", "Q child done", "Q child open")

	res, _, err = runQueryFor(t, 53, all, query.Request{})
	if err != nil {
		t.Fatal(err)
	}
	expectTitles(t, res.Tasks, "Q secret")

	// Naming the hidden task's id directly still finds nothing.
	res, _, err = runQueryFor(t, 52,
		fmt.Sprintf(`{"where":{"clauses":[{"field":"id","operator":"=","value":%d}]}}`, queryIDs["secret"]), query.Request{})
	if err != nil {
		t.Fatal(err)
	}
	expectTitles(t, res.Tasks)
}

func TestQueryRejectsHiddenReferences(t *testing.T) {
	setupQueryFixtures(t)
	cases := map[string]string{
		"scope project":    `{"scope":{"projects":[31]}}`,
		"clause project":   `{"where":{"clauses":[{"field":"project","operator":"in","value":[30,31]}]}}`,
		"missing project":  `{"scope":{"projects":[9999]}}`,
		"hidden sprint":    `{"where":{"clauses":[{"field":"sprint","operator":"=","value":401}]}}`,
		"hidden person":    `{"where":{"clauses":[{"field":"claimed_by","operator":"=","value":53}]}}`,
		"nonexistent user": `{"where":{"clauses":[{"field":"created_by","operator":"=","value":987654}]}}`,
	}
	for name, raw := range cases {
		t.Run(name, func(t *testing.T) {
			_, _, err := runQueryFor(t, 52, raw, query.Request{})
			if !errors.Is(err, query.ErrUnknownReference) {
				t.Fatalf("expected ErrUnknownReference, got %v", err)
			}
		})
	}
	// Co-members of a shared project are fair game.
	if _, _, err := runQueryFor(t, 52, `{"where":{"clauses":[{"field":"claimed_by","operator":"=","value":51}]}}`, query.Request{}); err != nil {
		t.Fatalf("co-member rejected: %v", err)
	}
}

func TestQueryLenientLostAccessMatchesNothing(t *testing.T) {
	setupQueryFixtures(t)
	res, un, err := runQueryFor(t, 52, `{"scope":{"projects":[31],"include_personal":false}}`, query.Request{Lenient: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(un.Projects) != 1 || un.Projects[0] != 31 {
		t.Fatalf("expected project 31 unavailable, got %+v", un)
	}
	if res.Total != 0 {
		t.Fatalf("a scope that lost every project must return nothing, got %d", res.Total)
	}
}

func TestQueryForceProject(t *testing.T) {
	setupQueryFixtures(t)
	res, _, err := runQueryFor(t, 52, `{"scope":{"projects":[]}}`, query.Request{ForceProject: 30})
	if err != nil {
		t.Fatal(err)
	}
	expectTitles(t, res.Tasks, "Q parent", "Q other", "Q child done", "Q child open")

	// Pinning to a project the caller cannot open returns nothing.
	res, _, err = runQueryFor(t, 52, `{}`, query.Request{ForceProject: 31, Lenient: true})
	if err != nil {
		t.Fatal(err)
	}
	if res.Total != 0 {
		t.Fatalf("forced inaccessible project leaked %d tasks", res.Total)
	}
}

func TestQueryExcludeViewOnly(t *testing.T) {
	setupQueryFixtures(t)
	res, _, err := runQueryFor(t, 52, `{"scope":{"exclude_view_only":true}}`, query.Request{})
	if err != nil {
		t.Fatal(err)
	}
	expectTitles(t, res.Tasks)
	res, _, err = runQueryFor(t, 51, `{"scope":{"exclude_view_only":true}}`, query.Request{})
	if err != nil {
		t.Fatal(err)
	}
	expectTitles(t, res.Tasks, "Q parent", "Q other", "Q child done", "Q child open")
}

func TestQueryFieldsAndMacros(t *testing.T) {
	setupQueryFixtures(t)
	cases := []struct {
		name string
		raw  string
		want []string
	}{
		{"status by name", `{"where":{"clauses":[{"field":"workflow_status","operator":"=","value":"doing"}]}}`, []string{"Q parent"}},
		{"status not", `{"where":{"clauses":[{"field":"workflow_status","operator":"<>","value":"Doing"},{"field":"title","operator":"contains","value":"Q "}]}}`,
			[]string{"Q other", "Q child done", "Q child open"}},
		{"claimed by person", `{"where":{"clauses":[{"field":"claimed_by","operator":"=","value":51}]}}`, []string{"Q parent"}},
		{"claimed by me", `{"where":{"clauses":[{"field":"claimed_by","operator":"=","value":"@me"}]}}`, []string{"Q parent"}},
		{"current sprint", `{"where":{"clauses":[{"field":"sprint","operator":"=","value":"@current_sprint"}]}}`, []string{"Q parent"}},
		{"overdue", `{"where":{"clauses":[{"field":"due_date","operator":"<","value":"@today"},{"field":"completed","operator":"=","value":false}]}}`, []string{"Q other"}},
		{"due between excludes earlier", `{"where":{"clauses":[{"field":"due_date","operator":"between","value":["@today","@end_of_week+7"]}]}}`, []string{}},
		{"due between includes ends", `{"where":{"clauses":[{"field":"due_date","operator":"between","value":["@today-1","@today"]}]}}`, []string{"Q other"}},
		{"priority in", `{"where":{"clauses":[{"field":"priority","operator":"in","value":[3]}]}}`, []string{"Q child open"}},
		{"is subtask", `{"where":{"clauses":[{"field":"is_subtask","operator":"=","value":true}]}}`, []string{"Q child done", "Q child open"}},
		{"has subtasks", `{"where":{"clauses":[{"field":"has_subtasks","operator":"=","value":true}]}}`, []string{"Q parent"}},
		{"created by", `{"where":{"clauses":[{"field":"created_by","operator":"=","value":50},{"field":"completed","operator":"=","value":true}]}}`, []string{"Q child done"}},
		{"text search", `{"where":{"clauses":[{"field":"text","operator":"contains","value":"child"}]}}`, []string{"Q child done", "Q child open"}},
		{"like wildcards are literal", `{"where":{"clauses":[{"field":"title","operator":"contains","value":"Q%"}]}}`, []string{}},
		{"per-row joins: and binds before or", `{"where":{"clauses":[
			{"field":"title","operator":"contains","value":"child"},
			{"join":"and","field":"completed","operator":"=","value":true},
			{"join":"or","field":"due_date","operator":"is_not_empty"}]}}`, []string{"Q child done", "Q other"}},
		{"or group", `{"where":{"op":"or","clauses":[{"field":"priority","operator":"=","value":3},{"field":"due_date","operator":"is_not_empty"}]}}`,
			[]string{"Q other", "Q child open"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			res, _, err := runQueryFor(t, 51, c.raw, query.Request{})
			if err != nil {
				t.Fatal(err)
			}
			expectTitles(t, res.Tasks, c.want...)
		})
	}
}

func TestQueryTreeMode(t *testing.T) {
	setupQueryFixtures(t)
	res, _, err := runQueryFor(t, 51, `{"mode":"tree","scope":{"projects":[30],"include_personal":false},"sort":[{"field":"title","dir":"asc"}]}`, query.Request{})
	if err != nil {
		t.Fatal(err)
	}
	if res.Total != 2 {
		t.Fatalf("tree total should count top-level rows only, got %d", res.Total)
	}
	expectTitles(t, res.Tasks, "Q other", "Q parent")
	var parent tasks.Task
	for _, tk := range res.Tasks {
		if tk.Title == "Q parent" {
			parent = tk
		}
	}
	if len(parent.Children) != 2 || parent.ChildCount != 2 || parent.ChildrenCompleted != 1 {
		t.Fatalf("parent children not nested: %+v", parent)
	}
	if parent.ClaimedByName == "" || parent.StatusName != "Doing" || parent.CreatedByName != "Olive Owner" {
		t.Fatalf("parent not decorated: %+v", parent)
	}

	// A subtask whose parent does not match is a top-level row of its own.
	res, _, err = runQueryFor(t, 51, `{"mode":"tree","where":{"clauses":[{"field":"priority","operator":"=","value":3}]}}`, query.Request{})
	if err != nil {
		t.Fatal(err)
	}
	expectTitles(t, res.Tasks, "Q child open")
	if res.Tasks[0].ParentTitle != "Q parent" {
		t.Fatalf("orphan row should carry its parent title: %+v", res.Tasks[0])
	}
}

func TestQueryOptionsOnlyListAccessibleData(t *testing.T) {
	setupQueryFixtures(t)
	req := httptest.NewRequest(http.MethodGet, "/api/v2/queries/options", nil)
	req = utils.SetAPIUserID(req, 52)
	rec := httptest.NewRecorder()
	handlers.APIV1QueriesRouter(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("options: %d %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	for _, hidden := range []string{"Secret project", "Otto Outsider", "Secret sprint", "outsider53"} {
		if strings.Contains(body, hidden) {
			t.Fatalf("options leaked %q: %s", hidden, body)
		}
	}
	for _, shown := range []string{"Query project", "Eddie Editor", "\"Now\"", "Doing"} {
		if !strings.Contains(body, shown) {
			t.Fatalf("options missing %q: %s", shown, body)
		}
	}
}

func TestQueryRunEndpoint(t *testing.T) {
	setupQueryFixtures(t)
	post := func(userID int, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/api/v2/queries/run", strings.NewReader(body))
		req = utils.SetAPIUserID(req, userID)
		rec := httptest.NewRecorder()
		handlers.APIV1QueriesRouter(rec, req)
		return rec
	}
	rec := post(52, `{"definition":{"scope":{"projects":[31]}}}`)
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "invalid_query") {
		t.Fatalf("hidden scope should be rejected: %d %s", rec.Code, rec.Body.String())
	}
	rec = post(52, `{"definition":{},"project_id":31}`)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("preview pinned to a hidden project should 404: %d %s", rec.Code, rec.Body.String())
	}
	rec = post(52, `{"definition":{"mode":"tree","where":{"clauses":[{"field":"title","operator":"contains","value":"Q "}]}},"page_size":10}`)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"Q child open"`) || !strings.Contains(rec.Body.String(), `"created_by_name":"Olive Owner"`) {
		t.Fatalf("tree run: %d %s", rec.Code, rec.Body.String())
	}
	rec = post(52, `{"definition":{"where":{"clauses":[{"field":"title","operator":"contains","value":"Q"}]}},"page_size":-1}`)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"total":4`) || !strings.Contains(rec.Body.String(), `"tasks":[]`) {
		t.Fatalf("count only: %d %s", rec.Code, rec.Body.String())
	}
}

func TestSharedSavedQueries(t *testing.T) {
	setupQueryFixtures(t)
	// Organization 60 holds project 30: user 50 owns it, 51 and 52 are plain
	// members, and 53 is outside it.
	pool, err := storage.OpenDatabase()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(context.Background(), `
		INSERT INTO organizations (id, name, created_by) VALUES (60, 'Query org', 50) ON CONFLICT DO NOTHING;
		INSERT INTO organization_members (organization_id, user_id, role) VALUES
			(60, 50, 'owner'), (60, 51, 'member'), (60, 52, 'member') ON CONFLICT DO NOTHING;
		UPDATE projects SET organization_id = 60 WHERE id = 30;`); err != nil {
		t.Fatalf("org fixture: %v", err)
	}
	storage.CloseDatabase(pool)

	do := func(method, path string, userID int, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req = utils.SetAPIUserID(req, userID)
		rec := httptest.NewRecorder()
		handlers.APIV1SavedViewsRouter(rec, req)
		return rec
	}

	// Any member may share a query with the organization. Projects are part
	// of the query itself.
	create := do(http.MethodPost, "/api/v2/saved-views", 51,
		`{"name":"Team open work","scope":"organization","organization_id":60,
		  "definition":{"scope":{"organizations":[60],"include_personal":false},"where":{"clauses":[{"field":"completed","operator":"=","value":false}]},"mode":"tree"}}`)
	if create.Code != http.StatusCreated {
		t.Fatalf("create shared: %d %s", create.Code, create.Body.String())
	}
	var view struct {
		ID               int    `json:"id"`
		Scope            string `json:"scope"`
		OrganizationName string `json:"organization_name"`
		Shared           bool   `json:"shared"`
		CanEdit          bool   `json:"can_edit"`
		IsOwner          bool   `json:"is_owner"`
	}
	decodeSavedViewResponse(t, create, &view)
	if !view.Shared || !view.CanEdit || !view.IsOwner || view.Scope != "organization" || view.OrganizationName != "Query org" {
		t.Fatalf("unexpected shared view: %+v", view)
	}
	path := fmt.Sprintf("/api/v2/saved-views/%d", view.ID)

	// Every reference is still checked against the author's access.
	bad := do(http.MethodPost, "/api/v2/saved-views", 51,
		`{"name":"Cross project","scope":"organization","organization_id":60,"definition":{"where":{"clauses":[{"field":"sprint","operator":"=","value":401}]}}}`)
	if bad.Code != http.StatusBadRequest {
		t.Fatalf("hidden reference accepted: %d %s", bad.Code, bad.Body.String())
	}
	if rec := do(http.MethodPost, "/api/v2/saved-views", 51, `{"name":"x","scope":"organization","definition":{}}`); rec.Code != http.StatusBadRequest {
		t.Fatalf("organization scope without an id: %d %s", rec.Code, rec.Body.String())
	}
	// Site-wide sharing needs queries:site.
	if rec := do(http.MethodPost, "/api/v2/saved-views", 51, `{"name":"Everyone","scope":"site","definition":{}}`); rec.Code != http.StatusForbidden {
		t.Fatalf("site query without permission: %d %s", rec.Code, rec.Body.String())
	}

	// Another member sees and runs it but cannot change it.
	list := do(http.MethodGet, "/api/v2/saved-views", 52, "")
	if !strings.Contains(list.Body.String(), "Team open work") || !strings.Contains(list.Body.String(), `"can_edit":false`) {
		t.Fatalf("member list: %s", list.Body.String())
	}
	results := do(http.MethodGet, path+"/results", 52, "")
	if results.Code != http.StatusOK || !strings.Contains(results.Body.String(), "Q parent") || strings.Contains(results.Body.String(), "Q child done") {
		t.Fatalf("member results: %d %s", results.Code, results.Body.String())
	}
	if rec := do(http.MethodPatch, path, 52, `{"name":"Mine now"}`); rec.Code != http.StatusForbidden {
		t.Fatalf("member edit: %d %s", rec.Code, rec.Body.String())
	}
	if rec := do(http.MethodDelete, path, 52, ""); rec.Code != http.StatusForbidden {
		t.Fatalf("member delete: %d %s", rec.Code, rec.Body.String())
	}
	if rec := do(http.MethodPut, path+"/dashboard", 52, ""); rec.Code != http.StatusNoContent {
		t.Fatalf("member pin: %d %s", rec.Code, rec.Body.String())
	}

	// The organization's owner may edit it, but only its author moves it.
	if rec := do(http.MethodPatch, path, 50, `{"name":"Team open work (all)"}`); rec.Code != http.StatusOK {
		t.Fatalf("org owner edit: %d %s", rec.Code, rec.Body.String())
	}
	if rec := do(http.MethodPatch, path, 50, `{"scope":"personal"}`); rec.Code != http.StatusForbidden {
		t.Fatalf("non-author move: %d %s", rec.Code, rec.Body.String())
	}

	// An outsider cannot tell it exists, nor use the organization.
	for _, p := range []string{path, path + "/results", path + "/export"} {
		if rec := do(http.MethodGet, p, 53, ""); rec.Code != http.StatusNotFound {
			t.Fatalf("outsider GET %s: %d %s", p, rec.Code, rec.Body.String())
		}
	}
	if rec := do(http.MethodPut, path+"/dashboard", 53, ""); rec.Code != http.StatusNotFound {
		t.Fatalf("outsider pin: %d", rec.Code)
	}
	if list := do(http.MethodGet, "/api/v2/saved-views", 53, ""); strings.Contains(list.Body.String(), "Team open work") {
		t.Fatalf("outsider list leaked: %s", list.Body.String())
	}
	if rec := do(http.MethodPost, "/api/v2/saved-views", 53, `{"name":"x","scope":"organization","organization_id":60,"definition":{}}`); rec.Code != http.StatusNotFound {
		t.Fatalf("outsider create: %d %s", rec.Code, rec.Body.String())
	}
	if _, _, err := runQueryFor(t, 53, `{"scope":{"organizations":[60]}}`, query.Request{}); !errors.Is(err, query.ErrUnknownReference) {
		t.Fatalf("outsider naming the organization: %v", err)
	}

	// CSV export neutralizes formula-looking cells.
	pool, _ = storage.OpenDatabase()
	_, _ = pool.Exec(context.Background(), `UPDATE tasks SET title = '=HYPERLINK("x")' WHERE id = $1`, queryIDs["other"])
	storage.CloseDatabase(pool)
	export := do(http.MethodGet, path+"/export", 50, "")
	if export.Code != http.StatusOK || !strings.Contains(export.Body.String(), `'=HYPERLINK`) {
		t.Fatalf("export: %d %s", export.Code, export.Body.String())
	}

	// Its author takes it back as a personal query; members lose it.
	if rec := do(http.MethodPatch, path, 51, `{"scope":"personal"}`); rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"scope":"personal"`) {
		t.Fatalf("author move: %d %s", rec.Code, rec.Body.String())
	}
	if rec := do(http.MethodGet, path, 52, ""); rec.Code != http.StatusNotFound {
		t.Fatalf("member still sees a personal query: %d %s", rec.Code, rec.Body.String())
	}
	if rec := do(http.MethodDelete, path, 51, ""); rec.Code != http.StatusNoContent {
		t.Fatalf("author delete: %d %s", rec.Code, rec.Body.String())
	}
}

func TestQueryOrganizationScope(t *testing.T) {
	setupQueryFixtures(t)
	pool, err := storage.OpenDatabase()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(context.Background(), `
		INSERT INTO organizations (id, name, created_by) VALUES (60, 'Query org', 50) ON CONFLICT DO NOTHING;
		INSERT INTO organization_members (organization_id, user_id, role) VALUES
			(60, 50, 'owner'), (60, 51, 'member'), (60, 52, 'member') ON CONFLICT DO NOTHING;
		UPDATE projects SET organization_id = 60 WHERE id = 30;`); err != nil {
		t.Fatalf("org fixture: %v", err)
	}
	storage.CloseDatabase(pool)

	res, _, err := runQueryFor(t, 52, `{"scope":{"organizations":[60],"include_personal":false}}`, query.Request{})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if res.Total == 0 {
		t.Fatal("organization scope should find project 30's tasks")
	}
	for _, task := range res.Tasks {
		if task.ProjectID != 30 {
			t.Fatalf("organization scope returned a task outside its projects: %+v", task)
		}
	}
	// Leaving the organization makes a saved query's organization match nothing.
	res, un, err := runQueryFor(t, 53, `{"scope":{"organizations":[60],"include_personal":false}}`, query.Request{Lenient: true})
	if err != nil {
		t.Fatalf("lenient run: %v", err)
	}
	if res.Total != 0 || len(un.Organizations) != 1 {
		t.Fatalf("hidden organization should match nothing: total=%d unavailable=%+v", res.Total, un)
	}
}

func TestQueryRunPinnedToProjectKey(t *testing.T) {
	setupQueryFixtures(t)
	post := func(body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/api/v2/queries/run", strings.NewReader(body))
		req = utils.SetAPIUserID(req, 50)
		req = utils.WithAPIKeyPrincipal(req, &storage.APIKeyPrincipal{KeyID: 9, UserID: 50, ProjectID: 30,
			Scopes: []string{storage.APIScopeTasksRead}})
		rec := httptest.NewRecorder()
		handlers.APIV1QueriesRouter(rec, req)
		return rec
	}
	if rec := post(`{"definition":{},"project_id":31}`); rec.Code != http.StatusForbidden {
		t.Fatalf("key asking for another project: %d %s", rec.Code, rec.Body.String())
	}
	// The scope is pinned to the key's project, so personal tasks never appear.
	pool, _ := storage.OpenDatabase()
	_, _ = pool.Exec(context.Background(), `INSERT INTO tasks (title, description, user_id) VALUES ('Q personal 50', '', 50)`)
	storage.CloseDatabase(pool)
	rec := post(`{"definition":{"scope":{"include_personal":true}},"page_size":200}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("pinned run: %d %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "Q parent") || strings.Contains(rec.Body.String(), "Q personal 50") ||
		strings.Contains(rec.Body.String(), "Q secret") {
		t.Fatalf("project key run escaped its project: %s", rec.Body.String())
	}
}

// Task visibility still shows a creator the tasks they made in a project they
// have since left; queries must not.
func TestQueryHidesTasksInProjectsTheCreatorLeft(t *testing.T) {
	setupQueryFixtures(t)
	pool, err := storage.OpenDatabase()
	if err != nil {
		t.Fatal(err)
	}
	var id int
	if err := pool.QueryRow(context.Background(),
		`INSERT INTO tasks (title, description, user_id, project_id) VALUES ('Q stranded', '', 53, 30) RETURNING id`).Scan(&id); err != nil {
		t.Fatal(err)
	}
	storage.CloseDatabase(pool)
	t.Cleanup(func() {
		pool, _ := storage.OpenDatabase()
		_, _ = pool.Exec(context.Background(), `DELETE FROM tasks WHERE id = $1`, id)
		storage.CloseDatabase(pool)
	})

	for _, raw := range []string{
		`{}`,
		`{"scope":{"projects":[]},"where":{"op":"or","clauses":[{"field":"created_by","operator":"=","value":"@me"},{"field":"id","operator":">","value":0}]}}`,
	} {
		res, _, err := runQueryFor(t, 53, raw, query.Request{})
		if err != nil {
			t.Fatal(err)
		}
		expectTitles(t, res.Tasks, "Q secret")
	}
}

func TestQueryTimeInStatus(t *testing.T) {
	setupQueryFixtures(t)
	pool, err := storage.OpenDatabase()
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	ids := []int{}
	add := func(title string, movedDaysAgo int) {
		var id int
		if err := pool.QueryRow(ctx, `
			INSERT INTO tasks (title, description, user_id, project_id, status_id, time_stamp)
			VALUES ($1, '', 50, 30, 300, (NOW() AT TIME ZONE 'UTC') - INTERVAL '30 days') RETURNING id`, title).Scan(&id); err != nil {
			t.Fatal(err)
		}
		if _, err := pool.Exec(ctx, `
			INSERT INTO task_events (task_id, user_id, event_type, created_at)
			VALUES ($1, 50, 'status_changed', (NOW() AT TIME ZONE 'UTC') - make_interval(days => $2))`, id, movedDaysAgo); err != nil {
			t.Fatal(err)
		}
		ids = append(ids, id)
	}
	add("Q stale", 12)
	add("Q fresh", 1)
	storage.CloseDatabase(pool)
	t.Cleanup(func() {
		pool, _ := storage.OpenDatabase()
		_, _ = pool.Exec(context.Background(), `DELETE FROM tasks WHERE id = ANY($1)`, ids)
		storage.CloseDatabase(pool)
	})

	cases := []struct {
		name string
		raw  string
		want []string
	}{
		{"days in status", `{"where":{"clauses":[
			{"field":"workflow_status","operator":"=","value":"doing"},
			{"field":"days_in_status","operator":">","value":10}]}}`, []string{"Q stale"}},
		{"entered status before", `{"where":{"clauses":[
			{"field":"status_changed_date","operator":"<","value":"@today-10"}]}}`, []string{"Q stale"}},
		// Q parent never moved, so it entered "Doing" when it was created today.
		{"never moved counts from creation", `{"where":{"clauses":[
			{"field":"days_in_status","operator":"<=","value":1}]}}`, []string{"Q parent", "Q fresh"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			res, _, err := runQueryFor(t, 51, c.raw, query.Request{})
			if err != nil {
				t.Fatal(err)
			}
			expectTitles(t, res.Tasks, c.want...)
		})
	}

	res, _, err := runQueryFor(t, 51, `{"where":{"clauses":[{"field":"title","operator":"=","value":"Q stale"}]}}`, query.Request{})
	if err != nil || len(res.Tasks) != 1 {
		t.Fatalf("lookup: %v %+v", err, res)
	}
	if d := res.Tasks[0].DaysInStatus; d == nil || *d != 12 {
		t.Fatalf("days in status column: %v", d)
	}
}

func TestMigrateProjectSharedViews(t *testing.T) {
	setupQueryFixtures(t)
	pool, err := storage.OpenDatabase()
	if err != nil {
		t.Fatal(err)
	}
	defer storage.CloseDatabase(pool)
	ctx := context.Background()
	// Recreate the old shape: views shared on a project via project_id.
	if _, err := pool.Exec(ctx, `
		INSERT INTO organizations (id, name, created_by) VALUES (60, 'Query org', 50) ON CONFLICT DO NOTHING;
		UPDATE projects SET organization_id = 60 WHERE id = 30;
		UPDATE projects SET organization_id = NULL WHERE id = 31;
		ALTER TABLE saved_views ADD COLUMN project_id INTEGER REFERENCES projects(id) ON DELETE CASCADE;
		DROP INDEX idx_saved_views_personal_name_v2;
		INSERT INTO saved_views (user_id, name, scope) VALUES (53, 'Mig secret', 'personal');
		INSERT INTO saved_views (user_id, name, project_id, definition) VALUES
			(51, 'Mig team', 30, '{"version":2,"scope":{"projects":[],"include_personal":true},"where":{"op":"and"}}'),
			(53, 'Mig secret', 31, NULL);`); err != nil {
		t.Fatalf("legacy fixture: %v", err)
	}
	if err := storage.MigrateSavedViewsForQueries(); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	var hasColumn bool
	_ = pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM information_schema.columns
		WHERE table_name = 'saved_views' AND column_name = 'project_id')`).Scan(&hasColumn)
	if hasColumn {
		t.Fatal("project_id should be dropped after conversion")
	}

	views, err := storage.ListVisibleSavedViews(51)
	if err != nil {
		t.Fatal(err)
	}
	var team *storage.SavedView
	for i := range views {
		if views[i].Name == "Mig team" {
			team = &views[i]
		}
	}
	if team == nil || team.Scope != storage.SavedViewScopeOrg || team.OrgID() != 60 {
		t.Fatalf("project view with an organization should be shared with it: %+v", team)
	}
	def, err := query.Parse(team.Definition)
	if err != nil || len(def.Scope.Projects) != 1 || def.Scope.Projects[0] != 30 || def.Scope.Personal() {
		t.Fatalf("project should move into the definition: %+v (%v)", def.Scope, err)
	}

	// No organization: it becomes its author's personal view, renamed on a clash.
	mine, err := storage.ListSavedViewsForUser(53)
	if err != nil {
		t.Fatal(err)
	}
	names := map[string]bool{}
	for _, v := range mine {
		names[v.Name] = true
	}
	if !names["Mig secret"] || !names["Mig secret (Secret project)"] {
		t.Fatalf("expected the original and a renamed copy: %v", names)
	}
	_, _ = pool.Exec(ctx, `DELETE FROM saved_views WHERE name LIKE 'Mig %'`)
}
