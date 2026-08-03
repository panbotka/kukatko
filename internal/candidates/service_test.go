package candidates

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"
	"testing"

	"github.com/panbotka/kukatko/internal/feedback"
	"github.com/panbotka/kukatko/internal/mediaurl"
	"github.com/panbotka/kukatko/internal/people"
	"github.com/panbotka/kukatko/internal/photos"
	"github.com/panbotka/kukatko/internal/vectors"
)

// oneHot returns a FaceDim vector that is 1 at index i and 0 elsewhere — a distinct
// exemplar direction so the fake search can identify which exemplar is querying.
func oneHot(i int) []float32 {
	v := make([]float32, vectors.FaceDim)
	v[i] = 1
	return v
}

// hotIndex returns the index of the largest element, recovering the exemplar a
// oneHot vector represents.
func hotIndex(vec []float32) int {
	best, at := float32(-1), 0
	for i, x := range vec {
		if x > best {
			best, at = x, i
		}
	}
	return at
}

// fakeFaces scripts the vectors.Store behaviour: which candidates each exemplar's
// kNN returns, and the embeddings behind a set of keys.
type fakeFaces struct {
	bySubject   []vectors.Face
	perExemplar map[int][]vectors.FaceCandidate
	byKeys      map[vectors.FaceKey]vectors.Face
	// searchLimit records the neighbour cap the kNN was asked for, so a test can
	// assert how the per-exemplar limit was sized. It is atomic because the exemplar
	// searches run concurrently.
	searchLimit atomic.Int64
}

func (f *fakeFaces) SampleFacesBySubject(
	_ context.Context, _ string, limit int,
) (vectors.SubjectFaces, error) {
	out := vectors.SubjectFaces{Faces: f.bySubject, Total: len(f.bySubject)}
	photos := map[string]struct{}{}
	for _, face := range f.bySubject {
		photos[face.PhotoUID] = struct{}{}
	}
	out.Photos = len(photos)
	if limit > 0 && len(out.Faces) > limit {
		out.Faces = out.Faces[:limit]
	}
	return out, nil
}

func (f *fakeFaces) FindSimilarUnassignedFaceCandidates(
	_ context.Context, vec []float32, limit int, maxDistance float64, exclude []vectors.FaceKey,
) ([]vectors.FaceCandidate, error) {
	f.searchLimit.Store(int64(limit))
	excluded := make(map[vectors.FaceKey]struct{}, len(exclude))
	for _, key := range exclude {
		excluded[key] = struct{}{}
	}
	var out []vectors.FaceCandidate
	for _, cand := range f.perExemplar[hotIndex(vec)] {
		if cand.Distance > maxDistance {
			continue
		}
		if _, drop := excluded[vectors.FaceKey{PhotoUID: cand.PhotoUID, FaceIndex: cand.FaceIndex}]; drop {
			continue
		}
		out = append(out, cand)
	}
	return out, nil
}

func (f *fakeFaces) FacesByKeys(_ context.Context, keys []vectors.FaceKey) ([]vectors.Face, error) {
	var out []vectors.Face
	for _, key := range keys {
		if face, ok := f.byKeys[key]; ok {
			out = append(out, face)
		}
	}
	return out, nil
}

// fakePeople scripts the people.Store behaviour.
type fakePeople struct {
	subjects map[string]people.Subject
	markers  map[string]people.Marker
	marked   map[string][]string
}

func (f *fakePeople) GetSubjectByUID(_ context.Context, uid string) (people.Subject, error) {
	if subject, ok := f.subjects[uid]; ok {
		return subject, nil
	}
	return people.Subject{}, people.ErrSubjectNotFound
}

func (f *fakePeople) GetMarkerByUID(_ context.Context, uid string) (people.Marker, error) {
	if marker, ok := f.markers[uid]; ok {
		return marker, nil
	}
	return people.Marker{}, people.ErrMarkerNotFound
}

func (f *fakePeople) ListPhotoUIDsBySubject(_ context.Context, uid string) ([]string, error) {
	return f.marked[uid], nil
}

// fakeFeedback scripts the feedback.Store behaviour.
type fakeFeedback struct {
	rejections map[string][]feedback.FaceRef
}

