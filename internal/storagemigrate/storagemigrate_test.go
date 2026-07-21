package storagemigrate_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"sync"
	"testing"

	"github.com/panbotka/kukatko/internal/sidecarexport"
	"github.com/panbotka/kukatko/internal/storage"
	"github.com/panbotka/kukatko/internal/storagemigrate"
	"github.com/panbotka/kukatko/internal/thumb"
)

// gridSize is the one thumbnail size the fixtures generate, standing in for the
// partially populated cache every real library has.
const gridSize = thumb.GridSize

// memCatalogue is an in-memory photos table: the work list, the stamps, and
// nothing else. It is safe for the migrator's concurrency.
type memCatalogue struct {
	mu       sync.Mutex
	items    []storagemigrate.Item
	migrated map[string]bool
	marks    []string
	markErr  error
}

// newMemCatalogue returns a catalogue holding items, none of them migrated.
func newMemCatalogue(items ...storagemigrate.Item) *memCatalogue {
	sorted := slices.Clone(items)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].UID < sorted[j].UID })
	return &memCatalogue{items: sorted, migrated: map[string]bool{}}
}

func (c *memCatalogue) Progress(context.Context) (storagemigrate.Progress, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	progress := storagemigrate.Progress{Total: int64(len(c.items))}
	for _, item := range c.items {
		if c.migrated[item.UID] {
			progress.Migrated++
			continue
		}
		progress.PendingBytes += item.FileSize
	}
	progress.Pending = progress.Total - progress.Migrated
	return progress, nil
}

func (c *memCatalogue) PendingBatch(_ context.Context, cursor string, limit int) ([]storagemigrate.Item, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	batch := make([]storagemigrate.Item, 0, limit)
	for _, item := range c.items {
		if c.migrated[item.UID] || item.UID <= cursor {
			continue
		}
		batch = append(batch, item)
		if len(batch) == limit {
			break
		}
	}
	return batch, nil
}

func (c *memCatalogue) MarkMigrated(_ context.Context, uid string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.markErr != nil {
		return c.markErr
	}
	c.migrated[uid] = true
	c.marks = append(c.marks, uid)
	return nil
}

// isMigrated reports whether uid carries the stamp.
func (c *memCatalogue) isMigrated(uid string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.migrated[uid]
}

// unmark clears a stamp, standing in for a photo that was never migrated.
func (c *memCatalogue) unmark(uid string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.migrated, uid)
}

// brokenDestination fails every operation with the same error, which is how a
// misconfigured bucket behaves.
type brokenDestination struct {
	err   error
	mu    sync.Mutex
	calls int
}

func (d *brokenDestination) Check(context.Context) error { return d.err }

func (d *brokenDestination) Head(context.Context, string) (storage.StoredFile, error) {
	d.mu.Lock()
	d.calls++
	d.mu.Unlock()
	return storage.StoredFile{}, d.err
}

func (d *brokenDestination) Put(context.Context, io.Reader, storage.StoredFile) error {
	return d.err
}

// sidecarRejectingDestination writes everything except the sidecar, which it
// refuses with a non-systemic error — the way a corrupted sidecar upload would
// fail verification. It exists to prove that an original is not deleted until its
// sidecar is durable in the destination.
type sidecarRejectingDestination struct {
	inner storagemigrate.Destination
}

func (d sidecarRejectingDestination) Check(ctx context.Context) error {
	return d.inner.Check(ctx)
}

func (d sidecarRejectingDestination) Head(ctx context.Context, relPath string) (storage.StoredFile, error) {
	return d.inner.Head(ctx, relPath)
}

func (d sidecarRejectingDestination) Put(ctx context.Context, src io.Reader, file storage.StoredFile) error {
	if strings.HasPrefix(file.RelPath, sidecarexport.Prefix+"/") {
		return fmt.Errorf("%w: %s", storage.ErrHashMismatch, file.RelPath)
	}
	return d.inner.Put(ctx, src, file)
}

