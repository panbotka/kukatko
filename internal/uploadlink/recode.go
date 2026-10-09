package uploadlink

import (
	"context"
	"crypto/subtle"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/panbotka/kukatko/internal/audit"
)

// lockedCode is what RestoreCode and RotateCode read of a link under its row
// lock: the hash every lookup goes through and the readable code, nil while it
// is unknown.
type lockedCode struct {
	hash string
	code *string
}

// lockLiveCode locks the link with uid on tx and returns its code columns. It
// returns ErrNotFound for an unknown link and ErrRevoked for a revoked one — the
// same refusal Extend gives. An expired link is not refused: it can be extended
// back to life, and its address is worth having (or replacing) before that.
func lockLiveCode(ctx context.Context, tx pgx.Tx, uid string) (lockedCode, error) {
	var (
		locked  lockedCode
		revoked *time.Time
	)
	err := tx.QueryRow(ctx, "SELECT code_hash, code, revoked_at FROM upload_links WHERE uid = $1 FOR UPDATE",
		uid).Scan(&locked.hash, &locked.code, &revoked)
	if errors.Is(err, pgx.ErrNoRows) {
		return lockedCode{}, ErrNotFound
	}
	if err != nil {
		return lockedCode{}, fmt.Errorf("uploadlink: locking link: %w", err)
	}
	if revoked != nil {
		return lockedCode{}, ErrRevoked
	}
	return locked, nil
}

// RestoreCode makes the code of a link created before migration 0091 readable
// again: code is stored only when it hashes to the link's code_hash, so nothing
// but the original code can ever be stored and the link's URL stays exactly what
// it was. The change and entry commit together and return the updated link.
//
// It returns ErrNotFound for an unknown link, ErrRevoked for a revoked one and
// ErrCodeMismatch (with nothing changed) for any other code. Restoring a code
// that is already readable changes nothing and writes no audit entry.
func (s *Store) RestoreCode(ctx context.Context, uid, code string, entry audit.Entry) (Link, error) {
	err := s.inTx(ctx, func(tx pgx.Tx) error {
		locked, err := lockLiveCode(ctx, tx, uid)
		if err != nil {
			return err
		}
		if !ValidCode(code) || subtle.ConstantTimeCompare([]byte(HashSecret(code)), []byte(locked.hash)) != 1 {
			return ErrCodeMismatch
		}
		if locked.code != nil {
			return nil
		}
		if _, err := tx.Exec(ctx, "UPDATE upload_links SET code = $2 WHERE uid = $1", uid, code); err != nil {
			return fmt.Errorf("uploadlink: restoring code: %w", err)
		}
		entry.TargetType, entry.TargetUID = TargetType, uid
		return audit.Write(ctx, tx, entry)
	})
	if err != nil {
		return Link{}, err
	}
	return s.Get(ctx, uid)
}

// rotateCodeSQL replaces a link's code unless the fresh one collides with
// another link's, in which case it updates no row and RotateCode draws again.
const rotateCodeSQL = `
UPDATE upload_links SET code_hash = $2, code = $3
WHERE uid = $1 AND NOT EXISTS (SELECT 1 FROM upload_links o WHERE o.code_hash = $2)`

// RotateCode gives the link with uid a fresh code and returns the updated link,
// whose Code is the new one. The old URL stops working at once — that is the
// point, for a link that leaked — and nothing else about the link changes. The
// change and entry commit together. It returns ErrNotFound for an unknown link
// and ErrRevoked for a revoked one.
func (s *Store) RotateCode(ctx context.Context, uid string, entry audit.Entry) (Link, error) {
	err := s.inTx(ctx, func(tx pgx.Tx) error {
		if _, err := lockLiveCode(ctx, tx, uid); err != nil {
			return err
		}
		if err := replaceCode(ctx, tx, uid); err != nil {
			return err
		}
		entry.TargetType, entry.TargetUID = TargetType, uid
		return audit.Write(ctx, tx, entry)
	})
	if err != nil {
		return Link{}, err
	}
	return s.Get(ctx, uid)
}

// replaceCode stores a fresh code (and its hash) on the link with uid on tx,
// redrawing on the astronomically unlikely collision with another link.
func replaceCode(ctx context.Context, tx pgx.Tx, uid string) error {
	for range codeAttempts {
		code, err := NewCode()
		if err != nil {
			return err
		}
		tag, err := tx.Exec(ctx, rotateCodeSQL, uid, HashSecret(code), code)
		if err != nil {
			return fmt.Errorf("uploadlink: replacing code: %w", err)
		}
		if tag.RowsAffected() == 1 {
			return nil
		}
	}
	return errors.New("uploadlink: could not draw a unique code")
}
