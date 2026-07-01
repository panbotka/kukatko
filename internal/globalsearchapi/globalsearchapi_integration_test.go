//go:build integration

package globalsearchapi_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/panbotka/kukatko/internal/database/dbtest"
	"github.com/panbotka/kukatko/internal/globalsearchapi"
	"github.com/panbotka/kukatko/internal/organize"
	"github.com/panbotka/kukatko/internal/people"
	"github.com/panbotka/kukatko/internal/photos"
)

// These tests run only under `make test-integration` against the database named
// by KUKATKO_TEST_DATABASE_URL. They share one database and truncate up front, so
// they intentionally do not run in parallel.

// globalHit mirrors the endpoint's JSON envelope for decoding in the test.
type globalHit struct {
	Query  string `json:"query"`
	Albums []struct {
		UID        string `json:"uid"`
		Title      string `json:"title"`
		PhotoCount int    `json:"photo_count"`
	} `json:"albums"`
	Labels []struct {
		UID  string `json:"uid"`
		Name string `json:"name"`
	} `json:"labels"`
	People []struct {
		UID  string `json:"uid"`
		Name string `json:"name"`
	} `json:"people"`
	Photos []struct {
		UID   string `json:"uid"`
		Title string `json:"title"`
	} `json:"photos"`
}

// passthrough admits every request, isolating the handler from real auth.
func passthrough(next http.Handler) http.Handler { return next }

// newEnv truncates the integration database, seeds one accent-bearing match of
// each kind (plus extras to exercise the per-group cap), and returns a running
// server backed by the real stores.
func newEnv(t *testing.T, limit int) *httptest.Server {
	t.Helper()
	db := dbtest.New(t)
	dbtest.TruncateAll(t, db)
	ctx := context.Background()

	organizeStore := organize.NewStore(db.Pool())
	peopleStore := people.NewStore(db.Pool())
	photoStore := photos.NewStore(db.Pool())

	// Albums: two accent-bearing title matches for "dovolena" to test the cap.
	for _, title := range []string{"Dovolená u moře", "Dovolená v horách"} {
		if _, err := organizeStore.CreateAlbum(ctx, organize.Album{Title: title}); err != nil {
			t.Fatalf("CreateAlbum %q: %v", title, err)
		}
	}
	if _, err := organizeStore.CreateLabel(ctx, organize.Label{Name: "Dovolená"}); err != nil {
		t.Fatalf("CreateLabel: %v", err)
	}
	if _, err := peopleStore.CreateSubject(ctx, people.Subject{Name: "Dovolená s Tomášem"}); err != nil {
		t.Fatalf("CreateSubject: %v", err)
	}
	if _, err := photoStore.Create(ctx, photos.Photo{
		Title: "Dovolená 2024", FileHash: "gs-hash", FilePath: "2024/01/gs.jpg", FileName: "gs.jpg",
	}); err != nil {
		t.Fatalf("Create photo: %v", err)
	}

	api := globalsearchapi.NewAPI(globalsearchapi.Config{
		Organizer: organizeStore, People: peopleStore, Photos: photoStore,
		Limit: limit, RequireAuth: passthrough,
	})
	r := chi.NewRouter()
	r.Route("/api/v1", api.RegisterRoutes)
	return httptest.NewServer(r)
}

// getGlobal issues a GET and decodes the grouped body, asserting a 200.
func getGlobal(t *testing.T, url string) globalHit {
	t.Helper()
	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, url, nil)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	var body globalHit
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	return body
}

// TestGlobalSearch_matchesEveryGroup verifies a single accent-insensitive query
// finds a match in each entity group at once.
func TestGlobalSearch_matchesEveryGroup(t *testing.T) {
	srv := newEnv(t, 10)
	defer srv.Close()

	// "dovolena" is unaccented and lower-case; every seed carries "Dovolená".
	got := getGlobal(t, srv.URL+"/api/v1/search/global?q=dovolena")
	if got.Query != "dovolena" {
		t.Fatalf("query = %q, want dovolena", got.Query)
	}
	if len(got.Albums) != 2 {
		t.Fatalf("albums = %d, want 2", len(got.Albums))
	}
	if len(got.Labels) != 1 || got.Labels[0].Name != "Dovolená" {
		t.Fatalf("labels = %+v, want [Dovolená]", got.Labels)
	}
	if len(got.People) != 1 || got.People[0].Name != "Dovolená s Tomášem" {
		t.Fatalf("people = %+v, want [Dovolená s Tomášem]", got.People)
	}
	if len(got.Photos) != 1 || got.Photos[0].Title != "Dovolená 2024" {
		t.Fatalf("photos = %+v, want [Dovolená 2024]", got.Photos)
	}
}

// TestGlobalSearch_perGroupLimit verifies each group is capped at the configured
// limit even when more rows match.
func TestGlobalSearch_perGroupLimit(t *testing.T) {
	srv := newEnv(t, 1)
	defer srv.Close()

	got := getGlobal(t, srv.URL+"/api/v1/search/global?q=dovolena")
	if len(got.Albums) != 1 {
		t.Fatalf("albums = %d, want 1 (capped)", len(got.Albums))
	}
}

// TestGlobalSearch_emptyQuery verifies a blank query is rejected with 400.
func TestGlobalSearch_emptyQuery(t *testing.T) {
	srv := newEnv(t, 10)
	defer srv.Close()

	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, srv.URL+"/api/v1/search/global?q=%20", nil)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
}