// fixture is a small library on disk: originals under sourceRoot, thumbnails
// under cacheDir, and a destination root the migration writes into.
type fixture struct {
	catalogue   *memCatalogue
	source      *storage.FS
	destination *storage.FS
	sourceRoot  string
	destRoot    string
	cacheDir    string
}

// hashOf returns the hex SHA256 of b.
func hashOf(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// newFixture builds a library of n photos, each with an original and a cached
// grid thumbnail, none of them migrated.
func newFixture(t *testing.T, n int) *fixture {
	t.Helper()

	sourceRoot, destRoot, cacheDir := t.TempDir(), t.TempDir(), t.TempDir()
	source, err := storage.NewFS(sourceRoot)
	if err != nil {
		t.Fatalf("NewFS(source): %v", err)
	}
	destination, err := storage.NewFS(destRoot)
	if err != nil {
		t.Fatalf("NewFS(destination): %v", err)
	}

	items := make([]storagemigrate.Item, 0, n)
	for i := range n {
		uid := "photo" + string(rune('a'+i))
		content := []byte("original bytes for " + uid)
		relPath := "2024/05/" + uid + ".jpg"
		writeFile(t, filepath.Join(sourceRoot, filepath.FromSlash(relPath)), content)

		hash := hashOf(content)
		writeFile(t, thumbPath(t, cacheDir, hash), []byte("thumbnail bytes for "+uid))
		items = append(items, storagemigrate.Item{
			UID: uid, FilePath: relPath, FileHash: hash,
			FileSize: int64(len(content)), FileMIME: "image/jpeg",
		})
	}
	return &fixture{
		catalogue: newMemCatalogue(items...), source: source, destination: destination,
		sourceRoot: sourceRoot, destRoot: destRoot, cacheDir: cacheDir,
	}
}

// config returns a Config over the fixture, with the defaults a test wants:
// serial, and reporting after every photo.
func (f *fixture) config() storagemigrate.Config {
	return storagemigrate.Config{
		Catalogue:   f.catalogue,
		Source:      f.source,
		Destination: f.destination,
		CacheDir:    f.cacheDir,
		Concurrency: 1,
	}
}

// run builds a migrator over cfg and runs it.
func run(t *testing.T, cfg storagemigrate.Config) (storagemigrate.Result, error) {
	t.Helper()
	migrator, err := storagemigrate.New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return migrator.Run(t.Context())
}

// writeFile writes content at absPath, creating the parent directories.
func writeFile(t *testing.T, absPath string, content []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(absPath), 0o750); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(absPath), err)
	}
	if err := os.WriteFile(absPath, content, 0o600); err != nil {
		t.Fatalf("write %s: %v", absPath, err)
	}
}

// writeSidecar writes a sidecar for the original at relPath under root, at the
// parallel sidecars/ key the exporter uses, and returns that key.
func writeSidecar(t *testing.T, root, relPath string, content []byte) string {
	t.Helper()
	key, err := sidecarexport.KeyFor(relPath)
	if err != nil {
		t.Fatalf("sidecarexport.KeyFor(%s): %v", relPath, err)
	}
	writeFile(t, filepath.Join(root, filepath.FromSlash(key)), content)
	return key
}

// thumbPath returns the absolute cache path of the grid thumbnail for hash.
func thumbPath(t *testing.T, cacheDir, hash string) string {
	t.Helper()
	rel, err := thumb.RelPath(hash, gridSize)
	if err != nil {
		t.Fatalf("thumb.RelPath: %v", err)
	}
	return filepath.Join(cacheDir, filepath.FromSlash(rel))
}

