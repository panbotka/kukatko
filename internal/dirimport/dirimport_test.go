package dirimport

import (
	"context"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/panbotka/kukatko/internal/importer"
	"github.com/panbotka/kukatko/internal/ingest"
	"github.com/panbotka/kukatko/internal/organize"
	"github.com/panbotka/kukatko/internal/photos"
)

// fakeIngester records what it was asked to ingest and replies from a scripted
// table keyed by filename, defaulting to a fresh create. It stands in for the
// real pipeline so the walk, the tally and the album/label wiring can be tested
// without storage, a thumbnailer or a database.
type fakeIngester struct {
	mu sync.Mutex
	// seen is every filename handed to Ingest, in call order.
	seen []string
	// script maps a filename to the outcome the pipeline should report; a filename
	// that is absent is created.
	script map[string]ingest.FileResult
	// uploaders records the uploadedBy passed with each call.
	uploaders []string
}

// Ingest records the call, drains the reader (as the real pipeline does) and
// replies from the script.
func (f *fakeIngester) Ingest(_ context.Context, src io.Reader, filename, uploadedBy string) ingest.FileResult {
	_, _ = io.Copy(io.Discard, src)

	f.mu.Lock()
	defer f.mu.Unlock()
	f.seen = append(f.seen, filename)
	f.uploaders = append(f.uploaders, uploadedBy)
	if res, ok := f.script[filename]; ok {
		return res
	}
	return ingest.FileResult{
		Filename: filename,
		Status:   http.StatusCreated,
		Outcome:  ingest.OutcomeCreated,
		PhotoUID: "uid-" + filename,
	}
}

// ingested returns the filenames handed to Ingest, sorted for comparison (the
// worker pool completes out of order).
func (f *fakeIngester) ingested() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := slices.Clone(f.seen)
	slices.Sort(out)
	return out
}

// fakeRuns is an in-memory RunStore recording the lifecycle of the import run.
type fakeRuns struct {
	mu sync.Mutex
	// started counts Start calls.
	started int
	// startErr, when set, fails Start.
	startErr error
	// checkpoints holds every tally written by UpdateCounts.
	checkpoints []importer.Counts
	// completed holds the tally of a Complete call, if any.
	completed *importer.Counts
	// failed holds the reason of a Fail call, if any.
	failed *string
}

// Start opens a fake run with id 1.
func (f *fakeRuns) Start(_ context.Context, source importer.Source) (importer.Run, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.startErr != nil {
		return importer.Run{}, f.startErr
	}
	f.started++
	return importer.Run{ID: 1, Source: source, Status: importer.StatusRunning}, nil
}

// UpdateCounts records a checkpoint.
func (f *fakeRuns) UpdateCounts(_ context.Context, _ int64, counts importer.Counts) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.checkpoints = append(f.checkpoints, counts)
	return nil
}

// Complete records the final tally.
func (f *fakeRuns) Complete(_ context.Context, _ int64, _ *time.Time, counts importer.Counts) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.completed = &counts
	return nil
}

// Fail records the failure reason.
func (f *fakeRuns) Fail(_ context.Context, _ int64, lastErr string, _ importer.Counts) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.failed = &lastErr
	return nil
}

// fakePhotos is an in-memory PhotoLookup over a hash→photo table.
type fakePhotos struct {
	// byHash maps a SHA256 content hash to the photo already holding it.
	byHash map[string]photos.Photo
	// byUID maps a photo UID to the photo.
	byUID map[string]photos.Photo
}

// GetByFileHash returns the photo with this content hash, or ErrPhotoNotFound.
func (f *fakePhotos) GetByFileHash(_ context.Context, hash string) (photos.Photo, error) {
	if photo, ok := f.byHash[hash]; ok {
		return photo, nil
	}
	return photos.Photo{}, photos.ErrPhotoNotFound
}

// GetByUID returns the photo with this UID, or ErrPhotoNotFound.
func (f *fakePhotos) GetByUID(_ context.Context, uid string) (photos.Photo, error) {
	if photo, ok := f.byUID[uid]; ok {
		return photo, nil
	}
	return photos.Photo{}, photos.ErrPhotoNotFound
}

