// Package importverify is a read-only import-completeness reconciliation tool. It
// enumerates the source libraries — the whole PhotoPrism photo catalogue and,
// when configured, photo-sorter's pre-computed embeddings/faces feeds — and
// compares them against the Kukátko catalogue in Postgres, answering "the source
// has N, Kukátko has M" together with a concrete, capped list of what is still
// missing.
//
// It is strictly reconciliation, not import: it never writes to the catalogue and
// never opens an import_runs row. External dependencies are reached only through
// the narrow PhotoPrismSource, FeedsSource and Catalog interfaces, so the whole
// reconciler is unit-testable with in-memory fakes; a concrete Store backs
// Catalog over a pgx pool.
package importverify

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"strings"

	"github.com/panbotka/kukatko/internal/photoprism"
	"github.com/panbotka/kukatko/internal/psfeeds"
)

// DefaultSampleLimit is the number of ids listed per "missing" list when Config
// leaves SampleLimit unset; the counts always stay the full total regardless.
const DefaultSampleLimit = 100

// PhotoPrismSource is the read-only slice of the PhotoPrism client the reconciler
// needs: the full photo listing plus the album, label and subject listings. It is
// satisfied by *photoprism.HTTPClient and by any photoprism.Client.
type PhotoPrismSource interface {
	// ListPhotos returns one page of photos for the given params. The reconciler
	// pages a full, unfiltered listing by advancing Offset until a short page.
	ListPhotos(ctx context.Context, params photoprism.PhotoListParams) ([]photoprism.Photo, error)
	// ListAlbums returns one page of albums of a single album type (params.Type).
	ListAlbums(ctx context.Context, params photoprism.ListParams) ([]photoprism.Album, error)
	// ListLabels returns one page of labels.
	ListLabels(ctx context.Context, params photoprism.ListParams) ([]photoprism.Label, error)
	// ListSubjects returns one page of subjects (people).
	ListSubjects(ctx context.Context, params photoprism.ListParams) ([]photoprism.Subject, error)
}

// FeedsSource is the read-only slice of photo-sorter's feeds client the
// reconciler needs: the aggregate completeness stats. It is satisfied by
// *psfeeds.HTTPClient and by any psfeeds.Client.
type FeedsSource interface {
	// Stats returns photo-sorter's aggregate embeddings/faces totals.
	Stats(ctx context.Context) (psfeeds.Stats, error)
}

// CatalogCounts holds the catalogue aggregates the reconciler compares against
// the sources. The embeddings and faces counts are restricted to
// PhotoPrism-imported photos so they line up with photo-sorter's population.
type CatalogCounts struct {
	// Photos is the total number of catalogue photos.
	Photos int
	// PhotoprismImported is the number of photos with a non-null photoprism_uid.
	PhotoprismImported int
	// Embeddings is the number of embeddings rows over PhotoPrism-imported photos.
	Embeddings int
	// FacePhotos is the number of PhotoPrism-imported photos that have faces.
	FacePhotos int
	// Faces is the total number of face rows over PhotoPrism-imported photos.
	Faces int
	// Albums, Labels and Subjects are the catalogue's structural row counts.
	Albums   int
	Labels   int
	Subjects int
}

// Catalog is the read-only view of the Kukátko catalogue the reconciler needs. It
// is an interface so the reconciler is testable with an in-memory fake; the
// concrete Store implements it over a pgx pool.
type Catalog interface {
	// ImportedRefs returns the sets of photoprism_uid and photoprism_file_hash of
	// imported photos, used to classify each source photo as imported or
	// deduplicated.
	ImportedRefs(ctx context.Context) (uids map[string]struct{}, fileHashes map[string]struct{}, err error)
	// OriginalFileCounts maps photoprism_uid to the number of role='original'
	// photo_files for that photo.
	OriginalFileCounts(ctx context.Context) (map[string]int, error)
	// Counts returns the catalogue aggregates for reconciliation.
	Counts(ctx context.Context) (CatalogCounts, error)
	// PhotosMissingEmbeddings returns up to limit photoprism_uids of imported
	// photos lacking an embeddings row, plus the full total.
	PhotosMissingEmbeddings(ctx context.Context, limit int) (sample []string, total int, err error)
	// PhotosMissingFaces returns up to limit photoprism_uids of imported photos
	// lacking a face-detection record, plus the full total.
	PhotosMissingFaces(ctx context.Context, limit int) (sample []string, total int, err error)
	// AlbumTitles returns the set of catalogue album titles.
	AlbumTitles(ctx context.Context) (map[string]struct{}, error)
	// LabelNames returns the set of catalogue label names.
	LabelNames(ctx context.Context) (map[string]struct{}, error)
	// SubjectNames returns the set of catalogue subject names.
	SubjectNames(ctx context.Context) (map[string]struct{}, error)
}