// objectKeys lists every file under root as a slash-separated key, skipping the
// backend's own temp directory. It is how a test asserts what landed.
func objectKeys(t *testing.T, root string) []string {
	t.Helper()
	var keys []string
	err := filepath.WalkDir(root, func(abs string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, relErr := filepath.Rel(root, abs)
		if relErr != nil {
			return relErr
		}
		key := filepath.ToSlash(rel)
		if entry.IsDir() {
			if key == ".tmp" {
				return filepath.SkipDir
			}
			return nil
		}
		keys = append(keys, key)
		return nil
	})
	if err != nil {
		t.Fatalf("walking %s: %v", root, err)
	}
	sort.Strings(keys)
	return keys
}

// exists reports whether a file is present at absPath.
func exists(t *testing.T, absPath string) bool {
	t.Helper()
	_, err := os.Stat(absPath)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("stat %s: %v", absPath, err)
	}
	return err == nil
}

func TestRun_movesOriginalsAndCachedThumbnails(t *testing.T) {
	t.Parallel()
	fixture := newFixture(t, 3)

	result, err := run(t, fixture.config())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if result.Photos != 3 || result.Failed != 0 {
		t.Fatalf("Run = %d photos, %d failed; want 3, 0", result.Photos, result.Failed)
	}
	if result.Objects != 6 {
		t.Errorf("uploaded %d objects, want 6 (3 originals + 3 thumbnails)", result.Objects)
	}

	// Object keys are the catalogue's paths verbatim: nothing is re-keyed.
	keys := objectKeys(t, fixture.destRoot)
	for _, item := range fixture.catalogue.items {
		if !slices.Contains(keys, item.FilePath) {
			t.Errorf("original %s did not land in the destination; got %v", item.FilePath, keys)
		}
		if !fixture.catalogue.isMigrated(item.UID) {
			t.Errorf("photo %s was moved but its row was not committed", item.UID)
		}
	}
	if got := len(keys); got != 6 {
		t.Errorf("destination holds %d objects, want 6: %v", got, keys)
	}
	// Without --delete-local nothing on the local disk is touched.
	for _, item := range fixture.catalogue.items {
		if !exists(t, filepath.Join(fixture.sourceRoot, filepath.FromSlash(item.FilePath))) {
			t.Errorf("original %s was removed without --delete-local", item.FilePath)
		}
	}
}

func TestRun_dryRunMeasuresAndChangesNothing(t *testing.T) {
	t.Parallel()
	fixture := newFixture(t, 2)
	// Sidecars count toward the estimate too, and a dry run must measure them
	// (from a cheap stat) without hashing, uploading or deleting them.
	sidecarKeys := make([]string, 0, len(fixture.catalogue.items))
	for _, item := range fixture.catalogue.items {
		sidecarKeys = append(sidecarKeys, writeSidecar(t, fixture.sourceRoot, item.FilePath, []byte("sidecar "+item.UID)))
	}
	cfg := fixture.config()
	cfg.DryRun = true
	// A dry run must not even look at the destination, let alone write to it.
	cfg.Destination = &brokenDestination{err: storage.ErrBucketNotFound}
	cfg.DeleteLocal = true

	result, err := run(t, cfg)
	if err != nil {
		t.Fatalf("Run(dry): %v", err)
	}
	if !result.DryRun || result.Photos != 2 || result.Objects != 6 {
		t.Errorf("dry run = %+v, want 2 photos and 6 objects (2 originals + 2 thumbnails + 2 sidecars)",
			result.Snapshot)
	}
	if result.Bytes == 0 {
		t.Error("a dry run must report how many bytes would move")
	}
	if result.Deleted != 0 || result.Skipped != 0 {
		t.Errorf("a dry run deleted %d and skipped %d; both must be zero", result.Deleted, result.Skipped)
	}
	if keys := objectKeys(t, fixture.destRoot); len(keys) != 0 {
		t.Errorf("a dry run wrote %v to the destination", keys)
	}
	for i, item := range fixture.catalogue.items {
		if fixture.catalogue.isMigrated(item.UID) {
			t.Errorf("a dry run committed the row of %s", item.UID)
		}
		if !exists(t, filepath.Join(fixture.sourceRoot, filepath.FromSlash(item.FilePath))) {
			t.Errorf("a dry run removed the local original %s", item.FilePath)
		}
		if !exists(t, filepath.Join(fixture.sourceRoot, filepath.FromSlash(sidecarKeys[i]))) {
			t.Errorf("a dry run removed the local sidecar %s", sidecarKeys[i])
		}
	}
}

