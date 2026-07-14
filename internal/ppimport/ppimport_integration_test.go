//go:build integration

package ppimport_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"image"
	"image/color"
	"image/draw"
	"image/jpeg"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/panbotka/kukatko/internal/database"
	"github.com/panbotka/kukatko/internal/database/dbtest"
	"github.com/panbotka/kukatko/internal/importer"
	"github.com/panbotka/kukatko/internal/jobs"
	"github.com/panbotka/kukatko/internal/organize"
	"github.com/panbotka/kukatko/internal/people"
	"github.com/panbotka/kukatko/internal/photoprism"
	"github.com/panbotka/kukatko/internal/photos"
	"github.com/panbotka/kukatko/internal/ppimport"
	"github.com/panbotka/kukatko/internal/storage"
	"github.com/panbotka/kukatko/internal/thumb"
)

// tinyMP4 is a 64x64, 1-second H.264 sample clip served by the fake PhotoPrism
// for the video and live-photo import tests; it is small enough to probe and to
// extract a poster frame from with the real ffmpeg/ffprobe in CI.
//
//go:embed testdata/tiny.mp4
var tinyMP4 []byte

// tinyMP4b is a second, byte-distinct sample clip so the incremental video test
// imports a genuinely new file rather than tripping content dedup.
//
//go:embed testdata/tiny2.mp4
var tinyMP4b []byte

// These tests run only under `make test-integration` against the database named
// by KUKATKO_TEST_DATABASE_URL. They share one database and truncate between
// cases, so they intentionally do not run in parallel.

// fakePPClient is an in-memory PhotoPrism client that serves real JPEG originals,
// pages the incremental and scoped listings, and can be made to fail a download.
type fakePPClient struct {
	photos      []photoprism.Photo
	albums      []photoprism.Album
	labels      []photoprism.Label
	albumPhotos map[string][]photoprism.Photo
	// queryPhotos answers a scoped listing by its exact q= expression (e.g.
	// `label:"beach"`, `label:"beach" year:2023`).
	queryPhotos map[string][]photoprism.Photo
	// details answers the photo-detail endpoint, the only place the source serves
	// a photo's albums and labels.
	details  map[string]photoprism.PhotoDetail
	files    map[string][]byte
	failHash string

	mu        sync.Mutex
	downloads int
}

// ListPhotos returns the photos scoped by album, by search query, or by the
// incremental watermark.
func (c *fakePPClient) ListPhotos(_ context.Context, p photoprism.PhotoListParams) ([]photoprism.Photo, error) {
	switch {
	case p.AlbumUID != "":
		return c.albumPhotos[p.AlbumUID], nil
	case p.Query != "":
		return c.queryPhotos[p.Query], nil
	default:
		return filterByUpdated(c.photos, p.UpdatedSince), nil
	}
}

// GetPhoto returns the photo's registered detail: the albums it belongs to and
// the labels it carries.
func (c *fakePPClient) GetPhoto(_ context.Context, uid string) (photoprism.PhotoDetail, error) {
	detail, ok := c.details[uid]
	if !ok {
		return photoprism.PhotoDetail{}, photoprism.ErrNotFound
	}
	return detail, nil
}

// setContext registers a photo's detail — its albums and labels — which the
// listing payload does not carry.
func (c *fakePPClient) setContext(p photoprism.Photo, albums []photoprism.Album, labels []photoprism.PhotoLabel) {
	if c.details == nil {
		c.details = map[string]photoprism.PhotoDetail{}
	}
	c.details[p.UID] = photoprism.PhotoDetail{Photo: p, Albums: albums, Labels: labels}
}

// ListAlbums returns all albums.
func (c *fakePPClient) ListAlbums(_ context.Context, _ photoprism.ListParams) ([]photoprism.Album, error) {
	return c.albums, nil
}

// ListLabels returns all labels.
func (c *fakePPClient) ListLabels(_ context.Context, _ photoprism.ListParams) ([]photoprism.Label, error) {
	return c.labels, nil
}

// DownloadOriginal streams the stored bytes for a file hash, or fails the
// configured hash.
func (c *fakePPClient) DownloadOriginal(_ context.Context, fileHash string) (*photoprism.Download, error) {
	c.mu.Lock()
	c.downloads++
	c.mu.Unlock()
	if fileHash == c.failHash {
		return nil, photoprism.ErrUnavailable
	}
	data, ok := c.files[fileHash]
	if !ok {
		return nil, photoprism.ErrNotFound
	}
	return &photoprism.Download{
		Body:          io.NopCloser(bytes.NewReader(data)),
		ContentType:   "image/jpeg",
		ContentLength: int64(len(data)),
	}, nil
}

