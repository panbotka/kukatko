package review

// Queue building: run the existing candidate searches over a bounded window of
// the library, split what they return into the two confidence tiers (see
// tiers.go), order each tier by its own rule, blend them in the configured
// ratio, spread the questions across the entities they are about (see
// variety.go) and interleave the two kinds.
//
// The bound is the point. The game shows one question at a time, so producing a
// library-wide work list to fill a single batch is the wrong trade: on a real
// library (105 named subjects, 113 628 faces) sweeping every subject cost four
// minutes inside one request and no browser waited that long. Each source is
// therefore scanned in a rotating window — a handful of subjects, a handful of
// labels — and stops as soon as the batch has enough candidates, with a deadline
// behind that as a backstop. The cursors advance on every rebuild, so successive
// rebuilds walk the whole library instead of re-reading its head.
//
// A rebuild that finds nothing rotates and tries again rather than reporting an
// empty queue: the game has to stay playable while any unconfirmed candidate
// exists anywhere, and "this window happened to be exhausted" is not "there is
// nothing to do". The rounds share one deadline, so degrading costs latency
// only up to BuildTimeout.

import (
	"context"
	"errors"
	"fmt"
	"math"
	"sort"

	"golang.org/x/sync/errgroup"

	"github.com/panbotka/kukatko/internal/candidates"
	"github.com/panbotka/kukatko/internal/expand"
	"github.com/panbotka/kukatko/internal/organize"
	"github.com/panbotka/kukatko/internal/sweep"
)

// Queue returns the next batch of questions for the user from src (empty or
// unknown means both sources), at most limit long (non-positive limit means the
// configured default). The queue is rebuilt when it is cold, when it has run
// dry, when the source changed, or once per CacheTTL; between rebuilds batches
// are served from the cache, so answering stays fast. An empty batch carries a
// Reason the UI can show. The error is non-nil only when the underlying searches
// fail outright.
func (s *Service) Queue(ctx context.Context, userUID string, src Source, limit int) (QueueResult, error) {
	src = src.orBoth()
	if limit <= 0 {
		limit = s.queueSize
	}
	limit = min(limit, maxBatch)
	sess := s.session(userUID)
	sess.mu.Lock()
	defer sess.mu.Unlock()
	if s.needsRebuild(sess, src) {
		if err := s.rebuild(ctx, sess, src, limit); err != nil {
			return QueueResult{}, err
		}
	}
	batch := make([]Question, min(limit, len(sess.queue)))
	copy(batch, sess.queue)
	res := QueueResult{
		Questions: batch, Source: src, Answered: sess.answeredCount, Remaining: len(sess.queue),
	}
	if len(sess.queue) == 0 {
		res.Reason = sess.reason
	}
	return res, nil
}

// needsRebuild reports whether the session's cached queue has to be rebuilt: it
// was never built, it has run dry, it is for a different source, or it has aged
// past CacheTTL. Rebuilding a dry queue is what makes the bounded scan cover the
// library — each rebuild scans the next window, so a player who works through a
// batch immediately gets the next slice of the library instead of an empty queue
// until the TTL expires. A rebuild is cheap now precisely because it is bounded.
//
// The source check is why the cache cannot hand back the previous selection's
// batch: switching to labels is a request to stop being asked about faces, and
// a warm cache serving them anyway would look exactly like a broken toggle.
func (s *Service) needsRebuild(sess *session, src Source) bool {
	return !sess.hasQueue || sess.source != src || len(sess.queue) == 0 ||
		s.now().Sub(sess.builtAt) > s.cacheTTL
}

// material is what one rebuild collected before ordering and interleaving.
type material struct {
	// faceQs and labelQs are the two guess-checking sources' candidates, each
	// still split by confidence tier because the tiers are ordered differently
	// and mixed by a ratio (see tiers.go).
	faceQs  tiered
	labelQs tiered
	// placeQs, dupQs and outlierQs are the three checks over work the machine
	// already did. They carry no tier: their confidences are not points on one
	// comparable scale, so each arrives already ordered by its own notion of
	// "most worth asking" (see extras.go).
	placeQs   []Question
	dupQs     []Question
	outlierQs []Question
	// subjectsTotal, labelsTotal, placesTotal, dupsTotal and outlierSubjects are
	// the library-wide source counts (not the windows'), so the empty-queue
	// reason stays exact. A source the selection excluded is never scanned, so
	// its count stays zero — reasonFor only reads the counts of the sources that
	// were actually asked for.
	subjectsTotal   int
	labelsTotal     int
	placesTotal     int
	dupsTotal       int
	outlierSubjects int
	// degraded reports that the rebuild deadline cut a scan short, so the totals
	// may undercount and "no sources" must not be concluded from them.
	degraded bool
}

