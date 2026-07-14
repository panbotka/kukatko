package dirimport

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"

	"github.com/panbotka/kukatko/internal/importer"
	"github.com/panbotka/kukatko/internal/ingest"
	"github.com/panbotka/kukatko/internal/organize"
)

// ErrInterrupted is returned (wrapped) when a run was cancelled before it walked
// the whole directory. Everything already imported stays in the library and the
// run is recorded as failed; re-running finishes the rest.
var ErrInterrupted = errors.New("dirimport: import interrupted")

// Import walks opts.Root and ingests every media file below it, returning the
// final tally. Each file is handed to the ingest pipeline exactly as an upload
// would be, so the SHA256 dedup makes the whole run idempotent: files already in
// the library come back as duplicates and nothing is written for them.
//
// A per-file failure is recorded in the counts and the run continues; the error
// return is reserved for what makes the run itself meaningless — an unreadable
// root, an album or label that cannot be resolved, a run row that cannot be
// opened, or a cancelled context (ErrInterrupted). The caller decides what a
// non-zero Counts.Failed means; the CLI exits non-zero.
//
// With opts.DryRun the walk classifies every file (new, duplicate, skipped) by
// hashing it and looking the hash up, and writes nothing at all — no photos, no
// originals, no import run.
func (s *Service) Import(ctx context.Context, opts Options) (Result, error) {
	entries, err := plan(opts.Root, opts.Recursive)
	if err != nil {
		return Result{}, err
	}
	if opts.DryRun {
		return s.dryRun(ctx, entries, opts), nil
	}
	return s.run(ctx, entries, opts)
}

// dryRun reports what a real run would do without writing anything: every skip
// is reported as it was classified, and every media file is hashed and looked up
// so it can be called new or duplicate. Hashing is the same SHA256 the real
// ingest computes, so the verdict is the one the real run would reach.
//
// The sidecars are matched and read too, so the report a dry run prints — what
// paired, what did not, what could not be parsed — is the one the real run would
// produce, before a single byte is written.
func (s *Service) dryRun(ctx context.Context, entries []planEntry, opts Options) Result {
	idx := buildSidecarIndex(entries, opts)
	tal := newTally(len(entries), opts.Progress, idx.report)
	s.recordSkips(entries, tal)
	s.process(ctx, candidates(entries), tal, 0, func(ctx context.Context, entry planEntry) FileResult {
		return s.classifyAgainstCatalogue(ctx, entry, idx)
	})
	counts, sidecars := tal.snapshot()
	return Result{Counts: counts, Sidecars: sidecars, DryRun: true}
}

// classifyAgainstCatalogue hashes one media file and reports whether a real run
// would create it or find it already catalogued. A file that cannot be read is
// reported as failed — the real run would fail on it too.
func (s *Service) classifyAgainstCatalogue(ctx context.Context, entry planEntry, idx sidecarIndex) FileResult {
	_, res := s.readSidecar(ctx, entry, idx)
	res.Path = entry.rel

	hash, err := hashFile(entry.abs)
	if err != nil {
		res.Outcome, res.Err = OutcomeFailed, err
		return res
	}
	existing, err := s.photos.GetByFileHash(ctx, hash)
	if err != nil {
		res.Outcome = OutcomeImported
		return res
	}
	res.Outcome = OutcomeDuplicate
	res.PhotoUID = existing.UID
	res.ExistingPath = existing.FilePath
	return res
}

