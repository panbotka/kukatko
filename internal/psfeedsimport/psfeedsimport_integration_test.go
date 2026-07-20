//go:build integration

package psfeedsimport

import (
	"context"
	"testing"

	"github.com/panbotka/kukatko/internal/database/dbtest"
	"github.com/panbotka/kukatko/internal/importer"
	"github.com/panbotka/kukatko/internal/people"
	"github.com/panbotka/kukatko/internal/photos"
	"github.com/panbotka/kukatko/internal/psfeeds"
	"github.com/panbotka/kukatko/internal/vectors"
)

// intHarness bundles the real stores and the importer wired over a fake feeds
// client, so the whole enrichment path runs against a real database.
type intHarness struct {
	photos  *photos.Store
	vectors *vectors.Store
	people  *people.Store
	runs    *importer.Store
	feeds   *fakeFeeds
	svc     *Service
}

// newIntHarness builds the harness against a truncated test database.
func newIntHarness(t *testing.T, feeds *fakeFeeds) intHarness {
	t.Helper()
	db := dbtest.New(t)
	dbtest.TruncateAll(t, db)
	pool := db.Pool()
	h := intHarness{
		photos:  photos.NewStore(pool),
		vectors: vectors.NewStore(pool),
		people:  people.NewStore(pool),
		runs:    importer.NewStore(pool),
		feeds:   feeds,
	}
	h.svc = New(Config{
		Feeds: feeds, Photos: h.photos, Vectors: h.vectors, People: h.people, Runs: h.runs,
		PageSize: 2, Logger: quietLogger(),
	})
	return h
}

// makePhoto inserts a photo carrying the given PhotoPrism UID and returns its
// Kukátko uid.
func (h intHarness) makePhoto(t *testing.T, hash, ppUID string) string {
	t.Helper()
	created, err := h.photos.Create(context.Background(), photos.Photo{
		FileHash: hash, FilePath: "2026/01/" + hash + ".jpg", FileName: hash + ".jpg",
		FileWidth: 800, FileHeight: 600, FileOrientation: 1,
		PhotoprismUID: &ppUID,
	})
	if err != nil {
		t.Fatalf("creating photo %s: %v", hash, err)
	}
	return created.UID
}

// enrichmentFeeds returns a feed fixture with one embedding + one named,
// marked face for pp1, plus one embedding and one face for a photo that has not
// been imported (ppMISSING), to exercise the skip path.
func enrichmentFeeds() *fakeFeeds {
	return &fakeFeeds{
		embeddings: []psfeeds.Embedding{
			{PhotoUID: "pp1", Model: "ViT-L-14", Pretrained: "laion2b_s32b_b82k", Dim: 768, Vector: clipVec(0.11)},
			{PhotoUID: "ppMISSING", Model: "ViT-L-14", Pretrained: "laion2b_s32b_b82k", Dim: 768, Vector: clipVec(0.99)},
		},
		faces: []psfeeds.Face{
			{
				ID: 1, PhotoUID: "pp1", FaceIndex: 0, Model: "buffalo_l (ResNet100)", Dim: 512,
				Vector: faceVec(0.22), BBox: []float64{200, 150, 600, 450}, DetScore: 0.95,
				MarkerUID: "mt-alice", SubjectUID: "ps-alice", SubjectName: "Alice",
				PhotoWidth: 800, PhotoHeight: 600, Orientation: 1,
			},
			{
				ID: 2, PhotoUID: "ppMISSING", FaceIndex: 0, Model: "buffalo_l (ResNet100)", Dim: 512,
				Vector: faceVec(0.33), BBox: []float64{10, 20, 30, 40}, DetScore: 0.9,
				PhotoWidth: 800, PhotoHeight: 600, Orientation: 1,
			},
		},
	}
}

