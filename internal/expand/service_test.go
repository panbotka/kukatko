package expand

import (
	"context"
	"errors"
	"testing"

	"github.com/panbotka/kukatko/internal/mediaurl"
	"github.com/panbotka/kukatko/internal/organize"
	"github.com/panbotka/kukatko/internal/photos"
	"github.com/panbotka/kukatko/internal/vectors"
)

// oneHot returns a length-16 vector that is 1 at index i and 0 elsewhere — a
// distinct embedding direction per source photo so the fake search can recognise
// which source is querying, and a controllable geometry for the negative-exemplar
// rule.
func oneHot(i int) []float32 {
	v := make([]float32, 16)
	v[i] = 1
	return v
}

// vecEqual reports whether two vectors are element-wise identical.
func vecEqual(a, b []float32) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// fakeVectors scripts the VectorStore behaviour: the embeddings behind a set of
// photo UIDs, and which neighbours each source photo's kNN returns.
type fakeVectors struct {
	embeddings map[string][]float32       // GetEmbedding source of truth (any embedded photo)
	sourceEmb  map[string][]float32       // source photos only, to recognise a query vector
	perSource  map[string][]vectors.Match // scripted kNN result per source photo UID
}

func (f *fakeVectors) GetEmbedding(_ context.Context, uid string) (vectors.Embedding, error) {
	if v, ok := f.embeddings[uid]; ok {
		return vectors.Embedding{PhotoUID: uid, Vector: v}, nil
	}
	return vectors.Embedding{}, vectors.ErrEmbeddingNotFound
}

func (f *fakeVectors) FindSimilar(
	_ context.Context, vec []float32, limit int, maxDistance float64,
) ([]vectors.Match, error) {
	var out []vectors.Match
	for _, m := range f.perSource[f.sourceForVec(vec)] {
		if maxDistance > 0 && m.Distance > maxDistance {
			continue
		}
		out = append(out, m)
		if limit > 0 && len(out) >= limit {
			break
		}
	}
	return out, nil
}

// sourceForVec recovers which source photo a query vector belongs to.
func (f *fakeVectors) sourceForVec(vec []float32) string {
	for uid, ev := range f.sourceEmb {
		if vecEqual(ev, vec) {
			return uid
		}
	}
	return ""
}

// fakeOrganize scripts the OrganizeStore behaviour.
type fakeOrganize struct {
	albums      map[string]struct{}
	labels      map[string]struct{}
	albumPhotos map[string][]string
	labelPhotos map[string][]string
}

func (f *fakeOrganize) GetAlbumByUID(_ context.Context, uid string) (organize.Album, error) {
	if _, ok := f.albums[uid]; ok {
		return organize.Album{UID: uid}, nil
	}
	return organize.Album{}, organize.ErrAlbumNotFound
}

func (f *fakeOrganize) ListPhotoUIDs(_ context.Context, albumUID string) ([]string, error) {
	return f.albumPhotos[albumUID], nil
}

func (f *fakeOrganize) GetLabelByUID(_ context.Context, uid string) (organize.Label, error) {
	if _, ok := f.labels[uid]; ok {
		return organize.Label{UID: uid}, nil
	}
	return organize.Label{}, organize.ErrLabelNotFound
}

func (f *fakeOrganize) ListPhotoUIDsByLabel(_ context.Context, labelUID string) ([]string, error) {
	return f.labelPhotos[labelUID], nil
}

// fakeFeedback scripts the FeedbackStore behaviour.
type fakeFeedback struct {
	labelRejections map[string][]string
}

func (f *fakeFeedback) LabelRejectionsForLabel(_ context.Context, labelUID string) ([]string, error) {
	return f.labelRejections[labelUID], nil
}

// fakePhotos scripts the PhotoStore behaviour.
type fakePhotos struct {
	byUID map[string]photos.Photo
}

func (f *fakePhotos) ListByUIDs(_ context.Context, uids []string) ([]photos.Photo, error) {
	var out []photos.Photo
	for _, uid := range uids {
		if photo, ok := f.byUID[uid]; ok {
			out = append(out, photo)
		}
	}
	return out, nil
}

