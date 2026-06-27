package photoprism

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

// sessionResponse is the subset of PhotoPrism's create-session response the
// client reads: the embedded client config carrying the download token.
type sessionResponse struct {
	Config sessionConfig `json:"config"`
}

// sessionConfig holds the tokens PhotoPrism issues with a session.
type sessionConfig struct {
	DownloadToken string `json:"downloadToken"`
	PreviewToken  string `json:"previewToken"`
}

// DownloadOriginal streams the original file identified by its SHA1 file hash.
// It obtains a download token from a session on first use, refreshes it once and
// retries if PhotoPrism rejects the request as unauthorized (the token may have
// rotated), and streams the body straight through. The caller must close the
// returned Download's Body; doing so also releases the underlying request.
func (c *HTTPClient) DownloadOriginal(ctx context.Context, fileHash string) (*Download, error) {
	if strings.TrimSpace(fileHash) == "" {
		return nil, fmt.Errorf("dl: %w: empty file hash", ErrBadResponse)
	}
	// A cancellable context (not a timeout) bounds the streamed body: cancelling
	// on Close tears the request down without capping a long download.
	ctx, cancel := context.WithCancel(ctx)
	// The response body is handed to the caller via the returned Download and
	// closed there (through cancelReadCloser); it is not leaked here.
	resp, err := c.fetchDownload(ctx, fileHash) //nolint:bodyclose // body owned by returned Download.
	if err != nil {
		cancel()
		return nil, err
	}
	return &Download{
		Body:          &cancelReadCloser{rc: resp.Body, cancel: cancel},
		ContentType:   contentTypeOr(resp, "application/octet-stream"),
		ContentLength: resp.ContentLength,
	}, nil
}

// fetchDownload performs a download attempt and, on an auth rejection, refreshes
// the token and retries once. It returns a 200 response or a classified error;
// any non-success body is closed before returning.
func (c *HTTPClient) fetchDownload(ctx context.Context, hash string) (*http.Response, error) {
	resp, err := c.attemptDownload(ctx, hash)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode == http.StatusOK {
		return resp, nil
	}
	if isAuthStatus(resp.StatusCode) {
		_ = resp.Body.Close()
		return c.retryDownloadWithFreshToken(ctx, hash)
	}
	err = statusError("dl", resp.StatusCode)
	_ = resp.Body.Close()
	return nil, err
}

// retryDownloadWithFreshToken forces a new session token and retries the
// download exactly once, returning a 200 response or a classified error.
func (c *HTTPClient) retryDownloadWithFreshToken(ctx context.Context, hash string) (*http.Response, error) {
	if _, err := c.refreshDownloadToken(ctx); err != nil {
		return nil, err
	}
	resp, err := c.attemptDownload(ctx, hash)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		retryErr := statusError("dl", resp.StatusCode)
		_ = resp.Body.Close()
		return nil, retryErr
	}
	return resp, nil
}

// attemptDownload issues a single download request for hash using the current
// download token, obtaining one from a session if none is cached yet.
func (c *HTTPClient) attemptDownload(ctx context.Context, hash string) (*http.Response, error) {
	token, err := c.ensureDownloadToken(ctx)
	if err != nil {
		return nil, err
	}
	reqURL := c.endpoint("dl", hash)
	reqURL.RawQuery = url.Values{"t": {token}}.Encode()
	return c.doRequest(ctx, http.MethodGet, reqURL.String())
}

// ensureDownloadToken returns the cached download token, creating a session to
// obtain one when none is cached yet.
func (c *HTTPClient) ensureDownloadToken(ctx context.Context) (string, error) {
	c.mu.Lock()
	token := c.downloadToken
	c.mu.Unlock()
	if token != "" {
		return token, nil
	}
	return c.refreshDownloadToken(ctx)
}

// refreshDownloadToken creates a fresh session, caches the issued download token
// and returns it.
func (c *HTTPClient) refreshDownloadToken(ctx context.Context) (string, error) {
	token, err := c.createSession(ctx)
	if err != nil {
		return "", err
	}
	c.mu.Lock()
	c.downloadToken = token
	c.mu.Unlock()
	return token, nil
}

// createSession POSTs to the session endpoint and returns the issued download
// token, preferring the token in the response body and falling back to one
// rotated in via the X-Download-Token header.
func (c *HTTPClient) createSession(ctx context.Context) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	reqURL := c.endpoint("session")
	resp, err := c.doRequest(ctx, http.MethodPost, reqURL.String())
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return "", statusError("session", resp.StatusCode)
	}
	token := c.parseSessionToken(resp)
	if token == "" {
		return "", fmt.Errorf("session: %w: no download token issued", ErrBadResponse)
	}
	return token, nil
}

// parseSessionToken extracts the download token from a session response: the
// body's config.downloadToken when present, otherwise the token captured from
// the X-Download-Token header by doRequest.
func (c *HTTPClient) parseSessionToken(resp *http.Response) string {
	if requireJSON("session", resp) == nil {
		var decoded sessionResponse
		if decodeJSON(resp.Body, &decoded) == nil && decoded.Config.DownloadToken != "" {
			return decoded.Config.DownloadToken
		}
	}
	c.mu.Lock()
	token := c.downloadToken
	c.mu.Unlock()
	return token
}

// isAuthStatus reports whether code is an authentication/authorization rejection
// (401 or 403) that warrants a token refresh.
func isAuthStatus(code int) bool {
	return code == http.StatusUnauthorized || code == http.StatusForbidden
}

// contentTypeOr returns the response Content-Type, or fallback when it is unset.
func contentTypeOr(resp *http.Response, fallback string) string {
	if ct := resp.Header.Get("Content-Type"); ct != "" {
		return ct
	}
	return fallback
}
