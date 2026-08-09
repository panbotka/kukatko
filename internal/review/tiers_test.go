package review

// The tier-mix tests. The complaint they answer is that the game should mostly
// be a click on "yes": before this, every question came from the uncertainty
// band, so every question was a hard one by construction. What is measured here
// is the mix a batch actually lands on, that it holds in a prefix rather than
// only in aggregate, and that running out of one tier degrades to the other
// instead of emptying the queue.

import (
	"context"
	"fmt"
	"math"
	"strings"
	"testing"

	"github.com/panbotka/kukatko/internal/organize"
	"github.com/panbotka/kukatko/internal/sweep"
)

// tierTolerance is how far the measured confident share may sit from the
// configured one before the mix counts as wrong. It is deliberately loose: the
// share is a target for a *batch*, and a batch is assembled out of whole
// entities whose share is capped at MaxPerEntity, so the granularity of the
// rounding alone is worth several percent on a batch of twenty. Anything inside
// this band still gives the player a game that is mostly one-click yes with a
// real minority of hard questions, which is the property that matters.
const tierTolerance = 0.15

// tieredLibrary builds n subjects and n labels, each offering four confident
// candidates and four band ones — enough material on both sides that the mix is
// the blend's choice rather than a shortage.
func tieredLibrary(n int) func(*fixture) {
	return func(f *fixture) {
		for i := range n {
			subjectUID := fmt.Sprintf("subj%02d", i)
			f.sweeper.people = append(f.sweeper.people, scannedPerson(subjectUID,
				// Confidences 0.95/0.93/0.91/0.89 (confident) and
				// 0.70/0.68/0.66/0.64 (band).
				0.05, 0.07, 0.09, 0.11, 0.30, 0.32, 0.34, 0.36))
			labelUID := fmt.Sprintf("lab%02d", i)
			f.organize.labels = append(f.organize.labels, labelCount(labelUID, 10))
			f.expander.results[labelUID] = labelResult(labelUID,
				0.95, 0.93, 0.91, 0.89, 0.70, 0.68, 0.66, 0.64)
		}
	}
}

// shareOf returns the fraction of the questions drawn from the confident tier.
func shareOf(questions []Question) float64 {
	if len(questions) == 0 {
		return 0
	}
	sure, _ := tierCounts(questions)
	return float64(sure) / float64(len(questions))
}

func TestQueue_mixesTheTiersAtTheConfiguredShare(t *testing.T) {
	t.Parallel()
	f := newFixture(t, tieredLibrary(12))
	res, err := f.svc.Queue(context.Background(), "user", SourceBoth, 0)
	if err != nil {
		t.Fatalf("Queue: %v", err)
	}
	if len(res.Questions) != DefaultRoundSize {
		t.Fatalf("round = %d questions, want a full round of %d — a library with material in "+
			"both tiers must still fill one", len(res.Questions), DefaultRoundSize)
	}
	sure, band := tierCounts(res.Questions)
	got := shareOf(res.Questions)
	t.Logf("batch of %d: %d confident, %d band (share %.2f, want %.2f ±%.2f)",
		len(res.Questions), sure, band, got, DefaultSureShare, tierTolerance)
	if math.Abs(got-DefaultSureShare) > tierTolerance {
		t.Errorf("confident share = %.2f, want %.2f ±%.2f", got, DefaultSureShare, tierTolerance)
	}
	// The other half of the decision, and the one that is easy to lose by tuning:
	// the hard questions are load-bearing. A batch with none of them turns the
	// player into a rubber stamp who stops looking.
	if band == 0 {
		t.Error("no band questions at all — the minority of hard questions is the thing that " +
			"keeps the player reading the question")
	}
}

func TestQueue_theMixHoldsInThePrefixNotJustOverall(t *testing.T) {
	t.Parallel()
	// A batch is a prefix of the built queue, so a queue that is 70/30 across its
	// whole length but opens with ten band questions would hand the player
	// exactly the run of hard questions this change exists to remove.
	//
	// The bound is one-sided on purpose. The blend converges on the share from
	// above — it spends its confident allowance first — so a prefix leaning easy
	// is the intended behaviour and only a prefix leaning *hard* is the defect.
	f := newFixture(t, tieredLibrary(12))
	res, err := f.svc.Queue(context.Background(), "user", SourceBoth, maxBatch)
	if err != nil {
		t.Fatalf("Queue: %v", err)
	}
	if len(res.Questions) < 2*DefaultQueueSize {
		t.Fatalf("queue = %d questions, too short to say anything about prefixes",
			len(res.Questions))
	}
	for _, size := range []int{5, 10, 20, 40} {
		prefix := res.Questions[:min(size, len(res.Questions))]
		if got := shareOf(prefix); got < DefaultSureShare-tierTolerance {
			t.Errorf("first %d questions are only %.2f confident, want at least %.2f:\n%s",
				size, got, DefaultSureShare-tierTolerance, describe(prefix))
		}
	}
	// And across the whole queue the share must actually land on its target, not
	// merely stay above it — a queue that is 100 % easy is the rubber-stamp
	// failure mode.
	if got := shareOf(res.Questions); math.Abs(got-DefaultSureShare) > tierTolerance {
		t.Errorf("queue-wide confident share = %.2f, want %.2f ±%.2f",
			got, DefaultSureShare, tierTolerance)
	}
}

