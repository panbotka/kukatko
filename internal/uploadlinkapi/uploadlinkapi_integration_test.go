//go:build integration

package uploadlinkapi_test

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"image"
	"image/jpeg"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/panbotka/kukatko/internal/apitest"
	"github.com/panbotka/kukatko/internal/audit"
	"github.com/panbotka/kukatko/internal/auth"
	"github.com/panbotka/kukatko/internal/database"
	"github.com/panbotka/kukatko/internal/database/dbtest"
	"github.com/panbotka/kukatko/internal/ingest"
	"github.com/panbotka/kukatko/internal/jobs"
	"github.com/panbotka/kukatko/internal/mailjob"
	"github.com/panbotka/kukatko/internal/organize"
	"github.com/panbotka/kukatko/internal/photos"
	"github.com/panbotka/kukatko/internal/settings"
	"github.com/panbotka/kukatko/internal/storage"
	"github.com/panbotka/kukatko/internal/thumb"
	"github.com/panbotka/kukatko/internal/uploadlink"
	"github.com/panbotka/kukatko/internal/uploadlinkapi"
)

// These tests run only under `make test-integration`. They wire the real auth
// API (sessions, registration, the optional-auth guard) and the real link store
// over the integration database; only the ingest pipeline is a fake that maps a
// filename onto a pre-seeded photo, because the pipeline has its own suite.

// integrationPassword is the password of every account these tests sign in.
const integrationPassword = "correct horse battery staple"

// seededIngest answers every file with the photo seeded under its filename.
type seededIngest struct {
	photos map[string]string
}

// IngestFile drains src and reports the seeded photo as created.
func (s seededIngest) IngestFile(_ context.Context, src io.Reader, req ingest.Request) ingest.FileResult {
	_, _ = io.Copy(io.Discard, src)
	return ingest.FileResult{Filename: req.Filename, Status: 201, Outcome: ingest.OutcomeCreated, PhotoUID: s.photos[req.Filename]}
}

// integrationEnv is a running server over the integration database.
type integrationEnv struct {
	server   *httptest.Server
	db       *database.DB
	authSvc  *auth.Service
	settings *settings.Store
	album    organize.Album
	label    organize.Label
	photos   map[string]string
}

// newIntegrationEnv truncates the database, seeds an album, a label and two
// photos, and serves the auth and upload-link routes over the seeded fake
// pipeline.
func newIntegrationEnv(t *testing.T) *integrationEnv {
	t.Helper()
	return newIntegrationEnvWith(t, nil)
}