// fakeOrganize is an in-memory AlbumStore and LabelStore recording what the
// import filed where.
type fakeOrganize struct {
	mu sync.Mutex
	// albums are the existing albums, by UID.
	albums map[string]organize.Album
	// labels are the existing labels, by UID.
	labels map[string]organize.Label
	// members records albumUID→photoUIDs added by the import.
	members map[string][]string
	// attached records labelUID→photoUIDs attached by the import.
	attached map[string][]string
	// created counts albums and labels the import had to create.
	createdAlbums, createdLabels int
}

// newFakeOrganize returns an empty album/label catalogue.
func newFakeOrganize() *fakeOrganize {
	return &fakeOrganize{
		albums:   map[string]organize.Album{},
		labels:   map[string]organize.Label{},
		members:  map[string][]string{},
		attached: map[string][]string{},
	}
}

// GetAlbumByUID returns the album with this UID, or organize.ErrAlbumNotFound.
func (f *fakeOrganize) GetAlbumByUID(_ context.Context, uid string) (organize.Album, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if album, ok := f.albums[uid]; ok {
		return album, nil
	}
	return organize.Album{}, organize.ErrAlbumNotFound
}

// ListAlbums returns every album as a summary.
func (f *fakeOrganize) ListAlbums(_ context.Context) ([]organize.AlbumSummary, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]organize.AlbumSummary, 0, len(f.albums))
	for _, album := range f.albums {
		out = append(out, organize.AlbumSummary{AlbumCount: organize.AlbumCount{Album: album}})
	}
	return out, nil
}

// CreateAlbum inserts an album with a generated UID.
func (f *fakeOrganize) CreateAlbum(_ context.Context, a organize.Album) (organize.Album, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.createdAlbums++
	a.UID = "album-" + a.Title
	f.albums[a.UID] = a
	return a, nil
}

// AddPhoto records an album membership.
func (f *fakeOrganize) AddPhoto(_ context.Context, albumUID, photoUID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.members[albumUID] = append(f.members[albumUID], photoUID)
	return nil
}

// ListLabels returns every label with a zero count.
func (f *fakeOrganize) ListLabels(_ context.Context) ([]organize.LabelCount, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]organize.LabelCount, 0, len(f.labels))
	for _, label := range f.labels {
		out = append(out, organize.LabelCount{Label: label})
	}
	return out, nil
}

// CreateLabel inserts a label with a generated UID.
func (f *fakeOrganize) CreateLabel(_ context.Context, l organize.Label) (organize.Label, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.createdLabels++
	l.UID = "label-" + l.Name
	f.labels[l.UID] = l
	return l, nil
}

// AttachLabel records a label attachment.
func (f *fakeOrganize) AttachLabel(
	_ context.Context, photoUID, labelUID string, _ organize.LabelSource, _ int,
) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.attached[labelUID] = append(f.attached[labelUID], photoUID)
	return nil
}

// memberUIDs returns the photos filed into an album, sorted.
func (f *fakeOrganize) memberUIDs(albumUID string) []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := slices.Clone(f.members[albumUID])
	slices.Sort(out)
	return out
}

// testEnv bundles a Service over the fakes so a test can assert on both.
type testEnv struct {
	svc       *Service
	ingester  *fakeIngester
	runs      *fakeRuns
	photos    *fakePhotos
	organizer *fakeOrganize
}

// newEnv builds a Service over fresh fakes, with the ingest pipeline scripted by
// script (a filename→result table; absent filenames are created).
func newEnv(t *testing.T, script map[string]ingest.FileResult) *testEnv {
	t.Helper()
	env := &testEnv{
		ingester:  &fakeIngester{script: script},
		runs:      &fakeRuns{},
		photos:    &fakePhotos{byHash: map[string]photos.Photo{}, byUID: map[string]photos.Photo{}},
		organizer: newFakeOrganize(),
	}
	env.svc = New(Config{
		Ingest: env.ingester,
		Runs:   env.runs,
		Photos: env.photos,
		Albums: env.organizer,
		Labels: env.organizer,
	})
	return env
}

// writeFile creates a file with the given content under dir, creating any parent
// directories, and returns its path.
func writeFile(t *testing.T, dir, name, content string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		t.Fatalf("MkdirAll(%s): %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("WriteFile(%s): %v", path, err)
	}
	return path
}

