package photoapi

import (
	"context"
	"log"

	"github.com/panbotka/kukatko/internal/photos"
	"github.com/panbotka/kukatko/internal/places"
)

// PlaceResolver reads a photo's cached reverse-geocoded place (country / region /
// city / place name) from the photo_places side table. It is a narrow interface so
// photoapi depends on the behaviour, not on the places store's wiring;
// places.Store satisfies it and a test fake can stand in.
//
// It is deliberately a *cache* reader and nothing else: the detail endpoint never
// geocodes on demand. mapy.com credits are metered, so a coordinate is resolved
// exactly once — by the background `places` job — and every reader afterwards is
// served from the cache. Looking a place up because someone opened a photo would
// spend a credit per view.
type PlaceResolver interface {
	// GetPlace returns the cached place for the photo, or places.ErrPlaceNotFound
	// when it has not been geocoded yet.
	GetPlace(ctx context.Context, photoUID string) (places.Place, error)
}

// placeRef is the cached place block embedded in a photo detail response: the
// reverse-geocoded hierarchy of the photo's coordinate. It carries no lat/lng of
// its own — the photo already ships those — and no geocoded_at, which is
// bookkeeping the detail view has no use for.
type placeRef struct {
	Country   string `json:"country"`
	Region    string `json:"region"`
	City      string `json:"city"`
	PlaceName string `json:"place_name"`
}

// resolvePlace returns the photo's cached place for the detail response, or nil —
// so the response omits the block entirely — when there is nothing to show: no
// resolver wired, no cached row yet (the photo is not geotagged, or the `places`
// job has not reached it), a lookup failure, or an all-empty row.
//
// An all-empty row is the "processed, no place" marker the places job writes for a
// photo without usable coordinates; rendering it would put an empty Location block
// on the page. Like resolveUploader, this never fails the detail request over its
// own field: a photo is worth showing even when its place is not available.
func (a *API) resolvePlace(ctx context.Context, photoUID string) *placeRef {
	if a.places == nil {
		return nil
	}
	place, err := a.places.GetPlace(ctx, photoUID)
	if err != nil {
		return nil
	}
	if place.Country == "" && place.Region == "" && place.City == "" && place.PlaceName == "" {
		return nil
	}
	return &placeRef{
		Country:   place.Country,
		Region:    place.Region,
		City:      place.City,
		PlaceName: place.PlaceName,
	}
}

// PlacesEnqueuer schedules the reverse geocode of a photo's coordinates — the
// `places` job that fills the photo_places cache country/city browsing, the
// places hierarchy and the detail page's Location block all read from. It is
// satisfied by jobs.Enqueuer.
//
// A nil PlacesEnqueuer disables the scheduling: the coordinate is still saved,
// the cached place simply stays stale until the next backfill sweeps it up.
type PlacesEnqueuer interface {
	// EnqueuePlaces schedules reverse geocoding for photoUID. An active job for
	// the same photo is a no-op.
	EnqueuePlaces(ctx context.Context, photoUID string) error
}

// enqueueGeocode schedules a reverse geocode after an edit moved (or cleared) a
// photo's coordinates, so the cached place stops describing where the photo used
// to be. It is best-effort in exactly the way enqueueSidecar is: a failure is
// logged and swallowed, never returned, because the coordinate is safely in
// Postgres either way and refusing the save would be the derived work breaking
// the edit it derives from.
//
// It must run after the mutation has committed: the `places` job re-reads the
// photo and compares the cached coordinates with the row's, so enqueuing earlier
// would have it read the old location and decide it is already current. Clearing
// a location is enqueued for the same reason it is saved — the job answers a
// photo with no coordinates by writing the empty processed marker, which is what
// removes the stale place from the hierarchy.
//
// Enqueuing is free. The metered mapy.com call happens in the worker, behind the
// existing rate limit and credit budget, so this can never spend a credit on the
// request's thread.
func (a *API) enqueueGeocode(ctx context.Context, photoUID string) {
	if a.geocodes == nil || photoUID == "" {
		return
	}
	if err := a.geocodes.EnqueuePlaces(ctx, photoUID); err != nil {
		log.Printf("photoapi: enqueuing places for %s: %v", photoUID, err)
	}
}

// coordinateMoved reports whether an edit changed where the photo is: either
// coordinate set, cleared or moved to a different value. It compares the row as
// it was against the row as it was stored — not against what the request asked
// for — so a PATCH that resends the coordinate it already had schedules nothing.
func coordinateMoved(before, after photos.Photo) bool {
	return !sameCoordinate(before.Lat, after.Lat) || !sameCoordinate(before.Lng, after.Lng)
}

// sameCoordinate reports whether two nullable coordinate components are the same
// value, counting two absent ones as equal.
func sameCoordinate(a, b *float64) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}
