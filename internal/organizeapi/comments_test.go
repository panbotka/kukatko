package organizeapi_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/panbotka/kukatko/internal/audit"
	"github.com/panbotka/kukatko/internal/comments"
	"github.com/panbotka/kukatko/internal/organize"
	"github.com/panbotka/kukatko/internal/organizeapi"
)

// fakeComments is an in-memory CommentStore recording what the handlers asked
// of it. Who is signed in is the auth package's secret (its context key is
// unexported), so these unit tests cover the paths before a principal is
// needed; the author-dependent ones run over a real login in the integration
// suite.
type fakeComments struct {
	thread      []comments.Comment
	counts      map[string]int
	countsErr   error
	lastSubject comments.Subject
	countedKind comments.SubjectKind
	countedUIDs []string
	created     bool
}

// List returns the canned thread for any subject.
func (f *fakeComments) List(_ context.Context, subj comments.Subject) ([]comments.Comment, error) {
	f.lastSubject = subj
	return f.thread, nil
}

// CountsAmong records the question and answers the canned counts.
func (f *fakeComments) CountsAmong(
	_ context.Context, kind comments.SubjectKind, uids []string,
) (map[string]int, error) {
	f.countedKind, f.countedUIDs = kind, uids
	return f.counts, f.countsErr
}

// Get is never reached without a principal.
func (f *fakeComments) Get(context.Context, string) (comments.Comment, error) {
	return comments.Comment{}, comments.ErrNotFound
}

// Create records that a write got through — it must not, without a principal.
func (f *fakeComments) Create(
	context.Context, comments.Subject, string, string, audit.Entry,
) (comments.Comment, error) {
	f.created = true
	return comments.Comment{}, errors.New("unexpected create")
}

// Update is never reached without a principal.
func (f *fakeComments) Update(context.Context, string, string, audit.Entry) (comments.Comment, error) {
	return comments.Comment{}, errors.New("unexpected update")
}

// Delete is never reached without a principal.
func (f *fakeComments) Delete(context.Context, string, audit.Entry) error {
	return errors.New("unexpected delete")
}

// commentServer mounts the API with the given album and comment stores behind
// pass-through guards and an optional comment throttle.
func commentServer(
	albums organizeapi.AlbumStore, store organizeapi.CommentStore, throttle func(http.Handler) http.Handler,
) http.Handler {
	api := organizeapi.NewAPI(organizeapi.Config{
		Albums:          albums,
		Labels:          &fakeLabels{},
		Comments:        store,
		CommentThrottle: throttle,
		RequireAuth:     passThrough,
		RequireCurator:  passThrough,
	})
	r := chi.NewRouter()
	api.RegisterRoutes(r)
	return r
}

