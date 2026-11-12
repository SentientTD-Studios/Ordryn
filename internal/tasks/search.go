package tasks

import (
	"GoTodo/internal/storage"
	"context"
	"fmt"
	"slices"
	"strings"
	"unicode"

	"github.com/jackc/pgx/v5"
)

// Search box limits: anything past them is dropped rather than rejected.
const (
	maxSearchQueryLength = 500
	maxSearchTerms       = 12
	maxSearchTermLength  = 100
	// MaxSearchPageSize caps one page of site search results.
	MaxSearchPageSize = 50
	// maxSearchProjects caps how many matching projects a search returns.
	maxSearchProjects = 5
)

// SearchQuery is a parsed search box query. Free text is split into terms that
// must all match, each anywhere in the task's title, description, a tag name or
// a comment, or as its id; TitlesOnly (the header's live results) looks at
// titles and ids only. A title word one typo away also matches, see
// CloseSpellings. Qualifiers narrow the results:
//
//	#123, id:123             the task with that id
//	project:web              tasks in projects whose name contains "web"
//	tag:bug                  tasks with a tag whose name contains "bug"
//	status:review            tasks whose workflow status name contains "review"
//	is:open / is:done        incomplete / completed tasks (status:open / status:done too)
//	is:archived              also search archived tasks and archived projects
//	claimed:me / claimed:none
//
// Values with spaces are quoted: project:"web app", "exact phrase".
type SearchQuery struct {
	Terms     []string
	IDs       []int
	Projects  []string
	Tags      []string
	Statuses  []string
	Completed *bool
	Archived  bool
	Claimed   string
	// TitlesOnly limits free text to titles (and ids). Not parsed from the
	// query: callers set it for live, as-you-type results.
	TitlesOnly bool
}

// Empty reports whether the query asks for nothing, so callers can skip it.
func (q SearchQuery) Empty() bool {
	return len(q.Terms) == 0 && len(q.IDs) == 0 && len(q.Projects) == 0 && len(q.Tags) == 0 &&
		len(q.Statuses) == 0 && q.Completed == nil && q.Claimed == ""
}

// ParseSearchQuery parses a search box query. Unknown qualifiers ("http://x")
// stay plain text.
func ParseSearchQuery(raw string) SearchQuery {
	var q SearchQuery
	for _, tok := range tokenizeSearch(raw) {
		key, value, qualified := splitQualifier(tok)
		if !qualified {
			addSearchTerm(&q, unquoteSearch(tok))
			continue
		}
		value = unquoteSearch(value)
		if value == "" {
			continue
		}
		lower := strings.ToLower(value)
		switch key {
		case "id":
			if id, ok := parseSearchTaskID(value); ok {
				q.IDs = append(q.IDs, id)
			}
		case "project", "proj":
			q.Projects = appendCapped(q.Projects, clipTerm(value))
		case "tag", "tags":
			q.Tags = appendCapped(q.Tags, clipTerm(value))
		case "status", "state":
			if done, ok := completionWord(lower); ok {
				q.Completed = &done
			} else {
				q.Statuses = appendCapped(q.Statuses, clipTerm(value))
			}
		case "is":
			if done, ok := completionWord(lower); ok {
				q.Completed = &done
			} else if lower == "archived" {
				q.Archived = true
			}
		case "claimed", "assignee", "assigned":
			if c := NormalizeClaimFilter(strings.TrimPrefix(lower, "@")); c != "" {
				q.Claimed = c
			}
		}
	}
	return q
}

// SearchTerms splits a keyword filter into free-text terms (no qualifiers); a
// task must match every one. Used by the task list's in-project keyword filter.
func SearchTerms(raw string) []string {
	var terms []string
	for _, tok := range tokenizeSearch(raw) {
		terms = appendCapped(terms, clipTerm(unquoteSearch(tok)))
	}
	return terms
}

func addSearchTerm(q *SearchQuery, term string) {
	term = strings.TrimSpace(term)
	if term == "" {
		return
	}
	if strings.HasPrefix(term, "#") {
		if id, ok := parseSearchTaskID(term); ok {
			q.IDs = append(q.IDs, id)
			return
		}
	}
	q.Terms = appendCapped(q.Terms, clipTerm(term))
}