// harness wires a Service over the four fakes; tests populate the fakes before
// calling Album or Label.
type harness struct {
	vectors  *fakeVectors
	organize *fakeOrganize
	feedback *fakeFeedback
	photos   *fakePhotos
	svc      *Service
}

// newHarness builds a harness whose service uses a 0.5 threshold (so the vote-rule
// arithmetic matches the documented examples) and package defaults for everything
// else.
func newHarness(t *testing.T) *harness {
	t.Helper()
	h := &harness{
		vectors: &fakeVectors{
			embeddings: map[string][]float32{},
			sourceEmb:  map[string][]float32{},
			perSource:  map[string][]vectors.Match{},
		},
		organize: &fakeOrganize{
			albums: map[string]struct{}{}, labels: map[string]struct{}{},
			albumPhotos: map[string][]string{}, labelPhotos: map[string][]string{},
		},
		feedback: &fakeFeedback{labelRejections: map[string][]string{}},
		photos:   &fakePhotos{byUID: map[string]photos.Photo{}},
	}
	h.serviceWith(Config{MaxDistance: 0.5})
	return h
}

// serviceWith rebuilds the harness service over the fakes with the given tunables,
// so a test can, for example, force a small source cap.
func (h *harness) serviceWith(cfg Config) {
	cfg.Vectors, cfg.Organize, cfg.Feedback, cfg.Photos = h.vectors, h.organize, h.feedback, h.photos
	cfg.Media = mediaurl.NewBuilder(nil)
	h.svc = New(cfg)
}

// addSource registers a source photo with embedding vec whose kNN returns matches.
func (h *harness) addSource(uid string, vec []float32, matches []vectors.Match) {
	h.vectors.embeddings[uid] = vec
	h.vectors.sourceEmb[uid] = vec
	h.vectors.perSource[uid] = matches
}

// setEmbedding gives a non-source photo (a candidate or a rejected photo) an
// embedding, so the negative-exemplar rule can measure it.
func (h *harness) setEmbedding(uid string, vec []float32) { h.vectors.embeddings[uid] = vec }

// addPhoto registers a hydratable, standalone candidate photo record.
func (h *harness) addPhoto(uid string) {
	h.photos.byUID[uid] = photos.Photo{
		UID: uid, FileHash: uid + "hash", FilePath: "2024/01/" + uid + ".jpg",
		FileWidth: 1000, FileHeight: 800, FileOrientation: 1,
	}
}

// album registers an album with the given members.
func (h *harness) album(uid string, members ...string) {
	h.organize.albums[uid] = struct{}{}
	h.organize.albumPhotos[uid] = members
}

// label registers a label with the given members.
func (h *harness) label(uid string, members ...string) {
	h.organize.labels[uid] = struct{}{}
	h.organize.labelPhotos[uid] = members
}

// match is a shorthand for a scripted kNN hit.
func match(uid string, dist float64) vectors.Match {
	return vectors.Match{PhotoUID: uid, Distance: dist}
}

// TestAlbum_notFound checks the organize album sentinel is surfaced.
func TestAlbum_notFound(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	if _, err := h.svc.Album(context.Background(), "al_missing", Request{}); !errors.Is(err, organize.ErrAlbumNotFound) {
		t.Fatalf("Album = %v, want ErrAlbumNotFound", err)
	}
}

// TestLabel_notFound checks the organize label sentinel is surfaced.
func TestLabel_notFound(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	if _, err := h.svc.Label(context.Background(), "lb_missing", Request{}); !errors.Is(err, organize.ErrLabelNotFound) {
		t.Fatalf("Label = %v, want ErrLabelNotFound", err)
	}
}

