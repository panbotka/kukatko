package userpicapi_test

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/panbotka/kukatko/internal/avatar"
	"github.com/panbotka/kukatko/internal/photos"
	"github.com/panbotka/kukatko/internal/userpic"
	"github.com/panbotka/kukatko/internal/userpicapi"
)

// fakePictures answers the chain with a fixed outcome, so these tests are about
// what the HTTP layer does with each of them.
type fakePictures struct {
	source userpic.Source
	origin userpic.Origin
	err    error
}

func (f fakePictures) Resolve(context.Context, string) (userpic.Source, userpic.Origin, error) {
	return f.source, f.origin, f.err
}

func (f fakePictures) Describe(context.Context, string) (userpic.State, error) {
	return userpic.State{Origin: f.origin}, nil
}

func (f fakePictures) SetUpload(context.Context, string, []byte) error { return nil }

func (f fakePictures) SetPhoto(context.Context, string, string) error { return nil }

func (f fakePictures) Clear(context.Context, string) error { return nil }

// fakePhotos hands back one catalogued photo whatever uid is asked for.
type fakePhotos struct{ photo photos.Photo }

func (f fakePhotos) GetByUID(_ context.Context, uid string) (photos.Photo, error) {
	if f.photo.UID == "" {
		return photos.Photo{}, photos.ErrPhotoNotFound
	}
	f.photo.UID = uid
	return f.photo, nil
}

// fakeRenderer stands in for the avatar renderer, recording the face box it was
// handed so a test can prove an inherited face is cut as a face and a picked
// photo whole.
type fakeRenderer struct {
	data []byte
	etag string
	face *avatar.Box
}

func (f *fakeRenderer) Open(
	_ context.Context, _ photos.Photo, face *avatar.Box,
) (io.ReadCloser, string, error) {
	f.face = face
	return io.NopCloser(bytes.NewReader(f.data)), f.etag, nil
}

// passThroughGuard is a no-op read guard: authorization is auth's own test's job,
// and the write routes are covered end-to-end by the integration test.
func passThroughGuard(next http.Handler) http.Handler { return next }

// route builds a router over the API and returns it with the renderer, so a test
// can inspect what the renderer was asked for.
func route(t *testing.T, pictures userpicapi.Pictures, photo photos.Photo) (chi.Router, *fakeRenderer) {
	t.Helper()
	renderer := &fakeRenderer{data: []byte("rendered-jpeg"), etag: `"rendered"`}
	api := userpicapi.NewAPI(userpicapi.Config{
		Pictures:    pictures,
		Photos:      fakePhotos{photo: photo},
		Renderer:    renderer,
		RequireAuth: passThroughGuard,
	})
	router := chi.NewRouter()
	api.RegisterRoutes(router)
	return router, renderer
}

// get issues GET /users/u1/avatar, optionally with an If-None-Match.
func get(t *testing.T, router chi.Router, etag string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/users/u1/avatar", nil)
	if etag != "" {
		req.Header.Set("If-None-Match", etag)
	}
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec
}

func TestAvatar_anAccountWithNoPictureIs404(t *testing.T) {
	t.Parallel()

	router, _ := route(t, fakePictures{origin: userpic.OriginNone, err: userpic.ErrNoPicture}, photos.Photo{})
	if rec := get(t, router, ""); rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404 (%s)", rec.Code, rec.Body.String())
	}
}

// TestAvatar_aPictureThatIsNotTheCallersStaysCacheable pins the policy for the
// common case: an avatar in a comment thread belongs to somebody else, nothing
// the reader does changes it, so it is worth ten minutes of not asking. The
// caller's own picture — which does change under them — is the integration
// test's business, since it needs a real principal on the request.
func TestAvatar_aPictureThatIsNotTheCallersStaysCacheable(t *testing.T) {
	t.Parallel()

	router, _ := route(t,
		fakePictures{source: userpic.Source{Upload: []byte("stored")}, origin: userpic.OriginUpload},
		photos.Photo{})

	rec := get(t, router, "")
	if got, want := rec.Header().Get("Cache-Control"), "private, max-age=600, must-revalidate"; got != want {
		t.Errorf("Cache-Control = %q, want %q", got, want)
	}
}