// TestAlbumComments_listChecksTheAlbumFirst pins the visibility rule: the thread
// is read through the album's own existence check, so a missing album is a 404
// exactly as GET /albums/{uid} answers, and a present one lists the thread as an
// album subject.
func TestAlbumComments_listChecksTheAlbumFirst(t *testing.T) {
	t.Parallel()

	store := &fakeComments{thread: []comments.Comment{{UID: "cm1", AlbumUID: "al1", Body: "hi",
		Mentions: []comments.Mention{}}}}
	missing := commentServer(&fakeAlbums{getErr: organize.ErrAlbumNotFound}, store, nil)
	if rec := do(t, missing, http.MethodGet, "/albums/al_gone/comments", ""); rec.Code != http.StatusNotFound {
		t.Fatalf("missing album: status = %d, want 404", rec.Code)
	}
	if store.lastSubject != (comments.Subject{}) {
		t.Fatalf("the thread was read for a missing album: %+v", store.lastSubject)
	}

	present := commentServer(&fakeAlbums{album: organize.Album{UID: "al1"}}, store, nil)
	rec := do(t, present, http.MethodGet, "/albums/al1/comments", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	if store.lastSubject != comments.AlbumSubject("al1") {
		t.Errorf("subject = %+v, want the album", store.lastSubject)
	}
	var body struct {
		Comments []comments.Comment `json:"comments"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil || len(body.Comments) != 1 ||
		body.Comments[0].AlbumUID != "al1" {
		t.Errorf("body = %s (%v), want the one album comment", rec.Body.String(), err)
	}
}

// TestAlbumComments_writesNeedAPrincipal pins that no write reaches the store
// without a signed-in user: a guard that let nobody through is answered 401 and
// nothing is created.
func TestAlbumComments_writesNeedAPrincipal(t *testing.T) {
	t.Parallel()

	store := &fakeComments{}
	h := commentServer(&fakeAlbums{album: organize.Album{UID: "al1"}}, store, nil)
	for _, tt := range []struct{ method, path string }{
		{http.MethodPost, "/albums/al1/comments"},
		{http.MethodPatch, "/albums/al1/comments/cm1"},
		{http.MethodDelete, "/albums/al1/comments/cm1"},
	} {
		if rec := do(t, h, tt.method, tt.path, `{"body":"x"}`); rec.Code != http.StatusUnauthorized {
			t.Errorf("%s %s: status = %d, want 401", tt.method, tt.path, rec.Code)
		}
	}
	if store.created {
		t.Error("a comment was created without a principal")
	}
}

// TestAlbumComments_unavailableWithoutStore pins the 503 of an instance with no
// comments backend, and that the album detail still answers with a zero count.
func TestAlbumComments_unavailableWithoutStore(t *testing.T) {
	t.Parallel()

	h := commentServer(&fakeAlbums{album: organize.Album{UID: "al1"}}, nil, nil)
	for _, tt := range []struct{ method, path string }{
		{http.MethodGet, "/albums/al1/comments"},
		{http.MethodPost, "/albums/al1/comments"},
		{http.MethodPatch, "/albums/al1/comments/cm1"},
		{http.MethodDelete, "/albums/al1/comments/cm1"},
	} {
		if rec := do(t, h, tt.method, tt.path, `{"body":"x"}`); rec.Code != http.StatusServiceUnavailable {
			t.Errorf("%s %s: status = %d, want 503", tt.method, tt.path, rec.Code)
		}
	}
	rec := do(t, h, http.MethodGet, "/albums/al1", "")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"comment_count":0`) {
		t.Errorf("album detail without a store = %d %s, want 200 with comment_count 0", rec.Code, rec.Body.String())
	}
}

// TestAlbumGet_carriesTheCommentCount pins the badge's source: the detail asks
// the bulk count for its one album and carries the answer as comment_count; a
// failing count is a 500 whose cause reaches the log.
func TestAlbumGet_carriesTheCommentCount(t *testing.T) {
	store := &fakeComments{counts: map[string]int{"al1": 3}}
	h := commentServer(&fakeAlbums{album: organize.Album{UID: "al1", Title: "Svatba"}}, store, nil)
	rec := do(t, h, http.MethodGet, "/albums/al1", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
	}
	var body struct {
		UID          string `json:"uid"`
		Title        string `json:"title"`
		CommentCount int    `json:"comment_count"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.UID != "al1" || body.Title != "Svatba" || body.CommentCount != 3 {
		t.Errorf("body = %+v, want the album with comment_count 3", body)
	}
	if store.countedKind != comments.SubjectAlbum || len(store.countedUIDs) != 1 || store.countedUIDs[0] != "al1" {
		t.Errorf("counted %q %v, want the album kind for al1", store.countedKind, store.countedUIDs)
	}

	logs := captureLogs(t)
	broken := commentServer(&fakeAlbums{album: organize.Album{UID: "al1"}},
		&fakeComments{countsErr: errors.New("pool exhausted")}, nil)
	if rec := do(t, broken, http.MethodGet, "/albums/al1", ""); rec.Code != http.StatusInternalServerError {
		t.Errorf("broken count: status = %d, want 500", rec.Code)
	}
	if !strings.Contains(logs.String(), "pool exhausted") {
		t.Errorf("the log does not carry the cause: %q", logs.String())
	}
}

// TestAlbumComments_throttleIsMounted pins that a configured throttle sits on
// the create route and only there: a limiter refusing everything turns the post
// into its answer while the read still passes.
func TestAlbumComments_throttleIsMounted(t *testing.T) {
	t.Parallel()

	refuse := func(http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusTooManyRequests)
		})
	}
	h := commentServer(&fakeAlbums{album: organize.Album{UID: "al1"}}, &fakeComments{}, refuse)
	if rec := do(t, h, http.MethodPost, "/albums/al1/comments", `{"body":"x"}`); rec.Code != http.StatusTooManyRequests {
		t.Errorf("post: status = %d, want 429 from the throttle", rec.Code)
	}
	if rec := do(t, h, http.MethodGet, "/albums/al1/comments", ""); rec.Code != http.StatusOK {
		t.Errorf("get: status = %d, want 200 (the read is not throttled)", rec.Code)
	}
}
