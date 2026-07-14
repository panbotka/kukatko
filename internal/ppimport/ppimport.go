// Package ppimport implements Kukátko's read-only, incremental, idempotent
// import from a running PhotoPrism instance (ARCHITECTURE.md §9). PhotoPrism
// stays the primary catalogue during the migration, so the import only ever
// reads: it lists photos (incrementally, by a high-watermark stored in
// import_runs), downloads each new original, computes a fresh SHA256, dedups by
// content and by PhotoPrism UID, stores the original, catalogues the photo with
// PhotoPrism's curated metadata plus the external identifiers (photoprism_uid,
// photoprism_file_hash), generates thumbnails and enqueues the image_embed and
// face_detect jobs that compute embeddings and faces afterwards.
//
// Half of what PhotoPrism knows about a photo is served on the photo DETAIL
// endpoint and nowhere else — its Details block (subject, artist, copyright,
// licence, keywords, notes, software), its per-file technicals (still codec, colour
// profile, projection) and its face markers — while the listing answers a flattened
// search struct carrying none of them. A photo's detail is therefore read once and
// everything on it carried over from that one request (importPhotoDetail).
//
// PhotoPrism's global Favorite flag is deliberately NOT mapped: Kukátko's
// favourites are per-user, and an import that runs as a background job (or from the
// CLI) has no user to attribute one to.
//
// On top of the photos it maps the surrounding structure: albums and labels are
// found-or-created by name and their membership attached to the imported photos,
// and named face markers seed people (subjects) and their markers. A full run
// maps that structure by walking the source's album and label catalogues; a
// scoped run (ImportScoped) instead reads each imported photo's own detail, which
// names every album it belongs to and every label it carries, so a migrated slice
// arrives with each photo's whole context and not merely the one album or label
// that selected it.
//
// Robustness is the point of this package: a per-photo failure is recorded in the
// run's Failed tally and never aborts the whole run; the PhotoPrism client
// already backs off on HTTP 429; counts are checkpointed after every page so a
// long run leaves a progress trail; and because every step dedups, the whole
// import is safe to re-run (incremental on the second pass, full on the first).
//
// Every collaborator is an interface so the Service unit-tests against fakes with
// no real PhotoPrism, network, database or filesystem.
package ppimport

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"time"

	"github.com/panbotka/kukatko/internal/importer"
	"github.com/panbotka/kukatko/internal/organize"
	"github.com/panbotka/kukatko/internal/people"
	"github.com/panbotka/kukatko/internal/photoprism"
	"github.com/panbotka/kukatko/internal/photos"
	"github.com/panbotka/kukatko/internal/storage"
	"github.com/panbotka/kukatko/internal/video"
)

var (
	// ErrEmptyScope indicates ImportScoped was called with no filter set at all;
	// the caller wants Import (a full incremental run) instead.
	ErrEmptyScope = errors.New("ppimport: empty import scope")
	// ErrInvalidYear indicates the scoped year lies outside the plausible range.
	ErrInvalidYear = errors.New("ppimport: invalid year")
	// ErrAlbumNotFound indicates the requested album does not exist in the source
	// PhotoPrism instance.
	ErrAlbumNotFound = errors.New("ppimport: album not found in photoprism")
	// ErrLabelNotFound indicates the requested label does not exist in the source
	// PhotoPrism instance.
	ErrLabelNotFound = errors.New("ppimport: label not found in photoprism")
)

// DefaultPageSize is the listing page size used when Config.PageSize is left
// zero. It matches PhotoPrism's hard cap so a full library is walked in the
// fewest requests.
const DefaultPageSize = photoprism.MaxCount

// DefaultAlbumTypes are the PhotoPrism album types a full import maps when
// Config.AlbumTypes is empty. PhotoPrism serves five (photoprism.AlbumTypes) and
// generates most of them itself; "month" is left out because it holds one
// auto-generated album per calendar month — 560 of them on the production
// library — which Kukátko's timeline already covers.
var DefaultAlbumTypes = []string{"album", "folder", "moment", "state"}

