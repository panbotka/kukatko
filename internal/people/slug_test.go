package people

import "testing"

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
		{name: "non-latin falls back", in: "日本語", want: fallbackSlug},
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
