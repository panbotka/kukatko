//go:build integration

package userpicapi_test

import (
	"bytes"
	"context"
	"encoding/json"
	"image"
	"image/color"
	"image/jpeg"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/panbotka/kukatko/internal/auth"
	"github.com/panbotka/kukatko/internal/avatar"
	"github.com/panbotka/kukatko/internal/database/dbtest"
	"github.com/panbotka/kukatko/internal/people"
	"github.com/panbotka/kukatko/internal/photos"
	"github.com/panbotka/kukatko/internal/userpic"
	"github.com/panbotka/kukatko/internal/userpicapi"
)

// These tests run only under `make test-integration` against the database named
// by KUKATKO_TEST_DATABASE_URL. They share one database and truncate per case, so
// they do not run in parallel.

const testPassword = "correct horse battery staple"

// env wires the auth and profile-picture APIs behind an httptest server over the
// integration database.
type env struct {
	server    *httptest.Server
	authStore *auth.Store
	authSvc   *auth.Service
	people    *people.Store
	photos    *photos.Store
}

// squareJPEG builds a decodable side×side JPEG, standing in both for an uploaded
// picture and for a cached preview the renderer cuts from.
func squareJPEG(t *testing.T, side int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, side, side))
	for y := range side {
		for x := range side {
			img.Set(x, y, color.RGBA{R: 200, G: 120, B: 40, A: 255})
		}
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 90}); err != nil {
		t.Fatalf("encoding the fixture: %v", err)
	}
	return buf.Bytes()
}

// previewSource stands in for the thumbnailer: it hands back the same preview
// whatever size is asked for, so these tests are about the chain rather than
// about thumbnail generation.
type previewSource struct{ data []byte }

func (p *previewSource) OpenOrGenerate(_ context.Context, _ photos.Photo, _ string) (io.ReadCloser, error) {
	return io.NopCloser(bytes.NewReader(p.data)), nil
}

// newEnv builds the HTTP test environment over a freshly truncated database.
func newEnv(t *testing.T) *env {
	t.Helper()
	db := dbtest.New(t)
	dbtest.TruncateAll(t, db)

	pool := db.Pool()
	authStore := auth.NewStore(pool)
	authSvc := auth.NewService(authStore, auth.SessionPolicy{TTL: time.Hour, MaxLifetime: 3 * time.Hour})
	authAPI := auth.NewAPI(auth.APIConfig{Service: authSvc, Limiter: auth.NewLimiter(100, time.Minute)})
	photoStore := photos.NewStore(pool)
	peopleStore := people.NewStore(pool)

	api := userpicapi.NewAPI(userpicapi.Config{
		Pictures: userpic.NewService(userpic.Config{
			Pictures: userpic.NewStore(pool),
			Users:    authStore,
			Subjects: peopleStore,
			Photos:   photoStore,
		}),
		Photos:      photoStore,
		Renderer:    avatar.New(&previewSource{data: squareJPEG(t, 1280)}, t.TempDir()),
		RequireAuth: authAPI.RequireAuth,
	})

	r := chi.NewRouter()
	r.Route("/api/v1", func(r chi.Router) {
		authAPI.RegisterRoutes(r)
		api.RegisterRoutes(r)
	})
	server := httptest.NewServer(r)
	t.Cleanup(server.Close)
	return &env{
		server: server, authStore: authStore, authSvc: authSvc,
		people: peopleStore, photos: photoStore,
	}
}

