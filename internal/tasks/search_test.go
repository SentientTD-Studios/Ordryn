package tasks

import (
	"GoTodo/internal/storage"
	"reflect"
	"strconv"
	"strings"
	"testing"
)

func TestParseSearchQuery(t *testing.T) {
	q := ParseSearchQuery(`login bug #42 id:7 project:"web app" tag:ui status:review is:done claimed:@me "exact phrase" http://x.io`)
	if want := []string{"login", "bug", "exact phrase", "http://x.io"}; !reflect.DeepEqual(q.Terms, want) {
		t.Errorf("terms = %q, want %q", q.Terms, want)
	}
	if want := []int{42, 7}; !reflect.DeepEqual(q.IDs, want) {
		t.Errorf("ids = %v, want %v", q.IDs, want)
	}
	if want := []string{"web app"}; !reflect.DeepEqual(q.Projects, want) {
		t.Errorf("projects = %q", q.Projects)
	}
	if want := []string{"ui"}; !reflect.DeepEqual(q.Tags, want) {
		t.Errorf("tags = %q", q.Tags)
	}
	if want := []string{"review"}; !reflect.DeepEqual(q.Statuses, want) {
		t.Errorf("statuses = %q", q.Statuses)
	}
	if q.Completed == nil || !*q.Completed {
		t.Error("is:done should set Completed=true")
	}
	if q.Claimed != "me" {
		t.Errorf("claimed = %q", q.Claimed)
	}
}

func TestParseSearchQueryEdgeCases(t *testing.T) {
	if !ParseSearchQuery("   ").Empty() {
		t.Error("blank query should be empty")
	}
	if !ParseSearchQuery(`project: tag:""`).Empty() {
		t.Error("qualifiers without values should be ignored")
	}
	q := ParseSearchQuery("status:open Login LOGIN 123")
	if q.Completed == nil || *q.Completed {
		t.Error("status:open should set Completed=false")
	}
	// Duplicate terms collapse; a bare number stays a term (text or id).
	if want := []string{"Login", "123"}; !reflect.DeepEqual(q.Terms, want) {
		t.Errorf("terms = %q, want %q", q.Terms, want)
	}
	if q := ParseSearchQuery("is:archived"); !q.Archived || !q.Empty() {
		t.Error("is:archived alone widens the scope but asks for nothing")
	}
	long := strings.Repeat("x ", 50)
	if n := len(ParseSearchQuery(long + "a b c d e f g h i j k l m n").Terms); n > maxSearchTerms {
		t.Errorf("terms not capped: %d", n)
	}
}

func TestSearchTerms(t *testing.T) {
	got := SearchTerms(`  fix "the login"  #12 project:x `)
	want := []string{"fix", "the login", "#12", "project:x"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("SearchTerms = %q, want %q", got, want)
	}
	if SearchTerms("  ") != nil {
		t.Error("blank keyword should give no terms")
	}
}

func TestResolveSearchScope(t *testing.T) {
	projects := []storage.ProjectWithAccess{
		{ID: 1, Name: "Web App"},
		{ID: 2, Name: "Mobile"},
		{ID: 3, Name: "Old Web", Archived: true},
	}
	all := ResolveSearchScope(projects, ParseSearchQuery("x"), 0)
	if !reflect.DeepEqual(all.ProjectIDs, []int{1, 2}) || !all.IncludePersonal {
		t.Errorf("default scope = %+v", all)
	}
	arch := ResolveSearchScope(projects, ParseSearchQuery("x is:archived"), 0)
	if !reflect.DeepEqual(arch.ProjectIDs, []int{1, 2, 3}) {
		t.Errorf("archived scope = %+v", arch)
	}
	named := ResolveSearchScope(projects, ParseSearchQuery("x project:web"), 0)
	if !reflect.DeepEqual(named.ProjectIDs, []int{1}) || named.IncludePersonal {
		t.Errorf("project: scope = %+v", named)
	}
	one := ResolveSearchScope(projects, ParseSearchQuery("x"), 3)
	if !reflect.DeepEqual(one.ProjectIDs, []int{3}) || one.IncludePersonal {
		t.Errorf("project id scope (archived but named) = %+v", one)
	}
	none := ResolveSearchScope(projects, ParseSearchQuery("x"), 99)
	if len(none.ProjectIDs) != 0 || none.IncludePersonal {
		t.Errorf("inaccessible project must search nothing: %+v", none)
	}
}

