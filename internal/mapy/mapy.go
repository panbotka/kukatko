// Package mapy is Kukátko's server-side HTTP client to the mapy.com REST API. It
// proxies map tiles, reverse geocoding (coordinate → place) and forward geocoding
// (place name → coordinates) so the API key never reaches the browser: the key is
// sent only in the X-Mapy-Api-Key request header and never appears in a returned
// URL or error.
//
// Every request also carries a configurable User-Agent (Config.UserAgent). mapy.com
// can restrict a key to one exact User-Agent, so that string is a credential too:
// like the API key, it must never be logged or surfaced in an error.
//
// The client classifies upstream failures into sentinel errors (ErrUnauthorized,
// ErrNotFound, ErrRateLimited, ErrUpstream, ErrUnavailable) so the HTTP layer can
// map them to sane client-facing statuses without leaking provider internals.
// Every non-200 is also logged at WARN with its upstream status, so a rejected
// key never shows up as nothing but a grey tile; Health folds those outcomes into
// a state the admin status dashboard can report. Tile bytes are streamed straight
// from the upstream response body, never buffered whole in memory.
//
// Everything sits behind the Client interface so the HTTP API and its tests can
// substitute a fake without a real network or a real key.
package mapy

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// Defaults for the mapy.com client.
const (
	// DefaultBaseURL is the root of the mapy.com REST API.
	DefaultBaseURL = "https://api.mapy.com"
	// DefaultTimeout bounds a single tile or geocode request.
	DefaultTimeout = 15 * time.Second
	// DefaultLang is the language requested for reverse geocoding (Czech, the UI
	// default).
	DefaultLang = "cs"
	// apiKeyHeader carries the secret key to mapy.com. It is the only place the
	// key appears, so URLs and errors stay safe to surface to clients.
	//nolint:gosec // G101: this is the name of an HTTP header, not a credential.
	apiKeyHeader = "X-Mapy-Api-Key"
	// tileSize is the standard tile edge in pixels; retina doubles it as a "@2x"
	// suffix.
	tileSize = "256"
	// DefaultGeocodeLimit is how many forward-geocoding suggestions are asked for
	// when a caller names no limit: enough to disambiguate the several Veselís,
	// short enough to read at a glance.
	DefaultGeocodeLimit = 5
	// MaxGeocodeLimit caps the suggestions one forward-geocoding call may ask for.
	// Each call costs mapy.com credits regardless of the count, but a typeahead
	// has no use for a hundred rows and the cap keeps one caller from demanding
	// them.
	MaxGeocodeLimit = 15
)

// Sentinel errors classifying an upstream outcome. They never carry the API key
// (it is sent only as a header), so they are safe to wrap and surface.
var (
	// ErrInvalidURL indicates the configured base URL is not a usable HTTP(S) URL.
	ErrInvalidURL = errors.New("mapy: invalid base URL")
	// ErrInvalidMapset indicates a tile was requested for an unsupported mapset.
	ErrInvalidMapset = errors.New("mapy: unsupported mapset")
	// ErrEmptyQuery indicates forward geocoding was asked for a blank place name.
	// It is caught before any upstream call, so an empty typeahead never costs a
	// credit.
	ErrEmptyQuery = errors.New("mapy: empty geocode query")
	// ErrUnauthorized indicates mapy.com rejected the API key (HTTP 401/403). It
	// is a server-side configuration problem, not a client one.
	ErrUnauthorized = errors.New("mapy: upstream rejected the API key")
	// ErrNotFound indicates the requested tile or location does not exist (404).
	ErrNotFound = errors.New("mapy: upstream resource not found")
	// ErrRateLimited indicates the monthly credit or per-second rate cap was hit
	// (429). Callers should back off.
	ErrRateLimited = errors.New("mapy: upstream rate limit exceeded")
	// ErrUpstream indicates mapy.com returned an otherwise unexpected status or an
	// unparseable body.
	ErrUpstream = errors.New("mapy: upstream error")
	// ErrUnavailable indicates mapy.com could not be reached (transport failure or
	// gateway-style 502/503/504).
	ErrUnavailable = errors.New("mapy: upstream unavailable")
)

