package review

// The bounded-work tests. GET /review/queue used to run the whole recognition
// sweep inside the request — on the production library (105 named subjects,
// 113 628 faces) that was 250 s, so the page never opened. The regression these
// tests guard is structural, not temporal: the work behind one queue rebuild
// must not grow with the number of named subjects. A wall-clock assertion would
// be flaky on a shared machine; a query count is exact.

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/panbotka/kukatko/internal/candidates"
	"github.com/panbotka/kukatko/internal/expand"
	"github.com/panbotka/kukatko/internal/organize"
	"github.com/panbotka/kukatko/internal/people"
	"github.com/panbotka/kukatko/internal/photos"
	"github.com/panbotka/kukatko/internal/sweep"
)

// exemplarsPerSubject is the production library's ratio: 113 628 faces spread
// over 105 named subjects is roughly 240 exemplar kNN queries per subject, each
// measured at ~40 ms against the real HNSW index.
const exemplarsPerSubject = 240

// knnFinder stands in for the per-subject candidate search over a synthetic
// library, counting the per-exemplar kNN queries each subject's search would
// run. *candidates.Service is the real thing; what matters here is how many
// times the sweep asks for one.
type knnFinder struct {
	mu sync.Mutex
	// inBand is how many uncertainty-band candidates each subject yields.
	inBand int
	// subjects counts Find calls; knn counts the exemplar searches behind them.
	subjects int
	knn      int
}

// Find records the work one subject's search would cost and returns inBand
// candidates sitting in the middle of the uncertainty band.
func (f *knnFinder) Find(
	_ context.Context, subjectUID string, _ candidates.Request,
) (candidates.Result, error) {
	f.mu.Lock()
	f.subjects++
	f.knn += exemplarsPerSubject
	f.mu.Unlock()
	res := candidates.Result{SubjectUID: subjectUID}
	for i := range f.inBand {
		res.Candidates = append(res.Candidates, candidates.Candidate{
			Photo:     photos.Photo{UID: fmt.Sprintf("photo-%s-%d", subjectUID, i)},
			FaceIndex: i,
			Distance:  0.4, // confidence 0.60 — the middle of the default band
			Action:    candidates.ActionCreateMarker,
		})
	}
	return res, nil
}

// counts returns the finder's tallies under its lock.
func (f *knnFinder) counts() (subjects, knn int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.subjects, f.knn
}

// staticSubjects lists a fixed synthetic set of named subjects.
type staticSubjects struct {
	subjects []people.SubjectCount
}

// ListSubjects returns the synthetic subject list.
func (s *staticSubjects) ListSubjects(context.Context) ([]people.SubjectCount, error) {
	return s.subjects, nil
}

// libraryOf builds a review service over a synthetic library of n named
// subjects, wired to the real sweep service so the bound under test is the
// production one, not a test double's.
func libraryOf(t *testing.T, n int) (*Service, *knnFinder) {
	t.Helper()
	lister := &staticSubjects{}
	for i := range n {
		uid := fmt.Sprintf("subj%04d", i)
		lister.subjects = append(lister.subjects, people.SubjectCount{
			Subject: people.Subject{UID: uid, Name: "Person " + uid}, MarkerCount: 4,
		})
	}
	finder := &knnFinder{inBand: 3}
	quiet := slog.New(slog.NewTextHandler(io.Discard, nil))
	svc := New(Config{
		Sweeper: sweep.New(sweep.Config{
			Subjects: lister, Finder: finder, Concurrency: 4, Log: quiet,
		}),
		Expander: &fakeExpander{results: map[string]expand.Result{}, errs: map[string]error{}},
		Organize: &fakeOrganize{},
		Faces:    &fakeFaces{},
		Feedback: &fakeFeedback{},
		Assigner: &fakeAssigner{},
		Log:      quiet,
	})
	return svc, finder
}

