// Package psimport migrates a photo-sorter database into Kukátko (see
// ARCHITECTURE.md §10). Because photo-sorter and Kukátko share the same embedding
// models and dimensions (CLIP 768 + InsightFace 512) and the same SHA256 file
// hashes, embeddings and faces transfer 1:1 without recomputation and photos
// deduplicate directly: a photo already present (for example imported from
// PhotoPrism) is matched by file_hash and simply gains its photo-sorter
// embeddings, faces and photosorter_uid.
//
// The migration is read-only against photo-sorter (via internal/photosorter),
// incremental (it resumes from the last successful run's high-watermark on
// photos.updated_at) and idempotent: re-running matches by photosorter_uid or
// file_hash and never duplicates, and the satellite transfers (embeddings, faces,
// markers, album/label membership, perceptual hashes, edits) all upsert, replace
// or guard on identity. A per-photo failure is recorded in the run counts and the
// migration continues; the watermark never advances past the earliest failure.
//
// Photo-book and share-link tables are deliberately out of scope and never read.
package psimport

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"time"

	"github.com/panbotka/kukatko/internal/importer"
	"github.com/panbotka/kukatko/internal/organize"
	"github.com/panbotka/kukatko/internal/people"
	"github.com/panbotka/kukatko/internal/photos"
	"github.com/panbotka/kukatko/internal/photosorter"
	"github.com/panbotka/kukatko/internal/storage"
	"github.com/panbotka/kukatko/internal/vectors"
)

// DefaultPageSize is the photo-listing page size used when Config.PageSize is
// non-positive.
const DefaultPageSize = 500

// Source is the read-only photo-sorter database, satisfied by
// *photosorter.Reader. Every method reads one table or one photo's satellites.
type Source interface {
	// ListPhotos returns one page of photos modified after params.UpdatedSince.
	ListPhotos(ctx context.Context, params photosorter.PhotoListParams) ([]photosorter.Photo, error)
	// ListSubjects returns one page of subjects.
	ListSubjects(ctx context.Context, params photosorter.ListParams) ([]photosorter.Subject, error)
	// ListAlbums returns one page of albums.
	ListAlbums(ctx context.Context, params photosorter.ListParams) ([]photosorter.Album, error)
	// ListLabels returns one page of labels.
	ListLabels(ctx context.Context, params photosorter.ListParams) ([]photosorter.Label, error)
	// Embedding returns the CLIP embedding for a photo, false when absent.
	Embedding(ctx context.Context, photoUID string) (photosorter.Embedding, bool, error)
	// Faces returns the detected faces for a photo.
	Faces(ctx context.Context, photoUID string) ([]photosorter.Face, error)
	// FacesProcessed reports whether face detection was recorded for a photo.
	FacesProcessed(ctx context.Context, photoUID string) (int, bool, error)
	// Phash returns the perceptual hashes for a photo, false when absent.
	Phash(ctx context.Context, photoUID string) (photosorter.Phash, bool, error)
	// Edit returns the non-destructive edits for a photo, false when absent.
	Edit(ctx context.Context, photoUID string) (photosorter.Edit, bool, error)
	// Markers returns the markers on a photo.
	Markers(ctx context.Context, photoUID string) ([]photosorter.Marker, error)
	// AlbumMemberships returns a photo's album memberships.
	AlbumMemberships(ctx context.Context, photoUID string) ([]photosorter.AlbumPhoto, error)
	// LabelMemberships returns a photo's label attachments.
	LabelMemberships(ctx context.Context, photoUID string) ([]photosorter.PhotoLabel, error)
}

