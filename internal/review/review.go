// Package review builds the "review game" question queue and applies answers.
//
// The game asks one question at a time — "is this face subject X?" over a photo
// with an unnamed face, or "should this photo carry label Y?" over a photo that
// looks like the ones already on that label — and the user answers yes, no or
// don't-know. Questions are picked from the uncertainty band: candidates whose
// confidence (1 − cosine distance) falls inside [BandMin, BandMax). Below the
// band the guess is noise and the question demoralising; at or above BandMax the
// candidate is confirmed in bulk on the /recognition or /expand pages instead,
// so asking one-by-one would waste the player's time. Inside the band a human
// answer buys the most: questions are ordered by distance from the band's
// midpoint (closest to the decision boundary first) and the two kinds are
// interleaved deterministically, skewed toward whichever kind has more
// candidates.
//
// The queue composes the existing read-only searches — the recognition sweep
// (per-subject face candidates across all named subjects, which already excludes
// assigned faces, persisted rejections, negative exemplars and sub-reviewable
// faces) and the label-similarity search (which already excludes members and
// rejected photos). Answers route through the existing write paths: yes on a
// face goes through the facematch assign state machine, yes on a label through
// the organize attach path, and no records a persisted rejection in feedback.
// The package never opens a second write path.
//
// Built queues are cached per user for CacheTTL so a batch fetch does not rerun
// the expensive vector searches, and answered or skipped questions are tracked
// in an in-memory session: a skip lasts for the session (an idle session is
// pruned after sessionIdleTTL, and a restart forgets skips — deliberately, since
// "don't know" is not "no"), while yes/no answers persist through the underlying
// stores and never come back.
package review

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"

	"github.com/panbotka/kukatko/internal/audit"
	"github.com/panbotka/kukatko/internal/candidates"
	"github.com/panbotka/kukatko/internal/expand"
	"github.com/panbotka/kukatko/internal/facematch"
	"github.com/panbotka/kukatko/internal/feedback"
	"github.com/panbotka/kukatko/internal/organize"
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
	// DefaultBandMax is the upper edge of the uncertainty band: candidates at
	// least this confident belong on the bulk-confirm pages, not in the game.
	DefaultBandMax = 0.75
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
)

const (
	// maxBatch is the hard cap on the per-request batch size.
	maxBatch = 100
	// labelCandidateLimit is how many candidates one label search may return;
	// it matches expand's maximum so band candidates are not truncated away
	// behind the too-certain ones that sort first.
	labelCandidateLimit = 200
	// sessionIdleTTL is how long an untouched per-user session (its skip set
	// and counters) survives before being pruned.
	sessionIdleTTL = 12 * time.Hour
)

// Sentinel errors returned by Answer for client mistakes.
var (
	// ErrInvalidQuestion indicates a malformed question id.
	ErrInvalidQuestion = errors.New("review: invalid question id")
	// ErrInvalidAnswer indicates an answer outside yes/no/skip.
	ErrInvalidAnswer = errors.New("review: invalid answer")
)

// Kind tells face and label questions apart.
type Kind string

// The two question kinds served by the queue.
const (
	// KindFace asks whether an unnamed face is a given subject.
	KindFace Kind = "face"
	// KindLabel asks whether a photo should carry a given label.
	KindLabel Kind = "label"
)

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
	resultSkipped         = "skipped"
	resultAlreadyAnswered = "already_answered"
	resultGone            = "gone"
)

// Reason values reported by QueueResult.Reason when the queue is empty.
const (
	// ReasonNoSources means the library has no named people and no labels yet,
	// so there is nothing to ask about.
	ReasonNoSources = "no_people_no_labels"
	// ReasonNoCandidates means sources exist but no candidate currently falls
	// inside the uncertainty band.
	ReasonNoCandidates = "no_candidates"
)

// Question is one yes/no/skip decision served to the player.
type Question struct {
	// ID is the stable, content-derived question id the answer endpoint takes.
	ID string `json:"id"`
	// Kind is "face" or "label".
	Kind Kind `json:"kind"`
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
}

