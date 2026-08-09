//go:build integration

package organize_test

import (
	"context"
	"slices"
	"testing"

	"github.com/panbotka/kukatko/internal/organize"
)

// makeAlbum creates an album with the given title and returns it.
func makeAlbum(t *testing.T, store *organize.Store, title string) organize.Album {
	t.Helper()
	album, err := store.CreateAlbum(context.Background(), organize.Album{Title: title})
	if err != nil {
		t.Fatalf("creating album %s: %v", title, err)
	}
	return album
}

// The batch album-membership lookup behind the review mixer's "do not put two
// questions about photos from one album next to each other" rule. It answers a
// question the per-photo AlbumsForPhoto could answer too, but once for a whole
// pool of photos instead of once per photo.

func TestAlbumUIDsForPhotos_batchMembership(t *testing.T) {
	store, photoStore, _, _ := newStores(t)
	ctx := context.Background()

	wedding := makeAlbum(t, store, "Svatba")
	holiday := makeAlbum(t, store, "Dovolená")
	inBoth := makePhoto(t, photoStore, "hash-both")
	weddingOnly := makePhoto(t, photoStore, "hash-wedding")
	orphan := makePhoto(t, photoStore, "hash-orphan")
	addPhotos(t, store, wedding.UID, inBoth, weddingOnly)
	addPhotos(t, store, holiday.UID, inBoth)

	got, err := store.AlbumUIDsForPhotos(ctx, []string{inBoth, weddingOnly, orphan, "nosuchuid"})
	if err != nil {
		t.Fatalf("AlbumUIDsForPhotos: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("membership = %v, want only the two photos that are in an album", got)
	}
	both := got[inBoth]
	slices.Sort(both)
	want := []string{holiday.UID, wedding.UID}
	slices.Sort(want)
	if !slices.Equal(both, want) {
		t.Errorf("albums of the shared photo = %v, want %v", both, want)
	}
	if !slices.Equal(got[weddingOnly], []string{wedding.UID}) {
		t.Errorf("albums of the wedding-only photo = %v, want just %s",
			got[weddingOnly], wedding.UID)
	}
	// A photo in no album is absent rather than present with an empty slice, so a
	// caller reads "no albums" the same way for both.
	if _, ok := got[orphan]; ok {
		t.Errorf("a photo in no album appeared in the map: %v", got)
	}
}

func TestAlbumUIDsForPhotos_emptyInputTouchesNothing(t *testing.T) {
	store, _, _, _ := newStores(t)
	got, err := store.AlbumUIDsForPhotos(context.Background(), nil)
	if err != nil {
		t.Fatalf("AlbumUIDsForPhotos: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("membership = %v, want empty", got)
	}
}
