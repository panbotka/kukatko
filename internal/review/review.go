// Package review builds the "review game" question queue and applies answers.
//
// The game asks one question at a time and the user answers yes, no or
// don't-know. There are five kinds of question, and every one of them is a
// yes/no over data some other part of the app already produced:
//
//   - face — "is this face subject X?" over a photo with an unnamed face;
//   - label — "should this photo carry label Y?" over a photo that looks like
//     the ones already on that label;
//   - place — "was this photo taken in Z?" over a photo whose coordinates the
//     geo-estimator guessed and nobody has ruled on;
//   - duplicate — "is this the same photo?" over the two members of a
//     near-duplicate pair, side by side;
//   - outlier — "is this really X?" over a face already assigned to X but
//     sitting far from X's centroid.
//
// The last three are a deliberate widening of what the game is for. The first
// two clean up guesses the machine made about things nobody had decided yet; the
// new ones clean up guesses the machine made *and acted on* — a location written
// onto a photo, a pair the detector linked, an assignment somebody (or something)
// already made. Those are the errors that otherwise sit in the library unnoticed,
// because no page lists them as questions.
//
// None of the three ever destroys anything on its own: the duplicate check
// records an opinion and NEVER merges, the outlier check detaches through the
// ordinary assign state machine (the marker survives), and the place check only
// ever moves a coordinate the estimator invented in the first place.
//
// A batch is mixed from two confidence tiers, because what the game is actually
// buying is confirmed assignments per minute of a human's attention, not
// information per answer. SureShare of it (0.70) comes from the confident tier —
// confidence (1 − cosine distance) at or above SureMin, where the answer is
// almost always a one-click yes — and the rest from the uncertainty band
// [BandMin, BandMax), where a human verdict teaches the system the most. Below
// BandMin the guess is noise and the question demoralising, so it is never
// asked. See tiers.go for why the ratio is enforced positionally and why the
// minority of hard questions must not be tuned away.
//
// Within a tier the order is that tier's own: the band by distance from its
// midpoint (closest to the decision boundary first), the confident tier by
// confidence descending. The tiers apply to the face and label questions only —
// the three checks over already-applied work have no single comparable
// confidence axis, so each carries its own ordering (see extras.go).
//
// Which of the five the game may ask about, and in what proportion, is
// configuration: review.kind_shares carries one weight per kind, and the default
// is one line long — faces, and nothing else, because that is what the game is
// for. A kind at zero is never scanned, so switching one off pays for the wider
// face scan the rest of the game needs; the enabled ones size their own
// collectors and are preferred by the round mixer in proportion to their share.
// See kinds.go. Within a kind the questions are then spread through the others
// so no kind arrives in a block — a kind that is the only one left still fills
// the batch, because an exhausted library should not withhold the work it does
// have.
//
// Informativeness alone does not make the game playable, though. One label that
// matches half the library supplies hundreds of band candidates and used to fill
// a whole batch by itself — twenty questions in a row about the same label. A
// pool therefore holds at most MaxPerEntity questions about any one subject or
// label *across every kind* — a person's unnamed faces and their outliers are
// the same person to a player — and a round never asks about one entity more
// than maxSameEntityRun times in a row while any other entity still has a
// question waiting. The mixer refuses such a candidate rather than pricing it,
// which is what makes that promise hold. See variety.go and mixer.go.
//
// All of that produces a *pool*. What the player is served is a round mixed out
// of it: RoundSize questions (10 by default) chosen one slot at a time so that
// consecutive questions differ in the ways a player actually notices — the
// person or label they are about, the kind of question, the tier, the album the
// photo is in, the moment it was taken, its era. One request is one round, and
// QueueResult.Round says which round it is and what it is made of, for the
// between-rounds summary. The rules are penalties rather than prohibitions, so a
// one-sided pool still yields a full round; see mixer.go.
//
// A round may also carry a Breather: a photo somebody already rated or
// favourited, with its title and year and nothing to answer (breathers.go). It
// travels outside the questions array and carries a kind of its own, so it can
// never be mistaken for a question. And a yes that confirms a face assignment
// answers with a small Reveal — how many photos that person is on now, and how
// far back their collection reaches.
//
// What the game asks about is the player's choice: people, labels or both (see
// source.go). The selection is pushed into the rebuild rather than applied to
// its output, so a player who only wants label questions never pays for the
// subject sweep.
//
// The queue composes the existing read-only searches — the recognition scan
// (per-subject face candidates, which already excludes assigned faces, persisted
// rejections, negative exemplars and sub-reviewable faces), the label-similarity
// search (which already excludes members and rejected photos), the estimated
// locations awaiting a verdict, the near-duplicate grouping and the per-person
// outlier ranking. Each is run in a bounded, rotating window: one batch of
// questions costs at most FaceBudget subjects, LabelBudget labels, OutlierBudget
// rankings and a page of the other two, stops as soon as the batch is
// full, and is capped by BuildTimeout on top of that. Answering one question
// must not cost a library-wide work list — sweeping all of it took four minutes
// on a real library, which is longer than any browser waits. The cursors advance
// on every rebuild, so successive rebuilds walk the whole library, and a rebuild
// whose window came back empty rotates and tries again (up to maxRebuildRounds,
// inside the one deadline) rather than telling the player there is nothing left.
//
// A label can be taken out of the game entirely on the labels page
// (labels.review_enabled): it then produces no questions and is not scanned, so
// it costs a rebuild nothing. Subjects have no equivalent switch.
//
// Answers route through the existing write paths and the package never opens a
// second one: yes on a face goes through the facematch assign state machine, yes
// on a label through the organize attach path, a place verdict through the
// geo-estimate reviewer's own accept/reject, an outlier "no" back through the
// assign state machine as unassign_person, and every other verdict records a
// persisted opinion in feedback. See answer.go for the whole map.
//
// What the game asks about is also bounded by what it is allowed to do: it never
// merges duplicates, never deletes a photo and never invalidates a marker.
//
// Built queues are cached per user for CacheTTL so a batch fetch does not rerun
// the vector searches, and answered or skipped questions are tracked in an
// in-memory session: a skip shelves a question for the session (an idle session
// is pruned after sessionIdleTTL), while yes/no answers persist through the
// underlying stores and never come back.
//
// A skip on a *face* is remembered beyond the session as well. Three "don't
// know"s about one person mute that person for that player for a cooling-off
// period, and the photos they were skipped on stay silent for good, so the game
// stops asking a player to name somebody they have already said they cannot
// place. It is per user, it is a pause rather than a verdict, and it reaches
// nothing in the catalogue: a skip is never a rejection. See skips.go.
package review

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"

	"github.com/panbotka/kukatko/internal/audit"
	"github.com/panbotka/kukatko/internal/candidates"
	"github.com/panbotka/kukatko/internal/duplicates"
	"github.com/panbotka/kukatko/internal/expand"
	"github.com/panbotka/kukatko/internal/facematch"
	"github.com/panbotka/kukatko/internal/feedback"
	"github.com/panbotka/kukatko/internal/geoestimate"
	"github.com/panbotka/kukatko/internal/mediaurl"
	"github.com/panbotka/kukatko/internal/organize"
	"github.com/panbotka/kukatko/internal/outliers"
	"github.com/panbotka/kukatko/internal/people"
	"github.com/panbotka/kukatko/internal/photos"
	"github.com/panbotka/kukatko/internal/sweep"
	"github.com/panbotka/kukatko/internal/vectors"
)

