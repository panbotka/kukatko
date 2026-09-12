package familyapi_test

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
	"github.com/panbotka/kukatko/internal/family"
	"github.com/panbotka/kukatko/internal/familyapi"
	"github.com/panbotka/kukatko/internal/people"
)

// fakeStore is an in-memory familyapi.Store for handler tests: it records what
// the handlers asked of it and returns canned answers, so the tests are about
// routing, decoding, status mapping and the audit entry rather than about SQL.
type fakeStore struct {
	relations family.Relations
	tree      family.Tree
	added     family.AddResult
	fam       family.Family
	updated   family.Family

	relationsErr error
	treeErr      error
	addErr       error
	removeErr    error
	getErr       error
	updateErr    error

	lastDirection   family.Direction
	lastGenerations int
	lastAdd         family.AddRelation
	lastRemoved     [2]string
	lastUpdate      family.Update
	lastEntry       audit.Entry
}

// Relations returns the canned relations or error.
func (f *fakeStore) Relations(_ context.Context, _ string) (family.Relations, error) {
	return f.relations, f.relationsErr
}

// Tree records the walk parameters and returns the canned tree or error.
func (f *fakeStore) Tree(
	_ context.Context, _ string, direction family.Direction, generations int,
) (family.Tree, error) {
	f.lastDirection, f.lastGenerations = direction, generations
	return f.tree, f.treeErr
}

// AddRelationAudited records the request and audit entry and returns the canned
// result or error.
func (f *fakeStore) AddRelationAudited(
	_ context.Context, _ string, rel family.AddRelation, entry audit.Entry,
) (family.AddResult, error) {
	f.lastAdd, f.lastEntry = rel, entry
	return f.added, f.addErr
}

// RemoveRelationAudited records both uids and the audit entry and returns the
// canned error.
func (f *fakeStore) RemoveRelationAudited(
	_ context.Context, subjectUID, otherUID string, entry audit.Entry,
) error {
	f.lastRemoved, f.lastEntry = [2]string{subjectUID, otherUID}, entry
	return f.removeErr
}

// GetFamily returns the canned family or error.
func (f *fakeStore) GetFamily(_ context.Context, _ string) (family.Family, error) {
	return f.fam, f.getErr
}

// UpdateFamilyAudited records the update and audit entry and returns the canned
// family or error.
func (f *fakeStore) UpdateFamilyAudited(
	_ context.Context, _ string, upd family.Update, entry audit.Entry,
) (family.Family, error) {
	f.lastUpdate, f.lastEntry = upd, entry
	return f.updated, f.updateErr
}

// passThrough is a no-op guard so handler behaviour is tested without auth.
func passThrough(next http.Handler) http.Handler { return next }

// fakeExport counts the genealogy-export rewrites the handlers scheduled, and can
// refuse them.
type fakeExport struct {
	calls int
	err   error
}

// EnqueueFamilyExport counts the call and returns the canned error.
func (f *fakeExport) EnqueueFamilyExport(context.Context) error {
	f.calls++
	return f.err
}

// newServer mounts an API backed by store behind pass-through guards, with no
// export scheduler — most handler tests are not about it.
func newServer(store familyapi.Store) http.Handler {
	return newServerWithExport(store, nil)
}

// newServerWithExport mounts an API that schedules its export rewrites through
// export.
func newServerWithExport(store familyapi.Store, export familyapi.ExportEnqueuer) http.Handler {
	r := chi.NewRouter()
	familyapi.NewAPI(familyapi.Config{
		Store:        store,
		Export:       export,
		RequireAuth:  passThrough,
		RequireWrite: passThrough,
	}).RegisterRoutes(r)
	return r
}