// Config configures a Service. PhotoPrism and Catalog are required; Feeds is
// optional (nil marks the vectors section NotConfigured). The remaining knobs
// fall back to package defaults when left zero/nil.
type Config struct {
	// PhotoPrism is the required PhotoPrism source.
	PhotoPrism PhotoPrismSource
	// Feeds is the optional photo-sorter feeds source; nil skips the vectors
	// section and marks it NotConfigured.
	Feeds FeedsSource
	// Catalog is the required catalogue view.
	Catalog Catalog
	// SampleLimit caps every "missing" list; a non-positive value uses
	// DefaultSampleLimit.
	SampleLimit int
	// AlbumTypes are the PhotoPrism album types to walk; empty uses
	// photoprism.AlbumTypes.
	AlbumTypes []string
	// Logger receives a debug line per completed pass; nil uses slog.Default().
	Logger *slog.Logger
}

// Service reconciles the source libraries against the catalogue. It holds no
// mutable state and is safe for concurrent use.
type Service struct {
	photoPrism  PhotoPrismSource
	feeds       FeedsSource
	catalog     Catalog
	sampleLimit int
	albumTypes  []string
	log         *slog.Logger
}

// NewService builds a Service from cfg, applying defaults for the optional knobs.
// It panics if PhotoPrism or Catalog is nil, since neither has a sensible default
// and a missing one is a wiring bug that should surface at startup.
func NewService(cfg Config) *Service {
	if cfg.PhotoPrism == nil || cfg.Catalog == nil {
		panic("importverify: NewService requires PhotoPrism and Catalog")
	}
	sampleLimit := cfg.SampleLimit
	if sampleLimit <= 0 {
		sampleLimit = DefaultSampleLimit
	}
	albumTypes := cfg.AlbumTypes
	if len(albumTypes) == 0 {
		albumTypes = photoprism.AlbumTypes
	}
	logger := cfg.Logger
	if logger == nil {
		logger = slog.Default()
	}
	return &Service{
		photoPrism:  cfg.PhotoPrism,
		feeds:       cfg.Feeds,
		catalog:     cfg.Catalog,
		sampleLimit: sampleLimit,
		albumTypes:  albumTypes,
		log:         logger,
	}
}

// Verify runs a full reconciliation pass across the photos, vectors and structure
// sections and returns the assembled Report. It aborts with a wrapped error if
// any source listing or catalogue query fails; a nil Feeds source is not an error
// but marks the vectors section NotConfigured.
func (s *Service) Verify(ctx context.Context) (Report, error) {
	photoReport, err := s.reconcilePhotos(ctx)
	if err != nil {
		return Report{}, err
	}
	counts, err := s.catalog.Counts(ctx)
	if err != nil {
		return Report{}, fmt.Errorf("importverify: reading catalog counts: %w", err)
	}
	vectorsReport, err := s.reconcileVectors(ctx, counts)
	if err != nil {
		return Report{}, err
	}
	structureReport, err := s.reconcileStructure(ctx, counts)
	if err != nil {
		return Report{}, err
	}
	report := Report{
		PhotoPrism: photoReport,
		Vectors:    vectorsReport,
		Structure:  structureReport,
	}
	report.Complete = isComplete(report)
	s.log.DebugContext(ctx, "import verification reconciled",
		"complete", report.Complete,
		"missing_photos", report.PhotoPrism.MissingCount,
		"file_gaps", report.PhotoPrism.FileGapCount,
	)
	return report, nil
}

