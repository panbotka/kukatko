package uploadlinkapi_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/panbotka/kukatko/internal/audit"
	"github.com/panbotka/kukatko/internal/auth"
	"github.com/panbotka/kukatko/internal/ingest"
	"github.com/panbotka/kukatko/internal/ratelimit"
	"github.com/panbotka/kukatko/internal/uploadlink"
	"github.com/panbotka/kukatko/internal/uploadlinkapi"
)

// now is the pinned clock of every test.
var now = time.Date(2026, 10, 3, 12, 0, 0, 0, time.UTC)

// testCode is the short code of the fake store's link.
const testCode = "Ab3dEf7h"

// fakeStore is an in-memory uploadlinkapi.Store holding at most one link, with
// the inputs of the mutations recorded.
type fakeStore struct {
	mu        sync.Mutex
	link      uploadlink.Link
	hasLink   bool
	createErr error
	recordErr error

	listedFor *string
	created   *uploadlink.NewLink
	extended  time.Time
	revoked   bool
	recorded  []uploadlink.Upload
	entries   []audit.Entry
}

// Create records in and returns the link with the test code.
func (f *fakeStore) Create(_ context.Context, in uploadlink.NewLink, entry audit.Entry) (uploadlink.Link, string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.createErr != nil {
		return uploadlink.Link{}, "", f.createErr
	}
	f.created, f.entries = &in, append(f.entries, entry)
	creator := in.CreatedBy
	return uploadlink.Link{UID: "ul1", Title: in.Title, CreatedBy: &creator, ExpiresAt: in.ExpiresAt}, testCode, nil
}

// Get returns the link when its UID matches.
func (f *fakeStore) Get(_ context.Context, uid string) (uploadlink.Link, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if !f.hasLink || f.link.UID != uid {
		return uploadlink.Link{}, uploadlink.ErrNotFound
	}
	return f.link, nil
}

// ByCode returns the link for the test code.
func (f *fakeStore) ByCode(_ context.Context, code string) (uploadlink.Link, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if !f.hasLink || code != testCode {
		return uploadlink.Link{}, uploadlink.ErrNotFound
	}
	return f.link, nil
}

// List records the creator scope and returns the link.
func (f *fakeStore) List(_ context.Context, creatorUID string) ([]uploadlink.Link, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.listedFor = &creatorUID
	if !f.hasLink {
		return []uploadlink.Link{}, nil
	}
	return []uploadlink.Link{f.link}, nil
}

// Extend records the new expiry.
func (f *fakeStore) Extend(_ context.Context, _ string, expiresAt time.Time, entry audit.Entry) (uploadlink.Link, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.link.RevokedAt != nil {
		return uploadlink.Link{}, uploadlink.ErrRevoked
	}
	f.extended, f.entries = expiresAt, append(f.entries, entry)
	f.link.ExpiresAt = expiresAt
	return f.link, nil
}

// Revoke marks the link revoked.
func (f *fakeStore) Revoke(_ context.Context, _ string, entry audit.Entry) (uploadlink.Link, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.revoked, f.entries = true, append(f.entries, entry)
	at := now
	f.link.RevokedAt = &at
	return f.link, nil
}

// RecordUpload records up.
func (f *fakeStore) RecordUpload(_ context.Context, up uploadlink.Upload, entry audit.Entry) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.recordErr != nil {
		return f.recordErr
	}
	f.recorded, f.entries = append(f.recorded, up), append(f.entries, entry)
	return nil
}

// fakeIngest reads each file fully and answers created, or duplicate for a file
// whose content is "dup"; a read error becomes the pipeline's error result.
type fakeIngest struct {
	mu       sync.Mutex
	requests []ingest.Request
}

