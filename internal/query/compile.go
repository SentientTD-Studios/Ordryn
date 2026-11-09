package query

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"GoTodo/internal/storage"
)

// Context is what a definition compiles against: who is asking and what they
// may reference. Values outside these sets match nothing.
type Context struct {
	UserID   int
	Timezone string
	// Today is the caller's local calendar date.
	Today time.Time
	// Projects the caller can open.
	Projects map[int]bool
	// Sprints and Users the caller may reference.
	Sprints map[int]bool
	Users   map[int]bool
	// CustomFields maps a custom field key to its type and the accessible
	// projects it applies to. A key missing here matches nothing.
	CustomFields map[string]CustomField
	// ForceProject pins the scope to one project (a shared query or a
	// project-scoped API key), ignoring the definition's scope.
	ForceProject int
}

// CustomField is a custom field the caller can query.
type CustomField struct {
	Type     string
	Projects []int
}

// CustomFieldType implements FieldTypes.
func (c Context) CustomFieldType(key string) (string, bool) {
	f, ok := c.CustomFields[key]
	if !ok || len(f.Projects) == 0 {
		return "", false
	}
	return f.Type, true
}

// builder collects bound arguments.
type builder struct {
	args []any
	ctx  Context
	tz   string // placeholder for the timezone, added on first use
	user string
}

func (b *builder) arg(v any) string {
	b.args = append(b.args, v)
	return "$" + strconv.Itoa(len(b.args))
}

func (b *builder) tzArg() string {
	if b.tz == "" {
		tz := b.ctx.Timezone
		if tz == "" {
			tz = "UTC"
		}
		b.tz = b.arg(tz)
	}
	return b.tz
}

func (b *builder) userArg() string {
	if b.user == "" {
		b.user = b.arg(b.ctx.UserID)
	}
	return b.user
}

// Compiled is a definition turned into SQL over "tasks t".
type Compiled struct {
	// Where is a full WHERE condition (without the keyword): visibility,
	// scope, archive rules and the clause tree.
	Where string
	// OrderBy is an ORDER BY list (without the keyword). It needs the joins
	// in SelectJoins.
	OrderBy string
	Args    []any
}

// SelectJoins are the joins OrderBy may reference. Every one is a LEFT JOIN
// on a key of t, so it never adds or drops rows.
const SelectJoins = ` LEFT JOIN projects p ON p.id = t.project_id
	LEFT JOIN project_statuses qs ON qs.id = t.status_id
	LEFT JOIN project_sprints qsp ON qsp.id = t.sprint_id
	LEFT JOIN users quc ON quc.id = t.claimed_by
	LEFT JOIN users qcr ON qcr.id = t.user_id `

// Compile builds the SQL for d, numbering placeholders after the given args.
// d must already be normalized.
func Compile(d Definition, ctx Context, args []any) Compiled {
	b := &builder{args: append([]any(nil), args...), ctx: ctx}
	parts := []string{
		// Visibility first and outside every user clause: nothing below can widen it.
		storage.TaskVisibleCondition("t", b.userArg()),
		b.scopeSQL(d.Scope),
	}
	if !d.Scope.IncludeArchived && !referencesArchivedTag(d.Where) {
		parts = append(parts, "NOT "+storage.ArchivedTaskExistsSQL("t.id"))
	}
	if clause := b.node(d.Where); clause != "" && clause != "TRUE" {
		parts = append(parts, "("+clause+")")
	}
	return Compiled{
		Where:   strings.Join(parts, " AND "),
		OrderBy: orderBy(d.Sort),
		Args:    b.args,
	}
}

