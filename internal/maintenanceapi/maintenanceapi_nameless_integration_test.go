//go:build integration

package maintenanceapi_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/panbotka/kukatko/internal/audit"
	"github.com/panbotka/kukatko/internal/auth"
	"github.com/panbotka/kukatko/internal/database"
	"github.com/panbotka/kukatko/internal/database/dbtest"
	"github.com/panbotka/kukatko/internal/jobs"
	"github.com/panbotka/kukatko/internal/maintenanceapi"
	"github.com/panbotka/kukatko/internal/namelessjob"
	"github.com/panbotka/kukatko/internal/people"
	"github.com/panbotka/kukatko/internal/photos"
	"github.com/panbotka/kukatko/internal/vectors"
)

// These tests run only under `make test-integration` against the database named
// by KUKATKO_TEST_DATABASE_URL. They drive the admin surface of the
// nameless-subject repair end to end: the read-only report, the apply that hands
// the undo file over before it schedules anything, and the undo that takes the
// file back. The store layer itself is covered in
// internal/people/nameless_integration_test.go; what is proved here is the HTTP
// contract on top of it and the queue hand-off in between.

const (
	namelessReportPath  = "/api/v1/maintenance/nameless-subjects"
	namelessDetachPath  = "/api/v1/maintenance/nameless-subjects/detach"
	namelessRestorePath = "/api/v1/maintenance/nameless-subjects/restore"
)

// namelessEnv wires the auth and maintenance APIs over the integration database
// with the real nameless repair behind them, plus the stores a test seeds with.
type namelessEnv struct {
	server  *httptest.Server
	authSvc *auth.Service
	db      *database.DB
	people  *people.Store
	photos  *photos.Store
	vectors *vectors.Store
	jobs    *jobs.Store
	svc     *namelessjob.Service
}

// newNamelessEnv builds the environment over a freshly truncated database. The
// repair is wrapped by wrap, so a test can make the undo snapshot fail while
// everything behind it stays real.
func newNamelessEnv(t *testing.T, wrap func(maintenanceapi.NamelessRepair) maintenanceapi.NamelessRepair) *namelessEnv {
	t.Helper()
	db := dbtest.New(t)
	dbtest.TruncateAll(t, db)

	authStore := auth.NewStore(db.Pool())
	authSvc := auth.NewService(authStore, auth.SessionPolicy{TTL: time.Hour, MaxLifetime: 3 * time.Hour})
	authAPI := auth.NewAPI(auth.APIConfig{Service: authSvc, Limiter: auth.NewLimiter(100, time.Minute)})

	peopleStore := people.NewStore(db.Pool())
	jobStore := jobs.NewStore(db.Pool())
	svc := namelessjob.New(peopleStore, jobStore, nil)

	var repair maintenanceapi.NamelessRepair = svc
	if wrap != nil {
		repair = wrap(repair)
	}
	api := maintenanceapi.NewAPI(maintenanceapi.Config{
		Nameless:          repair,
		RequireMaintainer: authAPI.RequireMaintainer,
	})

	r := chi.NewRouter()
	r.Route("/api/v1", func(r chi.Router) {
		authAPI.RegisterRoutes(r)
		api.RegisterRoutes(r)
	})
	server := httptest.NewServer(r)
	t.Cleanup(server.Close)

	return &namelessEnv{
		server: server, authSvc: authSvc, db: db, people: peopleStore,
		photos: photos.NewStore(db.Pool()), vectors: vectors.NewStore(db.Pool()),
		jobs: jobStore, svc: svc,
	}
}

// loginMaintainer creates a maintainer and returns a cookie-bearing client.
func (e *namelessEnv) loginMaintainer(t *testing.T) *http.Client {
	t.Helper()
	if _, err := e.authSvc.CreateUser(t.Context(), auth.CreateUserInput{
		Username: "boss", Email: "boss@example.test", Password: testPassword, Role: auth.RoleMaintainer,
	}); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("cookiejar.New: %v", err)
	}
	client := &http.Client{Jar: jar}
	body, _ := json.Marshal(map[string]string{"username": "boss", "password": testPassword})
	resp := e.do(t, client, http.MethodPost, "/api/v1/auth/login", body)
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("login status = %d, want 200", resp.StatusCode)
	}
	return client
}