// questions is how many questions the round collected across every source and
// both tiers.
func (m material) questions() int {
	return m.faceQs.len() + m.labelQs.len() + len(m.placeQs) + len(m.dupQs) + len(m.outlierQs)
}

// sources is how many things the round found worth scanning at all, library-wide
// — named subjects, labels the game may ask about, estimated locations and
// duplicate groups. Zero means another rotation would scan the same nothing, so
// it is the signal to stop rotating.
func (m material) sources() int {
	return m.subjectsTotal + m.labelsTotal + m.placesTotal + m.dupsTotal + m.outlierSubjects
}

// rebuild recomputes the session's queue from the current library state for the
// selected source, aiming for need questions per source. The caller holds
// sess.mu, so concurrent batch fetches for one user never run the searches
// twice. The result is deterministic for a fixed library state and a fixed pair
// of cursors: both searches' outputs are re-sorted here, so goroutine completion
// order cannot leak into the queue, and the tier blend and variety rules on top
// of that sort are pure functions of it.
//
// The whole rebuild shares one deadline. That is what lets it rotate on an empty
// round without turning a slow library into a request that never answers: the
// rounds spend one BuildTimeout between them, not one each.
func (s *Service) rebuild(ctx context.Context, sess *session, src Source, need int) error {
	buildCtx, cancel := context.WithTimeout(ctx, s.buildTimeout)
	defer cancel()

	mat, err := s.collectRotating(buildCtx, ctx, src, need)
	if err != nil {
		return err
	}
	// Each source is spread before the merge, not after. Interleaving only ever
	// inserts questions of another kind between two of the same kind, and two
	// questions of different kinds are never about the same entity, so any run in
	// the merged queue is a run inside one source: bounding the sources bounds
	// the queue.
	sess.queue = capQueue(interleaveKinds([][]Question{
		spread(s.compose(mat.faceQs, sess), maxSameEntityRun),
		spread(s.compose(mat.labelQs, sess), maxSameEntityRun),
		spread(s.composePlain(mat.placeQs, sess), maxSameEntityRun),
		spread(s.composePlain(mat.dupQs, sess), maxSameEntityRun),
		spread(s.composePlain(mat.outlierQs, sess), maxSameEntityRun),
	}))
	sess.hasQueue = true
	sess.source = src
	sess.builtAt = s.now()
	sess.reason = reasonFor(src, mat)
	sure, band := tierCounts(sess.queue)
	s.log.DebugContext(ctx, "review: queue rebuilt", "questions", len(sess.queue),
		"source", string(src), "entities", countEntities(sess.queue),
		"longest_run", longestEntityRun(sess.queue), "sure", sure, "band", band)
	return nil
}

// collectRotating runs collect until a round comes back with candidates,
// rotating the scan cursors on every empty one (each scan advances its cursor by
// the whole window it walked, so the next round looks somewhere else), and
// returns that round's material. It gives up after maxRebuildRounds, when the
// deadline fires, when a round was cut short (its totals cannot be trusted to
// say the library is empty) or when the library genuinely holds no source to ask
// about.
//
// This is what "infinite" means in practice. Every scan is a bounded window over
// a rotating cursor, so an empty result means "nothing left in *this* window",
// which is not a reason to tell the player there is nothing to do — the next
// window may be full. Only a library with no named people and no reviewable
// label is genuinely out of questions.
//
// A round whose candidates the session has all already answered or skipped is
// not retried here (excludeSeen runs later, in compose): that queue comes back
// empty, and needsRebuild then rebuilds on the very next request, which rotates
// just the same — one request later instead of one round.
func (s *Service) collectRotating(
	buildCtx, ctx context.Context, src Source, need int,
) (material, error) {
	var mat material
	for round := range maxRebuildRounds {
		var err error
		if mat, err = s.collect(buildCtx, ctx, src, need); err != nil {
			return material{}, err
		}
		if roundIsFinal(mat, buildCtx) {
			break
		}
		s.log.DebugContext(ctx, "review: empty rebuild round, rotating to the next window",
			"round", round+1, "source", string(src))
	}
	return mat, nil
}

