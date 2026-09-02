//go:build integration

package bulkapi_test

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/panbotka/kukatko/internal/auth"
	"github.com/panbotka/kukatko/internal/bulk"
	"github.com/panbotka/kukatko/internal/photos"
)

// seedPlacedPhoto inserts a photo that already knows where it was taken — the
// geotagged stray among a box of scans, whose coordinates a batch either
// overwrites or is asked to leave alone.
func (e *env) seedPlacedPhoto(t *testing.T, hash string, lat, lng float64) string {
	t.Helper()
	p, err := e.photos.Create(t.Context(), photos.Photo{
		FileHash: hash, FilePath: "2024/01/" + hash + ".jpg", FileName: hash + ".jpg",
		FileMime: "image/jpeg",
		Lat:      &lat, Lng: &lng, LocationSource: photos.LocationSourceExif,
	})
	if err != nil {
		t.Fatalf("seed placed photo %s: %v", hash, err)
	}
	return p.UID
}

// assertLocation fails unless the photo sits exactly on lat/lng with the given
// provenance.
func assertLocation(t *testing.T, ctx context.Context, env *env, uid string, lat, lng float64, source string) {
	t.Helper()
	photo, err := env.photos.GetByUID(ctx, uid)
	if err != nil {
		t.Fatalf("get %s: %v", uid, err)
	}
	if photo.Lat == nil || *photo.Lat != lat || photo.Lng == nil || *photo.Lng != lng {
		t.Errorf("%s location = (%v,%v), want (%v,%v)", uid, photo.Lat, photo.Lng, lat, lng)
	}
	if photo.LocationSource != source {
		t.Errorf("%s location_source = %q, want %q", uid, photo.LocationSource, source)
	}
}

// TestBulkLocation_overwritesEveryPhoto is the plain case: one pin dropped for a
// whole selection replaces every target's coordinates, whatever they were, and
// stamps them as the user's own decision — a picked location is never an
// estimate. Each moved photo owes a reverse geocode, so each is scheduled.
func TestBulkLocation_overwritesEveryPhoto(t *testing.T) {
	env := newEnv(t, 1000)
	editor, _ := env.login(t, "editor", auth.RoleEditor)
	ctx := t.Context()
	empty := env.seedPhoto(t, "loc-empty-1")
	placed := env.seedPlacedPhoto(t, "loc-placed-1", 50.1, 14.4)

	result := env.setLocation(t, editor, []string{empty, placed},
		map[string]any{"lat": 49.19522, "lng": 16.60796})

	if result.Counts.Updated != 2 || result.Counts.Skipped != 0 {
		t.Fatalf("counts = %+v, want updated=2 skipped=0", result.Counts)
	}
	assertLocation(t, ctx, env, empty, 49.19522, 16.60796, photos.LocationSourceManual)
	assertLocation(t, ctx, env, placed, 49.19522, 16.60796, photos.LocationSourceManual)
	if len(env.places.uids) != 2 {
		t.Errorf("geocodes scheduled for %v, want both moved photos", env.places.uids)
	}
}

// TestBulkLocation_onlyMissingKeepsExisting is the other half of the choice the
// dialog offers: fill in the photos that have no location and leave the ones that
// do exactly as they were — coordinates, provenance and all. A photo left alone
// is reported skipped rather than updated, so the summary the reader gets back
// says what really happened, and it is not handed to the geocoder.
func TestBulkLocation_onlyMissingKeepsExisting(t *testing.T) {
	env := newEnv(t, 1000)
	editor, _ := env.login(t, "editor", auth.RoleEditor)
	ctx := t.Context()
	empty := env.seedPhoto(t, "loc-empty-2")
	placed := env.seedPlacedPhoto(t, "loc-placed-2", 50.1, 14.4)

	result := env.setLocation(t, editor, []string{empty, placed},
		map[string]any{"lat": 49.19522, "lng": 16.60796, "only_missing": true})

	if result.Counts.Updated != 1 || result.Counts.Skipped != 1 {
		t.Fatalf("counts = %+v, want updated=1 skipped=1", result.Counts)
	}
	assertLocation(t, ctx, env, empty, 49.19522, 16.60796, photos.LocationSourceManual)
	assertLocation(t, ctx, env, placed, 50.1, 14.4, photos.LocationSourceExif)
	if len(env.places.uids) != 1 || env.places.uids[0] != empty {
		t.Errorf("geocodes scheduled for %v, want only the filled photo [%s]", env.places.uids, empty)
	}
}

