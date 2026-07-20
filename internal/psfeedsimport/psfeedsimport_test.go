package psfeedsimport

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/panbotka/kukatko/internal/importer"
	"github.com/panbotka/kukatko/internal/people"
	"github.com/panbotka/kukatko/internal/photos"
	"github.com/panbotka/kukatko/internal/psfeeds"
	"github.com/panbotka/kukatko/internal/vectors"
)

// hasStage reports whether any recorded failure is of the given stage.
func hasStage(failures []importer.Failure, stage importer.Stage) bool {
	for _, f := range failures {
		if f.Stage == stage {
			return true
		}
	}
	return false
}

// quietLogger is a logger that discards output so best-effort warnings do not
// clutter test output.
func quietLogger() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

// clipVec returns a 768-element CLIP vector with the first slot set, so it passes
// the store's dimension check.
func clipVec(first float32) []float32 {
	v := make([]float32, vectors.ImageDim)
	v[0] = first
	return v
}

// faceVec returns a 512-element face vector with the first slot set.
func faceVec(first float32) []float32 {
	v := make([]float32, vectors.FaceDim)
	v[0] = first
	return v
}

// newService wires the importer over the given fakes with a small page size so a
// multi-item fixture pages more than once.
func newService(feeds *fakeFeeds, ph *fakePhotos, vec *fakeVectors, ppl *fakePeople, runs *fakeRuns) *Service {
	return New(Config{
		Feeds: feeds, Photos: ph, Vectors: vec, People: ppl, Runs: runs,
		PageSize: 2, Logger: quietLogger(),
	})
}

// photoByPP builds a fakePhotos mapping the given PhotoPrism UIDs to Kukátko
// photos with a 1000x800 frame.
func photoByPP(ppUIDs ...string) *fakePhotos {
	m := map[string]photos.Photo{}
	for _, pp := range ppUIDs {
		m[pp] = photos.Photo{UID: "kk-" + pp, FileWidth: 1000, FileHeight: 800, FileOrientation: 1}
	}
	return &fakePhotos{byPPUID: m}
}

func TestImport_embeddingsAttachByPhotoprismUID(t *testing.T) {
	t.Parallel()
	feeds := &fakeFeeds{embeddings: []psfeeds.Embedding{
		{PhotoUID: "pp1", Model: "ViT-L-14", Pretrained: "laion", Vector: clipVec(0.1)},
		{PhotoUID: "pp2", Model: "ViT-L-14", Pretrained: "laion", Vector: clipVec(0.2)},
		{PhotoUID: "pp3", Model: "ViT-L-14", Pretrained: "laion", Vector: clipVec(0.3)},
	}}
	vec := newFakeVectors()
	runs := &fakeRuns{}
	svc := newService(feeds, photoByPP("pp1", "pp2", "pp3"), vec, newFakePeople(), runs)

	res, err := svc.Import(context.Background())
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	if len(vec.embeddings) != 3 {
		t.Fatalf("saved %d embeddings, want 3", len(vec.embeddings))
	}
	got := vec.embeddings["kk-pp2"]
	if got.PhotoUID != "kk-pp2" || got.Pretrained != "laion" || got.Vector[0] != 0.2 {
		t.Errorf("embedding for pp2 attached wrong: %+v", got)
	}
	if res.Counts.Imported != 3 || res.Counts.Skipped != 0 || res.Counts.Failed != 0 {
		t.Errorf("counts = %+v, want imported=3", res.Counts)
	}
	if runs.completed != 1 || runs.failed != 0 {
		t.Errorf("run lifecycle completed=%d failed=%d, want 1/0", runs.completed, runs.failed)
	}
	if runs.updateCounts < 2 {
		t.Errorf("updateCounts = %d, want >= 2 (paged)", runs.updateCounts)
	}
}

