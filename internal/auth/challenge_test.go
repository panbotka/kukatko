package auth

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestWriteUnauthorized_carriesTheBearerChallenge pins the header every 401 this
// package writes must carry. RFC 9110 requires it on a 401, and an MCP client
// that does not find it starts guessing OAuth metadata under /.well-known/…
// instead of reporting that it needs a token (see docs/MCP.md).
//
// The scheme must stay Bearer. Basic would make a browser pop its own native
// sign-in dialog on top of the SPA, which handles its 401s itself.
func TestWriteUnauthorized_carriesTheBearerChallenge(t *testing.T) {
	t.Parallel()

	rec := httptest.NewRecorder()
	writeUnauthorized(rec, "authentication required")

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
	if got := rec.Header().Get("WWW-Authenticate"); got != "Bearer" {
		t.Errorf("WWW-Authenticate = %q, want %q", got, "Bearer")
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json; charset=utf-8" {
		t.Errorf("Content-Type = %q, want JSON", ct)
	}
	if body := rec.Body.String(); body != "{\"error\":\"authentication required\"}\n" {
		t.Errorf("body = %q, want the standard error envelope", body)
	}
}

// TestRequireAuth_unauthenticatedGetsTheChallenge checks the guard itself, not
// just the helper: a request with no credential is refused with the challenge
// and the wrapped handler never runs.
func TestRequireAuth_unauthenticatedGetsTheChallenge(t *testing.T) {
	t.Parallel()

	called := false
	api := &API{svc: &Service{}}
	guarded := api.RequireAuth(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		called = true
	}))

	rec := httptest.NewRecorder()
	guarded.ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api/v1/photos", nil))

	if called {
		t.Fatal("the guarded handler ran for an unauthenticated request")
	}
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
	if got := rec.Header().Get("WWW-Authenticate"); got != Challenge {
		t.Errorf("WWW-Authenticate = %q, want %q", got, Challenge)
	}
}
