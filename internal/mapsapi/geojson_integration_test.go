//go:build integration

package mapsapi_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/panbotka/kukatko/internal/database/dbtest"
	"github.com/panbotka/kukatko/internal/mapsapi"
	"github.com/panbotka/kukatko/internal/organize"
	"github.com/panbotka/kukatko/internal/photos"
)

// These tests run only under `make test-integration` against the database named
// by KUKATKO_TEST_DATABASE_URL. They share one database and truncate per case, so
// they do not run in parallel.

// geoFeatureCollection decodes the GeoJSON response far enough for the assertions.
type geoFeatureCollection struct {
	Type     string `json:"type"`
	Features []struct {
		Geometry struct {
			Coordinates [2]float64 `json:"coordinates"`
		} `json:"geometry"`
		Properties struct {
			UID string `json:"uid"`
		} `json:"properties"`
	} `json:"features"`
}

// geoEnv wires the maps API over the integration database behind an httptest
// server, with a real photo store and a passthrough auth guard.
type geoEnv struct {
	server   *httptest.Server
	photos   *photos.Store
	organize *organize.Store
}

// newGeoEnv builds the GeoJSON test environment over a freshly truncated database.
func newGeoEnv(t *testing.T) *geoEnv {
	t.Helper()
	db := dbtest.New(t)
	dbtest.TruncateAll(t, db)

	photoStore := photos.NewStore(db.Pool())
	api := mapsapi.NewAPI(mapsapi.Config{
		Photos:      photoStore,
		RequireAuth: passthroughAuth,
	})
	r := chi.NewRouter()
	r.Route("/api/v1", api.RegisterRoutes)
	srv := httptest.NewServer(r)
	t.Cleanup(srv.Close)
	return &geoEnv{server: srv, photos: photoStore, organize: organize.NewStore(db.Pool())}
}

// seedGeo catalogues a photo with the given coordinates (nil lat/lng for a
// non-geotagged photo) and metadata, returning it.
func (e *geoEnv) seedGeo(t *testing.T, hash string, p photos.Photo) photos.Photo {
	t.Helper()
	p.FileHash = hash
	p.FilePath = hash + ".jpg"
	p.FileName = hash + ".jpg"
	p.FileMime = "image/jpeg"
	created, err := e.photos.Create(context.Background(), p)
	if err != nil {
		t.Fatalf("Create(%s): %v", hash, err)
	}
	return created
}

// fetchGeo fetches the GeoJSON feed at the given query and decodes it.
func (e *geoEnv) fetchGeo(t *testing.T, query string) geoFeatureCollection {
	t.Helper()
	resp, err := http.Get(e.server.URL + "/api/v1/map/photos" + query)
	if err != nil {
		t.Fatalf("GET %s: %v", query, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET %s status = %d, want 200", query, resp.StatusCode)
	}
	var fc geoFeatureCollection
	if err := json.NewDecoder(resp.Body).Decode(&fc); err != nil {
		t.Fatalf("decode %s: %v", query, err)
	}
	return fc
}

// geoUIDs collects the feature UIDs into a set.
func geoUIDs(fc geoFeatureCollection) map[string]bool {
	set := make(map[string]bool, len(fc.Features))
	for _, f := range fc.Features {
		set[f.Properties.UID] = true
	}
	return set
}

func ptr[T any](v T) *T { return &v }

// TestGeoJSON_geotaggedAndFilters exercises the GeoJSON feed end to end against
// the real database: only geotagged photos appear, coordinates are [lng,lat], and
// the date-range, album-scope and archived filters all apply.
func TestGeoJSON_geotaggedAndFilters(t *testing.T) {
	env := newGeoEnv(t)
	ctx := context.Background()

	t1 := time.Date(2021, 1, 1, 12, 0, 0, 0, time.UTC)
	t2 := time.Date(2023, 6, 15, 12, 0, 0, 0, time.UTC)

	withGPS := env.seedGeo(t, "gps1", photos.Photo{
		Title: "with gps", Lat: ptr(50.0), Lng: ptr(14.0), TakenAt: &t1,
	})
	recent := env.seedGeo(t, "gps2", photos.Photo{
		Title: "recent gps", Lat: ptr(49.0), Lng: ptr(15.0), TakenAt: &t2,
	})
	env.seedGeo(t, "nogps", photos.Photo{Title: "no gps", TakenAt: &t1})
	archived := env.seedGeo(t, "gps3", photos.Photo{
		Title: "archived gps", Lat: ptr(48.0), Lng: ptr(16.0), TakenAt: &t2,
		ArchivedAt: &t2,
	})

	t.Run("only geotagged live photos with [lng,lat] coordinates", func(t *testing.T) {
		fc := env.fetchGeo(t, "")
		if fc.Type != "FeatureCollection" {
			t.Fatalf("type = %s, want FeatureCollection", fc.Type)
		}
		set := geoUIDs(fc)
		if len(set) != 2 || !set[withGPS.UID] || !set[recent.UID] {
			t.Fatalf("uids = %v, want {%s,%s} (no nogps, no archived)", set, withGPS.UID, recent.UID)
		}
		for _, f := range fc.Features {
			if f.Properties.UID == withGPS.UID && f.Geometry.Coordinates != [2]float64{14.0, 50.0} {
				t.Errorf("coordinates = %v, want [14,50] ([lng,lat])", f.Geometry.Coordinates)
			}
		}
	})

	t.Run("date range filter", func(t *testing.T) {
		fc := env.fetchGeo(t, "?taken_after=2022-01-01")
		set := geoUIDs(fc)
		if len(set) != 1 || !set[recent.UID] {
			t.Fatalf("uids = %v, want {%s} after 2022", set, recent.UID)
		}
	})

	t.Run("archived=only scope", func(t *testing.T) {
		fc := env.fetchGeo(t, "?archived=only")
		set := geoUIDs(fc)
		if len(set) != 1 || !set[archived.UID] {
			t.Fatalf("uids = %v, want {%s} archived", set, archived.UID)
		}
	})

	t.Run("album scope", func(t *testing.T) {
		album, err := env.organize.CreateAlbum(ctx, organize.Album{Title: "Trip"})
		if err != nil {
			t.Fatalf("CreateAlbum: %v", err)
		}
		if err := env.organize.AddPhoto(ctx, album.UID, withGPS.UID); err != nil {
			t.Fatalf("AddPhoto: %v", err)
		}
		fc := env.fetchGeo(t, "?album="+album.UID)
		set := geoUIDs(fc)
		if len(set) != 1 || !set[withGPS.UID] {
			t.Fatalf("uids = %v, want {%s} in album", set, withGPS.UID)
		}
	})
}
