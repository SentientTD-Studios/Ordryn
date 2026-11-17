// Package query implements saved task queries: a versioned definition (scope,
// a tree of AND/OR clauses, columns, sort, flat or tree mode), its validation,
// and its compilation into parameterized SQL.
//
// Access control is layered so a definition can never widen what the caller
// sees:
//   - The compiled clause tree is always wrapped in parentheses and ANDed with
//     storage.TaskVisibleCondition by the runner, so no OR can escape it.
//   - Every referenced project, sprint, person and custom field is checked
//     against the caller's access when a query is saved or run ad hoc, and
//     rejected with one generic error whether it is missing or just hidden.
//   - When a saved query runs, references the caller has since lost access to
//     match nothing instead of failing, and are reported back as unavailable.
//   - Every value is a bound parameter; field and operator names only pick
//     from fixed SQL fragments in this package.
package query

import (
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

// CurrentVersion is the definition version this package writes.
const CurrentVersion = 2

// Limits on a definition's size.
const (
	MaxClauses    = 50
	MaxDepth      = 5
	MaxListValues = 50
	MaxTextLength = 200
	MaxColumns    = 25
	MaxSortKeys   = 3
	MaxScopeProjs = 100
)

// Modes.
const (
	ModeFlat = "flat"
	ModeTree = "tree"
)

// Group operators.
const (
	GroupAnd = "and"
	GroupOr  = "or"
)

// Clause operators.
const (
	OpEq          = "="
	OpNe          = "<>"
	OpLt          = "<"
	OpLe          = "<="
	OpGt          = ">"
	OpGe          = ">="
	OpIn          = "in"
	OpNotIn       = "not_in"
	OpContains    = "contains"
	OpNotContains = "not_contains"
	OpEmpty       = "is_empty"
	OpNotEmpty    = "is_not_empty"
	// OpBetween takes [from, to] and matches both ends (dates only).
	OpBetween = "between"
)

// Macros usable as values.
const (
	MacroMe            = "@me"
	MacroCurrentSprint = "@current_sprint"
)

// Definition is a saved or ad-hoc query.
type Definition struct {
	Version int       `json:"version"`
	Scope   Scope     `json:"scope"`
	Where   Node      `json:"where"`
	Mode    string    `json:"mode"`
	Columns []string  `json:"columns"`
	Sort    []SortKey `json:"sort"`
}

// Scope picks which tasks a query starts from, before its clauses.
type Scope struct {
	// Projects limits the query to these projects. Organizations adds every
	// project of these organizations that the caller can open. With both
	// empty, the query covers every project the caller can open.
	Projects      []int `json:"projects"`
	Organizations []int `json:"organizations,omitempty"`
	// IncludePersonal adds the caller's tasks that have no project. Nil means true.
	IncludePersonal *bool `json:"include_personal,omitempty"`
	// IncludeArchived adds archived projects and archived tasks.
	IncludeArchived bool `json:"include_archived"`
	// ExcludeViewOnly leaves out projects the caller can only view.
	ExcludeViewOnly bool `json:"exclude_view_only"`
}

// Personal reports whether tasks without a project are in scope.
func (s Scope) Personal() bool {
	return s.IncludePersonal == nil || *s.IncludePersonal
}

// Node is either a group (Op + Clauses) or a clause (Field + Operator + Value).
//
// Join ("and" or "or") connects a node to the sibling before it, so every row
// in a group picks its own connector; the first row has none. A row without a
// Join uses its group's Op. AND binds tighter than OR: "A and B or C" is
// "(A and B) or C"; nest a group to change that.
type Node struct {
	Join     string          `json:"join,omitempty"`
	Op       string          `json:"op,omitempty"`
	Clauses  []Node          `json:"clauses,omitempty"`
	Field    string          `json:"field,omitempty"`
	Operator string          `json:"operator,omitempty"`
	Value    json.RawMessage `json:"value,omitempty"`

	// never marks a clause that can no longer apply (its custom field is
	// gone or hidden); it compiles to FALSE. Only lenient normalization sets it.
	never bool
}

// IsGroup reports whether n is a group node.
func (n Node) IsGroup() bool { return n.Field == "" }

// SortKey orders results.
type SortKey struct {
	Field string `json:"field"`
	Dir   string `json:"dir"`
}

// ValidationError is a problem with a definition the caller can fix.
type ValidationError struct{ msg string }

func (e *ValidationError) Error() string { return e.msg }

func invalid(format string, args ...any) error {
	return &ValidationError{msg: fmt.Sprintf(format, args...)}
}

// IsValidationError reports whether err is a ValidationError.
func IsValidationError(err error) bool {
	var v *ValidationError
	return errors.As(err, &v)
}

// DefaultDefinition is an empty query: every task in scope, flat, default columns.
func DefaultDefinition() Definition {
	t := true
	return Definition{
		Version: CurrentVersion,
		Scope:   Scope{Projects: []int{}, IncludePersonal: &t},
		Where:   Node{Op: GroupAnd, Clauses: []Node{}},
		Mode:    ModeFlat,
		Columns: DefaultColumns(),
		Sort:    []SortKey{},
	}
}

// DefaultColumns is the column set a new query shows.
func DefaultColumns() []string {
	return []string{"id", "title", "project", "workflow_status", "priority", "due_date", "claimed_by", "tags"}
}

// Parse decodes a definition, rejecting unknown fields.
func Parse(raw []byte) (Definition, error) {
	var d Definition
	dec := json.NewDecoder(strings.NewReader(string(raw)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&d); err != nil {
		return Definition{}, invalid("definition is not valid JSON: %s", trimJSONErr(err))
	}
	return d, nil
}

func trimJSONErr(err error) string {
	s := err.Error()
	if len(s) > 120 {
		s = s[:120]
	}
	return s
}

// FieldTypes looks up the type of a custom field ("cf:<key>"). ok is false for
// a field the caller cannot query.
type FieldTypes interface {
	CustomFieldType(key string) (typ string, ok bool)
}

// Normalize validates d's structure and returns a canonical copy: defaults
// filled in, names lower-cased, values in their canonical JSON form. It does
// not check access; see CheckAccess.
func Normalize(d Definition, ft FieldTypes) (Definition, error) {
	out, _, err := normalize(d, ft, false)
	return out, err
}

// NormalizeLenient is Normalize for running a saved query: a clause on a
// custom field the caller can no longer query matches nothing instead of
// failing, and its key is returned. Columns on such fields are dropped.
func NormalizeLenient(d Definition, ft FieldTypes) (Definition, []string, error) {
	return normalize(d, ft, true)
}

func normalize(d Definition, ft FieldTypes, lenient bool) (Definition, []string, error) {
	out := Definition{Version: CurrentVersion}
	n := &normalizer{ft: ft, lenient: lenient}
	o, err := n.run(d, out)
	return o, n.unavailable, err
}

type normalizer struct {
	ft          FieldTypes
	lenient     bool
	count       int
	unavailable []string
}

func (z *normalizer) markUnavailable(field string) {
	key := strings.TrimPrefix(field, "cf:")
	for _, k := range z.unavailable {
		if k == key {
			return
		}
	}
	z.unavailable = append(z.unavailable, key)
}

func (z *normalizer) run(d Definition, out Definition) (Definition, error) {
	ft := z.ft
	if d.Version != 0 && d.Version != CurrentVersion {
		return out, invalid("definition.version must be %d.", CurrentVersion)
	}

	// Scope.
	seen := map[int]bool{}
	out.Scope.Projects = []int{}
	for _, id := range d.Scope.Projects {
		if id <= 0 {
			return out, invalid("scope.projects must contain positive project ids.")
		}
		if !seen[id] {
			seen[id] = true
			out.Scope.Projects = append(out.Scope.Projects, id)
		}
	}
	if len(out.Scope.Projects) > MaxScopeProjs {
		return out, invalid("scope.projects may list at most %d projects.", MaxScopeProjs)
	}
	seenOrg := map[int]bool{}
	for _, id := range d.Scope.Organizations {
		if id <= 0 {
			return out, invalid("scope.organizations must contain positive organization ids.")
		}
		if !seenOrg[id] {
			seenOrg[id] = true
			out.Scope.Organizations = append(out.Scope.Organizations, id)
		}
	}
	if len(out.Scope.Organizations) > MaxScopeProjs {
		return out, invalid("scope.organizations may list at most %d organizations.", MaxScopeProjs)
	}
	personal := d.Scope.Personal()
	out.Scope.IncludePersonal = &personal
	out.Scope.IncludeArchived = d.Scope.IncludeArchived
	out.Scope.ExcludeViewOnly = d.Scope.ExcludeViewOnly

	// Mode.
	switch strings.ToLower(strings.TrimSpace(d.Mode)) {
	case "", ModeFlat:
		out.Mode = ModeFlat
	case ModeTree:
		out.Mode = ModeTree
	default:
		return out, invalid("mode must be %q or %q.", ModeFlat, ModeTree)
	}

	// Clauses.
	where := d.Where
	if where.IsGroup() && where.Op == "" && len(where.Clauses) == 0 {
		where = Node{Op: GroupAnd}
	}
	if !where.IsGroup() {
		where = Node{Op: GroupAnd, Clauses: []Node{where}}
	}
	norm, err := z.node(where, 1)
	if err != nil {
		return out, err
	}
	out.Where = norm

	// Columns.
	out.Columns = []string{}
	seenCol := map[string]bool{}
	for _, c := range d.Columns {
		c = canonicalFieldName(c)
		if !validColumn(c, ft) {
			if z.lenient && strings.HasPrefix(c, "cf:") {
				z.markUnavailable(c)
				continue
			}
			return out, invalid("columns: unknown column %q.", c)
		}
		if !seenCol[c] {
			seenCol[c] = true
			out.Columns = append(out.Columns, c)
		}
	}
	if len(out.Columns) > MaxColumns {
		return out, invalid("columns may list at most %d columns.", MaxColumns)
	}
	if len(out.Columns) == 0 {
		out.Columns = DefaultColumns()
	}

	// Sort.
	out.Sort = []SortKey{}
	if len(d.Sort) > MaxSortKeys {
		return out, invalid("sort may have at most %d keys.", MaxSortKeys)
	}
	for _, s := range d.Sort {
		f := canonicalFieldName(s.Field)
		if _, ok := sortExprs[f]; !ok {
			return out, invalid("sort: field %q cannot be sorted.", s.Field)
		}
		dir := strings.ToLower(strings.TrimSpace(s.Dir))
		switch dir {
		case "", "asc":
			dir = "asc"
		case "desc":
		default:
			return out, invalid("sort.dir must be \"asc\" or \"desc\".")
		}
		out.Sort = append(out.Sort, SortKey{Field: f, Dir: dir})
	}
	return out, nil
}

func (z *normalizer) node(n Node, depth int) (Node, error) {
	if n.IsGroup() {
		if depth > MaxDepth {
			return Node{}, invalid("clause groups may be nested at most %d deep.", MaxDepth)
		}
		if n.Operator != "" || len(n.Value) > 0 {
			return Node{}, invalid("a clause group cannot have an operator or value.")
		}
		op := strings.ToLower(strings.TrimSpace(n.Op))
		if op == "" {
			op = GroupAnd
		}
		if op != GroupAnd && op != GroupOr {
			return Node{}, invalid("group op must be \"and\" or \"or\".")
		}
		out := Node{Op: op, Clauses: []Node{}}
		for _, c := range n.Clauses {
			join := strings.ToLower(strings.TrimSpace(c.Join))
			if join == "" {
				join = op
			}
			if join != GroupAnd && join != GroupOr {
				return Node{}, invalid("join must be \"and\" or \"or\".")
			}
			nc, err := z.node(c, depth+1)
			if err != nil {
				return Node{}, err
			}
			// Drop empty groups so they never compile to "()".
			if nc.IsGroup() && len(nc.Clauses) == 0 {
				continue
			}
			// Every row but the first states its connector explicitly.
			if len(out.Clauses) > 0 {
				nc.Join = join
			} else {
				nc.Join = ""
			}
			out.Clauses = append(out.Clauses, nc)
		}
		return out, nil
	}
	if n.Op != "" || len(n.Clauses) > 0 {
		return Node{}, invalid("a clause cannot also be a group.")
	}
	z.count++
	if z.count > MaxClauses {
		return Node{}, invalid("a query may have at most %d clauses.", MaxClauses)
	}
	c, err := normalizeClause(n, z.ft)
	if err != nil && z.lenient && strings.HasPrefix(canonicalFieldName(n.Field), "cf:") {
		field := canonicalFieldName(n.Field)
		z.markUnavailable(field)
		return Node{Field: field, Operator: OpEmpty, never: true}, nil
	}
	return c, err
}

func canonicalFieldName(s string) string {
	s = strings.TrimSpace(s)
	if strings.HasPrefix(strings.ToLower(s), "cf:") {
		// Custom field keys keep their case.
		return "cf:" + s[3:]
	}
	return strings.ToLower(s)
}

func normalizeClause(n Node, ft FieldTypes) (Node, error) {
	field := canonicalFieldName(n.Field)
	kind, ok := fieldKind(field, ft)
	if !ok {
		return Node{}, invalid("unknown field %q.", n.Field)
	}
	op := strings.ToLower(strings.TrimSpace(n.Operator))
	if !kind.allows(op) {
		return Node{}, invalid("operator %q cannot be used with field %q.", n.Operator, field)
	}
	out := Node{Field: field, Operator: op}
	if op == OpEmpty || op == OpNotEmpty {
		if len(n.Value) > 0 && string(n.Value) != "null" {
			return Node{}, invalid("%s %s takes no value.", field, op)
		}
		return out, nil
	}
	if len(n.Value) == 0 || string(n.Value) == "null" {
		return Node{}, invalid("%s %s needs a value.", field, op)
	}
	if op == OpBetween {
		var raw []json.RawMessage
		if err := json.Unmarshal(n.Value, &raw); err != nil || len(raw) != 2 {
			return Node{}, invalid("%s between needs two values: [from, to].", field)
		}
		vals := make([]any, 0, 2)
		for _, r := range raw {
			v, err := kind.normalizeValue(field, r)
			if err != nil {
				return Node{}, err
			}
			vals = append(vals, v)
		}
		enc, _ := json.Marshal(vals)
		out.Value = enc
		return out, nil
	}
	if op == OpIn || op == OpNotIn {
		var raw []json.RawMessage
		if err := json.Unmarshal(n.Value, &raw); err != nil {
			return Node{}, invalid("%s %s needs a list of values.", field, op)
		}
		if len(raw) == 0 {
			return Node{}, invalid("%s %s needs at least one value.", field, op)
		}
		if len(raw) > MaxListValues {
			return Node{}, invalid("%s %s may list at most %d values.", field, op, MaxListValues)
		}
		vals := make([]any, 0, len(raw))
		for _, r := range raw {
			v, err := kind.normalizeValue(field, r)
			if err != nil {
				return Node{}, err
			}
			vals = append(vals, v)
		}
		enc, _ := json.Marshal(vals)
		out.Value = enc
		return out, nil
	}
	v, err := kind.normalizeValue(field, n.Value)
	if err != nil {
		return Node{}, err
	}
	enc, _ := json.Marshal(v)
	out.Value = enc
	return out, nil
}

// ---- field kinds ----

type kindID int

const (
	kText kindID = iota
	kNumber
	kDate
	kBool
	kProject
	kUser
	kSprint
	kStatus
	kTags
	kSearch
	kEnum
)

type fieldKindInfo struct {
	id kindID
	// priority restricts number values to 0..3.
	priority bool
}

var opsByKind = map[kindID][]string{
	kText:    {OpContains, OpNotContains, OpEq, OpNe, OpEmpty, OpNotEmpty},
	kNumber:  {OpEq, OpNe, OpLt, OpLe, OpGt, OpGe, OpIn, OpNotIn, OpEmpty, OpNotEmpty},
	kDate:    {OpBetween, OpEq, OpNe, OpLt, OpLe, OpGt, OpGe, OpEmpty, OpNotEmpty},
	kBool:    {OpEq, OpNe},
	kProject: {OpEq, OpNe, OpIn, OpNotIn, OpEmpty, OpNotEmpty},
	kUser:    {OpEq, OpNe, OpIn, OpNotIn, OpEmpty, OpNotEmpty},
	kSprint:  {OpEq, OpNe, OpIn, OpNotIn, OpEmpty, OpNotEmpty},
	kStatus:  {OpEq, OpNe, OpIn, OpNotIn, OpEmpty, OpNotEmpty},
	kTags:    {OpIn, OpNotIn, OpEmpty, OpNotEmpty},
	kSearch:  {OpContains, OpNotContains},
	kEnum:    {OpEq, OpNe, OpIn, OpNotIn, OpEmpty, OpNotEmpty},
}

func (k fieldKindInfo) allows(op string) bool {
	for _, o := range opsByKind[k.id] {
		if o == op {
			return true
		}
	}
	return false
}

// builtinFields maps each built-in field to its kind.
var builtinFields = map[string]fieldKindInfo{
	"id":                  {id: kNumber},
	"title":               {id: kText},
	"description":         {id: kText},
	"text":                {id: kSearch},
	"project":             {id: kProject},
	"completed":           {id: kBool},
	"workflow_status":     {id: kStatus},
	"priority":            {id: kNumber, priority: true},
	"tags":                {id: kTags},
	"due_date":            {id: kDate},
	"created_date":        {id: kDate},
	"modified_date":       {id: kDate},
	"completed_date":      {id: kDate},
	"status_changed_date": {id: kDate},
	"days_in_status":      {id: kNumber},
	"claimed_by":          {id: kUser},
	"reviewer":            {id: kUser},
	"created_by":          {id: kUser},
	"sprint":              {id: kSprint},
	"estimate":            {id: kNumber},
	"is_subtask":          {id: kBool},
	"has_subtasks":        {id: kBool},
	"favorite":            {id: kBool},
}

// BuiltinFieldNames lists the built-in queryable fields, sorted for display.
func BuiltinFieldNames() []string {
	return []string{
		"id", "title", "description", "text", "project", "completed", "workflow_status",
		"priority", "tags", "due_date", "created_date", "modified_date", "completed_date",
		"status_changed_date", "days_in_status", "claimed_by", "reviewer", "created_by", "sprint", "estimate", "is_subtask", "has_subtasks", "favorite",
	}
}

// OperatorsFor returns the operators a field allows, or nil for an unknown field.
func OperatorsFor(field string, ft FieldTypes) []string {
	k, ok := fieldKind(canonicalFieldName(field), ft)
	if !ok {
		return nil
	}
	return append([]string(nil), opsByKind[k.id]...)
}

// customKind maps a custom field type to a kind.
func customKind(typ string) (fieldKindInfo, bool) {
	switch typ {
	case "string", "url":
		return fieldKindInfo{id: kText}, true
	case "number":
		return fieldKindInfo{id: kNumber}, true
	case "boolean":
		return fieldKindInfo{id: kBool}, true
	case "date":
		return fieldKindInfo{id: kDate}, true
	case "enum":
		return fieldKindInfo{id: kEnum}, true
	case "user":
		return fieldKindInfo{id: kUser}, true
	}
	return fieldKindInfo{}, false
}

func fieldKind(field string, ft FieldTypes) (fieldKindInfo, bool) {
	if strings.HasPrefix(field, "cf:") {
		key := strings.TrimPrefix(field, "cf:")
		if key == "" || ft == nil {
			return fieldKindInfo{}, false
		}
		typ, ok := ft.CustomFieldType(key)
		if !ok {
			return fieldKindInfo{}, false
		}
		return customKind(typ)
	}
	k, ok := builtinFields[field]
	return k, ok
}

var columnOnly = map[string]bool{"parent": true, "estimate": true}

func validColumn(c string, ft FieldTypes) bool {
	if strings.HasPrefix(c, "cf:") {
		_, ok := fieldKind(c, ft)
		return ok
	}
	if c == "text" {
		return false
	}
	if _, ok := builtinFields[c]; ok {
		return true
	}
	return columnOnly[c]
}

var dateMacroRe = regexp.MustCompile(`^@(today|start_of_week|end_of_week|start_of_month|start_of_year)([+-]\d{1,4})?$`)
var dateRe = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)

func (k fieldKindInfo) normalizeValue(field string, raw json.RawMessage) (any, error) {
	switch k.id {
	case kText, kSearch, kStatus, kTags, kEnum:
		var s string
		if err := json.Unmarshal(raw, &s); err != nil {
			return nil, invalid("%s needs a text value.", field)
		}
		s = strings.TrimSpace(s)
		if s == "" {
			return nil, invalid("%s needs a non-empty value.", field)
		}
		if utf8.RuneCountInString(s) > MaxTextLength {
			return nil, invalid("%s values must be %d characters or less.", field, MaxTextLength)
		}
		if k.id == kStatus || k.id == kTags {
			s = strings.ToLower(s)
		}
		return s, nil
	case kNumber:
		var f float64
		if err := json.Unmarshal(raw, &f); err != nil {
			return nil, invalid("%s needs a number.", field)
		}
		if f < -1e12 || f > 1e12 {
			return nil, invalid("%s is out of range.", field)
		}
		if k.priority {
			if f != float64(int(f)) || f < 0 || f > 3 {
				return nil, invalid("priority must be 0-3.")
			}
			return int(f), nil
		}
		if field == "id" || field == "days_in_status" {
			if f != float64(int64(f)) {
				return nil, invalid("%s must be a whole number.", field)
			}
			return int64(f), nil
		}
		return f, nil
	case kBool:
		var b bool
		if err := json.Unmarshal(raw, &b); err != nil {
			return nil, invalid("%s needs true or false.", field)
		}
		return b, nil
	case kDate:
		var s string
		if err := json.Unmarshal(raw, &s); err != nil {
			return nil, invalid("%s needs a date (YYYY-MM-DD) or a date macro.", field)
		}
		s = strings.ToLower(strings.TrimSpace(s))
		if dateMacroRe.MatchString(s) {
			return s, nil
		}
		if dateRe.MatchString(s) {
			if _, err := time.Parse("2006-01-02", s); err == nil {
				return s, nil
			}
		}
		return nil, invalid("%s needs a date (YYYY-MM-DD) or a date macro like @today-7.", field)
	case kProject:
		id, err := positiveIDValue(raw)
		if err != nil {
			return nil, invalid("project values must be project ids.")
		}
		return id, nil
	case kUser:
		var s string
		if json.Unmarshal(raw, &s) == nil {
			if strings.ToLower(strings.TrimSpace(s)) == MacroMe {
				return MacroMe, nil
			}
		}
		id, err := positiveIDValue(raw)
		if err != nil {
			return nil, invalid("%s values must be user ids or @me.", field)
		}
		return id, nil
	case kSprint:
		var s string
		if json.Unmarshal(raw, &s) == nil {
			if strings.ToLower(strings.TrimSpace(s)) == MacroCurrentSprint {
				return MacroCurrentSprint, nil
			}
		}
		id, err := positiveIDValue(raw)
		if err != nil {
			return nil, invalid("sprint values must be sprint ids or @current_sprint.")
		}
		return id, nil
	}
	return nil, invalid("unsupported field %q.", field)
}

func positiveIDValue(raw json.RawMessage) (int, error) {
	var f float64
	if err := json.Unmarshal(raw, &f); err == nil {
		if f >= 1 && f == float64(int(f)) && f <= 2147483647 {
			return int(f), nil
		}
		return 0, errors.New("bad id")
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		if n, err := strconv.Atoi(strings.TrimSpace(s)); err == nil && n > 0 {
			return n, nil
		}
	}
	return 0, errors.New("bad id")
}

// clauseValues decodes a normalized clause's value as a list (one item for a
// scalar operator).
func clauseValues(n Node) []any {
	if len(n.Value) == 0 {
		return nil
	}
	if n.Operator == OpIn || n.Operator == OpNotIn || n.Operator == OpBetween {
		var vals []any
		_ = json.Unmarshal(n.Value, &vals)
		return vals
	}
	var v any
	_ = json.Unmarshal(n.Value, &v)
	return []any{v}
}

// intValue reads a normalized id value (JSON numbers decode as float64).
func intValue(v any) (int, bool) {
	f, ok := v.(float64)
	if !ok {
		return 0, false
	}
	return int(f), true
}

// Walk calls fn for every clause in the tree.
func Walk(n Node, fn func(Node)) {
	if n.IsGroup() {
		for _, c := range n.Clauses {
			Walk(c, fn)
		}
		return
	}
	fn(n)
}

// References lists the ids a definition refers to, by kind.
type References struct {
	Projects      []int
	Organizations []int
	Sprints       []int
	Users         []int
	CustomFields  []string
}

// Refs collects the references in d. ft resolves custom field types so ids in
// user-typed custom fields count as people.
func Refs(d Definition, ft FieldTypes) References {
	var r References
	seen := map[string]bool{}
	add := func(kind string, id int, dst *[]int) {
		k := kind + ":" + strconv.Itoa(id)
		if !seen[k] {
			seen[k] = true
			*dst = append(*dst, id)
		}
	}
	for _, p := range d.Scope.Projects {
		add("p", p, &r.Projects)
	}
	for _, o := range d.Scope.Organizations {
		add("o", o, &r.Organizations)
	}
	Walk(d.Where, func(n Node) {
		if strings.HasPrefix(n.Field, "cf:") {
			key := strings.TrimPrefix(n.Field, "cf:")
			if !seen["cf:"+key] {
				seen["cf:"+key] = true
				r.CustomFields = append(r.CustomFields, key)
			}
		}
		for _, v := range clauseValues(n) {
			id, ok := intValue(v)
			if !ok {
				continue
			}
			switch {
			case n.Field == "project":
				add("p", id, &r.Projects)
			case n.Field == "sprint":
				add("s", id, &r.Sprints)
			case n.Field == "claimed_by" || n.Field == "reviewer" || n.Field == "created_by":
				add("u", id, &r.Users)
			case strings.HasPrefix(n.Field, "cf:"):
				if k, ok := fieldKind(n.Field, ft); ok && k.id == kUser {
					add("u", id, &r.Users)
				}
			}
		}
	})
	for _, c := range d.Columns {
		if strings.HasPrefix(c, "cf:") {
			key := strings.TrimPrefix(c, "cf:")
			if !seen["cf:"+key] {
				seen["cf:"+key] = true
				r.CustomFields = append(r.CustomFields, key)
			}
		}
	}
	return r
}

var kindNames = map[kindID]string{
	kText: "text", kNumber: "number", kDate: "date", kBool: "boolean", kProject: "project",
	kUser: "user", kSprint: "sprint", kStatus: "status", kTags: "tags", kSearch: "search", kEnum: "enum",
}

// FieldType names a field's value type for editors: text, number, date,
// boolean, project, user, sprint, status, tags, search, or enum. It is empty
// for an unknown field.
func FieldType(field string, ft FieldTypes) string {
	k, ok := fieldKind(canonicalFieldName(field), ft)
	if !ok {
		return ""
	}
	return kindNames[k.id]
}
