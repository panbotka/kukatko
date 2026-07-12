//go:build integration

package duplicatesapi_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"slices"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/panbotka/kukatko/internal/audit"
	"github.com/panbotka/kukatko/internal/auth"
	"github.com/panbotka/kukatko/internal/database/dbtest"
	"github.com/panbotka/kukatko/internal/duplicatesapi"
	"github.com/panbotka/kukatko/internal/dupmerge"
	"github.com/panbotka/kukatko/internal/organize"
	"github.com/panbotka/kukatko/internal/people"
	"github.com/panbotka/kukatko/internal/photos"
)

// These tests run only under `make test-integration` against the database named
// by KUKATKO_TEST_DATABASE_URL. They share one database and truncate per case, so
// they do not run in parallel.

const testPassword = "correct horse battery staple"

// env wires the auth and duplicates APIs behind an httptest server over the
// integration database, plus the stores used to seed and verify state.
type env struct {
	server   *httptest.Server
	authSvc  *auth.Service
	photos   *photos.Store
	organize *organize.Store
	people   *people.Store
	audit    *audit.Store
}

// newEnv builds the HTTP test environment over a freshly truncated database with
// the real dupmerge service behind the write guard.
func newEnv(t *testing.T) *env {
	t.Helper()
	db := dbtest.New(t)
	dbtest.TruncateAll(t, db)

	authStore := auth.NewStore(db.Pool())
	authSvc := auth.NewService(authStore, auth.SessionPolicy{TTL: time.Hour, MaxLifetime: 3 * time.Hour})
	authAPI := auth.NewAPI(auth.APIConfig{Service: authSvc, Limiter: auth.NewLimiter(100, time.Minute)})

	api := duplicatesapi.NewAPI(duplicatesapi.Config{
		Merge:        dupmerge.NewService(db.Pool()),
		RequireWrite: authAPI.RequireWrite,
	})

	r := chi.NewRouter()
	r.Route("/api/v1", func(r chi.Router) {
		authAPI.RegisterRoutes(r)
		api.RegisterRoutes(r)
	})
	server := httptest.NewServer(r)
	t.Cleanup(server.Close)
	return &env{
		server:   server,
		authSvc:  authSvc,
		photos:   photos.NewStore(db.Pool()),
		organize: organize.NewStore(db.Pool()),
		people:   people.NewStore(db.Pool()),
		audit:    audit.NewStore(db.Pool()),
	}
}

// login creates a user with the given role and returns a cookie-bearing client
// plus the new user's UID.
func (e *env) login(t *testing.T, username string, role auth.Role) (*http.Client, string) {
	t.Helper()
	user, err := e.authSvc.CreateUser(t.Context(), auth.CreateUserInput{
		Username: username, Password: testPassword, Role: role,
	})
	if err != nil {
		t.Fatalf("CreateUser(%s): %v", username, err)
	}
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("cookiejar.New: %v", err)
	}
	client := &http.Client{Jar: jar}
	body, _ := json.Marshal(map[string]string{"username": username, "password": testPassword})
	resp := e.do(t, client, http.MethodPost, "/api/v1/auth/login", body)
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("login status = %d, want 200", resp.StatusCode)
	}
	return client, user.UID
}

// seedPhoto inserts a photo with the given hash, title, description and pixel
// dimensions, returning its UID.
func (e *env) seedPhoto(t *testing.T, hash, title, description string, width, height int) string {
	t.Helper()
	p, err := e.photos.Create(t.Context(), photos.Photo{
		FileHash: hash, FilePath: "2024/01/" + hash + ".jpg", FileName: hash + ".jpg",
		FileMime: "image/jpeg", FileWidth: width, FileHeight: height,
		Title: title, Description: description,
	})
	if err != nil {
		t.Fatalf("seed photo %s: %v", hash, err)
	}
	return p.UID
}

// makeAlbum creates an album and returns its UID.
func (e *env) makeAlbum(t *testing.T, title string) string {
	t.Helper()
	a, err := e.organize.CreateAlbum(t.Context(), organize.Album{Title: title})
	if err != nil {
		t.Fatalf("create album %s: %v", title, err)
	}
	return a.UID
}

// makeLabel creates a label and returns its UID.
func (e *env) makeLabel(t *testing.T, name string) string {
	t.Helper()
	l, err := e.organize.CreateLabel(t.Context(), organize.Label{Name: name})
	if err != nil {
		t.Fatalf("create label %s: %v", name, err)
	}
	return l.UID
}

