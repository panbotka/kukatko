// Package storagemigrate moves a library's originals and their cached
// thumbnails off the local disk and into the object store, one photo at a time,
// safely enough that the process may be killed at any instant and simply started
// again.
//
// # The safety rule
//
// Every step of a photo happens in one order and no other: upload each of its
// objects — the original, its metadata sidecar, its cached thumbnails — read
// each back and check it holds the size and the SHA256 the catalogue promised,
// commit the row, and only then — and only when asked to — remove the local
// original and its sidecar. Nothing about a failure is silent and nothing about
// it is destructive: an object that does not verify is not committed, and an
// original whose row is not committed is not deleted. There is no path through
// this package on which bytes exist only in a place that has not answered for
// them.
//
// # Resuming
//
// The cursor is photos.storage_migrated_at: NULL means "not known to be in the
// object store". That is the high-watermark rule the importers follow — only a
// verified, committed step advances the cursor, so a crash retries work rather
// than losing or skipping it — kept per row rather than per run, because under
// bounded concurrency photo N+1 routinely finishes before photo N and a scalar
// watermark would have to lie about one of them. An interrupted run therefore
// resumes exactly where it stopped, and a photo already in the bucket costs one
// cheap metadata request on the second run, not a second upload.
//
// # Cost
//
// R2 bills a Class A operation per write and includes a million of them a month.
// A migration that re-uploaded everything on every run would blow through that;
// this one asks the destination what it already holds and uploads only what is
// missing or wrong, so a resumed or repeated run is nearly free.
package storagemigrate

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"slices"
	"sync"
	"time"

	"golang.org/x/sync/errgroup"

	"github.com/panbotka/kukatko/internal/sidecarexport"
	"github.com/panbotka/kukatko/internal/storage"
)

// Defaults for the knobs a caller may leave unset. Concurrency is deliberately
// low: this job exists to run on a small VPS, where a wide fan-out of uploads
// buys little bandwidth and costs memory and file descriptors that the rest of
// the process needs.
const (
	// DefaultConcurrency is the number of photos migrated in parallel.
	DefaultConcurrency = 2
	// DefaultBatchSize is how many pending photos are read from the catalogue at
	// once.
	DefaultBatchSize = 200
	// DefaultReportEvery is the progress-reporting interval the CLI uses. A job
	// that runs for hours without saying anything is indistinguishable from one
	// that has hung.
	DefaultReportEvery = 15 * time.Second
)

// fallbackMIME is the media type given to an original whose catalogue row
// records none.
const fallbackMIME = "application/octet-stream"

// ErrIncompleteConfig indicates New was called without one of the collaborators
// the migration cannot run without.
var ErrIncompleteConfig = errors.New(
	"storagemigrate: catalogue, source, destination and cache directory are required")

// Destination is the object store the migration writes into. It is satisfied by
// *storage.R2 — and by *storage.FS, which is what lets the whole pipeline be
// exercised without a bucket.
type Destination interface {
	// Check reports whether the destination is reachable and usable.
	Check(ctx context.Context) error
	// Head returns the identity of the object at relPath, or an error wrapping
	// os.ErrNotExist when there is none.
	Head(ctx context.Context, relPath string) (storage.StoredFile, error)
	// Put writes src at file.RelPath with the identity file declares.
	Put(ctx context.Context, src io.Reader, file storage.StoredFile) error
}

// Source is the local store the originals and their sidecars are read from and —
// only once their objects are verified and their rows committed — removed from.
type Source interface {
	// Open opens the file at relPath for reading.
	Open(ctx context.Context, relPath string) (io.ReadCloser, error)
	// Stat reports the size of the file at relPath without reading its content, or
	// an error wrapping os.ErrNotExist when the source holds none. Whether a photo
	// has a sidecar at all — and the sidecar's size for the dry-run estimate — is
	// answered from here, cheaply enough to ask for every photo.
	Stat(ctx context.Context, relPath string) (os.FileInfo, error)
	// Delete removes the file at relPath.
	Delete(ctx context.Context, relPath string) error
}

