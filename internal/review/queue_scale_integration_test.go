//go:build integration

package review_test

// Scale tests for GET /review/queue against the real database.
//
// A small library proves nothing here: the defect this guards against only
// appears once the library has many named people, because building the queue
// used to run the whole recognition sweep — one kNN per exemplar per subject —
// inside the request. On production (105 named subjects) that was 250 s.
//
// The subject dimension is therefore seeded at production scale (105 named
// subjects). The face dimension is not: 100 000 faces would mean 100 000 HNSW
// index inserts before the first assertion, minutes of fixture on the test box,
// and it would not change what is asserted — the number of kNN queries is a
// function of the subject count, not of how many rows each query walks. The
// assertion is that count, not a wall-clock number, which would be flaky on a
// shared machine.

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/panbotka/kukatko/internal/candidates"
	"github.com/panbotka/kukatko/internal/expand"
	"github.com/panbotka/kukatko/internal/facematch"
	"github.com/panbotka/kukatko/internal/mediaurl"
	"github.com/panbotka/kukatko/internal/review"
	"github.com/panbotka/kukatko/internal/sweep"
	"github.com/panbotka/kukatko/internal/vectors"
)

// productionSubjects is the live library's named-subject count, the dimension
// that drove the 250 s request.
const productionSubjects = 105

// countingFaces wraps the real vector store and counts the per-exemplar kNN
// queries the candidate search runs, so a test can assert on the work behind one
// queue request. Every other method passes straight through to the real store.
type countingFaces struct {
	*vectors.Store
	mu  sync.Mutex
	knn int
}

// FindSimilarUnassignedFaceCandidates counts the query and runs the real one.
func (c *countingFaces) FindSimilarUnassignedFaceCandidates(
	ctx context.Context, vec []float32, limit int, maxDistance float64, exclude []vectors.FaceKey,
) ([]vectors.FaceCandidate, error) {
	c.mu.Lock()
	c.knn++
	c.mu.Unlock()
	return c.Store.FindSimilarUnassignedFaceCandidates(ctx, vec, limit, maxDistance, exclude)
}

// queries returns the kNN tally under the lock.
func (c *countingFaces) queries() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.knn
}

// instrumentedService composes the review service over the harness stores with
// the face store instrumented, mirroring cmd/kukatko's wiring otherwise. cfg is
// applied on top of the shared defaults so a test can widen the bounds.
func (h *reviewHarness) instrumentedService(cfg review.Config) (*review.Service, *countingFaces) {
	faces := &countingFaces{Store: h.vectors}
	candSvc := candidates.New(candidates.Config{
		Faces: faces, People: h.people, Feedback: h.feedback, Photos: h.photos,
		Media:       mediaurl.NewBuilder(nil),
		MaxDistance: 0.5, SearchLimit: 1000, MinFacePx: 32, Concurrency: 2, MinFaceRel: 0.02,
	})
	cfg.Sweeper = sweep.New(sweep.Config{Subjects: h.people, Finder: candSvc, Concurrency: 4})
	cfg.Expander = expand.New(expand.Config{
		Vectors: h.vectors, Organize: h.organize, Feedback: h.feedback, Photos: h.photos,
		Media:       mediaurl.NewBuilder(nil),
		MaxDistance: 0.5, SearchLimit: 200, Concurrency: 2,
	})
	cfg.Organize = h.organize
	cfg.Faces = h.vectors
	cfg.Feedback = h.feedback
	cfg.Assigner = facematch.New(facematch.Config{Photos: h.photos, Faces: h.vectors, People: h.people})
	cfg.BandMin, cfg.BandMax = 0.45, 0.75
	return review.New(cfg), faces
}

// seedNamedSubjects creates n named subjects, each with one exemplar face and an
// assigned marker so the recognition scan picks it up.
func (h *reviewHarness) seedNamedSubjects(t *testing.T, n int) {
	t.Helper()
	for i := range n {
		name := fmt.Sprintf("Person %03d", i)
		// Exemplars fan out across the first two dimensions so every subject sits
		// a different distance from the unassigned faces, as a real library does.
		h.namedSubject(t, name, fmt.Sprintf("src-%03d", i), vec(map[int]float32{
			0: 1, 1: float32(i%7) / 10,
		}))
	}
}

