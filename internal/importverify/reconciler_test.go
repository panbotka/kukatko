package importverify_test

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"testing"

	"github.com/panbotka/kukatko/internal/importverify"
	"github.com/panbotka/kukatko/internal/photoprism"
	"github.com/panbotka/kukatko/internal/psfeeds"
)

// errListing is a sentinel used to assert a source-listing failure is wrapped and
// surfaced by Verify.
var errListing = errors.New("boom")

// fakePhotoPrism is an in-memory PhotoPrismSource. It pages each listing by
// offset/count and can inject a listing error.
type fakePhotoPrism struct {
	photos       []photoprism.Photo
	albumsByType map[string][]photoprism.Album
	labels       []photoprism.Label
	subjects     []photoprism.Subject
	listErr      error
	countsErr    error
	// reportedTotal overrides what Counts reports the library holds; zero means
	// "as many as the fake serves". A value above the number of photos models the
	// case this whole guard exists for: a source that holds more than its listing
	// ever admits to.
	reportedTotal int
	// lastOrder records the order the last photo listing asked for, so a test can
	// assert the reconciler never walks the library through a filtering one.
	lastOrder string
	// merged, when set, serves the photo listing the way PhotoPrism serves a merged
	// one: offset/count select FILE rows — one per entry in a photo's Files — and
	// the rows of one photo then collapse into a single entry. A page is therefore
	// shorter than the requested count whenever its window holds a multi-file photo,
	// which is exactly what made a short page look like the end of the library.
	merged bool
}

// ListPhotos returns one page of the fake's photos, or the injected error. The
// page is drawn from the photos the requested order actually exposes, so a
// reconciler that asks for a filtering order gets the narrowed library back and
// pages it to exhaustion none the wiser — exactly as the real source behaves.
func (f *fakePhotoPrism) ListPhotos(
	_ context.Context, params photoprism.PhotoListParams,
) ([]photoprism.Photo, error) {
	if f.listErr != nil {
		return nil, f.listErr
	}
	f.lastOrder = params.Order
	listable := exposedByOrder(f.photos, params.Order)
	if f.merged {
		return mergedPhotoPage(listable, params.Offset, params.Count), nil
	}
	return pageSlice(listable, params.Offset, params.Count), nil
}

// exposedByOrder models PhotoPrism's filtering sort orders: several of them
// compile to a WHERE clause on top of the ordering, so asking for one narrows the
// library instead of just reordering it. The narrowing modelled here is the one
// that hit production — order=updated adds `photos.updated_at >
// photos.created_at`, hiding every photo untouched since it was indexed — and it
// stands in for the rest, since what matters is that a filtering order serves a
// subset without ever saying so.
func exposedByOrder(in []photoprism.Photo, order string) []photoprism.Photo {
	if _, filters := photoprism.FilteringOrderPredicate(order); !filters {
		return in
	}
	out := make([]photoprism.Photo, 0, len(in))
	for i := range in {
		if in[i].UpdatedAt.After(in[i].CreatedAt) {
			out = append(out, in[i])
		}
	}
	return out
}

// Counts reports the library totals. reportedTotal wins when set; otherwise the
// fake reports exactly what it serves, so an ordinary fixture carries no
// shortfall.
func (f *fakePhotoPrism) Counts(_ context.Context) (photoprism.LibraryCounts, error) {
	if f.countsErr != nil {
		return photoprism.LibraryCounts{}, f.countsErr
	}
	all := f.reportedTotal
	if all == 0 {
		all = len(f.photos)
	}
	return photoprism.LibraryCounts{All: all, Photos: all}, nil
}

// mergedPhotoPage expands the photos into one row per file, slices the row window
// and collapses consecutive rows of the same photo, mirroring PhotoPrism's merged
// listing. A photo straddling the window keeps only the files inside it, exactly
// as the source serves it.
func mergedPhotoPage(in []photoprism.Photo, offset, count int) []photoprism.Photo {
	rows := make([]photoprism.Photo, 0, len(in))
	for i := range in {
		if len(in[i].Files) == 0 {
			rows = append(rows, in[i])
			continue
		}
		for j := range in[i].Files {
			row := in[i]
			row.Files = in[i].Files[j : j+1]
			rows = append(rows, row)
		}
	}
	merged := make([]photoprism.Photo, 0, count)
	for _, row := range pageSlice(rows, offset, count) {
		if n := len(merged); n > 0 && merged[n-1].UID == row.UID {
			merged[n-1].Files = append(merged[n-1].Files, row.Files...)
			continue
		}
		row.Files = slices.Clone(row.Files)
		merged = append(merged, row)
	}
	return merged
}