// mixedTree lays out a directory with one media file at the root, one in a nested
// subfolder, and every kind of file the walk is supposed to skip.
func mixedTree(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	writeFile(t, root, "a.jpg", "aaa")
	writeFile(t, root, "nested/b.png", "bbb")
	writeFile(t, root, "Thumbs.db", "junk")
	writeFile(t, root, ".DS_Store", "junk")
	writeFile(t, root, ".hidden.jpg", "hidden")
	writeFile(t, root, "notes.txt", "text")
	writeFile(t, root, "a.jpg.xmp", "<xmp/>")
	writeFile(t, root, "takeout.json", "{}")
	writeFile(t, root, "empty.jpg", "")
	writeFile(t, root, "@eaDir/c.jpg", "synology")
	writeFile(t, root, "__MACOSX/d.jpg", "apple")
	writeFile(t, root, ".git/e.jpg", "vcs")
	return root
}

// TestImport_skipRulesAndRecursion checks that a mixed tree yields exactly the
// media files — and that every junk, hidden, sidecar, empty and unsupported file
// is skipped with the reason that explains it.
func TestImport_skipRulesAndRecursion(t *testing.T) {
	t.Parallel()

	env := newEnv(t, nil)
	root := mixedTree(t)

	result, err := env.svc.Import(t.Context(), Options{Root: root, Recursive: true})
	if err != nil {
		t.Fatalf("Import: %v", err)
	}

	if got, want := env.ingester.ingested(), []string{"a.jpg", "b.png"}; !slices.Equal(got, want) {
		t.Errorf("ingested files = %v, want %v", got, want)
	}
	if result.Counts.Imported != 2 {
		t.Errorf("imported = %d, want 2", result.Counts.Imported)
	}
	if result.Counts.Failed != 0 {
		t.Errorf("failed = %d, want 0", result.Counts.Failed)
	}
	wantReasons := map[SkipReason]int{
		SkipJunk:        2, // Thumbs.db, .DS_Store
		SkipHidden:      1, // .hidden.jpg
		SkipSidecar:     2, // a.jpg.xmp, takeout.json
		SkipUnsupported: 1, // notes.txt
		SkipEmpty:       1, // empty.jpg
	}
	for reason, want := range wantReasons {
		if got := result.Counts.ByReason[reason]; got != want {
			t.Errorf("skipped %s = %d, want %d (all: %v)", reason, got, want, result.Counts.ByReason)
		}
	}
	// @eaDir, __MACOSX and .git are pruned whole, so their contents are never even
	// listed: the tally only counts what the walk reached.
	if got := result.Counts.Skipped; got != 7 {
		t.Errorf("skipped = %d, want 7 (%v)", got, result.Counts.ByReason)
	}
}

// TestImport_nonRecursive checks that --no-recursive imports the flat directory
// and never descends into a subfolder.
func TestImport_nonRecursive(t *testing.T) {
	t.Parallel()

	env := newEnv(t, nil)
	root := t.TempDir()
	writeFile(t, root, "a.jpg", "aaa")
	writeFile(t, root, "nested/b.jpg", "bbb")

	result, err := env.svc.Import(t.Context(), Options{Root: root, Recursive: false})
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	if got, want := env.ingester.ingested(), []string{"a.jpg"}; !slices.Equal(got, want) {
		t.Errorf("ingested files = %v, want %v", got, want)
	}
	if result.Counts.Imported != 1 {
		t.Errorf("imported = %d, want 1", result.Counts.Imported)
	}
}

// TestImport_symlinksAreSkipped checks that a symlink is reported and never
// followed, so the walk cannot loop through a link back to its own parent.
func TestImport_symlinksAreSkipped(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("symlinks need privileges on windows")
	}

	env := newEnv(t, nil)
	root := t.TempDir()
	writeFile(t, root, "a.jpg", "aaa")
	if err := os.Symlink(filepath.Join(root, "a.jpg"), filepath.Join(root, "link.jpg")); err != nil {
		t.Fatalf("Symlink: %v", err)
	}
	if err := os.Symlink(root, filepath.Join(root, "loop")); err != nil {
		t.Fatalf("Symlink: %v", err)
	}

	result, err := env.svc.Import(t.Context(), Options{Root: root, Recursive: true})
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	if got, want := env.ingester.ingested(), []string{"a.jpg"}; !slices.Equal(got, want) {
		t.Errorf("ingested files = %v, want %v", got, want)
	}
	if got := result.Counts.ByReason[SkipSymlink]; got != 2 {
		t.Errorf("skipped symlinks = %d, want 2 (%v)", got, result.Counts.ByReason)
	}
}

