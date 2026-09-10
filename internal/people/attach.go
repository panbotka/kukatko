package people

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/panbotka/kukatko/internal/audit"
)

// PhotoSubject is one person attached to a media item by hand: a marker of type
// MarkerPerson, resolved to the subject it names.
//
// It carries the subject's name, slug and type as well as its uid so a client
// renders the list of who is on a photo without a second round trip — the uid
// alone is the half of the link that means nothing to a reader.
type PhotoSubject struct {
	// SubjectUID, Slug, Name and Type identify the attached person.
	SubjectUID string      `json:"subject_uid"`
	Slug       string      `json:"slug"`
	Name       string      `json:"name"`
	Type       SubjectType `json:"type"`
	// MarkerUID is the row that records the link, so a caller can trace it back
	// to the marker table (and to the audit trail, which targets it).
	MarkerUID string `json:"marker_uid"`
	// AttachedAt is when the link was first made.
	AttachedAt time.Time `json:"attached_at"`
}

// listPhotoSubjectsSQL reads the people attached to a photo by hand, by name then
// uid so the list reads the same on every request.
//
// It matches type = 'person' only: a face marker is a region, and the roll-call
// of regions is what GET /photos/{uid}/faces answers. invalid = FALSE is carried
// for consistency with every other person read, even though nothing in the app
// flags a hand-attached link invalid — a link is either made or removed.
const listPhotoSubjectsSQL = `
SELECT s.uid, s.slug, s.name, s.type, m.uid, m.created_at
FROM markers m
JOIN subjects s ON s.uid = m.subject_uid
WHERE m.photo_uid = $1 AND m.type = 'person' AND m.invalid = FALSE
ORDER BY s.name, s.uid`

// ListPhotoSubjects returns the subjects attached to photoUID by hand, ordered by
// name. A photo with no hand-attached person — and a uid that names nothing —
// yields an empty, non-nil slice and a nil error, because "nobody is attached"
// and "no such photo" are the same answer to a reader and the caller that cares
// checks the photo itself.
func (s *Store) ListPhotoSubjects(ctx context.Context, photoUID string) ([]PhotoSubject, error) {
	return listPhotoSubjects(ctx, s.pool, photoUID)
}

// rowsQuerier is the one method the hand-attached listing needs. Both
// *pgxpool.Pool and pgx.Tx satisfy it, which is what lets the plain read and the
// read that closes a mutation's transaction run the very same statement.
type rowsQuerier interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
}

// listPhotoSubjects runs listPhotoSubjectsSQL over q and collects the rows.
func listPhotoSubjects(ctx context.Context, q rowsQuerier, photoUID string) ([]PhotoSubject, error) {
	rows, err := q.Query(ctx, listPhotoSubjectsSQL, photoUID)
	if err != nil {
		return nil, fmt.Errorf("people: listing attached subjects of %s: %w", photoUID, err)
	}
	defer rows.Close()

	out := make([]PhotoSubject, 0)
	for rows.Next() {
		var ps PhotoSubject
		if err := rows.Scan(&ps.SubjectUID, &ps.Slug, &ps.Name, &ps.Type,
			&ps.MarkerUID, &ps.AttachedAt); err != nil {
			return nil, fmt.Errorf("people: scanning attached subject of %s: %w", photoUID, err)
		}
		out = append(out, ps)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("people: iterating attached subjects of %s: %w", photoUID, err)
	}
	return out, nil
}

// attachSubjectSQL records the link, or leaves the existing one in place. The
// conflict target names the partial unique index of migration 0071, so attaching
// somebody who is already attached is idempotent instead of a second row — and
// the DO UPDATE (rather than DO NOTHING) is what makes the statement always
// return the row, so the caller need not branch on whether it was new.
const attachSubjectSQL = `
INSERT INTO markers (uid, photo_uid, subject_uid, type)
VALUES ($1, $2, $3, 'person')
ON CONFLICT (photo_uid, subject_uid) WHERE type = 'person'
DO UPDATE SET invalid = FALSE, updated_at = now()
RETURNING uid`

// AttachSubjectToPhoto links subjectUID to photoUID by hand — no bounding box, no
// detected face — and writes entry in the same transaction, so the link and the
// record of who made it commit atomically. It returns the photo's resulting
// hand-attached people, ordered by name.
//
// It is idempotent: attaching somebody already attached leaves exactly one link
// and answers the same body as the first attach. entry's TargetUID defaults to
// the marker recording the link. A missing photo returns ErrPhotoNotFound and a
// missing subject ErrSubjectNotFound; both roll back, writing no audit row.
func (s *Store) AttachSubjectToPhoto(
	ctx context.Context, photoUID, subjectUID string, entry audit.Entry,
) ([]PhotoSubject, error) {
	markerUID, err := newMarkerUID()
	if err != nil {
		return nil, err
	}
	return s.auditedAttachTx(ctx, photoUID, entry, func(tx pgx.Tx) (string, error) {
		if err := requireLinkable(ctx, tx, photoUID, subjectUID); err != nil {
			return "", err
		}
		var stored string
		if err := tx.QueryRow(ctx, attachSubjectSQL, markerUID, photoUID, subjectUID).
			Scan(&stored); err != nil {
			return "", fmt.Errorf("people: attaching %s to %s: %w", subjectUID, photoUID, err)
		}
		return stored, nil
	})
}