// IngestFile mimics the pipeline closely enough for the handler.
func (f *fakeIngest) IngestFile(_ context.Context, src io.Reader, req ingest.Request) ingest.FileResult {
	f.mu.Lock()
	f.requests = append(f.requests, req)
	f.mu.Unlock()
	body, err := io.ReadAll(src)
	if err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, ingest.ErrFileTooLarge) {
			status = http.StatusRequestEntityTooLarge
		}
		return ingest.FileResult{Filename: req.Filename, Status: status, Outcome: ingest.OutcomeError, Error: err.Error()}
	}
	if string(body) == "dup" {
		return ingest.FileResult{Filename: req.Filename, Status: 409, Outcome: ingest.OutcomeDuplicate, PhotoUID: "ph_dup"}
	}
	return ingest.FileResult{Filename: req.Filename, Status: 201, Outcome: ingest.OutcomeCreated, PhotoUID: "ph_" + req.Filename}
}

// fakeSidecar records the photos whose sidecars were scheduled.
type fakeSidecar struct {
	mu   sync.Mutex
	uids []string
}

// EnqueueSidecar records photoUID.
func (f *fakeSidecar) EnqueueSidecar(_ context.Context, photoUID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.uids = append(f.uids, photoUID)
	return nil
}

// userHeader names the test-only header carrying "uid:role" of the caller.
const userHeader = "X-Test-User"

// currentUser resolves the caller from userHeader.
func currentUser(r *http.Request) (auth.User, bool) {
	value := r.Header.Get(userHeader)
	if value == "" {
		return auth.User{}, false
	}
	uid, role, _ := strings.Cut(value, ":")
	return auth.User{UID: uid, Role: auth.Role(role)}, true
}

// passThrough is a no-op guard.
func passThrough(next http.Handler) http.Handler { return next }

// harness is a mounted API over the fakes.
type harness struct {
	store   *fakeStore
	ingest  *fakeIngest
	sidecar *fakeSidecar
	handler http.Handler
}

// newHarness mounts an API over fresh fakes; tweak adjusts the config.
func newHarness(t *testing.T, tweak func(*uploadlinkapi.Config)) *harness {
	t.Helper()
	creator := "us_owner"
	h := &harness{
		store: &fakeStore{hasLink: true, link: uploadlink.Link{
			UID: "ul1", Title: "Pouť", Note: "Díky", CreatedBy: &creator, ExpiresAt: now.Add(24 * time.Hour),
			Albums: []uploadlink.Target{{UID: "al1", Name: "Pouť 2026"}},
			Labels: []uploadlink.Target{{UID: "lb1", Name: "pouť"}},
		}},
		ingest:  &fakeIngest{},
		sidecar: &fakeSidecar{},
	}
	cfg := uploadlinkapi.Config{
		Store: h.store, Ingest: h.ingest, Sidecar: h.sidecar,
		RequireCurator: passThrough, OptionalAuth: passThrough, CurrentUser: currentUser,
		Now: func() time.Time { return now },
	}
	if tweak != nil {
		tweak(&cfg)
	}
	r := chi.NewRouter()
	uploadlinkapi.NewAPI(cfg).RegisterRoutes(r)
	h.handler = r
	return h
}

// do sends a JSON request as user ("" for anonymous).
func (h *harness) do(t *testing.T, method, target, user, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequestWithContext(context.Background(), method, target, strings.NewReader(body))
	if user != "" {
		req.Header.Set(userHeader, user)
	}
	rec := httptest.NewRecorder()
	h.handler.ServeHTTP(rec, req)
	return rec
}

// decode unmarshals the recorder's body into a generic map.
func decode(t *testing.T, rec *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var out map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decoding %q: %v", rec.Body.String(), err)
	}
	return out
}

// TestList_scopes verifies a curator lists their own links and an admin all.
func TestList_scopes(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		user string
		want string
	}{
		{"curator sees own", "us_c:curator", "us_c"},
		{"admin sees all", "us_a:admin", ""},
		{"maintainer sees all", "us_m:maintainer", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			h := newHarness(t, nil)
			rec := h.do(t, http.MethodGet, "/upload-links", tt.user, "")
			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d: %s", rec.Code, rec.Body)
			}
			if h.store.listedFor == nil || *h.store.listedFor != tt.want {
				t.Errorf("listed for %v, want %q", h.store.listedFor, tt.want)
			}
			body := decode(t, rec)
			if body["default_days"] != 30.0 || body["max_days"] != 365.0 {
				t.Errorf("bounds = %v / %v", body["default_days"], body["max_days"])
			}
			links, _ := body["links"].([]any)
			if len(links) != 1 || links[0].(map[string]any)["state"] != "active" {
				t.Errorf("links = %v", body["links"])
			}
		})
	}
}

