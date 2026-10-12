package photos

import (
	"context"
	"errors"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/panbotka/kukatko/internal/query"
)

// testProjectUID is the project the pure query-builder tests compile for. Any
// non-empty value will do: the builders bind it, they never look it up.
const testProjectUID = "pr_test"

// projectPredicate is the clause every compiled listing must open with.
const projectPredicate = "photos.project_id = $1"

// TestRequireProject verifies the guard every scoped method starts with: an
// empty project is ErrNoProject, anything else passes.
func TestRequireProject(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		project string
		want    error
	}{
		{name: "empty project is a programming error", project: "", want: ErrNoProject},
		{name: "a project passes", project: testProjectUID, want: nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := requireProject(tt.project); !errors.Is(got, tt.want) {
				t.Errorf("requireProject(%q) = %v, want %v", tt.project, got, tt.want)
			}
		})
	}
}

// TestProjectClause verifies the predicate is qualified, so it stays
// unambiguous in a query joining another table that carries project_id.
func TestProjectClause(t *testing.T) {
	t.Parallel()
	if got := projectClause("$3"); got != "photos.project_id = $3" {
		t.Errorf("projectClause($3) = %q", got)
	}
}

// TestBuilders_openWithTheProject verifies every builder of the shared listing
// path binds the project as $1 and puts its predicate first — including the
// unsatisfiable MatchNone scope, which short-circuits everything else.
func TestBuilders_openWithTheProject(t *testing.T) {
	t.Parallel()

	builders := map[string]func(string, ListParams) (string, []any){
		"list":     buildListQuery,
		"count":    buildCountQuery,
		"media":    buildMediaCountQuery,
		"timeline": buildTimelineQuery,
		"years":    buildYearsQuery,
		"uploader": buildUploadersQuery,
		"search": func(project string, p ListParams) (string, []any) {
			p.FullText = "x"
			return buildSearchQuery(project, p)
		},
	}
	for name, build := range builders {
		for _, params := range []ListParams{{}, {MatchNone: true}, {UploadedBy: "us_1", Search: "dům"}} {
			sql, args := build(testProjectUID, params)
			if !strings.Contains(sql, projectPredicate) {
				t.Errorf("%s(%+v) lacks %q: %s", name, params, projectPredicate, sql)
			}
			if len(args) == 0 || args[0] != testProjectUID {
				t.Errorf("%s(%+v) binds %v, want the project first", name, params, args)
			}
		}
	}
}

// TestWhereClauses_projectComesFirst verifies the project predicate is the
// first clause, ahead of the MatchNone constant and every other filter.
func TestWhereClauses_projectComesFirst(t *testing.T) {
	t.Parallel()
	for _, params := range []ListParams{{}, {MatchNone: true}, {AlbumUIDs: []string{"al_1"}}} {
		where, args := buildWhere(testProjectUID, params)
		if len(where) == 0 || where[0] != projectPredicate {
			t.Errorf("buildWhere(%+v) = %v, want %q first", params, where, projectPredicate)
		}
		if len(args) == 0 || args[0] != testProjectUID {
			t.Errorf("buildWhere(%+v) args = %v, want the project first", params, args)
		}
	}
}

// sampleValues are the values tried, in order, to give a filter key of each
// kind a value it parses. Several are tried because the numeric keys have
// bounds of their own (a month is 1–12, a year is not 1).
var sampleValues = map[query.Kind][]string{
	query.KindText:     {"abc"},
	query.KindNumber:   {"1", "2000"},
	query.KindDate:     {"2020"},
	query.KindBool:     {"yes"},
	query.KindID:       {"ph_abc"},
	query.KindCount:    {"yes"},
	query.KindDuration: {"10s"},
}

// sampleFilter returns the parsed filter for key with a value of its kind, or
// false when none of the samples parses.
func sampleFilter(info query.KeyInfo) (query.Filter, bool) {
	candidates := sampleValues[info.Kind]
	if info.Kind == query.KindEnum && len(info.Values) > 0 {
		candidates = []string{info.Values[0]}
	}
	for _, value := range candidates {
		for _, f := range query.Parse(string(info.Key) + ":" + value).Filters {
			if f.Key == info.Key {
				return f, true
			}
		}
	}
	return query.Filter{}, false
}

