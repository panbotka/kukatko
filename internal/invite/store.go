package invite

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/panbotka/kukatko/internal/audit"
	"github.com/panbotka/kukatko/internal/uploadlink"
)

// codeAttempts bounds how often Create redraws a code that collides with an
// existing one; the bound only keeps a broken random source from looping.
const codeAttempts = 5

// day is one day of validity.
const day = 24 * time.Hour

// Store is the database access layer for invitation links. It borrows the
// shared pgx pool supplied at construction.
type Store struct {
	pool *pgxpool.Pool
	now  func() time.Time
}

// NewStore returns a Store backed by pool, using time.Now as its clock. The
// pool stays owned by the caller.
func NewStore(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool, now: time.Now}
}

// selectInviteSQL reads invites with their creator's name and their project, so
// one query yields a complete Invite.
const selectInviteSQL = `
SELECT i.uid, i.role, i.expires_at, i.max_uses, i.uses, i.revoked_at, i.created_by,
       COALESCE(NULLIF(u.display_name, ''), u.username, ''), i.created_at, i.code,
       p.uid, p.slug, p.name, p.active
FROM project_invites i
JOIN projects p ON p.uid = i.project_uid
LEFT JOIN users u ON u.uid = i.created_by`

// scanInvite reads one selectInviteSQL row, mapping pgx.ErrNoRows to notFound.
func scanInvite(row pgx.Row, notFound error) (Invite, error) {
	var inv Invite
	err := row.Scan(&inv.UID, &inv.Role, &inv.ExpiresAt, &inv.MaxUses, &inv.Uses, &inv.RevokedAt,
		&inv.CreatedBy, &inv.CreatorName, &inv.CreatedAt, &inv.Code,
		&inv.ProjectUID, &inv.ProjectSlug, &inv.ProjectName, &inv.projectActive)
	if errors.Is(err, pgx.ErrNoRows) {
		return Invite{}, notFound
	}
	if err != nil {
		return Invite{}, fmt.Errorf("invite: scanning invite: %w", err)
	}
	return inv, nil
}

// Get returns the invite with uid in the project projectUID, or ErrNotFound —
// also for another project's invite, answered exactly like a missing one. An
// empty projectUID is ErrNoProject.
func (s *Store) Get(ctx context.Context, projectUID, uid string) (Invite, error) {
	if projectUID == "" {
		return Invite{}, ErrNoProject
	}
	return scanInvite(s.pool.QueryRow(ctx, selectInviteSQL+" WHERE i.uid = $1 AND i.project_uid = $2",
		uid, projectUID), ErrNotFound)
}

// List returns every invite of the project, newest first, whatever its state.
// No invites yield an empty, non-nil slice; an empty projectUID is ErrNoProject.
func (s *Store) List(ctx context.Context, projectUID string) ([]Invite, error) {
	if projectUID == "" {
		return nil, ErrNoProject
	}
	rows, err := s.pool.Query(ctx,
		selectInviteSQL+" WHERE i.project_uid = $1 ORDER BY i.created_at DESC, i.uid", projectUID)
	if err != nil {
		return nil, fmt.Errorf("invite: listing invites: %w", err)
	}
	defer rows.Close()
	invites := []Invite{}
	for rows.Next() {
		inv, err := scanInvite(rows, ErrNotFound)
		if err != nil {
			return nil, err
		}
		invites = append(invites, inv)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("invite: iterating invites: %w", err)
	}
	return invites, nil
}

// Resolve returns the live invite code names, in whichever project, or
// ErrInvalid — one answer for a malformed, unknown, expired, revoked or used-up
// code and for an invite of a deactivated project. It is the lookup of the
// public /invite/{code} page.
func (s *Store) Resolve(ctx context.Context, code string) (Invite, error) {
	if !uploadlink.ValidCode(code) {
		return Invite{}, ErrInvalid
	}
	inv, err := scanInvite(s.pool.QueryRow(ctx, selectInviteSQL+" WHERE i.code_hash = $1",
		uploadlink.HashSecret(code)), ErrInvalid)
	if err != nil {
		return Invite{}, err
	}
	if !inv.openAt(s.now()) || inv.usedUp() {
		return Invite{}, ErrInvalid
	}
	return inv, nil
}

// insertInviteSQL inserts an invite unless its code hash collides, in which case
// it returns no row and Create draws another code.
const insertInviteSQL = `
INSERT INTO project_invites (uid, project_uid, code_hash, code, role, expires_at, max_uses, created_by)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
ON CONFLICT (code_hash) DO NOTHING
RETURNING uid`