// TestCreate verifies a create answers the code and path once, defaults the
// validity, audits, and refuses an out-of-range validity or bad targets.
func TestCreate(t *testing.T) {
	t.Parallel()

	h := newHarness(t, nil)
	rec := h.do(t, http.MethodPost, "/upload-links", "us_c:curator",
		`{"title":"  Pouť  ","album_uids":["al1"],"label_uids":["lb1"]}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body)
	}
	body := decode(t, rec)
	if body["code"] != testCode || body["path"] != "/u/"+testCode {
		t.Errorf("code/path = %v / %v", body["code"], body["path"])
	}
	in := h.store.created
	if in == nil || in.Title != "Pouť" || in.CreatedBy != "us_c" || !in.ExpiresAt.Equal(now.Add(30*24*time.Hour)) {
		t.Errorf("created = %+v", in)
	}
	if len(h.store.entries) != 1 || h.store.entries[0].Action != audit.ActionUploadLinkCreate ||
		h.store.entries[0].ActorUID != "us_c" {
		t.Errorf("entries = %+v", h.store.entries)
	}

	for _, body := range []string{`{"album_uids":["al1"],"valid_days":366}`, `{"album_uids":["al1"],"valid_days":-1}`, `{`} {
		if rec := h.do(t, http.MethodPost, "/upload-links", "us_c:curator", body); rec.Code != http.StatusBadRequest {
			t.Errorf("body %s: status = %d, want 400", body, rec.Code)
		}
	}
	h.store.createErr = uploadlink.ErrNoTargets
	if rec := h.do(t, http.MethodPost, "/upload-links", "us_c:curator", `{}`); rec.Code != http.StatusBadRequest {
		t.Errorf("no targets: status = %d, want 400", rec.Code)
	}
	if rec := h.do(t, http.MethodPost, "/upload-links", "", `{}`); rec.Code != http.StatusUnauthorized {
		t.Errorf("anonymous: status = %d, want 401", rec.Code)
	}
}

// TestExtendAndRevoke_ownership verifies only the creator or an admin manage a
// link, and somebody else gets a 404.
func TestExtendAndRevoke_ownership(t *testing.T) {
	t.Parallel()

	h := newHarness(t, nil)
	if rec := h.do(t, http.MethodPost, "/upload-links/ul1/extend", "us_x:curator", `{"valid_days":7}`); rec.Code != http.StatusNotFound {
		t.Errorf("stranger extend: status = %d, want 404", rec.Code)
	}
	if rec := h.do(t, http.MethodPost, "/upload-links/ul1/revoke", "us_x:editor", ""); rec.Code != http.StatusNotFound {
		t.Errorf("stranger revoke: status = %d, want 404", rec.Code)
	}
	rec := h.do(t, http.MethodPost, "/upload-links/ul1/extend", "us_owner:curator", `{"valid_days":7}`)
	if rec.Code != http.StatusOK || !h.store.extended.Equal(now.Add(7*24*time.Hour)) {
		t.Errorf("owner extend: status = %d, expiry %v", rec.Code, h.store.extended)
	}
	if rec := h.do(t, http.MethodPost, "/upload-links/ul1/revoke", "us_a:admin", ""); rec.Code != http.StatusOK || !h.store.revoked {
		t.Errorf("admin revoke: status = %d, revoked %v", rec.Code, h.store.revoked)
	}
	if decode(t, h.do(t, http.MethodGet, "/upload-links", "us_a:admin", ""))["links"].([]any)[0].(map[string]any)["state"] != "revoked" {
		t.Error("revoked link not listed as revoked")
	}
	if rec := h.do(t, http.MethodPost, "/upload-links/ul1/extend", "us_owner:curator", `{"valid_days":7}`); rec.Code != http.StatusConflict {
		t.Errorf("extend revoked: status = %d, want 409", rec.Code)
	}
	if rec := h.do(t, http.MethodPost, "/upload-links/ul_none/revoke", "us_a:admin", ""); rec.Code != http.StatusNotFound {
		t.Errorf("unknown link: status = %d, want 404", rec.Code)
	}
}

// TestPublic verifies the public description reveals only names and expiry,
// and that dead links say so.
func TestPublic(t *testing.T) {
	t.Parallel()

	h := newHarness(t, nil)
	rec := h.do(t, http.MethodGet, "/u/"+testCode, "", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body)
	}
	body := decode(t, rec)
	for key := range body {
		switch key {
		case "title", "note", "albums", "labels", "expires_at":
		default:
			t.Errorf("public description leaks %q", key)
		}
	}
	if strings.Contains(rec.Body.String(), "al1") || strings.Contains(rec.Body.String(), "us_owner") {
		t.Errorf("public description leaks a UID: %s", rec.Body)
	}
	if albums := body["albums"].([]any); len(albums) != 1 || albums[0] != "Pouť 2026" {
		t.Errorf("albums = %v", albums)
	}
	// The page starts the anonymous session, so a batch of concurrent uploads
	// shares one; a signed-in visitor gets none.
	if cookies := rec.Result().Cookies(); len(cookies) != 1 || cookies[0].Name != uploadlinkapi.SessionCookieName {
		t.Errorf("anonymous page cookies = %v, want the session cookie", cookies)
	}
	if signedIn := h.do(t, http.MethodGet, "/u/"+testCode, "us_v:viewer", ""); len(signedIn.Result().Cookies()) != 0 {
		t.Errorf("signed-in page set cookies %v", signedIn.Result().Cookies())
	}

	if rec := h.do(t, http.MethodGet, "/u/Zz9Zz9Zz", "", ""); rec.Code != http.StatusNotFound {
		t.Errorf("unknown: status = %d, want 404", rec.Code)
	}
	h.store.link.ExpiresAt = now.Add(-time.Second)
	rec = h.do(t, http.MethodGet, "/u/"+testCode, "", "")
	if rec.Code != http.StatusGone || decode(t, rec)["state"] != "expired" {
		t.Errorf("expired: %d %s", rec.Code, rec.Body)
	}
	revoked := now.Add(-time.Hour)
	h.store.link.RevokedAt = &revoked
	rec = h.do(t, http.MethodGet, "/u/"+testCode, "", "")
	if rec.Code != http.StatusGone || decode(t, rec)["state"] != "revoked" {
		t.Errorf("revoked: %d %s", rec.Code, rec.Body)
	}
}

// part is one multipart part: a form field when filename is empty.
type part struct {
	field, filename, content string
}

// upload posts parts to the test link's upload route as user, with cookies.
func (h *harness) upload(t *testing.T, user string, cookies []*http.Cookie, parts ...part) *httptest.ResponseRecorder {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	for _, p := range parts {
		var (
			w   io.Writer
			err error
		)
		if p.filename == "" {
			w, err = mw.CreateFormField(p.field)
		} else {
			w, err = mw.CreateFormFile(p.field, p.filename)
		}
		if err != nil {
			t.Fatalf("multipart: %v", err)
		}
		if _, err := io.WriteString(w, p.content); err != nil {
			t.Fatalf("multipart: %v", err)
		}
	}
	if err := mw.Close(); err != nil {
		t.Fatalf("multipart: %v", err)
	}
	req := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/u/"+testCode+"/upload", &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	if user != "" {
		req.Header.Set(userHeader, user)
	}
	for _, c := range cookies {
		req.AddCookie(c)
	}
	rec := httptest.NewRecorder()
	h.handler.ServeHTTP(rec, req)
	return rec
}

// results decodes an upload response's per-file results.
func results(t *testing.T, rec *httptest.ResponseRecorder) []ingest.FileResult {
	t.Helper()
	var body struct {
		Results []ingest.FileResult `json:"results"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decoding %q: %v", rec.Body.String(), err)
	}
	return body.Results
}

// TestUpload_anonymous verifies an anonymous upload gets a session cookie, its
// typed name and session hash are recorded with each photo, a duplicate is
// filed too, and an unsupported file never reaches the pipeline.
func TestUpload_anonymous(t *testing.T) {
	t.Parallel()

	h := newHarness(t, nil)
	rec := h.upload(t, "", nil,
		part{field: "name", content: "  Jana  "},
		part{field: "files", filename: "a.jpg", content: "aaa"},
		part{field: "files", filename: "b.heic", content: "dup"},
		part{field: "files", filename: "notes.txt", content: "x"},
	)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body)
	}
	got := results(t, rec)
	if len(got) != 3 || got[0].Outcome != ingest.OutcomeCreated || got[1].Outcome != ingest.OutcomeDuplicate ||
		got[2].Status != http.StatusUnsupportedMediaType || got[2].Code != "unsupported_type" {
		t.Fatalf("results = %+v", got)
	}
	if len(h.ingest.requests) != 2 || h.ingest.requests[0].UploadedBy != "" {
		t.Errorf("ingested = %+v", h.ingest.requests)
	}
	var cookie *http.Cookie
	for _, c := range rec.Result().Cookies() {
		if c.Name == uploadlinkapi.SessionCookieName {
			cookie = c
		}
	}
	if cookie == nil || !cookie.HttpOnly || cookie.Path != "/api/v1" {
		t.Fatalf("session cookie = %+v", cookie)
	}
	if len(h.store.recorded) != 2 {
		t.Fatalf("recorded = %+v", h.store.recorded)
	}
	first, second := h.store.recorded[0], h.store.recorded[1]
	if first.UploaderName != "Jana" || !first.Created || first.SessionHash != uploadlink.HashSecret(cookie.Value) {
		t.Errorf("first recorded = %+v", first)
	}
	if second.Created || second.PhotoUID != "ph_dup" {
		t.Errorf("second recorded = %+v", second)
	}
	if h.store.entries[0].Action != audit.ActionUploadLinkUpload || h.store.entries[0].ActorUID != "" {
		t.Errorf("entry = %+v", h.store.entries[0])
	}
	if len(h.sidecar.uids) != 2 {
		t.Errorf("sidecars scheduled = %v", h.sidecar.uids)
	}

	// The same browser keeps its session.
	h.upload(t, "", []*http.Cookie{cookie}, part{field: "files", filename: "c.jpg", content: "ccc"})
	if last := h.store.recorded[len(h.store.recorded)-1]; last.SessionHash != first.SessionHash {
		t.Errorf("second request session = %q, want %q", last.SessionHash, first.SessionHash)
	}
}