func TestAvatar_anUploadIsServedFromItsStoredBytes(t *testing.T) {
	t.Parallel()

	stored := []byte("stored-jpeg-bytes")
	router, renderer := route(t,
		fakePictures{source: userpic.Source{Upload: stored}, origin: userpic.OriginUpload}, photos.Photo{})

	rec := get(t, router, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (%s)", rec.Code, rec.Body.String())
	}
	if got := rec.Body.String(); got != string(stored) {
		t.Errorf("body = %q, want the stored bytes", got)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "image/jpeg" {
		t.Errorf("Content-Type = %q, want image/jpeg", ct)
	}
	if rec.Header().Get("ETag") == "" {
		t.Error("an uploaded picture was served without an ETag")
	}
	if renderer.face != nil {
		t.Error("an uploaded picture went through the photo renderer")
	}

	// The same request carrying that ETag must be answered 304 with no body.
	second := get(t, router, rec.Header().Get("ETag"))
	if second.Code != http.StatusNotModified {
		t.Errorf("conditional status = %d, want 304", second.Code)
	}
	if second.Body.Len() != 0 {
		t.Errorf("a 304 carried %d bytes of body", second.Body.Len())
	}
}

func TestAvatar_aPickedPhotoIsCutWhole(t *testing.T) {
	t.Parallel()

	router, renderer := route(t,
		fakePictures{source: userpic.Source{PhotoUID: "p1"}, origin: userpic.OriginPhoto},
		photos.Photo{UID: "p1", FileHash: "abc"})

	rec := get(t, router, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (%s)", rec.Code, rec.Body.String())
	}
	if rec.Body.String() != "rendered-jpeg" {
		t.Errorf("body = %q, want the rendition", rec.Body.String())
	}
	if renderer.face != nil {
		t.Errorf("a picked photo was cut to face box %+v, want the whole frame", renderer.face)
	}
	if rec.Header().Get("ETag") != `"rendered"` {
		t.Errorf("ETag = %q, want the renderer's", rec.Header().Get("ETag"))
	}
}

func TestAvatar_anInheritedFaceIsCutAsAFace(t *testing.T) {
	t.Parallel()

	box := userpic.Box{X: 0.5, Y: 0.25, W: 0.1, H: 0.12}
	router, renderer := route(t,
		fakePictures{source: userpic.Source{PhotoUID: "p1", Face: &box}, origin: userpic.OriginSubject},
		photos.Photo{UID: "p1", FileHash: "abc"})

	if rec := get(t, router, ""); rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (%s)", rec.Code, rec.Body.String())
	}
	if renderer.face == nil || renderer.face.X != box.X || renderer.face.H != box.H {
		t.Errorf("renderer got face %+v, want %+v", renderer.face, box)
	}
}

func TestAvatar_aPhotoThatVanishedBetweenResolveAndReadIs404(t *testing.T) {
	t.Parallel()

	router, _ := route(t,
		fakePictures{source: userpic.Source{PhotoUID: "p1"}, origin: userpic.OriginPhoto}, photos.Photo{})
	if rec := get(t, router, ""); rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404 (%s)", rec.Code, rec.Body.String())
	}
}

func TestAvatar_withoutARendererAPhotoIs503ButAnUploadStillWorks(t *testing.T) {
	t.Parallel()

	build := func(source userpic.Source) chi.Router {
		api := userpicapi.NewAPI(userpicapi.Config{
			Pictures:    fakePictures{source: source, origin: userpic.OriginPhoto},
			Photos:      fakePhotos{photo: photos.Photo{UID: "p1", FileHash: "abc"}},
			RequireAuth: passThroughGuard,
		})
		router := chi.NewRouter()
		api.RegisterRoutes(router)
		return router
	}

	if rec := get(t, build(userpic.Source{PhotoUID: "p1"}), ""); rec.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503 (%s)", rec.Code, rec.Body.String())
	}
	if rec := get(t, build(userpic.Source{Upload: []byte("bytes")}), ""); rec.Code != http.StatusOK {
		t.Errorf("an upload needs no renderer: status = %d, want 200", rec.Code)
	}
}