func TestQueue_faceWorkDoesNotScaleWithSubjectCount(t *testing.T) {
	t.Parallel()
	// The ceiling one rebuild may cost, plus the subjects the worker pool may
	// already have in flight when the batch fills up. It is a constant: nothing
	// in it mentions the size of the library.
	maxSubjects := DefaultFaceBudget + sweep.DefaultConcurrency
	for _, size := range []int{10, 105, 1000} {
		t.Run(fmt.Sprintf("%d_named_subjects", size), func(t *testing.T) {
			t.Parallel()
			svc, finder := libraryOf(t, size)
			res, err := svc.Queue(context.Background(), "user", SourceBoth, 0)
			if err != nil {
				t.Fatalf("Queue: %v", err)
			}
			if len(res.Questions) != DefaultRoundSize {
				t.Fatalf("questions = %d, want a full round of %d", len(res.Questions), DefaultRoundSize)
			}
			// The round is mixed from a pool the same bounded scan filled, so the
			// pool has to be full too — a short pool would mean the bound cut the
			// rebuild short rather than the round merely being shorter than it.
			if res.Remaining < DefaultQueueSize {
				t.Errorf("pool = %d questions, want at least the %d behind the round",
					res.Remaining, DefaultQueueSize)
			}
			subjects, knn := finder.counts()
			if subjects > maxSubjects {
				t.Errorf("scanned %d subjects, want at most %d — the bound must not follow the library",
					subjects, maxSubjects)
			}
			if want := maxSubjects * exemplarsPerSubject; knn > want {
				t.Errorf("ran %d kNN queries, want at most %d (a full sweep would be %d)",
					knn, want, size*exemplarsPerSubject)
			}
		})
	}
}

func TestQueue_rotatesThroughSubjectsAcrossRebuilds(t *testing.T) {
	t.Parallel()
	// A budget small enough that it, rather than the pool filling up, is what
	// ends each window: the assertion is about the cursor, and a scan that
	// stopped early because it had enough would move it by a different amount.
	const budget = 5
	f := newFixture(t, func(f *fixture) {
		f.faceBudget = budget
		for i := range 20 {
			f.sweeper.people = append(f.sweeper.people, scannedPerson(fmt.Sprintf("subj%02d", i), 0.4))
		}
	})
	ctx := context.Background()
	offsets := make([]int, 0, 3)
	for range 3 {
		if _, err := f.svc.Queue(ctx, "user", SourceBoth, 0); err != nil {
			t.Fatalf("Queue: %v", err)
		}
		*f.now = f.now.Add(2 * DefaultCacheTTL) // force a rebuild
		offsets = append(offsets, f.sweeper.windows[len(f.sweeper.windows)-1].Offset)
	}
	if fmt.Sprint(offsets) != fmt.Sprint([]int{0, budget, 2 * budget}) {
		t.Fatalf("window offsets = %v, want successive windows — a bounded scan that never "+
			"rotates would ignore the rest of the library forever", offsets)
	}
	if f.sweeper.visited != 3*budget {
		t.Errorf("subjects visited = %d, want %d (%d per rebuild)",
			f.sweeper.visited, 3*budget, budget)
	}
}

func TestQueue_rotatesThroughLabelsAcrossRebuilds(t *testing.T) {
	t.Parallel()
	f := newFixture(t, func(f *fixture) {
		for i := range 20 {
			uid := fmt.Sprintf("lab%02d", i)
			f.organize.labels = append(f.organize.labels, labelCount(uid, 2))
			f.expander.results[uid] = labelResult(uid, 0.6)
		}
	})
	ctx := context.Background()
	for range 2 {
		if _, err := f.svc.Queue(ctx, "user", SourceBoth, 0); err != nil {
			t.Fatalf("Queue: %v", err)
		}
		*f.now = f.now.Add(2 * DefaultCacheTTL)
	}
	// Two rebuilds, each capped at LabelBudget labels; without the bound both
	// would have searched all 20.
	if f.expander.calls != 2*DefaultLabelBudget {
		t.Fatalf("label searches = %d, want %d (%d per rebuild over 20 labels)",
			f.expander.calls, 2*DefaultLabelBudget, DefaultLabelBudget)
	}
	if f.svc.labelOffset() != 2*DefaultLabelBudget {
		t.Errorf("label cursor = %d, want %d — successive rebuilds must move on",
			f.svc.labelOffset(), 2*DefaultLabelBudget)
	}
}