// downloadCount reports how many originals were requested.
func (c *fakePPClient) downloadCount() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.downloads
}

// addPhoto registers a JPEG original of the given shade and returns the photo.
func (c *fakePPClient) addPhoto(uid string, updated time.Time, title string, shade uint8, markers ...photoprism.Marker) photoprism.Photo {
	if c.files == nil {
		c.files = map[string][]byte{}
	}
	hash := "h-" + uid
	c.files[hash] = jpegOf(shade)
	return photoprism.Photo{
		UID: uid, Type: "image", Title: title, TakenAt: updated, UpdatedAt: updated,
		Width: 8, Height: 8,
		Files: []photoprism.File{{UID: "f-" + uid, Hash: hash, Primary: true, Mime: "image/jpeg", Markers: markers}},
	}
}

// addVideo registers the given MP4 sample as a video original and returns the
// photo. Callers pass byte-distinct samples so content dedup does not collapse
// separate videos.
func (c *fakePPClient) addVideo(uid string, updated time.Time, title string, data []byte) photoprism.Photo {
	if c.files == nil {
		c.files = map[string][]byte{}
	}
	hash := "h-" + uid
	c.files[hash] = data
	return photoprism.Photo{
		UID: uid, Type: "video", Title: title, TakenAt: updated, UpdatedAt: updated,
		Width: 64, Height: 64,
		Files: []photoprism.File{
			{UID: "f-" + uid, Hash: hash, Primary: true, Video: true, Mime: "video/mp4", Name: uid + ".mp4"},
		},
	}
}

// addLive registers a JPEG still and the sample MP4 motion clip, returning the
// live photo that links them.
func (c *fakePPClient) addLive(uid string, updated time.Time, title string, shade uint8) photoprism.Photo {
	if c.files == nil {
		c.files = map[string][]byte{}
	}
	still := "h-" + uid
	motion := "hm-" + uid
	c.files[still] = jpegOf(shade)
	c.files[motion] = tinyMP4
	return photoprism.Photo{
		UID: uid, Type: "live", Title: title, TakenAt: updated, UpdatedAt: updated,
		Width: 8, Height: 8,
		Files: []photoprism.File{
			{UID: "fs-" + uid, Hash: still, Primary: true, Mime: "image/jpeg", Name: uid + ".jpg"},
			{UID: "fm-" + uid, Hash: motion, Video: true, Mime: "video/mp4", Name: uid + ".mov"},
		},
	}
}

// filterByUpdated returns photos updated at or after since (inclusive).
func filterByUpdated(in []photoprism.Photo, since time.Time) []photoprism.Photo {
	if since.IsZero() {
		return in
	}
	out := make([]photoprism.Photo, 0, len(in))
	for _, p := range in {
		if !p.UpdatedAt.Before(since) {
			out = append(out, p)
		}
	}
	return out
}

// jpegOf encodes a solid 8x8 JPEG of the given grey shade, giving each photo
// distinct bytes (and thus a distinct SHA256).
func jpegOf(shade uint8) []byte {
	img := image.NewRGBA(image.Rect(0, 0, 8, 8))
	draw.Draw(img, img.Bounds(), &image.Uniform{C: color.RGBA{R: shade, G: shade, B: shade, A: 255}}, image.Point{}, draw.Src)
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, nil); err != nil {
		panic(err)
	}
	return buf.Bytes()
}