func (f *fakeFeedback) FaceRejectionsForSubject(_ context.Context, uid string) ([]feedback.FaceRef, error) {
	return f.rejections[uid], nil
}

// fakePhotos scripts the photos.Store behaviour and records how many uids it was
// asked to hydrate, so a test can assert the memory bounds cut before hydration.
type fakePhotos struct {
	byUID     map[string]photos.Photo
	requested int
}

func (f *fakePhotos) ListByUIDs(_ context.Context, uids []string) ([]photos.Photo, error) {
	f.requested += len(uids)
	var out []photos.Photo
	for _, uid := range uids {
		if photo, ok := f.byUID[uid]; ok {
			out = append(out, photo)
		}
	}
	return out, nil
}

// harness wires a Service over the four fakes with the given photo records.
type harness struct {
	faces    *fakeFaces
	people   *fakePeople
	feedback *fakeFeedback
	photos   *fakePhotos
	svc      *Service
}

// newHarness builds a Service over empty fakes; tests populate the fakes before
// calling Find.
func newHarness(t *testing.T) *harness {
	t.Helper()
	h := &harness{
		faces:    &fakeFaces{perExemplar: map[int][]vectors.FaceCandidate{}, byKeys: map[vectors.FaceKey]vectors.Face{}},
		people:   &fakePeople{subjects: map[string]people.Subject{}, markers: map[string]people.Marker{}, marked: map[string][]string{}},
		feedback: &fakeFeedback{rejections: map[string][]feedback.FaceRef{}},
		photos:   &fakePhotos{byUID: map[string]photos.Photo{}},
	}
	h.svc = New(Config{
		Faces: h.faces, People: h.people, Feedback: h.feedback, Photos: h.photos,
		Media:       mediaurl.NewBuilder(nil),
		MaxDistance: 0.5, SearchLimit: 1000, MinFacePx: 32, Concurrency: 4, MinFaceRel: 0.02,
	})
	return h
}

// addSubject registers a subject and returns its uid.
func (h *harness) addSubject(uid string) string {
	h.people.subjects[uid] = people.Subject{UID: uid, Name: uid}
	return uid
}

// addPhoto registers a reviewable 1000x800 upright photo.
func (h *harness) addPhoto(uid string) {
	h.photos.byUID[uid] = photos.Photo{
		UID: uid, FileHash: uid + "hash", FilePath: "2024/01/" + uid + ".jpg",
		FileWidth: 1000, FileHeight: 800, FileOrientation: 1,
	}
}

// bigBox is a comfortably reviewable normalised face box.
var bigBox = [4]float64{0.3, 0.3, 0.3, 0.3}

// TestFind_subjectNotFound checks the people sentinel is surfaced.
func TestFind_subjectNotFound(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	_, err := h.svc.Find(context.Background(), "su_missing", Request{})
	if !errors.Is(err, people.ErrSubjectNotFound) {
		t.Fatalf("Find = %v, want ErrSubjectNotFound", err)
	}
}

// TestFind_noFaces checks a subject with nothing tagged returns an empty, non-error
// result carrying ReasonNoFaces.
func TestFind_noFaces(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	subj := h.addSubject("su_empty")
	res, err := h.svc.Find(context.Background(), subj, Request{})
	if err != nil {
		t.Fatalf("Find: %v", err)
	}
	if res.Reason != ReasonNoFaces || len(res.Candidates) != 0 || res.MinMatchCount != 0 {
		t.Fatalf("result = %+v, want ReasonNoFaces empty", res)
	}
	if res.Candidates == nil {
		t.Error("Candidates is nil, want an empty slice for a clean JSON []")
	}
}

// TestFind_noEmbeddings checks a subject tagged on photos with no embedded faces
// reports the count and ReasonNoEmbeddings rather than silently returning nothing.
func TestFind_noEmbeddings(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	subj := h.addSubject("su_noemb")
	h.people.marked[subj] = []string{"p1", "p2"}
	res, err := h.svc.Find(context.Background(), subj, Request{})
	if err != nil {
		t.Fatalf("Find: %v", err)
	}
	if res.Reason != ReasonNoEmbeddings || res.FacesWithoutEmbedding != 2 {
		t.Fatalf("result = %+v, want ReasonNoEmbeddings with count 2", res)
	}
}