// roundIsFinal reports whether one collect round is worth stopping on: it found
// questions, it was cut short (so its emptiness proves nothing and another
// window would only burn what is left of the deadline), the deadline has already
// fired, or the library holds no source to rotate through at all.
func roundIsFinal(mat material, buildCtx context.Context) bool {
	return mat.questions() > 0 || mat.degraded || mat.sources() == 0 || buildCtx.Err() != nil
}

// collect gathers up to need candidates from each selected source under the
// rebuild deadline carried by buildCtx; ctx is the caller's own context, used
// only to tell the deadline firing (tolerable) from the client going away (not).
// Whatever the library does, the request cannot stay open longer than
// BuildTimeout, and a scan cut short degrades to a shorter batch instead of a
// failed request.
//
// A source the player did not choose is not scanned at all. Skipping it is not
// an optimisation on the side: the scans are what a rebuild costs — a subject
// sweep hydrates a whole photo record per match — so filtering their output
// afterwards would spend the whole price of a batch on questions nobody asked
// for. The unscanned side's total therefore stays zero, which reasonFor knows
// not to read as an empty library.
func (s *Service) collect(buildCtx, ctx context.Context, src Source, need int) (material, error) {
	var mat material
	if src.wantsFaces() {
		faceQs, subjectsTotal, err := s.faceQuestions(buildCtx, need)
		if err != nil {
			if !s.tolerateDeadline(ctx, err) {
				return material{}, err
			}
			mat.degraded = true
		}
		mat.faceQs, mat.subjectsTotal = faceQs, subjectsTotal
	}
	if src.wantsLabels() {
		labelQs, labelsTotal, err := s.labelQuestions(buildCtx, need)
		if err != nil {
			if !s.tolerateDeadline(ctx, err) {
				return material{}, err
			}
			mat.degraded = true
		}
		mat.labelQs, mat.labelsTotal = labelQs, labelsTotal
	}
	if err := s.collectChecks(buildCtx, ctx, src, need, &mat); err != nil {
		return material{}, err
	}
	return mat, nil
}

// collectChecks gathers the three checks over already-applied machine work.
// Each is independent of the others and of the two guess searches, so one of
// them running out of time degrades that kind alone: the round is marked
// degraded (its totals can no longer prove the library is empty) and the batch
// is served from whatever the rest produced.
func (s *Service) collectChecks(
	buildCtx, ctx context.Context, src Source, need int, mat *material,
) error {
	if !src.wantsChecks() {
		return nil
	}
	checks := []struct {
		collect func(context.Context, int) ([]Question, int, error)
		into    *[]Question
		total   *int
	}{
		{s.placeQuestions, &mat.placeQs, &mat.placesTotal},
		{s.duplicateQuestions, &mat.dupQs, &mat.dupsTotal},
		{s.outlierQuestions, &mat.outlierQs, &mat.outlierSubjects},
	}
	for _, check := range checks {
		questions, total, err := check.collect(buildCtx, need)
		if err != nil {
			if !s.tolerateDeadline(ctx, err) {
				return err
			}
			mat.degraded = true
			continue
		}
		*check.into, *check.total = questions, total
	}
	return nil
}

// compose turns one source's collected material into the ordered, blended,
// share-capped sequence the variety rules then reorder: each tier is stripped of
// what the session has already answered or skipped and ordered by its own rule,
// the two are blended in the configured ratio, and the per-entity share is
// enforced across the result.
func (s *Service) compose(mat tiered, sess *session) []Question {
	sure := excludeSeen(mat.sure, sess)
	band := excludeSeen(mat.band, sess)
	s.orderQuestions(sure, tierSure)
	s.orderQuestions(band, tierBand)
	return capEntities(blend(sure, band, s.sureShare), s.maxPerEntity)
}

// composePlain is compose for a tier-less kind: the collector already ordered
// its questions by its own notion of "most worth asking", so all that is left is
// dropping what the session has settled and enforcing the per-entity share.
func (s *Service) composePlain(questions []Question, sess *session) []Question {
	return capEntities(excludeSeen(questions, sess), s.maxPerEntity)
}

