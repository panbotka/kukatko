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

// The project scope of the face opinions.
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

// faceTargetInProjectSQL reports whether the photo ($1) and the subject ($2) of a
// face opinion both belong to the project ($3).
const faceTargetInProjectSQL = `
SELECT EXISTS (SELECT 1 FROM photos WHERE uid = $1 AND project_id = $3)
   AND EXISTS (SELECT 1 FROM subjects WHERE uid = $2 AND project_id = $3)`

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

// inProjectAuditedTx is inAuditedTx for a face opinion: it proves the face's
// photo and the subject inside projectUID on the transaction first, and defaults
// the entry's ProjectUID to the project.
func (s *Store) inProjectAuditedTx(
	ctx context.Context, projectUID, photoUID, subjectUID string, entry audit.Entry, mutate func(tx pgx.Tx) error,
) error {
	if projectUID == "" {
		return ErrNoProject
	}
	if entry.ProjectUID == "" {
		entry.ProjectUID = projectUID
	}
	return s.inAuditedTx(ctx, entry, func(tx pgx.Tx) error {
		if err := requireFaceTargetInProject(ctx, tx, projectUID, photoUID, subjectUID); err != nil {
			return err
		}
		return mutate(tx)
	})
}
