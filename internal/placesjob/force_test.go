package placesjob

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/panbotka/kukatko/internal/jobs"
	"github.com/panbotka/kukatko/internal/mapy"
	"github.com/panbotka/kukatko/internal/photos"
	"github.com/panbotka/kukatko/internal/places"
	"github.com/panbotka/kukatko/internal/worker"
)

// forcedFixture wires a service over a photo already geocoded for its current
// coordinates — the state that tells the repair and the rebuild apart.
func forcedFixture() (*Service, *fakePlaces, *fakeGeocoder) {
	photo := photos.Photo{UID: "ph1", Lat: new(50.0), Lng: new(14.0)}
	pho := &fakePhotos{byUID: map[string]photos.Photo{"ph1": photo}}
	pl := newFakePlaces()
	pl.saved["ph1"] = places.Place{PhotoUID: "ph1", City: "Stale", Lat: new(50.0), Lng: new(14.0)}
	geo := &fakeGeocoder{result: czGeo()}
	return newService(pho, pl, geo, &fakeEnqueuer{}, nil), pl, geo
}

// TestForceGeocode_recomputesWhereGeocodeSkips is the whole point of the forced
// path: on a photo already geocoded for these very coordinates the repair calls
// mapy.com not at all, while the rebuild asks again and replaces the cached place.
func TestForceGeocode_recomputesWhereGeocodeSkips(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		run       func(*Service) error
		wantCalls int
		wantCity  string
	}{
		{
			name:      "repair skips a coordinate already resolved",
			run:       func(s *Service) error { return s.Geocode(context.Background(), "ph1") },
			wantCalls: 0,
			wantCity:  "Stale",
		},
		{
			name:      "rebuild asks again and replaces the place",
			run:       func(s *Service) error { return s.ForceGeocode(context.Background(), "ph1") },
			wantCalls: 1,
			wantCity:  "Praha",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			svc, pl, geo := forcedFixture()
			if err := tt.run(svc); err != nil {
				t.Fatalf("run: %v", err)
			}
			if geo.calls != tt.wantCalls {
				t.Errorf("geocoder calls = %d, want %d", geo.calls, tt.wantCalls)
			}
			if got := pl.saved["ph1"].City; got != tt.wantCity {
				t.Errorf("cached city = %q, want %q", got, tt.wantCity)
			}
		})
	}
}

// TestHandle_forcePayloadRegeocodes proves the queued job honours the payload's
// force flag, which is what lets the forced enqueue keep dedup keyed on type +
// photo uid rather than needing a job type of its own.
func TestHandle_forcePayloadRegeocodes(t *testing.T) {
	t.Parallel()

	for _, force := range []bool{false, true} {
		name := "plain payload skips"
		if force {
			name = "forced payload re-geocodes"
		}
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			svc, _, geo := forcedFixture()
			payload, err := json.Marshal(map[string]any{"photo_uid": "ph1", "force": force})
			if err != nil {
				t.Fatalf("marshal payload: %v", err)
			}
			if err := svc.Handle(context.Background(), jobs.Job{
				Type: jobs.TypePlaces, Payload: payload,
			}); err != nil {
				t.Fatalf("Handle: %v", err)
			}
			want := 0
			if force {
				want = 1
			}
			if geo.calls != want {
				t.Errorf("geocoder calls = %d, want %d", geo.calls, want)
			}
		})
	}
}

// TestForceGeocode_offlineDefers confirms the rebuild answers an unreachable
// geocoder the way the repair does — a worker deferral — so the on-demand caller
// can queue the work instead of failing, and the cached place survives untouched.
func TestForceGeocode_offlineDefers(t *testing.T) {
	t.Parallel()

	svc, pl, geo := forcedFixture()
	geo.result, geo.err = nil, mapy.ErrUnavailable

	if err := svc.ForceGeocode(context.Background(), "ph1"); !worker.IsDeferral(err) {
		t.Fatalf("ForceGeocode with mapy.com down = %v, want a worker deferral", err)
	}
	if got := pl.saved["ph1"].City; got != "Stale" {
		t.Errorf("cached city = %q, want the previous %q left untouched", got, "Stale")
	}
}