func TestQueue_labelScanStopsOnceTheBatchIsFull(t *testing.T) {
	t.Parallel()
	f := newFixture(t, func(f *fixture) {
		for i := range 6 {
			uid := fmt.Sprintf("lab%02d", i)
			f.organize.labels = append(f.organize.labels, labelCount(uid, 2))
			// 20 band candidates each, of which the per-entity share takes four.
			sims := make([]float64, 20)
			for j := range sims {
				sims[j] = 0.6
			}
			f.expander.results[uid] = labelResult(uid, sims...)
		}
		// The scan fills the *pool*, not the round, so the pool target is what
		// decides when it has enough: two labels' worth. Asking for a bigger one
		// would (rightly) walk further — a pool of 20 that may take only 4
		// questions per label cannot be filled from two labels.
		f.queueSize = DefaultLabelConcurrency * DefaultMaxPerEntity
		// The pool must also be big enough for the round, so the round is sized
		// down with it — otherwise the round's size would raise the target back up.
		f.roundSize = f.queueSize
	})
	if _, err := f.svc.Queue(context.Background(), "user", SourceBoth, 0); err != nil {
		t.Fatalf("Queue: %v", err)
	}
	// LabelConcurrency is 2, so the scan runs one chunk and stops: it must not
	// walk the rest of the budget once the batch is already full.
	if f.expander.calls != DefaultLabelConcurrency {
		t.Fatalf("label searches = %d, want %d (one chunk, then enough)",
			f.expander.calls, DefaultLabelConcurrency)
	}
}

func TestQueue_labelScanSpendsItsBudgetOnVariety(t *testing.T) {
	t.Parallel()
	// The other half of the trade: one label can no longer end the scan on its
	// own, so filling a full batch costs more label searches than it used to —
	// bounded, as always, by LabelBudget.
	f := newFixture(t, func(f *fixture) {
		for i := range 10 {
			uid := fmt.Sprintf("lab%02d", i)
			f.organize.labels = append(f.organize.labels, labelCount(uid, 2))
			sims := make([]float64, 20)
			for j := range sims {
				sims[j] = 0.6
			}
			f.expander.results[uid] = labelResult(uid, sims...)
		}
	})
	res, err := f.svc.Queue(context.Background(), "user", SourceBoth, 0)
	if err != nil {
		t.Fatalf("Queue: %v", err)
	}
	if f.expander.calls > DefaultLabelBudget {
		t.Fatalf("label searches = %d, want at most the budget of %d",
			f.expander.calls, DefaultLabelBudget)
	}
	if want := DefaultQueueSize / DefaultMaxPerEntity; f.expander.calls < want {
		t.Errorf("label searches = %d, want at least %d — a batch of %d that takes %d "+
			"questions per label cannot come from fewer",
			f.expander.calls, want, DefaultQueueSize, DefaultMaxPerEntity)
	}
	if got := longestEntityRun(res.Questions); got > maxSameEntityRun {
		t.Errorf("longest run of one label = %d, want at most %d", got, maxSameEntityRun)
	}
}

func TestQueue_rebuildsWhenTheQueueRunsDry(t *testing.T) {
	t.Parallel()
	f := newFixture(t, nil) // no sources at all: every rebuild yields an empty queue
	ctx := context.Background()
	for range 3 {
		if _, err := f.svc.Queue(ctx, "user", SourceBoth, 0); err != nil {
			t.Fatalf("Queue: %v", err)
		}
	}
	// A dry queue must not be cached for the whole TTL: the next request has to
	// be able to scan the next window instead of waiting the cache out.
	if f.sweeper.calls != 3 {
		t.Fatalf("scans for three requests on a dry queue = %d, want 3", f.sweeper.calls)
	}
}

func TestQueue_buildDeadlineServesWhatItHas(t *testing.T) {
	t.Parallel()
	f := newFixture(t, func(f *fixture) {
		f.sweeper.err = context.DeadlineExceeded
		f.organize.labels = []organize.LabelCount{labelCount("lab1", 2)}
		f.expander.results["lab1"] = labelResult("lab1", 0.6)
	})
	res, err := f.svc.Queue(context.Background(), "user", SourceBoth, 0)
	if err != nil {
		t.Fatalf("Queue with a face scan that ran out of time: %v", err)
	}
	if len(res.Questions) != 1 || res.Questions[0].Kind != KindLabel {
		t.Fatalf("questions = %+v, want the one label question the other source still produced",
			res.Questions)
	}
}