func (b *builder) scopeSQL(s Scope) string {
	if b.ctx.ForceProject > 0 {
		if !b.ctx.Projects[b.ctx.ForceProject] {
			return "FALSE"
		}
		return "t.project_id = " + b.arg(b.ctx.ForceProject)
	}
	// "All projects" means the projects the caller can open right now, not
	// every project with a task they once created: task visibility alone
	// still shows a creator their tasks in projects they have since left.
	ids := make([]int, 0, len(b.ctx.Projects))
	if len(s.Projects) == 0 {
		for id := range b.ctx.Projects {
			ids = append(ids, id)
		}
		sort.Ints(ids)
	} else {
		for _, id := range s.Projects {
			if b.ctx.Projects[id] {
				ids = append(ids, id)
			}
		}
	}
	// A list that lost every project matches nothing: it must never fall back
	// to "all projects".
	projectPart := "FALSE"
	if len(ids) > 0 {
		projectPart = "t.project_id = ANY(" + b.arg(ids) + ")"
	}
	if projectPart != "FALSE" {
		if !s.IncludeArchived {
			projectPart += " AND NOT EXISTS (SELECT 1 FROM projects qpa WHERE qpa.id = t.project_id AND COALESCE(qpa.archived, false))"
		}
		if s.ExcludeViewOnly {
			projectPart += " AND NOT EXISTS (SELECT 1 FROM project_members qpv WHERE qpv.project_id = t.project_id AND qpv.user_id = " +
				b.userArg() + " AND qpv.role IN ('viewer', 'view'))"
		}
	}
	if s.Personal() {
		return "((" + projectPart + ") OR t.project_id IS NULL)"
	}
	return "(" + projectPart + ")"
}

func referencesArchivedTag(n Node) bool {
	found := false
	Walk(n, func(c Node) {
		if c.Field != "tags" || (c.Operator != OpIn) {
			return
		}
		for _, v := range clauseValues(c) {
			if s, ok := v.(string); ok && (storage.IsArchivedTagName(s) || storage.IsRemovedTagName(s)) {
				found = true
			}
		}
	})
	return found
}

func (b *builder) node(n Node) string {
	if n.never {
		return "FALSE"
	}
	if !n.IsGroup() {
		return b.clause(n)
	}
	if len(n.Clauses) == 0 {
		return "TRUE"
	}
	// AND binds tighter than OR: split the rows into runs at each OR, AND
	// each run together, then OR the runs.
	var runs [][]string
	for i, c := range n.Clauses {
		join := c.Join
		if join == "" {
			join = n.Op
		}
		if i == 0 || join == GroupOr {
			runs = append(runs, nil)
		}
		runs[len(runs)-1] = append(runs[len(runs)-1], "("+b.node(c)+")")
	}
	if len(runs) == 1 {
		return strings.Join(runs[0], " AND ")
	}
	ors := make([]string, 0, len(runs))
	for _, r := range runs {
		if len(r) == 1 {
			ors = append(ors, r[0])
		} else {
			ors = append(ors, "("+strings.Join(r, " AND ")+")")
		}
	}
	return strings.Join(ors, " OR ")
}

// ---- clause compilation ----

