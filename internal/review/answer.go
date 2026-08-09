package review

// Applying answers. Every verdict routes through a write path that already
// exists somewhere else in the app, and the package opens none of its own:
//
//   - face yes → the facematch assign state machine; no → a face rejection;
//   - label yes → the organize attach path; no → a label rejection;
//   - place yes → the geo-estimate reviewer's accept (coordinates kept, marked
//     a decision); no → its reject (coordinates cleared, tombstone left);
//   - duplicate yes → a duplicate confirmation; no → a duplicate dismissal.
//     Neither merges anything — the game never destroys a photo;
//   - outlier yes → a face confirmation, which takes the face out of the
//     ranking for good; no → unassign_person through the same assign state
//     machine the /outliers page uses (the marker survives, only the person is
//     detached).
//
// Skip only touches session state. Every decisive answer carries `via: review`
// in its audit details, which is what makes it countable on the leaderboard and
// findable in the admin decision view.

import (
	"context"
	"errors"
	"fmt"

	"github.com/panbotka/kukatko/internal/audit"
	"github.com/panbotka/kukatko/internal/facematch"
	"github.com/panbotka/kukatko/internal/feedback"
	"github.com/panbotka/kukatko/internal/organize"
	"github.com/panbotka/kukatko/internal/people"
	"github.com/panbotka/kukatko/internal/photos"
	"github.com/panbotka/kukatko/internal/vectors"
)

// viaReview is the details.via marker stamped onto every decisive review answer's
// audit entry. It is what the leaderboard aggregation filters on to distinguish
// review-game decisions from ordinary recognition or curation actions, and it is
// the predicate of the partial index in migration 0037.
const viaReview = audit.ViaReview

// Answer applies the player's verdict on one question and returns the updated
// session counters. It is idempotent: repeating an answered question reports
// already_answered without a second write, and the underlying paths tolerate
// replays. A question whose photo/face/label vanished since the queue was
// built reports the "gone" result instead of failing, so the UI moves on.
// Returns ErrInvalidQuestion or ErrInvalidAnswer for malformed input.
func (s *Service) Answer(
	ctx context.Context, userUID, questionID string, answer Answer, meta audit.Meta,
) (AnswerResult, error) {
	ref, err := parseQuestionID(questionID)
	if err != nil {
		return AnswerResult{}, err
	}
	sess := s.session(userUID)
	switch answer {
	case AnswerSkip:
		return sess.consume(questionID, resultSkipped, false), nil
	case AnswerYes, AnswerNo:
	default:
		return AnswerResult{}, ErrInvalidAnswer
	}
	if sess.alreadyAnswered(questionID) {
		return sess.consume(questionID, resultAlreadyAnswered, false), nil
	}
	result, err := s.apply(ctx, ref, answer, meta)
	if err != nil {
		return AnswerResult{}, err
	}
	return sess.consume(questionID, result, result != resultGone), nil
}

// apply performs the durable write for a yes/no answer and names the outcome.
// A question of a kind whose backing service is not wired (the place check
// without a geo-estimate reviewer, say) reports "gone" rather than failing: the
// only way to hold such a question is to have been served it before the wiring
// changed, and that is exactly the mid-game race "gone" exists for.
func (s *Service) apply(ctx context.Context, ref questionRef, answer Answer, meta audit.Meta) (string, error) {
	yes := answer == AnswerYes
	switch ref.Kind {
	case KindFace:
		if yes {
			return s.applyFaceYes(ctx, ref, meta)
		}
		return s.applyFaceNo(ctx, ref, meta)
	case KindLabel:
		if yes {
			return s.applyLabelYes(ctx, ref, meta)
		}
		return s.applyLabelNo(ctx, ref, meta)
	case KindPlace:
		return s.applyPlace(ctx, ref, yes, meta)
	case KindDuplicate:
		return s.applyDuplicate(ctx, ref, yes, meta)
	case KindOutlier:
		return s.applyOutlier(ctx, ref, yes, meta)
	default:
		return "", ErrInvalidQuestion
	}
}