func TestQueue_sureShareIsConfigurable(t *testing.T) {
	t.Parallel()
	for _, share := range []float64{0.3, 0.5, 0.9} {
		t.Run(fmt.Sprintf("share_%.1f", share), func(t *testing.T) {
			t.Parallel()
			f := newFixture(t, func(f *fixture) {
				tieredLibrary(12)(f)
				f.sureShare = share
			})
			res, err := f.svc.Queue(context.Background(), "user", SourceBoth, 0)
			if err != nil {
				t.Fatalf("Queue: %v", err)
			}
			got := shareOf(res.Questions)
			if math.Abs(got-share) > tierTolerance {
				t.Errorf("confident share = %.2f, want the configured %.2f ±%.2f",
					got, share, tierTolerance)
			}
		})
	}
}

func TestQueue_exhaustedConfidentTierDegradesToTheBand(t *testing.T) {
	t.Parallel()
	// A library with nothing confident left — every candidate is uncertain. The
	// queue must not come back short or empty just because its majority tier has
	// run out; it fills from the band instead.
	f := newFixture(t, func(f *fixture) {
		for i := range 12 {
			f.sweeper.people = append(f.sweeper.people,
				scannedPerson(fmt.Sprintf("subj%02d", i), 0.30, 0.32, 0.34, 0.36))
		}
	})
	res, err := f.svc.Queue(context.Background(), "user", SourceBoth, 0)
	if err != nil {
		t.Fatalf("Queue: %v", err)
	}
	if len(res.Questions) != DefaultRoundSize {
		t.Fatalf("round = %d questions, want a full round of %d from the band alone",
			len(res.Questions), DefaultRoundSize)
	}
	if sure, _ := tierCounts(res.Questions); sure != 0 {
		t.Errorf("%d confident questions from a library that has none", sure)
	}
	if res.Reason != "" {
		t.Errorf("reason = %q on a full batch, want none", res.Reason)
	}
}

func TestQueue_exhaustedBandDegradesToTheConfidentTier(t *testing.T) {
	t.Parallel()
	// The mirror image: nothing uncertain is left, only easy questions. That is
	// what a well-worked library eventually looks like, and it must stay playable.
	f := newFixture(t, func(f *fixture) {
		for i := range 12 {
			f.sweeper.people = append(f.sweeper.people,
				scannedPerson(fmt.Sprintf("subj%02d", i), 0.05, 0.07, 0.09, 0.11))
		}
	})
	res, err := f.svc.Queue(context.Background(), "user", SourceBoth, 0)
	if err != nil {
		t.Fatalf("Queue: %v", err)
	}
	if len(res.Questions) != DefaultRoundSize {
		t.Fatalf("round = %d questions, want a full round of %d from the confident tier alone",
			len(res.Questions), DefaultRoundSize)
	}
	if _, band := tierCounts(res.Questions); band != 0 {
		t.Errorf("%d band questions from a library that has none", band)
	}
}

func TestQueue_emptyWindowRotatesInsteadOfReportingNothingToDo(t *testing.T) {
	t.Parallel()
	// Twelve subjects, only the last of which has anything to ask about. One
	// rebuild scans FaceBudget (8) of them, so the first window comes back empty
	// — and an empty *window* is not an empty library. The rebuild has to rotate
	// to the next one rather than telling the player there is nothing left.
	f := newFixture(t, func(f *fixture) {
		for i := range 11 {
			f.sweeper.people = append(f.sweeper.people, scannedPerson(fmt.Sprintf("subj%02d", i)))
		}
		f.sweeper.people = append(f.sweeper.people, scannedPerson("subj11", 0.4, 0.05))
	})
	res, err := f.svc.Queue(context.Background(), "user", SourceBoth, 0)
	if err != nil {
		t.Fatalf("Queue: %v", err)
	}
	if len(res.Questions) == 0 {
		t.Fatalf("empty queue with reason %q, want the questions from the second window — "+
			"a rebuild that stops at the first empty window ignores the rest of the library",
			res.Reason)
	}
	if f.sweeper.calls < 2 {
		t.Errorf("scans = %d, want at least 2 — the rebuild must rotate past an empty window",
			f.sweeper.calls)
	}
}

