package maintenance

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/panbotka/kukatko/internal/audit"
	"github.com/panbotka/kukatko/internal/processing"
)

// fakeGaps is an in-memory ProcessingGaps: it reports pinned gaps and records the
// steps the repair schedules.
type fakeGaps struct {
	gaps   []processing.Gap
	err    error
	runErr map[string]error
	ran    []string
}

func (f *fakeGaps) Unscheduled(context.Context) ([]processing.Gap, error) {
	return f.gaps, f.err
}

func (f *fakeGaps) Run(_ context.Context, photoUID string, step processing.Step) (processing.Status, error) {
	key := photoUID + "/" + string(step)
	f.ran = append(f.ran, key)
	if err := f.runErr[key]; err != nil {
		return processing.Status{}, err
	}
	return processing.Status{Step: step, State: processing.StateQueued}, nil
}

// gapScenario builds an otherwise clean service whose processing collaborator is
// gaps (nil for an instance wired without one).
func gapScenario(gaps ProcessingGaps) *Service {
	return New(Config{
		Photos:     &fakePhotos{},
		Vectors:    &fakeVectors{},
		Originals:  fakeOriginals{present: map[string]bool{}},
		Store:      fakeStore{},
		Thumbs:     fakeThumbs{have: map[string]bool{}},
		Enqueuer:   &fakeEnqueuer{},
		Embed:      &fakeBackfiller{},
		Faces:      &fakeFaceBackfiller{},
		FaceCache:  &fakeFaceCache{},
		Processing: gaps,
	})
}

// interruptedUpload is the gaps an upload cut short after its original was
// stored leaves behind.
func interruptedUpload() []processing.Gap {
	return []processing.Gap{
		{PhotoUID: "ph4egajk", Step: processing.StepThumbnail},
		{PhotoUID: "ph4egajk", Step: processing.StepImageEmbed},
		{PhotoUID: "ph4egajk", Step: processing.StepFaceDetect},
	}
}

// TestScanReportsUnscheduledSteps verifies the gaps surface as a finding counted
// per (photo, step), sampled as <photo_uid>/<step>, and count against Clean.
func TestScanReportsUnscheduledSteps(t *testing.T) {
	t.Parallel()
	report, err := gapScenario(&fakeGaps{gaps: interruptedUpload()}).Scan(context.Background())
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	want := []string{"ph4egajk/thumbnail", "ph4egajk/image_embed", "ph4egajk/face_detect"}
	if report.UnscheduledSteps.Count != 3 || !slices.Equal(report.UnscheduledSteps.Samples, want) {
		t.Errorf("finding = %+v, want 3 with samples %v", report.UnscheduledSteps, want)
	}
	if report.Clean() {
		t.Error("a library with unscheduled steps reported clean")
	}
}

// TestScanUnscheduledSteps_unwiredAndFailing covers the two edges: no processing
// collaborator is an empty finding, a failing one fails the scan.
func TestScanUnscheduledSteps_unwiredAndFailing(t *testing.T) {
	t.Parallel()
	report, err := gapScenario(nil).Scan(context.Background())
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if report.UnscheduledSteps.Count != 0 || report.UnscheduledSteps.Samples == nil {
		t.Errorf("unwired finding = %+v, want empty with a non-nil sample list", report.UnscheduledSteps)
	}
	boom := errors.New("db down")
	if _, err := gapScenario(&fakeGaps{err: boom}).Scan(context.Background()); !errors.Is(err, boom) {
		t.Errorf("Scan err = %v, want the gap listing's failure", err)
	}
}

// TestRepairUnscheduledSteps verifies the repair schedules each gap once, skips a
// step that stopped applying, aborts on any other failure, and refuses without a
// processing collaborator.
func TestRepairUnscheduledSteps(t *testing.T) {
	t.Parallel()
	opts := RepairOptions{UnscheduledSteps: true}

	gaps := &fakeGaps{
		gaps:   interruptedUpload(),
		runErr: map[string]error{"ph4egajk/face_detect": processing.ErrStepNotApplicable},
	}
	res, err := gapScenario(gaps).Repair(context.Background(), opts, audit.Meta{})
	if err != nil {
		t.Fatalf("Repair: %v", err)
	}
	if res.StepsScheduled != 2 || len(gaps.ran) != 3 {
		t.Errorf("scheduled %d of %v, want 2 scheduled of 3 tried", res.StepsScheduled, gaps.ran)
	}

	boom := errors.New("queue down")
	failing := &fakeGaps{gaps: interruptedUpload(), runErr: map[string]error{"ph4egajk/thumbnail": boom}}
	if _, err := gapScenario(failing).Repair(context.Background(), opts, audit.Meta{}); !errors.Is(err, boom) {
		t.Errorf("Repair err = %v, want the enqueue failure", err)
	}

	if _, err := gapScenario(nil).Repair(context.Background(), opts, audit.Meta{}); !errors.Is(err, ErrProcessingUnavailable) {
		t.Errorf("unwired Repair err = %v, want ErrProcessingUnavailable", err)
	}

	untouched := &fakeGaps{gaps: interruptedUpload()}
	if _, err := gapScenario(untouched).Repair(context.Background(), RepairOptions{Thumbnails: true}, audit.Meta{}); err != nil {
		t.Fatalf("Repair without the flag: %v", err)
	}
	if len(untouched.ran) != 0 {
		t.Errorf("the repair ran without being selected: %v", untouched.ran)
	}
}

// TestUnscheduledSteps_leaveDeferredPlacesOut verifies a pending `places` step is
// neither counted nor scheduled: it is the reverse-geocode backlog missing_places
// and `repair --places` own, not a gap an interrupted upload left.
func TestUnscheduledSteps_leaveDeferredPlacesOut(t *testing.T) {
	t.Parallel()
	gaps := &fakeGaps{gaps: []processing.Gap{
		{PhotoUID: "ph4egajk", Step: processing.StepImageEmbed},
		{PhotoUID: "ph4egajk", Step: processing.StepPlaces},
		{PhotoUID: "ph9mxq2r", Step: processing.StepPlaces},
	}}
	svc := gapScenario(gaps)

	report, err := svc.Scan(context.Background())
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	want := []string{"ph4egajk/image_embed"}
	if report.UnscheduledSteps.Count != 1 || !slices.Equal(report.UnscheduledSteps.Samples, want) {
		t.Errorf("finding = %+v, want 1 with samples %v", report.UnscheduledSteps, want)
	}

	res, err := svc.Repair(context.Background(), RepairOptions{UnscheduledSteps: true}, audit.Meta{})
	if err != nil {
		t.Fatalf("Repair: %v", err)
	}
	if res.StepsScheduled != 1 || !slices.Equal(gaps.ran, want) {
		t.Errorf("scheduled %d: %v, want only %v", res.StepsScheduled, gaps.ran, want)
	}
}

// TestUnscheduledSteps_placesOnlyIsClean verifies a library whose only gaps are
// deferred places reports an empty finding, so it does not keep the scan dirty.
func TestUnscheduledSteps_placesOnlyIsClean(t *testing.T) {
	t.Parallel()
	gaps := &fakeGaps{gaps: []processing.Gap{{PhotoUID: "ph4egajk", Step: processing.StepPlaces}}}
	report, err := gapScenario(gaps).Scan(context.Background())
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if report.UnscheduledSteps.Count != 0 || len(report.UnscheduledSteps.Samples) != 0 {
		t.Errorf("finding = %+v, want empty", report.UnscheduledSteps)
	}
}