// photoRef is the compact per-photo record the reconciler keeps while enumerating
// the PhotoPrism library, so classification runs without holding whole photos.
type photoRef struct {
	uid           string
	primaryHash   string
	expectedFiles int
}

// reconcilePhotos enumerates the PhotoPrism library and classifies each photo
// against the catalogue into imported, deduplicated or missing, recording file
// gaps for imported photos with fewer catalogue originals than source files.
func (s *Service) reconcilePhotos(ctx context.Context) (PhotoPrismReport, error) {
	refs, byType, err := s.enumeratePhotos(ctx)
	if err != nil {
		return PhotoPrismReport{}, err
	}
	importedUIDs, importedHashes, err := s.catalog.ImportedRefs(ctx)
	if err != nil {
		return PhotoPrismReport{}, fmt.Errorf("importverify: reading imported refs: %w", err)
	}
	fileCounts, err := s.catalog.OriginalFileCounts(ctx)
	if err != nil {
		return PhotoPrismReport{}, fmt.Errorf("importverify: reading original file counts: %w", err)
	}
	return s.classifyPhotos(refs, byType, importedUIDs, importedHashes, fileCounts), nil
}

// enumeratePhotos pages the whole, unfiltered PhotoPrism photo listing to
// exhaustion, returning one photoRef per photo and the per-type histogram. It
// advances the offset by each page's length and stops on a short page.
func (s *Service) enumeratePhotos(ctx context.Context) ([]photoRef, map[string]int, error) {
	refs := make([]photoRef, 0)
	byType := make(map[string]int)
	offset := 0
	for {
		page, err := s.photoPrism.ListPhotos(ctx, photoprism.PhotoListParams{
			Count:  photoprism.MaxCount,
			Offset: offset,
		})
		if err != nil {
			return nil, nil, fmt.Errorf("importverify: listing photoprism photos at offset %d: %w", offset, err)
		}
		for i := range page {
			byType[strings.ToLower(page[i].Type)]++
			hash := ""
			if primary, ok := page[i].PrimaryFile(); ok {
				hash = primary.Hash
			}
			refs = append(refs, photoRef{
				uid:           page[i].UID,
				primaryHash:   hash,
				expectedFiles: len(page[i].Files),
			})
		}
		if len(page) < photoprism.MaxCount {
			return refs, byType, nil
		}
		offset += len(page)
	}
}

// classifyPhotos buckets each enumerated photo against the catalogue sets and
// assembles the PhotoPrismReport, capping every listed slice at the sample limit
// while the counts stay the full totals.
func (s *Service) classifyPhotos(
	refs []photoRef,
	byType map[string]int,
	importedUIDs, importedHashes map[string]struct{},
	fileCounts map[string]int,
) PhotoPrismReport {
	report := PhotoPrismReport{
		SourceTotal:  len(refs),
		SourceByType: byType,
		MissingUIDs:  make([]string, 0),
		FileGaps:     make([]FileGap, 0),
	}
	for _, ref := range refs {
		switch {
		case contains(importedUIDs, ref.uid):
			report.ImportedCount++
			s.recordFileGap(&report, ref, fileCounts)
		case ref.primaryHash != "" && contains(importedHashes, ref.primaryHash):
			report.DeduplicatedCount++
		default:
			report.MissingCount++
			if len(report.MissingUIDs) < s.sampleLimit {
				report.MissingUIDs = append(report.MissingUIDs, ref.uid)
			}
		}
	}
	return report
}

// recordFileGap appends a FileGap for an imported photo whose catalogue
// original-file count is below its source file count, keeping the count full and
// the listed slice capped at the sample limit.
func (s *Service) recordFileGap(report *PhotoPrismReport, ref photoRef, fileCounts map[string]int) {
	actual := fileCounts[ref.uid]
	if ref.expectedFiles <= actual {
		return
	}
	report.FileGapCount++
	if len(report.FileGaps) < s.sampleLimit {
		report.FileGaps = append(report.FileGaps, FileGap{
			PhotoprismUID: ref.uid,
			Expected:      ref.expectedFiles,
			Actual:        actual,
		})
	}
}

