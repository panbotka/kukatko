package ppimport

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/panbotka/kukatko/internal/organize"
	"github.com/panbotka/kukatko/internal/photoprism"
	"github.com/panbotka/kukatko/internal/photos"
)

// mergedPageSize is how many file rows one listed photo occupies in these tests.
// The harness pages two rows at a time, so every page comes back HALF the
// requested count — short, but nowhere near the end of the library. A fake that
// served full pages would pass against the old short-page termination and prove
// nothing.
const mergedPageSize = 2

// pagedPhotos registers count photos on the client and serves them merged, so the
// listing behaves like PhotoPrism's: file rows are paged, photo entries are
// collapsed, and no page ever fills the requested count.
func pagedPhotos(client *fakeClient, count int) []photoprism.Photo {
	client.filesPerPhoto = mergedPageSize
	base := time.Date(2023, 6, 1, 10, 0, 0, 0, time.UTC)
	out := make([]photoprism.Photo, 0, count)
	for i := range count {
		uid := fmt.Sprintf("pp%d", i+1)
		out = append(out, client.makePhoto(uid, base.Add(time.Duration(i)*time.Minute), "Photo "+uid))
	}
	return out
}

// seedImported registers a photo in the fake catalogue as already imported from
// ppUID, so a membership test starts from a library the importer has walked.
func (h *harness) seedImported(t *testing.T, ppUID string) string {
	t.Helper()
	uid := ppUID
	created, err := h.photos.Create(context.Background(), photos.Photo{
		Title:         ppUID,
		FileHash:      hashBytes([]byte("bytes-" + ppUID)),
		PhotoprismUID: &uid,
	})
	if err != nil {
		t.Fatalf("seeding %s: %v", ppUID, err)
	}
	return created.UID
}

// TestImportPhotos_mergedShortPagesReadWholeLibrary pins that the full photo
// import keeps paging past a short page. Against a merged listing every page is
// shorter than the requested count, so terminating on a short page imported the
// first page of the library and reported the run done.
func TestImportPhotos_mergedShortPagesReadWholeLibrary(t *testing.T) {
	t.Parallel()
	client := &fakeClient{}
	client.photos = pagedPhotos(client, 5)

	h := newHarness(client)
	result, err := h.svc.Import(context.Background())
	if err != nil {
		t.Fatalf("Import: %v", err)
	}

	if result.Counts.Imported != len(client.photos) {
		t.Errorf("imported = %d, want %d", result.Counts.Imported, len(client.photos))
	}
	for i := range client.photos {
		if _, ok := h.photos.byPPUID[client.photos[i].UID]; !ok {
			t.Errorf("photo %s not imported", client.photos[i].UID)
		}
	}
}

// TestImportPhotos_emptyPageEndsTheWalk pins the loop still terminates: an empty
// page ends it, and the page after the last photo must be the last request.
func TestImportPhotos_emptyPageEndsTheWalk(t *testing.T) {
	t.Parallel()
	client := &fakeClient{}
	client.photos = pagedPhotos(client, 3)

	h := newHarness(client)
	if _, err := h.svc.Import(context.Background()); err != nil {
		t.Fatalf("Import: %v", err)
	}

	// Every photo is listed at least once, and the overlap the offset arithmetic
	// re-serves is deduplicated by uid into a single catalogue row.
	if got := len(h.photos.byPPUID); got != len(client.photos) {
		t.Errorf("catalogued photos = %d, want %d", got, len(client.photos))
	}
}

// TestAttachAlbumMembers_mergedShortPages pins that album membership pages the
// whole album. A short merged page used to end the walk, so an album larger than
// one page silently lost every member past it.
func TestAttachAlbumMembers_mergedShortPages(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	client := &fakeClient{}
	members := pagedPhotos(client, 5)
	client.albumPhotos = map[string][]photoprism.Photo{"ppal1": members}

	h := newHarness(client)
	for i := range members {
		h.seedImported(t, members[i].UID)
	}
	album, err := h.albums.CreateAlbum(ctx, organize.Album{Title: "Holiday"})
	if err != nil {
		t.Fatalf("CreateAlbum: %v", err)
	}

	if err := h.svc.attachAlbumMembers(ctx, "ppal1", album.UID); err != nil {
		t.Fatalf("attachAlbumMembers: %v", err)
	}
	if got := len(h.albums.members[album.UID]); got != len(members) {
		t.Errorf("album members = %d, want %d", got, len(members))
	}
}

// TestAttachLabelMembers_mergedShortPages pins that label membership pages every
// tagged photo, not just the first short page of them.
func TestAttachLabelMembers_mergedShortPages(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	client := &fakeClient{}
	tagged := pagedPhotos(client, 5)
	client.queryPhotos = map[string][]photoprism.Photo{`label:"beach"`: tagged}

	h := newHarness(client)
	for i := range tagged {
		h.seedImported(t, tagged[i].UID)
	}
	label, err := h.labels.CreateLabel(ctx, organize.Label{Name: "Beach"})
	if err != nil {
		t.Fatalf("CreateLabel: %v", err)
	}

	if err := h.svc.attachLabelMembers(ctx, "beach", "Beach", label.UID); err != nil {
		t.Fatalf("attachLabelMembers: %v", err)
	}
	if got := len(h.labels.attached[label.UID]); got != len(tagged) {
		t.Errorf("labelled photos = %d, want %d", got, len(tagged))
	}
}

// TestImportPhotos_sourceIgnoringOffsetFailsInsteadOfHanging pins the guard on the
// walk's termination. The loop ends on an EMPTY page, so a source that serves the
// same window whatever offset it is asked for never ends it: the import would spin
// re-importing page one until something killed it — which is exactly how it
// presented, as a 30-minute test timeout rather than a failure. It must stop and
// say why.
func TestImportPhotos_sourceIgnoringOffsetFailsInsteadOfHanging(t *testing.T) {
	t.Parallel()
	client := &fakeClient{}
	client.photos = pagedPhotos(client, 5)
	client.ignoreOffset = true

	h := newHarness(client)
	done := make(chan error, 1)
	go func() { _, err := h.svc.Import(context.Background()); done <- err }()

	select {
	case err := <-done:
		if err == nil {
			t.Fatal("Import succeeded against a source that never advances, want an error")
		}
	case <-time.After(30 * time.Second):
		t.Fatal("Import did not return: the walk is spinning on a source that ignores the offset")
	}
}
