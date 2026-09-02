//go:build integration

package photoapi_test

import (
	"encoding/json"
	"testing"

	"github.com/panbotka/kukatko/internal/auth"
	"github.com/panbotka/kukatko/internal/jobs"
	"github.com/panbotka/kukatko/internal/photos"
)

// veseliLat / veseliLng are the coordinates of Veselí nad Moravou, the town the
// scanned-photo case is named after: you know where the photo was taken, the file
// does not.
const (
	veseliLat = 48.95363
	veseliLng = 17.37649
)

// TestUpdateMetadata_pickLocation covers what setting a photo's location on the
// map has to do beyond writing two numbers: the coordinates are stored, the
// provenance says a person chose them (so nothing presents them as an estimate,
// and no backfill overwrites them), and both pieces of derived work — the reverse
// geocode that keeps the places hierarchy honest and the sidecar rewrite that
// keeps the on-disk catalogue current — are scheduled.
func TestUpdateMetadata_pickLocation(t *testing.T) {
	env := newEnv(t)
	editor, _ := env.login(t, "editor", auth.RoleEditor)

	seeded := env.seedPhoto(t, photos.Photo{
		Title: "Svatba u kostela", TakenAtSource: "manual",
	}, "pick-location.jpg", 12, 34, 56)
	url := env.server.URL + "/api/v1/photos/" + seeded.UID
	if seeded.Lat != nil || seeded.Lng != nil {
		t.Fatalf("seeded photo already has a location: %+v", seeded)
	}

	body := []byte(`{"lat":48.95363,"lng":17.37649}`)
	patched := patchPhoto(t, editor, url, body)
	if patched.Lat == nil || patched.Lng == nil {
		t.Fatalf("coordinates not stored: %+v", patched)
	}
	if *patched.Lat != veseliLat || *patched.Lng != veseliLng {
		t.Errorf("coordinates = %v,%v, want %v,%v", *patched.Lat, *patched.Lng, veseliLat, veseliLng)
	}
	// A picked location is the user's decision, not a guess: it must never come
	// back marked as an estimate, which is what puts the dashed pin and the
	// "accept or discard" banner on the detail page.
	if patched.LocationSource != photos.LocationSourceManual {
		t.Errorf("location_source = %q, want %q", patched.LocationSource, photos.LocationSourceManual)
	}

	assertQueuedForPhoto(t, env, seeded.UID, jobs.TypePlaces)
	assertQueuedForPhoto(t, env, seeded.UID, jobs.TypeSidecar)
}

// TestUpdateMetadata_pickLocationOverEstimate verifies a manual pick overrules an
// estimated location outright: the coordinates are the ones that were picked and
// the provenance stops saying "estimate", so the photo is no longer offered for
// accepting or discarding.
func TestUpdateMetadata_pickLocationOverEstimate(t *testing.T) {
	env := newEnv(t)
	editor, _ := env.login(t, "editor", auth.RoleEditor)

	guessLat, guessLng := 50.08804, 14.42076
	seeded := env.seedPhoto(t, photos.Photo{
		Title: "Odhad", TakenAtSource: "manual",
		Lat: &guessLat, Lng: &guessLng, LocationSource: photos.LocationSourceEstimate,
	}, "estimate-location.jpg", 21, 43, 65)
	url := env.server.URL + "/api/v1/photos/" + seeded.UID

	patched := patchPhoto(t, editor, url, []byte(`{"lat":48.95363,"lng":17.37649}`))
	if patched.Lat == nil || *patched.Lat != veseliLat {
		t.Errorf("lat = %v, want the picked %v", patched.Lat, veseliLat)
	}
	if patched.LocationSource != photos.LocationSourceManual {
		t.Errorf("location_source = %q, want %q", patched.LocationSource, photos.LocationSourceManual)
	}
	assertQueuedForPhoto(t, env, seeded.UID, jobs.TypePlaces)
}

// TestUpdateMetadata_removeLocation verifies removing a location clears the
// coordinates and still schedules the geocode: the `places` job answers a photo
// with no coordinates by writing its empty processed marker, which is what takes
// the photo back out of the places hierarchy.
func TestUpdateMetadata_removeLocation(t *testing.T) {
	env := newEnv(t)
	editor, _ := env.login(t, "editor", auth.RoleEditor)

	lat, lng := veseliLat, veseliLng
	seeded := env.seedPhoto(t, photos.Photo{
		Title: "Ke smazání", TakenAtSource: "manual", Lat: &lat, Lng: &lng,
		LocationSource: photos.LocationSourceManual,
	}, "remove-location.jpg", 31, 41, 51)
	url := env.server.URL + "/api/v1/photos/" + seeded.UID

	patched := patchPhoto(t, editor, url, []byte(`{"lat":null,"lng":null}`))
	if patched.Lat != nil || patched.Lng != nil {
		t.Errorf("location not cleared: lat=%v lng=%v", patched.Lat, patched.Lng)
	}
	assertQueuedForPhoto(t, env, seeded.UID, jobs.TypePlaces)
}

// TestUpdateMetadata_untouchedLocationSchedulesNoGeocode pins the condition on
// the geocode: an edit that does not move the photo must not schedule one. Every
// geocode costs a metered mapy.com credit, so retitling a photo may not spend one.
func TestUpdateMetadata_untouchedLocationSchedulesNoGeocode(t *testing.T) {
	env := newEnv(t)
	editor, _ := env.login(t, "editor", auth.RoleEditor)

	lat, lng := veseliLat, veseliLng
	seeded := env.seedPhoto(t, photos.Photo{
		Title: "Beze změny", TakenAtSource: "manual", Lat: &lat, Lng: &lng,
		LocationSource: photos.LocationSourceManual,
	}, "same-location.jpg", 61, 71, 81)
	url := env.server.URL + "/api/v1/photos/" + seeded.UID

	// A title-only edit, and then a PATCH resending the coordinates the photo
	// already has: neither is a move, so neither is worth a credit.
	patchPhoto(t, editor, url, []byte(`{"title":"Jiný název"}`))
	patchPhoto(t, editor, url, []byte(`{"lat":48.95363,"lng":17.37649}`))

	if queuedForPhoto(t, env, seeded.UID, jobs.TypePlaces) {
		t.Error("a geocode was scheduled for an edit that did not move the photo")
	}
	// The sidecar, unlike the geocode, is free and follows every edit.
	assertQueuedForPhoto(t, env, seeded.UID, jobs.TypeSidecar)
}

// assertQueuedForPhoto fails the test unless a job of jobType is waiting for the
// photo.
func assertQueuedForPhoto(t *testing.T, e *env, photoUID, jobType string) {
	t.Helper()
	if !queuedForPhoto(t, e, photoUID, jobType) {
		t.Errorf("no %s job scheduled for %s", jobType, photoUID)
	}
}

// queuedForPhoto reports whether an unfinished job of jobType exists for the
// photo.
func queuedForPhoto(t *testing.T, e *env, photoUID, jobType string) bool {
	t.Helper()
	all, err := e.jobs.List(t.Context(), jobs.ListOptions{Limit: 100})
	if err != nil {
		t.Fatalf("listing jobs: %v", err)
	}
	for _, job := range all {
		if job.Type != jobType {
			continue
		}
		var payload struct {
			PhotoUID string `json:"photo_uid"`
		}
		if err := json.Unmarshal(job.Payload, &payload); err != nil {
			t.Fatalf("decode %s payload %q: %v", jobType, job.Payload, err)
		}
		if payload.PhotoUID == photoUID {
			return true
		}
	}
	return false
}