// ListAlbums returns one page of the albums registered for params.Type.
func (f *fakePhotoPrism) ListAlbums(
	_ context.Context, params photoprism.ListParams,
) ([]photoprism.Album, error) {
	return pageSlice(f.albumsByType[params.Type], params.Offset, params.Count), nil
}

// ListLabels returns one page of the fake's labels.
func (f *fakePhotoPrism) ListLabels(
	_ context.Context, params photoprism.ListParams,
) ([]photoprism.Label, error) {
	return pageSlice(f.labels, params.Offset, params.Count), nil
}

// ListSubjects returns one page of the fake's subjects.
func (f *fakePhotoPrism) ListSubjects(
	_ context.Context, params photoprism.ListParams,
) ([]photoprism.Subject, error) {
	return pageSlice(f.subjects, params.Offset, params.Count), nil
}

// fakeFeeds is an in-memory FeedsSource returning fixed stats or an error.
type fakeFeeds struct {
	stats    psfeeds.Stats
	statsErr error
}

// Stats returns the fake's stats or the injected error.
func (f *fakeFeeds) Stats(_ context.Context) (psfeeds.Stats, error) {
	return f.stats, f.statsErr
}

// fakeCatalog is an in-memory Catalog. The "missing" lookups honour the limit by
// capping the returned sample while reporting the full total.
type fakeCatalog struct {
	importedUIDs   map[string]struct{}
	aliasUIDs      map[string]struct{}
	importedHashes map[string]struct{}
	fileCounts     map[string]int
	counts         importverify.CatalogCounts
	missingEmb     []string
	missingFaces   []string
	albumTitles    map[string]struct{}
	labelNames     map[string]struct{}
	subjectNames   map[string]struct{}
}

// newFakeCatalog returns a fakeCatalog with every set initialised empty so a test
// that only cares about one section leaves the others inert.
func newFakeCatalog() *fakeCatalog {
	return &fakeCatalog{
		importedUIDs:   map[string]struct{}{},
		aliasUIDs:      map[string]struct{}{},
		importedHashes: map[string]struct{}{},
		fileCounts:     map[string]int{},
		albumTitles:    map[string]struct{}{},
		labelNames:     map[string]struct{}{},
		subjectNames:   map[string]struct{}{},
	}
}

// ImportedRefs returns the fake's uid, alias and file-hash sets.
func (c *fakeCatalog) ImportedRefs(_ context.Context) (importverify.Refs, error) {
	return importverify.Refs{
		UIDs:       c.importedUIDs,
		Aliases:    c.aliasUIDs,
		FileHashes: c.importedHashes,
	}, nil
}

// OriginalFileCounts returns the fake's per-uid original-file counts.
func (c *fakeCatalog) OriginalFileCounts(_ context.Context) (map[string]int, error) {
	return c.fileCounts, nil
}

// Counts returns the fake's catalogue aggregates.
func (c *fakeCatalog) Counts(_ context.Context) (importverify.CatalogCounts, error) {
	return c.counts, nil
}

// PhotosMissingEmbeddings returns up to limit of the fake's missing-embedding
// uids plus the full total.
func (c *fakeCatalog) PhotosMissingEmbeddings(
	_ context.Context, limit int,
) ([]string, int, error) {
	return capStrings(c.missingEmb, limit), len(c.missingEmb), nil
}

// PhotosMissingFaces returns up to limit of the fake's missing-faces uids plus the
// full total.
func (c *fakeCatalog) PhotosMissingFaces(
	_ context.Context, limit int,
) ([]string, int, error) {
	return capStrings(c.missingFaces, limit), len(c.missingFaces), nil
}

// AlbumTitles returns the fake's catalogue album-title set.
func (c *fakeCatalog) AlbumTitles(_ context.Context) (map[string]struct{}, error) {
	return c.albumTitles, nil
}

// LabelNames returns the fake's catalogue label-name set.
func (c *fakeCatalog) LabelNames(_ context.Context) (map[string]struct{}, error) {
	return c.labelNames, nil
}

// SubjectNames returns the fake's catalogue subject-name set.
func (c *fakeCatalog) SubjectNames(_ context.Context) (map[string]struct{}, error) {
	return c.subjectNames, nil
}

// pageSlice returns items[offset:offset+count], clamped to the slice bounds, or
// nil past the end — the paging contract the reconciler expects.
func pageSlice[T any](items []T, offset, count int) []T {
	if offset >= len(items) {
		return nil
	}
	end := min(offset+count, len(items))
	return items[offset:end]
}

// capStrings returns the first limit elements of in (all of them when limit is
// non-positive or exceeds the length).
func capStrings(in []string, limit int) []string {
	if limit <= 0 || limit >= len(in) {
		return in
	}
	return in[:limit]
}