func TestQueue_rotationGivesUpOnAGenuinelyEmptyLibrary(t *testing.T) {
	t.Parallel()
	// The other side of the rotation: a library with no sources at all must not
	// be scanned maxRebuildRounds times over to learn what the first round
	// already proved.
	f := newFixture(t, nil)
	res, err := f.svc.Queue(context.Background(), "user", SourceBoth, 0)
	if err != nil {
		t.Fatalf("Queue: %v", err)
	}
	if res.Reason != ReasonNoSources {
		t.Errorf("reason = %q, want %q", res.Reason, ReasonNoSources)
	}
	if f.sweeper.calls != 1 {
		t.Errorf("scans = %d, want 1 — there is nothing to rotate through", f.sweeper.calls)
	}
}

func TestQueue_confidentTierIsOrderedSurestFirst(t *testing.T) {
	t.Parallel()
	// Within the confident tier the surest candidate is the cheapest yes, so it
	// comes first. Ranking it by distance from the band midpoint — the band's
	// rule — would put the *least* certain of them at the head, which is the
	// opposite of what the tier is for.
	f := newFixture(t, func(f *fixture) {
		f.sweeper.people = []*sweep.Person{scannedPerson("subj1", 0.18, 0.02, 0.10)}
	})
	res, err := f.svc.Queue(context.Background(), "user", SourceBoth, 0)
	if err != nil {
		t.Fatalf("Queue: %v", err)
	}
	got := make([]float64, 0, len(res.Questions))
	for _, q := range res.Questions {
		got = append(got, math.Round(q.Confidence*100)/100)
	}
	want := []float64{0.98, 0.90, 0.82}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("confident-tier order = %v, want %v (surest first)", got, want)
	}
}

func TestQueue_perEntityShareCountsBothTiersTogether(t *testing.T) {
	t.Parallel()
	// One subject with a full share of material in each tier. The batch-wide rule
	// is MaxPerEntity questions about one entity, not MaxPerEntity per tier —
	// otherwise the monotony fix quietly doubles its allowance.
	f := newFixture(t, func(f *fixture) {
		f.sweeper.people = []*sweep.Person{scannedPerson("subj1",
			0.05, 0.07, 0.09, 0.11, 0.13, 0.30, 0.32, 0.34, 0.36, 0.38)}
	})
	res, err := f.svc.Queue(context.Background(), "user", SourceBoth, maxBatch)
	if err != nil {
		t.Fatalf("Queue: %v", err)
	}
	if len(res.Questions) != DefaultMaxPerEntity {
		t.Errorf("one subject contributed %d questions, want at most its share of %d:\n%s",
			len(res.Questions), DefaultMaxPerEntity, describe(res.Questions))
	}
}

func TestQueue_reviewDisabledLabelIsNeitherAskedNorScanned(t *testing.T) {
	t.Parallel()
	f := newFixture(t, func(f *fixture) {
		off := labelCount("off", 50)
		off.ReviewEnabled = false
		f.organize.labels = []organize.LabelCount{off, labelCount("on", 50)}
		f.expander.results["off"] = labelResult("off", 0.6, 0.9)
		f.expander.results["on"] = labelResult("on", 0.6, 0.9)
	})
	res, err := f.svc.Queue(context.Background(), "user", SourceLabels, 0)
	if err != nil {
		t.Fatalf("Queue: %v", err)
	}
	if len(res.Questions) == 0 {
		t.Fatal("no questions at all, so the exclusion proves nothing")
	}
	for _, q := range res.Questions {
		if q.Label != nil && q.Label.UID == "off" {
			t.Errorf("a switched-off label produced a question: %+v", q)
		}
	}
	// Not scanning it is half the point: a label search is a per-member kNN
	// fan-out, so a label nobody wants questions about must not cost a rebuild
	// anything either.
	if f.expander.calls != 1 {
		t.Errorf("label searches = %d, want 1 — a switched-off label must not be searched",
			f.expander.calls)
	}
}

func TestBlend_shareHoldsInEveryPrefix(t *testing.T) {
	t.Parallel()
	mk := func(which tier, n int) []Question {
		out := make([]Question, n)
		for i := range out {
			out[i] = Question{ID: fmt.Sprintf("%s-%d", which, i), Tier: string(which)}
		}
		return out
	}
	tests := []struct {
		name        string
		sure, band  int
		share       float64
		wantPattern string
	}{
		{"seven in ten", 7, 3, 0.7, "sssbssbssb"},
		{"half and half", 5, 5, 0.5, "sbsbsbsbsb"},
		{"nine in ten", 9, 1, 0.9, "sssssssssb"},
		{"no confident material", 0, 4, 0.7, "bbbb"},
		{"no band material", 4, 0, 0.7, "ssss"},
		// The band runs out first: the confident remainder simply follows, rather
		// than the merge stopping at the ratio and dropping questions.
		{"more confident than the share wants", 10, 2, 0.7, "sssbssbsssss"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var pattern strings.Builder
			for _, q := range blend(mk(tierSure, tt.sure), mk(tierBand, tt.band), tt.share) {
				pattern.WriteString(q.Tier[:1])
			}
			if got := pattern.String(); got != tt.wantPattern {
				t.Errorf("blend(%d sure, %d band, %.1f) = %q, want %q",
					tt.sure, tt.band, tt.share, got, tt.wantPattern)
			}
		})
	}
}

