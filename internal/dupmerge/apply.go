package dupmerge

import (
	"context"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/panbotka/kukatko/internal/audit"
)

// Write statements. Each mirrors the idempotent upsert the organize store uses,
// so applying a plan is safe to repeat: an association the keeper already carries
// is left untouched and an already-archived copy is not touched again.
const (
	addAlbumSQL = `INSERT INTO album_photos (album_uid, photo_uid) ` +
		`VALUES ($1, $2) ON CONFLICT (album_uid, photo_uid) DO NOTHING`
	addLabelSQL = `INSERT INTO photo_labels (photo_uid, label_uid, source, uncertainty) ` +
		`VALUES ($1, $2, 'manual', 0) ON CONFLICT (photo_uid, label_uid) DO NOTHING`
	// addMarkerSQL records "this person is present" on the keeper. A face marker's
	// box is pixel-specific to the copy it came from and cannot be transferred, so
	// the marker is a box-less ('label' type, zero box via column defaults) tag
	// whose only job is to associate the subject with the keeper. No faces row
	// references the new marker, so the denormalised faces cache needs no update.
	addMarkerSQL   = `INSERT INTO markers (uid, photo_uid, subject_uid, type) VALUES ($1, $2, $3, 'label')`
	addFavoriteSQL = `INSERT INTO user_favorites (user_uid, photo_uid) ` +
		`VALUES ($1, $2) ON CONFLICT (user_uid, photo_uid) DO NOTHING`
	setRatingSQL = `INSERT INTO user_ratings (user_uid, photo_uid, rating) VALUES ($1, $2, $3) ` +
		`ON CONFLICT (user_uid, photo_uid) DO UPDATE SET rating = EXCLUDED.rating, updated_at = now()`
	setFlagSQL = `INSERT INTO user_ratings (user_uid, photo_uid, flag) VALUES ($1, $2, $3) ` +
		`ON CONFLICT (user_uid, photo_uid) DO UPDATE SET flag = EXCLUDED.flag, updated_at = now()`
	archiveSQL = `UPDATE photos SET archived_at = now(), updated_at = now() ` +
		`WHERE uid = $1 AND archived_at IS NULL`
)

// apply writes the whole plan to the keeper through tx: it adds the album, label
// and people associations, fills the scalar gaps and archives the copies. It is
// only called for a non-empty plan and never commits — the caller owns the
// transaction.
func (p plan) apply(ctx context.Context, tx pgx.Tx, in Input) error {
	if err := addAlbums(ctx, tx, in.KeeperUID, p.albumsToAdd); err != nil {
		return err
	}
	if err := addLabels(ctx, tx, in.KeeperUID, p.labelsToAdd); err != nil {
		return err
	}
	if err := addPeople(ctx, tx, in.KeeperUID, p.subjectsToAdd); err != nil {
		return err
	}
	if err := fillScalars(ctx, tx, in, p.fill); err != nil {
		return err
	}
	return archiveCopies(ctx, tx, p.archiveUIDs)
}

// addAlbums makes the keeper a member of each album, idempotently.
func addAlbums(ctx context.Context, tx pgx.Tx, keeperUID string, albums []string) error {
	for _, albumUID := range albums {
		if _, err := tx.Exec(ctx, addAlbumSQL, albumUID, keeperUID); err != nil {
			return fmt.Errorf("dupmerge: adding keeper %s to album %s: %w", keeperUID, albumUID, err)
		}
	}
	return nil
}

// addLabels attaches each label to the keeper as a manual label, idempotently.
func addLabels(ctx context.Context, tx pgx.Tx, keeperUID string, labels []string) error {
	for _, labelUID := range labels {
		if _, err := tx.Exec(ctx, addLabelSQL, keeperUID, labelUID); err != nil {
			return fmt.Errorf("dupmerge: attaching label %s to keeper %s: %w", labelUID, keeperUID, err)
		}
	}
	return nil
}

// addPeople tags the keeper with each subject by inserting a box-less marker. The
// plan already excludes subjects the keeper carries, so no duplicate tag is made.
func addPeople(ctx context.Context, tx pgx.Tx, keeperUID string, subjects []string) error {
	for _, subjectUID := range subjects {
		uid, err := newMarkerUID()
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, addMarkerSQL, uid, keeperUID, subjectUID); err != nil {
			return fmt.Errorf("dupmerge: tagging keeper %s with subject %s: %w", keeperUID, subjectUID, err)
		}
	}
	return nil
}