// reconcileVectors builds the vectors section. With no feeds source it returns a
// NotConfigured report; otherwise it reads the feed stats and the catalogue's
// missing-embeddings/missing-faces samples.
func (s *Service) reconcileVectors(ctx context.Context, counts CatalogCounts) (VectorsReport, error) {
	if s.feeds == nil {
		return VectorsReport{
			NotConfigured:     true,
			MissingEmbeddings: make([]string, 0),
			MissingFaces:      make([]string, 0),
		}, nil
	}
	stats, err := s.feeds.Stats(ctx)
	if err != nil {
		return VectorsReport{}, fmt.Errorf("importverify: reading feeds stats: %w", err)
	}
	missingEmb, embTotal, err := s.catalog.PhotosMissingEmbeddings(ctx, s.sampleLimit)
	if err != nil {
		return VectorsReport{}, fmt.Errorf("importverify: reading photos missing embeddings: %w", err)
	}
	missingFaces, facesTotal, err := s.catalog.PhotosMissingFaces(ctx, s.sampleLimit)
	if err != nil {
		return VectorsReport{}, fmt.Errorf("importverify: reading photos missing faces: %w", err)
	}
	return VectorsReport{
		SourceTotalPhotos:          stats.TotalPhotos,
		SourcePhotosWithEmbeddings: stats.PhotosWithEmbeddings,
		SourcePhotosWithFaces:      stats.PhotosWithFaces,
		SourceTotalFaces:           stats.TotalFaces,
		CatalogEmbeddings:          counts.Embeddings,
		CatalogFacePhotos:          counts.FacePhotos,
		CatalogFaces:               counts.Faces,
		MissingEmbeddingsCount:     embTotal,
		MissingEmbeddings:          normalizeStrings(missingEmb),
		MissingFacesCount:          facesTotal,
		MissingFaces:               normalizeStrings(missingFaces),
	}, nil
}

// reconcileStructure builds the structure section by comparing the source name
// sets (albums by title, labels and subjects by name) against the catalogue sets,
// taking the catalogue row counts from counts.
func (s *Service) reconcileStructure(ctx context.Context, counts CatalogCounts) (StructureReport, error) {
	srcAlbums, srcLabels, srcSubjects, err := s.sourceStructure(ctx)
	if err != nil {
		return StructureReport{}, err
	}
	catAlbums, err := s.catalog.AlbumTitles(ctx)
	if err != nil {
		return StructureReport{}, fmt.Errorf("importverify: reading catalog album titles: %w", err)
	}
	catLabels, err := s.catalog.LabelNames(ctx)
	if err != nil {
		return StructureReport{}, fmt.Errorf("importverify: reading catalog label names: %w", err)
	}
	catSubjects, err := s.catalog.SubjectNames(ctx)
	if err != nil {
		return StructureReport{}, fmt.Errorf("importverify: reading catalog subject names: %w", err)
	}
	return StructureReport{
		Albums:   s.entityReport(srcAlbums, catAlbums, counts.Albums),
		Labels:   s.entityReport(srcLabels, catLabels, counts.Labels),
		Subjects: s.entityReport(srcSubjects, catSubjects, counts.Subjects),
	}, nil
}

