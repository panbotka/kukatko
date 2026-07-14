//go:build integration

package dirimport_test

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/panbotka/kukatko/internal/auth"
	"github.com/panbotka/kukatko/internal/database"
	"github.com/panbotka/kukatko/internal/database/dbtest"
	"github.com/panbotka/kukatko/internal/dirimport"
	"github.com/panbotka/kukatko/internal/importer"
	"github.com/panbotka/kukatko/internal/ingest"
	"github.com/panbotka/kukatko/internal/organize"
	"github.com/panbotka/kukatko/internal/photos"
	"github.com/panbotka/kukatko/internal/storage"
	"github.com/panbotka/kukatko/internal/thumb"
	"github.com/panbotka/kukatko/internal/video"
)

// These tests run only under `make test-integration` against the database named
// by KUKATKO_TEST_DATABASE_URL. They share one database and truncate between
// cases, so they intentionally do not run in parallel.

// testEnv bundles a folder-import service wired to the real ingest pipeline over
// a freshly truncated database and temp-backed storage/cache directories.
type testEnv struct {
	svc      *dirimport.Service
	db       *database.DB
	photos   *photos.Store
	runs     *importer.Store
	organize *organize.Store
	uploader string
}

// newEnv builds the folder import over real storage, a real thumbnailer, the real
// ingest pipeline and the integration database, plus an editor user to own the
// imported photos (photos.uploaded_by is a foreign key).
func newEnv(t *testing.T) *testEnv {
	t.Helper()
	db := dbtest.New(t)
	dbtest.TruncateAll(t, db)

	authSvc := auth.NewService(auth.NewStore(db.Pool()),
		auth.SessionPolicy{TTL: time.Hour, MaxLifetime: 3 * time.Hour})
	uploader, err := authSvc.CreateUser(t.Context(), auth.CreateUserInput{
		Username: "importer", Password: "correct horse battery staple", Role: auth.RoleEditor,
	})
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}

	fs, err := storage.NewFS(t.TempDir())
	if err != nil {
		t.Fatalf("storage.NewFS: %v", err)
	}
	pool := db.Pool()
	photoStore := photos.NewStore(pool)
	organizeStore := organize.NewStore(pool)
	svc := dirimport.New(dirimport.Config{
		Ingest: ingest.New(ingest.Config{
			Storage:     fs,
			Photos:      photoStore,
			Thumbnailer: thumb.New(fs, t.TempDir()),
			TempDir:     t.TempDir(),
		}),
		Runs:        importer.NewStore(pool),
		Photos:      photoStore,
		Albums:      organizeStore,
		Labels:      organizeStore,
		Concurrency: 2,
	})
	return &testEnv{
		svc: svc, db: db, photos: photoStore,
		runs: importer.NewStore(pool), organize: organizeStore, uploader: uploader.UID,
	}
}

// jpegBytes encodes a small solid-colour JPEG; different colours yield different
// content hashes.
func jpegBytes(t *testing.T, r, g, b uint8) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 64, 48))
	for y := range 48 {
		for x := range 64 {
			img.Set(x, y, color.RGBA{R: r, G: g, B: b, A: 255})
		}
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 90}); err != nil {
		t.Fatalf("jpeg.Encode: %v", err)
	}
	return buf.Bytes()
}

