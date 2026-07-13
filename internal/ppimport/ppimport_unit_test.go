package ppimport

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"slices"
	"testing"
	"time"

	"github.com/panbotka/kukatko/internal/importer"
	"github.com/panbotka/kukatko/internal/people"
	"github.com/panbotka/kukatko/internal/photoprism"
	"github.com/panbotka/kukatko/internal/photos"
)

// discardLogger is a slog logger that drops every record, keeping test output
// quiet despite the importer's per-item warnings.
func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// harness bundles a Service over fully in-memory collaborators.
type harness struct {
	svc     *Service
	client  *fakeClient
	runs    *fakeRunStore
	photos  *fakePhotoStore
	albums  *fakeAlbumStore
	labels  *fakeLabelStore
	people  *fakePeopleStore
	enq     *fakeEnqueuer
	storage *fakeStorage
	prober  *fakeProber
}

// newHarness builds a Service wired to fakes, with a small page size so the
// paging loops are exercised. The fake prober returns empty metadata by default;
// video/live tests set h.prober.meta before importing.
func newHarness(client *fakeClient) *harness {
	runs := &fakeRunStore{}
	photoStore := newFakePhotoStore()
	albums := newFakeAlbumStore()
	labels := newFakeLabelStore()
	peopleStore := newFakePeopleStore()
	enq := &fakeEnqueuer{}
	store := newFakeStorage()
	prober := &fakeProber{}
	svc := New(Config{
		Client:      client,
		Runs:        runs,
		Photos:      photoStore,
		Storage:     store,
		Thumbnailer: &fakeThumbs{},
		Albums:      albums,
		Labels:      labels,
		People:      peopleStore,
		Enqueuer:    enq,
		Prober:      prober,
		PageSize:    2,
		Logger:      discardLogger(),
	})
	return &harness{
		svc: svc, client: client, runs: runs, photos: photoStore,
		albums: albums, labels: labels, people: peopleStore, enq: enq, storage: store, prober: prober,
	}
}

// makePhoto builds a PhotoPrism photo with a single primary file whose SHA1 hash
// and stored bytes are registered on the client, plus optional markers.
func (c *fakeClient) makePhoto(uid string, updated time.Time, title string, markers ...photoprism.Marker) photoprism.Photo {
	hash := "h-" + uid
	if c.files == nil {
		c.files = map[string][]byte{}
	}
	c.files[hash] = []byte("bytes-" + uid)
	return photoprism.Photo{
		UID:       uid,
		Type:      "image",
		Title:     title,
		TakenAt:   updated,
		UpdatedAt: updated,
		Width:     100,
		Height:    80,
		Files: []photoprism.File{
			{UID: "f-" + uid, Hash: hash, Primary: true, Mime: "image/jpeg", Markers: markers},
		},
	}
}

// TestImport_firstRun verifies a first import creates photos with external IDs,
// enqueues embed/face jobs, seeds people from markers, and maps album and label
// membership, recording the watermark.
func TestImport_firstRun(t *testing.T) {
	t.Parallel()
	t0 := time.Date(2023, 6, 1, 10, 0, 0, 0, time.UTC)
	t1 := t0.Add(time.Hour)
	client := &fakeClient{}
	p1 := client.makePhoto("pp1", t0, "Beach", photoprism.Marker{
		Type: "face", Name: "Alice", X: 0.1, Y: 0.1, W: 0.2, H: 0.2, Score: 90,
	})
	p2 := client.makePhoto("pp2", t1, "Sunset")
	client.photos = []photoprism.Photo{p1, p2}
	client.albums = []photoprism.Album{{UID: "ppal1", Title: "Holiday", Type: "album"}}
	client.albumPhotos = map[string][]photoprism.Photo{"ppal1": {p1, p2}}
	client.labels = []photoprism.Label{{UID: "pplb1", Name: "Beach", Slug: "beach"}}
	client.labelPhotos = map[string][]photoprism.Photo{`label:"beach"`: {p1}}

	h := newHarness(client)
	result, err := h.svc.Import(context.Background())
	if err != nil {
		t.Fatalf("Import: %v", err)
	}

	if result.Counts.Imported != 2 {
		t.Errorf("imported = %d, want 2", result.Counts.Imported)
	}
	if result.Watermark == nil || !result.Watermark.Equal(t1) {
		t.Errorf("watermark = %v, want %v", result.Watermark, t1)
	}
	if got := h.runs.last().Status; got != importer.StatusDone {
		t.Errorf("run status = %q, want done", got)
	}
	assertExternalIDs(t, h, "pp1")
	if !slices.Contains(h.enq.embeds, h.photos.byPPUID["pp1"]) {
		t.Error("image_embed not enqueued for pp1")
	}
	if !slices.Contains(h.enq.faces, h.photos.byPPUID["pp2"]) {
		t.Error("face_detect not enqueued for pp2")
	}
	if _, err := h.people.GetSubjectBySlug(context.Background(), people.Slugify("Alice")); err != nil {
		t.Errorf("subject Alice not created: %v", err)
	}
	if len(h.people.markers) != 1 {
		t.Errorf("markers = %d, want 1", len(h.people.markers))
	}
	assertAlbumLabel(t, h)
}