// applyFaceYes confirms a face question through the existing assign state
// machine. The current face row decides the action — assign_person when a
// marker already exists, create_marker (with the face's stored display-relative
// box) when not — and a face already carrying the subject short-circuits to
// success, which keeps a replayed yes from minting a duplicate marker.
func (s *Service) applyFaceYes(ctx context.Context, ref questionRef, meta audit.Meta) (string, error) {
	key := vectors.FaceKey{PhotoUID: ref.PhotoUID, FaceIndex: ref.FaceIndex}
	faces, err := s.faces.FacesByKeys(ctx, []vectors.FaceKey{key})
	if err != nil {
		return "", fmt.Errorf("review: loading face %s/%d: %w", ref.PhotoUID, ref.FaceIndex, err)
	}
	if len(faces) == 0 {
		return resultGone, nil
	}
	face := faces[0]
	if face.SubjectUID != nil && *face.SubjectUID == ref.SubjectUID {
		return resultAssigned, nil
	}
	req := facematch.AssignRequest{
		PhotoUID:   ref.PhotoUID,
		SubjectUID: ref.SubjectUID,
		FaceIndex:  &ref.FaceIndex,
		Via:        viaReview,
	}
	if face.MarkerUID != nil && *face.MarkerUID != "" {
		req.Action = facematch.ActionAssignPerson
		req.MarkerUID = *face.MarkerUID
	} else {
		req.Action = facematch.ActionCreateMarker
		box := face.BBox
		req.BBox = &box
	}
	if _, err := s.assigner.Apply(ctx, req, meta); err != nil {
		if isGone(err) {
			return resultGone, nil
		}
		return "", fmt.Errorf("review: assigning face %s/%d: %w", ref.PhotoUID, ref.FaceIndex, err)
	}
	return resultAssigned, nil
}

// applyLabelYes confirms a label question through the existing organize attach
// path (idempotent upsert), audited in the same transaction.
func (s *Service) applyLabelYes(ctx context.Context, ref questionRef, meta audit.Meta) (string, error) {
	entry := meta.Entry(audit.ActionLabelAttach, "labels", ref.LabelUID, map[string]any{
		"photo_uid": ref.PhotoUID, "source": string(organize.SourceManual), "via": viaReview,
	})
	err := s.organize.AttachLabelAudited(ctx, ref.PhotoUID, ref.LabelUID, organize.SourceManual, 0, entry)
	if err != nil {
		if isGone(err) {
			return resultGone, nil
		}
		return "", fmt.Errorf("review: attaching label %s to %s: %w", ref.LabelUID, ref.PhotoUID, err)
	}
	return resultLabeled, nil
}

// applyFaceNo records the face rejection that makes the game converge: the pair
// never comes back and the negative-exemplar rule kills lookalike candidates.
// The write is idempotent and audits inside the mutation's transaction.
func (s *Service) applyFaceNo(ctx context.Context, ref questionRef, meta audit.Meta) (string, error) {
	key := feedback.FaceRejectionKey{
		PhotoUID: ref.PhotoUID, FaceIndex: ref.FaceIndex, SubjectUID: ref.SubjectUID,
	}
	entry := meta.Entry(audit.ActionFaceReject, "subjects", ref.SubjectUID, map[string]any{
		"photo_uid": ref.PhotoUID, "face_index": ref.FaceIndex, "via": viaReview,
	})
	return decided(resultRejected, s.feedback.RejectFace(ctx, key, entry), "recording a face rejection")
}

// applyLabelNo records the persisted "this photo should not carry this label",
// which the label-similarity search then excludes for good.
func (s *Service) applyLabelNo(ctx context.Context, ref questionRef, meta audit.Meta) (string, error) {
	key := feedback.LabelRejectionKey{PhotoUID: ref.PhotoUID, LabelUID: ref.LabelUID}
	entry := meta.Entry(audit.ActionLabelReject, "labels", ref.LabelUID, map[string]any{
		"photo_uid": ref.PhotoUID, "via": viaReview,
	})
	return decided(resultRejected, s.feedback.RejectLabel(ctx, key, entry), "recording a label rejection")
}

// applyPlace settles an estimated location: yes keeps the coordinates and
// promotes them to a decision, no throws them away and leaves the tombstone that
// stops the nightly backfill handing the same guess straight back. Both go
// through the geo-estimate reviewer, which does the read-modify-write against
// the live row — so a photo somebody edited meanwhile is not clobbered, and a
// photo whose location is no longer an estimate is left exactly as it is.
func (s *Service) applyPlace(
	ctx context.Context, ref questionRef, yes bool, meta audit.Meta,
) (string, error) {
	if s.places == nil {
		return resultGone, nil
	}
	if yes {
		return decided(resultConfirmed, s.places.Accept(ctx, ref.PhotoUID, meta),
			"accepting an estimated location")
	}
	return decided(resultCleared, s.places.Reject(ctx, ref.PhotoUID, meta),
		"clearing an estimated location")
}

