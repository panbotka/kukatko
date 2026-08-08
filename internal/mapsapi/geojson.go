package mapsapi

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"time"

	"github.com/panbotka/kukatko/internal/photos"
)

// geoThumbSize is the thumbnail size linked from each map feature; the
// crop-square tile suits a marker preview.
const geoThumbSize = "tile_224"

// thumbPathPrefix is the API path under which photo thumbnails are served. The
// whole API mounts under /api/v1 (see internal/server), so a feature can carry a
// ready-to-use relative thumbnail URL.
const thumbPathPrefix = "/api/v1/photos/"

// featureCollection is a GeoJSON FeatureCollection (RFC 7946).
type featureCollection struct {
	Type     string    `json:"type"`
	Features []feature `json:"features"`
	// Coverage is a foreign member (RFC 7946 §6.1 explicitly permits them) saying
	// how much of the filtered library this map can show at all. It rides on the
	// feed rather than being a request of its own because only the server knows
	// the exact filter set behind these features; a client counting for itself
	// would have to reimplement them and would drift the moment one changed.
	Coverage coverage `json:"coverage"`
}

// coverage is the map's honest report of what it is not showing: how many photos
// match the active filters (Total) against how many of them carry a location and
// so became a marker (Located). The difference is what the map is silent about.
type coverage struct {
	Located int `json:"located"`
	Total   int `json:"total"`
}

// feature is a single GeoJSON Feature: a point geometry plus the marker
// properties the map view needs.
type feature struct {
	Type       string        `json:"type"`
	Geometry   pointGeometry `json:"geometry"`
	Properties featureProps  `json:"properties"`
}

// pointGeometry is a GeoJSON Point. Per RFC 7946 the coordinate order is
// [longitude, latitude].
type pointGeometry struct {
	Type        string     `json:"type"`
	Coordinates [2]float64 `json:"coordinates"`
}

// featureProps carries the per-photo marker metadata: the UID, a title, the
// capture time, the media type, a ready-to-use thumbnail path and whether the
// pin's position is an estimate.
type featureProps struct {
	UID       string           `json:"uid"`
	Title     string           `json:"title,omitempty"`
	TakenAt   *time.Time       `json:"taken_at,omitempty"`
	MediaType photos.MediaType `json:"media_type,omitempty"`
	Thumb     string           `json:"thumb"`
	// LocationEstimated marks a pin whose coordinates were inferred from photos
	// taken nearby in time rather than measured. Estimated photos are on the map by
	// default — putting them there is the point of estimating them — but a pin that
	// looks identical to a measured one is the map quietly lying, so the marker
	// carries the distinction and the client renders it differently.
	//
	// Emitted only when true: an absent key means "not an estimate", which is the
	// overwhelmingly common case and the same default an older client assumes.
	LocationEstimated bool `json:"location_estimated,omitempty"`
}

// handlePhotos returns a GeoJSON FeatureCollection of geotagged photos, honouring
// the standard list filters (date range, album/label scope, archived).
// Only photos with both coordinates are included; the response is capped at
// maxGeoPhotos features. The collection also carries the coverage foreign member,
// so the map can say how many of the filtered photos it is leaving off. Invalid
// filter values are answered with 400.
func (a *API) handlePhotos(w http.ResponseWriter, r *http.Request) {
	params, err := a.parseGeoParams(r.URL.Query())
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	list, err := a.photos.List(r.Context(), params)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "listing photos failed")
		return
	}

	fc := featureCollection{Type: "FeatureCollection", Features: make([]feature, 0, len(list))}
	for i := range list {
		if f, ok := toFeature(&list[i]); ok {
			fc.Features = append(fc.Features, f)
		}
	}
	fc.Coverage, err = a.countCoverage(r.Context(), params, len(fc.Features))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "counting photos failed")
		return
	}
	writeJSON(w, http.StatusOK, fc)
}