// TestFind_plantedCandidateFound checks a single-exemplar subject surfaces an
// unnamed neighbour and classifies it create_marker.
func TestFind_plantedCandidateFound(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	subj := h.addSubject("su_a")
	h.faces.bySubject = []vectors.Face{{PhotoUID: "src", FaceIndex: 0, Vector: oneHot(0), DetScore: 0.9}}
	h.people.marked[subj] = []string{"src"}
	h.addPhoto("cand")
	h.faces.perExemplar[0] = []vectors.FaceCandidate{
		{PhotoUID: "cand", FaceIndex: 0, Distance: 0.2, BBox: bigBox},
	}

	res, err := h.svc.Find(context.Background(), subj, Request{})
	if err != nil {
		t.Fatalf("Find: %v", err)
	}
	if len(res.Candidates) != 1 {
		t.Fatalf("candidates = %d, want 1: %+v", len(res.Candidates), res)
	}
	got := res.Candidates[0]
	if got.Photo.UID != "cand" || got.FaceIndex != 0 || got.Action != ActionCreateMarker {
		t.Errorf("candidate = %+v, want photo cand#0 create_marker", got)
	}
	if got.MatchCount != 1 || got.Distance != 0.2 {
		t.Errorf("candidate vote/distance = %d/%v, want 1/0.2", got.MatchCount, got.Distance)
	}
	if res.SourceFaceCount != 1 || res.SourcePhotoCount != 1 || res.MinMatchCount != 1 {
		t.Errorf("summary = faces %d photos %d min %d, want 1/1/1",
			res.SourceFaceCount, res.SourcePhotoCount, res.MinMatchCount)
	}
	if got.Photo.ThumbURL == "" {
		t.Error("candidate photo has no thumb_url; media was not stamped")
	}
}

// TestFind_voteRuleFilters checks that with min_match_count 2 a candidate seen by
// only one exemplar is dropped while one seen by two survives.
func TestFind_voteRuleFilters(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	subj := h.addSubject("su_votes")
	// Nine exemplars on nine photos → min_match_count 2 at the default threshold.
	for i := range 9 {
		h.faces.bySubject = append(h.faces.bySubject, vectors.Face{
			PhotoUID: "src" + string(rune('a'+i)), FaceIndex: 0, Vector: oneHot(i), DetScore: 0.9,
		})
	}
	h.addPhoto("one")
	h.addPhoto("two")
	// "one" is returned by a single exemplar; "two" by two exemplars.
	h.faces.perExemplar[0] = []vectors.FaceCandidate{
		{PhotoUID: "one", FaceIndex: 0, Distance: 0.2, BBox: bigBox},
		{PhotoUID: "two", FaceIndex: 0, Distance: 0.3, BBox: bigBox},
	}
	h.faces.perExemplar[1] = []vectors.FaceCandidate{
		{PhotoUID: "two", FaceIndex: 0, Distance: 0.25, BBox: bigBox},
	}

	res, err := h.svc.Find(context.Background(), subj, Request{})
	if err != nil {
		t.Fatalf("Find: %v", err)
	}
	if res.MinMatchCount != 2 {
		t.Fatalf("MinMatchCount = %d, want 2", res.MinMatchCount)
	}
	if len(res.Candidates) != 1 || res.Candidates[0].Photo.UID != "two" {
		t.Fatalf("candidates = %+v, want only 'two' (seen by two exemplars)", res.Candidates)
	}
	if res.Candidates[0].MatchCount != 2 || res.Candidates[0].Distance != 0.25 {
		t.Errorf("survivor vote/distance = %d/%v, want 2/0.25 (nearest)",
			res.Candidates[0].MatchCount, res.Candidates[0].Distance)
	}
}

