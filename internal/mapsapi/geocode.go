package mapsapi

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/panbotka/kukatko/internal/mapy"
)

// maxPlaceQueryRunes bounds the place name one search may carry. Nobody types a
// 200-character place name; a request that does is either a mistake or an attempt
// to make us spend a credit on nonsense, and either way the answer is a 400 rather
// than an upstream call.
const maxPlaceQueryRunes = 200

// placesBody is the JSON body of a place-search answer: the ranked suggestions,
// best match first. It is an object rather than a bare array so the response can
// grow a field later without breaking every client.
type placesBody struct {
	Items []mapy.Place `json:"items"`
}

// handleGeocode proxies a mapy.com forward-geocode (place name → coordinates)
// lookup for the q query parameter, returning at most `limit` ranked suggestions.
//
// A typeahead fires on typing, and every uncached lookup is metered mapy.com
// credits, so the cheap guards come first and in order: a blank or over-long query
// is a 400 before any upstream call, a repeat of a query already asked is served
// from the cache, and only what survives both reaches the shared geocode rate
// limiter (429 when the budget is momentarily exhausted). The client debounces
// too; this is the half of the throttle that a client cannot skip.
//
// A query mapy.com matches nothing for is an empty list and a 200 — "no
// suggestions yet" is the normal answer to a half-typed name, not a failure. An
// unconfigured proxy answers 503, which the UI shows as "place search
// unavailable" while the rest of the location editor keeps working.
func (a *API) handleGeocode(w http.ResponseWriter, r *http.Request) {
	if a.places == nil {
		writeError(w, http.StatusServiceUnavailable, "place search is not configured")
		return
	}
	query, limit, err := parsePlaceQuery(r.URL.Query())
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	key := placeKey(query, limit)
	if cached, ok := a.placesCache.get(key); ok {
		a.writePlaces(w, cached)
		return
	}
	if !a.geocodeLimiter.allow() {
		writeError(w, http.StatusTooManyRequests, "place-search rate limit exceeded, try again shortly")
		return
	}

	found, err := a.places.Geocode(r.Context(), query, limit)
	a.health.Record(err)
	if errors.Is(err, mapy.ErrNotFound) {
		// mapy.com answers 404 for a name it cannot place at all. For a typeahead
		// that is an empty dropdown, not an error to show the user.
		found, err = []mapy.Place{}, nil
	}
	if err != nil {
		writeGeocodeError(w, err)
		return
	}
	a.placesCache.set(key, found, a.geocodeCacheTTL)
	a.writePlaces(w, found)
}

// writePlaces writes a successful place-search answer with a private, long-lived
// cache header: a place's coordinates never move, so a browser that asks the same
// question twice (a retyped or re-opened search) may answer itself.
//
// A nil slice is normalised to an empty one. "No results" as `[]Place(nil)` is
// perfectly idiomatic for a PlaceSearcher to return, but it would serialise to
// `"items": null` and hand the client a null where it was promised a list.
func (a *API) writePlaces(w http.ResponseWriter, places []mapy.Place) {
	if places == nil {
		places = []mapy.Place{}
	}
	w.Header().Set("Cache-Control", fmt.Sprintf("private, max-age=%d", int(a.geocodeCacheTTL.Seconds())))
	writeJSON(w, http.StatusOK, placesBody{Items: places})
}

// parsePlaceQuery extracts and validates the place-search parameters: the q place
// name (required, length-capped) and the optional limit, which is clamped rather
// than rejected — an absurd count is a request worth answering with a sane number
// of rows, not a 400. It returns a descriptive error for a missing, over-long or
// non-numeric value.
func parsePlaceQuery(values url.Values) (query string, limit int, err error) {
	query = strings.TrimSpace(values.Get("q"))
	if query == "" {
		return "", 0, errors.New("q is required")
	}
	if utf8.RuneCountInString(query) > maxPlaceQueryRunes {
		return "", 0, fmt.Errorf("q must be at most %d characters", maxPlaceQueryRunes)
	}
	if raw := values.Get("limit"); raw != "" {
		limit, err = strconv.Atoi(raw)
		if err != nil {
			return "", 0, errors.New("limit must be a number")
		}
	}
	return query, mapy.ClampGeocodeLimit(limit), nil
}

// placeKey builds the cache key for a place search. The query is casefolded and
// its internal runs of whitespace collapsed, so "Veselí nad  Moravou" and "veselí
// nad Moravou" share one cached answer — but diacritics are kept, because "veseli"
// and "veselí" are genuinely different questions to ask mapy.com and folding them
// together would serve one the other's answer. The limit is part of the key: the
// same name asked for more rows is a different answer.
func placeKey(query string, limit int) string {
	return strconv.Itoa(limit) + ":" + strings.ToLower(strings.Join(strings.Fields(query), " "))
}
