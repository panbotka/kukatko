// Package dupmerge resolves a group of near-duplicate photos by merging the
// redundant copies into a chosen keeper and archiving them, all in one
// transaction, so nothing organizational is thrown away. The keeper inherits the
// union of every album, label and tagged person carried by any copy in the
// group, and any scalar field it is missing (title, description, and the acting
// user's rating, favorite and flag) is filled from a copy that has one — an
// existing keeper value is never overwritten. Only then are the redundant copies
// soft-archived (their originals are retained until a later purge) and the whole
// operation recorded in the audit trail.
//
// # Why raw SQL against one transaction
//
// Like internal/bulk, the merge issues the table SQL directly against a single
// pgx.Tx rather than calling the organize/people/photos stores. Those stores'
// audited methods each open and commit their own transaction, so they cannot be
// composed into the single atomic unit this operation requires. The SQL here is
// deliberately the same idempotent upsert the stores use (INSERT ... ON CONFLICT
// DO NOTHING), and the set of associations to add is computed as "what the copies
// have that the keeper lacks", so re-running on an already-resolved group is a
// safe no-op: the copies are archived, the keeper already carries everything, the
// plan comes back empty and nothing is written.
package dupmerge

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Sentinel errors returned by the service for a malformed request, so callers
// (the HTTP layer) can map them to a 4xx before any change is attempted.
var (
	// ErrNoKeeper indicates the request did not name a keeper.
	ErrNoKeeper = errors.New("dupmerge: keeper uid is required")
	// ErrTooFewMembers indicates the group had fewer than two members, so there
	// is nothing to merge.
	ErrTooFewMembers = errors.New("dupmerge: a group needs at least two members")
	// ErrKeeperNotInGroup indicates the keeper uid is not among the member uids.
	ErrKeeperNotInGroup = errors.New("dupmerge: keeper is not a member of the group")
	// ErrKeeperNotFound indicates the keeper photo does not exist.
	ErrKeeperNotFound = errors.New("dupmerge: keeper photo not found")
)

// Input identifies a duplicate group to resolve and which member to keep.
type Input struct {
	// KeeperUID is the member to keep; every other member is merged into it and
	// then archived.
	KeeperUID string
	// MemberUIDs is the full group membership, including the keeper. Unknown or
	// already-archived copies are tolerated so a re-run stays a safe no-op.
	MemberUIDs []string
	// ActorUID is the acting user, used for the per-user scalar fields (favorite,
	// rating, flag) and recorded on the audit entry. An empty ActorUID disables
	// the per-user fills (there is no user to attribute them to).
	ActorUID string
}

// Result reports what a merge did — or, for a Preview, would do: how many
// album/label/person associations were added to the keeper, which scalar fields
// were filled, and how many copies were archived.
type Result struct {
	KeeperUID      string   `json:"keeper_uid"`
	AlbumsAdded    int      `json:"albums_added"`
	LabelsAdded    int      `json:"labels_added"`
	PeopleAdded    int      `json:"people_added"`
	MetadataFilled []string `json:"metadata_filled"`
	Archived       int      `json:"archived"`
	// DryRun is true when the result comes from Preview (nothing was changed).
	DryRun bool `json:"dry_run"`
}

// Service performs duplicate-group merges against a PostgreSQL pool.
type Service struct {
	pool *pgxpool.Pool
}

// NewService returns a Service backed by pool.
func NewService(pool *pgxpool.Pool) *Service {
	return &Service{pool: pool}
}

// Merge resolves the group in one transaction: the keeper inherits the union of
// albums, labels and people from the copies, its missing scalar fields are
// filled, the copies are archived and the operation is audited. An empty plan
// (nothing to add and nothing to archive, e.g. an already-resolved group) is a
// no-op that writes nothing. It returns a validation error (ErrNoKeeper,
// ErrTooFewMembers, ErrKeeperNotInGroup) or ErrKeeperNotFound before any change,
// and a wrapped database error on failure.
func (s *Service) Merge(ctx context.Context, in Input) (Result, error) {
	if err := validate(in); err != nil {
		return Result{}, err
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Result{}, fmt.Errorf("dupmerge: begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	p, err := buildPlan(ctx, tx, in)
	if err != nil {
		return Result{}, err
	}
	if p.isEmpty() {
		return p.result(in.KeeperUID, false), nil
	}
	if err := p.apply(ctx, tx, in); err != nil {
		return Result{}, err
	}
	if err := writeAudit(ctx, tx, in, p); err != nil {
		return Result{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Result{}, fmt.Errorf("dupmerge: commit transaction: %w", err)
	}
	return p.result(in.KeeperUID, false), nil
}

// Preview computes what Merge would do without changing anything: it builds the
// same plan inside a transaction that is always rolled back. It returns the same
// validation errors as Merge.
func (s *Service) Preview(ctx context.Context, in Input) (Result, error) {
	if err := validate(in); err != nil {
		return Result{}, err
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Result{}, fmt.Errorf("dupmerge: begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	p, err := buildPlan(ctx, tx, in)
	if err != nil {
		return Result{}, err
	}
	return p.result(in.KeeperUID, true), nil
}

// validate checks the request shape before a transaction is opened: a keeper is
// named, the group has at least two members, and the keeper is one of them.
func validate(in Input) error {
	if in.KeeperUID == "" {
		return ErrNoKeeper
	}
	if len(in.MemberUIDs) < 2 {
		return ErrTooFewMembers
	}
	if !slices.Contains(in.MemberUIDs, in.KeeperUID) {
		return ErrKeeperNotInGroup
	}
	return nil
}

// querier runs the read queries a plan is built from. Both *pgxpool.Pool and
// pgx.Tx satisfy it, so a plan can be gathered on the pool or inside a caller's
// transaction unchanged.
type querier interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}
