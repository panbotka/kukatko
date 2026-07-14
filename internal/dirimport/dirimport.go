// Package dirimport ingests a directory of originals from disk into the
// catalogue: `kukatko import dir <path>`. It is the way a folder of photos —
// scans, a camera card, an old backup — gets into Kukátko without a browser.
//
// It owns no pipeline of its own. The walk decides which files are media and
// which are junk, and every media file is then handed to internal/ingest exactly
// as an HTTP upload would be: streamed, SHA256-hashed, deduplicated, stored
// under YYYY/MM, thumbnailed, and queued for embedding and face detection. The
// source directory is only ever read — originals are copied, never moved or
// modified.
//
// Two properties make a folder import safe to re-run, which is the whole point:
//
//   - Idempotent. Identity is the content hash, so a file already in the library
//     is reported as a duplicate and nothing is written. Re-running a folder
//     after adding ten files imports exactly those ten.
//   - Resumable. Every file is committed on its own; a run that dies (or is
//     interrupted) leaves what it already imported in the library, and a re-run
//     finishes the rest. A per-file failure is recorded and the walk continues —
//     one corrupt JPEG never aborts a 2000-file run.
//
// The run is recorded through internal/importer under importer.SourceFolder, so
// it shows up in /import and GET /import/runs beside the PhotoPrism and
// photo-sorter runs. It records no high-watermark: a folder has no source
// timestamp to resume from, the content hash does that job.
//
// Concurrency is deliberately small (see DefaultConcurrency): thumbnailing a
// wide fan-out of large images on a 16 GB box will swap the machine solid.
package dirimport

import (
	"context"
	"io"
	"log/slog"
	"time"

	"github.com/panbotka/kukatko/internal/importer"
	"github.com/panbotka/kukatko/internal/ingest"
	"github.com/panbotka/kukatko/internal/organize"
	"github.com/panbotka/kukatko/internal/photos"
)

// Concurrency bounds for the ingest fan-out. The cap is a hard limit, not a
// suggestion: each worker decodes and thumbnails a full-size image, so a wide
// fan-out is the fastest way to exhaust the memory this box shares with
// everything else running on it.
const (
	// DefaultConcurrency is the number of files ingested in parallel when the
	// caller does not choose.
	DefaultConcurrency = 3
	// MaxConcurrency caps the fan-out however large a value the caller asks for.
	MaxConcurrency = 8
)

// checkpointEvery is how many decided files pass between two writes of the
// running tally to import_runs, so a long run shows live progress in the UI
// without one UPDATE per file.
const checkpointEvery = 25

// Outcome classifies what happened to one file on disk.
type Outcome string

const (
	// OutcomeImported means the file was catalogued as a new photo.
	OutcomeImported Outcome = "imported"
	// OutcomeDuplicate means the file's content is already in the library; nothing
	// was created.
	OutcomeDuplicate Outcome = "duplicate"
	// OutcomeSkipped means the walk decided the file is not media to import; the
	// reason says which rule fired.
	OutcomeSkipped Outcome = "skipped"
	// OutcomeFailed means the file is media but could not be ingested.
	OutcomeFailed Outcome = "failed"
)

// SkipReason names the rule that excluded a file from the import. Skips are
// tallied per reason and reported in the summary; none of them fails the run.
type SkipReason string

const (
	// SkipHidden is a dotfile or a file inside a dot-directory.
	SkipHidden SkipReason = "hidden"
	// SkipJunk is filesystem cruft: Thumbs.db, .DS_Store, desktop.ini, and the
	// contents of @eaDir / __MACOSX directories.
	SkipJunk SkipReason = "junk"
	// SkipSidecar is a metadata sidecar (.xmp, .json, .aae, .thm) — not media.
	// Reading sidecars for metadata is a separate concern; here they are ignored.
	SkipSidecar SkipReason = "sidecar"
	// SkipUnsupported is a file whose extension is neither a supported image nor a
	// supported video.
	SkipUnsupported SkipReason = "unsupported"
	// SkipSymlink is a symbolic link. Links are skipped, never followed: it is the
	// only walk rule that cannot loop forever, and the target is either inside the
	// tree (and imported on its own) or outside it (and not what the user pointed
	// at).
	SkipSymlink SkipReason = "symlink"
	// SkipEmpty is a zero-byte file — there is nothing to catalogue.
	SkipEmpty SkipReason = "empty"
)

// FileResult is what the import decided about one file on disk. Exactly one of
// Reason (a skip), Err (a failure) or PhotoUID (an import or duplicate) is set.
type FileResult struct {
	// Path is the file's path as walked, relative to the import root.
	Path string
	// Outcome is the file's fate.
	Outcome Outcome
	// Reason is the skip rule that fired; set only for OutcomeSkipped.
	Reason SkipReason
	// Sidecar is the metadata sidecar matched to this media file (relative to the
	// import root), empty when it has none. SidecarErr says why a matched sidecar
	// could not be read; the file is still imported, only without the metadata the
	// sidecar held.
	Sidecar    string
	SidecarErr error
	// PhotoUID identifies the photo created (OutcomeImported) or the photo that
	// already holds this content (OutcomeDuplicate).
	PhotoUID string
	// ExistingPath is the library path of the photo this file duplicates, so the
	// user can see the same bytes already arrived under another name. Set only for
	// OutcomeDuplicate (and empty if the photo could not be read back).
	ExistingPath string
	// Warnings are the ingest pipeline's non-fatal complaints about a photo it
	// nonetheless created (a thumbnail that could not be rendered, a job that could
	// not be queued). The photo is in the library and the original is intact, so
	// this is not a failure — but a folder import is unattended, so the codes are
	// reported rather than left in the log.
	Warnings []string
	// Err is why the file could not be ingested; set only for OutcomeFailed.
	Err error
}