// applyDuplicate settles a near-duplicate pair. Yes records the confirmation the
// duplicates page ranks on; no records the dismissal that drops the edge from
// every later scan, so the pair stops being offered.
//
// Neither merges. That is not an omission: merging archives copies, and a game
// played at one keypress per second is the last place a photo should be able to
// disappear from. The confirmation is what turns the game's work into progress —
// it puts the group at the top of the duplicates page, where merging is an
// explicit act with a preview in front of it.
func (s *Service) applyDuplicate(
	ctx context.Context, ref questionRef, yes bool, meta audit.Meta,
) (string, error) {
	details := map[string]any{"other_uid": ref.OtherUID, "via": viaReview}
	if yes {
		key := feedback.DuplicateConfirmationKey{PhotoUID: ref.PhotoUID, OtherUID: ref.OtherUID}
		entry := meta.Entry(audit.ActionDuplicateConfirm, "photos", ref.PhotoUID, details)
		return decided(resultConfirmed, s.feedback.ConfirmDuplicate(ctx, key, entry),
			"confirming a duplicate pair")
	}
	key := feedback.DuplicateDismissalKey{PhotoUID: ref.PhotoUID, OtherUID: ref.OtherUID}
	entry := meta.Entry(audit.ActionDuplicateDismiss, "photos", ref.PhotoUID, details)
	return decided(resultRejected, s.feedback.DismissDuplicate(ctx, key, entry),
		"dismissing a duplicate pair")
}

// applyOutlier settles a face that sits far from the person it is assigned to.
// Yes records a face confirmation, which takes the face out of the outlier
// ranking for good — the /outliers page already reads that set, so the two views
// agree without either knowing about the other. No detaches the person through
// the ordinary assign state machine: the marker and the face survive, only the
// assignment goes, exactly as the ✓ on the /outliers page does it.
//
// A yes on a face whose marker has gone still records the opinion: the
// confirmation is about the face, and the face is still there.
func (s *Service) applyOutlier(
	ctx context.Context, ref questionRef, yes bool, meta audit.Meta,
) (string, error) {
	if yes {
		key := feedback.FaceConfirmationKey{
			PhotoUID: ref.PhotoUID, FaceIndex: ref.FaceIndex, SubjectUID: ref.SubjectUID,
		}
		entry := meta.Entry(audit.ActionFaceConfirm, "subjects", ref.SubjectUID, map[string]any{
			"photo_uid": ref.PhotoUID, "face_index": ref.FaceIndex, "via": viaReview,
		})
		return decided(resultConfirmed, s.feedback.ConfirmFace(ctx, key, entry),
			"confirming an assigned face")
	}
	return s.detachOutlier(ctx, ref, meta)
}

// detachOutlier clears the subject from the marker the question's face is tied
// to. The marker is re-read rather than taken from the queue, so a face detached
// or re-detected since the batch was built resolves to "gone" instead of
// unassigning whatever marker used to be there.
func (s *Service) detachOutlier(ctx context.Context, ref questionRef, meta audit.Meta) (string, error) {
	key := vectors.FaceKey{PhotoUID: ref.PhotoUID, FaceIndex: ref.FaceIndex}
	faces, err := s.faces.FacesByKeys(ctx, []vectors.FaceKey{key})
	if err != nil {
		return "", fmt.Errorf("review: loading face %s/%d: %w", ref.PhotoUID, ref.FaceIndex, err)
	}
	if len(faces) == 0 {
		return resultGone, nil
	}
	face := faces[0]
	if face.MarkerUID == nil || *face.MarkerUID == "" {
		// Nothing ties the face to the person any more; somebody got there first.
		return resultGone, nil
	}
	req := facematch.AssignRequest{
		Action:    facematch.ActionUnassignPerson,
		PhotoUID:  ref.PhotoUID,
		MarkerUID: *face.MarkerUID,
		FaceIndex: &ref.FaceIndex,
		Via:       viaReview,
	}
	if _, err := s.assigner.Apply(ctx, req, meta); err != nil {
		if isGone(err) {
			return resultGone, nil
		}
		return "", fmt.Errorf("review: detaching face %s/%d: %w", ref.PhotoUID, ref.FaceIndex, err)
	}
	return resultDetached, nil
}

// decided turns one write path's error into the answer's outcome: success names
// the result, a vanished target degrades to "gone" so the UI moves on, and
// anything else is wrapped with what was being attempted.
func decided(result string, err error, doing string) (string, error) {
	switch {
	case err == nil:
		return result, nil
	case isGone(err):
		return resultGone, nil
	default:
		return "", fmt.Errorf("review: %s: %w", doing, err)
	}
}

// isGone reports whether an error means the question's underlying photo, face,
// marker, subject or label no longer exists — an expected mid-game race that
// must fail the one answer gracefully rather than 500.
func isGone(err error) bool {
	return errors.Is(err, photos.ErrPhotoNotFound) ||
		errors.Is(err, people.ErrMarkerNotFound) ||
		errors.Is(err, people.ErrSubjectNotFound) ||
		errors.Is(err, organize.ErrPhotoNotFound) ||
		errors.Is(err, organize.ErrLabelNotFound) ||
		errors.Is(err, feedback.ErrTargetNotFound)
}
