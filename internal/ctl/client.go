package ctl

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	// apiBasePath is where server.New mounts every /api/v1 route group.
	apiBasePath = "/api/v1"
	// defaultTimeout bounds a single request. Photo listing is a cheap indexed
	// query; a slower answer than this means the server is in trouble.
	defaultTimeout = 30 * time.Second
	// maxResponseBody caps how much of a response body is read into memory. A
	// page of 500 photo rows is far below this; the cap only stops a hostile or
	// broken server from exhausting the client.
	maxResponseBody = 32 << 20
	// maxErrorSnippet caps how much of a non-JSON error body is echoed back.
	maxErrorSnippet = 400
)

// ErrInvalidServerURL indicates a server URL that is not an absolute http(s) URL.
var ErrInvalidServerURL = errors.New("ctl: server must be an absolute http(s) URL")

// UnauthorizedError reports a 401 from the server. Its message is deliberately
// actionable — the server never says whether the token was missing, expired or
// revoked (by design, see internal/auth), so the client names all three and tells
// the operator how to get a fresh one. The token itself is never echoed.
type UnauthorizedError struct {
	// Server is the endpoint that rejected the credential.
	Server string
}

// Error renders the actionable 401 message.
func (e *UnauthorizedError) Error() string {
	return "authentication failed (HTTP 401) at " + e.Server + ": " +
		"the API token is missing, expired, or revoked.\n" +
		"Create a new one (POST " + apiBasePath + "/auth/tokens while logged in), then store it:\n" +
		"    kukatko ctl config set-context <name> --server " + e.Server + " --token-stdin\n" +
		"or export " + EnvToken + " for a one-off command."
}

// StatusError reports any other non-2xx response. Message is the server's own
// {"error": …} text when it sent one, or a bounded snippet of the body.
type StatusError struct {
	// Status is the HTTP status code.
	Status int
	// Message is the server's explanation, already bounded in length.
	Message string
}

// Error renders the status and the server's explanation.
func (e *StatusError) Error() string {
	return fmt.Sprintf("server returned HTTP %d: %s", e.Status, e.Message)
}

// Client talks to one Kukátko server's /api/v1 with a bearer token. The zero
// value is unusable; build one with NewClient.
type Client struct {
	server string
	token  string
	httpc  *http.Client
}

// NewClient builds a client for the given server root (scheme and host, with no
// /api/v1 suffix) authenticating with token, which may be empty — an unauthorized
// call then fails with the same actionable UnauthorizedError as a bad token. It
// returns ErrInvalidServerURL when server is not an absolute http or https URL.
func NewClient(server, token string) (*Client, error) {
	server = NormalizeServer(server)
	parsed, err := url.Parse(server)
	if err != nil {
		return nil, fmt.Errorf("%w: %q: %w", ErrInvalidServerURL, server, err)
	}
	if (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		return nil, fmt.Errorf("%w: %q", ErrInvalidServerURL, server)
	}
	return &Client{
		server: server,
		token:  token,
		httpc:  &http.Client{Timeout: defaultTimeout},
	}, nil
}

// Server returns the server root this client was built for.
func (c *Client) Server() string {
	return c.server
}

// get issues an authenticated GET against apiBasePath+path and returns the raw
// response body. The body is returned undecoded on purpose: `-o json` prints the
// API's own bytes back, unchanged, and only the table renderer needs a decoder.
//
// A 401 yields *UnauthorizedError, any other non-200 a *StatusError, and a
// transport failure a wrapped error.
func (c *Client) get(ctx context.Context, path string, query url.Values) (json.RawMessage, error) {
	endpoint := c.server + apiBasePath + path
	if len(query) > 0 {
		endpoint += "?" + query.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("building request for %s: %w", path, err)
	}
	req.Header.Set("Accept", "application/json")
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}

	resp, err := c.httpc.Do(req)
	if err != nil {
		// url.Error already embeds the endpoint; the token is never part of it.
		return nil, fmt.Errorf("requesting %s: %w", path, err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBody))
	if err != nil {
		return nil, fmt.Errorf("reading response from %s: %w", path, err)
	}
	if resp.StatusCode == http.StatusUnauthorized {
		return nil, &UnauthorizedError{Server: c.server}
	}
	if resp.StatusCode != http.StatusOK {
		return nil, &StatusError{Status: resp.StatusCode, Message: errorMessage(body)}
	}
	return body, nil
}

// errorMessage extracts the server's {"error": …} text from an error body,
// falling back to a bounded snippet of whatever it actually sent.
func errorMessage(body []byte) string {
	var payload struct {
		Error string `json:"error"`
	}
	if err := json.Unmarshal(body, &payload); err == nil && payload.Error != "" {
		return payload.Error
	}
	snippet := strings.TrimSpace(string(body))
	if snippet == "" {
		return "(empty response body)"
	}
	if len(snippet) > maxErrorSnippet {
		return snippet[:maxErrorSnippet] + "…"
	}
	return snippet
}