// detachSubjectSQL removes one hand-attached link. It never touches a face or
// label marker: detaching a person is not the same decision as deleting a region
// somebody drew, and the two must not be reachable through one route.
const detachSubjectSQL = `
DELETE FROM markers WHERE photo_uid = $1 AND subject_uid = $2 AND type = 'person'`

// DetachSubjectFromPhoto removes the hand-attached link between photoUID and
// subjectUID and writes entry in the same transaction. It returns the photo's
// remaining hand-attached people, ordered by name.
//
// It is idempotent in the same way the attach is: detaching somebody who is not
// attached is not an error, because the caller asked for a state ("this person is
// not on this photo") that already holds. entry's TargetUID defaults to the
// subject. A missing photo returns ErrPhotoNotFound and a missing subject
// ErrSubjectNotFound; both roll back, writing no audit row.
func (s *Store) DetachSubjectFromPhoto(
	ctx context.Context, photoUID, subjectUID string, entry audit.Entry,
) ([]PhotoSubject, error) {
	return s.auditedAttachTx(ctx, photoUID, entry, func(tx pgx.Tx) (string, error) {
		if err := requireLinkable(ctx, tx, photoUID, subjectUID); err != nil {
			return "", err
		}
		if _, err := tx.Exec(ctx, detachSubjectSQL, photoUID, subjectUID); err != nil {
			return "", fmt.Errorf("people: detaching %s from %s: %w", subjectUID, photoUID, err)
		}
		return subjectUID, nil
	})
}

// auditedAttachTx runs write, stamps entry with the target uid write reports (when
// the caller left it empty), records entry and reads the photo's resulting people
// — all in one transaction, so the link, its audit row and the body the caller is
// answered with are one consistent state.
//
// It exists instead of mutateAudited because the audit target is only known once
// the write has run: attaching somebody already attached keeps the existing
// marker, and that marker — not the one this call would have created — is what
// the trail must point at.
func (s *Store) auditedAttachTx(
	ctx context.Context, photoUID string, entry audit.Entry, write func(tx pgx.Tx) (string, error),
) ([]PhotoSubject, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("people: begin audited transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	target, err := write(tx)
	if err != nil {
		return nil, err
	}
	if entry.TargetUID == "" {
		entry.TargetUID = target
	}
	if err := audit.Write(ctx, tx, entry); err != nil {
		return nil, fmt.Errorf("people: writing audit entry: %w", err)
	}
	out, err := listPhotoSubjects(ctx, tx, photoUID)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("people: commit audited transaction: %w", err)
	}
	return out, nil
}

// requireLinkable returns ErrPhotoNotFound or ErrSubjectNotFound unless both ends
// of the link exist. The check runs inside the mutation's transaction so a
// missing end is answered as a 404 rather than surfacing later as a foreign-key
// violation — or, for a detach, as a silent no-op.
func requireLinkable(ctx context.Context, tx pgx.Tx, photoUID, subjectUID string) error {
	var exists bool
	err := tx.QueryRow(ctx, "SELECT EXISTS (SELECT 1 FROM photos WHERE uid = $1)", photoUID).Scan(&exists)
	if err != nil {
		return fmt.Errorf("people: looking up photo %s: %w", photoUID, err)
	}
	if !exists {
		return ErrPhotoNotFound
	}
	_, err = subjectName(ctx, tx, subjectUID)
	return err
}

// deletePersonMarkersTx removes every hand-attached link naming subjectUID inside
// tx. It is called wherever a subject is deleted, because the foreign key's ON
// DELETE SET NULL is the wrong answer for this kind of marker: a face marker is a
// fact about the picture and survives losing its name as an unassigned region,
// while a hand-attached link is nothing but the claim "this person is here" —
// with the person gone there is no claim left, only a row that would be counted
// forever as a nameless marker nobody can ever name.
func deletePersonMarkersTx(ctx context.Context, tx pgx.Tx, subjectUID string) error {
	if _, err := tx.Exec(ctx,
		"DELETE FROM markers WHERE subject_uid = $1 AND type = 'person'", subjectUID,
	); err != nil {
		return fmt.Errorf("people: deleting hand-attached links of subject %s: %w", subjectUID, err)
	}
	return nil
}