func appendCapped(list []string, v string) []string {
	if v == "" || len(list) >= maxSearchTerms {
		return list
	}
	for _, existing := range list {
		if strings.EqualFold(existing, v) {
			return list
		}
	}
	return append(list, v)
}

func clipTerm(v string) string {
	r := []rune(strings.TrimSpace(v))
	if len(r) > maxSearchTermLength {
		r = r[:maxSearchTermLength]
	}
	return string(r)
}

func completionWord(v string) (done bool, ok bool) {
	switch v {
	case "open", "incomplete", "todo", "active":
		return false, true
	case "done", "complete", "completed", "closed":
		return true, true
	}
	return false, false
}

// tokenizeSearch splits on whitespace outside double quotes.
func tokenizeSearch(raw string) []string {
	r := []rune(raw)
	if len(r) > maxSearchQueryLength {
		r = r[:maxSearchQueryLength]
	}
	var out []string
	var cur strings.Builder
	inQuote := false
	for _, c := range r {
		switch {
		case c == '"':
			inQuote = !inQuote
			cur.WriteRune(c)
		case unicode.IsSpace(c) && !inQuote:
			if cur.Len() > 0 {
				out = append(out, cur.String())
				cur.Reset()
			}
		default:
			cur.WriteRune(c)
		}
	}
	if cur.Len() > 0 {
		out = append(out, cur.String())
	}
	return out
}

var searchQualifiers = map[string]bool{
	"id": true, "project": true, "proj": true, "tag": true, "tags": true, "status": true, "state": true,
	"is": true, "claimed": true, "assignee": true, "assigned": true,
}

func splitQualifier(tok string) (key, value string, ok bool) {
	i := strings.IndexRune(tok, ':')
	if i <= 0 {
		return "", "", false
	}
	key = strings.ToLower(tok[:i])
	if !searchQualifiers[key] {
		return "", "", false
	}
	return key, tok[i+1:], true
}

func unquoteSearch(v string) string {
	return strings.TrimSpace(strings.ReplaceAll(v, `"`, ""))
}

// textTermsSQL is the condition that idCol matches every term: each anywhere in
// the title, description, or a tag name, or (for a number) as the task id. Used
// by list keyword filters and export.
// Returns "TRUE" for no terms.
func textTermsSQL(idCol string, args []interface{}, terms []string) (string, []interface{}) {
	return termsSQL(idCol, args, terms, nil, searchStandard)
}

// Typo tolerance (site search only): a term of minFuzzyTermRunes or more
// letters also matches titles holding a word one edit away
// (an inserted, missing, wrong or swapped letter), so "recieve" finds
// "receive" and "lgoin" finds "login". CloseSpellings finds those words in the
// search_words dictionary (see storage.MigrateSearchWords) with an indexed
// lookup, and each becomes an ordinary substring match.
const (
	minFuzzyTermRunes = 5
	// maxSpellingsPerTerm caps how many close words one term expands to.
	maxSpellingsPerTerm = 6
)

// fuzzyTerm reports whether a term is a single word long enough, and not a
// task id, to be matched by close spellings too.
func fuzzyTerm(term string) bool {
	if _, isID := parseSearchTaskID(term); isID {
		return false
	}
	n := 0
	for _, r := range strings.TrimSpace(term) {
		if !unicode.IsLetter(r) && !unicode.IsDigit(r) {
			return false
		}
		n++
	}
	return n >= minFuzzyTermRunes
}

// spellingVariants is the word itself plus each way of dropping one letter.
// Two words are one edit apart only if their variant sets overlap.
func spellingVariants(word string) []string {
	r := []rune(word)
	out := make([]string, 0, len(r)+1)
	out = append(out, word)
	seen := map[string]bool{word: true}
	for i := range r {
		v := string(r[:i]) + string(r[i+1:])
		if !seen[v] {
			seen[v] = true
			out = append(out, v)
		}
	}
	return out
}