// validMapsets is the allow-list of tile mapsets exposed by the proxy. Anything
// outside it is rejected before an upstream call, so a caller can never drive an
// arbitrary path segment into the mapy.com URL.
var validMapsets = map[string]bool{
	"basic":   true,
	"outdoor": true,
	"aerial":  true,
	"winter":  true,
}

// retinaMapsets is the subset of mapsets for which mapy.com serves @2x retina
// tiles; for the others a retina request falls back to the standard tile.
var retinaMapsets = map[string]bool{
	"basic":   true,
	"outdoor": true,
}

// IsValidMapset reports whether mapset is on the tile allow-list.
func IsValidMapset(mapset string) bool {
	return validMapsets[mapset]
}

// RetinaSupported reports whether mapy.com serves retina (@2x) tiles for mapset.
func RetinaSupported(mapset string) bool {
	return retinaMapsets[mapset]
}

// TileParams identifies a single map tile to fetch. Z, X and Y are the slippy-map
// coordinates; Retina requests the @2x variant where the mapset supports it.
type TileParams struct {
	Mapset string
	Z      int
	X      int
	Y      int
	Retina bool
}

// TileResult carries a tile's streamed body and the metadata needed to relay it
// to the browser. The caller owns Body and must close it.
type TileResult struct {
	// Body streams the tile bytes from the upstream response; never buffered whole.
	Body io.ReadCloser
	// ContentType is the upstream image MIME type (e.g. image/png).
	ContentType string
	// ContentLength is the upstream byte count, or -1 when unknown.
	ContentLength int64
}

// RegionalItem is one component of a reverse-geocoded address (e.g. a street, a
// municipality, a country) with its mapy.com type tag.
type RegionalItem struct {
	Name string `json:"name"`
	Type string `json:"type"`
}

// GeocodeResult is the simplified reverse-geocode answer surfaced to clients: the
// best match's name, its human-readable location string and the regional
// breakdown, with all provider-internal fields dropped.
type GeocodeResult struct {
	Name              string         `json:"name"`
	Location          string         `json:"location"`
	RegionalStructure []RegionalItem `json:"regional_structure"`
}

// Place is one forward-geocoding suggestion: a place mapy.com matched a name to,
// with everything needed to tell it from its namesakes and to drop a pin on it.
// Provider-internal fields (bbox, zip, the regional breakdown) are dropped.
type Place struct {
	// Name is the place's own name, e.g. "Veselí nad Moravou".
	Name string `json:"name"`
	// Label is the localised name of the kind of place, as mapy.com writes it in
	// the requested language, e.g. "Město" / "Zámek". It is display text, not a
	// value to branch on — see Type for that.
	Label string `json:"label"`
	// Type is mapy.com's machine-readable kind, e.g. "regional.municipality",
	// "regional.street", "regional.region" or "poi".
	Type string `json:"type"`
	// Location disambiguates the name by naming what contains it, from the
	// narrowest upwards, e.g. "Veselí nad Moravou, okres Hodonín, Jihomoravský
	// kraj, Česko". It is empty for a place with nothing above it (a country).
	Location string `json:"location"`
	// Lat is the suggestion's latitude in decimal degrees.
	Lat float64 `json:"lat"`
	// Lng is the suggestion's longitude in decimal degrees.
	Lng float64 `json:"lng"`
}

// ClampGeocodeLimit bounds a requested suggestion count to [1, MaxGeocodeLimit],
// mapping a non-positive limit to DefaultGeocodeLimit. Callers at the HTTP
// boundary use it to normalise a query parameter (so an absurd limit is answered,
// not rejected) and it is applied again inside Geocode, so no call site can drive
// an unbounded count upstream.
func ClampGeocodeLimit(limit int) int {
	switch {
	case limit <= 0:
		return DefaultGeocodeLimit
	case limit > MaxGeocodeLimit:
		return MaxGeocodeLimit
	default:
		return limit
	}
}