// makeSubject creates a subject (person) and returns its UID.
func (e *env) makeSubject(t *testing.T, name string) string {
	t.Helper()
	s, err := e.people.CreateSubject(t.Context(), people.Subject{Name: name})
	if err != nil {
		t.Fatalf("create subject %s: %v", name, err)
	}
	return s.UID
}

// addToAlbum makes photoUID a member of albumUID.
func (e *env) addToAlbum(t *testing.T, albumUID, photoUID string) {
	t.Helper()
	if err := e.organize.AddPhoto(t.Context(), albumUID, photoUID); err != nil {
		t.Fatalf("add photo to album: %v", err)
	}
}

// attachLabel attaches labelUID to photoUID.
func (e *env) attachLabel(t *testing.T, photoUID, labelUID string) {
	t.Helper()
	if err := e.organize.AttachLabel(t.Context(), photoUID, labelUID, organize.SourceManual, 0); err != nil {
		t.Fatalf("attach label: %v", err)
	}
}

// tagPerson creates a face marker linking subjectUID to photoUID.
func (e *env) tagPerson(t *testing.T, photoUID, subjectUID string) {
	t.Helper()
	_, err := e.people.CreateMarker(t.Context(), people.Marker{
		PhotoUID: photoUID, SubjectUID: &subjectUID, Type: people.MarkerFace,
		X: 0.1, Y: 0.1, W: 0.2, H: 0.2,
	})
	if err != nil {
		t.Fatalf("tag person: %v", err)
	}
}

// merge POSTs a merge request and returns the decoded result and the status code.
func (e *env) merge(t *testing.T, c *http.Client, keeper string, members []string, dryRun bool) (dupmerge.Result, int) {
	t.Helper()
	body, _ := json.Marshal(map[string]any{
		"keeper_uid": keeper, "member_uids": members, "dry_run": dryRun,
	})
	resp := e.do(t, c, http.MethodPost, "/api/v1/duplicates/merge", body)
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return dupmerge.Result{}, resp.StatusCode
	}
	var result dupmerge.Result
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("decode merge result: %v", err)
	}
	return result, resp.StatusCode
}

