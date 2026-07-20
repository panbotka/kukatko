// Package psfeedsimport enriches PhotoPrism-imported photos with photo-sorter's
// already-computed CLIP image embeddings and InsightFace face vectors, copied
// 1:1 from photo-sorter's read-only migration feeds (internal/psfeeds).
//
// In production photo-sorter holds no photos of its own — it is a vector/faces
// layer keyed by the PhotoPrism photo UID. So the migration imports the photos
// from PhotoPrism (internal/ppimport) and this importer attaches each feed item
// to the Kukátko photo whose photoprism_uid equals the feed's photo_uid. Copying
// the vectors verbatim means the often-offline GPU box never has to recompute the
// migrated library's ~20k embeddings and ~112k faces.
//
// The run has two passes over the feeds:
//
//   - Embeddings: each item's 768-dim CLIP vector is upserted onto its photo
//     (idempotent; one row per photo).
//   - Faces: each photo's faces are recorded together (an atomic replace), the
//     pixel bounding box normalised via facejob.NormalizeBBox, and the marker and
//     subject the feed carries are materialised — subjects matched by name slug
//     (reusing any created by the PhotoPrism import), markers reused by their
//     preserved UID — so people and faces come across, not just raw vectors.
//
// A feed entry whose photo has not (yet) been imported from PhotoPrism is skipped
// and counted, never erroring the whole run, so the importer is safe to run
// before, during or after the PhotoPrism import and safe to re-run: every write
// is an upsert or a find-or-create guard, so a second pass converges. Each pass
// scans the whole feed (the feeds carry no incremental cursor); the recorded
// high-watermark is the newest item's timestamp and is informational only.
//
// Everything external sits behind an interface so the importer's tests substitute
// fakes without a real photo-sorter, network, or token.
package psfeedsimport

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/panbotka/kukatko/internal/importer"
	"github.com/panbotka/kukatko/internal/people"
	"github.com/panbotka/kukatko/internal/photos"
	"github.com/panbotka/kukatko/internal/psfeeds"
	"github.com/panbotka/kukatko/internal/vectors"
)

// DefaultPageSize is the requested feed page size when none is configured. The
// feed server clamps it to its own per-endpoint maximum.
const DefaultPageSize = 500

// Feeds is the read-only photo-sorter feeds contract the importer pages. It is
// the import-facing subset of psfeeds.Client, satisfied by *psfeeds.HTTPClient.
type Feeds interface {
	// ListEmbeddings returns one page of the embeddings feed for the keyset cursor
	// after (empty to start); NextAfter is nil at the end of the walk.
	ListEmbeddings(ctx context.Context, limit int, after string) (psfeeds.EmbeddingsPage, error)
	// ListFaces returns one page of the faces feed for the keyset cursor after
	// (zero to start); NextAfter is nil at the end of the walk.
	ListFaces(ctx context.Context, limit int, after int64) (psfeeds.FacesPage, error)
}

// PhotoStore resolves the Kukátko photo a feed item attaches to. It is the
// import-facing subset of photos.Store.
type PhotoStore interface {
	// GetByPhotoprismUID returns the photo with the given PhotoPrism UID, or
	// photos.ErrPhotoNotFound when it has not been imported yet.
	GetByPhotoprismUID(ctx context.Context, ppUID string) (photos.Photo, error)
}

// VectorStore stores the copied embeddings and faces. It is the import-facing
// subset of vectors.Store.
type VectorStore interface {
	// SaveEmbedding upserts a photo's image embedding (idempotent on photo_uid).
	SaveEmbedding(ctx context.Context, emb vectors.Embedding) (vectors.Embedding, error)
	// RecordFaceDetection atomically replaces a photo's faces and marks it
	// processed, so a re-run overwrites rather than duplicates.
	RecordFaceDetection(ctx context.Context, photoUID string, faces []vectors.Face, model string) error
}

