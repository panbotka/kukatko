//go:build integration

package photos_test

import (
	"testing"

	"github.com/panbotka/kukatko/internal/photos"
)

// These tests run only under `make test-integration` against the database named
// by KUKATKO_TEST_DATABASE_URL. They exercise the generated fts column and the
// Search repository method end to end against real PostgreSQL full-text search.

// textPhoto builds a Photo with a distinct file hash and the given searchable
// text fields, leaving the rest of the metadata at zero values.
func textPhoto(hash, fileName, title, description, notes string) photos.Photo {
	return photos.Photo{
		FileHash:    hash,
		FilePath:    "2023/06/" + hash + ".jpg",
		FileName:    fileName,
		FileMime:    "image/jpeg",
		Title:       title,
		Description: description,
		Notes:       notes,
	}
}

// searchUIDs runs Search and returns the resulting photo UIDs in rank order.
func searchUIDs(t *testing.T, store *photos.Store, params photos.ListParams) []string {
	t.Helper()
	got, err := store.Search(t.Context(), params)
	if err != nil {
		t.Fatalf("Search(%+v): %v", params, err)
	}
	out := make([]string, len(got))
	for i, p := range got {
		out[i] = p.UID
	}
	return out
}

// TestSearch_diacriticsInsensitive verifies an unaccented query matches an
// accented title ("tomas" finds "Tomáš") and vice versa.
func TestSearch_diacriticsInsensitive(t *testing.T) {
	store, _ := newStore(t)
	ctx := t.Context()

	tomas, err := store.Create(ctx, textPhoto("d-1", "a.jpg", "Tomáš na hřišti", "", ""))
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, err := store.Create(ctx, textPhoto("d-2", "b.jpg", "Praha", "", "")); err != nil {
		t.Fatalf("create: %v", err)
	}

	got := searchUIDs(t, store, photos.ListParams{FullText: "tomas"})
	if len(got) != 1 || got[0] != tomas.UID {
		t.Fatalf("Search(tomas) = %v, want [%s]", got, tomas.UID)
	}

	// The reverse also holds: an accented query finds the same row.
	if got := searchUIDs(t, store, photos.ListParams{FullText: "Tomáš"}); len(got) != 1 || got[0] != tomas.UID {
		t.Fatalf("Search(Tomáš) = %v, want [%s]", got, tomas.UID)
	}
}

// TestSearch_fieldWeighting verifies a title hit ranks above a notes hit for the
// same term, exercising the A>B>C>D setweight on the generated column.
func TestSearch_fieldWeighting(t *testing.T) {
	store, _ := newStore(t)
	ctx := t.Context()

	titleHit, err := store.Create(ctx, textPhoto("w-1", "a.jpg", "Sunset", "", "ordinary evening"))
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	notesHit, err := store.Create(ctx, textPhoto("w-2", "b.jpg", "Evening", "", "a lovely sunset over the hills"))
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	got := searchUIDs(t, store, photos.ListParams{FullText: "sunset"})
	if len(got) != 2 {
		t.Fatalf("Search(sunset) = %v, want 2 results", got)
	}
	if got[0] != titleHit.UID || got[1] != notesHit.UID {
		t.Fatalf("Search(sunset) order = %v, want title hit %s before notes hit %s",
			got, titleHit.UID, notesHit.UID)
	}
}

// TestSearch_fileNameToken verifies the normalised file_name is searchable: a
// token split out of "IMG_2024.heic" matches.
func TestSearch_fileNameToken(t *testing.T) {
	store, _ := newStore(t)
	ctx := t.Context()

	photo, err := store.Create(ctx, textPhoto("f-1", "IMG_2024.heic", "", "", ""))
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, err := store.Create(ctx, textPhoto("f-2", "DSC_0001.jpg", "", "", "")); err != nil {
		t.Fatalf("create: %v", err)
	}

	got := searchUIDs(t, store, photos.ListParams{FullText: "2024"})
	if len(got) != 1 || got[0] != photo.UID {
		t.Fatalf("Search(2024) = %v, want [%s]", got, photo.UID)
	}
}

