package review

// The variety tests. The complaint they exist for is "sorting asked me about the
// same label twenty times in a row", so they are written around a measurement —
// longestEntityRun over the sequence the player actually sees — rather than
// around the shape of the code producing it. TestQueue_monotonyBaseline runs the
// pre-fix pipeline (order + interleave, no per-entity share, no run cap) over the
// same fixture and pins what it scores, so the improvement is a number and the
// fixture cannot quietly stop reproducing the problem.

import (
	"context"
	"fmt"
	"math"
	"testing"

	"github.com/panbotka/kukatko/internal/organize"
	"github.com/panbotka/kukatko/internal/sweep"
)

// nearMid returns n distinct confidences packed symmetrically around the band's
// midpoint at the given step — the shape of an entity with plenty of genuinely
// uncertain candidates. Every value stays strictly inside the band, and a
// smaller step means the entity sits closer to the decision boundary and so
// sorts ahead of everything else.
func nearMid(n int, step float64) []float64 {
	mid := (DefaultBandMin + DefaultBandMax) / 2
	out := make([]float64, 0, n)
	for i := range n {
		offset := float64(i/2+1) * step
		if i%2 == 1 {
			offset = -offset
		}
		out = append(out, mid+offset)
	}
	return out
}

// distancesOf converts confidences into the cosine distances scannedPerson takes.
func distancesOf(confidences []float64) []float64 {
	out := make([]float64, len(confidences))
	for i, conf := range confidences {
		out[i] = 1 - conf
	}
	return out
}

// monotonousLibrary is the shape that produced the complaint: one label that
// matches half the library and therefore hands back a whole batch worth of band
// candidates clustered right on the decision boundary, five thinner labels
// further out, and a face side with little to offer. Ordered by informativeness
// alone, the prolific label owns the head of the queue and the player answers
// about it over and over.
func monotonousLibrary(f *fixture) {
	f.sweeper.people = []*sweep.Person{
		scannedPerson("subj00", distancesOf(nearMid(2, 0.06))...),
		scannedPerson("subj01", distancesOf(nearMid(2, 0.10))...),
	}
	f.organize.labels = []organize.LabelCount{labelCount("lab00", 900)}
	f.expander.results["lab00"] = labelResult("lab00", nearMid(60, 0.0005)...)
	for i := 1; i < 6; i++ {
		uid := fmt.Sprintf("lab%02d", i)
		f.organize.labels = append(f.organize.labels, labelCount(uid, 20))
		f.expander.results[uid] = labelResult(uid, nearMid(4, 0.05)...)
	}
}

func TestQueue_monotonyBaseline(t *testing.T) {
	t.Parallel()
	// The pre-fix build: no per-entity share (MaxPerEntity out of reach) and no
	// spread — just informativeness plus the kind interleave.
	f := newFixture(t, func(f *fixture) {
		monotonousLibrary(f)
		f.perEntity = math.MaxInt32
	})
	mat, err := f.svc.collect(context.Background(), SourceBoth, DefaultQueueSize)
	if err != nil {
		t.Fatalf("collect: %v", err)
	}
	f.svc.orderQuestions(mat.faceQs)
	f.svc.orderQuestions(mat.labelQs)
	old := interleave(mat.faceQs, mat.labelQs)
	batch := old[:min(DefaultQueueSize, len(old))]
	run := longestEntityRun(batch)
	t.Logf("baseline: a batch of %d asks about %d entities, longest run %d\n%s",
		len(batch), countEntities(batch), run, describe(batch))
	// Half a batch about one entity is already the complaint; the fixture
	// currently scores far worse than that. If this ever stops holding, the
	// fixture no longer reproduces the defect and the tests below prove nothing.
	if run < DefaultQueueSize/2 {
		t.Fatalf("baseline longest run = %d, want at least %d — the fixture no longer "+
			"reproduces the monotony the variety rules exist for", run, DefaultQueueSize/2)
	}
}

func TestQueue_noEntityRepeatsMoreThanTheRunCap(t *testing.T) {
	t.Parallel()
	f := newFixture(t, monotonousLibrary)
	res, err := f.svc.Queue(context.Background(), "user", SourceBoth, 0)
	if err != nil {
		t.Fatalf("Queue: %v", err)
	}
	if len(res.Questions) != DefaultQueueSize {
		t.Fatalf("batch = %d questions, want a full batch of %d", len(res.Questions), DefaultQueueSize)
	}
	// The counterpart of the baseline's number, on the same fixture.
	t.Logf("spread: a batch of %d asks about %d entities, longest run %d\n%s",
		len(res.Questions), countEntities(res.Questions),
		longestEntityRun(res.Questions), describe(res.Questions))
	if got := longestEntityRun(res.Questions); got > maxSameEntityRun {
		t.Errorf("longest run of one entity = %d, want at most %d:\n%s",
			got, maxSameEntityRun, describe(res.Questions))
	}
	// A batch of 20 with a share of 4 has to come from at least five entities; the
	// run cap alone would be satisfied by ping-ponging between two of them.
	if want := DefaultQueueSize / DefaultMaxPerEntity; countEntities(res.Questions) < want {
		t.Errorf("batch asks about %d entities, want at least %d:\n%s",
			countEntities(res.Questions), want, describe(res.Questions))
	}
}