// seedUnassignedFaces writes faces spread over photos, none of them assigned to
// a subject — the skew of a real library, where most faces are unnamed. Each
// sits inside the uncertainty band of the subjects' exemplars.
func (h *reviewHarness) seedUnassignedFaces(t *testing.T, photoCount, perPhoto int) {
	t.Helper()
	ctx := context.Background()
	for p := range photoCount {
		photoUID := h.photo(t, fmt.Sprintf("crowd-%04d", p))
		faces := make([]vectors.Face, 0, perPhoto)
		for i := range perPhoto {
			faces = append(faces, vectors.Face{
				FaceIndex: i,
				Vector:    vec(map[int]float32{0: 0.6, 1: 0.8, 2: float32(i) / 100}),
				DetScore:  0.9, BBox: reviewableBox,
				PhotoWidth: 1000, PhotoHeight: 800, Orientation: 1,
			})
		}
		if err := h.vectors.SaveFaces(ctx, photoUID, faces); err != nil {
			t.Fatalf("SaveFaces(%s): %v", photoUID, err)
		}
	}
}

func TestReviewQueue_workIsBoundedAtProductionScaleDB(t *testing.T) {
	h := newReviewHarness(t)
	start := time.Now()
	h.seedNamedSubjects(t, productionSubjects)
	h.seedUnassignedFaces(t, 200, 20) // 4 000 unassigned faces, the library's skew
	t.Logf("fixture: %d named subjects, 4000 unassigned faces over 200 photos, seeded in %s",
		productionSubjects, time.Since(start).Round(time.Millisecond))

	svc, faces := h.instrumentedService(review.Config{})
	begin := time.Now()
	res, err := svc.Queue(context.Background(), "tester", 0)
	elapsed := time.Since(begin)
	if err != nil {
		t.Fatalf("Queue: %v", err)
	}

	// Every subject here has exactly one exemplar, so one kNN per subject
	// scanned. The ceiling is the budget plus whatever the worker pool already
	// had in flight when the batch filled up — a constant, with no term for the
	// size of the library.
	ceiling := review.DefaultFaceBudget + sweep.DefaultConcurrency
	t.Logf("cold GET /review/queue: %d questions in %s behind %d kNN queries "+
		"(a full sweep of this library would run %d)",
		len(res.Questions), elapsed.Round(time.Millisecond), faces.queries(), productionSubjects)
	if got := faces.queries(); got > ceiling {
		t.Fatalf("kNN queries = %d, want at most %d — the queue must not scale with the "+
			"number of named subjects", got, ceiling)
	}
	if len(res.Questions) == 0 {
		t.Fatal("no questions: the fixture should put candidates inside the band")
	}
}

func TestReviewQueue_workDoesNotGrowWithTheLibraryDB(t *testing.T) {
	measure := func(t *testing.T, subjects int) int {
		t.Helper()
		h := newReviewHarness(t)
		h.seedNamedSubjects(t, subjects)
		h.seedUnassignedFaces(t, 50, 20)
		svc, faces := h.instrumentedService(review.Config{})
		if _, err := svc.Queue(context.Background(), "tester", 0); err != nil {
			t.Fatalf("Queue over %d subjects: %v", subjects, err)
		}
		return faces.queries()
	}
	small := measure(t, 10)
	large := measure(t, productionSubjects)
	t.Logf("kNN queries: %d over 10 subjects, %d over %d subjects", small, large, productionSubjects)
	ceiling := review.DefaultFaceBudget + sweep.DefaultConcurrency
	if large > ceiling {
		t.Fatalf("%d subjects cost %d kNN queries, want at most %d", productionSubjects, large, ceiling)
	}
}

func TestReviewQueue_boundedQueueMatchesAnUnboundedOneDB(t *testing.T) {
	h := newReviewHarness(t)
	// Small enough to enumerate: the budgets do not bite, so the bounded queue
	// must be byte-for-byte the queue an unbounded scan would have produced.
	h.seedNamedSubjects(t, 3)
	h.seedUnassignedFaces(t, 4, 3)
	alice := h.embedded(t, "lab-src", imgVec(map[int]float32{0: 1}))
	h.embedded(t, "lab-cand", imgVec(map[int]float32{0: 0.6, 1: 0.8}))
	h.label(t, "Ostatky", alice)

	bounded, _ := h.instrumentedService(review.Config{})
	unbounded, _ := h.instrumentedService(review.Config{
		FaceBudget: 1000, LabelBudget: 1000,
	})
	got, want := queueIDs(t, bounded), queueIDs(t, unbounded)
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("bounded queue differs from the unbounded one:\n bounded = %v\nunbounded = %v", got, want)
	}
	if len(want) == 0 {
		t.Fatal("the fixture produced no questions, so the comparison proves nothing")
	}
}