// do issues a request with an optional body and returns the response.
func (e *namelessEnv) do(t *testing.T, c *http.Client, method, path string, body []byte) *http.Response {
	t.Helper()
	var rdr io.Reader
	if body != nil {
		rdr = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(context.Background(), method, e.server.URL+path, rdr)
	if err != nil {
		t.Fatalf("new request %s %s: %v", method, path, err)
	}
	resp, err := c.Do(req)
	if err != nil {
		t.Fatalf("do %s %s: %v", method, path, err)
	}
	return resp
}

// plantCatchAll seeds the production shape in miniature: a subject with an empty
// name owning one face marker and the cached faces row that names it.
func (e *namelessEnv) plantCatchAll(t *testing.T) (subject people.Subject, markerUID string) {
	t.Helper()
	ctx := t.Context()
	subject, err := e.people.CreateSubject(ctx, people.Subject{Name: ""})
	if err != nil {
		t.Fatalf("CreateSubject nameless: %v", err)
	}
	photo, err := e.photos.Create(ctx, photos.Photo{
		FileHash: "nameless_http", FilePath: "2026/08/nameless.jpg", FileName: "nameless.jpg",
		FileWidth: 4000, FileHeight: 3000,
	})
	if err != nil {
		t.Fatalf("creating photo: %v", err)
	}
	marker, err := e.people.CreateMarker(ctx, people.Marker{
		PhotoUID: photo.UID, Type: people.MarkerFace, X: 0.1, Y: 0.1, W: 0.3, H: 0.3,
	})
	if err != nil {
		t.Fatalf("CreateMarker: %v", err)
	}
	vec := make([]float32, vectors.FaceDim)
	vec[0] = 1
	if err := e.vectors.SaveFaces(ctx, photo.UID, []vectors.Face{{
		FaceIndex: 0, Vector: vec, BBox: [4]float64{0.1, 0.1, 0.3, 0.3}, MarkerUID: &marker.UID,
	}}); err != nil {
		t.Fatalf("SaveFaces: %v", err)
	}
	if _, err := e.people.AssignSubject(ctx, marker.UID, subject.UID); err != nil {
		t.Fatalf("AssignSubject: %v", err)
	}
	return subject, marker.UID
}

// queued returns the queued jobs of jobType. The queue store lists by state
// rather than by type, so the type filter happens here.
func (e *namelessEnv) queued(t *testing.T, jobType string) []jobs.Job {
	t.Helper()
	listed, err := e.jobs.List(t.Context(), jobs.ListOptions{Limit: 100})
	if err != nil {
		t.Fatalf("listing jobs: %v", err)
	}
	out := make([]jobs.Job, 0, len(listed))
	for _, job := range listed {
		if job.Type == jobType {
			out = append(out, job)
		}
	}
	return out
}

// claimAll runs every queued job of jobType through fn, returning how many ran.
// It stands in for the worker: the destructive half of the repair happens in the
// queue, so a test that only issued the HTTP call has changed nothing yet.
func (e *namelessEnv) claimAll(t *testing.T, jobType string, fn func(context.Context, jobs.Job) error) int {
	t.Helper()
	listed := e.queued(t, jobType)
	for _, job := range listed {
		if err := fn(t.Context(), job); err != nil {
			t.Fatalf("running %s job %d: %v", jobType, job.ID, err)
		}
	}
	return len(listed)
}

// countJobs returns how many jobs of jobType are in the queue.
func (e *namelessEnv) countJobs(t *testing.T, jobType string) int {
	t.Helper()
	return len(e.queued(t, jobType))
}

// TestNamelessReportFindsThePlantedSubject verifies the read-only report names
// the catch-all with what hangs off it, and that running it changes nothing.
func TestNamelessReportFindsThePlantedSubject(t *testing.T) {
	env := newNamelessEnv(t, nil)
	subject, markerUID := env.plantCatchAll(t)
	client := env.loginMaintainer(t)

	resp := env.do(t, client, http.MethodGet, namelessReportPath, nil)
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	var got struct {
		Subjects []struct {
			UID         string `json:"uid"`
			Name        string `json:"name"`
			MarkerCount int    `json:"marker_count"`
			FaceCount   int    `json:"face_count"`
		} `json:"subjects"`
		MarkerTotal int `json:"marker_total"`
		FaceTotal   int `json:"face_total"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(got.Subjects) != 1 || got.Subjects[0].UID != subject.UID {
		t.Fatalf("report = %+v, want the planted catch-all %s", got.Subjects, subject.UID)
	}
	if got.Subjects[0].Name != "" || got.MarkerTotal != 1 || got.FaceTotal != 1 {
		t.Errorf("report = name %q, %d markers, %d faces; want an empty name with 1/1",
			got.Subjects[0].Name, got.MarkerTotal, got.FaceTotal)
	}
	if sub := markerSubjectUID(t, env, markerUID); sub == nil || *sub != subject.UID {
		t.Error("the report detached the marker; it must be read-only")
	}
}

// TestNamelessReportRequiresMaintainer verifies the repair is maintainer-only,
// like the rest of /maintenance.
func TestNamelessReportRequiresMaintainer(t *testing.T) {
	env := newNamelessEnv(t, nil)
	if _, err := env.authSvc.CreateUser(t.Context(), auth.CreateUserInput{
		Username: "editor", Email: "editor@example.test", Password: testPassword, Role: auth.RoleEditor,
	}); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("cookiejar.New: %v", err)
	}
	client := &http.Client{Jar: jar}
	body, _ := json.Marshal(map[string]string{"username": "editor", "password": testPassword})
	login := env.do(t, client, http.MethodPost, "/api/v1/auth/login", body)
	_ = login.Body.Close()

	for _, path := range []string{namelessReportPath, namelessDetachPath} {
		method := http.MethodGet
		if path == namelessDetachPath {
			method = http.MethodPost
		}
		resp := env.do(t, client, method, path, nil)
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusForbidden {
			t.Errorf("%s %s as an editor = %d, want 403", method, path, resp.StatusCode)
		}
	}
}

// failingSnapshot wraps the real repair with an undo snapshot that cannot be
// produced, standing in for the state where the operator would end up with no
// undo file.
type failingSnapshot struct {
	maintenanceapi.NamelessRepair
}

func (failingSnapshot) Snapshot(context.Context) (namelessjob.Undo, error) {
	return namelessjob.Undo{}, errors.New("snapshot unavailable")
}

// TestNamelessDetachRefusesWithoutADeliverableSnapshot verifies the HTTP form of
// `--apply requires --undo-file`: with no undo file to hand over, the apply fails
// and — crucially — schedules nothing, leaving the subject and its marker intact.
func TestNamelessDetachRefusesWithoutADeliverableSnapshot(t *testing.T) {
	env := newNamelessEnv(t, func(r maintenanceapi.NamelessRepair) maintenanceapi.NamelessRepair {
		return failingSnapshot{NamelessRepair: r}
	})
	subject, markerUID := env.plantCatchAll(t)
	client := env.loginMaintainer(t)

	resp := env.do(t, client, http.MethodPost, namelessDetachPath, nil)
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", resp.StatusCode)
	}
	if n := env.countJobs(t, jobs.TypeNamelessDetach); n != 0 {
		t.Errorf("scheduled %d detach job(s) with no undo file, want none", n)
	}
	if _, err := env.people.GetSubjectByUID(t.Context(), subject.UID); err != nil {
		t.Errorf("the subject was removed despite the refusal: %v", err)
	}
	if sub := markerSubjectUID(t, env, markerUID); sub == nil || *sub != subject.UID {
		t.Error("the marker was detached despite the refusal")
	}
}

// TestNamelessDetachDeliversUndoDetachesAndAudits walks the whole apply: the undo
// file comes back as a download, the destructive work waits in the queue, and
// running it removes the subject, unassigns its marker and cached face, and
// leaves an audit row naming the maintainer who asked for it.
func TestNamelessDetachDeliversUndoDetachesAndAudits(t *testing.T) {
	env := newNamelessEnv(t, nil)
	subject, markerUID := env.plantCatchAll(t)
	client := env.loginMaintainer(t)

	undo := detachAndReadUndo(t, env, client, subject.UID)

	// The HTTP call scheduled the work; nothing is detached until it runs.
	if sub := markerSubjectUID(t, env, markerUID); sub == nil || *sub != subject.UID {
		t.Fatal("the request detached the marker inline; the work belongs in the queue")
	}
	if ran := env.claimAll(t, jobs.TypeNamelessDetach, env.svc.HandleDetach); ran != 1 {
		t.Fatalf("ran %d detach job(s), want 1", ran)
	}

	if _, err := env.people.GetSubjectByUID(t.Context(), subject.UID); !errors.Is(err, people.ErrSubjectNotFound) {
		t.Errorf("GetSubjectByUID after the detach = %v, want ErrSubjectNotFound", err)
	}
	if sub := markerSubjectUID(t, env, markerUID); sub != nil {
		t.Errorf("marker still assigned to %s, want unassigned", *sub)
	}
	if got := auditCount(t, env, audit.ActionSubjectDelete, subject.UID); got != 1 {
		t.Errorf("%d audit row(s) for the detach, want 1", got)
	}
	if len(undo.Subjects) != 1 || len(undo.Subjects[0].MarkerUIDs) != 1 {
		t.Fatalf("undo file = %+v, want one subject with one marker", undo.Subjects)
	}
}

// TestNamelessRestoreReplaysTheUndoFile verifies the undo takes the downloaded
// file back: the subject returns under its original uid, slug and created_at and
// the marker and cached face point at it again.
func TestNamelessRestoreReplaysTheUndoFile(t *testing.T) {
	env := newNamelessEnv(t, nil)
	subject, markerUID := env.plantCatchAll(t)
	client := env.loginMaintainer(t)

	undo := detachAndReadUndo(t, env, client, subject.UID)
	if ran := env.claimAll(t, jobs.TypeNamelessDetach, env.svc.HandleDetach); ran != 1 {
		t.Fatalf("ran %d detach job(s), want 1", ran)
	}

	raw, err := json.Marshal(undo)
	if err != nil {
		t.Fatalf("marshal undo: %v", err)
	}
	resp := env.do(t, client, http.MethodPost, namelessRestorePath, raw)
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("restore status = %d, want 202", resp.StatusCode)
	}
	if ran := env.claimAll(t, jobs.TypeNamelessRestore, env.svc.HandleRestore); ran != 1 {
		t.Fatalf("ran %d restore job(s), want 1", ran)
	}

	restored, err := env.people.GetSubjectByUID(t.Context(), subject.UID)
	if err != nil {
		t.Fatalf("GetSubjectByUID after the undo: %v", err)
	}
	if restored.Slug != subject.Slug || restored.Name != subject.Name {
		t.Errorf("restored subject = slug %q name %q, want %q / %q",
			restored.Slug, restored.Name, subject.Slug, subject.Name)
	}
	if !restored.CreatedAt.Equal(subject.CreatedAt) {
		t.Errorf("restored created_at = %s, want the original %s", restored.CreatedAt, subject.CreatedAt)
	}
	if sub := markerSubjectUID(t, env, markerUID); sub == nil || *sub != subject.UID {
		t.Errorf("marker subject after the undo = %v, want %s", sub, subject.UID)
	}
	faces, err := env.vectors.ListFaces(t.Context(), facePhotoUID(t, env, markerUID))
	if err != nil {
		t.Fatalf("ListFaces: %v", err)
	}
	if len(faces) != 1 || faces[0].SubjectUID == nil || *faces[0].SubjectUID != subject.UID {
		t.Errorf("cached face after the undo = %+v, want it to name %s again", faces, subject.UID)
	}
	if got := auditCount(t, env, audit.ActionSubjectCreate, subject.UID); got != 1 {
		t.Errorf("%d audit row(s) for the restore, want 1", got)
	}
}

// detachAndReadUndo issues the apply, asserts the response is the undo file as a
// download and returns the parsed file.
func detachAndReadUndo(
	t *testing.T, env *namelessEnv, client *http.Client, subjectUID string,
) namelessjob.Undo {
	t.Helper()
	resp := env.do(t, client, http.MethodPost, namelessDetachPath, nil)
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("detach status = %d, want 200", resp.StatusCode)
	}
	if got := resp.Header.Get("Content-Disposition"); !strings.HasPrefix(got, "attachment; filename=") {
		t.Fatalf("Content-Disposition = %q, want an attachment", got)
	}
	// Read to EOF rather than decoding the first JSON value: the response is
	// chunked, so the stream ends only when the handler has finished scheduling.
	// A browser taking the download as a Blob waits for exactly the same point.
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("reading the undo file: %v", err)
	}
	var undo namelessjob.Undo
	if err := json.Unmarshal(raw, &undo); err != nil {
		t.Fatalf("the response body is not a readable undo file: %v", err)
	}
	if len(undo.Subjects) != 1 || undo.Subjects[0].Subject.UID != subjectUID {
		t.Fatalf("undo file covers %+v, want %s", undo.Subjects, subjectUID)
	}
	return undo
}

// markerSubjectUID returns the subject a marker is assigned to, or nil.
func markerSubjectUID(t *testing.T, env *namelessEnv, markerUID string) *string {
	t.Helper()
	marker, err := env.people.GetMarkerByUID(t.Context(), markerUID)
	if err != nil {
		t.Fatalf("GetMarkerByUID: %v", err)
	}
	return marker.SubjectUID
}

// facePhotoUID returns the photo a marker sits on.
func facePhotoUID(t *testing.T, env *namelessEnv, markerUID string) string {
	t.Helper()
	marker, err := env.people.GetMarkerByUID(t.Context(), markerUID)
	if err != nil {
		t.Fatalf("GetMarkerByUID: %v", err)
	}
	return marker.PhotoUID
}

// auditCount returns how many audit rows exist for an action on a target.
func auditCount(t *testing.T, env *namelessEnv, action, targetUID string) int {
	t.Helper()
	n, err := audit.NewStore(env.db.Pool()).Count(t.Context(),
		audit.Filter{Action: action, TargetUID: targetUID})
	if err != nil {
		t.Fatalf("counting audit %q: %v", action, err)
	}
	return n
}