// TestMutations_scheduleTheGenealogyExport pins the promise this API carries for
// the file on disk: every write schedules a rewrite of families.yaml, so losing
// the database does not lose the relation that was just recorded.
func TestMutations_scheduleTheGenealogyExport(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		method, target string
		body           string
	}{
		{
			name: "add a relation", method: http.MethodPost, target: "/subjects/su_a/relations",
			body: `{"role":"parent","subject_uid":"su_b"}`,
		},
		{name: "remove a relation", method: http.MethodDelete, target: "/subjects/su_a/relations/su_b"},
		{name: "edit a family", method: http.MethodPatch, target: "/families/fam1", body: `{"kind":"marriage"}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			export := &fakeExport{}
			rec := do(t, newServerWithExport(&fakeStore{}, export), tt.method, tt.target, tt.body)
			if rec.Code >= http.StatusBadRequest {
				t.Fatalf("status = %d, want a success: %s", rec.Code, rec.Body)
			}
			if export.calls != 1 {
				t.Errorf("scheduled %d export rewrite(s), want 1", export.calls)
			}
		})
	}
}

// TestMutations_doNotScheduleAfterAFailure verifies a refused mutation schedules
// nothing: there is nothing new to write, and a rewrite would only cost an
// object-store request for bytes that have not changed.
func TestMutations_doNotScheduleAfterAFailure(t *testing.T) {
	t.Parallel()

	export := &fakeExport{}
	store := &fakeStore{addErr: family.ErrSubjectNotFound}
	rec := do(t, newServerWithExport(store, export), http.MethodPost, "/subjects/su_a/relations",
		`{"role":"parent","subject_uid":"su_b"}`)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
	if export.calls != 0 {
		t.Errorf("scheduled %d export rewrite(s) after a failure, want none", export.calls)
	}
}

// TestMutations_surviveAFailedSchedule verifies a queue that cannot take the
// rewrite does not fail the user's edit: the relation is safely in Postgres, and
// the file is a second copy of it.
func TestMutations_surviveAFailedSchedule(t *testing.T) {
	t.Parallel()

	export := &fakeExport{err: errors.New("queue unavailable")}
	rec := do(t, newServerWithExport(&fakeStore{}, export), http.MethodDelete,
		"/subjects/su_a/relations/su_b", "")
	if rec.Code != http.StatusNoContent {
		t.Errorf("status = %d, want 204 despite the failed schedule", rec.Code)
	}
}

// do issues a request against the mounted API and returns the recorder.
func do(t *testing.T, h http.Handler, method, target, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequestWithContext(context.Background(), method, target, strings.NewReader(body))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

// TestHandleRelations_ok returns the four derived lists as the strip reads them.
func TestHandleRelations_ok(t *testing.T) {
	t.Parallel()
	store := &fakeStore{relations: family.Relations{
		Parents:  []family.Relative{{UID: "su_dad", Name: "Bohumil", PhotoCount: 4}},
		Children: []family.Relative{{UID: "su_kid", Name: "Marie"}},
	}}
	rec := do(t, newServer(store), http.MethodGet, "/subjects/su_a/relations", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var got family.Relations
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(got.Parents) != 1 || got.Parents[0].PhotoCount != 4 || len(got.Children) != 1 {
		t.Errorf("body mismatch: %+v", got)
	}
}

// TestHandleRelations_unknownSubject answers 404 rather than an empty tree, so a
// client can tell "nobody has filled this in" from "no such person".
func TestHandleRelations_unknownSubject(t *testing.T) {
	t.Parallel()
	store := &fakeStore{relationsErr: family.ErrSubjectNotFound}
	rec := do(t, newServer(store), http.MethodGet, "/subjects/su_ghost/relations", "")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}

// TestHandleTree_defaults walks down and asks for the whole bounded walk when
// neither parameter is given: "the Nečas family" means the descendants.
func TestHandleTree_defaults(t *testing.T) {
	t.Parallel()
	store := &fakeStore{}
	rec := do(t, newServer(store), http.MethodGet, "/subjects/su_a/tree", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if store.lastDirection != family.DirectionDescendants || store.lastGenerations != 0 {
		t.Errorf("walk = %q/%d, want descendants/0", store.lastDirection, store.lastGenerations)
	}
}

// TestHandleTree_params passes the requested direction and generation bound on.
func TestHandleTree_params(t *testing.T) {
	t.Parallel()
	store := &fakeStore{}
	rec := do(t, newServer(store), http.MethodGet,
		"/subjects/su_a/tree?direction=ancestors&generations=3", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if store.lastDirection != family.DirectionAncestors || store.lastGenerations != 3 {
		t.Errorf("walk = %q/%d, want ancestors/3", store.lastDirection, store.lastGenerations)
	}
}

// TestHandleTree_badParams rejects a walk nobody can perform instead of quietly
// answering a different question.
func TestHandleTree_badParams(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ name, query string }{
		{name: "direction", query: "?direction=sideways"},
		{name: "generations not a number", query: "?generations=many"},
		{name: "generations negative", query: "?generations=-2"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			rec := do(t, newServer(&fakeStore{}), http.MethodGet, "/subjects/su_a/tree"+tc.query, "")
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400", rec.Code)
			}
		})
	}
}

// TestHandleAddRelation_existingSubject records a relation to a person the
// library already knows and answers 201 with the family it landed in.
func TestHandleAddRelation_existingSubject(t *testing.T) {
	t.Parallel()
	store := &fakeStore{added: family.AddResult{
		Family:   family.Family{UID: "fm_1"},
		Relative: family.Relative{UID: "su_dad", Name: "Bohumil"},
	}}
	rec := do(t, newServer(store), http.MethodPost, "/subjects/su_a/relations",
		`{"role":"parent","subject_uid":"su_dad"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201: %s", rec.Code, rec.Body.String())
	}
	if store.lastAdd.Role != family.RoleParent || store.lastAdd.SubjectUID != "su_dad" {
		t.Errorf("request mismatch: %+v", store.lastAdd)
	}
	if store.lastEntry.Action != audit.ActionSubjectRelationAdd || store.lastEntry.TargetUID != "su_a" {
		t.Errorf("audit entry mismatch: %+v", store.lastEntry)
	}
	if store.lastEntry.Details == nil {
		t.Error("audit details are nil; the store must have a map to stamp into")
	}
}

// TestHandleAddRelation_newSubject passes the inline person through untouched —
// the atomic half is the store's, but the body has to arrive intact.
func TestHandleAddRelation_newSubject(t *testing.T) {
	t.Parallel()
	store := &fakeStore{added: family.AddResult{Created: true}}
	rec := do(t, newServer(store), http.MethodPost, "/subjects/su_a/relations",
		`{"role":"parent","new_subject":{"name":"  Marie Nečasová  ","birth_year":1921}}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201: %s", rec.Code, rec.Body.String())
	}
	if store.lastAdd.New == nil || store.lastAdd.New.Name != "Marie Nečasová" {
		t.Fatalf("new subject mismatch: %+v", store.lastAdd.New)
	}
	if store.lastAdd.New.BirthYear == nil || *store.lastAdd.New.BirthYear != 1921 {
		t.Errorf("birth year mismatch: %+v", store.lastAdd.New.BirthYear)
	}
}

// TestHandleAddRelation_badBody rejects what the request itself got wrong before
// the store is ever reached.
func TestHandleAddRelation_badBody(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ name, body string }{
		{name: "not json", body: `{`},
		{name: "no role", body: `{"subject_uid":"su_dad"}`},
		{name: "unknown field", body: `{"role":"parent","subject":"su_dad"}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			store := &fakeStore{}
			rec := do(t, newServer(store), http.MethodPost, "/subjects/su_a/relations", tc.body)
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400", rec.Code)
			}
			if store.lastAdd.Role != "" {
				t.Error("store was called with a body the handler should have refused")
			}
		})
	}
}