func TestMatchSearchProjects(t *testing.T) {
	projects := []storage.ProjectWithAccess{
		{ID: 1, Name: "Web App"},
		{ID: 2, Name: "Web Archive", Archived: true},
		{ID: 3, Name: "Mobile app"},
	}
	got := MatchSearchProjects(projects, ParseSearchQuery("app web"))
	if len(got) != 1 || got[0].ID != 1 {
		t.Errorf("match = %+v", got)
	}
	if MatchSearchProjects(projects, ParseSearchQuery("#5")) != nil {
		t.Error("no free text, no project matches")
	}
}

func TestSearchWhereSQL(t *testing.T) {
	q := ParseSearchQuery("login 42 tag:bug status:review is:open claimed:me")
	where, args, ok := searchWhereSQL(q, SearchScope{ProjectIDs: []int{1, 2}, IncludePersonal: true}, 9, nil, []interface{}{"UTC"})
	if !ok {
		t.Fatal("scope with projects should be searchable")
	}
	// Every placeholder must have an argument and every argument a placeholder.
	for i := 2; i <= len(args); i++ {
		if !strings.Contains(where, "$"+strconv.Itoa(i)) {
			t.Errorf("argument $%d unused in %s", i, where)
		}
	}
	if strings.Contains(where, "$"+strconv.Itoa(len(args)+1)) {
		t.Errorf("placeholder past the arguments in %s", where)
	}
	for _, want := range []string{"t.deleted_at IS NULL", "t.project_id = ANY(", "t.user_id = $", "t.claimed_by = $", "completed IS NULL", "project_statuses", "task_tags"} {
		if !strings.Contains(where, want) {
			t.Errorf("where lacks %q: %s", want, where)
		}
	}
	// The user id is appended once and reused.
	n := 0
	for _, a := range args {
		if a == 9 {
			n++
		}
	}
	if n != 1 {
		t.Errorf("user id passed %d times", n)
	}
	if _, _, ok := searchWhereSQL(q, SearchScope{}, 9, nil, nil); ok {
		t.Error("empty scope must search nothing")
	}
}

func TestSearchWhereSQLNoUserArgWhenUnused(t *testing.T) {
	where, args, _ := searchWhereSQL(ParseSearchQuery("x"), SearchScope{ProjectIDs: []int{1}}, 9, nil, nil)
	for _, a := range args {
		if a == 9 {
			t.Errorf("user id passed but unused: %s", where)
		}
	}
}

func TestTextTermsSQLNumericTerm(t *testing.T) {
	sql, args := textTermsSQL("t.id", nil, []string{"a", "#12"})
	if len(args) != 3 || args[2] != 12 {
		t.Fatalf("args = %v", args)
	}
	if !strings.Contains(sql, "t.id = $3") || strings.Count(sql, " AND ") < 1 {
		t.Errorf("sql = %s", sql)
	}
}

