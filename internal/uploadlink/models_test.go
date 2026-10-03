package uploadlink

import (
	"errors"
	"slices"
	"strings"
	"testing"
	"time"
)

// TestLink_StateAt covers active, the expiry boundary, and revoked winning over
// a future expiry.
func TestLink_StateAt(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 10, 3, 12, 0, 0, 0, time.UTC)
	revoked := now.Add(-time.Hour)
	tests := []struct {
		name string
		link Link
		want State
	}{
		{"active", Link{ExpiresAt: now.Add(time.Minute)}, StateActive},
		{"expires exactly now", Link{ExpiresAt: now}, StateExpired},
		{"expired", Link{ExpiresAt: now.Add(-time.Minute)}, StateExpired},
		{"revoked beats a future expiry", Link{ExpiresAt: now.Add(time.Hour), RevokedAt: &revoked}, StateRevoked},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := tt.link.StateAt(now); got != tt.want {
				t.Errorf("StateAt = %q, want %q", got, tt.want)
			}
		})
	}
}

// TestNewLink_Validate covers every refusal and the accepted shapes.
func TestNewLink_Validate(t *testing.T) {
	t.Parallel()

	many := make([]string, MaxTargets+1)
	for i := range many {
		many[i] = "al" + strings.Repeat("x", i+1)
	}
	tests := []struct {
		name string
		in   NewLink
		want error
	}{
		{"album only", NewLink{AlbumUIDs: []string{"al1"}}, nil},
		{"label only", NewLink{LabelUIDs: []string{"lb1"}}, nil},
		{"no targets", NewLink{Title: "x"}, ErrNoTargets},
		{"blank targets only", NewLink{AlbumUIDs: []string{""}, LabelUIDs: []string{""}}, ErrNoTargets},
		{"title too long", NewLink{Title: strings.Repeat("á", MaxTitleLen+1), AlbumUIDs: []string{"al1"}}, ErrTitleTooLong},
		{"title at the cap", NewLink{Title: strings.Repeat("á", MaxTitleLen), AlbumUIDs: []string{"al1"}}, nil},
		{"note too long", NewLink{Note: strings.Repeat("a", MaxNoteLen+1), AlbumUIDs: []string{"al1"}}, ErrNoteTooLong},
		{"too many albums", NewLink{AlbumUIDs: many}, ErrTooManyTargets},
		{"repeats do not count twice", NewLink{AlbumUIDs: append(slices.Clone(many[:MaxTargets]), many[0])}, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if err := tt.in.Validate(); !errors.Is(err, tt.want) {
				t.Errorf("Validate() = %v, want %v", err, tt.want)
			}
		})
	}
}

// TestUniqueNonEmpty verifies order is kept and blanks and repeats dropped.
func TestUniqueNonEmpty(t *testing.T) {
	t.Parallel()

	got := uniqueNonEmpty([]string{"b", "", "a", "b", "a", "c"})
	want := []string{"b", "a", "c"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("uniqueNonEmpty = %v, want %v", got, want)
	}
	if uniqueNonEmpty(nil) == nil {
		t.Error("uniqueNonEmpty(nil) = nil, want an empty slice")
	}
}

// TestWithDetails verifies a nil base is allocated and keys are merged.
func TestWithDetails(t *testing.T) {
	t.Parallel()

	got := withDetails(nil, map[string]any{"a": 1})
	if got["a"] != 1 {
		t.Errorf("withDetails(nil) = %v", got)
	}
	base := map[string]any{"a": 1}
	got = withDetails(base, map[string]any{"b": 2})
	if got["a"] != 1 || got["b"] != 2 {
		t.Errorf("withDetails merged = %v", got)
	}
}
