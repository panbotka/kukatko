package globalsearchapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/panbotka/kukatko/internal/organize"
	"github.com/panbotka/kukatko/internal/people"
	"github.com/panbotka/kukatko/internal/photos"
)

// fakeSearcher is an in-memory implementation of every store interface the
// handler needs. It records the query and limit it was asked for and returns
// canned rows or a canned error.
type fakeSearcher struct {
	gotQuery string
	gotLimit int
	albums   []organize.AlbumCount
	labels   []organize.LabelCount
	subjects []people.Subject
	photos   []photos.Photo
	err      error
}

// SearchAlbums records the query/limit and returns the canned albums or error.
func (f *fakeSearcher) SearchAlbums(_ context.Context, q string, limit int) ([]organize.AlbumCount, error) {
	f.gotQuery, f.gotLimit = q, limit
	return f.albums, f.err
}

// SearchLabels returns the canned labels or error.
func (f *fakeSearcher) SearchLabels(_ context.Context, _ string, _ int) ([]organize.LabelCount, error) {
	return f.labels, f.err
}

// SearchSubjects returns the canned subjects or error.
func (f *fakeSearcher) SearchSubjects(_ context.Context, _ string, _ int) ([]people.Subject, error) {
	return f.subjects, f.err
}

// Search returns the canned photos or error, ignoring the list params beyond
// what the handler set on them.
func (f *fakeSearcher) Search(_ context.Context, _ photos.ListParams) ([]photos.Photo, error) {
	return f.photos, f.err
}

// passthrough is an auth guard stand-in that admits every request.
func passthrough(next http.Handler) http.Handler { return next }

// newTestServer mounts the global-search API backed by f under /api/v1 with the
// given per-group limit (0 uses the package default).
func newTestServer(f *fakeSearcher, limit int) *httptest.Server {
	api := NewAPI(Config{
		Organizer: f, People: f, Photos: f, Limit: limit, RequireAuth: passthrough,
	})
	r := chi.NewRouter()
	r.Route("/api/v1", api.RegisterRoutes)
	return httptest.NewServer(r)
}

// getGlobal issues a context-aware GET against the global-search endpoint.
func getGlobal(t *testing.T, url string) *http.Response {
	t.Helper()
	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, url, nil)
	if err != nil {
		t.Fatalf("NewRequest %s: %v", url, err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}
	return resp
}

// TestHandleGlobal_grouped verifies the endpoint returns every entity group under
// the grouped envelope and echoes the query.
func TestHandleGlobal_grouped(t *testing.T) {
	t.Parallel()
	cover := "ph-cover"
	f := &fakeSearcher{
		albums:   []organize.AlbumCount{{Album: organize.Album{UID: "al1", Title: "Dovolená", CoverPhotoUID: &cover}, PhotoCount: 3}},
		labels:   []organize.LabelCount{{Label: organize.Label{UID: "lb1", Name: "sunset"}, PhotoCount: 7}},
		subjects: []people.Subject{{UID: "su1", Name: "Tomáš", CoverPhotoUID: &cover}},
		photos:   []photos.Photo{{UID: "ph1"}},
	}
	srv := newTestServer(f, 0)
	defer srv.Close()

	resp := getGlobal(t, srv.URL+"/api/v1/search/global?q=dov")
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	var body response
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Query != "dov" {
		t.Fatalf("query = %q, want dov", body.Query)
	}
	if len(body.Albums) != 1 || body.Albums[0].UID != "al1" || body.Albums[0].PhotoCount != 3 {
		t.Fatalf("albums = %+v, want one al1/3", body.Albums)
	}
	if body.Albums[0].Cover == nil || *body.Albums[0].Cover != cover {
		t.Fatalf("album cover = %v, want %q", body.Albums[0].Cover, cover)
	}
	if len(body.Labels) != 1 || body.Labels[0].Name != "sunset" || body.Labels[0].PhotoCount != 7 {
		t.Fatalf("labels = %+v, want one sunset/7", body.Labels)
	}
	if len(body.People) != 1 || body.People[0].Name != "Tomáš" {
		t.Fatalf("people = %+v, want one Tomáš", body.People)
	}
	if len(body.Photos) != 1 || body.Photos[0].UID != "ph1" {
		t.Fatalf("photos = %+v, want one ph1", body.Photos)
	}
}

// TestHandleGlobal_trimsAndPassesLimit verifies the query is trimmed and the
// configured per-group limit reaches the stores.
func TestHandleGlobal_trimsAndPassesLimit(t *testing.T) {
	t.Parallel()
	f := &fakeSearcher{}
	srv := newTestServer(f, 5)
	defer srv.Close()

	resp := getGlobal(t, srv.URL+"/api/v1/search/global?q=%20%20dovolena%20%20")
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if f.gotQuery != "dovolena" {
		t.Fatalf("store saw query %q, want trimmed dovolena", f.gotQuery)
	}
	if f.gotLimit != 5 {
		t.Fatalf("store saw limit %d, want 5", f.gotLimit)
	}
}

// TestHandleGlobal_emptyQuery verifies a blank or whitespace-only q is 400 and no
// store call is made.
func TestHandleGlobal_emptyQuery(t *testing.T) {
	t.Parallel()
	for _, q := range []string{"", "%20%20"} {
		f := &fakeSearcher{}
		srv := newTestServer(f, 0)
		resp := getGlobal(t, srv.URL+"/api/v1/search/global?q="+q)
		if resp.StatusCode != http.StatusBadRequest {
			_ = resp.Body.Close()
			srv.Close()
			t.Fatalf("q=%q status = %d, want 400", q, resp.StatusCode)
		}
		if f.gotQuery != "" {
			_ = resp.Body.Close()
			srv.Close()
			t.Fatalf("store was queried for blank q")
		}
		_ = resp.Body.Close()
		srv.Close()
	}
}

// TestHandleGlobal_storeError verifies a store failure becomes a 500.
func TestHandleGlobal_storeError(t *testing.T) {
	t.Parallel()
	f := &fakeSearcher{err: errors.New("boom")}
	srv := newTestServer(f, 0)
	defer srv.Close()

	resp := getGlobal(t, srv.URL+"/api/v1/search/global?q=dovolena")
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", resp.StatusCode)
	}
}

// TestNewAPI_defaultLimit verifies a non-positive configured limit falls back to
// the package default.
func TestNewAPI_defaultLimit(t *testing.T) {
	t.Parallel()
	f := &fakeSearcher{}
	srv := newTestServer(f, 0)
	defer srv.Close()

	resp := getGlobal(t, srv.URL+"/api/v1/search/global?q=x")
	defer func() { _ = resp.Body.Close() }()
	if f.gotLimit != defaultGroupLimit {
		t.Fatalf("store saw limit %d, want default %d", f.gotLimit, defaultGroupLimit)
	}
}
