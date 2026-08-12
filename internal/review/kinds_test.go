package review

import (
	"fmt"
	"testing"
)

// TestNewKindShares_defaultsToFacesAlone pins the shape of the default: the game
// is about people, so a Service built without a configured mix asks about
// nothing else. Every other kind being off is what pays for the wider face scan
// and what makes the empty-queue reason exact.
func TestNewKindShares_defaultsToFacesAlone(t *testing.T) {
	t.Parallel()
	for _, raw := range []map[Kind]float64{nil, {}, {KindFace: 0, KindLabel: 0}} {
		shares := newKindShares(raw)
		if !shares.only(KindFace) {
			t.Errorf("shares from %v = %v, want faces alone", raw, shares)
		}
		if got := shares.share(KindFace); got != 1 {
			t.Errorf("face share = %v, want the whole game", got)
		}
	}
}

// TestNewKindShares_zeroSwitchesAKindOff covers the switch every other rule
// keys on: a kind at zero is not enabled, so it is never scanned, never asked
// and never counted as a source.
func TestNewKindShares_zeroSwitchesAKindOff(t *testing.T) {
	t.Parallel()
	shares := newKindShares(map[Kind]float64{
		KindFace: 0.95, KindLabel: 0.05, KindPlace: 0, KindDuplicate: -1,
	})
	for _, kind := range []Kind{KindPlace, KindDuplicate, KindOutlier} {
		if shares.enabled(kind) {
			t.Errorf("%s is enabled at share %v, want switched off", kind, shares.share(kind))
		}
	}
	if !shares.enabled(KindFace) || !shares.enabled(KindLabel) {
		t.Errorf("shares = %v, want faces and labels on", shares)
	}
}

// TestNewKindShares_normalisesRelativeWeights checks that only the ratio
// matters, so an operator can write 19/1 or 0.95/0.05 and mean the same thing.
func TestNewKindShares_normalisesRelativeWeights(t *testing.T) {
	t.Parallel()
	weights := newKindShares(map[Kind]float64{KindFace: 19, KindLabel: 1})
	fractions := newKindShares(map[Kind]float64{KindFace: 0.95, KindLabel: 0.05})
	for _, kind := range Kinds {
		if weights.share(kind) != fractions.share(kind) {
			t.Errorf("%s: 19/1 gives %v, 0.95/0.05 gives %v — the ratio is the configuration",
				kind, weights.share(kind), fractions.share(kind))
		}
	}
}

// TestKindShares_wantedFollowsTheConfiguredMix drives the selection rule itself:
// filling a hundred slots from a 95/5 game must ask about labels about five
// times, and never in a block.
func TestKindShares_wantedFollowsTheConfiguredMix(t *testing.T) {
	t.Parallel()
	shares := newKindShares(map[Kind]float64{KindFace: 0.95, KindLabel: 0.05})
	placed := make(map[Kind]int)
	for slot := range 100 {
		placed[shares.wanted(placed, slot, "")]++
	}
	if placed[KindLabel] != 5 {
		t.Errorf("labels = %d of 100, want 5 — the share is the configuration",
			placed[KindLabel])
	}
	if placed[KindFace] != 95 {
		t.Errorf("faces = %d of 100, want 95", placed[KindFace])
	}
}

// TestKindShares_wantedIsAllFacesAtTheDefault is the same rule read at the
// default: a game about people never wants anything else, whatever a stale pool
// happens to still hold.
func TestKindShares_wantedIsAllFacesAtTheDefault(t *testing.T) {
	t.Parallel()
	shares := defaultKindShares()
	placed := make(map[Kind]int)
	for slot := range 20 {
		if got := shares.wanted(placed, slot, KindLabel); got != KindFace {
			t.Fatalf("slot %d wanted %s, want %s — a share of zero is not a preference",
				slot, got, KindFace)
		}
		placed[KindFace]++
	}
}

