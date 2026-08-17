package maintenance

import (
	"context"
	"strings"
	"testing"
)

// sidewaysScenario builds a service whose catalogue reports three quarter-turned
// photos detected on a sideways image, and returns the fakes so a test can see
// exactly what the repair cleared and scheduled.
func sidewaysScenario() (*Service, *fakeVectors, *fakeEnqueuer) {
	vec := &fakeVectors{sideways: []string{"p1", "p2", "p3"}}
	enq := &fakeEnqueuer{}
	svc := New(Config{
		Photos:    &fakePhotos{},
		Vectors:   vec,
		Originals: fakeOriginals{present: map[string]bool{}},
		Disk:      fakeDisk{},
		Thumbs:    fakeThumbs{have: map[string]bool{}},
		Enqueuer:  enq,
		Embed:     &fakeBackfiller{},
		Faces:     &fakeFaceBackfiller{},
		FaceCache: &fakeFaceCache{},
	})
	return svc, vec, enq
}

// TestScanReportsSidewaysDetections verifies the scan surfaces the photos detected
// sideways as a finding — the dry run of the repair — and that they count against
// Clean.
func TestScanReportsSidewaysDetections(t *testing.T) {
	t.Parallel()
	svc, _, _ := sidewaysScenario()

	report, err := svc.Scan(context.Background())
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if report.SidewaysFaceDetections.Count != 3 {
		t.Errorf("count = %d, want 3", report.SidewaysFaceDetections.Count)
	}
	if got := strings.Join(report.SidewaysFaceDetections.Samples, ","); got != "p1,p2,p3" {
		t.Errorf("samples = %v, want the three affected photos", got)
	}
	if report.Clean() {
		t.Error("Clean() = true, want false with sideways detections outstanding")
	}
}

// TestRepairSidewaysFacesIsOptIn verifies a bare repair never clears a detection:
// re-detection depends on a sidecar that is usually asleep, so it has to be asked
// for.
func TestRepairSidewaysFacesIsOptIn(t *testing.T) {
	t.Parallel()
	svc, vec, enq := sidewaysScenario()

	res, err := svc.Repair(context.Background(), RepairOptions{Thumbnails: true})
	if err != nil {
		t.Fatalf("Repair: %v", err)
	}
	if res.SidewaysFacesEnqueued != 0 {
		t.Errorf("enqueued = %d, want 0", res.SidewaysFacesEnqueued)
	}
	if len(vec.clearedDetections) != 0 || len(enq.faceDetect) != 0 {
		t.Errorf("cleared %v and enqueued %v, want nothing", vec.clearedDetections, enq.faceDetect)
	}
}

// TestRepairSidewaysFacesClearsAndEnqueues verifies the repair does both halves for
// every affected photo: the detection record is cleared (which is what makes the
// photo eligible again) and a face_detect job is scheduled, counted per photo.
func TestRepairSidewaysFacesClearsAndEnqueues(t *testing.T) {
	t.Parallel()
	svc, vec, enq := sidewaysScenario()

	res, err := svc.Repair(context.Background(), RepairOptions{SidewaysFaces: true})
	if err != nil {
		t.Fatalf("Repair: %v", err)
	}
	if res.SidewaysFacesEnqueued != 3 {
		t.Errorf("enqueued = %d, want 3", res.SidewaysFacesEnqueued)
	}
	if got := strings.Join(vec.clearedDetections, ","); got != "p1,p2,p3" {
		t.Errorf("cleared = %v, want p1,p2,p3", got)
	}
	if got := strings.Join(enq.faceDetect, ","); got != "p1,p2,p3" {
		t.Errorf("enqueued = %v, want p1,p2,p3", got)
	}
}

// TestRepairSidewaysFacesLeavesOtherRepairsAlone verifies the repair is confined to
// its own half: it schedules face detection for the photos it cleared and does not
// touch the faces backfill, whose job is the photos that were never processed.
func TestRepairSidewaysFacesLeavesOtherRepairsAlone(t *testing.T) {
	t.Parallel()
	vec := &fakeVectors{sideways: []string{"p1"}, missingFaces: []string{"other"}}
	faces := &fakeFaceBackfiller{}
	enq := &fakeEnqueuer{}
	svc := New(Config{
		Photos:    &fakePhotos{},
		Vectors:   vec,
		Originals: fakeOriginals{present: map[string]bool{}},
		Disk:      fakeDisk{},
		Thumbs:    fakeThumbs{have: map[string]bool{}},
		Enqueuer:  enq,
		Embed:     &fakeBackfiller{},
		Faces:     faces,
		FaceCache: &fakeFaceCache{},
	})

	if _, err := svc.Repair(context.Background(), RepairOptions{SidewaysFaces: true}); err != nil {
		t.Fatalf("Repair: %v", err)
	}
	if faces.called {
		t.Error("the faces backfill ran, want only the sideways photos re-detected")
	}
	if got := strings.Join(enq.faceDetect, ","); got != "p1" {
		t.Errorf("enqueued = %v, want only p1", got)
	}
}
