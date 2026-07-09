package ctl

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// testClient starts an httptest server with the given handler and returns a
// client pointed at it, authenticating with token.
func testClient(t *testing.T, token string, handler http.HandlerFunc) *Client {
	t.Helper()

	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)

	client, err := NewClient(srv.URL, token)
	if err != nil {
		t.Fatalf("NewClient(%q) returned %v", srv.URL, err)
	}
	return client
}

// TestNewClient_validURLs verifies absolute http and https URLs are accepted and
// normalized.
func TestNewClient_validURLs(t *testing.T) {
	t.Parallel()

	for _, in := range []string{"http://localhost:8080", "https://k.example.com/", "https://k.example.com"} {
		client, err := NewClient(in, "kkt_a_b")
		if err != nil {
			t.Fatalf("NewClient(%q) returned %v", in, err)
		}
		if strings.HasSuffix(client.Server(), "/") {
			t.Errorf("NewClient(%q).Server() = %q, want no trailing slash", in, client.Server())
		}
	}
}

// TestNewClient_invalidURLs verifies anything that is not an absolute http(s)
// URL is rejected before a request is ever attempted.
func TestNewClient_invalidURLs(t *testing.T) {
	t.Parallel()

	for _, in := range []string{"", "localhost:8080", "ftp://example.com", "://x", "/api/v1", "https://"} {
		if _, err := NewClient(in, ""); !errors.Is(err, ErrInvalidServerURL) {
			t.Errorf("NewClient(%q) error = %v, want ErrInvalidServerURL", in, err)
		}
	}
}

// TestClient_get_sendsBearer verifies the credential travels in an RFC 6750
// Authorization header and that JSON is requested.
func TestClient_get_sendsBearer(t *testing.T) {
	t.Parallel()

	var gotAuth, gotAccept, gotPath, gotQuery string
	client := testClient(t, "kkt_abc_secret", func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotAccept = r.Header.Get("Accept")
		gotPath = r.URL.Path
		gotQuery = r.URL.RawQuery
		w.Write([]byte(`{"ok":true}`))
	})

	if _, err := client.get(t.Context(), "/photos", map[string][]string{"limit": {"2"}}); err != nil {
		t.Fatalf("get returned %v", err)
	}
	if gotAuth != "Bearer kkt_abc_secret" {
		t.Errorf("Authorization = %q, want the bearer credential", gotAuth)
	}
	if gotAccept != "application/json" {
		t.Errorf("Accept = %q, want application/json", gotAccept)
	}
	if gotPath != "/api/v1/photos" {
		t.Errorf("path = %q, want /api/v1/photos", gotPath)
	}
	if gotQuery != "limit=2" {
		t.Errorf("query = %q, want limit=2", gotQuery)
	}
}

// TestClient_get_omitsEmptyBearer verifies no Authorization header is sent when
// no token is configured, so the server answers 401 rather than parsing "Bearer ".
func TestClient_get_omitsEmptyBearer(t *testing.T) {
	t.Parallel()

	var hadAuth bool
	client := testClient(t, "", func(w http.ResponseWriter, r *http.Request) {
		_, hadAuth = r.Header["Authorization"]
		w.Write([]byte(`{}`))
	})

	if _, err := client.get(t.Context(), "/photos", nil); err != nil {
		t.Fatalf("get returned %v", err)
	}
	if hadAuth {
		t.Error("an Authorization header was sent without a token")
	}
}

// TestClient_get_unauthorized verifies a 401 becomes a short, actionable message
// naming all three causes the server refuses to distinguish — and that it never
// echoes the token or the response body.
func TestClient_get_unauthorized(t *testing.T) {
	t.Parallel()

	client := testClient(t, "kkt_abc_supersecret", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"error":"authentication required"}`))
	})

	_, err := client.get(t.Context(), "/photos", nil)
	var unauthorized *UnauthorizedError
	if !errors.As(err, &unauthorized) {
		t.Fatalf("get error = %v (%T), want *UnauthorizedError", err, err)
	}
	msg := err.Error()
	for _, want := range []string{"401", "missing, expired, or revoked", "/auth/tokens", EnvToken} {
		if !strings.Contains(msg, want) {
			t.Errorf("401 message %q does not mention %q", msg, want)
		}
	}
	if strings.Contains(msg, "supersecret") {
		t.Errorf("401 message leaks the token: %q", msg)
	}
	if strings.Contains(msg, "authentication required") {
		t.Errorf("401 message dumps the response body: %q", msg)
	}
}

// TestClient_get_statusError verifies a non-401 failure carries the server's own
// error text.
func TestClient_get_statusError(t *testing.T) {
	t.Parallel()

	client := testClient(t, "kkt_a_b", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte(`{"error":"photo not found"}`))
	})

	_, err := client.get(t.Context(), "/photos/nope", nil)
	var status *StatusError
	if !errors.As(err, &status) {
		t.Fatalf("get error = %v (%T), want *StatusError", err, err)
	}
	if status.Status != http.StatusNotFound || status.Message != "photo not found" {
		t.Errorf("StatusError = %+v, want 404 photo not found", status)
	}
	if !strings.Contains(err.Error(), "HTTP 404") {
		t.Errorf("error text %q does not name the status", err.Error())
	}
}

// TestClient_get_transportFailure verifies an unreachable server yields a wrapped
// transport error rather than a panic or a nil body.
func TestClient_get_transportFailure(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.NotFoundHandler())
	client, err := NewClient(srv.URL, "kkt_a_b")
	if err != nil {
		t.Fatalf("NewClient returned %v", err)
	}
	srv.Close()

	if _, err := client.get(t.Context(), "/photos", nil); err == nil {
		t.Error("get against a closed server returned no error")
	}
}

// TestClient_get_contextCancelled verifies the request honours its context.
func TestClient_get_contextCancelled(t *testing.T) {
	t.Parallel()

	client := testClient(t, "kkt_a_b", func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte(`{}`))
	})
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	if _, err := client.get(ctx, "/photos", nil); !errors.Is(err, context.Canceled) {
		t.Errorf("get with a cancelled context error = %v, want context.Canceled", err)
	}
}

// TestErrorMessage verifies the server's {"error": …} text is preferred, that a
// non-JSON body is echoed as a bounded snippet, and that an empty body is named.
func TestErrorMessage(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		body string
		want string
	}{
		{name: "json error field", body: `{"error":"unknown sort \"nope\""}`, want: `unknown sort "nope"`},
		{name: "plain text body", body: "  502 bad gateway\n", want: "502 bad gateway"},
		{name: "empty body", body: "", want: "(empty response body)"},
		{name: "json without an error field", body: `{"detail":"x"}`, want: `{"detail":"x"}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := errorMessage([]byte(tt.body)); got != tt.want {
				t.Errorf("errorMessage(%q) = %q, want %q", tt.body, got, tt.want)
			}
		})
	}
}

// TestErrorMessage_bounded verifies an oversized body is truncated rather than
// dumped in full.
func TestErrorMessage_bounded(t *testing.T) {
	t.Parallel()

	got := errorMessage([]byte(strings.Repeat("x", maxErrorSnippet*2)))
	if len([]rune(got)) != maxErrorSnippet+1 {
		t.Errorf("errorMessage of an oversized body has %d runes, want %d", len([]rune(got)), maxErrorSnippet+1)
	}
	if !strings.HasSuffix(got, "…") {
		t.Errorf("truncated message %q is not marked with an ellipsis", got[len(got)-8:])
	}
}