// rootTablePattern finds a reference to a table carrying project_id, the
// tables a filter must not resolve a name against across projects.
var rootTablePattern = regexp.MustCompile(
	`\b(?:FROM|JOIN)\s+(photos|albums|labels|subjects|photo_tasks|photoprism_aliases|subject_families)\b`)

// TestQueryKeys_compileInsideTheProject is the query-language parity guard of
// the tenancy: it walks the parser's own registry (never a hand-typed list, so
// a key added tomorrow is covered tomorrow) and checks that every key compiles
// to a query bound to the project — and that a key whose condition reaches into
// another root table confines that lookup to the project as well.
func TestQueryKeys_compileInsideTheProject(t *testing.T) {
	t.Parallel()

	user := "us_1"
	for _, info := range query.Keys() {
		t.Run(string(info.Key), func(t *testing.T) {
			t.Parallel()
			f, ok := sampleFilter(info)
			if !ok {
				t.Fatalf("no sample value of kind %s parses for %q; add one to sampleValues", info.Kind, info.Key)
			}
			sql, args := buildCountQuery(testProjectUID, ListParams{RatedBy: &user, QueryFilters: []query.Filter{f}})
			if !strings.Contains(sql, projectPredicate) || len(args) == 0 || args[0] != testProjectUID {
				t.Errorf("%s: query not bound to the project: %s %v", info.Key, sql, args)
			}
			var bound []any
			bind := func(v any) string {
				bound = append(bound, v)
				return "$" + strconv.Itoa(len(bound)+1)
			}
			clause, ok := queryFilterClause(f, condEnv{bind: bind, project: "$1", ratedBy: &user, radiusKm: 5})
			if !ok {
				return // dist: and friends add no condition of their own
			}
			if rootTablePattern.MatchString(clause) && !strings.Contains(clause, "project_id = $1") {
				t.Errorf("%s: condition reaches a root table outside the project: %s", info.Key, clause)
			}
		})
	}
}

// TestScopedReads_refuseAnEmptyProject verifies every scoped read refuses an
// empty project before it touches the database — the nil pool would panic
// otherwise, so a passing test also proves no query was attempted.
func TestScopedReads_refuseAnEmptyProject(t *testing.T) {
	t.Parallel()

	s := &Store{}
	ctx := context.Background()
	reads := map[string]func() error{
		"List":   func() error { _, err := s.List(ctx, "", ListParams{}); return err },
		"Search": func() error { _, err := s.Search(ctx, "", ListParams{FullText: "x"}); return err },
		"FilterUIDs": func() error {
			_, err := s.FilterUIDs(ctx, "", []string{"ph_1"}, ListParams{})
			return err
		},
		"Count":             func() error { _, err := s.Count(ctx, "", ListParams{}); return err },
		"CountMedia":        func() error { _, err := s.CountMedia(ctx, "", ListParams{}); return err },
		"TimelineBuckets":   func() error { _, err := s.TimelineBuckets(ctx, "", ListParams{}); return err },
		"YearBuckets":       func() error { _, err := s.YearBuckets(ctx, "", ListParams{}); return err },
		"UploaderBuckets":   func() error { _, err := s.UploaderBuckets(ctx, "", ListParams{}); return err },
		"GetByUIDInProject": func() error { _, err := s.GetByUIDInProject(ctx, "", "ph_1"); return err },
		"GetByFileHashInProject": func() error {
			_, err := s.GetByFileHashInProject(ctx, "", "abc")
			return err
		},
		"GetByPhotoprismUID":      func() error { _, err := s.GetByPhotoprismUID(ctx, "", "pt1"); return err },
		"GetByPhotoprismFileHash": func() error { _, err := s.GetByPhotoprismFileHash(ctx, "", "abc"); return err },
		"GetByPhotosorterUID":     func() error { _, err := s.GetByPhotosorterUID(ctx, "", "ps1"); return err },
		"GetByPhotoprismAlias":    func() error { _, err := s.GetByPhotoprismAlias(ctx, "", "pt1"); return err },
		"ListByUIDsInProject": func() error {
			_, err := s.ListByUIDsInProject(ctx, "", []string{"ph_1"})
			return err
		},
		"ListStackMembers": func() error { _, err := s.ListStackMembers(ctx, "", "st_1"); return err },
		"StackCounts":      func() error { _, err := s.StackCounts(ctx, "", []string{"st_1"}); return err },
		"GetOCR":           func() error { _, err := s.GetOCR(ctx, "", "ph_1"); return err },
	}
	for name, read := range reads {
		if err := read(); !errors.Is(err, ErrNoProject) {
			t.Errorf("%s with no project = %v, want ErrNoProject", name, err)
		}
	}
}