// TestSearch_combinedWithFilter verifies a full-text query honours the list
// filters: a private-only filter excludes a non-private match.
func TestSearch_combinedWithFilter(t *testing.T) {
	store, _ := newStore(t)
	ctx := t.Context()

	publicMatch, err := store.Create(ctx, textPhoto("c-1", "a.jpg", "beach holiday", "", ""))
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	privatePhoto := textPhoto("c-2", "b.jpg", "beach sunset", "", "")
	privatePhoto.Private = true
	privateMatch, err := store.Create(ctx, privatePhoto)
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	yes, no := true, false
	if got := searchUIDs(t, store, photos.ListParams{FullText: "beach", Private: &yes}); len(got) != 1 ||
		got[0] != privateMatch.UID {
		t.Fatalf("Search(beach, private) = %v, want [%s]", got, privateMatch.UID)
	}
	if got := searchUIDs(t, store, photos.ListParams{FullText: "beach", Private: &no}); len(got) != 1 ||
		got[0] != publicMatch.UID {
		t.Fatalf("Search(beach, public) = %v, want [%s]", got, publicMatch.UID)
	}
}

// TestSearch_pagination verifies limit/offset page through ranked results and
// that Count reports the full match total.
func TestSearch_pagination(t *testing.T) {
	store, _ := newStore(t)
	ctx := t.Context()

	// Three matches, ranked title > description > notes for the same term.
	if _, err := store.Create(ctx, textPhoto("p-1", "a.jpg", "garden", "", "")); err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, err := store.Create(ctx, textPhoto("p-2", "b.jpg", "", "garden", "")); err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, err := store.Create(ctx, textPhoto("p-3", "c.jpg", "", "", "garden")); err != nil {
		t.Fatalf("create: %v", err)
	}

	params := photos.ListParams{FullText: "garden"}
	total, err := store.Count(ctx, params)
	if err != nil {
		t.Fatalf("Count: %v", err)
	}
	if total != 3 {
		t.Fatalf("Count(garden) = %d, want 3", total)
	}

	params.Limit, params.Offset = 2, 0
	first := searchUIDs(t, store, params)
	if len(first) != 2 {
		t.Fatalf("page 1 = %v, want 2 results", first)
	}
	params.Offset = 2
	second := searchUIDs(t, store, params)
	if len(second) != 1 {
		t.Fatalf("page 2 = %v, want 1 result", second)
	}
	// Pages must not overlap.
	for _, uid := range second {
		for _, prev := range first {
			if uid == prev {
				t.Fatalf("page 2 %v overlaps page 1 %v", second, first)
			}
		}
	}
}

// TestSearch_updatedTitleChangesResults verifies the generated fts column is
// kept current by a metadata update: a term added to the title becomes matchable
// and one removed stops matching.
func TestSearch_updatedTitleChangesResults(t *testing.T) {
	store, _ := newStore(t)
	ctx := t.Context()

	photo, err := store.Create(ctx, textPhoto("u-1", "a.jpg", "mountains", "", ""))
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	if got := searchUIDs(t, store, photos.ListParams{FullText: "mountains"}); len(got) != 1 {
		t.Fatalf("Search(mountains) before update = %v, want 1 result", got)
	}
	if got := searchUIDs(t, store, photos.ListParams{FullText: "rivers"}); len(got) != 0 {
		t.Fatalf("Search(rivers) before update = %v, want 0 results", got)
	}

	if _, err := store.UpdateMetadata(ctx, photo.UID, photos.MetadataUpdate{Title: "rivers"}); err != nil {
		t.Fatalf("UpdateMetadata: %v", err)
	}

	if got := searchUIDs(t, store, photos.ListParams{FullText: "rivers"}); len(got) != 1 || got[0] != photo.UID {
		t.Fatalf("Search(rivers) after update = %v, want [%s]", got, photo.UID)
	}
	if got := searchUIDs(t, store, photos.ListParams{FullText: "mountains"}); len(got) != 0 {
		t.Fatalf("Search(mountains) after update = %v, want 0 results", got)
	}
}

// TestSearch_emptyQuery verifies an empty full-text query is rejected rather
// than ranking every photo.
func TestSearch_emptyQuery(t *testing.T) {
	store, _ := newStore(t)
	if _, err := store.Search(t.Context(), photos.ListParams{}); err == nil {
		t.Fatal("Search with empty query returned nil error, want ErrEmptySearch")
	}
}
