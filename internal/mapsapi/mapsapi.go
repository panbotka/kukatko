// Package mapsapi exposes the maps HTTP API: a mapy.com tile proxy, a reverse-
// geocode proxy (coordinate → place) and a place-search proxy (name →
// coordinates), so the API key never reaches the browser, plus a GeoJSON feed of
// geotagged photos for the map view. The tile and geocode endpoints stream/relay
// answers from mapy.com via the mapy client; the GeoJSON endpoint reads the photo
// catalogue honouring the standard list filters. Route guarding is injected as
// middleware so the package stays decoupled from the auth subsystem's wiring.
//
// Everything mapy.com answers is metered in credits, so every proxied endpoint is
// cached server-side (bounded, TTL'd, successes only; failures are never cached)
// and the two geocode directions additionally share one rate limiter, because they
// share one monthly budget. That is what makes a second look at an already-browsed
// area, or a typeahead firing as someone types a place name, cost nothing.
//
// The budget is the instance's but the libraries are many, so geocoding is also
// fair between projects: every project has its own bucket in front of the
// instance-wide one, and the geocode caches are keyed by project, so neither the
// credits nor the "somebody already asked about this place" a cache hit implies
// cross from one project to another.
//
// Every upstream outcome is recorded on the mapy.Health tracker, and a rejected
// API key is relayed as its own status (StatusMapKeyRejected) rather than a
// generic upstream error, so the map view can say why the tiles are missing
// instead of showing a silent grey grid.
package mapsapi

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/panbotka/kukatko/internal/mapy"
	"github.com/panbotka/kukatko/internal/photos"
	"github.com/panbotka/kukatko/internal/ratelimit"
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
	// defaultGeocodeProjectRatePerSec is one project's share of the instance's
	// geocode rate: enough for a person typing into the place search (the client
	// debounces), short of the whole instance budget, so a second project's
	// lookups always find tokens left.
	defaultGeocodeProjectRatePerSec = 3
	// defaultGeocodeProjectRateBurst is one project's geocode bucket size. It
	// matches the instance burst, so on a quiet instance a single project bursts
	// exactly as far as it did before projects had buckets of their own.
	defaultGeocodeProjectRateBurst = 10
	// defaultGeocodeCacheSize bounds the reverse-geocode cache entry count.
	defaultGeocodeCacheSize = 10000
	// defaultPlacesCacheSize bounds the place-search cache entry count. A place
	// search is typed, so the distinct-query space is far smaller than the
	// coordinate space a reverse-geocode cache has to cover — and every prefix of
	// a name someone is typing is a key of its own, which is exactly what makes
	// caching pay here.
	defaultPlacesCacheSize = 2000
	// defaultMaxGeoPhotos caps how many geotagged photos one GeoJSON response may
	// carry, bounding the work a single request can demand.
	defaultMaxGeoPhotos = 50000
	// geoCompressLevel is the gzip level of the GeoJSON feed: the default
	// speed/size trade-off, since level 9 saves a few percent more of a document
	// that is mostly repeated keys and costs several times the CPU.
	geoCompressLevel = 5
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

// PlaceSearcher forward-geocodes a place name to ranked coordinate suggestions.
// mapy.Client satisfies it; a test fake can stand in.
type PlaceSearcher interface {
	// Geocode resolves a free-text place name to at most limit suggestions.
	Geocode(ctx context.Context, query string, limit int) ([]mapy.Place, error)
}

// PhotoLister is the subset of the photos repository the GeoJSON endpoint needs:
// listing photos under a set of filters, and counting them under the same filters
// with the has-GPS restriction lifted (the map's coverage figure). Both are
// confined to the project named by projectUID. photos.Store satisfies it.
type PhotoLister interface {
	// List returns the project's photos matching params, ordered and paginated
	// as requested.
	List(ctx context.Context, projectUID string, params photos.ListParams) ([]photos.Photo, error)
	// Count returns how many of the project's photos match params, ignoring
	// paging.
	Count(ctx context.Context, projectUID string, params photos.ListParams) (int, error)
}

// API exposes the maps endpoints over HTTP. The route guard is supplied by the
// caller (the auth subsystem) so this package depends on auth for the caller's
// identity, not its wiring.
type API struct {
	tiles           TileFetcher
	geocoder        Geocoder
	places          PlaceSearcher
	photos          PhotoLister
	health          *mapy.Health
	requireAuth     func(http.Handler) http.Handler
	tileRateLimit   func(http.Handler) http.Handler
	tileCacheMaxAge time.Duration
	tileCacheTTL    time.Duration
	tileCache       *tileCache
	geocodeCacheTTL time.Duration
	geocodeCache    *ttlCache[mapy.GeocodeResult]
	placesCache     *ttlCache[[]mapy.Place]
	// geocodeLimiter is the instance-wide geocode bucket (one key), and
	// projectGeocodeLimiter the per-project one in front of it (see
	// allowGeocode).
	geocodeLimiter        *ratelimit.Limiter
	projectGeocodeLimiter *ratelimit.Limiter
	// projectOf names the project a request runs in; requestProject in
	// production, swapped by this package's own tests.
	projectOf    func(*http.Request) string
	maxGeoPhotos int
}

