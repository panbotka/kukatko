package family

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/panbotka/kukatko/internal/audit"
)

// uniqueViolation is the PostgreSQL SQLSTATE for a unique-constraint violation.
const uniqueViolation = "23505"

// foreignKeyViolation is the PostgreSQL SQLSTATE for a foreign-key violation,
// which here always means a named subject does not exist.
const foreignKeyViolation = "23503"

// Store is the database access layer for families and their children. It owns no
// connection; it borrows the shared pgx pool supplied at construction.
type Store struct {
	pool *pgxpool.Pool
}

// NewStore returns a Store backed by pool. The pool stays owned by the caller.
func NewStore(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool}
}

// querier is the subset of pgx the read helpers need. Both *pgxpool.Pool and
// pgx.Tx satisfy it, so a read runs either on its own connection or inside a
// caller's transaction — which is what lets the write paths check their
// invariants against the rows they are about to change.
type querier interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// constraintOf reports whether err is a PostgreSQL unique-constraint violation
// and, if so, the name of the violated constraint or index.
func constraintOf(err error) (string, bool) {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == uniqueViolation {
		return pgErr.ConstraintName, true
	}
	return "", false
}

// isMissingSubject reports whether err is the foreign-key violation a write gets
// when one of the subjects it names does not exist.
func isMissingSubject(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == foreignKeyViolation
}

// mutateAudited opens a transaction, runs mutate, writes entry on the same
// transaction and commits, returning the row mutate produced. The mutation and
// its audit record therefore commit atomically, and any failure rolls both back —
// the durable-audit convention of internal/people and internal/photos.
//
// entry is taken by value and copied into the transaction as it was passed, so a
// caller that means to stamp entry.TargetUID must do it *before* calling: a stamp
// made inside mutate is lost and the audit row gets a NULL target.
func mutateAudited[T any](
	ctx context.Context, pool *pgxpool.Pool, entry audit.Entry, mutate func(tx pgx.Tx) (T, error),
) (T, error) {
	var zero T
	tx, err := pool.Begin(ctx)
	if err != nil {
		return zero, fmt.Errorf("family: begin audited transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	row, err := mutate(tx)
	if err != nil {
		return zero, err
	}
	if err := audit.Write(ctx, tx, entry); err != nil {
		return zero, fmt.Errorf("family: writing audit entry: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return zero, fmt.Errorf("family: commit audited transaction: %w", err)
	}
	return row, nil
}

// familyColumns is the canonical, ordered column list for family reads, matched
// by scanFamily.
const familyColumns = "uid, partner_a_uid, partner_b_uid, kind, from_year, to_year, note, created_at, updated_at"

// scanFamily reads one family row in familyColumns order, wrapping any scan error
// (including pgx.ErrNoRows, which callers translate to ErrFamilyNotFound).
func scanFamily(row pgx.Row) (Family, error) {
	var fam Family
	if err := row.Scan(
		&fam.UID, &fam.PartnerA, &fam.PartnerB, &fam.Kind, &fam.FromYear,
		&fam.ToYear, &fam.Note, &fam.CreatedAt, &fam.UpdatedAt,
	); err != nil {
		return Family{}, fmt.Errorf("family: scanning family: %w", err)
	}
	return fam, nil
}

// getFamilySQL reads one family by UID.
const getFamilySQL = "SELECT " + familyColumns + " FROM subject_families WHERE uid = $1"

// GetFamily returns the family with the given UID, or ErrFamilyNotFound.
func (s *Store) GetFamily(ctx context.Context, uid string) (Family, error) {
	return getFamily(ctx, s.pool, uid)
}

// getFamily reads one family through q, translating pgx.ErrNoRows into
// ErrFamilyNotFound.
func getFamily(ctx context.Context, q querier, uid string) (Family, error) {
	fam, err := scanFamily(q.QueryRow(ctx, getFamilySQL, uid))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Family{}, ErrFamilyNotFound
		}
		return Family{}, err
	}
	return fam, nil
}

// findFamilyByPairSQL reads the family of an exact stored pair. Both arguments
// are already normalised, and NULL is compared with IS NOT DISTINCT FROM so a
// lone parent's empty second column matches — the same "NULLs are equal here"
// rule the unique pair index applies with NULLS NOT DISTINCT.
const findFamilyByPairSQL = "SELECT " + familyColumns + ` FROM subject_families
WHERE partner_a_uid IS NOT DISTINCT FROM $1 AND partner_b_uid IS NOT DISTINCT FROM $2`

// findFamilyByPair returns the family stored for the normalised pair (a, b), or
// ErrFamilyNotFound when the couple has none yet.
func findFamilyByPair(ctx context.Context, q querier, a, b *string) (Family, error) {
	fam, err := scanFamily(q.QueryRow(ctx, findFamilyByPairSQL, a, b))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Family{}, ErrFamilyNotFound
		}
		return Family{}, err
	}
	return fam, nil
}

// childFamilySQL reads the family a subject is a child in, together with the kind
// of that membership. A subject is a child in at most one family
// (idx_subject_family_children_child), so this is a single row or none.
const childFamilySQL = `
SELECT f.uid, f.partner_a_uid, f.partner_b_uid, f.kind, f.from_year, f.to_year,
       f.note, f.created_at, f.updated_at, c.kind
FROM subject_family_children c
JOIN subject_families f ON f.uid = c.family_uid
WHERE c.child_uid = $1`

// childFamily returns the family subjectUID is a child in and how they belong to
// it, or ErrFamilyNotFound when they are nobody's recorded child.
func childFamily(ctx context.Context, q querier, subjectUID string) (Family, ChildKind, error) {
	var fam Family
	var kind ChildKind
	err := q.QueryRow(ctx, childFamilySQL, subjectUID).Scan(
		&fam.UID, &fam.PartnerA, &fam.PartnerB, &fam.Kind, &fam.FromYear,
		&fam.ToYear, &fam.Note, &fam.CreatedAt, &fam.UpdatedAt, &kind,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return Family{}, "", ErrFamilyNotFound
	}
	if err != nil {
		return Family{}, "", fmt.Errorf("family: reading child family of %s: %w", subjectUID, err)
	}
	return fam, kind, nil
}

// subjectExistsSQL reports whether a subject row exists.
const subjectExistsSQL = "SELECT 1 FROM subjects WHERE uid = $1"

// requireSubjects returns ErrSubjectNotFound unless every named subject exists.
// The foreign keys would refuse a missing subject anyway, but a sentinel the
// caller can map to a 404 is worth the extra round trip: an FK violation names a
// constraint, not a person.
func requireSubjects(ctx context.Context, q querier, uids ...string) error {
	for _, uid := range uids {
		var one int
		err := q.QueryRow(ctx, subjectExistsSQL, uid).Scan(&one)
		if errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("%w: %s", ErrSubjectNotFound, uid)
		}
		if err != nil {
			return fmt.Errorf("family: checking subject %s: %w", uid, err)
		}
	}
	return nil
}
