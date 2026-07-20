package bulk

import (
	"context"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/panbotka/kukatko/internal/audit"
	"github.com/panbotka/kukatko/internal/photos"
)

// addAlbumPhotoSQL adds a photo to an album, leaving an existing membership
// untouched so the operation is idempotent. Albums are always presented
// chronologically, so a membership carries no position.
const addAlbumPhotoSQL = `
INSERT INTO album_photos (album_uid, photo_uid)
VALUES ($1, $2)
ON CONFLICT (album_uid, photo_uid) DO NOTHING`

// removeAlbumPhotoSQL removes a photo from an album (idempotent).
const removeAlbumPhotoSQL = `DELETE FROM album_photos WHERE album_uid = $1 AND photo_uid = $2`

// addLabelSQL attaches a label to a photo as a manual label, leaving an existing
// attachment untouched so the operation is idempotent.
const addLabelSQL = `
INSERT INTO photo_labels (photo_uid, label_uid, source, uncertainty)
VALUES ($1, $2, 'manual', 0)
ON CONFLICT (photo_uid, label_uid) DO NOTHING`

// removeLabelSQL detaches a label from a photo (idempotent).
const removeLabelSQL = `DELETE FROM photo_labels WHERE photo_uid = $1 AND label_uid = $2`

// addFavoriteSQL records the acting user's favorite (idempotent).
const addFavoriteSQL = `
INSERT INTO user_favorites (user_uid, photo_uid)
VALUES ($1, $2)
ON CONFLICT (user_uid, photo_uid) DO NOTHING`

// removeFavoriteSQL removes the acting user's favorite (idempotent).
const removeFavoriteSQL = `DELETE FROM user_favorites WHERE user_uid = $1 AND photo_uid = $2`

// setRatingSQL upserts the acting user's star rating, leaving any flag at its
// existing value (or its 'none' default for a new row).
const setRatingSQL = `
INSERT INTO user_ratings (user_uid, photo_uid, rating)
VALUES ($1, $2, $3)
ON CONFLICT (user_uid, photo_uid) DO UPDATE SET rating = EXCLUDED.rating, updated_at = now()`

// setFlagSQL upserts the acting user's pick/reject flag, leaving any rating at
// its existing value (or its 0 default for a new row).
const setFlagSQL = `
INSERT INTO user_ratings (user_uid, photo_uid, flag)
VALUES ($1, $2, $3)
ON CONFLICT (user_uid, photo_uid) DO UPDATE SET flag = EXCLUDED.flag, updated_at = now()`

// pruneRatingSQL drops a rating row that has fallen back to all defaults (rating
// 0 and flag 'none'), keeping the table sparse like the organize store does.
const pruneRatingSQL = `
DELETE FROM user_ratings
WHERE user_uid = $1 AND photo_uid = $2 AND rating = 0 AND flag = 'none'`

