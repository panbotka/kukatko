package review

// Two tiers of question, and how a batch is mixed from them.
//
// The game used to serve the uncertainty band and nothing else. That maximises
// information per answer — a candidate the system is 60 % sure about is exactly
// the one where a human verdict teaches it something — but it optimises for the
// wrong quantity. What the operator is buying with an evening of clicking is
// *confirmed assignments per minute of attention*, plus a game that is bearable
// to play. A 90 %-confident candidate answered "yes" in one click is real work
// done; it is merely unsurprising. Excluding it by design, as band-only did,
// meant every single question was a hard one.
//
// So a batch is mixed: sureShare of it (0.70 by default) from the confident
// tier — confidence at or above sureFloor, where the answer is almost always yes
// — and the rest from today's band. The minority of hard questions is
// load-bearing and must not be tuned away: a game that is 95 % "yes" turns the
// player into a rubber stamp who stops looking, and wrong assignments then enter
// the library through the very feature meant to clean it.
//
// The mix is enforced *positionally*, not just in aggregate. A batch is a prefix
// of the built queue, so a queue that merely ends up 70/30 overall could still
// open with ten hard questions in a row. blend interleaves the two tiers so the
// ratio holds in every prefix.

// tier names one of the two confidence ranges a question can be drawn from.
type tier string

const (
	// tierSure is the confident tier: confidence >= sureFloor. Its questions are
	// ordered surest-first, because within a tier whose point is "this is almost
	// certainly yes", the surest candidate is the one that costs the player the
	// least to confirm.
	tierSure tier = "sure"
	// tierBand is the uncertainty band [bandMin, bandMax). Its questions are
	// ordered by distance from the band midpoint — closest to the decision
	// boundary first, where a human answer buys the most.
	tierBand tier = "band"
)

// tiered is one source's material split by tier, as one rebuild collected it.
// The two halves are kept apart all the way to the blend because they are
// ordered by different rules and mixed by a ratio; merging them earlier would
// lose both.
type tiered struct {
	// sure holds the confident-tier questions, band the uncertainty-band ones.
	sure []Question
	band []Question
}

// add appends q to the half its tier names.
func (t *tiered) add(which tier, q Question) {
	if which == tierSure {
		t.sure = append(t.sure, q)
	} else {
		t.band = append(t.band, q)
	}
}

// len is how many questions the two halves hold together — what a scan counts
// when deciding it has enough.
func (t *tiered) len() int {
	return len(t.sure) + len(t.band)
}

// merge folds another tiered's halves into t, keeping each tier's order.
func (t *tiered) merge(other tiered) {
	t.sure = append(t.sure, other.sure...)
	t.band = append(t.band, other.band...)
}

// tierOf classifies a candidate's confidence, reporting false when it belongs to
// neither tier: below bandMin (the guess is noise, and asking would demoralise),
// or in the gap between bandMax and sureFloor when the operator has configured
// one. The gap is closed by setting review.sure_min equal to review.band_max.
func (s *Service) tierOf(confidence float64) (tier, bool) {
	switch {
	case confidence >= s.sureFloor():
		return tierSure, true
	case s.inBand(confidence):
		return tierBand, true
	default:
		return "", false
	}
}

// sureFloor is the confident tier's effective lower edge: the configured
// SureMin, never below BandMax. Clamping is what keeps the tiers disjoint — an
// operator who sets sure_min below band_max would otherwise have candidates
// belonging to both, and the same photo would be asked about twice in one batch.
func (s *Service) sureFloor() float64 {
	return max(s.sureMin, s.bandMax)
}

// blend interleaves the two tiers so that the confident share holds in every
// prefix of the result, not merely across the whole of it. That distinction is
// the whole point: a batch is a prefix of the built queue, and a queue that is
// 70/30 overall but front-loads the band would hand the player exactly the run
// of hard questions this change exists to remove.
//
// At each step it takes from the confident tier when doing so keeps the running
// confident fraction no higher than share, and from the band otherwise; once one
// tier runs out the other's remainder follows. It is a pure function of two
// already-ordered inputs, so the queue stays reproducible: no randomness, and
// the same library state blends the same way twice.
//
// A share outside (0, 1) is not defended against here — Service clamps it at
// construction — but the arithmetic degrades sanely anyway: 0 yields band-first,
// 1 yields sure-first, and both still emit every question.
func blend(sure, band []Question, share float64) []Question {
	out := make([]Question, 0, len(sure)+len(band))
	si, bi := 0, 0
	for si < len(sure) && bi < len(band) {
		if float64(si) < share*float64(si+bi+1) {
			out = append(out, sure[si])
			si++
		} else {
			out = append(out, band[bi])
			bi++
		}
	}
	out = append(out, sure[si:]...)
	out = append(out, band[bi:]...)
	return out
}

// capEntities enforces the per-entity share across the blended sequence, keeping
// at most maxPerEntity questions about any one subject or label and dropping the
// rest.
//
// Each tier is already capped on its own while it is collected, which bounds the
// material a rebuild carries. This is the cut that makes the *batch* invariant
// hold: without it an entity with a full share in both tiers would contribute
// twice the allowance, and "no more than four questions about one label" — the
// rule the monotony complaint produced — would quietly become eight.
//
// It keeps the head of each entity's run, so what survives is that entity's
// highest-ranked material in the blended order, and the tier ratio around it is
// disturbed as little as the cut allows.
func capEntities(questions []Question, maxPerEntity int) []Question {
	if maxPerEntity <= 0 {
		return questions
	}
	counts := make(map[string]int, len(questions))
	kept := questions[:0]
	for _, q := range questions {
		entity := questionEntity(q)
		if counts[entity] >= maxPerEntity {
			continue
		}
		counts[entity]++
		kept = append(kept, q)
	}
	return kept
}

// tierCounts tallies a sequence by tier, for the rebuild log line and the tests
// that assert the mix actually lands where sureShare says it should.
func tierCounts(questions []Question) (sure, band int) {
	for _, q := range questions {
		if q.Tier == string(tierSure) {
			sure++
		} else {
			band++
		}
	}
	return sure, band
}
