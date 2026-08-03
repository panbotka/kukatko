package maintenanceapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/panbotka/kukatko/internal/audit"
	"github.com/panbotka/kukatko/internal/namelessjob"
	"github.com/panbotka/kukatko/internal/people"
)

// fakeNameless is a stub NamelessRepair recording what the API scheduled through
// it, so a test can assert that a refused apply scheduled nothing at all.
type fakeNameless struct {
	subjects    []people.NamelessSubject
	listErr     error
	undo        namelessjob.Undo
	snapshotErr error
	detachErr   error
	restoreErr  error
	detached    []namelessjob.Undo
	restored    []namelessjob.Undo
	lastMeta    audit.Meta
}

func (f *fakeNameless) List(context.Context) ([]people.NamelessSubject, error) {
	return f.subjects, f.listErr
}

func (f *fakeNameless) Snapshot(context.Context) (namelessjob.Undo, error) {
	return f.undo, f.snapshotErr
}

func (f *fakeNameless) EnqueueDetach(_ context.Context, undo namelessjob.Undo, meta audit.Meta) (int, error) {
	f.lastMeta = meta
	if f.detachErr != nil {
		return 0, f.detachErr
	}
	f.detached = append(f.detached, undo)
	return len(undo.Subjects), nil
}

func (f *fakeNameless) EnqueueRestore(_ context.Context, undo namelessjob.Undo, meta audit.Meta) (int, error) {
	f.lastMeta = meta
	if f.restoreErr != nil {
		return 0, f.restoreErr
	}
	f.restored = append(f.restored, undo)
	return len(undo.Subjects), nil
}

// newNamelessRouter mounts the API over the nameless repair (which may be nil).
func newNamelessRouter(n NamelessRepair) http.Handler {
	r := chi.NewRouter()
	NewAPI(Config{Nameless: n, RequireMaintainer: passthrough}).RegisterRoutes(r)
	return r
}

// namelessFixture is the production shape in miniature: one subject with an empty
// name owning one marker and one cached face.
func namelessFixture() (people.NamelessSubject, people.SubjectSnapshot) {
	subj := people.Subject{
		UID: "sunuikf1e9jdpjog5qgomsvgrb", Slug: "subject", Name: "", Type: people.SubjectPerson,
		CreatedAt: time.Date(2026, 8, 2, 10, 0, 0, 0, time.UTC),
	}
	return people.NamelessSubject{Subject: subj, MarkerCount: 1, FaceCount: 1},
		people.SubjectSnapshot{
			Subject:    subj,
			MarkerUIDs: []string{"mrk1"},
			Faces:      []people.FaceRef{{PhotoUID: "pho1", FaceIndex: 0}},
		}
}