func (b *builder) clause(n Node) string {
	if strings.HasPrefix(n.Field, "cf:") {
		return b.customClause(n)
	}
	vals := clauseValues(n)
	switch n.Field {
	case "id":
		return b.scalar("t.id", n, int64Values(vals), "bigint")
	case "priority":
		return b.scalar("COALESCE(t.priority, 0)", n, int64Values(vals), "bigint")
	case "estimate":
		return b.scalar("t.estimate_points", n, int64Values(vals), "bigint")
	case "title":
		return b.text("t.title", n, vals)
	case "description":
		return b.text("COALESCE(t.description, '')", n, vals)
	case "text":
		return b.search(n, vals)
	case "completed":
		return b.boolean("COALESCE(t.completed, false)", n, vals)
	case "favorite":
		return b.boolean("COALESCE(t.is_favorite, false)", n, vals)
	case "is_subtask":
		return b.boolean("(t.parent_id IS NOT NULL)", n, vals)
	case "has_subtasks":
		return b.boolean("EXISTS (SELECT 1 FROM tasks qc WHERE qc.parent_id = t.id AND qc.deleted_at IS NULL)", n, vals)
	case "due_date":
		return b.date("t.due_date", n, vals)
	case "created_date":
		return b.date("((t.time_stamp AT TIME ZONE 'UTC') AT TIME ZONE "+b.tzArg()+")::date", n, vals)
	case "modified_date":
		return b.date("((t.date_modified AT TIME ZONE 'UTC') AT TIME ZONE "+b.tzArg()+")::date", n, vals)
	case "completed_date":
		tz := b.tzArg()
		expr := "(CASE WHEN COALESCE(t.completed, false) THEN ((COALESCE((SELECT MAX(qte.created_at) FROM task_events qte WHERE qte.task_id = t.id AND qte.event_type = 'completed'), t.date_modified, t.time_stamp) AT TIME ZONE 'UTC') AT TIME ZONE " + tz + ")::date END)"
		return b.date(expr, n, vals)
	case "status_changed_date":
		return b.date("(("+StatusEnteredSQL+" AT TIME ZONE 'UTC') AT TIME ZONE "+b.tzArg()+")::date", n, vals)
	case "days_in_status":
		// Whole days between the caller's today and the day the task entered
		// its status, in the caller's timezone.
		today := b.arg(b.ctx.Today.Format("2006-01-02"))
		expr := "(" + today + "::date - ((" + StatusEnteredSQL + " AT TIME ZONE 'UTC') AT TIME ZONE " + b.tzArg() + ")::date)"
		return b.scalar(expr, n, int64Values(vals), "bigint")
	case "project":
		return b.idSet("t.project_id", n, b.allowed(vals, b.ctx.Projects), nil)
	case "claimed_by":
		return b.idSet("t.claimed_by", n, b.userIDs(vals), nil)
	case "created_by":
		return b.idSet("t.user_id", n, b.userIDs(vals), nil)
	case "sprint":
		ids, current := b.sprintIDs(vals)
		var extra func() string
		if current {
			extra = func() string {
				today := b.arg(b.ctx.Today.Format("2006-01-02"))
				return "t.sprint_id IN (SELECT qsc.id FROM project_sprints qsc WHERE qsc.start_date <= " + today +
					"::date AND qsc.end_date >= " + today + "::date)"
			}
		}
		return b.idSet("t.sprint_id", n, ids, extra)
	case "workflow_status":
		if n.Operator == OpEmpty {
			return "t.status_id IS NULL"
		}
		if n.Operator == OpNotEmpty {
			return "t.status_id IS NOT NULL"
		}
		match := "EXISTS (SELECT 1 FROM project_statuses qps WHERE qps.id = t.status_id AND LOWER(qps.name) = ANY(" +
			b.arg(stringValues(vals)) + "))"
		return positiveOrNegative(n.Operator, match)
	case "tags":
		switch n.Operator {
		case OpEmpty:
			return "NOT EXISTS (SELECT 1 FROM task_tags qtt WHERE qtt.task_id = t.id)"
		case OpNotEmpty:
			return "EXISTS (SELECT 1 FROM task_tags qtt WHERE qtt.task_id = t.id)"
		}
		names := stringValues(vals)
		for _, name := range names {
			// The archived tag still answers to its legacy "removed" name.
			if storage.IsArchivedTagName(name) || storage.IsRemovedTagName(name) {
				names = append(names, strings.ToLower(storage.ArchivedTagName), strings.ToLower(storage.RemovedTagName))
				break
			}
		}
		match := "EXISTS (SELECT 1 FROM task_tags qtt JOIN tags qtg ON qtg.id = qtt.tag_id WHERE qtt.task_id = t.id AND LOWER(qtg.name) = ANY(" +
			b.arg(names) + "))"
		return positiveOrNegative(n.Operator, match)
	}
	return "FALSE"
}

// positiveOrNegative turns a match condition into the clause for =/in (match)
// or <>/not_in (no match, counting NULL as no match).
func positiveOrNegative(op, match string) string {
	switch op {
	case OpEq, OpIn:
		return match
	default:
		return "NOT COALESCE((" + match + "), false)"
	}
}

func (b *builder) scalar(expr string, n Node, vals []int64, _ string) string {
	switch n.Operator {
	case OpEmpty:
		return expr + " IS NULL"
	case OpNotEmpty:
		return expr + " IS NOT NULL"
	case OpEq:
		return expr + " = " + b.arg(vals[0])
	case OpNe:
		return expr + " IS DISTINCT FROM " + b.arg(vals[0])
	case OpLt, OpLe, OpGt, OpGe:
		return expr + " " + n.Operator + " " + b.arg(vals[0])
	case OpIn:
		return expr + " = ANY(" + b.arg(vals) + ")"
	case OpNotIn:
		return "NOT COALESCE(" + expr + " = ANY(" + b.arg(vals) + "), false)"
	}
	return "FALSE"
}