func TestSearchPatternEscapes(t *testing.T) {
	if got := searchPattern(`50%_off\`); got != `%50\%\_off\\%` {
		t.Errorf("searchPattern = %q", got)
	}
}

func TestEditDistance(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"", "", 0}, {"abc", "abc", 0}, {"recieve", "receive", 1}, {"tset", "test", 1},
		{"kitten", "sitting", 3}, {"web", "", 3}, {"dashbord", "dashboard", 1},
	}
	for _, c := range cases {
		if got := editDistance([]rune(c.a), []rune(c.b)); got != c.want {
			t.Errorf("editDistance(%q, %q) = %d, want %d", c.a, c.b, got, c.want)
		}
	}
}

func TestMatchSearchProjectsFuzzy(t *testing.T) {
	projects := []storage.ProjectWithAccess{
		{ID: 1, Name: "Marketing Website"},
		{ID: 2, Name: "Infrastructure"},
		{ID: 3, Name: "Market research"},
	}
	ids := func(list []storage.ProjectWithAccess) []int {
		out := []int{}
		for _, p := range list {
			out = append(out, p.ID)
		}
		return out
	}
	if got := ids(MatchSearchProjects(projects, ParseSearchQuery("markting"))); len(got) != 1 || got[0] != 1 {
		t.Errorf("typo = %v", got)
	}
	if got := ids(MatchSearchProjects(projects, ParseSearchQuery("infrastructre"))); len(got) != 1 || got[0] != 2 {
		t.Errorf("long typo = %v", got)
	}
	// Exact matches come first, then close ones.
	if got := ids(MatchSearchProjects(projects, ParseSearchQuery("market"))); len(got) != 2 || got[0] != 1 || got[1] != 3 {
		t.Errorf("market = %v", got)
	}
	if got := ids(MatchSearchProjects(projects, ParseSearchQuery("mrkting"))); len(got) != 0 {
		t.Errorf("two edits is too far: %v", got)
	}
	if got := ids(MatchSearchProjects(projects, ParseSearchQuery("webiste"))); len(got) != 1 || got[0] != 1 {
		t.Errorf("swap = %v", got)
	}
}

func TestSearchWhereSQLSpellings(t *testing.T) {
	q := ParseSearchQuery("dashbord 42 ab")
	plain, plainArgs, _ := searchWhereSQL(q, SearchScope{ProjectIDs: []int{1}}, 9, nil, nil)
	fuzzy, args, _ := searchWhereSQL(q, SearchScope{ProjectIDs: []int{1}}, 9, map[string][]string{"dashbord": {"dashboard", "dashbore"}}, nil)
	if len(args) != len(plainArgs)+2 {
		t.Fatalf("args = %v, plain %v", args, plainArgs)
	}
	if strings.Count(fuzzy, "title ILIKE") != strings.Count(plain, "title ILIKE")+2 {
		t.Errorf("each spelling should add a title match: %s", fuzzy)
	}
	for i := 1; i <= len(args); i++ {
		if !strings.Contains(fuzzy, "$"+strconv.Itoa(i)) {
			t.Errorf("argument $%d unused in %s", i, fuzzy)
		}
	}
}

func TestFuzzyTerm(t *testing.T) {
	for term, want := range map[string]bool{"login": true, "lgoin": true, "test": false, "#12345": false, "12345": false, "web app": false, "e-mail": false, "Über1": true} {
		if got := fuzzyTerm(term); got != want {
			t.Errorf("fuzzyTerm(%q) = %v, want %v", term, got, want)
		}
	}
}

func TestSpellingVariantsOverlapWithinOneEdit(t *testing.T) {
	overlap := func(a, b string) bool {
		set := map[string]bool{}
		for _, v := range spellingVariants(a) {
			set[v] = true
		}
		for _, v := range spellingVariants(b) {
			if set[v] {
				return true
			}
		}
		return false
	}
	// Every kind of single edit is found by the variant lookup.
	for _, pair := range [][2]string{{"receive", "recieve"}, {"login", "lgoin"}, {"vendor", "vendr"}, {"dashboard", "dashbord"}, {"report", "reprot"}, {"invoice", "invoise"}, {"plan", "plans"}} {
		if !overlap(pair[0], pair[1]) {
			t.Errorf("%q and %q share no variant", pair[0], pair[1])
		}
	}
}