// TestImport_duplicatesAndFailures checks that the pipeline's per-file outcomes
// land in the right buckets: a duplicate is not an import and does not fail the
// run, and a failing file neither aborts the run nor stops the others.
func TestImport_duplicatesAndFailures(t *testing.T) {
	t.Parallel()

	env := newEnv(t, map[string]ingest.FileResult{
		"dup.jpg": {
			Filename: "dup.jpg", Status: http.StatusConflict,
			Outcome: ingest.OutcomeDuplicate, PhotoUID: "existing",
		},
		"bad.jpg": {
			Filename: "bad.jpg", Status: http.StatusInternalServerError,
			Outcome: ingest.OutcomeError, Error: "decoding original: corrupt",
		},
	})
	env.photos.byUID["existing"] = photos.Photo{UID: "existing", FilePath: "2014/06/abc.jpg"}

	root := t.TempDir()
	writeFile(t, root, "new.jpg", "new")
	writeFile(t, root, "dup.jpg", "dup")
	writeFile(t, root, "bad.jpg", "bad")

	var seen []FileResult
	result, err := env.svc.Import(t.Context(), Options{
		Root:      root,
		Recursive: true,
		Progress:  func(res FileResult, _, _ int) { seen = append(seen, res) },
	})
	if err != nil {
		t.Fatalf("Import: %v", err)
	}

	want := Counts{Imported: 1, Duplicates: 1, Failed: 1}
	got := result.Counts
	if got.Imported != want.Imported || got.Duplicates != want.Duplicates || got.Failed != want.Failed {
		t.Errorf("counts = %+v, want imported=1 duplicates=1 failed=1", got)
	}
	if len(seen) != 3 {
		t.Fatalf("progress reported %d files, want 3", len(seen))
	}
	dup := findResult(t, seen, "dup.jpg")
	if dup.ExistingPath != "2014/06/abc.jpg" {
		t.Errorf("duplicate ExistingPath = %q, want the library path of the photo it collided with", dup.ExistingPath)
	}
	if bad := findResult(t, seen, "bad.jpg"); bad.Err == nil {
		t.Error("failed file carries no error")
	}
	if env.runs.completed == nil {
		t.Fatal("run was not completed")
	}
	// import_runs has no duplicates bucket: duplicates and skipped junk both count
	// as skipped there.
	if *env.runs.completed != (importer.Counts{Imported: 1, Skipped: 1, Failed: 1}) {
		t.Errorf("recorded counts = %+v, want imported=1 skipped=1 failed=1", *env.runs.completed)
	}
}

// findResult returns the reported result for a path, failing the test if the file
// was never reported.
func findResult(t *testing.T, results []FileResult, path string) FileResult {
	t.Helper()
	for _, res := range results {
		if res.Path == path {
			return res
		}
	}
	t.Fatalf("no result reported for %q", path)
	return FileResult{}
}

// TestImport_reportsIngestWarnings checks that a photo the pipeline created but
// complained about (an undecodable file gets an original but no thumbnail) is
// still an import — and that the complaint reaches the report, not only the log.
func TestImport_reportsIngestWarnings(t *testing.T) {
	t.Parallel()

	env := newEnv(t, map[string]ingest.FileResult{
		"weird.jpg": {
			Filename: "weird.jpg", Status: http.StatusCreated,
			Outcome: ingest.OutcomeCreated, PhotoUID: "uid-weird",
			Warnings: []ingest.Warning{
				{Code: "thumbnail_failed", Message: "decode: unknown format"},
				{Code: "phash_failed", Message: "decode: unknown format"},
			},
		},
	})
	root := t.TempDir()
	writeFile(t, root, "weird.jpg", "not really a jpeg")

	var seen []FileResult
	result, err := env.svc.Import(t.Context(), Options{
		Root: root, Recursive: true, Progress: func(res FileResult, _, _ int) { seen = append(seen, res) },
	})
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	if result.Counts.Imported != 1 || result.Counts.Failed != 0 {
		t.Errorf("counts = %+v, want imported=1 failed=0 (a warning is not a failure)", result.Counts)
	}
	got := findResult(t, seen, "weird.jpg")
	if want := []string{"thumbnail_failed", "phash_failed"}; !slices.Equal(got.Warnings, want) {
		t.Errorf("warnings = %v, want %v", got.Warnings, want)
	}
}

