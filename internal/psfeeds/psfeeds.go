// Package psfeeds is Kukátko's read-only HTTP client to photo-sorter's migration
// feeds. In production photo-sorter runs as a vector/faces layer on top of
// PhotoPrism: it holds no photos of its own, only pre-computed CLIP image
// embeddings and InsightFace face vectors keyed by the PhotoPrism photo UID. The
// migration therefore imports the photos from PhotoPrism (internal/ppimport) and
// enriches them here with those already-computed vectors, so the often-offline
// GPU box never has to recompute the whole library.
//
// The client pages two keyset-paginated feeds — GET /api/v1/embeddings (CLIP
// ViT-L/14, 768-dim) and GET /api/v1/faces (buffalo_l, 512-dim, carrying the
// face marker and subject assignment) — and reads GET /api/v1/stats for a
// completeness check. Authentication is a read-only psat_ bearer token sent as an
// Authorization: Bearer header on every request. The importer only ever reads.
//
// Upstream failures are classified into sentinel errors (ErrUnauthorized,
// ErrNotFound, ErrRateLimited, ErrUpstream, ErrUnavailable, ErrBadResponse) so
// the importer can react without parsing strings, and HTTP 429 is retried with a
// short exponential backoff. Everything sits behind the Client interface so the
// importer and its tests can substitute a fake without a real photo-sorter, a
// real network, or a real token.
package psfeeds

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// Defaults for the photo-sorter feeds client.
const (
	// DefaultTimeout bounds a single feed request.
	DefaultTimeout = 60 * time.Second
	// DefaultMaxRetries is how many extra attempts an HTTP 429 triggers before the
	// rate-limit error is returned. Zero disables retrying.
	DefaultMaxRetries = 4
	// DefaultRetryDelay is the first backoff delay; it doubles each retry.
	DefaultRetryDelay = time.Second
	// maxRetryDelay caps the exponential backoff delay.
	maxRetryDelay = 30 * time.Second
)

// Sentinel errors classifying an outcome. They never embed the access token or a
// response body, so they are safe to wrap and surface.
var (
	// ErrInvalidURL indicates the configured base URL is not a usable HTTP(S) URL.
	ErrInvalidURL = errors.New("psfeeds: invalid base URL")
	// ErrUnauthorized indicates photo-sorter rejected the token (HTTP 401/403).
	ErrUnauthorized = errors.New("psfeeds: upstream rejected the token")
	// ErrNotFound indicates the requested resource does not exist (HTTP 404).
	ErrNotFound = errors.New("psfeeds: upstream resource not found")
	// ErrRateLimited indicates the rate limit was hit (HTTP 429) and the backoff
	// retries were exhausted.
	ErrRateLimited = errors.New("psfeeds: upstream rate limit exceeded")
	// ErrUpstream indicates photo-sorter returned an unexpected status.
	ErrUpstream = errors.New("psfeeds: upstream error")
	// ErrUnavailable indicates photo-sorter could not be reached (transport
	// failure or a gateway-style 502/503/504).
	ErrUnavailable = errors.New("psfeeds: upstream unavailable")
	// ErrBadResponse indicates photo-sorter was reachable but returned a malformed
	// or non-JSON body where JSON was required.
	ErrBadResponse = errors.New("psfeeds: bad response")
)

// Embedding is one image-embedding feed item: a CLIP vector plus the model tags,
// keyed by the PhotoPrism photo UID it was computed for.
type Embedding struct {
	// PhotoUID is the PhotoPrism photo UID the embedding belongs to; the keyset
	// pagination sorts on it.
	PhotoUID string `json:"photo_uid"`
	// Model and Pretrained are the CLIP model identifiers (e.g. ViT-L-14 /
	// laion2b_s32b_b82k), copied verbatim into Kukátko's embeddings row.
	Model      string `json:"model"`
	Pretrained string `json:"pretrained"`
	// Dim is the vector length reported by the feed (768 for CLIP).
	Dim int `json:"dim"`
	// CreatedAt is when photo-sorter computed the embedding.
	CreatedAt time.Time `json:"created_at"`
	// Vector is the embedding itself, a JSON array of float32 (default encoding).
	Vector []float32 `json:"embedding"`
}

// EmbeddingsPage is one page of the embeddings feed. NextAfter is the keyset
// cursor (the last item's photo_uid); it is nil at the end of the walk.
type EmbeddingsPage struct {
	Embeddings []Embedding `json:"embeddings"`
	Total      int         `json:"total"`
	NextAfter  *string     `json:"next_after"`
}