// Create stores the invite in describes in the project projectUID and returns
// it, its readable code in Invite.Code. The invite and entry (stamped with the
// new invite as its target, ActionProjectInviteCreate, the project, the role,
// the expiry and the cap) commit together. It returns ErrNoProject for an empty
// projectUID and the Validate errors for input it will not store.
func (s *Store) Create(ctx context.Context, projectUID string, in NewInvite, meta audit.Meta) (Invite, error) {
	if projectUID == "" {
		return Invite{}, ErrNoProject
	}
	if err := in.Validate(); err != nil {
		return Invite{}, err
	}
	uid, err := newInviteUID()
	if err != nil {
		return Invite{}, err
	}
	expiresAt := s.now().Add(time.Duration(in.days()) * day)
	err = s.inTx(ctx, func(tx pgx.Tx) error {
		if err := insertInvite(ctx, tx, projectUID, uid, in, expiresAt); err != nil {
			return err
		}
		entry := meta.Entry(audit.ActionProjectInviteCreate, TargetType, uid, map[string]any{
			"project_uid": projectUID, "role": string(in.Role), "expires_at": expiresAt.UTC(),
			"max_uses": in.MaxUses,
		})
		entry.ProjectUID = projectUID
		return audit.Write(ctx, tx, entry)
	})
	if err != nil {
		return Invite{}, err
	}
	return s.Get(ctx, projectUID, uid)
}

// insertInvite inserts the invite row on tx under a fresh code, redrawing the
// code on the (astronomically unlikely) collision.
func insertInvite(ctx context.Context, tx pgx.Tx, projectUID, uid string, in NewInvite, expiresAt time.Time) error {
	for range codeAttempts {
		code, err := uploadlink.NewCode()
		if err != nil {
			return fmt.Errorf("invite: drawing a code: %w", err)
		}
		var got string
		err = tx.QueryRow(ctx, insertInviteSQL, uid, projectUID, uploadlink.HashSecret(code), code,
			string(in.Role), expiresAt, in.MaxUses, in.CreatedBy).Scan(&got)
		if errors.Is(err, pgx.ErrNoRows) {
			continue
		}
		if err != nil {
			return fmt.Errorf("invite: inserting invite: %w", err)
		}
		return nil
	}
	return errors.New("invite: could not draw a unique code")
}

// Revoke revokes the project's invite with uid for good and returns it.
// Revoking an already revoked invite changes nothing and writes no audit entry;
// otherwise the change and entry (ActionProjectInviteRevoke) commit together. It
// returns ErrNotFound for an unknown invite or one of another project.
func (s *Store) Revoke(ctx context.Context, projectUID, uid string, meta audit.Meta) (Invite, error) {
	if projectUID == "" {
		return Invite{}, ErrNoProject
	}
	err := s.inTx(ctx, func(tx pgx.Tx) error {
		var (
			revoked *time.Time
			role    string
		)
		err := tx.QueryRow(ctx,
			"SELECT revoked_at, role FROM project_invites WHERE uid = $1 AND project_uid = $2 FOR UPDATE",
			uid, projectUID).Scan(&revoked, &role)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return fmt.Errorf("invite: locking invite: %w", err)
		}
		if revoked != nil {
			return nil
		}
		if _, err := tx.Exec(ctx, "UPDATE project_invites SET revoked_at = $2 WHERE uid = $1",
			uid, s.now()); err != nil {
			return fmt.Errorf("invite: revoking invite: %w", err)
		}
		entry := meta.Entry(audit.ActionProjectInviteRevoke, TargetType, uid, map[string]any{
			"project_uid": projectUID, "role": role,
		})
		entry.ProjectUID = projectUID
		return audit.Write(ctx, tx, entry)
	})
	if err != nil {
		return Invite{}, err
	}
	return s.Get(ctx, projectUID, uid)
}

// inTx runs fn in a transaction and commits it, rolling back when fn fails.
func (s *Store) inTx(ctx context.Context, fn func(tx pgx.Tx) error) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("invite: beginning transaction: %w", err)
	}
	// After a commit the rollback is a no-op; on every early return it undoes
	// whatever fn wrote.
	defer func() { _ = tx.Rollback(ctx) }()
	if err := fn(tx); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("invite: committing transaction: %w", err)
	}
	return nil
}
