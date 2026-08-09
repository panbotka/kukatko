package review

// Rounds: composing a playlist instead of serving a sorted list.
//
// The queue used to hand out whatever order the sources produced. Every
// individual rule behind that order was defensible — most informative first
// inside a tier, the tiers blended by ratio, the kinds interleaved by
// proportion, no entity twice too often — and the result was still monotonous to
// play, because none of those rules looks at the *photo*. Twenty questions can
// obey every one of them and still be twenty pictures from the same afternoon,
// or a run of eight equally-easy confirmations, or the same person alternating
// with the same label. The live instance has one player and 38 answers, which is
// what "technically varied" buys.
//
// So the last step before a batch reaches the player is no longer a sort but a
// mix. A round is built one slot at a time: every unplaced question is scored
// against what the round already holds, and the cheapest one goes next. The
// score is a sum of penalties, each one a rule the round would rather not break:
//
//   - a third question in a row about one entity, or more than MaxPerRound of
//     them in the whole round (the two hard rules, weighted far above the rest);
//   - the same kind as the previous question, and worse, as the previous two;
//   - the tier the running confident share does not currently want;
//   - a photo from an album the previous photo was also in;
//   - a photo taken within nearMoment of the previous one — the same burst;
//   - a photo from the same era as the previous one.
//
// Nothing is forbidden outright. A pool with one subject in it, or ten photos
// from one wedding, still yields a full round: every candidate is merely
// expensive, and the cheapest expensive one wins. That is what "degrade
// gracefully" means here — the rules are preferences over a total order, so
// there is no way for them to produce an empty round while a question exists.
//
// Ties are broken by how deep in the informativeness order a candidate sits, so
// variety is never bought with a less relevant question when an equally
// well-behaved better one exists: the pool arrives ordered (band by distance
// from the decision boundary, confident tier surest first, see queue.go) and
// that order survives everywhere the variety rules do not override it.
//
// The seed picks which kind the round tries to open with — enough that two
// players working one library do not get the same opening, and cheap because it
// only ever decides between candidates that are otherwise equal. Everything else
// is a pure function of (pool, config), which is what makes a re-fetch before
// answering return the same round rather than a different one, and what lets the
// tests assert the rules above.

import (
	"hash/fnv"
	"slices"
	"strconv"
	"time"

	"github.com/panbotka/kukatko/internal/photos"
)

const (
	// nearMoment is how close two capture times have to be for their photos to
	// count as the same moment. Ten minutes is a burst, a ceremony, one room of a
	// party: pictures that look alike because they *are* alike, and asking two
	// questions about them back to back is the flavour of repetition a player
	// notices first.
	nearMoment = 10 * time.Minute
	// eraYears is how many years one "era" spans. A decade is the unit people
	// actually narrate a family archive in ("the seventies", "when the kids were
	// small"), and it is coarse enough that a round drawn from a library with any
	// spread at all can genuinely alternate between two of them.
	eraYears = 10
)

// The mixer's penalty weights. They are powers of two an order of magnitude
// apart rather than tuned numbers, because what matters is only their relative
// order: a rule never trades against a more important one, no matter how many
// lesser rules a candidate breaks at once. Read top to bottom, this list *is*
// the priority of the variety rules.
const (
	// costEntityCap is the round's per-entity ceiling — the hardest rule, since
	// exceeding it is what turns a round back into an interrogation.
	costEntityCap = 1 << 20
	// costEntityRun is a third consecutive question about one entity.
	costEntityRun = 1 << 16
	// costKindRun is a third consecutive question of one kind, costKindRepeat a
	// second. Both apply together, so a run is always dearer than a repeat.
	costKindRun    = 1 << 12
	costKindRepeat = 1 << 10
	// costTier is drawing from the tier the running confident share does not want.
	costTier = 1 << 8
	// costAlbum, costMoment and costEra are the photo-spread rules, in the order
	// a player perceives them: the same album is the most obvious repetition, the
	// same minute the next, the same decade the mildest.
	costAlbum  = 1 << 6
	costMoment = 1 << 5
	costEra    = 1 << 3
	// costOpening is not opening the round with the kind the seed chose. It is
	// the cheapest rule of all: it only ever decides between candidates that are
	// otherwise equal, which is exactly what an opening should be.
	costOpening = 1 << 1
)

// albumLookup reports which albums a photo belongs to. A nil lookup — no album
// store wired, or a lookup that failed — simply switches the album rule off
// rather than failing a round.
type albumLookup func(photoUID string) []string

// mixConfig are the knobs one round is mixed with. It is a value, not a read of
// the Service, so the mixer stays a pure function the tests can drive directly.
type mixConfig struct {
	// RoundSize is how many questions the round holds at most.
	RoundSize int
	// MaxPerRound is how many questions about one entity may enter the round;
	// non-positive switches the cap off.
	MaxPerRound int
	// MaxRun is how many questions in a row may be about one entity;
	// non-positive switches the run rule off.
	MaxRun int
	// SureShare is the fraction of the round's *tiered* questions that should
	// come from the confident tier.
	SureShare float64
	// Seed picks which kind the round opens with. The mixer is deterministic
	// given it.
	Seed uint64
}