// Face is one face-feed item: an InsightFace embedding, its bounding box and the
// carried marker/subject assignment, keyed by the PhotoPrism photo UID.
//
// BBox is [x1, y1, x2, y2] in RAW PIXELS of the photo_width×photo_height frame —
// not the normalized [x, y, w, h] Kukátko stores. The importer converts it via
// facejob.NormalizeBBox (which mirrors photo-sorter's own conversion), passing
// PhotoWidth/PhotoHeight/Orientation as the reference frame.
type Face struct {
	// ID is photo-sorter's BIGSERIAL face id; the keyset pagination sorts on it.
	ID int64 `json:"id"`
	// PhotoUID is the PhotoPrism photo UID the face belongs to.
	PhotoUID string `json:"photo_uid"`
	// FaceIndex is the per-photo face slot.
	FaceIndex int `json:"face_index"`
	// Model is the detector model (e.g. "buffalo_l (ResNet100)").
	Model string `json:"model"`
	// Dim is the vector length reported by the feed (512 for InsightFace).
	Dim int `json:"dim"`
	// BBox is the raw-pixel [x1, y1, x2, y2] bounding box; see the type doc.
	BBox []float64 `json:"bbox"`
	// DetScore is the detector's confidence.
	DetScore float64 `json:"det_score"`
	// MarkerUID, SubjectUID and SubjectName carry the assignment. They are the
	// empty string (not absent) when the face is unassigned.
	MarkerUID   string `json:"marker_uid"`
	SubjectUID  string `json:"subject_uid"`
	SubjectName string `json:"subject_name"`
	// PhotoWidth, PhotoHeight and Orientation are the frame the raw-pixel BBox is
	// expressed in; they are also cached on the Kukátko face row.
	PhotoWidth  int `json:"photo_width"`
	PhotoHeight int `json:"photo_height"`
	Orientation int `json:"orientation"`
	// FileUID is photo-sorter's primary-file UID; unused by the importer.
	FileUID string `json:"file_uid"`
	// CreatedAt is when photo-sorter detected the face.
	CreatedAt time.Time `json:"created_at"`
	// Vector is the face embedding, a JSON array of float32 (default encoding).
	Vector []float32 `json:"embedding"`
}

// FacesPage is one page of the faces feed. NextAfter is the keyset cursor (the
// last item's id); it is nil at the end of the walk.
type FacesPage struct {
	Faces     []Face `json:"faces"`
	Total     int    `json:"total"`
	NextAfter *int64 `json:"next_after"`
}

// Stats is the aggregate feed used for the completeness check.
type Stats struct {
	TotalPhotos          int `json:"total_photos"`
	PhotosProcessed      int `json:"photos_processed"`
	PhotosWithEmbeddings int `json:"photos_with_embeddings"`
	PhotosWithFaces      int `json:"photos_with_faces"`
	TotalFaces           int `json:"total_faces"`
	TotalEmbeddings      int `json:"total_embeddings"`
}

// Client is the read-only photo-sorter feeds contract used by the importer. It is
// an interface so the importer and tests can substitute a fake without a real
// photo-sorter, network, or token.
type Client interface {
	// ListEmbeddings returns one page of the embeddings feed. limit is the
	// requested page size (the server clamps it); after is the keyset cursor (the
	// previous page's NextAfter, empty to start). The caller paginates by
	// advancing after until a page's NextAfter is nil.
	ListEmbeddings(ctx context.Context, limit int, after string) (EmbeddingsPage, error)
	// ListFaces returns one page of the faces feed. after is the keyset cursor
	// (the previous page's NextAfter, zero to start).
	ListFaces(ctx context.Context, limit int, after int64) (FacesPage, error)
	// Stats returns the aggregate totals for the completeness check.
	Stats(ctx context.Context) (Stats, error)
}

// Config configures an HTTPClient. BaseURL and Token are required for live use;
// the timeout and backoff knobs fall back to package defaults when left zero.
type Config struct {
	// BaseURL is the root of the photo-sorter instance (e.g. https://sorter.example).
	BaseURL string
	// Token is the read-only psat_ bearer token.
	Token string
	// Timeout bounds a single request (default DefaultTimeout).
	Timeout time.Duration
	// MaxRetries is the number of extra attempts on HTTP 429 (default
	// DefaultMaxRetries). Zero disables retrying.
	MaxRetries int
	// RetryDelay is the first backoff delay (default DefaultRetryDelay).
	RetryDelay time.Duration
	// HTTPClient lets callers inject a custom client; a default one is used when nil.
	HTTPClient *http.Client
}