// photo builds a photoprism.Photo of the given type with fileCount files; the
// first file is primary and carries primaryHash when primaryHash is non-empty
// (empty leaves the photo with no primary file).
func photo(uid, typ, primaryHash string, fileCount int) photoprism.Photo {
	files := make([]photoprism.File, 0, fileCount)
	for i := range fileCount {
		file := photoprism.File{Hash: fmt.Sprintf("%s-f%d", uid, i)}
		if i == 0 && primaryHash != "" {
			file.Primary = true
			file.Hash = primaryHash
		}
		files = append(files, file)
	}
	return photoprism.Photo{UID: uid, Type: typ, Files: files}
}

// set builds a set from the given keys, for concise catalogue fixtures.
func set(keys ...string) map[string]struct{} {
	out := make(map[string]struct{}, len(keys))
	for _, key := range keys {
		out[key] = struct{}{}
	}
	return out
}

// TestService_Verify_classifiesPhotos covers the photo classification: imported,
// missing, SHA-deduplicated, uid-match-beats-dedup, and empty-hash-not-dedup.
func TestService_Verify_classifiesPhotos(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		photos        []photoprism.Photo
		importedUIDs  map[string]struct{}
		aliasUIDs     map[string]struct{}
		importedHash  map[string]struct{}
		wantImported  int
		wantDedup     int
		wantMissing   int
		wantMissingID []string
	}{
		{
			name:          "imported by uid",
			photos:        []photoprism.Photo{photo("ppA", "image", "h1", 1)},
			importedUIDs:  set("ppA"),
			wantImported:  1,
			wantMissingID: []string{},
		},
		{
			name:          "missing when neither uid nor hash present",
			photos:        []photoprism.Photo{photo("ppX", "image", "h9", 1)},
			wantMissing:   1,
			wantMissingID: []string{"ppX"},
		},
		{
			name:          "deduplicated by shared file hash",
			photos:        []photoprism.Photo{photo("ppY", "image", "h1", 1)},
			importedHash:  set("h1"),
			wantDedup:     1,
			wantMissingID: []string{},
		},
		{
			name:          "uid match beats dedup",
			photos:        []photoprism.Photo{photo("ppA", "image", "h1", 1)},
			importedUIDs:  set("ppA"),
			importedHash:  set("h1"),
			wantImported:  1,
			wantMissingID: []string{},
		},
		{
			// The shape that lost 450 production photos: the source photo has no row of
			// its own AND no catalogue row carries its file hash (the winner was
			// catalogued from the OTHER source photo's file), so only the alias can
			// account for it. Without it this reads as missing forever.
			name:          "aliased onto identical content is deduplicated, not missing",
			photos:        []photoprism.Photo{photo("ppDup", "image", "h-dup", 1)},
			aliasUIDs:     set("ppDup"),
			wantDedup:     1,
			wantMissingID: []string{},
		},
		{
			name:          "empty primary hash is not deduplicated",
			photos:        []photoprism.Photo{photo("ppZ", "image", "", 1)},
			importedHash:  set(""),
			wantMissing:   1,
			wantMissingID: []string{"ppZ"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			cat := newFakeCatalog()
			if tt.importedUIDs != nil {
				cat.importedUIDs = tt.importedUIDs
			}
			if tt.aliasUIDs != nil {
				cat.aliasUIDs = tt.aliasUIDs
			}
			if tt.importedHash != nil {
				cat.importedHashes = tt.importedHash
			}
			svc := importverify.NewService(importverify.Config{
				PhotoPrism: &fakePhotoPrism{photos: tt.photos},
				Catalog:    cat,
			})

			report, err := svc.Verify(context.Background())
			if err != nil {
				t.Fatalf("Verify: %v", err)
			}
			pp := report.PhotoPrism
			if pp.ImportedCount != tt.wantImported {
				t.Errorf("ImportedCount = %d, want %d", pp.ImportedCount, tt.wantImported)
			}
			if pp.DeduplicatedCount != tt.wantDedup {
				t.Errorf("DeduplicatedCount = %d, want %d", pp.DeduplicatedCount, tt.wantDedup)
			}
			if pp.MissingCount != tt.wantMissing {
				t.Errorf("MissingCount = %d, want %d", pp.MissingCount, tt.wantMissing)
			}
			if !slices.Equal(pp.MissingUIDs, tt.wantMissingID) {
				t.Errorf("MissingUIDs = %v, want %v", pp.MissingUIDs, tt.wantMissingID)
			}
		})
	}
}

