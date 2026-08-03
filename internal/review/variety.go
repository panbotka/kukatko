package review

// Variety: keeping the game from turning into an interrogation about one person
// or one label.
//
// The measured symptom was twenty consecutive questions about the same label.
// Two independent things produced it.
//
// The first is how a rebuild filled its batch. Both scans stop as soon as they
// hold `need` band candidates, and they counted every candidate an entity
// offered — but one label that matches half the library offers hundreds of them
// in a single search. A single label (or subject) could therefore fill the whole
// batch and the scan stopped before it ever looked at a second one. maxPerEntity
// changes what counts: only an entity's few most informative questions enter the
// batch, so filling a batch of twenty means visiting at least
// twenty/maxPerEntity distinct entities. FaceBudget, LabelBudget and
// BuildTimeout still bound the cost of one rebuild; what changes is that a
// rebuild now spends its budget instead of stopping at the first prolific
// entity.
//
// The second is the ordering. Sorting purely by informativeness lets whichever
// entity has the most candidates sit everywhere in the sequence, so even a batch
// drawn from ten entities can open with ten questions about one of them. spread
// reorders each source: it still takes the most informative question available
// at every step — variety must not be bought with irrelevant questions — but it
// refuses to take one more from an entity that was just asked about
// maxSameEntityRun times in a row, as long as any other entity still has a
// question left.
//
// Both rules are pure functions of an already-deterministic order, so the queue
// stays reproducible for a fixed library state and a fixed pair of cursors.

// maxSameEntityRun is how many questions in a row may be about the same subject
// or the same label while another entity still has a question waiting.
//
// It is a constant rather than a config key because it is a property of the
// game, not an operational trade-off: unlike maxPerEntity it costs nothing to
// enforce, so there is no dial worth turning. Two, not one: keeping the same
// face on screen for a second question reuses the recognition the player has
// just done ("that is the same evening, so yes, that is Anna again"), which is
// the one repetition that helps rather than bores. The third in a row is where
// it starts to feel like the same question.
const maxSameEntityRun = 2

// questionEntity returns what a question is about: the subject for a face
// question, the label for a label question. It is the identity a player
// perceives as "this again" — the photo is different every time, the entity is
// what gets repetitive. The kind is part of the key so a subject and a label
// that happen to share a uid can never collide.
func questionEntity(q Question) string {
	switch {
	case q.Subject != nil:
		return string(KindFace) + ":" + q.Subject.UID
	case q.Label != nil:
		return string(KindLabel) + ":" + q.Label.UID
	default:
		return string(q.Kind)
	}
}

// longestEntityRun returns the longest run of consecutive questions about one
// entity. This is the number the monotony complaint was actually about, so it
// is what the tests assert on and what a rebuild logs — without it there is no
// way to tell whether an ordering change helped. An empty sequence scores zero.
func longestEntityRun(questions []Question) int {
	best, run, prev := 0, 0, ""
	for _, q := range questions {
		if entity := questionEntity(q); entity == prev {
			run++
		} else {
			prev, run = entity, 1
		}
		best = max(best, run)
	}
	return best
}

// countEntities returns how many distinct entities a sequence asks about, the
// companion measure to longestEntityRun: a queue can have short runs and still
// be a ping-pong between two entities.
func countEntities(questions []Question) int {
	seen := make(map[string]struct{}, len(questions))
	for _, q := range questions {
		seen[questionEntity(q)] = struct{}{}
	}
	return len(seen)
}

// spread reorders one source's questions so no entity monopolises the sequence.
// At every step it takes the most informative question left whose entity was not
// just asked about maxRun times in a row; only when every remaining question
// belongs to that entity does it take one anyway, because a library with a
// single named subject still has to be playable. questions must already be
// ordered by informativeness — "most informative left" is then simply "first
// left", which is what makes the result deterministic and the variation free of
// randomness.
//
// The scan is quadratic in the number of questions. That is deliberate and safe:
// the input is one rebuild's material, already capped at maxPerEntity questions
// per entity over a budget-bounded number of entities, so it is tens of items,
// not thousands.
func spread(questions []Question, maxRun int) []Question {
	if maxRun <= 0 || len(questions) <= maxRun {
		return questions
	}
	out := make([]Question, 0, len(questions))
	taken := make([]bool, len(questions))
	last, run := "", 0
	for range questions {
		blocked := ""
		if run >= maxRun {
			blocked = last
		}
		idx := nextQuestion(questions, taken, blocked)
		taken[idx] = true
		if entity := questionEntity(questions[idx]); entity == last {
			run++
		} else {
			last, run = entity, 1
		}
		out = append(out, questions[idx])
	}
	return out
}

// nextQuestion returns the index of the first question not yet taken whose
// entity is not blocked, falling back to the first one left when the blocked
// entity is all that remains. Since questions is ordered by informativeness,
// "first" means "most informative", so the run cap only ever costs the minimum
// amount of relevance needed to break a run.
func nextQuestion(questions []Question, taken []bool, blocked string) int {
	fallback := -1
	for i := range questions {
		if taken[i] {
			continue
		}
		if fallback < 0 {
			fallback = i
		}
		if questionEntity(questions[i]) != blocked {
			return i
		}
	}
	return fallback
}
