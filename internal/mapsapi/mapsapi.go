// Package mapsapi exposes the maps HTTP API: a mapy.com tile proxy and reverse-
// geocode proxy (so the API key never reaches the browser) plus a GeoJSON feed of
// geotagged photos for the map view. The tile and geocode endpoints stream/relay
// answers from mapy.com via the mapy client; the GeoJSON endpoint reads the photo
// catalogue honouring the standard list filters. Route guarding is injected as
// middleware so the package stays decoupled from the auth subsystem's wiring.
//
// Successful tiles are cached server-side (bounded, TTL'd, successes only), so a
// second look at an area a user has already browsed costs no mapy.com credit;
// upstream failures are never cached. Every upstream outcome is recorded on the
// mapy.Health tracker, and a rejected API key is relayed as its own status
// (StatusMapKeyRejected) rather than a generic upstream error, so the map view can
// say why the tiles are missing instead of showing a silent grey grid.
package mapsapi

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/panbotka/kukatko/internal/mapy"
	"github.com/panbotka/kukatko/internal/photos"
)

// Defaults applied when the corresponding Config field is left zero.
const (
	// defaultTileCacheMaxAge is how long browsers may cache a proxied tile. Tiles
	// for a given z/x/y are effectively immutable, so this is long.
	defaultTileCacheMaxAge = 24 * time.Hour
	// defaultTileCacheTTL is how long a tile stays in the server-side cache. Tiles
	// change rarely, and every cache hit is one mapy.com credit not spent.
	defaultTileCacheTTL = 24 * time.Hour
	// defaultTileCacheBytes is the server-side tile cache's memory budget. At the
	// typical few tens of kilobytes per tile this holds a few thousand tiles, i.e.
	// several full screens of every mapset a user has browsed.
	defaultTileCacheBytes = 64 << 20
	// defaultGeocodeCacheTTL is how long a reverse-geocode answer is reused before
	// a coordinate is looked up again, conserving the 4-credit geocode cost.
	defaultGeocodeCacheTTL = 24 * time.Hour
	// defaultGeocodeRatePerSec caps how many distinct reverse-geocode lookups reach
	// mapy.com per second, well under the provider's 200/s, to conserve credits.
	defaultGeocodeRatePerSec = 5
	// defaultGeocodeRateBurst is the geocode rate limiter's bucket size.
	defaultGeocodeRateBurst = 10
	// defaultGeocodeCacheSize bounds the reverse-geocode cache entry count.
	defaultGeocodeCacheSize = 10000
	// defaultMaxGeoPhotos caps how many geotagged photos one GeoJSON response may
	// carry, bounding the work a single request can demand.
	defaultMaxGeoPhotos = 50000
)

// TileFetcher fetches a single map tile. mapy.Client satisfies it; a test fake
// can stand in.
type TileFetcher interface {
	// Tile fetches the tile named by params and returns its streamed body and
	// metadata. The caller must close the result body.
	Tile(ctx context.Context, params mapy.TileParams) (*mapy.TileResult, error)
}

// Geocoder reverse-geocodes a coordinate. mapy.Client satisfies it; a test fake
// can stand in.
type Geocoder interface {
	// ReverseGeocode resolves lat/lng to a simplified location.
	ReverseGeocode(ctx context.Context, lat, lng float64) (*mapy.GeocodeResult, error)
}

// PhotoLister is the subset of the photos repository the GeoJSON endpoint needs:
// listing photos under a set of filters. photos.Store satisfies it.
type PhotoLister interface {
	// List returns photos matching params, ordered and paginated as requested.
	List(ctx context.Context, params photos.ListParams) ([]photos.Photo, error)
}

// API exposes the maps endpoints over HTTP. The route guard is supplied by the
// caller (the auth subsystem) so this package depends on auth for the caller's
// identity, not its wiring.
type API struct {
	tiles           TileFetcher
	geocoder        Geocoder
	photos          PhotoLister
	health          *mapy.Health
	requireAuth     func(http.Handler) http.Handler
	tileRateLimit   func(http.Handler) http.Handler
	tileCacheMaxAge time.Duration
	tileCacheTTL    time.Duration
	tileCache       *tileCache
	geocodeCacheTTL time.Duration
	geocodeCache    *geocodeCache
	geocodeLimiter  *rateLimiter
	maxGeoPhotos    int
}