// Catalogue is the photos table as this package needs it: the work list, and the
// stamp that retires a photo from it.
type Catalogue interface {
	// Progress returns the catalogue-wide state of the migration.
	Progress(ctx context.Context) (Progress, error)
	// PendingBatch returns the next page of photos not yet in the object store.
	PendingBatch(ctx context.Context, cursor string, limit int) ([]Item, error)
	// MarkMigrated stamps a photo as verified present in the object store.
	MarkMigrated(ctx context.Context, uid string) error
}

// compile-time assertions that the real backends satisfy the narrow interfaces
// this package consumes.
var (
	_ Destination = (*storage.R2)(nil)
	_ Destination = (*storage.FS)(nil)
	_ Source      = (*storage.FS)(nil)
)

// Snapshot is a point-in-time tally of a run, handed to the Report callback
// while it works and returned as part of the Result when it stops.
type Snapshot struct {
	// Photos is how many photos are done: every object verified, row committed.
	Photos int64
	// Objects is how many objects were uploaded, originals and thumbnails alike.
	Objects int64
	// Bytes is how many bytes those uploads transferred.
	Bytes int64
	// Skipped is how many objects the destination already held, byte for byte,
	// and were therefore not uploaded again.
	Skipped int64
	// Deleted is how many local originals were removed (always zero unless
	// DeleteLocal is set).
	Deleted int64
	// Failed is how many photos failed. Their rows are untouched and their local
	// originals are still on disk.
	Failed int64
	// PhotosRemaining estimates how many photos this run has not reached yet.
	PhotosRemaining int64
	// BytesRemaining estimates how many bytes of originals it has not reached
	// yet. Thumbnails are not in the estimate; only the local cache knows how
	// many of those exist.
	BytesRemaining int64
	// Elapsed is how long the run has been going.
	Elapsed time.Duration
}

// Failure is one photo the run could not move. The photo is untouched: its row
// carries no stamp, its local original is still there, and the next run will try
// it again.
//
// One case is reported here without being a lost photo: an original whose
// objects verified and whose row committed, but which DeleteLocal could not then
// remove. Its bytes are safely in the object store and the next run will skip
// it; only the local copy lingers, and an operator should know.
type Failure struct {
	// UID identifies the photo.
	UID string
	// FilePath is its original's path, the most useful thing to go and look at.
	FilePath string
	// Err is what went wrong.
	Err error
}

// String renders the failure as one line for an operator's terminal.
func (f Failure) String() string {
	return fmt.Sprintf("%s (%s): %v", f.UID, f.FilePath, f.Err)
}

// Result is everything a finished run has to say. Failures is the point: a job
// of this length must not abort on the first unreadable file, so per-photo
// failures are collected here and reported at the end.
type Result struct {
	Snapshot
	// Start is the catalogue state the run began from.
	Start Progress
	// Failures lists every photo that did not make it, in completion order.
	Failures []Failure
	// DryRun reports whether the run only measured. A dry run's Objects and Bytes
	// are what *would* move; nothing was uploaded, committed or deleted.
	DryRun bool
}

// Config assembles a Migrator. Catalogue, Source, Destination and CacheDir are
// required; the rest have defaults.
type Config struct {
	// Catalogue supplies the work list and takes the commits.
	Catalogue Catalogue
	// Source reads the local originals, and deletes them when DeleteLocal is set.
	Source Source
	// Destination receives the objects. It is not touched at all by a dry run.
	Destination Destination
	// CacheDir is the derived-artifact cache root (storage.cache_path), under
	// which the thumbnails to publish are found.
	CacheDir string
	// Concurrency bounds how many photos are in flight at once; non-positive
	// means DefaultConcurrency.
	Concurrency int
	// BatchSize bounds how many pending photos are read at once; non-positive
	// means DefaultBatchSize.
	BatchSize int
	// DryRun measures what would move and changes nothing, anywhere.
	DryRun bool
	// DeleteLocal removes each local original — and its metadata sidecar — once
	// every object of the photo is verified in the destination and its row is
	// committed. Both sit under the originals root this migration exists to empty.
	// Cached thumbnails are never removed: they are regenerable from the original,
	// and the cache they sit in is not the disk this migration empties.
	DeleteLocal bool
	// ReportEvery throttles the Report callback. Non-positive reports every photo.
	ReportEvery time.Duration
	// Report receives progress while the run works. It is never called
	// concurrently with itself, so it may write to a terminal without locking. A
	// nil Report discards the progress.
	Report func(Snapshot)
}