func TestQueue_buildDeadlineNeverReportsNoSources(t *testing.T) {
	t.Parallel()
	f := newFixture(t, func(f *fixture) { f.sweeper.err = context.DeadlineExceeded })
	res, err := f.svc.Queue(context.Background(), "user", SourceBoth, 0)
	if err != nil {
		t.Fatalf("Queue: %v", err)
	}
	// The scan never got far enough to know whether the library has people, so
	// it must not claim there are none.
	if res.Reason != ReasonNoCandidates {
		t.Fatalf("reason = %q, want %q — a timed-out scan cannot prove the library is empty",
			res.Reason, ReasonNoCandidates)
	}
}

func TestQueue_clientCancellationStillFails(t *testing.T) {
	t.Parallel()
	f := newFixture(t, func(f *fixture) { f.sweeper.err = context.DeadlineExceeded })
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := f.svc.Queue(ctx, "user", SourceBoth, 0); err == nil {
		t.Fatal("Queue with a cancelled caller: want an error, got nil")
	}
}

func TestNew_boundedDefaults(t *testing.T) {
	t.Parallel()
	svc := New(Config{
		Sweeper: &fakeSweeper{}, Expander: &fakeExpander{}, Organize: &fakeOrganize{},
		Faces: &fakeFaces{}, Feedback: &fakeFeedback{}, Assigner: &fakeAssigner{},
		FaceBudget: -1, LabelBudget: 0, BuildTimeout: -time.Second,
	})
	if svc.faceBudget != DefaultFaceBudget || svc.labelBudget != DefaultLabelBudget {
		t.Errorf("budgets = %d/%d, want the defaults %d/%d",
			svc.faceBudget, svc.labelBudget, DefaultFaceBudget, DefaultLabelBudget)
	}
	if svc.buildTimeout != DefaultBuildTimeout {
		t.Errorf("buildTimeout = %s, want the default %s", svc.buildTimeout, DefaultBuildTimeout)
	}
}

func TestQueue_faceScanAsksForBothTiers(t *testing.T) {
	t.Parallel()
	f := newFixture(t, func(f *fixture) {
		f.sweeper.people = append(f.sweeper.people, scannedPerson("subj01", 0.4))
	})
	if _, err := f.svc.Queue(context.Background(), "user", SourceBoth, 0); err != nil {
		t.Fatalf("Queue: %v", err)
	}
	if len(f.sweeper.params) != 1 {
		t.Fatalf("scan calls = %d, want 1 — asking for both tiers must not cost a second scan",
			len(f.sweeper.params))
	}
	// The window runs from the confident tier all the way down to BandMin, so one
	// scan serves both tiers. The MinDistance floor that used to cut it at the
	// band's far edge is deliberately gone: what keeps a rebuild off the
	// library's growth curve is candidates' MaxCandidates cut *before* hydration,
	// not this floor (see internal/candidates/memory_test.go, and the 10.9 GB
	// OOM that put the floor there in the first place).
	got := f.sweeper.params[0]
	if got.Threshold != 1-DefaultBandMin || got.MinDistance != 0 {
		t.Errorf("scan params = threshold %v, min distance %v; want %v and 0 — one scan has to "+
			"cover the confident tier and the band alike",
			got.Threshold, got.MinDistance, 1-DefaultBandMin)
	}
}

func TestQueue_cachedQueueIsCapped(t *testing.T) {
	t.Parallel()
	// One subject offering far more band candidates than a session has any use
	// for. Each question carries the whole photo record it asks about, and the
	// queue is cached per user until the session is pruned, so the cache is a
	// retention bound as much as the searches behind it are an allocation one.
	// The per-entity share is now the tighter of the two bounds — one subject
	// gets four questions, not five hundred — and capQueue stays the backstop
	// for a wide MaxPerEntity (TestCapQueue_backstop covers it directly).
	confidences := make([]float64, 4*maxQueued)
	for i := range confidences {
		confidences[i] = 0.4
	}
	f := newFixture(t, func(f *fixture) {
		f.sweeper.people = append(f.sweeper.people, scannedPerson("subj01", confidences...))
	})
	res, err := f.svc.Queue(context.Background(), "user", SourceBoth, 0)
	if err != nil {
		t.Fatalf("Queue: %v", err)
	}
	if res.Remaining != DefaultMaxPerEntity {
		t.Errorf("cached queue = %d questions from a single subject, want its share of %d",
			res.Remaining, DefaultMaxPerEntity)
	}
	if len(res.Questions) != res.Remaining {
		t.Errorf("batch = %d questions, want the whole %d-question queue",
			len(res.Questions), res.Remaining)
	}
}
