//go:build integration

package candidates_test

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/panbotka/kukatko/internal/audit"
	"github.com/panbotka/kukatko/internal/candidates"
	"github.com/panbotka/kukatko/internal/database/dbtest"
	"github.com/panbotka/kukatko/internal/feedback"
	"github.com/panbotka/kukatko/internal/mediaurl"
	"github.com/panbotka/kukatko/internal/people"
	"github.com/panbotka/kukatko/internal/photos"
	"github.com/panbotka/kukatko/internal/vectors"
)

// These tests run only under `make test-integration` against KUKATKO_TEST_DATABASE_URL.
// They share one database and truncate between cases, so they do not run in parallel.

// candHarness bundles the stores and service over a freshly truncated database.
type candHarness struct {
	faces    *vectors.Store
	people   *people.Store
	feedback *feedback.Store
	photos   *photos.Store
	svc      *candidates.Service
}

// newCandHarness returns a harness over a truncated integration database.
func newCandHarness(t *testing.T) candHarness {
	t.Helper()
	db := dbtest.New(t)
	dbtest.TruncateAll(t, db)
	faceStore := vectors.NewStore(db.Pool())
	peopleStore := people.NewStore(db.Pool())
	feedbackStore := feedback.NewStore(db.Pool())
	photoStore := photos.NewStore(db.Pool())
	return candHarness{
		faces:    faceStore,
		people:   peopleStore,
		feedback: feedbackStore,
		photos:   photoStore,
		svc: candidates.New(candidates.Config{
			Faces: faceStore, People: peopleStore, Feedback: feedbackStore, Photos: photoStore,
			Media:       mediaurl.NewBuilder(nil),
			MaxDistance: 0.5, SearchLimit: 1000, MinFacePx: 32, Concurrency: 4, MinFaceRel: 0.02,
		}),
	}
}

// vec builds a FaceDim vector from index→value overrides.
func vec(set map[int]float32) []float32 {
	v := make([]float32, vectors.FaceDim)
	for i, x := range set {
		v[i] = x
	}
	return v
}

// nearE0 is a 1000x800 face vector 0.2 cosine-distance from e0 (well within 0.5).
func nearE0() []float32 { return vec(map[int]float32{0: 0.8, 1: 0.6}) }

// reviewableBox is a normalised face box large enough to clear both size floors on
// an 800px-tall photo (0.3*1000 = 300px wide).
var reviewableBox = [4]float64{0.3, 0.3, 0.3, 0.3}

// makePhoto inserts a reviewable 1000x800 photo and returns its uid.
func (h candHarness) makePhoto(t *testing.T, hash string) string {
	t.Helper()
	created, err := h.photos.Create(context.Background(), photos.Photo{
		FileHash: hash, FilePath: "2024/01/" + hash + ".jpg", FileName: hash + ".jpg",
		FileWidth: 1000, FileHeight: 800, FileOrientation: 1,
	})
	if err != nil {
		t.Fatalf("creating photo %s: %v", hash, err)
	}
	return created.UID
}

// saveFace writes one face onto a fresh photo with the given assignment/marker and
// returns the photo uid.
func (h candHarness) saveFace(t *testing.T, hash string, face vectors.Face) string {
	t.Helper()
	photoUID := h.makePhoto(t, hash)
	face.PhotoWidth, face.PhotoHeight, face.Orientation = 1000, 800, 1
	if face.BBox == ([4]float64{}) {
		face.BBox = reviewableBox
	}
	if err := h.faces.SaveFaces(context.Background(), photoUID, []vectors.Face{face}); err != nil {
		t.Fatalf("SaveFaces(%s): %v", hash, err)
	}
	return photoUID
}

// exemplarFor plants an assigned face (subject_uid set) so the subject has an
// exemplar to search from.
func (h candHarness) exemplarFor(t *testing.T, hash, subjectUID string, v []float32) {
	t.Helper()
	h.saveFace(t, hash, vectors.Face{FaceIndex: 0, Vector: v, DetScore: 0.95, SubjectUID: &subjectUID})
}

