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
	"maps"
	"mime/multipart"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/panbotka/kukatko/internal/audit"
	"github.com/panbotka/kukatko/internal/audit/audittest"
	"github.com/panbotka/kukatko/internal/auth"
	"github.com/panbotka/kukatko/internal/avatar"
	"github.com/panbotka/kukatko/internal/database/dbtest"
	"github.com/panbotka/kukatko/internal/people"
	"github.com/panbotka/kukatko/internal/photos"
	"github.com/panbotka/kukatko/internal/project"
	"github.com/panbotka/kukatko/internal/userpic"
	"github.com/panbotka/kukatko/internal/userpicapi"
)

// These tests run only under `make test-integration` against the database named
// by KUKATKO_TEST_DATABASE_URL. They share one database and truncate per case, so
// they do not run in parallel.

const testPassword = "correct horse battery staple"

// apiV is the bootstrap project's API prefix: the project-scoped reads (the
// avatar and the caller's own picture state) go through it, because only there
// does the chain know whose person to inherit a face from.
const apiV = "/api/v1/p/" + project.BootstrapSlug

// env wires the auth and profile-picture APIs behind an httptest server over the
// integration database.
type env struct {
	server    *httptest.Server
	authStore *auth.Store
	authSvc   *auth.Service
	people    *people.Store
	photos    *photos.Store
	projects  *project.Store
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
	audittest.Guard(t, db)

	pool := db.Pool()
	authStore := auth.NewStore(pool)
	authSvc := auth.NewService(authStore, auth.SessionPolicy{TTL: time.Hour, MaxLifetime: 3 * time.Hour})
	authAPI := auth.NewAPI(auth.APIConfig{Service: authSvc, Limiter: auth.NewLimiter(100, time.Minute)})
	photoStore := photos.NewStore(pool)
	peopleStore := people.NewStore(pool)

	api := userpicapi.NewAPI(userpicapi.Config{
		Pictures: userpic.NewService(userpic.Config{
			Pictures: userpic.NewStore(pool),
			Members:  project.NewStore(pool),
			Subjects: peopleStore,
			Photos:   photoStore,
		}),
		Renderer:        avatar.New(&previewSource{data: squareJPEG(t, 1280)}, t.TempDir()),
		RequireAuth:     authAPI.RequireAuth,
		RequireSignedIn: authAPI.RequireSignedIn,
	})

	// Mounted as the server mounts it: the instance-wide routes bare, the
	// project-scoped reads under the project prefix.
	r := chi.NewRouter()
	r.Route("/api/v1", func(r chi.Router) {
		authAPI.RegisterInstanceRoutes(r)
		api.RegisterRoutes(r)
		r.Route("/p/{"+auth.ProjectURLParam+"}", func(r chi.Router) {
			r.Use(auth.WithProject(project.NewStore(pool)))
			api.RegisterProjectRoutes(r)
		})
	})
	server := httptest.NewServer(r)
	t.Cleanup(server.Close)
	return &env{
		server: server, authStore: authStore, authSvc: authSvc,
		people: peopleStore, photos: photoStore, projects: project.NewStore(pool),
	}
}

// login creates a viewer account and returns its uid and a cookie-bearing client.
// A viewer on purpose: a profile picture is self-service, so the lowest role must
// be able to set one.
func (e *env) login(t *testing.T, username string) (string, *http.Client) {
	t.Helper()
	return e.loginAs(t, username, auth.AccountViewer)
}

