package organize

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
)

// labelColumns is the canonical, ordered column list for label reads, matched by
// scanLabel.
const labelColumns = "uid, slug, name, priority, created_at, updated_at"

// insertLabelSQL inserts a label and returns the stored row.
const insertLabelSQL = `
INSERT INTO labels (uid, slug, name, priority)
VALUES ($1, $2, $3, $4)
RETURNING ` + labelColumns

// scanLabel reads one label row in labelColumns order, wrapping any scan error
// (including pgx.ErrNoRows, which callers translate to ErrLabelNotFound).
func scanLabel(row pgx.Row) (Label, error) {
	var l Label
	if err := row.Scan(
		&l.UID, &l.Slug, &l.Name, &l.Priority, &l.CreatedAt, &l.UpdatedAt,
	); err != nil {
		return Label{}, fmt.Errorf("organize: scanning label: %w", err)
	}
	return l, nil
}

// scanLabelCount reads one label-with-count row (the labelColumns list followed
// by a photo_count column) in order, wrapping any scan error. It is shared by
// ListLabels and SearchLabels, whose projections match.
func scanLabelCount(row pgx.Row) (LabelCount, error) {
	var lc LabelCount
	if err := row.Scan(
		&lc.UID, &lc.Slug, &lc.Name, &lc.Priority, &lc.CreatedAt, &lc.UpdatedAt, &lc.PhotoCount,
	); err != nil {
		return LabelCount{}, fmt.Errorf("organize: scanning label count: %w", err)
	}
	return lc, nil
}

// prepareLabelInsert ensures l carries a UID and returns it together with the base
// slug derived from its name. It is shared by CreateLabel and CreateLabelAudited.
func prepareLabelInsert(l Label) (Label, string, error) {
	if l.UID == "" {
		uid, err := newLabelUID()
		if err != nil {
			return Label{}, "", err
		}
		l.UID = uid
	}
	return l, slugify(l.Name, labelFallbackSlug), nil
}

// insertLabelRow inserts l with the given slug using q (a pool or a transaction)
// and returns the stored row.
func insertLabelRow(ctx context.Context, q rowQuerier, l Label, slug string) (Label, error) {
	return scanLabel(q.QueryRow(ctx, insertLabelSQL, l.UID, slug, l.Name, l.Priority))
}

// CreateLabel inserts l and returns it refreshed with the generated UID, unique
// slug and timestamps. The slug is derived from l.Name and a numeric suffix is
// appended on collision.
func (s *Store) CreateLabel(ctx context.Context, l Label) (Label, error) {
	prepared, base, err := prepareLabelInsert(l)
	if err != nil {
		return Label{}, err
	}
	return insertWithUniqueSlug(base, func(slug string) (Label, error) {
		return insertLabelRow(ctx, s.pool, prepared, slug)
	})
}

// GetLabelByUID returns the label with the given UID, or ErrLabelNotFound.
func (s *Store) GetLabelByUID(ctx context.Context, uid string) (Label, error) {
	return s.getLabel(ctx, "uid", uid)
}

// GetLabelBySlug returns the label with the given slug, or ErrLabelNotFound.
func (s *Store) GetLabelBySlug(ctx context.Context, slug string) (Label, error) {
	return s.getLabel(ctx, "slug", slug)
}

// getLabel fetches a single label filtered by an equality on the trusted column
// name col (an internal constant, never user input), translating pgx.ErrNoRows
// into ErrLabelNotFound.
func (s *Store) getLabel(ctx context.Context, col, val string) (Label, error) {
	q := "SELECT " + labelColumns + " FROM labels WHERE " + col + " = $1"
	l, err := scanLabel(s.pool.QueryRow(ctx, q, val))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Label{}, ErrLabelNotFound
		}
		return Label{}, err
	}
	return l, nil
}

// updateLabelSQL rewrites a label's editable fields (including a re-derived slug)
// and returns the refreshed row.
const updateLabelSQL = `
UPDATE labels SET slug = $2, name = $3, priority = $4, updated_at = now()
WHERE uid = $1
RETURNING ` + labelColumns

// updateLabelRow rewrites the label identified by uid with upd and the given slug
// using q (a pool or a transaction), returning the refreshed row (or pgx.ErrNoRows
// when no label matches, which callers translate to ErrLabelNotFound).
func updateLabelRow(ctx context.Context, q rowQuerier, uid string, upd LabelUpdate, slug string) (Label, error) {
	return scanLabel(q.QueryRow(ctx, updateLabelSQL, uid, slug, upd.Name, upd.Priority))
}