// Migrator moves photos into the object store. Build one with New and call Run
// once; it is safe for the internal concurrency it creates and for nothing else.
type Migrator struct {
	cfg Config

	// mu guards everything below it: the workers finish out of order and all of
	// them tally, fail and report.
	mu         sync.Mutex
	tally      Snapshot
	failures   []Failure
	movedBytes int64
	started    time.Time
	lastReport time.Time
	start      Progress

	// reportMu serialises the Report callback without holding mu across it, so a
	// slow reporter cannot stall the workers and two workers finishing at once
	// cannot interleave a line of output.
	reportMu sync.Mutex
}

// New validates cfg, fills in the defaults, and returns a Migrator. It returns
// ErrIncompleteConfig when a required collaborator is missing.
func New(cfg Config) (*Migrator, error) {
	if cfg.Catalogue == nil || cfg.Source == nil || cfg.Destination == nil || cfg.CacheDir == "" {
		return nil, ErrIncompleteConfig
	}
	if cfg.Concurrency <= 0 {
		cfg.Concurrency = DefaultConcurrency
	}
	if cfg.BatchSize <= 0 {
		cfg.BatchSize = DefaultBatchSize
	}
	if cfg.Report == nil {
		cfg.Report = func(Snapshot) {}
	}
	return &Migrator{cfg: cfg}, nil
}

// Run migrates every photo not yet confirmed in the object store and returns
// what happened. Per-photo failures do not stop it; they are collected into
// Result.Failures and the run continues. Two things do stop it, and both return
// a non-nil error alongside the partial Result: a cancelled context, and a
// systemic destination error (bad credentials, missing bucket) that would
// otherwise repeat itself once per photo for the next several hours.
//
// A dry run never touches the destination — it does not even check that it
// exists — and never writes to the catalogue or the disk.
func (m *Migrator) Run(ctx context.Context) (Result, error) {
	progress, err := m.cfg.Catalogue.Progress(ctx)
	if err != nil {
		return Result{}, err //nolint:wrapcheck // Catalogue is this package's own interface; Store already names the failure.
	}
	m.begin(progress)

	if !m.cfg.DryRun {
		if err := m.cfg.Destination.Check(ctx); err != nil {
			return m.result(), fmt.Errorf("storagemigrate: destination unusable: %w", err)
		}
	}
	if err := m.walk(ctx); err != nil {
		return m.result(), err
	}
	return m.result(), nil
}

// begin stamps the run's start and remembers the work it set out to do, which is
// what every later "remaining" estimate is measured against.
func (m *Migrator) begin(progress Progress) {
	now := time.Now()
	m.mu.Lock()
	defer m.mu.Unlock()
	m.start = progress
	m.started = now
	m.lastReport = now
}

// walk pages through the pending photos, migrating each batch before asking for
// the next. The cursor advances past every photo the batch handed out, failures
// included, so a photo that cannot be moved is retried by the next run rather
// than by the next batch of this one.
func (m *Migrator) walk(ctx context.Context) error {
	cursor := ""
	for {
		items, err := m.cfg.Catalogue.PendingBatch(ctx, cursor, m.cfg.BatchSize)
		if err != nil {
			return err //nolint:wrapcheck // as in Run: the Catalogue implementation names its own failure.
		}
		if len(items) == 0 {
			return nil
		}
		if err := m.runBatch(ctx, items); err != nil {
			return err
		}
		cursor = items[len(items)-1].UID
	}
}

// runBatch migrates one batch with bounded concurrency. Only an abort — a
// cancelled context or a systemic destination error — comes back as an error;
// the group's context then cancels the photos still in flight.
func (m *Migrator) runBatch(ctx context.Context, items []Item) error {
	group, groupCtx := errgroup.WithContext(ctx)
	group.SetLimit(m.cfg.Concurrency)
	for _, item := range items {
		group.Go(func() error { return m.handle(groupCtx, item) })
	}
	if err := group.Wait(); err != nil {
		return fmt.Errorf("storagemigrate: aborting run: %w", err)
	}
	return nil
}