func TestRun_isIdempotentAndSkipsObjectsTheBucketAlreadyHolds(t *testing.T) {
	t.Parallel()
	fixture := newFixture(t, 2)

	first, err := run(t, fixture.config())
	if err != nil {
		t.Fatalf("Run(first): %v", err)
	}
	if first.Objects != 4 || first.Skipped != 0 {
		t.Fatalf("first run = %d uploaded, %d skipped; want 4, 0", first.Objects, first.Skipped)
	}

	// A completed photo is skipped outright on a second run: no work at all.
	second, err := run(t, fixture.config())
	if err != nil {
		t.Fatalf("Run(second): %v", err)
	}
	if second.Photos != 0 || second.Objects != 0 {
		t.Errorf("second run redid %d photos / %d objects; want none", second.Photos, second.Objects)
	}

	// A photo whose objects landed but whose row never committed — a crash between
	// the two — re-verifies its objects and uploads nothing. This is the whole
	// reason a resumed migration stays inside R2's free Class A allowance.
	fixture.catalogue.unmark("photoa")
	third, err := run(t, fixture.config())
	if err != nil {
		t.Fatalf("Run(third): %v", err)
	}
	if third.Photos != 1 {
		t.Errorf("third run migrated %d photos, want 1", third.Photos)
	}
	if third.Objects != 0 || third.Skipped != 2 {
		t.Errorf("third run uploaded %d and skipped %d; want 0 uploaded, 2 skipped",
			third.Objects, third.Skipped)
	}
	if !fixture.catalogue.isMigrated("photoa") {
		t.Error("the resumed photo's row was not committed")
	}
}

func TestRun_deleteLocalRemovesOriginalsOnlyAfterTheRowIsCommitted(t *testing.T) {
	t.Parallel()
	fixture := newFixture(t, 2)
	cfg := fixture.config()
	cfg.DeleteLocal = true

	result, err := run(t, cfg)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if result.Deleted != 2 {
		t.Errorf("removed %d local originals, want 2", result.Deleted)
	}
	for _, item := range fixture.catalogue.items {
		if exists(t, filepath.Join(fixture.sourceRoot, filepath.FromSlash(item.FilePath))) {
			t.Errorf("original %s survived --delete-local", item.FilePath)
		}
		if !fixture.catalogue.isMigrated(item.UID) {
			t.Errorf("original %s was deleted but its row was never committed", item.FilePath)
		}
		// The thumbnail cache is regenerable and is not what this migration empties.
		if !exists(t, thumbPath(t, fixture.cacheDir, item.FileHash)) {
			t.Errorf("the cached thumbnail of %s was removed", item.UID)
		}
	}
}

func TestRun_movesTheMetadataSidecarWithTheOriginal(t *testing.T) {
	t.Parallel()
	fixture := newFixture(t, 2)
	// Every photo carries a sidecar on local disk — the disaster-recovery artifact
	// a rebuild reads the catalogue back out of. The migration must carry it into
	// the destination rather than strand it on the disk it exists to empty.
	sidecarKeys := make([]string, 0, len(fixture.catalogue.items))
	for _, item := range fixture.catalogue.items {
		sidecarKeys = append(sidecarKeys, writeSidecar(t, fixture.sourceRoot, item.FilePath, []byte("sidecar "+item.UID)))
	}

	result, err := run(t, fixture.config())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	// Three objects per photo now: the original, its thumbnail, and its sidecar.
	if result.Objects != 6 {
		t.Errorf("uploaded %d objects, want 6 (2 originals + 2 thumbnails + 2 sidecars)", result.Objects)
	}
	keys := objectKeys(t, fixture.destRoot)
	for i, item := range fixture.catalogue.items {
		if !slices.Contains(keys, sidecarKeys[i]) {
			t.Errorf("sidecar %s of %s did not land in the destination; got %v", sidecarKeys[i], item.UID, keys)
		}
		// Without --delete-local the local sidecar, like the local original, stays.
		if !exists(t, filepath.Join(fixture.sourceRoot, filepath.FromSlash(sidecarKeys[i]))) {
			t.Errorf("local sidecar of %s was removed without --delete-local", item.UID)
		}
	}
}

