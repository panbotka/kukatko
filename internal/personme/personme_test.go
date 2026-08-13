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