// run performs the real import: it opens an import_runs row, resolves the album
// and labels every photo is filed under, ingests the media files over a bounded
// worker pool, and closes the run. A cancelled context closes the run as failed
// and returns ErrInterrupted with the partial tally — the photos already
// committed stay in the library, which is what makes a re-run cheap.
func (s *Service) run(ctx context.Context, entries []planEntry, opts Options) (Result, error) {
	run, err := s.runs.Start(ctx, importer.SourceFolder)
	if err != nil {
		return Result{}, fmt.Errorf("dirimport: starting run: %w", err)
	}
	result := Result{RunID: run.ID}

	dest, err := s.resolveTarget(ctx, opts)
	if err != nil {
		s.fail(ctx, run.ID, err, Counts{})
		return result, err
	}

	idx := buildSidecarIndex(entries, opts)
	tal := newTally(len(entries), opts.Progress, idx.report)
	s.recordSkips(entries, tal)
	s.process(ctx, candidates(entries), tal, run.ID, func(ctx context.Context, entry planEntry) FileResult {
		return s.ingestOne(ctx, entry, opts.UploadedBy, dest, idx)
	})
	result.Counts, result.Sidecars = tal.snapshot()

	if err := ctx.Err(); err != nil {
		interrupted := fmt.Errorf("%w: %w", ErrInterrupted, err)
		s.fail(ctx, run.ID, interrupted, result.Counts)
		return result, interrupted
	}
	if err := s.runs.Complete(context.WithoutCancel(ctx), run.ID, nil, result.Counts.toImporter()); err != nil {
		return result, fmt.Errorf("dirimport: completing run %d: %w", run.ID, err)
	}
	return result, nil
}

// fail closes the run as failed with the tally it reached. The context is
// detached from cancellation so an interrupted run still records why it stopped.
// A bookkeeping failure here cannot undo the import, so it is only logged.
func (s *Service) fail(ctx context.Context, runID int64, cause error, counts Counts) {
	if err := s.runs.Fail(context.WithoutCancel(ctx), runID, cause.Error(), counts.toImporter()); err != nil {
		s.log.Warn("dirimport: recording failed run", "run", runID, "err", err)
	}
}

// ingestOne streams one media file through the ingest pipeline and maps its
// per-file result onto a FileResult, filing the resulting photo under the album
// and labels the run targets. The file's sidecar (a Google Takeout JSON, an XMP)
// travels with it: for an export whose EXIF was stripped, that sidecar *is* the
// photo's date, caption and location.
//
// A duplicate is resolved back to the photo already holding those bytes so the
// user sees what the file collided with — and its metadata gaps are filled from
// the sidecar, which is what makes re-importing a folder worth doing after the
// sidecars were noticed.
func (s *Service) ingestOne(
	ctx context.Context, entry planEntry, uploadedBy string, dest target, idx sidecarIndex,
) FileResult {
	sc, res := s.readSidecar(ctx, entry, idx)
	res.Path = entry.rel

	file, err := os.Open(entry.abs)
	if err != nil {
		return res.failed(fmt.Errorf("dirimport: opening file: %w", err))
	}
	defer func() { _ = file.Close() }()

	ingested := s.ingest.IngestFile(ctx, file, ingest.Request{
		Filename:   filepath.Base(entry.abs),
		UploadedBy: uploadedBy,
		Sidecar:    sc,
	})
	s.logWarnings(entry.rel, ingested.Warnings)

	switch ingested.Outcome {
	case ingest.OutcomeCreated:
		s.applyTarget(ctx, ingested.PhotoUID, dest)
		s.applyCuration(ctx, ingested.PhotoUID, uploadedBy, sc)
		res.Outcome = OutcomeImported
		res.PhotoUID = ingested.PhotoUID
		res.Warnings = warningCodes(ingested.Warnings)
		return res
	case ingest.OutcomeDuplicate:
		s.applyTarget(ctx, ingested.PhotoUID, dest)
		s.fillFromSidecar(ctx, ingested.PhotoUID, sc)
		res.Outcome = OutcomeDuplicate
		res.PhotoUID = ingested.PhotoUID
		res.ExistingPath = s.libraryPath(ctx, ingested.PhotoUID)
		return res
	case ingest.OutcomeError:
		return res.failed(errors.New(ingested.Error))
	}
	return res.failed(fmt.Errorf("dirimport: unknown ingest outcome %q", ingested.Outcome))
}

// failed marks a result as a per-file failure, keeping whatever the run already
// knew about the file (its path, its sidecar). The run records it and carries on.
func (r FileResult) failed(err error) FileResult {
	r.Outcome = OutcomeFailed
	r.Err = err
	return r
}