// CloseSpellings maps each fuzzy-eligible term (lowercased) to dictionary
// words one edit away from it, closest-first by length difference; words that
// already contain the term are left out because the substring match finds them.
func CloseSpellings(ctx context.Context, q interface {
	Query(context.Context, string, ...any) (pgx.Rows, error)
}, terms []string) (map[string][]string, error) {
	out := map[string][]string{}
	for _, term := range terms {
		if !fuzzyTerm(term) {
			continue
		}
		lt := strings.ToLower(strings.TrimSpace(term))
		if _, done := out[lt]; done {
			continue
		}
		rows, err := q.Query(ctx, `SELECT DISTINCT word FROM search_word_variants WHERE variant = ANY($1) LIMIT 200`, spellingVariants(lt))
		if err != nil {
			return nil, err
		}
		var words []string
		for rows.Next() {
			var w string
			if err := rows.Scan(&w); err != nil {
				rows.Close()
				return nil, err
			}
			if !strings.Contains(w, lt) && editDistance([]rune(w), []rune(lt)) == 1 {
				words = append(words, w)
			}
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return nil, err
		}
		if len(words) == 0 {
			continue
		}
		slices.SortFunc(words, func(a, b string) int {
			da, db := abs(len(a)-len(lt)), abs(len(b)-len(lt))
			if da != db {
				return da - db
			}
			return strings.Compare(a, b)
		})
		if len(words) > maxSpellingsPerTerm {
			words = words[:maxSpellingsPerTerm]
		}
		out[lt] = words
	}
	return out, nil
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}

// spellingIDsSQL selects ids of tasks whose title contains one of the words at
// argument indexes idx. Separate ILIKEs ORed together so the title trigram
// index serves each one.
func spellingIDsSQL(idx []int) string {
	conds := make([]string, len(idx))
	for k, i := range idx {
		conds[k] = fmt.Sprintf("title ILIKE $%d", i)
	}
	return "(SELECT id FROM tasks WHERE " + strings.Join(conds, " OR ") + ")"
}

// termsSQL is textTermsSQL that also accepts, per term, the close spellings
// from CloseSpellings (keyed by the lowercased term).
func termsSQL(idCol string, args []interface{}, terms []string, spellings map[string][]string, fields searchFields) (string, []interface{}) {
	if len(terms) == 0 {
		return "TRUE", args
	}
	parts := make([]string, 0, len(terms))
	for _, term := range terms {
		args = append(args, searchPattern(term))
		patIdx := len(args)
		idIdx := 0
		if id, ok := parseSearchTaskID(term); ok {
			args = append(args, id)
			idIdx = len(args)
		}
		part := searchMatchSQL(idCol, patIdx, idIdx, fields)
		if words := spellings[strings.ToLower(strings.TrimSpace(term))]; len(words) > 0 {
			idx := make([]int, len(words))
			for k, w := range words {
				args = append(args, searchPattern(w))
				idx[k] = len(args)
			}
			part = fmt.Sprintf("(%s OR %s IN %s)", part, idCol, spellingIDsSQL(idx))
		}
		parts = append(parts, part)
	}
	return "(" + strings.Join(parts, " AND ") + ")", args
}

// SearchScope is what a site search may look at: the caller's current projects
// (never tasks they created in a project they have since left) and, outside a
// project scope, their own project-less tasks.
type SearchScope struct {
	ProjectIDs      []int
	IncludePersonal bool
}

// ResolveSearchScope picks the projects a search covers from the caller's
// accessible projects. projectID > 0 limits it to that one project (which must
// be accessible); project: qualifiers keep projects whose name contains any of
// them. Archived projects are left out unless asked for or named directly.
func ResolveSearchScope(projects []storage.ProjectWithAccess, q SearchQuery, projectID int) SearchScope {
	var scope SearchScope
	for _, p := range projects {
		if projectID > 0 {
			if p.ID == projectID {
				scope.ProjectIDs = append(scope.ProjectIDs, p.ID)
			}
			continue
		}
		if p.Archived && !q.Archived {
			continue
		}
		if len(q.Projects) > 0 && !containsAnyFold(p.Name, q.Projects) {
			continue
		}
		scope.ProjectIDs = append(scope.ProjectIDs, p.ID)
	}
	scope.IncludePersonal = projectID <= 0 && len(q.Projects) == 0
	return scope
}

// MatchSearchProjects returns accessible projects whose name matches every free
// term, for the project section of search results: names containing every term
// first, then names that match only with a typo allowed (see fuzzyWordMatch).
func MatchSearchProjects(projects []storage.ProjectWithAccess, q SearchQuery) []storage.ProjectWithAccess {
	if len(q.Terms) == 0 {
		return nil
	}
	out := []storage.ProjectWithAccess{}
	var close []storage.ProjectWithAccess
	for _, p := range projects {
		if p.Archived && !q.Archived {
			continue
		}
		if containsAllFold(p.Name, q.Terms) {
			out = append(out, p)
			if len(out) >= maxSearchProjects {
				return out
			}
		} else if fuzzyMatchesAll(p.Name, q.Terms) {
			close = append(close, p)
		}
	}
	for _, p := range close {
		if len(out) >= maxSearchProjects {
			break
		}
		out = append(out, p)
	}
	return out
}

