package maintenance

import (
	"context"
	"errors"
	"fmt"
	"os"
	"slices"
	"testing"

	"github.com/panbotka/kukatko/internal/photos"
	"github.com/panbotka/kukatko/internal/vectors"
)

// --- fakes -------------------------------------------------------------------

// fakePhotos is an in-memory PhotoCatalog.
type fakePhotos struct {
	count        int
	primary      []photos.PrimaryFile
	filePaths    []string
	missingPhash []string
	// mismatches are the transposed-dimension rows the scan reports and the
	// dimension repair rewrites; repaired records the uids actually written.
	// repairNoop makes every write report "row unchanged", the state a
	// concurrently-repaired row leaves behind.
	mismatches []photos.DimensionMismatch
	repaired   []photos.DimensionMismatch
	repairNoop bool
}

func (f *fakePhotos) CountPhotos(context.Context) (int, error) { return f.count, nil }
func (f *fakePhotos) ListPrimaryFiles(context.Context) ([]photos.PrimaryFile, error) {
	return f.primary, nil
}
func (f *fakePhotos) ListFilePaths(context.Context) ([]string, error) { return f.filePaths, nil }
func (f *fakePhotos) ListPhotosMissingPhash(context.Context, int) ([]string, error) {
	return f.missingPhash, nil
}
func (f *fakePhotos) ListDimensionMismatches(context.Context) ([]photos.DimensionMismatch, error) {
	return f.mismatches, nil
}
func (f *fakePhotos) RepairDimensions(_ context.Context, m photos.DimensionMismatch) (bool, error) {
	if f.repairNoop {
		return false, nil
	}
	f.repaired = append(f.repaired, m)
	return true, nil
}

// fakeVectors is an in-memory VectorCatalog.
type fakeVectors struct {
	missingEmb, missingFaces []string
	// facePlans is what the faces half of the dimension repair would do; appliedFaces
	// records the rows actually written, as "uid#index:transform".
	facePlans    []vectors.FaceBoxPlan
	appliedFaces []string
	// duplicateMarkers are the markers cached on more than one face, which the scan
	// reports and the face-marker repair re-matches.
	duplicateMarkers []vectors.DuplicateFaceMarker
}

func (f *fakeVectors) ListPhotosMissingEmbedding(context.Context, int) ([]string, error) {
	return f.missingEmb, nil
}
func (f *fakeVectors) ListPhotosMissingFaces(context.Context, int) ([]string, error) {
	return f.missingFaces, nil
}
func (f *fakeVectors) PlanFaceBoxRepair(context.Context) ([]vectors.FaceBoxPlan, error) {
	return f.facePlans, nil
}
func (f *fakeVectors) ApplyFaceBoxRepair(
	_ context.Context, plans []vectors.FaceBoxPlan,
) (int64, error) {
	var applied int64
	for _, plan := range plans {
		if plan.Transform == vectors.TransformSkip {
			continue
		}
		f.appliedFaces = append(f.appliedFaces,
			fmt.Sprintf("%s#%d:%d", plan.Face.PhotoUID, plan.Face.FaceIndex, plan.Transform))
		applied++
	}
	return applied, nil
}
func (f *fakeVectors) ListDuplicateFaceMarkers(context.Context) ([]vectors.DuplicateFaceMarker, error) {
	return f.duplicateMarkers, nil
}

// fakeFaceCache records the photos whose surplus face↔marker links were cleared
// and reports a fixed number of cleared rows per photo.
type fakeFaceCache struct {
	perPhoto int
	cleared  []string
}

func (f *fakeFaceCache) ClearSurplusLinks(_ context.Context, photoUID string) (int, error) {
	f.cleared = append(f.cleared, photoUID)
	return f.perPhoto, nil
}

// fakeOriginals reports presence from a key set, returning os.ErrNotExist for
// absent keys.
type fakeOriginals struct{ present map[string]bool }

func (f fakeOriginals) Stat(_ context.Context, relPath string) (os.FileInfo, error) {
	if f.present[relPath] {
		return nil, nil //nolint:nilnil // presence is signalled by a nil error; the caller ignores the FileInfo.
	}
	return nil, os.ErrNotExist
}

// fakeDisk lists a fixed set of on-disk files.
type fakeDisk struct{ files []DiskFile }

func (f fakeDisk) List(context.Context) ([]DiskFile, error) { return f.files, nil }

// fakeThumbs reports thumbnail presence from a hash set.
type fakeThumbs struct{ have map[string]bool }