// TestAlbum_empty checks an album with no photos returns an empty, non-error result
// carrying ReasonEmpty and zeroed counts, with a non-nil candidate slice.
func TestAlbum_empty(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	h.album("al_empty")
	res, err := h.svc.Album(context.Background(), "al_empty", Request{})
	if err != nil {
		t.Fatalf("Album: %v", err)
	}
	if res.Reason != ReasonEmpty || res.SourcePhotoCount != 0 || len(res.Candidates) != 0 {
		t.Fatalf("result = %+v, want empty ReasonEmpty", res)
	}
	if res.Candidates == nil {
		t.Error("Candidates is nil, want an empty slice for a clean JSON []")
	}
	if res.Kind != KindAlbum {
		t.Errorf("Kind = %q, want %q", res.Kind, KindAlbum)
	}
}

// TestLabel_noEmbeddings checks a label whose members are all unembedded reports the
// counts and ReasonNoEmbeddings rather than silently returning nothing.
func TestLabel_noEmbeddings(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	h.label("lb", "p1", "p2")
	res, err := h.svc.Label(context.Background(), "lb", Request{})
	if err != nil {
		t.Fatalf("Label: %v", err)
	}
	if res.Reason != ReasonNoEmbeddings {
		t.Fatalf("reason = %q, want %q", res.Reason, ReasonNoEmbeddings)
	}
	if res.SourcePhotoCount != 2 || res.SourcePhotosSampled != 2 || res.SourcePhotosWithEmbedding != 0 {
		t.Errorf("counts = total %d sampled %d embedded %d, want 2/2/0",
			res.SourcePhotoCount, res.SourcePhotosSampled, res.SourcePhotosWithEmbedding)
	}
}

// TestAlbum_singlePhoto checks a one-photo collection degenerates cleanly to
// per-photo similarity: its neighbour is surfaced with match_count 1, the similarity
// filled in, and the media URL stamped.
func TestAlbum_singlePhoto(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	h.addPhoto("cand")
	// The source's own kNN returns itself and the candidate; the self hit is excluded.
	h.addSource("src", oneHot(0), []vectors.Match{match("src", 0.0), match("cand", 0.2)})
	h.album("al", "src")

	res, err := h.svc.Album(context.Background(), "al", Request{})
	if err != nil {
		t.Fatalf("Album: %v", err)
	}
	if len(res.Candidates) != 1 || res.Candidates[0].Photo.UID != "cand" {
		t.Fatalf("candidates = %+v, want only 'cand'", res.Candidates)
	}
	got := res.Candidates[0]
	if got.MatchCount != 1 || got.Distance != 0.2 || got.Similarity != 0.8 {
		t.Errorf("candidate = mc %d dist %v sim %v, want 1/0.2/0.8", got.MatchCount, got.Distance, got.Similarity)
	}
	if res.MinMatchCount != 1 || res.SourcePhotosWithEmbedding != 1 {
		t.Errorf("summary min %d embedded %d, want 1/1", res.MinMatchCount, res.SourcePhotosWithEmbedding)
	}
	if got.Photo.ThumbURL == "" {
		t.Error("candidate photo has no thumb_url; media was not stamped")
	}
}

// TestExpand_membersExcluded checks photos already in the collection never appear,
// even when other members' kNN returns them.
func TestExpand_membersExcluded(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	h.addPhoto("cand")
	h.addSource("a", oneHot(0), []vectors.Match{match("a", 0.0), match("b", 0.1), match("cand", 0.2)})
	h.addSource("b", oneHot(1), []vectors.Match{match("b", 0.0), match("a", 0.1), match("cand", 0.25)})
	h.album("al", "a", "b")

	res, err := h.svc.Album(context.Background(), "al", Request{})
	if err != nil {
		t.Fatalf("Album: %v", err)
	}
	if len(res.Candidates) != 1 || res.Candidates[0].Photo.UID != "cand" {
		t.Fatalf("candidates = %+v, want only the non-member 'cand'", res.Candidates)
	}
	if res.Candidates[0].MatchCount != 2 || res.Candidates[0].Distance != 0.2 {
		t.Errorf("cand vote/distance = %d/%v, want 2/0.2 (nearest across both members)",
			res.Candidates[0].MatchCount, res.Candidates[0].Distance)
	}
}