// tolerateDeadline reports whether err is the rebuild's own deadline firing —
// the bound doing its job — rather than a real failure. The caller's own
// cancellation (the client went away) and every other error still propagate.
func (s *Service) tolerateDeadline(ctx context.Context, err error) bool {
	if ctx.Err() != nil || !errors.Is(err, context.DeadlineExceeded) {
		return false
	}
	s.log.WarnContext(ctx, "review: queue rebuild hit its deadline, serving a partial queue",
		"timeout", s.buildTimeout)
	return true
}

// faceQuestions scans a bounded, rotating window of the named subjects and keeps
// the candidates that fall in either tier, stopping as soon as need of them are
// in hand — where each subject contributes at most MaxPerEntity per tier, so
// "enough" can only be reached by scanning several people. It also returns how
// many named subjects the library holds — the full count, not the window's — for
// the empty-library reason. The scan bounds its own concurrency and already
// excludes assigned faces, persisted rejections, negative exemplars and
// sub-reviewable faces.
//
// The search window now runs from the confident tier down to BandMin (Threshold
// = 1 - BandMin, no MinDistance floor) because the game asks about both tiers.
// That is a deliberate reversal of d0d6518, which had pushed the band's far edge
// into the search as MinDistance after one rebuild reached 10.9 GB anon-rss and
// the host OOM killer took the whole box down. What actually bounds the memory
// is not that floor: it is candidates' MaxExemplars and MaxCandidates and the
// fact that the cut to MaxCandidates happens *before* hydration, so at most a
// fixed number of photo records — EXIF blobs included — are ever built,
// whatever the window admits. Those must keep doing the work;
// internal/candidates/memory_test.go measures exactly this window.
//
// One consequence is worth naming: MaxCandidates keeps the *nearest* survivors,
// so a subject with more than MaxCandidates confident matches contributes no
// band candidates at all. That takes a person who resembles hundreds of
// still-unnamed faces at over BandMax — the shape the catch-all-subject bug
// produced, not a healthy library — and the rotation moves past them either way.
func (s *Service) faceQuestions(ctx context.Context, need int) (tiered, int, error) {
	var questions tiered
	collected := 0
	params := sweep.Params{Threshold: 1 - s.bandMin}
	win := sweep.Window{Offset: s.faceOffset(), Budget: s.faceBudget}
	cov, err := s.sweeper.Scan(ctx, params, win, func(person *sweep.Person) (bool, error) {
		theirs := s.personQuestions(person)
		questions.merge(theirs)
		collected += s.batchShare(theirs)
		return collected >= need, nil
	})
	if err != nil {
		return tiered{}, 0, fmt.Errorf("review: scanning face candidates: %w", err)
	}
	s.advanceFaceOffset(cov.NextOffset)
	return questions, cov.SubjectsTotal, nil
}

// personQuestions converts one subject's scanned candidates into face questions,
// split by tier, dropping what falls in neither tier, stale already-done rows and
// photos hidden from the library, and keeping at most MaxPerEntity per tier.
//
// The hidden ones are dropped here rather than in the sweep because hiding is a
// statement about browsing, not about the data: the face is still detected, its
// vector still indexed, and a candidate search that skipped hidden photos
// entirely would quietly weaken every other feature built on it. The game is a
// browse, though — a scanned document is exactly what nobody wants to be asked
// twenty questions about — so the questions stop here.
//
// The cap is applied here rather than
// after the scan on purpose: the scan stops once it holds need questions, so
// capping first is what makes "enough" mean "enough from enough different
// people" instead of "enough from whoever happened to be scanned first". The
// batch-wide share across both tiers is enforced later, by capEntities.
func (s *Service) personQuestions(person *sweep.Person) tiered {
	if person == nil {
		return tiered{}
	}
	var questions tiered
	for _, cand := range person.Candidates {
		confidence := 1 - cand.Distance
		which, ok := s.tierOf(confidence)
		if !ok || cand.Action == candidates.ActionAlreadyDone || cand.Photo.HiddenFromLibrary {
			continue
		}
		subject := person.Subject
		faceIndex := cand.FaceIndex
		box := cand.BBox
		questions.add(which, Question{
			ID:         faceQuestionID(cand.Photo.UID, cand.FaceIndex, subject.UID),
			Kind:       KindFace,
			Tier:       string(which),
			Confidence: confidence,
			Photo:      cand.Photo,
			Subject:    &subject,
			FaceIndex:  &faceIndex,
			BBox:       &box,
			Action:     string(cand.Action),
			MarkerUID:  cand.MarkerUID,
		})
	}
	return s.keepBest(questions)
}