// do issues a request with an optional JSON body and returns the response.
func (e *env) do(t *testing.T, c *http.Client, method, path string, body []byte) *http.Response {
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

// TestMerge_unionsAlbumsLabelsPeopleAndArchives exercises the headline case: the
// keeper inherits the union of albums, labels and people, fills its empty
// title/description, the copies are archived and the merge is audited.
func TestMerge_unionsAlbumsLabelsPeopleAndArchives(t *testing.T) {
	env := newEnv(t)
	editor, editorUID := env.login(t, "editor", auth.RoleEditor)
	ctx := t.Context()

	keeper := env.seedPhoto(t, "keep", "", "", 4000, 3000)
	copy1 := env.seedPhoto(t, "dup1", "Sunset", "", 2000, 1500)
	copy2 := env.seedPhoto(t, "dup2", "", "On the beach", 1000, 750)

	albumA, albumB := env.makeAlbum(t, "A"), env.makeAlbum(t, "B")
	labelX, labelY := env.makeLabel(t, "x"), env.makeLabel(t, "y")
	subj1, subj2 := env.makeSubject(t, "Alice"), env.makeSubject(t, "Bob")

	env.addToAlbum(t, albumA, keeper) // keeper already in A
	env.addToAlbum(t, albumB, copy1)  // only a copy is in B
	env.attachLabel(t, keeper, labelX)
	env.attachLabel(t, copy1, labelY)
	env.tagPerson(t, keeper, subj1)
	env.tagPerson(t, copy2, subj2)

	result, status := env.merge(t, editor, keeper, []string{keeper, copy1, copy2}, false)
	if status != http.StatusOK {
		t.Fatalf("merge status = %d, want 200", status)
	}
	if result.AlbumsAdded != 1 || result.LabelsAdded != 1 || result.PeopleAdded != 1 || result.Archived != 2 {
		t.Fatalf("result counts = %+v, want albums=1 labels=1 people=1 archived=2", result)
	}

	assertAlbumHas(t, ctx, env, albumB, keeper)
	assertLabelHas(t, ctx, env, labelY, keeper)
	assertSubjectHas(t, ctx, env, subj2, keeper)

	got, err := env.photos.GetByUID(ctx, keeper)
	if err != nil {
		t.Fatalf("get keeper: %v", err)
	}
	if got.Title != "Sunset" || got.Description != "On the beach" {
		t.Errorf("keeper scalars = (%q,%q), want (Sunset, On the beach)", got.Title, got.Description)
	}
	if got.ArchivedAt != nil {
		t.Errorf("keeper was archived")
	}
	assertArchived(t, ctx, env, copy1)
	assertArchived(t, ctx, env, copy2)
	assertMergeAudited(t, ctx, env, editorUID)
}

// TestMerge_fillsOnlyMissingScalars verifies an existing keeper value is never
// overwritten while the acting user's favorite/rating/flag carry over when the
// keeper lacks them.
func TestMerge_fillsOnlyMissingScalars(t *testing.T) {
	env := newEnv(t)
	editor, editorUID := env.login(t, "editor", auth.RoleEditor)
	ctx := t.Context()

	keeper := env.seedPhoto(t, "keep", "Original", "", 4000, 3000)
	copy1 := env.seedPhoto(t, "dup1", "Different", "", 2000, 1500)

	// The copy carries the acting user's favorite, rating and flag; the keeper has
	// none of them.
	if err := env.organize.AddFavorite(ctx, editorUID, copy1); err != nil {
		t.Fatalf("add favorite: %v", err)
	}
	if err := env.organize.SetRating(ctx, editorUID, copy1, 4); err != nil {
		t.Fatalf("set rating: %v", err)
	}
	if err := env.organize.SetFlag(ctx, editorUID, copy1, "pick"); err != nil {
		t.Fatalf("set flag: %v", err)
	}

	result, status := env.merge(t, editor, keeper, []string{keeper, copy1}, false)
	if status != http.StatusOK {
		t.Fatalf("merge status = %d, want 200", status)
	}
	if slices.Contains(result.MetadataFilled, "title") {
		t.Errorf("title was filled despite the keeper already having one: %v", result.MetadataFilled)
	}
	for _, want := range []string{"rating", "favorite", "flag"} {
		if !slices.Contains(result.MetadataFilled, want) {
			t.Errorf("metadata_filled = %v, missing %q", result.MetadataFilled, want)
		}
	}

	got, err := env.photos.GetByUID(ctx, keeper)
	if err != nil {
		t.Fatalf("get keeper: %v", err)
	}
	if got.Title != "Original" {
		t.Errorf("keeper title = %q, want it untouched (Original)", got.Title)
	}
	fav, err := env.organize.IsFavorite(ctx, editorUID, keeper)
	if err != nil {
		t.Fatalf("IsFavorite: %v", err)
	}
	if !fav {
		t.Errorf("keeper favorite not carried over")
	}
	ratings, err := env.organize.RatingsAmong(ctx, editorUID, []string{keeper})
	if err != nil {
		t.Fatalf("RatingsAmong: %v", err)
	}
	if r := ratings[keeper]; r.Rating != 4 || r.Flag != "pick" {
		t.Errorf("keeper rating/flag = %d/%q, want 4/pick", r.Rating, r.Flag)
	}
}

// TestMerge_dryRunPreviewsWithoutChanging verifies a dry run reports the counts
// but changes nothing in the database.
func TestMerge_dryRunPreviewsWithoutChanging(t *testing.T) {
	env := newEnv(t)
	editor, _ := env.login(t, "editor", auth.RoleEditor)
	ctx := t.Context()

	keeper := env.seedPhoto(t, "keep", "", "", 4000, 3000)
	copy1 := env.seedPhoto(t, "dup1", "", "", 2000, 1500)
	albumB := env.makeAlbum(t, "B")
	env.addToAlbum(t, albumB, copy1)

	result, status := env.merge(t, editor, keeper, []string{keeper, copy1}, true)
	if status != http.StatusOK {
		t.Fatalf("dry-run status = %d, want 200", status)
	}
	if !result.DryRun || result.AlbumsAdded != 1 || result.Archived != 1 {
		t.Fatalf("preview = %+v, want dry_run albums=1 archived=1", result)
	}

	assertAlbumMissing(t, ctx, env, albumB, keeper)
	got, err := env.photos.GetByUID(ctx, copy1)
	if err != nil {
		t.Fatalf("get copy: %v", err)
	}
	if got.ArchivedAt != nil {
		t.Errorf("dry run archived a copy")
	}
}

// TestMerge_idempotentReRun verifies re-running a resolved merge is a safe no-op.
func TestMerge_idempotentReRun(t *testing.T) {
	env := newEnv(t)
	editor, _ := env.login(t, "editor", auth.RoleEditor)

	keeper := env.seedPhoto(t, "keep", "", "", 4000, 3000)
	copy1 := env.seedPhoto(t, "dup1", "", "", 2000, 1500)
	albumB := env.makeAlbum(t, "B")
	env.addToAlbum(t, albumB, copy1)

	first, _ := env.merge(t, editor, keeper, []string{keeper, copy1}, false)
	if first.AlbumsAdded != 1 || first.Archived != 1 {
		t.Fatalf("first merge = %+v, want albums=1 archived=1", first)
	}
	second, status := env.merge(t, editor, keeper, []string{keeper, copy1}, false)
	if status != http.StatusOK {
		t.Fatalf("second merge status = %d, want 200", status)
	}
	if second.AlbumsAdded != 0 || second.Archived != 0 || len(second.MetadataFilled) != 0 {
		t.Errorf("re-run was not a no-op: %+v", second)
	}
}

// TestMerge_viewerForbidden verifies a viewer cannot resolve a group.
func TestMerge_viewerForbidden(t *testing.T) {
	env := newEnv(t)
	viewer, _ := env.login(t, "viewer", auth.RoleViewer)
	keeper := env.seedPhoto(t, "keep", "", "", 4000, 3000)
	copy1 := env.seedPhoto(t, "dup1", "", "", 2000, 1500)

	_, status := env.merge(t, viewer, keeper, []string{keeper, copy1}, false)
	if status != http.StatusForbidden {
		t.Fatalf("viewer merge status = %d, want 403", status)
	}
}

// assertAlbumHas fails unless the album contains the photo.
func assertAlbumHas(t *testing.T, ctx context.Context, env *env, albumUID, photoUID string) {
	t.Helper()
	uids, err := env.organize.ListPhotoUIDs(ctx, albumUID)
	if err != nil {
		t.Fatalf("list album photos: %v", err)
	}
	if !slices.Contains(uids, photoUID) {
		t.Errorf("album %s = %v, missing keeper %s", albumUID, uids, photoUID)
	}
}

// assertAlbumMissing fails when the album contains the photo.
func assertAlbumMissing(t *testing.T, ctx context.Context, env *env, albumUID, photoUID string) {
	t.Helper()
	uids, err := env.organize.ListPhotoUIDs(ctx, albumUID)
	if err != nil {
		t.Fatalf("list album photos: %v", err)
	}
	if slices.Contains(uids, photoUID) {
		t.Errorf("album %s unexpectedly contains %s", albumUID, photoUID)
	}
}

// assertLabelHas fails unless the label is attached to the photo.
func assertLabelHas(t *testing.T, ctx context.Context, env *env, labelUID, photoUID string) {
	t.Helper()
	uids, err := env.organize.ListPhotoUIDsByLabel(ctx, labelUID)
	if err != nil {
		t.Fatalf("list label photos: %v", err)
	}
	if !slices.Contains(uids, photoUID) {
		t.Errorf("label %s = %v, missing keeper %s", labelUID, uids, photoUID)
	}
}

// assertSubjectHas fails unless the subject's gallery includes the photo.
func assertSubjectHas(t *testing.T, ctx context.Context, env *env, subjectUID, photoUID string) {
	t.Helper()
	uids, err := env.people.ListPhotoUIDsBySubject(ctx, subjectUID)
	if err != nil {
		t.Fatalf("list subject photos: %v", err)
	}
	if !slices.Contains(uids, photoUID) {
		t.Errorf("subject %s gallery = %v, missing keeper %s", subjectUID, uids, photoUID)
	}
}

// assertArchived fails unless the photo is archived.
func assertArchived(t *testing.T, ctx context.Context, env *env, photoUID string) {
	t.Helper()
	p, err := env.photos.GetByUID(ctx, photoUID)
	if err != nil {
		t.Fatalf("get photo %s: %v", photoUID, err)
	}
	if p.ArchivedAt == nil {
		t.Errorf("photo %s not archived", photoUID)
	}
}

// assertMergeAudited fails unless a photos.merge entry by actorUID exists.
func assertMergeAudited(t *testing.T, ctx context.Context, env *env, actorUID string) {
	t.Helper()
	records, err := env.audit.List(ctx, audit.Filter{Limit: 20})
	if err != nil {
		t.Fatalf("list audit: %v", err)
	}
	for _, rec := range records {
		if rec.Action == audit.ActionPhotosMerge && rec.ActorUID != nil && *rec.ActorUID == actorUID {
			return
		}
	}
	t.Fatalf("no %s audit entry for actor %s", audit.ActionPhotosMerge, actorUID)
}