// Tunable defaults, used when the corresponding Config field is unset.
const (
	// DefaultBandMin is the lower edge of the uncertainty band: candidates less
	// confident than this are noise, not fair questions.
	DefaultBandMin = 0.45
	// DefaultBandMax is the upper edge of the uncertainty band: above it a
	// candidate is no longer a hard question, and belongs to the confident tier.
	DefaultBandMax = 0.75
	// DefaultSureMin is the floor of the confident tier: candidates at least this
	// confident are the ones a player confirms in one click.
	DefaultSureMin = 0.80
	// DefaultSureShare is the fraction of a batch drawn from the confident tier.
	// Seven in ten, decided with the operator: enough that the game mostly feels
	// like progress, not so much that the player stops reading the question.
	DefaultSureShare = 0.70
	// DefaultQueueSize is the default number of questions per batch, sized so
	// the UI can prefetch and stay instant between answers.
	DefaultQueueSize = 20
	// DefaultCacheTTL is how long a built queue is served from the per-user
	// cache before the expensive candidate searches run again.
	DefaultCacheTTL = time.Minute
	// DefaultMaxLabels caps how many labels one queue rebuild scans.
	DefaultMaxLabels = 200
	// DefaultLabelConcurrency bounds concurrent label-similarity searches (each
	// already fans out internally; the box is RAM-constrained).
	DefaultLabelConcurrency = 2
	// DefaultFaceBudget is how many named subjects one rebuild may scan. It is
	// the bound that keeps the queue off the library's growth curve: the cost of
	// a rebuild is subjects × exemplars × faces, so the subject count is the only
	// factor a batch of questions has no business paying for.
	//
	// Twenty-four, raised from eight, and the raise is what the default kind
	// shares pay for. A window of eight regularly yielded candidates from only
	// one or two people — most subjects have nothing unassigned left in the band
	// — and a pool drawn from two people cannot be mixed into a round that does
	// not keep coming back to them. The scan still stops the moment the pool is
	// full, so a library where the first few subjects are productive costs
	// exactly what it did before; the wider window only buys more chances when
	// they are not.
	DefaultFaceBudget = 24
	// DefaultLabelBudget is how many labels one rebuild may scan, for the same
	// reason (each label search is itself a per-member kNN fan-out).
	DefaultLabelBudget = 6
	// DefaultBuildTimeout caps how long one rebuild may run. It is the backstop
	// behind the budgets: whatever a single subject or label turns out to cost,
	// GET /review/queue answers rather than holding the request open.
	DefaultBuildTimeout = 15 * time.Second
	// DefaultMaxPerEntity is how many questions about one subject or one label
	// may enter a single batch. It is the variety knob: with the default batch of
	// 20 it forces a rebuild to draw on at least five different people or labels,
	// which is what stops the game being twenty questions about the same label in
	// a row. Four rather than one, because a couple of questions about the same
	// face in a row is easier for the player, not harder — see maxSameEntityRun.
	// Raising it makes a rebuild cheaper and the game more repetitive; lowering it
	// does the opposite.
	DefaultMaxPerEntity = 4
	// DefaultOutlierBudget is how many subjects one rebuild ranks for outliers.
	// Ranking a subject means loading every face assigned to them and scoring it
	// against a trimmed centroid, which is the most expensive per-entity read of
	// the five kinds, so the window is smaller than the face sweep's.
	DefaultOutlierBudget = 4
	// DefaultRoundSize is how many questions one round holds. Ten, because a
	// round has to be short enough that finishing one is a decision the player
	// makes several times an evening rather than a commitment they weigh up
	// once — and because the between-rounds summary is only interesting if it
	// arrives often enough to notice.
	DefaultRoundSize = 10
	// DefaultRoundMaxPerEntity is how many questions about one subject or label a
	// single round may hold. Three of ten, deliberately tighter than
	// MaxPerEntity's share of the whole pool: the pool is material a rebuild
	// gathered, the round is what a player actually sits through, and the
	// monotony complaint was always about the second.
	DefaultRoundMaxPerEntity = 3
	// DefaultOutlierThreshold is the minimum cosine distance from a person's
	// centroid a face must have before the game asks about it. Two embeddings of
	// different people sit around 1.0, so half of that is comfortably past "this
	// is a bad photo of the right person" — which matters more here than
	// elsewhere, because the question is about an assignment somebody already
	// made, and asking about ten correct ones to find one wrong one is how a
	// player learns to answer yes without looking.
	DefaultOutlierThreshold = 0.5
)