func TestBlend_keepsEveryQuestion(t *testing.T) {
	t.Parallel()
	// Whatever the share, the blend is a merge and not a filter: no question may
	// be dropped on the way through, or a tier could vanish from the queue
	// without anything reporting it.
	mk := func(which tier, n int) []Question {
		out := make([]Question, n)
		for i := range out {
			out[i] = Question{ID: fmt.Sprintf("%s-%d", which, i), Tier: string(which)}
		}
		return out
	}
	for _, share := range []float64{0.1, 0.5, 0.7, 0.99} {
		merged := blend(mk(tierSure, 6), mk(tierBand, 4), share)
		if len(merged) != 10 {
			t.Errorf("blend at share %.2f returned %d questions, want all 10", share, len(merged))
		}
		seen := map[string]bool{}
		for _, q := range merged {
			if seen[q.ID] {
				t.Errorf("blend at share %.2f duplicated %q", share, q.ID)
			}
			seen[q.ID] = true
		}
	}
}

func TestCapEntities_share(t *testing.T) {
	t.Parallel()
	questions := []Question{
		labelQuestion("a", 1), labelQuestion("a", 2), labelQuestion("b", 1),
		labelQuestion("a", 3), labelQuestion("b", 2), labelQuestion("a", 4),
	}
	got := capEntities(questions, 2)
	want := []string{"a-1", "a-2", "b-1", "b-2"}
	if ids := idsOf(got); fmt.Sprint(ids) != fmt.Sprint(want) {
		t.Errorf("capEntities = %v, want %v (each entity's head, in order)", idsOf(got), want)
	}
	if all := capEntities(questions, 0); len(all) != len(questions) {
		t.Errorf("a non-positive share dropped %d questions, want none",
			len(questions)-len(all))
	}
}

func TestTierOf_boundaries(t *testing.T) {
	t.Parallel()
	svc := newFixture(t, nil).svc
	tests := []struct {
		confidence float64
		want       tier
		wantOK     bool
	}{
		{0.99, tierSure, true},
		{DefaultSureMin, tierSure, true},
		{0.78, "", false}, // the gap the default band_max/sure_min pair leaves
		{DefaultBandMax, "", false},
		{0.74, tierBand, true},
		{DefaultBandMin, tierBand, true},
		{0.44, "", false},
		{0, "", false},
	}
	for _, tt := range tests {
		got, ok := svc.tierOf(tt.confidence)
		if got != tt.want || ok != tt.wantOK {
			t.Errorf("tierOf(%v) = %q/%v, want %q/%v", tt.confidence, got, ok, tt.want, tt.wantOK)
		}
	}
}

func TestNew_tierFallbacks(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name                     string
		sureMin, sureShare       float64
		wantFloor, wantSureShare float64
	}{
		{"unset falls back", 0, 0, DefaultSureMin, DefaultSureShare},
		{"out of range falls back", 1.5, 1.0, DefaultSureMin, DefaultSureShare},
		{"negative falls back", -0.5, -1, DefaultSureMin, DefaultSureShare},
		// Below the band's ceiling the tiers would overlap and one candidate would
		// be asked about twice, so the floor is clamped up rather than honoured.
		{"below band_max is clamped up", 0.5, 0.4, DefaultBandMax, 0.4},
		{"honoured when sane", 0.9, 0.6, 0.9, 0.6},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			svc := New(Config{
				Sweeper: &fakeSweeper{}, Expander: &fakeExpander{}, Organize: &fakeOrganize{},
				Faces: &fakeFaces{}, Feedback: &fakeFeedback{}, Assigner: &fakeAssigner{},
				SureMin: tt.sureMin, SureShare: tt.sureShare,
			})
			if svc.sureFloor() != tt.wantFloor {
				t.Errorf("sureFloor() = %v, want %v", svc.sureFloor(), tt.wantFloor)
			}
			if svc.sureShare != tt.wantSureShare {
				t.Errorf("sureShare = %v, want %v", svc.sureShare, tt.wantSureShare)
			}
		})
	}
}
