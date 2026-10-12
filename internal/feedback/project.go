package feedback

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/panbotka/kukatko/internal/audit"
)

// ErrNoProject is returned by a project-scoped write called without a project —
// a programming error, never "anywhere in the instance".
var ErrNoProject = errors.New("feedback: no project given")

// The project scope of the opinions.
//
// face_rejections and face_confirmations carry no project of their own: a face
// belongs to the project of its photo, and the subject it is (or is not) to the
// subject's. The four face writes (RejectFace, UnrejectFace, ConfirmFace,
// UnconfirmFace) take the project uid after the context and, on their own
// transaction, prove that the face's photo and the subject both belong to it
// before writing anything — so an opinion never joins two projects, and one aimed
// at another project's face or subject is ErrTargetNotFound (the HTTP layer's
// 404), with no row and no audit entry written. The reads keyed by a subject
// (FaceRejectionsForSubject, FaceConfirmationsForSubject) and the Is… probes stay
// keyed by uid: their callers resolve the subject inside the project first.
//
// The label and near-duplicate opinions follow the same rule. label_rejections
// is scoped through its photo AND its label: the label writes prove both in the
// project. duplicate_dismissals and duplicate_confirmations are scoped through
// their two photos: the pair writes prove both in the project. A target outside
// it is ErrTargetNotFound, with no row and no audit entry. The bulk reads
// (LabelRejectionsForLabel, DismissedDuplicatePairs, ConfirmedDuplicatePairs)
// and the Is… probes stay keyed by uid as above: a label is resolved inside the
// project by its caller first, and the pair lists are only ever intersected with
// a duplicate scan that is itself confined to one project's photos — since no
// write can join two projects, a pair never names a foreign photo next to an own.

// faceTargetInProjectSQL reports whether the photo ($1) and the subject ($2) of a
// face opinion both belong to the project ($3).
const faceTargetInProjectSQL = `
SELECT EXISTS (SELECT 1 FROM photos WHERE uid = $1 AND project_id = $3)
   AND EXISTS (SELECT 1 FROM subjects WHERE uid = $2 AND project_id = $3)`

// labelTargetInProjectSQL reports whether the photo ($1) and the label ($2) of a
// label rejection both belong to the project ($3).
const labelTargetInProjectSQL = `
SELECT EXISTS (SELECT 1 FROM photos WHERE uid = $1 AND project_id = $3)
   AND EXISTS (SELECT 1 FROM labels WHERE uid = $2 AND project_id = $3)`

// pairInProjectSQL reports whether both photos of a pair ($1, $2 — never equal,
// checkPairKey refuses that) belong to the project ($3).
const pairInProjectSQL = `
SELECT count(*) = 2 FROM photos WHERE uid IN ($1, $2) AND project_id = $3`

// proof checks, on the transaction of the write it guards, that the write's
// targets belong to the project; it returns ErrTargetNotFound when they do not.
type proof func(ctx context.Context, tx pgx.Tx, projectUID string) error

// requireFaceTargetInProject returns ErrNoProject without a project and
// ErrTargetNotFound unless photoUID and subjectUID both belong to projectUID, run
// on the transaction of the write it guards.
func requireFaceTargetInProject(ctx context.Context, tx pgx.Tx, projectUID, photoUID, subjectUID string) error {
	if projectUID == "" {
		return ErrNoProject
	}
	var ok bool
	if err := tx.QueryRow(ctx, faceTargetInProjectSQL, photoUID, subjectUID, projectUID).Scan(&ok); err != nil {
		return fmt.Errorf("feedback: checking the project of face %s and subject %s: %w", photoUID, subjectUID, err)
	}
	if !ok {
		return ErrTargetNotFound
	}
	return nil
}

// labelProof is the proof of a label rejection: the photo and the label of key
// both belong to the project.
func labelProof(key LabelRejectionKey) proof {
	return func(ctx context.Context, tx pgx.Tx, projectUID string) error {
		var ok bool
		if err := tx.QueryRow(ctx, labelTargetInProjectSQL, key.PhotoUID, key.LabelUID, projectUID).Scan(&ok); err != nil {
			return fmt.Errorf("feedback: checking the project of photo %s and label %s: %w",
				key.PhotoUID, key.LabelUID, err)
		}
		if !ok {
			return ErrTargetNotFound
		}
		return nil
	}
}

// pairProof is the proof of a near-duplicate opinion: both photos of the pair
// belong to the project.
func pairProof(pair photoPair) proof {
	return func(ctx context.Context, tx pgx.Tx, projectUID string) error {
		var ok bool
		if err := tx.QueryRow(ctx, pairInProjectSQL, pair.PhotoUID, pair.OtherUID, projectUID).Scan(&ok); err != nil {
			return fmt.Errorf("feedback: checking the project of pair %s/%s: %w", pair.PhotoUID, pair.OtherUID, err)
		}
		if !ok {
			return ErrTargetNotFound
		}
		return nil
	}
}

// inProjectAuditedTx is inAuditedTx for a face opinion: it proves the face's
// photo and the subject inside projectUID on the transaction first, and defaults
// the entry's ProjectUID to the project.
func (s *Store) inProjectAuditedTx(
	ctx context.Context, projectUID, photoUID, subjectUID string, entry audit.Entry, mutate func(tx pgx.Tx) error,
) error {
	return s.inScopedAuditedTx(ctx, projectUID, entry,
		func(ctx context.Context, tx pgx.Tx, projectUID string) error {
			return requireFaceTargetInProject(ctx, tx, projectUID, photoUID, subjectUID)
		}, mutate)
}

// inScopedAuditedTx is inAuditedTx for any project-scoped opinion: it returns
// ErrNoProject without a project, defaults the entry's ProjectUID to the project,
// and runs prove on the transaction before mutate — so a refused target writes
// neither the row nor the audit entry.
func (s *Store) inScopedAuditedTx(
	ctx context.Context, projectUID string, entry audit.Entry, prove proof, mutate func(tx pgx.Tx) error,
) error {
	if projectUID == "" {
		return ErrNoProject
	}
	if entry.ProjectUID == "" {
		entry.ProjectUID = projectUID
	}
	return s.inAuditedTx(ctx, entry, func(tx pgx.Tx) error {
		if err := prove(ctx, tx, projectUID); err != nil {
			return err
		}
		return mutate(tx)
	})
}