func (b *builder) floatScalar(expr string, n Node, vals []float64) string {
	switch n.Operator {
	case OpEmpty:
		return expr + " IS NULL"
	case OpNotEmpty:
		return expr + " IS NOT NULL"
	case OpEq:
		return expr + " = " + b.arg(vals[0])
	case OpNe:
		return expr + " IS DISTINCT FROM " + b.arg(vals[0])
	case OpLt, OpLe, OpGt, OpGe:
		return expr + " " + n.Operator + " " + b.arg(vals[0])
	case OpIn:
		return expr + " = ANY(" + b.arg(vals) + ")"
	case OpNotIn:
		return "NOT COALESCE(" + expr + " = ANY(" + b.arg(vals) + "), false)"
	}
	return "FALSE"
}

func (b *builder) text(expr string, n Node, vals []any) string {
	switch n.Operator {
	case OpEmpty:
		return "COALESCE(" + expr + ", '') = ''"
	case OpNotEmpty:
		return "COALESCE(" + expr + ", '') <> ''"
	case OpContains:
		return expr + " ILIKE " + b.arg(likePattern(firstString(vals)))
	case OpNotContains:
		return "NOT COALESCE(" + expr + " ILIKE " + b.arg(likePattern(firstString(vals))) + ", false)"
	case OpEq:
		return "LOWER(" + expr + ") = LOWER(" + b.arg(firstString(vals)) + ")"
	case OpNe:
		return "LOWER(" + expr + ") IS DISTINCT FROM LOWER(" + b.arg(firstString(vals)) + ")"
	}
	return "FALSE"
}

func (b *builder) search(n Node, vals []any) string {
	q := firstString(vals)
	p := b.arg(likePattern(q))
	match := "(t.title ILIKE " + p + " OR COALESCE(t.description, '') ILIKE " + p +
		" OR EXISTS (SELECT 1 FROM task_tags qst JOIN tags qsg ON qsg.id = qst.tag_id WHERE qst.task_id = t.id AND qsg.name ILIKE " + p + ")"
	if id, ok := searchTaskID(q); ok {
		match += " OR t.id = " + b.arg(int64(id))
	}
	match += ")"
	if n.Operator == OpNotContains {
		return "NOT " + match
	}
	return match
}

func (b *builder) boolean(expr string, n Node, vals []any) string {
	v, _ := firstValue(vals).(bool)
	if n.Operator == OpNe {
		v = !v
	}
	if v {
		return expr
	}
	return "NOT " + expr
}

func (b *builder) date(expr string, n Node, vals []any) string {
	switch n.Operator {
	case OpEmpty:
		return expr + " IS NULL"
	case OpNotEmpty:
		return expr + " IS NOT NULL"
	case OpBetween:
		if len(vals) != 2 {
			return "FALSE"
		}
		from, ok1 := ResolveDate(stringAt(vals, 0), b.ctx.Today)
		to, ok2 := ResolveDate(stringAt(vals, 1), b.ctx.Today)
		if !ok1 || !ok2 {
			return "FALSE"
		}
		// Either order works: "between @end_of_week and @start_of_week" still means this week.
		if to.Before(from) {
			from, to = to, from
		}
		return expr + " BETWEEN " + b.arg(from.Format("2006-01-02")) + "::date AND " + b.arg(to.Format("2006-01-02")) + "::date"
	}
	d, ok := ResolveDate(firstString(vals), b.ctx.Today)
	if !ok {
		return "FALSE"
	}
	p := b.arg(d.Format("2006-01-02")) + "::date"
	switch n.Operator {
	case OpEq:
		return expr + " = " + p
	case OpNe:
		return expr + " IS DISTINCT FROM " + p
	case OpLt, OpLe, OpGt, OpGe:
		return expr + " " + n.Operator + " " + p
	}
	return "FALSE"
}

// idSet compiles =, <>, in, not_in, is_empty and is_not_empty over an id
// column. ids are only the values the caller may reference; extra adds an
// alternative match (the current sprint).
func (b *builder) idSet(expr string, n Node, ids []int, extra func() string) string {
	switch n.Operator {
	case OpEmpty:
		return expr + " IS NULL"
	case OpNotEmpty:
		return expr + " IS NOT NULL"
	}
	alts := []string{}
	if len(ids) > 0 {
		alts = append(alts, expr+" = ANY("+b.arg(ids)+")")
	}
	if extra != nil {
		alts = append(alts, extra())
	}
	match := "FALSE"
	if len(alts) > 0 {
		match = strings.Join(alts, " OR ")
	}
	return positiveOrNegative(n.Operator, match)
}

