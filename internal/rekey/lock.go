package rekey

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/panbotka/kukatko/internal/storekeys"
)

// The photo lock keeps a storage move and everything else that writes or
// deletes a photo's objects out of each other's way, one photo at a time. It is
// a transaction-scoped Postgres advisory lock keyed by the photo's uid, so it is
// shared by every process on the database and is released by the end of its
// transaction whatever happens to the caller — a crash, a cancelled context, a
// dropped connection.
//
// The move takes it exclusively (lockPhoto) from just before it commits the
// photo's row under the new prefix until the photo's derived objects are copied
// and the root ones deleted: everything it snapshots of the photo is then
// consistent with what it commits. A publisher of a derived object (a thumbnail,
// a streaming rendition, a sidecar) takes it shared for the re-read of the
// photo's key and the upload that follows (Gate.Publish), so its upload lands
// either before the move copies — at the root, from where the move takes it
// along — or after the move is through, under the new prefix; never in between,
// where the move would copy the stale root object over the fresh one or leave
// the fresh one at a root it has already emptied. A trash purge takes it shared
// too (Gate.Purge), so a purge and the move never interleave on one photo.
//
// The publishers never wait for the lock: one that finds it taken gets
// storekeys.ErrMoving and defers, so a busy job never holds a pooled connection
// while it waits — the move, which waits, needs the pool to finish. A purge,
// which runs one photo at a time, waits.

// photoLockKey is the advisory-lock key of the photo whose uid is $1.
const photoLockKey = `hashtextextended('rekey_photo:' || $1, 0)`

// lockPhotoSQL takes the photo's lock exclusively, waiting for it.
const lockPhotoSQL = `SELECT pg_advisory_xact_lock(` + photoLockKey + `)`

// tryHoldPhotoSQL takes the photo's lock shared when nobody holds it
// exclusively, reporting whether it did; it never waits.
const tryHoldPhotoSQL = `SELECT pg_try_advisory_xact_lock_shared(` + photoLockKey + `)`

// waitHoldPhotoSQL takes the photo's lock shared, waiting while the move holds
// it.
const waitHoldPhotoSQL = `SELECT pg_advisory_xact_lock_shared(` + photoLockKey + `)`

// heldPhotoSQL reads, under the photo's lock, the key of the photo's original
// and whether a storage move has the photo pending — committed under the new
// prefix by a step that crashed before it moved the derived objects, which the
// next step will do.
const heldPhotoSQL = `SELECT p.file_path, EXISTS (SELECT 1 FROM storage_rekey_runs r
	WHERE r.project_uid = p.project_id AND r.pending_photo_uid = p.uid)
FROM photos p WHERE p.uid = $1 AND p.project_id = $2`

// lockPhoto runs fn while holding the lock of the photo with the given uid
// exclusively, waiting for whoever holds it shared to finish first. fn's error
// is returned as it is. fn runs its own statements on the pool, never on the
// lock's transaction, which only holds the lock.
func (s *Service) lockPhoto(ctx context.Context, photoUID string, fn func() error) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("rekey: opening the lock of photo %s: %w", photoUID, err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, lockPhotoSQL, photoUID); err != nil {
		return fmt.Errorf("rekey: locking photo %s: %w", photoUID, err)
	}
	return fn()
}

// Publish runs publish while keeping a storage move off the photo photoUID of
// project projectUID: it takes the photo's lock shared without waiting, re-reads
// the key of the photo's original under it and hands that key to publish (found
// false when the project has no such photo — an upload whose row is not
// committed yet), holding the lock until publish returns. publish's error is
// returned as it is.
//
// While a move holds the photo — it is copying the photo's objects right now,
// or committed the photo before a crash and has yet to finish it — publish is
// not called and Publish returns an error wrapping storekeys.ErrMoving; the
// caller defers its work for a moment. publish should do nothing slow but the
// upload itself: a move waiting for the photo waits for it.
func (g *Gate) Publish(
	ctx context.Context, projectUID, photoUID string, publish func(filePath string, found bool) error,
) error {
	return g.hold(ctx, projectUID, photoUID, false, publish)
}

// Purge runs purge while keeping a storage move off the photo photoUID of
// project projectUID, for a deletion of the photo's objects and row: it takes
// the photo's lock shared, waiting while a move is copying the photo, and holds
// it until purge returns. A photo a crashed move committed and has yet to finish
// is refused with an error wrapping storekeys.ErrMoving: its objects are under
// both prefixes until the move's next step, and a purge now would leave one of
// the two behind. purge's error is returned as it is.
func (g *Gate) Purge(ctx context.Context, projectUID, photoUID string, purge func() error) error {
	return g.hold(ctx, projectUID, photoUID, true, func(string, bool) error { return purge() })
}

// hold takes the photo's lock shared — waiting for it with wait set, refusing
// with storekeys.ErrMoving when it is taken otherwise — reads the photo under
// it, and calls fn unless a move holds the photo.
func (g *Gate) hold(
	ctx context.Context, projectUID, photoUID string, wait bool, fn func(filePath string, found bool) error,
) error {
	tx, err := g.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("rekey: opening the lock of photo %s: %w", photoUID, err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := takeShared(ctx, tx, photoUID, wait); err != nil {
		return err
	}
	var (
		filePath string
		pending  bool
		found    = true
	)
	err = tx.QueryRow(ctx, heldPhotoSQL, photoUID, projectUID).Scan(&filePath, &pending)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		found = false
	case err != nil:
		return fmt.Errorf("rekey: reading photo %s under its lock: %w", photoUID, err)
	case pending:
		return fmt.Errorf("%w: %s is pending in a storage move", storekeys.ErrMoving, photoUID)
	}
	return fn(filePath, found)
}

// takeShared takes the photo's lock shared on tx: waiting for it with wait set,
// otherwise returning an error wrapping storekeys.ErrMoving when the move holds
// it.
func takeShared(ctx context.Context, tx pgx.Tx, photoUID string, wait bool) error {
	if wait {
		if _, err := tx.Exec(ctx, waitHoldPhotoSQL, photoUID); err != nil {
			return fmt.Errorf("rekey: locking photo %s: %w", photoUID, err)
		}
		return nil
	}
	var taken bool
	if err := tx.QueryRow(ctx, tryHoldPhotoSQL, photoUID).Scan(&taken); err != nil {
		return fmt.Errorf("rekey: locking photo %s: %w", photoUID, err)
	}
	if !taken {
		return fmt.Errorf("%w: %s", storekeys.ErrMoving, photoUID)
	}
	return nil
}
