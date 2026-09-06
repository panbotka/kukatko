package maintenance

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/panbotka/kukatko/internal/audit"
	"github.com/panbotka/kukatko/internal/photos"
)

// impossibleFixture is the catalogue a scan sees when two photos carry a capture
// date no photograph can have: the Facebook download that dates itself to the
// year 9009 and a row from before photography existed.
func impossibleFixture() []photos.ImpossibleDate {
	return []photos.ImpossibleDate{
		{
			UID:           "p-old",
			FileName:      "17000101_scan.jpg",
			TakenAt:       time.Date(1700, 1, 1, 0, 0, 0, 0, time.UTC),
			TakenAtSource: "filename",
		},
		{
			UID:           "p-fb",
			FileName:      "90090310_638783213372240_7483353598378639360_n.jpg",
			TakenAt:       time.Date(9009, 3, 10, 0, 0, 0, 0, time.UTC),
			TakenAtSource: "filename",
		},
	}
}

// impossibleScenario builds a service whose catalogue reports the two impossibly
// dated photos, returning the fakes so a test can see exactly what was cleared
// and what was scheduled afterwards.
func impossibleScenario() (*Service, *fakePhotos, *fakeEnqueuer) {
	ph := &fakePhotos{impossible: impossibleFixture()}
	enq := &fakeEnqueuer{}
	svc := New(Config{
		Photos:    ph,
		Vectors:   &fakeVectors{},
		Originals: fakeOriginals{present: map[string]bool{}},
		Store:     fakeStore{},
		Thumbs:    fakeThumbs{have: map[string]bool{}},
		Enqueuer:  enq,
		Embed:     &fakeBackfiller{},
		Faces:     &fakeFaceBackfiller{},
		FaceCache: &fakeFaceCache{},
		Sidecar:   enq,
	})
	return svc, ph, enq
}

// TestScanReportsImpossibleDates verifies the scan surfaces the impossibly dated
// photos as a finding — the dry run of the repair — and that they count against
// Clean.
func TestScanReportsImpossibleDates(t *testing.T) {
	t.Parallel()
	svc, _, _ := impossibleScenario()

	report, err := svc.Scan(context.Background())
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if report.ImpossibleDates.Count != 2 {
		t.Errorf("count = %d, want 2", report.ImpossibleDates.Count)
	}
	if got := strings.Join(report.ImpossibleDates.Samples, ","); got != "p-old,p-fb" {
		t.Errorf("samples = %v, want the two affected photos", got)
	}
	if report.Clean() {
		t.Error("Clean() = true, want false with impossible dates outstanding")
	}
}

// TestRepairImpossibleDatesIsOptIn verifies a repair that was not asked for it
// never withdraws a date: clearing catalogue metadata is a decision, not a side
// effect of regenerating thumbnails.
func TestRepairImpossibleDatesIsOptIn(t *testing.T) {
	t.Parallel()
	svc, ph, enq := impossibleScenario()

	res, err := svc.Repair(context.Background(), RepairOptions{Thumbnails: true}, audit.Meta{})
	if err != nil {
		t.Fatalf("Repair: %v", err)
	}
	if res.ImpossibleDatesCleared != 0 {
		t.Errorf("cleared = %d, want 0", res.ImpossibleDatesCleared)
	}
	if len(ph.cleared) != 0 || len(enq.sidecar) != 0 {
		t.Errorf("cleared %v and scheduled %v, want nothing", ph.cleared, enq.sidecar)
	}
}

