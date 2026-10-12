package comments

import (
	"reflect"
	"strings"
	"testing"
)

// TestParseMentions pins the token syntax and its exclusions: a token is
// found once per account in order of first mention, a bare @name or a
// malformed token is not a mention, and a token inside code — a fence, an
// indented block, a span — is not either.
func TestParseMentions(t *testing.T) {
	t.Parallel()

	anna := MentionRef{UserUID: "us-anna", Name: "Anna Nováková"}
	tomas := MentionRef{UserUID: "us-tomas", Name: "Tomáš"}
	tests := []struct {
		name string
		body string
		want []MentionRef
	}{
		{name: "empty", body: "", want: []MentionRef{}},
		{name: "no mention", body: "Kdo je ten kluk vlevo?", want: []MentionRef{}},
		{name: "one token", body: "Hele @[Anna Nováková](user:us-anna), víš to?", want: []MentionRef{anna}},
		{
			name: "two accounts in order",
			body: "@[Tomáš](user:us-tomas) a @[Anna Nováková](user:us-anna)",
			want: []MentionRef{tomas, anna},
		},
		{
			name: "same account twice is one mention",
			body: "@[Anna Nováková](user:us-anna) a znovu @[Anna N.](user:us-anna)",
			want: []MentionRef{anna},
		},
		{name: "bare @name is text", body: "@Anna víš to?", want: []MentionRef{}},
		{name: "link without the @ is a link", body: "[Anna](user:us-anna)", want: []MentionRef{}},
		{name: "wrong scheme", body: "@[Anna](mailto:anna@example.test)", want: []MentionRef{}},
		{name: "space before the link", body: "@ [Anna](user:us-anna)", want: []MentionRef{}},
		{name: "empty name", body: "@[](user:us-anna)", want: []MentionRef{}},
		{name: "uid with a slash", body: "@[Anna](user:us/anna)", want: []MentionRef{}},
		{
			name: "name is trimmed",
			body: "@[ Anna Nováková ](user:us-anna)",
			want: []MentionRef{anna},
		},
		{
			name: "inside a code span",
			body: "takhle se to píše: `@[Anna Nováková](user:us-anna)`",
			want: []MentionRef{},
		},
		{
			name: "inside a double-backtick span with a single backtick in it",
			body: "`` ` @[Anna Nováková](user:us-anna) `` ok",
			want: []MentionRef{},
		},
		{
			name: "after an unclosed backtick",
			body: "` @[Anna Nováková](user:us-anna)",
			want: []MentionRef{anna},
		},
		{
			name: "span closed, mention after it",
			body: "`kód` @[Anna Nováková](user:us-anna)",
			want: []MentionRef{anna},
		},
		{
			name: "inside a fenced block",
			body: "```\n@[Anna Nováková](user:us-anna)\n```\n@[Tomáš](user:us-tomas)",
			want: []MentionRef{tomas},
		},
		{
			name: "inside a tilde fence with an info string",
			body: "~~~md\n@[Anna Nováková](user:us-anna)\n~~~",
			want: []MentionRef{},
		},
		{
			name: "unclosed fence runs to the end",
			body: "```\n@[Anna Nováková](user:us-anna)",
			want: []MentionRef{},
		},
		{
			name: "a shorter closing fence does not close",
			body: "````\n```\n@[Anna Nováková](user:us-anna)\n````",
			want: []MentionRef{},
		},
		{
			name: "inside an indented block after a blank line",
			body: "text\n\n    @[Anna Nováková](user:us-anna)\n\n@[Tomáš](user:us-tomas)",
			want: []MentionRef{tomas},
		},
		{
			name: "indented block at the top",
			body: "\t@[Anna Nováková](user:us-anna)",
			want: []MentionRef{},
		},
		{
			name: "indentation inside a paragraph is not code",
			body: "text\n    @[Anna Nováková](user:us-anna)",
			want: []MentionRef{anna},
		},
		{
			name: "a line of backticks with more backticks is a span, not a fence",
			body: "``` x ``` @[Anna Nováková](user:us-anna)",
			want: []MentionRef{anna},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := ParseMentions(tt.body); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("ParseMentions(%q) = %+v, want %+v", tt.body, got, tt.want)
			}
		})
	}
}

// TestMentionToken checks the token round-trips through the parser and that a
// name with brackets or a line break cannot break out of it.
func TestMentionToken(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name, person, uid, want string
	}{
		{name: "plain", person: "Anna Nováková", uid: "us-anna", want: "@[Anna Nováková](user:us-anna)"},
		{name: "trimmed", person: "  Anna ", uid: "us-anna", want: "@[Anna](user:us-anna)"},
		{name: "brackets dropped", person: "An[na] N", uid: "us-anna", want: "@[Anna N](user:us-anna)"},
		{name: "line break", person: "Anna\nN", uid: "us-anna", want: "@[Anna N](user:us-anna)"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := MentionToken(tt.person, tt.uid)
			if got != tt.want {
				t.Fatalf("MentionToken(%q, %q) = %q, want %q", tt.person, tt.uid, got, tt.want)
			}
			if refs := ParseMentions("x " + got + " y"); len(refs) != 1 || refs[0].UserUID != tt.uid {
				t.Errorf("token %q does not parse back: %+v", got, refs)
			}
		})
	}
}

// TestExcerpt checks the notification's quote of a comment: tokens read as
// @name, whitespace collapses, and a long body is cut with an ellipsis.
func TestExcerpt(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		body  string
		limit int
		want  string
	}{
		{name: "token reads as a name", body: "Hele @[Anna Nováková](user:us-anna),\n víš  to?", want: "Hele @Anna Nováková, víš to?"},
		{name: "within the limit", body: "krátké", limit: 10, want: "krátké"},
		{name: "cut counts runes", body: strings.Repeat("ě", 20), limit: 10, want: strings.Repeat("ě", 9) + "…"},
		{name: "cut trims a trailing space", body: "jedna dvě tři", limit: 7, want: "jedna…"},
		{name: "zero limit is the default", body: strings.Repeat("a", MaxExcerptLen+5), want: strings.Repeat("a", MaxExcerptLen-1) + "…"},
		{name: "empty", body: "", want: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := Excerpt(tt.body, tt.limit); got != tt.want {
				t.Errorf("Excerpt(%q, %d) = %q, want %q", tt.body, tt.limit, got, tt.want)
			}
		})
	}
}
