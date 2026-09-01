//go:build integration

package avatarapi_test

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/jpeg"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/panbotka/kukatko/internal/avatar"
	"github.com/panbotka/kukatko/internal/avatarapi"
	"github.com/panbotka/kukatko/internal/database/dbtest"
	"github.com/panbotka/kukatko/internal/people"
	"github.com/panbotka/kukatko/internal/photos"
)

// These tests run only under `make test-integration` against the database named
// by KUKATKO_TEST_DATABASE_URL. They share one database and truncate between
// cases, so they intentionally do not run in parallel.

// sourceJPEG is a decodable 1280×960 preview standing in for a cached thumbnail:
// black on the left, white on the right, so the crop's position is visible in
// the rendered avatar's pixels.
func sourceJPEG(t *testing.T) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 1280, 960))
	for y := range 960 {
		for x := range 1280 {
			shade := uint8(0)
			if x >= 640 {
				shade = 255
			}
			img.Set(x, y, color.RGBA{R: shade, G: shade, B: shade, A: 255})
		}
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 90}); err != nil {
		t.Fatalf("encoding the source preview: %v", err)
	}
	return buf.Bytes()
}

// previewSource stands in for the thumbnailer: it hands back the same preview
// whatever size is asked for, so the test is about the chain from the database to
// the JPEG rather than about thumbnail generation.
type previewSource struct{ data []byte }

// OpenOrGenerate returns the fixed preview.
func (p *previewSource) OpenOrGenerate(_ context.Context, _ photos.Photo, _ string) (io.ReadCloser, error) {
	return io.NopCloser(bytes.NewReader(p.data)), nil
}

// passThroughGuard is a no-op read guard: authorization is auth's own test's job.
func passThroughGuard(next http.Handler) http.Handler { return next }

// TestAvatarRoute_rendersFromTheCatalogue drives the whole chain against a real
// database: a subject with a marker on a real photo row → the avatar source query
// → the photo record → a rendered JPEG on the wire, cut where the face is.
func TestAvatarRoute_rendersFromTheCatalogue(t *testing.T) {
	db := dbtest.New(t)
	dbtest.TruncateAll(t, db)
	ctx := context.Background()
	peopleStore := people.NewStore(db.Pool())
	photoStore := photos.NewStore(db.Pool())

	photo, err := photoStore.Create(ctx, photos.Photo{
		FileHash:   "0f1e2d3c4b5a69788796a5b4c3d2e1f00f1e2d3c4b5a69788796a5b4c3d2e1f0",
		FilePath:   "2024/01/face.jpg",
		FileName:   "face.jpg",
		FileWidth:  4000,
		FileHeight: 3000,
	})
	if err != nil {
		t.Fatalf("creating the photo: %v", err)
	}
	subject, err := peopleStore.CreateSubject(ctx, people.Subject{Name: "Anna"})
	if err != nil {
		t.Fatalf("creating the subject: %v", err)
	}
	// A face on the white right-hand half of the frame.
	if _, err := peopleStore.CreateMarker(ctx, people.Marker{
		PhotoUID: photo.UID, SubjectUID: &subject.UID,
		X: 0.7, Y: 0.4, W: 0.15, H: 0.2, Score: 90,
	}); err != nil {
		t.Fatalf("creating the marker: %v", err)
	}

	api := avatarapi.NewAPI(avatarapi.Config{
		Subjects:    peopleStore,
		Photos:      photoStore,
		Renderer:    avatar.New(&previewSource{data: sourceJPEG(t)}, t.TempDir()),
		RequireAuth: passThroughGuard,
	})
	router := chi.NewRouter()
	api.RegisterRoutes(router)

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequestWithContext(
		ctx, http.MethodGet, "/subjects/"+subject.UID+"/avatar", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (%s)", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); ct != "image/jpeg" {
		t.Errorf("Content-Type = %q, want image/jpeg", ct)
	}
	img, err := jpeg.Decode(bytes.NewReader(rec.Body.Bytes()))
	if err != nil {
		t.Fatalf("the response is not a JPEG: %v", err)
	}
	if img.Bounds().Dx() != img.Bounds().Dy() {
		t.Errorf("avatar is %v, want a square", img.Bounds())
	}
	if r, _, _, _ := img.At(img.Bounds().Dx()/2, img.Bounds().Dy()/2).RGBA(); r < 0x8000 {
		t.Errorf("the crop landed on the dark half of the frame (centre red = %d)", r)
	}

	// The same request with the returned ETag must be answered 304.
	second := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(ctx, http.MethodGet, "/subjects/"+subject.UID+"/avatar", nil)
	req.Header.Set("If-None-Match", rec.Header().Get("ETag"))
	router.ServeHTTP(second, req)
	if second.Code != http.StatusNotModified {
		t.Errorf("conditional request status = %d, want 304", second.Code)
	}
}

// TestAvatarRoute_subjectWithoutAPictureIs404 proves the placeholder case is a
// clean 404 from the real query rather than an empty 200 or a 500.
func TestAvatarRoute_subjectWithoutAPictureIs404(t *testing.T) {
	db := dbtest.New(t)
	dbtest.TruncateAll(t, db)
	ctx := context.Background()
	peopleStore := people.NewStore(db.Pool())

	subject, err := peopleStore.CreateSubject(ctx, people.Subject{Name: "Nobody"})
	if err != nil {
		t.Fatalf("creating the subject: %v", err)
	}

	api := avatarapi.NewAPI(avatarapi.Config{
		Subjects:    peopleStore,
		Photos:      photos.NewStore(db.Pool()),
		Renderer:    avatar.New(&previewSource{data: sourceJPEG(t)}, t.TempDir()),
		RequireAuth: passThroughGuard,
	})
	router := chi.NewRouter()
	api.RegisterRoutes(router)

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequestWithContext(
		ctx, http.MethodGet, "/subjects/"+subject.UID+"/avatar", nil))
	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404 (%s)", rec.Code, rec.Body.String())
	}
}