// PhotoStore is the photo-catalogue subset the migration uses.
type PhotoStore interface {
	// GetByPhotosorterUID finds an already-migrated photo (ErrPhotoNotFound).
	GetByPhotosorterUID(ctx context.Context, psUID string) (photos.Photo, error)
	// GetByFileHash finds a photo by its SHA256 content hash (ErrPhotoNotFound).
	GetByFileHash(ctx context.Context, hash string) (photos.Photo, error)
	// SetPhotosorterRef backfills photosorter_uid onto a SHA256-deduped photo.
	SetPhotosorterRef(ctx context.Context, uid, psUID string) (photos.Photo, error)
	// Create inserts a new photo, returning ErrFileHashTaken on a content clash.
	Create(ctx context.Context, p photos.Photo) (photos.Photo, error)
	// CreateFile inserts a photo's primary file row.
	CreateFile(ctx context.Context, f photos.PhotoFile) (photos.PhotoFile, error)
	// SetPhash upserts a photo's perceptual hashes.
	SetPhash(ctx context.Context, p photos.Phash) error
	// SetEdit upserts a photo's non-destructive edits.
	SetEdit(ctx context.Context, e photos.Edit) error
	// Delete removes a photo (used to roll back a half-created record).
	Delete(ctx context.Context, uid string) error
}

// VectorStore is the embeddings/faces subset the migration uses; both operations
// are idempotent (upsert / atomic replace).
type VectorStore interface {
	// SaveEmbedding upserts a photo's CLIP image embedding.
	SaveEmbedding(ctx context.Context, emb vectors.Embedding) (vectors.Embedding, error)
	// RecordFaceDetection replaces a photo's faces and records the detection event.
	RecordFaceDetection(ctx context.Context, photoUID string, faces []vectors.Face, model string) error
}

// PeopleStore is the subjects/markers subset the migration uses.
type PeopleStore interface {
	// GetSubjectBySlug finds a subject by slug (ErrSubjectNotFound).
	GetSubjectBySlug(ctx context.Context, slug string) (people.Subject, error)
	// CreateSubject inserts a new subject.
	CreateSubject(ctx context.Context, subj people.Subject) (people.Subject, error)
	// GetMarkerByUID finds a marker by uid (ErrMarkerNotFound).
	GetMarkerByUID(ctx context.Context, uid string) (people.Marker, error)
	// CreateMarker inserts a face/label marker, optionally assigning a subject.
	CreateMarker(ctx context.Context, m people.Marker) (people.Marker, error)
}

// AlbumStore is the album subset the migration uses. Albums are find-or-created
// by title (via ListAlbums), mirroring the PhotoPrism importer, since organize's
// slug helper is unexported.
type AlbumStore interface {
	// ListAlbums lists existing albums with their photo counts.
	ListAlbums(ctx context.Context) ([]organize.AlbumSummary, error)
	// CreateAlbum inserts a new album.
	CreateAlbum(ctx context.Context, a organize.Album) (organize.Album, error)
	// AddPhoto adds a photo to an album (idempotent).
	AddPhoto(ctx context.Context, albumUID, photoUID string) error
}

// LabelStore is the label subset the migration uses. Labels are find-or-created
// by name (via ListLabels).
type LabelStore interface {
	// ListLabels lists existing labels with their photo counts.
	ListLabels(ctx context.Context) ([]organize.LabelCount, error)
	// CreateLabel inserts a new label.
	CreateLabel(ctx context.Context, l organize.Label) (organize.Label, error)
	// AttachLabel attaches a label to a photo (idempotent).
	AttachLabel(ctx context.Context, photoUID, labelUID string, source organize.LabelSource, uncertainty int) error
}

