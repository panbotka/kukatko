package main

import (
	"context"
	"testing"

	"github.com/panbotka/kukatko/internal/config"
	"github.com/panbotka/kukatko/internal/jobs"
	"github.com/panbotka/kukatko/internal/mapy"
	"github.com/panbotka/kukatko/internal/photos"
	"github.com/panbotka/kukatko/internal/places"
	"github.com/panbotka/kukatko/internal/placesjob"
)

// The four stubs below only exist to satisfy placesjob.New, which panics on a nil
// collaborator; the wiring test never runs a geocode through them.

type stubPlacePhotos struct{}

func (stubPlacePhotos) GetByUID(context.Context, string) (photos.Photo, error) {
	return photos.Photo{}, photos.ErrPhotoNotFound
}

type stubPlaceStore struct{}

func (stubPlaceStore) GetPlace(context.Context, string) (places.Place, error) {
	return places.Place{}, places.ErrPlaceNotFound
}

func (stubPlaceStore) SavePlace(_ context.Context, p places.Place) (places.Place, error) {
	return p, nil
}

func (stubPlaceStore) ListPhotosMissingPlaces(context.Context, int) ([]string, error) {
	return nil, nil
}

type stubGeocoder struct{}

func (stubGeocoder) ReverseGeocode(context.Context, float64, float64) (*mapy.GeocodeResult, error) {
	return nil, mapy.ErrNotFound
}

type stubPlaceEnqueuer struct{}

func (stubPlaceEnqueuer) EnqueuePlaces(context.Context, string) error { return nil }

// TestPlacesEnqueuerOrNil asserts the upload pipeline is handed a reverse-geocode
// enqueuer exactly when geocoding is configured. It must be a *nil interface* with
// no key, not a typed nil: ingest checks the interface, and a typed nil would make
// it schedule `places` jobs on an instance where no handler is registered, leaving
// them queued for ever.
func TestPlacesEnqueuerOrNil(t *testing.T) {
	t.Parallel()

	enqueuer := jobs.NewEnqueuer(nil)
	if got := placesEnqueuerOrNil(&config.Config{}, enqueuer); got != nil {
		t.Errorf("with no mapy.com key the enqueuer = %v, want nil", got)
	}
	cfg := &config.Config{Maps: config.MapsConfig{MapyAPIKey: "k"}}
	if got := placesEnqueuerOrNil(cfg, enqueuer); got == nil {
		t.Error("with a mapy.com key the enqueuer is nil, so uploads would never be geocoded")
	}
}

// TestMaintenancePlaceBackfillerOrNil asserts the maintenance service gets a place
// backfiller only when one was built, again as a nil interface — that is what makes
// the scan report no reverse-geocode backlog and `repair --places` refuse instead
// of panicking on a typed nil.
func TestMaintenancePlaceBackfillerOrNil(t *testing.T) {
	t.Parallel()

	if got := maintenancePlaceBackfillerOrNil(nil); got != nil {
		t.Errorf("without a places service the backfiller = %v, want nil", got)
	}
	svc := placesjob.New(placesjob.Config{
		Photos: stubPlacePhotos{}, Places: stubPlaceStore{}, Geocoder: stubGeocoder{},
		Enqueuer: stubPlaceEnqueuer{},
	})
	if got := maintenancePlaceBackfillerOrNil(svc); got == nil {
		t.Error("with a places service the backfiller is nil, so the repair could never run")
	}
}

// TestMaintenanceRepairCmd_hasPlacesFlag asserts the reverse-geocode repair is
// reachable from the CLI and reads into RepairOptions.Places, so the release can
// run it with the server stopped.
func TestMaintenanceRepairCmd_hasPlacesFlag(t *testing.T) {
	t.Parallel()

	cmd := newMaintenanceRepairCmd()
	if cmd.Flags().Lookup("places") == nil {
		t.Fatal("maintenance repair has no --places flag")
	}
	if err := cmd.Flags().Set("places", "true"); err != nil {
		t.Fatalf("setting --places: %v", err)
	}
	opts, err := repairOptionsFromFlags(cmd)
	if err != nil {
		t.Fatalf("repairOptionsFromFlags: %v", err)
	}
	if !opts.Places {
		t.Error("--places did not select the place repair")
	}
	if !opts.Any() {
		t.Error("--places alone does not count as a selection, so the command would do nothing")
	}
}