const (
	// maxBatch is the hard cap on the per-request batch size.
	maxBatch = 100
	// maxQueued is the hard cap on how many questions one rebuild caches for a
	// user. Several batches deep is all the prefetch needs; beyond that a queue is
	// just photo records pinned in memory until the session is pruned.
	maxQueued = 5 * maxBatch
	// labelCandidateLimit is how many candidates one label search may return;
	// it matches expand's maximum so band candidates are not truncated away
	// behind the too-certain ones that sort first.
	labelCandidateLimit = 200
	// sessionIdleTTL is how long an untouched per-user session (its skip set
	// and counters) survives before being pruned.
	sessionIdleTTL = 12 * time.Hour
	// maxRebuildRounds is how many rotating windows one rebuild may try before
	// it accepts that it has nothing. It exists so "never come back empty while
	// a candidate exists somewhere" cannot turn into a request that walks the
	// whole library: the rounds share one BuildTimeout, and three of them is
	// enough to get past a run of exhausted windows without the deadline ever
	// becoming the thing that stops it in the normal case.
	maxRebuildRounds = 3
)

// Sentinel errors returned for client mistakes.
var (
	// ErrInvalidQuestion indicates a malformed question id.
	ErrInvalidQuestion = errors.New("review: invalid question id")
	// ErrInvalidAnswer indicates an answer outside yes/no/skip.
	ErrInvalidAnswer = errors.New("review: invalid answer")
	// ErrInvalidSource indicates a queue source outside people/labels/both.
	ErrInvalidSource = errors.New("review: invalid source")
)

// Kind tells the question types apart.
type Kind string

// The question kinds served by the queue.
const (
	// KindFace asks whether an unnamed face is a given subject.
	KindFace Kind = "face"
	// KindLabel asks whether a photo should carry a given label.
	KindLabel Kind = "label"
	// KindPlace asks whether a photo with an estimated location really was taken
	// at the place those coordinates name.
	KindPlace Kind = "place"
	// KindDuplicate asks whether two near-identical photos are the same shot.
	KindDuplicate Kind = "duplicate"
	// KindOutlier asks whether a face already assigned to a person, but sitting
	// far from that person's centroid, really is them.
	KindOutlier Kind = "outlier"
)

// Kinds lists every question kind, in the order a batch interleaves them. The
// order is fixed rather than incidental: it is the tie-break of the merge, so a
// rebuild over an unchanged library stays byte-for-byte reproducible.
var Kinds = []Kind{KindFace, KindLabel, KindPlace, KindDuplicate, KindOutlier}

// Answer is the player's verdict on one question.
type Answer string

// The accepted answer values.
const (
	// AnswerYes confirms the question: assign the face or attach the label.
	AnswerYes Answer = "yes"
	// AnswerNo rejects the question permanently via a persisted rejection.
	AnswerNo Answer = "no"
	// AnswerSkip means "don't know": the question is shelved for this session
	// but never recorded as a rejection.
	AnswerSkip Answer = "skip"
)

// Result values reported by AnswerResult.Result.
const (
	resultAssigned        = "assigned"
	resultLabeled         = "labeled"
	resultRejected        = "rejected"
	resultConfirmed       = "confirmed"
	resultCleared         = "cleared"
	resultDetached        = "detached"
	resultSkipped         = "skipped"
	resultAlreadyAnswered = "already_answered"
	resultGone            = "gone"
)

// Reason values reported by QueueResult.Reason when the queue is empty.
const (
	// ReasonNoSources means the library has no named people and no labels yet,
	// so there is nothing to ask about.
	ReasonNoSources = "no_people_no_labels"
	// ReasonNoPeople means the game was restricted to people but the library has
	// no named subjects — the chosen source itself is empty, not the band.
	ReasonNoPeople = "no_people"
	// ReasonNoLabels means the game was restricted to labels but the library has
	// no label with photos on it that is still switched on for the game.
	ReasonNoLabels = "no_labels"
	// ReasonNoCandidates means sources exist but neither tier produced a
	// candidate — across every window the rebuild rotated through, so it is not
	// merely "this tier is exhausted here".
	ReasonNoCandidates = "no_candidates"
)