// RunStore records the migration run lifecycle and resume watermark. It mirrors
// the subset of importer.Store the migration uses.
type RunStore interface {
	// Start opens a new running migration run for source.
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

// Storage publishes copied originals into Kukátko's on-disk layout.
type Storage interface {
	// Store streams src into the layout and returns the stored file descriptor.
	Store(ctx context.Context, src io.Reader, takenAt time.Time, originalName string) (storage.StoredFile, error)
	// Delete removes a stored original by its relative path.
	Delete(ctx context.Context, relPath string) error
}

// Thumbnailer renders derived images for a freshly copied original.
type Thumbnailer interface {
	// GenerateAll renders every registered thumbnail size for photo.
	GenerateAll(ctx context.Context, photo photos.Photo) (map[string]string, error)
}

// Enqueuer schedules Kukátko's own embedding/face jobs for photos photo-sorter
// never processed, so coverage is filled in without recomputing what transferred.
type Enqueuer interface {
	// EnqueueImageEmbed schedules embedding for a photo (dedup no-op).
	EnqueueImageEmbed(ctx context.Context, photoUID string) error
	// EnqueueFaceDetect schedules face detection for a photo (dedup no-op).
	EnqueueFaceDetect(ctx context.Context, photoUID string) error
}

// Config bundles the dependencies and tunables of New. Every collaborator is
// required; OpenOriginal defaults to os.Open and PageSize to DefaultPageSize.
type Config struct {
	Source      Source
	Runs        RunStore
	Photos      PhotoStore
	Vectors     VectorStore
	People      PeopleStore
	Albums      AlbumStore
	Labels      LabelStore
	Storage     Storage
	Thumbnailer Thumbnailer
	Enqueuer    Enqueuer
	// OpenOriginal opens a photo-sorter original by its on-disk file_path; nil
	// uses os.Open. It is injected so the create path can be unit-tested without
	// real files on disk.
	OpenOriginal func(path string) (io.ReadCloser, error)
	// PageSize is the photo-listing page size; non-positive uses DefaultPageSize.
	PageSize int
	// Logger receives per-item failure diagnostics; nil uses slog.Default().
	Logger *slog.Logger
	// Metrics receives per-page progress tallies; nil disables instrumentation.
	Metrics importer.ProgressObserver
}

// Service runs the photo-sorter migration over the injected collaborators.
type Service struct {
	src      Source
	runs     RunStore
	photos   PhotoStore
	vectors  VectorStore
	people   PeopleStore
	albums   AlbumStore
	labels   LabelStore
	storage  Storage
	thumbs   Thumbnailer
	enqueuer Enqueuer
	open     func(path string) (io.ReadCloser, error)
	pageSize int
	log      *slog.Logger
	metrics  importer.ProgressObserver
}

// New builds a Service from cfg, applying defaults for the optional tunables. It
// panics if any required collaborator is nil, since none has a sensible default
// and a missing one is a wiring bug that should surface at startup.
func New(cfg Config) *Service {
	requireCollaborators(cfg)
	open := cfg.OpenOriginal
	if open == nil {
		// The path comes from the trusted photo-sorter database (its file_path
		// column), not from user input, so opening it directly is safe.
		open = func(path string) (io.ReadCloser, error) {
			return os.Open(path) //nolint:gosec // trusted photo-sorter file_path, not user input
		}
	}
	pageSize := cfg.PageSize
	if pageSize <= 0 {
		pageSize = DefaultPageSize
	}
	logger := cfg.Logger
	if logger == nil {
		logger = slog.Default()
	}
	metrics := cfg.Metrics
	if metrics == nil {
		metrics = importer.NopProgressObserver{}
	}
	return &Service{
		src: cfg.Source, runs: cfg.Runs, photos: cfg.Photos, vectors: cfg.Vectors,
		people: cfg.People, albums: cfg.Albums, labels: cfg.Labels,
		storage: cfg.Storage, thumbs: cfg.Thumbnailer, enqueuer: cfg.Enqueuer,
		open: open, pageSize: pageSize, log: logger, metrics: metrics,
	}
}

// requireCollaborators panics if any required collaborator in cfg is nil, since
// none has a sensible default and a missing one is a wiring bug.
func requireCollaborators(cfg Config) {
	checks := []struct {
		missing bool
		name    string
	}{
		{cfg.Source == nil, "Source"},
		{cfg.Runs == nil, "Runs store"},
		{cfg.Photos == nil, "Photos store"},
		{cfg.Vectors == nil, "Vectors store"},
		{cfg.People == nil, "People store"},
		{cfg.Albums == nil, "Albums store"},
		{cfg.Labels == nil, "Labels store"},
		{cfg.Storage == nil, "Storage"},
		{cfg.Thumbnailer == nil, "Thumbnailer"},
		{cfg.Enqueuer == nil, "Enqueuer"},
	}
	for _, c := range checks {
		if c.missing {
			panic("psimport: New requires a " + c.name)
		}
	}
}

// Result reports the outcome of one migration run.
type Result struct {
	// RunID is the import_runs row id of this run.
	RunID int64 `json:"run_id"`
	// Counts is the final tally of imported/updated/skipped/failed photos.
	Counts importer.Counts `json:"counts"`
	// Watermark is the resume cursor recorded for the next incremental run, or
	// nil when no new cursor was produced.
	Watermark *time.Time `json:"watermark,omitempty"`
}

// Migrate runs one full migration pass: it opens an import run, maps the
// photo-sorter catalogues (subjects, albums, labels) onto Kukátko, then pages
// through the photos modified since the last successful watermark, migrating each
// photo and its satellites. Per-photo failures are tallied without aborting; an
// infrastructure failure (listing or catalogue mapping) fails the run. On success
// the run is completed with the resume watermark. It returns the Result and a
// non-nil error only on an infrastructure failure.
func (s *Service) Migrate(ctx context.Context) (Result, error) {
	run, err := s.runs.Start(ctx, importer.SourcePhotoSorter)
	if err != nil {
		return Result{}, fmt.Errorf("psimport: starting run: %w", err)
	}
	maps, err := s.buildMappings(ctx)
	if err != nil {
		return s.failRun(ctx, run.ID, err, importer.Counts{})
	}
	since, _, err := s.runs.LatestWatermark(ctx, importer.SourcePhotoSorter)
	if err != nil {
		return s.failRun(ctx, run.ID, err, importer.Counts{})
	}

	state := &runState{since: since}
	if err := s.migratePhotos(ctx, run.ID, maps, state); err != nil {
		return s.failRun(ctx, run.ID, err, state.counts)
	}
	watermark := state.watermark()
	if err := s.runs.Complete(ctx, run.ID, watermark, state.counts); err != nil {
		return Result{}, fmt.Errorf("psimport: completing run: %w", err)
	}
	return Result{RunID: run.ID, Counts: state.counts, Watermark: watermark}, nil
}

// failRun fails the run identified by id with cause and counts, returning cause
// (the original failure) wrapped so callers see why the run aborted. A failure to
// record the failed state is logged but does not mask the original cause.
func (s *Service) failRun(ctx context.Context, id int64, cause error, counts importer.Counts) (Result, error) {
	if err := s.runs.Fail(ctx, id, cause.Error(), counts); err != nil {
		s.log.Error("psimport: recording failed run", "run", id, "err", err)
	}
	return Result{RunID: id, Counts: counts}, cause
}

// migratePhotos pages through the in-scope photos, migrating each and
// checkpointing the run counts after every page. Only a listing failure is
// returned (to fail the run); per-photo failures are recorded in state.
func (s *Service) migratePhotos(ctx context.Context, runID int64, maps mappings, state *runState) error {
	for offset := 0; ; {
		page, err := s.src.ListPhotos(ctx, photosorter.PhotoListParams{
			UpdatedSince: state.since,
			Limit:        s.pageSize,
			Offset:       offset,
		})
		if err != nil {
			return fmt.Errorf("psimport: listing photos at offset %d: %w", offset, err)
		}
		for i := range page {
			s.migrateOnePhoto(ctx, page[i], maps, state)
		}
		if err := s.runs.UpdateCounts(ctx, runID, state.counts); err != nil {
			return fmt.Errorf("psimport: checkpointing counts: %w", err)
		}
		c := state.counts
		s.metrics.SetImportProgress("photosorter", c.Imported, c.Updated, c.Skipped, c.Failed)
		if len(page) < s.pageSize {
			return nil
		}
		offset += len(page)
	}
}

// migrateOnePhoto migrates a single photo, translating its outcome (or failure)
// into the run state. A failure is logged and tallied; it never propagates.
func (s *Service) migrateOnePhoto(ctx context.Context, ps photosorter.Photo, maps mappings, state *runState) {
	result, err := s.processPhoto(ctx, ps, maps)
	if err != nil {
		s.log.Warn("psimport: photo failed", "ps_uid", ps.UID, "err", err)
		state.recordFailure(ps.UpdatedAt)
		return
	}
	state.recordSuccess(ps.UpdatedAt)
	switch result {
	case outcomeImported:
		state.counts.Imported++
	case outcomeUpdated:
		state.counts.Updated++
	case outcomeSkipped:
		state.counts.Skipped++
	}
}