// QueueResult is one batch of questions plus the session counters.
type QueueResult struct {
	// Questions is the batch, most informative first.
	Questions []Question `json:"questions"`
	// Answered is how many questions this session answered so far.
	Answered int `json:"answered"`
	// Remaining estimates how many candidates are still queued (the cached
	// queue's length — not recomputed per answer).
	Remaining int `json:"remaining"`
	// Reason explains an empty queue: ReasonNoSources or ReasonNoCandidates.
	Reason string `json:"reason,omitempty"`
}

// AnswerResult reports what one answer did plus the session counters.
type AnswerResult struct {
	// Result is one of assigned, labeled, rejected, skipped, already_answered
	// or gone (the question's target vanished; the UI moves on).
	Result string `json:"result"`
	// Answered is how many questions this session answered so far.
	Answered int `json:"answered"`
	// Remaining estimates how many questions are still queued.
	Remaining int `json:"remaining"`
}

// Sweeper streams face candidates across all named subjects; *sweep.Service
// satisfies it.
type Sweeper interface {
	// Sweep runs the per-subject candidate search across named subjects and
	// streams events to emit.
	Sweep(ctx context.Context, params sweep.Params, emit func(sweep.Event) error) error
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

// FeedbackStore is the slice of *feedback.Store the review game needs.
type FeedbackStore interface {
	// RejectFace persists "this face is not this subject"; idempotent.
	RejectFace(ctx context.Context, key feedback.FaceRejectionKey, entry audit.Entry) error
	// RejectLabel persists "this photo should not carry this label"; idempotent.
	RejectLabel(ctx context.Context, key feedback.LabelRejectionKey, entry audit.Entry) error
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
	// Feedback persists rejections for no answers.
	Feedback FeedbackStore
	// Assigner applies yes answers on faces.
	Assigner Assigner
	// Log receives non-fatal rebuild warnings; nil means slog.Default().
	Log *slog.Logger
	// BandMin is the inclusive lower confidence bound of the uncertainty band.
	BandMin float64
	// BandMax is the exclusive upper confidence bound of the uncertainty band.
	BandMax float64
	// QueueSize is the default batch size for Queue.
	QueueSize int
	// CacheTTL is how long a built queue is reused before rebuilding.
	CacheTTL time.Duration
	// MaxLabels caps how many labels one rebuild scans.
	MaxLabels int
	// LabelConcurrency bounds concurrent label-similarity searches.
	LabelConcurrency int
	// Now overrides the clock in tests; nil means time.Now.
	Now func() time.Time
}

// Service builds review queues and applies answers. It is safe for concurrent
// use; per-user session state lives in memory.
type Service struct {
	sweeper  Sweeper
	expander Expander
	organize OrganizeStore
	faces    FaceStore
	feedback FeedbackStore
	assigner Assigner
	log      *slog.Logger

	bandMin          float64
	bandMax          float64
	queueSize        int
	cacheTTL         time.Duration
	maxLabels        int
	labelConcurrency int
	now              func() time.Time

	mu       sync.Mutex
	sessions map[string]*session
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
		log:              cfg.Log,
		bandMin:          cfg.BandMin,
		bandMax:          cfg.BandMax,
		queueSize:        orDefaultInt(cfg.QueueSize, DefaultQueueSize),
		cacheTTL:         cfg.CacheTTL,
		maxLabels:        orDefaultInt(cfg.MaxLabels, DefaultMaxLabels),
		labelConcurrency: orDefaultInt(cfg.LabelConcurrency, DefaultLabelConcurrency),
		now:              cfg.Now,
		sessions:         make(map[string]*session),
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
func (s *Service) applyFallbacks() {
	if s.log == nil {
		s.log = slog.Default()
	}
	if s.bandMin <= 0 || s.bandMin >= 1 || s.bandMax <= s.bandMin || s.bandMax > 1 {
		s.bandMin, s.bandMax = DefaultBandMin, DefaultBandMax
	}
	if s.cacheTTL <= 0 {
		s.cacheTTL = DefaultCacheTTL
	}
	if s.now == nil {
		s.now = time.Now
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