// Client is the mapy.com proxy contract: stream a tile, reverse-geocode a
// coordinate and forward-geocode a place name. It is an interface so the HTTP API
// and tests can substitute a fake.
type Client interface {
	// Tile fetches the tile named by params and returns its streamed body and
	// metadata, or a classified sentinel error. The caller must close the result
	// body.
	Tile(ctx context.Context, params TileParams) (*TileResult, error)
	// ReverseGeocode resolves lat/lng to a simplified location, or a classified
	// sentinel error. It returns ErrNotFound when mapy.com has no match.
	ReverseGeocode(ctx context.Context, lat, lng float64) (*GeocodeResult, error)
	// Geocode resolves a free-text place name to at most limit ranked
	// suggestions, best match first, or a classified sentinel error. A name it
	// matches nothing for yields an empty slice, not an error.
	Geocode(ctx context.Context, query string, limit int) ([]Place, error)
}

// Config configures an HTTPClient. APIKey is required for live use; BaseURL falls
// back to DefaultBaseURL and Timeout to DefaultTimeout when left zero.
type Config struct {
	// BaseURL is the root of the mapy.com REST API (default DefaultBaseURL).
	BaseURL string
	// APIKey is the secret mapy.com key, sent only via the X-Mapy-Api-Key header.
	APIKey string
	// UserAgent is the exact User-Agent sent on every upstream request. mapy.com
	// can restrict a key to one exact User-Agent, which makes the value a second
	// secret: treat it like APIKey and never log it. Empty means "send no explicit
	// header" — Go's default applies and the key must then be restricted otherwise
	// (e.g. by source IP).
	UserAgent string
	// Lang is the reverse-geocoding language (default DefaultLang).
	Lang string
	// Timeout bounds a single request (default DefaultTimeout).
	Timeout time.Duration
	// HTTPClient lets callers inject a custom client; a default one is used when
	// nil. Per-request deadlines are applied via context.
	HTTPClient *http.Client
}

// HTTPClient is the production Client backed by the mapy.com REST API.
type HTTPClient struct {
	baseURL   *url.URL
	apiKey    string
	userAgent string
	lang      string
	timeout   time.Duration
	client    *http.Client
}

// compile-time assertion that HTTPClient satisfies Client.
var _ Client = (*HTTPClient)(nil)

// New builds an HTTPClient from cfg. It returns ErrInvalidURL when BaseURL is not
// a valid HTTP(S) URL with a host.
func New(cfg Config) (*HTTPClient, error) {
	base := cfg.BaseURL
	if base == "" {
		base = DefaultBaseURL
	}
	parsed, err := url.Parse(strings.TrimSuffix(base, "/"))
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrInvalidURL, err)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return nil, fmt.Errorf("%w: scheme %q must be http or https", ErrInvalidURL, parsed.Scheme)
	}
	if parsed.Host == "" {
		return nil, fmt.Errorf("%w: missing host", ErrInvalidURL)
	}
	lang := cfg.Lang
	if lang == "" {
		lang = DefaultLang
	}
	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = DefaultTimeout
	}
	client := cfg.HTTPClient
	if client == nil {
		client = &http.Client{}
	}
	return &HTTPClient{
		baseURL:   parsed,
		apiKey:    cfg.APIKey,
		userAgent: cfg.UserAgent,
		lang:      lang,
		timeout:   timeout,
		client:    client,
	}, nil
}

// Tile fetches a map tile from mapy.com, streaming its body back. It returns
// ErrInvalidMapset for a mapset outside the allow-list before any upstream call,
// and otherwise classifies the upstream status into a sentinel error. The caller
// must close the returned body.
func (c *HTTPClient) Tile(ctx context.Context, params TileParams) (*TileResult, error) {
	if !IsValidMapset(params.Mapset) {
		return nil, fmt.Errorf("%w: %q", ErrInvalidMapset, params.Mapset)
	}
	size := tileSize
	if params.Retina && RetinaSupported(params.Mapset) {
		size = tileSize + "@2x"
	}
	reqURL := c.baseURL.JoinPath(
		"v1", "maptiles", params.Mapset, size,
		strconv.Itoa(params.Z), strconv.Itoa(params.X), strconv.Itoa(params.Y),
	)

	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	resp, err := c.do(ctx, reqURL, "tile")
	if err != nil {
		cancel()
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		err := statusError("tile", resp.StatusCode)
		// A grey tile in the browser is otherwise the only trace of an upstream
		// failure, so carry the status and the mapset into the log.
		slog.WarnContext(ctx, "mapy: tile request failed",
			"status", resp.StatusCode, "mapset", params.Mapset,
			"z", params.Z, "x", params.X, "y", params.Y, "error", err)
		_ = resp.Body.Close()
		cancel()
		return nil, err
	}
	contentType := resp.Header.Get("Content-Type")
	if contentType == "" {
		contentType = "image/png"
	}
	// cancel must outlive this call: it fires when the caller closes the body,
	// tearing down the request context once the stream is fully relayed.
	return &TileResult{
		Body:          &cancelReadCloser{rc: resp.Body, cancel: cancel},
		ContentType:   contentType,
		ContentLength: resp.ContentLength,
	}, nil
}