// TestRepairImpossibleDatesClearsAndAudits verifies the selected repair withdraws
// every impossible date, stamps the acting maintainer's provenance onto an audit
// entry carrying what was discarded, and schedules the sidecar rewrite that
// carries the withdrawal out to storage.
func TestRepairImpossibleDatesClearsAndAudits(t *testing.T) {
	t.Parallel()
	svc, ph, enq := impossibleScenario()
	meta := audit.Meta{ActorUID: "us-admin", IP: "192.0.2.7", UserAgent: "kukatko-test"}

	res, err := svc.Repair(context.Background(), RepairOptions{ImpossibleDates: true}, meta)
	if err != nil {
		t.Fatalf("Repair: %v", err)
	}
	if res.ImpossibleDatesCleared != 2 {
		t.Errorf("cleared = %d, want 2", res.ImpossibleDatesCleared)
	}
	if got := strings.Join(ph.cleared, ","); got != "p-old,p-fb" {
		t.Errorf("cleared %v, want both affected photos", ph.cleared)
	}
	if got := strings.Join(enq.sidecar, ","); got != "p-old,p-fb" {
		t.Errorf("sidecar rewrites scheduled for %v, want both affected photos", enq.sidecar)
	}
	entry := ph.clearEntries[1]
	if entry.Action != audit.ActionPhotoDateClear {
		t.Errorf("action = %q, want %q", entry.Action, audit.ActionPhotoDateClear)
	}
	if entry.ActorUID != meta.ActorUID || entry.IP != meta.IP || entry.UserAgent != meta.UserAgent {
		t.Errorf("entry provenance = %+v, want the acting maintainer's", entry)
	}
	if entry.TargetType != "photos" || entry.TargetUID != "p-fb" {
		t.Errorf("target = %s/%s, want photos/p-fb", entry.TargetType, entry.TargetUID)
	}
	if got := entry.Details["taken_at"]; got != "9009-03-10T00:00:00Z" {
		t.Errorf("details taken_at = %v, want the discarded date", got)
	}
	if got := entry.Details["taken_at_source"]; got != "filename" {
		t.Errorf("details taken_at_source = %v, want filename", got)
	}
}

// TestRepairImpossibleDatesSkipsUnchanged verifies a photo whose date stopped
// being impossible between the scan and the repair is neither counted nor
// followed by a sidecar rewrite: the guard is in the write, and the result
// reports what happened rather than what was attempted.
func TestRepairImpossibleDatesSkipsUnchanged(t *testing.T) {
	t.Parallel()
	svc, ph, enq := impossibleScenario()
	ph.clearNoop = true

	res, err := svc.Repair(context.Background(), RepairOptions{ImpossibleDates: true}, audit.Meta{})
	if err != nil {
		t.Fatalf("Repair: %v", err)
	}
	if res.ImpossibleDatesCleared != 0 {
		t.Errorf("cleared = %d, want 0", res.ImpossibleDatesCleared)
	}
	if len(enq.sidecar) != 0 {
		t.Errorf("scheduled %v sidecar rewrites, want none", enq.sidecar)
	}
}

// TestRepairImpossibleDatesWithoutSidecarExport verifies the repair still
// withdraws the dates on an instance whose sidecar export is off: clearing is the
// repair, the sidecar rewrite only carries it out to storage, and a job no handler
// would drain must not be queued.
func TestRepairImpossibleDatesWithoutSidecarExport(t *testing.T) {
	t.Parallel()
	ph := &fakePhotos{impossible: impossibleFixture()}
	enq := &fakeEnqueuer{}
	svc := New(Config{
		Photos:    ph,
		Vectors:   &fakeVectors{},
		Originals: fakeOriginals{present: map[string]bool{}},
		Store:     fakeStore{},
		Thumbs:    fakeThumbs{have: map[string]bool{}},
		Enqueuer:  enq,
		Embed:     &fakeBackfiller{},
		Faces:     &fakeFaceBackfiller{},
		FaceCache: &fakeFaceCache{},
	})

	res, err := svc.Repair(context.Background(), RepairOptions{ImpossibleDates: true}, audit.Meta{})
	if err != nil {
		t.Fatalf("Repair: %v", err)
	}
	if res.ImpossibleDatesCleared != 2 {
		t.Errorf("cleared = %d, want 2", res.ImpossibleDatesCleared)
	}
	if len(enq.sidecar) != 0 {
		t.Errorf("scheduled %v sidecar rewrites with the export off, want none", enq.sidecar)
	}
}

// TestRepairOptionsAnyCoversImpossibleDates verifies the option alone is a
// request, so the API does not reject "clear the impossible dates" as an empty
// selection.
func TestRepairOptionsAnyCoversImpossibleDates(t *testing.T) {
	t.Parallel()
	if !(RepairOptions{ImpossibleDates: true}).Any() {
		t.Error("Any() = false for an impossible-dates-only selection, want true")
	}
}
