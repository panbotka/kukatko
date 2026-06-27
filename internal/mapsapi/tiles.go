package mapsapi

import (
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/panbotka/kukatko/internal/mapy"
)

// maxTileZoom bounds the slippy-map zoom level accepted from a client, rejecting
// nonsensical values before an upstream call.
const maxTileZoom = 22

// handleTile proxies a single mapy.com map tile, adding the API key server-side
// and streaming the bytes back with long-lived cache headers. The mapset is
// validated against the allow-list and z/x/y must be in-range integers (otherwise
// 400). The optional retina @2x variant is requested via a "@2x" suffix on the y
// segment or a retina=true query parameter, and applied only where the mapset
// supports it. Upstream failures map to sane statuses without leaking the key; an
// unconfigured proxy answers 503.
func (a *API) handleTile(w http.ResponseWriter, r *http.Request) {
	if a.tiles == nil {
		writeError(w, http.StatusServiceUnavailable, "map tiles are not configured")
		return
	}
	params, err := parseTileParams(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	res, err := a.tiles.Tile(r.Context(), params)
	if err != nil {
		writeTileError(w, err)
		return
	}
	defer func() { _ = res.Body.Close() }()

	w.Header().Set("Content-Type", res.ContentType)
	if res.ContentLength >= 0 {
		w.Header().Set("Content-Length", strconv.FormatInt(res.ContentLength, 10))
	}
	w.Header().Set("Cache-Control", fmt.Sprintf("public, max-age=%d, immutable", int(a.tileCacheMaxAge.Seconds())))
	w.WriteHeader(http.StatusOK)
	if _, err := io.Copy(w, res.Body); err != nil {
		// The header and status are already sent, so we can only log a mid-stream
		// failure (typically the client going away).
		log.Printf("mapsapi: streaming tile: %v", err)
	}
}

// parseTileParams extracts and validates the tile coordinates and retina flag from
// the request. It returns a descriptive error for an unknown mapset or a
// non-integer/out-of-range coordinate so the caller can answer 400.
func parseTileParams(r *http.Request) (mapy.TileParams, error) {
	mapset := chi.URLParam(r, "mapset")
	if !mapy.IsValidMapset(mapset) {
		return mapy.TileParams{}, fmt.Errorf("unknown mapset %q (want basic, outdoor, aerial or winter)", mapset)
	}

	rawY := chi.URLParam(r, "y")
	retina := false
	if suffix := "@2x"; strings.HasSuffix(rawY, suffix) {
		retina = true
		rawY = strings.TrimSuffix(rawY, suffix)
	}
	if q := r.URL.Query().Get("retina"); q != "" {
		b, err := strconv.ParseBool(q)
		if err != nil {
			return mapy.TileParams{}, errors.New("retina must be true or false")
		}
		retina = retina || b
	}

	z, err := tileCoord(chi.URLParam(r, "z"), "z")
	if err != nil {
		return mapy.TileParams{}, err
	}
	if z > maxTileZoom {
		return mapy.TileParams{}, fmt.Errorf("z must be between 0 and %d", maxTileZoom)
	}
	x, err := tileCoord(chi.URLParam(r, "x"), "x")
	if err != nil {
		return mapy.TileParams{}, err
	}
	y, err := tileCoord(rawY, "y")
	if err != nil {
		return mapy.TileParams{}, err
	}
	return mapy.TileParams{Mapset: mapset, Z: z, X: x, Y: y, Retina: retina}, nil
}

// tileCoord parses a single non-negative tile coordinate, returning a descriptive
// error naming the offending component.
func tileCoord(raw, name string) (int, error) {
	n, err := strconv.Atoi(raw)
	if err != nil {
		return 0, fmt.Errorf("%s must be an integer", name)
	}
	if n < 0 {
		return 0, fmt.Errorf("%s must not be negative", name)
	}
	return n, nil
}

// writeTileError maps a mapy client error to a client-facing status without
// exposing the API key or provider internals: a bad key (a server-side
// misconfiguration) and generic upstream failures become 502, an unreachable
// provider 503, a missing tile 404, and a hit rate limit 429.
func writeTileError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, mapy.ErrInvalidMapset):
		writeError(w, http.StatusBadRequest, "unknown mapset")
	case errors.Is(err, mapy.ErrNotFound):
		writeError(w, http.StatusNotFound, "tile not found")
	case errors.Is(err, mapy.ErrRateLimited):
		writeError(w, http.StatusTooManyRequests, "map provider rate limit exceeded")
	case errors.Is(err, mapy.ErrUnavailable):
		writeError(w, http.StatusServiceUnavailable, "map provider unavailable")
	default:
		// ErrUnauthorized and ErrUpstream both indicate an upstream problem the
		// client cannot fix and must not see details of.
		writeError(w, http.StatusBadGateway, "map provider error")
	}
}
