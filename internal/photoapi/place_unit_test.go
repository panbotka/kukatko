package photoapi

import (
	"context"
	"errors"
	"testing"

	"github.com/panbotka/kukatko/internal/photos"
	"github.com/panbotka/kukatko/internal/places"
)

// fakePlaceResolver is a controllable PlaceResolver: it returns a fixed place (or
// error) and counts how often it was asked, so a test can assert the detail
// endpoint reads the cache exactly once and never more.
type fakePlaceResolver struct {
	place places.Place
	err   error
	calls int
}

// GetPlace records the call and returns the fake's configured answer.
func (f *fakePlaceResolver) GetPlace(_ context.Context, _ string) (places.Place, error) {
	f.calls++
	return f.place, f.err
}

// TestResolvePlace covers the four ways the detail response ends up without a
// place — no resolver, no cached row, a lookup failure and the "processed, no
// place" marker — and the one way it carries one.
func TestResolvePlace(t *testing.T) {
	full := places.Place{
		PhotoUID: "ph_1", Country: "Česko", Region: "Jihomoravský kraj",
		City: "Brno", PlaceName: "Špilberk",
	}

	tests := []struct {
		name     string
		resolver *fakePlaceResolver
		want     *placeRef
	}{
		{
			name:     "cached place",
			resolver: &fakePlaceResolver{place: full},
			want: &placeRef{
				Country: "Česko", Region: "Jihomoravský kraj",
				City: "Brno", PlaceName: "Špilberk",
			},
		},
		{
			name:     "not geocoded yet",
			resolver: &fakePlaceResolver{err: places.ErrPlaceNotFound},
			want:     nil,
		},
		{
			name:     "lookup failure never fails the detail",
			resolver: &fakePlaceResolver{err: errors.New("db is down")},
			want:     nil,
		},
		{
			// The places job writes an all-empty row for a photo it cannot geocode;
			// it means "processed", not "here is a place".
			name:     "processed marker row",
			resolver: &fakePlaceResolver{place: places.Place{PhotoUID: "ph_1"}},
			want:     nil,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			api := &API{places: tc.resolver}
			got := api.resolvePlace(t.Context(), "ph_1")
			assertPlaceRef(t, got, tc.want)
			if tc.resolver.calls != 1 {
				t.Errorf("resolver calls = %d, want exactly 1", tc.resolver.calls)
			}
		})
	}

	t.Run("no resolver wired", func(t *testing.T) {
		api := &API{}
		if got := api.resolvePlace(t.Context(), "ph_1"); got != nil {
			t.Errorf("resolvePlace() = %+v, want nil without a resolver", got)
		}
	})
}

// assertPlaceRef fails the test when got does not match want, comparing nil-ness
// first so a nil pointer never gets dereferenced.
func assertPlaceRef(t *testing.T, got, want *placeRef) {
	t.Helper()
	if want == nil {
		if got != nil {
			t.Errorf("resolvePlace() = %+v, want nil", got)
		}
		return
	}
	if got == nil {
		t.Fatalf("resolvePlace() = nil, want %+v", want)
	}
	if *got != *want {
		t.Errorf("resolvePlace() = %+v, want %+v", got, want)
	}
}

// fakePlacesEnqueuer is a controllable PlacesEnqueuer: it records the photo UIDs
// it was asked to geocode and can fail, so a test can assert both that the
// scheduling happens and that a queue failure never escapes.
type fakePlacesEnqueuer struct {
	uids []string
	err  error
}

// EnqueuePlaces records the request and returns the fake's configured error.
func (f *fakePlacesEnqueuer) EnqueuePlaces(_ context.Context, photoUID string) error {
	f.uids = append(f.uids, photoUID)
	return f.err
}

// TestCoordinateMoved covers what counts as "the photo is somewhere else now" —
// the question that decides whether an edit spends a mapy.com credit on a fresh
// reverse geocode.
func TestCoordinateMoved(t *testing.T) {
	t.Parallel()

	brnoLat, brnoLng := 49.19522, 16.60796
	praLat := 50.08804

	tests := []struct {
		name          string
		before, after photos.Photo
		want          bool
	}{
		{
			name:   "unchanged coordinate",
			before: photos.Photo{Lat: &brnoLat, Lng: &brnoLng},
			after:  photos.Photo{Lat: &brnoLat, Lng: &brnoLng},
			want:   false,
		},
		{
			name:   "no coordinate before or after",
			before: photos.Photo{},
			after:  photos.Photo{},
			want:   false,
		},
		{
			name:   "location picked on the map",
			before: photos.Photo{},
			after:  photos.Photo{Lat: &brnoLat, Lng: &brnoLng},
			want:   true,
		},
		{
			name:   "location removed",
			before: photos.Photo{Lat: &brnoLat, Lng: &brnoLng},
			after:  photos.Photo{},
			want:   true,
		},
		{
			name:   "pin dragged north",
			before: photos.Photo{Lat: &brnoLat, Lng: &brnoLng},
			after:  photos.Photo{Lat: &praLat, Lng: &brnoLng},
			want:   true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := coordinateMoved(tc.before, tc.after); got != tc.want {
				t.Errorf("coordinateMoved() = %v, want %v", got, tc.want)
			}
		})
	}
}

// TestEnqueueGeocode verifies the scheduling helper: it asks the queue once, is
// a no-op without one, and swallows a queue failure — the coordinate is already
// saved and a stale cached place is not worth failing the edit over.
func TestEnqueueGeocode(t *testing.T) {
	t.Parallel()

	enqueuer := &fakePlacesEnqueuer{}
	api := &API{geocodes: enqueuer}
	api.enqueueGeocode(t.Context(), "ph_1")
	if len(enqueuer.uids) != 1 || enqueuer.uids[0] != "ph_1" {
		t.Errorf("enqueued %v, want [ph_1]", enqueuer.uids)
	}

	unwired := &API{}
	unwired.enqueueGeocode(t.Context(), "ph_1") // must not panic

	failing := &fakePlacesEnqueuer{err: errors.New("queue is down")}
	api = &API{geocodes: failing}
	api.enqueueGeocode(t.Context(), "ph_1") // must not panic or propagate
	if len(failing.uids) != 1 {
		t.Errorf("enqueued %v, want one attempt", failing.uids)
	}
}