// keepBest orders one entity's questions within each tier and keeps at most
// MaxPerEntity of each, which is the share of a batch a single subject or label
// may claim there. It orders before it cuts, so what survives is the entity's
// best material per tier — its surest confident candidates and its most
// informative uncertain ones — not whatever the search happened to return first.
func (s *Service) keepBest(questions tiered) tiered {
	return tiered{
		sure: s.keepBestTier(questions.sure, tierSure),
		band: s.keepBestTier(questions.band, tierBand),
	}
}

// keepBestTier orders one entity's questions from a single tier and cuts them to
// the per-entity share.
func (s *Service) keepBestTier(questions []Question, which tier) []Question {
	s.orderQuestions(questions, which)
	if len(questions) > s.maxPerEntity {
		questions = questions[:s.maxPerEntity]
	}
	return questions
}

// batchShare is how many of one entity's questions can actually reach a batch:
// its material, capped at the per-entity share.
//
// It is what a scan counts toward "enough", and counting anything else breaks
// the batch. Each tier keeps up to MaxPerEntity of an entity's questions, so an
// entity that has material in both offers twice the share — and a scan that
// counted all of it would stop after half the people it needs, leaving
// capEntities to cut the batch back below the size that was asked for. Counting
// what survives the cut makes the two agree exactly: a scan that stops at need
// yields a batch of need.
func (s *Service) batchShare(questions tiered) int {
	if s.maxPerEntity <= 0 {
		return questions.len()
	}
	return min(questions.len(), s.maxPerEntity)
}

// labelQuestions runs the label-similarity search over a bounded, rotating
// window of the labels the game may ask about and keeps the candidates that fall
// in either tier, stopping as soon as need of them are in hand — again counting
// at most MaxPerEntity per label per tier, so one prolific label cannot end the
// scan on its own. It also returns how many such labels the library holds, for
// the empty-library reason. A single label's search failing is logged and
// skipped, like the per-subject scan's policy.
func (s *Service) labelQuestions(ctx context.Context, need int) (tiered, int, error) {
	labels, total, err := s.labelPlan(ctx)
	if err != nil {
		return tiered{}, 0, err
	}
	if len(labels) == 0 {
		return tiered{}, total, nil
	}
	offset := wrapOffset(s.labelOffset(), len(labels))
	window := rotateLabels(labels, offset, s.labelBudget)

	var questions tiered
	scanned, collected := 0, 0
	for start := 0; start < len(window) && collected < need; start += s.labelConcurrency {
		chunk := window[start:min(start+s.labelConcurrency, len(window))]
		results, chunkErr := s.scanLabels(ctx, chunk)
		if chunkErr != nil {
			return tiered{}, 0, chunkErr
		}
		scanned += len(chunk)
		for i := range chunk {
			label := s.labelResultQuestions(chunk[i].Label, results[i])
			questions.merge(label)
			collected += s.batchShare(label)
		}
	}
	s.advanceLabelOffset(wrapOffset(offset+scanned, len(labels)))
	return questions, total, nil
}

// labelPlan lists the labels the review game may ask about — those that have
// photos and have not been switched off on the labels page — capped at
// MaxLabels, and returns them with the uncapped total the empty-library reason
// is derived from.
//
// A switched-off label is dropped here, before the plan, rather than filtered
// out of the questions afterwards. That is the point of the switch: one label's
// similarity search is a per-member kNN fan-out, so a label nobody wants to be
// asked about must not spend a rebuild's budget either. Dropping it from the
// total as well is deliberate too — a library whose every label is switched off
// is, as far as the game is concerned, a library with no labels, and that is
// what the empty-queue reason should say.
func (s *Service) labelPlan(ctx context.Context) ([]organize.LabelCount, int, error) {
	all, err := s.organize.ListLabels(ctx)
	if err != nil {
		return nil, 0, fmt.Errorf("review: listing labels: %w", err)
	}
	labels := make([]organize.LabelCount, 0, len(all))
	for _, label := range all {
		if label.PhotoCount > 0 && label.ReviewEnabled {
			labels = append(labels, label)
		}
	}
	total := len(labels)
	if len(labels) > s.maxLabels {
		s.log.Warn("review: label scan capped", "total", total, "cap", s.maxLabels)
		labels = labels[:s.maxLabels]
	}
	return labels, total, nil
}

