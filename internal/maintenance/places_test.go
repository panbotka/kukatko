package maintenance

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/panbotka/kukatko/internal/audit"
)

// fakePlaceBackfiller reports a fixed reverse-geocode backlog and records that
// the backfill ran. Its BackfillPlaces returns the length of that backlog, which
// is what the real placesjob.Service does: one `places` job per listed uid.
type fakePlaceBackfiller struct {
	missing []string
	called  bool
	listErr error
}

func (f *fakePlaceBackfiller) MissingPlaces(context.Context) ([]string, error) {
	if f.listErr != nil {
		return nil, f.listErr
	}
	return slices.Clone(f.missing), nil
}

func (f *fakePlaceBackfiller) BackfillPlaces(context.Context) (int, error) {
	f.called = true
	return len(f.missing), nil
}

// placesScenario builds a Service whose only interesting collaborator is the
// place backfiller: everything else is empty, so the assertions are about the
// reverse-geocode backlog alone. A nil backfiller stands for an instance with no
// mapy.com key.
func placesScenario(places PlaceBackfiller) *Service {
	return New(Config{
		Photos:    &fakePhotos{},
		Vectors:   &fakeVectors{},
		Originals: fakeOriginals{present: map[string]bool{}},
		Store:     fakeStore{},
		Thumbs:    fakeThumbs{have: map[string]bool{}},
		Enqueuer:  &fakeEnqueuer{},
		Embed:     &fakeBackfiller{},
		Faces:     &fakeFaceBackfiller{},
		FaceCache: &fakeFaceCache{},
		Places:    places,
	})
}

// TestScan_missingPlaces asserts the scan counts the reverse-geocode backlog the
// backfill would schedule, and reports none at all when geocoding is
// unconfigured — nothing on such an instance could fill those places in, so
// counting them would leave the scan permanently dirty.
func TestScan_missingPlaces(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		places    PlaceBackfiller
		wantCount int
		wantClean bool
	}{
		"counts the backlog": {
			places:    &fakePlaceBackfiller{missing: []string{"p1", "p2", "p3"}},
			wantCount: 3,
			wantClean: false,
		},
		"nothing owed is clean": {
			places:    &fakePlaceBackfiller{},
			wantCount: 0,
			wantClean: true,
		},
		"geocoding unconfigured reports nothing": {
			places:    nil,
			wantCount: 0,
			wantClean: true,
		},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			report, err := placesScenario(tc.places).Scan(context.Background())
			if err != nil {
				t.Fatalf("Scan: %v", err)
			}
			if report.MissingPlaces.Count != tc.wantCount {
				t.Errorf("MissingPlaces.Count = %d, want %d", report.MissingPlaces.Count, tc.wantCount)
			}
			if len(report.MissingPlaces.Samples) != tc.wantCount {
				t.Errorf("MissingPlaces.Samples = %v, want %d uids",
					report.MissingPlaces.Samples, tc.wantCount)
			}
			if report.Clean() != tc.wantClean {
				t.Errorf("Clean() = %v, want %v", report.Clean(), tc.wantClean)
			}
		})
	}
}

// TestRepair_places asserts the option runs the backfill and reports how many
// geocodes it queued, and that leaving it unselected runs nothing — the whole
// point of an opt-in repair over metered work.
func TestRepair_places(t *testing.T) {
	t.Parallel()

	t.Run("selected schedules the backlog", func(t *testing.T) {
		t.Parallel()
		places := &fakePlaceBackfiller{missing: []string{"p1", "p2"}}
		res, err := placesScenario(places).Repair(context.Background(), RepairOptions{Places: true}, audit.Meta{})
		if err != nil {
			t.Fatalf("Repair: %v", err)
		}
		if !places.called {
			t.Error("the place backfill did not run")
		}
		if res.PlacesEnqueued != 2 {
			t.Errorf("PlacesEnqueued = %d, want 2", res.PlacesEnqueued)
		}
	})

	t.Run("unselected runs nothing", func(t *testing.T) {
		t.Parallel()
		places := &fakePlaceBackfiller{missing: []string{"p1", "p2"}}
		res, err := placesScenario(places).Repair(context.Background(), RepairOptions{Thumbnails: true}, audit.Meta{})
		if err != nil {
			t.Fatalf("Repair: %v", err)
		}
		if places.called {
			t.Error("the place backfill ran without being selected")
		}
		if res.PlacesEnqueued != 0 {
			t.Errorf("PlacesEnqueued = %d, want 0", res.PlacesEnqueued)
		}
	})

	t.Run("unconfigured refuses", func(t *testing.T) {
		t.Parallel()
		_, err := placesScenario(nil).Repair(context.Background(), RepairOptions{Places: true}, audit.Meta{})
		if !errors.Is(err, ErrPlaceBackfillUnavailable) {
			t.Fatalf("Repair error = %v, want ErrPlaceBackfillUnavailable", err)
		}
	})
}

// TestRepairOptions_anyPlaces asserts the places option alone counts as a
// selection, so the CLI and the HTTP endpoint do not reject it as a no-op
// request.
func TestRepairOptions_anyPlaces(t *testing.T) {
	t.Parallel()
	if !(RepairOptions{Places: true}).Any() {
		t.Error("RepairOptions{Places: true}.Any() = false, want true")
	}
}