// TestImport_endToEndAttachesVectorsMarkersSubjectsDB verifies the whole path
// against a real database: the embedding and faces land 1:1 on the photo found by
// photoprism_uid, the marker and subject transfer, and a feed entry for a
// not-yet-imported photo is skipped rather than erroring.
func TestImport_endToEndAttachesVectorsMarkersSubjectsDB(t *testing.T) {
	h := newIntHarness(t, enrichmentFeeds())
	ctx := t.Context()
	kkUID := h.makePhoto(t, "hash-pp1", "pp1")

	res, err := h.svc.Import(ctx)
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	if res.Counts.Imported != 2 || res.Counts.Skipped != 2 || res.Counts.Failed != 0 {
		t.Fatalf("counts = %+v, want imported=2 skipped=2 failed=0", res.Counts)
	}

	emb, err := h.vectors.GetEmbedding(ctx, kkUID)
	if err != nil {
		t.Fatalf("GetEmbedding: %v", err)
	}
	// The vector is stored as halfvec (float16), so 0.11 round-trips approximately.
	if emb.Pretrained != "laion2b_s32b_b82k" || len(emb.Vector) != vectors.ImageDim ||
		emb.Vector[0] < 0.10 || emb.Vector[0] > 0.12 {
		t.Errorf("embedding attached wrong: model=%q pretrained=%q len=%d v0=%v",
			emb.Model, emb.Pretrained, len(emb.Vector), emb.Vector[0])
	}

	faces, err := h.vectors.ListFaces(ctx, kkUID)
	if err != nil {
		t.Fatalf("ListFaces: %v", err)
	}
	if len(faces) != 1 {
		t.Fatalf("faces = %d, want 1", len(faces))
	}
	face := faces[0]
	want := [4]float64{0.25, 0.25, 0.5, 0.5} // [200,150,600,450] in an 800x600 frame.
	if face.BBox != want {
		t.Errorf("face bbox = %v, want %v (pixel→normalised)", face.BBox, want)
	}
	if face.SubjectName != "Alice" || face.SubjectUID == nil || face.MarkerUID == nil || *face.MarkerUID != "mt-alice" {
		t.Errorf("face assignment cache wrong: name=%q subj=%v marker=%v",
			face.SubjectName, face.SubjectUID, face.MarkerUID)
	}

	subject, err := h.people.GetSubjectBySlug(ctx, "alice")
	if err != nil {
		t.Fatalf("subject not created: %v", err)
	}
	if face.SubjectUID == nil || *face.SubjectUID != subject.UID {
		t.Errorf("face subject_uid %v != created subject %s", face.SubjectUID, subject.UID)
	}

	marker, err := h.people.GetMarkerByUID(ctx, "mt-alice")
	if err != nil {
		t.Fatalf("marker not created: %v", err)
	}
	if marker.PhotoUID != kkUID || marker.SubjectUID == nil || *marker.SubjectUID != subject.UID ||
		marker.Type != people.MarkerFace || marker.X != 0.25 {
		t.Errorf("marker wrong: %+v", marker)
	}

	run, ok, err := h.runs.LatestRun(ctx, importer.SourcePhotoSorterFeeds)
	if err != nil || !ok {
		t.Fatalf("LatestRun ok=%v err=%v", ok, err)
	}
	if run.Status != importer.StatusDone {
		t.Errorf("run status = %q, want done", run.Status)
	}
}

// TestImport_rerunIsIdempotentDB verifies a second full pass over the same feeds
// does not duplicate subjects, markers, embeddings or faces.
func TestImport_rerunIsIdempotentDB(t *testing.T) {
	h := newIntHarness(t, enrichmentFeeds())
	ctx := t.Context()
	kkUID := h.makePhoto(t, "hash-pp1", "pp1")

	if _, err := h.svc.Import(ctx); err != nil {
		t.Fatalf("first Import: %v", err)
	}
	if _, err := h.svc.Import(ctx); err != nil {
		t.Fatalf("second Import: %v", err)
	}

	faces, err := h.vectors.ListFaces(ctx, kkUID)
	if err != nil {
		t.Fatalf("ListFaces: %v", err)
	}
	if len(faces) != 1 {
		t.Errorf("faces after re-run = %d, want 1 (replaced, not duplicated)", len(faces))
	}
	subjects, err := h.people.ListSubjects(ctx)
	if err != nil {
		t.Fatalf("ListSubjects: %v", err)
	}
	if len(subjects) != 1 {
		t.Errorf("subjects after re-run = %d, want 1 (reused by slug)", len(subjects))
	}
	if _, err := h.people.GetMarkerByUID(ctx, "mt-alice"); err != nil {
		t.Errorf("marker missing after re-run: %v", err)
	}
}