// logWarnings reports the ingest pipeline's non-fatal per-file warnings (a failed
// thumbnail, an unqueued job) against the file they belong to, with their full
// message. The photo exists either way, so they never change the outcome.
func (s *Service) logWarnings(rel string, warnings []ingest.Warning) {
	for _, w := range warnings {
		s.log.Warn("dirimport: ingest warning", "file", rel, "code", w.Code, "message", w.Message)
	}
}

// warningCodes reduces the ingest warnings to their codes, which is what a
// per-file progress line has room for; the full messages are in the log.
func warningCodes(warnings []ingest.Warning) []string {
	if len(warnings) == 0 {
		return nil
	}
	codes := make([]string, 0, len(warnings))
	for _, w := range warnings {
		codes = append(codes, w.Code)
	}
	return codes
}

// libraryPath returns the storage path of the photo with this UID, or "" when it
// cannot be read back. It is only used to enrich a duplicate report.
func (s *Service) libraryPath(ctx context.Context, photoUID string) string {
	if photoUID == "" {
		return ""
	}
	photo, err := s.photos.GetByUID(ctx, photoUID)
	if err != nil {
		return ""
	}
	return photo.FilePath
}

// recordSkips feeds every already-decided skip into the tally, in walk order,
// before the ingest of the media files begins.
func (s *Service) recordSkips(entries []planEntry, tal *tally) {
	for _, entry := range entries {
		if entry.skip == "" {
			continue
		}
		tal.record(FileResult{Path: entry.rel, Outcome: OutcomeSkipped, Reason: entry.skip})
	}
}

// candidates returns the media files of a plan — the entries no skip rule
// excluded.
func candidates(entries []planEntry) []planEntry {
	out := make([]planEntry, 0, len(entries))
	for _, entry := range entries {
		if entry.skip == "" {
			out = append(out, entry)
		}
	}
	return out
}

// process runs work over the candidate files on a bounded pool of Concurrency
// workers, recording each result in the tally. When runID is non-zero the running
// tally is checkpointed into import_runs every checkpointEvery files, so a long
// run shows live progress in the UI.
//
// A cancelled context drains the queue without doing any further work: the files
// already ingested are committed and the caller sees ctx.Err().
func (s *Service) process(
	ctx context.Context, entries []planEntry, tal *tally, runID int64,
	work func(context.Context, planEntry) FileResult,
) {
	queue := make(chan planEntry)
	var wg sync.WaitGroup
	for range s.concurrency {
		wg.Go(func() {
			for entry := range queue {
				if ctx.Err() != nil {
					continue
				}
				counts, due := tal.record(work(ctx, entry))
				if due && runID != 0 {
					s.checkpoint(ctx, runID, counts)
				}
			}
		})
	}
	for _, entry := range entries {
		queue <- entry
	}
	close(queue)
	wg.Wait()
}

// checkpoint writes the running tally to the import run, so /import shows a long
// folder import advancing. A failed checkpoint is only bookkeeping and never
// aborts the import.
func (s *Service) checkpoint(ctx context.Context, runID int64, counts Counts) {
	if err := s.runs.UpdateCounts(ctx, runID, counts.toImporter()); err != nil {
		s.log.Warn("dirimport: checkpointing run", "run", runID, "err", err)
	}
}

// toImporter maps the folder tally onto the import_runs shape. A folder import
// never updates an existing photo, and everything it did not create and did not
// fail on — duplicates and skipped junk alike — is what import_runs calls
// skipped.
func (c Counts) toImporter() importer.Counts {
	return importer.Counts{
		Imported: c.Imported,
		Skipped:  c.Duplicates + c.Skipped,
		Failed:   c.Failed,
	}
}

// target is the resolved destination every photo of a run is filed under.
type target struct {
	// albumUID is the album to add each photo to; empty means no album.
	albumUID string
	// labelUIDs are the labels to attach to each photo.
	labelUIDs []string
}