// login creates a viewer account and returns its uid and a cookie-bearing client.
// A viewer on purpose: a profile picture is self-service, so the lowest role must
// be able to set one.
func (e *env) login(t *testing.T, username string) (string, *http.Client) {
	t.Helper()
	user, err := e.authSvc.CreateUser(t.Context(), auth.CreateUserInput{
		Username: username, Email: username + "@example.test",
		Password: testPassword, Role: auth.RoleViewer,
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
	resp := e.do(t, client, http.MethodPost, "/api/v1/auth/login", "application/json", bytes.NewReader(body))
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("login status = %d, want 200", resp.StatusCode)
	}
	return user.UID, client
}

// do issues one request against the test server.
func (e *env) do(
	t *testing.T, client *http.Client, method, path, contentType string, body io.Reader,
) *http.Response {
	t.Helper()
	req, err := http.NewRequestWithContext(t.Context(), method, e.server.URL+path, body)
	if err != nil {
		t.Fatalf("building %s %s: %v", method, path, err)
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	return resp
}

// pick points the client's own picture at a photo, returning the status.
func (e *env) pick(t *testing.T, client *http.Client, photoUID string) (int, string) {
	t.Helper()
	body, _ := json.Marshal(map[string]string{"photo_uid": photoUID})
	resp := e.do(t, client, http.MethodPut, "/api/v1/auth/picture", "application/json", bytes.NewReader(body))
	defer func() { _ = resp.Body.Close() }()
	message, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(message)
}

// upload posts a picture as multipart form data, returning the status.
func (e *env) upload(t *testing.T, client *http.Client, field string, data []byte) int {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile(field, "me.jpg")
	if err != nil {
		t.Fatalf("building the multipart body: %v", err)
	}
	if _, err := part.Write(data); err != nil {
		t.Fatalf("writing the multipart body: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("closing the multipart body: %v", err)
	}
	resp := e.do(t, client, http.MethodPut, "/api/v1/auth/picture", writer.FormDataContentType(), &body)
	defer func() { _ = resp.Body.Close() }()
	return resp.StatusCode
}

// avatarOf fetches one account's picture, optionally conditionally.
func (e *env) avatarOf(t *testing.T, client *http.Client, uid, etag string) *http.Response {
	t.Helper()
	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet,
		e.server.URL+"/api/v1/users/"+uid+"/avatar", nil)
	if err != nil {
		t.Fatalf("building the avatar request: %v", err)
	}
	if etag != "" {
		req.Header.Set("If-None-Match", etag)
	}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("GET avatar: %v", err)
	}
	return resp
}

// describe reads the account page's view of the caller's own picture.
func (e *env) describe(t *testing.T, client *http.Client) userpic.State {
	t.Helper()
	resp := e.do(t, client, http.MethodGet, "/api/v1/auth/picture", "", nil)
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /auth/picture status = %d, want 200", resp.StatusCode)
	}
	var state userpic.State
	if err := json.NewDecoder(resp.Body).Decode(&state); err != nil {
		t.Fatalf("decoding the state: %v", err)
	}
	return state
}

// catalogue creates one photo row, applying opts before the insert.
func (e *env) catalogue(t *testing.T, hash string, apply func(*photos.Photo)) photos.Photo {
	t.Helper()
	photo := photos.Photo{
		FileHash: hash, FilePath: "2024/01/" + hash + ".jpg", FileName: hash + ".jpg",
		FileWidth: 4000, FileHeight: 3000,
	}
	if apply != nil {
		apply(&photo)
	}
	created, err := e.photos.Create(t.Context(), photo)
	if err != nil {
		t.Fatalf("creating the photo: %v", err)
	}
	return created
}

// TestPicture_anAccountWithNothingIs404 is the state almost every account starts
// in, and the 404 is what tells the client to draw the coloured initial.
func TestPicture_anAccountWithNothingIs404(t *testing.T) {
	env := newEnv(t)
	uid, client := env.login(t, "nobody")

	resp := env.avatarOf(t, client, uid, "")
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("status = %d, want 404", resp.StatusCode)
	}
	if state := env.describe(t, client); state.Origin != userpic.OriginNone {
		t.Errorf("origin = %q, want %q", state.Origin, userpic.OriginNone)
	}
}

