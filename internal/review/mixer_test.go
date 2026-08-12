package review

// The mixer's tests drive mixRound directly rather than through Queue. That is
// the point of having made it a pure function: a variety rule is a statement
// about a sequence, and asserting it needs a pool, a config and nothing else —
// no fakes, no clock, no session.

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/panbotka/kukatko/internal/organize"
	"github.com/panbotka/kukatko/internal/people"
	"github.com/panbotka/kukatko/internal/photos"
)

// testMix is the config the tests mix with unless they say otherwise: the
// package defaults, expressed as the mixer sees them.
func testMix(size int) mixConfig {
	return mixConfig{
		RoundSize:   size,
		MaxPerRound: DefaultRoundMaxPerEntity,
		MaxRun:      maxSameEntityRun,
		SureShare:   DefaultSureShare,
		Shares:      newKindShares(allKinds()),
	}
}

// faceQ builds a face question about subject, on its own photo, in the given
// tier. The photo uid carries the index so every question has a distinct photo,
// which keeps the photo-spread rules out of the way of tests that are not about
// them.
func faceQ(subject string, index int, which tier) Question {
	subj := people.Subject{UID: subject, Name: "Person " + subject}
	return Question{
		ID:      faceQuestionID(fmt.Sprintf("photo-%s-%d", subject, index), index, subject),
		Kind:    KindFace,
		Tier:    string(which),
		Photo:   photos.Photo{UID: fmt.Sprintf("photo-%s-%d", subject, index)},
		Subject: &subj,
	}
}

// labelQ builds a label question about label, on its own photo, in the given
// tier.
func labelQ(label string, index int, which tier) Question {
	lab := organize.Label{UID: label, Name: "Label " + label}
	return Question{
		ID:    labelQuestionID(fmt.Sprintf("photo-%s-%d", label, index), label),
		Kind:  KindLabel,
		Tier:  string(which),
		Photo: photos.Photo{UID: fmt.Sprintf("photo-%s-%d", label, index)},
		Label: &lab,
	}
}

// placeQ builds a place question about a named place, on its own photo. Place
// questions carry no tier, which is what makes them useful for asserting that
// the tier rule leaves the untiered kinds alone.
func placeQ(name string, index int) Question {
	uid := fmt.Sprintf("photo-%s-%d", name, index)
	return Question{
		ID:    placeQuestionID(uid),
		Kind:  KindPlace,
		Photo: photos.Photo{UID: uid},
		Place: &PlaceGuess{Name: name},
	}
}

// mixEntities renders a round's entities in order, for readable failures.
func mixEntities(round []Question) string {
	out := make([]string, 0, len(round))
	for _, q := range round {
		out = append(out, questionEntity(q))
	}
	return strings.Join(out, " ")
}

// kindsOf renders a round's kinds in order.
func kindsOf(round []Question) string {
	out := make([]string, 0, len(round))
	for _, q := range round {
		out = append(out, string(q.Kind))
	}
	return strings.Join(out, " ")
}

// tiersOf renders a round's tiers in order.
func tiersOf(round []Question) string {
	out := make([]string, 0, len(round))
	for _, q := range round {
		if q.Tier == "" {
			out = append(out, "-")
			continue
		}
		out = append(out, q.Tier)
	}
	return strings.Join(out, " ")
}

// mixIDs renders a sequence's question ids, for order comparisons.
func mixIDs(questions []Question) string {
	out := make([]string, 0, len(questions))
	for _, q := range questions {
		out = append(out, q.ID)
	}
	return strings.Join(out, " ")
}

func TestMixRound_neverThreeInARowAboutOneEntity(t *testing.T) {
	t.Parallel()
	// A pool sorted the way the sources produce it: everything about one person,
	// then everything about the next. Served as-is this is the monotony
	// complaint verbatim.
	pool := make([]Question, 0, 16)
	for _, subject := range []string{"anna", "bara", "cyril", "dana"} {
		for i := range 4 {
			pool = append(pool, faceQ(subject, i, tierSure))
		}
	}
	round, _ := mixRound(pool, testMix(DefaultRoundSize), nil)
	if len(round) != DefaultRoundSize {
		t.Fatalf("round = %d questions, want %d", len(round), DefaultRoundSize)
	}
	if got := longestEntityRun(round); got > maxSameEntityRun {
		t.Errorf("longest entity run = %d, want at most %d: %s",
			got, maxSameEntityRun, mixEntities(round))
	}
}