func TestImport_skipsNotYetImportedPhoto(t *testing.T) {
	t.Parallel()
	feeds := &fakeFeeds{
		embeddings: []psfeeds.Embedding{
			{PhotoUID: "pp1", Vector: clipVec(0.1)},
			{PhotoUID: "ppMISSING", Vector: clipVec(0.9)},
		},
		faces: []psfeeds.Face{
			{ID: 1, PhotoUID: "ppMISSING", FaceIndex: 0, Vector: faceVec(0.1), BBox: []float64{1, 2, 3, 4}, PhotoWidth: 1000, PhotoHeight: 800, Orientation: 1},
			{ID: 2, PhotoUID: "pp1", FaceIndex: 0, Vector: faceVec(0.2), BBox: []float64{10, 20, 30, 40}, PhotoWidth: 1000, PhotoHeight: 800, Orientation: 1},
		},
	}
	vec := newFakeVectors()
	runs := &fakeRuns{}
	svc := newService(feeds, photoByPP("pp1"), vec, newFakePeople(), runs)

	res, err := svc.Import(context.Background())
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	if _, ok := vec.embeddings["kk-pp1"]; !ok {
		t.Errorf("pp1 embedding missing")
	}
	if _, ok := vec.faces["kk-pp1"]; !ok {
		t.Errorf("pp1 faces missing")
	}
	// One missing-photo embedding + one missing-photo face group = 2 skipped.
	if res.Counts.Skipped != 2 {
		t.Errorf("skipped = %d, want 2", res.Counts.Skipped)
	}
	if runs.failed != 0 || runs.completed != 1 {
		t.Errorf("run should complete despite skips: completed=%d failed=%d", runs.completed, runs.failed)
	}
}

func TestImport_facesTransferMarkersSubjectsAndNormaliseBBox(t *testing.T) {
	t.Parallel()
	feeds := &fakeFeeds{faces: []psfeeds.Face{
		{
			ID: 1, PhotoUID: "pp1", FaceIndex: 0, Model: "buffalo_l", Vector: faceVec(0.1),
			BBox: []float64{200, 150, 600, 450}, DetScore: 0.9,
			MarkerUID: "mk1", SubjectUID: "psSubjA", SubjectName: "Alice",
			PhotoWidth: 800, PhotoHeight: 600, Orientation: 1,
		},
		{
			ID: 2, PhotoUID: "pp1", FaceIndex: 1, Model: "buffalo_l", Vector: faceVec(0.2),
			BBox: []float64{0, 0, 80, 60}, DetScore: 0.8,
			SubjectName: "Alice", // second Alice face, no marker
			PhotoWidth:  800, PhotoHeight: 600, Orientation: 1,
		},
	}}
	vec := newFakeVectors()
	ppl := newFakePeople()
	svc := newService(feeds, photoByPP("pp1"), vec, ppl, &fakeRuns{})

	if _, err := svc.Import(context.Background()); err != nil {
		t.Fatalf("Import: %v", err)
	}
	saved := vec.faces["kk-pp1"]
	if len(saved.faces) != 2 || saved.model != "buffalo_l" {
		t.Fatalf("faces = %+v model=%q, want 2 faces / buffalo_l", saved.faces, saved.model)
	}
	// BBox [200,150,600,450] in an 800x600 frame → [0.25, 0.25, 0.5, 0.5].
	want := [4]float64{0.25, 0.25, 0.5, 0.5}
	if saved.faces[0].BBox != want {
		t.Errorf("bbox = %v, want %v (pixel→normalised)", saved.faces[0].BBox, want)
	}
	// Subject "Alice" created once and reused across both faces.
	if ppl.createSubjects != 1 {
		t.Errorf("createSubjects = %d, want 1 (same name reused)", ppl.createSubjects)
	}
	if saved.faces[0].SubjectUID == nil || saved.faces[1].SubjectUID == nil ||
		*saved.faces[0].SubjectUID != *saved.faces[1].SubjectUID {
		t.Errorf("both faces should reference the same subject uid: %+v / %+v",
			saved.faces[0].SubjectUID, saved.faces[1].SubjectUID)
	}
	// One marker created, carrying the preserved feed UID and the subject.
	if ppl.createMarkers != 1 {
		t.Errorf("createMarkers = %d, want 1", ppl.createMarkers)
	}
	marker, ok := ppl.markers["mk1"]
	if !ok || marker.SubjectUID == nil || marker.PhotoUID != "kk-pp1" || marker.Type != people.MarkerFace {
		t.Errorf("marker mk1 wrong: %+v (ok=%v)", marker, ok)
	}
	if saved.faces[0].MarkerUID == nil || *saved.faces[0].MarkerUID != "mk1" {
		t.Errorf("face 0 marker_uid = %v, want mk1", saved.faces[0].MarkerUID)
	}
}