// scanLabels runs the similarity search for one chunk of labels concurrently and
// returns the results positionally. A single label's search failing is logged and
// left as a zero result rather than failing the rebuild; only the errgroup itself
// erroring is fatal.
func (s *Service) scanLabels(ctx context.Context, chunk []organize.LabelCount) ([]expand.Result, error) {
	results := make([]expand.Result, len(chunk))
	grp, gctx := errgroup.WithContext(ctx)
	grp.SetLimit(s.labelConcurrency)
	for i, label := range chunk {
		grp.Go(func() error {
			req := expand.Request{Threshold: 1 - s.bandMin, Limit: labelCandidateLimit}
			res, findErr := s.expander.Label(gctx, label.UID, req)
			if findErr != nil {
				s.log.WarnContext(gctx, "review: label similarity failed",
					"label_uid", label.UID, "error", findErr)
				return nil
			}
			results[i] = res
			return nil
		})
	}
	if err := grp.Wait(); err != nil {
		return nil, fmt.Errorf("review: scanning labels: %w", err)
	}
	return results, nil
}

// labelResultQuestions converts one label's similarity candidates into label
// questions split by tier, dropping what falls in neither, dropping photos
// hidden from the library (see personQuestions for why that cut is made here),
// and keeping at most MaxPerEntity of each tier — the same share rule the face
// side applies, and for the same reason: a label that matches half the library
// returns hundreds of candidates and would otherwise be the only thing the batch
// asks about.
//
// The label search costs the same for both tiers: it already ran at the band's
// threshold and returned the confident matches nearest-first, and the old code
// simply threw them away. Splitting them out is free.
func (s *Service) labelResultQuestions(label organize.Label, res expand.Result) tiered {
	var questions tiered
	for _, cand := range res.Candidates {
		which, ok := s.tierOf(cand.Similarity)
		if !ok || cand.Photo.HiddenFromLibrary {
			continue
		}
		labelCopy := label
		questions.add(which, Question{
			ID:         labelQuestionID(cand.Photo.UID, label.UID),
			Kind:       KindLabel,
			Tier:       string(which),
			Confidence: cand.Similarity,
			Photo:      cand.Photo,
			Label:      &labelCopy,
		})
	}
	return s.keepBest(questions)
}

// rotateLabels returns the budget-long window of labels starting at offset,
// wrapping past the end. A non-positive budget — or one at least as large as the
// list — returns every planned label, still starting at offset.
func rotateLabels(labels []organize.LabelCount, offset, budget int) []organize.LabelCount {
	total := len(labels)
	if budget <= 0 || budget > total {
		budget = total
	}
	out := make([]organize.LabelCount, 0, budget)
	for i := range budget {
		out = append(out, labels[(offset+i)%total])
	}
	return out
}

// wrapOffset folds an arbitrary offset into [0, total), so a cursor can keep
// growing without ever indexing out of range.
func wrapOffset(offset, total int) int {
	if total <= 0 {
		return 0
	}
	offset %= total
	if offset < 0 {
		offset += total
	}
	return offset
}

// inBand reports whether a confidence sits inside [BandMin, BandMax).
func (s *Service) inBand(confidence float64) bool {
	return confidence >= s.bandMin && confidence < s.bandMax
}

// capQueue cuts a freshly built queue down to maxQueued, keeping the head — the
// most informative questions, since the queue is already ordered. What is dropped
// is not lost: the queue rebuilds when it runs dry, and the rotation cursors have
// moved on, so the next rebuild covers new ground rather than re-serving this tail.
//
// The cap is a memory bound. A question carries the whole photo record it is asked
// about, EXIF blob included, and a built queue is cached per user for CacheTTL and
// kept for up to sessionIdleTTL — so an uncapped queue would let a handful of
// players pin hundreds of megabytes of photo rows in the process for half a day.
func capQueue(questions []Question) []Question {
	if len(questions) <= maxQueued {
		return questions
	}
	return questions[:maxQueued]
}

// excludeSeen drops questions the session already answered or skipped.
func excludeSeen(questions []Question, sess *session) []Question {
	kept := questions[:0]
	for _, q := range questions {
		if !sess.seen(q.ID) {
			kept = append(kept, q)
		}
	}
	return kept
}

