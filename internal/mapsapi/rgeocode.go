package mapsapi

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"

	"github.com/panbotka/kukatko/internal/mapy"
)

// coordinate bounds for reverse-geocode requests.
const (
	maxLatitude  = 90
	maxLongitude = 180
)

// handleReverseGeocode proxies a mapy.com reverse-geocode lookup for the lat/lng
// query parameters, returning the simplified {name, location, regional_structure}
// answer. Answers are cached (keyed by rounded coordinate) to conserve the
// 4-credit geocode cost, and uncached lookups are rate-limited to protect the
// monthly credit budget (429 when the budget is momentarily exhausted). An
// unconfigured proxy answers 503; a coordinate with no match answers 404.
func (a *API) handleReverseGeocode(w http.ResponseWriter, r *http.Request) {
	if a.geocoder == nil {
		writeError(w, http.StatusServiceUnavailable, "reverse geocoding is not configured")
		return
	}
	lat, lng, err := parseLatLng(r.URL.Query().Get("lat"), r.URL.Query().Get("lng"))
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	key := geocodeKey(lat, lng)
	if cached, ok := a.geocodeCache.get(key); ok {
		a.writeGeocode(w, cached)
		return
	}
	if !a.geocodeLimiter.allow() {
		writeError(w, http.StatusTooManyRequests, "reverse-geocode rate limit exceeded, try again shortly")
		return
	}

	result, err := a.geocoder.ReverseGeocode(r.Context(), lat, lng)
	a.health.Record(err)
	if err != nil {
		writeGeocodeError(w, err)
		return
	}
	a.geocodeCache.set(key, *result, a.geocodeCacheTTL)
	a.writeGeocode(w, *result)
}

// writeGeocode writes a successful reverse-geocode answer with a private,
// long-lived cache header (a coordinate's location is stable).
func (a *API) writeGeocode(w http.ResponseWriter, result mapy.GeocodeResult) {
	w.Header().Set("Cache-Control", fmt.Sprintf("private, max-age=%d", int(a.geocodeCacheTTL.Seconds())))
	writeJSON(w, http.StatusOK, result)
}

// parseLatLng parses and range-checks the latitude/longitude query values,
// returning a descriptive error for a missing, non-numeric or out-of-range value.
func parseLatLng(rawLat, rawLng string) (lat, lng float64, err error) {
	if rawLat == "" || rawLng == "" {
		return 0, 0, errors.New("lat and lng are required")
	}
	lat, err = strconv.ParseFloat(rawLat, 64)
	if err != nil {
		return 0, 0, errors.New("lat must be a number")
	}
	lng, err = strconv.ParseFloat(rawLng, 64)
	if err != nil {
		return 0, 0, errors.New("lng must be a number")
	}
	if lat < -maxLatitude || lat > maxLatitude {
		return 0, 0, fmt.Errorf("lat must be between -%d and %d", maxLatitude, maxLatitude)
	}
	if lng < -maxLongitude || lng > maxLongitude {
		return 0, 0, fmt.Errorf("lng must be between -%d and %d", maxLongitude, maxLongitude)
	}
	return lat, lng, nil
}

// geocodeKey rounds a coordinate to ~1 m precision (five decimal places) and
// formats it as the cache key, so near-identical lookups share a cached answer.
func geocodeKey(lat, lng float64) string {
	return strconv.FormatFloat(lat, 'f', 5, 64) + "," + strconv.FormatFloat(lng, 'f', 5, 64)
}

// writeGeocodeError maps a mapy client error to a client-facing status without
// leaking the API key: no match is 404, a rejected key StatusMapKeyRejected (our
// key, not the caller's request, is the problem), a hit rate limit 429, an
// unreachable provider 503, and any other upstream problem 502.
func writeGeocodeError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, mapy.ErrNotFound):
		writeError(w, http.StatusNotFound, "no location found for those coordinates")
	case errors.Is(err, mapy.ErrUnauthorized):
		writeError(w, StatusMapKeyRejected, "map provider rejected the server's API key")
	case errors.Is(err, mapy.ErrRateLimited):
		writeError(w, http.StatusTooManyRequests, "map provider rate limit exceeded")
	case errors.Is(err, mapy.ErrUnavailable):
		writeError(w, http.StatusServiceUnavailable, "map provider unavailable")
	default:
		writeError(w, http.StatusBadGateway, "map provider error")
	}
}