// assertExternalIDs checks that the photo imported from ppUID carries the
// PhotoPrism references.
func assertExternalIDs(t *testing.T, h *harness, ppUID string) {
	t.Helper()
	uid, ok := h.photos.byPPUID[ppUID]
	if !ok {
		t.Fatalf("photo for %s not imported", ppUID)
	}
	photo := h.photos.byUID[uid]
	if photo.PhotoprismUID == nil || *photo.PhotoprismUID != ppUID {
		t.Errorf("photoprism_uid = %v, want %s", photo.PhotoprismUID, ppUID)
	}
	if photo.PhotoprismFileHash == nil || *photo.PhotoprismFileHash != "h-"+ppUID {
		t.Errorf("photoprism_file_hash = %v, want h-%s", photo.PhotoprismFileHash, ppUID)
	}
}

// assertAlbumLabel checks album and label membership was mapped.
func assertAlbumLabel(t *testing.T, h *harness) {
	t.Helper()
	if len(h.albums.albums) != 1 {
		t.Fatalf("albums = %d, want 1", len(h.albums.albums))
	}
	if members := h.albums.members[h.albums.albums[0].UID]; len(members) != 2 {
		t.Errorf("album members = %d, want 2", len(members))
	}
	if len(h.labels.labels) != 1 {
		t.Fatalf("labels = %d, want 1", len(h.labels.labels))
	}
	if attached := h.labels.attached[h.labels.labels[0].UID]; len(attached) != 1 {
		t.Errorf("label attachments = %d, want 1", len(attached))
	}
}

// TestImport_idempotentRerun verifies a second run over the same source creates no
// duplicates: the boundary photo is skipped and nothing is re-downloaded.
func TestImport_idempotentRerun(t *testing.T) {
	t.Parallel()
	t0 := time.Date(2023, 6, 1, 10, 0, 0, 0, time.UTC)
	client := &fakeClient{}
	client.photos = []photoprism.Photo{
		client.makePhoto("pp1", t0, "A"),
		client.makePhoto("pp2", t0.Add(time.Hour), "B"),
	}
	h := newHarness(client)

	if _, err := h.svc.Import(context.Background()); err != nil {
		t.Fatalf("first import: %v", err)
	}
	downloadsAfterFirst := h.client.downloadCount()
	photosAfterFirst := len(h.photos.byUID)

	second, err := h.svc.Import(context.Background())
	if err != nil {
		t.Fatalf("second import: %v", err)
	}
	if len(h.photos.byUID) != photosAfterFirst {
		t.Errorf("photo count changed on re-run: %d -> %d", photosAfterFirst, len(h.photos.byUID))
	}
	if second.Counts.Imported != 0 {
		t.Errorf("re-run imported = %d, want 0", second.Counts.Imported)
	}
	if second.Counts.Skipped == 0 {
		t.Error("re-run skipped = 0, want > 0 (boundary photo)")
	}
	if h.client.downloadCount() != downloadsAfterFirst {
		t.Errorf("re-run re-downloaded originals: %d -> %d", downloadsAfterFirst, h.client.downloadCount())
	}
}