// resolveTarget resolves --album and --labels into UIDs before a single file is
// ingested, creating the album or labels that do not exist yet. Resolving up
// front means a typo fails the run immediately rather than after two thousand
// files have been imported into nothing.
func (s *Service) resolveTarget(ctx context.Context, opts Options) (target, error) {
	albumUID, err := s.resolveAlbum(ctx, strings.TrimSpace(opts.Album))
	if err != nil {
		return target{}, err
	}
	labelUIDs, err := s.resolveLabels(ctx, opts.Labels)
	if err != nil {
		return target{}, err
	}
	return target{albumUID: albumUID, labelUIDs: labelUIDs}, nil
}

// resolveAlbum turns the --album reference into an album UID: an existing album's
// UID is used as is, and anything else is treated as a title — matched
// case-insensitively against the existing albums, and created when no album
// carries it. An empty reference resolves to no album.
func (s *Service) resolveAlbum(ctx context.Context, ref string) (string, error) {
	if ref == "" {
		return "", nil
	}
	if s.albums == nil {
		return "", errors.New("dirimport: --album given but no album store is configured")
	}
	album, err := s.albums.GetAlbumByUID(ctx, ref)
	switch {
	case err == nil:
		return album.UID, nil
	case !errors.Is(err, organize.ErrAlbumNotFound):
		return "", fmt.Errorf("dirimport: looking up album %q: %w", ref, err)
	}

	existing, err := s.albums.ListAlbums(ctx)
	if err != nil {
		return "", fmt.Errorf("dirimport: listing albums: %w", err)
	}
	for i := range existing {
		if strings.EqualFold(strings.TrimSpace(existing[i].Title), ref) {
			return existing[i].UID, nil
		}
	}
	created, err := s.albums.CreateAlbum(ctx, organize.Album{Title: ref, Type: organize.AlbumManual})
	if err != nil {
		return "", fmt.Errorf("dirimport: creating album %q: %w", ref, err)
	}
	return created.UID, nil
}

// resolveLabels turns the --labels names into label UIDs, matching existing
// labels case-insensitively by name and creating the ones that do not exist yet.
// Blank names are ignored.
func (s *Service) resolveLabels(ctx context.Context, names []string) ([]string, error) {
	wanted := cleanNames(names)
	if len(wanted) == 0 {
		return nil, nil
	}
	if s.labels == nil {
		return nil, errors.New("dirimport: --labels given but no label store is configured")
	}
	existing, err := s.labels.ListLabels(ctx)
	if err != nil {
		return nil, fmt.Errorf("dirimport: listing labels: %w", err)
	}
	byName := make(map[string]string, len(existing))
	for i := range existing {
		byName[strings.ToLower(existing[i].Name)] = existing[i].UID
	}

	uids := make([]string, 0, len(wanted))
	for _, name := range wanted {
		uid, ok := byName[strings.ToLower(name)]
		if !ok {
			created, createErr := s.labels.CreateLabel(ctx, organize.Label{Name: name})
			if createErr != nil {
				return nil, fmt.Errorf("dirimport: creating label %q: %w", name, createErr)
			}
			uid = created.UID
			byName[strings.ToLower(name)] = uid
		}
		uids = append(uids, uid)
	}
	return uids, nil
}