// fuzzyMatchesAll reports whether every term is in name, or (when long enough)
// one typo from one of its words.
func fuzzyMatchesAll(name string, terms []string) bool {
	ln := strings.ToLower(name)
	words := strings.FieldsFunc(ln, func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) })
	for _, term := range terms {
		lt := strings.ToLower(term)
		if strings.Contains(ln, lt) {
			continue
		}
		if !fuzzyTerm(term) || !fuzzyWordMatch(words, lt) {
			return false
		}
	}
	return true
}

// fuzzyWordMatch: some word is one edit from term (the same rule task search
// uses). An edit is an inserted, missing, wrong or swapped letter.
func fuzzyWordMatch(words []string, term string) bool {
	t := []rune(term)
	for _, w := range words {
		if editDistance([]rune(w), t) <= 1 {
			return true
		}
	}
	return false
}

// editDistance is the optimal string alignment distance (Levenshtein plus
// adjacent swaps).
func editDistance(a, b []rune) int {
	prev2 := make([]int, len(b)+1)
	prev := make([]int, len(b)+1)
	cur := make([]int, len(b)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(a); i++ {
		cur[0] = i
		for j := 1; j <= len(b); j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			cur[j] = min(prev[j]+1, cur[j-1]+1, prev[j-1]+cost)
			if i > 1 && j > 1 && a[i-1] == b[j-2] && a[i-2] == b[j-1] {
				cur[j] = min(cur[j], prev2[j-2]+1)
			}
		}
		prev2, prev, cur = prev, cur, prev2
	}
	return prev[len(b)]
}

func containsAnyFold(s string, subs []string) bool {
	ls := strings.ToLower(s)
	for _, sub := range subs {
		if strings.Contains(ls, strings.ToLower(sub)) {
			return true
		}
	}
	return false
}

func containsAllFold(s string, subs []string) bool {
	ls := strings.ToLower(s)
	for _, sub := range subs {
		if !strings.Contains(ls, strings.ToLower(sub)) {
			return false
		}
	}
	return true
}

// searchWhereSQL builds a site search's WHERE clause, appending its arguments
// to args (only those it uses). ok is false when the scope covers nothing.
func searchWhereSQL(q SearchQuery, scope SearchScope, userID int, spellings map[string][]string, args []interface{}) (where string, out []interface{}, ok bool) {
	userIdx := 0
	userArg := func() string {
		if userIdx == 0 {
			args = append(args, userID)
			userIdx = len(args)
		}
		return fmt.Sprintf("$%d", userIdx)
	}
	var scopeParts []string
	if len(scope.ProjectIDs) > 0 {
		args = append(args, scope.ProjectIDs)
		scopeParts = append(scopeParts, fmt.Sprintf("t.project_id = ANY($%d)", len(args)))
	}
	if scope.IncludePersonal {
		scopeParts = append(scopeParts, "(t.project_id IS NULL AND t.user_id = "+userArg()+")")
	}
	if len(scopeParts) == 0 {
		return "", args, false
	}
	where = "WHERE t.deleted_at IS NULL AND (" + strings.Join(scopeParts, " OR ") + ")"

	var text string
	fields := searchEverything
	if q.TitlesOnly {
		fields = searchTitles
	}
	text, args = termsSQL("t.id", args, q.Terms, spellings, fields)
	if text != "TRUE" {
		where += " AND " + text
	}
	if len(q.IDs) > 0 {
		args = append(args, q.IDs)
		where += fmt.Sprintf(" AND t.id = ANY($%d)", len(args))
	}
	for _, tag := range q.Tags {
		args = append(args, searchPattern(tag))
		where += fmt.Sprintf(" AND t.id IN (SELECT tt.task_id FROM task_tags tt JOIN tags tg ON tg.id = tt.tag_id WHERE tg.name ILIKE $%d)", len(args))
	}
	if len(q.Statuses) > 0 {
		patterns := make([]string, 0, len(q.Statuses))
		for _, s := range q.Statuses {
			patterns = append(patterns, searchPattern(s))
		}
		args = append(args, patterns)
		where += fmt.Sprintf(" AND EXISTS (SELECT 1 FROM project_statuses qs WHERE qs.id = t.status_id AND qs.name ILIKE ANY($%d))", len(args))
	}
	if q.Completed != nil {
		if *q.Completed {
			where += " AND t.completed = true"
		} else {
			where += " AND (t.completed IS NULL OR t.completed = false)"
		}
	}
	switch q.Claimed {
	case "me":
		where += " AND t.claimed_by = " + userArg()
	case "none":
		where += " AND t.claimed_by IS NULL"
	}
	if !q.Archived {
		where += " AND NOT " + storage.ArchivedTaskExistsSQL("t.id")
	}
	return where, args, true
}