func TestImport_idempotentRerun(t *testing.T) {
	t.Parallel()
	feeds := &fakeFeeds{
		embeddings: []psfeeds.Embedding{{PhotoUID: "pp1", Vector: clipVec(0.1)}},
		faces: []psfeeds.Face{{
			ID: 1, PhotoUID: "pp1", FaceIndex: 0, Vector: faceVec(0.1), BBox: []float64{10, 20, 30, 40},
			MarkerUID: "mk1", SubjectName: "Alice", PhotoWidth: 1000, PhotoHeight: 800, Orientation: 1,
		}},
	}
	vec := newFakeVectors()
	ppl := newFakePeople()
	ph := photoByPP("pp1")

	first := newService(feeds, ph, vec, ppl, &fakeRuns{})
	if _, err := first.Import(context.Background()); err != nil {
		t.Fatalf("first Import: %v", err)
	}
	second := newService(feeds, ph, vec, ppl, &fakeRuns{})
	res, err := second.Import(context.Background())
	if err != nil {
		t.Fatalf("second Import: %v", err)
	}
	// The subject and marker are found on the re-run, not recreated.
	if ppl.createSubjects != 1 {
		t.Errorf("createSubjects after two runs = %d, want 1", ppl.createSubjects)
	}
	if ppl.createMarkers != 1 {
		t.Errorf("createMarkers after two runs = %d, want 1", ppl.createMarkers)
	}
	if res.Counts.Imported != 2 { // one embedding + one face group, again
		t.Errorf("second run imported = %d, want 2 (idempotent upserts)", res.Counts.Imported)
	}
}

func TestImport_dimMismatchCountedFailedNotFatal(t *testing.T) {
	t.Parallel()
	feeds := &fakeFeeds{embeddings: []psfeeds.Embedding{{PhotoUID: "pp1", Vector: clipVec(0.1)}}}
	vec := newFakeVectors()
	vec.embErr = vectors.ErrDimMismatch
	runs := &fakeRuns{}
	svc := newService(feeds, photoByPP("pp1"), vec, newFakePeople(), runs)

	res, err := svc.Import(context.Background())
	if err != nil {
		t.Fatalf("Import should not fail on a per-item dim mismatch: %v", err)
	}
	if res.Counts.Failed != 1 || res.Counts.Imported != 0 {
		t.Errorf("counts = %+v, want failed=1 imported=0", res.Counts)
	}
	if runs.completed != 1 {
		t.Errorf("run should still complete, completed=%d", runs.completed)
	}
	// The dim mismatch is persisted as a StageEmbedding failure, so the run closes
	// 'partial' rather than 'done'.
	if !hasStage(runs.failures, importer.StageEmbedding) {
		t.Errorf("no StageEmbedding failure recorded: %+v", runs.failures)
	}
	if runs.lastStatus != importer.StatusPartial {
		t.Errorf("run status = %q, want partial", runs.lastStatus)
	}
}

func TestImport_feedErrorFailsRun(t *testing.T) {
	t.Parallel()
	sentinel := errors.New("boom")
	feeds := &fakeFeeds{embErr: sentinel}
	runs := &fakeRuns{}
	svc := newService(feeds, photoByPP(), newFakeVectors(), newFakePeople(), runs)

	_, err := svc.Import(context.Background())
	if !errors.Is(err, sentinel) {
		t.Fatalf("Import error = %v, want sentinel", err)
	}
	if runs.failed != 1 || runs.completed != 0 {
		t.Errorf("run lifecycle failed=%d completed=%d, want 1/0", runs.failed, runs.completed)
	}
}

func TestImport_watermarkIsNewestTimestamp(t *testing.T) {
	t.Parallel()
	early := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	late := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	feeds := &fakeFeeds{embeddings: []psfeeds.Embedding{
		{PhotoUID: "pp1", Vector: clipVec(0.1), CreatedAt: early},
		{PhotoUID: "pp2", Vector: clipVec(0.2), CreatedAt: late},
	}}
	runs := &fakeRuns{}
	svc := newService(feeds, photoByPP("pp1", "pp2"), newFakeVectors(), newFakePeople(), runs)

	res, err := svc.Import(context.Background())
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	if res.Watermark == nil || !res.Watermark.Equal(late) {
		t.Errorf("watermark = %v, want %v", res.Watermark, late)
	}
}
