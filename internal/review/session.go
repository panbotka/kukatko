package review

// Per-user session state: the cached queue plus the answered/skipped sets that
// keep questions from repeating. Everything here is in-memory by design — the
// durable outcomes of the game (assignments, labels, rejections) live in the
// underlying stores, so losing a session on restart only forgets which questions
// were shelved for it and the session counter.
//
// One thing used to be lost with it that should not have been: "I don't know"
// about a person. That now has a durable home of its own (skips.go); what the
// session still owns is the shelf — a question put aside for this sitting,
// whatever its kind.
//
// Only the cached queue belongs to one question source; the answered/skipped
// sets and the counters span all of them, because a skipped question is skipped
// whichever selection happened to surface it.

import (
	"sync"
	"time"
)

// session holds one user's cached queue and bookkeeping. Its mutex serialises
// queue rebuilds with answer bookkeeping for that user; different users never
// contend.
type session struct {
	mu sync.Mutex
	// queue is the ordered list of questions not yet answered or skipped.
	queue []Question
	// hasQueue reports whether queue was ever built (an empty built queue is
	// still a valid cache entry).
	hasQueue bool
	// source is the selection queue was built for. The cache is keyed on it as
	// much as on time: a batch of face questions is worthless to a player who
	// has just switched the game to labels.
	source Source
	// builtAt is when queue was last rebuilt, for the CacheTTL check.
	builtAt time.Time
	// reason explains an empty queue (ReasonNoSources / ReasonNoCandidates).
	reason string
	// roundLen is how many questions at the head of queue form the current
	// round. The round is not held apart from the pool: it *is* the pool's head,
	// which keeps one list to answer against and makes "the round shrinks as it
	// is answered" fall out of the same bookkeeping as everything else.
	roundLen int
	// round summarises the current round as it was minted, so a summary shown
	// between rounds reports what the player played rather than what is left.
	round RoundInfo
	// roundSeq counts the rounds minted this session. It seeds the mixer, so two
	// consecutive rounds over an unchanged pool differ from each other.
	roundSeq int
	// breathers are the current round's non-question cards.
	breathers []Breather
	// albums is the album membership of the pool's photos, read once per rebuild
	// for the mixer's album rule; nil means the rule is off.
	albums map[string][]string
	// skips is the player's persisted "I don't know" memory as the last rebuild
	// read it: which people they have given up on and on which photos. It is
	// cached with the pool rather than re-read per question, and a nil one
	// silences nothing (see skips.go).
	skips SkipMemory
	// answered marks question ids already answered yes/no (or found gone).
	answered map[string]bool
	// skipped marks question ids shelved for this session.
	skipped map[string]bool
	// answeredCount is the session counter of yes/no answers.
	answeredCount int
	// lastSeen drives idle pruning.
	lastSeen time.Time
}

// session returns the caller's session, creating it on first use and pruning
// sessions idle beyond sessionIdleTTL.
func (s *Service) session(userUID string) *session {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now()
	for uid, sess := range s.sessions {
		if uid != userUID && now.Sub(sess.lastSeen) > sessionIdleTTL {
			delete(s.sessions, uid)
		}
	}
	sess, ok := s.sessions[userUID]
	if !ok {
		sess = &session{
			answered: make(map[string]bool),
			skipped:  make(map[string]bool),
		}
		s.sessions[userUID] = sess
	}
	sess.lastSeen = now
	return sess
}

// seen reports whether the session already consumed the question (answered or
// skipped), so rebuilds and batches never serve it again.
func (sess *session) seen(questionID string) bool {
	return sess.answered[questionID] || sess.skipped[questionID]
}

// consume records the outcome of one answer under the session lock: it marks
// the question seen, drops it from the cached queue, and bumps the counter for
// countable (yes/no) outcomes answered for the first time. A question dropped
// from inside the current round shortens the round too, which is what makes the
// next batch fetch serve the rest of it rather than mint a new one.
func (sess *session) consume(questionID, result string, countIt bool) AnswerResult {
	sess.mu.Lock()
	defer sess.mu.Unlock()
	if result == resultSkipped {
		sess.skipped[questionID] = true
	} else {
		if countIt && !sess.answered[questionID] {
			sess.answeredCount++
		}
		sess.answered[questionID] = true
	}
	sess.dropFromQueue(questionID)
	return AnswerResult{Result: result, Answered: sess.answeredCount, Remaining: len(sess.queue)}
}

// dropFromQueue removes one question from the cached queue, shrinking the round
// when the question was part of it. The caller holds sess.mu.
func (sess *session) dropFromQueue(questionID string) {
	for i, q := range sess.queue {
		if q.ID != questionID {
			continue
		}
		sess.queue = append(sess.queue[:i:i], sess.queue[i+1:]...)
		if i < sess.roundLen {
			sess.roundLen--
		}
		return
	}
}

// albumsOf reports which albums a photo belongs to, as the last rebuild read it.
// It is the mixer's album lookup; an unknown photo (or no album store) simply
// yields nothing, which switches the rule off for that pair.
func (sess *session) albumsOf(photoUID string) []string {
	return sess.albums[photoUID]
}

// alreadyAnswered reports whether the question was already answered yes/no in
// this session, making a repeated answer a no-op.
func (sess *session) alreadyAnswered(questionID string) bool {
	sess.mu.Lock()
	defer sess.mu.Unlock()
	return sess.answered[questionID]
}