// handle migrates one photo and decides what its outcome means for the run: an
// ordinary failure is collected and the run goes on, while a cancellation or a
// destination that cannot be used at all stops everything.
//
// The context is checked up front rather than left to the backends: a store that
// reads a local file does not necessarily notice a cancellation, and a run that
// was told to stop must not quietly work through the rest of its batch.
func (m *Migrator) handle(ctx context.Context, item Item) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("storagemigrate: %s: %w", item.UID, err)
	}
	err := m.migrateOne(ctx, item)
	switch {
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		return err
	case err != nil && storage.IsSystemic(err):
		return err
	}
	m.finish(item, err)
	return nil
}

// migrateOne performs the whole ordered ritual for one photo: transfer every
// object — the original, its sidecar, its cached thumbnails — commit the row,
// then — last, and only if asked — remove the local original and its sidecar. A
// dry run stops after measuring.
func (m *Migrator) migrateOne(ctx context.Context, item Item) error {
	objects, err := m.plan(ctx, item)
	if err != nil {
		return err
	}
	if m.cfg.DryRun {
		m.measure(objects)
		return nil
	}
	for _, obj := range objects {
		if err := m.transfer(ctx, obj); err != nil {
			return err
		}
	}
	if err := m.cfg.Catalogue.MarkMigrated(ctx, item.UID); err != nil {
		return err //nolint:wrapcheck // as in Run: the Catalogue implementation names its own failure.
	}
	if m.cfg.DeleteLocal {
		return m.deleteLocal(ctx, item)
	}
	return nil
}

// transfer puts one object into the destination and checks that it landed,
// skipping the upload when the destination already holds those exact bytes. That
// skip is what makes a resumed run cheap: a HEAD is a Class B operation, of which
// R2 gives ten million a month, while the PUT it avoids is a Class A.
func (m *Migrator) transfer(ctx context.Context, obj object) error {
	digest, err := obj.digest(ctx)
	if err != nil {
		return err
	}
	want := obj.stored(digest)

	present, err := m.present(ctx, want)
	if err != nil {
		return err
	}
	if present {
		m.record(func(t *Snapshot) { t.Skipped++ })
		return nil
	}
	if err := m.upload(ctx, obj, want); err != nil {
		return err
	}
	if err := m.verify(ctx, want); err != nil {
		return err
	}
	m.record(func(t *Snapshot) {
		t.Objects++
		t.Bytes += want.Size
	})
	return nil
}

// upload streams the object's bytes into the destination, closing the source
// reader on every path.
func (m *Migrator) upload(ctx context.Context, obj object, want storage.StoredFile) error {
	src, err := obj.open(ctx)
	if err != nil {
		return err
	}
	putErr := m.cfg.Destination.Put(ctx, src, want)
	closeErr := src.Close()
	if putErr != nil {
		return fmt.Errorf("storagemigrate: uploading %s: %w", want.RelPath, putErr)
	}
	if closeErr != nil {
		return fmt.Errorf("storagemigrate: closing %s: %w", want.RelPath, closeErr)
	}
	return nil
}

// present reports whether the destination already holds exactly the object want
// describes. Anything else — absent, a different length, a different digest, or
// no digest at all because something other than this application wrote it —
// counts as absent, and the object is written again over the top of it.
func (m *Migrator) present(ctx context.Context, want storage.StoredFile) (bool, error) {
	got, err := m.cfg.Destination.Head(ctx, want.RelPath)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("storagemigrate: heading %s: %w", want.RelPath, err)
	}
	return got.Size == want.Size && got.Hash == want.Hash, nil
}

// verify reads the freshly written object's identity back out of the destination
// and refuses it unless the store agrees about both its length and its content.
// It transfers no bytes: the size and the digest come from object metadata.
func (m *Migrator) verify(ctx context.Context, want storage.StoredFile) error {
	got, err := m.cfg.Destination.Head(ctx, want.RelPath)
	if err != nil {
		return fmt.Errorf("storagemigrate: verifying %s: %w", want.RelPath, err)
	}
	if got.Size != want.Size {
		return fmt.Errorf("%w: %s: stored %d bytes, expected %d",
			storage.ErrSizeMismatch, want.RelPath, got.Size, want.Size)
	}
	if got.Hash != want.Hash {
		return fmt.Errorf("%w: %s: stored digest %q, expected %q",
			storage.ErrHashMismatch, want.RelPath, got.Hash, want.Hash)
	}
	return nil
}

