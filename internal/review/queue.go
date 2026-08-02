package review

// Queue building: run the existing candidate searches over a bounded window of
// the library, keep only the uncertainty band, order by informativeness and
// interleave the two kinds.
//
// The bound is the point. The game shows one question at a time, so producing a
// library-wide work list to fill a single batch is the wrong trade: on a real
// library (105 named subjects, 113 628 faces) sweeping every subject cost four
// minutes inside one request and no browser waited that long. Each source is
// therefore scanned in a rotating window — a handful of subjects, a handful of
// labels — and stops as soon as the batch has enough band candidates, with a
// deadline behind that as a backstop. The cursors advance on every rebuild, so
// successive rebuilds walk the whole library instead of re-reading its head.

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

// Queue returns the next batch of questions for the user, at most limit long
// (non-positive limit means the configured default). The queue is rebuilt when
// it is cold, when it has run dry, or once per CacheTTL; between rebuilds
// batches are served from the cache, so answering stays fast. An empty batch
// carries a Reason the UI can show. The error is non-nil only when the
// underlying searches fail outright.
func (s *Service) Queue(ctx context.Context, userUID string, limit int) (QueueResult, error) {
	if limit <= 0 {
		limit = s.queueSize
	}
	limit = min(limit, maxBatch)
	sess := s.session(userUID)
	sess.mu.Lock()
	defer sess.mu.Unlock()
	if s.needsRebuild(sess) {
		if err := s.rebuild(ctx, sess, limit); err != nil {
			return QueueResult{}, err
		}
	}
	batch := make([]Question, min(limit, len(sess.queue)))
	copy(batch, sess.queue)
	res := QueueResult{Questions: batch, Answered: sess.answeredCount, Remaining: len(sess.queue)}
	if len(sess.queue) == 0 {
		res.Reason = sess.reason
	}
	return res, nil
}

// needsRebuild reports whether the session's cached queue has to be rebuilt: it
// was never built, it has run dry, or it has aged past CacheTTL. Rebuilding a dry
// queue is what makes the bounded scan cover the library — each rebuild scans the
// next window, so a player who works through a batch immediately gets the next
// slice of the library instead of an empty queue until the TTL expires. A rebuild
// is cheap now precisely because it is bounded.
func (s *Service) needsRebuild(sess *session) bool {
	return !sess.hasQueue || len(sess.queue) == 0 || s.now().Sub(sess.builtAt) > s.cacheTTL
}

// material is what one rebuild collected before ordering and interleaving.
type material struct {
	// faceQs and labelQs are the band candidates from the two sources.
	faceQs  []Question
	labelQs []Question
	// subjectsTotal and labelsTotal are the library-wide source counts (not the
	// window's), so the empty-queue reason stays exact.
	subjectsTotal int
	labelsTotal   int
	// degraded reports that the rebuild deadline cut a scan short, so the totals
	// may undercount and "no sources" must not be concluded from them.
	degraded bool
}

// rebuild recomputes the session's queue from the current library state, aiming
// for need questions per source. The caller holds sess.mu, so concurrent batch
// fetches for one user never run the searches twice. The result is deterministic
// for a fixed library state and a fixed pair of cursors: both searches' outputs
// are re-sorted here, so goroutine completion order cannot leak into the queue.
func (s *Service) rebuild(ctx context.Context, sess *session, need int) error {
	mat, err := s.collect(ctx, need)
	if err != nil {
		return err
	}
	faceQs := excludeSeen(mat.faceQs, sess)
	labelQs := excludeSeen(mat.labelQs, sess)
	s.orderQuestions(faceQs)
	s.orderQuestions(labelQs)
	sess.queue = capQueue(interleave(faceQs, labelQs))
	sess.hasQueue = true
	sess.builtAt = s.now()
	sess.reason = ReasonNoCandidates
	if !mat.degraded && mat.subjectsTotal == 0 && mat.labelsTotal == 0 {
		sess.reason = ReasonNoSources
	}
	return nil
}

