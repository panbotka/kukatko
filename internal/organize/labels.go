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
const labelColumns = "uid, slug, name, priority, review_enabled, created_at, updated_at"

// insertLabelSQL inserts a label and returns the stored row. review_enabled is
// deliberately absent from the column list: a new label always takes part in the
// review game (the column's DEFAULT true), so no caller can create one the game
// ignores by passing a zero-valued struct. Switching it off is an update.
const insertLabelSQL = `
INSERT INTO labels (uid, slug, name, priority, project_id)
VALUES ($1, $2, $3, $4, $5)
RETURNING ` + labelColumns

// scanLabel reads one label row in labelColumns order, wrapping any scan error
// (including pgx.ErrNoRows, which callers translate to ErrLabelNotFound).
func scanLabel(row pgx.Row) (Label, error) {
	var l Label
	if err := row.Scan(
		&l.UID, &l.Slug, &l.Name, &l.Priority, &l.ReviewEnabled, &l.CreatedAt, &l.UpdatedAt,
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
		&lc.UID, &lc.Slug, &lc.Name, &lc.Priority, &lc.ReviewEnabled,
		&lc.CreatedAt, &lc.UpdatedAt, &lc.PhotoCount,
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

// insertLabelRow inserts l into the project with the given slug using q (a pool
// or a transaction) and returns the stored row.
func insertLabelRow(ctx context.Context, q rowQuerier, projectUID string, l Label, slug string) (Label, error) {
	return scanLabel(q.QueryRow(ctx, insertLabelSQL, l.UID, slug, l.Name, l.Priority, projectUID))
}

// CreateLabel inserts l into the project and returns it refreshed with the
// generated UID, unique slug and timestamps. The slug is derived from l.Name and
// a numeric suffix is appended on collision; slugs are unique per project.
// l.ReviewEnabled is ignored — a new label always takes part in the review game;
// see insertLabelSQL.
func (s *Store) CreateLabel(ctx context.Context, projectUID string, l Label) (Label, error) {
	if err := requireProject(projectUID); err != nil {
		return Label{}, err
	}
	prepared, base, err := prepareLabelInsert(l)
	if err != nil {
		return Label{}, err
	}
	return insertWithUniqueSlug(base, func(slug string) (Label, error) {
		return insertLabelRow(ctx, s.pool, projectUID, prepared, slug)
	})
}

// GetLabelByUID returns the label of the project with the given UID, or
// ErrLabelNotFound (also for a label of another project).
func (s *Store) GetLabelByUID(ctx context.Context, projectUID, uid string) (Label, error) {
	return s.getLabel(ctx, projectUID, "uid", uid)
}

// GetLabelBySlug returns the label of the project with the given slug, or
// ErrLabelNotFound.
func (s *Store) GetLabelBySlug(ctx context.Context, projectUID, slug string) (Label, error) {
	return s.getLabel(ctx, projectUID, "slug", slug)
}

// getLabel fetches a single label of the project filtered by an equality on the
// trusted column name col (an internal constant, never user input), translating
// pgx.ErrNoRows into ErrLabelNotFound.
func (s *Store) getLabel(ctx context.Context, projectUID, col, val string) (Label, error) {
	if err := requireProject(projectUID); err != nil {
		return Label{}, err
	}
	q := "SELECT " + labelColumns + " FROM labels WHERE " + col + " = $1 AND project_id = $2"
	l, err := scanLabel(s.pool.QueryRow(ctx, q, val, projectUID))
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
UPDATE labels SET slug = $2, name = $3, priority = $4, review_enabled = $5, updated_at = now()
WHERE uid = $1 AND project_id = $6
RETURNING ` + labelColumns

// updateLabelRow rewrites the label of the project identified by uid with upd
// and the given slug using q (a pool or a transaction), returning the refreshed
// row (or pgx.ErrNoRows when no label of the project matches, which callers
// translate to ErrLabelNotFound).
func updateLabelRow(
	ctx context.Context, q rowQuerier, projectUID, uid string, upd LabelUpdate, slug string,
) (Label, error) {
	return scanLabel(q.QueryRow(ctx, updateLabelSQL,
		uid, slug, upd.Name, upd.Priority, upd.ReviewEnabled, projectUID))
}

// UpdateLabel applies upd to the label of the project identified by uid: it
// re-slugs from the new name (kept unique within the project) and rewrites the
// editable fields, the review-game switch included — upd carries the value to
// store, not a delta. It returns ErrLabelNotFound if the project holds no such
// label.
func (s *Store) UpdateLabel(ctx context.Context, projectUID, uid string, upd LabelUpdate) (Label, error) {
	if err := requireProject(projectUID); err != nil {
		return Label{}, err
	}
	base := slugify(upd.Name, labelFallbackSlug)
	updated, err := insertWithUniqueSlug(base, func(slug string) (Label, error) {
		return updateLabelRow(ctx, s.pool, projectUID, uid, upd, slug)
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
SELECT l.uid, l.slug, l.name, l.priority, l.review_enabled, l.created_at, l.updated_at,
       COUNT(p.uid) AS photo_count
FROM labels l
LEFT JOIN photo_labels pl ON pl.label_uid = l.uid
LEFT JOIN photos p ON p.uid = pl.photo_uid AND p.project_id = l.project_id AND p.archived_at IS NULL
    AND (p.stack_uid IS NULL OR p.stack_primary)
WHERE l.project_id = $1
GROUP BY l.uid
ORDER BY l.priority DESC, l.name, l.uid`

// ListLabels returns every label of the project together with how many of the
// project's photos carry it and the photo that stands for it, ordered by
// priority then name. A project with no labels yields an empty slice and a nil
// error.
//
// The covers are resolved in a second, batched query over the whole listing
// rather than per label (see stampLabelCovers), so drawing previews costs two
// queries whatever the library holds.
func (s *Store) ListLabels(ctx context.Context, projectUID string) ([]LabelCount, error) {
	if err := requireProject(projectUID); err != nil {
		return nil, err
	}
	rows, err := s.pool.Query(ctx, listLabelsSQL, projectUID)
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
	return s.stampLabelCovers(ctx, projectUID, out)
}

// stampLabelCovers fills CoverUID on every label in list from one batched cover
// lookup, leaving a label with no visible photo untouched (nil). It returns list
// itself, so a caller can return its result directly.
func (s *Store) stampLabelCovers(ctx context.Context, projectUID string, list []LabelCount) ([]LabelCount, error) {
	uids := make([]string, 0, len(list))
	for _, lc := range list {
		uids = append(uids, lc.UID)
	}
	covers, err := s.LabelCovers(ctx, projectUID, uids)
	if err != nil {
		return nil, err
	}
	for i := range list {
		if cover, ok := covers[list[i].UID]; ok {
			list[i].CoverUID = &cover.PhotoUID
		}
	}
	return list, nil
}

// deleteLabelRow deletes the label of the project identified by uid using e (a
// pool or a transaction), returning ErrLabelNotFound when no row matched. Its
// photo_labels attachment rows are removed by ON DELETE CASCADE.
func deleteLabelRow(ctx context.Context, e execer, projectUID, uid string) error {
	if err := requireProject(projectUID); err != nil {
		return err
	}
	tag, err := e.Exec(ctx, "DELETE FROM labels WHERE uid = $1 AND project_id = $2", uid, projectUID)
	if err != nil {
		return fmt.Errorf("organize: deleting label %s: %w", uid, err)
	}
	if tag.RowsAffected() == 0 {
		return ErrLabelNotFound
	}
	return nil
}

// DeleteLabel removes the label of the project identified by uid. Its
// photo_labels attachment rows are removed by ON DELETE CASCADE. It returns
// ErrLabelNotFound if the project holds no such label.
func (s *Store) DeleteLabel(ctx context.Context, projectUID, uid string) error {
	return deleteLabelRow(ctx, s.pool, projectUID, uid)
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
// q (a pool or a transaction) after proving the label and the photo inside the
// project (ErrLabelNotFound, ErrPhotoNotFound otherwise, nothing written),
// translating a foreign-key violation the same way. The source must already be
// normalised.
func attachLabelRow(
	ctx context.Context, q querierExecer, projectUID, photoUID, labelUID string, source LabelSource, uncertainty int,
) error {
	if err := requireLabelInProject(ctx, q, projectUID, labelUID); err != nil {
		return err
	}
	if err := requirePhotosInProject(ctx, q, projectUID, photoUID); err != nil {
		return err
	}
	if _, err := q.Exec(ctx, attachLabelSQL, photoUID, labelUID, source, uncertainty); err != nil {
		return translateAttachFK(err)
	}
	return nil
}

// AttachLabelTx attaches labelUID to photoUID on tx, the caller's open
// transaction, as a manual label with no uncertainty — the label a curator chose
// for every photo an upload link brings in. It is idempotent like AttachLabel and
// returns ErrLabelNotFound or ErrPhotoNotFound when either side does not exist in
// the project.
func AttachLabelTx(ctx context.Context, tx pgx.Tx, projectUID, photoUID, labelUID string) error {
	return attachLabelRow(ctx, tx, projectUID, photoUID, labelUID, SourceManual, 0)
}

// AttachLabel attaches labelUID to photoUID with the given source and uncertainty,
// updating both if the label is already attached (idempotent). An empty source
// defaults to SourceManual; an unrecognised source returns ErrInvalidSource. It
// returns ErrLabelNotFound or ErrPhotoNotFound when either side does not exist in
// the project.
func (s *Store) AttachLabel(
	ctx context.Context, projectUID, photoUID, labelUID string, source LabelSource, uncertainty int,
) error {
	if err := requireProject(projectUID); err != nil {
		return err
	}
	normalized, err := normalizeLabelSource(source)
	if err != nil {
		return err
	}
	return attachLabelRow(ctx, s.pool, projectUID, photoUID, labelUID, normalized, uncertainty)
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
// that is not attached returns a nil error. A label outside the project is
// ErrLabelNotFound.
func (s *Store) DetachLabel(ctx context.Context, projectUID, photoUID, labelUID string) error {
	if err := requireLabelInProject(ctx, s.pool, projectUID, labelUID); err != nil {
		return err
	}
	return detachLabelRow(ctx, s.pool, photoUID, labelUID)
}

// listLabelPhotoUIDsSQL returns the photos of the project ($2) carrying a label
// of it, newest attachment first then by uid for a stable order.
const listLabelPhotoUIDsSQL = `
SELECT pl.photo_uid FROM photo_labels pl
JOIN labels l ON l.uid = pl.label_uid AND l.project_id = $2
JOIN photos p ON p.uid = pl.photo_uid AND p.project_id = $2
WHERE pl.label_uid = $1
ORDER BY pl.created_at DESC, pl.photo_uid`

// ListPhotoUIDsByLabel returns the UIDs of every photo the label of the project
// identified by labelUID is attached to. A label on no photos — and a label
// outside the project — yields an empty slice and a nil error. The caller
// resolves the UIDs to full photo records.
func (s *Store) ListPhotoUIDsByLabel(ctx context.Context, projectUID, labelUID string) ([]string, error) {
	if err := requireProject(projectUID); err != nil {
		return nil, err
	}
	rows, err := s.pool.Query(ctx, listLabelPhotoUIDsSQL, labelUID, projectUID)
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
const listLabelsForPhotoSQL = "SELECT l.uid, l.slug, l.name, l.priority, l.review_enabled, " +
	"l.created_at, l.updated_at " +
	"FROM labels l JOIN photo_labels pl ON pl.label_uid = l.uid " +
	"WHERE pl.photo_uid = $1 AND l.project_id = $2 " +
	"ORDER BY l.priority DESC, l.name"

// LabelsForPhoto returns the labels of the project attached to the photo
// identified by photoUID, ordered by priority then name. It backs the photo
// detail view's inline label chips. An unknown photo yields an empty slice (not
// an error).
func (s *Store) LabelsForPhoto(ctx context.Context, projectUID, photoUID string) ([]Label, error) {
	if err := requireProject(projectUID); err != nil {
		return nil, err
	}
	rows, err := s.pool.Query(ctx, listLabelsForPhotoSQL, photoUID, projectUID)
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