// TestImport_incrementalUpdate verifies a changed photo on a later run updates the
// existing record rather than creating a new one.
func TestImport_incrementalUpdate(t *testing.T) {
	t.Parallel()
	t0 := time.Date(2023, 6, 1, 10, 0, 0, 0, time.UTC)
	client := &fakeClient{}
	client.photos = []photoprism.Photo{client.makePhoto("pp1", t0, "Old")}
	h := newHarness(client)

	if _, err := h.svc.Import(context.Background()); err != nil {
		t.Fatalf("first import: %v", err)
	}
	// The same photo, edited and re-touched after the first run's watermark.
	client.photos[0].Title = "New"
	client.photos[0].UpdatedAt = t0.Add(2 * time.Hour)

	second, err := h.svc.Import(context.Background())
	if err != nil {
		t.Fatalf("second import: %v", err)
	}
	if second.Counts.Updated != 1 {
		t.Errorf("updated = %d, want 1", second.Counts.Updated)
	}
	if got := h.photos.byUID[h.photos.byPPUID["pp1"]].Title; got != "New" {
		t.Errorf("title = %q, want New", got)
	}
}

// TestImport_sha256Dedup verifies that an original whose content already exists is
// not re-created: the existing photo is stamped with the PhotoPrism references.
func TestImport_sha256Dedup(t *testing.T) {
	t.Parallel()
	t0 := time.Date(2023, 6, 1, 10, 0, 0, 0, time.UTC)
	client := &fakeClient{}
	pp := client.makePhoto("pp1", t0, "Dup")
	client.photos = []photoprism.Photo{pp}
	h := newHarness(client)

	// Pre-seed a photo with the identical content but no PhotoPrism reference,
	// as if it had been uploaded directly.
	existing, err := h.photos.Create(context.Background(), photos.Photo{
		FileHash: hashBytes([]byte("bytes-pp1")), FilePath: "x/y.jpg", FileName: "y.jpg",
	})
	if err != nil {
		t.Fatalf("seeding photo: %v", err)
	}

	result, err := h.svc.Import(context.Background())
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	if result.Counts.Imported != 0 || result.Counts.Skipped != 1 {
		t.Errorf("counts = %+v, want imported 0 skipped 1", result.Counts)
	}
	if len(h.photos.byUID) != 1 {
		t.Errorf("photo count = %d, want 1 (no new photo)", len(h.photos.byUID))
	}
	stamped := h.photos.byUID[existing.UID]
	if stamped.PhotoprismUID == nil || *stamped.PhotoprismUID != "pp1" {
		t.Errorf("photoprism_uid backfill = %v, want pp1", stamped.PhotoprismUID)
	}
}

// TestImport_perPhotoFailureDoesNotAbort verifies a failed download is recorded
// without aborting the run, and the watermark does not advance past the failure.
func TestImport_perPhotoFailureDoesNotAbort(t *testing.T) {
	t.Parallel()
	tFail := time.Date(2023, 6, 1, 10, 0, 0, 0, time.UTC)
	tOK := tFail.Add(time.Hour)
	client := &fakeClient{downloadErr: map[string]error{"h-bad": errDownload}}
	bad := client.makePhoto("bad", tFail, "Bad")
	good := client.makePhoto("good", tOK, "Good")
	client.photos = []photoprism.Photo{bad, good}
	h := newHarness(client)

	result, err := h.svc.Import(context.Background())
	if err != nil {
		t.Fatalf("Import returned error, want nil: %v", err)
	}
	if result.Counts.Failed != 1 || result.Counts.Imported != 1 {
		t.Errorf("counts = %+v, want failed 1 imported 1", result.Counts)
	}
	if got := h.runs.last().Status; got != importer.StatusDone {
		t.Errorf("run status = %q, want done (failure must not abort)", got)
	}
	if result.Watermark == nil || !result.Watermark.Equal(tFail) {
		t.Errorf("watermark = %v, want %v (capped at the failure)", result.Watermark, tFail)
	}
}

// TestImport_listErrorFailsRun verifies an infrastructure listing failure aborts
// the run, marking it failed and returning the error.
func TestImport_listErrorFailsRun(t *testing.T) {
	t.Parallel()
	client := &fakeClient{listErr: photoprism.ErrUnavailable}
	h := newHarness(client)
	if _, err := h.svc.Import(context.Background()); err == nil {
		t.Fatal("Import error = nil, want listing failure")
	}
	if got := h.runs.last().Status; got != importer.StatusFailed {
		t.Errorf("run status = %q, want failed", got)
	}
}

