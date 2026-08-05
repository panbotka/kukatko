//go:build integration

package dupmarkersapi_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/panbotka/kukatko/internal/audit"
	"github.com/panbotka/kukatko/internal/database"
	"github.com/panbotka/kukatko/internal/database/dbtest"
	"github.com/panbotka/kukatko/internal/dupmarkers"
	"github.com/panbotka/kukatko/internal/dupmarkersapi"
	"github.com/panbotka/kukatko/internal/facematch"
	"github.com/panbotka/kukatko/internal/feedback"
	"github.com/panbotka/kukatko/internal/feedbackapi"
	"github.com/panbotka/kukatko/internal/people"
	"github.com/panbotka/kukatko/internal/photos"
	"github.com/panbotka/kukatko/internal/vectors"
)

// These tests run only under `make test-integration` against the database named
// by KUKATKO_TEST_DATABASE_URL. They share one database and truncate between
// cases, so they intentionally do not run in parallel.

// intHarness wires the real stores and both route groups over a freshly
// truncated integration database, so a decision made through HTTP is checked
// against what actually landed in Postgres.
type intHarness struct {
	db       *database.DB
	people   *people.Store
	photos   *photos.Store
	feedback *feedback.Store
	audit    *audit.Store
	router   chi.Router
}

// newIntHarness returns a harness over a truncated integration database, with the
// repeated-marker routes and the feedback routes mounted on one router behind
// pass-through guards.
func newIntHarness(t *testing.T) intHarness {
	t.Helper()
	db := dbtest.New(t)
	dbtest.TruncateAll(t, db)

	peopleStore := people.NewStore(db.Pool())
	photoStore := photos.NewStore(db.Pool())
	feedbackStore := feedback.NewStore(db.Pool())
	matchSvc := facematch.New(facematch.Config{
		Photos: photoStore,
		People: peopleStore,
		Faces:  vectors.NewStore(db.Pool()),
	})
	svc := dupmarkers.New(dupmarkers.Config{
		Markers:    dupmarkers.NewStore(db.Pool()),
		Dismissals: feedbackStore,
	})

	r := chi.NewRouter()
	dupmarkersapi.NewAPI(dupmarkersapi.Config{
		Service:      svc,
		Markers:      peopleStore,
		Assigner:     matchSvc,
		RequireAuth:  passthrough,
		RequireWrite: passthrough,
	}).RegisterRoutes(r)
	feedbackapi.NewAPI(feedbackapi.Config{
		Store:        feedbackStore,
		RequireWrite: passthrough,
	}).RegisterRoutes(r)

	return intHarness{
		db:       db,
		people:   peopleStore,
		photos:   photoStore,
		feedback: feedbackStore,
		audit:    audit.NewStore(db.Pool()),
		router:   r,
	}
}

// makePhoto inserts a minimal photo and returns its uid.
func (h intHarness) makePhoto(t *testing.T, hash string) string {
	t.Helper()
	created, err := h.photos.Create(t.Context(), photos.Photo{
		FileHash:   hash,
		FilePath:   "2024/01/" + hash + ".jpg",
		FileName:   hash + ".jpg",
		FileWidth:  4000,
		FileHeight: 3000,
	})
	if err != nil {
		t.Fatalf("creating photo %s: %v", hash, err)
	}
	return created.UID
}

// makeSubject inserts a named subject and returns its uid.
func (h intHarness) makeSubject(t *testing.T, name string) string {
	t.Helper()
	subj, err := h.people.CreateSubject(t.Context(), people.Subject{Name: name})
	if err != nil {
		t.Fatalf("creating subject %s: %v", name, err)
	}
	return subj.UID
}

// makeMarker inserts a face marker for subjectUID on photoUID at the given x and
// returns its uid.
func (h intHarness) makeMarker(t *testing.T, photoUID, subjectUID string, x float64) string {
	t.Helper()
	m, err := h.people.CreateMarker(t.Context(), people.Marker{
		PhotoUID:   photoUID,
		SubjectUID: &subjectUID,
		Type:       people.MarkerFace,
		X:          x, Y: 0.2, W: 0.1, H: 0.1,
		Reviewed: true,
	})
	if err != nil {
		t.Fatalf("creating marker: %v", err)
	}
	return m.UID
}