// mixer builds one round out of a pool, one slot at a time, carrying the state
// every rule is measured against.
type mixer struct {
	cfg    mixConfig
	albums albumLookup
	// open is the kind the round tries to start with, chosen from the kinds the
	// pool actually holds.
	open Kind
	// counts is how many questions about each entity the round already holds.
	counts map[string]int
	// last and prev are the two questions placed most recently; placed says how
	// many of them are real.
	last   Question
	prev   Question
	placed int
	// run is how many consecutive questions the round ends with about last's
	// entity.
	run int
	// sure and band count the round's tiered questions, for the ratio rule.
	sure int
	band int
}

// newMixer prepares a mixer for one round over pool.
func newMixer(pool []Question, cfg mixConfig, albums albumLookup) *mixer {
	return &mixer{
		cfg:    cfg,
		albums: albums,
		open:   openingKind(pool, cfg.Seed),
		counts: make(map[string]int, len(pool)),
	}
}

// mixRound composes the next round out of pool and returns it together with
// everything left over, the leftovers keeping their original relative order so
// the next round is mixed from an unchanged informativeness ranking.
//
// A non-positive round size, or an empty pool, yields no round and the pool
// untouched — the caller then has nothing to serve, which is the honest answer
// rather than an invented one.
func mixRound(pool []Question, cfg mixConfig, albums albumLookup) (round, rest []Question) {
	size := min(cfg.RoundSize, len(pool))
	if size <= 0 {
		return nil, pool
	}
	mix := newMixer(pool, cfg, albums)
	taken := make([]bool, len(pool))
	round = make([]Question, 0, size)
	for range size {
		idx := mix.best(pool, taken)
		taken[idx] = true
		mix.place(pool[idx])
		round = append(round, pool[idx])
	}
	rest = make([]Question, 0, len(pool)-size)
	for i, q := range pool {
		if !taken[i] {
			rest = append(rest, q)
		}
	}
	return round, rest
}

// best returns the index of the cheapest unplaced candidate, ties going to
// whichever sits earlier in the pool's informativeness order. It is called once
// per slot and scans the whole pool, which is bounded by maxQueued, so a round
// costs a few thousand integer comparisons — nothing next to the vector searches
// that produced the pool in the first place.
func (m *mixer) best(pool []Question, taken []bool) int {
	best, bestCost := -1, 0
	for i, q := range pool {
		if taken[i] {
			continue
		}
		if cost := m.cost(q); best < 0 || cost < bestCost {
			best, bestCost = i, cost
		}
	}
	return best
}

// cost sums every variety rule q would break if it went into the next slot.
func (m *mixer) cost(q Question) int {
	return m.entityCost(q) + m.kindCost(q.Kind) + m.tierCost(q.Tier) +
		m.photoCost(q.Photo) + m.openingCost(q.Kind)
}

// entityCost charges the two hard rules: the round's per-entity ceiling and the
// run of consecutive questions about one subject, label, place or duplicate
// group. Both are charged, not enforced, so a pool that offers only one entity
// still fills a round.
func (m *mixer) entityCost(q Question) int {
	entity := questionEntity(q)
	cost := 0
	if m.cfg.MaxPerRound > 0 && m.counts[entity] >= m.cfg.MaxPerRound {
		cost += costEntityCap
	}
	if m.cfg.MaxRun > 0 && m.placed > 0 && m.run >= m.cfg.MaxRun &&
		entity == questionEntity(m.last) {
		cost += costEntityRun
	}
	return cost
}

// kindCost charges repeating the previous question's kind, and charges again
// when the two before it were the same — the difference between "two face
// questions in a row" and "the game has become the face game".
//
// A second question of one kind is deliberately cheap: switching kind switches
// the whole card layout, and doing it every single question is its own kind of
// tiring. It is the third that reads as a cluster.
func (m *mixer) kindCost(kind Kind) int {
	if m.placed == 0 || m.last.Kind != kind {
		return 0
	}
	if m.placed > 1 && m.prev.Kind == kind {
		return costKindRun + costKindRepeat
	}
	return costKindRepeat
}

// tierCost charges drawing from the tier the round does not currently want. The
// want is the same positional rule blend applies (take a confident question
// while the running confident fraction would stay at or below SureShare), so the
// mix over a whole round is the configured one — what changes here is only that
// the two tiers end up interleaved rather than laid out in blocks.
//
// The three kinds that carry no tier are neutral: their confidences are not
// points on the same scale, so counting them either way would distort the ratio
// the operator configured.
func (m *mixer) tierCost(which string) int {
	if which != string(tierSure) && which != string(tierBand) {
		return 0
	}
	want := string(tierBand)
	if float64(m.sure) < m.cfg.SureShare*float64(m.sure+m.band+1) {
		want = string(tierSure)
	}
	if which == want {
		return 0
	}
	return costTier
}