// TestFind_plantedUnassignedFaceFoundDB checks a planted unassigned lookalike is
// surfaced and classified create_marker, and an identical face assigned to a
// different person is never returned.
func TestFind_plantedUnassignedFaceFoundDB(t *testing.T) {
	h := newCandHarness(t)
	ctx := t.Context()
	alice, err := h.people.CreateSubject(ctx, people.Subject{Name: "Alice"})
	if err != nil {
		t.Fatalf("CreateSubject: %v", err)
	}
	bob, err := h.people.CreateSubject(ctx, people.Subject{Name: "Bob"})
	if err != nil {
		t.Fatalf("CreateSubject: %v", err)
	}
	h.exemplarFor(t, "alice-src", alice.UID, vec(map[int]float32{0: 1}))
	wanted := h.saveFace(t, "unassigned-lookalike", vectors.Face{FaceIndex: 0, Vector: nearE0(), DetScore: 0.9})
	// Same embedding, but already assigned to Bob: must never be returned for Alice.
	h.saveFace(t, "bob-lookalike", vectors.Face{FaceIndex: 0, Vector: nearE0(), DetScore: 0.9, SubjectUID: &bob.UID})

	res, err := h.svc.Find(ctx, alice.UID, candidates.Request{})
	if err != nil {
		t.Fatalf("Find: %v", err)
	}
	if len(res.Candidates) != 1 {
		t.Fatalf("candidates = %d, want 1: %+v", len(res.Candidates), res.Candidates)
	}
	got := res.Candidates[0]
	if got.Photo.UID != wanted || got.Action != candidates.ActionCreateMarker {
		t.Errorf("candidate = %s/%s, want %s/create_marker", got.Photo.UID, got.Action, wanted)
	}
	if got.BBox.Pixel[2] == 0 || got.BBox.Relative != reviewableBox {
		t.Errorf("bbox not projected: %+v", got.BBox)
	}
	if res.SourceFaceCount != 1 || res.SourcePhotoCount != 1 || res.MinMatchCount != 1 {
		t.Errorf("summary faces/photos/min = %d/%d/%d, want 1/1/1",
			res.SourceFaceCount, res.SourcePhotoCount, res.MinMatchCount)
	}
}

// TestFind_rejectedFaceNotReturnedDB checks a face rejected for the subject is
// excluded from the candidates.
func TestFind_rejectedFaceNotReturnedDB(t *testing.T) {
	h := newCandHarness(t)
	ctx := t.Context()
	alice, err := h.people.CreateSubject(ctx, people.Subject{Name: "Alice"})
	if err != nil {
		t.Fatalf("CreateSubject: %v", err)
	}
	h.exemplarFor(t, "alice-src", alice.UID, vec(map[int]float32{0: 1}))
	// "keep" sits on e0 (nearest the exemplar, far from the rejection); "rejected"
	// is a distinct near-e0 face that the user rejects.
	keep := h.saveFace(t, "keep", vectors.Face{FaceIndex: 0, Vector: vec(map[int]float32{0: 1}), DetScore: 0.9})
	rejected := h.saveFace(t, "rejected", vectors.Face{FaceIndex: 0, Vector: nearE0(), DetScore: 0.9})

	entry := audit.Entry{Action: audit.ActionFaceReject, TargetType: "subjects", TargetUID: alice.UID}
	key := feedback.FaceRejectionKey{PhotoUID: rejected, FaceIndex: 0, SubjectUID: alice.UID}
	if err := h.feedback.RejectFace(ctx, key, entry); err != nil {
		t.Fatalf("RejectFace: %v", err)
	}

	res, err := h.svc.Find(ctx, alice.UID, candidates.Request{})
	if err != nil {
		t.Fatalf("Find: %v", err)
	}
	for _, c := range res.Candidates {
		if c.Photo.UID == rejected {
			t.Fatalf("rejected face %s was returned: %+v", rejected, res.Candidates)
		}
	}
	if len(res.Candidates) != 1 || res.Candidates[0].Photo.UID != keep {
		t.Errorf("want only the kept face %s, got %+v", keep, res.Candidates)
	}
}

// TestFind_negativeExemplarDroppedDB checks a candidate closer to a rejected face
// than to any accepted face is dropped by the margin rule.
func TestFind_negativeExemplarDroppedDB(t *testing.T) {
	h := newCandHarness(t)
	ctx := t.Context()
	alice, err := h.people.CreateSubject(ctx, people.Subject{Name: "Alice"})
	if err != nil {
		t.Fatalf("CreateSubject: %v", err)
	}
	// Accepted evidence sits on e0.
	h.exemplarFor(t, "alice-src", alice.UID, vec(map[int]float32{0: 1}))
	// A rejected face sits on e1 (unassigned; rejected for Alice).
	rejected := h.saveFace(t, "rej", vectors.Face{FaceIndex: 0, Vector: vec(map[int]float32{1: 1}), DetScore: 0.9})
	entry := audit.Entry{Action: audit.ActionFaceReject, TargetType: "subjects", TargetUID: alice.UID}
	if err := h.feedback.RejectFace(ctx,
		feedback.FaceRejectionKey{PhotoUID: rejected, FaceIndex: 0, SubjectUID: alice.UID}, entry); err != nil {
		t.Fatalf("RejectFace: %v", err)
	}
	// "near" (~55° off e0) is within the search threshold of the e0 exemplar, yet
	// closer to the rejected e1 face than to e0 → dropped. "far" (~37° off e0, well
	// away from e1) survives.
	near := h.saveFace(t, "near", vectors.Face{FaceIndex: 0, Vector: vec(map[int]float32{0: 0.574, 1: 0.819}), DetScore: 0.9})
	far := h.saveFace(t, "far", vectors.Face{FaceIndex: 0, Vector: nearE0(), DetScore: 0.9})

	res, err := h.svc.Find(ctx, alice.UID, candidates.Request{})
	if err != nil {
		t.Fatalf("Find: %v", err)
	}
	seen := map[string]bool{}
	for _, c := range res.Candidates {
		seen[c.Photo.UID] = true
	}
	if seen[near] {
		t.Errorf("negative-exemplar face %s was returned: %+v", near, res.Candidates)
	}
	if !seen[far] {
		t.Errorf("distant face %s was dropped, want kept: %+v", far, res.Candidates)
	}
}