// orderQuestions sorts one tier's questions by that tier's own notion of "best
// first", with the stable question id as the deterministic tie-break:
//
//   - tierBand by informativeness — distance from the band midpoint ascending,
//     since the candidate closest to the decision boundary teaches the most;
//   - tierSure by confidence descending, since a tier whose purpose is the
//     one-click yes is best served surest-first.
//
// Ranking the confident tier by boundary distance instead would put its *least*
// certain members at the head, which is the opposite of what it is for.
func (s *Service) orderQuestions(questions []Question, which tier) {
	if which == tierSure {
		sort.Slice(questions, func(i, j int) bool {
			if questions[i].Confidence != questions[j].Confidence {
				return questions[i].Confidence > questions[j].Confidence
			}
			return questions[i].ID < questions[j].ID
		})
		return
	}
	mid := s.bandMid()
	sort.Slice(questions, func(i, j int) bool {
		di := math.Abs(questions[i].Confidence - mid)
		dj := math.Abs(questions[j].Confidence - mid)
		if di != dj {
			return di < dj
		}
		return questions[i].ID < questions[j].ID
	})
}

// interleaveKinds merges the ordered per-kind lists into one sequence, spreading
// the sparser kinds evenly through the denser ones — roughly round-robin when
// the counts match, skewed toward whichever kind has more candidates otherwise.
//
// This is what stops any one kind dominating a session, and it does so
// positionally rather than in aggregate. A batch is a *prefix* of the built
// queue, so a queue that merely ends up evenly mixed overall could still open
// with twenty duplicate pairs in a row. Every list contributes at most `need`
// candidates (each collector stops there), so with k kinds supplying material a
// batch of n holds about n/k of each — and when only one kind has anything left,
// it fills the batch alone, which is the right degradation: an exhausted library
// should not withhold the work it still has.
//
// A list's i-th element is placed at the exact rational (2i+1)/(2·len), so the
// merge is deterministic with no floating point and no randomness: positions are
// compared by integer cross-multiplication and ties go to the earlier kind in
// Kinds. It takes from each list in order and never reorders within one, so a
// run of one entity in the merged sequence is a run inside one of the inputs —
// whatever bound spread put on the sources survives the merge, and the later
// truncation to a batch only ever keeps a prefix.
func interleaveKinds(lists [][]Question) []Question {
	total, cursors := 0, make([]int, len(lists))
	for _, list := range lists {
		total += len(list)
	}
	merged := make([]Question, 0, total)
	for range total {
		pick := nextKind(lists, cursors)
		merged = append(merged, lists[pick][cursors[pick]])
		cursors[pick]++
	}
	return merged
}

// nextKind returns the index of the list whose next unconsumed question sits
// earliest in the merged order. It compares (2i+1)/(2·len) fractions by integer
// cross-multiplication — a·d < c·b for a/b < c/d with positive denominators —
// so the comparison is exact and the result reproducible. At most one question
// per list is inspected per step, and there are five lists, so the linear scan
// is free.
func nextKind(lists [][]Question, cursors []int) int {
	best, bestNum, bestDen := -1, 0, 0
	for k, list := range lists {
		if cursors[k] >= len(list) {
			continue
		}
		num, den := 2*cursors[k]+1, 2*len(list)
		if best < 0 || num*bestDen < bestNum*den {
			best, bestNum, bestDen = k, num, den
		}
	}
	return best
}

// sortByConfidence orders a tier-less kind's questions most-confident first,
// with the stable question id as the deterministic tie-break. It is what the
// duplicate check wants: the pair the detector is surest about is the one a
// player can settle in a glance.
func sortByConfidence(questions []Question) {
	sort.SliceStable(questions, func(i, j int) bool {
		if questions[i].Confidence != questions[j].Confidence {
			return questions[i].Confidence > questions[j].Confidence
		}
		return questions[i].ID < questions[j].ID
	})
}

// sortBySuspicion orders outlier questions most-suspicious first — furthest from
// the person's centroid — with the stable question id as the tie-break. It is
// deliberately the opposite of sortByConfidence: the confident tier's logic does
// not apply to a check over an assignment somebody already made, where the whole
// value is in the faces that look wrong. It matches the /outliers page's own
// ranking, so the same face is the first question in either place.
func sortBySuspicion(questions []Question) {
	sort.SliceStable(questions, func(i, j int) bool {
		if questions[i].Distance != questions[j].Distance {
			return questions[i].Distance > questions[j].Distance
		}
		return questions[i].ID < questions[j].ID
	})
}
