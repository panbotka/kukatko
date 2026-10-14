package resolveapi

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
)

// panicResolver fails the test if the handler ever reaches it.
type panicResolver struct{ t *testing.T }

func (p panicResolver) ProjectSlug(context.Context, Kind, string, Caller) (string, error) {
	p.t.Error("the resolver was asked without a signed-in caller")
	return "", ErrNotFound
}

// TestHandleResolve_withoutCallerIsRefused: a request that reaches the handler
// without a signed-in caller (a guard wired wrong) is a 401 and asks nothing.
// The answers for a signed-in caller are the integration test's.
func TestHandleResolve_withoutCallerIsRefused(t *testing.T) {
	t.Parallel()
	pass := func(next http.Handler) http.Handler { return next }
	r := chi.NewRouter()
	NewAPI(Config{Resolver: panicResolver{t}, RequireSignedIn: pass}).RegisterRoutes(r)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/resolve/photo/ph1", nil))
	body, _ := io.ReadAll(rec.Body)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("got %d %s, want 401", rec.Code, body)
	}
}

func TestStore_unknownKindAsksNothing(t *testing.T) {
	t.Parallel()
	// A nil pool would panic if the store reached the database.
	s := NewStore(nil)
	for _, tc := range []struct {
		kind   Kind
		uid    string
		caller Caller
	}{
		{"saved", "ss1", Caller{UserUID: "us1"}},
		{KindPhoto, "", Caller{UserUID: "us1"}},
		{KindPhoto, "ph1", Caller{}},
	} {
		if _, err := s.ProjectSlug(t.Context(), tc.kind, tc.uid, tc.caller); !errors.Is(err, ErrNotFound) {
			t.Errorf("ProjectSlug(%q, %q, %+v) = %v, want ErrNotFound", tc.kind, tc.uid, tc.caller, err)
		}
	}
}