// TestFind_voteRuleFiltersDB checks that with nine exemplars (min_match_count 2) a
// candidate seen by a single exemplar is filtered while one seen by two survives.
func TestFind_voteRuleFiltersDB(t *testing.T) {
	h := newCandHarness(t)
	ctx := t.Context()
	subj, err := h.people.CreateSubject(ctx, people.Subject{Name: "Multi"})
	if err != nil {
		t.Fatalf("CreateSubject: %v", err)
	}
	for i := 0; i < 9; i++ {
		h.exemplarFor(t, fmt.Sprintf("src-%d", i), subj.UID, vec(map[int]float32{i: 1}))
	}
	// "one" is close only to e0. "two" is close to both e0 and e1.
	one := h.saveFace(t, "one", vectors.Face{FaceIndex: 0, Vector: vec(map[int]float32{0: 0.95, 20: 0.31}), DetScore: 0.9})
	two := h.saveFace(t, "two", vectors.Face{FaceIndex: 0, Vector: vec(map[int]float32{0: 0.7071, 1: 0.7071}), DetScore: 0.9})

	res, err := h.svc.Find(ctx, subj.UID, candidates.Request{})
	if err != nil {
		t.Fatalf("Find: %v", err)
	}
	if res.MinMatchCount != 2 {
		t.Fatalf("MinMatchCount = %d, want 2", res.MinMatchCount)
	}
	seen := map[string]int{}
	for _, c := range res.Candidates {
		seen[c.Photo.UID] = c.MatchCount
	}
	if _, ok := seen[one]; ok {
		t.Errorf("single-vote face %s survived the vote rule: %+v", one, res.Candidates)
	}
	if seen[two] != 2 {
		t.Errorf("two-vote face %s missing or miscounted (%d): %+v", two, seen[two], res.Candidates)
	}
}

// TestFind_alreadyDoneClassifiedDB checks a candidate whose marker already points at
// the subject (a stale faces cache) is classified already_done.
func TestFind_alreadyDoneClassifiedDB(t *testing.T) {
	h := newCandHarness(t)
	ctx := t.Context()
	alice, err := h.people.CreateSubject(ctx, people.Subject{Name: "Alice"})
	if err != nil {
		t.Fatalf("CreateSubject: %v", err)
	}
	h.exemplarFor(t, "alice-src", alice.UID, vec(map[int]float32{0: 1}))

	stalePhoto := h.makePhoto(t, "stale")
	marker, err := h.people.CreateMarker(ctx, people.Marker{
		PhotoUID: stalePhoto, SubjectUID: &alice.UID, Type: people.MarkerFace,
		X: 0.3, Y: 0.3, W: 0.3, H: 0.3, Reviewed: true,
	})
	if err != nil {
		t.Fatalf("CreateMarker: %v", err)
	}
	// A face on that photo carries the marker but a NULL subject cache (the stale
	// state), so the unassigned search still returns it.
	if err := h.faces.SaveFaces(ctx, stalePhoto, []vectors.Face{{
		FaceIndex: 0, Vector: nearE0(), DetScore: 0.9, BBox: reviewableBox,
		MarkerUID: &marker.UID, PhotoWidth: 1000, PhotoHeight: 800, Orientation: 1,
	}}); err != nil {
		t.Fatalf("SaveFaces(stale): %v", err)
	}

	res, err := h.svc.Find(ctx, alice.UID, candidates.Request{})
	if err != nil {
		t.Fatalf("Find: %v", err)
	}
	if len(res.Candidates) != 1 || res.Candidates[0].Action != candidates.ActionAlreadyDone {
		t.Fatalf("candidates = %+v, want one already_done", res.Candidates)
	}
	if res.Counts.AlreadyDone != 1 {
		t.Errorf("counts = %+v, want AlreadyDone 1", res.Counts)
	}
}

// TestFind_zeroFacesEmptyDB checks a subject with no faces returns an empty,
// non-error result with a clear reason.
func TestFind_zeroFacesEmptyDB(t *testing.T) {
	h := newCandHarness(t)
	ctx := t.Context()
	subj, err := h.people.CreateSubject(ctx, people.Subject{Name: "Nobody"})
	if err != nil {
		t.Fatalf("CreateSubject: %v", err)
	}
	res, err := h.svc.Find(ctx, subj.UID, candidates.Request{})
	if err != nil {
		t.Fatalf("Find: %v", err)
	}
	if res.Reason != candidates.ReasonNoFaces || len(res.Candidates) != 0 {
		t.Fatalf("result = %+v, want empty ReasonNoFaces", res)
	}
}

// TestFind_subjectNotFoundDB checks the people sentinel is surfaced from the DB.
func TestFind_subjectNotFoundDB(t *testing.T) {
	h := newCandHarness(t)
	_, err := h.svc.Find(t.Context(), "su_missing", candidates.Request{})
	if !errors.Is(err, people.ErrSubjectNotFound) {
		t.Fatalf("Find = %v, want ErrSubjectNotFound", err)
	}
}
