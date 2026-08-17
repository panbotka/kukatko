package personme_test

import (
	"testing"

	"github.com/panbotka/kukatko/internal/personme"
	"github.com/panbotka/kukatko/internal/query"
)

// personValues returns the text of every alternative of the query's person
// filters, in order, so a test can assert what the compiler will be handed.
func personValues(t *testing.T, q query.Query) []string {
	t.Helper()
	var out []string
	for _, f := range q.Filters {
		if f.Key != query.KeyPerson {
			continue
		}
		for _, v := range f.Values {
			out = append(out, v.Text)
		}
	}
	return out
}

// TestParse_leavesTheTokenAlone pins the contract that makes this package
// necessary: internal/query is a pure parser and has no notion of a caller, so
// `person:me` survives parsing as the literal text "me" — a plain name match
// until somebody who knows the caller rewrites it.
func TestParse_leavesTheTokenAlone(t *testing.T) {
	t.Parallel()

	q := query.Parse("person:me year:1998")

	got := personValues(t, q)
	if len(got) != 1 || got[0] != "me" {
		t.Fatalf("query.Parse(person:me) person values = %v, want [me]", got)
	}
	if len(q.Unknown) != 0 {
		t.Errorf("query.Parse(person:me) Unknown = %v, want none", q.Unknown)
	}
}

func TestResolve_rewritesTheToken(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		input        string
		linked       *string
		wantUsed     bool
		wantResolved bool
		wantValues   []string
	}{
		{
			name:         "linked caller gets their subject uid",
			input:        "person:me",
			linked:       new("sub123"),
			wantUsed:     true,
			wantResolved: true,
			wantValues:   []string{"sub123"},
		},
		{
			name:         "unlinked caller leaves the token unresolved",
			input:        "person:me",
			linked:       nil,
			wantUsed:     true,
			wantResolved: false,
			wantValues:   []string{"me"},
		},
		{
			name:         "the token composes with other filters",
			input:        "person:me year:1998 person:babicka",
			linked:       new("sub123"),
			wantUsed:     true,
			wantResolved: true,
			wantValues:   []string{"sub123", "babicka"},
		},
		{
			name:         "a negated alternative is rewritten too",
			input:        "person:!me",
			linked:       new("sub123"),
			wantUsed:     true,
			wantResolved: true,
			wantValues:   []string{"sub123"},
		},
		{
			name:         "one alternative of an OR is rewritten",
			input:        "person:me|babicka",
			linked:       new("sub123"),
			wantUsed:     true,
			wantResolved: true,
			wantValues:   []string{"sub123", "babicka"},
		},
		{
			name:         "a capitalised Me is the person, not the caller",
			input:        "person:Me",
			linked:       new("sub123"),
			wantUsed:     false,
			wantResolved: true,
			wantValues:   []string{"Me"},
		},
		{
			name:         "the token means nothing under another key",
			input:        "album:me",
			linked:       nil,
			wantUsed:     false,
			wantResolved: true,
			wantValues:   nil,
		},
		{
			name:         "an unlinked caller who never asked is unaffected",
			input:        "person:babicka",
			linked:       nil,
			wantUsed:     false,
			wantResolved: true,
			wantValues:   []string{"babicka"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			parsed := query.Parse(tt.input)

			used, resolved := personme.Resolve(parsed.Filters, tt.linked)

			if used != tt.wantUsed || resolved != tt.wantResolved {
				t.Errorf("Resolve(%q) = (used %v, resolved %v), want (%v, %v)",
					tt.input, used, resolved, tt.wantUsed, tt.wantResolved)
			}
			got := personValues(t, parsed)
			if len(got) != len(tt.wantValues) {
				t.Fatalf("Resolve(%q) person values = %v, want %v", tt.input, got, tt.wantValues)
			}
			for i, want := range tt.wantValues {
				if got[i] != want {
					t.Errorf("Resolve(%q) person value %d = %q, want %q", tt.input, i, got[i], want)
				}
			}
		})
	}
}

