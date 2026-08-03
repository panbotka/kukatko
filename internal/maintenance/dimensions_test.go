package maintenance

import (
	"context"
	"testing"

	"github.com/panbotka/kukatko/internal/photos"
)

// dimensionScenario builds a service whose catalogue reports two quarter-turned
// photos with transposed dimensions, and returns the two fakes so the test can
// see exactly what the repair wrote.
func dimensionScenario() (*Service, *fakePhotos, *fakeVectors) {
	ph := &fakePhotos{
		mismatches: []photos.DimensionMismatch{
			{UID: "p1", StoredWidth: 3648, StoredHeight: 5472, Orientation: 8, RawWidth: 5472, RawHeight: 3648},
			{UID: "p2", StoredWidth: 3000, StoredHeight: 4000, Orientation: 6, RawWidth: 4000, RawHeight: 3000},
		},
	}
	vec := &fakeVectors{facesPerPhoto: 2}
	svc := New(Config{
		Photos:    ph,
		Vectors:   vec,
		Originals: fakeOriginals{present: map[string]bool{}},
		Disk:      fakeDisk{},
		Thumbs:    fakeThumbs{have: map[string]bool{}},
		Enqueuer:  &fakeEnqueuer{},
		Embed:     &fakeBackfiller{},
		Faces:     &fakeFaceBackfiller{},
	})
	return svc, ph, vec
}

// TestScanReportsTransposedDimensions verifies the scan surfaces the mismatched
// photos as a finding — the dry run of the repair — and that they count against
// Clean.
func TestScanReportsTransposedDimensions(t *testing.T) {
	t.Parallel()
	svc, _, _ := dimensionScenario()

	report, err := svc.Scan(context.Background())
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if report.TransposedDimensions.Count != 2 {
		t.Errorf("count = %d, want 2", report.TransposedDimensions.Count)
	}
	want := []string{"p1", "p2"}
	if got := report.TransposedDimensions.Samples; len(got) != 2 || got[0] != want[0] || got[1] != want[1] {
		t.Errorf("samples = %v, want %v", got, want)
	}
	if report.Clean() {
		t.Error("Clean() = true, want false with transposed dimensions outstanding")
	}
}

// TestRepairDimensionsIsOptIn verifies the dimension repair writes nothing unless
// it is selected — it is the one repair that changes catalogue rows rather than
// enqueuing regenerable work, so a bare `repair` must never trigger it.
func TestRepairDimensionsIsOptIn(t *testing.T) {
	t.Parallel()
	svc, ph, vec := dimensionScenario()

	res, err := svc.Repair(context.Background(), RepairOptions{Thumbnails: true})
	if err != nil {
		t.Fatalf("Repair: %v", err)
	}
	if res.DimensionsFixed != 0 || res.FaceBoxesFixed != 0 {
		t.Errorf("fixed = %d photos / %d faces, want 0/0", res.DimensionsFixed, res.FaceBoxesFixed)
	}
	if len(ph.repaired) != 0 || len(vec.repairedFaces) != 0 {
		t.Errorf("wrote %v / %v, want nothing", ph.repaired, vec.repairedFaces)
	}
}

// TestRepairDimensionsFixesPhotosAndFaces verifies the repair rewrites each
// mismatched photo with the file's own dimensions and re-normalises that photo's
// faces against the same corrected frame.
func TestRepairDimensionsFixesPhotosAndFaces(t *testing.T) {
	t.Parallel()
	svc, ph, vec := dimensionScenario()

	res, err := svc.Repair(context.Background(), RepairOptions{Dimensions: true})
	if err != nil {
		t.Fatalf("Repair: %v", err)
	}
	if res.DimensionsFixed != 2 {
		t.Errorf("DimensionsFixed = %d, want 2", res.DimensionsFixed)
	}
	if res.FaceBoxesFixed != 4 {
		t.Errorf("FaceBoxesFixed = %d, want 4", res.FaceBoxesFixed)
	}
	if len(ph.repaired) != 2 || ph.repaired[0].RawWidth != 5472 || ph.repaired[0].RawHeight != 3648 {
		t.Errorf("repaired photos = %+v", ph.repaired)
	}
	// The faces repair must be keyed on the photo's corrected (stored) pair, not
	// on the transposed one it replaced.
	want := []string{"p1:5472x3648", "p2:4000x3000"}
	if len(vec.repairedFaces) != 2 || vec.repairedFaces[0] != want[0] || vec.repairedFaces[1] != want[1] {
		t.Errorf("repaired faces = %v, want %v", vec.repairedFaces, want)
	}
}

// TestRepairDimensionsSkipsFacesForUnchangedPhotos verifies a photo whose row did
// not change (someone else fixed it first) does not have its faces rescaled — the
// two halves must move together or a re-run would rescale twice.
func TestRepairDimensionsSkipsFacesForUnchangedPhotos(t *testing.T) {
	t.Parallel()
	svc, ph, vec := dimensionScenario()
	ph.repairNoop = true

	res, err := svc.Repair(context.Background(), RepairOptions{Dimensions: true})
	if err != nil {
		t.Fatalf("Repair: %v", err)
	}
	if res.DimensionsFixed != 0 || res.FaceBoxesFixed != 0 {
		t.Errorf("fixed = %d photos / %d faces, want 0/0", res.DimensionsFixed, res.FaceBoxesFixed)
	}
	if len(vec.repairedFaces) != 0 {
		t.Errorf("repaired faces = %v, want none", vec.repairedFaces)
	}
}
