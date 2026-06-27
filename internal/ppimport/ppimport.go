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
// On top of the photos it maps the surrounding structure: albums and labels are
// found-or-created by name and their membership attached to the imported photos,
// and named face markers seed people (subjects) and their markers.
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
)

// DefaultPageSize is the listing page size used when Config.PageSize is left
// zero. It matches PhotoPrism's hard cap so a full library is walked in the
// fewest requests.
const DefaultPageSize = photoprism.MaxCount

// PhotoPrismClient is the read-only PhotoPrism contract the importer needs: photo
// listing (incremental or scoped to an album/label) and original download, plus
// the album and label catalogues. It is the import-facing subset of
// photoprism.Client.
type PhotoPrismClient interface {
	// ListPhotos returns one page of photos for the given params.
	ListPhotos(ctx context.Context, params photoprism.PhotoListParams) ([]photoprism.Photo, error)
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
	ListAlbums(ctx context.Context) ([]organize.AlbumCount, error)
	// CreateAlbum inserts a new album.
	CreateAlbum(ctx context.Context, a organize.Album) (organize.Album, error)
	// AddPhoto adds a photo to an album at the given sort key (idempotent).
	AddPhoto(ctx context.Context, albumUID, photoUID string, sortKey int) error
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
}

// Enqueuer schedules the post-import background jobs. It is satisfied by
// jobs.Enqueuer.
type Enqueuer interface {
	// EnqueueImageEmbed schedules embedding for a photo (dedup no-op).
	EnqueueImageEmbed(ctx context.Context, photoUID string) error
	// EnqueueFaceDetect schedules face detection for a photo (dedup no-op).
	EnqueueFaceDetect(ctx context.Context, photoUID string) error
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
	// PageSize is the listing page size (default DefaultPageSize).
	PageSize int
	// TempDir is where downloads are staged before publishing ("" uses the OS
	// temp directory).
	TempDir string
	// MaxFileSize caps a single downloaded original in bytes; 0 means unlimited.
	MaxFileSize int64
	// Logger receives per-item failure diagnostics; nil uses slog.Default().
	Logger *slog.Logger
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
	pageSize    int
	tempDir     string
	maxFileSize int64
	log         *slog.Logger
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
		pageSize:    pageSize,
		tempDir:     cfg.TempDir,
		maxFileSize: cfg.MaxFileSize,
		log:         logger,
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
	since      time.Time
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
