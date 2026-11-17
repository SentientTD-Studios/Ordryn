package query

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"GoTodo/internal/storage"
)

func mustParse(t *testing.T, s string) Definition {
	t.Helper()
	d, err := Parse([]byte(s))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	return d
}

type fakeTypes map[string]string

func (f fakeTypes) CustomFieldType(key string) (string, bool) {
	t, ok := f[key]
	return t, ok
}

func TestNormalizeDefaults(t *testing.T) {
	d, err := Normalize(mustParse(t, `{}`), nil)
	if err != nil {
		t.Fatal(err)
	}
	if d.Version != CurrentVersion || d.Mode != ModeFlat || !d.Scope.Personal() || len(d.Columns) == 0 {
		t.Fatalf("unexpected defaults: %+v", d)
	}
	if !d.Where.IsGroup() || d.Where.Op != GroupAnd {
		t.Fatalf("where should be an AND group: %+v", d.Where)
	}
}

func TestNormalizeRejectsBadDefinitions(t *testing.T) {
	cases := map[string]string{
		"unknown field":       `{"where":{"clauses":[{"field":"password","operator":"=","value":"x"}]}}`,
		"bad operator":        `{"where":{"clauses":[{"field":"title","operator":"<","value":"x"}]}}`,
		"sql in operator":     `{"where":{"clauses":[{"field":"title","operator":"= 1 OR 1=1 --","value":"x"}]}}`,
		"missing value":       `{"where":{"clauses":[{"field":"priority","operator":"="}]}}`,
		"value on is_empty":   `{"where":{"clauses":[{"field":"due_date","operator":"is_empty","value":"2024-01-01"}]}}`,
		"bad priority":        `{"where":{"clauses":[{"field":"priority","operator":"=","value":9}]}}`,
		"bad date":            `{"where":{"clauses":[{"field":"due_date","operator":"=","value":"2024-13-40"}]}}`,
		"bad macro":           `{"where":{"clauses":[{"field":"due_date","operator":"=","value":"@yesterday"}]}}`,
		"in needs list":       `{"where":{"clauses":[{"field":"priority","operator":"in","value":1}]}}`,
		"empty list":          `{"where":{"clauses":[{"field":"priority","operator":"in","value":[]}]}}`,
		"bad group op":        `{"where":{"op":"xor","clauses":[]}}`,
		"negative project":    `{"scope":{"projects":[-1]}}`,
		"bad mode":            `{"mode":"graph"}`,
		"unknown column":      `{"columns":["password_hash"]}`,
		"unsortable":          `{"sort":[{"field":"description"}]}`,
		"bad sort dir":        `{"sort":[{"field":"id","dir":"sideways"}]}`,
		"unknown custom":      `{"where":{"clauses":[{"field":"cf:secret","operator":"=","value":"x"}]}}`,
		"project not id":      `{"where":{"clauses":[{"field":"project","operator":"=","value":"abc"}]}}`,
		"user not id":         `{"where":{"clauses":[{"field":"claimed_by","operator":"=","value":"@you"}]}}`,
		"clause also a group": `{"where":{"clauses":[{"field":"title","operator":"contains","value":"x","op":"or"}]}}`,
		"version":             `{"version":3}`,
		"between one value":   `{"where":{"clauses":[{"field":"due_date","operator":"between","value":["@today"]}]}}`,
		"between three":       `{"where":{"clauses":[{"field":"due_date","operator":"between","value":["@today","@today","@today"]}]}}`,
		"between scalar":      `{"where":{"clauses":[{"field":"due_date","operator":"between","value":"@today"}]}}`,
		"between bad date":    `{"where":{"clauses":[{"field":"due_date","operator":"between","value":["@today","soon"]}]}}`,
		"between on text":     `{"where":{"clauses":[{"field":"title","operator":"between","value":["a","b"]}]}}`,
	}
	for name, raw := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := Normalize(mustParse(t, raw), fakeTypes{}); err == nil || !IsValidationError(err) {
				t.Fatalf("expected a validation error, got %v", err)
			}
		})
	}
}

