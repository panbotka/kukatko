//go:build integration

package whatsnewapi_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/panbotka/kukatko/internal/auth"
	"github.com/panbotka/kukatko/internal/database"
	"github.com/panbotka/kukatko/internal/database/dbtest"
	"github.com/panbotka/kukatko/internal/whatsnew"
	"github.com/panbotka/kukatko/internal/whatsnewapi"
)

// These tests run only under `make test-integration` against the database named
// by KUKATKO_TEST_DATABASE_URL. They share one database and truncate per case, so
// they do not run in parallel.

const testPassword = "correct horse battery staple"

// testGap is the inactivity threshold the test store runs on. A test cannot wait
// six hours to see a visit rotate, so the clock is moved instead: the store is
// built with a one-minute gap and the API with an injectable clock, which
// reproduces the production transition exactly without changing its logic.
const testGap = time.Minute

// env wires the auth and what's-new APIs behind an httptest server over the
// integration database, with a clock the test moves by hand.
type env struct {
	server  *httptest.Server
	authSvc *auth.Service
	db      *database.DB
	now     time.Time
}

// newEnv builds the HTTP test environment over a freshly truncated database.
func newEnv(t *testing.T) *env {
	t.Helper()
	db := dbtest.New(t)
	dbtest.TruncateAll(t, db)

	authStore := auth.NewStore(db.Pool())
	authSvc := auth.NewService(authStore, auth.SessionPolicy{TTL: time.Hour, MaxLifetime: 3 * time.Hour})
	authAPI := auth.NewAPI(auth.APIConfig{Service: authSvc, Limiter: auth.NewLimiter(100, time.Minute)})

	e := &env{authSvc: authSvc, db: db, now: time.Date(2026, 8, 9, 9, 0, 0, 0, time.UTC)}
	api := whatsnewapi.NewAPI(whatsnewapi.Config{
		Store:       whatsnew.NewStore(db.Pool()).WithGap(testGap),
		RequireAuth: authAPI.RequireAuth,
		Now:         func() time.Time { return e.now },
	})

	r := chi.NewRouter()
	r.Route("/api/v1", func(r chi.Router) {
		authAPI.RegisterRoutes(r)
		api.RegisterRoutes(r)
	})
	e.server = httptest.NewServer(r)
	t.Cleanup(e.server.Close)
	return e
}

// login creates a user with the given role and returns a cookie-bearing client.
func (e *env) login(t *testing.T, username string, role auth.Role) *http.Client {
	t.Helper()
	if _, err := e.authSvc.CreateUser(t.Context(), auth.CreateUserInput{
		Username: username, Password: testPassword, Role: role,
	}); err != nil {
		t.Fatalf("CreateUser(%s): %v", username, err)
	}
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("cookiejar.New: %v", err)
	}
	client := &http.Client{Jar: jar}
	body, _ := json.Marshal(map[string]string{"username": username, "password": testPassword})
	resp := e.mustDo(t, client, http.MethodPost, "/api/v1/auth/login", body)
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("login status = %d, want 200", resp.StatusCode)
	}
	return client
}

