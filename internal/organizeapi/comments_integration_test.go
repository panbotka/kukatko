//go:build integration

package organizeapi_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/panbotka/kukatko/internal/audit"
	"github.com/panbotka/kukatko/internal/auth"
	"github.com/panbotka/kukatko/internal/comments"
	"github.com/panbotka/kukatko/internal/database/dbtest"
	"github.com/panbotka/kukatko/internal/organize"
	"github.com/panbotka/kukatko/internal/organizeapi"
	"github.com/panbotka/kukatko/internal/photos"
	"github.com/panbotka/kukatko/internal/ratelimit"
)

// newCommentEnv is newEnv with the comments store wired and a per-user comment
// throttle allowing burst posts per user: the same shape cmd/kukatko wires,
// with a limiter small enough to run into.
func newCommentEnv(t *testing.T, burst int) *env {
	t.Helper()
	db := dbtest.New(t)
	dbtest.TruncateAll(t, db)

	authStore := auth.NewStore(db.Pool())
	authSvc := auth.NewService(authStore, auth.SessionPolicy{TTL: time.Hour, MaxLifetime: 3 * time.Hour})
	authAPI := auth.NewAPI(auth.APIConfig{Service: authSvc, Limiter: auth.NewLimiter(100, time.Minute)})

	store := organize.NewStore(db.Pool())
	limit := ratelimit.New(0.0001, burst)
	api := organizeapi.NewAPI(organizeapi.Config{
		Albums:   store,
		Labels:   store,
		Comments: comments.NewStore(db.Pool()),
		CommentThrottle: limit.KeyedMiddleware(func(r *http.Request) string {
			user, _ := auth.UserFromContext(r.Context())
			return user.UID
		}),
		RequireAuth:    authAPI.RequireAuth,
		RequireCurator: authAPI.RequireCurator,
	})

	r := chi.NewRouter()
	r.Route("/api/v1", func(r chi.Router) {
		authAPI.RegisterRoutes(r)
		api.RegisterRoutes(r)
	})
	server := httptest.NewServer(r)
	t.Cleanup(server.Close)
	return &env{
		server: server, authSvc: authSvc, photos: photos.NewStore(db.Pool()), audit: audit.NewStore(db.Pool()), db: db,
	}
}

// newAlbum makes an album titled title over HTTP as c and returns its uid,
// through the featured tests' createAlbum.
func (e *env) newAlbum(t *testing.T, c *http.Client, title string) string {
	t.Helper()
	body, _ := json.Marshal(map[string]string{"title": title})
	return e.createAlbum(t, c, string(body)).UID
}

// postComment writes body on the album's thread as c and returns the status and
// the created comment (zero unless 201).
func (e *env) postComment(t *testing.T, c *http.Client, albumUID, body string) (int, comments.Comment) {
	t.Helper()
	payload, _ := json.Marshal(map[string]string{"body": body})
	resp := e.mustDo(t, c, http.MethodPost, "/api/v1/albums/"+albumUID+"/comments", payload)
	if resp.StatusCode != http.StatusCreated {
		_ = resp.Body.Close()
		return resp.StatusCode, comments.Comment{}
	}
	var created comments.Comment
	decodeBody(t, resp, &created)
	return resp.StatusCode, created
}

// listComments reads the album's thread as c, failing on anything but 200.
func (e *env) listComments(t *testing.T, c *http.Client, albumUID string) []comments.Comment {
	t.Helper()
	resp := e.mustDo(t, c, http.MethodGet, "/api/v1/albums/"+albumUID+"/comments", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("list status = %d, want 200", resp.StatusCode)
	}
	var body struct {
		Comments []comments.Comment `json:"comments"`
	}
	decodeBody(t, resp, &body)
	return body.Comments
}

// albumCommentCount reads comment_count off GET /albums/{uid}.
func (e *env) albumCommentCount(t *testing.T, c *http.Client, albumUID string) int {
	t.Helper()
	resp := e.mustDo(t, c, http.MethodGet, "/api/v1/albums/"+albumUID, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("album status = %d, want 200", resp.StatusCode)
	}
	var body struct {
		CommentCount int `json:"comment_count"`
	}
	decodeBody(t, resp, &body)
	return body.CommentCount
}