func (b *builder) allowed(vals []any, set map[int]bool) []int {
	out := []int{}
	for _, v := range vals {
		if id, ok := intValue(v); ok && set[id] {
			out = append(out, id)
		}
	}
	return out
}

func (b *builder) userIDs(vals []any) []int {
	out := []int{}
	for _, v := range vals {
		if s, ok := v.(string); ok && s == MacroMe {
			out = append(out, b.ctx.UserID)
			continue
		}
		if id, ok := intValue(v); ok && (id == b.ctx.UserID || b.ctx.Users[id]) {
			out = append(out, id)
		}
	}
	return out
}

func (b *builder) sprintIDs(vals []any) ([]int, bool) {
	out := []int{}
	current := false
	for _, v := range vals {
		if s, ok := v.(string); ok && s == MacroCurrentSprint {
			current = true
			continue
		}
		if id, ok := intValue(v); ok && b.ctx.Sprints[id] {
			out = append(out, id)
		}
	}
	return out, current
}

func (b *builder) customClause(n Node) string {
	key := strings.TrimPrefix(n.Field, "cf:")
	cf, ok := b.ctx.CustomFields[key]
	if !ok || len(cf.Projects) == 0 {
		return "FALSE"
	}
	kind, ok := customKind(cf.Type)
	if !ok {
		return "FALSE"
	}
	vals := clauseValues(n)
	value := func(projection string) string {
		// Only projects where the field applies count; elsewhere it reads as empty.
		return "(SELECT " + projection + " FROM custom_field_values qcf WHERE qcf.task_id = t.id AND qcf.field_key = " +
			b.arg(key) + " AND t.project_id = ANY(" + b.arg(cf.Projects) + "))"
	}
	switch kind.id {
	case kText:
		return b.text(value("qcf.value #>> '{}'"), n, vals)
	case kEnum:
		expr := value("qcf.value #>> '{}'")
		switch n.Operator {
		case OpEmpty:
			return "COALESCE(" + expr + ", '') = ''"
		case OpNotEmpty:
			return "COALESCE(" + expr + ", '') <> ''"
		case OpEq, OpIn:
			return "COALESCE(" + expr + " = ANY(" + b.arg(stringValues(vals)) + "), false)"
		default:
			return "NOT COALESCE(" + expr + " = ANY(" + b.arg(stringValues(vals)) + "), false)"
		}
	case kNumber:
		expr := value("CASE WHEN jsonb_typeof(qcf.value) = 'number' THEN (qcf.value #>> '{}')::float8 END")
		return b.floatScalar(expr, n, float64Values(vals))
	case kBool:
		return b.boolean("COALESCE("+value("qcf.value = 'true'::jsonb")+", false)", n, vals)
	case kDate:
		expr := value(`CASE WHEN (qcf.value #>> '{}') ~ '^\d{4}-(0[1-9]|1[0-2])-(0[1-9]|[12]\d|3[01])' THEN LEFT(qcf.value #>> '{}', 10)::date END`)
		return b.date(expr, n, vals)
	case kUser:
		expr := value("CASE WHEN jsonb_typeof(qcf.value) = 'number' THEN (qcf.value #>> '{}')::float8::int END")
		return b.idSet(expr, n, b.userIDs(vals), nil)
	}
	return "FALSE"
}

// StatusEnteredSQL is when task t entered its current workflow status: its
// last status move, or its creation if it never moved (the same rule the
// automation "in status for N days" trigger uses). It is NULL for tasks with
// no workflow status. The value is a UTC timestamp.
const StatusEnteredSQL = `(CASE WHEN t.status_id IS NOT NULL THEN COALESCE(
	(SELECT MAX(qse.created_at) FROM task_events qse WHERE qse.task_id = t.id AND qse.event_type = 'status_changed'),
	t.time_stamp) END)`

// ---- sorting ----

var sortExprs = map[string]string{
	"id":              "t.id",
	"title":           "LOWER(t.title)",
	"project":         "LOWER(p.name)",
	"priority":        "COALESCE(t.priority, 0)",
	"due_date":        "t.due_date",
	"created_date":    "t.time_stamp",
	"modified_date":   "t.date_modified",
	"completed":       "COALESCE(t.completed, false)",
	"workflow_status": "LOWER(qs.name)",
	"estimate":        "t.estimate_points",
	"claimed_by":      "LOWER(COALESCE(NULLIF(quc.full_name, ''), NULLIF(quc.user_name, ''), quc.email))",
	"created_by":      "LOWER(COALESCE(NULLIF(qcr.full_name, ''), NULLIF(qcr.user_name, ''), qcr.email))",
	"sprint":          "qsp.start_date",
	// Longest in status first when descending.
	"status_changed_date": StatusEnteredSQL,
	"days_in_status":      "(NOW() AT TIME ZONE 'UTC') - " + StatusEnteredSQL,
}

