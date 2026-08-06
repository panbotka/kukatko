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

// directHit mirrors the endpoint's uid-lookup payload for decoding in the test.
type directHit struct {
	UID        string   `json:"uid"`
	Kind       string   `json:"kind"`
	Found      bool     `json:"found"`
	TargetKind string   `json:"target_kind"`
	TargetUID  string   `json:"target_uid"`
	Title      string   `json:"title"`
	States     []string `json:"states"`
}

// globalHit mirrors the endpoint's JSON envelope for decoding in the test.
type globalHit struct {
	Query  string     `json:"query"`
	Direct *directHit `json:"direct"`
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

// uidEnv is the fixture the uid-lookup tests run against: a server over the real
// stores plus the ids of everything it seeded.
type uidEnv struct {
	srv *httptest.Server
	// uids maps a fixture name to the id it was given.
	uids map[string]string
}

// newUIDEnv seeds one row of every kind a pasted id can name — a photo with a
// PhotoPrism source uid, an album, a label, a subject, a marker on the photo, a
// stack the photo is the primary of, and an archived photo — and returns a
// running server over the real stores.
func newUIDEnv(t *testing.T) uidEnv {
	t.Helper()
	db := dbtest.New(t)
	dbtest.TruncateAll(t, db)
	ctx := context.Background()

	organizeStore := organize.NewStore(db.Pool())
	peopleStore := people.NewStore(db.Pool())
	photoStore := photos.NewStore(db.Pool())
	env := uidEnv{uids: map[string]string{}}

	ppUID := uidFixturePhotoprism
	photo, err := photoStore.Create(ctx, photos.Photo{
		Title: "Beach sunset", FileHash: "uid-1", FilePath: "2024/01/a.jpg", FileName: "a.jpg",
		PhotoprismUID: &ppUID,
	})
	if err != nil {
		t.Fatalf("Create photo: %v", err)
	}
	variant, err := photoStore.Create(ctx, photos.Photo{
		FileHash: "uid-2", FilePath: "2024/01/a.dng", FileName: "a.dng",
	})
	if err != nil {
		t.Fatalf("Create variant: %v", err)
	}
	archived, err := photoStore.Create(ctx, photos.Photo{
		Title: "Old attic", FileHash: "uid-3", FilePath: "2020/01/c.jpg", FileName: "c.jpg",
	})
	if err != nil {
		t.Fatalf("Create archived photo: %v", err)
	}
	if _, err := photoStore.Archive(ctx, archived.UID); err != nil {
		t.Fatalf("Archive: %v", err)
	}
	stackUID, err := photoStore.CreateStack(ctx, photo.UID, []string{photo.UID, variant.UID})
	if err != nil {
		t.Fatalf("CreateStack: %v", err)
	}

	album, err := organizeStore.CreateAlbum(ctx, organize.Album{Title: "Léto 2024"})
	if err != nil {
		t.Fatalf("CreateAlbum: %v", err)
	}
	label, err := organizeStore.CreateLabel(ctx, organize.Label{Name: "sunset"})
	if err != nil {
		t.Fatalf("CreateLabel: %v", err)
	}
	subject, err := peopleStore.CreateSubject(ctx, people.Subject{Name: "Alice"})
	if err != nil {
		t.Fatalf("CreateSubject: %v", err)
	}
	marker, err := peopleStore.CreateMarker(ctx, people.Marker{
		PhotoUID: photo.UID, SubjectUID: &subject.UID, Type: people.MarkerFace,
		X: 0.1, Y: 0.1, W: 0.2, H: 0.2,
	})
	if err != nil {
		t.Fatalf("CreateMarker: %v", err)
	}

	env.uids = map[string]string{
		"photo": photo.UID, "variant": variant.UID, "archived": archived.UID,
		"stack": stackUID, "album": album.UID, "label": label.UID,
		"person": subject.UID, "marker": marker.UID,
	}

	api := globalsearchapi.NewAPI(globalsearchapi.Config{
		Organizer: organizeStore, People: peopleStore, Photos: photoStore,
		Limit: 10, RequireAuth: passthrough,
	})
	r := chi.NewRouter()
	r.Route("/api/v1", api.RegisterRoutes)
	env.srv = httptest.NewServer(r)
	return env
}

// uidFixturePhotoprism is the PhotoPrism source uid the seeded photo was
// imported under.
const uidFixturePhotoprism = "pt8suk5b57jgshdz"

// TestGlobalSearch_directUID verifies every uid prefix resolves against the real
// database: a photo, album, label and subject by their own id, a marker to the
// photo it sits on, a stack to its primary, and a PhotoPrism id to the catalogue
// row that carries it.
func TestGlobalSearch_directUID(t *testing.T) {
	env := newUIDEnv(t)
	defer env.srv.Close()

	tests := []struct {
		name       string
		uid        string
		wantKind   string
		wantTarget string
		wantUID    string
		wantTitle  string
	}{
		{"photo", env.uids["photo"], "photo", "photo", env.uids["photo"], "Beach sunset"},
		{"album", env.uids["album"], "album", "album", env.uids["album"], "Léto 2024"},
		{"label", env.uids["label"], "label", "label", env.uids["label"], "sunset"},
		{"person", env.uids["person"], "person", "person", env.uids["person"], "Alice"},
		{"marker", env.uids["marker"], "marker", "photo", env.uids["photo"], "Beach sunset"},
		{"stack", env.uids["stack"], "stack", "photo", env.uids["photo"], "Beach sunset"},
		// A shipped feature (v0.5.0) and the regression to fear after the
		// PhotoPrism/photo-sorter import was removed in August 2026: paste a `pt…`
		// source id into search and the app must still take you to that photo.
		// `photos.photoprism_uid` is provenance, not an import leftover.
		{"photoprism", uidFixturePhotoprism, "photoprism", "photo", env.uids["photo"], "Beach sunset"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := getGlobal(t, env.srv.URL+"/api/v1/search/global?q="+tt.uid)
			if got.Direct == nil {
				t.Fatalf("q=%s: no direct hit", tt.uid)
			}
			d := got.Direct
			if !d.Found || d.Kind != tt.wantKind || d.TargetKind != tt.wantTarget ||
				d.TargetUID != tt.wantUID || d.Title != tt.wantTitle {
				t.Fatalf("q=%s: direct = %+v, want %s→%s/%s %q",
					tt.uid, *d, tt.wantKind, tt.wantTarget, tt.wantUID, tt.wantTitle)
			}
			if len(got.Albums) != 0 || len(got.Labels) != 0 || len(got.People) != 0 || len(got.Photos) != 0 {
				t.Fatalf("q=%s: the fuzzy groups also ran: %+v", tt.uid, got)
			}
		})
	}
}