// mustDo issues a request with an optional JSON body and returns the response.
func (e *env) mustDo(t *testing.T, c *http.Client, method, path string, body []byte) *http.Response {
	t.Helper()
	var rdr io.Reader
	if body != nil {
		rdr = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(context.Background(), method, e.server.URL+path, rdr)
	if err != nil {
		t.Fatalf("new request %s %s: %v", method, path, err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.Do(req)
	if err != nil {
		t.Fatalf("do %s %s: %v", method, path, err)
	}
	return resp
}

// summary GETs /whats-new and returns the decoded digest.
func (e *env) summary(t *testing.T, c *http.Client) whatsnew.Summary {
	t.Helper()
	resp := e.mustDo(t, c, http.MethodGet, "/api/v1/whats-new", nil)
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /whats-new status = %d, want 200", resp.StatusCode)
	}
	var got whatsnew.Summary
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatalf("decode digest: %v", err)
	}
	return got
}

// advance moves the injected clock forward by d.
func (e *env) advance(d time.Duration) {
	e.now = e.now.Add(d)
}

// exec runs a statement against the test database, failing the test on error.
func (e *env) exec(t *testing.T, sql string, args ...any) {
	t.Helper()
	if _, err := e.db.Pool().Exec(context.Background(), sql, args...); err != nil {
		t.Fatalf("exec %q: %v", sql, err)
	}
}

// insertPhoto inserts a live library photo created at the given time.
func (e *env) insertPhoto(t *testing.T, uid string, createdAt time.Time) {
	t.Helper()
	e.exec(t, `INSERT INTO photos (uid, file_hash, file_path, created_at, updated_at)
	           VALUES ($1, $2, $3, $4, $4)`, uid, uid, uid+".jpg", createdAt)
}

// insertAlbum inserts a hand-curated album created at the given time.
func (e *env) insertAlbum(t *testing.T, uid, title string, createdAt time.Time) {
	t.Helper()
	e.exec(t, `INSERT INTO albums (uid, slug, title, type, created_at, updated_at)
	           VALUES ($1, $2, $3, 'album', $4, $4)`, uid, uid, title, createdAt)
}

// insertSubject inserts a named person created at the given time.
func (e *env) insertSubject(t *testing.T, uid, name string, createdAt time.Time) {
	t.Helper()
	e.exec(t, `INSERT INTO subjects (uid, slug, name, type, created_at, updated_at)
	           VALUES ($1, $2, $3, 'person', $4, $4)`, uid, uid, name, createdAt)
}

// insertComment inserts a live comment on a photo, created at the given time.
func (e *env) insertComment(t *testing.T, uid, photoUID string, createdAt time.Time) {
	t.Helper()
	e.exec(t, `INSERT INTO photo_comments (uid, photo_uid, body, created_at)
	           VALUES ($1, $2, 'kdo je to?', $3)`, uid, photoUID, createdAt)
}

// TestFirstVisitHasNoDigest: an account reading the summary for the first time
// has no reference point, so nothing is reported however full the library is.
func TestFirstVisitHasNoDigest(t *testing.T) {
	env := newEnv(t)
	env.insertPhoto(t, "ph-old", env.now.Add(-time.Hour))

	viewer := env.login(t, "viewer", auth.RoleViewer)
	if got := env.summary(t, viewer); got.HasNews {
		t.Fatalf("first visit digest = %+v, want has_news false", got)
	}
}

// TestVisitSurvivesReloads: within one visit the reference point does not move,
// so a reload shows the same digest — and a change made mid-visit joins it
// rather than replacing it.
func TestVisitSurvivesReloads(t *testing.T) {
	env := newEnv(t)
	viewer := env.login(t, "viewer", auth.RoleViewer)

	// First read establishes the account; nothing to report yet.
	if got := env.summary(t, viewer); got.HasNews {
		t.Fatalf("first read digest = %+v, want has_news false", got)
	}

	// Come back after a long absence: the reference rotates onto the previous
	// read, and the photo uploaded in between is news.
	env.advance(2 * testGap)
	env.insertPhoto(t, "ph-1", env.now.Add(-time.Minute))
	first := env.summary(t, viewer)
	if !first.HasNews || first.Photos != 1 {
		t.Fatalf("second visit digest = %+v, want 1 new photo", first)
	}

	// Reload twice inside the same visit: same reference, same digest.
	for i := range 2 {
		env.advance(time.Second)
		again := env.summary(t, viewer)
		if !again.HasNews || again.Photos != 1 {
			t.Fatalf("reload %d digest = %+v, want the same 1 new photo", i, again)
		}
		if !again.Since.Equal(first.Since) {
			t.Fatalf("reload %d since = %v, want %v", i, again.Since, first.Since)
		}
	}
}

// TestNewVisitRotatesReference: after the inactivity gap the reference moves to
// the end of the previous visit, so what was already reported is not reported
// again.
func TestNewVisitRotatesReference(t *testing.T) {
	env := newEnv(t)
	viewer := env.login(t, "viewer", auth.RoleViewer)
	env.summary(t, viewer)

	env.advance(2 * testGap)
	env.insertPhoto(t, "ph-1", env.now.Add(-time.Minute))
	if got := env.summary(t, viewer); got.Photos != 1 {
		t.Fatalf("second visit photos = %d, want 1", got.Photos)
	}

	// A third visit with nothing added in between reports nothing: the photo of
	// the previous visit is now behind the reference point.
	env.advance(2 * testGap)
	if got := env.summary(t, viewer); got.HasNews {
		t.Fatalf("third visit digest = %+v, want has_news false", got)
	}
}

// TestCountsAndLinks: the digest counts photos and comments and names the new
// albums and people, with each count reporting the true total.
func TestCountsAndLinks(t *testing.T) {
	env := newEnv(t)
	viewer := env.login(t, "viewer", auth.RoleViewer)
	env.summary(t, viewer)

	env.advance(2 * testGap)
	at := env.now.Add(-time.Minute)
	for _, uid := range []string{"ph-1", "ph-2", "ph-3"} {
		env.insertPhoto(t, uid, at)
	}
	env.insertComment(t, "cm-1", "ph-1", at)
	env.insertComment(t, "cm-2", "ph-1", at)
	env.insertAlbum(t, "al-1", "Léto 2026", at)
	env.insertSubject(t, "su-1", "Anna", at)
	env.insertSubject(t, "su-2", "Bedřich", at)

	got := env.summary(t, viewer)
	if !got.HasNews {
		t.Fatalf("digest = %+v, want has_news true", got)
	}
	if got.Photos != 3 || got.Comments != 2 {
		t.Errorf("photos/comments = %d/%d, want 3/2", got.Photos, got.Comments)
	}
	if got.AlbumCount != 1 || len(got.Albums) != 1 || got.Albums[0].Title != "Léto 2026" {
		t.Errorf("albums = %+v (count %d), want one 'Léto 2026'", got.Albums, got.AlbumCount)
	}
	if got.PersonCount != 2 || len(got.People) != 2 {
		t.Errorf("people = %+v (count %d), want 2", got.People, got.PersonCount)
	}
	// Newest first: the two subjects share a timestamp, so only membership is
	// asserted, not their order.
	names := map[string]bool{}
	for _, p := range got.People {
		names[p.Name] = true
	}
	if !names["Anna"] || !names["Bedřich"] {
		t.Errorf("people names = %v, want Anna and Bedřich", names)
	}
}

// TestExcludedFromCounts: the digest mirrors the library grid — archived and
// hidden photos are not counted, deleted comments are not counted, and neither
// auto-generated albums nor unnamed subjects are announced.
func TestExcludedFromCounts(t *testing.T) {
	env := newEnv(t)
	viewer := env.login(t, "viewer", auth.RoleViewer)
	env.summary(t, viewer)

	env.advance(2 * testGap)
	at := env.now.Add(-time.Minute)
	env.insertPhoto(t, "ph-visible", at)
	env.insertPhoto(t, "ph-archived", at)
	env.exec(t, `UPDATE photos SET archived_at = $2 WHERE uid = $1`, "ph-archived", at)
	env.insertPhoto(t, "ph-hidden", at)
	env.exec(t, `UPDATE photos SET hidden_from_library = true WHERE uid = $1`, "ph-hidden")
	env.insertComment(t, "cm-gone", "ph-visible", at)
	env.exec(t, `UPDATE photo_comments SET deleted_at = $2 WHERE uid = $1`, "cm-gone", at)
	env.exec(t, `INSERT INTO albums (uid, slug, title, type, created_at, updated_at)
	             VALUES ('al-folder', 'al-folder', '2026/08', 'folder', $1, $1)`, at)
	env.exec(t, `INSERT INTO subjects (uid, slug, name, type, created_at, updated_at)
	             VALUES ('su-blank', 'su-blank', '', 'person', $1, $1)`, at)

	got := env.summary(t, viewer)
	if got.Photos != 1 {
		t.Errorf("photos = %d, want 1 (archived and hidden excluded)", got.Photos)
	}
	if got.Comments != 0 {
		t.Errorf("comments = %d, want 0 (soft-deleted excluded)", got.Comments)
	}
	if got.AlbumCount != 0 {
		t.Errorf("albums = %d, want 0 (auto-generated groupings excluded)", got.AlbumCount)
	}
	if got.PersonCount != 0 {
		t.Errorf("people = %d, want 0 (unnamed subjects excluded)", got.PersonCount)
	}
}

// TestLinksAreCapped: a visit that produced more new albums than the panel can
// name links the newest [whatsnew.MaxItems] and still reports the true total.
func TestLinksAreCapped(t *testing.T) {
	env := newEnv(t)
	viewer := env.login(t, "viewer", auth.RoleViewer)
	env.summary(t, viewer)

	env.advance(2 * testGap)
	total := whatsnew.MaxItems + 3
	for i := range total {
		uid := "al-" + string(rune('a'+i))
		env.insertAlbum(t, uid, uid, env.now.Add(-time.Duration(total-i)*time.Second))
	}

	got := env.summary(t, viewer)
	if got.AlbumCount != total {
		t.Errorf("album_count = %d, want %d", got.AlbumCount, total)
	}
	if len(got.Albums) != whatsnew.MaxItems {
		t.Fatalf("linked albums = %d, want %d", len(got.Albums), whatsnew.MaxItems)
	}
	// Newest first: the last album inserted has the latest created_at.
	if want := "al-" + string(rune('a'+total-1)); got.Albums[0].UID != want {
		t.Errorf("first linked album = %q, want %q", got.Albums[0].UID, want)
	}
}

// TestEveryRoleMayRead: the digest is not a curation power — a read-only viewer
// sees it exactly as a maintainer does.
func TestEveryRoleMayRead(t *testing.T) {
	env := newEnv(t)
	roles := []auth.Role{auth.RoleViewer, auth.RoleEditor, auth.RoleAdmin, auth.RoleMaintainer}

	clients := make(map[auth.Role]*http.Client, len(roles))
	for _, role := range roles {
		clients[role] = env.login(t, "u_"+string(role), role)
		env.summary(t, clients[role]) // The first read establishes the account.
	}

	env.advance(2 * testGap)
	env.insertPhoto(t, "ph-1", env.now.Add(-time.Minute))

	for _, role := range roles {
		got := env.summary(t, clients[role])
		if !got.HasNews || got.Photos != 1 {
			t.Errorf("%s digest = %+v, want 1 new photo", role, got)
		}
	}
}

// TestUnauthenticatedRejected: no session cookie is a 401, like every other
// authenticated read.
func TestUnauthenticatedRejected(t *testing.T) {
	env := newEnv(t)
	resp := env.mustDo(t, &http.Client{}, http.MethodGet, "/api/v1/whats-new", nil)
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("anonymous GET status = %d, want 401", resp.StatusCode)
	}
}

// TestVisitsAreIndependentPerUser: one reader's visit does not move another's
// reference point.
func TestVisitsAreIndependentPerUser(t *testing.T) {
	env := newEnv(t)
	alice := env.login(t, "alice", auth.RoleEditor)
	bob := env.login(t, "bob", auth.RoleViewer)
	env.summary(t, alice)
	env.summary(t, bob)

	env.advance(2 * testGap)
	env.insertPhoto(t, "ph-1", env.now.Add(-time.Minute))

	// Alice reads first and consumes the news; Bob's own reference is untouched,
	// so he still learns about the same photo.
	if got := env.summary(t, alice); got.Photos != 1 {
		t.Fatalf("alice photos = %d, want 1", got.Photos)
	}
	if got := env.summary(t, bob); got.Photos != 1 {
		t.Fatalf("bob photos = %d, want 1", got.Photos)
	}
}