// PhotoPrismClient is the read-only PhotoPrism contract the importer needs: photo
// listing (incremental or scoped to an album/label), the detail of one photo (its
// albums and labels, which the listing omits) and original download, plus the
// album and label catalogues. It is the import-facing subset of photoprism.Client.
type PhotoPrismClient interface {
	// ListPhotos returns one page of photos for the given params.
	ListPhotos(ctx context.Context, params photoprism.PhotoListParams) ([]photoprism.Photo, error)
	// GetPhoto returns one photo with its whole context: the albums it belongs to
	// and the labels it carries. Only a scoped run calls it (one request per photo).
	GetPhoto(ctx context.Context, uid string) (photoprism.PhotoDetail, error)
	// ListAlbums returns one page of albums.
	ListAlbums(ctx context.Context, params photoprism.ListParams) ([]photoprism.Album, error)
	// ListLabels returns one page of labels.
	ListLabels(ctx context.Context, params photoprism.ListParams) ([]photoprism.Label, error)
	// DownloadOriginal streams the original identified by its SHA1 file hash.
	DownloadOriginal(ctx context.Context, fileHash string) (*photoprism.Download, error)
}

// RunStore records the lifecycle of an import run and the resume watermark. It is
// the import-facing subset of importer.Store.
type RunStore interface {
	// Start opens a new running import run for source.
	Start(ctx context.Context, source importer.Source) (importer.Run, error)
	// UpdateCounts checkpoints the running tally of a run.
	UpdateCounts(ctx context.Context, id int64, counts importer.Counts) error
	// Complete closes a run as done, recording the resume watermark and counts.
	Complete(ctx context.Context, id int64, watermark *time.Time, counts importer.Counts) error
	// Fail closes a run as failed, recording the error and counts.
	Fail(ctx context.Context, id int64, lastErr string, counts importer.Counts) error
	// LatestWatermark returns the resume cursor of the last successful run.
	LatestWatermark(ctx context.Context, source importer.Source) (time.Time, bool, error)
}

// PhotoStore is the subset of photos.Store the importer reads and writes.
type PhotoStore interface {
	// Create inserts a new photo, returning ErrFileHashTaken on a content clash.
	Create(ctx context.Context, p photos.Photo) (photos.Photo, error)
	// CreateFile inserts a photo's primary file row.
	CreateFile(ctx context.Context, f photos.PhotoFile) (photos.PhotoFile, error)
	// GetByFileHash finds a photo by its SHA256 content hash (ErrPhotoNotFound).
	GetByFileHash(ctx context.Context, hash string) (photos.Photo, error)
	// GetByPhotoprismUID finds an already-imported photo (ErrPhotoNotFound).
	GetByPhotoprismUID(ctx context.Context, ppUID string) (photos.Photo, error)
	// UpdateMetadata applies changed metadata to an existing photo.
	UpdateMetadata(ctx context.Context, uid string, m photos.MetadataUpdate) (photos.Photo, error)
	// ApplyImportMetadata carries the source's credits and file-technical fields onto
	// an existing photo, reporting whether anything changed. It never erases: an empty
	// source value leaves a non-empty column alone.
	ApplyImportMetadata(ctx context.Context, uid string, m photos.ImportMetadata) (bool, error)
	// SetPhotoprismRef backfills the external IDs onto a SHA256-deduped photo.
	SetPhotoprismRef(ctx context.Context, uid, ppUID, ppFileHash string) (photos.Photo, error)
	// Delete removes a photo (used to roll back a half-created record).
	Delete(ctx context.Context, uid string) error
}

// Storage publishes downloaded originals into the on-disk layout. It is the
// subset of storage.Storage the importer uses.
type Storage interface {
	// Store streams src into the layout and returns the stored file descriptor.
	Store(ctx context.Context, src io.Reader, takenAt time.Time, originalName string) (storage.StoredFile, error)
	// Delete removes a stored original by its relative path.
	Delete(ctx context.Context, relPath string) error
}

// Thumbnailer renders the derived images for a catalogued photo. It is satisfied
// by thumb.Thumbnailer.
type Thumbnailer interface {
	// GenerateAll renders every registered thumbnail size for photo.
	GenerateAll(ctx context.Context, photo photos.Photo) (map[string]string, error)
}

// AlbumStore maps albums and their membership. It is the subset of
// organize.Store the importer uses for albums.
type AlbumStore interface {
	// ListAlbums lists existing albums with their photo counts.
	ListAlbums(ctx context.Context) ([]organize.AlbumSummary, error)
	// CreateAlbum inserts a new album.
	CreateAlbum(ctx context.Context, a organize.Album) (organize.Album, error)
	// AddPhoto adds a photo to an album (idempotent).
	AddPhoto(ctx context.Context, albumUID, photoUID string) error
}