// TestBulkLocation_resendingTheSameCoordinateSchedulesNothing guards the metered
// half of the feature: every reverse geocode spends a mapy.com credit, so a batch
// that restates a coordinate a photo already had must not buy the same answer
// twice.
func TestBulkLocation_resendingTheSameCoordinateSchedulesNothing(t *testing.T) {
	env := newEnv(t, 1000)
	editor, _ := env.login(t, "editor", auth.RoleEditor)
	placed := env.seedPlacedPhoto(t, "loc-placed-3", 49.19522, 16.60796)

	result := env.setLocation(t, editor, []string{placed},
		map[string]any{"lat": 49.19522, "lng": 16.60796})

	if result.Counts.Updated != 1 {
		t.Fatalf("counts = %+v, want updated=1", result.Counts)
	}
	if len(env.places.uids) != 0 {
		t.Errorf("geocodes scheduled for %v, want none — the photo never moved", env.places.uids)
	}
}

// TestBulkLocation_rejectsCoordinatesOffTheGlobe verifies the contract's bounds:
// a latitude or longitude outside the real ones is a 400, not a row nobody can
// draw on a map.
func TestBulkLocation_rejectsCoordinatesOffTheGlobe(t *testing.T) {
	env := newEnv(t, 1000)
	editor, _ := env.login(t, "editor", auth.RoleEditor)
	uid := env.seedPhoto(t, "loc-bounds-1")

	for _, coords := range []map[string]any{
		{"lat": 91.0, "lng": 16.6},
		{"lat": 49.1, "lng": -181.0},
	} {
		body, _ := json.Marshal(map[string]any{
			"photo_uids": []string{uid},
			"operations": map[string]any{"set_location": coords},
		})
		resp := env.mustDo(t, editor, http.MethodPost, "/api/v1/photos/bulk", body)
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("set_location %v status = %d, want 400", coords, resp.StatusCode)
		}
	}
}

// setLocation posts a set-location batch and returns its result, failing the test
// on anything but a 200.
func (e *env) setLocation(
	t *testing.T, client *http.Client, uids []string, location map[string]any,
) bulk.Result {
	t.Helper()
	body, _ := json.Marshal(map[string]any{
		"photo_uids": uids,
		"operations": map[string]any{"set_location": location},
	})
	resp := e.mustDo(t, client, http.MethodPost, "/api/v1/photos/bulk", body)
	if resp.StatusCode != http.StatusOK {
		_ = resp.Body.Close()
		t.Fatalf("bulk status = %d, want 200", resp.StatusCode)
	}
	var result bulk.Result
	decodeBody(t, resp, &result)
	return result
}

// TestBulkLocationSummary_countsPlacedPhotos covers what the confirmation says
// before anything is written: how many of the selected photos already have a
// location. A UID repeated in the selection is one photo and a UID of a photo
// that is gone is none, so the total the dialog quotes is the batch the apply
// would really see.
func TestBulkLocationSummary_countsPlacedPhotos(t *testing.T) {
	env := newEnv(t, 1000)
	editor, _ := env.login(t, "editor", auth.RoleEditor)
	empty := env.seedPhoto(t, "sum-empty-1")
	placed := env.seedPlacedPhoto(t, "sum-placed-1", 50.1, 14.4)

	body, _ := json.Marshal(map[string]any{
		"photo_uids": []string{empty, placed, placed, "phMISSING0000000000000000000000"},
	})
	resp := env.mustDo(t, editor, http.MethodPost, "/api/v1/photos/bulk/location-summary", body)
	if resp.StatusCode != http.StatusOK {
		_ = resp.Body.Close()
		t.Fatalf("summary status = %d, want 200", resp.StatusCode)
	}
	var summary bulk.LocationSummary
	decodeBody(t, resp, &summary)
	if summary.Total != 2 || summary.WithLocation != 1 {
		t.Errorf("summary = %+v, want total=2 with_location=1", summary)
	}
}

// TestBulkLocationSummary_roleEnforcement verifies the preview is guarded like
// the write it precedes: a viewer never sees it, because a viewer can never act
// on it.
func TestBulkLocationSummary_roleEnforcement(t *testing.T) {
	env := newEnv(t, 1000)
	viewer, _ := env.login(t, "viewer", auth.RoleViewer)
	uid := env.seedPhoto(t, "sum-view-1")

	body, _ := json.Marshal(map[string]any{"photo_uids": []string{uid}})
	resp := env.mustDo(t, viewer, http.MethodPost, "/api/v1/photos/bulk/location-summary", body)
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("viewer summary status = %d, want 403", resp.StatusCode)
	}
}