// Config bundles the dependencies of NewAPI. Tiles and Geocoder may be nil (their
// endpoints then answer 503), reflecting an unconfigured mapy.com key; Photos and
// RequireAuth are required.
type Config struct {
	// Tiles backs the tile proxy. When nil the tile endpoint answers 503.
	Tiles TileFetcher
	// Geocoder backs the reverse-geocode proxy. When nil that endpoint answers 503.
	Geocoder Geocoder
	// Photos backs the GeoJSON endpoint.
	Photos PhotoLister
	// RequireAuth guards every endpoint for authenticated users.
	RequireAuth func(http.Handler) http.Handler
	// TileRateLimit is an optional per-client-IP throttle on the tile proxy,
	// applied ahead of the auth check. A nil value disables throttling. The
	// geocode proxy keeps its own credit-protecting limiter (GeocodeRatePerSec).
	TileRateLimit func(http.Handler) http.Handler
	// Health records the outcome of every upstream tile and geocode call, so the
	// admin status dashboard can report a rejected key. Nil (the default) means
	// no key is configured and nothing is tracked; the tracker is nil-safe.
	Health *mapy.Health
	// TileCacheMaxAge sets the Cache-Control max-age on proxied tiles (default
	// defaultTileCacheMaxAge).
	TileCacheMaxAge time.Duration
	// TileCacheTTL sets how long a tile stays in the server-side cache (default
	// defaultTileCacheTTL). A negative value disables the cache.
	TileCacheTTL time.Duration
	// TileCacheBytes is the server-side tile cache's memory budget in bytes
	// (default defaultTileCacheBytes). A negative value disables the cache.
	TileCacheBytes int64
	// GeocodeCacheTTL sets how long reverse-geocode answers are cached (default
	// defaultGeocodeCacheTTL). A non-positive value disables caching.
	GeocodeCacheTTL time.Duration
	// GeocodeRatePerSec caps reverse-geocode lookups to mapy.com per second
	// (default defaultGeocodeRatePerSec). A non-positive value disables the limiter.
	GeocodeRatePerSec float64
	// GeocodeRateBurst is the geocode rate limiter's burst (default
	// defaultGeocodeRateBurst).
	GeocodeRateBurst int
	// MaxGeoPhotos caps the GeoJSON feature count (default defaultMaxGeoPhotos).
	MaxGeoPhotos int
}

// NewAPI returns an API from cfg, applying defaults for any zero-valued tunable.
func NewAPI(cfg Config) *API {
	tileMaxAge := cfg.TileCacheMaxAge
	if tileMaxAge <= 0 {
		tileMaxAge = defaultTileCacheMaxAge
	}
	geoTTL := defaultGeocodeCacheTTL
	if cfg.GeocodeCacheTTL != 0 {
		geoTTL = cfg.GeocodeCacheTTL
	}
	ratePerSec := float64(defaultGeocodeRatePerSec)
	if cfg.GeocodeRatePerSec != 0 {
		ratePerSec = cfg.GeocodeRatePerSec
	}
	burst := cfg.GeocodeRateBurst
	if burst <= 0 {
		burst = defaultGeocodeRateBurst
	}
	maxGeo := cfg.MaxGeoPhotos
	if maxGeo <= 0 {
		maxGeo = defaultMaxGeoPhotos
	}
	tileRateLimit := cfg.TileRateLimit
	if tileRateLimit == nil {
		tileRateLimit = passthroughMiddleware
	}
	tileTTL := defaultTileCacheTTL
	if cfg.TileCacheTTL != 0 {
		tileTTL = cfg.TileCacheTTL
	}
	tileBytes := int64(defaultTileCacheBytes)
	if cfg.TileCacheBytes != 0 {
		tileBytes = cfg.TileCacheBytes
	}
	return &API{
		tiles:           cfg.Tiles,
		geocoder:        cfg.Geocoder,
		photos:          cfg.Photos,
		health:          cfg.Health,
		requireAuth:     cfg.RequireAuth,
		tileRateLimit:   tileRateLimit,
		tileCacheMaxAge: tileMaxAge,
		tileCacheTTL:    tileTTL,
		tileCache:       newTileCache(tileBytes),
		geocodeCacheTTL: geoTTL,
		geocodeCache:    newGeocodeCache(defaultGeocodeCacheSize),
		geocodeLimiter:  newRateLimiter(ratePerSec, burst),
		maxGeoPhotos:    maxGeo,
	}
}

// passthroughMiddleware is a no-op middleware used when no tile rate limiter is configured.
func passthroughMiddleware(next http.Handler) http.Handler { return next }

// RegisterRoutes mounts the maps endpoints onto r, which the caller has scoped
// under the API base path (for example /api/v1). Every route requires auth:
//
//	GET /map/tiles/{mapset}/{z}/{x}/{y} proxied mapy.com tile (key added server-side)
//	GET /map/rgeocode?lat=&lng=         reverse geocode (cached, rate-limited)
//	GET /map/photos                     GeoJSON FeatureCollection of geotagged photos
func (a *API) RegisterRoutes(r chi.Router) {
	r.Route("/map", func(r chi.Router) {
		r.With(a.tileRateLimit, a.requireAuth).Get("/tiles/{mapset}/{z}/{x}/{y}", a.handleTile)
		r.With(a.requireAuth).Get("/rgeocode", a.handleReverseGeocode)
		r.With(a.requireAuth).Get("/photos", a.handlePhotos)
	})
}

// errorBody is the JSON body returned for error responses.
type errorBody struct {
	Error string `json:"error"`
}

// writeJSON writes payload as a JSON response with the given status code.
func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(payload); err != nil {
		log.Printf("mapsapi: encoding JSON response: %v", err)
	}
}

// writeError writes an error response with the given status code and message.
func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, errorBody{Error: message})
}