func TestRun_deleteLocalRemovesTheSidecarAlongsideTheOriginal(t *testing.T) {
	t.Parallel()
	fixture := newFixture(t, 2)
	sidecarKeys := make([]string, 0, len(fixture.catalogue.items))
	for _, item := range fixture.catalogue.items {
		sidecarKeys = append(sidecarKeys, writeSidecar(t, fixture.sourceRoot, item.FilePath, []byte("sidecar "+item.UID)))
	}
	cfg := fixture.config()
	cfg.DeleteLocal = true

	result, err := run(t, cfg)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	// The Deleted tally counts originals, not the sidecars removed alongside them.
	if result.Deleted != 2 {
		t.Errorf("removed %d local originals, want 2", result.Deleted)
	}
	for i, item := range fixture.catalogue.items {
		// The sidecar is durable in the destination before its local copy is gone.
		if !exists(t, filepath.Join(fixture.destRoot, filepath.FromSlash(sidecarKeys[i]))) {
			t.Errorf("sidecar of %s was not in the destination", item.UID)
		}
		// And the local copies — both on the disk this migration empties — are gone.
		if exists(t, filepath.Join(fixture.sourceRoot, filepath.FromSlash(sidecarKeys[i]))) {
			t.Errorf("local sidecar of %s survived --delete-local", item.UID)
		}
		if exists(t, filepath.Join(fixture.sourceRoot, filepath.FromSlash(item.FilePath))) {
			t.Errorf("local original of %s survived --delete-local", item.UID)
		}
	}
}

func TestRun_keepsTheOriginalWhenTheSidecarFailsVerification(t *testing.T) {
	t.Parallel()
	fixture := newFixture(t, 1)
	item := fixture.catalogue.items[0]
	sidecarKey := writeSidecar(t, fixture.sourceRoot, item.FilePath, []byte("sidecar "+item.UID))

	cfg := fixture.config()
	cfg.DeleteLocal = true
	// The destination refuses the sidecar, as a corrupted upload would. The
	// original must not be deleted while its sidecar is not durable there.
	cfg.Destination = sidecarRejectingDestination{inner: fixture.destination}

	result, err := run(t, cfg)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if result.Failed != 1 || len(result.Failures) != 1 {
		t.Fatalf("Run = %d failures (%v), want exactly 1", result.Failed, result.Failures)
	}
	if !errors.Is(result.Failures[0].Err, storage.ErrHashMismatch) {
		t.Errorf("failure = %v, want a hash mismatch on the sidecar", result.Failures[0].Err)
	}
	if fixture.catalogue.isMigrated(item.UID) {
		t.Error("a photo whose sidecar failed verification was committed as migrated")
	}
	if result.Deleted != 0 {
		t.Errorf("deleted %d local originals though the sidecar never became durable", result.Deleted)
	}
	if !exists(t, filepath.Join(fixture.sourceRoot, filepath.FromSlash(item.FilePath))) {
		t.Error("the local original was deleted before its sidecar was durable in the destination")
	}
	if !exists(t, filepath.Join(fixture.sourceRoot, filepath.FromSlash(sidecarKey))) {
		t.Error("the local sidecar was deleted though its upload failed verification")
	}
}