// rgeocodeResponse is the subset of the mapy.com /v1/rgeocode response the proxy
// reads: an ordered list of candidate items, best match first.
type rgeocodeResponse struct {
	Items []rgeocodeItem `json:"items"`
}

// rgeocodeItem is one reverse-geocode candidate; only the fields surfaced in the
// simplified result are decoded.
type rgeocodeItem struct {
	Name              string         `json:"name"`
	Location          string         `json:"location"`
	RegionalStructure []RegionalItem `json:"regionalStructure"`
}

// ReverseGeocode resolves a coordinate to its nearest named place via the
// mapy.com /v1/rgeocode endpoint, returning the simplified best match. It returns
// ErrNotFound when mapy.com has no candidate for the coordinate.
func (c *HTTPClient) ReverseGeocode(ctx context.Context, lat, lng float64) (*GeocodeResult, error) {
	reqURL := c.baseURL.JoinPath("v1", "rgeocode")
	q := url.Values{}
	q.Set("lon", strconv.FormatFloat(lng, 'f', -1, 64))
	q.Set("lat", strconv.FormatFloat(lat, 'f', -1, 64))
	q.Set("lang", c.lang)
	reqURL.RawQuery = q.Encode()

	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	resp, err := c.do(ctx, reqURL, "rgeocode")
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		err := statusError("rgeocode", resp.StatusCode)
		if resp.StatusCode != http.StatusNotFound {
			// 404 is a normal "no place here" answer; anything else is a real
			// upstream failure worth a line in the log with its status.
			slog.WarnContext(ctx, "mapy: reverse geocode failed",
				"status", resp.StatusCode, "error", err)
		}
		return nil, err
	}

	var decoded rgeocodeResponse
	if err := json.NewDecoder(resp.Body).Decode(&decoded); err != nil {
		return nil, fmt.Errorf("rgeocode: %w: decoding response: %w", ErrUpstream, err)
	}
	if len(decoded.Items) == 0 {
		return nil, ErrNotFound
	}
	item := decoded.Items[0]
	return &GeocodeResult{
		Name:              item.Name,
		Location:          item.Location,
		RegionalStructure: item.RegionalStructure,
	}, nil
}

// geocodeResponse is the subset of the mapy.com /v1/geocode response the proxy
// reads: an ordered list of matches, best first.
type geocodeResponse struct {
	Items []geocodeItem `json:"items"`
}

// geocodeItem is one forward-geocode match; only the fields surfaced in Place are
// decoded.
type geocodeItem struct {
	Name     string          `json:"name"`
	Label    string          `json:"label"`
	Type     string          `json:"type"`
	Location string          `json:"location"`
	Position geocodePosition `json:"position"`
}

// geocodePosition is a match's coordinate as mapy.com spells it (lon before lat).
type geocodePosition struct {
	Lon float64 `json:"lon"`
	Lat float64 `json:"lat"`
}