// TestNamelessReportOK verifies the read-only report returns the nameless
// subjects with the totals across them.
func TestNamelessReportOK(t *testing.T) {
	t.Parallel()
	ns, _ := namelessFixture()
	ns.MarkerCount, ns.FaceCount = 16531, 111155
	fake := &fakeNameless{subjects: []people.NamelessSubject{ns}}

	rec := do(newNamelessRouter(fake), http.MethodGet, "/maintenance/nameless-subjects", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var got namelessReportResponse
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(got.Subjects) != 1 || got.Subjects[0].UID != ns.UID || got.Subjects[0].Name != "" {
		t.Fatalf("subjects = %+v, want the nameless catch-all", got.Subjects)
	}
	if got.MarkerTotal != 16531 || got.FaceTotal != 111155 {
		t.Errorf("totals = %d markers / %d faces, want 16531 / 111155", got.MarkerTotal, got.FaceTotal)
	}
}

// TestNamelessReportUnavailable verifies a nil repair answers 503 on every one of
// its endpoints rather than 404.
func TestNamelessReportUnavailable(t *testing.T) {
	t.Parallel()
	router := newNamelessRouter(nil)
	for _, tc := range []struct{ method, target string }{
		{http.MethodGet, "/maintenance/nameless-subjects"},
		{http.MethodPost, "/maintenance/nameless-subjects/detach"},
		{http.MethodPost, "/maintenance/nameless-subjects/restore"},
	} {
		if rec := do(router, tc.method, tc.target, ""); rec.Code != http.StatusServiceUnavailable {
			t.Errorf("%s %s status = %d, want 503", tc.method, tc.target, rec.Code)
		}
	}
}

// TestNamelessDetachDeliversUndoThenSchedules verifies the apply hands the undo
// file to the browser as a download — attachment disposition, the snapshot as the
// body, the plan in the headers — and only then schedules the detach.
func TestNamelessDetachDeliversUndoThenSchedules(t *testing.T) {
	t.Parallel()
	_, snap := namelessFixture()
	fake := &fakeNameless{undo: namelessjob.Undo{Subjects: []people.SubjectSnapshot{snap}}}

	rec := do(newNamelessRouter(fake), http.MethodPost, "/maintenance/nameless-subjects/detach", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if got := rec.Header().Get("Content-Disposition"); !strings.HasPrefix(got, "attachment; filename=") {
		t.Errorf("Content-Disposition = %q, want an attachment", got)
	}
	if got := rec.Header().Get(headerNamelessMarkers); got != "1" {
		t.Errorf("%s = %q, want 1", headerNamelessMarkers, got)
	}
	if got := rec.Header().Get(headerNamelessFaces); got != "1" {
		t.Errorf("%s = %q, want 1", headerNamelessFaces, got)
	}
	var body namelessjob.Undo
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("the response body is not a readable undo file: %v", err)
	}
	if len(body.Subjects) != 1 || body.Subjects[0].Subject.UID != snap.Subject.UID {
		t.Fatalf("undo file = %+v, want the snapshot of %s", body.Subjects, snap.Subject.UID)
	}
	if len(body.Subjects[0].MarkerUIDs) != 1 || body.Subjects[0].MarkerUIDs[0] != "mrk1" {
		t.Errorf("undo markers = %v, want [mrk1]", body.Subjects[0].MarkerUIDs)
	}
	if len(fake.detached) != 1 {
		t.Fatalf("scheduled %d detach(es), want 1 after the undo file was delivered", len(fake.detached))
	}
}

// TestNamelessDetachRefusesWithoutSnapshot verifies the HTTP form of "--apply
// requires --undo-file": when the undo snapshot cannot be read, the request fails
// and nothing is scheduled.
func TestNamelessDetachRefusesWithoutSnapshot(t *testing.T) {
	t.Parallel()
	fake := &fakeNameless{snapshotErr: errors.New("boom")}

	rec := do(newNamelessRouter(fake), http.MethodPost, "/maintenance/nameless-subjects/detach", "")
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
	if len(fake.detached) != 0 {
		t.Errorf("scheduled %d detach(es) with no undo snapshot, want none", len(fake.detached))
	}
}

// TestNamelessDetachRefusesWhenUndelivered verifies that a client the undo file
// cannot be written to leaves the catalogue untouched: the operator has no undo,
// so the detach must not be scheduled.
func TestNamelessDetachRefusesWhenUndelivered(t *testing.T) {
	t.Parallel()
	_, snap := namelessFixture()
	fake := &fakeNameless{undo: namelessjob.Undo{Subjects: []people.SubjectSnapshot{snap}}}

	req := httptest.NewRequestWithContext(context.Background(),
		http.MethodPost, "/maintenance/nameless-subjects/detach", strings.NewReader(""))
	newNamelessRouter(fake).ServeHTTP(&brokenWriter{header: http.Header{}}, req)

	if len(fake.detached) != 0 {
		t.Errorf("scheduled %d detach(es) after the undo file failed to reach the client, want none",
			len(fake.detached))
	}
}

// TestNamelessDetachNothingToDo verifies an empty library answers 409 and
// schedules nothing, rather than handing over an empty undo file.
func TestNamelessDetachNothingToDo(t *testing.T) {
	t.Parallel()
	fake := &fakeNameless{}

	rec := do(newNamelessRouter(fake), http.MethodPost, "/maintenance/nameless-subjects/detach", "")
	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409", rec.Code)
	}
	if len(fake.detached) != 0 {
		t.Errorf("scheduled %d detach(es) with nothing to detach, want none", len(fake.detached))
	}
}

// TestNamelessRestoreSchedules verifies an uploaded undo file is scheduled for
// replay and the count is reported back.
func TestNamelessRestoreSchedules(t *testing.T) {
	t.Parallel()
	_, snap := namelessFixture()
	raw, err := json.Marshal(namelessjob.Undo{Subjects: []people.SubjectSnapshot{snap}})
	if err != nil {
		t.Fatalf("marshal undo: %v", err)
	}
	fake := &fakeNameless{}

	rec := do(newNamelessRouter(fake), http.MethodPost, "/maintenance/nameless-subjects/restore", string(raw))
	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want 202", rec.Code)
	}
	var got namelessRestoreResponse
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.Queued != 1 {
		t.Errorf("queued = %d, want 1", got.Queued)
	}
	if len(fake.restored) != 1 || fake.restored[0].Subjects[0].Subject.UID != snap.Subject.UID {
		t.Errorf("restored = %+v, want the uploaded snapshot", fake.restored)
	}
}

// TestNamelessRestoreRejectsUnusableFile verifies a body that is not a usable
// undo file is refused with 400 and schedules nothing.
func TestNamelessRestoreRejectsUnusableFile(t *testing.T) {
	t.Parallel()
	for name, body := range map[string]string{
		"not json":     "{",
		"no subjects":  `{"subjects": []}`,
		"empty object": `{}`,
	} {
		fake := &fakeNameless{}
		rec := do(newNamelessRouter(fake), http.MethodPost, "/maintenance/nameless-subjects/restore", body)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("%s: status = %d, want 400", name, rec.Code)
		}
		if len(fake.restored) != 0 {
			t.Errorf("%s: scheduled %d restore(s), want none", name, len(fake.restored))
		}
	}
}

// brokenWriter is a ResponseWriter whose body writes always fail, standing in for
// a client that hung up before the undo file reached it.
type brokenWriter struct {
	header http.Header
}

func (b *brokenWriter) Header() http.Header { return b.header }

func (b *brokenWriter) Write([]byte) (int, error) { return 0, errors.New("connection reset") }

func (b *brokenWriter) WriteHeader(int) {}