// Question is one yes/no/skip decision served to the player.
type Question struct {
	// ID is the stable, content-derived question id the answer endpoint takes.
	ID string `json:"id"`
	// Kind is "face" or "label".
	Kind Kind `json:"kind"`
	// Tier is which confidence tier the question was drawn from: "sure" (at or
	// above the confident floor — the answer is almost certainly yes) or "band"
	// (the uncertainty band). It is exposed so an operator can see what the mix
	// actually is; the UI asks the same question either way.
	Tier string `json:"tier,omitempty"`
	// Confidence is the candidate's 0–1 confidence (1 − cosine distance),
	// shown by the UI.
	Confidence float64 `json:"confidence"`
	// Photo is the full catalog record with media URLs stamped.
	Photo photos.Photo `json:"photo"`
	// Subject is the person under question (face questions only).
	Subject *people.Subject `json:"subject,omitempty"`
	// FaceIndex is the face's per-photo slot (face questions only).
	FaceIndex *int `json:"face_index,omitempty"`
	// BBox is the face's bounding box, pixel and display-relative, honouring
	// EXIF orientation (face questions only).
	BBox *candidates.FaceBox `json:"bbox,omitempty"`
	// Action is what confirming would do: "create_marker" when the face has no
	// marker yet, "assign_person" when a marker exists (face questions only).
	Action string `json:"action,omitempty"`
	// MarkerUID is the existing marker a yes would assign (face questions with
	// Action "assign_person" only).
	MarkerUID string `json:"marker_uid,omitempty"`
	// Label is the label under question (label questions only).
	Label *organize.Label `json:"label,omitempty"`
	// Place is the estimated location under question (place questions only).
	Place *PlaceGuess `json:"place,omitempty"`
	// Other is the second photo of the pair (duplicate questions only).
	Other *photos.Photo `json:"other,omitempty"`
	// GroupID is the duplicate group the pair belongs to (duplicate questions
	// only). It is what the variety rules treat as the question's entity, so one
	// crowded group cannot own a batch.
	GroupID string `json:"group_id,omitempty"`
	// Distance is the face's cosine distance from its subject's centroid —
	// how much of an outlier it is (outlier questions only).
	Distance float64 `json:"distance,omitempty"`
}

// PlaceGuess is the estimated location a place question asks about: the
// coordinates the estimator inferred and the name they reverse-geocoded to.
type PlaceGuess struct {
	// Name is the most specific human-readable name of the place — the place
	// name, else the city, else the region, else the country. It is never empty:
	// a coordinate nobody has geocoded cannot be put into a question, so those
	// photos are not asked about at all.
	Name string `json:"name"`
	// Country, City and PlaceName are the cached hierarchy behind Name, so a UI
	// can show the fuller address under the question without a second request.
	Country   string `json:"country,omitempty"`
	City      string `json:"city,omitempty"`
	PlaceName string `json:"place_name,omitempty"`
	// Lat and Lng are the estimated coordinates themselves.
	Lat float64 `json:"lat"`
	Lng float64 `json:"lng"`
}

// RoundInfo describes the round a batch belongs to. One request is one round —
// the questions array *is* the round — so a client needs no boundary markers
// inside it: Index says which round this is, Size how long it was minted, and
// Remaining how much of it is still unanswered, which is what tells a client
// whether it is starting a round or resuming one it already showed.
//
// The composition counts are the between-rounds summary: what the round asked
// about and in what mix. They are the round's own numbers, fixed when it was
// minted, so a summary shown at the end of a round reports what the player just
// played rather than what is left of it.
type RoundInfo struct {
	// Index is the round's 1-based number within this session.
	Index int `json:"index"`
	// Size is how many questions the round was minted with.
	Size int `json:"size"`
	// Remaining is how many of them are still unanswered — the length of the
	// questions array in this response.
	Remaining int `json:"remaining"`
	// Kinds counts the round's questions per kind ("face", "label", …).
	Kinds map[string]int `json:"kinds,omitempty"`
	// Sure and Band count the round's confident-tier and uncertainty-band
	// questions. The three kinds that carry no tier count toward neither.
	Sure int `json:"sure"`
	Band int `json:"band"`
	// Entities is how many distinct people, labels, places and duplicate groups
	// the round asked about — the number the variety rules exist to raise.
	Entities int `json:"entities"`
	// Last reports that nothing is queued beyond this round, so a client can tell
	// "take a breath" from "that was everything for now".
	Last bool `json:"last"`
}

// Breather is a non-question card the game can show alongside a round: a photo
// somebody already liked, with its title and year, and nothing to answer.
//
// It is carried outside the questions array and tagged with a Kind of its own so
// no client can mistake one for a question, and it has no id the answer endpoint
// would accept. See breathers.go for why the game has one at all.
type Breather struct {
	// Kind is always BreatherKind.
	Kind string `json:"kind"`
	// Photo is the full catalog record with media URLs stamped.
	Photo photos.Photo `json:"photo"`
	// Title is the photo's title, falling back to its file name.
	Title string `json:"title"`
	// Year is the capture year, omitted for an undated photo.
	Year int `json:"year,omitempty"`
	// Reason says why the photo qualified: BreatherReasonFavorite or
	// BreatherReasonRated.
	Reason string `json:"reason"`
}

// Reveal is the small payoff a confirmed face assignment carries back: what the
// player just added to, in the person's own terms. It is read after the write,
// from one indexed query, and its absence is never an error — a reveal that
// could not be read simply is not shown.
type Reveal struct {
	// SubjectUID and Name identify the person the answer assigned a face to.
	SubjectUID string `json:"subject_uid"`
	Name       string `json:"name"`
	// PhotoCount is how many visible photos they now appear on.
	PhotoCount int `json:"photo_count"`
	// OldestYear and NewestYear span their dated photos; both zero when none of
	// their photos carries a date.
	OldestYear int `json:"oldest_year,omitempty"`
	NewestYear int `json:"newest_year,omitempty"`
}