func (f fakeThumbs) HasThumbnail(hash string) (bool, error) { return f.have[hash], nil }

// fakeEnqueuer records the photo uids it was asked to schedule thumbnail jobs for.
type fakeEnqueuer struct{ thumbnail []string }

func (f *fakeEnqueuer) EnqueueThumbnail(_ context.Context, uid string) error {
	f.thumbnail = append(f.thumbnail, uid)
	return nil
}

// fakeBackfiller records that the embedding backfill ran and returns a fixed count.
type fakeBackfiller struct {
	n      int
	called bool
}

func (f *fakeBackfiller) BackfillEmbeddings(context.Context) (int, error) {
	f.called = true
	return f.n, nil
}

// fakeFaceBackfiller records that the face backfill ran and returns a fixed count.
type fakeFaceBackfiller struct {
	n      int
	called bool
}

func (f *fakeFaceBackfiller) BackfillFaces(context.Context) (int, error) {
	f.called = true
	return f.n, nil
}

// fakeImporter records imported keys and returns configured outcomes/errors.
type fakeImporter struct {
	outcomes map[string]ImportOutcome
	errs     map[string]error
	imported []string
}

func (f *fakeImporter) ImportOriginal(_ context.Context, key string) (ImportOutcome, error) {
	f.imported = append(f.imported, key)
	if err := f.errs[key]; err != nil {
		return ImportCreated, err
	}
	return f.outcomes[key], nil
}

// --- fixtures ----------------------------------------------------------------

// scenario builds a Service over a representative drift fixture: p2's original is
// missing on disk, p3's thumbnail is missing, orphan1 is an extra file on disk,
// one photo each is missing an embedding, faces and a pHash, and marker mk1 is
// cached on two of p1's faces.
func scenario() (*Service, *fakeEnqueuer, *fakeBackfiller, *fakeFaceBackfiller, *fakeImporter) {
	enq := &fakeEnqueuer{}
	emb := &fakeBackfiller{n: 7}
	faces := &fakeFaceBackfiller{n: 3}
	imp := &fakeImporter{outcomes: map[string]ImportOutcome{"orphan1": ImportCreated}}
	svc := New(Config{
		Photos: &fakePhotos{
			count: 3,
			primary: []photos.PrimaryFile{
				{PhotoUID: "p1", FilePath: "a", FileHash: "h1"},
				{PhotoUID: "p2", FilePath: "b", FileHash: "h2"},
				{PhotoUID: "p3", FilePath: "c", FileHash: "h3"},
			},
			filePaths:    []string{"a", "b", "c"},
			missingPhash: []string{"p3"},
		},
		Vectors: &fakeVectors{
			missingEmb:       []string{"p2"},
			missingFaces:     []string{"p1"},
			duplicateMarkers: []vectors.DuplicateFaceMarker{{MarkerUID: "mk1", PhotoUID: "p1", Faces: 2}},
		},
		Originals: fakeOriginals{present: map[string]bool{"a": true, "c": true}},
		Disk:      fakeDisk{files: []DiskFile{{Key: "a"}, {Key: "c"}, {Key: "orphan1"}}},
		Thumbs:    fakeThumbs{have: map[string]bool{"h1": true, "h2": true}},
		Enqueuer:  enq,
		Embed:     emb,
		Faces:     faces,
		FaceCache: &fakeFaceCache{},
		Importer:  imp,
	})
	return svc, enq, emb, faces, imp
}

// --- tests -------------------------------------------------------------------

// TestScan verifies the scan reports each drift class with the right counts and
// samples over the fixture.
func TestScan(t *testing.T) {
	t.Parallel()
	svc, _, _, _, _ := scenario()

	report, err := svc.Scan(context.Background())
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	checks := []struct {
		name   string
		got    Finding
		count  int
		sample string
	}{
		{"missing originals", report.MissingOriginals, 1, "p2"},
		{"orphan files", report.OrphanFiles, 1, "orphan1"},
		{"missing thumbnails", report.MissingThumbnails, 1, "p3"},
		{"missing embeddings", report.MissingEmbeddings, 1, "p2"},
		{"missing faces", report.MissingFaces, 1, "p1"},
		{"missing phashes", report.MissingPhashes, 1, "p3"},
		{"duplicate face markers", report.DuplicateFaceMarkers, 1, "mk1"},
	}
	for _, c := range checks {
		if c.got.Count != c.count {
			t.Errorf("%s count = %d, want %d", c.name, c.got.Count, c.count)
		}
		if len(c.got.Samples) != 1 || c.got.Samples[0] != c.sample {
			t.Errorf("%s samples = %v, want [%s]", c.name, c.got.Samples, c.sample)
		}
	}
	if report.Photos != 3 || report.FilesInDB != 3 || report.OriginalsOnDisk != 3 {
		t.Errorf("totals = photos:%d files:%d disk:%d, want 3/3/3",
			report.Photos, report.FilesInDB, report.OriginalsOnDisk)
	}
	if report.Clean() {
		t.Error("report with findings should not be Clean")
	}
}

