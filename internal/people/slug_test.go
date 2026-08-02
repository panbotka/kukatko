package people

import (
	"strings"
	"testing"
)

// TestSlugify checks diacritics stripping, lower-casing, separator collapsing and
// the empty-name fallback.
func TestSlugify(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		in   string
		want string
	}{
		{name: "simple", in: "Alice", want: "alice"},
		{name: "spaces collapse", in: "Anna  Nováková", want: "anna-novakova"},
		{name: "czech diacritics", in: "Děti u Řeky", want: "deti-u-reky"},
		{name: "punctuation to hyphen", in: "Rex (the dog)!", want: "rex-the-dog"},
		{name: "leading and trailing trimmed", in: "  --Bobík--  ", want: "bobik"},
		{name: "digits kept", in: "Tým 2024", want: "tym-2024"},
		{name: "empty falls back", in: "", want: fallbackSlug},
		{name: "only punctuation falls back", in: "!!! ???", want: fallbackSlug},
		{name: "non-latin gets a digest slug", in: "日本語", want: NameSlug("日本語")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := Slugify(tt.in); got != tt.want {
				t.Errorf("Slugify(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

// TestNameSlug_unusableNames checks the guard every find-or-create-by-name path
// relies on: a name that identifies nobody yields "", while a name that merely
// cannot be written in ASCII does not.
func TestNameSlug_unusableNames(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		in       string
		wantSlug string
	}{
		{name: "plain name", in: "Alice", wantSlug: "alice"},
		{name: "empty", in: "", wantSlug: ""},
		{name: "whitespace only", in: " \t\n ", wantSlug: ""},
		{name: "non-breaking space only", in: " ", wantSlug: ""},
		{name: "punctuation only", in: "!!! ???", wantSlug: ""},
		{name: "dashes only", in: "---", wantSlug: ""},
		{name: "digit is a name", in: "7", wantSlug: "7"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := NameSlug(tt.in); got != tt.wantSlug {
				t.Errorf("NameSlug(%q) = %q, want %q", tt.in, got, tt.wantSlug)
			}
		})
	}
}

// TestNameSlug_nonASCIINamesAreDistinct checks that two names no ASCII slug can
// represent do not share a key — the catch-all failure mode a constant fallback
// caused — and that the same name keeps the same key across calls.
func TestNameSlug_nonASCIINamesAreDistinct(t *testing.T) {
	t.Parallel()

	japanese, chinese := NameSlug("日本語"), NameSlug("中文")
	switch {
	case japanese == "" || chinese == "":
		t.Fatalf("NameSlug dropped a real name: 日本語 = %q, 中文 = %q", japanese, chinese)
	case japanese == chinese:
		t.Errorf("NameSlug collapsed two names onto %q", japanese)
	case japanese != NameSlug("日本語"):
		t.Errorf("NameSlug(%q) is not stable across calls", "日本語")
	case !strings.HasPrefix(japanese, fallbackSlug+"-"):
		t.Errorf("NameSlug(%q) = %q, want the %q- prefix", "日本語", japanese, fallbackSlug)
	}
}

// TestCandidateSlug checks that the first attempt is the base and later attempts
// append the expected numeric suffix.
func TestCandidateSlug(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		base    string
		attempt int
		want    string
	}{
		{name: "first attempt is base", base: "alice", attempt: 0, want: "alice"},
		{name: "second attempt suffix 2", base: "alice", attempt: 1, want: "alice-2"},
		{name: "tenth attempt suffix 11", base: "alice", attempt: 10, want: "alice-11"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := candidateSlug(tt.base, tt.attempt); got != tt.want {
				t.Errorf("candidateSlug(%q, %d) = %q, want %q", tt.base, tt.attempt, got, tt.want)
			}
		})
	}
}
