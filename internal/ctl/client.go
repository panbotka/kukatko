package ctl

import (
	"bytes"
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

// ForbiddenError reports a 403 from the server: the token authenticated, but its
// role is not allowed to perform the request. Every mutating ctl command is
// guarded server-side by the editor/admin write check, so this is what a viewer's
// token gets. The message says that plainly instead of dumping the server's body,
// which is only ever the opaque "insufficient permissions".
type ForbiddenError struct {
	// Server is the endpoint that refused the request.
	Server string
}

// Error renders the actionable 403 message.
func (e *ForbiddenError) Error() string {
	return "permission denied (HTTP 403) at " + e.Server + ": " +
		"this API token's role may not perform this operation.\n" +
		"Creating, editing and deleting need the editor or admin role; a viewer token may only read."
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
func (c *Client) get(ctx context.Context, path string, query url.Values) (json.RawMessage, error) {
	return c.do(ctx, http.MethodGet, path, query, nil)
}

// send issues an authenticated mutating request (POST, PUT, PATCH or DELETE)
// carrying body as JSON, or no body at all when body is nil. It returns the raw
// response body, which is nil for the endpoints that answer 204 No Content.
func (c *Client) send(ctx context.Context, method, path string, body any) (json.RawMessage, error) {
	return c.do(ctx, method, path, nil, body)
}

// do performs one authenticated request and returns the raw response body, or nil
// when the server answered 204 No Content.
//
// A 401 yields *UnauthorizedError, a 403 *ForbiddenError, any other non-2xx a
// *StatusError, and a transport failure a wrapped error. The token is never part
// of any of them.
func (c *Client) do(
	ctx context.Context, method, path string, query url.Values, body any,
) (json.RawMessage, error) {
	req, err := c.newRequest(ctx, method, path, query, body)
	if err != nil {
		return nil, err
	}
	resp, err := c.httpc.Do(req)
	if err != nil {
		// url.Error already embeds the endpoint; the token is never part of it.
		return nil, fmt.Errorf("requesting %s: %w", path, err)
	}
	defer func() { _ = resp.Body.Close() }()

	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBody))
	if err != nil {
		return nil, fmt.Errorf("reading response from %s: %w", path, err)
	}
	if err := c.statusError(resp.StatusCode, raw); err != nil {
		return nil, err
	}
	if len(bytes.TrimSpace(raw)) == 0 {
		return nil, nil
	}
	return raw, nil
}

// newRequest builds the authenticated request for one call, encoding body as JSON
// when it is not nil.
func (c *Client) newRequest(
	ctx context.Context, method, path string, query url.Values, body any,
) (*http.Request, error) {
	endpoint := c.server + apiBasePath + path
	if len(query) > 0 {
		endpoint += "?" + query.Encode()
	}
	var payload io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("encoding the request body for %s: %w", path, err)
		}
		payload = bytes.NewReader(encoded)
	}
	req, err := http.NewRequestWithContext(ctx, method, endpoint, payload)
	if err != nil {
		return nil, fmt.Errorf("building request for %s: %w", path, err)
	}
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	return req, nil
}

// statusError maps a response status onto the typed error the CLI reports, or nil
// for any 2xx. The API answers 200, 201 and 204 across the resources ctl drives,
// so the whole 2xx range is accepted rather than one status per endpoint.
func (c *Client) statusError(status int, body []byte) error {
	switch {
	case status == http.StatusUnauthorized:
		return &UnauthorizedError{Server: c.server}
	case status == http.StatusForbidden:
		return &ForbiddenError{Server: c.server}
	case status < http.StatusOK || status >= http.StatusMultipleChoices:
		return &StatusError{Status: status, Message: errorMessage(body)}
	default:
		return nil
	}
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