// TestGlobalSearch_directUIDStates verifies an id reaches a photo the library
// view hides and labels the state it is in: an archived photo and a non-primary
// stack member both resolve, and both say why they are not in the grid.
func TestGlobalSearch_directUIDStates(t *testing.T) {
	env := newUIDEnv(t)
	defer env.srv.Close()

	tests := []struct {
		name      string
		uid       string
		wantState string
	}{
		{"archived", env.uids["archived"], "archived"},
		{"stack member", env.uids["variant"], "stack_member"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := getGlobal(t, env.srv.URL+"/api/v1/search/global?q="+tt.uid)
			if got.Direct == nil || !got.Direct.Found || got.Direct.TargetUID != tt.uid {
				t.Fatalf("q=%s: direct = %+v, want a found photo", tt.uid, got.Direct)
			}
			if len(got.Direct.States) != 1 || got.Direct.States[0] != tt.wantState {
				t.Fatalf("q=%s: states = %v, want [%s]", tt.uid, got.Direct.States, tt.wantState)
			}
		})
	}
}

// TestGlobalSearch_directUIDNotFound verifies a well-formed id that names
// nothing is reported as such, rather than as an empty free-text result that
// looks like a broken search.
func TestGlobalSearch_directUIDNotFound(t *testing.T) {
	env := newUIDEnv(t)
	defer env.srv.Close()

	got := getGlobal(t, env.srv.URL+"/api/v1/search/global?q=al000000000000000000000000")
	if got.Direct == nil {
		t.Fatal("no direct hit for a well-formed uid")
	}
	if got.Direct.Found || got.Direct.Kind != "album" {
		t.Fatalf("direct = %+v, want an unresolved album id", *got.Direct)
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