// Geocode resolves a free-text place name to ranked coordinate suggestions via
// the mapy.com /v1/geocode endpoint, asking for at most ClampGeocodeLimit(limit)
// of them in the client's configured language. A blank query returns ErrEmptyQuery
// without an upstream call, so an empty typeahead never spends a credit.
//
// A name mapy.com matches nothing for is an empty slice and a nil error, not
// ErrNotFound: "no suggestions yet" is the normal answer to a half-typed name, and
// the caller must not treat it as a failure.
func (c *HTTPClient) Geocode(ctx context.Context, query string, limit int) ([]Place, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, ErrEmptyQuery
	}
	reqURL := c.baseURL.JoinPath("v1", "geocode")
	q := url.Values{}
	q.Set("query", query)
	q.Set("lang", c.lang)
	q.Set("limit", strconv.Itoa(ClampGeocodeLimit(limit)))
	reqURL.RawQuery = q.Encode()

	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	resp, err := c.do(ctx, reqURL, "geocode")
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		err := statusError("geocode", resp.StatusCode)
		if resp.StatusCode != http.StatusNotFound {
			// 404 is a normal "no such place"; anything else is a real upstream
			// failure worth a line in the log with its status. The query is left
			// out: it is the user's own text, and the status is what explains the
			// failure.
			slog.WarnContext(ctx, "mapy: geocode failed", "status", resp.StatusCode, "error", err)
		}
		return nil, err
	}

	var decoded geocodeResponse
	if err := json.NewDecoder(resp.Body).Decode(&decoded); err != nil {
		return nil, fmt.Errorf("geocode: %w: decoding response: %w", ErrUpstream, err)
	}
	places := make([]Place, 0, len(decoded.Items))
	for _, item := range decoded.Items {
		places = append(places, Place{
			Name:     item.Name,
			Label:    item.Label,
			Type:     item.Type,
			Location: item.Location,
			Lat:      item.Position.Lat,
			Lng:      item.Position.Lon,
		})
	}
	return places, nil
}

// do issues an authenticated GET to reqURL, attaching the API key header and, when
// configured, the User-Agent. Every upstream call (tile and rgeocode alike) goes
// through here, so no call site can forget either header. It returns the live
// response (whose body the caller must close) or a transport error classified as
// ErrUnavailable. op is used only for error context.
func (c *HTTPClient) do(ctx context.Context, reqURL *url.URL, op string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("%s: build request: %w", op, err)
	}
	req.Header.Set(apiKeyHeader, c.apiKey)
	if c.userAgent != "" {
		req.Header.Set("User-Agent", c.userAgent)
	}
	resp, err := c.client.Do(req)
	if err != nil {
		if cerr := ctx.Err(); cerr != nil && errors.Is(cerr, context.Canceled) {
			return nil, fmt.Errorf("%s: %w", op, cerr)
		}
		return nil, fmt.Errorf("%s: %w: %w", op, ErrUnavailable, err)
	}
	return resp, nil
}

// statusError maps a non-200 upstream status to a sentinel error. The body is not
// included so the API key (which mapy.com sometimes echoes in error payloads) can
// never leak through. op is used only for error context.
func statusError(op string, code int) error {
	switch code {
	case http.StatusUnauthorized, http.StatusForbidden:
		return fmt.Errorf("%s: %w (status %d)", op, ErrUnauthorized, code)
	case http.StatusNotFound:
		return fmt.Errorf("%s: %w (status %d)", op, ErrNotFound, code)
	case http.StatusTooManyRequests:
		return fmt.Errorf("%s: %w (status %d)", op, ErrRateLimited, code)
	case http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout:
		return fmt.Errorf("%s: %w (status %d)", op, ErrUnavailable, code)
	default:
		return fmt.Errorf("%s: %w (status %d)", op, ErrUpstream, code)
	}
}

// cancelReadCloser wraps a response body so closing it also cancels the request
// context, releasing resources once the tile stream has been fully relayed.
type cancelReadCloser struct {
	rc     io.ReadCloser
	cancel context.CancelFunc
}

// Read delegates to the wrapped body. The error is returned unwrapped so callers
// (e.g. io.Copy) keep seeing the sentinel io.EOF.
func (c *cancelReadCloser) Read(p []byte) (int, error) {
	return c.rc.Read(p) //nolint:wrapcheck // must pass io.EOF through verbatim.
}

// Close closes the wrapped body and cancels the request context.
func (c *cancelReadCloser) Close() error {
	err := c.rc.Close()
	c.cancel()
	return err //nolint:wrapcheck // relays the underlying body's Close error verbatim.
}