// TestUpload_signedIn verifies a signed-in uploader owns the photo and gets no
// session cookie.
func TestUpload_signedIn(t *testing.T) {
	t.Parallel()

	h := newHarness(t, nil)
	rec := h.upload(t, "us_v:viewer", nil, part{field: "files", filename: "a.jpg", content: "aaa"})
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body)
	}
	if h.ingest.requests[0].UploadedBy != "us_v" || h.store.recorded[0].UploadedBy != "us_v" ||
		h.store.recorded[0].SessionHash != "" || h.store.entries[0].ActorUID != "us_v" {
		t.Errorf("request %+v, recorded %+v", h.ingest.requests[0], h.store.recorded[0])
	}
	if len(rec.Result().Cookies()) != 0 {
		t.Errorf("signed-in upload set cookies %v", rec.Result().Cookies())
	}
}

// TestUpload_limits covers the size cap, the per-link file cap and the
// per-link request rate.
func TestUpload_limits(t *testing.T) {
	t.Parallel()

	t.Run("size cap", func(t *testing.T) {
		t.Parallel()
		h := newHarness(t, func(c *uploadlinkapi.Config) { c.MaxFileSize = 4 })
		got := results(t, h.upload(t, "", nil,
			part{field: "files", filename: "big.jpg", content: "12345"},
			part{field: "files", filename: "ok.jpg", content: "1234"}))
		if got[0].Status != http.StatusRequestEntityTooLarge || got[1].Outcome != ingest.OutcomeCreated {
			t.Errorf("results = %+v", got)
		}
	})
	t.Run("file cap", func(t *testing.T) {
		t.Parallel()
		h := newHarness(t, func(c *uploadlinkapi.Config) { c.MaxUploadsPerLink = 3 })
		h.store.link.UploadCount = 2
		got := results(t, h.upload(t, "", nil,
			part{field: "files", filename: "a.jpg", content: "a"},
			part{field: "files", filename: "b.jpg", content: "b"}))
		if got[0].Outcome != ingest.OutcomeCreated || got[1].Status != http.StatusTooManyRequests {
			t.Errorf("results = %+v", got)
		}
	})
	t.Run("link rate", func(t *testing.T) {
		t.Parallel()
		h := newHarness(t, func(c *uploadlinkapi.Config) { c.LinkLimit = ratelimit.New(0.001, 1) })
		h.upload(t, "", nil, part{field: "files", filename: "a.jpg", content: "a"})
		if rec := h.upload(t, "", nil, part{field: "files", filename: "b.jpg", content: "b"}); rec.Code != http.StatusTooManyRequests {
			t.Errorf("second request status = %d, want 429", rec.Code)
		}
	})
}