// QueueResult is one round of questions plus the session counters.
type QueueResult struct {
	// Questions is the round, mixed for variety (see mixer.go).
	Questions []Question `json:"questions"`
	// Round describes the round the questions form: where it sits in the session
	// and what it is made of.
	Round RoundInfo `json:"round"`
	// Breathers are the round's non-question cards, empty when the library has
	// nothing worth pausing on (or no breather source is wired).
	Breathers []Breather `json:"breathers,omitempty"`
	// Source is the applied question source, echoed back so a client can tell a
	// batch built for the selection it is showing from one that is already stale.
	Source Source `json:"source"`
	// Answered is how many questions this session answered so far.
	Answered int `json:"answered"`
	// Remaining estimates how many candidates are still queued (the cached
	// queue's length — not recomputed per answer).
	Remaining int `json:"remaining"`
	// Reason explains an empty queue: ReasonNoSources, ReasonNoPeople,
	// ReasonNoLabels or ReasonNoCandidates.
	Reason string `json:"reason,omitempty"`
}

// AnswerResult reports what one answer did plus the session counters.
type AnswerResult struct {
	// Result names the write the answer produced: assigned (face yes), labeled
	// (label yes), confirmed (duplicate or outlier yes, place accept), cleared
	// (place no), detached (outlier no), rejected (face or label no, duplicate
	// dismissal), skipped, already_answered, or gone — the question's target
	// vanished, and the UI simply moves on.
	Result string `json:"result"`
	// Answered is how many questions this session answered so far.
	Answered int `json:"answered"`
	// Remaining estimates how many questions are still queued.
	Remaining int `json:"remaining"`
	// Reveal is present only when the answer confirmed a face assignment: what
	// the person's collection looks like now that it holds one more photo.
	Reveal *Reveal `json:"reveal,omitempty"`
}

// Sweeper scans face candidates over a bounded window of the named subjects;
// *sweep.Service satisfies it. The queue deliberately depends on the bounded
// form, not on the full sweep behind GET /faces/sweep: filling one batch of
// questions must never cost a library-wide scan.
type Sweeper interface {
	// Scan runs the per-subject candidate search over a window of the named
	// subjects, hands each subject's actionable candidates to collect, and stops
	// as soon as collect reports it has enough.
	Scan(ctx context.Context, params sweep.Params, win sweep.Window,
		collect sweep.Collect) (sweep.Coverage, error)
}

// Expander finds photos similar to a label's members; *expand.Service
// satisfies it.
type Expander interface {
	// Label returns photos similar to the label's members, excluding members
	// and rejected photos.
	Label(ctx context.Context, labelUID string, req expand.Request) (expand.Result, error)
}

// OrganizeStore is the slice of *organize.Store the review game needs.
type OrganizeStore interface {
	// ListLabels returns all labels with their photo counts.
	ListLabels(ctx context.Context) ([]organize.LabelCount, error)
	// AttachLabelAudited attaches a label to a photo, writing the audit entry
	// in the same transaction.
	AttachLabelAudited(ctx context.Context, photoUID, labelUID string,
		source organize.LabelSource, uncertainty int, entry audit.Entry) error
}

// FaceStore is the slice of *vectors.Store the review game needs.
type FaceStore interface {
	// FacesByKeys returns the faces for the given (photo, index) keys; missing
	// keys are simply absent from the result.
	FacesByKeys(ctx context.Context, keys []vectors.FaceKey) ([]vectors.Face, error)
}

// FeedbackStore is the slice of *feedback.Store the review game needs. Every
// method records an opinion and mutates nothing it is about — which is exactly
// why the game may call them: a game must never quietly merge or delete.
type FeedbackStore interface {
	// RejectFace persists "this face is not this subject"; idempotent.
	RejectFace(ctx context.Context, key feedback.FaceRejectionKey, entry audit.Entry) error
	// RejectLabel persists "this photo should not carry this label"; idempotent.
	RejectLabel(ctx context.Context, key feedback.LabelRejectionKey, entry audit.Entry) error
	// ConfirmFace persists "this assigned face really is this subject", which
	// takes it out of the outlier ranking for good; idempotent.
	ConfirmFace(ctx context.Context, key feedback.FaceConfirmationKey, entry audit.Entry) error
	// ConfirmDuplicate persists "these two really are the same shot". It merges
	// nothing; idempotent.
	ConfirmDuplicate(ctx context.Context, key feedback.DuplicateConfirmationKey, entry audit.Entry) error
	// DismissDuplicate persists "these two are genuinely different", which drops
	// the edge from every later duplicate scan; idempotent.
	DismissDuplicate(ctx context.Context, key feedback.DuplicateDismissalKey, entry audit.Entry) error
}

// PlaceReviewer lists the locations the estimator guessed and applies a verdict
// to one; *geoestimate.Reviewer satisfies it. A nil one switches the place check
// off — the queue simply never asks that kind of question.
type PlaceReviewer interface {
	// Pending returns one window of the photos awaiting a verdict plus the total
	// size of that set.
	Pending(ctx context.Context, offset, limit int) ([]geoestimate.Pending, int, error)
	// Accept keeps an estimated location and promotes it to a decision.
	Accept(ctx context.Context, photoUID string, meta audit.Meta) error
	// Reject throws an estimated location away, leaving the tombstone that stops
	// the estimator handing it back.
	Reject(ctx context.Context, photoUID string, meta audit.Meta) error
}

