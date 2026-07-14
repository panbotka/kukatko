package photoapi

import (
	"context"

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
