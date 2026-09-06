//go:build integration

package ingest_test

import (
	"bytes"
	"testing"

	"github.com/panbotka/kukatko/internal/config"
	"github.com/panbotka/kukatko/internal/ingest"
	"github.com/panbotka/kukatko/internal/sidecar"
)

// gpsSidecar returns the metadata an export would have written beside a photo
// taken at Veselice, which is how these tests give an upload a GPS fix without
// having to craft EXIF bytes.
func gpsSidecar() *sidecar.Metadata {
	return &sidecar.Metadata{Lat: new(49.3717), Lng: new(16.7204)}
}

// ingestWithSidecar runs one in-memory file through the pipeline with the given
// sidecar metadata folded in.
func (e *testEnv) ingestWithSidecar(t *testing.T, data []byte, name string, sc *sidecar.Metadata) ingest.FileResult {
	t.Helper()
	return e.svc.IngestFile(t.Context(), bytes.NewReader(data), ingest.Request{
		Filename: name, UploadedBy: e.uploader, Sidecar: sc,
	})
}

// TestIngest_enqueuesPlacesForGeotaggedUpload proves the upload pipeline
// schedules the reverse geocode itself: a photo that arrives with coordinates
// gets a `places` job, so its place fills in on its own instead of waiting for
// somebody to run a backfill. A photo with no coordinates gets none — there is
// nothing to look up, and every lookup costs a metered mapy.com credit.
func TestIngest_enqueuesPlacesForGeotaggedUpload(t *testing.T) {
	env := newEnv(t, config.DuplicateConfig{})
	ctx := t.Context()

	geotagged := env.ingestWithSidecar(t, jpegBytes(t, 200, 50, 50, 90), "beach.jpg", gpsSidecar())
	if geotagged.Outcome != ingest.OutcomeCreated {
		t.Fatalf("geotagged result = %+v, want created", geotagged)
	}
	plain := env.ingest(ctx, jpegBytes(t, 50, 200, 50, 90), "indoors.jpg")
	if plain.Outcome != ingest.OutcomeCreated {
		t.Fatalf("plain result = %+v, want created", plain)
	}

	// The coordinates really did land on the photo — otherwise the assertion
	// below would pass for the wrong reason.
	photo, err := env.store.GetByUID(ctx, geotagged.PhotoUID)
	if err != nil {
		t.Fatalf("GetByUID: %v", err)
	}
	if photo.Lat == nil || photo.Lng == nil {
		t.Fatalf("geotagged upload has lat=%v lng=%v, want a coordinate", photo.Lat, photo.Lng)
	}

	if !env.places.enqueued(geotagged.PhotoUID) {
		t.Errorf("no places job for the geotagged upload %s", geotagged.PhotoUID)
	}
	if env.places.enqueued(plain.PhotoUID) {
		t.Errorf("places job scheduled for %s, which carries no coordinates", plain.PhotoUID)
	}
	if got := env.places.count(); got != 1 {
		t.Errorf("places jobs = %d, want exactly 1", got)
	}
}

// TestIngest_noPlacesWithoutGeocoding proves an instance with no mapy.com key
// schedules no `places` job even for a photo that carries coordinates: with
// geocoding off no `places` handler is registered, so such a job would sit in the
// queue forever. The upload itself must still succeed.
func TestIngest_noPlacesWithoutGeocoding(t *testing.T) {
	env := newEnvWithPlaces(t, config.DuplicateConfig{}, nil)

	res := env.ingestWithSidecar(t, jpegBytes(t, 200, 50, 50, 90), "beach.jpg", gpsSidecar())
	if res.Outcome != ingest.OutcomeCreated {
		t.Fatalf("result = %+v, want created", res)
	}
	if len(res.Warnings) != 0 {
		t.Errorf("warnings = %+v, want none", res.Warnings)
	}
	photo, err := env.store.GetByUID(t.Context(), res.PhotoUID)
	if err != nil {
		t.Fatalf("GetByUID: %v", err)
	}
	if photo.Lat == nil || photo.Lng == nil {
		t.Fatalf("upload has lat=%v lng=%v, want a coordinate", photo.Lat, photo.Lng)
	}
	// The embedding job is the control: post-ingest scheduling ran, it simply had
	// no reverse geocode to schedule.
	if !env.enq.enqueuedEmbed(res.PhotoUID) {
		t.Error("no image_embed job scheduled, so the enqueue step did not run at all")
	}
}
