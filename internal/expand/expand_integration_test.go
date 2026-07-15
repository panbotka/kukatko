//go:build integration

package expand_test

import (
	"context"
	"errors"
	"testing"

	"github.com/panbotka/kukatko/internal/audit"
	"github.com/panbotka/kukatko/internal/database/dbtest"
	"github.com/panbotka/kukatko/internal/expand"
	"github.com/panbotka/kukatko/internal/feedback"
	"github.com/panbotka/kukatko/internal/mediaurl"
	"github.com/panbotka/kukatko/internal/organize"
	"github.com/panbotka/kukatko/internal/photos"
	"github.com/panbotka/kukatko/internal/vectors"
)

// These tests run only under `make test-integration` against KUKATKO_TEST_DATABASE_URL.
// They share one database and truncate between cases, so they do not run in parallel.

// expandHarness bundles the stores and service over a freshly truncated database.
type expandHarness struct {
	vectors  *vectors.Store
	organize *organize.Store
	feedback *feedback.Store
	photos   *photos.Store
	svc      *expand.Service
}

// newExpandHarness returns a harness over a truncated integration database, with the
// given source-set cap (0 uses the package default).
func newExpandHarness(t *testing.T, sourceCap int) expandHarness {
	t.Helper()
	db := dbtest.New(t)
	dbtest.TruncateAll(t, db)
	vectorStore := vectors.NewStore(db.Pool())
	organizeStore := organize.NewStore(db.Pool())
	feedbackStore := feedback.NewStore(db.Pool())
	photoStore := photos.NewStore(db.Pool())
	return expandHarness{
		vectors: vectorStore, organize: organizeStore, feedback: feedbackStore, photos: photoStore,
		svc: expand.New(expand.Config{
			Vectors: vectorStore, Organize: organizeStore, Feedback: feedbackStore, Photos: photoStore,
			Media:       mediaurl.NewBuilder(nil),
			MaxDistance: 0.5, SearchLimit: 200, SourceCap: sourceCap, Concurrency: 4,
		}),
	}
}

// imgVec builds an ImageDim image embedding from index→value overrides.
func imgVec(set map[int]float32) []float32 {
	v := make([]float32, vectors.ImageDim)
	for i, x := range set {
		v[i] = x
	}
	return v
}

// nearE0 is an image vector 0.2 cosine-distance from e0 (well within the 0.5 threshold).
func nearE0() []float32 { return imgVec(map[int]float32{0: 0.8, 1: 0.6}) }