// PeopleStore materialises the subjects and markers the faces feed carries. It is
// the import-facing subset of people.Store.
type PeopleStore interface {
	// GetSubjectBySlug returns the subject with the given slug, or
	// people.ErrSubjectNotFound.
	GetSubjectBySlug(ctx context.Context, slug string) (people.Subject, error)
	// CreateSubject inserts a new subject, generating its UID and slug.
	CreateSubject(ctx context.Context, subj people.Subject) (people.Subject, error)
	// GetMarkerByUID returns the marker with the given UID, or
	// people.ErrMarkerNotFound.
	GetMarkerByUID(ctx context.Context, uid string) (people.Marker, error)
	// CreateMarker inserts a marker (with the caller-supplied UID), keeping the
	// faces cache consistent.
	CreateMarker(ctx context.Context, m people.Marker) (people.Marker, error)
}

// RunStore records the import-run bookkeeping. It is the import-facing subset of
// importer.Store.
type RunStore interface {
	// Start opens a new run for the given source.
	Start(ctx context.Context, source importer.Source) (importer.Run, error)
	// UpdateCounts checkpoints the running counts after each page.
	UpdateCounts(ctx context.Context, id int64, counts importer.Counts) error
	// Complete marks the run done with its final watermark and counts; it reports
	// the run 'partial' rather than 'done' when unresolved failures were recorded.
	Complete(ctx context.Context, id int64, watermark *time.Time, counts importer.Counts) error
	// Fail marks the run failed with the error and the counts reached so far.
	Fail(ctx context.Context, id int64, lastErr string, counts importer.Counts) error
	// RecordFailures persists the per-item failures of a run so they can be listed
	// and retried instead of being lost to the log.
	RecordFailures(ctx context.Context, failures []importer.Failure) error
}

// Config bundles the importer's collaborators. All are required; a nil one is a
// wiring bug that panics in New. PageSize and Logger are optional.
type Config struct {
	// Feeds is the read-only photo-sorter feeds client.
	Feeds Feeds
	// Photos resolves the target Kukátko photo by PhotoPrism UID.
	Photos PhotoStore
	// Vectors stores the copied embeddings and faces.
	Vectors VectorStore
	// People materialises the subjects and markers the faces feed carries.
	People PeopleStore
	// Runs records the import-run bookkeeping.
	Runs RunStore
	// PageSize is the requested feed page size; non-positive uses DefaultPageSize.
	PageSize int
	// Logger receives per-item diagnostics; nil uses slog.Default().
	Logger *slog.Logger
}

// Service runs the photo-sorter feeds import.
type Service struct {
	feeds    Feeds
	photos   PhotoStore
	vectors  VectorStore
	people   PeopleStore
	runs     RunStore
	pageSize int
	log      *slog.Logger
}

// New assembles a Service from cfg, applying the page-size and logger defaults. It
// panics when a required collaborator is nil, surfacing a wiring bug at startup.
func New(cfg Config) *Service {
	cfg.requireCollaborators()
	pageSize := cfg.PageSize
	if pageSize <= 0 {
		pageSize = DefaultPageSize
	}
	logger := cfg.Logger
	if logger == nil {
		logger = slog.Default()
	}
	return &Service{
		feeds:    cfg.Feeds,
		photos:   cfg.Photos,
		vectors:  cfg.Vectors,
		people:   cfg.People,
		runs:     cfg.Runs,
		pageSize: pageSize,
		log:      logger,
	}
}

// requireCollaborators panics when any required collaborator is nil.
func (c Config) requireCollaborators() {
	switch {
	case c.Feeds == nil:
		panic("psfeedsimport: New requires Feeds")
	case c.Photos == nil:
		panic("psfeedsimport: New requires Photos")
	case c.Vectors == nil:
		panic("psfeedsimport: New requires Vectors")
	case c.People == nil:
		panic("psfeedsimport: New requires People")
	case c.Runs == nil:
		panic("psfeedsimport: New requires Runs")
	}
}

