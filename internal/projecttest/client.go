//go:build integration

package projecttest

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/panbotka/kukatko/internal/apitest"
	"github.com/panbotka/kukatko/internal/auth"
)

// ClientFor returns a client signed in on server as the member of which: it
// carries the session cookie POST /api/v1/auth/login set. It runs on
// apitest.Client's pool-free transport (never http.DefaultClient) with a cookie
// jar of its own, so two sides' clients never share a session. The fixture
// remembers the client's side and server, which is how Get and the HTTP
// assertions know where to send a path and whose rows must stay out of the
// answer.
func (f *Fixture) ClientFor(tb testing.TB, server *httptest.Server, which Side) *http.Client {
	tb.Helper()
	jar, err := cookiejar.New(nil)
	if err != nil {
		tb.Fatalf("projecttest: cookiejar.New: %v", err)
	}
	client := *apitest.Client()
	client.Jar = jar

	body, err := json.Marshal(map[string]string{"username": f.Tenant(which).Username, "password": Password})
	if err != nil {
		tb.Fatalf("projecttest: encoding the login: %v", err)
	}
	req, err := http.NewRequestWithContext(tb.Context(), http.MethodPost,
		server.URL+auth.APIBasePath+"/auth/login", bytes.NewReader(body))
	if err != nil {
		tb.Fatalf("projecttest: building the login request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		tb.Fatalf("projecttest: signing %s in: %v", which, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		answer, _ := io.ReadAll(resp.Body)
		tb.Fatalf("projecttest: signing %s in = %d, want 200; body %s", which, resp.StatusCode, answer)
	}

	f.mu.Lock()
	defer f.mu.Unlock()
	f.clients[&client] = signedIn{side: which, baseURL: server.URL}
	return &client
}

// PathFor returns the path of suffix inside the project of which:
// "/api/v1/p/<slug>/<suffix>". A leading slash on suffix is optional.
func (f *Fixture) PathFor(which Side, suffix string) string {
	return auth.APIBasePath + "/p/" + f.Tenant(which).Slug + "/" + strings.TrimPrefix(suffix, "/")
}

// Get sends GET path (a PathFor result, or any server path) as client, which
// must come from ClientFor, and returns the response with its body already
// read into memory: the caller need not close it, and AssertSameShape can read
// it.
func (f *Fixture) Get(tb testing.TB, client *http.Client, path string) *http.Response {
	tb.Helper()
	status, header, body := fetch(tb, client, f.lookup(tb, client).baseURL, path)
	return &http.Response{StatusCode: status, Header: header, Body: io.NopCloser(bytes.NewReader(body))}
}

// Send sends method path (a PathFor result, or any server path) as client,
// which must come from ClientFor, with body encoded as JSON — or no body when
// body is nil — and returns the response with its body already read into
// memory, like Get. It is how a test drives a write (PATCH, PUT, POST, DELETE)
// at the other project's row and compares the answer with AssertSameShape.
func (f *Fixture) Send(tb testing.TB, client *http.Client, method, path string, body any) *http.Response {
	tb.Helper()
	var payload io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			tb.Fatalf("projecttest: encoding the %s %s body: %v", method, path, err)
		}
		payload = bytes.NewReader(encoded)
	}
	status, header, answer := do(tb, client, method, f.lookup(tb, client).baseURL+path, payload)
	return &http.Response{StatusCode: status, Header: header, Body: io.NopCloser(bytes.NewReader(answer))}
}

// lookup returns what the fixture remembers about client, failing tb when
// client did not come from ClientFor.
func (f *Fixture) lookup(tb testing.TB, client *http.Client) signedIn {
	tb.Helper()
	f.mu.Lock()
	defer f.mu.Unlock()
	info, ok := f.clients[client]
	if !ok {
		tb.Fatalf("projecttest: the client was not signed in through this fixture's ClientFor")
	}
	return info
}

// fetch sends GET baseURL+path as client and returns the answer's status,
// headers and whole body.
func fetch(tb testing.TB, client *http.Client, baseURL, path string) (int, http.Header, []byte) {
	tb.Helper()
	return do(tb, client, http.MethodGet, baseURL+path, nil)
}

// do sends method target as client with payload (JSON, or nil for none) and
// returns the answer's status, headers and whole body.
func do(tb testing.TB, client *http.Client, method, target string, payload io.Reader) (int, http.Header, []byte) {
	tb.Helper()
	req, err := http.NewRequestWithContext(tb.Context(), method, target, payload)
	if err != nil {
		tb.Fatalf("projecttest: building %s %s: %v", method, target, err)
	}
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := client.Do(req)
	if err != nil {
		tb.Fatalf("projecttest: %s %s: %v", method, target, err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		tb.Fatalf("projecttest: reading %s %s: %v", method, target, err)
	}
	return resp.StatusCode, resp.Header, body
}