// TestImport_dryRunWritesNothing checks that a dry run classifies every file
// against the catalogue — new versus duplicate — while writing nothing at all: no
// ingest, no import run, no album.
func TestImport_dryRunWritesNothing(t *testing.T) {
	t.Parallel()

	env := newEnv(t, nil)
	root := t.TempDir()
	writeFile(t, root, "new.jpg", "brand new bytes")
	dupPath := writeFile(t, root, "dup.jpg", "already in the library")
	writeFile(t, root, "Thumbs.db", "junk")

	hash, err := hashFile(dupPath)
	if err != nil {
		t.Fatalf("hashFile: %v", err)
	}
	env.photos.byHash[hash] = photos.Photo{UID: "existing", FilePath: "2019/01/dup.jpg"}

	result, err := env.svc.Import(t.Context(), Options{
		Root: root, Recursive: true, DryRun: true, Album: "Scans", Labels: []string{"scan"},
	})
	if err != nil {
		t.Fatalf("Import: %v", err)
	}

	if !result.DryRun || result.RunID != 0 {
		t.Errorf("result = %+v, want a dry run with no run id", result)
	}
	if got := result.Counts; got.Imported != 1 || got.Duplicates != 1 || got.Skipped != 1 {
		t.Errorf("counts = %+v, want imported=1 duplicates=1 skipped=1", got)
	}
	if len(env.ingester.ingested()) != 0 {
		t.Errorf("dry run ingested %v, want nothing", env.ingester.ingested())
	}
	if env.runs.started != 0 {
		t.Errorf("dry run opened %d import runs, want 0", env.runs.started)
	}
	if env.organizer.createdAlbums != 0 || env.organizer.createdLabels != 0 {
		t.Errorf("dry run created %d albums and %d labels, want none",
			env.organizer.createdAlbums, env.organizer.createdLabels)
	}
}

// TestImport_albumAndLabels checks that --album and --labels are resolved once,
// creating what does not exist, and applied to imported and duplicate photos
// alike (re-importing a folder into an album is how a forgotten --album is fixed).
func TestImport_albumAndLabels(t *testing.T) {
	t.Parallel()

	env := newEnv(t, map[string]ingest.FileResult{
		"dup.jpg": {
			Filename: "dup.jpg", Status: http.StatusConflict,
			Outcome: ingest.OutcomeDuplicate, PhotoUID: "existing",
		},
	})
	env.organizer.labels["label-old"] = organize.Label{UID: "label-old", Name: "Scan"}

	root := t.TempDir()
	writeFile(t, root, "new.jpg", "new")
	writeFile(t, root, "dup.jpg", "dup")

	if _, err := env.svc.Import(t.Context(), Options{
		Root: root, Recursive: true, Album: "Old scans", Labels: []string{"scan", "1985"},
	}); err != nil {
		t.Fatalf("Import: %v", err)
	}

	if env.organizer.createdAlbums != 1 {
		t.Errorf("created %d albums, want 1 (the title did not exist)", env.organizer.createdAlbums)
	}
	// "Scan" already exists and matches case-insensitively; only "1985" is new.
	if env.organizer.createdLabels != 1 {
		t.Errorf("created %d labels, want 1", env.organizer.createdLabels)
	}
	members := env.organizer.memberUIDs("album-Old scans")
	if want := []string{"existing", "uid-new.jpg"}; !slices.Equal(members, want) {
		t.Errorf("album members = %v, want %v (the duplicate is filed too)", members, want)
	}
	if got := len(env.organizer.attached["label-old"]); got != 2 {
		t.Errorf("existing label attached to %d photos, want 2", got)
	}
}

// TestImport_albumUIDIsUsedAsIs checks that an --album that names an existing
// album by uid files photos into it instead of creating an album called after the
// uid.
func TestImport_albumUIDIsUsedAsIs(t *testing.T) {
	t.Parallel()

	env := newEnv(t, nil)
	env.organizer.albums["alb123"] = organize.Album{UID: "alb123", Title: "Holidays"}

	root := t.TempDir()
	writeFile(t, root, "a.jpg", "aaa")

	if _, err := env.svc.Import(t.Context(), Options{Root: root, Recursive: true, Album: "alb123"}); err != nil {
		t.Fatalf("Import: %v", err)
	}
	if env.organizer.createdAlbums != 0 {
		t.Errorf("created %d albums, want 0 (the uid exists)", env.organizer.createdAlbums)
	}
	if got, want := env.organizer.memberUIDs("alb123"), []string{"uid-a.jpg"}; !slices.Equal(got, want) {
		t.Errorf("album members = %v, want %v", got, want)
	}
}

