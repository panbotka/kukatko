//go:build integration

package organize_test

import (
	"context"
	"testing"

	"github.com/panbotka/kukatko/internal/organize"
)

// mustCreateAlbum inserts an album with the given title and description, failing
// the test on error and returning the stored album.
func mustCreateAlbum(t *testing.T, store *organize.Store, title, description string) organize.Album {
	t.Helper()
	album, err := store.CreateAlbum(context.Background(), organize.Album{Title: title, Description: description})
	if err != nil {
		t.Fatalf("creating album %q: %v", title, err)
	}
	return album
}

// mustCreateLabel inserts a label with the given name, failing the test on error
// and returning the stored label.
func mustCreateLabel(t *testing.T, store *organize.Store, name string) organize.Label {
	t.Helper()
	label, err := store.CreateLabel(context.Background(), organize.Label{Name: name})
	if err != nil {
		t.Fatalf("creating label %q: %v", name, err)
	}
	return label
}

// TestSearchAlbums exercises accent- and case-insensitive matching over an
// album's title and description, plus the limit cap.
func TestSearchAlbums(t *testing.T) {
	store, photoStore, _, _ := newStores(t)
	ctx := context.Background()

	sea := mustCreateAlbum(t, store, "Dovolená u moře", "letní fotky")
	winter := mustCreateAlbum(t, store, "Vánoce 2024", "dovolená v prosinci")
	mustCreateAlbum(t, store, "Praha", "výlet")

	// The sea album gets one photo so the count travels with the search row.
	photoUID := makePhoto(t, photoStore, "sea-hash")
	if err := store.AddPhoto(ctx, sea.UID, photoUID); err != nil {
		t.Fatalf("adding photo to album: %v", err)
	}

	t.Run("accent- and case-insensitive over title and description", func(t *testing.T) {
		got, err := store.SearchAlbums(ctx, "DOVOLENA", 10)
		if err != nil {
			t.Fatalf("SearchAlbums: %v", err)
		}
		// "DOVOLENA" matches the title of sea and the description of winter.
		if len(got) != 2 {
			t.Fatalf("matches = %d (%+v), want 2", len(got), albumTitles(got))
		}
		byUID := map[string]organize.AlbumCount{}
		for _, a := range got {
			byUID[a.UID] = a
		}
		if _, ok := byUID[sea.UID]; !ok {
			t.Fatalf("sea album missing from %v", albumTitles(got))
		}
		if _, ok := byUID[winter.UID]; !ok {
			t.Fatalf("winter album missing from %v", albumTitles(got))
		}
		if byUID[sea.UID].PhotoCount != 1 {
			t.Fatalf("sea photo_count = %d, want 1", byUID[sea.UID].PhotoCount)
		}
	})

	t.Run("no match yields empty", func(t *testing.T) {
		got, err := store.SearchAlbums(ctx, "zzz-nothing", 10)
		if err != nil {
			t.Fatalf("SearchAlbums: %v", err)
		}
		if len(got) != 0 {
			t.Fatalf("matches = %d, want 0", len(got))
		}
	})

	t.Run("limit caps the result set", func(t *testing.T) {
		got, err := store.SearchAlbums(ctx, "dovolena", 1)
		if err != nil {
			t.Fatalf("SearchAlbums: %v", err)
		}
		if len(got) != 1 {
			t.Fatalf("matches = %d, want 1 (capped)", len(got))
		}
	})
}

// TestSearchLabels exercises accent- and case-insensitive matching over a label
// name plus the limit cap.
func TestSearchLabels(t *testing.T) {
	store, _, _, _ := newStores(t)
	ctx := context.Background()

	mustCreateLabel(t, store, "Léto")
	mustCreateLabel(t, store, "Letadlo")
	mustCreateLabel(t, store, "Zima")

	t.Run("accent-insensitive prefix contains", func(t *testing.T) {
		got, err := store.SearchLabels(ctx, "let", 10)
		if err != nil {
			t.Fatalf("SearchLabels: %v", err)
		}
		// "let" matches both "Léto" (accent-folded) and "Letadlo".
		if len(got) != 2 {
			t.Fatalf("matches = %d, want 2", len(got))
		}
	})

	t.Run("limit caps the result set", func(t *testing.T) {
		got, err := store.SearchLabels(ctx, "let", 1)
		if err != nil {
			t.Fatalf("SearchLabels: %v", err)
		}
		if len(got) != 1 {
			t.Fatalf("matches = %d, want 1 (capped)", len(got))
		}
	})
}

// albumTitles extracts the titles of album search rows for readable failures.
func albumTitles(rows []organize.AlbumCount) []string {
	out := make([]string, 0, len(rows))
	for _, a := range rows {
		out = append(out, a.Title)
	}
	return out
}