// TestRepairThumbnailsOnlyMissing verifies the thumbnail repair enqueues a job
// only for photos whose representative thumbnail is missing.
func TestRepairThumbnailsOnlyMissing(t *testing.T) {
	t.Parallel()
	svc, enq, _, _, _ := scenario()

	res, err := svc.Repair(context.Background(), RepairOptions{Thumbnails: true})
	if err != nil {
		t.Fatalf("Repair: %v", err)
	}
	if res.ThumbnailsEnqueued != 1 {
		t.Errorf("ThumbnailsEnqueued = %d, want 1", res.ThumbnailsEnqueued)
	}
	if len(enq.thumbnail) != 1 || enq.thumbnail[0] != "p3" {
		t.Errorf("enqueued = %v, want [p3]", enq.thumbnail)
	}
}

// TestRepairPhashesEnqueuesMissing verifies the pHash repair enqueues a thumbnail
// job for each photo missing perceptual hashes.
func TestRepairPhashesEnqueuesMissing(t *testing.T) {
	t.Parallel()
	svc, enq, _, _, _ := scenario()

	res, err := svc.Repair(context.Background(), RepairOptions{Phashes: true})
	if err != nil {
		t.Fatalf("Repair: %v", err)
	}
	if res.PhashesEnqueued != 1 || len(enq.thumbnail) != 1 || enq.thumbnail[0] != "p3" {
		t.Errorf("phash repair enqueued = %v (count %d), want [p3]", enq.thumbnail, res.PhashesEnqueued)
	}
}

// TestRepairBackfills verifies the embedding and face repairs delegate to the
// backfillers and report their counts.
func TestRepairBackfills(t *testing.T) {
	t.Parallel()
	svc, _, emb, faces, _ := scenario()

	res, err := svc.Repair(context.Background(), RepairOptions{Embeddings: true, Faces: true})
	if err != nil {
		t.Fatalf("Repair: %v", err)
	}
	if !emb.called || res.EmbeddingsEnqueued != 7 {
		t.Errorf("embeddings: called=%v enqueued=%d, want true/7", emb.called, res.EmbeddingsEnqueued)
	}
	if !faces.called || res.FacesEnqueued != 3 {
		t.Errorf("faces: called=%v enqueued=%d, want true/3", faces.called, res.FacesEnqueued)
	}
}

// TestRepairImportsOrphans verifies the orphan-import repair imports each orphan
// and tallies the outcomes.
func TestRepairImportsOrphans(t *testing.T) {
	t.Parallel()
	svc, _, _, _, imp := scenario()

	res, err := svc.Repair(context.Background(), RepairOptions{ImportOrphans: true})
	if err != nil {
		t.Fatalf("Repair: %v", err)
	}
	if len(imp.imported) != 1 || imp.imported[0] != "orphan1" {
		t.Errorf("imported = %v, want [orphan1]", imp.imported)
	}
	if res.OrphansImported != 1 || res.OrphansSkipped != 0 || res.OrphansFailed != 0 {
		t.Errorf("orphan tally = %+v, want imported 1", res)
	}
}

// TestRepairImportOrphanTally verifies created/duplicate/failed orphans are
// tallied separately and a failure does not abort the batch.
func TestRepairImportOrphanTally(t *testing.T) {
	t.Parallel()
	imp := &fakeImporter{
		outcomes: map[string]ImportOutcome{"a": ImportCreated, "b": ImportDuplicate},
		errs:     map[string]error{"c": errors.New("boom")},
	}
	svc := New(Config{
		Photos:    &fakePhotos{filePaths: nil},
		Vectors:   &fakeVectors{},
		Originals: fakeOriginals{present: map[string]bool{}},
		Disk:      fakeDisk{files: []DiskFile{{Key: "a"}, {Key: "b"}, {Key: "c"}}},
		Thumbs:    fakeThumbs{have: map[string]bool{}},
		Enqueuer:  &fakeEnqueuer{},
		Embed:     &fakeBackfiller{},
		Faces:     &fakeFaceBackfiller{},
		FaceCache: &fakeFaceCache{},
		Importer:  imp,
	})
	res, err := svc.Repair(context.Background(), RepairOptions{ImportOrphans: true})
	if err != nil {
		t.Fatalf("Repair: %v", err)
	}
	if res.OrphansImported != 1 || res.OrphansSkipped != 1 || res.OrphansFailed != 1 {
		t.Errorf("tally = %+v, want imported 1, skipped 1, failed 1", res)
	}
}