// Counts is the tally of a folder import: every walked file lands in exactly one
// of Imported, Duplicates, Skipped or Failed. ByReason breaks Skipped down by the
// rule that fired.
type Counts struct {
	Imported   int
	Duplicates int
	Skipped    int
	Failed     int
	ByReason   map[SkipReason]int
}

// Total returns how many files the run decided on — the sum of every bucket.
func (c Counts) Total() int {
	return c.Imported + c.Duplicates + c.Skipped + c.Failed
}

// SidecarReport is what the run made of the metadata sidecars beside the media —
// the Google Takeout JSON and Apple XMP files that carry the capture date, the
// caption and the GPS of an export whose media files no longer do.
//
// Everything that did not pair is named, not counted away: a sidecar silently
// matched to the wrong photo, or to none, is how somebody loses a decade of
// dates without ever being told.
type SidecarReport struct {
	// Matched is how many media files a sidecar was paired with.
	Matched int
	// Applied is how many of those sidecars were read and folded into the photo.
	Applied int
	// Unreadable are the paths of sidecars that were paired but could not be
	// parsed. Their media file is still imported, with whatever metadata it
	// carries itself.
	Unreadable []string
	// Orphans are sidecars that describe no media file in their directory.
	Orphans []string
	// Missing are media files with no sidecar, reported only for directories that
	// hold sidecars at all — in an export folder that is a photo about to lose its
	// date; in an ordinary folder of camera files it is simply the normal case.
	Missing []string
}

// Result is the outcome of one folder import.
type Result struct {
	// RunID is the import_runs row recording the run; 0 for a dry run, which
	// records nothing.
	RunID int64
	// Counts is the final tally.
	Counts Counts
	// Sidecars is what became of the metadata sidecars beside the media files.
	Sidecars SidecarReport
	// DryRun echoes whether the run only reported what it would have done.
	DryRun bool
}

// Options scopes a single Import call.
type Options struct {
	// Root is the directory to walk (required).
	Root string
	// Recursive walks nested subdirectories; false imports only the flat Root.
	Recursive bool
	// DryRun classifies every file — new, duplicate, skipped — and writes nothing
	// at all: no photos, no originals, and no import run.
	DryRun bool
	// Album puts every imported (and duplicate) photo in this album, named by uid
	// or by title. A title with no matching album is created.
	Album string
	// Labels are attached to every imported (and duplicate) photo, by name. A name
	// with no matching label is created.
	Labels []string
	// UploadedBy is the UID of the user who owns the imported photos; empty leaves
	// photos.uploaded_by NULL. It is also who an export's per-user marks land on:
	// favourites and ratings belong to a user in Kukátko, not to a photo.
	UploadedBy string
	// NoSidecars ignores the metadata sidecars beside the media files. The default
	// (false) reads them — a Google Takeout export imported without its JSON
	// sidecars arrives with no dates and no captions at all.
	NoSidecars bool
	// Progress, when set, is called once per decided file with the running tally
	// (done of total). It is called from the worker goroutines but serialised, so
	// an implementation may write to a terminal without locking.
	Progress func(res FileResult, done, total int)
}

// Ingester is the upload pipeline a folder import feeds: the one in
// internal/ingest, satisfied by *ingest.Service. Depending on the interface keeps
// the walk testable without storage, a thumbnailer or a database.
type Ingester interface {
	// IngestFile streams one file through the pipeline and reports its per-file
	// outcome; it never returns an error, failures are carried in the result. The
	// request carries the file's sidecar metadata, when it had one.
	IngestFile(ctx context.Context, src io.Reader, req ingest.Request) ingest.FileResult
}

// RunStore records the run in import_runs, satisfied by *importer.Store.
type RunStore interface {
	// Start opens a run in the running state.
	Start(ctx context.Context, source importer.Source) (importer.Run, error)
	// UpdateCounts checkpoints the running tally.
	UpdateCounts(ctx context.Context, id int64, counts importer.Counts) error
	// Complete closes the run as done with its final tally. A folder run passes a
	// nil watermark: it has no source cursor to resume from.
	Complete(ctx context.Context, id int64, watermark *time.Time, counts importer.Counts) error
	// Fail closes the run as failed, recording why.
	Fail(ctx context.Context, id int64, lastErr string, counts importer.Counts) error
}