func TestRun_neverDeletesAnOriginalThatFailedVerification(t *testing.T) {
	t.Parallel()
	fixture := newFixture(t, 3)
	// photob's catalogue digest does not describe its bytes: a corrupted local
	// file, or a row that was always wrong. Its upload must be refused.
	corrupt := &fixture.catalogue.items[1]
	corrupt.FileHash = hashOf([]byte("a digest describing nothing on this disk"))

	cfg := fixture.config()
	cfg.DeleteLocal = true
	result, err := run(t, cfg)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	if result.Failed != 1 || len(result.Failures) != 1 {
		t.Fatalf("Run = %d failures (%v), want exactly 1", result.Failed, result.Failures)
	}
	failure := result.Failures[0]
	if failure.UID != corrupt.UID || !errors.Is(failure.Err, storage.ErrHashMismatch) {
		t.Errorf("failure = %+v, want a hash mismatch on %s", failure, corrupt.UID)
	}

	// The three invariants that make this command safe to run on 120 GB of
	// irreplaceable photographs.
	if fixture.catalogue.isMigrated(corrupt.UID) {
		t.Error("a photo that failed verification was committed")
	}
	corruptPath := filepath.Join(fixture.sourceRoot, filepath.FromSlash(corrupt.FilePath))
	if !exists(t, corruptPath) {
		t.Error("a local original whose upload failed verification was deleted")
	}
	if slices.Contains(objectKeys(t, fixture.destRoot), corrupt.FilePath) {
		t.Error("an object that failed verification was left in the destination")
	}

	// And the run carried on: one bad photo does not abort a job of a hundred
	// thousand.
	if result.Photos != 2 {
		t.Errorf("migrated %d photos around the failure, want 2", result.Photos)
	}
}

func TestRun_stopsImmediatelyOnASystemicDestinationError(t *testing.T) {
	t.Parallel()
	fixture := newFixture(t, 50)
	broken := &brokenDestination{err: storage.ErrBucketNotFound}
	cfg := fixture.config()
	cfg.Destination = broken

	result, err := run(t, cfg)
	if !errors.Is(err, storage.ErrBucketNotFound) {
		t.Fatalf("Run = %v, want ErrBucketNotFound", err)
	}
	// Check runs before any photo does, so nothing was even attempted.
	if broken.calls != 0 {
		t.Errorf("the destination was used %d times after a failed preflight check", broken.calls)
	}
	if result.Photos != 0 || result.Failed != 0 {
		t.Errorf("a preflight failure recorded %d photos and %d failures", result.Photos, result.Failed)
	}
}

func TestRun_stopsMidRunOnASystemicErrorRatherThanFailingEveryPhoto(t *testing.T) {
	t.Parallel()
	fixture := newFixture(t, 50)
	// Check passes, then every object fails as if the token were revoked mid-run.
	cfg := fixture.config()
	cfg.Destination = &lateBrokenDestination{err: storage.ErrBucketNotFound}

	result, err := run(t, cfg)
	if !errors.Is(err, storage.ErrBucketNotFound) {
		t.Fatalf("Run = %v, want ErrBucketNotFound", err)
	}
	if result.Failed != 0 {
		t.Errorf("a systemic error was collected as %d per-photo failures; it must abort instead",
			result.Failed)
	}
}

// lateBrokenDestination passes its preflight check and then fails everything, as
// a revoked token would.
type lateBrokenDestination struct{ err error }

func (lateBrokenDestination) Check(context.Context) error { return nil }

func (d lateBrokenDestination) Head(context.Context, string) (storage.StoredFile, error) {
	return storage.StoredFile{}, d.err
}

func (d lateBrokenDestination) Put(context.Context, io.Reader, storage.StoredFile) error {
	return d.err
}