func TestQueue_noEntityOwnsMoreThanItsShare(t *testing.T) {
	t.Parallel()
	f := newFixture(t, monotonousLibrary)
	res, err := f.svc.Queue(context.Background(), "user", SourceBoth, maxBatch)
	if err != nil {
		t.Fatalf("Queue: %v", err)
	}
	counts := map[string]int{}
	for _, q := range res.Questions {
		counts[questionEntity(q)]++
	}
	if len(counts) == 0 {
		t.Fatal("empty queue, so the share assertion proves nothing")
	}
	for entity, n := range counts {
		if n > DefaultMaxPerEntity {
			t.Errorf("%s owns %d questions of the queue, want at most %d",
				entity, n, DefaultMaxPerEntity)
		}
	}
}

func TestQueue_varietyStaysInsideTheBand(t *testing.T) {
	t.Parallel()
	f := newFixture(t, func(f *fixture) {
		monotonousLibrary(f)
		// Candidates on both sides of the band mixed in with the uncertain ones:
		// variety must not be bought by asking about things the system is already
		// sure of, or ones it is only guessing at.
		f.sweeper.people = append(f.sweeper.people,
			scannedPerson("certain", 0.02, 0.05), scannedPerson("hopeless", 0.90, 0.95))
		f.expander.results["lab01"] = labelResult("lab01", 0.99, 0.98, 0.2, 0.6)
	})
	res, err := f.svc.Queue(context.Background(), "user", SourceBoth, maxBatch)
	if err != nil {
		t.Fatalf("Queue: %v", err)
	}
	if len(res.Questions) == 0 {
		t.Fatal("no questions at all, so the band assertion proves nothing")
	}
	for _, q := range res.Questions {
		if !f.svc.inBand(q.Confidence) {
			t.Errorf("question %s has confidence %v, outside the band [%v, %v)",
				q.ID, q.Confidence, DefaultBandMin, DefaultBandMax)
		}
	}
}

func TestQueue_spreadStaysDeterministic(t *testing.T) {
	t.Parallel()
	build := func() []string {
		f := newFixture(t, monotonousLibrary)
		res, err := f.svc.Queue(context.Background(), "user", SourceBoth, maxBatch)
		if err != nil {
			t.Fatalf("Queue: %v", err)
		}
		return idsOf(res.Questions)
	}
	first, second := build(), build()
	if fmt.Sprint(first) != fmt.Sprint(second) {
		t.Fatalf("the spread queue is not reproducible:\n first = %v\nsecond = %v", first, second)
	}
	if len(first) == 0 {
		t.Fatal("empty queue, so the determinism check proves nothing")
	}
}

func TestQueue_maxPerEntityIsConfigurable(t *testing.T) {
	t.Parallel()
	f := newFixture(t, func(f *fixture) {
		monotonousLibrary(f)
		f.perEntity = 1
	})
	res, err := f.svc.Queue(context.Background(), "user", SourceBoth, maxBatch)
	if err != nil {
		t.Fatalf("Queue: %v", err)
	}
	if len(res.Questions) != countEntities(res.Questions) {
		t.Fatalf("with a share of 1 the queue must ask about each entity once, got %d "+
			"questions over %d entities:\n%s",
			len(res.Questions), countEntities(res.Questions), describe(res.Questions))
	}
}

func TestSpread_keepsTheMostInformativeFirst(t *testing.T) {
	t.Parallel()
	// Informativeness order A1 A2 A3 B1 C1: the run cap may only move what it has
	// to, so A keeps the head and B is pulled forward exactly once.
	in := []Question{
		labelQuestion("A", 1), labelQuestion("A", 2), labelQuestion("A", 3),
		labelQuestion("B", 1), labelQuestion("C", 1),
	}
	got := idsOf(spread(in, 2))
	want := []string{"A-1", "A-2", "B-1", "A-3", "C-1"}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("spread = %v, want %v", got, want)
	}
}

func TestSpread_singleEntityStaysPlayable(t *testing.T) {
	t.Parallel()
	// A library with one named subject and nothing else: there is no other entity
	// to cut to, so the cap has to yield rather than drop questions on the floor.
	in := []Question{labelQuestion("A", 1), labelQuestion("A", 2), labelQuestion("A", 3)}
	got := idsOf(spread(in, 2))
	want := []string{"A-1", "A-2", "A-3"}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("spread = %v, want %v (a one-entity library must still be playable)", got, want)
	}
}

