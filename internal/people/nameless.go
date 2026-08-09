package people

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/panbotka/kukatko/internal/audit"
)

// NamelessSubject is a stored subject whose name identifies nobody, together with
// how much of the catalogue currently points at it. It is what the nameless-
// subject repair reports before it touches anything: a subject with a five-figure
// marker count is a catch-all an importer minted, not a person, and the counts are
// what let an operator tell the two apart before deciding.
type NamelessSubject struct {
	Subject
	// MarkerCount is how many markers are assigned to the subject.
	MarkerCount int `json:"marker_count"`
	// FaceCount is how many rows of the faces cache name the subject.
	FaceCount int `json:"face_count"`
}

// FaceRef identifies one row of the faces cache by its natural key, which is what
// a snapshot needs: face ids are not stable across a re-detection, but
// (photo, slot) is.
type FaceRef struct {
	PhotoUID  string `json:"photo_uid"`
	FaceIndex int    `json:"face_index"`
}

// SubjectSnapshot is everything DetachSubject removed, and everything
// RestoreSubject needs to put it back: the subject row itself plus every marker
// and cached face that pointed at it. Detaching is otherwise irreversible — the
// links are set NULL and nothing records what they were — so the snapshot is the
// undo, and callers are expected to persist it before applying the change.
type SubjectSnapshot struct {
	// Subject is the deleted subject row, verbatim.
	Subject Subject `json:"subject"`
	// MarkerUIDs are the markers that were assigned to it, in uid order.
	MarkerUIDs []string `json:"marker_uids"`
	// Faces are the cached face rows that named it, in (photo, slot) order.
	Faces []FaceRef `json:"faces"`
}

// listSubjectCountsSQL reads every subject with its marker and cached-face counts.
// It deliberately does not filter by name: "identifies nobody" is defined by
// NameSlug, in Go, and re-expressing that predicate as a SQL regex would give the
// repair a second definition free to drift from the one the importers guard on.
// The subjects table holds hundreds of rows at most, so listing it whole costs
// nothing.
const listSubjectCountsSQL = `
SELECT ` + subjectColumns + `,
       (SELECT COUNT(*) FROM markers m WHERE m.subject_uid = s.uid),
       (SELECT COUNT(*) FROM faces f WHERE f.subject_uid = s.uid)
FROM subjects s
ORDER BY s.created_at, s.uid`