func TestRun_resumesAfterAnInterruptionWithoutRedoingCommittedWork(t *testing.T) {
	t.Parallel()
	fixture := newFixture(t, 5)

	// Kill the run after its second photo, the way a signal would.
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	cfg := fixture.config()
	cfg.Report = func(snapshot storagemigrate.Snapshot) {
		if snapshot.Photos == 2 {
			cancel()
		}
	}
	migrator, err := storagemigrate.New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	interrupted, err := migrator.Run(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("interrupted Run = %v, want context.Canceled", err)
	}
	if interrupted.Photos != 2 {
		t.Fatalf("interrupted run committed %d photos, want 2", interrupted.Photos)
	}

	// The resumed run picks up exactly the photos the first one never committed.
	resumed, err := run(t, fixture.config())
	if err != nil {
		t.Fatalf("Run(resumed): %v", err)
	}
	if resumed.Photos != 3 {
		t.Errorf("resumed run migrated %d photos, want the 3 left over", resumed.Photos)
	}
	if resumed.Objects != 6 {
		t.Errorf("resumed run uploaded %d objects, want 6 — it must not redo committed photos",
			resumed.Objects)
	}
	if got := len(objectKeys(t, fixture.destRoot)); got != 10 {
		t.Errorf("destination holds %d objects, want 10 — every object exactly once", got)
	}
	if got := len(fixture.catalogue.marks); got != 5 {
		t.Errorf("%d rows were committed across both runs, want 5", got)
	}
}

func TestRun_reportsProgressAndAnEstimateOfWhatRemains(t *testing.T) {
	t.Parallel()
	fixture := newFixture(t, 4)
	cfg := fixture.config()

	var snapshots []storagemigrate.Snapshot
	cfg.Report = func(snapshot storagemigrate.Snapshot) {
		snapshots = append(snapshots, snapshot)
	}
	result, err := run(t, cfg)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	if len(snapshots) != 4 {
		t.Fatalf("got %d progress reports, want one per photo", len(snapshots))
	}
	first, last := snapshots[0], snapshots[len(snapshots)-1]
	if first.PhotosRemaining != 3 || last.PhotosRemaining != 0 {
		t.Errorf("remaining went %d → %d, want 3 → 0", first.PhotosRemaining, last.PhotosRemaining)
	}
	if first.BytesRemaining <= last.BytesRemaining {
		t.Error("the remaining-bytes estimate must fall as originals move")
	}
	if last.Bytes != result.Bytes || last.Elapsed <= 0 {
		t.Errorf("the last snapshot %+v disagrees with the result", last)
	}
}

func TestRun_failsThePhotoWhenItsOriginalIsGoneFromDisk(t *testing.T) {
	t.Parallel()
	fixture := newFixture(t, 2)
	missing := fixture.catalogue.items[0]
	if err := os.Remove(filepath.Join(fixture.sourceRoot, filepath.FromSlash(missing.FilePath))); err != nil {
		t.Fatalf("removing the fixture original: %v", err)
	}

	result, err := run(t, fixture.config())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if result.Failed != 1 || result.Photos != 1 {
		t.Fatalf("Run = %d failed, %d done; want 1, 1", result.Failed, result.Photos)
	}
	if !errors.Is(result.Failures[0].Err, os.ErrNotExist) {
		t.Errorf("failure = %v, want os.ErrNotExist", result.Failures[0].Err)
	}
	if fixture.catalogue.isMigrated(missing.UID) {
		t.Error("a photo whose original is gone was committed as migrated")
	}
}

func TestRun_uncommittedRowLeavesTheOriginalOnDisk(t *testing.T) {
	t.Parallel()
	fixture := newFixture(t, 1)
	cfg := fixture.config()
	cfg.DeleteLocal = true
	fixture.catalogue.markErr = errors.New("the database went away")

	result, err := run(t, cfg)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if result.Failed != 1 || result.Deleted != 0 {
		t.Fatalf("Run = %d failed, %d deleted; want 1, 0", result.Failed, result.Deleted)
	}
	original := filepath.Join(fixture.sourceRoot, filepath.FromSlash(fixture.catalogue.items[0].FilePath))
	if !exists(t, original) {
		t.Error("an original was deleted before its row pointed at the object")
	}
}

