package peopleapi_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/panbotka/kukatko/internal/audit"
	"github.com/panbotka/kukatko/internal/people"
	"github.com/panbotka/kukatko/internal/peopleapi"
	"github.com/panbotka/kukatko/internal/photos"
)

// fakeSubjects is an in-memory SubjectStore for handler tests. The various err
// fields force a specific error from the matching method.
type fakeSubjects struct {
	list      []people.SubjectCount
	subject   people.Subject
	byUID     map[string]people.Subject
	photoUIDs []string
	created   people.Subject
	updated   people.Subject
	merged    people.MergeResult
	listErr   error
	getErr    error
	createErr error
	updateErr error
	deleteErr error
	mergeErr  error
	photosErr error

	lastUpdate  people.SubjectUpdate
	lastCreate  people.Subject
	deletedUID  string
	mergeSource string
	mergeKeeper string
	lastEntry   audit.Entry
}

// ListSubjects returns the canned subject list or error.
func (f *fakeSubjects) ListSubjects(_ context.Context) ([]people.SubjectCount, error) {
	return f.list, f.listErr
}

// GetSubjectByUID returns the subject registered under uid when the fake was
// given a map (the merge handler looks two different subjects up), otherwise the
// single canned subject, or the canned error.
func (f *fakeSubjects) GetSubjectByUID(_ context.Context, uid string) (people.Subject, error) {
	if f.getErr != nil {
		return people.Subject{}, f.getErr
	}
	if f.byUID == nil {
		return f.subject, nil
	}
	subj, ok := f.byUID[uid]
	if !ok {
		return people.Subject{}, people.ErrSubjectNotFound
	}
	return subj, nil
}

// CreateSubjectAudited records the input and audit entry and returns the canned
// created subject or error.
func (f *fakeSubjects) CreateSubjectAudited(
	_ context.Context, subj people.Subject, entry audit.Entry,
) (people.Subject, error) {
	f.lastCreate = subj
	f.lastEntry = entry
	return f.created, f.createErr
}

// UpdateSubjectAudited records the update and audit entry and returns the canned
// updated subject or error.
func (f *fakeSubjects) UpdateSubjectAudited(
	_ context.Context, _ string, upd people.SubjectUpdate, entry audit.Entry,
) (people.Subject, error) {
	f.lastUpdate = upd
	f.lastEntry = entry
	return f.updated, f.updateErr
}

// DeleteSubjectAudited records the deleted UID and audit entry and returns the
// canned error.
func (f *fakeSubjects) DeleteSubjectAudited(_ context.Context, uid string, entry audit.Entry) error {
	f.deletedUID = uid
	f.lastEntry = entry
	return f.deleteErr
}