// TestHandleAddRelation_statusMapping maps each refusal the store can return to
// the status a client should see. The 409s are the point: a cycle is about the
// state of the tree, not about a malformed request, and a 500 would send somebody
// looking for a bug that is not there.
func TestHandleAddRelation_statusMapping(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		err  error
		want int
	}{
		{name: "cycle", err: family.ErrCycle, want: http.StatusConflict},
		{name: "already a child", err: family.ErrAlreadyChild, want: http.StatusConflict},
		{name: "family conflict", err: family.ErrFamilyConflict, want: http.StatusConflict},
		{name: "unknown subject", err: family.ErrSubjectNotFound, want: http.StatusNotFound},
		{name: "self relation", err: family.ErrSelfRelation, want: http.StatusBadRequest},
		{name: "invalid role", err: family.ErrInvalidRole, want: http.StatusBadRequest},
		{name: "ambiguous", err: family.ErrAmbiguousRelation, want: http.StatusBadRequest},
		{name: "invalid subject type", err: people.ErrInvalidType, want: http.StatusBadRequest},
		{name: "invalid life years", err: people.ErrInvalidLifeYears, want: http.StatusBadRequest},
		{name: "something else", err: errors.New("boom"), want: http.StatusInternalServerError},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			store := &fakeStore{addErr: tc.err}
			rec := do(t, newServer(store), http.MethodPost, "/subjects/su_a/relations",
				`{"role":"parent","subject_uid":"su_dad"}`)
			if rec.Code != tc.want {
				t.Fatalf("status = %d, want %d", rec.Code, tc.want)
			}
		})
	}
}

