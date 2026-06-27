// Package photoprism is Kukátko's read-only HTTP client to a running PhotoPrism
// instance. PhotoPrism stays the primary catalog during the migration, so this
// client only ever reads: it lists photos incrementally, reads albums, labels
// and subjects (people), and streams original files for re-import.
//
// Authentication uses a long-lived app password / access token sent as an
// Authorization: Bearer header on every request — never a per-request login,
// because PhotoPrism rate-limits the login endpoint hardest. Downloading an
// original additionally needs a short-lived download token obtained from a
// create-session call; PhotoPrism may rotate it, so the client refreshes the
// token from the X-Download-Token response header whenever it changes and
// re-creates a session if a download is rejected as unauthorized.
//
// Upstream failures are classified into sentinel errors (ErrUnauthorized,
// ErrNotFound, ErrRateLimited, ErrUpstream, ErrUnavailable, ErrBadResponse) so
// callers can react without parsing strings. HTTP 429 is retried with
// exponential backoff (honouring Retry-After). JSON endpoints require an
// application/json response. Original files are streamed straight from the
// upstream body and never buffered whole in memory.
//
// Everything sits behind the Client interface so the importer and its tests can
// substitute a fake without a real PhotoPrism, a real network, or a real token.
package photoprism

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Defaults for the PhotoPrism client.
const (
	// MaxCount is PhotoPrism's hard cap on the count (page size) query parameter;
	// requests are clamped to it. A non-positive requested count uses MaxCount.
	MaxCount = 1000

	// DefaultTimeout bounds a single JSON request (list/session calls). It is not
	// applied to original downloads, whose body may stream for a long time.
	DefaultTimeout = 30 * time.Second
	// DefaultMaxRetries is how many extra attempts a 429 response triggers before
	// the rate-limit error is returned.
	DefaultMaxRetries = 4
	// DefaultRetryBaseDelay is the first backoff delay; it doubles each retry up
	// to DefaultRetryMaxDelay unless the response carries a Retry-After header.
	DefaultRetryBaseDelay = 500 * time.Millisecond
	// DefaultRetryMaxDelay caps the exponential backoff delay.
	DefaultRetryMaxDelay = 30 * time.Second

	// downloadTokenHeader is the response header through which PhotoPrism may
	// rotate the download token.
	downloadTokenHeader = "X-Download-Token"
	// defaultPhotoOrder is the listing order used for incremental pulls so newly
	// updated photos are walked deterministically.
	defaultPhotoOrder = "updated"
)

// Sentinel errors classifying an outcome. They never embed the access token or a
// response body, so they are safe to wrap and surface.
var (
	// ErrInvalidURL indicates the configured base URL is not a usable HTTP(S) URL.
	ErrInvalidURL = errors.New("photoprism: invalid base URL")
	// ErrUnauthorized indicates PhotoPrism rejected the access or download token
	// (HTTP 401/403). It is a configuration problem, not transient.
	ErrUnauthorized = errors.New("photoprism: upstream rejected the token")
	// ErrNotFound indicates the requested resource does not exist (HTTP 404).
	ErrNotFound = errors.New("photoprism: upstream resource not found")
	// ErrRateLimited indicates PhotoPrism's rate limit was hit (HTTP 429) and the
	// backoff retries were exhausted.
	ErrRateLimited = errors.New("photoprism: upstream rate limit exceeded")
	// ErrUpstream indicates PhotoPrism returned an unexpected status.
	ErrUpstream = errors.New("photoprism: upstream error")
	// ErrUnavailable indicates PhotoPrism could not be reached (transport failure
	// or gateway-style 502/503/504).
	ErrUnavailable = errors.New("photoprism: upstream unavailable")
	// ErrBadResponse indicates PhotoPrism was reachable but returned a malformed
	// or non-JSON body where JSON was required.
	ErrBadResponse = errors.New("photoprism: bad response")
)

// PhotoListParams selects a page of photos for an incremental pull. UpdatedSince,
// when non-zero, restricts the listing to photos updated at or after it via the
// q=updated:"<RFC3339>" filter — the basis of repeatable, incremental imports.
type PhotoListParams struct {
	// Count is the page size; it is clamped to MaxCount and defaults to MaxCount
	// when non-positive.
	Count int
	// Offset is the zero-based page offset; negative values are treated as zero.
	Offset int
	// UpdatedSince, when non-zero, filters to photos with UpdatedAt >= it.
	UpdatedSince time.Time
	// Order overrides the listing order; it defaults to "updated".
	Order string
}

