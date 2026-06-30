//go:build integration

package places_test

import (
	"context"
	"errors"
	"testing"

	"github.com/panbotka/kukatko/internal/database/dbtest"
	"github.com/panbotka/kukatko/internal/photos"
	"github.com/panbotka/kukatko/internal/places"
)

// These tests run only under `make test-integration` against the database named
// by KUKATKO_TEST_DATABASE_URL. They share one database and truncate between
// cases, so they intentionally do not run in parallel.

// ptr returns a pointer to v.
func ptr[T any](v T) *T { return &v }

// makePhoto inserts a photo with the given uid suffix and optional coordinates,
// returning the created record. A distinct file hash per name avoids the
// file_hash unique constraint.
func makePhoto(t *testing.T, store *photos.Store, name string, lat, lng *float64) photos.Photo {
	t.Helper()
	created, err := store.Create(context.Background(), photos.Photo{
		FileHash:        "hash-" + name,
		FilePath:        "2024/01/" + name + ".jpg",
		FileName:        name + ".jpg",
		FileSize:        100,
		FileMime:        "image/jpeg",
		FileOrientation: 1,
		Lat:             lat,
		Lng:             lng,
	})
	if err != nil {
		t.Fatalf("create photo %s: %v", name, err)
	}
	return created
}

// TestSavePlaceAndGet verifies a place writes and reads back, and that SavePlace
// upserts (a second write for the same photo replaces the first).
func TestSavePlaceAndGet(t *testing.T) {
	db := dbtest.New(t)
	dbtest.TruncateAll(t, db)
	ctx := context.Background()

	photoStore := photos.NewStore(db.Pool())
	store := places.NewStore(db.Pool())
	photo := makePhoto(t, photoStore, "geo", ptr(50.09), ptr(14.40))

	saved, err := store.SavePlace(ctx, places.Place{
		PhotoUID: photo.UID, Country: "Česko", Region: "Praha", City: "Praha",
		PlaceName: "Pražský hrad", Lat: ptr(50.09), Lng: ptr(14.40),
	})
	if err != nil {
		t.Fatalf("SavePlace: %v", err)
	}
	if saved.GeocodedAt.IsZero() {
		t.Error("SavePlace did not stamp geocoded_at")
	}

	got, err := store.GetPlace(ctx, photo.UID)
	if err != nil {
		t.Fatalf("GetPlace: %v", err)
	}
	if got.Country != "Česko" || got.City != "Praha" || got.PlaceName != "Pražský hrad" {
		t.Errorf("GetPlace = %+v", got)
	}
	if got.Lat == nil || *got.Lat != 50.09 {
		t.Errorf("GetPlace lat = %v, want 50.09", got.Lat)
	}

	// Upsert: a second save replaces the row in place.
	if _, err := store.SavePlace(ctx, places.Place{PhotoUID: photo.UID, City: "Brno"}); err != nil {
		t.Fatalf("SavePlace (upsert): %v", err)
	}
	got, err = store.GetPlace(ctx, photo.UID)
	if err != nil {
		t.Fatalf("GetPlace (after upsert): %v", err)
	}
	if got.City != "Brno" || got.Country != "" || got.Lat != nil {
		t.Errorf("after upsert = %+v, want City=Brno and cleared fields", got)
	}
}

// TestGetPlace_notFound verifies the sentinel for a photo without a place row.
func TestGetPlace_notFound(t *testing.T) {
	db := dbtest.New(t)
	dbtest.TruncateAll(t, db)

	store := places.NewStore(db.Pool())
	if _, err := store.GetPlace(context.Background(), "ph-missing"); !errors.Is(err, places.ErrPlaceNotFound) {
		t.Errorf("GetPlace = %v, want ErrPlaceNotFound", err)
	}
}

// TestListPhotosMissingPlaces verifies the backfill source returns only
// non-archived, geotagged photos without a place row.
func TestListPhotosMissingPlaces(t *testing.T) {
	db := dbtest.New(t)
	dbtest.TruncateAll(t, db)
	ctx := context.Background()

	photoStore := photos.NewStore(db.Pool())
	store := places.NewStore(db.Pool())

	geoMissing := makePhoto(t, photoStore, "missing", ptr(50.0), ptr(14.0))
	geoDone := makePhoto(t, photoStore, "done", ptr(49.0), ptr(13.0))
	makePhoto(t, photoStore, "nogps", nil, nil)                        // no GPS — excluded
	archived := makePhoto(t, photoStore, "arch", ptr(48.0), ptr(12.0)) // archived — excluded

	if _, err := store.SavePlace(ctx, places.Place{PhotoUID: geoDone.UID, City: "Done"}); err != nil {
		t.Fatalf("seed place: %v", err)
	}
	if _, err := photoStore.Archive(ctx, archived.UID); err != nil {
		t.Fatalf("archive: %v", err)
	}

	uids, err := store.ListPhotosMissingPlaces(ctx, 0)
	if err != nil {
		t.Fatalf("ListPhotosMissingPlaces: %v", err)
	}
	if len(uids) != 1 || uids[0] != geoMissing.UID {
		t.Errorf("missing = %v, want [%s]", uids, geoMissing.UID)
	}
}