// TestService_Verify_sourceByType checks the per-type histogram and source total.
func TestService_Verify_sourceByType(t *testing.T) {
	t.Parallel()

	svc := importverify.NewService(importverify.Config{
		PhotoPrism: &fakePhotoPrism{photos: []photoprism.Photo{
			photo("a", "Image", "ha", 1),
			photo("b", "image", "hb", 1),
			photo("c", "RAW", "hc", 1),
		}},
		Catalog: newFakeCatalog(),
	})

	report, err := svc.Verify(context.Background())
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if report.PhotoPrism.SourceTotal != 3 {
		t.Errorf("SourceTotal = %d, want 3", report.PhotoPrism.SourceTotal)
	}
	want := map[string]int{"image": 2, "raw": 1}
	for typ, count := range want {
		if report.PhotoPrism.SourceByType[typ] != count {
			t.Errorf("SourceByType[%q] = %d, want %d", typ, report.PhotoPrism.SourceByType[typ], count)
		}
	}
}

// TestService_Verify_fileGap checks that an imported photo with fewer catalogue
// original files than source files yields a capped FileGap.
func TestService_Verify_fileGap(t *testing.T) {
	t.Parallel()

	cat := newFakeCatalog()
	cat.importedUIDs = set("ppA", "ppB")
	cat.fileCounts = map[string]int{"ppA": 1, "ppB": 2}
	svc := importverify.NewService(importverify.Config{
		PhotoPrism: &fakePhotoPrism{photos: []photoprism.Photo{
			photo("ppA", "raw", "ha", 2),   // 2 source files, 1 catalogue original -> gap
			photo("ppB", "image", "hb", 2), // 2 source files, 2 catalogue originals -> no gap
		}},
		Catalog: cat,
	})

	report, err := svc.Verify(context.Background())
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	pp := report.PhotoPrism
	if pp.FileGapCount != 1 {
		t.Fatalf("FileGapCount = %d, want 1", pp.FileGapCount)
	}
	want := importverify.FileGap{PhotoprismUID: "ppA", Expected: 2, Actual: 1}
	if len(pp.FileGaps) != 1 || pp.FileGaps[0] != want {
		t.Errorf("FileGaps = %+v, want [%+v]", pp.FileGaps, want)
	}
}

// TestService_Verify_vectors covers the vectors section: not-configured when no
// feeds source, and the passthrough of stats plus catalogue missing lists.
func TestService_Verify_vectors(t *testing.T) {
	t.Parallel()

	t.Run("not configured without feeds", func(t *testing.T) {
		t.Parallel()
		svc := importverify.NewService(importverify.Config{
			PhotoPrism: &fakePhotoPrism{},
			Catalog:    newFakeCatalog(),
		})
		report, err := svc.Verify(context.Background())
		if err != nil {
			t.Fatalf("Verify: %v", err)
		}
		if !report.Vectors.NotConfigured {
			t.Error("Vectors.NotConfigured = false, want true")
		}
		if report.Vectors.EmbeddingsMissingUIDs == nil || report.Vectors.FacesMissingUIDs == nil {
			t.Error("missing slices should be non-nil so they marshal as []")
		}
		if !report.Vectors.FullSourceCoverage() {
			t.Error("FullSourceCoverage() = false, want true (no source means nothing to cover)")
		}
		if !report.Complete {
			t.Error("Complete = false, want true (vectors ignored when not configured)")
		}
	})

	t.Run("reports source stats and missing lists", func(t *testing.T) {
		t.Parallel()
		cat := newFakeCatalog()
		cat.counts = importverify.CatalogCounts{Embeddings: 8, FacePhotos: 3, Faces: 5}
		cat.missingEmb = []string{"e1", "e2"}
		cat.missingFaces = []string{"f1"}
		svc := importverify.NewService(importverify.Config{
			PhotoPrism: &fakePhotoPrism{},
			Feeds: &fakeFeeds{stats: psfeeds.Stats{
				TotalPhotos: 10, PhotosWithEmbeddings: 8, PhotosWithFaces: 3, TotalFaces: 5,
			}},
			Catalog: cat,
		})
		report, err := svc.Verify(context.Background())
		if err != nil {
			t.Fatalf("Verify: %v", err)
		}
		v := report.Vectors
		if v.NotConfigured {
			t.Error("NotConfigured = true, want false")
		}
		if v.SourceTotalPhotos != 10 || v.SourcePhotosWithEmbeddings != 8 ||
			v.SourcePhotosWithFaces != 3 || v.SourceTotalFaces != 5 {
			t.Errorf("source stats not propagated: %+v", v)
		}
		if v.CatalogEmbeddings != 8 || v.CatalogFacePhotos != 3 || v.CatalogFaces != 5 {
			t.Errorf("catalog counts not propagated: %+v", v)
		}
		if v.EmbeddingsMissingForImportedPhotos != 2 ||
			!slices.Equal(v.EmbeddingsMissingUIDs, []string{"e1", "e2"}) {
			t.Errorf("missing embeddings = %d/%v", v.EmbeddingsMissingForImportedPhotos, v.EmbeddingsMissingUIDs)
		}
		if v.FacesMissingForImportedPhotos != 1 || !slices.Equal(v.FacesMissingUIDs, []string{"f1"}) {
			t.Errorf("missing faces = %d/%v", v.FacesMissingForImportedPhotos, v.FacesMissingUIDs)
		}
		if !v.FullSourceCoverage() {
			t.Errorf("FullSourceCoverage() = false, want true (catalogue holds the whole source): %+v", v)
		}
		if report.Complete {
			t.Error("Complete = true, want false (missing vectors present)")
		}
	})
}