// fillScalars writes the gap-filling scalar values onto the keeper: the photo
// columns first, then the acting user's per-user fields.
func fillScalars(ctx context.Context, tx pgx.Tx, in Input, f scalarFill) error {
	if err := fillPhotoColumns(ctx, tx, in.KeeperUID, f); err != nil {
		return err
	}
	return fillUserScalars(ctx, tx, in, f)
}

// fillPhotoColumns updates the keeper's title and/or description when the fill
// supplies them, in one statement. It is a no-op when neither is set.
func fillPhotoColumns(ctx context.Context, tx pgx.Tx, keeperUID string, f scalarFill) error {
	set := []string{}
	args := []any{}
	if f.title != nil {
		args = append(args, *f.title)
		set = append(set, fmt.Sprintf("title = $%d", len(args)))
	}
	if f.description != nil {
		args = append(args, *f.description)
		set = append(set, fmt.Sprintf("description = $%d", len(args)))
	}
	if len(set) == 0 {
		return nil
	}
	set = append(set, "updated_at = now()")
	args = append(args, keeperUID)
	sql := "UPDATE photos SET " + strings.Join(set, ", ") + fmt.Sprintf(" WHERE uid = $%d", len(args))
	if _, err := tx.Exec(ctx, sql, args...); err != nil {
		return fmt.Errorf("dupmerge: filling keeper metadata: %w", err)
	}
	return nil
}

// fillUserScalars writes the acting user's favorite, rating and flag onto the
// keeper when the fill supplies them. It is a no-op without an actor.
func fillUserScalars(ctx context.Context, tx pgx.Tx, in Input, f scalarFill) error {
	if in.ActorUID == "" {
		return nil
	}
	if f.favorite {
		if _, err := tx.Exec(ctx, addFavoriteSQL, in.ActorUID, in.KeeperUID); err != nil {
			return fmt.Errorf("dupmerge: favoriting keeper %s: %w", in.KeeperUID, err)
		}
	}
	if f.rating != nil {
		if _, err := tx.Exec(ctx, setRatingSQL, in.ActorUID, in.KeeperUID, *f.rating); err != nil {
			return fmt.Errorf("dupmerge: rating keeper %s: %w", in.KeeperUID, err)
		}
	}
	if f.flag != nil {
		if _, err := tx.Exec(ctx, setFlagSQL, in.ActorUID, in.KeeperUID, *f.flag); err != nil {
			return fmt.Errorf("dupmerge: flagging keeper %s: %w", in.KeeperUID, err)
		}
	}
	return nil
}

// archiveCopies soft-archives each still-active copy. The archived_at IS NULL
// guard makes re-archiving a no-op, keeping the operation idempotent.
func archiveCopies(ctx context.Context, tx pgx.Tx, copies []string) error {
	for _, uid := range copies {
		if _, err := tx.Exec(ctx, archiveSQL, uid); err != nil {
			return fmt.Errorf("dupmerge: archiving copy %s: %w", uid, err)
		}
	}
	return nil
}

// writeAudit records the merge in the audit trail within the open transaction, so
// the record commits atomically with the mutations. The keeper is the target; the
// archived copies and the counts of what moved are in the details.
func writeAudit(ctx context.Context, tx pgx.Tx, in Input, p plan) error {
	entry := audit.Entry{
		ActorUID:   in.ActorUID,
		Action:     audit.ActionPhotosMerge,
		TargetType: "photos",
		TargetUID:  in.KeeperUID,
		Details: map[string]any{
			"keeper_uid":      in.KeeperUID,
			"archived_uids":   p.archiveUIDs,
			"albums_added":    len(p.albumsToAdd),
			"labels_added":    len(p.labelsToAdd),
			"people_added":    len(p.subjectsToAdd),
			"metadata_filled": p.fill.filledFields(),
		},
	}
	if err := audit.Write(ctx, tx, entry); err != nil {
		return fmt.Errorf("dupmerge: writing audit entry: %w", err)
	}
	return nil
}
