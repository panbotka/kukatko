//go:build integration

package maintenance_test

import (
	"context"
	"testing"
	"time"

	"github.com/panbotka/kukatko/internal/audit"
	"github.com/panbotka/kukatko/internal/auth"
	"github.com/panbotka/kukatko/internal/exif"
	"github.com/panbotka/kukatko/internal/jobs"
	"github.com/panbotka/kukatko/internal/maintenance"
	"github.com/panbotka/kukatko/internal/photos"
)

// facebookDownloadName is the file name that produced the defect this repair
// exists for: a Facebook asset id whose leading digits read as 9009-03-10.
const facebookDownloadName = "90090310_638783213372240_7483353598378639360_n.jpg"

// maintainerMeta inserts the maintainer account the audit rows point at (the
// actor is a foreign key into users) and returns the provenance a request from
// them would carry.
func (h *harness) maintainerMeta(t *testing.T) audit.Meta {
	t.Helper()
	const uid = "usimpossibledates0000000000"
	if err := auth.NewStore(h.db.Pool()).CreateUser(context.Background(), auth.User{
		UID:          uid,
		Username:     "impossible-dates",
		Email:        "impossible-dates@example.test",
		PasswordHash: "x",
		Role:         auth.RoleMaintainer,
	}); err != nil {
		t.Fatalf("creating the acting maintainer: %v", err)
	}
	return audit.Meta{ActorUID: uid, IP: "192.0.2.7", UserAgent: "kukatko-test"}
}

// dateAs stamps a capture date and its provenance onto an already-catalogued
// photo, writing the columns directly so a date the guard would refuse today can
// be planted — which is exactly the state the rows this repair finds are in.
func (h *harness) dateAs(t *testing.T, uid, fileName string, takenAt time.Time, source string) {
	t.Helper()
	_, err := h.db.Pool().Exec(context.Background(),
		`UPDATE photos SET file_name = $2, taken_at = $3, taken_at_source = $4 WHERE uid = $1`,
		uid, fileName, takenAt, source)
	if err != nil {
		t.Fatalf("dating %s: %v", uid, err)
	}
}

// takenAtOf reads a photo's capture date back from the catalogue.
func (h *harness) takenAtOf(t *testing.T, uid string) *time.Time {
	t.Helper()
	photo, err := h.photos.GetByUID(context.Background(), uid)
	if err != nil {
		t.Fatalf("GetByUID(%s): %v", uid, err)
	}
	return photo.TakenAt
}

// sidecarJobs returns how many unfinished `sidecar` jobs are queued for uid.
func (h *harness) sidecarJobs(t *testing.T, uid string) int {
	t.Helper()
	unfinished, err := h.jobs.UnfinishedForPhoto(context.Background(), uid)
	if err != nil {
		t.Fatalf("UnfinishedForPhoto(%s): %v", uid, err)
	}
	n := 0
	for _, job := range unfinished {
		if job.Type == jobs.TypeSidecar {
			n++
		}
	}
	return n
}

// TestScanAndRepair_impossibleDates drives the option end to end over a real
// database: the scan counts exactly the photos dated to a year no photograph can
// have been taken in, and the repair withdraws those dates — leaving the photo
// without one rather than inventing a replacement — while every plausibly dated
// photo, and every photo with no date at all, is left untouched.
func TestScanAndRepair_impossibleDates(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()

	facebook := h.storeRealPhoto(t, "facebook", 0x41)
	h.dateAs(t, facebook.UID, facebookDownloadName,
		time.Date(9009, 3, 10, 0, 0, 0, 0, time.UTC), "filename")
	predating := h.storeRealPhoto(t, "predating", 0x42)
	h.dateAs(t, predating.UID, "17000101_scan.jpg",
		time.Date(1700, 1, 1, 0, 0, 0, 0, time.UTC), "filename")
	real := h.storeRealPhoto(t, "real", 0x43)
	h.dateAs(t, real.UID, "IMG_20230115_143052.jpg",
		time.Date(2023, 1, 15, 14, 30, 52, 0, time.UTC), "exif")
	undated := h.storeRealPhoto(t, "undated", 0x44)

	report, err := h.svc.Scan(ctx)
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if report.ImpossibleDates.Count != 2 {
		t.Fatalf("ImpossibleDates.Count = %d (%v), want 2",
			report.ImpossibleDates.Count, report.ImpossibleDates.Samples)
	}
	if report.Clean() {
		t.Error("Clean() = true with two impossible dates outstanding, want false")
	}

	meta := h.maintainerMeta(t)
	res, err := h.svc.Repair(ctx, maintenance.RepairOptions{ImpossibleDates: true}, meta)
	if err != nil {
		t.Fatalf("Repair: %v", err)
	}
	if res.ImpossibleDatesCleared != 2 {
		t.Fatalf("ImpossibleDatesCleared = %d, want 2", res.ImpossibleDatesCleared)
	}
	for _, uid := range []string{facebook.UID, predating.UID} {
		if got := h.takenAtOf(t, uid); got != nil {
			t.Errorf("%s is still dated %v, want no date", uid, got)
		}
		if got := h.sidecarJobs(t, uid); got != 1 {
			t.Errorf("sidecar jobs for %s = %d, want 1", uid, got)
		}
	}
	if got := h.takenAtOf(t, real.UID); got == nil || !got.Equal(time.Date(2023, 1, 15, 14, 30, 52, 0, time.UTC)) {
		t.Errorf("the plausibly dated photo is now %v, want its own date", got)
	}
	if got := h.takenAtOf(t, undated.UID); got != nil {
		t.Errorf("the undated photo is now dated %v, want no date", got)
	}
	if got := h.sidecarJobs(t, real.UID); got != 0 {
		t.Errorf("sidecar jobs for the untouched photo = %d, want 0", got)
	}

	// The withdrawn date is preserved, not destroyed: the clear stays reversible
	// and the metadata panel can still say what the date used to be.
	photo, err := h.photos.GetByUID(ctx, facebook.UID)
	if err != nil {
		t.Fatalf("GetByUID: %v", err)
	}
	if photo.TakenAtSource != photos.TakenAtSourceUnknown {
		t.Errorf("taken_at_source = %q, want %q", photo.TakenAtSource, photos.TakenAtSourceUnknown)
	}
	if photo.TakenAtBeforeUnknown == nil || photo.TakenAtBeforeUnknown.UTC().Year() != 9009 {
		t.Errorf("preserved date = %v, want the discarded 9009 one", photo.TakenAtBeforeUnknown)
	}

	// A second scan reports nothing, and a second repair has nothing left to do.
	after, err := h.svc.Scan(ctx)
	if err != nil {
		t.Fatalf("second Scan: %v", err)
	}
	if after.ImpossibleDates.Count != 0 {
		t.Errorf("ImpossibleDates.Count after the repair = %d, want 0", after.ImpossibleDates.Count)
	}
	again, err := h.svc.Repair(ctx, maintenance.RepairOptions{ImpossibleDates: true}, meta)
	if err != nil {
		t.Fatalf("second Repair: %v", err)
	}
	if again.ImpossibleDatesCleared != 0 {
		t.Errorf("second run cleared %d dates, want 0", again.ImpossibleDatesCleared)
	}
}