// photo inserts a 1000x800 photo and returns its uid.
func (h expandHarness) photo(t *testing.T, hash string) string {
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

// embedded inserts a photo and stores its image embedding, returning the uid.
func (h expandHarness) embedded(t *testing.T, hash string, vec []float32) string {
	t.Helper()
	uid := h.photo(t, hash)
	if _, err := h.vectors.SaveEmbedding(context.Background(), vectors.Embedding{
		PhotoUID: uid, Vector: vec, Model: "clip", Pretrained: "test",
	}); err != nil {
		t.Fatalf("SaveEmbedding(%s): %v", hash, err)
	}
	return uid
}

// album creates an album with the given members and returns its uid.
func (h expandHarness) album(t *testing.T, title string, members ...string) string {
	t.Helper()
	created, err := h.organize.CreateAlbum(context.Background(), organize.Album{Title: title})
	if err != nil {
		t.Fatalf("CreateAlbum(%s): %v", title, err)
	}
	for _, member := range members {
		if err := h.organize.AddPhoto(context.Background(), created.UID, member); err != nil {
			t.Fatalf("AddPhoto(%s): %v", member, err)
		}
	}
	return created.UID
}

// label creates a label, attaches the given members, and returns its uid.
func (h expandHarness) label(t *testing.T, name string, members ...string) string {
	t.Helper()
	created, err := h.organize.CreateLabel(context.Background(), organize.Label{Name: name})
	if err != nil {
		t.Fatalf("CreateLabel(%s): %v", name, err)
	}
	for _, member := range members {
		if err := h.organize.AttachLabel(context.Background(), member, created.UID, organize.SourceManual, 0); err != nil {
			t.Fatalf("AttachLabel(%s): %v", member, err)
		}
	}
	return created.UID
}

// rejectLabel records a "not this label" rejection for the photo.
func (h expandHarness) rejectLabel(t *testing.T, photoUID, labelUID string) {
	t.Helper()
	entry := audit.Entry{Action: audit.ActionLabelReject, TargetType: "labels", TargetUID: labelUID}
	key := feedback.LabelRejectionKey{PhotoUID: photoUID, LabelUID: labelUID}
	if err := h.feedback.RejectLabel(context.Background(), key, entry); err != nil {
		t.Fatalf("RejectLabel(%s): %v", photoUID, err)
	}
}

// uids returns the candidate photo UIDs as a set.
func uids(res expand.Result) map[string]bool {
	seen := map[string]bool{}
	for _, c := range res.Candidates {
		seen[c.Photo.UID] = true
	}
	return seen
}

// TestAlbumSimilar_findsNeighbourExcludesMembersDB checks a non-member lookalike is
// surfaced while the album's own members never appear.
func TestAlbumSimilar_findsNeighbourExcludesMembersDB(t *testing.T) {
	h := newExpandHarness(t, 0)
	ctx := t.Context()
	member1 := h.embedded(t, "m1", imgVec(map[int]float32{0: 1}))
	member2 := h.embedded(t, "m2", nearE0())
	cand := h.embedded(t, "cand", imgVec(map[int]float32{0: 0.9, 2: 0.436}))
	al := h.album(t, "Trip", member1, member2)

	res, err := h.svc.Album(ctx, al, expand.Request{})
	if err != nil {
		t.Fatalf("Album: %v", err)
	}
	seen := uids(res)
	if !seen[cand] {
		t.Errorf("candidate %s was not surfaced: %+v", cand, res.Candidates)
	}
	if seen[member1] || seen[member2] {
		t.Errorf("a member appeared in the results: %+v", res.Candidates)
	}
	if res.SourcePhotoCount != 2 || res.SourcePhotosWithEmbedding != 2 {
		t.Errorf("summary total/embedded = %d/%d, want 2/2", res.SourcePhotoCount, res.SourcePhotosWithEmbedding)
	}
}

// TestLabelSimilar_rejectedExcludedDB checks a photo rejected for the label is not
// returned while another lookalike is kept.
func TestLabelSimilar_rejectedExcludedDB(t *testing.T) {
	h := newExpandHarness(t, 0)
	ctx := t.Context()
	member := h.embedded(t, "src", imgVec(map[int]float32{0: 1}))
	keep := h.embedded(t, "keep", imgVec(map[int]float32{0: 0.95, 3: 0.312}))
	rejected := h.embedded(t, "rej", nearE0())
	lb := h.label(t, "Ostatky", member)
	h.rejectLabel(t, rejected, lb)

	res, err := h.svc.Label(ctx, lb, expand.Request{})
	if err != nil {
		t.Fatalf("Label: %v", err)
	}
	seen := uids(res)
	if seen[rejected] {
		t.Errorf("rejected photo %s was returned: %+v", rejected, res.Candidates)
	}
	if !seen[keep] {
		t.Errorf("kept lookalike %s was dropped: %+v", keep, res.Candidates)
	}
}

// TestLabelSimilar_negativeExemplarDroppedDB checks a candidate closer to a rejected
// photo than to any photo carrying the label is dropped by the margin rule.
func TestLabelSimilar_negativeExemplarDroppedDB(t *testing.T) {
	h := newExpandHarness(t, 0)
	ctx := t.Context()
	member := h.embedded(t, "src", imgVec(map[int]float32{0: 1}))
	rejected := h.embedded(t, "rej", imgVec(map[int]float32{1: 1}))
	// "near" (~55° off e0) is within the threshold yet closer to the rejected e1 photo
	// than to e0 → dropped. "far" (~37° off e0, away from e1) survives.
	near := h.embedded(t, "near", imgVec(map[int]float32{0: 0.574, 1: 0.819}))
	far := h.embedded(t, "far", nearE0())
	lb := h.label(t, "Ostatky", member)
	h.rejectLabel(t, rejected, lb)

	res, err := h.svc.Label(ctx, lb, expand.Request{})
	if err != nil {
		t.Fatalf("Label: %v", err)
	}
	seen := uids(res)
	if seen[near] {
		t.Errorf("negative-exemplar photo %s was returned: %+v", near, res.Candidates)
	}
	if !seen[far] {
		t.Errorf("distant photo %s was dropped, want kept: %+v", far, res.Candidates)
	}
}

// TestAlbumSimilar_emptyDB checks an album with no photos returns an empty, non-error
// result carrying ReasonEmpty.
func TestAlbumSimilar_emptyDB(t *testing.T) {
	h := newExpandHarness(t, 0)
	al := h.album(t, "Empty")
	res, err := h.svc.Album(t.Context(), al, expand.Request{})
	if err != nil {
		t.Fatalf("Album: %v", err)
	}
	if res.Reason != expand.ReasonEmpty || len(res.Candidates) != 0 {
		t.Fatalf("result = %+v, want empty ReasonEmpty", res)
	}
}

// TestAlbumSimilar_sourceCapReportedDB checks a collection larger than the cap is
// sampled down and the truncation reported.
func TestAlbumSimilar_sourceCapReportedDB(t *testing.T) {
	h := newExpandHarness(t, 2)
	ctx := t.Context()
	members := []string{
		h.embedded(t, "c0", imgVec(map[int]float32{0: 1})),
		h.embedded(t, "c1", imgVec(map[int]float32{0: 0.99, 1: 0.14})),
		h.embedded(t, "c2", imgVec(map[int]float32{0: 0.98, 2: 0.2})),
		h.embedded(t, "c3", imgVec(map[int]float32{0: 0.97, 3: 0.24})),
	}
	cand := h.embedded(t, "cand", imgVec(map[int]float32{0: 0.9, 5: 0.436}))
	al := h.album(t, "Big", members...)

	res, err := h.svc.Album(ctx, al, expand.Request{})
	if err != nil {
		t.Fatalf("Album: %v", err)
	}
	if !res.SourceCapped || res.SourceCap != 2 || res.SourcePhotosSampled != 2 || res.SourcePhotoCount != 4 {
		t.Fatalf("summary = capped %v cap %d sampled %d total %d, want true/2/2/4",
			res.SourceCapped, res.SourceCap, res.SourcePhotosSampled, res.SourcePhotoCount)
	}
	if !uids(res)[cand] {
		t.Errorf("candidate %s was not surfaced by the sampled sources: %+v", cand, res.Candidates)
	}
}

// TestAlbumSimilar_notFoundDB checks the organize album sentinel is surfaced from the DB.
func TestAlbumSimilar_notFoundDB(t *testing.T) {
	h := newExpandHarness(t, 0)
	if _, err := h.svc.Album(t.Context(), "al_missing", expand.Request{}); !errors.Is(err, organize.ErrAlbumNotFound) {
		t.Fatalf("Album = %v, want ErrAlbumNotFound", err)
	}
}

// TestLabelSimilar_notFoundDB checks the organize label sentinel is surfaced from the DB.
func TestLabelSimilar_notFoundDB(t *testing.T) {
	h := newExpandHarness(t, 0)
	if _, err := h.svc.Label(t.Context(), "lb_missing", expand.Request{}); !errors.Is(err, organize.ErrLabelNotFound) {
		t.Fatalf("Label = %v, want ErrLabelNotFound", err)
	}
}