// TestService_Verify_vectorsCoverageOnPartialCatalogue is the regression guard for
// the counters that read as "done" on an all-but-empty catalogue: with the
// catalogue a strict subset of the source, every imported photo can have its
// vectors — so the per-photo gaps are legitimately 0 — while the source coverage
// stays far below 1. The report must not be readable as complete coverage.
func TestService_Verify_vectorsCoverageOnPartialCatalogue(t *testing.T) {
	t.Parallel()

	// The production shape from docs/READINESS_AUDIT.md §2.3: 280 of 20 670 photos
	// imported, 50 embeddings held against a source of 20 092.
	cat := newFakeCatalog()
	cat.counts = importverify.CatalogCounts{Embeddings: 50, FacePhotos: 20, Faces: 30}
	svc := importverify.NewService(importverify.Config{
		PhotoPrism: &fakePhotoPrism{photos: []photoprism.Photo{photo("ppMissing", "image", "h1", 1)}},
		Feeds: &fakeFeeds{stats: psfeeds.Stats{
			TotalPhotos: 20670, PhotosWithEmbeddings: 20092, PhotosWithFaces: 8000, TotalFaces: 15000,
		}},
		Catalog: cat,
	})

	report, err := svc.Verify(context.Background())
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	v := report.Vectors

	// The per-photo gap is 0 — no imported photo lacks anything — and that number
	// on its own is exactly what used to read as a finished vector migration.
	if v.EmbeddingsMissingForImportedPhotos != 0 || v.FacesMissingForImportedPhotos != 0 {
		t.Fatalf("per-photo gaps = %d/%d, want 0/0 (nothing imported lacks vectors)",
			v.EmbeddingsMissingForImportedPhotos, v.FacesMissingForImportedPhotos)
	}
	// The coverage figures contradict that reading, so the section cannot be
	// mistaken for full coverage.
	if v.EmbeddingsSourceCoverage >= 1 {
		t.Errorf("EmbeddingsSourceCoverage = %v, want < 1 (50 of 20092 held)", v.EmbeddingsSourceCoverage)
	}
	if want := 0.0025; v.EmbeddingsSourceCoverage != want {
		t.Errorf("EmbeddingsSourceCoverage = %v, want %v", v.EmbeddingsSourceCoverage, want)
	}
	if want := 0.002; v.FacesSourceCoverage != want {
		t.Errorf("FacesSourceCoverage = %v, want %v", v.FacesSourceCoverage, want)
	}
	if v.FullSourceCoverage() {
		t.Error("FullSourceCoverage() = true, want false (the catalogue is a strict subset of the source)")
	}
	if report.Complete {
		t.Error("Complete = true, want false (source photos are still missing)")
	}
}