// query renders the params as PhotoPrism photo-search query parameters,
// always requesting merged file lists so each photo carries its Files[].
func (p PhotoListParams) query() url.Values {
	order := p.Order
	if order == "" {
		order = defaultPhotoOrder
	}
	q := url.Values{}
	q.Set("count", strconv.Itoa(clampCount(p.Count)))
	q.Set("offset", strconv.Itoa(max(0, p.Offset)))
	q.Set("merged", "true")
	q.Set("order", order)
	if !p.UpdatedSince.IsZero() {
		q.Set("q", `updated:"`+p.UpdatedSince.UTC().Format(time.RFC3339)+`"`)
	}
	return q
}

// ListParams selects a page of a simple list endpoint (albums, labels,
// subjects). Count is clamped to MaxCount; a non-positive Count uses MaxCount.
type ListParams struct {
	Count  int
	Offset int
}

// query renders the params as count/offset query parameters.
func (p ListParams) query() url.Values {
	q := url.Values{}
	q.Set("count", strconv.Itoa(clampCount(p.Count)))
	q.Set("offset", strconv.Itoa(max(0, p.Offset)))
	return q
}

// Download is a streamed original file. The caller owns Body and must close it;
// closing it also releases the underlying request.
type Download struct {
	// Body streams the original bytes from the upstream response; never buffered
	// whole in memory.
	Body io.ReadCloser
	// ContentType is the upstream MIME type, or "application/octet-stream" when
	// the upstream omits it.
	ContentType string
	// ContentLength is the upstream byte count, or -1 when unknown.
	ContentLength int64
}

// Client is the read-only PhotoPrism contract used by the importer. It is an
// interface so the importer and tests can substitute a fake without a real
// PhotoPrism, network, or token.
type Client interface {
	// ListPhotos returns one page of photos for the given params (incremental
	// when UpdatedSince is set). The caller paginates by advancing Offset until a
	// page returns fewer than Count photos.
	ListPhotos(ctx context.Context, params PhotoListParams) ([]Photo, error)
	// ListAlbums returns one page of albums.
	ListAlbums(ctx context.Context, params ListParams) ([]Album, error)
	// ListLabels returns one page of labels.
	ListLabels(ctx context.Context, params ListParams) ([]Label, error)
	// ListSubjects returns one page of subjects (people).
	ListSubjects(ctx context.Context, params ListParams) ([]Subject, error)
	// DownloadOriginal streams the original file identified by its SHA1 file
	// hash, obtaining and refreshing the download token as needed. The caller
	// must close the returned Download's Body.
	DownloadOriginal(ctx context.Context, fileHash string) (*Download, error)
}

// Config configures an HTTPClient. BaseURL and Token are required for live use;
// the timeout and backoff knobs fall back to package defaults when left zero.
type Config struct {
	// BaseURL is the root of the PhotoPrism instance (e.g. https://photos.example).
	BaseURL string
	// Token is the long-lived app password / access token sent as a Bearer token.
	Token string
	// Timeout bounds a single JSON request (default DefaultTimeout). Downloads are
	// bounded only by the caller's context.
	Timeout time.Duration
	// MaxRetries is the number of extra attempts on HTTP 429 (default
	// DefaultMaxRetries). Zero disables retrying.
	MaxRetries int
	// RetryBaseDelay is the first backoff delay (default DefaultRetryBaseDelay).
	RetryBaseDelay time.Duration
	// RetryMaxDelay caps the backoff delay (default DefaultRetryMaxDelay).
	RetryMaxDelay time.Duration
	// HTTPClient lets callers inject a custom client; a default one is used when
	// nil. Per-request deadlines are applied via context for JSON calls.
	HTTPClient *http.Client
}

// HTTPClient is the production Client backed by a real PhotoPrism REST API.
type HTTPClient struct {
	baseURL    *url.URL
	token      string
	timeout    time.Duration
	maxRetries int
	retryBase  time.Duration
	retryMax   time.Duration
	client     *http.Client

	// mu guards downloadToken, which PhotoPrism may rotate at any time.
	mu            sync.Mutex
	downloadToken string
}