// Apply runs the bulk operations against the target photos in a single
// transaction. actorUID is the acting user (used for favorites and the audit
// entry). It validates the batch, verifies that every album/label referenced by
// an add operation exists (else ErrAlbumNotFound/ErrLabelNotFound), applies the
// operations photo-by-photo (missing photos become per-photo errors without
// aborting the rest), writes a durable audit_log entry in the same transaction,
// and commits. A database failure mid-batch rolls everything back and is
// returned. The Result carries per-photo statuses and aggregate counts.
func (s *Service) Apply(
	ctx context.Context, actorUID string, photoUIDs []string, ops Operations,
) (Result, error) {
	if err := s.validateBatch(photoUIDs, ops); err != nil {
		return Result{}, err
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Result{}, fmt.Errorf("bulk: begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if err := validateReferences(ctx, tx, ops); err != nil {
		return Result{}, err
	}
	result, err := applyAll(ctx, tx, actorUID, photoUIDs, ops)
	if err != nil {
		return Result{}, err
	}
	if err := writeAudit(ctx, tx, actorUID, photoUIDs, ops, result.Counts); err != nil {
		return Result{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Result{}, fmt.Errorf("bulk: commit transaction: %w", err)
	}
	return result, nil
}

// validateBatch checks the request shape before opening a transaction: a
// non-empty photo list within the batch limit and a non-empty operation set.
func (s *Service) validateBatch(photoUIDs []string, ops Operations) error {
	if len(photoUIDs) == 0 {
		return ErrNoPhotos
	}
	if len(photoUIDs) > s.maxBatch {
		return fmt.Errorf("%w: %d exceeds limit %d", ErrBatchTooLarge, len(photoUIDs), s.maxBatch)
	}
	if ops.IsEmpty() {
		return ErrNoOperations
	}
	return nil
}

// validateReferences verifies that every album and label referenced by an add
// operation exists, so the per-photo loop never trips a foreign-key violation.
func validateReferences(ctx context.Context, tx pgx.Tx, ops Operations) error {
	if err := requireAllExist(ctx, tx, "albums", ops.AddAlbums, ErrAlbumNotFound); err != nil {
		return err
	}
	return requireAllExist(ctx, tx, "labels", ops.AddLabels, ErrLabelNotFound)
}

// requireAllExist returns notFound (wrapped with the offending UID) if any uid is
// absent from table. table is an internal constant ("albums"/"labels"), never
// user input, so interpolating it into the query is safe.
func requireAllExist(ctx context.Context, tx pgx.Tx, table string, uids []string, notFound error) error {
	if len(uids) == 0 {
		return nil
	}
	query := fmt.Sprintf("SELECT EXISTS(SELECT 1 FROM %s WHERE uid = $1)", table)
	for _, uid := range uids {
		ok, err := existsRow(ctx, tx, query, uid)
		if err != nil {
			return err
		}
		if !ok {
			return fmt.Errorf("%w: %s", notFound, uid)
		}
	}
	return nil
}

// applyAll iterates the target photos, applying the operations to each and
// recording its outcome. Duplicate UIDs are skipped; missing photos are errored;
// a database error aborts the whole batch.
func applyAll(
	ctx context.Context, tx pgx.Tx, actorUID string, photoUIDs []string, ops Operations,
) (Result, error) {
	seen := make(map[string]bool, len(photoUIDs))
	var result Result
	for _, uid := range photoUIDs {
		if seen[uid] {
			result.add(uid, StatusSkipped, "duplicate uid in request")
			continue
		}
		seen[uid] = true

		exists, err := photoExists(ctx, tx, uid)
		if err != nil {
			return Result{}, err
		}
		if !exists {
			result.add(uid, StatusError, "photo not found")
			continue
		}
		if err := processPhoto(ctx, tx, uid, actorUID, ops); err != nil {
			return Result{}, err
		}
		result.add(uid, StatusUpdated, "")
	}
	result.Counts.Total = len(result.Results)
	return result, nil
}

// processPhoto applies every requested operation to a single existing photo.
func processPhoto(ctx context.Context, tx pgx.Tx, uid, actorUID string, ops Operations) error {
	if query, args, ok := ops.photoColumnUpdate(uid); ok {
		if _, err := tx.Exec(ctx, query, args...); err != nil {
			return fmt.Errorf("bulk: updating photo %s: %w", uid, err)
		}
	}
	// Archiving takes the photo out of its stack, in this transaction: the default
	// visibility gate is (stack_uid IS NULL OR stack_primary), so archiving a
	// primary without re-electing one would hide its still-live siblings.
	if ops.Archive != nil && *ops.Archive {
		if err := photos.LeaveStackTx(ctx, tx, uid); err != nil {
			return fmt.Errorf("bulk: repairing stack of photo %s: %w", uid, err)
		}
	}
	if err := applyAlbums(ctx, tx, uid, ops); err != nil {
		return err
	}
	if err := applyLabels(ctx, tx, uid, ops); err != nil {
		return err
	}
	if err := applyFavorite(ctx, tx, uid, actorUID, ops); err != nil {
		return err
	}
	return applyRating(ctx, tx, uid, actorUID, ops)
}

// photoColumnUpdate builds the UPDATE photos statement for the column-level
// operations (title, description, location, archive state). It returns
// ok=false when no column-level change is requested.
func (o Operations) photoColumnUpdate(uid string) (string, []any, bool) {
	set := []string{"updated_at = now()"}
	args := []any{uid}
	appendSet := func(column string, value any) {
		args = append(args, value)
		set = append(set, fmt.Sprintf("%s = $%d", column, len(args)))
	}
	if o.Title != nil {
		appendSet("title", *o.Title)
	}
	if o.Description != nil {
		appendSet("description", *o.Description)
	}
	if o.Location != nil {
		appendSet("lat", o.Location.Lat)
		appendSet("lng", o.Location.Lng)
	}
	o.appendLocationAndArchive(&set)
	if len(set) == 1 {
		return "", nil, false
	}
	return "UPDATE photos SET " + strings.Join(set, ", ") + " WHERE uid = $1", args, true
}

// appendLocationAndArchive adds the literal (argument-free) SET clauses for
// clearing location and for the archive/unarchive toggle.
func (o Operations) appendLocationAndArchive(set *[]string) {
	if o.ClearLocation {
		*set = append(*set, "lat = NULL", "lng = NULL")
	}
	if o.Archive != nil {
		if *o.Archive {
			*set = append(*set, "archived_at = now()")
		} else {
			*set = append(*set, "archived_at = NULL")
		}
	}
}

// applyAlbums adds the photo to and removes it from the requested albums.
func applyAlbums(ctx context.Context, tx pgx.Tx, uid string, ops Operations) error {
	for _, albumUID := range ops.AddAlbums {
		if _, err := tx.Exec(ctx, addAlbumPhotoSQL, albumUID, uid); err != nil {
			return fmt.Errorf("bulk: adding photo %s to album %s: %w", uid, albumUID, err)
		}
	}
	for _, albumUID := range ops.RemoveAlbums {
		if _, err := tx.Exec(ctx, removeAlbumPhotoSQL, albumUID, uid); err != nil {
			return fmt.Errorf("bulk: removing photo %s from album %s: %w", uid, albumUID, err)
		}
	}
	return nil
}

// applyLabels attaches and detaches the requested labels for the photo.
func applyLabels(ctx context.Context, tx pgx.Tx, uid string, ops Operations) error {
	for _, labelUID := range ops.AddLabels {
		if _, err := tx.Exec(ctx, addLabelSQL, uid, labelUID); err != nil {
			return fmt.Errorf("bulk: adding label %s to photo %s: %w", labelUID, uid, err)
		}
	}
	for _, labelUID := range ops.RemoveLabels {
		if _, err := tx.Exec(ctx, removeLabelSQL, uid, labelUID); err != nil {
			return fmt.Errorf("bulk: removing label %s from photo %s: %w", labelUID, uid, err)
		}
	}
	return nil
}

// applyFavorite toggles the acting user's favorite for the photo when requested.
func applyFavorite(ctx context.Context, tx pgx.Tx, uid, actorUID string, ops Operations) error {
	if ops.Favorite == nil {
		return nil
	}
	query := removeFavoriteSQL
	if *ops.Favorite {
		query = addFavoriteSQL
	}
	if _, err := tx.Exec(ctx, query, actorUID, uid); err != nil {
		return fmt.Errorf("bulk: setting favorite for photo %s: %w", uid, err)
	}
	return nil
}

// applyRating writes the acting user's star rating and/or pick/reject flag for the
// photo when requested, then prunes the row should it have fallen back to all
// defaults, keeping user_ratings sparse. Values are validated by the API layer.
func applyRating(ctx context.Context, tx pgx.Tx, uid, actorUID string, ops Operations) error {
	if ops.Rating == nil && ops.Flag == nil {
		return nil
	}
	if ops.Rating != nil {
		if _, err := tx.Exec(ctx, setRatingSQL, actorUID, uid, *ops.Rating); err != nil {
			return fmt.Errorf("bulk: setting rating for photo %s: %w", uid, err)
		}
	}
	if ops.Flag != nil {
		if _, err := tx.Exec(ctx, setFlagSQL, actorUID, uid, *ops.Flag); err != nil {
			return fmt.Errorf("bulk: setting flag for photo %s: %w", uid, err)
		}
	}
	if _, err := tx.Exec(ctx, pruneRatingSQL, actorUID, uid); err != nil {
		return fmt.Errorf("bulk: pruning rating for photo %s: %w", uid, err)
	}
	return nil
}

// existsRow runs an EXISTS query bound to a single string argument.
func existsRow(ctx context.Context, tx pgx.Tx, query, arg string) (bool, error) {
	var ok bool
	if err := tx.QueryRow(ctx, query, arg).Scan(&ok); err != nil {
		return false, fmt.Errorf("bulk: existence check: %w", err)
	}
	return ok, nil
}

// photoExists reports whether a photo with the given UID exists.
func photoExists(ctx context.Context, tx pgx.Tx, uid string) (bool, error) {
	return existsRow(ctx, tx, "SELECT EXISTS(SELECT 1 FROM photos WHERE uid = $1)", uid)
}

// writeAudit appends the bulk change to the audit log within the open
// transaction, so the record commits atomically with the mutations.
func writeAudit(
	ctx context.Context, tx pgx.Tx, actorUID string, photoUIDs []string, ops Operations, counts Counts,
) error {
	entry := audit.Entry{
		ActorUID:   actorUID,
		Action:     audit.ActionPhotosBulk,
		TargetType: "photos",
		Details: map[string]any{
			"photo_uids": photoUIDs,
			"operations": ops.Summary(),
			"counts": map[string]any{
				"total":   counts.Total,
				"updated": counts.Updated,
				"skipped": counts.Skipped,
				"errored": counts.Errored,
			},
		},
	}
	if err := audit.Write(ctx, tx, entry); err != nil {
		return fmt.Errorf("bulk: writing audit entry: %w", err)
	}
	return nil
}