func TestRun_skipsThumbnailSizesThatWereNeverGenerated(t *testing.T) {
	t.Parallel()
	fixture := newFixture(t, 1)
	item := fixture.catalogue.items[0]
	if err := os.Remove(thumbPath(t, fixture.cacheDir, item.FileHash)); err != nil {
		t.Fatalf("removing the fixture thumbnail: %v", err)
	}

	result, err := run(t, fixture.config())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	// An empty cache is the normal state of a library; it is not an error, and
	// nothing is rendered to fill it.
	if result.Objects != 1 || result.Photos != 1 {
		t.Errorf("Run = %d objects for %d photos; want the original alone",
			result.Objects, result.Photos)
	}
	keys := objectKeys(t, fixture.destRoot)
	for _, key := range keys {
		if strings.HasPrefix(key, "thumb/") {
			t.Errorf("a thumbnail %s was invented for a photo that had none", key)
		}
	}
}

func TestRun_overwritesAHalfWrittenObjectFromAKilledRun(t *testing.T) {
	t.Parallel()
	fixture := newFixture(t, 1)
	item := fixture.catalogue.items[0]
	// A truncated object at the right key, as a killed upload might have left.
	writeFile(t, filepath.Join(fixture.destRoot, filepath.FromSlash(item.FilePath)), []byte("trunc"))

	result, err := run(t, fixture.config())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if result.Objects != 2 || result.Skipped != 0 {
		t.Errorf("Run = %d uploaded, %d skipped; a wrong-sized object must be rewritten",
			result.Objects, result.Skipped)
	}
	head, err := fixture.destination.Head(t.Context(), item.FilePath)
	if err != nil {
		t.Fatalf("Head: %v", err)
	}
	if head.Hash != item.FileHash {
		t.Errorf("the destination still holds the truncated object: %+v", head)
	}
}

func TestNew_requiresItsCollaborators(t *testing.T) {
	t.Parallel()
	fixture := newFixture(t, 0)

	full := fixture.config()
	cases := map[string]func(*storagemigrate.Config){
		"no catalogue":   func(c *storagemigrate.Config) { c.Catalogue = nil },
		"no source":      func(c *storagemigrate.Config) { c.Source = nil },
		"no destination": func(c *storagemigrate.Config) { c.Destination = nil },
		"no cache dir":   func(c *storagemigrate.Config) { c.CacheDir = "" },
	}
	for name, breakIt := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			cfg := full
			breakIt(&cfg)
			if _, err := storagemigrate.New(cfg); !errors.Is(err, storagemigrate.ErrIncompleteConfig) {
				t.Errorf("New(%s) = %v, want ErrIncompleteConfig", name, err)
			}
		})
	}

	if _, err := storagemigrate.New(full); err != nil {
		t.Errorf("New(complete) = %v, want nil", err)
	}
}

func TestThumbnailKeysAreTheCachePathsVerbatim(t *testing.T) {
	t.Parallel()
	fixture := newFixture(t, 1)

	if _, err := run(t, fixture.config()); err != nil {
		t.Fatalf("Run: %v", err)
	}
	want, err := thumb.RelPath(fixture.catalogue.items[0].FileHash, gridSize)
	if err != nil {
		t.Fatalf("thumb.RelPath: %v", err)
	}
	// The Worker resolves a thumbnail URL to exactly this key; if the migration
	// wrote it anywhere else, every tile in the library would 404 after cutover.
	if !slices.Contains(objectKeys(t, fixture.destRoot), want) {
		t.Errorf("the thumbnail did not land at %s", path.Clean(want))
	}
}