// sha256Hex returns the hex SHA256 of b.
func sha256Hex(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// testEnv bundles a ready import service over a freshly truncated database and
// temp-backed storage/cache directories.
type testEnv struct {
	svc    *ppimport.Service
	client *fakePPClient
	photos *photos.Store
	db     *database.DB
	cache  string
}

// newEnv builds an import service wired to real stores, storage and thumbnailer
// over the integration database and the given fake PhotoPrism client.
func newEnv(t *testing.T, client *fakePPClient) *testEnv {
	t.Helper()
	db := dbtest.New(t)
	dbtest.TruncateAll(t, db)

	store, err := storage.NewFS(t.TempDir())
	if err != nil {
		t.Fatalf("storage: %v", err)
	}
	cache := t.TempDir()
	pool := db.Pool()
	svc := ppimport.New(ppimport.Config{
		Client:      client,
		Runs:        importer.NewStore(pool),
		Photos:      photos.NewStore(pool),
		Storage:     store,
		Thumbnailer: thumb.New(store, cache),
		Albums:      organize.NewStore(pool),
		Labels:      organize.NewStore(pool),
		People:      people.NewStore(pool),
		Enqueuer:    jobs.NewEnqueuer(jobs.NewStore(pool)),
		PageSize:    50,
	})
	return &testEnv{svc: svc, client: client, photos: photos.NewStore(pool), db: db, cache: cache}
}

// thumbCount returns how many JPEG thumbnails the thumbnailer wrote under the
// cache directory; for a video this is non-zero only if the poster frame was
// extracted and resized.
func (e *testEnv) thumbCount(t *testing.T) int {
	t.Helper()
	count := 0
	err := filepath.WalkDir(e.cache, func(_ string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() && strings.HasSuffix(d.Name(), ".jpg") {
			count++
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walking thumb cache: %v", err)
	}
	return count
}

// jobCount returns how many jobs of the given type are queued.
func (e *testEnv) jobCount(t *testing.T, jobType string) int {
	t.Helper()
	var n int
	err := e.db.Pool().QueryRow(t.Context(),
		"SELECT count(*) FROM jobs WHERE type = $1", jobType).Scan(&n)
	if err != nil {
		t.Fatalf("counting %s jobs: %v", jobType, err)
	}
	return n
}

// TestIntegration_firstImport verifies a first import creates photos with external
// IDs, albums, labels and people, and enqueues embed/face jobs.
func TestIntegration_firstImport(t *testing.T) {
	ctx := t.Context()
	t0 := time.Date(2023, 6, 1, 10, 0, 0, 0, time.UTC)
	client := &fakePPClient{}
	p1 := client.addPhoto("pp1", t0, "Beach", 10, photoprism.Marker{
		Type: "face", Name: "Alice", X: 0.1, Y: 0.1, W: 0.2, H: 0.2, Score: 90,
	})
	p2 := client.addPhoto("pp2", t0.Add(time.Hour), "Sunset", 20)
	client.photos = []photoprism.Photo{p1, p2}
	client.albums = []photoprism.Album{{UID: "ppal1", Title: "Holiday", Type: "album"}}
	client.albumPhotos = map[string][]photoprism.Photo{"ppal1": {p1, p2}}
	client.labels = []photoprism.Label{{UID: "pplb1", Name: "Beach", Slug: "beach"}}
	client.queryPhotos = map[string][]photoprism.Photo{`label:"beach"`: {p1}}

	env := newEnv(t, client)
	result, err := env.svc.Import(ctx)
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	if result.Counts.Imported != 2 {
		t.Fatalf("imported = %d, want 2", result.Counts.Imported)
	}

	assertPhotoImported(t, env, "pp1")
	assertAlbumMembership(t, env)
	assertLabelMembership(t, env)
	assertSubject(t, env, "Alice")
	if got := env.jobCount(t, jobs.TypeImageEmbed); got != 2 {
		t.Errorf("image_embed jobs = %d, want 2", got)
	}
	if got := env.jobCount(t, jobs.TypeFaceDetect); got != 2 {
		t.Errorf("face_detect jobs = %d, want 2", got)
	}
}

// assertPhotoImported checks a photo was catalogued with its PhotoPrism IDs and a
// SHA256 file hash matching its bytes.
func assertPhotoImported(t *testing.T, env *testEnv, ppUID string) {
	t.Helper()
	photo, err := env.photos.GetByPhotoprismUID(t.Context(), ppUID)
	if err != nil {
		t.Fatalf("GetByPhotoprismUID(%s): %v", ppUID, err)
	}
	if photo.PhotoprismUID == nil || *photo.PhotoprismUID != ppUID {
		t.Errorf("photoprism_uid = %v, want %s", photo.PhotoprismUID, ppUID)
	}
	if photo.PhotoprismFileHash == nil || *photo.PhotoprismFileHash != "h-"+ppUID {
		t.Errorf("photoprism_file_hash = %v", photo.PhotoprismFileHash)
	}
	if want := sha256Hex(env.client.files["h-"+ppUID]); photo.FileHash != want {
		t.Errorf("file_hash = %s, want %s", photo.FileHash, want)
	}
}

// assertAlbumMembership checks the Holiday album exists with both photos.
func assertAlbumMembership(t *testing.T, env *testEnv) {
	t.Helper()
	store := organize.NewStore(env.db.Pool())
	album, err := store.GetAlbumBySlug(t.Context(), "holiday")
	if err != nil {
		t.Fatalf("album not created: %v", err)
	}
	uids, err := store.ListPhotoUIDs(t.Context(), album.UID)
	if err != nil {
		t.Fatalf("ListPhotoUIDs: %v", err)
	}
	if len(uids) != 2 {
		t.Errorf("album members = %d, want 2", len(uids))
	}
}

// assertLabelMembership checks the Beach label exists with one tagged photo.
func assertLabelMembership(t *testing.T, env *testEnv) {
	t.Helper()
	store := organize.NewStore(env.db.Pool())
	label, err := store.GetLabelBySlug(t.Context(), "beach")
	if err != nil {
		t.Fatalf("label not created: %v", err)
	}
	uids, err := store.ListPhotoUIDsByLabel(t.Context(), label.UID)
	if err != nil {
		t.Fatalf("ListPhotoUIDsByLabel: %v", err)
	}
	if len(uids) != 1 {
		t.Errorf("label members = %d, want 1", len(uids))
	}
}

// assertSubject checks a subject was created from a named marker and has a marker.
func assertSubject(t *testing.T, env *testEnv, name string) {
	t.Helper()
	store := people.NewStore(env.db.Pool())
	subject, err := store.GetSubjectBySlug(t.Context(), people.Slugify(name))
	if err != nil {
		t.Fatalf("subject %q not created: %v", name, err)
	}
	uids, err := store.ListPhotoUIDsBySubject(t.Context(), subject.UID)
	if err != nil {
		t.Fatalf("ListPhotoUIDsBySubject: %v", err)
	}
	if len(uids) != 1 {
		t.Errorf("subject photos = %d, want 1", len(uids))
	}
}

// TestIntegration_idempotentRerun verifies a second pass creates no duplicate
// photos, albums, labels or subjects, and re-downloads nothing.
func TestIntegration_idempotentRerun(t *testing.T) {
	ctx := t.Context()
	t0 := time.Date(2023, 6, 1, 10, 0, 0, 0, time.UTC)
	client := &fakePPClient{}
	p1 := client.addPhoto("pp1", t0, "A", 30, photoprism.Marker{
		Type: "face", Name: "Bob", X: 0.2, Y: 0.2, W: 0.3, H: 0.3,
	})
	p2 := client.addPhoto("pp2", t0.Add(time.Hour), "B", 40)
	client.photos = []photoprism.Photo{p1, p2}
	client.albums = []photoprism.Album{{UID: "a1", Title: "Trip", Type: "album"}}
	client.albumPhotos = map[string][]photoprism.Photo{"a1": {p1, p2}}

	env := newEnv(t, client)
	if _, err := env.svc.Import(ctx); err != nil {
		t.Fatalf("first import: %v", err)
	}
	photosBefore := countRows(t, env, "photos")
	albumsBefore := countRows(t, env, "albums")
	subjectsBefore := countRows(t, env, "subjects")
	markersBefore := countRows(t, env, "markers")
	downloadsBefore := client.downloadCount()

	if _, err := env.svc.Import(ctx); err != nil {
		t.Fatalf("second import: %v", err)
	}
	assertUnchanged(t, env, "photos", photosBefore)
	assertUnchanged(t, env, "albums", albumsBefore)
	assertUnchanged(t, env, "subjects", subjectsBefore)
	assertUnchanged(t, env, "markers", markersBefore)
	if client.downloadCount() != downloadsBefore {
		t.Errorf("re-downloaded originals: %d -> %d", downloadsBefore, client.downloadCount())
	}
}

// countRows returns the number of rows in a data table.
func countRows(t *testing.T, env *testEnv, table string) int {
	t.Helper()
	var n int
	if err := env.db.Pool().QueryRow(t.Context(), "SELECT count(*) FROM "+table).Scan(&n); err != nil {
		t.Fatalf("counting %s: %v", table, err)
	}
	return n
}

// assertUnchanged fails if a table's row count changed from want.
func assertUnchanged(t *testing.T, env *testEnv, table string, want int) {
	t.Helper()
	if got := countRows(t, env, table); got != want {
		t.Errorf("%s rows = %d, want %d (idempotent re-run)", table, got, want)
	}
}

// TestIntegration_incremental verifies only photos changed since the watermark
// are processed on a later run.
func TestIntegration_incremental(t *testing.T) {
	ctx := t.Context()
	t0 := time.Date(2023, 6, 1, 10, 0, 0, 0, time.UTC)
	client := &fakePPClient{}
	client.photos = []photoprism.Photo{
		client.addPhoto("pp1", t0, "One", 50),
		client.addPhoto("pp2", t0.Add(time.Hour), "Two", 60),
	}
	env := newEnv(t, client)
	if _, err := env.svc.Import(ctx); err != nil {
		t.Fatalf("first import: %v", err)
	}
	downloadsBefore := client.downloadCount()

	// Edit pp2 and touch it after the watermark; pp1 is untouched and older.
	client.photos[1].Title = "Two-edited"
	client.photos[1].UpdatedAt = t0.Add(2 * time.Hour)

	result, err := env.svc.Import(ctx)
	if err != nil {
		t.Fatalf("incremental import: %v", err)
	}
	if result.Counts.Updated != 1 || result.Counts.Imported != 0 {
		t.Errorf("counts = %+v, want updated 1 imported 0", result.Counts)
	}
	if client.downloadCount() != downloadsBefore {
		t.Errorf("incremental re-downloaded: %d -> %d", downloadsBefore, client.downloadCount())
	}
	photo, err := env.photos.GetByPhotoprismUID(ctx, "pp2")
	if err != nil || photo.Title != "Two-edited" {
		t.Errorf("pp2 title = %q (err %v), want Two-edited", photo.Title, err)
	}
}

// TestIntegration_sha256Dedup verifies an original whose content already exists is
// not re-created; the existing photo is stamped with the PhotoPrism references.
func TestIntegration_sha256Dedup(t *testing.T) {
	ctx := t.Context()
	t0 := time.Date(2023, 6, 1, 10, 0, 0, 0, time.UTC)
	client := &fakePPClient{}
	client.photos = []photoprism.Photo{client.addPhoto("pp1", t0, "Dup", 70)}
	env := newEnv(t, client)

	// Pre-seed a photo with the identical content but no PhotoPrism reference.
	bytesPP1 := client.files["h-pp1"]
	existing, err := env.photos.Create(ctx, photos.Photo{
		FileHash: sha256Hex(bytesPP1), FilePath: "2020/01/seed.jpg", FileName: "seed.jpg",
		FileMime: "image/jpeg", MediaType: photos.MediaImage, TakenAtSource: "unknown",
	})
	if err != nil {
		t.Fatalf("seeding photo: %v", err)
	}

	result, err := env.svc.Import(ctx)
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	if result.Counts.Skipped != 1 || result.Counts.Imported != 0 {
		t.Errorf("counts = %+v, want skipped 1 imported 0", result.Counts)
	}
	if got := countRows(t, env, "photos"); got != 1 {
		t.Errorf("photos = %d, want 1 (no new photo)", got)
	}
	stamped, err := env.photos.GetByUID(ctx, existing.UID)
	if err != nil {
		t.Fatalf("GetByUID: %v", err)
	}
	if stamped.PhotoprismUID == nil || *stamped.PhotoprismUID != "pp1" {
		t.Errorf("photoprism_uid backfill = %v, want pp1", stamped.PhotoprismUID)
	}
}

// TestIntegration_perPhotoFailure verifies a failed download is recorded without
// aborting the run, the other photos import, and the run completes.
func TestIntegration_perPhotoFailure(t *testing.T) {
	ctx := t.Context()
	t0 := time.Date(2023, 6, 1, 10, 0, 0, 0, time.UTC)
	client := &fakePPClient{}
	client.photos = []photoprism.Photo{
		client.addPhoto("bad", t0, "Bad", 80),
		client.addPhoto("good", t0.Add(time.Hour), "Good", 90),
	}
	client.failHash = "h-bad"
	env := newEnv(t, client)

	result, err := env.svc.Import(ctx)
	if err != nil {
		t.Fatalf("Import returned error, want nil: %v", err)
	}
	if result.Counts.Failed != 1 || result.Counts.Imported != 1 {
		t.Errorf("counts = %+v, want failed 1 imported 1", result.Counts)
	}
	if _, err := env.photos.GetByPhotoprismUID(ctx, "good"); err != nil {
		t.Errorf("good photo not imported: %v", err)
	}
	if _, err := env.photos.GetByPhotoprismUID(ctx, "bad"); err == nil {
		t.Error("bad photo was imported despite download failure")
	}
	// The run is recorded as done with the failure tallied.
	var status string
	if err := env.db.Pool().QueryRow(ctx,
		"SELECT status FROM import_runs WHERE id = $1", result.RunID).Scan(&status); err != nil {
		t.Fatalf("reading run: %v", err)
	}
	if status != string(importer.StatusDone) {
		t.Errorf("run status = %q, want done", status)
	}
}

// TestIntegration_scopedImportLeavesWatermarkNull verifies the safety property of
// a scoped run against the real import_runs table: a --label --year run imports
// only its slice of the library, maps that label, and completes with a NULL
// high_watermark — so the next full import still walks the whole catalogue.
func TestIntegration_scopedImportLeavesWatermarkNull(t *testing.T) {
	ctx := t.Context()
	t0 := time.Date(2023, 6, 1, 10, 0, 0, 0, time.UTC)
	client := &fakePPClient{}
	tagged := client.addPhoto("pp1", t0.Add(time.Hour), "Beach", 10)
	other := client.addPhoto("pp2", t0, "Sunset", 20)
	client.photos = []photoprism.Photo{tagged, other}
	client.labels = []photoprism.Label{{UID: "pplb1", Name: "Beach", Slug: "beach"}}
	client.queryPhotos = map[string][]photoprism.Photo{
		`label:"beach" year:2023`: {tagged},
		`label:"beach"`:           {tagged},
	}
	client.setContext(tagged, nil, []photoprism.PhotoLabel{
		{LabelSrc: "image", Uncertainty: 10, Label: photoprism.Label{UID: "pplb1", Name: "Beach", Slug: "beach"}},
	})
	env := newEnv(t, client)

	result, err := env.svc.ImportScoped(ctx, ppimport.Scope{Label: "beach", Year: 2023})
	if err != nil {
		t.Fatalf("ImportScoped: %v", err)
	}
	if result.Counts.Imported != 1 {
		t.Fatalf("imported = %d, want 1 (only the labelled photo)", result.Counts.Imported)
	}
	if result.Watermark != nil {
		t.Errorf("watermark = %v, want nil", result.Watermark)
	}
	if _, err := env.photos.GetByPhotoprismUID(ctx, "pp2"); err == nil {
		t.Error("pp2 was imported, but it is outside the scope")
	}
	assertLabelMembership(t, env)

	var watermark *time.Time
	if err := env.db.Pool().QueryRow(ctx,
		"SELECT high_watermark FROM import_runs WHERE id = $1", result.RunID).Scan(&watermark); err != nil {
		t.Fatalf("reading run: %v", err)
	}
	if watermark != nil {
		t.Errorf("recorded high_watermark = %v, want NULL: a scoped run must not move the cursor", watermark)
	}

	// The next full import must still see pp2, which the scoped run never listed.
	full, err := env.svc.Import(ctx)
	if err != nil {
		t.Fatalf("Import after the scoped run: %v", err)
	}
	if full.Counts.Imported != 1 {
		t.Errorf("full run imported = %d, want 1 (pp2)", full.Counts.Imported)
	}
	if _, err := env.photos.GetByPhotoprismUID(ctx, "pp2"); err != nil {
		t.Errorf("pp2 still missing after the full import: %v", err)
	}
}

// TestIntegration_video verifies a video is imported as a video photo with probed
// metadata, a generated poster, external IDs and an enqueued embedding job; the
// re-run is idempotent and a later run incrementally picks up a new video.
func TestIntegration_video(t *testing.T) {
	ctx := t.Context()
	t0 := time.Date(2023, 6, 1, 10, 0, 0, 0, time.UTC)
	client := &fakePPClient{}
	client.photos = []photoprism.Photo{client.addVideo("vid1", t0, "Clip", tinyMP4)}
	env := newEnv(t, client)

	result, err := env.svc.Import(ctx)
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	if result.Counts.Imported != 1 {
		t.Fatalf("imported = %d, want 1", result.Counts.Imported)
	}

	photo := assertVideoPhoto(t, env, "vid1")
	if photo.PhotoprismUID == nil || *photo.PhotoprismUID != "vid1" ||
		photo.PhotoprismFileHash == nil || *photo.PhotoprismFileHash != "h-vid1" {
		t.Errorf("external IDs = %v / %v", photo.PhotoprismUID, photo.PhotoprismFileHash)
	}
	if env.thumbCount(t) == 0 {
		t.Error("no poster/thumbnails generated for the video")
	}
	if got := env.jobCount(t, jobs.TypeImageEmbed); got != 1 {
		t.Errorf("image_embed jobs = %d, want 1 (poster embedding)", got)
	}

	// Re-run is idempotent: no new photo, no re-download.
	downloadsBefore := client.downloadCount()
	if _, err := env.svc.Import(ctx); err != nil {
		t.Fatalf("re-run: %v", err)
	}
	if got := countRows(t, env, "photos"); got != 1 {
		t.Errorf("photos after re-run = %d, want 1", got)
	}
	if client.downloadCount() != downloadsBefore {
		t.Errorf("re-run re-downloaded: %d -> %d", downloadsBefore, client.downloadCount())
	}

	// Incremental: a new video added after the watermark is picked up.
	client.photos = append(client.photos, client.addVideo("vid2", t0.Add(2*time.Hour), "Clip2", tinyMP4b))
	inc, err := env.svc.Import(ctx)
	if err != nil {
		t.Fatalf("incremental import: %v", err)
	}
	if inc.Counts.Imported != 1 {
		t.Errorf("incremental imported = %d, want 1 (the new video)", inc.Counts.Imported)
	}
	assertVideoPhoto(t, env, "vid2")
}

// assertVideoPhoto fetches the imported video photo and checks its media type and
// probed video metadata, returning it for further assertions.
func assertVideoPhoto(t *testing.T, env *testEnv, ppUID string) photos.Photo {
	t.Helper()
	photo, err := env.photos.GetByPhotoprismUID(t.Context(), ppUID)
	if err != nil {
		t.Fatalf("GetByPhotoprismUID(%s): %v", ppUID, err)
	}
	if photo.MediaType != photos.MediaVideo {
		t.Errorf("media_type = %q, want video", photo.MediaType)
	}
	if photo.DurationMs == nil || *photo.DurationMs <= 0 {
		t.Errorf("duration_ms = %v, want > 0 (probed)", photo.DurationMs)
	}
	if photo.VideoCodec == "" {
		t.Error("video_codec empty, want a probed codec")
	}
	return photo
}

// TestIntegration_livePhoto verifies a live photo stores the still as the primary
// original and the motion clip as a sidecar file, catalogued as media_type live.
func TestIntegration_livePhoto(t *testing.T) {
	ctx := t.Context()
	t0 := time.Date(2023, 6, 1, 10, 0, 0, 0, time.UTC)
	client := &fakePPClient{}
	client.photos = []photoprism.Photo{client.addLive("live1", t0, "Moment", 120)}
	env := newEnv(t, client)

	result, err := env.svc.Import(ctx)
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	if result.Counts.Imported != 1 {
		t.Fatalf("imported = %d, want 1", result.Counts.Imported)
	}

	photo, err := env.photos.GetByPhotoprismUID(ctx, "live1")
	if err != nil {
		t.Fatalf("GetByPhotoprismUID: %v", err)
	}
	if photo.MediaType != photos.MediaLive {
		t.Errorf("media_type = %q, want live", photo.MediaType)
	}
	files, err := env.photos.ListFiles(ctx, photo.UID)
	if err != nil {
		t.Fatalf("ListFiles: %v", err)
	}
	assertLivePhotoFiles(t, files)

	// Re-run links no duplicate file rows.
	if _, err := env.svc.Import(ctx); err != nil {
		t.Fatalf("re-run: %v", err)
	}
	again, err := env.photos.ListFiles(ctx, photo.UID)
	if err != nil {
		t.Fatalf("ListFiles after re-run: %v", err)
	}
	if len(again) != len(files) {
		t.Errorf("file rows after re-run = %d, want %d (idempotent)", len(again), len(files))
	}
}

// assertLivePhotoFiles checks the still primary original and the motion sidecar
// were both linked.
func assertLivePhotoFiles(t *testing.T, files []photos.PhotoFile) {
	t.Helper()
	if len(files) != 2 {
		t.Fatalf("file rows = %d, want 2 (still + motion)", len(files))
	}
	var primary, sidecar *photos.PhotoFile
	for i := range files {
		switch files[i].Role {
		case photos.RoleOriginal:
			primary = &files[i]
		case photos.RoleSidecar:
			sidecar = &files[i]
		default:
		}
	}
	if primary == nil || !primary.IsPrimary {
		t.Errorf("primary still missing or not primary: %+v", primary)
	}
	if sidecar == nil || sidecar.IsPrimary {
		t.Errorf("sidecar motion missing or marked primary: %+v", sidecar)
	}
	if sidecar != nil && !strings.HasPrefix(sidecar.FileMime, "video/") {
		t.Errorf("sidecar mime = %q, want video/*", sidecar.FileMime)
	}
}

// TestIntegration_scopedImportBringsWholeContext is the point of a scoped run: a
// photo selected because it sits in one album is migrated with its whole context
// — the two other albums it also lives in and the label it carries, with that
// label's source and uncertainty — and a second run changes nothing.
func TestIntegration_scopedImportBringsWholeContext(t *testing.T) {
	ctx := t.Context()
	t0 := time.Date(2023, 6, 1, 10, 0, 0, 0, time.UTC)
	client := &fakePPClient{}
	inScope := client.addPhoto("pp1", t0, "Ostatky", 10)
	outside := client.addPhoto("pp2", t0.Add(time.Hour), "Sunset", 20)
	client.photos = []photoprism.Photo{inScope, outside}
	// The source knows four albums; only the first is named by the scope, and the
	// last belongs to the photo the scope leaves out.
	albums := []photoprism.Album{
		{UID: "ppal1", Title: "Ostatky 2016", Type: "album", Description: "Masopust"},
		{UID: "ppal2", Title: "Rodina", Type: "folder"},
		{UID: "ppal3", Title: "Léto", Type: "moment"},
	}
	client.albums = append(slices.Clone(albums), photoprism.Album{UID: "ppal4", Title: "Other", Type: "album"})
	client.albumPhotos = map[string][]photoprism.Photo{"ppal1": {inScope}, "ppal4": {outside}}
	client.labels = []photoprism.Label{{UID: "pplb1", Name: "SDH", Slug: "sdh", Priority: 10}}
	client.setContext(inScope, albums, []photoprism.PhotoLabel{
		{LabelSrc: "image", Uncertainty: 20, Label: photoprism.Label{UID: "pplb1", Name: "SDH", Slug: "sdh", Priority: 10}},
	})
	env := newEnv(t, client)

	result, err := env.svc.ImportScoped(ctx, ppimport.Scope{AlbumUID: "ppal1"})
	if err != nil {
		t.Fatalf("ImportScoped: %v", err)
	}
	if result.Counts.Imported != 1 {
		t.Fatalf("imported = %d, want 1 (only the album's photo)", result.Counts.Imported)
	}
	assertWholeContext(t, env)

	// A re-run re-reads the same context and creates no duplicate row.
	rerun, err := env.svc.ImportScoped(ctx, ppimport.Scope{AlbumUID: "ppal1"})
	if err != nil {
		t.Fatalf("re-run: %v", err)
	}
	if rerun.Counts.Imported != 0 || rerun.Counts.Skipped != 1 {
		t.Errorf("re-run counts = %+v, want the photo skipped", rerun.Counts)
	}
	assertWholeContext(t, env)
	if got := countRows(t, env, "photos"); got != 1 {
		t.Errorf("photos after re-run = %d, want 1", got)
	}
}

// assertWholeContext checks the scoped photo carries all three of its source
// albums and its label (with the source's own source and uncertainty), and that
// nothing outside its context was mapped — the run must not have walked the
// source catalogue.
func assertWholeContext(t *testing.T, env *testEnv) {
	t.Helper()
	ctx := t.Context()
	photo, err := env.photos.GetByPhotoprismUID(ctx, "pp1")
	if err != nil {
		t.Fatalf("GetByPhotoprismUID(pp1): %v", err)
	}

	rows, err := env.db.Pool().Query(ctx, `
		SELECT a.title, a.type FROM albums a
		JOIN album_photos ap ON ap.album_uid = a.uid
		WHERE ap.photo_uid = $1 ORDER BY a.title`, photo.UID)
	if err != nil {
		t.Fatalf("reading album membership: %v", err)
	}
	defer rows.Close()
	var titles, types []string
	for rows.Next() {
		var title, albumType string
		if err := rows.Scan(&title, &albumType); err != nil {
			t.Fatalf("scanning album: %v", err)
		}
		titles, types = append(titles, title), append(types, albumType)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterating albums: %v", err)
	}
	if want := []string{"Léto", "Ostatky 2016", "Rodina"}; !slices.Equal(titles, want) {
		t.Errorf("albums of the scoped photo = %v, want %v", titles, want)
	}
	if want := []string{"moment", "album", "folder"}; !slices.Equal(types, want) {
		t.Errorf("album types = %v, want %v (every album type carried over)", types, want)
	}
	if got := countRows(t, env, "albums"); got != len(titles) {
		t.Errorf("albums in the catalogue = %d, want %d: nothing outside the photo's context", got, len(titles))
	}

	var name, source string
	var uncertainty int
	err = env.db.Pool().QueryRow(ctx, `
		SELECT l.name, pl.source, pl.uncertainty FROM labels l
		JOIN photo_labels pl ON pl.label_uid = l.uid
		WHERE pl.photo_uid = $1`, photo.UID).Scan(&name, &source, &uncertainty)
	if err != nil {
		t.Fatalf("reading the photo's label: %v", err)
	}
	if name != "SDH" || source != string(organize.SourceAI) || uncertainty != 20 {
		t.Errorf("label = %q from %q uncertainty %d, want SDH/ai/20", name, source, uncertainty)
	}
	if got := countRows(t, env, "labels"); got != 1 {
		t.Errorf("labels in the catalogue = %d, want 1", got)
	}
}