// TestAlbumCommentsAPI_everyRoleReadsAndWrites pins the visibility rule: the
// thread is open to whoever can open the album, which is every signed-in role —
// a viewer reads it, writes into it, and is recorded as the author; the album
// detail counts the thread; nobody signed out gets anything.
func TestAlbumCommentsAPI_everyRoleReadsAndWrites(t *testing.T) {
	env := newCommentEnv(t, 10)
	curator := env.login(t, "curator", auth.RoleCurator)
	viewer := env.login(t, "viewer", auth.RoleViewer)
	album := env.newAlbum(t, curator, "Svatba 1987")

	if got := env.albumCommentCount(t, viewer, album); got != 0 {
		t.Fatalf("comment_count before = %d, want 0", got)
	}
	status, created := env.postComment(t, viewer, album, "Kdo tam ještě byl?")
	if status != http.StatusCreated {
		t.Fatalf("viewer post status = %d, want 201", status)
	}
	if created.AlbumUID != album || created.AuthorName != "viewer" || created.PhotoUID != "" {
		t.Fatalf("created = %+v, want an album comment by the viewer", created)
	}
	if status, _ := env.postComment(t, curator, album, "Strejda Karel."); status != http.StatusCreated {
		t.Fatalf("curator post status = %d, want 201", status)
	}

	list := env.listComments(t, viewer, album)
	if len(list) != 2 || list[0].UID != created.UID || list[1].AuthorName != "curator" {
		t.Fatalf("thread = %+v, want the viewer's comment then the curator's", list)
	}
	if got := env.albumCommentCount(t, viewer, album); got != 2 {
		t.Fatalf("comment_count after = %d, want 2", got)
	}

	anon := &http.Client{}
	resp := env.mustDo(t, anon, http.MethodGet, "/api/v1/albums/"+album+"/comments", nil)
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("anonymous list status = %d, want 401", resp.StatusCode)
	}
	resp = env.mustDo(t, anon, http.MethodPost, "/api/v1/albums/"+album+"/comments", []byte(`{"body":"x"}`))
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("anonymous post status = %d, want 401", resp.StatusCode)
	}

	resp = env.mustDo(t, viewer, http.MethodGet, "/api/v1/albums/al_missing/comments", nil)
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("missing album list status = %d, want 404 like GET /albums/{uid}", resp.StatusCode)
	}
	if status, _ := env.postComment(t, viewer, "al_missing", "Haló?"); status != http.StatusNotFound {
		t.Errorf("missing album post status = %d, want 404", status)
	}
}

// TestAlbumCommentsAPI_ownershipAndAudit pins who may change what and that every
// mutation is audited against the album: the author edits and deletes their
// own, a viewer cannot touch another's, an admin deletes but never edits
// another's, a comment addressed through the wrong album is 404.
func TestAlbumCommentsAPI_ownershipAndAudit(t *testing.T) {
	env := newCommentEnv(t, 10)
	curator := env.login(t, "curator", auth.RoleCurator)
	viewer := env.login(t, "viewer", auth.RoleViewer)
	admin := env.login(t, "admin", auth.RoleAdmin)
	album := env.newAlbum(t, curator, "Svatba")
	other := env.newAlbum(t, curator, "Pouť")
	_, own := env.postComment(t, viewer, album, "moje")
	_, foreign := env.postComment(t, curator, album, "cizí")

	path := func(albumUID, commentUID string) string {
		return "/api/v1/albums/" + albumUID + "/comments/" + commentUID
	}
	expect := func(name string, c *http.Client, method, p string, body []byte, want int) {
		resp := env.mustDo(t, c, method, p, body)
		_ = resp.Body.Close()
		if resp.StatusCode != want {
			t.Errorf("%s: status = %d, want %d", name, resp.StatusCode, want)
		}
	}
	edit := []byte(`{"body":"opraveno"}`)
	expect("viewer edits foreign", viewer, http.MethodPatch, path(album, foreign.UID), edit, http.StatusForbidden)
	expect("viewer deletes foreign", viewer, http.MethodDelete, path(album, foreign.UID), nil, http.StatusForbidden)
	expect("admin edits foreign", admin, http.MethodPatch, path(album, foreign.UID), edit, http.StatusForbidden)
	expect("wrong album", viewer, http.MethodDelete, path(other, own.UID), nil, http.StatusNotFound)
	expect("blank edit", viewer, http.MethodPatch, path(album, own.UID), []byte(`{"body":"  "}`), http.StatusBadRequest)
	expect("author edits own", viewer, http.MethodPatch, path(album, own.UID), edit, http.StatusOK)
	expect("admin deletes foreign", admin, http.MethodDelete, path(album, foreign.UID), nil, http.StatusNoContent)
	expect("author deletes own", viewer, http.MethodDelete, path(album, own.UID), nil, http.StatusNoContent)
	expect("deleting twice", viewer, http.MethodDelete, path(album, own.UID), nil, http.StatusNotFound)

	if list := env.listComments(t, viewer, album); len(list) != 0 {
		t.Errorf("thread after both deletes = %+v, want empty", list)
	}
	for action, want := range map[string]int{
		audit.ActionCommentCreate: 2, audit.ActionCommentUpdate: 1, audit.ActionCommentDelete: 2,
	} {
		rows, err := env.audit.List(t.Context(), audit.Filter{Action: action, TargetType: "albums"})
		if err != nil {
			t.Fatalf("audit %s: %v", action, err)
		}
		if len(rows) != want {
			t.Errorf("audit %s rows targeting albums = %d, want %d", action, len(rows), want)
		}
		for _, row := range rows {
			if row.TargetUID == nil || *row.TargetUID != album || row.Details["comment_uid"] == nil {
				t.Errorf("audit %s row = %+v, want the album as target with comment_uid", action, row)
			}
		}
	}
}