// TestFind_rejectionExcluded checks a rejected face is filtered out of the search.
func TestFind_rejectionExcluded(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	subj := h.addSubject("su_rej")
	h.faces.bySubject = []vectors.Face{{PhotoUID: "src", FaceIndex: 0, Vector: oneHot(0), DetScore: 0.9}}
	h.addPhoto("keep")
	h.addPhoto("rejected")
	h.faces.perExemplar[0] = []vectors.FaceCandidate{
		{PhotoUID: "keep", FaceIndex: 0, Distance: 0.2, BBox: bigBox},
		{PhotoUID: "rejected", FaceIndex: 0, Distance: 0.1, BBox: bigBox},
	}
	h.feedback.rejections[subj] = []feedback.FaceRef{{PhotoUID: "rejected", FaceIndex: 0}}
	// Embeddings for the negative-exemplar pass: far apart, so nothing extra drops.
	h.faces.byKeys[vectors.FaceKey{PhotoUID: "keep"}] = vectors.Face{PhotoUID: "keep", Vector: oneHot(3)}
	h.faces.byKeys[vectors.FaceKey{PhotoUID: "rejected"}] = vectors.Face{PhotoUID: "rejected", Vector: oneHot(7)}

	res, err := h.svc.Find(context.Background(), subj, Request{})
	if err != nil {
		t.Fatalf("Find: %v", err)
	}
	if len(res.Candidates) != 1 || res.Candidates[0].Photo.UID != "keep" {
		t.Fatalf("candidates = %+v, want only 'keep' (rejected excluded)", res.Candidates)
	}
}

// TestFind_negativeExemplarDrop checks a candidate closer to a rejected face than to
// any accepted face is dropped, while a distant candidate survives.
func TestFind_negativeExemplarDrop(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	subj := h.addSubject("su_neg")
	h.faces.bySubject = []vectors.Face{{PhotoUID: "src", FaceIndex: 0, Vector: oneHot(0), DetScore: 0.9}}
	h.addPhoto("near")
	h.addPhoto("far")
	h.faces.perExemplar[0] = []vectors.FaceCandidate{
		{PhotoUID: "near", FaceIndex: 0, Distance: 0.4, BBox: bigBox},
		{PhotoUID: "far", FaceIndex: 0, Distance: 0.4, BBox: bigBox},
	}
	// A rejection exists (so the negative pass runs) but on a face not itself a
	// candidate, so the coarse exclusion does not remove "near".
	h.feedback.rejections[subj] = []feedback.FaceRef{{PhotoUID: "rej", FaceIndex: 0}}
	rejVec := make([]float32, vectors.FaceDim)
	rejVec[1], rejVec[2] = 0.9, 0.1 // very close to oneHot(1), far from oneHot(0)/oneHot(3)
	h.faces.byKeys[vectors.FaceKey{PhotoUID: "rej"}] = vectors.Face{PhotoUID: "rej", Vector: rejVec}
	h.faces.byKeys[vectors.FaceKey{PhotoUID: "near"}] = vectors.Face{PhotoUID: "near", Vector: oneHot(1)}
	h.faces.byKeys[vectors.FaceKey{PhotoUID: "far"}] = vectors.Face{PhotoUID: "far", Vector: oneHot(3)}

	res, err := h.svc.Find(context.Background(), subj, Request{})
	if err != nil {
		t.Fatalf("Find: %v", err)
	}
	if len(res.Candidates) != 1 || res.Candidates[0].Photo.UID != "far" {
		t.Fatalf("candidates = %+v, want only 'far' ('near' trips the negative rule)", res.Candidates)
	}
}

// TestFind_actionClassification checks the three actions are assigned correctly,
// including the rare already_done stale-cache case.
func TestFind_actionClassification(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	subj := h.addSubject("su_act")
	h.faces.bySubject = []vectors.Face{{PhotoUID: "src", FaceIndex: 0, Vector: oneHot(0), DetScore: 0.9}}
	for _, uid := range []string{"none", "other", "mine"} {
		h.addPhoto(uid)
	}
	h.people.markers["mk_other"] = people.Marker{UID: "mk_other", SubjectUID: nil}
	h.people.markers["mk_mine"] = people.Marker{UID: "mk_mine", SubjectUID: new(subj)}
	h.faces.perExemplar[0] = []vectors.FaceCandidate{
		{PhotoUID: "none", FaceIndex: 0, Distance: 0.1, BBox: bigBox},
		{PhotoUID: "other", FaceIndex: 0, Distance: 0.2, BBox: bigBox, MarkerUID: new("mk_other")},
		{PhotoUID: "mine", FaceIndex: 0, Distance: 0.3, BBox: bigBox, MarkerUID: new("mk_mine")},
	}

	res, err := h.svc.Find(context.Background(), subj, Request{})
	if err != nil {
		t.Fatalf("Find: %v", err)
	}
	byPhoto := map[string]Action{}
	markerByPhoto := map[string]string{}
	for _, c := range res.Candidates {
		byPhoto[c.Photo.UID] = c.Action
		markerByPhoto[c.Photo.UID] = c.MarkerUID
	}
	want := map[string]Action{"none": ActionCreateMarker, "other": ActionAssignPerson, "mine": ActionAlreadyDone}
	for uid, wantAction := range want {
		if byPhoto[uid] != wantAction {
			t.Errorf("action for %s = %q, want %q", uid, byPhoto[uid], wantAction)
		}
	}
	// The overlapping marker is surfaced so the UI can route the assign call; an
	// unmarked face (create_marker) carries no marker.
	wantMarker := map[string]string{"none": "", "other": "mk_other", "mine": "mk_mine"}
	for uid, want := range wantMarker {
		if markerByPhoto[uid] != want {
			t.Errorf("marker_uid for %s = %q, want %q", uid, markerByPhoto[uid], want)
		}
	}
	if res.Counts != (Counts{CreateMarker: 1, AssignPerson: 1, AlreadyDone: 1}) {
		t.Errorf("counts = %+v, want 1/1/1", res.Counts)
	}
}

