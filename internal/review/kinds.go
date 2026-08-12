package review

// What the game is about, as configuration rather than as an accident.
//
// The five kinds used to reach a round by no rule at all. Each collector stopped
// at `need` candidates and the merge interleaved whatever came back, so the
// actual proportion of faces to labels fell out of how much material each search
// happened to produce — and the nearest thing to a knob was the scan budget
// (eight subjects against six labels), which is why a game meant to be about
// people spent so much of itself on labels.
//
// A share per kind replaces that. It decides three things at once, which is what
// makes it a knob rather than a hint:
//
//   - a kind at zero is never scanned, so it costs a rebuild nothing. That is
//     where the budget for a wider face scan comes from;
//   - a kind at zero is not a source either, so an empty queue can never explain
//     itself by naming a kind the operator switched off;
//   - the round mixer prefers whichever kind its running share is furthest
//     behind on, so the proportion holds in what the player actually sits
//     through and not merely across a pool they never see.
//
// What a share deliberately does *not* do is shrink a collector's target. Every
// enabled kind still gathers as much material as the pool wants, so a kind that
// is the only one with anything left fills the pool alone — an exhausted library
// must not withhold the work it does have, and a five percent kind starving the
// pool on a day when nothing else has material would be exactly that. The share
// decides the mix of what is asked, not how hard the rebuild looks.
//
// The default is one line long — faces, and nothing else — because that is what
// the game is for. Restoring another kind is then a config edit: the mix this
// started as is `face: 0.95, label: 0.05`.
//
// The weights are relative, not percentages: only their ratio matters, so
// `face: 19, label: 1` says the same thing. Everything below reads them
// normalised over the enabled kinds.

// kindShares is one weight per question kind. Absent or non-positive means the
// kind is switched off entirely; the positive ones are relative to each other.
type kindShares map[Kind]float64

// defaultKindShares leaves the queue to faces. It is a function rather than a
// package-level map because a map is mutable and this one is handed out.
func defaultKindShares() kindShares {
	return kindShares{KindFace: 1}
}

// newKindShares normalises a configured set of weights: non-positive entries are
// dropped, unknown kinds are ignored, and a set that switches everything off
// falls back to the default rather than yielding a game that can ask nothing.
func newKindShares(raw map[Kind]float64) kindShares {
	shares := make(kindShares, len(Kinds))
	total := 0.0
	for _, kind := range Kinds {
		weight := raw[kind]
		if weight <= 0 {
			continue
		}
		shares[kind] = weight
		total += weight
	}
	if total <= 0 {
		return defaultKindShares()
	}
	for kind, weight := range shares {
		shares[kind] = weight / total
	}
	return shares
}

// enabled reports whether the game may ask this kind at all. A disabled kind is
// not scanned, not asked and not counted as a source the empty-queue reason
// could point at.
func (k kindShares) enabled(kind Kind) bool {
	return k[kind] > 0
}

// share is the kind's normalised fraction of the game, zero for a disabled one.
func (k kindShares) share(kind Kind) float64 {
	return k[kind]
}

// only reports whether the game is configured down to this single kind, which is
// what lets an empty queue say "there are no people to ask about" instead of the
// vaguer "no people and no labels".
func (k kindShares) only(kind Kind) bool {
	return len(k) == 1 && k.enabled(kind)
}

// presentIn narrows the shares to the kinds a pool actually holds, renormalised
// over what is left. Without it a share reserved for a kind the pool has none of
// would go on being demanded slot after slot, and the kinds that do have
// material would be charged for filling a place nothing can take — a pool of
// faces alone would be paced as though four fifths of it were something else.
// Narrowing makes the rule say what it means: of the questions there are, this
// is the mix.
func (k kindShares) presentIn(pool []Question) kindShares {
	present := make(kindShares, len(k))
	total := 0.0
	for _, kind := range Kinds {
		if k.share(kind) <= 0 || !holdsKind(pool, kind) {
			continue
		}
		present[kind] = k.share(kind)
		total += k.share(kind)
	}
	for kind, weight := range present {
		present[kind] = weight / total
	}
	return present
}

// wanted returns the kind the round is furthest behind on: the one whose share
// of the slots placed so far, counting the slot about to be filled, is least
// satisfied. Ties go to prefer when it is one of them, else to the earlier kind
// in Kinds, so the choice is deterministic either way.
//
// It is the same positional rule the tier blend uses, and for the same reason: a
// round is what the player sits through, so a mix that is only right in
// aggregate — ten faces and then the label — is not right at all. With the
// shares normalised there is always exactly one such kind, and when it has no
// candidate left every remaining question is charged equally, so the rule costs
// nothing in a pool it cannot be satisfied from.
//
// prefer is the round's seeded kind, and it is the whole of the seed's influence
// on a round: it decides only between kinds the shares are equally behind on,
// which at the first slot of an evenly configured game is every kind the pool
// holds — so two players get different openings — and at any slot of a game
// weighted 95/5 is none of them, because there the mix is not a matter of taste.
func (k kindShares) wanted(placed map[Kind]int, total int, prefer Kind) Kind {
	best, bestDeficit := Kind(""), 0.0
	for _, kind := range Kinds {
		share := k.share(kind)
		if share <= 0 {
			continue
		}
		deficit := share*float64(total+1) - float64(placed[kind])
		if best == "" || deficit > bestDeficit ||
			(deficit == bestDeficit && kind == prefer) {
			best, bestDeficit = kind, deficit
		}
	}
	return best
}