// countCoverage reports how many photos the same filters match with the has-GPS
// restriction lifted, so the map can name the photos it cannot place. Located is
// the number of markers actually drawn — the truthful answer to "how many are on
// the map", including the case where the feed hit its cap — and the total is the
// one value that needs asking the database for.
func (a *API) countCoverage(ctx context.Context, params photos.ListParams, located int) (coverage, error) {
	// Every other filter is kept: a coverage figure over the whole library while
	// the map shows one album would be a number that lies. Count already ignores
	// paging, but the feed's page size is a listing concern and saying so here
	// keeps the two apart.
	countParams := params
	countParams.HasGPS = nil
	countParams.Limit = 0
	countParams.Offset = 0

	total, err := a.photos.Count(ctx, countParams)
	if err != nil {
		return coverage{}, fmt.Errorf("mapsapi: counting photos for coverage: %w", err)
	}
	return coverage{Located: located, Total: total}, nil
}

// toFeature converts a photo to a GeoJSON feature, reporting false when the photo
// lacks either coordinate (and so cannot be placed on the map).
func toFeature(p *photos.Photo) (feature, bool) {
	if p.Lat == nil || p.Lng == nil {
		return feature{}, false
	}
	return feature{
		Type:     "Feature",
		Geometry: pointGeometry{Type: "Point", Coordinates: [2]float64{*p.Lng, *p.Lat}},
		Properties: featureProps{
			UID:               p.UID,
			Title:             p.Title,
			TakenAt:           p.TakenAt,
			MediaType:         p.MediaType,
			Thumb:             thumbPathPrefix + url.PathEscape(p.UID) + "/thumb/" + geoThumbSize,
			LocationEstimated: p.LocationSource == photos.LocationSourceEstimate,
		},
	}, true
}

// parseGeoParams builds the photo list parameters for the GeoJSON feed from the
// query: the date range, album/label scope and the archived filter, with has-GPS
// forced on and the page sized to the configured feature cap so the whole map's
// markers come back in one response.
func (a *API) parseGeoParams(q url.Values) (photos.ListParams, error) {
	params := photos.ListParams{
		Limit: a.maxGeoPhotos,
		Sort:  photos.SortByTakenAt,
		Order: photos.OrderDesc,
	}
	// The map filter offers a single album/label; wrap each set value in the
	// list the shared list path now expects (an empty value adds no scope).
	if album := q.Get("album"); album != "" {
		params.AlbumUIDs = []string{album}
	}
	if label := q.Get("label"); label != "" {
		params.LabelUIDs = []string{label}
	}
	hasGPS := true
	params.HasGPS = &hasGPS

	if err := applyArchivedFilter(q.Get("archived"), &params); err != nil {
		return photos.ListParams{}, err
	}

	after, err := parseTime(q.Get("taken_after"))
	if err != nil {
		return photos.ListParams{}, errors.New("taken_after must be an RFC3339 timestamp or YYYY-MM-DD date")
	}
	params.TakenAfter = after
	before, err := parseTime(q.Get("taken_before"))
	if err != nil {
		return photos.ListParams{}, errors.New("taken_before must be an RFC3339 timestamp or YYYY-MM-DD date")
	}
	params.TakenBefore = before
	return params, nil
}

// applyArchivedFilter applies the archived selector (live by default, included
// with "true", exclusively with "only"), returning a descriptive error for an
// unknown value.
func applyArchivedFilter(raw string, params *photos.ListParams) error {
	switch raw {
	case "", "false":
		// Default: live photos only.
	case "true":
		params.IncludeArchived = true
	case "only":
		params.OnlyArchived = true
	default:
		return fmt.Errorf("unknown archived %q (want true, false or only)", raw)
	}
	return nil
}

// parseTime parses an optional timestamp query value (RFC3339 or YYYY-MM-DD),
// returning nil when absent.
func parseTime(raw string) (*time.Time, error) {
	if raw == "" {
		return nil, nil //nolint:nilnil // absent optional filter: no value, no error
	}
	if t, err := time.Parse(time.RFC3339, raw); err == nil {
		return &t, nil
	}
	if t, err := time.Parse("2006-01-02", raw); err == nil {
		return &t, nil
	}
	return nil, fmt.Errorf("unparseable time %q", raw)
}