// DuplicateFinder returns a page of near-duplicate groups; *duplicates.Service
// satisfies it. A nil one switches the duplicate check off.
type DuplicateFinder interface {
	// FindGroups returns one page of duplicate groups, largest and
	// human-confirmed first.
	FindGroups(ctx context.Context, limit, offset int) (duplicates.Result, error)
}

// OutlierRanker ranks one subject's assigned faces by distance from that
// subject's centroid; *outliers.Service satisfies it. A nil one switches the
// outlier check off.
type OutlierRanker interface {
	// Outliers returns the subject's faces, most suspicious first, narrowed by
	// opts and with the already-confirmed ones excluded.
	Outliers(ctx context.Context, subjectUID string, opts outliers.Options) (outliers.Result, error)
}

// SubjectLister lists the subjects the outlier rotation walks; *people.Store
// satisfies it.
type SubjectLister interface {
	// ListSubjects returns every subject with its non-invalid marker count.
	ListSubjects(ctx context.Context) ([]people.SubjectCount, error)
}

// PhotoStore hydrates the catalogue records the duplicate and outlier questions
// are about; *photos.Store satisfies it. The face and label searches hand their
// photos over already built, but a duplicate group carries only comparison
// fields and an outlier face only a photo uid.
type PhotoStore interface {
	// ListByUIDs returns the photos for the given uids, ignoring unknown ones.
	ListByUIDs(ctx context.Context, uids []string) ([]photos.Photo, error)
}

// AlbumMembership reports which albums a set of photos belongs to;
// *organize.Store satisfies it. The mixer uses it for one rule only — do not put
// two questions about photos from the same album next to each other — so a nil
// one (or a lookup that fails) switches that rule off and changes nothing else.
type AlbumMembership interface {
	// AlbumUIDsForPhotos returns the album uids of each given photo; photos in no
	// album are absent from the map.
	AlbumUIDsForPhotos(ctx context.Context, photoUIDs []string) (map[string][]string, error)
}

// BreatherSource picks the photos a round's breather card can show;
// *review.BreatherStore satisfies it. A nil one simply means no breathers.
type BreatherSource interface {
	// PickBreathers returns up to limit high-rated or favourited photos for the
	// user, at most one per era and newest era first.
	PickBreathers(ctx context.Context, userUID string, limit int) ([]BreatherPick, error)
}

// SubjectStatsReader reads one person's headline numbers for the answer reveal;
// *people.Store satisfies it. A nil one switches the reveal off.
type SubjectStatsReader interface {
	// SubjectStats returns the subject's visible photo count and year span.
	SubjectStats(ctx context.Context, subjectUID string) (people.SubjectStats, error)
}

// Assigner applies the existing face-assignment state machine; *facematch.Service
// satisfies it.
type Assigner interface {
	// Apply runs one assignment action (create_marker / assign_person) with
	// its audit entry in the marker mutation's transaction.
	Apply(ctx context.Context, req facematch.AssignRequest, meta audit.Meta) (facematch.AssignResult, error)
}

// Config assembles a Service. All store/service fields are required; numeric
// fields fall back to the package defaults when non-positive.
type Config struct {
	// Sweeper streams face candidates across named subjects.
	Sweeper Sweeper
	// Expander runs the label-similarity search.
	Expander Expander
	// Organize lists labels and attaches confirmed ones.
	Organize OrganizeStore
	// Faces resolves a question's face at answer time.
	Faces FaceStore
	// Feedback persists the opinions the answers record.
	Feedback FeedbackStore
	// Assigner applies yes answers on faces and no answers on outliers.
	Assigner Assigner
	// Skips remembers a player's "I don't know" about a person across sessions;
	// nil switches the persisted memory off and leaves skips shelved for the
	// current session only.
	Skips SkipStore
	// Places supplies and settles the estimated-location questions; nil switches
	// the place check off.
	Places PlaceReviewer
	// Duplicates supplies the near-duplicate pairs; nil switches the duplicate
	// check off.
	Duplicates DuplicateFinder
	// Outliers ranks a subject's assigned faces; nil switches the outlier check
	// off, as does a nil Subjects.
	Outliers OutlierRanker
	// Subjects lists the subjects the outlier rotation walks.
	Subjects SubjectLister
	// Photos hydrates the photo records of duplicate and outlier questions; nil
	// switches both of those checks off.
	Photos PhotoStore
	// Albums reports the album membership the mixer spreads a round over; nil
	// switches that one variety rule off.
	Albums AlbumMembership
	// Breathers picks the round's non-question cards; nil switches them off.
	Breathers BreatherSource
	// Stats reads the person behind a confirmed assignment for the answer's
	// reveal; nil switches the reveal off.
	Stats SubjectStatsReader
	// Media stamps thumbnail/download URLs onto the photos the three new
	// question kinds carry. A nil builder yields the application's own routes.
	Media *mediaurl.Builder
	// Log receives non-fatal rebuild warnings; nil means slog.Default().
	Log *slog.Logger
	// BandMin is the inclusive lower confidence bound of the uncertainty band.
	BandMin float64
	// BandMax is the exclusive upper confidence bound of the uncertainty band.
	BandMax float64
	// SureMin is the inclusive lower bound of the confident tier; it is clamped
	// up to BandMax so the tiers stay disjoint.
	SureMin float64
	// SureShare is the fraction of a batch drawn from the confident tier; a
	// value outside (0, 1) falls back to the default.
	SureShare float64
	// QueueSize is how many questions one rebuild gathers into the pool a round
	// is mixed from.
	QueueSize int
	// RoundSize is how many questions one round holds — the default size of a
	// Queue response.
	RoundSize int
	// RoundMaxPerEntity caps how many questions about one subject or label a
	// single round may hold.
	RoundMaxPerEntity int
	// CacheTTL is how long a built queue is reused before rebuilding.
	CacheTTL time.Duration
	// MaxLabels caps how many labels one rebuild considers.
	MaxLabels int
	// LabelConcurrency bounds concurrent label-similarity searches.
	LabelConcurrency int
	// FaceBudget caps how many named subjects one rebuild scans.
	FaceBudget int
	// LabelBudget caps how many labels one rebuild scans.
	LabelBudget int
	// BuildTimeout caps how long one rebuild may run before it serves what it
	// has.
	BuildTimeout time.Duration
	// MaxPerEntity caps how many questions about one subject or one label may
	// enter a batch.
	MaxPerEntity int
	// OutlierBudget caps how many subjects one rebuild ranks for outliers;
	// non-positive means DefaultOutlierBudget.
	OutlierBudget int
	// OutlierThreshold is the minimum centroid distance a face must have to be
	// worth asking about; non-positive means DefaultOutlierThreshold.
	OutlierThreshold float64
	// KindShares is the weight of each question kind — which kinds the game may
	// ask about and in what proportion. A kind at zero or absent is switched off
	// entirely; a set that switches everything off falls back to faces only.
	KindShares map[Kind]float64
	// SkipMuteThreshold is how many "don't know" answers about one person mute
	// them for that player; non-positive means DefaultSkipMuteThreshold.
	SkipMuteThreshold int
	// SkipMuteCooldown is how long the first mute lasts before the game may ask
	// about that person once more, doubling with every further skip;
	// non-positive means DefaultSkipMuteCooldown.
	SkipMuteCooldown time.Duration
	// Now overrides the clock in tests; nil means time.Now.
	Now func() time.Time
}