// searchOrderSQL ranks exact id hits first, then tasks whose title holds every
// term as typed (so typo matches come after), then open work before done, then
// the most recently changed.
func searchOrderSQL(q SearchQuery, args []interface{}) (string, []interface{}) {
	var keys []string
	ids := append([]int(nil), q.IDs...)
	for _, term := range q.Terms {
		if id, ok := parseSearchTaskID(term); ok {
			ids = append(ids, id)
		}
	}
	if len(ids) > 0 {
		args = append(args, ids)
		keys = append(keys, fmt.Sprintf("(t.id = ANY($%d)) DESC", len(args)))
	}
	if len(q.Terms) > 0 {
		parts := make([]string, 0, len(q.Terms))
		for _, term := range q.Terms {
			args = append(args, searchPattern(term))
			parts = append(parts, fmt.Sprintf("t.title ILIKE $%d", len(args)))
		}
		keys = append(keys, "("+strings.Join(parts, " AND ")+") DESC")
	}
	keys = append(keys, "COALESCE(t.completed, false) ASC", "t.date_modified DESC NULLS LAST", "t.id DESC")
	return " ORDER BY " + strings.Join(keys, ", "), args
}

// searchSelectSQL is taskSelectSQL with the timezone as $1.
func searchSelectSQL() string {
	return strings.ReplaceAll(taskSelectSQL(), "$2", "$1")
}

// SiteSearchTasks runs a search across scope, most relevant first. Results are
// flat: subtasks come back as their own rows, with their parent's title.
func SiteSearchTasks(userID int, timezone string, q SearchQuery, scope SearchScope, page, pageSize int) ([]Task, int, error) {
	if q.Empty() {
		return []Task{}, 0, nil
	}
	if _, _, ok := searchWhereSQL(q, scope, userID, nil, nil); !ok {
		return []Task{}, 0, nil
	}
	if pageSize <= 0 || pageSize > MaxSearchPageSize {
		pageSize = MaxSearchPageSize
	}
	if page < 1 {
		page = 1
	}

	pool, err := storage.OpenDatabase()
	if err != nil {
		return nil, 0, err
	}
	defer storage.CloseDatabase(pool)
	ctx := context.Background()

	spellings, err := CloseSpellings(ctx, pool, q.Terms)
	if err != nil {
		return nil, 0, err
	}

	countWhere, countArgs, _ := searchWhereSQL(q, scope, userID, spellings, nil)
	var total int
	if err := pool.QueryRow(ctx, "SELECT COUNT(*) FROM tasks t "+countWhere, countArgs...).Scan(&total); err != nil {
		return nil, 0, err
	}
	if total == 0 {
		return []Task{}, 0, nil
	}

	where, args, _ := searchWhereSQL(q, scope, userID, spellings, []interface{}{timezone})
	order, args := searchOrderSQL(q, args)
	args = append(args, pageSize, (page-1)*pageSize)
	limit := fmt.Sprintf(" LIMIT $%d OFFSET $%d", len(args)-1, len(args))
	rows, err := pool.Query(ctx, searchSelectSQL()+where+order+limit, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	list := make([]Task, 0)
	for rows.Next() {
		task, err := scanFavoriteTaskRow(rows)
		if err != nil {
			return nil, 0, err
		}
		list = append(list, task)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}
	if err := attachTagsToTasks(list); err != nil {
		return nil, 0, err
	}
	if err := attachParentTitles(list); err != nil {
		return nil, 0, err
	}
	return list, total, nil
}
