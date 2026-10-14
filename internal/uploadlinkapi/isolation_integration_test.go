//go:build integration

package uploadlinkapi_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"image"
	"image/color"
	"image/jpeg"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/panbotka/kukatko/internal/apitest"
	"github.com/panbotka/kukatko/internal/audit"
	"github.com/panbotka/kukatko/internal/audit/audittest"
	"github.com/panbotka/kukatko/internal/auth"
	"github.com/panbotka/kukatko/internal/config"
	"github.com/panbotka/kukatko/internal/database/dbtest"
	"github.com/panbotka/kukatko/internal/ingest"
	"github.com/panbotka/kukatko/internal/jobs"
	"github.com/panbotka/kukatko/internal/mailjob"
	"github.com/panbotka/kukatko/internal/photos"
	"github.com/panbotka/kukatko/internal/project"
	"github.com/panbotka/kukatko/internal/projecttest"
	"github.com/panbotka/kukatko/internal/settings"
	"github.com/panbotka/kukatko/internal/storage"
	"github.com/panbotka/kukatko/internal/thumb"
	"github.com/panbotka/kukatko/internal/uploadlink"
	"github.com/panbotka/kukatko/internal/uploadlinkapi"
)

// isolationEnv is the two seeded projects behind one server: the public routes
// instance-level, the management routes under /p/{project}, the real ingest
// pipeline over a temp-dir store, and registration through a link.
type isolationEnv struct {
	f        *projecttest.Fixture
	pool     *pgxpool.Pool
	server   *httptest.Server
	photos   *photos.Store
	settings *settings.Store
}

// newIsolationEnv seeds the two tenants and builds the server.
func newIsolationEnv(t *testing.T) *isolationEnv {
	t.Helper()
	db := dbtest.New(t)
	dbtest.TruncateAll(t, db)
	audittest.Guard(t, db)
	pool := db.Pool()
	e := &isolationEnv{f: projecttest.Seed(t, pool), pool: pool, photos: photos.NewStore(pool), settings: settings.NewStore(pool)}

	store := uploadlink.NewStore(pool)
	projects := project.NewStore(pool)
	svc := auth.NewService(auth.NewStore(pool), auth.SessionPolicy{TTL: time.Hour, MaxLifetime: 3 * time.Hour})
	authAPI := auth.NewAPI(auth.APIConfig{
		Service: svc, Limiter: auth.NewLimiter(100, time.Minute),
		Registration: auth.NewRegistration(auth.RegistrationConfig{
			Service: svc, Settings: e.settings, Mail: mailjob.NewEnqueuer(mailjob.EnqueuerConfig{}),
		}),
		UploadLinks: uploadlinkapi.NewRegistrationGate(store, nil, nil),
	})
	fs, err := storage.NewFS(t.TempDir())
	if err != nil {
		t.Fatalf("storage.NewFS: %v", err)
	}
	enqueuer := jobs.NewEnqueuer(jobs.NewStore(pool))
	api := uploadlinkapi.NewAPI(uploadlinkapi.Config{
		Store: store, Projects: projects,
		Ingest: ingest.New(ingest.Config{
			Storage: fs, Photos: e.photos, Thumbnailer: thumb.New(fs, t.TempDir()),
			Enqueuer: enqueuer, OCR: enqueuer, TempDir: t.TempDir(),
			Duplicate: config.DuplicateConfig{Enabled: true, PhashMaxDiff: 12},
		}),
		RequireCurator: authAPI.RequireCurator, OptionalAuth: authAPI.OptionalAuth,
	})
	r := chi.NewRouter()
	r.Route(auth.APIBasePath, func(r chi.Router) {
		authAPI.RegisterRoutes(r)
		api.RegisterPublicRoutes(r)
		r.Route("/p/{"+auth.ProjectURLParam+"}", func(r chi.Router) {
			r.Use(auth.WithProject(projects))
			api.RegisterManagementRoutes(r)
		})
	})
	e.server = httptest.NewServer(r)
	t.Cleanup(e.server.Close)
	return e
}

// jpegOf encodes a small JPEG filled with shade, so two shades are two
// different files and one shade twice is the same bytes.
func jpegOf(t *testing.T, shade uint8) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 32, 24))
	for y := range 24 {
		for x := range 32 {
			img.Set(x, y, color.RGBA{R: shade, G: uint8(x * 7), B: uint8(y * 9), A: 255})
		}
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, nil); err != nil {
		t.Fatalf("encoding the JPEG: %v", err)
	}
	return buf.Bytes()
}

// upload sends content through the link code anonymously, carrying cookies
// (the anonymous session) when given, and returns the uploaded photo's uid and
// the session cookies the answer set or kept.
func (e *isolationEnv) upload(
	t *testing.T, code string, content []byte, cookies []*http.Cookie,
) (string, []*http.Cookie) {
	t.Helper()
	res, cookies := e.uploadResult(t, code, content, cookies)
	return res.PhotoUID, cookies
}