// collect gathers up to need band candidates from each source under the rebuild
// deadline. The deadline is the backstop behind the per-source budgets: whatever
// the library does, the request cannot stay open longer than BuildTimeout, and a
// scan cut short degrades to a shorter batch instead of a failed request.
func (s *Service) collect(ctx context.Context, need int) (material, error) {
	buildCtx, cancel := context.WithTimeout(ctx, s.buildTimeout)
	defer cancel()

	var mat material
	faceQs, subjectsTotal, err := s.faceQuestions(buildCtx, need)
	if err != nil {
		if !s.tolerateDeadline(ctx, err) {
			return material{}, err
		}
		mat.degraded = true
	}
	labelQs, labelsTotal, err := s.labelQuestions(buildCtx, need)
	if err != nil {
		if !s.tolerateDeadline(ctx, err) {
			return material{}, err
		}
		mat.degraded = true
	}
	mat.faceQs, mat.subjectsTotal = faceQs, subjectsTotal
	mat.labelQs, mat.labelsTotal = labelQs, labelsTotal
	return mat, nil
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
// the candidates inside the uncertainty band, stopping as soon as need of them
// are in hand. It also returns how many named subjects the library holds — the
// full count, not the window's — for the empty-library reason. The scan bounds
// its own concurrency and already excludes assigned faces, persisted rejections,
// negative exemplars and sub-reviewable faces.
//
// The band is pushed all the way down into the search as a distance window rather
// than filtered out here: confidence >= BandMin is the scan's Threshold, and
// confidence < BandMax is its MinDistance. Asking for the whole threshold and
// discarding the confident matches afterwards made the scan hydrate a full photo
// record — EXIF blob included — for every match it was about to throw away, which
// on a subject that matches half the library is the difference between megabytes
// and gigabytes. inBand still runs below; this only stops the waste upstream.
func (s *Service) faceQuestions(ctx context.Context, need int) ([]Question, int, error) {
	var questions []Question
	params := sweep.Params{Threshold: 1 - s.bandMin, MinDistance: 1 - s.bandMax}
	win := sweep.Window{Offset: s.faceOffset(), Budget: s.faceBudget}
	cov, err := s.sweeper.Scan(ctx, params, win, func(person *sweep.Person) (bool, error) {
		questions = append(questions, s.personQuestions(person)...)
		return len(questions) >= need, nil
	})
	if err != nil {
		return nil, 0, fmt.Errorf("review: scanning face candidates: %w", err)
	}
	s.advanceFaceOffset(cov.NextOffset)
	return questions, cov.SubjectsTotal, nil
}

// personQuestions converts one subject's scanned candidates into face questions,
// keeping only the uncertainty band and dropping stale already-done rows.
func (s *Service) personQuestions(person *sweep.Person) []Question {
	if person == nil {
		return nil
	}
	var questions []Question
	for _, cand := range person.Candidates {
		confidence := 1 - cand.Distance
		if !s.inBand(confidence) || cand.Action == candidates.ActionAlreadyDone {
			continue
		}
		subject := person.Subject
		faceIndex := cand.FaceIndex
		box := cand.BBox
		questions = append(questions, Question{
			ID:         faceQuestionID(cand.Photo.UID, cand.FaceIndex, subject.UID),
			Kind:       KindFace,
			Confidence: confidence,
			Photo:      cand.Photo,
			Subject:    &subject,
			FaceIndex:  &faceIndex,
			BBox:       &box,
			Action:     string(cand.Action),
			MarkerUID:  cand.MarkerUID,
		})
	}
	return questions
}

// labelQuestions runs the label-similarity search over a bounded, rotating
// window of the labels that have photos and keeps the candidates inside the
// uncertainty band, stopping as soon as need of them are in hand. It also
// returns how many labels have photos library-wide, for the empty-library
// reason. A single label's search failing is logged and skipped, like the
// per-subject scan's policy.
func (s *Service) labelQuestions(ctx context.Context, need int) ([]Question, int, error) {
	labels, total, err := s.labelPlan(ctx)
	if err != nil {
		return nil, 0, err
	}
	if len(labels) == 0 {
		return nil, total, nil
	}
	offset := wrapOffset(s.labelOffset(), len(labels))
	window := rotateLabels(labels, offset, s.labelBudget)

	var questions []Question
	scanned := 0
	for start := 0; start < len(window) && len(questions) < need; start += s.labelConcurrency {
		chunk := window[start:min(start+s.labelConcurrency, len(window))]
		results, chunkErr := s.scanLabels(ctx, chunk)
		if chunkErr != nil {
			return nil, 0, chunkErr
		}
		scanned += len(chunk)
		for i := range chunk {
			questions = append(questions, s.labelResultQuestions(chunk[i].Label, results[i])...)
		}
	}
	s.advanceLabelOffset(wrapOffset(offset+scanned, len(labels)))
	return questions, total, nil
}

// labelPlan lists the labels that have photos, capped at MaxLabels, and returns
// them with the uncapped total the empty-library reason is derived from.
func (s *Service) labelPlan(ctx context.Context) ([]organize.LabelCount, int, error) {
	all, err := s.organize.ListLabels(ctx)
	if err != nil {
		return nil, 0, fmt.Errorf("review: listing labels: %w", err)
	}
	labels := make([]organize.LabelCount, 0, len(all))
	for _, label := range all {
		if label.PhotoCount > 0 {
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
// questions, keeping only the uncertainty band.
func (s *Service) labelResultQuestions(label organize.Label, res expand.Result) []Question {
	var questions []Question
	for _, cand := range res.Candidates {
		if !s.inBand(cand.Similarity) {
			continue
		}
		labelCopy := label
		questions = append(questions, Question{
			ID:         labelQuestionID(cand.Photo.UID, label.UID),
			Kind:       KindLabel,
			Confidence: cand.Similarity,
			Photo:      cand.Photo,
			Label:      &labelCopy,
		})
	}
	return questions
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

// orderQuestions sorts questions by informativeness: distance from the band
// midpoint ascending (the closest to the decision boundary teaches the most),
// with the stable question id as the deterministic tie-break.
func (s *Service) orderQuestions(questions []Question) {
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

// interleave merges the two ordered kinds into one sequence, spreading the
// sparser kind evenly through the denser one — roughly alternating when counts
// match, skewed toward the kind with more candidates otherwise. Positions are
// compared as exact integer rationals ((2i+1)/2·len) so the merge is
// deterministic with no floating-point or randomness involved.
func interleave(faceQs, labelQs []Question) []Question {
	merged := make([]Question, 0, len(faceQs)+len(labelQs))
	fi, li := 0, 0
	for fi < len(faceQs) && li < len(labelQs) {
		if (2*fi+1)*len(labelQs) <= (2*li+1)*len(faceQs) {
			merged = append(merged, faceQs[fi])
			fi++
		} else {
			merged = append(merged, labelQs[li])
			li++
		}
	}
	merged = append(merged, faceQs[fi:]...)
	merged = append(merged, labelQs[li:]...)
	return merged
}
