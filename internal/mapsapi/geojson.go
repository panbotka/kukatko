package mapsapi

import (
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
// capture time, the media type and a ready-to-use thumbnail path.
type featureProps struct {
	UID       string           `json:"uid"`
	Title     string           `json:"title,omitempty"`
	TakenAt   *time.Time       `json:"taken_at,omitempty"`
	MediaType photos.MediaType `json:"media_type,omitempty"`
	Thumb     string           `json:"thumb"`
}

// handlePhotos returns a GeoJSON FeatureCollection of geotagged photos, honouring
// the standard list filters (date range, album/label scope, archived, private).
// Only photos with both coordinates are included; the response is capped at
// maxGeoPhotos features. Invalid filter values are answered with 400.
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
	writeJSON(w, http.StatusOK, fc)
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
			UID:       p.UID,
			Title:     p.Title,
			TakenAt:   p.TakenAt,
			MediaType: p.MediaType,
			Thumb:     thumbPathPrefix + url.PathEscape(p.UID) + "/thumb/" + geoThumbSize,
		},
	}, true
}

// parseGeoParams builds the photo list parameters for the GeoJSON feed from the
// query: the date range, album/label scope, archived and private filters, with
// has-GPS forced on and the page sized to the configured feature cap so the whole
// map's markers come back in one response.
func (a *API) parseGeoParams(q url.Values) (photos.ListParams, error) {
	params := photos.ListParams{
		// Repeated (or comma-joined) values scope the feed to every listed album or
		// label (AND). q["album"] is nil when absent, so an unscoped feed adds no
		// membership clause; empty entries are skipped in the store.
		AlbumUIDs: q["album"],
		LabelUIDs: q["label"],
		Limit:     a.maxGeoPhotos,
		Sort:      photos.SortByTakenAt,
		Order:     photos.OrderDesc,
	}
	hasGPS := true
	params.HasGPS = &hasGPS

	if err := applyArchivedFilter(q.Get("archived"), &params); err != nil {
		return photos.ListParams{}, err
	}
	private, err := parseBool(q.Get("private"))
	if err != nil {
		return photos.ListParams{}, errors.New("private must be true or false")
	}
	params.Private = private

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

// parseBool parses an optional boolean query value, returning nil when absent.
func parseBool(raw string) (*bool, error) {
	if raw == "" {
		return nil, nil //nolint:nilnil // absent optional filter: no value, no error
	}
	switch raw {
	case "true":
		b := true
		return &b, nil
	case "false":
		b := false
		return &b, nil
	default:
		return nil, fmt.Errorf("invalid boolean %q", raw)
	}
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