// Config bundles the dependencies of NewAPI. Tiles and Geocoder may be nil (their
// endpoints then answer 503), reflecting an unconfigured mapy.com key; Photos and
// RequireAuth are required.
type Config struct {
	// Tiles backs the tile proxy. When nil the tile endpoint answers 503.
	Tiles TileFetcher
	// Geocoder backs the reverse-geocode proxy. When nil that endpoint answers 503.
	Geocoder Geocoder
	// Places backs the place-search (forward-geocode) proxy. When nil that
	// endpoint answers 503, which the location editor shows as "place search
	// unavailable" while its coordinate and map-click paths keep working.
	Places PlaceSearcher
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
	// GeocodeCacheTTL sets how long geocode answers — both directions — are cached
	// (default defaultGeocodeCacheTTL). A non-positive value disables caching.
	GeocodeCacheTTL time.Duration
	// GeocodeRatePerSec caps geocode lookups to mapy.com per second (default
	// defaultGeocodeRatePerSec). A non-positive value disables the limiter. Both
	// geocode directions draw on this one bucket: they are billed from the same
	// monthly credit budget, so one budget is one limiter.
	GeocodeRatePerSec float64
	// GeocodeRateBurst is the geocode rate limiter's burst (default
	// defaultGeocodeRateBurst).
	GeocodeRateBurst int
	// GeocodeProjectRatePerSec caps one project's geocode lookups to mapy.com
	// per second (default defaultGeocodeProjectRatePerSec), checked before the
	// instance-wide GeocodeRatePerSec. A negative value disables the per-project
	// share, leaving only the instance cap.
	GeocodeProjectRatePerSec float64
	// GeocodeProjectRateBurst is one project's geocode bucket size (default
	// defaultGeocodeProjectRateBurst).
	GeocodeProjectRateBurst int
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
		places:          cfg.Places,
		photos:          cfg.Photos,
		health:          cfg.Health,
		requireAuth:     cfg.RequireAuth,
		tileRateLimit:   tileRateLimit,
		tileCacheMaxAge: tileMaxAge,
		tileCacheTTL:    tileTTL,
		tileCache:       newTileCache(tileBytes),
		geocodeCacheTTL: geoTTL,
		geocodeCache:    newTTLCache[mapy.GeocodeResult](defaultGeocodeCacheSize),
		placesCache:     newTTLCache[[]mapy.Place](defaultPlacesCacheSize),
		geocodeLimiter: newGeocodeLimiter(
			cfg.GeocodeRatePerSec, cfg.GeocodeRateBurst, defaultGeocodeRatePerSec, defaultGeocodeRateBurst),
		projectGeocodeLimiter: newGeocodeLimiter(cfg.GeocodeProjectRatePerSec, cfg.GeocodeProjectRateBurst,
			defaultGeocodeProjectRatePerSec, defaultGeocodeProjectRateBurst),
		projectOf:    requestProject,
		maxGeoPhotos: maxGeo,
	}
}

// newGeocodeLimiter builds one of the geocode limiters from its configured rate
// and burst, falling back to the given defaults: a zero rate takes defRate (a
// negative one disables the limiter), a non-positive burst takes defBurst.
func newGeocodeLimiter(ratePerSec float64, burst int, defRate float64, defBurst int) *ratelimit.Limiter {
	if ratePerSec == 0 {
		ratePerSec = defRate
	}
	if burst <= 0 {
		burst = defBurst
	}
	return ratelimit.New(ratePerSec, burst)
}

// passthroughMiddleware is a no-op middleware used when no tile rate limiter is configured.
func passthroughMiddleware(next http.Handler) http.Handler { return next }

// RegisterRoutes mounts the maps endpoints onto r, which the caller has scoped
// under the API base path (for example /api/v1). Every route requires auth:
//
//	GET /map/tiles/{mapset}/{z}/{x}/{y} proxied mapy.com tile (key added server-side)
//	GET /map/rgeocode?lat=&lng=         reverse geocode (cached per project, rate-limited)
//	GET /map/geocode?q=&limit=          place search (cached per project, rate-limited)
//	GET /map/photos                     GeoJSON FeatureCollection of geotagged photos
func (a *API) RegisterRoutes(r chi.Router) {
	r.Route("/map", func(r chi.Router) {
		r.With(a.tileRateLimit, a.requireAuth).Get("/tiles/{mapset}/{z}/{x}/{y}", a.handleTile)
		r.With(a.requireAuth).Get("/rgeocode", a.handleReverseGeocode)
		r.With(a.requireAuth).Get("/geocode", a.handleGeocode)
		// The feed is the whole located library in one JSON document — ~0.8 MB
		// for prod's 2 456 photos — and nothing in front of the app compresses
		// it, so the route does: gzip takes it to ~0.18 MB (docs/PERF.md).
		r.With(a.requireAuth, middleware.Compress(geoCompressLevel, "application/json")).
			Get("/photos", a.handlePhotos)
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
