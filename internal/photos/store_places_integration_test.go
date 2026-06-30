//go:build integration

package photos_test

import (
	"testing"

	"github.com/panbotka/kukatko/internal/photos"
	"github.com/panbotka/kukatko/internal/places"
)

// placeFixture is one seeded photo and the place to cache for it. An empty
// country marks a photo whose place row carries no real location (a no-GPS
// "processed" marker), which the aggregation and the place scope must ignore.
type placeFixture struct {
	hash     string
	country  string
	city     string
	archived bool
	noPlace  bool // create the photo but no photo_places row at all
}

// seedPlaces creates the fixture photos, caches their places, and archives the
// ones marked archived, returning each fixture's photo UID keyed by hash.
func seedPlaces(
	t *testing.T, store *photos.Store, placeStore *places.Store, fixtures []placeFixture,
) map[string]string {
	t.Helper()
	ctx := t.Context()
	uids := make(map[string]string, len(fixtures))
	for _, f := range fixtures {
		photo := mustCreate(t, store, photos.Photo{
			FileHash: f.hash, FilePath: "p/" + f.hash + ".jpg",
			FileName: f.hash + ".jpg", FileMime: "image/jpeg",
		})
		uids[f.hash] = photo.UID
		if !f.noPlace {
			if _, err := placeStore.SavePlace(ctx, places.Place{
				PhotoUID: photo.UID, Country: f.country, City: f.city,
			}); err != nil {
				t.Fatalf("SavePlace(%s): %v", f.hash, err)
			}
		}
		if f.archived {
			if _, err := store.Archive(ctx, photo.UID); err != nil {
				t.Fatalf("Archive(%s): %v", f.hash, err)
			}
		}
	}
	return uids
}

// placesFixtures is the shared dataset for the aggregation and scope tests:
// Czechia has three live photos (Praha×2, Brno×1), Austria one (Wien), plus an
// archived Praha photo, a no-place photo, and an empty-country marker — all three
// of which must be excluded from both the counts and the place scope.
var placesFixtures = []placeFixture{
	{hash: "pp-a", country: "Czechia", city: "Praha"},
	{hash: "pp-b", country: "Czechia", city: "Praha"},
	{hash: "pp-c", country: "Czechia", city: "Brno"},
	{hash: "pp-d", country: "Austria", city: "Wien"},
	{hash: "pp-e", country: "Czechia", city: "Praha", archived: true},
	{hash: "pp-f", noPlace: true},
	{hash: "pp-g", country: "", city: ""},
}

// TestAggregatePlaces verifies the place hierarchy counts non-archived photos
// that have place data, groups them by country and city, drills into one country
// with the country filter, and excludes archived photos and photos without place
// data.
func TestAggregatePlaces(t *testing.T) {
	store, db := newStore(t)
	placeStore := places.NewStore(db.Pool())
	seedPlaces(t, store, placeStore, placesFixtures)
	ctx := t.Context()

	t.Run("full hierarchy counts and sorting", func(t *testing.T) {
		got, err := store.AggregatePlaces(ctx, "")
		if err != nil {
			t.Fatalf("AggregatePlaces(all): %v", err)
		}
		// Czechia (3) before Austria (1); the archived Praha, the no-place photo
		// and the empty-country marker are all excluded.
		if len(got) != 2 {
			t.Fatalf("countries = %+v, want Czechia and Austria", got)
		}
		if got[0].Country != "Czechia" || got[0].Count != 3 {
			t.Fatalf("first country = %+v, want Czechia/3", got[0])
		}
		if got[1].Country != "Austria" || got[1].Count != 1 {
			t.Fatalf("second country = %+v, want Austria/1", got[1])
		}
		cities := got[0].Cities
		if len(cities) != 2 || cities[0].City != "Praha" || cities[0].Count != 2 ||
			cities[1].City != "Brno" || cities[1].Count != 1 {
			t.Fatalf("Czechia cities = %+v, want Praha/2 then Brno/1", cities)
		}
	})

	t.Run("country drill-down scopes to one country", func(t *testing.T) {
		got, err := store.AggregatePlaces(ctx, "Czechia")
		if err != nil {
			t.Fatalf("AggregatePlaces(Czechia): %v", err)
		}
		if len(got) != 1 || got[0].Country != "Czechia" || got[0].Count != 3 {
			t.Fatalf("drill-down = %+v, want only Czechia/3", got)
		}
		if len(got[0].Cities) != 2 {
			t.Fatalf("Czechia cities = %+v, want Praha and Brno", got[0].Cities)
		}
	})

	t.Run("unknown country yields empty hierarchy", func(t *testing.T) {
		got, err := store.AggregatePlaces(ctx, "Narnia")
		if err != nil {
			t.Fatalf("AggregatePlaces(Narnia): %v", err)
		}
		if len(got) != 0 {
			t.Fatalf("unknown country = %+v, want empty", got)
		}
	})
}

// TestList_placeScope verifies that scoping List/Count by country and city
// restricts the result to that place's photos while honouring the standard
// filters, and that archived photos are excluded — the contract the shared
// GET /photos?country=&city= grid relies on.
func TestList_placeScope(t *testing.T) {
	store, db := newStore(t)
	placeStore := places.NewStore(db.Pool())
	uids := seedPlaces(t, store, placeStore, placesFixtures)
	ctx := t.Context()

	t.Run("country scope keeps that country's live photos", func(t *testing.T) {
		list, err := store.List(ctx, photos.ListParams{Country: "Czechia"})
		if err != nil {
			t.Fatalf("List(country): %v", err)
		}
		set := uidSet(list)
		// a, b, c are the live Czechia photos; the archived e and Austrian d are out.
		if len(set) != 3 || set[uids["pp-d"]] || set[uids["pp-e"]] {
			t.Fatalf("country scope = %v, want pp-a/b/c only", set)
		}
		total, err := store.Count(ctx, photos.ListParams{Country: "Czechia"})
		if err != nil || total != 3 {
			t.Fatalf("Count(country) = %d, %v, want 3", total, err)
		}
	})

	t.Run("country and city scope narrows to one city", func(t *testing.T) {
		list, err := store.List(ctx, photos.ListParams{Country: "Czechia", City: "Praha"})
		if err != nil {
			t.Fatalf("List(country, city): %v", err)
		}
		set := uidSet(list)
		if len(set) != 2 || !set[uids["pp-a"]] || !set[uids["pp-b"]] {
			t.Fatalf("city scope = %v, want pp-a and pp-b", set)
		}
		total, err := store.Count(ctx, photos.ListParams{Country: "Czechia", City: "Praha"})
		if err != nil || total != 2 {
			t.Fatalf("Count(country, city) = %d, %v, want 2", total, err)
		}
	})

	t.Run("archived photos are included only when requested", func(t *testing.T) {
		list, err := store.List(ctx, photos.ListParams{
			Country: "Czechia", City: "Praha", OnlyArchived: true,
		})
		if err != nil {
			t.Fatalf("List(country, city, archived): %v", err)
		}
		set := uidSet(list)
		if len(set) != 1 || !set[uids["pp-e"]] {
			t.Fatalf("archived city scope = %v, want only pp-e", set)
		}
	})
}