// TestService_Verify_vectorsCoverageEdges pins the coverage ratio's boundaries: an
// empty source is covered by definition, an empty catalogue covers nothing, and a
// catalogue larger than the source (own uploads photo-sorter never had) clamps at
// 1 instead of reporting more than everything.
func TestService_Verify_vectorsCoverageEdges(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name             string
		counts           importverify.CatalogCounts
		stats            psfeeds.Stats
		wantEmbeddings   float64
		wantFaces        float64
		wantFullCoverage bool
	}{
		{
			name:             "empty source is covered by definition",
			wantEmbeddings:   1,
			wantFaces:        1,
			wantFullCoverage: true,
		},
		{
			name:           "empty catalogue covers nothing",
			stats:          psfeeds.Stats{PhotosWithEmbeddings: 100, TotalFaces: 40},
			wantEmbeddings: 0,
			wantFaces:      0,
		},
		{
			name:             "catalogue larger than the source clamps at 1",
			counts:           importverify.CatalogCounts{Embeddings: 120, Faces: 50},
			stats:            psfeeds.Stats{PhotosWithEmbeddings: 100, TotalFaces: 40},
			wantEmbeddings:   1,
			wantFaces:        1,
			wantFullCoverage: true,
		},
		{
			name:           "partial catalogue rounds to four decimals",
			counts:         importverify.CatalogCounts{Embeddings: 1, Faces: 1},
			stats:          psfeeds.Stats{PhotosWithEmbeddings: 3, TotalFaces: 8},
			wantEmbeddings: 0.3333,
			wantFaces:      0.125,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			cat := newFakeCatalog()
			cat.counts = tt.counts
			svc := importverify.NewService(importverify.Config{
				PhotoPrism: &fakePhotoPrism{},
				Feeds:      &fakeFeeds{stats: tt.stats},
				Catalog:    cat,
			})

			report, err := svc.Verify(context.Background())
			if err != nil {
				t.Fatalf("Verify: %v", err)
			}
			v := report.Vectors
			if v.EmbeddingsSourceCoverage != tt.wantEmbeddings {
				t.Errorf("EmbeddingsSourceCoverage = %v, want %v", v.EmbeddingsSourceCoverage, tt.wantEmbeddings)
			}
			if v.FacesSourceCoverage != tt.wantFaces {
				t.Errorf("FacesSourceCoverage = %v, want %v", v.FacesSourceCoverage, tt.wantFaces)
			}
			if got := v.FullSourceCoverage(); got != tt.wantFullCoverage {
				t.Errorf("FullSourceCoverage() = %v, want %v", got, tt.wantFullCoverage)
			}
		})
	}
}

// TestService_Verify_structure checks structural reconciliation: source names
// absent from the catalogue are reported missing while catalogue counts come from
// the aggregates.
func TestService_Verify_structure(t *testing.T) {
	t.Parallel()

	cat := newFakeCatalog()
	cat.counts = importverify.CatalogCounts{Albums: 1, Labels: 1, Subjects: 1}
	cat.albumTitles = set("Trip")
	cat.labelNames = set("cat")
	cat.subjectNames = set("Alice")
	svc := importverify.NewService(importverify.Config{
		PhotoPrism: &fakePhotoPrism{
			albumsByType: map[string][]photoprism.Album{
				"album": {{Title: "Trip"}, {Title: "Family"}},
			},
			labels:   []photoprism.Label{{Name: "cat"}, {Name: "dog"}},
			subjects: []photoprism.Subject{{Name: "Alice"}, {Name: "Bob"}},
		},
		Catalog:    cat,
		AlbumTypes: []string{"album"},
	})

	report, err := svc.Verify(context.Background())
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	assertEntity(t, "albums", report.Structure.Albums.EntityReport, 2, 1, []string{"Family"})
	assertEntity(t, "labels", report.Structure.Labels, 2, 1, []string{"dog"})
	assertEntity(t, "subjects", report.Structure.Subjects, 2, 1, []string{"Bob"})
	if report.Complete {
		t.Error("Complete = true, want false (structure gaps present)")
	}
}

// TestService_Verify_structureSurplus checks the other direction of the
// reconciliation: a catalogue name the source does not have is reported as
// surplus. It is the case the report used to be blind to — `people: source=104
// kukatko=105 missing=0` read as clean while the extra subject was an empty-named
// catch-all holding 16 532 markers — so the empty name has to survive into the
// report rather than being filtered out as a blank.
//
// A surplus must not make the report incomplete: subjects, albums and labels
// created in Kukátko itself are a permanent, legitimate surplus, so gating on it
// would put Complete out of reach.
func TestService_Verify_structureSurplus(t *testing.T) {
	t.Parallel()

	cat := newFakeCatalog()
	cat.counts = importverify.CatalogCounts{Albums: 1, Labels: 1, Subjects: 3}
	cat.albumTitles = set("Trip")
	cat.labelNames = set("cat")
	cat.subjectNames = set("Alice", "", "Local Only")
	svc := importverify.NewService(importverify.Config{
		PhotoPrism: &fakePhotoPrism{
			albumsByType: map[string][]photoprism.Album{"album": {{Title: "Trip"}}},
			labels:       []photoprism.Label{{Name: "cat"}},
			subjects:     []photoprism.Subject{{Name: "Alice"}},
		},
		Catalog:    cat,
		AlbumTypes: []string{"album"},
	})

	report, err := svc.Verify(context.Background())
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	subjects := report.Structure.Subjects
	if subjects.MissingCount != 0 {
		t.Errorf("subjects MissingCount = %d, want 0", subjects.MissingCount)
	}
	if subjects.SurplusCount != 2 {
		t.Fatalf("subjects SurplusCount = %d, want 2", subjects.SurplusCount)
	}
	if !slices.Equal(subjects.Surplus, []string{"", "Local Only"}) {
		t.Errorf("subjects Surplus = %q, want the empty name and the local one", subjects.Surplus)
	}
	if report.Structure.Albums.SurplusCount != 0 || report.Structure.Labels.SurplusCount != 0 {
		t.Errorf("albums/labels reported a surplus: %+v / %+v",
			report.Structure.Albums.EntityReport, report.Structure.Labels)
	}
	if !report.Complete {
		t.Error("Complete = false, want true: a surplus is reported, never enforced")
	}
}