// TestRepairImpossibleDates_audit verifies the withdrawal is recorded in the
// durable audit trail — one entry per cleared photo, carrying who asked and what
// was discarded — and that the entry is written in the mutation's transaction, so
// a photo left alone by the guard leaves no entry behind either.
func TestRepairImpossibleDates_audit(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	auditStore := audit.NewStore(h.db.Pool())

	facebook := h.storeRealPhoto(t, "facebook", 0x51)
	h.dateAs(t, facebook.UID, facebookDownloadName,
		time.Date(9009, 3, 10, 0, 0, 0, 0, time.UTC), "filename")

	meta := h.maintainerMeta(t)
	if _, err := h.svc.Repair(ctx, maintenance.RepairOptions{ImpossibleDates: true}, meta); err != nil {
		t.Fatalf("Repair: %v", err)
	}

	entries, err := auditStore.List(ctx, audit.Filter{Action: audit.ActionPhotoDateClear})
	if err != nil {
		t.Fatalf("audit List: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("audit entries = %d, want 1", len(entries))
	}
	entry := entries[0]
	if entry.ActorUID == nil || *entry.ActorUID != meta.ActorUID {
		t.Errorf("actor = %v, want %s", entry.ActorUID, meta.ActorUID)
	}
	if entry.TargetType != "photos" || entry.TargetUID == nil || *entry.TargetUID != facebook.UID {
		t.Errorf("target = %s/%v, want photos/%s", entry.TargetType, entry.TargetUID, facebook.UID)
	}
	if got := entry.Details["taken_at"]; got != "9009-03-10T00:00:00Z" {
		t.Errorf("details taken_at = %v, want the discarded date", got)
	}
	if got := entry.Details["file_name"]; got != facebookDownloadName {
		t.Errorf("details file_name = %v, want %s", got, facebookDownloadName)
	}

	// Nothing to clear the second time, so nothing to record: the entry belongs to
	// the write, not to the request.
	if _, err := h.svc.Repair(ctx, maintenance.RepairOptions{ImpossibleDates: true}, meta); err != nil {
		t.Fatalf("second Repair: %v", err)
	}
	after, err := auditStore.List(ctx, audit.Filter{Action: audit.ActionPhotoDateClear})
	if err != nil {
		t.Fatalf("second audit List: %v", err)
	}
	if len(after) != 1 {
		t.Errorf("audit entries after a no-op repair = %d, want 1", len(after))
	}
}

// TestImpossibleDateBoundsMatchTheGuard verifies the scan's predicate is the very
// rule internal/exif enforces on the way in: a date at either edge of the
// plausible range is left alone, one just outside it is found. The two must never
// drift apart — a date the importer would accept must not be reported as
// impossible, and vice versa.
func TestImpossibleDateBoundsMatchTheGuard(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()

	minYear, maxYear := exif.CaptureYearBounds()
	cases := []struct {
		name string
		year int
		want bool // want it reported as impossible
	}{
		{name: "just before photography", year: minYear - 1, want: true},
		{name: "first year of photography", year: minYear, want: false},
		{name: "last plausible year", year: maxYear, want: false},
		{name: "one year too far ahead", year: maxYear + 1, want: true},
	}
	seeded := make(map[string]bool, len(cases))
	for i, tc := range cases {
		photo := h.storeRealPhoto(t, tc.name, uint8(0x61+i))
		h.dateAs(t, photo.UID, tc.name+".jpg",
			time.Date(tc.year, 6, 1, 12, 0, 0, 0, time.UTC), "filename")
		seeded[photo.UID] = tc.want
	}

	report, err := h.svc.Scan(ctx)
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	found := make(map[string]bool, len(report.ImpossibleDates.Samples))
	for _, uid := range report.ImpossibleDates.Samples {
		found[uid] = true
	}
	for uid, want := range seeded {
		if found[uid] != want {
			t.Errorf("photo %s reported impossible = %v, want %v", uid, found[uid], want)
		}
	}
	if report.ImpossibleDates.Count != 2 {
		t.Errorf("Count = %d, want 2", report.ImpossibleDates.Count)
	}
}