// LabelStore maps labels and their membership. It is the subset of
// organize.Store the importer uses for labels.
type LabelStore interface {
	// ListLabels lists existing labels with their photo counts.
	ListLabels(ctx context.Context) ([]organize.LabelCount, error)
	// CreateLabel inserts a new label.
	CreateLabel(ctx context.Context, l organize.Label) (organize.Label, error)
	// AttachLabel attaches a label to a photo (idempotent).
	AttachLabel(ctx context.Context, photoUID, labelUID string, source organize.LabelSource, uncertainty int) error
}

// PeopleStore maps subjects and markers. It is the subset of people.Store the
// importer uses to seed people from PhotoPrism's named face markers.
type PeopleStore interface {
	// GetSubjectBySlug finds a subject by slug (ErrSubjectNotFound).
	GetSubjectBySlug(ctx context.Context, slug string) (people.Subject, error)
	// CreateSubject inserts a new subject.
	CreateSubject(ctx context.Context, subj people.Subject) (people.Subject, error)
	// CreateMarker inserts a face/label marker, optionally assigning a subject.
	CreateMarker(ctx context.Context, m people.Marker) (people.Marker, error)
	// GetMarkerByUID finds a marker by UID (ErrMarkerNotFound), which is how an
	// already-imported marker is recognised and skipped.
	GetMarkerByUID(ctx context.Context, uid string) (people.Marker, error)
}

// Enqueuer schedules the post-import background jobs. It is satisfied by
// jobs.Enqueuer.
type Enqueuer interface {
	// EnqueueImageEmbed schedules embedding for a photo (dedup no-op).
	EnqueueImageEmbed(ctx context.Context, photoUID string) error
	// EnqueueFaceDetect schedules face detection for a photo (dedup no-op).
	EnqueueFaceDetect(ctx context.Context, photoUID string) error
}

// VideoProber probes a downloaded video for its container metadata. It abstracts
// video.Probe so the importer can be unit-tested with canned metadata and no real
// ffprobe/ffmpeg. It is satisfied by the package's defaultProber.
type VideoProber interface {
	// Probe reads the container metadata of the video at path.
	Probe(ctx context.Context, path string) (video.Metadata, error)
}

// Config bundles the Service's collaborators and tunables. Every collaborator is
// required; the tunables fall back to package defaults when left zero.
type Config struct {
	// Client is the read-only PhotoPrism client.
	Client PhotoPrismClient
	// Runs records the import run lifecycle and watermark.
	Runs RunStore
	// Photos is the photo catalogue.
	Photos PhotoStore
	// Storage publishes downloaded originals.
	Storage Storage
	// Thumbnailer renders derived images.
	Thumbnailer Thumbnailer
	// Albums maps albums and membership.
	Albums AlbumStore
	// Labels maps labels and membership.
	Labels LabelStore
	// People maps subjects and markers.
	People PeopleStore
	// Enqueuer schedules the image_embed and face_detect jobs.
	Enqueuer Enqueuer
	// Prober probes downloaded videos for their metadata; nil uses video.Probe.
	Prober VideoProber
	// PageSize is the listing page size (default DefaultPageSize).
	PageSize int
	// AlbumTypes are the source album types a full import maps (default
	// DefaultAlbumTypes). The source takes one type per listing request.
	AlbumTypes []string
	// TempDir is where downloads are staged before publishing ("" uses the OS
	// temp directory).
	TempDir string
	// MaxFileSize caps a single downloaded original in bytes; 0 means unlimited.
	MaxFileSize int64
	// Logger receives per-item failure diagnostics; nil uses slog.Default().
	Logger *slog.Logger
	// Metrics receives per-page progress tallies; nil disables instrumentation.
	Metrics importer.ProgressObserver
}

// Service runs the PhotoPrism import. It is safe for use by a single run at a
// time; the admin trigger serialises runs via the job-queue dedup key.
type Service struct {
	client      PhotoPrismClient
	runs        RunStore
	photos      PhotoStore
	storage     Storage
	thumbs      Thumbnailer
	albums      AlbumStore
	labels      LabelStore
	people      PeopleStore
	enqueuer    Enqueuer
	prober      VideoProber
	pageSize    int
	albumTypes  []string
	tempDir     string
	maxFileSize int64
	log         *slog.Logger
	metrics     importer.ProgressObserver
}

