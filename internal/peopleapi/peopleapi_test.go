package peopleapi_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/panbotka/kukatko/internal/people"
	"github.com/panbotka/kukatko/internal/peopleapi"
	"github.com/panbotka/kukatko/internal/photos"
)

// fakeSubjects is an in-memory SubjectStore for handler tests. The various err
// fields force a specific error from the matching method.
type fakeSubjects struct {
	list       []people.SubjectCount
	subject    people.Subject
	photoUIDs  []string
	created    people.Subject
	updated    people.Subject
	listErr    error
	getErr     error
	createErr  error
	updateErr  error
	deleteErr  error
	photosErr  error
	lastUpdate people.SubjectUpdate
	lastCreate people.Subject
	deletedUID string
}

// ListSubjects returns the canned subject list or error.
func (f *fakeSubjects) ListSubjects(_ context.Context) ([]people.SubjectCount, error) {
	return f.list, f.listErr
}

// GetSubjectByUID returns the canned subject or error.
func (f *fakeSubjects) GetSubjectByUID(_ context.Context, _ string) (people.Subject, error) {
	return f.subject, f.getErr
}

// CreateSubject records the input and returns the canned created subject or error.
func (f *fakeSubjects) CreateSubject(_ context.Context, subj people.Subject) (people.Subject, error) {
	f.lastCreate = subj
	return f.created, f.createErr
}

// UpdateSubject records the update and returns the canned updated subject or error.
func (f *fakeSubjects) UpdateSubject(
	_ context.Context, _ string, upd people.SubjectUpdate,
) (people.Subject, error) {
	f.lastUpdate = upd
	return f.updated, f.updateErr
}

// DeleteSubject records the deleted UID and returns the canned error.
func (f *fakeSubjects) DeleteSubject(_ context.Context, uid string) error {
	f.deletedUID = uid
	return f.deleteErr
}

// ListPhotoUIDsBySubject returns the canned UID slice or error.
func (f *fakeSubjects) ListPhotoUIDsBySubject(_ context.Context, _ string) ([]string, error) {
	return f.photoUIDs, f.photosErr
}

// fakePhotos is a PhotoStore returning photos for the requested UIDs in reverse
// order, so tests can verify the handler restores the requested order.
type fakePhotos struct {
	byUID map[string]photos.Photo
	err   error
}

// ListByUIDs returns the photos for uids in reverse request order (to exercise
// the handler's reordering), or the canned error.
func (f fakePhotos) ListByUIDs(_ context.Context, uids []string) ([]photos.Photo, error) {
	if f.err != nil {
		return nil, f.err
	}
	out := make([]photos.Photo, 0, len(uids))
	for i := len(uids) - 1; i >= 0; i-- {
		if p, ok := f.byUID[uids[i]]; ok {
			out = append(out, p)
		}
	}
	return out, nil
}

// passThrough is a no-op guard so handler behaviour is tested without auth.
func passThrough(next http.Handler) http.Handler { return next }

// newServer mounts an API backed by the given stores behind pass-through guards.
func newServer(subjects peopleapi.SubjectStore, ps peopleapi.PhotoStore) http.Handler {
	api := peopleapi.NewAPI(peopleapi.Config{
		Subjects:     subjects,
		Photos:       ps,
		RequireAuth:  passThrough,
		RequireWrite: passThrough,
	})
	r := chi.NewRouter()
	api.RegisterRoutes(r)
	return r
}