// TestService_Verify_albumTypes checks the album reconciliation defaults to the
// importer's own type list: an album of a type the importer deliberately skips
// ("month", PhotoPrism's auto-generated per-calendar-month albums) is bucketed as
// skipped by design and never reported missing, while an album of an imported
// type that the catalogue lacks still is.
func TestService_Verify_albumTypes(t *testing.T) {
	t.Parallel()

	cat := newFakeCatalog()
	cat.counts = importverify.CatalogCounts{Albums: 1}
	cat.albumTitles = set("Trip")
	svc := importverify.NewService(importverify.Config{
		PhotoPrism: &fakePhotoPrism{
			albumsByType: map[string][]photoprism.Album{
				"album": {{Title: "Trip"}},
				"state": {{Title: "Moravia"}},
				"month": {{Title: "July 2019"}, {Title: "August 2019"}},
			},
		},
		Catalog: cat, // no AlbumTypes: the default must mirror the importer's
	})

	report, err := svc.Verify(context.Background())
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	albums := report.Structure.Albums
	// "Moravia" (type state) is imported and absent → missing; the two month
	// albums are skipped by design and stay out of both the source count and the
	// missing list.
	assertEntity(t, "albums", albums.EntityReport, 2, 1, []string{"Moravia"})
	if !slices.Equal(albums.SkippedTypes, []string{"month"}) {
		t.Errorf("SkippedTypes = %v, want [month]", albums.SkippedTypes)
	}
	if albums.SkippedByDesignCount != 2 {
		t.Errorf("SkippedByDesignCount = %d, want 2", albums.SkippedByDesignCount)
	}
}

// TestService_Verify_albumTypes_completeWithSkipped checks a catalogue holding
// every album of the imported types reports complete even though the source still
// serves albums of a skipped type — the mismatch that made a clean report
// unreachable by construction.
func TestService_Verify_albumTypes_completeWithSkipped(t *testing.T) {
	t.Parallel()

	cat := newFakeCatalog()
	cat.counts = importverify.CatalogCounts{Albums: 1}
	cat.albumTitles = set("Trip")
	svc := importverify.NewService(importverify.Config{
		PhotoPrism: &fakePhotoPrism{
			albumsByType: map[string][]photoprism.Album{
				"album": {{Title: "Trip"}},
				"month": {{Title: "July 2019"}},
			},
		},
		Catalog: cat,
	})

	report, err := svc.Verify(context.Background())
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if !report.Complete {
		t.Errorf("Complete = false, want true (albums missing = %v)", report.Structure.Albums.Missing)
	}
}

// assertEntity checks an EntityReport's source count, catalogue count and the
// sorted missing list.
func assertEntity(
	t *testing.T, name string, got importverify.EntityReport,
	wantSource, wantCatalog int, wantMissing []string,
) {
	t.Helper()
	if got.SourceCount != wantSource {
		t.Errorf("%s SourceCount = %d, want %d", name, got.SourceCount, wantSource)
	}
	if got.CatalogCount != wantCatalog {
		t.Errorf("%s CatalogCount = %d, want %d", name, got.CatalogCount, wantCatalog)
	}
	if got.MissingCount != len(wantMissing) {
		t.Errorf("%s MissingCount = %d, want %d", name, got.MissingCount, len(wantMissing))
	}
	if !slices.Equal(got.Missing, wantMissing) {
		t.Errorf("%s Missing = %v, want %v", name, got.Missing, wantMissing)
	}
}