// TestFind_limitTruncates checks the result honours the request limit, keeping the
// nearest.
func TestFind_limitTruncates(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	subj := h.addSubject("su_lim")
	h.faces.bySubject = []vectors.Face{{PhotoUID: "src", FaceIndex: 0, Vector: oneHot(0), DetScore: 0.9}}
	h.addPhoto("near")
	h.addPhoto("far")
	h.faces.perExemplar[0] = []vectors.FaceCandidate{
		{PhotoUID: "far", FaceIndex: 0, Distance: 0.4, BBox: bigBox},
		{PhotoUID: "near", FaceIndex: 0, Distance: 0.1, BBox: bigBox},
	}
	res, err := h.svc.Find(context.Background(), subj, Request{Limit: 1})
	if err != nil {
		t.Fatalf("Find: %v", err)
	}
	if len(res.Candidates) != 1 || res.Candidates[0].Photo.UID != "near" {
		t.Fatalf("candidates = %+v, want only nearest 'near'", res.Candidates)
	}
}

// TestFind_minDistanceKeepsTheUncertainMiddle checks the request's distance floor
// drops the confident matches before they are hydrated, leaving the band the
// review game asks about.
func TestFind_minDistanceKeepsTheUncertainMiddle(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	subj := h.addSubject("su_band")
	h.faces.bySubject = []vectors.Face{{PhotoUID: "src", FaceIndex: 0, Vector: oneHot(0), DetScore: 0.9}}
	for _, uid := range []string{"sure", "unsure"} {
		h.addPhoto(uid)
	}
	h.faces.perExemplar[0] = []vectors.FaceCandidate{
		{PhotoUID: "sure", FaceIndex: 0, Distance: 0.05, BBox: bigBox},
		{PhotoUID: "unsure", FaceIndex: 0, Distance: 0.4, BBox: bigBox},
	}

	res, err := h.svc.Find(context.Background(), subj, Request{MinDistance: 0.25})
	if err != nil {
		t.Fatalf("Find: %v", err)
	}
	if len(res.Candidates) != 1 || res.Candidates[0].Photo.UID != "unsure" {
		t.Fatalf("candidates = %+v, want only 'unsure' — a floor of 0.25 excludes the confident match",
			res.Candidates)
	}
}

