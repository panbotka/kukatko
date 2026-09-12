package people

import (
	"strings"
	"testing"
)

// TestLikePattern verifies the "contains" wrapping and that LIKE metacharacters
// in the query are escaped so they match literally.
func TestLikePattern(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		in   string
		want string
	}{
		{name: "plain word is wrapped", in: "tomas", want: "%tomas%"},
		{name: "percent is escaped", in: "50%", want: `%50\%%`},
		{name: "underscore is escaped", in: "a_b", want: `%a\_b%`},
		{name: "backslash is escaped", in: `a\b`, want: `%a\\b%`},
		{name: "empty stays a match-all contains", in: "", want: "%%"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := likePattern(tt.in); got != tt.want {
				t.Errorf("likePattern(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

// TestClampSearchLimit verifies positive limits pass through and non-positive
// limits fall back to the default.
func TestClampSearchLimit(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		in   int
		want int
	}{
		{name: "positive passes through", in: 3, want: 3},
		{name: "zero uses default", in: 0, want: defaultSearchLimit},
		{name: "negative uses default", in: -5, want: defaultSearchLimit},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := clampSearchLimit(tt.in); got != tt.want {
				t.Errorf("clampSearchLimit(%d) = %d, want %d", tt.in, got, tt.want)
			}
		})
	}
}

// TestSearchSubjectsSQL_matchesNameAndNickname pins down the predicate the subject
// search is built from: both the name and the nickname are compared, each folded
// through immutable_unaccent on both sides, and both read the SAME bound pattern
// so a third bind of one value is never paid for.
func TestSearchSubjectsSQL_matchesNameAndNickname(t *testing.T) {
	t.Parallel()

	for _, want := range []string{
		"immutable_unaccent(name) ILIKE immutable_unaccent($1)",
		"immutable_unaccent(nickname) ILIKE immutable_unaccent($1)",
		" OR ",
	} {
		if !strings.Contains(searchSubjectsSQL, want) {
			t.Errorf("searchSubjectsSQL missing %q: %q", want, searchSubjectsSQL)
		}
	}
	// A second placeholder for the pattern would mean the value is bound twice;
	// $2 is the limit and must stay the last parameter.
	if strings.Contains(searchSubjectsSQL, "$3") {
		t.Errorf("searchSubjectsSQL binds a third parameter: %q", searchSubjectsSQL)
	}
	if !strings.Contains(searchSubjectsSQL, "LIMIT $2") {
		t.Errorf("searchSubjectsSQL does not end in the bound limit: %q", searchSubjectsSQL)
	}
	// The nickname must not reach the ordering: the list is alphabetical by the
	// name, which is what the people index and the global search both show.
	if !strings.Contains(searchSubjectsSQL, "ORDER BY name, uid") {
		t.Errorf("searchSubjectsSQL is not ordered by name then uid: %q", searchSubjectsSQL)
	}
}
