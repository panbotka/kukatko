package review

// Remembering "I don't know".
//
// Yes and no leave a durable trace of their own — an assignment, a rejection, a
// dismissal — so the game never asks them twice. "Don't know" left none: it
// shelved the question in the in-memory session and was forgotten by the next
// restart, which is why a player who does not recognise somebody kept being
// asked about them.
//
// So a skip on a face is now written down, per (user, subject, photo). Three
// skips about one person mute that person for that player. Two are forgiven: an
// unclear photo is not the same as an unknown face, and a threshold of one would
// turn a single bad crop into a permanent silence.
//
// The mute is a pause, not a verdict:
//
//   - it lasts a cooling-off period, after which the game may ask once more;
//   - the retry only ever lands on a photo the player has not been asked about
//     before, because a different face of the same person deserves its own
//     chance — the skipped photos themselves stay silent for good;
//   - a photo that entered the library after the mute is never suppressed, since
//     a newly imported face of that person is exactly what might be
//     recognisable;
//   - another skip re-mutes, and the pause doubles rather than resetting to the
//     same short wait.
//
// All of that is derived from the rows themselves (see migration 0059), so there
// is no mute state to keep in step with the skip log.
//
// None of it reaches the catalogue. A skip is not a rejection: it never becomes
// an internal/feedback row, never narrows internal/candidates or internal/sweep,
// never touches an identity. It says "don't ask *me* this", never "this is not
// that person" — and it is strictly per user, so one player's "I don't know"
// cannot quiet the game for anybody else.

import (
	"context"
	"math"
	"time"
)

const (
	// DefaultSkipMuteThreshold is how many "don't know" answers about one person
	// mute that person for that player. Three, because two are forgiven: the
	// first two skips are far more often an unclear photo — a blurred profile, a
	// face half behind somebody's shoulder — than a person the player genuinely
	// cannot place, and muting on those would silence people the game only ever
	// asked about badly.
	DefaultSkipMuteThreshold = 3
	// DefaultSkipMuteCooldown is how long the first mute lasts before the game
	// may ask about that person once more. A week: long enough that the player
	// has stopped thinking about the question, short enough that a person they
	// later work out the name of does not stay silent for a season. Each further
	// skip doubles it.
	DefaultSkipMuteCooldown = 7 * 24 * time.Hour
	// maxSkipMuteCooldown caps the doubling. Without it a player who skips one
	// person twenty times would mute them for longer than the library will
	// exist, which is a rejection in all but name — and a skip must never become
	// one. A year is past any plausible "I'll ask my aunt at Christmas".
	maxSkipMuteCooldown = 365 * 24 * time.Hour
)

// SubjectSkips is one player's skip history for one person: which photos they
// could not place them on, and when the most recent of those was. It is the
// whole input to the mute policy — the mute window, its length and the photos it
// covers are all derived from these two fields.
type SubjectSkips struct {
	// Photos is the set of photo uids the player skipped this person on. It is
	// also the "already asked" set: those photos are never offered again for this
	// person, so a retry after the cooling-off period lands on an unseen face.
	Photos map[string]struct{}
	// LastAt is when the newest of those skips was recorded. It doubles as the
	// moment the current mute began, which is what lets a photo imported since
	// then through.
	LastAt time.Time
}

// SkipMemory is what one player has told the game they cannot recognise, keyed
// by subject uid. A nil map — no store wired, or a read that failed — silences
// nothing, which is the right degradation: the game asking one question too many
// is a far smaller failure than it refusing to ask at all.
type SkipMemory map[string]SubjectSkips

// SkipStore records and reads the persisted skips; *review.SkipRecorder
// satisfies it. A nil one switches the whole memory off — the in-memory session
// still shelves a skipped question for the session, exactly as before.
type SkipStore interface {
	// RecordSkip remembers that userUID could not place subjectUID on photoUID.
	// It is idempotent: the same face skipped twice is one unresolved photo.
	RecordSkip(ctx context.Context, userUID, subjectUID, photoUID string) error
	// SkipMemory returns everything userUID has skipped, grouped by subject.
	SkipMemory(ctx context.Context, userUID string) (SkipMemory, error)
}

// mutePolicy turns a skip history into a silence. It is a value rather than a
// method set on Service so the rules can be tested without a queue behind them.
type mutePolicy struct {
	// threshold is how many skips about one person mute them.
	threshold int
	// cooldown is how long the first mute lasts; each further skip doubles it.
	cooldown time.Duration
}

