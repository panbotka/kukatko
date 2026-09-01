package avatarapi_test

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/panbotka/kukatko/internal/avatar"
	"github.com/panbotka/kukatko/internal/avatarapi"
	"github.com/panbotka/kukatko/internal/people"
	"github.com/panbotka/kukatko/internal/photos"
)

// fakeSubjects answers the avatar-source lookup with a canned result or error.
type fakeSubjects struct {
	source people.AvatarSource
	err    error
}

// SubjectAvatar returns the canned source and error regardless of subject.
func (f *fakeSubjects) SubjectAvatar(_ context.Context, _ string) (people.AvatarSource, error) {
	return f.source, f.err
}

// fakePhotos answers the photo lookup with a canned record or error.
type fakePhotos struct {
	photo photos.Photo
	err   error
}

// GetByUID returns the canned photo and error regardless of uid.
func (f *fakePhotos) GetByUID(_ context.Context, _ string) (photos.Photo, error) {
	return f.photo, f.err
}

// fakeRenderer returns canned bytes and records the crop it was asked for.
type fakeRenderer struct {
	data []byte
	etag string
	err  error
	face *avatar.Box
	// calls counts how often the renderer was asked, so a 304 can be shown to
	// still resolve the picture (and no more).
	calls int
}

// Open records the requested crop and returns the canned rendition.
func (f *fakeRenderer) Open(
	_ context.Context, _ photos.Photo, face *avatar.Box,
) (io.ReadCloser, string, error) {
	f.calls++
	f.face = face
	if f.err != nil {
		return nil, "", f.err
	}
	return io.NopCloser(bytes.NewReader(f.data)), f.etag, nil
}

// passThrough is a no-op read guard so handler behaviour is tested without auth.
func passThrough(next http.Handler) http.Handler {
	return next
}

// newServer mounts an avatar API backed by the given fakes behind the
// pass-through guard.
func newServer(subjects avatarapi.Subjects, photoStore avatarapi.Photos, renderer avatarapi.Renderer) http.Handler {
	api := avatarapi.NewAPI(avatarapi.Config{
		Subjects:    subjects,
		Photos:      photoStore,
		Renderer:    renderer,
		RequireAuth: passThrough,
	})
	r := chi.NewRouter()
	api.RegisterRoutes(r)
	return r
}

// avatarRequest builds a GET for one subject's avatar, with an optional
// If-None-Match header.
func avatarRequest(uid, ifNoneMatch string) *http.Request {
	req := httptest.NewRequestWithContext(
		context.Background(), http.MethodGet, "/subjects/"+uid+"/avatar", nil)
	if ifNoneMatch != "" {
		req.Header.Set("If-None-Match", ifNoneMatch)
	}
	return req
}

func TestHandleAvatar_streamsTheFaceCrop(t *testing.T) {
	t.Parallel()

	box := people.Box{X: 0.4, Y: 0.3, W: 0.1, H: 0.15}
	subjects := &fakeSubjects{source: people.AvatarSource{PhotoUID: "ph_1", Face: &box}}
	renderer := &fakeRenderer{data: []byte("jpeg-bytes"), etag: `"tag"`}
	server := newServer(subjects, &fakePhotos{photo: photos.Photo{UID: "ph_1"}}, renderer)

	rec := httptest.NewRecorder()
	server.ServeHTTP(rec, avatarRequest("su_1", ""))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (%s)", rec.Code, rec.Body.String())
	}
	if got := rec.Body.String(); got != "jpeg-bytes" {
		t.Errorf("body = %q, want the rendered bytes", got)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "image/jpeg" {
		t.Errorf("Content-Type = %q, want image/jpeg", ct)
	}
	if tag := rec.Header().Get("ETag"); tag != `"tag"` {
		t.Errorf("ETag = %q, want the renderer's", tag)
	}
	if cc := rec.Header().Get("Cache-Control"); cc == "" {
		t.Error("no Cache-Control header: an avatar must be cacheable")
	}
	if renderer.face == nil || renderer.face.X != box.X || renderer.face.W != box.W {
		t.Errorf("renderer was asked for %+v, want the subject's face box", renderer.face)
	}
}

func TestHandleAvatar_coverPhotoAsksForNoCrop(t *testing.T) {
	t.Parallel()

	subjects := &fakeSubjects{source: people.AvatarSource{PhotoUID: "ph_cover"}}
	renderer := &fakeRenderer{data: []byte("jpeg"), etag: `"tag"`}
	server := newServer(subjects, &fakePhotos{photo: photos.Photo{UID: "ph_cover"}}, renderer)

	rec := httptest.NewRecorder()
	server.ServeHTTP(rec, avatarRequest("su_1", ""))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (%s)", rec.Code, rec.Body.String())
	}
	if renderer.face != nil {
		t.Errorf("renderer was asked to crop %+v, want the whole cover photo", renderer.face)
	}
}

func TestHandleAvatar_notModified(t *testing.T) {
	t.Parallel()

	subjects := &fakeSubjects{source: people.AvatarSource{PhotoUID: "ph_1"}}
	renderer := &fakeRenderer{data: []byte("jpeg"), etag: `"tag"`}
	server := newServer(subjects, &fakePhotos{photo: photos.Photo{UID: "ph_1"}}, renderer)

	rec := httptest.NewRecorder()
	server.ServeHTTP(rec, avatarRequest("su_1", `"tag"`))

	if rec.Code != http.StatusNotModified {
		t.Fatalf("status = %d, want 304", rec.Code)
	}
	if rec.Body.Len() != 0 {
		t.Errorf("body = %q, want nothing on a 304", rec.Body.String())
	}
}

func TestHandleAvatar_missingPictureIs404(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		subjects avatarapi.Subjects
		photos   avatarapi.Photos
	}{
		{
			name:     "unknown subject",
			subjects: &fakeSubjects{err: people.ErrSubjectNotFound},
			photos:   &fakePhotos{},
		},
		{
			name:     "subject with no cover and no usable face",
			subjects: &fakeSubjects{err: people.ErrNoAvatar},
			photos:   &fakePhotos{},
		},
		{
			name:     "picture names a photo that is gone",
			subjects: &fakeSubjects{source: people.AvatarSource{PhotoUID: "ph_gone"}},
			photos:   &fakePhotos{err: photos.ErrPhotoNotFound},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			server := newServer(tt.subjects, tt.photos, &fakeRenderer{})
			rec := httptest.NewRecorder()
			server.ServeHTTP(rec, avatarRequest("su_1", ""))
			if rec.Code != http.StatusNotFound {
				t.Errorf("status = %d, want 404 (%s)", rec.Code, rec.Body.String())
			}
		})
	}
}

func TestHandleAvatar_renderFailureIs500(t *testing.T) {
	t.Parallel()

	subjects := &fakeSubjects{source: people.AvatarSource{PhotoUID: "ph_1"}}
	renderer := &fakeRenderer{err: avatar.ErrRenderFailed}
	server := newServer(subjects, &fakePhotos{photo: photos.Photo{UID: "ph_1"}}, renderer)

	rec := httptest.NewRecorder()
	server.ServeHTTP(rec, avatarRequest("su_1", ""))

	if rec.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500 (%s)", rec.Code, rec.Body.String())
	}
}

func TestHandleAvatar_withoutARendererIs503(t *testing.T) {
	t.Parallel()

	server := newServer(&fakeSubjects{}, &fakePhotos{}, nil)
	rec := httptest.NewRecorder()
	server.ServeHTTP(rec, avatarRequest("su_1", ""))

	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503 (%s)", rec.Code, rec.Body.String())
	}
}