func TestSpread_edgeCases(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		in     []Question
		maxRun int
		want   []string
	}{
		{"empty", nil, 2, nil},
		{"non-positive cap is a no-op", []Question{
			labelQuestion("A", 1), labelQuestion("A", 2), labelQuestion("A", 3),
		}, 0, []string{"A-1", "A-2", "A-3"}},
		{"shorter than the cap", []Question{labelQuestion("A", 1)}, 2, []string{"A-1"}},
		{"strict alternation at cap 1", []Question{
			labelQuestion("A", 1), labelQuestion("A", 2), labelQuestion("B", 1), labelQuestion("B", 2),
		}, 1, []string{"A-1", "B-1", "A-2", "B-2"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := idsOf(spread(tt.in, tt.maxRun))
			if fmt.Sprint(got) != fmt.Sprint(tt.want) {
				t.Fatalf("spread = %v, want %v", got, tt.want)
			}
			if len(got) != len(tt.in) {
				t.Errorf("spread returned %d questions from %d — it reorders, it never drops",
					len(got), len(tt.in))
			}
		})
	}
}

func TestLongestEntityRun(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		in   []Question
		want int
	}{
		{"empty", nil, 0},
		{"single", []Question{labelQuestion("A", 1)}, 1},
		{"alternating", []Question{
			labelQuestion("A", 1), labelQuestion("B", 1), labelQuestion("A", 2),
		}, 1},
		{"a run in the middle", []Question{
			labelQuestion("A", 1), labelQuestion("B", 1), labelQuestion("B", 2),
			labelQuestion("B", 3), labelQuestion("C", 1),
		}, 3},
		{"a run at the tail", []Question{
			labelQuestion("A", 1), labelQuestion("C", 1), labelQuestion("C", 2),
		}, 2},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := longestEntityRun(tt.in); got != tt.want {
				t.Fatalf("longestEntityRun = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestQuestionEntity_kindsNeverCollide(t *testing.T) {
	t.Parallel()
	// A subject and a label may hold the same uid; they are not the same entity.
	face, label := faceQuestionFor("shared"), labelQuestion("shared", 1)
	if questionEntity(face) == questionEntity(label) {
		t.Fatalf("a face and a label with uid %q share the entity key %q",
			"shared", questionEntity(face))
	}
	if got := countEntities([]Question{face, label, labelQuestion("shared", 2)}); got != 2 {
		t.Errorf("countEntities = %d, want 2", got)
	}
}

func TestCapQueue_backstop(t *testing.T) {
	t.Parallel()
	// The per-entity share keeps a real queue far below this, but the memory
	// bound still has to hold whatever MaxPerEntity is configured to.
	long := make([]Question, 3*maxQueued)
	for i := range long {
		long[i] = labelQuestion(fmt.Sprintf("lab%04d", i), i)
	}
	if got := len(capQueue(long)); got != maxQueued {
		t.Errorf("capQueue kept %d questions, want %d", got, maxQueued)
	}
	if got := capQueue(long[:3]); len(got) != 3 {
		t.Errorf("capQueue trimmed a short queue to %d, want 3", len(got))
	}
}

// labelQuestion builds a minimal label question for the ordering helpers, with
// an id that reads as "<label>-<n>" in a failure message.
func labelQuestion(labelUID string, n int) Question {
	label := labelCount(labelUID, 1).Label
	return Question{ID: fmt.Sprintf("%s-%d", labelUID, n), Kind: KindLabel, Label: &label}
}

// faceQuestionFor builds a minimal face question about the given subject.
func faceQuestionFor(subjectUID string) Question {
	subject := scannedPerson(subjectUID, 0.4).Subject
	return Question{ID: "face-" + subjectUID, Kind: KindFace, Subject: &subject}
}

// idsOf returns the questions' ids, the readable form for an order assertion.
func idsOf(questions []Question) []string {
	if questions == nil {
		return nil
	}
	out := make([]string, 0, len(questions))
	for _, q := range questions {
		out = append(out, q.ID)
	}
	return out
}

// describe renders a sequence as its entities, which is how a monotony failure
// has to be read.
func describe(questions []Question) string {
	return fmt.Sprint(idsOf(questions)) + "\nentities: " + fmt.Sprint(entitiesOf(questions))
}

// entitiesOf returns the entity of each question, in order.
func entitiesOf(questions []Question) []string {
	out := make([]string, 0, len(questions))
	for _, q := range questions {
		out = append(out, questionEntity(q))
	}
	return out
}