func TestMixRound_capsOneEntitysShareOfTheRound(t *testing.T) {
	t.Parallel()
	// Ten questions about one person and three each about three others — enough
	// material that every entity can stay inside its cap. The run rule alone
	// would happily serve anna-anna-bara-anna-anna-cyril…; the cap is what bounds
	// how much of the round is about her at all.
	pool := make([]Question, 0, 19)
	for i := range 10 {
		pool = append(pool, faceQ("anna", i, tierSure))
	}
	for _, subject := range []string{"bara", "cyril", "dana"} {
		for i := range 3 {
			pool = append(pool, faceQ(subject, i, tierSure))
		}
	}
	round, _ := mixRound(pool, testMix(DefaultRoundSize), nil)
	counts := make(map[string]int)
	for _, q := range round {
		counts[questionEntity(q)]++
	}
	if got := counts["subject:anna"]; got != DefaultRoundMaxPerEntity {
		t.Errorf("anna got %d of the round, want the cap of %d: %s",
			got, DefaultRoundMaxPerEntity, mixEntities(round))
	}
}

func TestMixRound_capIsAPreferenceNotAWall(t *testing.T) {
	t.Parallel()
	// The degradation case: a pool that is one person and nothing else. The run
	// limit is a refusal rather than a price, but it stands down when every
	// remaining candidate is about the same entity — a library with one named
	// person must stay playable, and a round that came back two questions long
	// every time would not be.
	pool := make([]Question, 0, 8)
	for i := range 8 {
		pool = append(pool, faceQ("anna", i, tierSure))
	}
	round, rest := mixRound(pool, testMix(DefaultRoundSize), nil)
	if len(round) != len(pool) {
		t.Fatalf("round = %d questions, want all %d — a one-sided pool must not "+
			"come back short", len(round), len(pool))
	}
	if len(rest) != 0 {
		t.Errorf("rest = %d questions, want none", len(rest))
	}
	// And it stays in informativeness order, because nothing distinguishes the
	// candidates but their rank once every rule is equally broken.
	if got := mixIDs(round); got != mixIDs(pool) {
		t.Errorf("round order = %s, want the pool's %s", got, mixIDs(pool))
	}
}

func TestMixRound_rotatesTheKindsWhenSeveralExist(t *testing.T) {
	t.Parallel()
	// Three kinds, each arriving as a block. Clustering them is what the source
	// order does; the round has to interleave them.
	pool := make([]Question, 0, 12)
	for i := range 4 {
		pool = append(pool, faceQ(fmt.Sprintf("subj%d", i), i, tierSure))
	}
	for i := range 4 {
		pool = append(pool, labelQ(fmt.Sprintf("lab%d", i), i, tierSure))
	}
	for i := range 4 {
		pool = append(pool, placeQ(fmt.Sprintf("place%d", i), i))
	}
	round, _ := mixRound(pool, testMix(9), nil)
	runs := longestKindRun(round)
	if runs > 2 {
		t.Errorf("longest run of one kind = %d, want at most 2: %s", runs, kindsOf(round))
	}
	seen := make(map[Kind]int)
	for _, q := range round {
		seen[q.Kind]++
	}
	if len(seen) != 3 {
		t.Errorf("round covers %d kinds, want all 3: %s", len(seen), kindsOf(round))
	}
}

// longestKindRun returns the longest run of consecutive questions of one kind.
func longestKindRun(round []Question) int {
	best, run := 0, 0
	var prev Kind
	for _, q := range round {
		if q.Kind == prev {
			run++
		} else {
			prev, run = q.Kind, 1
		}
		best = max(best, run)
	}
	return best
}

func TestMixRound_interleavesTheTiersAtTheConfiguredShare(t *testing.T) {
	t.Parallel()
	// The pool arrives with every confident question first — which is what a
	// blend that has spent its allowance looks like from one end. The round must
	// hold the same ratio *and* alternate, not serve seven easy then three hard.
	pool := make([]Question, 0, 20)
	for i := range 10 {
		pool = append(pool, faceQ(fmt.Sprintf("subj%d", i), i, tierSure))
	}
	for i := range 10 {
		pool = append(pool, faceQ(fmt.Sprintf("other%d", i), i, tierBand))
	}
	round, _ := mixRound(pool, testMix(10), nil)
	sure, band := tierSplit(round)
	if sure != 7 || band != 3 {
		t.Errorf("round mix = %d confident / %d band, want 7/3 at a share of %.2f: %s",
			sure, band, DefaultSureShare, tiersOf(round))
	}
	// Three band questions in ten, spread out: the longest run of confident ones
	// must be shorter than the block the pool arrived as.
	if got := longestTierRun(round); got > 3 {
		t.Errorf("longest run of one tier = %d, want the tiers interleaved: %s",
			got, tiersOf(round))
	}
}

// longestTierRun returns the longest run of consecutive questions from one tier.
func longestTierRun(round []Question) int {
	best, run, prev := 0, 0, ""
	for _, q := range round {
		if q.Tier == prev {
			run++
		} else {
			prev, run = q.Tier, 1
		}
		best = max(best, run)
	}
	return best
}