// TestAlbumCommentsAPI_validationAndCascade pins the body rules shared with the
// other threads and the cascade: deleting the album over HTTP takes its thread
// with it while a photo's comment stays.
func TestAlbumCommentsAPI_validationAndCascade(t *testing.T) {
	env := newCommentEnv(t, 10)
	curator := env.login(t, "curator", auth.RoleCurator)
	album := env.newAlbum(t, curator, "Svatba")

	for name, body := range map[string]string{"blank": "   ", "long": string(make([]rune, 2001))} {
		if status, _ := env.postComment(t, curator, album, body); status != http.StatusBadRequest {
			t.Errorf("%s body: status = %d, want 400", name, status)
		}
	}
	resp := env.mustDo(t, curator, http.MethodPost, "/api/v1/albums/"+album+"/comments", []byte(`{"body":"x","album_uid":"y"}`))
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("unknown field: status = %d, want 400", resp.StatusCode)
	}

	_, kept := env.postComment(t, curator, album, "zůstane v logu")
	resp = env.mustDo(t, curator, http.MethodDelete, "/api/v1/albums/"+album, nil)
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("delete album status = %d, want 204", resp.StatusCode)
	}
	var n int
	if err := env.db.Pool().QueryRow(t.Context(),
		`SELECT count(*) FROM comments WHERE uid = $1`, kept.UID).Scan(&n); err != nil {
		t.Fatalf("counting: %v", err)
	}
	if n != 0 {
		t.Errorf("album comment rows after the album is gone = %d, want 0 (FK cascade)", n)
	}
}

// TestAlbumCommentsAPI_rateLimitPerUser pins the throttle: one user's burst runs
// out into 429 while another user's bucket is untouched and reads stay free.
func TestAlbumCommentsAPI_rateLimitPerUser(t *testing.T) {
	env := newCommentEnv(t, 2)
	curator := env.login(t, "curator", auth.RoleCurator)
	viewer := env.login(t, "viewer", auth.RoleViewer)
	album := env.newAlbum(t, curator, "Svatba")

	for i := range 2 {
		if status, _ := env.postComment(t, viewer, album, "x"); status != http.StatusCreated {
			t.Fatalf("post %d: status = %d, want 201", i, status)
		}
	}
	if status, _ := env.postComment(t, viewer, album, "x"); status != http.StatusTooManyRequests {
		t.Errorf("third post: status = %d, want 429", status)
	}
	if status, _ := env.postComment(t, curator, album, "x"); status != http.StatusCreated {
		t.Errorf("another user's post: status = %d, want 201 (own bucket)", status)
	}
	if list := env.listComments(t, viewer, album); len(list) != 3 {
		t.Errorf("thread = %d comments, want 3 — and the read itself is not throttled", len(list))
	}
}