// Result summarises one import run.
type Result struct {
	// RunID is the import_runs row id.
	RunID int64
	// Counts is the run's imported/updated/skipped/failed tally. Imported counts
	// embeddings written plus photos whose faces were recorded; Skipped counts
	// feed entries whose photo is not imported yet; Failed counts entries that
	// errored without aborting the run. Updated is unused (writes upsert).
	Counts importer.Counts
	// Watermark is the newest feed-item timestamp processed, or nil when the run
	// wrote nothing. It is informational: the passes always full-scan.
	Watermark *time.Time
}

// runState accumulates the counts, the newest item timestamp and the per-item
// failures across both feed passes of one run. It is threaded by pointer, so a
// failure recorded during either pass survives to the persist call before the run
// is closed.
type runState struct {
	// runID is the import_runs id this run records its failures against.
	runID int64
	// failures accumulates the per-item failures recorded during the run, persisted
	// once before the run is closed so a run with any unresolved failure is reported
	// 'partial' rather than 'done'.
	failures     []importer.Failure
	counts       importer.Counts
	maxCreatedAt time.Time
}

// recordItemFailure appends a per-item failure to the run's failure list so it is
// persisted (and the run reported 'partial') instead of only logged. photoUID is
// the resolved Kukátko uid when known, sourceRef the feed item's photo_uid, and
// detail a short hint such as the marker uid.
func (st *runState) recordItemFailure(stage importer.Stage, photoUID, sourceRef, detail string, err error) {
	st.failures = append(st.failures, importer.NewFailure(
		st.runID, importer.SourcePhotoSorterFeeds, stage, photoUID, sourceRef, detail, err))
}

// trackTime advances the run's high-watermark to the newest item timestamp seen.
func (st *runState) trackTime(t time.Time) {
	if t.After(st.maxCreatedAt) {
		st.maxCreatedAt = t
	}
}

// watermark returns the newest item timestamp seen, or nil when none was.
func (st *runState) watermark() *time.Time {
	if st.maxCreatedAt.IsZero() {
		return nil
	}
	t := st.maxCreatedAt
	return &t
}

// Import runs one full feeds import: it opens a run, pages the embeddings then the
// faces feed, and completes (or fails) the run. Per-item problems (a photo not yet
// imported, a bad vector) are counted and do not abort; an infrastructure error
// (a feed fetch or a database failure) fails the run and is returned.
func (s *Service) Import(ctx context.Context) (Result, error) {
	run, err := s.runs.Start(ctx, importer.SourcePhotoSorterFeeds)
	if err != nil {
		return Result{}, fmt.Errorf("starting feeds import run: %w", err)
	}
	st := &runState{runID: run.ID}
	if err := s.importEmbeddings(ctx, run.ID, st); err != nil {
		return s.failRun(ctx, run.ID, st, err)
	}
	if err := s.importFaces(ctx, run.ID, st); err != nil {
		return s.failRun(ctx, run.ID, st, err)
	}
	s.persistFailures(ctx, st)
	watermark := st.watermark()
	if err := s.runs.Complete(ctx, run.ID, watermark, st.counts); err != nil {
		return Result{RunID: run.ID, Counts: st.counts}, fmt.Errorf("completing feeds import run: %w", err)
	}
	return Result{RunID: run.ID, Counts: st.counts, Watermark: watermark}, nil
}

// failRun persists the run's per-item failures, then marks the run failed with
// cause and returns cause. A failure to record either is logged, not masked over
// the original error.
func (s *Service) failRun(ctx context.Context, runID int64, st *runState, cause error) (Result, error) {
	s.persistFailures(ctx, st)
	if err := s.runs.Fail(ctx, runID, cause.Error(), st.counts); err != nil {
		s.log.Warn("psfeedsimport: recording run failure", "run", runID, "err", err)
	}
	return Result{RunID: runID, Counts: st.counts}, cause
}

// persistFailures writes the run's accumulated per-item failures. A persistence
// error is logged, not fatal: the run still closes, only losing the itemised
// failure trail (the aggregate Failed count is still recorded).
func (s *Service) persistFailures(ctx context.Context, st *runState) {
	if err := s.runs.RecordFailures(ctx, st.failures); err != nil {
		s.log.Error("psfeedsimport: recording import failures", "run", st.runID, "err", err)
	}
}