// mute describes one person's current silence for one player.
type mute struct {
	// since is when the mute began — the newest skip. A photo added to the
	// library after it is never suppressed.
	since time.Time
	// until is when the game may ask about this person again.
	until time.Time
}

// muteFor returns the silence a skip history currently earns, or ok=false when
// it has not reached the threshold at all.
//
// The level is 1 at the threshold and grows by one with every skip past it, so
// the very first re-skip after a cooling-off period doubles the wait rather than
// serving the same short one again. Without that, a person the player will never
// place would be asked about every cooldown for ever.
func (p mutePolicy) muteFor(skips SubjectSkips) (mute, bool) {
	count := len(skips.Photos)
	if p.threshold <= 0 || count < p.threshold {
		return mute{}, false
	}
	level := count - p.threshold + 1
	return mute{since: skips.LastAt, until: skips.LastAt.Add(p.wait(level))}, true
}

// wait is how long a mute of the given level lasts: the cooldown doubled once
// per level past the first, capped at maxSkipMuteCooldown. The shift is computed
// in float64 so a large level cannot overflow the duration before the cap is
// applied.
func (p mutePolicy) wait(level int) time.Duration {
	if level <= 1 {
		return min(p.cooldown, maxSkipMuteCooldown)
	}
	grown := float64(p.cooldown) * math.Pow(2, float64(level-1))
	if grown >= float64(maxSkipMuteCooldown) {
		return maxSkipMuteCooldown
	}
	return time.Duration(grown)
}

// silences reports whether a question must not be put to this player right now.
//
// Only the two kinds that are about a person can be silenced — a label or a
// duplicate question is not "this person again". Within those, a photo the
// player already skipped for this person is silent for good (that is the "not
// asked about before" rule the post-mute retry needs), and while the mute holds
// so is every other photo of theirs that the library already had when it began.
func (m SkipMemory) silences(q Question, policy mutePolicy, now time.Time) bool {
	if q.Subject == nil || (q.Kind != KindFace && q.Kind != KindOutlier) {
		return false
	}
	skips, ok := m[q.Subject.UID]
	if !ok {
		return false
	}
	if _, asked := skips.Photos[q.Photo.UID]; asked {
		return true
	}
	silence, muted := policy.muteFor(skips)
	if !muted || !now.Before(silence.until) {
		return false
	}
	// A photo the library gained after the mute began is a face the player has
	// never had the chance to recognise, which is the whole reason the mute is a
	// pause rather than a verdict.
	return !q.Photo.CreatedAt.After(silence.since)
}

// skipPolicy is the mute policy the Service was configured with.
func (s *Service) skipPolicy() mutePolicy {
	return mutePolicy{threshold: s.skipMuteThreshold, cooldown: s.skipMuteCooldown}
}

// loadSkipMemory reads one player's skip history for a rebuild. Every failure
// degrades to an empty memory and a warning: the memory exists to ask fewer
// questions, and losing it must not cost a rebuild that has already paid for the
// vector searches.
func (s *Service) loadSkipMemory(ctx context.Context, userUID string) SkipMemory {
	if s.skips == nil {
		return nil
	}
	memory, err := s.skips.SkipMemory(ctx, userUID)
	if err != nil {
		s.log.WarnContext(ctx, "review: reading the skip memory failed",
			"user_uid", userUID, "error", err)
		return nil
	}
	return memory
}

// recordSkip writes down a "don't know" about a person. Only the two kinds that
// name a subject produce one — a skipped label or duplicate question has nothing
// to mute — and the write is best effort: a game answered at one keypress per
// second must not fail because the memory could not be updated, so a failure is
// logged and the skip still counts for the session.
func (s *Service) recordSkip(ctx context.Context, userUID string, ref questionRef) {
	if s.skips == nil || ref.SubjectUID == "" ||
		(ref.Kind != KindFace && ref.Kind != KindOutlier) {
		return
	}
	if err := s.skips.RecordSkip(ctx, userUID, ref.SubjectUID, ref.PhotoUID); err != nil {
		s.log.WarnContext(ctx, "review: remembering a skip failed",
			"user_uid", userUID, "subject_uid", ref.SubjectUID,
			"photo_uid", ref.PhotoUID, "error", err)
	}
}