// newIntegrationEnvWith is newIntegrationEnv with the pipeline chosen by the
// caller: pipeline builds one over the database, nil keeps the seeded fake.
func newIntegrationEnvWith(t *testing.T, pipeline func(*database.DB) uploadlinkapi.Ingester) *integrationEnv {
	t.Helper()
	db := dbtest.New(t)
	dbtest.TruncateAll(t, db)
	ctx := t.Context()
	e := &integrationEnv{db: db, settings: settings.NewStore(db.Pool()), photos: map[string]string{}}
	org := organize.NewStore(db.Pool())
	var err error
	if e.album, err = org.CreateAlbum(ctx, organize.Album{Title: "Pouť 2026"}); err != nil {
		t.Fatalf("CreateAlbum: %v", err)
	}
	if e.label, err = org.CreateLabel(ctx, organize.Label{Name: "pouť"}); err != nil {
		t.Fatalf("CreateLabel: %v", err)
	}
	photoStore := photos.NewStore(db.Pool())
	for _, name := range []string{"a.jpg", "b.jpg"} {
		created, err := photoStore.Create(ctx, photos.Photo{FileHash: "h-" + name, FilePath: "2026/06/" + name, FileName: name})
		if err != nil {
			t.Fatalf("creating photo: %v", err)
		}
		e.photos[name] = created.UID
	}

	store := uploadlink.NewStore(db.Pool())
	e.authSvc = auth.NewService(auth.NewStore(db.Pool()), auth.SessionPolicy{TTL: time.Hour, MaxLifetime: 3 * time.Hour})
	authAPI := auth.NewAPI(auth.APIConfig{
		Service: e.authSvc, Limiter: auth.NewLimiter(100, time.Minute),
		Registration: auth.NewRegistration(auth.RegistrationConfig{
			Service: e.authSvc, Settings: e.settings, Mail: mailjob.NewEnqueuer(mailjob.EnqueuerConfig{}),
		}),
		UploadLinks: uploadlinkapi.NewRegistrationGate(store, nil, nil),
	})
	var ingester uploadlinkapi.Ingester = seededIngest{photos: e.photos}
	if pipeline != nil {
		ingester = pipeline(db)
	}
	api := uploadlinkapi.NewAPI(uploadlinkapi.Config{
		Store: store, Ingest: ingester,
		RequireCurator: authAPI.RequireCurator, OptionalAuth: authAPI.OptionalAuth,
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

// login creates an account with role and returns its UID and session cookies.
func (e *integrationEnv) login(t *testing.T, username string, role auth.Role) (string, []*http.Cookie) {
	t.Helper()
	user, err := e.authSvc.CreateUser(t.Context(), auth.CreateUserInput{
		Username: username, Email: username + "@example.test", Password: integrationPassword, Role: role,
	})
	if err != nil {
		t.Fatalf("CreateUser(%s): %v", username, err)
	}
	body, _ := json.Marshal(map[string]string{"username": username, "password": integrationPassword})
	resp := e.send(t, http.MethodPost, "/api/v1/auth/login", "application/json", strings.NewReader(string(body)), nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("login %s = %d", username, resp.StatusCode)
	}
	return user.UID, resp.Cookies()
}

// send issues a request with cookies and returns the response, body drained
// into the returned value's Body as a fresh reader.
func (e *integrationEnv) send(
	t *testing.T, method, path, contentType string, body io.Reader, cookies []*http.Cookie,
) *http.Response {
	t.Helper()
	req, err := http.NewRequestWithContext(t.Context(), method, e.server.URL+path, body)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	for _, c := range cookies {
		req.AddCookie(c)
	}
	resp, err := apitest.Client().Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	raw, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	resp.Body = io.NopCloser(bytes.NewReader(raw))
	return resp
}

// createLink creates a link to the seeded album and label as the curator
// session and returns its code.
func (e *integrationEnv) createLink(t *testing.T, cookies []*http.Cookie) (string, string) {
	t.Helper()
	body, _ := json.Marshal(map[string]any{
		"title": "Pouť", "album_uids": []string{e.album.UID}, "label_uids": []string{e.label.UID}, "valid_days": 7,
	})
	resp := e.send(t, http.MethodPost, "/api/v1/upload-links", "application/json", bytes.NewReader(body), cookies)
	if resp.StatusCode != http.StatusCreated {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("create = %d: %s", resp.StatusCode, raw)
	}
	var out struct {
		Code string `json:"code"`
		Link struct {
			UID string `json:"uid"`
		} `json:"link"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("decoding create: %v", err)
	}
	return out.Code, out.Link.UID
}

// uploadFile uploads one file through code with name, as cookies.
func (e *integrationEnv) uploadFile(t *testing.T, code, filename, name string, cookies []*http.Cookie) *http.Response {
	t.Helper()
	return e.uploadContent(t, code, filename, name, []byte("bytes"), cookies)
}

// uploadContent uploads one file of content through code with name, as cookies.
func (e *integrationEnv) uploadContent(
	t *testing.T, code, filename, name string, content []byte, cookies []*http.Cookie,
) *http.Response {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	_ = mw.WriteField("name", name)
	w, _ := mw.CreateFormFile("files", filename)
	_, _ = w.Write(content)
	_ = mw.Close()
	return e.send(t, http.MethodPost, "/api/v1/u/"+code+"/upload", mw.FormDataContentType(), &buf, cookies)
}

// register posts a registration through code with cookies.
func (e *integrationEnv) register(t *testing.T, code string, cookies []*http.Cookie) *http.Response {
	t.Helper()
	body, _ := json.Marshal(map[string]string{
		"username": "novy", "email": "novy@example.test", "password": integrationPassword, "upload_link": code,
	})
	return e.send(t, http.MethodPost, "/api/v1/auth/register", "application/json", bytes.NewReader(body), cookies)
}

// uploadedBy reads photos.uploaded_by of photoUID ("" for NULL).
func (e *integrationEnv) uploadedBy(t *testing.T, photoUID string) string {
	t.Helper()
	var owner *string
	if err := e.db.Pool().QueryRow(t.Context(), "SELECT uploaded_by FROM photos WHERE uid = $1",
		photoUID).Scan(&owner); err != nil {
		t.Fatalf("reading uploaded_by: %v", err)
	}
	if owner == nil {
		return ""
	}
	return *owner
}

// openRegistration switches registration on or off. The secret is one nobody in
// these tests ever sends: the link is the only key they use.
func (e *integrationEnv) openRegistration(t *testing.T, enabled bool) {
	t.Helper()
	if _, err := e.settings.Set(t.Context(), settings.Update{RegistrationEnabled: enabled, RegistrationSecret: "never sent"}, "",
		audit.Entry{Action: audit.ActionSettingsUpdate, TargetType: "settings"}); err != nil {
		t.Fatalf("storing settings: %v", err)
	}
}

// TestAnonymousUploadThenRegisterClaimsThePhotos walks the whole anonymous path:
// a curator creates a link, somebody uploads through it without an account (the
// photo lands in the album at once), then registers with the link instead of the
// secret — and the photo becomes theirs while the account waits for approval.
func TestAnonymousUploadThenRegisterClaimsThePhotos(t *testing.T) {
	e := newIntegrationEnv(t)
	_, curator := e.login(t, "kurator", auth.RoleCurator)
	code, linkUID := e.createLink(t, curator)

	resp := e.uploadFile(t, code, "a.jpg", "Jana", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("upload = %d", resp.StatusCode)
	}
	var session []*http.Cookie
	for _, c := range resp.Cookies() {
		if c.Name == uploadlinkapi.SessionCookieName {
			session = append(session, c)
		}
	}
	if len(session) != 1 {
		t.Fatalf("anonymous upload set no session cookie: %v", resp.Cookies())
	}
	albums, err := organize.NewStore(e.db.Pool()).AlbumsForPhoto(t.Context(), e.photos["a.jpg"])
	if err != nil || len(albums) != 1 {
		t.Fatalf("photo not in the album: %+v, %v", albums, err)
	}
	if owner := e.uploadedBy(t, e.photos["a.jpg"]); owner != "" {
		t.Fatalf("anonymous photo already owned by %q", owner)
	}

	e.openRegistration(t, false)
	if resp := e.register(t, code, session); resp.StatusCode != http.StatusForbidden {
		t.Fatalf("register on a closed instance = %d, want 403", resp.StatusCode)
	}
	e.openRegistration(t, true)
	if resp := e.register(t, "Zz9Zz9Zz", session); resp.StatusCode != http.StatusForbidden {
		t.Fatalf("register with an unknown link = %d, want 403", resp.StatusCode)
	}
	resp = e.register(t, code, session)
	if resp.StatusCode != http.StatusCreated {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("register with the link = %d: %s", resp.StatusCode, raw)
	}
	var newUID string
	var approved *time.Time
	if err := e.db.Pool().QueryRow(t.Context(), "SELECT uid, approved_at FROM users WHERE username = 'novy'").
		Scan(&newUID, &approved); err != nil {
		t.Fatalf("reading the new account: %v", err)
	}
	if approved != nil {
		t.Error("an account registered through a link was approved without an administrator")
	}
	if owner := e.uploadedBy(t, e.photos["a.jpg"]); owner != newUID {
		t.Errorf("uploaded_by = %q, want the new account %q", owner, newUID)
	}
	var details string
	if err := e.db.Pool().QueryRow(t.Context(),
		"SELECT details->>'upload_link' FROM audit_log WHERE action = $1", audit.ActionUserRegister).
		Scan(&details); err != nil || details != linkUID {
		t.Errorf("register audit upload_link = %q, %v; want %q", details, err, linkUID)
	}
}

// TestSignedInUploadIsAttributed verifies the optional-auth guard: a signed-in
// uploader is the actor of the audit entry and the account on the provenance.
func TestSignedInUploadIsAttributed(t *testing.T) {
	e := newIntegrationEnv(t)
	_, curator := e.login(t, "kurator", auth.RoleCurator)
	code, _ := e.createLink(t, curator)
	viewerUID, viewer := e.login(t, "divak", auth.RoleViewer)

	resp := e.uploadFile(t, code, "b.jpg", "", viewer)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("upload = %d", resp.StatusCode)
	}
	var owner, actor *string
	if err := e.db.Pool().QueryRow(t.Context(),
		"SELECT uploaded_by FROM upload_link_photos WHERE photo_uid = $1", e.photos["b.jpg"]).Scan(&owner); err != nil {
		t.Fatalf("reading provenance: %v", err)
	}
	if err := e.db.Pool().QueryRow(t.Context(),
		"SELECT actor_uid FROM audit_log WHERE action = $1", audit.ActionUploadLinkUpload).Scan(&actor); err != nil {
		t.Fatalf("reading audit: %v", err)
	}
	if owner == nil || *owner != viewerUID || actor == nil || *actor != viewerUID {
		t.Errorf("provenance account %v, audit actor %v; want %q", owner, actor, viewerUID)
	}
}

// TestRevokedLinkRefusesEverything verifies a revoked link answers 410 to the
// page and the upload, and refuses a registration.
func TestRevokedLinkRefusesEverything(t *testing.T) {
	e := newIntegrationEnv(t)
	_, curator := e.login(t, "kurator", auth.RoleCurator)
	code, linkUID := e.createLink(t, curator)
	e.openRegistration(t, true)

	if resp := e.send(t, http.MethodPost, "/api/v1/upload-links/"+linkUID+"/revoke", "", nil, curator); resp.StatusCode != http.StatusOK {
		t.Fatalf("revoke = %d", resp.StatusCode)
	}
	if resp := e.send(t, http.MethodGet, "/api/v1/u/"+code, "", nil, nil); resp.StatusCode != http.StatusGone {
		t.Errorf("page = %d, want 410", resp.StatusCode)
	}
	if resp := e.uploadFile(t, code, "a.jpg", "", nil); resp.StatusCode != http.StatusGone {
		t.Errorf("upload = %d, want 410", resp.StatusCode)
	}
	if resp := e.register(t, code, nil); resp.StatusCode != http.StatusForbidden {
		t.Errorf("register = %d, want 403", resp.StatusCode)
	}
	if resp := e.send(t, http.MethodGet, "/api/v1/upload-links", "", nil, nil); resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("anonymous management list = %d, want 401", resp.StatusCode)
	}
}

// realPipeline builds the real ingest pipeline over db — temp-dir storage and
// cache, the real job queue — so a test sees exactly what an upload stores and
// schedules.
func realPipeline(t *testing.T) func(*database.DB) uploadlinkapi.Ingester {
	t.Helper()
	return func(db *database.DB) uploadlinkapi.Ingester {
		fs, err := storage.NewFS(t.TempDir())
		if err != nil {
			t.Fatalf("storage.NewFS: %v", err)
		}
		enqueuer := jobs.NewEnqueuer(jobs.NewStore(db.Pool()))
		return ingest.New(ingest.Config{
			Storage: fs, Photos: photos.NewStore(db.Pool()), Thumbnailer: thumb.New(fs, t.TempDir()),
			Enqueuer: enqueuer, OCR: enqueuer, TempDir: t.TempDir(),
		})
	}
}

// count runs a count(*) query and returns its result.
func (e *integrationEnv) count(t *testing.T, query string, args ...any) int {
	t.Helper()
	var n int
	if err := e.db.Pool().QueryRow(t.Context(), query, args...).Scan(&n); err != nil {
		t.Fatalf("%s: %v", query, err)
	}
	return n
}

// TestJunkThroughALiveLinkIsRefused is the prerelease repro: 5 KB of random
// bytes named broken.jpg, uploaded anonymously through a live link. It must come
// back as a per-file 415 with the stable not_media code, and leave nothing
// behind — no photos row, no upload_link_photos row, no job, no audit entry
// (the link audits only what it accepted). A real JPEG through the same link
// and pipeline is the control: it is created, filed and scheduled.
func TestJunkThroughALiveLinkIsRefused(t *testing.T) {
	e := newIntegrationEnvWith(t, realPipeline(t))
	_, curator := e.login(t, "kurator", auth.RoleCurator)
	code, _ := e.createLink(t, curator)
	photosBefore := e.count(t, "SELECT count(*) FROM photos")

	junk := make([]byte, 5000)
	if _, err := rand.Read(junk); err != nil {
		t.Fatalf("reading random bytes: %v", err)
	}
	// Pin the head off every signature the sniffer knows, so the one-in-millions
	// random stream that starts with a start code cannot flake the test.
	copy(junk, "junk")
	resp := e.uploadContent(t, code, "broken.jpg", "Jana", junk, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("upload = %d, want 200 with a per-file result", resp.StatusCode)
	}
	var body struct {
		Results []ingest.FileResult `json:"results"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decoding upload: %v", err)
	}
	if len(body.Results) != 1 {
		t.Fatalf("results = %+v, want one", body.Results)
	}
	if res := body.Results[0]; res.Status != http.StatusUnsupportedMediaType ||
		res.Code != ingest.CodeNotMedia || res.Outcome != ingest.OutcomeError || res.PhotoUID != "" {
		t.Errorf("result = %+v, want a 415 %q refusal", res, ingest.CodeNotMedia)
	}
	if got := e.count(t, "SELECT count(*) FROM photos"); got != photosBefore {
		t.Errorf("photos rows = %d, want %d (nothing created)", got, photosBefore)
	}
	if got := e.count(t, "SELECT count(*) FROM upload_link_photos"); got != 0 {
		t.Errorf("upload_link_photos rows = %d, want 0", got)
	}
	if got := e.count(t, "SELECT count(*) FROM jobs"); got != 0 {
		t.Errorf("jobs = %d, want 0", got)
	}
	if got := e.count(t, "SELECT count(*) FROM audit_log WHERE action = $1", audit.ActionUploadLinkUpload); got != 0 {
		t.Errorf("upload audit entries = %d, want 0", got)
	}

	var real bytes.Buffer
	if err := jpeg.Encode(&real, image.NewRGBA(image.Rect(0, 0, 32, 24)), nil); err != nil {
		t.Fatalf("encoding jpeg: %v", err)
	}
	if resp := e.uploadContent(t, code, "real.jpg", "Jana", real.Bytes(), nil); resp.StatusCode != http.StatusOK {
		t.Fatalf("control upload = %d", resp.StatusCode)
	}
	if got := e.count(t, "SELECT count(*) FROM upload_link_photos"); got != 1 {
		t.Errorf("control: upload_link_photos rows = %d, want 1", got)
	}
	if got := e.count(t, "SELECT count(*) FROM jobs"); got == 0 {
		t.Error("control: a real photo scheduled no jobs; the junk assertion above proves nothing")
	}
}

// TestAVIFAndTruncatedImagesThroughALinkAreRefused verifies the link refuses an
// AVIF by its name and by its bytes alike with the same unsupported_type code
// (one renamed .jpg gets past the extension check, not past the pipeline), and a
// truncated JPEG as damaged — with nothing created, filed or queued.
func TestAVIFAndTruncatedImagesThroughALinkAreRefused(t *testing.T) {
	e := newIntegrationEnvWith(t, realPipeline(t))
	_, curator := e.login(t, "kurator", auth.RoleCurator)
	code, _ := e.createLink(t, curator)
	photosBefore := e.count(t, "SELECT count(*) FROM photos")

	avif := append([]byte("\x00\x00\x00\x1cftypavif\x00\x00\x00\x00avifmif1miaf"), make([]byte, 600)...)
	var whole bytes.Buffer
	if err := jpeg.Encode(&whole, image.NewRGBA(image.Rect(0, 0, 32, 24)), nil); err != nil {
		t.Fatalf("encoding jpeg: %v", err)
	}
	cases := []struct {
		name string
		data []byte
		code string
	}{
		{"f.avif", avif, ingest.CodeUnsupportedType},
		{"renamed.jpg", avif, ingest.CodeUnsupportedType},
		{"trunc.jpg", whole.Bytes()[:whole.Len()/2], ingest.CodeDamaged},
	}
	for _, tc := range cases {
		resp := e.uploadContent(t, code, tc.name, "Jana", tc.data, nil)
		var body struct {
			Results []ingest.FileResult `json:"results"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
			t.Fatalf("decoding upload of %s: %v", tc.name, err)
		}
		if len(body.Results) != 1 {
			t.Fatalf("%s: results = %+v, want one", tc.name, body.Results)
		}
		if res := body.Results[0]; res.Status != http.StatusUnsupportedMediaType || res.Code != tc.code || res.PhotoUID != "" {
			t.Errorf("%s = %+v, want a 415 %q refusal", tc.name, res, tc.code)
		}
	}
	if got := e.count(t, "SELECT count(*) FROM photos"); got != photosBefore {
		t.Errorf("photos rows = %d, want %d (nothing created)", got, photosBefore)
	}
	if got := e.count(t, "SELECT count(*) FROM upload_link_photos"); got != 0 {
		t.Errorf("upload_link_photos rows = %d, want 0", got)
	}
	if got := e.count(t, "SELECT count(*) FROM jobs"); got != 0 {
		t.Errorf("jobs = %d, want 0", got)
	}
}