// Service builds review queues and applies answers. It is safe for concurrent
// use; per-user session state lives in memory.
type Service struct {
	sweeper    Sweeper
	expander   Expander
	organize   OrganizeStore
	faces      FaceStore
	feedback   FeedbackStore
	assigner   Assigner
	skips      SkipStore
	places     PlaceReviewer
	duplicates DuplicateFinder
	outliers   OutlierRanker
	subjects   SubjectLister
	photos     PhotoStore
	albums     AlbumMembership
	breathers  BreatherSource
	stats      SubjectStatsReader
	media      *mediaurl.Builder
	log        *slog.Logger

	bandMin           float64
	bandMax           float64
	sureMin           float64
	sureShare         float64
	queueSize         int
	roundSize         int
	roundMaxPerEntity int

	cacheTTL         time.Duration
	maxLabels        int
	labelConcurrency int
	faceBudget       int
	labelBudget      int
	buildTimeout     time.Duration
	maxPerEntity     int
	outlierBudget    int
	outlierThreshold float64

	shares            kindShares
	skipMuteThreshold int
	skipMuteCooldown  time.Duration

	now func() time.Time

	mu       sync.Mutex
	sessions map[string]*session

	// cursorMu guards the rotation cursors below. They are instance-wide, not
	// per user: every rebuild advances them, so consecutive rebuilds walk
	// successive windows of the library instead of re-reading its head. There is
	// one per question kind, because the kinds are scanned independently and a
	// shared cursor would make one kind's progress skip another's work.
	cursorMu      sync.Mutex
	faceCursor    int
	labelCursor   int
	placeCursor   int
	dupCursor     int
	outlierCursor int
}

// faceOffset returns the subject-rotation cursor for the next rebuild.
func (s *Service) faceOffset() int {
	s.cursorMu.Lock()
	defer s.cursorMu.Unlock()
	return s.faceCursor
}

// advanceFaceOffset moves the subject-rotation cursor to where the scan said the
// next window should start.
func (s *Service) advanceFaceOffset(next int) {
	s.cursorMu.Lock()
	defer s.cursorMu.Unlock()
	s.faceCursor = next
}

// labelOffset returns the label-rotation cursor for the next rebuild.
func (s *Service) labelOffset() int {
	s.cursorMu.Lock()
	defer s.cursorMu.Unlock()
	return s.labelCursor
}

// advanceLabelOffset moves the label-rotation cursor past the labels the last
// rebuild scanned.
func (s *Service) advanceLabelOffset(next int) {
	s.cursorMu.Lock()
	defer s.cursorMu.Unlock()
	s.labelCursor = next
}

// placeOffset returns the estimated-location rotation cursor for the next
// rebuild.
func (s *Service) placeOffset() int {
	s.cursorMu.Lock()
	defer s.cursorMu.Unlock()
	return s.placeCursor
}

// advancePlaceOffset moves the estimated-location cursor past the window the
// last rebuild read.
func (s *Service) advancePlaceOffset(next int) {
	s.cursorMu.Lock()
	defer s.cursorMu.Unlock()
	s.placeCursor = next
}

// dupOffset returns the duplicate-group rotation cursor for the next rebuild.
func (s *Service) dupOffset() int {
	s.cursorMu.Lock()
	defer s.cursorMu.Unlock()
	return s.dupCursor
}

// advanceDupOffset moves the duplicate-group cursor past the page the last
// rebuild read.
func (s *Service) advanceDupOffset(next int) {
	s.cursorMu.Lock()
	defer s.cursorMu.Unlock()
	s.dupCursor = next
}

