package searchhistoryapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/panbotka/kukatko/internal/searchhistory"
)

// passthrough is a guard that authenticates nothing, used to reach the handlers
// directly. Requests through it carry no principal, which is exactly what the
// fail-closed test needs; the authenticated paths are driven through the real
// auth guard in the integration test.
func passthrough(next http.Handler) http.Handler { return next }

// fakeStore records the calls it receives, so the handlers can be driven without
// a database. The real SQL is exercised in the integration test.
type fakeStore struct {
	recorded []string
	cleared  int
	listed   int
}

// List counts the call and returns an empty history.
func (f *fakeStore) List(context.Context, string) ([]searchhistory.Entry, error) {
	f.listed++
	return nil, nil
}

// Record appends the query to the recorded log.
func (f *fakeStore) Record(_ context.Context, _, query string) error {
	f.recorded = append(f.recorded, query)
	return nil
}

// Clear counts the call.
func (f *fakeStore) Clear(context.Context, string) error {
	f.cleared++
	return nil
}

// newRouter mounts the API with a pass-through guard over store.
func newRouter(store Store) *chi.Mux {
	api := NewAPI(Config{Store: store, RequireAuth: passthrough})
	router := chi.NewRouter()
	api.RegisterRoutes(router)
	return router
}

// do issues a request against router and returns the recorder.
func do(t *testing.T, router *chi.Mux, method, body string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(t.Context(), method, "/search-history", nil)
	if body != "" {
		req = httptest.NewRequestWithContext(t.Context(), method, "/search-history", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
	}
	router.ServeHTTP(rec, req)
	return rec
}

// TestWithoutPrincipal verifies every handler fails closed: a request that
// reaches it without an authenticated user — a guard wired wrong — is refused
// rather than served or applied to somebody's history, and the store is untouched.
func TestWithoutPrincipal(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		method string
		body   string
	}{
		{http.MethodGet, ""},
		{http.MethodPost, `{"query":"svatba"}`},
		{http.MethodDelete, ""},
	} {
		store := &fakeStore{}
		rec := do(t, newRouter(store), tt.method, tt.body)
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("%s status = %d, want 401", tt.method, rec.Code)
		}
		if store.listed != 0 || len(store.recorded) != 0 || store.cleared != 0 {
			t.Errorf("%s touched the store: %+v", tt.method, store)
		}
	}
}

// TestDecodeRecord verifies the record body's validation: a query survives
// verbatim (normalisation is the store's job), while a blank one, an unknown field
// and a malformed body are all rejected before the store is reached.
func TestDecodeRecord(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		body    string
		want    string
		wantErr bool
	}{
		{name: "plain query", body: `{"query":"svatba 1974"}`, want: "svatba 1974"},
		{name: "untrimmed query passes through", body: `{"query":"  person:Anna "}`, want: "  person:Anna "},
		{name: "blank query", body: `{"query":"   "}`, wantErr: true},
		{name: "missing query", body: `{}`, wantErr: true},
		{name: "unknown field", body: `{"query":"x","user_uid":"usbob"}`, wantErr: true},
		{name: "not json", body: `nonsense`, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			req := httptest.NewRequestWithContext(
				t.Context(), http.MethodPost, "/search-history", strings.NewReader(tt.body),
			)
			in, err := decodeRecord(req)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("decodeRecord(%s) error = nil, want an error", tt.body)
				}
				return
			}
			if err != nil {
				t.Fatalf("decodeRecord(%s): %v", tt.body, err)
			}
			if in.Query != tt.want {
				t.Errorf("query = %q, want %q", in.Query, tt.want)
			}
		})
	}
}

// TestDecodeRecordRejectsOversizedBody verifies the body limit bites: a query far
// longer than the limit is refused rather than read into memory whole.
func TestDecodeRecordRejectsOversizedBody(t *testing.T) {
	t.Parallel()

	body := `{"query":"` + strings.Repeat("a", maxBodyBytes*2) + `"}`
	req := httptest.NewRequestWithContext(
		t.Context(), http.MethodPost, "/search-history", strings.NewReader(body),
	)
	if _, err := decodeRecord(req); err == nil {
		t.Errorf("decodeRecord(oversized) error = nil, want an error")
	}
}

// TestListResponse verifies an empty history serialises as an empty array rather
// than null, so the client always parses one shape.
func TestListResponse(t *testing.T) {
	t.Parallel()

	empty, err := json.Marshal(listResponse(nil))
	if err != nil {
		t.Fatalf("marshalling empty history: %v", err)
	}
	if string(empty) != `{"searches":[]}` {
		t.Errorf("empty history = %s, want an empty array", empty)
	}

	at := time.Date(2026, time.August, 9, 12, 0, 0, 0, time.UTC)
	filled, err := json.Marshal(listResponse([]searchhistory.Entry{{Query: "svatba", SearchedAt: at}}))
	if err != nil {
		t.Fatalf("marshalling history: %v", err)
	}
	want := `{"searches":[{"query":"svatba","searched_at":"2026-08-09T12:00:00Z"}]}`
	if string(filled) != want {
		t.Errorf("history = %s, want %s", filled, want)
	}
}

// TestWriteError verifies the standard error envelope.
func TestWriteError(t *testing.T) {
	t.Parallel()

	rec := httptest.NewRecorder()
	writeError(rec, http.StatusInternalServerError, "boom")
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", rec.Code)
	}
	var body errorBody
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decoding body: %v", err)
	}
	if body.Error != "boom" {
		t.Errorf("error = %q, want %q", body.Error, "boom")
	}
	if got := rec.Header().Get("Content-Type"); got != "application/json; charset=utf-8" {
		t.Errorf("Content-Type = %q", got)
	}
}