// deleteLocal removes the photo's local original and then its local sidecar. It
// is reached only after every object of that photo — the original, its sidecar,
// any cached thumbnails — verified in the destination and the row was committed,
// so both the original and the sidecar are provably somewhere else first. The
// sidecar goes too because it sits under the very originals root this migration
// exists to empty; the cached thumbnails do not, being regenerable and living in
// a separate cache, so they are left alone. A file already gone is not an error.
func (m *Migrator) deleteLocal(ctx context.Context, item Item) error {
	if err := m.deleteLocalOriginal(ctx, item); err != nil {
		return err
	}
	return m.deleteLocalSidecar(ctx, item)
}

// deleteLocalOriginal removes the photo's local original, tallying it as deleted.
// An original already gone is not an error.
func (m *Migrator) deleteLocalOriginal(ctx context.Context, item Item) error {
	if err := m.cfg.Source.Delete(ctx, item.FilePath); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("storagemigrate: removing local original %s: %w", item.FilePath, err)
	}
	m.record(func(t *Snapshot) { t.Deleted++ })
	return nil
}

// deleteLocalSidecar removes the photo's local sidecar, which by now is durable
// in the destination. It is not counted in the Deleted tally — that is a count
// of originals — and a photo with no sidecar, or one already removed, is not an
// error.
func (m *Migrator) deleteLocalSidecar(ctx context.Context, item Item) error {
	key, err := sidecarexport.KeyFor(item.FilePath)
	if err != nil {
		return fmt.Errorf("storagemigrate: sidecar key for %s: %w", item.FilePath, err)
	}
	if err := m.cfg.Source.Delete(ctx, key); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("storagemigrate: removing local sidecar %s: %w", key, err)
	}
	return nil
}

// measure tallies what a photo's objects would cost, for a dry run.
func (m *Migrator) measure(objects []object) {
	m.record(func(t *Snapshot) {
		for _, obj := range objects {
			t.Objects++
			t.Bytes += obj.size
		}
	})
}

// record applies an update to the shared tally under the lock.
func (m *Migrator) record(update func(*Snapshot)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	update(&m.tally)
}

// finish records how one photo ended and reports progress when the interval has
// elapsed. A failed photo still counts against the remaining estimate: this run
// is not going to reach it again.
func (m *Migrator) finish(item Item, failure error) {
	now := time.Now()
	m.mu.Lock()
	if failure != nil {
		m.tally.Failed++
		m.failures = append(m.failures, Failure{UID: item.UID, FilePath: item.FilePath, Err: failure})
	} else {
		m.tally.Photos++
		m.movedBytes += item.FileSize
	}
	due := m.cfg.ReportEvery <= 0 || now.Sub(m.lastReport) >= m.cfg.ReportEvery
	if due {
		m.lastReport = now
	}
	snapshot := m.snapshotLocked(now)
	m.mu.Unlock()

	if due {
		m.reportMu.Lock()
		m.cfg.Report(snapshot)
		m.reportMu.Unlock()
	}
}

// snapshotLocked returns the current tally with the derived fields filled in.
// The caller must hold m.mu.
func (m *Migrator) snapshotLocked(now time.Time) Snapshot {
	snapshot := m.tally
	snapshot.Elapsed = now.Sub(m.started)
	snapshot.PhotosRemaining = max(0, m.start.Pending-snapshot.Photos-snapshot.Failed)
	snapshot.BytesRemaining = max(0, m.start.PendingBytes-m.movedBytes)
	return snapshot
}

// result assembles the run's final report.
func (m *Migrator) result() Result {
	m.mu.Lock()
	defer m.mu.Unlock()
	return Result{
		Snapshot: m.snapshotLocked(time.Now()),
		Start:    m.start,
		Failures: slices.Clone(m.failures),
		DryRun:   m.cfg.DryRun,
	}
}