// uploadResult is upload returning the whole per-file result, warnings
// included.
func (e *isolationEnv) uploadResult(
	t *testing.T, code string, content []byte, cookies []*http.Cookie,
) (ingest.FileResult, []*http.Cookie) {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	_ = mw.WriteField("name", "Jana")
	w, _ := mw.CreateFormFile("files", "pout.jpg")
	_, _ = w.Write(content)
	_ = mw.Close()
	resp := e.send(t, http.MethodPost, "/api/v1/u/"+code+"/upload", mw.FormDataContentType(), &buf, cookies)
	var out struct {
		Results []ingest.FileResult `json:"results"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil || resp.StatusCode != http.StatusOK ||
		len(out.Results) != 1 || out.Results[0].PhotoUID == "" {
		t.Fatalf("upload through %s = %d %+v (%v)", code, resp.StatusCode, out.Results, err)
	}
	if set := resp.Cookies(); len(set) > 0 {
		cookies = set
	}
	return out.Results[0], cookies
}

// send issues a request with cookies and returns the response with its body
// read into memory.
func (e *isolationEnv) send(
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

// TestPublicUpload_landsInTheLinksProject uploads through B's public link: the
// photo is B's — A's store cannot see it — and its original is stored under
// B's prefix. The same bytes sent through A's link become a photo of A's under
// a different key, not a duplicate of B's.
func TestPublicUpload_landsInTheLinksProject(t *testing.T) {
	e := newIsolationEnv(t)
	ctx := t.Context()
	content := jpegOf(t, 40)

	inB, _ := e.upload(t, e.f.B.UploadLinkCode, content, nil)
	photo, err := e.photos.GetByUIDInProject(ctx, e.f.B.ProjectUID, inB)
	if err != nil {
		t.Fatalf("the upload is not B's photo: %v", err)
	}
	if !strings.HasPrefix(photo.FilePath, e.f.B.StoragePrefix) || e.f.B.StoragePrefix == "" {
		t.Errorf("file_path = %q, want it under B's prefix %q", photo.FilePath, e.f.B.StoragePrefix)
	}
	if _, err := e.photos.GetByUIDInProject(ctx, e.f.A.ProjectUID, inB); !errors.Is(err, photos.ErrPhotoNotFound) {
		t.Errorf("B's upload read from A = %v, want ErrPhotoNotFound", err)
	}

	inA, _ := e.upload(t, e.f.A.UploadLinkCode, content, nil)
	if inA == inB {
		t.Fatalf("the same bytes through A's link resolved to B's photo %s", inB)
	}
	other, err := e.photos.GetByUIDInProject(ctx, e.f.A.ProjectUID, inA)
	if err != nil {
		t.Fatalf("the upload through A's link is not A's photo: %v", err)
	}
	if other.FilePath == photo.FilePath {
		t.Errorf("A's and B's copies share the key %q", photo.FilePath)
	}
}

// TestPublicUpload_nearDuplicateWarningStaysInTheLinksProject uploads a picture
// through A's link, then through B's link the same picture at another JPEG
// quality — a near-duplicate of A's — and a byte-identical copy
// of A's file. The anonymous uploader on B's link must never be handed A's
// photo uid: the near-duplicate finds nothing in B, the byte-identical copy is
// B's own new photo and may only be warned about B's earlier upload.
func TestPublicUpload_nearDuplicateWarningStaysInTheLinksProject(t *testing.T) {
	e := newIsolationEnv(t)
	original := jpegQuality(t, 95)

	inA, _ := e.uploadResult(t, e.f.A.UploadLinkCode, original, nil)
	nearInB, _ := e.uploadResult(t, e.f.B.UploadLinkCode, jpegQuality(t, 40), nil)
	sameInB, _ := e.uploadResult(t, e.f.B.UploadLinkCode, original, nil)

	if nearInB.Outcome != ingest.OutcomeCreated || sameInB.Outcome != ingest.OutcomeCreated ||
		sameInB.PhotoUID == inA.PhotoUID {
		t.Fatalf("uploads into B = %+v / %+v, want two photos of B's own, neither A's %s", nearInB, sameInB, inA.PhotoUID)
	}
	if len(nearInB.Warnings) != 0 {
		t.Errorf("near-duplicate into an empty B warned %+v, want nothing", nearInB.Warnings)
	}
	for _, res := range []ingest.FileResult{nearInB, sameInB} {
		for _, w := range res.Warnings {
			if w.PhotoUID == inA.PhotoUID || strings.Contains(w.Message, inA.PhotoUID) {
				t.Errorf("upload through B's link named A's photo: %+v", w)
			}
		}
	}
	if _, err := e.photos.GetByUIDInProject(t.Context(), e.f.B.ProjectUID, sameInB.PhotoUID); err != nil {
		t.Errorf("the byte-identical copy is not B's photo: %v", err)
	}
}

// jpegQuality encodes one fixed, detailed picture at the given JPEG quality, so
// two qualities are two different files of the same picture (pHash-near) and
// one quality twice is the same bytes.
func jpegQuality(t *testing.T, quality int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 64, 48))
	for y := range 48 {
		for x := range 64 {
			img.Set(x, y, color.RGBA{R: uint8(x * 4), G: uint8(y * 5), B: uint8((x + y) * 2), A: 255})
		}
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: quality}); err != nil {
		t.Fatalf("encoding the JPEG: %v", err)
	}
	return buf.Bytes()
}

// TestRegistrationThroughLink_joinsOnlyTheLinksProject uploads anonymously
// through A's link and then B's in one browser session, and registers with B's
// link: the account is a member of B only — a viewer nobody approved yet — and
// it claims the photo it uploaded into B, never the one it uploaded into A.
func TestRegistrationThroughLink_joinsOnlyTheLinksProject(t *testing.T) {
	e := newIsolationEnv(t)
	ctx := t.Context()
	if _, err := e.settings.Set(ctx, e.f.B.ProjectUID, settings.Update{
		RegistrationEnabled: true, RegistrationSecret: "never sent",
	}, "", audit.Entry{Action: audit.ActionSettingsUpdate, TargetType: "settings"}); err != nil {
		t.Fatalf("opening registration in B: %v", err)
	}

	inA, session := e.upload(t, e.f.A.UploadLinkCode, jpegOf(t, 80), nil)
	inB, session := e.upload(t, e.f.B.UploadLinkCode, jpegOf(t, 120), session)

	body, _ := json.Marshal(map[string]string{
		"username": "novy", "email": "novy@example.test", "password": integrationPassword,
		"upload_link": e.f.B.UploadLinkCode,
	})
	resp := e.send(t, http.MethodPost, "/api/v1/auth/register", "application/json", bytes.NewReader(body), session)
	if resp.StatusCode != http.StatusCreated {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("register through B's link = %d: %s", resp.StatusCode, raw)
	}
	var user struct{ UID string }
	if err := e.pool.QueryRow(ctx, "SELECT uid FROM users WHERE username = 'novy'").Scan(&user.UID); err != nil {
		t.Fatalf("reading the new account: %v", err)
	}

	memberships, err := project.NewStore(e.pool).MembershipsOf(ctx, user.UID)
	if err != nil {
		t.Fatalf("MembershipsOf: %v", err)
	}
	if len(memberships) != 1 || memberships[0].ProjectUID != e.f.B.ProjectUID ||
		memberships[0].Role != project.RoleViewer || memberships[0].Approved {
		t.Errorf("memberships = %+v, want one unapproved viewer membership of B", memberships)
	}

	claimed, err := e.photos.GetByUIDInProject(ctx, e.f.B.ProjectUID, inB)
	if err != nil || claimed.UploadedBy == nil || *claimed.UploadedBy != user.UID {
		t.Errorf("B's upload uploaded_by = %v (%v), want the new account", claimed.UploadedBy, err)
	}
	untouched, err := e.photos.GetByUIDInProject(ctx, e.f.A.ProjectUID, inA)
	if err != nil || untouched.UploadedBy != nil {
		t.Errorf("A's upload uploaded_by = %v (%v), want nobody — the registration was into B", untouched.UploadedBy, err)
	}
}

// TestManagement_foreignLinkIsMissing asks B's prefix for A's link: the listing
// never names it, and every per-link route answers exactly like a link that does
// not exist, changing nothing.
func TestManagement_foreignLinkIsMissing(t *testing.T) {
	e := newIsolationEnv(t)
	f := e.f
	client := f.ClientFor(t, e.server, projecttest.SideB)

	f.AssertListExcludes(t, client, f.PathFor(projecttest.SideB, "upload-links"), f.A.UploadLinkUID, f.A.UploadLinkCode)
	for _, action := range []string{"extend", "revoke", "new-code"} {
		foreign := f.Send(t, client, http.MethodPost,
			f.PathFor(projecttest.SideB, "upload-links/"+f.A.UploadLinkUID+"/"+action), map[string]int{"valid_days": 3})
		missing := f.Send(t, client, http.MethodPost,
			f.PathFor(projecttest.SideB, "upload-links/"+projecttest.MissingUID("ul")+"/"+action), map[string]int{"valid_days": 3})
		projecttest.AssertSameShape(t, foreign, missing)
	}
	link, err := uploadlink.NewStore(e.pool).ByCode(t.Context(), f.A.UploadLinkCode)
	if err != nil || link.RevokedAt != nil {
		t.Errorf("A's link after B's attempts = %+v, %v; want it live and its code unchanged", link, err)
	}
}