// TestImport_missingRootFails checks that a root that is not a directory fails the
// run rather than importing nothing quietly.
func TestImport_missingRootFails(t *testing.T) {
	t.Parallel()

	env := newEnv(t, nil)
	root := t.TempDir()
	file := writeFile(t, root, "a.jpg", "aaa")

	if _, err := env.svc.Import(t.Context(), Options{Root: file, Recursive: true}); !errors.Is(err, ErrNotDirectory) {
		t.Errorf("Import(file) error = %v, want ErrNotDirectory", err)
	}
	if _, err := env.svc.Import(t.Context(), Options{Root: filepath.Join(root, "nope")}); err == nil {
		t.Error("Import(missing path) error = nil, want an error")
	}
	if env.runs.started != 0 {
		t.Errorf("opened %d import runs for an unusable root, want 0", env.runs.started)
	}
}

// TestImport_cancelledRunIsRecordedAsFailed checks that a cancelled run reports
// ErrInterrupted and closes its import run as failed, so the UI does not show a
// run that is stuck "running" forever.
func TestImport_cancelledRunIsRecordedAsFailed(t *testing.T) {
	t.Parallel()

	env := newEnv(t, nil)
	root := t.TempDir()
	writeFile(t, root, "a.jpg", "aaa")

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	_, err := env.svc.Import(ctx, Options{Root: root, Recursive: true})
	if !errors.Is(err, ErrInterrupted) {
		t.Fatalf("Import(cancelled) error = %v, want ErrInterrupted", err)
	}
	if env.runs.failed == nil {
		t.Fatal("cancelled run was not recorded as failed")
	}
	if env.runs.completed != nil {
		t.Error("cancelled run was completed")
	}
	if len(env.ingester.ingested()) != 0 {
		t.Errorf("cancelled run ingested %v, want nothing", env.ingester.ingested())
	}
}

// TestImport_uploaderIsPassedThrough checks the imported photos are owned by the
// user the caller named.
func TestImport_uploaderIsPassedThrough(t *testing.T) {
	t.Parallel()

	env := newEnv(t, nil)
	root := t.TempDir()
	writeFile(t, root, "a.jpg", "aaa")

	if _, err := env.svc.Import(t.Context(), Options{
		Root: root, Recursive: true, UploadedBy: "user-1",
	}); err != nil {
		t.Fatalf("Import: %v", err)
	}
	if got := env.ingester.uploaders; len(got) != 1 || got[0] != "user-1" {
		t.Errorf("uploadedBy = %v, want [user-1]", got)
	}
}

// TestClampConcurrency checks the fan-out is bounded on both ends: a
// non-positive request falls back to the small default, and an ambitious one is
// capped so a wide fan-out of thumbnail jobs cannot swamp the box.
func TestClampConcurrency(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		in   int
		want int
	}{
		{name: "zero means the default", in: 0, want: DefaultConcurrency},
		{name: "negative means the default", in: -4, want: DefaultConcurrency},
		{name: "one is honoured", in: 1, want: 1},
		{name: "in range is honoured", in: 4, want: 4},
		{name: "above the cap is clamped", in: 64, want: MaxConcurrency},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := clampConcurrency(tt.in); got != tt.want {
				t.Errorf("clampConcurrency(%d) = %d, want %d", tt.in, got, tt.want)
			}
		})
	}
}

// TestCountsToImporter checks the folder tally maps onto the import_runs shape:
// duplicates and skipped junk both land in the run's skipped bucket, and a folder
// import never updates anything.
func TestCountsToImporter(t *testing.T) {
	t.Parallel()

	got := Counts{Imported: 10, Duplicates: 3, Skipped: 4, Failed: 1}.toImporter()
	want := importer.Counts{Imported: 10, Updated: 0, Skipped: 7, Failed: 1}
	if got != want {
		t.Errorf("toImporter() = %+v, want %+v", got, want)
	}
}

// TestCountsTotal checks every bucket is part of the total, so the progress
// counter reaches exactly the number of files walked.
func TestCountsTotal(t *testing.T) {
	t.Parallel()

	if got := (Counts{Imported: 2, Duplicates: 3, Skipped: 4, Failed: 5}).Total(); got != 14 {
		t.Errorf("Total() = %d, want 14", got)
	}
}