// cleanNames trims the names and drops the empty ones, so `--labels a,,b ` is
// two labels.
func cleanNames(names []string) []string {
	out := make([]string, 0, len(names))
	for _, name := range names {
		if trimmed := strings.TrimSpace(name); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}

// applyTarget files a photo under the run's album and labels. Both stores are
// idempotent, so this runs for a duplicate too: re-importing a folder into an
// album is how a user fixes a forgotten --album, and it stays a no-op otherwise.
// A failure here does not undo the import and is only logged — the photo is in
// the library either way.
func (s *Service) applyTarget(ctx context.Context, photoUID string, dest target) {
	if photoUID == "" {
		return
	}
	if dest.albumUID != "" {
		if err := s.albums.AddPhoto(ctx, dest.albumUID, photoUID); err != nil {
			s.log.Warn("dirimport: adding photo to album",
				"album", dest.albumUID, "photo", photoUID, "err", err)
		}
	}
	for _, labelUID := range dest.labelUIDs {
		if err := s.labels.AttachLabel(ctx, photoUID, labelUID, organize.SourceImport, 0); err != nil {
			s.log.Warn("dirimport: attaching label",
				"label", labelUID, "photo", photoUID, "err", err)
		}
	}
}

// hashFile computes a file's SHA256 content hash by streaming it, never holding
// the file in memory. It is the hash the ingest pipeline identifies photos by, so
// a dry run can predict the real run's dedup verdict exactly.
func hashFile(path string) (string, error) {
	file, err := os.Open(path) //nolint:gosec // G304: the path comes from walking the root the operator named.
	if err != nil {
		return "", fmt.Errorf("dirimport: opening file: %w", err)
	}
	defer func() { _ = file.Close() }()

	hasher := sha256.New()
	if _, err := io.Copy(hasher, file); err != nil {
		return "", fmt.Errorf("dirimport: hashing file: %w", err)
	}
	return hex.EncodeToString(hasher.Sum(nil)), nil
}

// tally is the concurrency-safe running tally of a run. It serialises the
// progress callback too, so the CLI can print a line per file from several
// workers without interleaving.
type tally struct {
	mu       sync.Mutex
	counts   Counts
	sidecars SidecarReport
	total    int
	done     int
	progress func(res FileResult, done, total int)
}

// newTally returns a tally over total files, reporting to progress (which may be
// nil). The sidecar report starts from what the matcher already knows — what
// paired and what did not — and the workers add to it what each sidecar turned
// out to be worth once read.
func newTally(total int, progress func(res FileResult, done, total int), sidecars SidecarReport) *tally {
	return &tally{
		counts:   Counts{ByReason: make(map[SkipReason]int)},
		sidecars: sidecars,
		total:    total,
		progress: progress,
	}
}

// record adds one file's outcome to the tally, reports it to the progress
// callback, and returns the tally so far together with whether a checkpoint is
// due (every checkpointEvery files).
func (t *tally) record(res FileResult) (Counts, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()

	switch res.Outcome {
	case OutcomeImported:
		t.counts.Imported++
	case OutcomeDuplicate:
		t.counts.Duplicates++
	case OutcomeSkipped:
		t.counts.Skipped++
		t.counts.ByReason[res.Reason]++
	case OutcomeFailed:
		t.counts.Failed++
	}
	t.recordSidecarLocked(res)
	t.done++
	if t.progress != nil {
		t.progress(res, t.done, t.total)
	}
	return t.countsLocked(), t.done%checkpointEvery == 0
}

// recordSidecarLocked notes what became of one file's sidecar; the caller must
// hold the mutex.
func (t *tally) recordSidecarLocked(res FileResult) {
	switch {
	case res.Sidecar == "":
	case res.SidecarErr != nil:
		t.sidecars.Unreadable = append(t.sidecars.Unreadable, res.Sidecar)
	default:
		t.sidecars.Applied++
	}
}

// snapshot returns a copy of the tally and of the sidecar report, safe to read
// while workers are still running.
func (t *tally) snapshot() (Counts, SidecarReport) {
	t.mu.Lock()
	defer t.mu.Unlock()
	sidecars := t.sidecars
	sidecars.Unreadable = slices.Sorted(slices.Values(t.sidecars.Unreadable))
	sidecars.Orphans = slices.Clone(t.sidecars.Orphans)
	sidecars.Missing = slices.Clone(t.sidecars.Missing)
	return t.countsLocked(), sidecars
}

// countsLocked copies the tally; the caller must hold the mutex.
func (t *tally) countsLocked() Counts {
	counts := t.counts
	counts.ByReason = maps.Clone(t.counts.ByReason)
	return counts
}
