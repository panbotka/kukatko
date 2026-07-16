package review

// Applying answers: yes routes through the existing write paths (the facematch
// assign state machine for faces, the organize attach path for labels), no
// records a persisted rejection in feedback, skip only touches session state.

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
func (s *Service) apply(ctx context.Context, ref questionRef, answer Answer, meta audit.Meta) (string, error) {
	switch {
	case answer == AnswerNo:
		return s.applyNo(ctx, ref, meta)
	case ref.Kind == KindFace:
		return s.applyFaceYes(ctx, ref, meta)
	default:
		return s.applyLabelYes(ctx, ref, meta)
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
		"photo_uid": ref.PhotoUID, "source": string(organize.SourceManual), "via": "review",
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

// applyNo records the rejection that makes the game converge: the pair never
// comes back and the negative-exemplar rule kills lookalike candidates. Both
// feedback paths are idempotent and audit inside the mutation's transaction.
func (s *Service) applyNo(ctx context.Context, ref questionRef, meta audit.Meta) (string, error) {
	var err error
	if ref.Kind == KindFace {
		key := feedback.FaceRejectionKey{
			PhotoUID: ref.PhotoUID, FaceIndex: ref.FaceIndex, SubjectUID: ref.SubjectUID,
		}
		entry := meta.Entry(audit.ActionFaceReject, "subjects", ref.SubjectUID, map[string]any{
			"photo_uid": ref.PhotoUID, "face_index": ref.FaceIndex, "via": "review",
		})
		err = s.feedback.RejectFace(ctx, key, entry)
	} else {
		key := feedback.LabelRejectionKey{PhotoUID: ref.PhotoUID, LabelUID: ref.LabelUID}
		entry := meta.Entry(audit.ActionLabelReject, "labels", ref.LabelUID, map[string]any{
			"photo_uid": ref.PhotoUID, "via": "review",
		})
		err = s.feedback.RejectLabel(ctx, key, entry)
	}
	if err != nil {
		if isGone(err) {
			return resultGone, nil
		}
		return "", fmt.Errorf("review: recording rejection: %w", err)
	}
	return resultRejected, nil
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