// New builds a Service from cfg, applying defaults for the optional tunables. It
// panics if any required collaborator is nil, since none has a sensible default
// and a missing one is a wiring bug that should surface at startup.
func New(cfg Config) *Service {
	requireCollaborators(cfg)
	pageSize := cfg.PageSize
	if pageSize <= 0 {
		pageSize = DefaultPageSize
	}
	logger := cfg.Logger
	if logger == nil {
		logger = slog.Default()
	}
	prober := cfg.Prober
	if prober == nil {
		prober = defaultProber{}
	}
	metrics := cfg.Metrics
	if metrics == nil {
		metrics = importer.NopProgressObserver{}
	}
	albumTypes := cfg.AlbumTypes
	if len(albumTypes) == 0 {
		albumTypes = DefaultAlbumTypes
	}
	return &Service{
		client:      cfg.Client,
		runs:        cfg.Runs,
		photos:      cfg.Photos,
		storage:     cfg.Storage,
		thumbs:      cfg.Thumbnailer,
		albums:      cfg.Albums,
		labels:      cfg.Labels,
		people:      cfg.People,
		enqueuer:    cfg.Enqueuer,
		prober:      prober,
		pageSize:    pageSize,
		albumTypes:  albumTypes,
		tempDir:     cfg.TempDir,
		maxFileSize: cfg.MaxFileSize,
		log:         logger,
		metrics:     metrics,
	}
}

// requireCollaborators panics when any required Config collaborator is nil.
func requireCollaborators(cfg Config) {
	if cfg.Client == nil || cfg.Runs == nil || cfg.Photos == nil || cfg.Storage == nil ||
		cfg.Thumbnailer == nil || cfg.Albums == nil || cfg.Labels == nil ||
		cfg.People == nil || cfg.Enqueuer == nil {
		panic("ppimport: New requires Client, Runs, Photos, Storage, Thumbnailer, " +
			"Albums, Labels, People and Enqueuer")
	}
}

// Result summarises a completed import run.
type Result struct {
	// RunID is the import_runs row id of this run.
	RunID int64 `json:"run_id"`
	// Counts is the final tally of imported/updated/skipped/failed photos.
	Counts importer.Counts `json:"counts"`
	// Watermark is the resume cursor recorded for the next incremental run, or
	// nil when no new cursor was produced.
	Watermark *time.Time `json:"watermark,omitempty"`
}

// Import runs one full pass: it opens an import run, resumes from the last
// successful watermark, imports photos page by page (checkpointing counts), maps
// albums, labels and people, and closes the run as done with the new watermark.
// A per-photo failure is recorded and never aborts the run; only an
// infrastructure failure (cannot list, cannot reach the run store) fails the run
// and returns an error. The returned Result is always populated with the run id.
func (s *Service) Import(ctx context.Context) (Result, error) {
	run, err := s.runs.Start(ctx, importer.SourcePhotoPrism)
	if err != nil {
		return Result{}, fmt.Errorf("ppimport: starting run: %w", err)
	}
	state := &runState{}
	if err := s.runImport(ctx, run.ID, state); err != nil {
		if failErr := s.runs.Fail(ctx, run.ID, err.Error(), state.counts); failErr != nil {
			s.log.Error("ppimport: marking run failed", "run", run.ID, "err", failErr)
		}
		return Result{RunID: run.ID, Counts: state.counts}, err
	}
	watermark := state.watermark()
	if err := s.runs.Complete(ctx, run.ID, watermark, state.counts); err != nil {
		return Result{RunID: run.ID, Counts: state.counts}, fmt.Errorf("ppimport: completing run: %w", err)
	}
	return Result{RunID: run.ID, Counts: state.counts, Watermark: watermark}, nil
}