func TestMixRound_untieredKindsDoNotDisturbTheShare(t *testing.T) {
	t.Parallel()
	// Place questions carry no tier. Counting them as band would make a round
	// that is half places look like a round of hard questions and pull the
	// confident share up to compensate.
	pool := make([]Question, 0, 15)
	for i := range 5 {
		pool = append(pool, placeQ(fmt.Sprintf("place%d", i), i))
	}
	for i := range 5 {
		pool = append(pool, faceQ(fmt.Sprintf("subj%d", i), i, tierSure))
	}
	for i := range 5 {
		pool = append(pool, faceQ(fmt.Sprintf("other%d", i), i, tierBand))
	}
	round, _ := mixRound(pool, testMix(10), nil)
	sure, band := tierSplit(round)
	if sure+band == 0 {
		t.Fatalf("no tiered questions in the round at all: %s", tiersOf(round))
	}
	got := float64(sure) / float64(sure+band)
	if got < 0.5 || got > 0.9 {
		t.Errorf("confident share of the tiered questions = %.2f, want near %.2f: %s",
			got, DefaultSureShare, tiersOf(round))
	}
}

func TestMixRound_spreadsPhotosAcrossAlbumsMomentsAndEras(t *testing.T) {
	t.Parallel()
	// Two shoots, each of three photos: same album, same minute, same decade.
	// Every question is about a different person, so only the photo rules can
	// separate them — and they must, or the round is two blocks of one afternoon.
	shoots := map[string][]string{"album-wedding": nil, "album-holiday": nil}
	pool := make([]Question, 0, 6)
	for shoot, at := range map[string]time.Time{
		"album-wedding": time.Date(1998, 6, 6, 12, 0, 0, 0, time.UTC),
		"album-holiday": time.Date(2015, 8, 1, 9, 0, 0, 0, time.UTC),
	} {
		for i := range 3 {
			q := faceQ(fmt.Sprintf("%s-p%d", shoot, i), i, tierSure)
			taken := at.Add(time.Duration(i) * time.Minute)
			q.Photo.TakenAt = &taken
			pool = append(pool, q)
			shoots[shoot] = append(shoots[shoot], q.Photo.UID)
		}
	}
	byPhoto := make(map[string][]string)
	for album, uids := range shoots {
		for _, uid := range uids {
			byPhoto[uid] = []string{album}
		}
	}
	round, _ := mixRound(pool, testMix(6), func(uid string) []string { return byPhoto[uid] })
	for i := 1; i < len(round); i++ {
		prev, cur := round[i-1].Photo, round[i].Photo
		if sameMoment(cur.TakenAt, prev.TakenAt) {
			t.Errorf("questions %d and %d are from the same moment (%v / %v)",
				i-1, i, prev.TakenAt, cur.TakenAt)
		}
		if len(byPhoto[cur.UID]) > 0 && byPhoto[cur.UID][0] == byPhoto[prev.UID][0] {
			t.Errorf("questions %d and %d are both from album %s", i-1, i, byPhoto[cur.UID][0])
		}
	}
}

func TestMixRound_deterministicForOnePoolAndSeed(t *testing.T) {
	t.Parallel()
	pool := make([]Question, 0, 12)
	for i := range 6 {
		pool = append(pool, faceQ(fmt.Sprintf("subj%d", i%3), i, tierSure))
		pool = append(pool, labelQ(fmt.Sprintf("lab%d", i%2), i, tierBand))
	}
	cfg := testMix(DefaultRoundSize)
	cfg.Seed = 7
	first, firstRest := mixRound(pool, cfg, nil)
	second, secondRest := mixRound(pool, cfg, nil)
	if mixIDs(first) != mixIDs(second) {
		t.Errorf("two mixes of one pool differ:\n%s\n%s", mixIDs(first), mixIDs(second))
	}
	if mixIDs(firstRest) != mixIDs(secondRest) {
		t.Errorf("two mixes leave different remainders:\n%s\n%s",
			mixIDs(firstRest), mixIDs(secondRest))
	}
}

func TestMixRound_seedPicksTheOpeningKind(t *testing.T) {
	t.Parallel()
	// The one thing the seed decides. Two kinds are present, so consecutive
	// seeds have to open with different ones — otherwise every round of a
	// session would start the same way.
	pool := make([]Question, 0, 12)
	for i := range 6 {
		pool = append(pool, faceQ(fmt.Sprintf("subj%d", i), i, tierSure))
		pool = append(pool, labelQ(fmt.Sprintf("lab%d", i), i, tierSure))
	}
	opens := make(map[Kind]bool)
	for seed := range uint64(2) {
		cfg := testMix(4)
		cfg.Seed = seed
		round, _ := mixRound(pool, cfg, nil)
		opens[round[0].Kind] = true
	}
	if len(opens) != 2 {
		t.Errorf("both seeds opened with the same kind, want the seed to vary it: %v", opens)
	}
}