// UpdateLabel applies upd to the label identified by uid: it re-slugs from the new
// name (kept unique) and rewrites the editable fields. It returns ErrLabelNotFound
// if no such label exists.
func (s *Store) UpdateLabel(ctx context.Context, uid string, upd LabelUpdate) (Label, error) {
	base := slugify(upd.Name, labelFallbackSlug)
	updated, err := insertWithUniqueSlug(base, func(slug string) (Label, error) {
		return updateLabelRow(ctx, s.pool, uid, upd, slug)
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return Label{}, ErrLabelNotFound
	}
	return updated, err
}

// listLabelsSQL reads every label with its photo count, ordered by priority
// (highest first) then name then uid for a stable display. The count joins photos
// so it counts only the label's visible members (not archived, not a non-primary
// stack member) — COUNT(p.uid) ignores the NULL rows hidden photos join as — so
// the badge agrees with the grid the label filter shows.
const listLabelsSQL = `
SELECT l.uid, l.slug, l.name, l.priority, l.created_at, l.updated_at,
       COUNT(p.uid) AS photo_count
FROM labels l
LEFT JOIN photo_labels pl ON pl.label_uid = l.uid
LEFT JOIN photos p ON p.uid = pl.photo_uid AND p.archived_at IS NULL
    AND (p.stack_uid IS NULL OR p.stack_primary)
GROUP BY l.uid
ORDER BY l.priority DESC, l.name, l.uid`

// ListLabels returns every label together with how many photos carry it, ordered
// by priority then name. A store with no labels yields an empty slice and a nil
// error.
func (s *Store) ListLabels(ctx context.Context) ([]LabelCount, error) {
	rows, err := s.pool.Query(ctx, listLabelsSQL)
	if err != nil {
		return nil, fmt.Errorf("organize: listing labels: %w", err)
	}
	defer rows.Close()

	out := make([]LabelCount, 0)
	for rows.Next() {
		lc, err := scanLabelCount(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, lc)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("organize: iterating labels: %w", err)
	}
	return out, nil
}

// deleteLabelRow deletes the label identified by uid using e (a pool or a
// transaction), returning ErrLabelNotFound when no row matched. Its photo_labels
// attachment rows are removed by ON DELETE CASCADE.
func deleteLabelRow(ctx context.Context, e execer, uid string) error {
	tag, err := e.Exec(ctx, "DELETE FROM labels WHERE uid = $1", uid)
	if err != nil {
		return fmt.Errorf("organize: deleting label %s: %w", uid, err)
	}
	if tag.RowsAffected() == 0 {
		return ErrLabelNotFound
	}
	return nil
}

// DeleteLabel removes the label identified by uid. Its photo_labels attachment
// rows are removed by ON DELETE CASCADE. It returns ErrLabelNotFound if no such
// label exists.
func (s *Store) DeleteLabel(ctx context.Context, uid string) error {
	return deleteLabelRow(ctx, s.pool, uid)
}

// attachLabelSQL attaches a label to a photo, updating the source and uncertainty
// if the attachment already exists so the call is idempotent.
const attachLabelSQL = `
INSERT INTO photo_labels (photo_uid, label_uid, source, uncertainty)
VALUES ($1, $2, $3, $4)
ON CONFLICT (photo_uid, label_uid)
DO UPDATE SET source = EXCLUDED.source, uncertainty = EXCLUDED.uncertainty`

// normalizeLabelSource defaults an empty source to SourceManual and validates it,
// returning ErrInvalidSource for an unrecognised value. It is shared by AttachLabel
// and AttachLabelAudited so the validation happens once, before any transaction.
func normalizeLabelSource(source LabelSource) (LabelSource, error) {
	if source == "" {
		source = SourceManual
	}
	if !source.valid() {
		return "", fmt.Errorf("%w: %q", ErrInvalidSource, source)
	}
	return source, nil
}

// attachLabelRow attaches labelUID to photoUID with source and uncertainty using
// e (a pool or a transaction), translating a foreign-key violation to
// ErrLabelNotFound or ErrPhotoNotFound. The source must already be normalised.
func attachLabelRow(
	ctx context.Context, e execer, photoUID, labelUID string, source LabelSource, uncertainty int,
) error {
	if _, err := e.Exec(ctx, attachLabelSQL, photoUID, labelUID, source, uncertainty); err != nil {
		return translateAttachFK(err)
	}
	return nil
}

// AttachLabel attaches labelUID to photoUID with the given source and uncertainty,
// updating both if the label is already attached (idempotent). An empty source
// defaults to SourceManual; an unrecognised source returns ErrInvalidSource. It
// returns ErrLabelNotFound or ErrPhotoNotFound when either side does not exist.
func (s *Store) AttachLabel(
	ctx context.Context, photoUID, labelUID string, source LabelSource, uncertainty int,
) error {
	normalized, err := normalizeLabelSource(source)
	if err != nil {
		return err
	}
	return attachLabelRow(ctx, s.pool, photoUID, labelUID, normalized, uncertainty)
}

// detachLabelRow removes labelUID from photoUID using e (a pool or a transaction),
// wrapping any error. Detaching a label that is not attached is a no-op.
func detachLabelRow(ctx context.Context, e execer, photoUID, labelUID string) error {
	if _, err := e.Exec(ctx,
		"DELETE FROM photo_labels WHERE photo_uid = $1 AND label_uid = $2", photoUID, labelUID); err != nil {
		return fmt.Errorf("organize: detaching label %s from photo %s: %w", labelUID, photoUID, err)
	}
	return nil
}

// DetachLabel removes labelUID from photoUID. It is idempotent: detaching a label
// that is not attached returns a nil error.
func (s *Store) DetachLabel(ctx context.Context, photoUID, labelUID string) error {
	return detachLabelRow(ctx, s.pool, photoUID, labelUID)
}

// listLabelPhotoUIDsSQL returns the photos carrying a label, newest attachment
// first then by uid for a stable order.
const listLabelPhotoUIDsSQL = `
SELECT photo_uid FROM photo_labels
WHERE label_uid = $1
ORDER BY created_at DESC, photo_uid`

// ListPhotoUIDsByLabel returns the UIDs of every photo the label identified by
// labelUID is attached to. A label on no photos yields an empty slice and a nil
// error. The caller resolves the UIDs to full photo records.
func (s *Store) ListPhotoUIDsByLabel(ctx context.Context, labelUID string) ([]string, error) {
	rows, err := s.pool.Query(ctx, listLabelPhotoUIDsSQL, labelUID)
	if err != nil {
		return nil, fmt.Errorf("organize: listing photos for label %s: %w", labelUID, err)
	}
	defer rows.Close()

	out := make([]string, 0)
	for rows.Next() {
		var uid string
		if err := rows.Scan(&uid); err != nil {
			return nil, fmt.Errorf("organize: scanning label photo uid: %w", err)
		}
		out = append(out, uid)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("organize: iterating label photos for %s: %w", labelUID, err)
	}
	return out, nil
}

// listLabelsForPhotoSQL selects every label attached to a photo, highest
// priority first then by name. The columns are alias-qualified because
// photo_labels also has a created_at column, which would otherwise be ambiguous.
const listLabelsForPhotoSQL = "SELECT l.uid, l.slug, l.name, l.priority, l.created_at, l.updated_at " +
	"FROM labels l JOIN photo_labels pl ON pl.label_uid = l.uid WHERE pl.photo_uid = $1 " +
	"ORDER BY l.priority DESC, l.name"

// LabelsForPhoto returns the labels attached to the photo identified by photoUID,
// ordered by priority then name. It backs the photo detail view's inline label
// chips. An unknown photo yields an empty slice (not an error).
func (s *Store) LabelsForPhoto(ctx context.Context, photoUID string) ([]Label, error) {
	rows, err := s.pool.Query(ctx, listLabelsForPhotoSQL, photoUID)
	if err != nil {
		return nil, fmt.Errorf("organize: listing labels for photo %s: %w", photoUID, err)
	}
	defer rows.Close()

	out := make([]Label, 0)
	for rows.Next() {
		label, err := scanLabel(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, label)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("organize: iterating labels for photo %s: %w", photoUID, err)
	}
	return out, nil
}

// translateAttachFK maps a foreign-key violation from a photo_labels write to
// ErrLabelNotFound or ErrPhotoNotFound by inspecting the violated constraint, and
// wraps any other error. The constraint name is matched on the referencing column
// ("photo_uid") rather than the table name, because the table name "photo_labels"
// contains "label" in both constraints.
func translateAttachFK(err error) error {
	if name, ok := isForeignKeyViolation(err); ok {
		if strings.Contains(name, "photo_uid") {
			return ErrPhotoNotFound
		}
		return ErrLabelNotFound
	}
	return fmt.Errorf("organize: label attachment write: %w", err)
}