// request runs one HTTP call against the harness router.
func (h intHarness) request(t *testing.T, method, target, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequestWithContext(t.Context(), method, target, strings.NewReader(body))
	rec := httptest.NewRecorder()
	h.router.ServeHTTP(rec, req)
	return rec
}

// listGroups calls GET /duplicate-markers and decodes the result.
func (h intHarness) listGroups(t *testing.T) dupmarkers.Result {
	t.Helper()
	rec := h.request(t, http.MethodGet, "/duplicate-markers", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /duplicate-markers status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var res dupmarkers.Result
	if err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil {
		t.Fatalf("decoding listing: %v", err)
	}
	return res
}

// auditActions returns the audit actions recorded so far, newest first.
func (h intHarness) auditActions(t *testing.T) []string {
	t.Helper()
	records, err := h.audit.List(t.Context(), audit.Filter{Limit: 100})
	if err != nil {
		t.Fatalf("listing audit records: %v", err)
	}
	actions := make([]string, 0, len(records))
	for _, rec := range records {
		actions = append(actions, rec.Action)
	}
	return actions
}

// hasAction reports whether the audit trail carries an entry with the action and
// target uid.
func (h intHarness) hasAction(t *testing.T, action, targetUID string) bool {
	t.Helper()
	records, err := h.audit.List(t.Context(), audit.Filter{Action: action, Limit: 100})
	if err != nil {
		t.Fatalf("listing audit records: %v", err)
	}
	for _, rec := range records {
		if rec.TargetUID != nil && *rec.TargetUID == targetUID {
			return true
		}
	}
	return false
}

// seedGroup creates one photo with `count` markers of the same person and returns
// the photo uid, the subject uid and the marker uids left to right.
func (h intHarness) seedGroup(t *testing.T, hash, name string, count int) (string, string, []string) {
	t.Helper()
	photoUID := h.makePhoto(t, hash)
	subjectUID := h.makeSubject(t, name)
	markerUIDs := make([]string, 0, count)
	for i := range count {
		markerUIDs = append(markerUIDs, h.makeMarker(t, photoUID, subjectUID, 0.1*float64(i+1)))
	}
	return photoUID, subjectUID, markerUIDs
}

func TestIntegration_listSurfacesOnlyRepeatedNamedFaceMarkers(t *testing.T) {
	h := newIntHarness(t)
	ctx := context.Background()

	photoUID, subjectUID, markerUIDs := h.seedGroup(t, "hash-a", "Marie", 3)
	// A second person marked once on the same photo is the normal case.
	other := h.makeSubject(t, "Jan")
	h.makeMarker(t, photoUID, other, 0.9)
	// The nameless catch-all subject must never surface, however many boxes it has.
	nameless, err := h.people.CreateSubject(ctx, people.Subject{Name: ""})
	if err != nil {
		t.Fatalf("creating nameless subject: %v", err)
	}
	h.makeMarker(t, photoUID, nameless.UID, 0.5)
	h.makeMarker(t, photoUID, nameless.UID, 0.6)

	res := h.listGroups(t)

	if res.Total != 1 || len(res.Groups) != 1 {
		t.Fatalf("total = %d, groups = %d, want 1 and 1", res.Total, len(res.Groups))
	}
	group := res.Groups[0]
	if group.PhotoUID != photoUID || group.SubjectUID != subjectUID {
		t.Errorf("group = (%s, %s), want (%s, %s)",
			group.PhotoUID, group.SubjectUID, photoUID, subjectUID)
	}
	if len(group.Markers) != len(markerUIDs) {
		t.Fatalf("group has %d markers, want %d", len(group.Markers), len(markerUIDs))
	}
	if group.Width != 4000 || group.Height != 3000 {
		t.Errorf("frame = %dx%d, want 4000x3000", group.Width, group.Height)
	}
}

func TestIntegration_keepDetachesTheOthersWithoutDeletingThem(t *testing.T) {
	h := newIntHarness(t)
	ctx := context.Background()

	photoUID, subjectUID, markerUIDs := h.seedGroup(t, "hash-b", "Marie", 3)
	keep := markerUIDs[1]

	rec := h.request(t, http.MethodPost, "/duplicate-markers/keep",
		`{"photo_uid":"`+photoUID+`","subject_uid":"`+subjectUID+`","keep_marker_uid":"`+keep+`"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("keep status = %d, body = %s", rec.Code, rec.Body.String())
	}

	for _, uid := range markerUIDs {
		marker, err := h.people.GetMarkerByUID(ctx, uid)
		if err != nil {
			// The whole point of "detach, don't delete": the region survives, so a
			// curator can hand it to whoever it really belongs to.
			t.Fatalf("marker %s no longer exists: %v", uid, err)
		}
		if uid == keep {
			if marker.SubjectUID == nil || *marker.SubjectUID != subjectUID {
				t.Errorf("kept marker lost its subject: %+v", marker.SubjectUID)
			}
			continue
		}
		if marker.SubjectUID != nil {
			t.Errorf("marker %s still names %s, want NULL", uid, *marker.SubjectUID)
		}
		if marker.Reviewed {
			t.Errorf("marker %s is still reviewed, want the flag cleared with the subject", uid)
		}
		if marker.Invalid {
			t.Errorf("marker %s was flagged invalid, want only the subject cleared", uid)
		}
		if !h.hasAction(t, audit.ActionFaceUnassign, uid) {
			t.Errorf("no %s audit entry for marker %s (actions: %v)",
				audit.ActionFaceUnassign, uid, h.auditActions(t))
		}
	}

	if res := h.listGroups(t); res.Total != 0 {
		t.Errorf("group still listed after keep: %+v", res.Groups)
	}
}

func TestIntegration_invalidFlagsOneMarkerAndShrinksTheGroup(t *testing.T) {
	h := newIntHarness(t)
	ctx := context.Background()

	_, subjectUID, markerUIDs := h.seedGroup(t, "hash-c", "Marie", 3)

	rec := h.request(t, http.MethodPost, "/duplicate-markers/invalid",
		`{"marker_uid":"`+markerUIDs[2]+`"}`)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("invalid status = %d, body = %s", rec.Code, rec.Body.String())
	}

	flagged, err := h.people.GetMarkerByUID(ctx, markerUIDs[2])
	if err != nil {
		t.Fatalf("flagged marker no longer exists: %v", err)
	}
	if !flagged.Invalid {
		t.Error("marker is not flagged invalid")
	}
	if flagged.SubjectUID == nil || *flagged.SubjectUID != subjectUID {
		t.Error("flagging invalid also cleared the subject, want the flag alone")
	}
	if !h.hasAction(t, audit.ActionMarkerInvalidate, markerUIDs[2]) {
		t.Errorf("no %s audit entry (actions: %v)", audit.ActionMarkerInvalidate, h.auditActions(t))
	}

	// Three minus one is two, which is still a finding — with only the two valid
	// markers left in it.
	res := h.listGroups(t)
	if res.Total != 1 {
		t.Fatalf("total = %d, want 1", res.Total)
	}
	if len(res.Groups[0].Markers) != 2 {
		t.Fatalf("group has %d markers, want 2", len(res.Groups[0].Markers))
	}
	for _, m := range res.Groups[0].Markers {
		if m.UID == markerUIDs[2] {
			t.Error("the invalid marker is still listed in the group")
		}
	}

	// Flagging one more takes the group to a single marker, i.e. the normal case.
	if rec := h.request(t, http.MethodPost, "/duplicate-markers/invalid",
		`{"marker_uid":"`+markerUIDs[1]+`"}`); rec.Code != http.StatusNoContent {
		t.Fatalf("second invalid status = %d", rec.Code)
	}
	if res := h.listGroups(t); res.Total != 0 {
		t.Errorf("group still listed at one marker: %+v", res.Groups)
	}
}

func TestIntegration_dismissedGroupDoesNotComeBack(t *testing.T) {
	h := newIntHarness(t)
	ctx := context.Background()

	photoUID, subjectUID, markerUIDs := h.seedGroup(t, "hash-d", "Marie", 2)
	body := `{"photo_uid":"` + photoUID + `","subject_uid":"` + subjectUID + `"}`

	if rec := h.request(t, http.MethodPost, "/feedback/duplicate-marker-dismissals",
		body); rec.Code != http.StatusNoContent {
		t.Fatalf("dismiss status = %d, body = %s", rec.Code, rec.Body.String())
	}

	if res := h.listGroups(t); res.Total != 0 {
		t.Errorf("dismissed group is still listed: %+v", res.Groups)
	}
	// Dismissing is an opinion: both markers must be untouched.
	for _, uid := range markerUIDs {
		marker, err := h.people.GetMarkerByUID(ctx, uid)
		if err != nil {
			t.Fatalf("marker %s no longer exists: %v", uid, err)
		}
		if marker.SubjectUID == nil || *marker.SubjectUID != subjectUID {
			t.Errorf("marker %s lost its subject, want the dismissal to change nothing", uid)
		}
		if marker.Invalid {
			t.Errorf("marker %s was flagged invalid by a dismissal", uid)
		}
	}
	if !h.hasAction(t, audit.ActionDuplicateMarkerDismiss, subjectUID) {
		t.Errorf("no %s audit entry (actions: %v)",
			audit.ActionDuplicateMarkerDismiss, h.auditActions(t))
	}

	// The decision is idempotent, and reversible: taking it back brings the group
	// straight back into the queue.
	if rec := h.request(t, http.MethodPost, "/feedback/duplicate-marker-dismissals",
		body); rec.Code != http.StatusNoContent {
		t.Fatalf("second dismiss status = %d", rec.Code)
	}
	if rec := h.request(t, http.MethodDelete, "/feedback/duplicate-marker-dismissals",
		body); rec.Code != http.StatusNoContent {
		t.Fatalf("undismiss status = %d", rec.Code)
	}
	if res := h.listGroups(t); res.Total != 1 {
		t.Errorf("group did not come back after the undo: total = %d", res.Total)
	}
}

func TestIntegration_dismissalIsScopedToOnePhoto(t *testing.T) {
	h := newIntHarness(t)

	photoA := h.makePhoto(t, "hash-e1")
	photoB := h.makePhoto(t, "hash-e2")
	subjectUID := h.makeSubject(t, "Marie")
	for _, photoUID := range []string{photoA, photoB} {
		h.makeMarker(t, photoUID, subjectUID, 0.1)
		h.makeMarker(t, photoUID, subjectUID, 0.2)
	}

	if rec := h.request(t, http.MethodPost, "/feedback/duplicate-marker-dismissals",
		`{"photo_uid":"`+photoA+`","subject_uid":"`+subjectUID+`"}`); rec.Code != http.StatusNoContent {
		t.Fatalf("dismiss status = %d", rec.Code)
	}

	res := h.listGroups(t)
	if res.Total != 1 {
		t.Fatalf("total = %d, want 1 (only the other photo's group)", res.Total)
	}
	if res.Groups[0].PhotoUID != photoB {
		t.Errorf("surviving group is on %s, want %s", res.Groups[0].PhotoUID, photoB)
	}
}

func TestIntegration_archivedPhotosAreNotOffered(t *testing.T) {
	h := newIntHarness(t)

	photoUID, _, _ := h.seedGroup(t, "hash-f", "Marie", 2)
	if _, err := h.photos.Archive(t.Context(), photoUID); err != nil {
		t.Fatalf("archiving photo: %v", err)
	}

	if res := h.listGroups(t); res.Total != 0 {
		t.Errorf("a trashed photo's group is offered for review: %+v", res.Groups)
	}
}

func TestIntegration_keepRefusesAMarkerFromAnotherGroup(t *testing.T) {
	h := newIntHarness(t)
	ctx := context.Background()

	photoUID, subjectUID, markerUIDs := h.seedGroup(t, "hash-g", "Marie", 2)
	other := h.makeSubject(t, "Jan")
	foreign := h.makeMarker(t, photoUID, other, 0.9)

	rec := h.request(t, http.MethodPost, "/duplicate-markers/keep",
		`{"photo_uid":"`+photoUID+`","subject_uid":"`+subjectUID+`","keep_marker_uid":"`+foreign+`"}`)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("keep status = %d, want 404", rec.Code)
	}

	for _, uid := range markerUIDs {
		marker, err := h.people.GetMarkerByUID(ctx, uid)
		if err != nil {
			t.Fatalf("marker %s no longer exists: %v", uid, err)
		}
		if marker.SubjectUID == nil {
			t.Errorf("marker %s was detached by a rejected request", uid)
		}
	}
}
