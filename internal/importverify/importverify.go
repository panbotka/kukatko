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
	"math"
	"slices"
	"sort"
	"strings"

	"github.com/panbotka/kukatko/internal/photoprism"
	"github.com/panbotka/kukatko/internal/ppimport"
	"github.com/panbotka/kukatko/internal/psfeeds"
)

// DefaultSampleLimit is the number of ids listed per "missing" list when Config
// leaves SampleLimit unset; the counts always stay the full total regardless.
const DefaultSampleLimit = 100

// PhotoPrismSource is the read-only slice of the PhotoPrism client the reconciler
// needs: the full photo listing, the instance's own library counts, and the
// album, label and subject listings. It is satisfied by *photoprism.HTTPClient.
type PhotoPrismSource interface {
	// ListPhotos returns one page of photos for the given params. The reconciler
	// pages a full, unfiltered listing by advancing Offset until an EMPTY page —
	// a short page is routine on a merged listing, not exhaustion.
	ListPhotos(ctx context.Context, params photoprism.PhotoListParams) ([]photoprism.Photo, error)
	// Counts returns PhotoPrism's own library totals, read from an aggregate the
	// photo search never touches. It is what lets the reconciler notice that the
	// listing it walked is narrower than the library behind it.
	Counts(ctx context.Context) (photoprism.LibraryCounts, error)
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
	// ImportedRefs returns the catalogue's PhotoPrism reference sets, used to
	// classify each source photo as imported, deduplicated or missing.
	ImportedRefs(ctx context.Context) (Refs, error)
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

// Refs are the catalogue's PhotoPrism reference sets: the three different ways a
// source photo can be accounted for. Keeping them apart is what lets the
// reconciler say WHY a source photo has no row of its own instead of calling it
// missing.
type Refs struct {
	// UIDs are the photoprism_uids of photos imported 1:1 (photos.photoprism_uid).
	UIDs map[string]struct{}
	// Aliases are the photoprism_uids of source photos that collapsed onto a
	// catalogue row already holding their exact content under another source uid
	// (photoprism_aliases, migration 0046). They are accounted for — the uid still
	// resolves to a row — but they are not imported photos of their own.
	Aliases map[string]struct{}
	// FileHashes are the photoprism_file_hashes the catalogue holds
	// (photos.photoprism_file_hash): the identity of a single SOURCE FILE, which
	// recognises a source photo whose primary file is catalogued under some other
	// row even when no alias was ever recorded for it.
	FileHashes map[string]struct{}
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
	// AlbumTypes are the PhotoPrism album types the catalogue is expected to hold;
	// empty uses ppimport.DefaultAlbumTypes — the importer's own list, so the
	// verifier can never demand a type the import deliberately skips. The
	// remaining photoprism.AlbumTypes are still walked, but their albums are
	// reported as skipped by design instead of missing.
	AlbumTypes []string
	// Logger receives a debug line per completed pass; nil uses slog.Default().
	Logger *slog.Logger
}

// Service reconciles the source libraries against the catalogue. It holds no
// mutable state and is safe for concurrent use.
type Service struct {
	photoPrism   PhotoPrismSource
	feeds        FeedsSource
	catalog      Catalog
	sampleLimit  int
	albumTypes   []string
	skippedTypes []string
	log          *slog.Logger
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
		albumTypes = ppimport.DefaultAlbumTypes
	}
	logger := cfg.Logger
	if logger == nil {
		logger = slog.Default()
	}
	return &Service{
		photoPrism:   cfg.PhotoPrism,
		feeds:        cfg.Feeds,
		catalog:      cfg.Catalog,
		sampleLimit:  sampleLimit,
		albumTypes:   albumTypes,
		skippedTypes: skippedAlbumTypes(albumTypes),
		log:          logger,
	}
}

