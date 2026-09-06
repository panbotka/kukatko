//go:build integration

package maintenance_test

import (
	"context"
	"testing"

	"github.com/panbotka/kukatko/internal/audit"
	"github.com/panbotka/kukatko/internal/jobs"
	"github.com/panbotka/kukatko/internal/maintenance"
	"github.com/panbotka/kukatko/internal/places"
)

// placesJobs returns how many unfinished `places` jobs are queued for uid.
func (h *harness) placesJobs(t *testing.T, uid string) int {
	t.Helper()
	unfinished, err := h.jobs.UnfinishedForPhoto(context.Background(), uid)
	if err != nil {
		t.Fatalf("UnfinishedForPhoto(%s): %v", uid, err)
	}
	n := 0
	for _, job := range unfinished {
		if job.Type == jobs.TypePlaces {
			n++
		}
	}
	return n
}

// TestScanAndRepair_places drives the maintenance option end to end over a real
// database: the scan counts exactly the live photos that carry coordinates and
// have no cached place, and the repair enqueues a `places` job for those and
// nothing else. A photo without coordinates and a photo already geocoded are both
// left alone — the first can never get a place, the second already has one, and
// re-asking mapy.com would cost a credit for an answer it already has.
func TestScanAndRepair_places(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()

	pending := h.storeRealPhotoAt(t, "pending", 0x11, new(49.3717), new(16.7204))
	geocoded := h.storeRealPhotoAt(t, "geocoded", 0x22, new(50.0755), new(14.4378))
	noGPS := h.storeRealPhoto(t, "nogps", 0x33)
	if _, err := h.places.SavePlace(ctx, places.Place{
		PhotoUID: geocoded.UID, Country: "Česko", City: "Praha",
	}); err != nil {
		t.Fatalf("SavePlace: %v", err)
	}

	report, err := h.svc.Scan(ctx)
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if report.MissingPlaces.Count != 1 {
		t.Fatalf("MissingPlaces.Count = %d (%v), want 1",
			report.MissingPlaces.Count, report.MissingPlaces.Samples)
	}
	if len(report.MissingPlaces.Samples) != 1 || report.MissingPlaces.Samples[0] != pending.UID {
		t.Errorf("MissingPlaces.Samples = %v, want [%s]", report.MissingPlaces.Samples, pending.UID)
	}

	res, err := h.svc.Repair(ctx, maintenance.RepairOptions{Places: true}, audit.Meta{})
	if err != nil {
		t.Fatalf("Repair: %v", err)
	}
	if res.PlacesEnqueued != 1 {
		t.Errorf("PlacesEnqueued = %d, want 1", res.PlacesEnqueued)
	}
	if got := h.placesJobs(t, pending.UID); got != 1 {
		t.Errorf("places jobs for the pending photo = %d, want 1", got)
	}
	for _, uid := range []string{geocoded.UID, noGPS.UID} {
		if got := h.placesJobs(t, uid); got != 0 {
			t.Errorf("places jobs for %s = %d, want 0", uid, got)
		}
	}

	// Re-running is a no-op rather than a second job: the enqueuer dedupes per
	// photo, so the option is safe to press twice.
	if _, err := h.svc.Repair(ctx, maintenance.RepairOptions{Places: true}, audit.Meta{}); err != nil {
		t.Fatalf("second Repair: %v", err)
	}
	if got := h.placesJobs(t, pending.UID); got != 1 {
		t.Errorf("after a second run there are %d places jobs, want 1", got)
	}
}
