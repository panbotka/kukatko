//go:build integration

package photoapi_test

import (
	"encoding/json"
	"errors"
	"net/http"
	"testing"

	"github.com/panbotka/kukatko/internal/auth"
	"github.com/panbotka/kukatko/internal/photos"
	"github.com/panbotka/kukatko/internal/places"
)

// placeRef mirrors the place block of the detail response.
type placeRef struct {
	Country   string `json:"country"`
	Region    string `json:"region"`
	City      string `json:"city"`
	PlaceName string `json:"place_name"`
}

// TestDetailPlace verifies the detail response carries the cached reverse-geocoded
// place when photo_places holds a row for the photo, omits the block cleanly when
// it does not (and for the job's "processed, no place" marker row), and — the point
// of serving the cache rather than the geocoder — that reading a detail never
// geocodes: a geotagged photo with no cached place still has none afterwards.
func TestDetailPlace(t *testing.T) {
	env := newEnv(t)
	client, _ := env.login(t, "viewer", auth.RoleViewer)
	base := env.server.URL

	decodePlace := func(t *testing.T, uid string) (*placeRef, bool) {
		t.Helper()
		resp := mustDo(t, client, http.MethodGet, base+"/api/v1/photos/"+uid, nil)
		defer func() { _ = resp.Body.Close() }()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("detail status = %d, want 200", resp.StatusCode)
		}
		// Decode into a map first so an absent key is distinguishable from a
		// present-but-empty one: the requirement is that the block is omitted.
		var body map[string]json.RawMessage
		if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
			t.Fatalf("decode detail: %v", err)
		}
		raw, ok := body["place"]
		if !ok {
			return nil, false
		}
		var place placeRef
		if err := json.Unmarshal(raw, &place); err != nil {
			t.Fatalf("decode place: %v", err)
		}
		return &place, true
	}

	t.Run("carries the cached place", func(t *testing.T) {
		seeded := env.seedPhoto(t, photos.Photo{
			Title: "Špilberk", TakenAtSource: "exif",
			Lat: new(49.194), Lng: new(16.599),
		}, "spilberk.jpg", 11, 22, 33)
		if _, err := env.places.SavePlace(t.Context(), places.Place{
			PhotoUID: seeded.UID, Country: "Česko", Region: "Jihomoravský kraj",
			City: "Brno", PlaceName: "Špilberk",
			Lat: new(49.194), Lng: new(16.599),
		}); err != nil {
			t.Fatalf("SavePlace: %v", err)
		}

		got, ok := decodePlace(t, seeded.UID)
		if !ok {
			t.Fatal("detail has no place block, want the cached place")
		}
		want := placeRef{
			Country: "Česko", Region: "Jihomoravský kraj",
			City: "Brno", PlaceName: "Špilberk",
		}
		if *got != want {
			t.Errorf("place = %+v, want %+v", *got, want)
		}
	})

	t.Run("omits the place and does not geocode", func(t *testing.T) {
		seeded := env.seedPhoto(t, photos.Photo{
			Title: "Ungeocoded", TakenAtSource: "exif",
			Lat: new(50.087), Lng: new(14.421),
		}, "ungeocoded.jpg", 44, 55, 66)

		if _, ok := decodePlace(t, seeded.UID); ok {
			t.Error("detail carries a place block, want it omitted for an ungeocoded photo")
		}
		// Serving the detail must not have resolved the coordinate: mapy.com credits
		// are metered, so the cache stays empty until the `places` job fills it.
		if _, err := env.places.GetPlace(t.Context(), seeded.UID); !errors.Is(err, places.ErrPlaceNotFound) {
			t.Errorf("GetPlace after detail = %v, want ErrPlaceNotFound (the detail geocoded)", err)
		}
	})

	t.Run("omits the processed marker row", func(t *testing.T) {
		seeded := env.seedPhoto(t, photos.Photo{
			Title: "No GPS", TakenAtSource: "exif",
		}, "nogps.jpg", 77, 88, 99)
		// The places job marks a photo it could not place with an all-empty row.
		if _, err := env.places.SavePlace(t.Context(), places.Place{PhotoUID: seeded.UID}); err != nil {
			t.Fatalf("SavePlace(marker): %v", err)
		}

		if _, ok := decodePlace(t, seeded.UID); ok {
			t.Error("detail carries a place block, want it omitted for the processed marker row")
		}
	})
}
