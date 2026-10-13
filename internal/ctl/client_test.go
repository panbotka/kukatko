package ctl

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// testProject is the slug of the project testClient's client works in: every
// project-scoped path a test expects is under /api/v1/p/fam/.
const testProject = "fam"

// testClient starts an httptest server with the given handler and returns a
// client pointed at it, authenticating with token and bound to testProject, as
// a project-scoped ctl command's client is. instanceClient is the same without
// the project, for the instance-wide routes.
//
// The client is re-pointed at a connection pool of its own. NewClient leaves
// Transport nil, which is right in production — ctl is a CLI, one process holds
// one client — but under test it means every client here shares
// http.DefaultTransport, a process-wide singleton, while each test raises and
// tears down its own httptest.Server. Closing one server evicts pooled
// connections another parallel test is about to reuse, and whichever request is
// in flight at that moment fails with "transport connection broken:
// http: CloseIdleConnections called". Which test loses the race is arbitrary,
// which is why it moved between runs. Scoping the pool to the test that owns it
// removes the coupling rather than papering over it.
func testClient(t *testing.T, token string, handler http.HandlerFunc) *Client {
	t.Helper()

	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)

	client, err := NewClient(srv.URL, token)
	if err != nil {
		t.Fatalf("NewClient(%q) returned %v", srv.URL, err)
	}
	transport := &http.Transport{}
	t.Cleanup(transport.CloseIdleConnections)
	client.httpc = &http.Client{Timeout: defaultTimeout, Transport: transport}
	// The media client shares the same reasoning and the same pool; it keeps its
	// production absence of a timeout, since a download is meant to be slow.
	client.stream = &http.Client{Transport: transport}
	return client.WithProject(testProject)
}

// instanceClient is testClient without a project: the client an
// instance-level command (projects, users) talks through.
func instanceClient(t *testing.T, token string, handler http.HandlerFunc) *Client {
	t.Helper()
	return testClient(t, token, handler).WithProject("")
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
	if gotPath != "/api/v1/p/fam/photos" {
		t.Errorf("path = %q, want /api/v1/p/fam/photos", gotPath)
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

// TestClient_forbidden verifies a viewer's token — which authenticates fine but
// may not mutate — gets a message that says exactly that, rather than the opaque
// "insufficient permissions" the server sends. The token is never echoed.
func TestClient_forbidden(t *testing.T) {
	t.Parallel()

	client := testClient(t, "kkt_viewer_supersecret", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		w.Write([]byte(`{"error":"insufficient permissions"}`))
	})

	_, err := client.CreateAlbum(t.Context(), AlbumInput{Title: "Trip"})
	var forbidden *ForbiddenError
	if !errors.As(err, &forbidden) {
		t.Fatalf("CreateAlbum error = %v (%T), want *ForbiddenError", err, err)
	}
	msg := err.Error()
	for _, want := range []string{"403", "permission denied", "editor or admin", "viewer"} {
		if !strings.Contains(msg, want) {
			t.Errorf("403 message %q does not mention %q", msg, want)
		}
	}
	if strings.Contains(msg, "supersecret") {
		t.Errorf("403 message leaks the token: %q", msg)
	}
	if strings.Contains(msg, "insufficient permissions") {
		t.Errorf("403 message dumps the response body: %q", msg)
	}
}

// TestClient_send_noContent verifies a 204 is a success that yields no body, which
// is what every toggle and attach endpoint answers with.
func TestClient_send_noContent(t *testing.T) {
	t.Parallel()

	client := testClient(t, "kkt_a_b", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})

	raw, err := client.send(t.Context(), http.MethodPut, "/photos/pht01/favorite", nil)
	if err != nil {
		t.Fatalf("send returned %v", err)
	}
	if raw != nil {
		t.Errorf("send returned %q, want no body for a 204", raw)
	}
}

// TestClient_send_created verifies a 201 is a success carrying the created
// resource, which is what the create endpoints answer with.
func TestClient_send_created(t *testing.T) {
	t.Parallel()

	var gotType string
	client := testClient(t, "kkt_a_b", func(w http.ResponseWriter, r *http.Request) {
		gotType = r.Header.Get("Content-Type")
		w.WriteHeader(http.StatusCreated)
		w.Write([]byte(`{"uid":"alb01"}`))
	})

	raw, err := client.send(t.Context(), http.MethodPost, "/albums", map[string]string{"title": "Trip"})
	if err != nil {
		t.Fatalf("send returned %v", err)
	}
	if gotType != "application/json" {
		t.Errorf("Content-Type = %q, want application/json for a body-carrying request", gotType)
	}
	if string(raw) != `{"uid":"alb01"}` {
		t.Errorf("send returned %q, want the created resource", raw)
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

// TestClient_pathBuilding pins where a request goes: a client bound to a project
// sends every path under /api/v1/p/<slug>/ (the slug escaped into its own
// segment), and one without a project sends it to the bare /api/v1/ — the
// instance-wide routes.
func TestClient_pathBuilding(t *testing.T) {
	t.Parallel()

	var got []string
	client := testClient(t, "kkt_a_b", func(w http.ResponseWriter, r *http.Request) {
		got = append(got, r.URL.EscapedPath())
		_, _ = w.Write([]byte(`{}`))
	})
	ctx := t.Context()
	if client.Project() != testProject {
		t.Fatalf("Project() = %q, want %q", client.Project(), testProject)
	}
	if _, err := client.get(ctx, "/photos", nil); err != nil {
		t.Fatalf("scoped get: %v", err)
	}
	if _, err := client.WithProject("").get(ctx, "/projects", nil); err != nil {
		t.Fatalf("instance get: %v", err)
	}
	if _, err := client.WithProject("a/b").get(ctx, "/albums", nil); err != nil {
		t.Fatalf("odd-slug get: %v", err)
	}
	want := []string{"/api/v1/p/fam/photos", "/api/v1/projects", "/api/v1/p/a%2Fb/albums"}
	if strings.Join(got, " ") != strings.Join(want, " ") {
		t.Errorf("paths = %v, want %v", got, want)
	}
	if client.Project() != testProject {
		t.Errorf("WithProject changed the original client's project to %q", client.Project())
	}
}

// TestClient_directoryNeedsAProject verifies the name directory is never read
// without a project: its unprefixed path is the system administrator's account
// list, so a project-less client refuses before any request.
func TestClient_directoryNeedsAProject(t *testing.T) {
	t.Parallel()

	client := instanceClient(t, "kkt_a_b", func(_ http.ResponseWriter, r *http.Request) {
		t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
	})
	if _, err := client.People(t.Context()); !errors.Is(err, ErrNoProject) {
		t.Errorf("People without a project = %v, want ErrNoProject", err)
	}
}