// compile-time assertion that HTTPClient satisfies Client.
var _ Client = (*HTTPClient)(nil)

// New builds an HTTPClient from cfg, applying defaults for any zero timeout or
// backoff value. It returns ErrInvalidURL when BaseURL is not a valid HTTP(S)
// URL with a host.
func New(cfg Config) (*HTTPClient, error) {
	parsed, err := parseBaseURL(cfg.BaseURL)
	if err != nil {
		return nil, err
	}
	client := cfg.HTTPClient
	if client == nil {
		client = &http.Client{}
	}
	return &HTTPClient{
		baseURL:    parsed,
		token:      cfg.Token,
		timeout:    orDuration(cfg.Timeout, DefaultTimeout),
		maxRetries: orInt(cfg.MaxRetries, DefaultMaxRetries),
		retryBase:  orDuration(cfg.RetryBaseDelay, DefaultRetryBaseDelay),
		retryMax:   orDuration(cfg.RetryMaxDelay, DefaultRetryMaxDelay),
		client:     client,
	}, nil
}

// parseBaseURL validates and normalises the configured base URL, returning
// ErrInvalidURL when it is not an HTTP(S) URL with a host.
func parseBaseURL(base string) (*url.URL, error) {
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
	return parsed, nil
}

// ListPhotos returns one page of photos matching params.
func (c *HTTPClient) ListPhotos(ctx context.Context, params PhotoListParams) ([]Photo, error) {
	reqURL := c.endpoint("photos")
	reqURL.RawQuery = params.query().Encode()
	var photos []Photo
	if err := c.getJSON(ctx, reqURL.String(), "photos", &photos); err != nil {
		return nil, err
	}
	return photos, nil
}

// ListAlbums returns one page of albums.
func (c *HTTPClient) ListAlbums(ctx context.Context, params ListParams) ([]Album, error) {
	reqURL := c.endpoint("albums")
	reqURL.RawQuery = params.query().Encode()
	var albums []Album
	if err := c.getJSON(ctx, reqURL.String(), "albums", &albums); err != nil {
		return nil, err
	}
	return albums, nil
}

// ListLabels returns one page of labels.
func (c *HTTPClient) ListLabels(ctx context.Context, params ListParams) ([]Label, error) {
	reqURL := c.endpoint("labels")
	reqURL.RawQuery = params.query().Encode()
	var labels []Label
	if err := c.getJSON(ctx, reqURL.String(), "labels", &labels); err != nil {
		return nil, err
	}
	return labels, nil
}

// ListSubjects returns one page of subjects (people).
func (c *HTTPClient) ListSubjects(ctx context.Context, params ListParams) ([]Subject, error) {
	reqURL := c.endpoint("subjects")
	reqURL.RawQuery = params.query().Encode()
	var subjects []Subject
	if err := c.getJSON(ctx, reqURL.String(), "subjects", &subjects); err != nil {
		return nil, err
	}
	return subjects, nil
}

// endpoint builds an /api/v1/<parts...> URL under the configured base URL.
func (c *HTTPClient) endpoint(parts ...string) *url.URL {
	return c.baseURL.JoinPath(append([]string{"api", "v1"}, parts...)...)
}

// getJSON issues an authenticated GET to reqURL, requires a 200 JSON response,
// and decodes it into dest. op is used only for error context.
func (c *HTTPClient) getJSON(ctx context.Context, reqURL, op string, dest any) error {
	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	resp, err := c.doRequest(ctx, http.MethodGet, reqURL)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return statusError(op, resp.StatusCode)
	}
	if err := requireJSON(op, resp); err != nil {
		return err
	}
	if err := decodeJSON(resp.Body, dest); err != nil {
		return fmt.Errorf("%s: %w", op, err)
	}
	return nil
}

// clampCount clamps a requested page size into PhotoPrism's [1, MaxCount] range,
// treating a non-positive request as the maximum page.
func clampCount(count int) int {
	if count <= 0 || count > MaxCount {
		return MaxCount
	}
	return count
}

// orDuration returns value when positive, otherwise fallback.
func orDuration(value, fallback time.Duration) time.Duration {
	if value > 0 {
		return value
	}
	return fallback
}

// orInt returns value when positive, otherwise fallback.
func orInt(value, fallback int) int {
	if value > 0 {
		return value
	}
	return fallback
}