// projectRefPattern matches a project predicate on $1 joined to the clause after
// it — the opening predicate of a listing as well as the ones the query language
// adds inside a sub-select.
var projectRefPattern = regexp.MustCompile(`\b\w+\.project_id = \$1 AND `)

// placeholderPattern matches one bound placeholder.
var placeholderPattern = regexp.MustCompile(`\$(\d+)`)

// stripProject undoes the project scope of a query compiled for testProjectUID:
// it drops every predicate on $1, drops the bound project and renumbers the
// remaining placeholders down by one. The filter tests compare against it, so
// they keep reading as the filter they test ($1 is the first filter value)
// while the project itself is asserted by the tests above, on the real output.
// A query that was not scoped is returned untouched, so its assertions fail.
func stripProject(sql string, args []any) (string, []any) {
	if len(args) == 0 || args[0] != testProjectUID || !strings.Contains(sql, projectPredicate) {
		return sql, args
	}
	sql = projectRefPattern.ReplaceAllString(sql, "")
	sql = strings.Replace(sql, " WHERE "+projectPredicate, "", 1)
	sql = placeholderPattern.ReplaceAllStringFunc(sql, func(p string) string {
		n, err := strconv.Atoi(p[1:])
		if err != nil {
			return p
		}
		return "$" + strconv.Itoa(n-1)
	})
	return sql, args[1:]
}

// unscopedListQuery is buildListQuery for testProjectUID with the scope stripped.
func unscopedListQuery(params ListParams) (string, []any) {
	return stripProject(buildListQuery(testProjectUID, params))
}

// unscopedCountQuery is buildCountQuery for testProjectUID with the scope stripped.
func unscopedCountQuery(params ListParams) (string, []any) {
	return stripProject(buildCountQuery(testProjectUID, params))
}

// unscopedSearchQuery is buildSearchQuery for testProjectUID with the scope
// stripped.
func unscopedSearchQuery(params ListParams) (string, []any) {
	return stripProject(buildSearchQuery(testProjectUID, params))
}

// unscopedTimelineQuery is buildTimelineQuery for testProjectUID with the scope
// stripped.
func unscopedTimelineQuery(params ListParams) (string, []any) {
	return stripProject(buildTimelineQuery(testProjectUID, params))
}

// TestStripProject verifies the helper the filter tests lean on: it removes the
// opening predicate and the ones inside sub-selects, renumbers, and leaves an
// unscoped query alone.
func TestStripProject(t *testing.T) {
	t.Parallel()

	sql, args := stripProject("SELECT 1 FROM photos WHERE photos.project_id = $1 AND x = $2 AND "+
		"EXISTS (SELECT 1 FROM albums a WHERE a.project_id = $1 AND a.uid = $3)", []any{testProjectUID, "x", "al"})
	want := "SELECT 1 FROM photos WHERE x = $1 AND EXISTS (SELECT 1 FROM albums a WHERE a.uid = $2)"
	if sql != want || len(args) != 2 || args[0] != "x" {
		t.Errorf("stripProject = %q %v, want %q [x al]", sql, args, want)
	}
	if sql, _ := stripProject("SELECT count(*) FROM photos WHERE photos.project_id = $1",
		[]any{testProjectUID}); sql != "SELECT count(*) FROM photos" {
		t.Errorf("a lone predicate leaves %q", sql)
	}
	if sql, args := stripProject("SELECT 1", []any{"a"}); sql != "SELECT 1" || len(args) != 1 {
		t.Errorf("an unscoped query was changed: %q %v", sql, args)
	}
}