func TestMixRound_leavesTheRestInPoolOrder(t *testing.T) {
	t.Parallel()
	// The leftovers are what the next round is mixed from, so they must keep the
	// informativeness ranking the sources produced — a remainder shuffled by the
	// first round would make the second one worse for no reason.
	pool := make([]Question, 0, 12)
	for i := range 12 {
		pool = append(pool, faceQ(fmt.Sprintf("subj%d", i%4), i, tierSure))
	}
	round, rest := mixRound(pool, testMix(5), nil)
	if len(round) != 5 || len(rest) != 7 {
		t.Fatalf("round/rest = %d/%d, want 5/7", len(round), len(rest))
	}
	taken := make(map[string]bool, len(round))
	for _, q := range round {
		taken[q.ID] = true
	}
	var want []Question
	for _, q := range pool {
		if !taken[q.ID] {
			want = append(want, q)
		}
	}
	if mixIDs(rest) != mixIDs(want) {
		t.Errorf("rest = %s, want the pool's own order %s", mixIDs(rest), mixIDs(want))
	}
}

func TestMixRound_emptyPoolAndNonPositiveSize(t *testing.T) {
	t.Parallel()
	if round, rest := mixRound(nil, testMix(DefaultRoundSize), nil); round != nil || rest != nil {
		t.Errorf("mixing an empty pool = %v / %v, want nothing", round, rest)
	}
	pool := []Question{faceQ("anna", 0, tierSure)}
	round, rest := mixRound(pool, testMix(0), nil)
	if len(round) != 0 || len(rest) != 1 {
		t.Errorf("round/rest = %d/%d for a size of 0, want 0/1", len(round), len(rest))
	}
}

func TestRoundSummary_countsTheRoundAsMinted(t *testing.T) {
	t.Parallel()
	round := []Question{
		faceQ("anna", 0, tierSure),
		labelQ("wedding", 0, tierBand),
		placeQ("Brno", 0),
		faceQ("anna", 1, tierSure),
	}
	got := roundSummary(3, round)
	if got.Index != 3 || got.Size != 4 {
		t.Errorf("index/size = %d/%d, want 3/4", got.Index, got.Size)
	}
	if got.Sure != 2 || got.Band != 1 {
		t.Errorf("sure/band = %d/%d, want 2/1 — the place question carries no tier",
			got.Sure, got.Band)
	}
	if got.Entities != 3 {
		t.Errorf("entities = %d, want 3 (anna, the label, the place)", got.Entities)
	}
	want := map[string]int{"face": 2, "label": 1, "place": 1}
	for kind, count := range want {
		if got.Kinds[kind] != count {
			t.Errorf("kinds[%s] = %d, want %d (%v)", kind, got.Kinds[kind], count, got.Kinds)
		}
	}
}

func TestSameMomentAndSameEra_undatedPhotosNeverMatch(t *testing.T) {
	t.Parallel()
	at := time.Date(1972, 3, 4, 5, 6, 0, 0, time.UTC)
	near := at.Add(nearMoment / 2)
	far := at.Add(2 * nearMoment)
	sameDecade := at.AddDate(3, 0, 0)
	nextDecade := at.AddDate(10, 0, 0)

	tests := []struct {
		name        string
		one, other  *time.Time
		moment, era bool
	}{
		{"inside the burst", &at, &near, true, true},
		{"outside the burst, same decade", &at, &far, false, true},
		{"same decade, years apart", &at, &sameDecade, false, true},
		{"different decades", &at, &nextDecade, false, false},
		{"one undated", &at, nil, false, false},
		{"both undated", nil, nil, false, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := sameMoment(tt.one, tt.other); got != tt.moment {
				t.Errorf("sameMoment = %v, want %v", got, tt.moment)
			}
			if got := sameEra(tt.one, tt.other); got != tt.era {
				t.Errorf("sameEra = %v, want %v", got, tt.era)
			}
		})
	}
}

func TestRoundSeed_variesByUserAndRound(t *testing.T) {
	t.Parallel()
	if roundSeed("anna", 0) == roundSeed("anna", 1) {
		t.Error("two rounds of one session share a seed")
	}
	if roundSeed("anna", 0) == roundSeed("bara", 0) {
		t.Error("two players share a seed")
	}
	first, second := roundSeed("anna", 2), roundSeed("anna", 2)
	if first != second {
		t.Errorf("the same user and round number gave two seeds: %d and %d", first, second)
	}
}