// PhotoLookup reads the catalogue to classify a file without ingesting it (the
// dry run) and to resolve what an already-imported duplicate is a duplicate of.
// It is satisfied by *photos.Store.
type PhotoLookup interface {
	// GetByFileHash returns the photo holding this SHA256 content hash.
	GetByFileHash(ctx context.Context, hash string) (photos.Photo, error)
	// GetByUID returns the photo with this UID.
	GetByUID(ctx context.Context, uid string) (photos.Photo, error)
}

// PhotoFiller backfills the metadata gaps of a photo that is already catalogued,
// satisfied by *photos.Store. It is what makes a re-import worth running: a
// folder first imported without its sidecars comes back as a wall of duplicates,
// and this is what still writes the dates and captions those duplicates never
// got. It never overwrites a field the photo already has.
type PhotoFiller interface {
	// FillMissingMetadata fills only the empty fields of the photo and reports
	// whether anything changed.
	FillMissingMetadata(ctx context.Context, uid string, fill photos.MetadataFill) (bool, error)
}

// CurationStore records the per-user marks an export carries — Google's
// "favorited" star, an XMP star rating. Both are per-user in Kukátko, so they
// land on the importing user; satisfied by *organize.Store.
type CurationStore interface {
	// AddFavorite marks the photo as a favourite of this user (idempotent).
	AddFavorite(ctx context.Context, userUID, photoUID string) error
	// SetRating sets this user's star rating (0..5) for the photo.
	SetRating(ctx context.Context, userUID, photoUID string, rating int) error
}

// AlbumStore is the album catalogue needed to place imported photos, satisfied by
// *organize.Store.
type AlbumStore interface {
	// GetAlbumByUID returns the album with this UID.
	GetAlbumByUID(ctx context.Context, uid string) (organize.Album, error)
	// ListAlbums lists every album, so --album can resolve a title.
	ListAlbums(ctx context.Context) ([]organize.AlbumSummary, error)
	// CreateAlbum inserts a new album.
	CreateAlbum(ctx context.Context, a organize.Album) (organize.Album, error)
	// AddPhoto adds a photo to an album (idempotent).
	AddPhoto(ctx context.Context, albumUID, photoUID string) error
}

// LabelStore is the label catalogue needed to tag imported photos, satisfied by
// *organize.Store.
type LabelStore interface {
	// ListLabels lists every label with its photo count, so --labels can resolve
	// names.
	ListLabels(ctx context.Context) ([]organize.LabelCount, error)
	// CreateLabel inserts a new label.
	CreateLabel(ctx context.Context, l organize.Label) (organize.Label, error)
	// AttachLabel attaches a label to a photo (idempotent).
	AttachLabel(ctx context.Context, photoUID, labelUID string, source organize.LabelSource, uncertainty int) error
}

// Config bundles the collaborators and tunables a Service needs.
type Config struct {
	// Ingest is the upload pipeline every media file is handed to (required).
	Ingest Ingester
	// Runs records the import run (required unless every call is a dry run).
	Runs RunStore
	// Photos reads the catalogue for dry-run classification and duplicate
	// reporting (required).
	Photos PhotoLookup
	// Filler backfills sidecar metadata onto photos that are already catalogued;
	// nil leaves duplicates untouched.
	Filler PhotoFiller
	// Curation applies an export's favourites and ratings to the importing user;
	// nil ignores them.
	Curation CurationStore
	// Albums resolves and populates --album (required only when --album is used).
	Albums AlbumStore
	// Labels resolves and attaches --labels (required only when --labels is used).
	Labels LabelStore
	// Concurrency is how many files are ingested in parallel; a non-positive value
	// means DefaultConcurrency and anything above MaxConcurrency is clamped to it.
	Concurrency int
	// Logger receives non-fatal problems (a failed checkpoint, an album that could
	// not be attached); nil means slog.Default().
	Logger *slog.Logger
}

// Service imports directories of originals from disk. It is safe for concurrent
// use; a single Import call fans its own work out over Concurrency workers.
type Service struct {
	ingest      Ingester
	runs        RunStore
	photos      PhotoLookup
	filler      PhotoFiller
	curation    CurationStore
	albums      AlbumStore
	labels      LabelStore
	concurrency int
	log         *slog.Logger
}

// New returns a Service from cfg, clamping the fan-out into
// [1, MaxConcurrency] and defaulting the logger.
func New(cfg Config) *Service {
	log := cfg.Logger
	if log == nil {
		log = slog.Default()
	}
	return &Service{
		ingest:      cfg.Ingest,
		runs:        cfg.Runs,
		photos:      cfg.Photos,
		filler:      cfg.Filler,
		curation:    cfg.Curation,
		albums:      cfg.Albums,
		labels:      cfg.Labels,
		concurrency: clampConcurrency(cfg.Concurrency),
		log:         log,
	}
}

// clampConcurrency maps a requested fan-out onto the supported range: a
// non-positive request becomes DefaultConcurrency and anything above
// MaxConcurrency is capped there.
func clampConcurrency(n int) int {
	if n <= 0 {
		return DefaultConcurrency
	}
	if n > MaxConcurrency {
		return MaxConcurrency
	}
	return n
}