// MergeSubjectsAudited records both uids and the audit entry and returns the
// canned merge result or error.
func (f *fakeSubjects) MergeSubjectsAudited(
	_ context.Context, sourceUID, keeperUID string, entry audit.Entry,
) (people.MergeResult, error) {
	f.mergeSource = sourceUID
	f.mergeKeeper = keeperUID
	f.lastEntry = entry
	return f.merged, f.mergeErr
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

// TestHandleList_ok returns the subjects with both of their counts. They are set
// apart here on purpose: a client picks between them by name, so the response
// must carry each under its own key rather than one number twice.
func TestHandleList_ok(t *testing.T) {
	t.Parallel()
	subjects := &fakeSubjects{list: []people.SubjectCount{
		{Subject: people.Subject{UID: "su_a", Name: "Alice"}, MarkerCount: 3, PhotoCount: 2},
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
	if len(got.Subjects) != 1 ||
		got.Subjects[0].MarkerCount != 3 || got.Subjects[0].PhotoCount != 2 {
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
	if subjects.lastEntry.Action != audit.ActionSubjectCreate || subjects.lastEntry.Details["name"] != "Bob" {
		t.Errorf("create audit entry = %+v, want subject.create with name Bob", subjects.lastEntry)
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

// TestHandleCreate_nameIdentifyingNobody rejects a name made of punctuation or
// symbols alone. Such a name has no slug of its own, so the subject would be
// stored under the shared fallback slug and read as unnamed everywhere — the same
// catch-all shape an importer once created by accident.
func TestHandleCreate_nameIdentifyingNobody(t *testing.T) {
	t.Parallel()

	for _, body := range []string{`{"name":"!!!"}`, `{"name":" - "}`, `{"name":"···"}`} {
		subjects := &fakeSubjects{}
		rec := do(t, newServer(subjects, fakePhotos{}), http.MethodPost, "/subjects", body)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("POST %s status = %d, want 400", body, rec.Code)
		}
		if subjects.lastCreate.Name != "" {
			t.Errorf("POST %s reached the store with %+v", body, subjects.lastCreate)
		}
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
	if subjects.lastEntry.Action != audit.ActionSubjectUpdate || subjects.lastEntry.TargetUID != "su_a" {
		t.Errorf("update audit entry = %+v, want subject.update targeting su_a", subjects.lastEntry)
	}
}

// TestHandleUpdate_recordsChanges loads the existing subject and records old→new
// for the fields the edit changed (name, type, favorite) under details.changes,
// omitting the unchanged ones.
func TestHandleUpdate_recordsChanges(t *testing.T) {
	t.Parallel()
	subjects := &fakeSubjects{
		subject: people.Subject{
			UID: "su_a", Name: "Alice", Type: people.SubjectPerson, Favorite: false, Notes: "same",
		},
		updated: people.Subject{UID: "su_a", Name: "Alice II"},
	}
	rec := do(t, newServer(subjects, fakePhotos{}), http.MethodPatch, "/subjects/su_a",
		`{"name":"Alice II","type":"pet","favorite":true,"notes":"same"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	changes := changeMap(t, subjects.lastEntry)
	assertChange(t, changes, "name", "Alice", "Alice II")
	assertChange(t, changes, "type", "person", "pet")
	assertChange(t, changes, "favorite", false, true)
	if _, ok := changes["notes"]; ok {
		t.Errorf("unchanged notes present in changes: %v", changes)
	}
}

// TestHandleUpdate_noChangesOmitsChangesKey verifies an edit that alters nothing
// records no details.changes key at all.
func TestHandleUpdate_noChangesOmitsChangesKey(t *testing.T) {
	t.Parallel()
	subjects := &fakeSubjects{
		subject: people.Subject{UID: "su_a", Name: "Alice", Type: people.SubjectPerson},
		updated: people.Subject{UID: "su_a", Name: "Alice"},
	}
	rec := do(t, newServer(subjects, fakePhotos{}), http.MethodPatch, "/subjects/su_a",
		`{"name":"Alice","type":"person"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if _, ok := subjects.lastEntry.Details["changes"]; ok {
		t.Errorf("no-op subject edit recorded a changes key: %v", subjects.lastEntry.Details)
	}
}

// TestHandleUpdate_notFound maps a missing subject to 404 before mutating (the
// handler now loads the subject first to capture its old values).
func TestHandleUpdate_notFound(t *testing.T) {
	t.Parallel()
	subjects := &fakeSubjects{getErr: people.ErrSubjectNotFound}
	rec := do(t, newServer(subjects, fakePhotos{}), http.MethodPatch, "/subjects/su_x",
		`{"name":"X","type":"person"}`)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}

// changeMap extracts the details.changes map recorded on an audit entry, failing
// the test when it is absent or not a map. The entry is inspected before any JSON
// round-trip, so each value is still an audit.Change rather than a decoded map.
func changeMap(t *testing.T, entry audit.Entry) map[string]any {
	t.Helper()
	raw, ok := entry.Details["changes"].(map[string]any)
	if !ok {
		t.Fatalf("audit details has no changes map: %v", entry.Details)
	}
	return raw
}

// assertChange fails unless the changes map records field's transition from old
// to want under the {old,new} convention.
func assertChange(t *testing.T, changes map[string]any, field string, old, want any) {
	t.Helper()
	change, ok := changes[field].(audit.Change)
	if !ok {
		t.Fatalf("changes[%q] type = %T, want audit.Change", field, changes[field])
	}
	if change.Old != old || change.New != want {
		t.Errorf("changes[%q] = %+v, want {old:%v new:%v}", field, change, old, want)
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

// TestHandleUpdate_lifeYears carries the two optional years through to the store
// and records their transition in the audit trail. A `null` is a real value here
// — it clears the year — so it has to reach the update as a nil pointer rather
// than be dropped as an absent field.
func TestHandleUpdate_lifeYears(t *testing.T) {
	t.Parallel()
	subjects := &fakeSubjects{
		subject: people.Subject{
			UID: "su_a", Name: "Jarmila", Type: people.SubjectPerson, DeathYear: new(1998),
		},
		updated: people.Subject{UID: "su_a", Name: "Jarmila", BirthYear: new(1923)},
	}
	rec := do(t, newServer(subjects, fakePhotos{}), http.MethodPatch, "/subjects/su_a",
		`{"name":"Jarmila","type":"person","birth_year":1923,"death_year":null}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if subjects.lastUpdate.BirthYear == nil || *subjects.lastUpdate.BirthYear != 1923 {
		t.Errorf("update birth year = %v, want 1923", subjects.lastUpdate.BirthYear)
	}
	if subjects.lastUpdate.DeathYear != nil {
		t.Errorf("update death year = %v, want nil (the null clears it)", subjects.lastUpdate.DeathYear)
	}
	changes := changeMap(t, subjects.lastEntry)
	if _, ok := changes["birth_year"]; !ok {
		t.Errorf("birth_year missing from the recorded changes: %v", changes)
	}
	if _, ok := changes["death_year"]; !ok {
		t.Errorf("death_year missing from the recorded changes: %v", changes)
	}
}

// TestHandleUpdate_invalidLifeYears maps the life-year sentinel to 400, so a
// death before a birth (or a year in the future) is a client error rather than a
// 500 the user can do nothing about.
func TestHandleUpdate_invalidLifeYears(t *testing.T) {
	t.Parallel()
	subjects := &fakeSubjects{updateErr: people.ErrInvalidLifeYears}
	rec := do(t, newServer(subjects, fakePhotos{}), http.MethodPatch, "/subjects/su_a",
		`{"name":"Jarmila","type":"person","birth_year":1998,"death_year":1923}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

// TestHandleCreate_invalidLifeYears maps the same sentinel to 400 on create.
func TestHandleCreate_invalidLifeYears(t *testing.T) {
	t.Parallel()
	subjects := &fakeSubjects{createErr: people.ErrInvalidLifeYears}
	rec := do(t, newServer(subjects, fakePhotos{}), http.MethodPost, "/subjects",
		`{"name":"Jarmila","type":"person","birth_year":1799}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

// TestHandleDelete_ok answers 204 and records the deleted UID.
func TestHandleDelete_ok(t *testing.T) {
	t.Parallel()
	subjects := &fakeSubjects{subject: people.Subject{UID: "su_a", Name: "Alice", Type: people.SubjectPerson}}
	rec := do(t, newServer(subjects, fakePhotos{}), http.MethodDelete, "/subjects/su_a", "")
	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", rec.Code)
	}
	if subjects.deletedUID != "su_a" {
		t.Errorf("deleted uid = %q, want su_a", subjects.deletedUID)
	}
	if subjects.lastEntry.Action != audit.ActionSubjectDelete || subjects.lastEntry.Details["name"] != "Alice" {
		t.Errorf("delete audit entry = %+v, want subject.delete recording name Alice", subjects.lastEntry)
	}
}

// mergePair returns a fake holding the two subjects a merge names, so the
// handler's two lookups both resolve.
func mergePair() *fakeSubjects {
	return &fakeSubjects{byUID: map[string]people.Subject{
		"su_a": {UID: "su_a", Name: "Alice", Type: people.SubjectPerson},
		"su_b": {UID: "su_b", Name: "Alena", Type: people.SubjectPerson},
	}}
}

// TestHandleMerge_ok merges the path subject into the keeper, echoes the store's
// counts and records both names on the audit entry — the source is deleted by the
// merge, so the trail is the only place its name survives.
func TestHandleMerge_ok(t *testing.T) {
	t.Parallel()
	subjects := mergePair()
	subjects.merged = people.MergeResult{
		KeeperUID: "su_b", SourceUID: "su_a", MarkersMoved: 7, SharedPhotos: 1,
	}
	rec := do(t, newServer(subjects, fakePhotos{}), http.MethodPost, "/subjects/su_a/merge",
		`{"keeper_uid":"su_b"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if subjects.mergeSource != "su_a" || subjects.mergeKeeper != "su_b" {
		t.Errorf("merged %q into %q, want su_a into su_b", subjects.mergeSource, subjects.mergeKeeper)
	}
	var got people.MergeResult
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.MarkersMoved != 7 || got.SharedPhotos != 1 {
		t.Errorf("body mismatch: %+v", got)
	}
	entry := subjects.lastEntry
	if entry.Action != audit.ActionSubjectMerge || entry.TargetUID != "su_b" {
		t.Errorf("merge audit entry = %+v, want subject.merge targeting su_b", entry)
	}
	if entry.Details["source_name"] != "Alice" || entry.Details["keeper_name"] != "Alena" {
		t.Errorf("merge audit details = %+v, want both names recorded", entry.Details)
	}
}

// TestHandleMerge_intoSelf rejects merging a subject into itself before the store
// is reached — the request describes nothing and would delete the very subject it
// claims to keep.
func TestHandleMerge_intoSelf(t *testing.T) {
	t.Parallel()
	subjects := mergePair()
	rec := do(t, newServer(subjects, fakePhotos{}), http.MethodPost, "/subjects/su_a/merge",
		`{"keeper_uid":"su_a"}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
	if subjects.mergeKeeper != "" {
		t.Errorf("store was called with keeper %q, want no call", subjects.mergeKeeper)
	}
}

// TestHandleMerge_missingKeeper answers 400 for a body naming no keeper and 404
// when the named keeper does not exist.
func TestHandleMerge_missingKeeper(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name string
		body string
		want int
	}{
		{name: "empty", body: `{"keeper_uid":"  "}`, want: http.StatusBadRequest},
		{name: "unknown", body: `{"keeper_uid":"su_zz"}`, want: http.StatusNotFound},
		{name: "unknown field", body: `{"keeper":"su_b"}`, want: http.StatusBadRequest},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			subjects := mergePair()
			rec := do(t, newServer(subjects, fakePhotos{}), http.MethodPost, "/subjects/su_a/merge", tc.body)
			if rec.Code != tc.want {
				t.Fatalf("status = %d, want %d", rec.Code, tc.want)
			}
			if subjects.mergeKeeper != "" {
				t.Errorf("store was called with keeper %q, want no call", subjects.mergeKeeper)
			}
		})
	}
}

// TestHandleMerge_storeError maps a store failure onto its status: a missing
// subject is 404, anything else 500.
func TestHandleMerge_storeError(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name string
		err  error
		want int
	}{
		{name: "not found", err: people.ErrSubjectNotFound, want: http.StatusNotFound},
		{name: "failed", err: errors.New("boom"), want: http.StatusInternalServerError},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			subjects := mergePair()
			subjects.mergeErr = tc.err
			rec := do(t, newServer(subjects, fakePhotos{}), http.MethodPost, "/subjects/su_a/merge",
				`{"keeper_uid":"su_b"}`)
			if rec.Code != tc.want {
				t.Fatalf("status = %d, want %d", rec.Code, tc.want)
			}
		})
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
