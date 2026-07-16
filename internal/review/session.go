package review

// Per-user session state: the cached queue plus the answered/skipped sets that
// keep questions from repeating. Everything here is in-memory by design — the
// durable outcomes of the game (assignments, labels, rejections) live in the
// underlying stores, so losing a session on restart only forgets skips and the
// session counter.

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
	// builtAt is when queue was last rebuilt, for the CacheTTL check.
	builtAt time.Time
	// reason explains an empty queue (ReasonNoSources / ReasonNoCandidates).
	reason string
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
// countable (yes/no) outcomes answered for the first time.
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
	sess.queue = dropQuestion(sess.queue, questionID)
	return AnswerResult{Result: result, Answered: sess.answeredCount, Remaining: len(sess.queue)}
}

// alreadyAnswered reports whether the question was already answered yes/no in
// this session, making a repeated answer a no-op.
func (sess *session) alreadyAnswered(questionID string) bool {
	sess.mu.Lock()
	defer sess.mu.Unlock()
	return sess.answered[questionID]
}

// dropQuestion returns queue without the given question, preserving order.
func dropQuestion(queue []Question, questionID string) []Question {
	for i, q := range queue {
		if q.ID == questionID {
			return append(queue[:i:i], queue[i+1:]...)
		}
	}
	return queue
}