// outlierOffset returns the outlier-subject rotation cursor for the next
// rebuild.
func (s *Service) outlierOffset() int {
	s.cursorMu.Lock()
	defer s.cursorMu.Unlock()
	return s.outlierCursor
}

// advanceOutlierOffset moves the outlier-subject cursor past the subjects the
// last rebuild ranked.
func (s *Service) advanceOutlierOffset(next int) {
	s.cursorMu.Lock()
	defer s.cursorMu.Unlock()
	s.outlierCursor = next
}

// New assembles a review Service from cfg. It panics when a required
// dependency is nil (a wiring bug, not a runtime condition); out-of-range
// tunables fall back to the package defaults.
func New(cfg Config) *Service {
	requireDeps(cfg)
	svc := &Service{
		sweeper:          cfg.Sweeper,
		expander:         cfg.Expander,
		organize:         cfg.Organize,
		faces:            cfg.Faces,
		feedback:         cfg.Feedback,
		assigner:         cfg.Assigner,
		skips:            cfg.Skips,
		places:           cfg.Places,
		duplicates:       cfg.Duplicates,
		outliers:         cfg.Outliers,
		subjects:         cfg.Subjects,
		photos:           cfg.Photos,
		albums:           cfg.Albums,
		breathers:        cfg.Breathers,
		stats:            cfg.Stats,
		media:            cfg.Media,
		log:              cfg.Log,
		bandMin:          cfg.BandMin,
		bandMax:          cfg.BandMax,
		sureMin:          cfg.SureMin,
		sureShare:        cfg.SureShare,
		queueSize:        orDefaultInt(cfg.QueueSize, DefaultQueueSize),
		roundSize:        orDefaultInt(cfg.RoundSize, DefaultRoundSize),
		cacheTTL:         cfg.CacheTTL,
		maxLabels:        orDefaultInt(cfg.MaxLabels, DefaultMaxLabels),
		labelConcurrency: orDefaultInt(cfg.LabelConcurrency, DefaultLabelConcurrency),
		faceBudget:       orDefaultInt(cfg.FaceBudget, DefaultFaceBudget),
		labelBudget:      orDefaultInt(cfg.LabelBudget, DefaultLabelBudget),
		buildTimeout:     cfg.BuildTimeout,
		maxPerEntity:     orDefaultInt(cfg.MaxPerEntity, DefaultMaxPerEntity),
		roundMaxPerEntity: orDefaultInt(
			cfg.RoundMaxPerEntity, DefaultRoundMaxPerEntity,
		),
		outlierBudget:     orDefaultInt(cfg.OutlierBudget, DefaultOutlierBudget),
		outlierThreshold:  cfg.OutlierThreshold,
		shares:            newKindShares(cfg.KindShares),
		skipMuteThreshold: orDefaultInt(cfg.SkipMuteThreshold, DefaultSkipMuteThreshold),
		skipMuteCooldown:  cfg.SkipMuteCooldown,
		now:               cfg.Now,
		sessions:          make(map[string]*session),
	}
	svc.applyFallbacks()
	return svc
}

// requireDeps panics when a required Config dependency is missing.
func requireDeps(cfg Config) {
	if cfg.Sweeper == nil || cfg.Expander == nil || cfg.Organize == nil ||
		cfg.Faces == nil || cfg.Feedback == nil || cfg.Assigner == nil {
		panic("review: New requires Sweeper, Expander, Organize, Faces, Feedback and Assigner")
	}
}

// applyFallbacks replaces unset or out-of-range tunables with the package
// defaults; an inconsistent band falls back as a pair so it stays non-empty.
//
// SureMin is only bounds-checked here, not compared with the band: sureFloor
// clamps it up to BandMax on every read, so a value below the band is a narrower
// confident tier rather than a configuration error, and the tiers cannot overlap
// however it is set.
func (s *Service) applyFallbacks() {
	if s.log == nil {
		s.log = slog.Default()
	}
	s.applyTierFallbacks()
	if s.outlierThreshold <= 0 || s.outlierThreshold >= 2 {
		s.outlierThreshold = DefaultOutlierThreshold
	}
	if s.cacheTTL <= 0 {
		s.cacheTTL = DefaultCacheTTL
	}
	if s.buildTimeout <= 0 {
		s.buildTimeout = DefaultBuildTimeout
	}
	if s.skipMuteCooldown <= 0 {
		s.skipMuteCooldown = DefaultSkipMuteCooldown
	}
	if s.now == nil {
		s.now = time.Now
	}
}

// applyTierFallbacks bounds-checks the four numbers that decide which candidates
// become questions and in what mix. The band falls back as a pair so it stays
// non-empty; the confident tier's floor and share fall back individually.
func (s *Service) applyTierFallbacks() {
	if s.bandMin <= 0 || s.bandMin >= 1 || s.bandMax <= s.bandMin || s.bandMax > 1 {
		s.bandMin, s.bandMax = DefaultBandMin, DefaultBandMax
	}
	if s.sureMin <= 0 || s.sureMin > 1 {
		s.sureMin = DefaultSureMin
	}
	if s.sureShare <= 0 || s.sureShare >= 1 {
		s.sureShare = DefaultSureShare
	}
}

// bandMid returns the uncertainty band's midpoint, the proxy for the decision
// boundary that question ordering measures distance from.
func (s *Service) bandMid() float64 {
	return (s.bandMin + s.bandMax) / 2
}

// orDefaultInt returns v when positive, else fallback.
func orDefaultInt(v, fallback int) int {
	if v > 0 {
		return v
	}
	return fallback
}