// pngBytes encodes a small solid-colour PNG.
func pngBytes(t *testing.T, r, g, b uint8) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 32, 32))
	for y := range 32 {
		for x := range 32 {
			img.Set(x, y, color.RGBA{R: r, G: g, B: b, A: 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("png.Encode: %v", err)
	}
	return buf.Bytes()
}

// write puts content at name under dir, creating parent directories.
func write(t *testing.T, dir, name string, content []byte) {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatalf("WriteFile(%s): %v", path, err)
	}
}

// fixtureTree lays out the directory the import is pointed at: two distinct
// images (one nested), a byte-identical copy of the first under another name, a
// video when ffmpeg is available, and the junk a real folder is full of. It
// returns the root and how many photos a first import should create.
func fixtureTree(t *testing.T) (root string, wantImported int) {
	t.Helper()
	root = t.TempDir()

	write(t, root, "green.jpg", jpegBytes(t, 30, 220, 30))
	write(t, root, "nested/blue.png", pngBytes(t, 30, 30, 220))
	// The same bytes under a different name: SHA256 catches it as a duplicate.
	// Which of the two wins the create is a race the pipeline resolves; the test
	// only asserts that exactly one of them does.
	red := jpegBytes(t, 220, 30, 30)
	write(t, root, "red.jpg", red)
	write(t, root, "nested/red-copy.jpg", red)

	write(t, root, "Thumbs.db", []byte("junk"))
	write(t, root, ".DS_Store", []byte("junk"))
	write(t, root, "red.jpg.xmp", []byte("<xmp/>"))
	write(t, root, "notes.txt", []byte("not media"))
	write(t, root, "@eaDir/red.jpg", red)

	wantImported = 3
	if video.FFmpegAvailable() {
		clip, err := os.ReadFile(filepath.Join("testdata", "sample.mp4"))
		if err != nil {
			t.Fatalf("reading sample.mp4: %v", err)
		}
		write(t, root, "clip.mp4", clip)
		wantImported = 4
	}
	return root, wantImported
}

// countPhotos returns how many photos are catalogued.
func countPhotos(t *testing.T, db *database.DB) int {
	t.Helper()
	var n int
	if err := db.Pool().QueryRow(t.Context(), "SELECT count(*) FROM photos").Scan(&n); err != nil {
		t.Fatalf("counting photos: %v", err)
	}
	return n
}

// countRuns returns how many import runs are recorded.
func countRuns(t *testing.T, db *database.DB) int {
	t.Helper()
	var n int
	if err := db.Pool().QueryRow(t.Context(), "SELECT count(*) FROM import_runs").Scan(&n); err != nil {
		t.Fatalf("counting import runs: %v", err)
	}
	return n
}

// TestImport_folderRoundTrip is the end-to-end contract of the folder import: the
// media files of a mixed directory land in the catalogue with their originals and
// metadata, in-folder duplicates are caught by content hash, junk is skipped with
// a reason, the run is recorded — and a second, identical run imports nothing.
func TestImport_folderRoundTrip(t *testing.T) {
	env := newEnv(t)
	root, wantImported := fixtureTree(t)

	opts := dirimport.Options{
		Root:       root,
		Recursive:  true,
		Album:      "Scans",
		Labels:     []string{"folder-import"},
		UploadedBy: env.uploader,
	}
	first, err := env.svc.Import(t.Context(), opts)
	if err != nil {
		t.Fatalf("first Import: %v", err)
	}

	if first.Counts.Imported != wantImported {
		t.Errorf("imported = %d, want %d (counts %+v)", first.Counts.Imported, wantImported, first.Counts)
	}
	if first.Counts.Duplicates != 1 {
		t.Errorf("duplicates = %d, want 1 (red-copy.jpg is the same bytes as red.jpg)", first.Counts.Duplicates)
	}
	if first.Counts.Failed != 0 {
		t.Errorf("failed = %d, want 0", first.Counts.Failed)
	}
	wantSkips := map[dirimport.SkipReason]int{
		dirimport.SkipJunk:        2, // Thumbs.db, .DS_Store
		dirimport.SkipSidecar:     1, // red.jpg.xmp
		dirimport.SkipUnsupported: 1, // notes.txt
	}
	for reason, want := range wantSkips {
		if got := first.Counts.ByReason[reason]; got != want {
			t.Errorf("skipped %s = %d, want %d (%v)", reason, got, want, first.Counts.ByReason)
		}
	}
	if got := countPhotos(t, env.db); got != wantImported {
		t.Errorf("catalogued %d photos, want %d", got, wantImported)
	}
	assertImportedMetadata(t, env)
	assertRunRecorded(t, env, first.RunID, wantImported)
	// The in-folder duplicate resolves to the photo that won the create, so it is
	// filed into the album as that photo — the membership is idempotent.
	assertFiledUnderAlbum(t, env, wantImported)

	// The whole point: running the same folder again is safe and does nothing.
	second, err := env.svc.Import(t.Context(), opts)
	if err != nil {
		t.Fatalf("second Import: %v", err)
	}
	if second.Counts.Imported != 0 {
		t.Errorf("second run imported = %d, want 0", second.Counts.Imported)
	}
	if want := wantImported + 1; second.Counts.Duplicates != want {
		t.Errorf("second run duplicates = %d, want %d (every media file)", second.Counts.Duplicates, want)
	}
	if got := countPhotos(t, env.db); got != wantImported {
		t.Errorf("after the second run %d photos are catalogued, want %d", got, wantImported)
	}
}

// assertImportedMetadata checks the catalogued photo carries what the pipeline
// extracted: the original was stored and hashed, and a file with no EXIF and no
// date in its name keeps a NULL taken_at rather than an invented one.
func assertImportedMetadata(t *testing.T, env *testEnv) {
	t.Helper()
	photo := photoByName(t, env, "green.jpg")

	if photo.FileHash == "" || photo.FilePath == "" {
		t.Errorf("green.jpg: hash=%q path=%q, want both set", photo.FileHash, photo.FilePath)
	}
	if photo.MediaType != photos.MediaImage {
		t.Errorf("green.jpg: media type = %q, want %q", photo.MediaType, photos.MediaImage)
	}
	if photo.FileWidth != 64 || photo.FileHeight != 48 {
		t.Errorf("green.jpg: %dx%d, want 64x48", photo.FileWidth, photo.FileHeight)
	}
	if photo.TakenAt != nil {
		t.Errorf("green.jpg: taken_at = %v, want NULL (no EXIF, no date in the filename)", photo.TakenAt)
	}
	if photo.UploadedBy == nil || *photo.UploadedBy != env.uploader {
		t.Errorf("green.jpg: uploaded_by = %v, want %q", photo.UploadedBy, env.uploader)
	}
}

// photoByName returns the catalogued photo whose original carried this filename,
// failing the test when it is missing.
func photoByName(t *testing.T, env *testEnv, name string) photos.Photo {
	t.Helper()
	var uid string
	err := env.db.Pool().QueryRow(t.Context(), "SELECT uid FROM photos WHERE file_name = $1", name).Scan(&uid)
	if err != nil {
		t.Fatalf("looking up photo %q: %v", name, err)
	}
	photo, err := env.photos.GetByUID(t.Context(), uid)
	if err != nil {
		t.Fatalf("GetByUID(%s): %v", uid, err)
	}
	return photo
}

// assertRunRecorded checks the folder run shows up in import_runs as a completed
// run of the folder source, with no watermark (a folder has no cursor to resume
// from) and the tally the import reached.
func assertRunRecorded(t *testing.T, env *testEnv, runID int64, wantImported int) {
	t.Helper()
	run, err := env.runs.Get(t.Context(), runID)
	if err != nil {
		t.Fatalf("Get(run %d): %v", runID, err)
	}
	if run.Source != importer.SourceFolder {
		t.Errorf("run source = %q, want %q", run.Source, importer.SourceFolder)
	}
	if run.Status != importer.StatusDone {
		t.Errorf("run status = %q, want %q (last error: %s)", run.Status, importer.StatusDone, run.LastError)
	}
	if run.HighWatermark != nil {
		t.Errorf("run watermark = %v, want NULL", run.HighWatermark)
	}
	if run.Counts.Imported != wantImported {
		t.Errorf("run counts = %+v, want imported=%d", run.Counts, wantImported)
	}
	// Duplicates and skipped junk share the run's skipped bucket.
	if run.Counts.Skipped != 5 {
		t.Errorf("run skipped = %d, want 5 (1 duplicate + 4 junk/sidecar/unsupported)", run.Counts.Skipped)
	}
}

// assertFiledUnderAlbum checks --album and --labels were applied to every photo
// the run touched, the in-folder duplicate included.
func assertFiledUnderAlbum(t *testing.T, env *testEnv, wantMembers int) {
	t.Helper()
	albums, err := env.organize.ListAlbums(t.Context())
	if err != nil {
		t.Fatalf("ListAlbums: %v", err)
	}
	if len(albums) != 1 || albums[0].Title != "Scans" {
		t.Fatalf("albums = %+v, want exactly one titled Scans", albums)
	}
	members, err := env.organize.ListPhotoUIDs(t.Context(), albums[0].UID)
	if err != nil {
		t.Fatalf("ListPhotoUIDs: %v", err)
	}
	if len(members) != wantMembers {
		t.Errorf("album holds %d photos, want %d (the duplicate is filed too)", len(members), wantMembers)
	}

	labels, err := env.organize.ListLabels(t.Context())
	if err != nil {
		t.Fatalf("ListLabels: %v", err)
	}
	if len(labels) != 1 || labels[0].Name != "folder-import" {
		t.Fatalf("labels = %+v, want exactly one named folder-import", labels)
	}
	if labels[0].PhotoCount != wantMembers {
		t.Errorf("label carries %d photos, want %d", labels[0].PhotoCount, wantMembers)
	}
}

// TestImport_dryRunWritesNothing checks --dry-run reaches the same verdicts
// (new, duplicate, skipped) while leaving the database and the storage untouched.
func TestImport_dryRunWritesNothing(t *testing.T) {
	env := newEnv(t)
	root, wantImported := fixtureTree(t)

	result, err := env.svc.Import(t.Context(), dirimport.Options{
		Root:       root,
		Recursive:  true,
		DryRun:     true,
		Album:      "Scans",
		Labels:     []string{"folder-import"},
		UploadedBy: env.uploader,
	})
	if err != nil {
		t.Fatalf("dry-run Import: %v", err)
	}

	if !result.DryRun || result.RunID != 0 {
		t.Errorf("result = %+v, want a dry run with no run id", result)
	}
	// Nothing is in the library yet, so every media file — the in-folder copy
	// included — reads as new; only a real run would collapse the copy into a
	// duplicate.
	if want := wantImported + 1; result.Counts.Imported != want {
		t.Errorf("dry run imported = %d, want %d", result.Counts.Imported, want)
	}
	if got := countPhotos(t, env.db); got != 0 {
		t.Errorf("dry run catalogued %d photos, want 0", got)
	}
	if got := countRuns(t, env.db); got != 0 {
		t.Errorf("dry run recorded %d import runs, want 0", got)
	}
	albums, err := env.organize.ListAlbums(t.Context())
	if err != nil {
		t.Fatalf("ListAlbums: %v", err)
	}
	if len(albums) != 0 {
		t.Errorf("dry run created %d albums, want 0", len(albums))
	}
}

// TestImport_dryRunSpotsExistingDuplicates checks a dry run over a folder that is
// already imported reports every file as a duplicate — the answer a user re-runs
// an import to get.
func TestImport_dryRunSpotsExistingDuplicates(t *testing.T) {
	env := newEnv(t)
	root, wantImported := fixtureTree(t)
	ctx := context.Background()

	if _, err := env.svc.Import(ctx, dirimport.Options{
		Root: root, Recursive: true, UploadedBy: env.uploader,
	}); err != nil {
		t.Fatalf("seeding Import: %v", err)
	}

	result, err := env.svc.Import(ctx, dirimport.Options{Root: root, Recursive: true, DryRun: true})
	if err != nil {
		t.Fatalf("dry-run Import: %v", err)
	}
	if result.Counts.Imported != 0 {
		t.Errorf("dry run over an imported folder reports %d new files, want 0", result.Counts.Imported)
	}
	if want := wantImported + 1; result.Counts.Duplicates != want {
		t.Errorf("dry run duplicates = %d, want %d", result.Counts.Duplicates, want)
	}
	if got := countRuns(t, env.db); got != 1 {
		t.Errorf("import runs = %d, want 1 (the dry run records nothing)", got)
	}
}