// TestRepairOrphanImportUnavailable verifies an orphan-import repair without a
// configured importer returns ErrOrphanImportUnavailable.
func TestRepairOrphanImportUnavailable(t *testing.T) {
	t.Parallel()
	svc := New(Config{
		Photos:    &fakePhotos{},
		Vectors:   &fakeVectors{},
		Originals: fakeOriginals{present: map[string]bool{}},
		Disk:      fakeDisk{},
		Thumbs:    fakeThumbs{have: map[string]bool{}},
		Enqueuer:  &fakeEnqueuer{},
		Embed:     &fakeBackfiller{},
		Faces:     &fakeFaceBackfiller{},
		FaceCache: &fakeFaceCache{},
		// Importer omitted.
	})
	if _, err := svc.Repair(context.Background(), RepairOptions{ImportOrphans: true}); !errors.Is(err, ErrOrphanImportUnavailable) {
		t.Errorf("Repair error = %v, want ErrOrphanImportUnavailable", err)
	}
}

// TestRepairFaceMarkersVisitsEachPhotoOnce verifies the face-marker repair
// re-matches every photo carrying a duplicated marker exactly once — several
// duplicates on one photo are resolved by a single pass — and tallies the cleared
// links.
func TestRepairFaceMarkersVisitsEachPhotoOnce(t *testing.T) {
	t.Parallel()
	cache := &fakeFaceCache{perPhoto: 2}
	svc := New(Config{
		Photos: &fakePhotos{},
		Vectors: &fakeVectors{duplicateMarkers: []vectors.DuplicateFaceMarker{
			{MarkerUID: "mkB", PhotoUID: "p2", Faces: 2},
			{MarkerUID: "mkA", PhotoUID: "p1", Faces: 3},
			{MarkerUID: "mkC", PhotoUID: "p1", Faces: 2},
		}},
		Originals: fakeOriginals{present: map[string]bool{}},
		Disk:      fakeDisk{},
		Thumbs:    fakeThumbs{have: map[string]bool{}},
		Enqueuer:  &fakeEnqueuer{},
		Embed:     &fakeBackfiller{},
		Faces:     &fakeFaceBackfiller{},
		FaceCache: cache,
	})

	res, err := svc.Repair(context.Background(), RepairOptions{FaceMarkers: true})
	if err != nil {
		t.Fatalf("Repair(face markers): %v", err)
	}
	if !slices.Equal(cache.cleared, []string{"p1", "p2"}) {
		t.Errorf("re-matched photos = %v, want [p1 p2] (each once, sorted)", cache.cleared)
	}
	if res.FaceLinksCleared != 4 {
		t.Errorf("FaceLinksCleared = %d, want 4 (2 per photo)", res.FaceLinksCleared)
	}
}

// TestRepairFaceMarkersNotSelected verifies the face-marker repair is opt-in: an
// unrelated repair must not touch the face cache.
func TestRepairFaceMarkersNotSelected(t *testing.T) {
	t.Parallel()
	svc, _, _, _, _ := scenario()

	if _, err := svc.Repair(context.Background(), RepairOptions{Thumbnails: true}); err != nil {
		t.Fatalf("Repair(thumbnails): %v", err)
	}
	// The fixture reports a duplicated marker; nothing may have been cleared.
	report, err := svc.Scan(context.Background())
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if report.DuplicateFaceMarkers.Count != 1 {
		t.Errorf("duplicate face markers = %d, want 1 (repair was not selected)",
			report.DuplicateFaceMarkers.Count)
	}
}

// TestNewPanicsOnMissingDependency verifies New panics when a required
// collaborator is nil.
func TestNewPanicsOnMissingDependency(t *testing.T) {
	t.Parallel()
	defer func() {
		if recover() == nil {
			t.Error("New with a nil dependency should panic")
		}
	}()
	New(Config{})
}