// TestImportAlbum_scopedRunDoesNotAdvanceWatermark is the safety property of the
// album-scoped import: it pulls one album whole, and leaves the incremental
// cursor alone so a later full import still lists every photo in the library —
// including the ones this run never saw.
func TestImportAlbum_scopedRunDoesNotAdvanceWatermark(t *testing.T) {
	t.Parallel()
	t0 := time.Date(2023, 6, 1, 10, 0, 0, 0, time.UTC)
	t1 := t0.Add(time.Hour)
	client := &fakeClient{}
	inAlbum := client.makePhoto("pp1", t1, "Beach", photoprism.Marker{
		Type: "face", Name: "Alice", X: 0.1, Y: 0.1, W: 0.2, H: 0.2, Score: 90,
	})
	outside := client.makePhoto("pp2", t0, "Sunset")
	client.photos = []photoprism.Photo{inAlbum, outside}
	client.albums = []photoprism.Album{
		{UID: "ppal1", Title: "Holiday", Type: "album"},
		{UID: "ppal2", Title: "Other", Type: "album"},
	}
	client.albumPhotos = map[string][]photoprism.Photo{"ppal1": {inAlbum}, "ppal2": {outside}}

	h := newHarness(client)
	result, err := h.svc.ImportAlbum(context.Background(), "ppal1")
	if err != nil {
		t.Fatalf("ImportAlbum: %v", err)
	}

	if result.Counts.Imported != 1 {
		t.Errorf("imported = %d, want 1 (only the album's photo)", result.Counts.Imported)
	}
	if _, ok := h.photos.byPPUID["pp2"]; ok {
		t.Error("pp2 was imported, but it is outside the scoped album")
	}
	if result.Watermark != nil {
		t.Errorf("watermark = %v, want nil: a scoped run must not move the cursor", result.Watermark)
	}
	run := h.runs.last()
	if run.Status != importer.StatusDone {
		t.Errorf("run status = %q, want done", run.Status)
	}
	if run.HighWatermark != nil {
		t.Errorf("recorded watermark = %v, want nil", run.HighWatermark)
	}
	if len(h.albums.albums) != 1 || h.albums.albums[0].Title != "Holiday" {
		t.Errorf("albums = %v, want only the scoped album mapped", h.albums.albums)
	}
	if _, err := h.people.GetSubjectBySlug(context.Background(), people.Slugify("Alice")); err != nil {
		t.Errorf("marker on the scoped photo did not seed a subject: %v", err)
	}

	// The whole point: the next full import must still see pp2 (never imported)
	// *and* re-list pp1 rather than resuming past it.
	full, err := h.svc.Import(context.Background())
	if err != nil {
		t.Fatalf("Import after scoped run: %v", err)
	}
	if full.Counts.Imported != 1 {
		t.Errorf("full run imported = %d, want 1 (pp2, which the scoped run skipped)", full.Counts.Imported)
	}
	if _, ok := h.photos.byPPUID["pp2"]; !ok {
		t.Error("pp2 still missing after the full import: the scoped run poisoned the watermark")
	}
	if full.Watermark == nil || !full.Watermark.Equal(t1) {
		t.Errorf("full-run watermark = %v, want %v", full.Watermark, t1)
	}
}

// TestImportAlbum_unknownAlbum verifies an album uid the source does not know is
// an error, not a silent no-op run.
func TestImportAlbum_unknownAlbum(t *testing.T) {
	t.Parallel()
	client := &fakeClient{albums: []photoprism.Album{{UID: "ppal1", Title: "Holiday", Type: "album"}}}
	h := newHarness(client)

	_, err := h.svc.ImportAlbum(context.Background(), "nope")
	if !errors.Is(err, ErrAlbumNotFound) {
		t.Fatalf("ImportAlbum error = %v, want ErrAlbumNotFound", err)
	}
	if got := h.runs.last().Status; got != importer.StatusFailed {
		t.Errorf("run status = %q, want failed", got)
	}
}

// TestImportAlbum_emptyUID verifies an empty uid is rejected before a run is even
// opened.
func TestImportAlbum_emptyUID(t *testing.T) {
	t.Parallel()
	h := newHarness(&fakeClient{})

	_, err := h.svc.ImportAlbum(context.Background(), "  ")
	if !errors.Is(err, ErrEmptyAlbumUID) {
		t.Fatalf("ImportAlbum error = %v, want ErrEmptyAlbumUID", err)
	}
	if h.runs.last() != nil {
		t.Error("a run was opened for an empty album uid")
	}
}