// sourceStructure fully pages the PhotoPrism album (walked per type), label and
// subject listings and returns their deduplicated title/name sets.
func (s *Service) sourceStructure(
	ctx context.Context,
) (albums, labels, subjects map[string]struct{}, err error) {
	albums, err = s.sourceAlbumTitles(ctx)
	if err != nil {
		return nil, nil, nil, err
	}
	labels, err = collectAll(func(offset int) ([]photoprism.Label, error) {
		return s.photoPrism.ListLabels(ctx, photoprism.ListParams{Count: photoprism.MaxCount, Offset: offset})
	}, func(label photoprism.Label) string { return label.Name })
	if err != nil {
		return nil, nil, nil, fmt.Errorf("importverify: listing photoprism labels: %w", err)
	}
	subjects, err = collectAll(func(offset int) ([]photoprism.Subject, error) {
		return s.photoPrism.ListSubjects(ctx, photoprism.ListParams{Count: photoprism.MaxCount, Offset: offset})
	}, func(subject photoprism.Subject) string { return subject.Name })
	if err != nil {
		return nil, nil, nil, fmt.Errorf("importverify: listing photoprism subjects: %w", err)
	}
	return albums, labels, subjects, nil
}

// sourceAlbumTitles walks the PhotoPrism album listing once per configured album
// type — the listing takes exactly one type per request — and returns the merged,
// deduplicated set of album titles.
func (s *Service) sourceAlbumTitles(ctx context.Context) (map[string]struct{}, error) {
	titles := make(map[string]struct{})
	for _, albumType := range s.albumTypes {
		found, err := collectAll(func(offset int) ([]photoprism.Album, error) {
			return s.photoPrism.ListAlbums(ctx, photoprism.ListParams{
				Type:   albumType,
				Count:  photoprism.MaxCount,
				Offset: offset,
			})
		}, func(album photoprism.Album) string { return album.Title })
		if err != nil {
			return nil, fmt.Errorf("importverify: listing photoprism albums of type %q: %w", albumType, err)
		}
		for title := range found {
			titles[title] = struct{}{}
		}
	}
	return titles, nil
}

// entityReport reconciles one structural entity: it lists the source names absent
// from the catalogue set (sorted for determinism, capped at the sample limit) and
// keeps the full missing total alongside the source and catalogue counts.
func (s *Service) entityReport(source, catalog map[string]struct{}, catalogCount int) EntityReport {
	missing := make([]string, 0, len(source))
	for name := range source {
		if _, ok := catalog[name]; !ok {
			missing = append(missing, name)
		}
	}
	sort.Strings(missing)
	report := EntityReport{
		SourceCount:  len(source),
		CatalogCount: catalogCount,
		MissingCount: len(missing),
		Missing:      missing,
	}
	if len(report.Missing) > s.sampleLimit {
		report.Missing = report.Missing[:s.sampleLimit]
	}
	return report
}

// isComplete reports whether the report shows nothing left to import: no missing
// or file-gapped photos, no missing vectors (unless the vectors section is not
// configured), and no missing structural entities.
func isComplete(report Report) bool {
	if report.PhotoPrism.MissingCount != 0 || report.PhotoPrism.FileGapCount != 0 {
		return false
	}
	if !report.Vectors.NotConfigured &&
		(report.Vectors.MissingEmbeddingsCount != 0 || report.Vectors.MissingFacesCount != 0) {
		return false
	}
	return report.Structure.Albums.MissingCount == 0 &&
		report.Structure.Labels.MissingCount == 0 &&
		report.Structure.Subjects.MissingCount == 0
}

// collectAll pages a PhotoPrism list endpoint to exhaustion via fetch and returns
// the deduplicated set of keys produced by key. It advances the offset by each
// page's length and stops when a page returns fewer than photoprism.MaxCount
// items.
func collectAll[T any](fetch func(offset int) ([]T, error), key func(T) string) (map[string]struct{}, error) {
	set := make(map[string]struct{})
	offset := 0
	for {
		page, err := fetch(offset)
		if err != nil {
			return nil, err
		}
		for i := range page {
			set[key(page[i])] = struct{}{}
		}
		if len(page) < photoprism.MaxCount {
			return set, nil
		}
		offset += len(page)
	}
}

// contains reports whether key is present in set.
func contains(set map[string]struct{}, key string) bool {
	_, ok := set[key]
	return ok
}

// normalizeStrings returns a non-nil slice so an empty "missing" list marshals as
// [] rather than null.
func normalizeStrings(in []string) []string {
	if in == nil {
		return make([]string, 0)
	}
	return in
}