// TestService_Verify_sampleLimit checks that the sample limit caps every listed
// slice while the counts stay the full totals.
func TestService_Verify_sampleLimit(t *testing.T) {
	t.Parallel()

	cat := newFakeCatalog()
	cat.labelNames = set() // catalogue has none, so all source labels are missing
	svc := importverify.NewService(importverify.Config{
		PhotoPrism: &fakePhotoPrism{
			photos: []photoprism.Photo{
				photo("x1", "image", "h1", 1),
				photo("x2", "image", "h2", 1),
				photo("x3", "image", "h3", 1),
			},
			labels: []photoprism.Label{{Name: "a"}, {Name: "b"}, {Name: "c"}},
		},
		Catalog:     cat,
		SampleLimit: 1,
	})

	report, err := svc.Verify(context.Background())
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if report.PhotoPrism.MissingCount != 3 || len(report.PhotoPrism.MissingUIDs) != 1 {
		t.Errorf("photos missing = %d, listed = %d, want 3/1",
			report.PhotoPrism.MissingCount, len(report.PhotoPrism.MissingUIDs))
	}
	if report.Structure.Labels.MissingCount != 3 || len(report.Structure.Labels.Missing) != 1 {
		t.Errorf("labels missing = %d, listed = %d, want 3/1",
			report.Structure.Labels.MissingCount, len(report.Structure.Labels.Missing))
	}
}

// TestService_Verify_complete tests the Complete flag across each blocking
// condition and the all-clear case.
func TestService_Verify_complete(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		build func() importverify.Config
		want  bool
	}{
		{
			name: "all clear",
			build: func() importverify.Config {
				cat := newFakeCatalog()
				cat.importedUIDs = set("ppA")
				cat.fileCounts = map[string]int{"ppA": 1}
				return importverify.Config{
					PhotoPrism: &fakePhotoPrism{photos: []photoprism.Photo{photo("ppA", "image", "h1", 1)}},
					Feeds:      &fakeFeeds{},
					Catalog:    cat,
				}
			},
			want: true,
		},
		{
			name: "missing photo blocks",
			build: func() importverify.Config {
				return importverify.Config{
					PhotoPrism: &fakePhotoPrism{photos: []photoprism.Photo{photo("ppX", "image", "h1", 1)}},
					Catalog:    newFakeCatalog(),
				}
			},
			want: false,
		},
		{
			name: "file gap blocks",
			build: func() importverify.Config {
				cat := newFakeCatalog()
				cat.importedUIDs = set("ppA")
				cat.fileCounts = map[string]int{"ppA": 0}
				return importverify.Config{
					PhotoPrism: &fakePhotoPrism{photos: []photoprism.Photo{photo("ppA", "raw", "h1", 2)}},
					Catalog:    cat,
				}
			},
			want: false,
		},
		{
			name: "missing embedding blocks",
			build: func() importverify.Config {
				cat := newFakeCatalog()
				cat.missingEmb = []string{"e1"}
				return importverify.Config{
					PhotoPrism: &fakePhotoPrism{},
					Feeds:      &fakeFeeds{},
					Catalog:    cat,
				}
			},
			want: false,
		},
		{
			name: "missing album blocks",
			build: func() importverify.Config {
				return importverify.Config{
					PhotoPrism: &fakePhotoPrism{
						albumsByType: map[string][]photoprism.Album{"album": {{Title: "Trip"}}},
					},
					Catalog:    newFakeCatalog(),
					AlbumTypes: []string{"album"},
				}
			},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			svc := importverify.NewService(tt.build())
			report, err := svc.Verify(context.Background())
			if err != nil {
				t.Fatalf("Verify: %v", err)
			}
			if report.Complete != tt.want {
				t.Errorf("Complete = %v, want %v", report.Complete, tt.want)
			}
		})
	}
}

// TestService_Verify_listError checks a source-listing failure aborts Verify with
// a wrapped error.
func TestService_Verify_listError(t *testing.T) {
	t.Parallel()

	svc := importverify.NewService(importverify.Config{
		PhotoPrism: &fakePhotoPrism{listErr: errListing},
		Catalog:    newFakeCatalog(),
	})
	_, err := svc.Verify(context.Background())
	if !errors.Is(err, errListing) {
		t.Fatalf("Verify error = %v, want wrapping %v", err, errListing)
	}
}

// TestNewService_panics checks NewService rejects a missing required collaborator.
func TestNewService_panics(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		cfg       importverify.Config
		wantPanic bool
	}{
		{
			name:      "nil PhotoPrism panics",
			cfg:       importverify.Config{Catalog: newFakeCatalog()},
			wantPanic: true,
		},
		{
			name:      "nil Catalog panics",
			cfg:       importverify.Config{PhotoPrism: &fakePhotoPrism{}},
			wantPanic: true,
		},
		{
			name:      "both present does not panic",
			cfg:       importverify.Config{PhotoPrism: &fakePhotoPrism{}, Catalog: newFakeCatalog()},
			wantPanic: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			defer func() {
				got := recover() != nil
				if got != tt.wantPanic {
					t.Errorf("panic = %v, want %v", got, tt.wantPanic)
				}
			}()
			_ = importverify.NewService(tt.cfg)
		})
	}
}
