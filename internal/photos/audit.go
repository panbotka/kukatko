package photos

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/panbotka/kukatko/internal/audit"
)

// rowQuerier runs a query that returns a single row. Both *pgxpool.Pool and
// pgx.Tx satisfy it, so a mutation's SQL can run on the pool or inside a
// caller's transaction unchanged — the basis for the audited variants below,
// which run the mutation and its audit row in one transaction.
type rowQuerier interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// UpdateMetadataAudited applies the metadata update to the photo identified by
// uid and writes entry to the audit log in the same transaction, so the audit
// row commits atomically with the change and rolls back with it on failure (the
// durable-audit convention; see internal/audit). entry's TargetUID is set to
// uid. It returns the refreshed photo or ErrPhotoNotFound.
func (s *Store) UpdateMetadataAudited(
	ctx context.Context, uid string, m MetadataUpdate, entry audit.Entry,
) (Photo, error) {
	return s.mutateAudited(ctx, uid, entry, func(tx pgx.Tx) (Photo, error) {
		return updateMetadataRow(ctx, tx, uid, m)
	})
}

// ArchiveAudited archives (soft-deletes) the photo identified by uid and writes
// entry to the audit log in the same transaction. See UpdateMetadataAudited for
// the atomicity guarantee. A stacked photo leaves its stack first (see
// LeaveStackTx), in the same transaction, so its still-live siblings are not
// hidden by the (stack_uid IS NULL OR stack_primary) visibility gate. It returns
// the refreshed photo or ErrPhotoNotFound.
func (s *Store) ArchiveAudited(ctx context.Context, uid string, entry audit.Entry) (Photo, error) {
	return s.mutateAudited(ctx, uid, entry, func(tx pgx.Tx) (Photo, error) {
		if err := LeaveStackTx(ctx, tx, uid); err != nil {
			return Photo{}, err
		}
		return setArchivedRow(ctx, tx, uid, true)
	})
}

// UnarchiveAudited restores the photo identified by uid from the trash and
// writes entry to the audit log in the same transaction. See
// UpdateMetadataAudited for the atomicity guarantee. It returns the refreshed
// photo or ErrPhotoNotFound.
func (s *Store) UnarchiveAudited(ctx context.Context, uid string, entry audit.Entry) (Photo, error) {
	return s.mutateAudited(ctx, uid, entry, func(tx pgx.Tx) (Photo, error) {
		return setArchivedRow(ctx, tx, uid, false)
	})
}

// DeleteAudited permanently deletes the photo identified by uid (cascading its
// satellite rows) and writes entry to the audit log in the same transaction, so
// the row deletion and the record of who purged it commit atomically and roll
// back together on failure (the durable-audit convention; see internal/audit).
// entry's TargetUID defaults to uid. A stacked photo leaves its stack first (see
// LeaveStackTx), in the same transaction, so the purge does not strand its
// siblings in a primary-less stack that no default view shows and no re-stacking
// can reach. It returns ErrPhotoNotFound when no row matched, in which case
// nothing is deleted and no audit entry is written.
func (s *Store) DeleteAudited(ctx context.Context, uid string, entry audit.Entry) error {
	_, err := s.mutateAudited(ctx, uid, entry, func(tx pgx.Tx) (Photo, error) {
		if err := LeaveStackTx(ctx, tx, uid); err != nil {
			return Photo{}, err
		}
		tag, err := tx.Exec(ctx, "DELETE FROM photos WHERE uid = $1", uid)
		if err != nil {
			return Photo{}, fmt.Errorf("photos: deleting photo: %w", err)
		}
		if tag.RowsAffected() == 0 {
			return Photo{}, ErrPhotoNotFound
		}
		return Photo{}, nil
	})
	return err
}

// mutateAudited opens a transaction, runs mutate, writes entry (with TargetUID
// defaulted to uid) on the same transaction and commits, so the mutation and its
// audit row are atomic. If mutate or the audit write fails the transaction rolls
// back and neither change persists. It returns the photo mutate produced.
func (s *Store) mutateAudited(
	ctx context.Context, uid string, entry audit.Entry, mutate func(tx pgx.Tx) (Photo, error),
) (Photo, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Photo{}, fmt.Errorf("photos: begin audited transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	photo, err := mutate(tx)
	if err != nil {
		return Photo{}, err
	}
	if entry.TargetUID == "" {
		entry.TargetUID = uid
	}
	if err := audit.Write(ctx, tx, entry); err != nil {
		return Photo{}, fmt.Errorf("photos: writing audit entry: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return Photo{}, fmt.Errorf("photos: commit audited transaction: %w", err)
	}
	return photo, nil
}
