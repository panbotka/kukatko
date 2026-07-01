//go:build integration

package people_test

import (
	"context"
	"testing"

	"github.com/panbotka/kukatko/internal/people"
)

// TestSearchSubjects exercises accent- and case-insensitive matching over a
// subject name plus the limit cap.
func TestSearchSubjects(t *testing.T) {
	store, _, _, _ := newStores(t)
	ctx := context.Background()

	for _, name := range []string{"Tomáš Novák", "Tereza", "Tomík", "Anna"} {
		if _, err := store.CreateSubject(ctx, people.Subject{Name: name}); err != nil {
			t.Fatalf("CreateSubject %q: %v", name, err)
		}
	}

	t.Run("accent- and case-insensitive contains", func(t *testing.T) {
		got, err := store.SearchSubjects(ctx, "TOMA", 10)
		if err != nil {
			t.Fatalf("SearchSubjects: %v", err)
		}
		// "TOMA" matches "Tomáš Novák" (accent-folded); "Tomík" does not contain it.
		if len(got) != 1 || got[0].Name != "Tomáš Novák" {
			t.Fatalf("matches = %+v, want [Tomáš Novák]", subjectNames(got))
		}
	})

	t.Run("limit caps the result set", func(t *testing.T) {
		got, err := store.SearchSubjects(ctx, "tom", 1)
		if err != nil {
			t.Fatalf("SearchSubjects: %v", err)
		}
		// "tom" matches both "Tomáš Novák" and "Tomík"; the cap keeps one.
		if len(got) != 1 {
			t.Fatalf("matches = %d, want 1 (capped)", len(got))
		}
	})

	t.Run("no match yields empty", func(t *testing.T) {
		got, err := store.SearchSubjects(ctx, "zzz-nothing", 10)
		if err != nil {
			t.Fatalf("SearchSubjects: %v", err)
		}
		if len(got) != 0 {
			t.Fatalf("matches = %d, want 0", len(got))
		}
	})
}

// subjectNames extracts the names of subject search rows for readable failures.
func subjectNames(rows []people.Subject) []string {
	out := make([]string, 0, len(rows))
	for _, s := range rows {
		out = append(out, s.Name)
	}
	return out
}
