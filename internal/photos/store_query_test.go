package photos

import (
	"strings"
	"testing"

	"github.com/panbotka/kukatko/internal/query"
)

// TestLikePattern verifies how a text filter's value becomes an ILIKE pattern:
// an unescaped '*' is the wildcard, an escaped one a literal star, and the LIKE
// metacharacters are escaped so they can only ever match themselves.
func TestLikePattern(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		pattern string
		want    string
	}{
		{"plain value is a substring match", "cat", "%cat%"},
		{"wildcard anchors the pattern", "IMG_*", `IMG\_%`},
		{"wildcard in the middle", "a*b", "a%b"},
		{"escaped star matches literally", `foo\*bar`, "%foo*bar%"},
		{"mixed escaped and operator star", `foo\**`, "foo*%"},
		{"underscore is escaped", "a_b", `%a\_b%`},
		{"percent is escaped", "50%", `%50\%%`},
		{"backslash is escaped", `a\\b`, `%a\\b%`},
		{"trailing backslash matches literally", `a\`, `%a\\%`},
		{"empty value matches everything", "", "%%"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := likePattern(tt.pattern); got != tt.want {
				t.Errorf("likePattern(%q) = %q, want %q", tt.pattern, got, tt.want)
			}
		})
	}
}

// TestLikePattern_fromQueryLanguage checks the whole path from the typed query
// to the bound pattern, the property the two halves have to share: only a star
// the user left unescaped acts as a wildcard, so a literal star is searchable.
func TestLikePattern_fromQueryLanguage(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"unescaped star is the wildcard", "title:foo*bar", "foo%bar"},
		{"escaped star is a literal star", `title:foo\*bar`, "%foo*bar%"},
		{"quoted star is a literal star", `title:"foo*bar"`, "%foo*bar%"},
		{"underscore never wildcards", "title:a_b", `%a\_b%`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			filters := query.Parse(tt.input).Filters
			if len(filters) != 1 || len(filters[0].Values) != 1 {
				t.Fatalf("Parse(%q) did not yield a single filter value", tt.input)
			}
			if got := likePattern(filters[0].Values[0].TextPattern()); got != tt.want {
				t.Errorf("pattern for %q = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

// TestBoundsCond_fractionalSlack verifies that every fractional bound — an exact
// match, both ends of a range and an open end alike — is widened by
// floatMatchEpsilon, so a REAL column that stores f/1.8 as 1.79999995 still
// falls inside the comparison, while whole-number bounds stay exact.
func TestBoundsCond_fractionalSlack(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input string
		wantS []string
		wantA []float64
	}{
		{
			name:  "exact fractional match is widened both ways",
			input: "f:1.8",
			wantS: []string{"aperture >= $1", "aperture <= $2"},
			wantA: []float64{1.8 - floatMatchEpsilon, 1.8 + floatMatchEpsilon},
		},
		{
			name:  "fractional range widens each end outwards",
			input: "f:1.8-2.8",
			wantS: []string{"aperture >= $1", "aperture <= $2"},
			wantA: []float64{1.8 - floatMatchEpsilon, 2.8 + floatMatchEpsilon},
		},
		{
			name:  "open fractional lower bound is widened down",
			input: "f:1.8-",
			wantS: []string{"aperture >= $1"},
			wantA: []float64{1.8 - floatMatchEpsilon},
		},
		{
			name:  "open fractional upper bound is widened up",
			input: "mm:-10.5",
			wantS: []string{"focal_length <= $1"},
			wantA: []float64{10.5 + floatMatchEpsilon},
		},
		{
			name:  "whole-number bounds stay exact",
			input: "mm:24-70",
			wantS: []string{"focal_length >= $1", "focal_length <= $2"},
			wantA: []float64{24, 70},
		},
		{
			name:  "an integer rating must not reach its neighbours",
			input: "rating:3",
			wantS: []string{">= $2", "<= $3"},
			wantA: []float64{3, 3},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			user := "us_1"
			sql, args := buildCountQuery(ListParams{
				RatedBy:      &user,
				QueryFilters: query.Parse(tt.input).Filters,
			})
			for _, want := range tt.wantS {
				if !strings.Contains(sql, want) {
					t.Errorf("query missing %q: %q", want, sql)
				}
			}
			bounds := floatArgs(args)
			if len(bounds) != len(tt.wantA) {
				t.Fatalf("bound args = %v, want %d entries", bounds, len(tt.wantA))
			}
			for i, want := range tt.wantA {
				if !nearlyEqual(bounds[i], want) {
					t.Errorf("bound %d = %v, want %v", i, bounds[i], want)
				}
			}
		})
	}
}

// floatArgs returns the float64 bound values among the bound arguments, in
// order; the other arguments (a user UID, for instance) are not bounds.
func floatArgs(args []any) []float64 {
	out := make([]float64, 0, len(args))
	for _, a := range args {
		if f, ok := a.(float64); ok {
			out = append(out, f)
		}
	}
	return out
}

// nearlyEqual compares two bounds with a tolerance far below floatMatchEpsilon,
// so the test asserts the slack was applied without depending on exact binary
// float arithmetic.
func nearlyEqual(got, want float64) bool {
	diff := got - want
	return diff < 1e-9 && diff > -1e-9
}

// TestTextClauses_escapesLikeMetacharacters verifies the camera, lens and
// free-text substring filters bind their value escaped, so '_' and '%' match
// themselves — the semantics the '-term' negation path already had, and which a
// term and its negation must share.
func TestTextClauses_escapesLikeMetacharacters(t *testing.T) {
	t.Parallel()

	sql, args := buildCountQuery(ListParams{
		Camera:    "EOS_5D",
		Lens:      "50%",
		Search:    "a_b",
		SearchNot: []string{"a_b"},
	})
	if !strings.Contains(sql, "camera_make ILIKE $1") {
		t.Errorf("query missing the camera filter: %q", sql)
	}
	// textClauses binds camera, lens and search; the negation follows last.
	want := []any{`%EOS\_5D%`, `%50\%%`, `%a\_b%`, `%a\_b%`}
	if len(args) != len(want) {
		t.Fatalf("args = %v, want %d entries", args, len(want))
	}
	for i, w := range want {
		if args[i] != w {
			t.Errorf("arg %d = %v, want %v", i, args[i], w)
		}
	}
	// The positive term and its negation must bind the very same pattern.
	if args[2] != args[3] {
		t.Errorf("search %v and its negation %v bind different patterns", args[2], args[3])
	}
}

// TestUploaderCond compiles the query language's uploader: filter: a name (or a
// UID) becomes an accent-folded match against the users table, the reserved
// `none` becomes the no-uploader test with nothing bound, and negating either
// keeps the NULL-safe wrapper — so `uploader:!none` really is "somebody
// uploaded this".
func TestUploaderCond(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    string
		contains []string
		absent   []string
		args     []any
	}{
		{
			name:  "a name matches username or display name, accent-insensitively",
			input: "uploader:tomas",
			contains: []string{
				"EXISTS (SELECT 1 FROM users u WHERE u.uid = photos.uploaded_by",
				"immutable_unaccent(u.username) ILIKE immutable_unaccent($1)",
				"immutable_unaccent(u.display_name) ILIKE immutable_unaccent($1)",
				"u.uid = $2",
			},
			args: []any{"%tomas%", "tomas"},
		},
		{
			name:     "a wildcard anchors the pattern",
			input:    "uploader:tom*",
			contains: []string{"immutable_unaccent(u.username) ILIKE immutable_unaccent($1)"},
			args:     []any{"tom%", "tom*"},
		},
		{
			name:     "none is the no-uploader test and binds nothing",
			input:    "uploader:none",
			contains: []string{"photos.uploaded_by IS NULL"},
			absent:   []string{"FROM users u"},
			args:     nil,
		},
		{
			name:     "negated none keeps the NULL-safe wrapper",
			input:    "uploader:!none",
			contains: []string{"NOT COALESCE((photos.uploaded_by IS NULL), FALSE)"},
			args:     nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			sql, args := buildCountQuery(ListParams{QueryFilters: query.Parse(tt.input).Filters})
			for _, want := range tt.contains {
				if !strings.Contains(sql, want) {
					t.Errorf("query missing %q: %q", want, sql)
				}
			}
			for _, unwanted := range tt.absent {
				if strings.Contains(sql, unwanted) {
					t.Errorf("query should not contain %q: %q", unwanted, sql)
				}
			}
			if len(args) != len(tt.args) {
				t.Fatalf("args = %v, want %v", args, tt.args)
			}
			for i, want := range tt.args {
				if args[i] != want {
					t.Errorf("arg %d = %v, want %v", i, args[i], want)
				}
			}
		})
	}
}