// photoCost charges the three photo-spread rules against the previous question's
// photo: a shared album, a capture time inside the same burst, and the same era.
// They are measured pairwise rather than over the whole round on purpose — what
// a player perceives as "this again" is the card they just answered, not the
// distribution of the ten before it.
func (m *mixer) photoCost(photo photos.Photo) int {
	if m.placed == 0 {
		return 0
	}
	cost := 0
	if m.sharesAlbum(photo.UID, m.last.Photo.UID) {
		cost += costAlbum
	}
	if sameMoment(photo.TakenAt, m.last.Photo.TakenAt) {
		cost += costMoment
	}
	if sameEra(photo.TakenAt, m.last.Photo.TakenAt) {
		cost += costEra
	}
	return cost
}

// openingCost charges the first slot for not being the kind the seed picked. It
// applies to that slot alone: which kind a round opens with is the cheapest way
// to make two rounds over an unchanged library feel like two rounds.
func (m *mixer) openingCost(kind Kind) int {
	if m.placed > 0 || m.open == "" || kind == m.open {
		return 0
	}
	return costOpening
}

// sharesAlbum reports whether two photos sit in a common album.
func (m *mixer) sharesAlbum(photoUID, otherUID string) bool {
	if m.albums == nil {
		return false
	}
	mine := m.albums(photoUID)
	if len(mine) == 0 {
		return false
	}
	for _, album := range m.albums(otherUID) {
		if slices.Contains(mine, album) {
			return true
		}
	}
	return false
}

// place records a question as taken, updating everything the rules read.
func (m *mixer) place(q Question) {
	entity := questionEntity(q)
	if m.placed > 0 && entity == questionEntity(m.last) {
		m.run++
	} else {
		m.run = 1
	}
	m.counts[entity]++
	switch q.Tier {
	case string(tierSure):
		m.sure++
	case string(tierBand):
		m.band++
	}
	m.prev, m.last = m.last, q
	m.placed++
}

// openingKind picks the kind the round should try to open with: the seed indexes
// into the kinds the pool actually holds, in the fixed order of Kinds. A pool of
// one kind always opens with it, so the rule costs nothing when there is no
// choice to make.
func openingKind(pool []Question, seed uint64) Kind {
	present := make([]Kind, 0, len(Kinds))
	for _, kind := range Kinds {
		if holdsKind(pool, kind) {
			present = append(present, kind)
		}
	}
	if len(present) == 0 {
		return ""
	}
	return present[seed%uint64(len(present))]
}

// holdsKind reports whether pool contains a question of the given kind.
func holdsKind(pool []Question, kind Kind) bool {
	for _, q := range pool {
		if q.Kind == kind {
			return true
		}
	}
	return false
}

// sameMoment reports whether two capture times sit inside one burst. A photo
// with no date is never "the same moment" as anything: an unknown date is not
// evidence of closeness, and scanned photos (which is most of what has none)
// would otherwise all clump together.
func sameMoment(one, other *time.Time) bool {
	if one == nil || other == nil {
		return false
	}
	gap := one.Sub(*other)
	if gap < 0 {
		gap = -gap
	}
	return gap < nearMoment
}

// sameEra reports whether two capture times fall in the same decade, with an
// unknown date again never matching.
func sameEra(one, other *time.Time) bool {
	if one == nil || other == nil {
		return false
	}
	return one.Year()/eraYears == other.Year()/eraYears
}

// roundSeed derives one round's seed from who is playing and which round of
// their session it is, so two players get different rounds out of one library
// and one player's rounds differ from each other — while a replayed session
// (same user, same round number, same pool) reproduces exactly.
func roundSeed(userUID string, sequence int) uint64 {
	sum := fnv.New64a()
	_, _ = sum.Write([]byte(userUID))
	_, _ = sum.Write([]byte(strconv.Itoa(sequence)))
	return sum.Sum64()
}

// roundSummary describes a freshly minted round for the client's between-rounds
// screen: what it asks about and in what mix. It is computed once, when the
// round is minted, so the numbers stay the round's own rather than shrinking as
// the player answers.
func roundSummary(index int, round []Question) RoundInfo {
	kinds := make(map[string]int, len(Kinds))
	for _, q := range round {
		kinds[string(q.Kind)]++
	}
	sure, band := tierSplit(round)
	return RoundInfo{
		Index:    index,
		Size:     len(round),
		Kinds:    kinds,
		Sure:     sure,
		Band:     band,
		Entities: countEntities(round),
	}
}

// tierSplit counts a sequence's confident and band questions, ignoring the kinds
// that carry no tier at all. tierCounts, which the rebuild log uses, folds those
// into the band; a summary shown to the player must not, or a round of place and
// duplicate questions would claim to be all-hard.
func tierSplit(questions []Question) (sure, band int) {
	for _, q := range questions {
		switch q.Tier {
		case string(tierSure):
			sure++
		case string(tierBand):
			band++
		}
	}
	return sure, band
}