// SortableFields lists the fields results can be sorted by.
func SortableFields() []string {
	return []string{"id", "title", "project", "priority", "due_date", "created_date", "modified_date",
		"completed", "workflow_status", "status_changed_date", "days_in_status", "estimate", "claimed_by", "created_by", "sprint"}
}

func orderBy(keys []SortKey) string {
	parts := make([]string, 0, len(keys)+1)
	for _, k := range keys {
		expr, ok := sortExprs[k.Field]
		if !ok {
			continue
		}
		dir := "ASC"
		if k.Dir == "desc" {
			dir = "DESC"
		}
		parts = append(parts, expr+" "+dir+" NULLS LAST")
	}
	parts = append(parts, "t.id ASC")
	return strings.Join(parts, ", ")
}

// ---- values ----

// ResolveDate turns a YYYY-MM-DD date or a date macro into a date, relative
// to today.
func ResolveDate(s string, today time.Time) (time.Time, bool) {
	s = strings.ToLower(strings.TrimSpace(s))
	if m := dateMacroRe.FindStringSubmatch(s); m != nil {
		base := time.Date(today.Year(), today.Month(), today.Day(), 0, 0, 0, 0, time.UTC)
		switch m[1] {
		case "start_of_week":
			// Weeks start on Monday, as Postgres date_trunc('week') does.
			offset := (int(base.Weekday()) + 6) % 7
			base = base.AddDate(0, 0, -offset)
		case "end_of_week":
			// The Sunday that ends the Monday-first week.
			offset := (7 - int(base.Weekday())) % 7
			base = base.AddDate(0, 0, offset)
		case "start_of_month":
			base = time.Date(base.Year(), base.Month(), 1, 0, 0, 0, 0, time.UTC)
		case "start_of_year":
			base = time.Date(base.Year(), 1, 1, 0, 0, 0, 0, time.UTC)
		}
		if m[2] != "" {
			n, err := strconv.Atoi(m[2])
			if err != nil {
				return time.Time{}, false
			}
			base = base.AddDate(0, 0, n)
		}
		return base, true
	}
	if !dateRe.MatchString(s) {
		return time.Time{}, false
	}
	t, err := time.Parse("2006-01-02", s)
	return t, err == nil
}

// likePattern matches s literally anywhere: %, _ and \ are escaped.
func likePattern(s string) string {
	return "%" + strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(s) + "%"
}

func searchTaskID(q string) (int, bool) {
	s := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(q), "#"))
	if s == "" {
		return 0, false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return 0, false
		}
	}
	id, err := strconv.Atoi(s)
	return id, err == nil && id > 0
}

func firstValue(vals []any) any {
	if len(vals) == 0 {
		return nil
	}
	return vals[0]
}

func stringAt(vals []any, i int) string {
	if i >= len(vals) {
		return ""
	}
	s, _ := vals[i].(string)
	return s
}

func firstString(vals []any) string {
	s, _ := firstValue(vals).(string)
	return s
}

func stringValues(vals []any) []string {
	out := make([]string, 0, len(vals))
	for _, v := range vals {
		if s, ok := v.(string); ok {
			out = append(out, s)
		}
	}
	return out
}

func int64Values(vals []any) []int64 {
	out := make([]int64, 0, len(vals))
	for _, v := range vals {
		if f, ok := v.(float64); ok {
			out = append(out, int64(f))
		}
	}
	if len(out) == 0 {
		out = append(out, 0)
	}
	return out
}

func float64Values(vals []any) []float64 {
	out := make([]float64, 0, len(vals))
	for _, v := range vals {
		if f, ok := v.(float64); ok {
			out = append(out, f)
		}
	}
	if len(out) == 0 {
		out = append(out, 0)
	}
	return out
}

// String renders a compiled query for debugging.
func (c Compiled) String() string {
	return fmt.Sprintf("WHERE %s ORDER BY %s %v", c.Where, c.OrderBy, c.Args)
}