// TestKindShares_presentInIgnoresKindsThePoolLacks pins the narrowing: a share
// reserved for a kind with no material must not pace the kinds that have some,
// or a pool of faces alone would be held back for questions that do not exist.
func TestKindShares_presentInIgnoresKindsThePoolLacks(t *testing.T) {
	t.Parallel()
	shares := newKindShares(map[Kind]float64{KindFace: 0.5, KindLabel: 0.5})
	pool := []Question{faceQ("anna", 0, tierSure), faceQ("bara", 1, tierSure)}
	narrowed := shares.presentIn(pool)
	if !narrowed.only(KindFace) {
		t.Fatalf("narrowed = %v, want faces alone", narrowed)
	}
	if got := narrowed.share(KindFace); got != 1 {
		t.Errorf("face share over a face-only pool = %v, want the whole of it", got)
	}
}

// TestMixRound_runLimitHoldsWhenOneSubjectDominates is the regression the whole
// change exists for. The pool is what a rebuild produced on the live library:
// one person with more material than everybody else put together, which is not
// the same as a pool with only one person in it.
//
// Pricing the run rule was not enough there. The price sat below the round's
// per-entity ceiling, so the moment every entity had taken its three questions the
// cheapest candidate was whichever also continued a run — and the player was
// asked about one person five times running. Refusing such a candidate is what
// makes the limit hold, and it holds wherever the crowd sits in the pool order.
func TestMixRound_runLimitHoldsWhenOneSubjectDominates(t *testing.T) {
	t.Parallel()
	for _, annaFirst := range []bool{true, false} {
		t.Run(fmt.Sprintf("anna_first_%v", annaFirst), func(t *testing.T) {
			t.Parallel()
			anna := make([]Question, 0, 16)
			for i := range 16 {
				anna = append(anna, faceQ("anna", i, tierSure))
			}
			others := make([]Question, 0, 10)
			for _, subject := range []string{"bara", "cyril"} {
				for i := range 5 {
					others = append(others, faceQ(subject, i, tierBand))
				}
			}
			pool := append(append([]Question{}, anna...), others...)
			if !annaFirst {
				pool = append(append([]Question{}, others...), anna...)
			}
			round, _ := mixRound(pool, testMix(DefaultRoundSize), nil)
			if len(round) != DefaultRoundSize {
				t.Fatalf("round = %d questions, want a full %d — the pool has the material",
					len(round), DefaultRoundSize)
			}
			if got := longestEntityRun(round); got > maxSameEntityRun {
				t.Errorf("longest entity run = %d, want at most %d: %s",
					got, maxSameEntityRun, mixEntities(round))
			}
		})
	}
}

// TestMixRound_shareKeepsTheRoundAboutPeople drives the kind rule through a
// whole round: a 95/5 game handed a pool with plenty of both must still be a
// game about people.
func TestMixRound_shareKeepsTheRoundAboutPeople(t *testing.T) {
	t.Parallel()
	pool := make([]Question, 0, 20)
	for i := range 10 {
		pool = append(pool, faceQ(fmt.Sprintf("subj%d", i), i, tierSure))
		pool = append(pool, labelQ(fmt.Sprintf("lab%d", i), i, tierSure))
	}
	cfg := testMix(DefaultRoundSize)
	cfg.Shares = newKindShares(map[Kind]float64{KindFace: 0.95, KindLabel: 0.05})
	round, _ := mixRound(pool, cfg, nil)
	labels := 0
	for _, q := range round {
		if q.Kind == KindLabel {
			labels++
		}
	}
	if labels > 1 {
		t.Errorf("round of %d holds %d label questions, want at most one at a 5 %% share: %s",
			len(round), labels, kindsOf(round))
	}
}

// TestMixRound_shareOfZeroIsNeverAsked is the same rule at its edge: a kind the
// operator switched off must lose to any kind that is on, however good its
// candidates look. It cannot normally reach a pool at all, but a pool built
// before the config changed still holds one.
func TestMixRound_shareOfZeroIsNeverAsked(t *testing.T) {
	t.Parallel()
	pool := []Question{
		labelQ("lab0", 0, tierSure),
		labelQ("lab1", 1, tierSure),
		faceQ("anna", 0, tierBand),
		faceQ("bara", 1, tierBand),
	}
	cfg := testMix(2)
	cfg.Shares = defaultKindShares()
	round, _ := mixRound(pool, cfg, nil)
	for _, q := range round {
		if q.Kind != KindFace {
			t.Errorf("round asked a %s question at a share of zero: %s", q.Kind, kindsOf(round))
		}
	}
}