// HTTPClient is the production Client backed by a real photo-sorter REST API.
type HTTPClient struct {
	baseURL    *url.URL
	token      string
	timeout    time.Duration
	maxRetries int
	retryDelay time.Duration
	client     *http.Client
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
		retryDelay: orDuration(cfg.RetryDelay, DefaultRetryDelay),
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

// ListEmbeddings returns one page of the embeddings feed for the given cursor.
func (c *HTTPClient) ListEmbeddings(ctx context.Context, limit int, after string) (EmbeddingsPage, error) {
	query := url.Values{}
	if limit > 0 {
		query.Set("limit", strconv.Itoa(limit))
	}
	if after != "" {
		query.Set("after", after)
	}
	var page EmbeddingsPage
	if err := c.get(ctx, "embeddings", query, &page); err != nil {
		return EmbeddingsPage{}, fmt.Errorf("listing embeddings: %w", err)
	}
	return page, nil
}

// ListFaces returns one page of the faces feed for the given cursor.
func (c *HTTPClient) ListFaces(ctx context.Context, limit int, after int64) (FacesPage, error) {
	query := url.Values{}
	if limit > 0 {
		query.Set("limit", strconv.Itoa(limit))
	}
	if after > 0 {
		query.Set("after", strconv.FormatInt(after, 10))
	}
	var page FacesPage
	if err := c.get(ctx, "faces", query, &page); err != nil {
		return FacesPage{}, fmt.Errorf("listing faces: %w", err)
	}
	return page, nil
}

// Stats returns the aggregate feed totals.
func (c *HTTPClient) Stats(ctx context.Context) (Stats, error) {
	var stats Stats
	if err := c.get(ctx, "stats", url.Values{}, &stats); err != nil {
		return Stats{}, fmt.Errorf("reading stats: %w", err)
	}
	return stats, nil
}

// get issues an authenticated GET to /api/v1/<path>?<query>, retrying HTTP 429
// with exponential backoff, and decodes a 200 JSON response into dest.
func (c *HTTPClient) get(ctx context.Context, path string, query url.Values, dest any) error {
	reqURL := c.baseURL.JoinPath("api", "v1", path)
	reqURL.RawQuery = query.Encode()

	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()

	var lastErr error
	for attempt := 0; attempt <= c.maxRetries; attempt++ {
		if attempt > 0 {
			if err := backoffSleep(ctx, c.retryDelay, attempt); err != nil {
				return err
			}
		}
		resp, err := c.send(ctx, reqURL.String())
		if err != nil {
			return err
		}
		if resp.StatusCode == http.StatusTooManyRequests {
			_ = resp.Body.Close()
			lastErr = statusError(resp.StatusCode)
			continue
		}
		return decodeResponse(resp, dest)
	}
	return lastErr
}

// send performs a single authenticated GET, classifying a transport failure as
// ErrUnavailable and passing a context cancellation through unwrapped.
func (c *HTTPClient) send(ctx context.Context, rawURL string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, fmt.Errorf("building request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Accept", "application/json")
	resp, err := c.client.Do(req)
	if err != nil {
		if cerr := ctx.Err(); cerr != nil && errors.Is(cerr, context.Canceled) {
			return nil, fmt.Errorf("request: %w", cerr)
		}
		return nil, fmt.Errorf("%w: %w", ErrUnavailable, err)
	}
	return resp, nil
}

// decodeResponse closes resp, requires a 200 JSON body and decodes it into dest.
func decodeResponse(resp *http.Response, dest any) error {
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return statusError(resp.StatusCode)
	}
	contentType := resp.Header.Get("Content-Type")
	if base, _, _ := strings.Cut(contentType, ";"); !strings.EqualFold(strings.TrimSpace(base), "application/json") {
		return fmt.Errorf("%w: expected application/json, got %q", ErrBadResponse, contentType)
	}
	if err := json.NewDecoder(resp.Body).Decode(dest); err != nil {
		return fmt.Errorf("%w: decoding response: %w", ErrBadResponse, err)
	}
	return nil
}

// statusError maps a non-success upstream status to a sentinel error. The body is
// never included, so a token echoed in an error payload cannot leak.
func statusError(status int) error {
	switch status {
	case http.StatusUnauthorized, http.StatusForbidden:
		return fmt.Errorf("%w (status %d)", ErrUnauthorized, status)
	case http.StatusNotFound:
		return fmt.Errorf("%w (status %d)", ErrNotFound, status)
	case http.StatusTooManyRequests:
		return fmt.Errorf("%w (status %d)", ErrRateLimited, status)
	case http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout:
		return fmt.Errorf("%w (status %d)", ErrUnavailable, status)
	default:
		return fmt.Errorf("%w (status %d)", ErrUpstream, status)
	}
}

// backoffSleep waits before a retry attempt using capped exponential backoff,
// returning the context error if the wait is cancelled.
func backoffSleep(ctx context.Context, base time.Duration, attempt int) error {
	delay := base << (attempt - 1)
	if delay <= 0 || delay > maxRetryDelay {
		delay = maxRetryDelay
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return fmt.Errorf("backoff: %w", ctx.Err())
	case <-timer.C:
		return nil
	}
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