func TestParseRejectsUnknownKeys(t *testing.T) {
	if _, err := Parse([]byte(`{"where":{"sql":"1=1"}}`)); err == nil {
		t.Fatal("unknown keys must be rejected")
	}
}

func TestNormalizeLimits(t *testing.T) {
	clauses := make([]string, MaxClauses+1)
	for i := range clauses {
		clauses[i] = `{"field":"id","operator":"=","value":1}`
	}
	raw := `{"where":{"clauses":[` + strings.Join(clauses, ",") + `]}}`
	if _, err := Normalize(mustParse(t, raw), nil); err == nil {
		t.Fatal("too many clauses must be rejected")
	}

	deep := `{"field":"id","operator":"=","value":1}`
	for i := 0; i < MaxDepth+1; i++ {
		deep = `{"op":"or","clauses":[` + deep + `]}`
	}
	if _, err := Normalize(mustParse(t, `{"where":`+deep+`}`), nil); err == nil {
		t.Fatal("too deep nesting must be rejected")
	}
}

func TestNormalizeCanonicalValues(t *testing.T) {
	d, err := Normalize(mustParse(t, `{"where":{"clauses":[
		{"field":"Workflow_Status","operator":"IN","value":["  Doing ","Review"]},
		{"field":"claimed_by","operator":"=","value":"@ME"},
		{"field":"project","operator":"in","value":["7", 8]},
		{"op":"or","clauses":[]}
	]},"scope":{"projects":[3,3,4]}}`), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(d.Where.Clauses) != 3 {
		t.Fatalf("empty group should be dropped: %+v", d.Where.Clauses)
	}
	if got := string(d.Where.Clauses[0].Value); got != `["doing","review"]` {
		t.Fatalf("status names not canonical: %s", got)
	}
	if d.Where.Clauses[0].Field != "workflow_status" || d.Where.Clauses[0].Operator != OpIn {
		t.Fatalf("field/operator not canonical: %+v", d.Where.Clauses[0])
	}
	if got := string(d.Where.Clauses[1].Value); got != `"@me"` {
		t.Fatalf("macro not canonical: %s", got)
	}
	if got := string(d.Where.Clauses[2].Value); got != `[7,8]` {
		t.Fatalf("project ids not canonical: %s", got)
	}
	if len(d.Scope.Projects) != 2 {
		t.Fatalf("duplicate scope projects kept: %v", d.Scope.Projects)
	}
}

func testCtx() Context {
	return Context{
		UserID:   42,
		Timezone: "UTC",
		Today:    time.Date(2026, 11, 11, 0, 0, 0, 0, time.UTC), // a Wednesday
		Projects: map[int]bool{1: true, 2: true},
		Sprints:  map[int]bool{10: true},
		Users:    map[int]bool{7: true},
	}
}

func compileRaw(t *testing.T, raw string, ctx Context) Compiled {
	t.Helper()
	d, err := Normalize(mustParse(t, raw), ctx)
	if err != nil {
		t.Fatal(err)
	}
	return Compile(d, ctx, nil)
}

// The visibility condition must come first and the user's clause tree must be
// parenthesized and ANDed after it, so an OR in the tree cannot widen results.
func TestCompileVisibilityWrapsClauses(t *testing.T) {
	c := compileRaw(t, `{"where":{"op":"or","clauses":[
		{"field":"id","operator":">","value":0},
		{"field":"completed","operator":"=","value":true}
	]}}`, testCtx())
	vis := storage.TaskVisibleCondition("t", "$1")
	if !strings.HasPrefix(c.Where, vis+" AND ") {
		t.Fatalf("visibility must lead the WHERE:\n%s", c.Where)
	}
	if !strings.HasSuffix(c.Where, "((t.id > $3) OR (COALESCE(t.completed, false)))") {
		t.Fatalf("clause tree must be one parenthesized group at the end:\n%s", c.Where)
	}
	if c.Args[0] != 42 {
		t.Fatalf("first arg must be the caller: %v", c.Args)
	}
}

func TestCompileValuesAreBound(t *testing.T) {
	evil := `x'); DROP TABLE tasks; --`
	raw, _ := json.Marshal(map[string]any{"where": map[string]any{"clauses": []any{
		map[string]any{"field": "title", "operator": "contains", "value": evil},
		map[string]any{"field": "tags", "operator": "in", "value": []string{evil}},
		map[string]any{"field": "workflow_status", "operator": "=", "value": evil},
	}}})
	c := compileRaw(t, string(raw), testCtx())
	if strings.Contains(c.Where, "DROP TABLE") {
		t.Fatalf("value leaked into SQL:\n%s", c.Where)
	}
}

func TestCompileScopeNeverFallsBackToAll(t *testing.T) {
	ctx := testCtx()
	// "All projects" binds the accessible list instead of matching any project.
	c0 := compileRaw(t, `{}`, ctx)
	if !strings.Contains(c0.Where, "((t.project_id = ANY($2) AND") {
		t.Fatalf("all-projects scope must list accessible projects:\n%s", c0.Where)
	}
	if ids, ok := c0.Args[1].([]int); !ok || len(ids) != 2 {
		t.Fatalf("all-projects scope should bind [1 2], got %v", c0.Args)
	}
	ctx.Projects = map[int]bool{}
	if c0 = compileRaw(t, `{"scope":{"include_personal":false}}`, ctx); !strings.Contains(c0.Where, "(FALSE)") {
		t.Fatalf("no accessible projects must match nothing:\n%s", c0.Where)
	}
	ctx = testCtx()
	c := compileRaw(t, `{"scope":{"projects":[99],"include_personal":false}}`, ctx)
	if !strings.Contains(c.Where, "(FALSE)") {
		t.Fatalf("a scope whose projects are all inaccessible must match nothing:\n%s", c.Where)
	}
	c = compileRaw(t, `{"scope":{"projects":[1,99]}}`, ctx)
	found := false
	for _, a := range c.Args {
		if ids, ok := a.([]int); ok {
			found = true
			if len(ids) != 1 || ids[0] != 1 {
				t.Fatalf("inaccessible scope project kept: %v", ids)
			}
		}
	}
	if !found {
		t.Fatalf("scope project list not bound: %v", c.Args)
	}
}

func TestCompileForceProject(t *testing.T) {
	ctx := testCtx()
	ctx.ForceProject = 2
	c := compileRaw(t, `{"scope":{"projects":[1]}}`, ctx)
	// The personal-tasks scope is "OR t.project_id IS NULL"; visibility has its own IS NULL check.
	if !strings.Contains(c.Where, "t.project_id = $") || strings.Contains(c.Where, ") OR t.project_id IS NULL)") {
		t.Fatalf("forced project must replace the scope:\n%s", c.Where)
	}
	ctx.ForceProject = 99
	c = compileRaw(t, `{}`, ctx)
	if !strings.Contains(c.Where, "FALSE") {
		t.Fatalf("an inaccessible forced project must match nothing:\n%s", c.Where)
	}
}

func TestCompileInaccessibleReferencesMatchNothing(t *testing.T) {
	ctx := testCtx()
	c := compileRaw(t, `{"where":{"clauses":[{"field":"claimed_by","operator":"=","value":999}]}}`, ctx)
	if !strings.HasSuffix(c.Where, "(FALSE))") {
		t.Fatalf("unknown person must match nothing:\n%s", c.Where)
	}
	c = compileRaw(t, `{"where":{"clauses":[{"field":"project","operator":"<>","value":99}]}}`, ctx)
	if !strings.HasSuffix(c.Where, "(NOT COALESCE((FALSE), false)))") {
		t.Fatalf("<> an inaccessible project must keep every visible task:\n%s", c.Where)
	}
	c = compileRaw(t, `{"where":{"clauses":[{"field":"claimed_by","operator":"in","value":["@me",7,8]}]}}`, ctx)
	// The last bound list is the clause's; the scope's project list comes first.
	last := c.Args[len(c.Args)-1].([]int)
	if len(last) != 2 || last[0] != 42 || last[1] != 7 {
		t.Fatalf("expected [@me, 7] only, got %v", last)
	}
}

func TestCompileArchivedTagIncludesArchived(t *testing.T) {
	ctx := testCtx()
	c := compileRaw(t, `{}`, ctx)
	if !strings.Contains(c.Where, "NOT EXISTS (\n\t\tSELECT 1 FROM task_tags tt") {
		t.Fatalf("archived tasks should be excluded by default:\n%s", c.Where)
	}
	c = compileRaw(t, `{"where":{"clauses":[{"field":"tags","operator":"in","value":["Archived"]}]}}`, ctx)
	if strings.Contains(c.Where, "NOT EXISTS (\n\t\tSELECT 1 FROM task_tags tt") {
		t.Fatalf("asking for the archived tag must include archived tasks:\n%s", c.Where)
	}
}

func TestCompileUnavailableCustomField(t *testing.T) {
	ctx := testCtx()
	d, lost, err := NormalizeLenient(mustParse(t, `{"where":{"clauses":[{"field":"cf:gone","operator":"=","value":"x"}]},"columns":["id","cf:gone"]}`), ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(lost) != 1 || lost[0] != "gone" {
		t.Fatalf("expected gone to be unavailable: %v", lost)
	}
	if len(d.Columns) != 1 {
		t.Fatalf("unavailable column kept: %v", d.Columns)
	}
	c := Compile(d, ctx, nil)
	if !strings.HasSuffix(c.Where, "(FALSE))") {
		t.Fatalf("unavailable custom field must match nothing:\n%s", c.Where)
	}
}

func TestResolveDate(t *testing.T) {
	today := time.Date(2026, 11, 11, 0, 0, 0, 0, time.UTC) // Wednesday
	cases := map[string]string{
		"@today":            "2026-11-11",
		"@today-7":          "2026-11-04",
		"@today+30":         "2026-12-11",
		"@start_of_week":    "2026-11-09",
		"@start_of_week+6":  "2026-11-15",
		"@end_of_week":      "2026-11-15",
		"@end_of_week+1":    "2026-11-16",
		"@start_of_month":   "2026-11-01",
		"@start_of_year-1":  "2025-12-31",
		"2024-02-29":        "2024-02-29",
		"@START_OF_MONTH+1": "2026-11-02",
	}
	for in, want := range cases {
		got, ok := ResolveDate(in, today)
		if !ok || got.Format("2006-01-02") != want {
			t.Errorf("ResolveDate(%q) = %v %v, want %s", in, got, ok, want)
		}
	}
}

func TestFromLegacyFilter(t *testing.T) {
	d := FromLegacyFilter(storage.SavedViewFilter{
		Project:  "5",
		Status:   "incomplete",
		Due:      "overdue",
		Priority: "2,3",
		Tag:      "7,urgent",
		Claimed:  "me",
		Search:   "release",
		Sort:     "priority",
	}, func(id int) (string, bool) { return "work", id == 7 })
	if len(d.Scope.Projects) != 1 || d.Scope.Projects[0] != 5 || d.Scope.Personal() {
		t.Fatalf("project not mapped to scope: %+v", d.Scope)
	}
	want := []string{"completed", "due_date", "completed", "priority", "tags", "claimed_by", "text"}
	if len(d.Where.Clauses) != len(want) {
		t.Fatalf("unexpected clauses: %+v", d.Where.Clauses)
	}
	for i, f := range want {
		if d.Where.Clauses[i].Field != f {
			t.Fatalf("clause %d: got %s want %s", i, d.Where.Clauses[i].Field, f)
		}
	}
	if string(d.Where.Clauses[4].Value) != `["work","urgent"]` {
		t.Fatalf("tags not mapped: %s", d.Where.Clauses[4].Value)
	}
	if len(d.Sort) != 1 || d.Sort[0].Field != "priority" || d.Sort[0].Dir != "desc" {
		t.Fatalf("sort not mapped: %+v", d.Sort)
	}
}

func TestPrepareStrictRejectsHiddenReferences(t *testing.T) {
	loader := Loader{
		AccessibleProjects: func(int) ([]int, error) { return []int{1}, nil },
		CustomFields: func([]int) (map[string]CustomField, error) {
			return map[string]CustomField{"sev": {Type: "enum", Projects: []int{1}}}, nil
		},
		VisibleSprints: func(ids, _ []int) (map[int]bool, error) { return map[int]bool{10: true}, nil },
		VisiblePeople:  func(_ int, ids, _ []int) (map[int]bool, error) { return map[int]bool{7: true}, nil },
	}
	req := Request{UserID: 42, Timezone: "UTC", Now: time.Now()}
	bad := map[string]string{
		"scope project":  `{"scope":{"projects":[2]}}`,
		"clause project": `{"where":{"clauses":[{"field":"project","operator":"=","value":2}]}}`,
		"sprint":         `{"where":{"clauses":[{"field":"sprint","operator":"=","value":11}]}}`,
		"person":         `{"where":{"clauses":[{"field":"created_by","operator":"in","value":[7,8]}]}}`,
		"custom field":   `{"where":{"clauses":[{"field":"cf:hidden","operator":"=","value":"x"}]}}`,
		"custom column":  `{"columns":["cf:hidden"]}`,
	}
	for name, raw := range bad {
		t.Run(name, func(t *testing.T) {
			_, _, _, err := loader.Prepare(req, mustParse(t, raw))
			if !errors.Is(err, ErrUnknownReference) {
				t.Fatalf("expected ErrUnknownReference, got %v", err)
			}
		})
	}
	ok := `{"scope":{"projects":[1]},"where":{"clauses":[
		{"field":"sprint","operator":"in","value":[10,"@current_sprint"]},
		{"field":"claimed_by","operator":"in","value":[7,"@me",42]},
		{"field":"cf:sev","operator":"=","value":"high"}]}}`
	if _, _, _, err := loader.Prepare(req, mustParse(t, ok)); err != nil {
		t.Fatalf("valid references rejected: %v", err)
	}

	// Lenient: the same hidden references are reported, not fatal.
	req.Lenient = true
	_, ctx, un, err := loader.Prepare(req, mustParse(t, `{"scope":{"projects":[2]},"where":{"clauses":[
		{"field":"sprint","operator":"=","value":11},{"field":"cf:hidden","operator":"=","value":"x"}]}}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(un.Projects) != 1 || len(un.Sprints) != 1 || len(un.CustomFields) != 1 {
		t.Fatalf("unexpected unavailable: %+v", un)
	}
	if ctx.Sprints[11] {
		t.Fatal("hidden sprint must not be allowed")
	}

	// A forced (shared) project limits references to that project.
	req = Request{UserID: 42, Timezone: "UTC", Now: time.Now(), ForceProject: 1}
	loader.AccessibleProjects = func(int) ([]int, error) { return []int{1, 2}, nil }
	if _, _, _, err := loader.Prepare(req, mustParse(t, `{"where":{"clauses":[{"field":"project","operator":"=","value":2}]}}`)); !errors.Is(err, ErrUnknownReference) {
		t.Fatalf("a shared query may not reference another project: %v", err)
	}
}

func TestEndOfWeekOnSunday(t *testing.T) {
	sunday := time.Date(2026, 11, 15, 0, 0, 0, 0, time.UTC)
	monday := sunday.AddDate(0, 0, 1)
	if got, _ := ResolveDate("@end_of_week", sunday); got.Format("2006-01-02") != "2026-11-15" {
		t.Fatalf("on a Sunday @end_of_week is that day, got %s", got.Format("2006-01-02"))
	}
	if got, _ := ResolveDate("@end_of_week", monday); got.Format("2006-01-02") != "2026-11-22" {
		t.Fatalf("on a Monday @end_of_week is the coming Sunday, got %s", got.Format("2006-01-02"))
	}
}

func TestCompileBetweenDates(t *testing.T) {
	ctx := testCtx() // Wednesday 2026-11-11
	c := compileRaw(t, `{"where":{"clauses":[{"field":"due_date","operator":"between","value":["@start_of_week","@end_of_week"]}]}}`, ctx)
	if !strings.Contains(c.Where, "t.due_date BETWEEN $") {
		t.Fatalf("expected a BETWEEN:\n%s", c.Where)
	}
	n := len(c.Args)
	if c.Args[n-2] != "2026-11-09" || c.Args[n-1] != "2026-11-15" {
		t.Fatalf("expected this week's Monday..Sunday, got %v", c.Args[n-2:])
	}
	// Reversed ends still mean the same range.
	c = compileRaw(t, `{"where":{"clauses":[{"field":"due_date","operator":"between","value":["@end_of_week","@start_of_week"]}]}}`, ctx)
	n = len(c.Args)
	if c.Args[n-2] != "2026-11-09" || c.Args[n-1] != "2026-11-15" {
		t.Fatalf("reversed range not reordered: %v", c.Args[n-2:])
	}
}

func TestPerRowJoins(t *testing.T) {
	// Old definitions (no joins) take their group's op; the first row has none.
	d, err := Normalize(mustParse(t, `{"where":{"op":"or","clauses":[
		{"field":"id","operator":"=","value":1},
		{"field":"id","operator":"=","value":2}]}}`), nil)
	if err != nil {
		t.Fatal(err)
	}
	if d.Where.Clauses[0].Join != "" || d.Where.Clauses[1].Join != "or" {
		t.Fatalf("joins not filled from op: %+v", d.Where.Clauses)
	}

	// A and B or C and D  =>  (A AND B) OR (C AND D)
	c := compileRaw(t, `{"where":{"clauses":[
		{"field":"id","operator":"=","value":1},
		{"join":"and","field":"id","operator":"=","value":2},
		{"join":"OR","field":"id","operator":"=","value":3},
		{"join":"and","field":"id","operator":"=","value":4}]}}`, testCtx())
	want := "(((t.id = $3) AND (t.id = $4)) OR ((t.id = $5) AND (t.id = $6)))"
	if !strings.HasSuffix(c.Where, want) {
		t.Fatalf("AND must bind tighter than OR, want suffix %s:\n%s", want, c.Where)
	}

	// The first row's join is meaningless and dropped, also after an empty group is removed.
	d, err = Normalize(mustParse(t, `{"where":{"clauses":[
		{"op":"and","clauses":[]},
		{"join":"or","field":"id","operator":"=","value":1}]}}`), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(d.Where.Clauses) != 1 || d.Where.Clauses[0].Join != "" {
		t.Fatalf("first row should have no join: %+v", d.Where.Clauses)
	}

	if _, err := Normalize(mustParse(t, `{"where":{"clauses":[
		{"field":"id","operator":"=","value":1},
		{"join":"xor","field":"id","operator":"=","value":2}]}}`), nil); err == nil {
		t.Fatal("bad join accepted")
	}
}

func TestCompileReviewerFilterAndSort(t *testing.T) {
	ctx := testCtx()
	c := compileRaw(t, `{"where":{"clauses":[{"field":"reviewer","operator":"=","value":"@me"}]},"sort":[{"field":"reviewer","dir":"asc"}]}`, ctx)
	if !strings.Contains(c.Where, "t.reviewer_id") {
		t.Fatalf("reviewer filter should use t.reviewer_id:\n%s", c.Where)
	}
	if last := c.Args[len(c.Args)-1].([]int); len(last) != 1 || last[0] != 42 {
		t.Fatalf("@me should bind the caller, got %v", last)
	}
	if !strings.Contains(c.OrderBy, "qrv.") {
		t.Fatalf("reviewer sort should use the reviewer join:\n%s", c.OrderBy)
	}
	c = compileRaw(t, `{"where":{"clauses":[{"field":"reviewer","operator":"=","value":999}]}}`, ctx)
	if !strings.HasSuffix(c.Where, "(FALSE))") {
		t.Fatalf("unknown reviewer must match nothing:\n%s", c.Where)
	}
}
