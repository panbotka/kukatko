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
}

// ListPhotos returns one page of the fake's photos, or the injected error.
func (f *fakePhotoPrism) ListPhotos(
	_ context.Context, params photoprism.PhotoListParams,
) ([]photoprism.Photo, error) {
	if f.listErr != nil {
		return nil, f.listErr
	}
	return pageSlice(f.photos, params.Offset, params.Count), nil
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
		importedHashes: map[string]struct{}{},
		fileCounts:     map[string]int{},
		albumTitles:    map[string]struct{}{},
		labelNames:     map[string]struct{}{},
		subjectNames:   map[string]struct{}{},
	}
}

// ImportedRefs returns the fake's uid and file-hash sets.
func (c *fakeCatalog) ImportedRefs(
	_ context.Context,
) (map[string]struct{}, map[string]struct{}, error) {
	return c.importedUIDs, c.importedHashes, nil
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
		if report.Vectors.MissingEmbeddings == nil || report.Vectors.MissingFaces == nil {
			t.Error("missing slices should be non-nil so they marshal as []")
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
		if v.MissingEmbeddingsCount != 2 || !slices.Equal(v.MissingEmbeddings, []string{"e1", "e2"}) {
			t.Errorf("missing embeddings = %d/%v", v.MissingEmbeddingsCount, v.MissingEmbeddings)
		}
		if v.MissingFacesCount != 1 || !slices.Equal(v.MissingFaces, []string{"f1"}) {
			t.Errorf("missing faces = %d/%v", v.MissingFacesCount, v.MissingFaces)
		}
		if report.Complete {
			t.Error("Complete = true, want false (missing vectors present)")
		}
	})
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
	assertEntity(t, "albums", report.Structure.Albums, 2, 1, []string{"Family"})
	assertEntity(t, "labels", report.Structure.Labels, 2, 1, []string{"dog"})
	assertEntity(t, "subjects", report.Structure.Subjects, 2, 1, []string{"Bob"})
	if report.Complete {
		t.Error("Complete = true, want false (structure gaps present)")
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
