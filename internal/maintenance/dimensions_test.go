package maintenance

import (
	"context"
	"testing"

	"github.com/panbotka/kukatko/internal/photos"
	"github.com/panbotka/kukatko/internal/vectors"
)

// facePlan builds one planned face row for the fake vector catalogue.
func facePlan(photoUID string, index int, transform vectors.FrameTransform) vectors.FaceBoxPlan {
	return vectors.FaceBoxPlan{
		Face: vectors.TransposedFace{
			PhotoUID:    photoUID,
			FaceIndex:   index,
			BBox:        [4]float64{0.1, 0.2, 0.2, 0.2},
			Orientation: 6,
			RawWidth:    4000,
			RawHeight:   3000,
		},
		Transform: transform,
		BBox:      [4]float64{0.6, 0.1, 0.2, 0.2},
	}
}

// dimensionScenario builds a service whose catalogue reports two quarter-turned
// photos with transposed dimensions and four face rows recorded against the
// transposed frame — two the evidence places (one to turn, one whose box is
// already right and only its cached frame is not) and one it cannot place — and
// returns the two fakes so the test can see exactly what the repair wrote.
func dimensionScenario() (*Service, *fakePhotos, *fakeVectors) {
	ph := &fakePhotos{
		mismatches: []photos.DimensionMismatch{
			{UID: "p1", StoredWidth: 3648, StoredHeight: 5472, Orientation: 8, RawWidth: 5472, RawHeight: 3648},
			{UID: "p2", StoredWidth: 3000, StoredHeight: 4000, Orientation: 6, RawWidth: 4000, RawHeight: 3000},
		},
	}
	vec := &fakeVectors{facePlans: []vectors.FaceBoxPlan{
		facePlan("p1", 0, vectors.TransformRotate),
		facePlan("p1", 1, vectors.TransformSkip),
		facePlan("p2", 0, vectors.TransformRotate),
		facePlan("p2", 1, vectors.TransformFrame),
	}}
	svc := New(Config{
		Photos:    ph,
		Vectors:   vec,
		Originals: fakeOriginals{present: map[string]bool{}},
		Disk:      fakeDisk{},
		Thumbs:    fakeThumbs{have: map[string]bool{}},
		Enqueuer:  &fakeEnqueuer{},
		Embed:     &fakeBackfiller{},
		Faces:     &fakeFaceBackfiller{},
		FaceCache: &fakeFaceCache{},
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

// TestScanReportsTransposedFaceBoxes verifies the scan is the dry run of the faces
// half too: it counts the face rows the repair would rewrite (per row, sampled by
// photo) and leaves out the row the evidence cannot place, which the repair would
// not touch either.
func TestScanReportsTransposedFaceBoxes(t *testing.T) {
	t.Parallel()
	svc, _, _ := dimensionScenario()

	report, err := svc.Scan(context.Background())
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if report.TransposedFaceBoxes.Count != 3 {
		t.Errorf("count = %d, want 3 (the skipped row is not reported)", report.TransposedFaceBoxes.Count)
	}
	want := []string{"p1", "p2"}
	if got := report.TransposedFaceBoxes.Samples; len(got) != 2 || got[0] != want[0] || got[1] != want[1] {
		t.Errorf("samples = %v, want the affected photos %v once each", got, want)
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
	if len(ph.repaired) != 0 || len(vec.appliedFaces) != 0 {
		t.Errorf("wrote %v / %v, want nothing", ph.repaired, vec.appliedFaces)
	}
}

// TestRepairDimensionsFixesPhotosAndFaces verifies the repair rewrites each
// mismatched photo with the file's own dimensions and applies the planned
// transform to every face row the evidence placed — and only to those.
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
	if res.FaceBoxesFixed != 3 || res.FaceBoxesSkipped != 1 {
		t.Errorf("faces fixed=%d skipped=%d, want 3/1", res.FaceBoxesFixed, res.FaceBoxesSkipped)
	}
	if len(ph.repaired) != 2 || ph.repaired[0].RawWidth != 5472 || ph.repaired[0].RawHeight != 3648 {
		t.Errorf("repaired photos = %+v", ph.repaired)
	}
	// The row the evidence could not place is not among the writes: it keeps the
	// fingerprint a later run finds it by.
	want := []string{"p1#0:2", "p2#0:2", "p2#1:1"}
	if len(vec.appliedFaces) != 3 || vec.appliedFaces[0] != want[0] ||
		vec.appliedFaces[1] != want[1] || vec.appliedFaces[2] != want[2] {
		t.Errorf("applied faces = %v, want %v", vec.appliedFaces, want)
	}
}

// TestRepairFaceBoxesRunsWithoutAPhotoToFix verifies the faces half no longer
// hangs off the photo half: a catalogue whose photo rows are all correct already
// (an earlier run fixed them) still has its face rows decided and repaired, which
// is what makes a row skipped for want of evidence reachable by a later run.
func TestRepairFaceBoxesRunsWithoutAPhotoToFix(t *testing.T) {
	t.Parallel()
	svc, ph, vec := dimensionScenario()
	ph.mismatches = nil

	res, err := svc.Repair(context.Background(), RepairOptions{Dimensions: true})
	if err != nil {
		t.Fatalf("Repair: %v", err)
	}
	if res.DimensionsFixed != 0 {
		t.Errorf("DimensionsFixed = %d, want 0 with no mismatched photo", res.DimensionsFixed)
	}
	if res.FaceBoxesFixed != 3 || res.FaceBoxesSkipped != 1 {
		t.Errorf("faces fixed=%d skipped=%d, want 3/1", res.FaceBoxesFixed, res.FaceBoxesSkipped)
	}
	if len(vec.appliedFaces) != 3 {
		t.Errorf("applied faces = %v, want three rows written", vec.appliedFaces)
	}
}
