package review

// Queue building: run the existing candidate searches, keep only the
// uncertainty band, order by informativeness and interleave the two kinds.

import (
	"context"
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
// (non-positive limit means the configured default). The queue is rebuilt at
// most once per CacheTTL per user; between rebuilds batches are served from the
// cache, so answering stays fast. An empty batch carries a Reason the UI can
// show. The error is non-nil only when the underlying searches fail outright.
func (s *Service) Queue(ctx context.Context, userUID string, limit int) (QueueResult, error) {
	if limit <= 0 {
		limit = s.queueSize
	}
	limit = min(limit, maxBatch)
	sess := s.session(userUID)
	sess.mu.Lock()
	defer sess.mu.Unlock()
	if !sess.hasQueue || s.now().Sub(sess.builtAt) > s.cacheTTL {
		if err := s.rebuild(ctx, sess); err != nil {
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

// rebuild recomputes the session's queue from the current library state. The
// caller holds sess.mu, so concurrent batch fetches for one user never run the
// expensive searches twice. The result is deterministic for a fixed library
// state: both searches' outputs are re-sorted here, so goroutine completion
// order cannot leak into the queue.
func (s *Service) rebuild(ctx context.Context, sess *session) error {
	faceQs, subjectsTotal, err := s.faceQuestions(ctx)
	if err != nil {
		return err
	}
	labelQs, labelsTotal, err := s.labelQuestions(ctx)
	if err != nil {
		return err
	}
	faceQs = excludeSeen(faceQs, sess)
	labelQs = excludeSeen(labelQs, sess)
	s.orderQuestions(faceQs)
	s.orderQuestions(labelQs)
	sess.queue = interleave(faceQs, labelQs)
	sess.hasQueue = true
	sess.builtAt = s.now()
	sess.reason = ReasonNoCandidates
	if subjectsTotal == 0 && labelsTotal == 0 {
		sess.reason = ReasonNoSources
	}
	return nil
}

// faceQuestions sweeps the per-subject candidate search across all named
// subjects and keeps the candidates inside the uncertainty band. It also
// returns how many named subjects the sweep covered, for the empty-library
// reason. The sweep bounds its own concurrency and already excludes assigned
// faces, persisted rejections, negative exemplars and sub-reviewable faces.
func (s *Service) faceQuestions(ctx context.Context) ([]Question, int, error) {
	var questions []Question
	var subjectsTotal int
	params := sweep.Params{Threshold: 1 - s.bandMin}
	err := s.sweeper.Sweep(ctx, params, func(ev sweep.Event) error {
		switch ev.Type {
		case sweep.EventPerson:
			questions = append(questions, s.personQuestions(ev.Person)...)
		case sweep.EventSummary:
			if ev.Summary != nil {
				subjectsTotal = ev.Summary.SubjectsTotal
			}
		case sweep.EventProgress:
			// Progress is UI chrome for the streaming endpoint; irrelevant here.
		}
		return nil
	})
	if err != nil {
		return nil, 0, fmt.Errorf("review: sweeping face candidates: %w", err)
	}
	return questions, subjectsTotal, nil
}

// personQuestions converts one subject's sweep candidates into face questions,
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

// labelQuestions runs the label-similarity search for every label that has
// photos (capped at MaxLabels, concurrency bounded by LabelConcurrency) and
// keeps the candidates inside the uncertainty band. It also returns how many
// labels had photos, for the empty-library reason. A single label's search
// failing is logged and skipped, like the sweep's per-subject policy.
func (s *Service) labelQuestions(ctx context.Context) ([]Question, int, error) {
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
	results := make([]expand.Result, len(labels))
	grp, gctx := errgroup.WithContext(ctx)
	grp.SetLimit(s.labelConcurrency)
	for i, label := range labels {
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
		return nil, 0, fmt.Errorf("review: scanning labels: %w", err)
	}
	var questions []Question
	for i, res := range results {
		questions = append(questions, s.labelResultQuestions(labels[i].Label, res)...)
	}
	return questions, total, nil
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

// inBand reports whether a confidence sits inside [BandMin, BandMax).
func (s *Service) inBand(confidence float64) bool {
	return confidence >= s.bandMin && confidence < s.bandMax
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