// TestHandleRemoveRelation_ok answers 204 and names both sides in the audit
// entry, since which relation was removed follows from the rows and not from the
// request.
func TestHandleRemoveRelation_ok(t *testing.T) {
	t.Parallel()
	store := &fakeStore{}
	rec := do(t, newServer(store), http.MethodDelete, "/subjects/su_a/relations/su_dad", "")
	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", rec.Code)
	}
	if store.lastRemoved != [2]string{"su_a", "su_dad"} {
		t.Errorf("removed = %v, want [su_a su_dad]", store.lastRemoved)
	}
	if store.lastEntry.Action != audit.ActionSubjectRelationRemove ||
		store.lastEntry.Details["other_uid"] != "su_dad" {
		t.Errorf("audit entry mismatch: %+v", store.lastEntry)
	}
}

// TestHandleRemoveRelation_notRelated answers 404, so a client is never told
// something was removed when the two were never related.
func TestHandleRemoveRelation_notRelated(t *testing.T) {
	t.Parallel()
	store := &fakeStore{removeErr: family.ErrRelationNotFound}
	rec := do(t, newServer(store), http.MethodDelete, "/subjects/su_a/relations/su_b", "")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}

// TestHandleUpdateFamily_ok rewrites the editable set and records the old→new
// diff of exactly the fields that changed.
func TestHandleUpdateFamily_ok(t *testing.T) {
	t.Parallel()
	from := 1948
	store := &fakeStore{
		fam:     family.Family{UID: "fm_1", Kind: family.KindPartnership, Note: "old"},
		updated: family.Family{UID: "fm_1", Kind: family.KindMarriage, FromYear: &from, Note: "old"},
	}
	rec := do(t, newServer(store), http.MethodPatch, "/families/fm_1",
		`{"kind":"marriage","from_year":1948,"note":"old"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	if store.lastUpdate.Kind != family.KindMarriage ||
		store.lastUpdate.FromYear == nil || *store.lastUpdate.FromYear != 1948 {
		t.Fatalf("update mismatch: %+v", store.lastUpdate)
	}
	changes, ok := store.lastEntry.Details[audit.ChangesKey].(map[string]any)
	if !ok {
		t.Fatalf("audit details carry no changes: %+v", store.lastEntry.Details)
	}
	if _, ok := changes["kind"]; !ok {
		t.Errorf("kind change not recorded: %+v", changes)
	}
	if _, ok := changes["note"]; ok {
		t.Errorf("unchanged note recorded as a change: %+v", changes)
	}
}

// TestHandleUpdateFamily_defaultKind treats an omitted kind as a partnership, the
// same default the store applies, so the recorded diff matches what is stored.
func TestHandleUpdateFamily_defaultKind(t *testing.T) {
	t.Parallel()
	store := &fakeStore{fam: family.Family{UID: "fm_1", Kind: family.KindPartnership}}
	rec := do(t, newServer(store), http.MethodPatch, "/families/fm_1", `{"note":"met in 1947"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	if store.lastUpdate.Kind != family.KindPartnership {
		t.Errorf("kind = %q, want partnership", store.lastUpdate.Kind)
	}
	if changes, ok := store.lastEntry.Details[audit.ChangesKey].(map[string]any); ok {
		if _, found := changes["kind"]; found {
			t.Errorf("unchanged kind recorded as a change: %+v", changes)
		}
	}
}

// TestHandleUpdateFamily_missing answers 404 before anything is written.
func TestHandleUpdateFamily_missing(t *testing.T) {
	t.Parallel()
	store := &fakeStore{getErr: family.ErrFamilyNotFound}
	rec := do(t, newServer(store), http.MethodPatch, "/families/fm_ghost", `{"kind":"marriage"}`)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
	if store.lastUpdate.Kind != "" {
		t.Error("store was updated despite a missing family")
	}
}

// TestHandleUpdateFamily_badYears surfaces the store's year refusal as a 400.
func TestHandleUpdateFamily_badYears(t *testing.T) {
	t.Parallel()
	store := &fakeStore{updateErr: family.ErrInvalidYears}
	rec := do(t, newServer(store), http.MethodPatch, "/families/fm_1",
		`{"kind":"marriage","from_year":1300}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}
