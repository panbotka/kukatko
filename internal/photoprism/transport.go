package photoprism

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// doRequest issues an authenticated request to rawURL and returns the live
// response (whose body the caller must close). It retries HTTP 429 up to
// maxRetries times with exponential backoff (honouring Retry-After) and records
// any rotated download token from the response headers. A transport failure is
// returned immediately as ErrUnavailable; an exhausted 429 budget as
// ErrRateLimited.
func (c *HTTPClient) doRequest(ctx context.Context, method, rawURL string) (*http.Response, error) {
	var lastErr error
	var retryAfter time.Duration
	for attempt := 0; attempt <= c.maxRetries; attempt++ {
		if attempt > 0 {
			if err := c.waitBackoff(ctx, attempt, retryAfter); err != nil {
				return nil, err
			}
		}
		resp, err := c.send(ctx, method, rawURL)
		if err != nil {
			return nil, err
		}
		c.captureDownloadToken(resp)
		if resp.StatusCode != http.StatusTooManyRequests {
			return resp, nil
		}
		retryAfter = parseRetryAfter(resp.Header.Get("Retry-After"))
		_ = resp.Body.Close()
		lastErr = statusError("request", resp.StatusCode)
	}
	return nil, lastErr
}

// send builds and performs a single authenticated request, classifying a
// transport failure as ErrUnavailable (or passing a context cancellation
// through unwrapped so it is not mistaken for the box being offline).
func (c *HTTPClient) send(ctx context.Context, method, rawURL string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, method, rawURL, nil)
	if err != nil {
		return nil, fmt.Errorf("request: build: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Accept", "application/json")
	resp, err := c.client.Do(req)
	if err != nil {
		if cerr := ctx.Err(); cerr != nil && errors.Is(cerr, context.Canceled) {
			return nil, fmt.Errorf("request: %w", cerr)
		}
		return nil, fmt.Errorf("request: %w: %w", ErrUnavailable, err)
	}
	return resp, nil
}

// waitBackoff sleeps before retry attempt, preferring an explicit Retry-After
// delay and otherwise using capped exponential backoff. It returns the context
// error if the wait is cancelled.
func (c *HTTPClient) waitBackoff(ctx context.Context, attempt int, retryAfter time.Duration) error {
	delay := retryAfter
	if delay <= 0 {
		delay = c.retryBase << (attempt - 1)
		if delay <= 0 || delay > c.retryMax {
			delay = c.retryMax
		}
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

// captureDownloadToken stores a rotated download token advertised via the
// X-Download-Token response header, so later downloads use the current token.
func (c *HTTPClient) captureDownloadToken(resp *http.Response) {
	token := resp.Header.Get(downloadTokenHeader)
	if token == "" {
		return
	}
	c.mu.Lock()
	c.downloadToken = token
	c.mu.Unlock()
}

// parseRetryAfter parses a Retry-After header value expressed as a number of
// seconds, returning zero when absent or not a valid non-negative integer.
func parseRetryAfter(value string) time.Duration {
	if value == "" {
		return 0
	}
	secs, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil || secs < 0 {
		return 0
	}
	return time.Duration(secs) * time.Second
}

// statusError maps a non-success upstream status to a sentinel error. The body
// is never included, so a token echoed in an error payload cannot leak. op is
// used only for error context.
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

// requireJSON returns ErrBadResponse unless resp declares a JSON content type,
// guarding against HTML error pages or proxies masquerading as success.
func requireJSON(op string, resp *http.Response) error {
	ct := resp.Header.Get("Content-Type")
	if i := strings.IndexByte(ct, ';'); i >= 0 {
		ct = ct[:i]
	}
	if !strings.EqualFold(strings.TrimSpace(ct), "application/json") {
		return fmt.Errorf("%s: %w: expected application/json, got %q", op, ErrBadResponse, ct)
	}
	return nil
}

// decodeJSON decodes the response body into dest, wrapping a parse failure as
// ErrBadResponse.
func decodeJSON(body io.Reader, dest any) error {
	if err := json.NewDecoder(body).Decode(dest); err != nil {
		return fmt.Errorf("%w: decoding response: %w", ErrBadResponse, err)
	}
	return nil
}

// cancelReadCloser wraps a response body so closing it also cancels the request
// context, releasing resources once the streamed download is fully relayed.
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
