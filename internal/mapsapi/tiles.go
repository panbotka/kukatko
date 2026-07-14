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

// StatusMapKeyRejected is the status the tile and reverse-geocode proxies answer
// with when mapy.com rejects *our* API key (its 401/403). The upstream status is
// deliberately not passed through: a 401/403 would tell the browser the caller's
// own request was unauthorised, when in truth the caller is fine and the server's
// key is expired, revoked or over quota. 424 Failed Dependency says exactly that
// — the request failed because a dependency of ours did — and gives the frontend
// a status it can recognise to explain the empty map instead of rendering grey
// tiles.
const StatusMapKeyRejected = http.StatusFailedDependency

// tileCacheHeader reports whether a tile came from the server-side cache ("hit")
// or from mapy.com ("miss"). It exists for operators and tests; the browser
// ignores it.
const tileCacheHeader = "X-Tile-Cache"

// handleTile proxies a single mapy.com map tile, adding the API key server-side
// and streaming the bytes back with long-lived cache headers. The mapset is
// validated against the allow-list and z/x/y must be in-range integers (otherwise
// 400). The optional retina @2x variant is requested via a "@2x" suffix on the y
// segment or a retina=true query parameter, and applied only where the mapset
// supports it. A tile already in the server-side cache is served straight from
// memory, costing no mapy.com credit. Upstream failures map to sane statuses
// without leaking the key — a rejected key becomes StatusMapKeyRejected — and an
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

	key := tileCacheKey(params)
	if entry, ok := a.tileCache.get(key); ok {
		a.writeTileHeaders(w, entry.contentType, int64(len(entry.body)), "hit")
		if _, err := w.Write(entry.body); err != nil {
			log.Printf("mapsapi: writing cached tile: %v", err)
		}
		return
	}

	res, err := a.tiles.Tile(r.Context(), params)
	a.health.Record(err)
	if err != nil {
		writeTileError(w, err)
		return
	}
	defer func() { _ = res.Body.Close() }()
	a.relayTile(w, key, res)
}

// relayTile streams an upstream tile to the client, caching it on the way when it
// is small enough to be worth holding. The body is read into memory only up to
// maxCachedTileBytes: a tile above that limit is relayed as a stream and left
// uncached, so an unexpectedly huge response can never be buffered whole.
func (a *API) relayTile(w http.ResponseWriter, key string, res *mapy.TileResult) {
	head, err := io.ReadAll(io.LimitReader(res.Body, maxCachedTileBytes+1))
	if err != nil {
		// Nothing has been written yet, so the failure is still reportable.
		a.health.Record(fmt.Errorf("%w: reading tile body: %w", mapy.ErrUpstream, err))
		writeError(w, http.StatusBadGateway, "map provider error")
		return
	}

	if len(head) <= maxCachedTileBytes {
		// The whole tile is in hand: cache it (successes only) and serve it.
		a.tileCache.set(key, head, res.ContentType, a.tileCacheTTL)
		a.writeTileHeaders(w, res.ContentType, int64(len(head)), "miss")
		if _, err := w.Write(head); err != nil {
			log.Printf("mapsapi: writing tile: %v", err)
		}
		return
	}

	a.writeTileHeaders(w, res.ContentType, res.ContentLength, "miss")
	if _, err := w.Write(head); err == nil {
		if _, err := io.Copy(w, res.Body); err != nil {
			// The header and status are already sent, so we can only log a mid-stream
			// failure (typically the client going away).
			log.Printf("mapsapi: streaming tile: %v", err)
		}
	}
}

// writeTileHeaders writes the tile response headers and a 200 status: the image
// content type, the byte count when known, the browser cache lifetime and the
// server-side cache outcome.
func (a *API) writeTileHeaders(w http.ResponseWriter, contentType string, length int64, cacheState string) {
	w.Header().Set("Content-Type", contentType)
	if length >= 0 {
		w.Header().Set("Content-Length", strconv.FormatInt(length, 10))
	}
	w.Header().Set("Cache-Control", fmt.Sprintf("public, max-age=%d, immutable", int(a.tileCacheMaxAge.Seconds())))
	w.Header().Set(tileCacheHeader, cacheState)
	w.WriteHeader(http.StatusOK)
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
// exposing the API key or provider internals: a rejected key (a server-side
// misconfiguration the caller cannot fix) becomes StatusMapKeyRejected, other
// upstream failures 502, an unreachable provider 503, a missing tile 404, and a
// hit rate limit 429.
func writeTileError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, mapy.ErrInvalidMapset):
		writeError(w, http.StatusBadRequest, "unknown mapset")
	case errors.Is(err, mapy.ErrNotFound):
		writeError(w, http.StatusNotFound, "tile not found")
	case errors.Is(err, mapy.ErrUnauthorized):
		writeError(w, StatusMapKeyRejected, "map provider rejected the server's API key")
	case errors.Is(err, mapy.ErrRateLimited):
		writeError(w, http.StatusTooManyRequests, "map provider rate limit exceeded")
	case errors.Is(err, mapy.ErrUnavailable):
		writeError(w, http.StatusServiceUnavailable, "map provider unavailable")
	default:
		// ErrUpstream: a problem the client cannot fix and must not see details of.
		writeError(w, http.StatusBadGateway, "map provider error")
	}
}