// TestPicture_uploadRoundTrips proves an upload is stored, re-encoded and served
// back as a square JPEG — and that the same request with its ETag is a 304.
func TestPicture_uploadRoundTrips(t *testing.T) {
	env := newEnv(t)
	uid, client := env.login(t, "uploader")

	if status := env.upload(t, client, "picture", squareJPEG(t, 900)); status != http.StatusNoContent {
		t.Fatalf("upload status = %d, want 204", status)
	}
	if state := env.describe(t, client); state.Origin != userpic.OriginUpload {
		t.Errorf("origin = %q, want %q", state.Origin, userpic.OriginUpload)
	}

	resp := env.avatarOf(t, client, uid, "")
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("avatar status = %d, want 200", resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("reading the avatar: %v", err)
	}
	img, err := jpeg.Decode(bytes.NewReader(body))
	if err != nil {
		t.Fatalf("the served picture is not a JPEG: %v", err)
	}
	if img.Bounds().Dx() != img.Bounds().Dy() || img.Bounds().Dx() > userpic.MaxSide {
		t.Errorf("served picture is %v, want a square of at most %d", img.Bounds(), userpic.MaxSide)
	}

	etag := resp.Header.Get("ETag")
	if etag == "" {
		t.Fatal("the picture was served without an ETag")
	}
	conditional := env.avatarOf(t, client, uid, etag)
	defer func() { _ = conditional.Body.Close() }()
	if conditional.StatusCode != http.StatusNotModified {
		t.Errorf("conditional status = %d, want 304", conditional.StatusCode)
	}
}

// TestPicture_uploadRefusesWhatCannotBeAPicture covers the two rejected inputs a
// client can actually send: bytes that are not a decodable image, and no file.
func TestPicture_uploadRefusesWhatCannotBeAPicture(t *testing.T) {
	env := newEnv(t)
	_, client := env.login(t, "confused")

	if status := env.upload(t, client, "picture", []byte("this is not an image")); status != http.StatusBadRequest {
		t.Errorf("undecodable bytes: status = %d, want 400", status)
	}
	if status := env.upload(t, client, "wrongfield", squareJPEG(t, 100)); status != http.StatusBadRequest {
		t.Errorf("wrong field name: status = %d, want 400", status)
	}
}

// TestPicture_pickRoundTripsAndFallsBackWhenThePhotoGoes is the acceptance case
// for a picked photo: it is served, and archiving it later drops the account back
// down the chain instead of erroring or serving a photo that is on its way out.
func TestPicture_pickRoundTripsAndFallsBackWhenThePhotoGoes(t *testing.T) {
	env := newEnv(t)
	uid, client := env.login(t, "picker")
	photo := env.catalogue(t, "1111111111111111111111111111111111111111111111111111111111111111", nil)

	if status, body := env.pick(t, client, photo.UID); status != http.StatusNoContent {
		t.Fatalf("pick status = %d, want 204 (%s)", status, body)
	}
	state := env.describe(t, client)
	if state.Origin != userpic.OriginPhoto || state.PhotoUID != photo.UID {
		t.Errorf("state = %+v, want the picked photo", state)
	}

	resp := env.avatarOf(t, client, uid, "")
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("avatar status = %d, want 200", resp.StatusCode)
	}

	// The photo is archived afterwards; nothing else in the chain answers, so the
	// account goes back to the coloured initial rather than keeping the picture.
	if _, err := env.photos.Archive(t.Context(), photo.UID); err != nil {
		t.Fatalf("archiving the photo: %v", err)
	}
	gone := env.avatarOf(t, client, uid, "")
	defer func() { _ = gone.Body.Close() }()
	if gone.StatusCode != http.StatusNotFound {
		t.Errorf("after archiving: status = %d, want 404", gone.StatusCode)
	}
}

// TestPicture_pickRefusesAPrivateOrHiddenPhoto is the flag that must not be
// sidesteppable: a photo kept out of the library cannot be put on a profile,
// where every reader of every thread would see it.
func TestPicture_pickRefusesAPrivateOrHiddenPhoto(t *testing.T) {
	env := newEnv(t)
	_, client := env.login(t, "sneaky")
	private := env.catalogue(t, "2222222222222222222222222222222222222222222222222222222222222222",
		func(p *photos.Photo) { p.Private = true })
	hidden := env.catalogue(t, "3333333333333333333333333333333333333333333333333333333333333333",
		func(p *photos.Photo) { p.HiddenFromLibrary = true })

	for _, photo := range []photos.Photo{private, hidden} {
		if status, body := env.pick(t, client, photo.UID); status != http.StatusBadRequest {
			t.Errorf("pick status = %d, want 400 (%s)", status, body)
		}
	}
	if state := env.describe(t, client); state.Origin != userpic.OriginNone {
		t.Errorf("a refused pick was stored anyway: %+v", state)
	}
}