// do issues a request against the mounted API and returns the recorder.
func do(t *testing.T, h http.Handler, method, target, body string) *httptest.ResponseRecorder {
	t.Helper()
	var rdr *strings.Reader
	if body != "" {
		rdr = strings.NewReader(body)
	} else {
		rdr = strings.NewReader("")
	}
	req := httptest.NewRequestWithContext(context.Background(), method, target, rdr)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

// TestHandleList_ok returns the subjects with their counts.
func TestHandleList_ok(t *testing.T) {
	t.Parallel()
	subjects := &fakeSubjects{list: []people.SubjectCount{
		{Subject: people.Subject{UID: "su_a", Name: "Alice"}, MarkerCount: 3},
	}}
	rec := do(t, newServer(subjects, fakePhotos{}), http.MethodGet, "/subjects", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var got struct {
		Subjects []people.SubjectCount `json:"subjects"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(got.Subjects) != 1 || got.Subjects[0].MarkerCount != 3 {
		t.Errorf("body mismatch: %+v", got.Subjects)
	}
}

// TestHandleCreate_ok creates a subject and echoes the stored record.
func TestHandleCreate_ok(t *testing.T) {
	t.Parallel()
	subjects := &fakeSubjects{created: people.Subject{UID: "su_new", Name: "Bob", Slug: "bob"}}
	rec := do(t, newServer(subjects, fakePhotos{}), http.MethodPost, "/subjects",
		`{"name":"Bob","type":"person"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201", rec.Code)
	}
	if subjects.lastCreate.Name != "Bob" || subjects.lastCreate.Type != people.SubjectPerson {
		t.Errorf("create input mismatch: %+v", subjects.lastCreate)
	}
}

// TestHandleCreate_emptyName rejects a body with no name.
func TestHandleCreate_emptyName(t *testing.T) {
	t.Parallel()
	rec := do(t, newServer(&fakeSubjects{}, fakePhotos{}), http.MethodPost, "/subjects", `{"name":"  "}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

// TestHandleCreate_unknownField rejects an unexpected JSON field.
func TestHandleCreate_unknownField(t *testing.T) {
	t.Parallel()
	rec := do(t, newServer(&fakeSubjects{}, fakePhotos{}), http.MethodPost, "/subjects",
		`{"name":"Bob","bogus":1}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

// TestHandleGet_notFound maps the subject sentinel to 404.
func TestHandleGet_notFound(t *testing.T) {
	t.Parallel()
	subjects := &fakeSubjects{getErr: people.ErrSubjectNotFound}
	rec := do(t, newServer(subjects, fakePhotos{}), http.MethodGet, "/subjects/su_x", "")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}

// TestHandleUpdate_ok forwards the editable fields and returns the refreshed row.
func TestHandleUpdate_ok(t *testing.T) {
	t.Parallel()
	subjects := &fakeSubjects{updated: people.Subject{UID: "su_a", Name: "Alice II"}}
	rec := do(t, newServer(subjects, fakePhotos{}), http.MethodPatch, "/subjects/su_a",
		`{"name":"Alice II","type":"pet","favorite":true}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if subjects.lastUpdate.Type != people.SubjectPet || !subjects.lastUpdate.Favorite {
		t.Errorf("update input mismatch: %+v", subjects.lastUpdate)
	}
}

// TestHandleUpdate_invalidType maps the type sentinel to 400.
func TestHandleUpdate_invalidType(t *testing.T) {
	t.Parallel()
	subjects := &fakeSubjects{updateErr: people.ErrInvalidType}
	rec := do(t, newServer(subjects, fakePhotos{}), http.MethodPatch, "/subjects/su_a",
		`{"name":"Alice","type":"alien"}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

// TestHandleDelete_ok answers 204 and records the deleted UID.
func TestHandleDelete_ok(t *testing.T) {
	t.Parallel()
	subjects := &fakeSubjects{}
	rec := do(t, newServer(subjects, fakePhotos{}), http.MethodDelete, "/subjects/su_a", "")
	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", rec.Code)
	}
	if subjects.deletedUID != "su_a" {
		t.Errorf("deleted uid = %q, want su_a", subjects.deletedUID)
	}
}

// TestHandlePhotos_paginates returns the requested page in newest-first order
// with the next offset set.
func TestHandlePhotos_paginates(t *testing.T) {
	t.Parallel()
	subjects := &fakeSubjects{photoUIDs: []string{"p1", "p2", "p3"}}
	ps := fakePhotos{byUID: map[string]photos.Photo{
		"p1": {UID: "p1"}, "p2": {UID: "p2"}, "p3": {UID: "p3"},
	}}
	rec := do(t, newServer(subjects, ps), http.MethodGet, "/subjects/su_a/photos?limit=2&offset=0", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var got struct {
		Photos     []photos.Photo `json:"photos"`
		Total      int            `json:"total"`
		NextOffset *int           `json:"next_offset"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.Total != 3 {
		t.Errorf("total = %d, want 3", got.Total)
	}
	if len(got.Photos) != 2 || got.Photos[0].UID != "p1" || got.Photos[1].UID != "p2" {
		t.Errorf("page order mismatch: %+v", got.Photos)
	}
	if got.NextOffset == nil || *got.NextOffset != 2 {
		t.Errorf("next_offset = %v, want 2", got.NextOffset)
	}
}

// TestHandlePhotos_lastPage omits the next offset when the page is the last.
func TestHandlePhotos_lastPage(t *testing.T) {
	t.Parallel()
	subjects := &fakeSubjects{photoUIDs: []string{"p1"}}
	ps := fakePhotos{byUID: map[string]photos.Photo{"p1": {UID: "p1"}}}
	rec := do(t, newServer(subjects, ps), http.MethodGet, "/subjects/su_a/photos", "")
	var got struct {
		NextOffset *int `json:"next_offset"`
	}
	_ = json.NewDecoder(rec.Body).Decode(&got)
	if got.NextOffset != nil {
		t.Errorf("next_offset = %v, want nil", got.NextOffset)
	}
}

// TestHandlePhotos_badLimit answers 400 for a non-numeric limit.
func TestHandlePhotos_badLimit(t *testing.T) {
	t.Parallel()
	rec := do(t, newServer(&fakeSubjects{}, fakePhotos{}), http.MethodGet,
		"/subjects/su_a/photos?limit=abc", "")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}