// ImportScoped runs one scoped pass: it imports every photo the Scope selects —
// an album, a label, a person, a year, or any combination of them — regardless
// of the incremental watermark, and brings each of those photos across whole: all
// the albums it belongs to and all the labels it carries are mapped (not just the
// one the scope named), and the people on it are seeded from its face markers. It
// is how the library is migrated in slices, and how the import is verified end to
// end against a production PhotoPrism without walking the whole catalogue. An
// empty scope is ErrEmptyScope: a full run is Import.
//
// A scoped run deliberately does NOT advance the watermark. It sees a slice of
// the library only, so recording its newest timestamp as the resume cursor would
// make the next full import skip every older photo — the one way this
// convenience could quietly lose data.
func (s *Service) ImportScoped(ctx context.Context, scope Scope) (Result, error) {
	scope = scope.normalized()
	if err := scope.validate(); err != nil {
		return Result{}, err
	}
	run, err := s.runs.Start(ctx, importer.SourcePhotoPrism)
	if err != nil {
		return Result{}, fmt.Errorf("ppimport: starting run: %w", err)
	}
	state := &runState{scope: scope}
	if err := s.runScopedImport(ctx, run.ID, state); err != nil {
		if failErr := s.runs.Fail(ctx, run.ID, err.Error(), state.counts); failErr != nil {
			s.log.Error("ppimport: marking run failed", "run", run.ID, "err", failErr)
		}
		return Result{RunID: run.ID, Counts: state.counts}, err
	}
	if err := s.runs.Complete(ctx, run.ID, nil, state.counts); err != nil {
		return Result{RunID: run.ID, Counts: state.counts}, fmt.Errorf("ppimport: completing run: %w", err)
	}
	return Result{RunID: run.ID, Counts: state.counts}, nil
}

// runScopedImport checks the scope names something the source knows, then imports
// the selected photos — each of which maps its own whole context as it goes
// (mapPhotoContext), against the album and label indexes read once up front. Any
// returned error is an infrastructure failure that should fail the whole run.
func (s *Service) runScopedImport(ctx context.Context, runID int64, state *runState) error {
	if err := s.validateScope(ctx, state.scope); err != nil {
		return err
	}
	pctx, err := s.newPhotoContext(ctx)
	if err != nil {
		return err
	}
	state.photoCtx = pctx
	return s.importPhotos(ctx, runID, state)
}

// runImport drives the three import phases over the open run, resuming from the
// last successful watermark. Any returned error is an infrastructure failure that
// should fail the whole run.
func (s *Service) runImport(ctx context.Context, runID int64, state *runState) error {
	since, _, err := s.runs.LatestWatermark(ctx, importer.SourcePhotoPrism)
	if err != nil {
		return fmt.Errorf("ppimport: reading watermark: %w", err)
	}
	state.since = since
	if err := s.importPhotos(ctx, runID, state); err != nil {
		return err
	}
	if err := s.mapAlbums(ctx); err != nil {
		return err
	}
	return s.mapLabels(ctx)
}

// runState accumulates a run's tally and the watermark bookkeeping. The watermark
// advances only to the largest source timestamp that can be advanced past without
// skipping a failed photo: it is the max UpdatedAt of successfully processed
// photos, capped to never exceed the earliest failed photo's UpdatedAt, so a
// failure is always re-listed (inclusively) by the next incremental run.
type runState struct {
	since time.Time
	// scope, when non-empty, narrows the photo listing to a slice of the source
	// catalogue and marks the run as partial (no watermark is recorded for it).
	scope Scope
	// photoCtx is set for a scoped run only: it carries the album and label
	// indexes each imported photo maps its own context against. It is nil for a
	// full run, which maps the structure by walking the source catalogue instead.
	photoCtx   *photoContext
	counts     importer.Counts
	maxSuccess time.Time
	minFailed  time.Time
	hasFailed  bool
	sawAny     bool
}

// recordSuccess advances the success watermark to include updatedAt.
func (st *runState) recordSuccess(updatedAt time.Time) {
	st.sawAny = true
	if updatedAt.After(st.maxSuccess) {
		st.maxSuccess = updatedAt
	}
}

// recordFailure tallies a failed photo and tracks the earliest failure timestamp
// so the watermark never advances past it.
func (st *runState) recordFailure(updatedAt time.Time) {
	st.sawAny = true
	st.counts.Failed++
	if !st.hasFailed || updatedAt.Before(st.minFailed) {
		st.minFailed = updatedAt
		st.hasFailed = true
	}
}

// watermark returns the resume cursor for the next run, or nil when nothing was
// processed or no positive cursor could be derived.
func (st *runState) watermark() *time.Time {
	if !st.sawAny {
		return nil
	}
	cursor := st.maxSuccess
	if st.hasFailed && st.minFailed.Before(cursor) {
		cursor = st.minFailed
	}
	if cursor.IsZero() {
		return nil
	}
	return &cursor
}