// skippedAlbumTypes returns the PhotoPrism album types outside verified — the
// ones the import deliberately does not map. Deriving them keeps a single source
// of truth for the type list: whatever the importer leaves out is exactly what
// the verifier buckets as skipped by design rather than missing.
func skippedAlbumTypes(verified []string) []string {
	skipped := make([]string, 0, len(photoprism.AlbumTypes))
	for _, albumType := range photoprism.AlbumTypes {
		if !slices.Contains(verified, albumType) {
			skipped = append(skipped, albumType)
		}
	}
	return skipped
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
		"listing_shortfall", report.PhotoPrism.ListingShortfall,
		"surplus_photos", report.PhotoPrism.SurplusCount,
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
//
// The classification is set-based in both directions — every source uid is looked
// up in the catalogue and every catalogue uid in the enumerated source — and the
// enumeration itself is measured against the source's own total, so a match of
// totals can no longer stand in for a match of sets.
func (s *Service) reconcilePhotos(ctx context.Context) (PhotoPrismReport, error) {
	refs, byType, err := s.enumeratePhotos(ctx)
	if err != nil {
		return PhotoPrismReport{}, err
	}
	sourceCounts, err := s.photoPrism.Counts(ctx)
	if err != nil {
		return PhotoPrismReport{}, fmt.Errorf("importverify: reading photoprism library counts: %w", err)
	}
	catalogRefs, err := s.catalog.ImportedRefs(ctx)
	if err != nil {
		return PhotoPrismReport{}, fmt.Errorf("importverify: reading imported refs: %w", err)
	}
	fileCounts, err := s.catalog.OriginalFileCounts(ctx)
	if err != nil {
		return PhotoPrismReport{}, fmt.Errorf("importverify: reading original file counts: %w", err)
	}
	report := s.classifyPhotos(refs, byType, catalogRefs, fileCounts)
	recordListingShortfall(&report, sourceCounts)
	s.recordSurplus(&report, refs, catalogRefs)
	return report, nil
}

// recordListingShortfall compares the number of photos the listing yielded
// against the number PhotoPrism itself reports holding, and records the
// difference when the listing came back short.
//
// This is the only check in the section that does not go through the listing, and
// it exists because everything that does is only ever as complete as the listing
// is. A narrowed listing does not fail: it pages to exhaustion, hands back a
// consistent subset, and every set comparison built on it agrees that nothing is
// missing. That is exactly how 17 production photos stayed invisible while the
// report read COMPLETE.
//
// Only a shortfall is recorded. The reported total is a lower bound — PhotoPrism
// subtracts pictures in review from it when that feature is on, and hides private
// ones from a restricted session — so a listing that yields MORE than it is a
// normal state, not a finding.
func recordListingShortfall(report *PhotoPrismReport, counts photoprism.LibraryCounts) {
	report.SourceReportedTotal = counts.All
	if gap := counts.All - report.SourceTotal; gap > 0 {
		report.ListingShortfall = gap
	}
}

// recordSurplus records the catalogue's PhotoPrism uids that the source
// enumeration never yielded — the other direction of the same set comparison.
//
// It never gates Complete: a photo deleted in PhotoPrism after Kukátko imported
// it leaves exactly this trace and is not a defect (nor is it fixable by
// importing). It is reported because a catalogue reference the source cannot
// resolve is either that, or the listing having gone narrow — and the second one
// is worth a human look.
func (s *Service) recordSurplus(report *PhotoPrismReport, refs []photoRef, catalog Refs) {
	enumerated := make(map[string]struct{}, len(refs))
	for _, ref := range refs {
		enumerated[ref.uid] = struct{}{}
	}
	surplus := make([]string, 0)
	for uid := range catalog.UIDs {
		if !contains(enumerated, uid) {
			surplus = append(surplus, uid)
		}
	}
	sort.Strings(surplus)
	report.SurplusCount = len(surplus)
	report.SurplusUIDs = s.sample(surplus)
}

// enumeratePhotos pages the whole, unfiltered PhotoPrism photo listing to
// exhaustion, returning one photoRef per distinct photo and the per-type
// histogram.
//
// The order is pinned to photoprism.FullListingOrder because several PhotoPrism
// sort orders are also filters. The default used to be "updated", which compiles
// to `WHERE photos.updated_at > photos.created_at`: every photo untouched since
// the source indexed it was absent from the listing, so the reconciler could
// neither import nor miss it — it enumerated 20 660 of 20 677 production photos
// and reported the catalogue complete. recordListingShortfall is the guard that
// notices the next such window.
//
// Only an EMPTY page ends the walk. The listing is served merged, so the source
// collapses a photo's file rows into one entry and a page comes back shorter than
// the requested count whenever the window holds a multi-file photo. Stopping on a
// short page reconciled the catalogue against the first page alone — and a
// verifier blind in the same way as the importer could call an import complete
// with most of the library missing.
//
// The offset advances by the page length, which under-advances against the
// source's file-row offset: no row is skipped, but the overlap re-serves photos
// already seen, so they are deduplicated by uid here.
func (s *Service) enumeratePhotos(ctx context.Context) ([]photoRef, map[string]int, error) {
	refs := make([]photoRef, 0)
	byType := make(map[string]int)
	seen := make(map[string]int)
	for offset := 0; ; {
		page, err := s.photoPrism.ListPhotos(ctx, photoprism.PhotoListParams{
			Count:  photoprism.MaxCount,
			Offset: offset,
			Order:  photoprism.FullListingOrder,
		})
		if err != nil {
			return nil, nil, fmt.Errorf("importverify: listing photoprism photos at offset %d: %w", offset, err)
		}
		if len(page) == 0 {
			return refs, byType, nil
		}
		refs = collectPhotoRefs(page, refs, byType, seen)
		offset += len(page)
	}
}

// collectPhotoRefs folds one listing page into the accumulating refs and per-type
// histogram, counting each photo uid once. seen maps an already-collected uid to
// its index in refs; a repeat only widens that ref's expected file count, since a
// photo straddling a page boundary is served with part of its files in one page
// and the rest in the next, and the narrower list would mask a real file gap.
func collectPhotoRefs(
	page []photoprism.Photo, refs []photoRef, byType map[string]int, seen map[string]int,
) []photoRef {
	for i := range page {
		files := len(page[i].Files)
		if idx, dup := seen[page[i].UID]; dup {
			refs[idx].expectedFiles = max(refs[idx].expectedFiles, files)
			continue
		}
		byType[strings.ToLower(page[i].Type)]++
		hash := ""
		if primary, ok := page[i].PrimaryFile(); ok {
			hash = primary.Hash
		}
		seen[page[i].UID] = len(refs)
		refs = append(refs, photoRef{uid: page[i].UID, primaryHash: hash, expectedFiles: files})
	}
	return refs
}

// classifyPhotos buckets each enumerated photo against the catalogue sets and
// assembles the PhotoPrismReport, capping every listed slice at the sample limit
// while the counts stay the full totals.
func (s *Service) classifyPhotos(
	refs []photoRef,
	byType map[string]int,
	catalog Refs,
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
		case contains(catalog.UIDs, ref.uid):
			report.ImportedCount++
			s.recordFileGap(&report, ref, fileCounts)
		// An aliased source photo is accounted for but has no row of its own, so it
		// is counted as deduplicated and NOT file-gap checked: the files it would be
		// measured against belong to the row that survived, under that row's uid.
		case contains(catalog.Aliases, ref.uid):
			report.DeduplicatedCount++
		case ref.primaryHash != "" && contains(catalog.FileHashes, ref.primaryHash):
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
// missing-embeddings/missing-faces samples, and derives the two source-coverage
// ratios that keep those per-photo gaps from reading as source coverage.
func (s *Service) reconcileVectors(ctx context.Context, counts CatalogCounts) (VectorsReport, error) {
	if s.feeds == nil {
		return VectorsReport{
			NotConfigured:            true,
			EmbeddingsSourceCoverage: 1,
			FacesSourceCoverage:      1,
			EmbeddingsMissingUIDs:    make([]string, 0),
			FacesMissingUIDs:         make([]string, 0),
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
		SourceTotalPhotos:                  stats.TotalPhotos,
		SourcePhotosWithEmbeddings:         stats.PhotosWithEmbeddings,
		SourcePhotosWithFaces:              stats.PhotosWithFaces,
		SourceTotalFaces:                   stats.TotalFaces,
		CatalogEmbeddings:                  counts.Embeddings,
		CatalogFacePhotos:                  counts.FacePhotos,
		CatalogFaces:                       counts.Faces,
		EmbeddingsSourceCoverage:           sourceCoverage(counts.Embeddings, stats.PhotosWithEmbeddings),
		FacesSourceCoverage:                sourceCoverage(counts.Faces, stats.TotalFaces),
		EmbeddingsMissingForImportedPhotos: embTotal,
		EmbeddingsMissingUIDs:              normalizeStrings(missingEmb),
		FacesMissingForImportedPhotos:      facesTotal,
		FacesMissingUIDs:                   normalizeStrings(missingFaces),
	}, nil
}

// sourceCoverage returns the share of the source's vectors the catalogue actually
// holds — catalog/source clamped to [0,1] and rounded to four decimals so the JSON
// stays readable and the value is stable to compare.
//
// A source holding nothing is fully covered by definition (1), which keeps an
// unconfigured or empty feed from reading as a permanent shortfall. A catalogue
// holding more than the source clamps at 1 rather than exceeding it: Kukátko may
// legitimately hold vectors for photos photo-sorter never had (own uploads), and a
// coverage above 1 would only read as an error.
func sourceCoverage(catalog, source int) float64 {
	if source <= 0 || catalog >= source {
		return 1
	}
	if catalog <= 0 {
		return 0
	}
	const precision = 10000
	return math.Round(float64(catalog)/float64(source)*precision) / precision
}

// reconcileStructure builds the structure section by comparing the source name
// sets (albums by title, labels and subjects by name) against the catalogue sets,
// taking the catalogue row counts from counts. Albums go through albumReport,
// which reconciles only the types the import maps.
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
		Albums:   s.albumReport(srcAlbums, catAlbums, counts.Albums),
		Labels:   s.entityReport(srcLabels, catLabels, counts.Labels),
		Subjects: s.entityReport(srcSubjects, catSubjects, counts.Subjects),
	}, nil
}

// sourceAlbums holds the source album titles split by whether their album type is
// one the import maps (verified) or one it deliberately skips (skipped).
type sourceAlbums struct {
	// verified are the titles of albums whose type the import maps; they are
	// reconciled against the catalogue and can be reported missing.
	verified map[string]struct{}
	// skipped are the titles found only under a deliberately skipped type; they
	// are counted, never reconciled.
	skipped map[string]struct{}
}

// sourceStructure fully pages the PhotoPrism album (walked per type), label and
// subject listings and returns their deduplicated title/name sets.
func (s *Service) sourceStructure(
	ctx context.Context,
) (albums sourceAlbums, labels, subjects map[string]struct{}, err error) {
	albums, err = s.sourceAlbumTitles(ctx)
	if err != nil {
		return sourceAlbums{}, nil, nil, err
	}
	labels, err = collectAll(func(offset int) ([]photoprism.Label, error) {
		return s.photoPrism.ListLabels(ctx, photoprism.ListParams{Count: photoprism.MaxCount, Offset: offset})
	}, func(label photoprism.Label) string { return label.Name })
	if err != nil {
		return sourceAlbums{}, nil, nil, fmt.Errorf("importverify: listing photoprism labels: %w", err)
	}
	subjects, err = collectAll(func(offset int) ([]photoprism.Subject, error) {
		return s.photoPrism.ListSubjects(ctx, photoprism.ListParams{Count: photoprism.MaxCount, Offset: offset})
	}, func(subject photoprism.Subject) string { return subject.Name })
	if err != nil {
		return sourceAlbums{}, nil, nil, fmt.Errorf("importverify: listing photoprism subjects: %w", err)
	}
	return albums, labels, subjects, nil
}

// sourceAlbumTitles walks the whole PhotoPrism album catalogue and returns its
// titles split into the reconciled types and the deliberately skipped ones. A
// title served under both (a "month" album whose name a real album repeats)
// belongs to the reconciled side, so it is checked rather than written off.
func (s *Service) sourceAlbumTitles(ctx context.Context) (sourceAlbums, error) {
	verified, err := s.albumTitlesOfTypes(ctx, s.albumTypes)
	if err != nil {
		return sourceAlbums{}, err
	}
	skipped, err := s.albumTitlesOfTypes(ctx, s.skippedTypes)
	if err != nil {
		return sourceAlbums{}, err
	}
	for title := range verified {
		delete(skipped, title)
	}
	return sourceAlbums{verified: verified, skipped: skipped}, nil
}

// albumTitlesOfTypes walks the PhotoPrism album listing once per given album type
// — the listing takes exactly one type per request — and returns the merged,
// deduplicated set of album titles.
func (s *Service) albumTitlesOfTypes(
	ctx context.Context, albumTypes []string,
) (map[string]struct{}, error) {
	titles := make(map[string]struct{})
	for _, albumType := range albumTypes {
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

// albumReport reconciles the albums of the mapped types against the catalogue and
// attaches the skipped-by-design tally, so the section accounts for the whole
// source album catalogue without ever demanding what the import does not map.
func (s *Service) albumReport(
	source sourceAlbums, catalog map[string]struct{}, catalogCount int,
) AlbumReport {
	return AlbumReport{
		EntityReport: s.entityReport(source.verified, catalog, catalogCount),
		// Cloned: the report travels out of the Service, which promises to hold no
		// state a caller can reach into.
		SkippedTypes:         slices.Clone(s.skippedTypes),
		SkippedByDesignCount: len(source.skipped),
	}
}

// entityReport reconciles one structural entity in both directions: the source
// names absent from the catalogue (missing) and the catalogue names the source
// does not have (surplus), each sorted for determinism and capped at the sample
// limit while its full total is kept.
//
// The surplus side is reported, never enforced — see EntityReport — but it is what
// makes a catalogue row that should not exist visible at all.
func (s *Service) entityReport(source, catalog map[string]struct{}, catalogCount int) EntityReport {
	missing := difference(source, catalog)
	surplus := difference(catalog, source)
	return EntityReport{
		SourceCount:  len(source),
		CatalogCount: catalogCount,
		MissingCount: len(missing),
		Missing:      s.sample(missing),
		SurplusCount: len(surplus),
		Surplus:      s.sample(surplus),
	}
}

// difference returns the sorted names present in a but not in b.
func difference(a, b map[string]struct{}) []string {
	out := make([]string, 0, len(a))
	for name := range a {
		if _, ok := b[name]; !ok {
			out = append(out, name)
		}
	}
	sort.Strings(out)
	return out
}

// sample truncates names to the service's sample limit, so a report stays a
// readable illustration while its counts remain the full totals.
func (s *Service) sample(names []string) []string {
	if len(names) > s.sampleLimit {
		return names[:s.sampleLimit]
	}
	return names
}

// isComplete reports whether the report shows nothing left to import: a source
// listing that accounted for the whole library, no missing or file-gapped photos,
// no imported photo left without its vectors (unless the vectors section is not
// configured), and no missing structural entities.
//
// A listing shortfall fails it even though nothing is known to be missing —
// because nothing CAN be known while the listing is narrower than the library it
// was drawn from, and "nothing known to be missing" is precisely the answer such
// a listing produces.
//
// It deliberately does not require full vector source coverage. photo-sorter's
// population and PhotoPrism's are not the same set, so a coverage below 1 can be
// a permanent, legitimate state — gating on it would make a finished import
// unreachable by construction, the same trap the month-albums reconciliation fell
// into. With every source photo imported, the per-photo gaps below are the honest
// measure; VectorsReport.FullSourceCoverage carries the rest of the picture to the
// CLI and the frontend.
func isComplete(report Report) bool {
	if report.PhotoPrism.MissingCount != 0 || report.PhotoPrism.FileGapCount != 0 {
		return false
	}
	if report.PhotoPrism.ListingShortfall != 0 {
		return false
	}
	if !report.Vectors.NotConfigured &&
		(report.Vectors.EmbeddingsMissingForImportedPhotos != 0 ||
			report.Vectors.FacesMissingForImportedPhotos != 0) {
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