// TestExpand_votingRanksAgreementFirst checks a candidate several source photos
// agree on ranks above one a single source matches very strongly.
func TestExpand_votingRanksAgreementFirst(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	h.addPhoto("agreed")
	h.addPhoto("strong")
	h.addSource("s0", oneHot(0), []vectors.Match{match("agreed", 0.40), match("strong", 0.05)})
	h.addSource("s1", oneHot(1), []vectors.Match{match("agreed", 0.45)})
	h.album("al", "s0", "s1")

	res, err := h.svc.Album(context.Background(), "al", Request{})
	if err != nil {
		t.Fatalf("Album: %v", err)
	}
	if len(res.Candidates) != 2 {
		t.Fatalf("candidates = %+v, want 2", res.Candidates)
	}
	if res.Candidates[0].Photo.UID != "agreed" || res.Candidates[1].Photo.UID != "strong" {
		t.Errorf("order = %s then %s, want agreed then strong (match_count beats distance)",
			res.Candidates[0].Photo.UID, res.Candidates[1].Photo.UID)
	}
	if res.Candidates[0].MatchCount != 2 || res.Candidates[0].Distance != 0.40 {
		t.Errorf("agreed vote/distance = %d/%v, want 2/0.40",
			res.Candidates[0].MatchCount, res.Candidates[0].Distance)
	}
}

// TestExpand_voteRuleFilters checks that with nine source photos (min_match_count 2)
// a candidate a single source returns is dropped while one two sources return
// survives.
func TestExpand_voteRuleFilters(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	h.addPhoto("one")
	h.addPhoto("two")
	members := make([]string, 0, 9)
	for i := range 9 {
		uid := "s" + string(rune('a'+i))
		members = append(members, uid)
		var matches []vectors.Match
		switch i {
		case 0:
			matches = []vectors.Match{match("one", 0.2), match("two", 0.3)}
		case 1:
			matches = []vectors.Match{match("two", 0.25)}
		}
		h.addSource(uid, oneHot(i), matches)
	}
	h.album("al", members...)

	res, err := h.svc.Album(context.Background(), "al", Request{})
	if err != nil {
		t.Fatalf("Album: %v", err)
	}
	if res.MinMatchCount != 2 {
		t.Fatalf("MinMatchCount = %d, want 2", res.MinMatchCount)
	}
	if len(res.Candidates) != 1 || res.Candidates[0].Photo.UID != "two" {
		t.Fatalf("candidates = %+v, want only 'two' (seen by two sources)", res.Candidates)
	}
	if res.Candidates[0].MatchCount != 2 || res.Candidates[0].Distance != 0.25 {
		t.Errorf("survivor vote/distance = %d/%v, want 2/0.25 (nearest)",
			res.Candidates[0].MatchCount, res.Candidates[0].Distance)
	}
}

// TestLabel_rejectedExcluded checks a photo rejected for the label never appears.
func TestLabel_rejectedExcluded(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	h.addPhoto("keep")
	h.addPhoto("rejected")
	h.addSource("src", oneHot(0), []vectors.Match{match("keep", 0.2), match("rejected", 0.1)})
	h.label("lb", "src")
	h.feedback.labelRejections["lb"] = []string{"rejected"}
	// Embeddings for the negative pass: far apart, so nothing extra drops.
	h.setEmbedding("keep", oneHot(3))
	h.setEmbedding("rejected", oneHot(7))

	res, err := h.svc.Label(context.Background(), "lb", Request{})
	if err != nil {
		t.Fatalf("Label: %v", err)
	}
	if len(res.Candidates) != 1 || res.Candidates[0].Photo.UID != "keep" {
		t.Fatalf("candidates = %+v, want only 'keep' (rejected excluded)", res.Candidates)
	}
}