// loginAs creates an account seeded with role (see auth.AccountRole) and
// returns its uid and a cookie-bearing client.
func (e *env) loginAs(t *testing.T, username string, role auth.AccountRole) (string, *http.Client) {
	t.Helper()
	user, err := e.authSvc.CreateUser(t.Context(), auth.CreateUserInput{
		Username: username, Email: username + "@example.test",
		Password: testPassword, Role: role,
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

// avatarOf fetches one account's picture in the bootstrap project, optionally
// conditionally.
func (e *env) avatarOf(t *testing.T, client *http.Client, uid, etag string) *http.Response {
	t.Helper()
	return e.avatarAt(t, client, apiV, uid, etag)
}

// avatarAt fetches one account's picture through the API prefix base — a
// project's, or the bare instance-wide /api/v1 — optionally conditionally.
func (e *env) avatarAt(t *testing.T, client *http.Client, base, uid, etag string) *http.Response {
	t.Helper()
	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet,
		e.server.URL+base+"/users/"+uid+"/avatar", nil)
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

// describe reads the account page's view of the caller's own picture in the
// bootstrap project.
func (e *env) describe(t *testing.T, client *http.Client) userpic.State {
	t.Helper()
	return e.describeAt(t, client, apiV)
}

// describeAt reads the caller's own picture state through the API prefix base.
func (e *env) describeAt(t *testing.T, client *http.Client, base string) userpic.State {
	t.Helper()
	resp := e.do(t, client, http.MethodGet, base+"/auth/picture", "", nil)
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
		ProjectID: project.BootstrapUID,
		FileHash:  hash, FilePath: "2024/01/" + hash + ".jpg", FileName: hash + ".jpg",
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
	if _, err := env.photos.Archive(t.Context(), project.BootstrapUID, photo.UID); err != nil {
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
	subject, err := env.people.CreateSubject(t.Context(), project.BootstrapUID, people.Subject{Name: "Anna"})
	if err != nil {
		t.Fatalf("creating the subject: %v", err)
	}
	if _, err := env.people.CreateMarker(t.Context(), project.BootstrapUID, people.Marker{
		PhotoUID: photo.UID, SubjectUID: &subject.UID,
		X: 0.4, Y: 0.3, W: 0.2, H: 0.25, Score: 90,
	}); err != nil {
		t.Fatalf("creating the marker: %v", err)
	}
	if _, err := env.authStore.SetUserSubject(t.Context(), project.BootstrapUID, uid, &subject.UID); err != nil {
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
	// The face is the person's in a project: the instance-wide route, which
	// enters none, has nobody to inherit it from.
	bare := env.avatarAt(t, client, "/api/v1", uid, "")
	_ = bare.Body.Close()
	if bare.StatusCode != http.StatusNotFound {
		t.Errorf("instance-wide avatar status = %d, want 404", bare.StatusCode)
	}
	if state := env.describeAt(t, client, "/api/v1"); state.Origin != userpic.OriginNone {
		t.Errorf("instance-wide state = %+v, want nothing", state)
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
// the library is not a public profile directory — neither in a project nor on
// the instance-wide route.
func TestPicture_requiresASession(t *testing.T) {
	env := newEnv(t)
	uid, _ := env.login(t, "somebody")
	anonymous := &http.Client{}

	for _, base := range []string{apiV, "/api/v1"} {
		resp := env.avatarAt(t, anonymous, base, uid, "")
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusUnauthorized {
			t.Errorf("%s: status = %d, want 401", base, resp.StatusCode)
		}
	}
}

// TestPicture_myOwnPictureIsNeverReusedWithoutAsking is the bug the bar had: a
// user set their picture on /account and the avatar above it went on showing the
// old one, across a reload, because the answer was still fresh in the browser's
// cache. Their own picture therefore revalidates every time — and an unchanged
// one is still a 304, so freshness is bought with an empty response and not with
// the picture all over again. Everybody else's keeps the ten-minute cache: no
// page a reader is on edits somebody else's account.
func TestPicture_myOwnPictureIsNeverReusedWithoutAsking(t *testing.T) {
	env := newEnv(t)
	mine, client := env.login(t, "mine")
	theirs, other := env.login(t, "theirs")
	for _, c := range []*http.Client{client, other} {
		if status := env.upload(t, c, "picture", squareJPEG(t, 600)); status != http.StatusNoContent {
			t.Fatalf("upload status = %d, want 204", status)
		}
	}

	own := env.avatarOf(t, client, mine, "")
	defer func() { _ = own.Body.Close() }()
	if own.StatusCode != http.StatusOK {
		t.Fatalf("own avatar status = %d, want 200", own.StatusCode)
	}
	if got, want := own.Header.Get("Cache-Control"), "private, max-age=0, must-revalidate"; got != want {
		t.Errorf("own Cache-Control = %q, want %q", got, want)
	}

	conditional := env.avatarOf(t, client, mine, own.Header.Get("ETag"))
	defer func() { _ = conditional.Body.Close() }()
	if conditional.StatusCode != http.StatusNotModified {
		t.Errorf("revalidating an unchanged picture = %d, want 304", conditional.StatusCode)
	}

	somebodyElse := env.avatarOf(t, client, theirs, "")
	defer func() { _ = somebodyElse.Body.Close() }()
	if somebodyElse.StatusCode != http.StatusOK {
		t.Fatalf("somebody else's avatar status = %d, want 200", somebodyElse.StatusCode)
	}
	if got, want := somebodyElse.Header.Get("Cache-Control"), "private, max-age=600, must-revalidate"; got != want {
		t.Errorf("somebody else's Cache-Control = %q, want %q", got, want)
	}
}

// projectB creates a second project — B to the bootstrap project's A — with
// no members.
func (e *env) projectB(t *testing.T) *project.Project {
	t.Helper()
	p, err := e.projects.Create(t.Context(), audit.Meta{}, project.CreateParams{Slug: "druhy", Name: "Druhý"})
	if err != nil {
		t.Fatalf("creating project B: %v", err)
	}
	return p
}

// join makes userUID an approved viewer of projectUID.
func (e *env) join(t *testing.T, projectUID, userUID string) {
	t.Helper()
	if _, err := e.projects.AddMember(t.Context(), audit.Meta{}, projectUID, userUID, project.RoleViewer, true); err != nil {
		t.Fatalf("adding %s to %s: %v", userUID, projectUID, err)
	}
}

// facedSubject creates a person of projectUID with one detected face on a
// fresh photo of that project.
func (e *env) facedSubject(t *testing.T, projectUID, name, hash string) people.Subject {
	t.Helper()
	photo := e.catalogue(t, hash, func(p *photos.Photo) { p.ProjectID = projectUID })
	subject, err := e.people.CreateSubject(t.Context(), projectUID, people.Subject{Name: name})
	if err != nil {
		t.Fatalf("creating %s: %v", name, err)
	}
	if _, err := e.people.CreateMarker(t.Context(), projectUID, people.Marker{
		PhotoUID: photo.UID, SubjectUID: &subject.UID,
		X: 0.4, Y: 0.3, W: 0.2, H: 0.25, Score: 90,
	}); err != nil {
		t.Fatalf("creating the marker of %s: %v", name, err)
	}
	return subject
}

// TestPicture_theInheritedFaceIsTheRequestsProjects verifies the face step
// reads the membership of the request's project (the one in the /p/{project}
// prefix): an account linked to a person only in project B wears no face in A
// but B's person in B, and once its A membership names a person, that one is
// the face in A.
func TestPicture_theInheritedFaceIsTheRequestsProjects(t *testing.T) {
	env := newEnv(t)
	uid, client := env.login(t, "twofaced")
	b := env.projectB(t)
	env.join(t, b.UID, uid)
	inB := env.facedSubject(t, b.UID, "Anna v B", "5555555555555555555555555555555555555555555555555555555555555555")
	if _, err := env.projects.SetSubject(t.Context(), audit.Meta{}, b.UID, uid, &inB.UID); err != nil {
		t.Fatalf("linking the account in B: %v", err)
	}

	resp := env.avatarOf(t, client, uid, "")
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("linked only in B, asked in A: status = %d, want 404", resp.StatusCode)
	}
	if state := env.describe(t, client); state.Origin != userpic.OriginNone || state.SubjectUID != "" {
		t.Errorf("linked only in B, described in A: %+v, want nothing", state)
	}
	inProjectB := "/api/v1/p/" + b.Slug
	if state := env.describeAt(t, client, inProjectB); state.Origin != userpic.OriginSubject ||
		state.SubjectUID != inB.UID {
		t.Errorf("linked in B, described in B: %+v, want B's person %s", state, inB.UID)
	}
	askedInB := env.avatarAt(t, client, inProjectB, uid, "")
	_ = askedInB.Body.Close()
	if askedInB.StatusCode != http.StatusOK {
		t.Errorf("linked in B, asked in B: status = %d, want 200", askedInB.StatusCode)
	}

	inA := env.facedSubject(t, project.BootstrapUID, "Anna v A",
		"6666666666666666666666666666666666666666666666666666666666666666")
	if _, err := env.authStore.SetUserSubject(t.Context(), project.BootstrapUID, uid, &inA.UID); err != nil {
		t.Fatalf("linking the account in A: %v", err)
	}
	state := env.describe(t, client)
	if state.Origin != userpic.OriginSubject || state.SubjectUID != inA.UID {
		t.Errorf("linked in both, described in A: %+v, want A's person %s", state, inA.UID)
	}
	ok := env.avatarOf(t, client, uid, "")
	defer func() { _ = ok.Body.Close() }()
	if ok.StatusCode != http.StatusOK {
		t.Errorf("linked in A: status = %d, want 200", ok.StatusCode)
	}
}

// TestPicture_aPickedPhotoOfBIsNotServedToAMemberOfAOnly verifies a picked
// photo is refused, as a private one is, to a viewer who may not see its
// project: the picker (a member of A and B) picks a photo of B, a member of A
// only gets the 404 of "no picture", and a member of both gets the picture.
func TestPicture_aPickedPhotoOfBIsNotServedToAMemberOfAOnly(t *testing.T) {
	env := newEnv(t)
	picker, pickerClient := env.login(t, "picker")
	_, onlyA := env.login(t, "onlya")
	both, bothClient := env.login(t, "both")
	b := env.projectB(t)
	env.join(t, b.UID, picker)
	env.join(t, b.UID, both)
	photo := env.catalogue(t, "7777777777777777777777777777777777777777777777777777777777777777",
		func(p *photos.Photo) { p.ProjectID = b.UID })

	if status, body := env.pick(t, pickerClient, photo.UID); status != http.StatusNoContent {
		t.Fatalf("pick status = %d, want 204 (%s)", status, body)
	}

	refused := env.avatarOf(t, onlyA, picker, "")
	_ = refused.Body.Close()
	if refused.StatusCode != http.StatusNotFound {
		t.Errorf("a member of A only: status = %d, want 404", refused.StatusCode)
	}
	served := env.avatarOf(t, bothClient, picker, "")
	_ = served.Body.Close()
	if served.StatusCode != http.StatusOK {
		t.Errorf("a member of both (%s): status = %d, want 200", both, served.StatusCode)
	}
}

// TestPicture_pickRefusesAPhotoOfAProjectTheCallerIsNotIn verifies PUT
// /auth/picture checks the caller is a member of the photo's project: a photo
// of B is "no such photo" to a member of A only, and nothing is stored.
func TestPicture_pickRefusesAPhotoOfAProjectTheCallerIsNotIn(t *testing.T) {
	env := newEnv(t)
	_, client := env.login(t, "outsider")
	b := env.projectB(t)
	photo := env.catalogue(t, "8888888888888888888888888888888888888888888888888888888888888888",
		func(p *photos.Photo) { p.ProjectID = b.UID })

	if status, body := env.pick(t, client, photo.UID); status != http.StatusBadRequest ||
		!strings.Contains(body, userpic.ErrPhotoNotFound.Error()) {
		t.Errorf("pick status = %d (%s), want 400 %q", status, body, userpic.ErrPhotoNotFound)
	}
	if state := env.describe(t, client); state.Origin != userpic.OriginNone {
		t.Errorf("a refused pick was stored anyway: %+v", state)
	}
}

// leave removes userUID's membership of projectUID.
func (e *env) leave(t *testing.T, projectUID, userUID string) {
	t.Helper()
	if err := e.projects.RemoveMember(t.Context(), audit.Meta{}, projectUID, userUID); err != nil {
		t.Fatalf("removing %s from %s: %v", userUID, projectUID, err)
	}
}

// avatarAnswer is everything a client can observe of one avatar response: the
// status, the body and every header but Date, which only says when.
type avatarAnswer struct {
	status int
	header http.Header
	body   string
}

// answerAt fetches uid's avatar through base and records the whole answer.
func (e *env) answerAt(t *testing.T, client *http.Client, base, uid string) avatarAnswer {
	t.Helper()
	resp := e.avatarAt(t, client, base, uid, "")
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("reading the avatar body: %v", err)
	}
	header := resp.Header.Clone()
	header.Del("Date")
	return avatarAnswer{status: resp.StatusCode, header: header, body: string(body)}
}

// TestPicture_isServedOnlyToWhoSharesAProjectWithTheAccount is SEC-025: an
// uploaded picture — which holds no photo whose project could be checked — went
// to every signed-in account by uid, on both routes, and 200 against 404 told
// which uids were accounts. Now an account of project B only is shown to a
// member of B (and a maintainer, and itself); a member of A only, an account in
// no project, a pending or withheld member of B, and a member of both asking
// through A's route all get exactly the answer a made-up uid gets.
func TestPicture_isServedOnlyToWhoSharesAProjectWithTheAccount(t *testing.T) {
	env := newEnv(t)
	b := env.projectB(t)
	inB := "/api/v1/p/" + b.Slug
	const madeUp = "us00000000000000000madeup"

	owner, ownerClient := env.login(t, "owner")
	env.leave(t, project.BootstrapUID, owner)
	env.join(t, b.UID, owner)
	withheld, withheldClient := env.login(t, "withheld")
	env.leave(t, project.BootstrapUID, withheld)
	for _, c := range []*http.Client{ownerClient, withheldClient} {
		if status := env.upload(t, c, "picture", squareJPEG(t, 300)); status != http.StatusNoContent {
			t.Fatalf("upload status = %d, want 204", status)
		}
	}

	_, onlyA := env.login(t, "onlya")
	loner, lonerClient := env.login(t, "loner")
	env.leave(t, project.BootstrapUID, loner)
	shared, sharedClient := env.login(t, "shared")
	env.join(t, b.UID, shared)

	pending, pendingClient := env.login(t, "pending")
	env.leave(t, project.BootstrapUID, pending)
	if _, err := env.projects.AddMember(t.Context(), audit.Meta{}, b.UID, pending, project.RoleViewer, false); err != nil {
		t.Fatalf("adding the pending member: %v", err)
	}
	outcome, err := env.projects.AddByIdentifier(t.Context(), audit.Meta{}, b.UID, "withheld", project.RoleViewer)
	if err != nil || outcome != project.OutcomeAdded {
		t.Fatalf("adding the withheld member: %q, %v", outcome, err)
	}
	_, maintainer := env.loginAs(t, "root", auth.AccountMaintainer)

	refused := []struct {
		name    string
		client  *http.Client
		base    string
		account string
	}{
		{"a member of A only, instance route", onlyA, "/api/v1", owner},
		{"a member of A only, A's route", onlyA, apiV, owner},
		{"an account in no project", lonerClient, "/api/v1", owner},
		{"a pending member of B", pendingClient, "/api/v1", owner},
		{"a member of both, A's route", sharedClient, apiV, owner},
		{"a member of B asking for a withheld member", sharedClient, inB, withheld},
		{"a maintainer, A's route", maintainer, apiV, owner},
	}
	for _, tc := range refused {
		want := env.answerAt(t, tc.client, tc.base, madeUp)
		if want.status != http.StatusNotFound {
			t.Fatalf("%s: a made-up uid answered %d, want 404", tc.name, want.status)
		}
		got := env.answerAt(t, tc.client, tc.base, tc.account)
		if got.status != want.status || got.body != want.body {
			t.Errorf("%s: answered %d with %d bytes, want the made-up uid's %d %q",
				tc.name, got.status, len(got.body), want.status, want.body)
		}
		if !maps.EqualFunc(got.header, want.header, slices.Equal) {
			t.Errorf("%s: headers %v, want the made-up uid's %v", tc.name, got.header, want.header)
		}
	}

	served := []struct {
		name    string
		client  *http.Client
		base    string
		account string
	}{
		{"the account itself", ownerClient, "/api/v1", owner},
		{"a member of B, instance route", sharedClient, "/api/v1", owner},
		{"a member of B, B's route", sharedClient, inB, owner},
		{"a maintainer, instance route", maintainer, "/api/v1", owner},
		{"a maintainer, B's route", maintainer, inB, owner},
	}
	for _, tc := range served {
		got := env.answerAt(t, tc.client, tc.base, tc.account)
		if got.status != http.StatusOK || got.header.Get("Content-Type") != "image/jpeg" {
			t.Errorf("%s: answered %d %q, want 200 image/jpeg", tc.name, got.status, got.header.Get("Content-Type"))
		}
	}
}