// ListNamelessSubjects returns every subject whose name identifies nobody, with
// the markers and faces currently pointing at it. It is read-only — the dry run of
// the nameless-subject repair — and yields an empty slice when the library is
// clean.
func (s *Store) ListNamelessSubjects(ctx context.Context) ([]NamelessSubject, error) {
	rows, err := s.pool.Query(ctx, listSubjectCountsSQL)
	if err != nil {
		return nil, fmt.Errorf("people: listing subjects with counts: %w", err)
	}
	defer rows.Close()

	out := make([]NamelessSubject, 0)
	for rows.Next() {
		var ns NamelessSubject
		if err := rows.Scan(
			&ns.UID, &ns.Slug, &ns.Name, &ns.Type, &ns.Favorite, &ns.Private,
			&ns.Notes, &ns.CoverPhotoUID, &ns.BirthYear, &ns.DeathYear,
			&ns.CreatedAt, &ns.UpdatedAt,
			&ns.MarkerCount, &ns.FaceCount,
		); err != nil {
			return nil, fmt.Errorf("people: scanning subject with counts: %w", err)
		}
		if NameSlug(ns.Name) == "" {
			out = append(out, ns)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("people: iterating subjects with counts: %w", err)
	}
	return out, nil
}

// SnapshotSubject returns the snapshot DetachSubject would take for uid without
// changing anything. It is the read-only half of the detach, split out for the
// caller that has to hand the undo over *before* the destructive write happens:
// the admin HTTP repair delivers the undo file to the browser and only then
// schedules the detach, because over HTTP there is no `--undo-file` path to
// write to first. It returns ErrSubjectNotFound if no such subject exists.
//
// The snapshot describes the moment it is taken, not a reservation: a marker
// assigned to the subject between this call and the detach is detached but not
// recorded here, so replaying the file would leave that one marker unassigned.
// The authoritative snapshot therefore stays the one DetachSubject returns, and
// the detach path compares the two.
func (s *Store) SnapshotSubject(ctx context.Context, uid string) (SubjectSnapshot, error) {
	// A plain (read-write) transaction, not a read-only one: snapshotSubjectTx
	// takes FOR UPDATE on the subject row, which Postgres rejects in a READ ONLY
	// transaction. The lock is held only for the length of the two list queries.
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return SubjectSnapshot{}, fmt.Errorf("people: begin snapshot transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	snap, err := snapshotSubjectTx(ctx, tx, uid)
	if err != nil {
		return SubjectSnapshot{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return SubjectSnapshot{}, fmt.Errorf("people: commit snapshot transaction: %w", err)
	}
	return snap, nil
}

// DetachSubject deletes the subject identified by uid and returns the snapshot
// that undoes it. Its markers are detached (markers.subject_uid is set NULL by the
// foreign key) and the cached subject_uid/subject_name on any faces pointing at it
// are cleared, all in the same transaction as entry's audit row. It returns
// ErrSubjectNotFound — writing nothing — if no such subject exists.
//
// This is deliberately not restricted to nameless subjects: the caller decides
// what to detach (the CLI offers only the nameless ones), and keeping the
// mechanism general keeps it honest about what it does. Persist the returned
// snapshot before you need it — this call is the only moment the removed links
// exist anywhere.
func (s *Store) DetachSubject(ctx context.Context, uid string, entry audit.Entry) (SubjectSnapshot, error) {
	if entry.TargetUID == "" {
		entry.TargetUID = uid
	}
	return mutateAudited(ctx, s.pool, entry, func(tx pgx.Tx) (SubjectSnapshot, error) {
		snap, err := snapshotSubjectTx(ctx, tx, uid)
		if err != nil {
			return SubjectSnapshot{}, err
		}
		if _, err := tx.Exec(ctx,
			"UPDATE faces SET subject_uid = NULL, subject_name = '' WHERE subject_uid = $1", uid,
		); err != nil {
			return SubjectSnapshot{}, fmt.Errorf("people: clearing faces cache for subject %s: %w", uid, err)
		}
		tag, err := tx.Exec(ctx, "DELETE FROM subjects WHERE uid = $1", uid)
		if err != nil {
			return SubjectSnapshot{}, fmt.Errorf("people: deleting subject %s: %w", uid, err)
		}
		if tag.RowsAffected() == 0 {
			return SubjectSnapshot{}, ErrSubjectNotFound
		}
		return snap, nil
	})
}

// snapshotSubjectTx reads the subject and every marker and cached face pointing at
// it inside tx, so the snapshot is consistent with the deletion that follows it in
// the same transaction. A missing subject yields ErrSubjectNotFound.
func snapshotSubjectTx(ctx context.Context, tx pgx.Tx, uid string) (SubjectSnapshot, error) {
	subj, err := scanSubject(tx.QueryRow(ctx,
		"SELECT "+subjectColumns+" FROM subjects WHERE uid = $1 FOR UPDATE", uid))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return SubjectSnapshot{}, ErrSubjectNotFound
		}
		return SubjectSnapshot{}, err
	}
	markerUIDs, err := collectRows(ctx, tx,
		"SELECT uid FROM markers WHERE subject_uid = $1 ORDER BY uid",
		func(rows pgx.Rows) (string, error) {
			var markerUID string
			if err := rows.Scan(&markerUID); err != nil {
				return "", fmt.Errorf("scanning marker uid: %w", err)
			}
			return markerUID, nil
		}, uid)
	if err != nil {
		return SubjectSnapshot{}, fmt.Errorf("people: listing markers of subject %s: %w", uid, err)
	}
	faces, err := collectRows(ctx, tx,
		"SELECT photo_uid, face_index FROM faces WHERE subject_uid = $1 ORDER BY photo_uid, face_index",
		func(rows pgx.Rows) (FaceRef, error) {
			var ref FaceRef
			if err := rows.Scan(&ref.PhotoUID, &ref.FaceIndex); err != nil {
				return FaceRef{}, fmt.Errorf("scanning face ref: %w", err)
			}
			return ref, nil
		}, uid)
	if err != nil {
		return SubjectSnapshot{}, fmt.Errorf("people: listing faces of subject %s: %w", uid, err)
	}
	return SubjectSnapshot{Subject: subj, MarkerUIDs: markerUIDs, Faces: faces}, nil
}

// restoreSubjectSQL re-inserts a snapshotted subject under its original uid and
// timestamps, so an undo restores the row rather than an approximation of it. The
// slug is a parameter because a base slug taken in the meantime has to be
// disambiguated.
const restoreSubjectSQL = `
INSERT INTO subjects (uid, slug, name, type, favorite, private, notes,
                      cover_photo_uid, birth_year, death_year, created_at, updated_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
RETURNING ` + subjectColumns

// RestoreSubject puts a snapshot back: it re-inserts the subject, re-assigns the
// markers that were detached from it and refreshes the cached subject on the faces
// that named it, in the same transaction as entry's audit row. It is the undo of
// DetachSubject and returns the restored subject.
//
// The restored row keeps its original uid, name and timestamps; only the slug may
// differ, because another subject may have taken the base slug in the meantime and
// slugs are unique. A marker or face deleted since the snapshot is simply not
// restored — the update matches what still exists — so a partially outdated
// snapshot restores what it can instead of failing.
func (s *Store) RestoreSubject(ctx context.Context, snap SubjectSnapshot, entry audit.Entry) (Subject, error) {
	if snap.Subject.UID == "" {
		return Subject{}, fmt.Errorf("%w: snapshot carries no subject uid", ErrSubjectNotFound)
	}
	if entry.TargetUID == "" {
		entry.TargetUID = snap.Subject.UID
	}
	// Each slug attempt runs in its own transaction (insertAuditedWithUniqueSlug),
	// which is what makes retrying possible at all: a unique violation aborts the
	// transaction it happens in, so a retry inside one would only ever hit 25P02.
	restored, err := insertAuditedWithUniqueSlug(ctx, s.pool, snap.Subject.Slug, entry,
		func(tx pgx.Tx, slug string) (Subject, error) {
			subj := snap.Subject
			// The insert's error is returned unwrapped by design: the caller inspects
			// it for the slug unique violation that drives the next attempt.
			restored, err := scanSubject(tx.QueryRow(ctx, restoreSubjectSQL,
				subj.UID, slug, subj.Name, subj.Type, subj.Favorite, subj.Private,
				subj.Notes, subj.CoverPhotoUID, subj.BirthYear, subj.DeathYear,
				timeOrNow(subj.CreatedAt), timeOrNow(subj.UpdatedAt)))
			if err != nil {
				return Subject{}, err
			}
			return restored, relinkSnapshotTx(ctx, tx, snap, restored)
		})
	if err != nil {
		return Subject{}, fmt.Errorf("people: restoring subject %s: %w", snap.Subject.UID, err)
	}
	return restored, nil
}

// relinkSnapshotTx re-points the snapshot's markers and cached faces at the
// restored subject inside tx. The faces update joins on the (photo, slot) pairs
// the snapshot carries, since that is the natural key a face keeps across a
// re-detection.
func relinkSnapshotTx(ctx context.Context, tx pgx.Tx, snap SubjectSnapshot, restored Subject) error {
	if len(snap.MarkerUIDs) > 0 {
		if _, err := tx.Exec(ctx,
			"UPDATE markers SET subject_uid = $1 WHERE uid = ANY($2)",
			restored.UID, snap.MarkerUIDs,
		); err != nil {
			return fmt.Errorf("people: reassigning markers to subject %s: %w", restored.UID, err)
		}
	}
	if len(snap.Faces) == 0 {
		return nil
	}
	photoUIDs := make([]string, len(snap.Faces))
	indexes := make([]int32, len(snap.Faces))
	for i, ref := range snap.Faces {
		photoUIDs[i] = ref.PhotoUID
		indexes[i] = int32(ref.FaceIndex) //nolint:gosec // a face slot is a small non-negative index.
	}
	if _, err := tx.Exec(ctx,
		`UPDATE faces SET subject_uid = $1, subject_name = $2
         FROM unnest($3::text[], $4::int[]) AS ref(photo_uid, face_index)
         WHERE faces.photo_uid = ref.photo_uid AND faces.face_index = ref.face_index`,
		restored.UID, restored.Name, photoUIDs, indexes,
	); err != nil {
		return fmt.Errorf("people: restoring faces cache for subject %s: %w", restored.UID, err)
	}
	return nil
}

// timeOrNow returns t, or the current time when t is the zero value, so a
// hand-written or truncated snapshot still inserts a valid NOT NULL timestamp.
func timeOrNow(t time.Time) time.Time {
	if t.IsZero() {
		return time.Now()
	}
	return t
}

// collectRows runs a query inside tx and scans every row with scan, returning the
// collected slice. It exists so the snapshot's two list queries do not each repeat
// the rows/defer/Err dance.
func collectRows[T any](
	ctx context.Context, tx pgx.Tx, query string, scan func(pgx.Rows) (T, error), args ...any,
) ([]T, error) {
	rows, err := tx.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("querying: %w", err)
	}
	defer rows.Close()

	out := make([]T, 0)
	for rows.Next() {
		item, err := scan(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating: %w", err)
	}
	return out, nil
}