// TestLabel_negativeExemplarDrop checks a candidate closer to a rejected photo than
// to any photo carrying the label is dropped, while a distant candidate survives.
func TestLabel_negativeExemplarDrop(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	h.addPhoto("near")
	h.addPhoto("far")
	h.addSource("src", oneHot(0), []vectors.Match{match("near", 0.4), match("far", 0.4)})
	h.label("lb", "src")
	// A rejection on a photo that is not itself a candidate, so the outright filter
	// does not remove "near" — only the margin rule can.
	h.feedback.labelRejections["lb"] = []string{"rej"}
	rejVec := make([]float32, 16)
	rejVec[1], rejVec[2] = 0.9, 0.1 // very close to oneHot(1), far from oneHot(0)/oneHot(3)
	h.setEmbedding("rej", rejVec)
	h.setEmbedding("near", oneHot(1))
	h.setEmbedding("far", oneHot(3))

	res, err := h.svc.Label(context.Background(), "lb", Request{})
	if err != nil {
		t.Fatalf("Label: %v", err)
	}
	if len(res.Candidates) != 1 || res.Candidates[0].Photo.UID != "far" {
		t.Fatalf("candidates = %+v, want only 'far' ('near' trips the negative rule)", res.Candidates)
	}
}

// TestExpand_sourceCapReported checks a collection larger than the cap is sampled
// down to it and the truncation is reported rather than silent.
func TestExpand_sourceCapReported(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	h.serviceWith(Config{MaxDistance: 0.5, SourceCap: 2})
	h.addPhoto("cand")
	members := []string{"m0", "m1", "m2", "m3", "m4"}
	for i, uid := range members {
		// Every member is embedded; only the two sampled (m0, m2) get queried.
		h.addSource(uid, oneHot(i), []vectors.Match{match("cand", 0.2)})
	}
	h.album("al", members...)

	res, err := h.svc.Album(context.Background(), "al", Request{})
	if err != nil {
		t.Fatalf("Album: %v", err)
	}
	if !res.SourceCapped || res.SourceCap != 2 || res.SourcePhotosSampled != 2 || res.SourcePhotoCount != 5 {
		t.Fatalf("summary = capped %v cap %d sampled %d total %d, want true/2/2/5",
			res.SourceCapped, res.SourceCap, res.SourcePhotosSampled, res.SourcePhotoCount)
	}
	if len(res.Candidates) != 1 || res.Candidates[0].Photo.UID != "cand" {
		t.Errorf("candidates = %+v, want the sampled sources' shared neighbour 'cand'", res.Candidates)
	}
}

// TestExpand_limitTruncates checks the result honours the request limit, keeping the
// top-ranked candidates.
func TestExpand_limitTruncates(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	h.addPhoto("near")
	h.addPhoto("far")
	h.addSource("src", oneHot(0), []vectors.Match{match("near", 0.1), match("far", 0.4)})
	h.album("al", "src")

	res, err := h.svc.Album(context.Background(), "al", Request{Limit: 1})
	if err != nil {
		t.Fatalf("Album: %v", err)
	}
	if res.Limit != 1 {
		t.Errorf("Limit = %d, want 1", res.Limit)
	}
	if len(res.Candidates) != 1 || res.Candidates[0].Photo.UID != "near" {
		t.Fatalf("candidates = %+v, want only the nearest 'near'", res.Candidates)
	}
}

// TestExpand_nonPrimaryStackMemberSkipped checks a candidate that is a non-primary
// stack member is kept out of the results (it surfaces only through its primary).
func TestExpand_nonPrimaryStackMemberSkipped(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	h.addPhoto("primary")
	stackUID := "st1"
	h.photos.byUID["stacked"] = photos.Photo{
		UID: "stacked", FileHash: "stackedhash", FilePath: "2024/01/stacked.jpg",
		StackUID: &stackUID, StackPrimary: false,
	}
	h.addSource("src", oneHot(0), []vectors.Match{match("primary", 0.2), match("stacked", 0.1)})
	h.album("al", "src")

	res, err := h.svc.Album(context.Background(), "al", Request{})
	if err != nil {
		t.Fatalf("Album: %v", err)
	}
	if len(res.Candidates) != 1 || res.Candidates[0].Photo.UID != "primary" {
		t.Fatalf("candidates = %+v, want only 'primary' (non-primary stack member hidden)", res.Candidates)
	}
}