// TestPicture_theLinkedSubjectIsTheDefault is the acceptance case that needs no
// configuring at all: an account that has said which person it is wears that
// person's face, and clearing an upload falls back to it rather than to the
// initial.
func TestPicture_theLinkedSubjectIsTheDefault(t *testing.T) {
	env := newEnv(t)
	uid, client := env.login(t, "linked")
	photo := env.catalogue(t, "4444444444444444444444444444444444444444444444444444444444444444", nil)
	subject, err := env.people.CreateSubject(t.Context(), people.Subject{Name: "Anna"})
	if err != nil {
		t.Fatalf("creating the subject: %v", err)
	}
	if _, err := env.people.CreateMarker(t.Context(), people.Marker{
		PhotoUID: photo.UID, SubjectUID: &subject.UID,
		X: 0.4, Y: 0.3, W: 0.2, H: 0.25, Score: 90,
	}); err != nil {
		t.Fatalf("creating the marker: %v", err)
	}
	if _, err := env.authStore.SetUserSubject(t.Context(), uid, &subject.UID); err != nil {
		t.Fatalf("linking the account: %v", err)
	}

	state := env.describe(t, client)
	if state.Origin != userpic.OriginSubject || state.SubjectUID != subject.UID {
		t.Errorf("state = %+v, want the linked subject's face", state)
	}
	resp := env.avatarOf(t, client, uid, "")
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("avatar status = %d, want 200", resp.StatusCode)
	}

	// An upload overrides the inherited face, and clearing it hands the face back.
	if status := env.upload(t, client, "picture", squareJPEG(t, 300)); status != http.StatusNoContent {
		t.Fatalf("upload status = %d, want 204", status)
	}
	if origin := env.describe(t, client).Origin; origin != userpic.OriginUpload {
		t.Errorf("origin = %q, want %q", origin, userpic.OriginUpload)
	}
	cleared := env.do(t, client, http.MethodDelete, "/api/v1/auth/picture", "", nil)
	defer func() { _ = cleared.Body.Close() }()
	if cleared.StatusCode != http.StatusNoContent {
		t.Fatalf("clear status = %d, want 204", cleared.StatusCode)
	}
	if origin := env.describe(t, client).Origin; origin != userpic.OriginSubject {
		t.Errorf("after clearing, origin = %q, want %q", origin, userpic.OriginSubject)
	}
}

// TestPicture_writingIsSelfScoped proves the routes that change a picture take
// the account from the session and never from the request: there is no way to
// name somebody else, and one account's upload does not appear on another's.
func TestPicture_writingIsSelfScoped(t *testing.T) {
	env := newEnv(t)
	mine, mineClient := env.login(t, "mine")
	theirs, _ := env.login(t, "theirs")

	if status := env.upload(t, mineClient, "picture", squareJPEG(t, 300)); status != http.StatusNoContent {
		t.Fatalf("upload status = %d, want 204", status)
	}
	own := env.avatarOf(t, mineClient, mine, "")
	defer func() { _ = own.Body.Close() }()
	if own.StatusCode != http.StatusOK {
		t.Errorf("own avatar status = %d, want 200", own.StatusCode)
	}
	other := env.avatarOf(t, mineClient, theirs, "")
	defer func() { _ = other.Body.Close() }()
	if other.StatusCode != http.StatusNotFound {
		t.Errorf("the other account's avatar status = %d, want 404", other.StatusCode)
	}
}

// TestPicture_requiresASession keeps the whole surface behind authentication:
// the library is not a public profile directory.
func TestPicture_requiresASession(t *testing.T) {
	env := newEnv(t)
	uid, _ := env.login(t, "somebody")
	anonymous := &http.Client{}

	resp := env.avatarOf(t, anonymous, uid, "")
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", resp.StatusCode)
	}
}