// TestUpload_refusals covers a dead link, a body that is not a multipart upload,
// an upload with no file and a photo the store could not file.
func TestUpload_refusals(t *testing.T) {
	t.Parallel()

	h := newHarness(t, nil)
	if rec := h.upload(t, "", nil, part{field: "name", content: "x"}); rec.Code != http.StatusBadRequest {
		t.Errorf("no files: status = %d, want 400", rec.Code)
	}
	if rec := h.do(t, http.MethodPost, "/u/"+testCode+"/upload", "", "plain"); rec.Code != http.StatusBadRequest {
		t.Errorf("not multipart: status = %d, want 400", rec.Code)
	}
	h.store.recordErr = errors.New("boom")
	got := results(t, h.upload(t, "", nil, part{field: "files", filename: "a.jpg", content: "a"}))
	if got[0].Outcome != ingest.OutcomeError || got[0].Status != http.StatusInternalServerError {
		t.Errorf("unfiled result = %+v", got[0])
	}
	h.store.link.ExpiresAt = now
	if rec := h.upload(t, "", nil, part{field: "files", filename: "a.jpg", content: "a"}); rec.Code != http.StatusGone {
		t.Errorf("expired: status = %d, want 410", rec.Code)
	}
}

// TestRegistrationGate covers admitting a live link, refusing dead and unknown
// ones with one error, and a claim with no session claiming nothing.
func TestRegistrationGate(t *testing.T) {
	t.Parallel()

	store := &fakeStore{hasLink: true, link: uploadlink.Link{UID: "ul1", ExpiresAt: now.Add(time.Hour)}}
	sidecar := &fakeSidecar{}
	gate := uploadlinkapi.NewRegistrationGate(store, sidecar, func() time.Time { return now })
	req := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/auth/register", nil)

	grant, err := gate.AdmitRegistration(context.Background(), req, testCode)
	if err != nil || grant.LinkUID != "ul1" || grant.Claim == nil || grant.Committed == nil {
		t.Fatalf("grant = %+v, %v", grant, err)
	}
	if err := grant.Claim(context.Background(), nil, "us_new"); err != nil {
		t.Errorf("claim without a session: %v", err)
	}
	grant.Committed(context.Background())
	if len(sidecar.uids) != 0 {
		t.Errorf("sidecars = %v, want none for nothing claimed", sidecar.uids)
	}

	if _, err := gate.AdmitRegistration(context.Background(), req, "Zz9Zz9Zz"); !errors.Is(err, auth.ErrRegistrationLink) {
		t.Errorf("unknown: %v, want ErrRegistrationLink", err)
	}
	store.link.ExpiresAt = now
	if _, err := gate.AdmitRegistration(context.Background(), req, testCode); !errors.Is(err, auth.ErrRegistrationLink) {
		t.Errorf("expired: %v, want ErrRegistrationLink", err)
	}
}