// TestFind_capsExemplars checks a subject with more faces than the cap searches
// from a sample of them, reports the true totals rather than the sample's, and
// says so.
func TestFind_capsExemplars(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	h.svc = New(Config{
		Faces: h.faces, People: h.people, Feedback: h.feedback, Photos: h.photos,
		Media: mediaurl.NewBuilder(nil), MaxDistance: 0.5, MinFacePx: 32,
		Concurrency: 4, MinFaceRel: 0.02, MaxExemplars: 3,
	})
	subj := h.addSubject("su_many")
	for i := range 10 {
		h.faces.bySubject = append(h.faces.bySubject, vectors.Face{
			PhotoUID: "src" + string(rune('a'+i)), FaceIndex: 0, Vector: oneHot(i), DetScore: 0.9,
		})
	}
	h.people.marked[subj] = []string{"srca", "srcb", "srcc", "srcd", "unembedded"}

	res, err := h.svc.Find(context.Background(), subj, Request{})
	if err != nil {
		t.Fatalf("Find: %v", err)
	}
	if res.ExemplarsUsed != 3 || !res.SourceCapped {
		t.Errorf("ExemplarsUsed/SourceCapped = %d/%v, want 3/true", res.ExemplarsUsed, res.SourceCapped)
	}
	// The summary must describe the subject, not the sample: ten faces over ten
	// photos, one marked photo with no embedded face at all.
	if res.SourceFaceCount != 10 || res.SourcePhotoCount != 10 || res.FacesWithoutEmbedding != 0 {
		t.Errorf("summary = faces %d photos %d without %d, want 10/10/0",
			res.SourceFaceCount, res.SourcePhotoCount, res.FacesWithoutEmbedding)
	}
}

// TestFind_capsHydratedCandidates checks the hydration ceiling keeps the nearest
// candidates and reports the cut instead of applying it silently.
func TestFind_capsHydratedCandidates(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	h.svc = New(Config{
		Faces: h.faces, People: h.people, Feedback: h.feedback, Photos: h.photos,
		Media: mediaurl.NewBuilder(nil), MaxDistance: 0.5, MinFacePx: 32,
		Concurrency: 4, MinFaceRel: 0.02, MaxCandidates: 2,
	})
	subj := h.addSubject("su_wide")
	h.faces.bySubject = []vectors.Face{{PhotoUID: "src", FaceIndex: 0, Vector: oneHot(0), DetScore: 0.9}}
	for i := range 5 {
		uid := "cand" + string(rune('a'+i))
		h.addPhoto(uid)
		h.faces.perExemplar[0] = append(h.faces.perExemplar[0], vectors.FaceCandidate{
			PhotoUID: uid, FaceIndex: 0, Distance: 0.4 - 0.05*float64(i), BBox: bigBox,
		})
	}

	res, err := h.svc.Find(context.Background(), subj, Request{})
	if err != nil {
		t.Fatalf("Find: %v", err)
	}
	if !res.Capped || len(res.Candidates) != 2 {
		t.Fatalf("candidates = %d, capped = %v; want 2 and true", len(res.Candidates), res.Capped)
	}
	// cande is nearest (0.20), candd next (0.25) — the ceiling keeps the nearest.
	if res.Candidates[0].Photo.UID != "cande" || res.Candidates[1].Photo.UID != "candd" {
		t.Errorf("kept %s and %s, want the two nearest cande and candd",
			res.Candidates[0].Photo.UID, res.Candidates[1].Photo.UID)
	}
	// Only the survivors may be hydrated: the ceiling is a memory bound, so it has
	// to cut before the photo records are loaded, not after.
	if h.photos.requested != 2 {
		t.Errorf("hydrated %d photos, want 2 — the ceiling must cut before hydration", h.photos.requested)
	}
}

// TestFind_perExemplarLimitFollowsTheSourceSet checks that the neighbour cap the kNN
// is asked for shrinks as the source set grows. It is the whole latency story of this
// search: the same query costs 10 ms per exemplar at a hundred neighbours and 41 ms at
// a thousand (measured, docs/PERF.md), and a subject tagged on 428 photos pays that
// difference once per exemplar. A lone exemplar has nobody to share the answer with
// and keeps the configured limit.
func TestFind_perExemplarLimitFollowsTheSourceSet(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		exemplars int
		want      int64
	}{
		{"one exemplar keeps the configured limit", 1, 1000},
		{"a heavily tagged subject lands on the floor", 60, 100},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			h := newHarness(t)
			subj := h.addSubject("su_limit")
			for i := range tt.exemplars {
				h.faces.bySubject = append(h.faces.bySubject, vectors.Face{
					PhotoUID: fmt.Sprintf("src%03d", i), FaceIndex: 0, Vector: oneHot(i), DetScore: 0.9,
				})
			}
			if _, err := h.svc.Find(context.Background(), subj, Request{}); err != nil {
				t.Fatalf("Find: %v", err)
			}
			if got := h.faces.searchLimit.Load(); got != tt.want {
				t.Errorf("per-exemplar limit = %d, want %d", got, tt.want)
			}
		})
	}
}