// TestResolve_rewritesThePattern guards the half of a text value the SQL
// compiler actually builds its LIKE pattern from: leaving Pattern on "me" would
// resolve the UID match and still match anybody called "me" by name.
func TestResolve_rewritesThePattern(t *testing.T) {
	t.Parallel()

	parsed := query.Parse("person:me")
	if _, resolved := personme.Resolve(parsed.Filters, new("sub123")); !resolved {
		t.Fatal("Resolve(person:me) with a linked caller did not resolve")
	}

	got := parsed.Filters[0].Values[0].TextPattern()
	if got != "sub123" {
		t.Errorf("TextPattern() after Resolve = %q, want %q", got, "sub123")
	}
}

// uploaderValues returns the text of every alternative of the query's uploader
// filters, in order, so a test can assert what the compiler will be handed.
func uploaderValues(t *testing.T, q query.Query) []string {
	t.Helper()
	var out []string
	for _, f := range q.Filters {
		if f.Key != query.KeyUploader {
			continue
		}
		for _, v := range f.Values {
			out = append(out, v.Text)
		}
	}
	return out
}

// TestResolveUploader_rewritesTheToken covers the other half of the caller's
// own word: `uploader:me` becomes the caller's account UID, every other
// spelling stays an ordinary name match, and an empty caller is left alone
// rather than rewritten into a pattern that would match every uploader.
func TestResolveUploader_rewritesTheToken(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		input      string
		callerUID  string
		wantValues []string
	}{
		{
			name:       "the caller gets their own account uid",
			input:      "uploader:me",
			callerUID:  "us123",
			wantValues: []string{"us123"},
		},
		{
			name:       "a negated alternative is rewritten too",
			input:      "uploader:!me",
			callerUID:  "us123",
			wantValues: []string{"us123"},
		},
		{
			name:       "one alternative of an OR is rewritten",
			input:      "uploader:me|anna",
			callerUID:  "us123",
			wantValues: []string{"us123", "anna"},
		},
		{
			name:       "the token composes with other filters",
			input:      "uploader:me year:1998 uploader:anna",
			callerUID:  "us123",
			wantValues: []string{"us123", "anna"},
		},
		{
			name:       "a capitalised Me is a name, not the caller",
			input:      "uploader:Me",
			callerUID:  "us123",
			wantValues: []string{"Me"},
		},
		{
			name:       "none is left to the store to compile",
			input:      "uploader:none",
			callerUID:  "us123",
			wantValues: []string{"none"},
		},
		{
			name:       "the token means nothing under another key",
			input:      "person:me",
			callerUID:  "us123",
			wantValues: nil,
		},
		{
			name:       "an unknown caller leaves the token alone",
			input:      "uploader:me",
			callerUID:  "",
			wantValues: []string{"me"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			parsed := query.Parse(tt.input)

			personme.ResolveUploader(parsed.Filters, tt.callerUID)

			got := uploaderValues(t, parsed)
			if len(got) != len(tt.wantValues) {
				t.Fatalf("ResolveUploader(%q) uploader values = %v, want %v", tt.input, got, tt.wantValues)
			}
			for i, want := range tt.wantValues {
				if got[i] != want {
					t.Errorf("ResolveUploader(%q) uploader value %d = %q, want %q", tt.input, i, got[i], want)
				}
			}
		})
	}
}

// TestResolveUploader_pattern pins the field the store actually compiles: the
// rewritten alternative's Pattern must carry the UID too, or the exact-match arm
// would be paired with a stale name pattern.
func TestResolveUploader_pattern(t *testing.T) {
	t.Parallel()

	parsed := query.Parse("uploader:me")
	personme.ResolveUploader(parsed.Filters, "us123")

	value := parsed.Filters[0].Values[0]
	if value.Text != "us123" || value.TextPattern() != "us123" {
		t.Errorf("value = (text %q, pattern %q), want both us123", value.Text, value.TextPattern())
	}
}
