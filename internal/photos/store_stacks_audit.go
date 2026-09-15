package photos

import (
	"context"
	"fmt"
	"maps"

	"github.com/jackc/pgx/v5"

	"github.com/panbotka/kukatko/internal/audit"
)

// The audited stack mutations. Each one is the plain mutation of store_stacks.go
// plus its audit row, written on the very same transaction, so the record and
// the change commit together and roll back together (the durable-audit
// convention; see internal/audit). Grouping photos, changing which variant a
// stack is shown as and ungrouping all change what the library shows, so none of
// them may leave the trail silent.
//
// The store fills the details in, not the caller: the stack uid, the primary a
// promotion replaced and the members a dissolved stack had are facts only the
// transaction knows, and they are exactly what makes an entry enough to
// reconstruct the change from.

// CreateStackAudited groups memberUIDs into one new stack whose primary is
// primaryUID and writes entry in the same transaction, returning the fresh
// stack_uid. entry's TargetUID defaults to the primary and its details are
// filled with the stack uid, the primary and every member. It behaves like
// CreateStack otherwise — ErrStackTooSmall for fewer than two distinct members,
// ErrPhotoNotFound when one is missing or archived — and each of those rolls
// back, writing no audit row.
func (s *Store) CreateStackAudited(
	ctx context.Context, primaryUID string, memberUIDs []string, entry audit.Entry,
) (string, error) {
	plan, err := StackPlan{PrimaryUID: primaryUID, MemberUIDs: memberUIDs}.normalize()
	if err != nil {
		return "", err
	}
	stackUID, err := newStackUID()
	if err != nil {
		return "", err
	}
	err = s.inTx(ctx, func(tx pgx.Tx) error {
		if err := applyNewStackTx(ctx, tx, stackUID, plan.PrimaryUID, plan.MemberUIDs); err != nil {
			return err
		}
		return writeStackAudit(ctx, tx, entry, plan.PrimaryUID, map[string]any{
			"stack_uid":   stackUID,
			"primary_uid": plan.PrimaryUID,
			"photo_uids":  plan.MemberUIDs,
		})
	})
	if err != nil {
		return "", err
	}
	return stackUID, nil
}

// CreateStacksAudited forms every plan's stack and writes entry once for the
// whole pass, all in one transaction, returning the new stack uids in plan
// order. It backs the automatic detection pass, which decides many stacks at
// once: one entry says which stacks that run created, and because the entry
// rides the same transaction as the stacks themselves, a pass that fails partway
// leaves neither the stacks nor a record claiming them. An empty plan list still
// records the pass — a maintainer asked, and "it found nothing" is an answer
// worth keeping — but touches no photo.
//
// A plan that does not describe a stack fails the whole pass with
// ErrStackTooSmall or ErrPhotoNotFound, as does a member that is missing or
// archived.
func (s *Store) CreateStacksAudited(
	ctx context.Context, plans []StackPlan, entry audit.Entry,
) ([]string, error) {
	normalized := make([]StackPlan, len(plans))
	for i, plan := range plans {
		p, err := plan.normalize()
		if err != nil {
			return nil, err
		}
		normalized[i] = p
	}
	stackUIDs := make([]string, len(normalized))
	photoCount := 0
	for i, plan := range normalized {
		uid, err := newStackUID()
		if err != nil {
			return nil, err
		}
		stackUIDs[i] = uid
		photoCount += len(plan.MemberUIDs)
	}
	err := s.inTx(ctx, func(tx pgx.Tx) error {
		for i, plan := range normalized {
			if err := applyNewStackTx(ctx, tx, stackUIDs[i], plan.PrimaryUID, plan.MemberUIDs); err != nil {
				return err
			}
		}
		// No single photo is the target of a whole pass, so the entry names none
		// and lists the stacks it formed instead, the way a bulk edit does.
		return writeStackAudit(ctx, tx, entry, "", map[string]any{
			"created":    len(stackUIDs),
			"stack_uids": stackUIDs,
			"photos":     photoCount,
		})
	})
	if err != nil {
		return nil, err
	}
	return stackUIDs, nil
}

// SetStackPrimaryAudited makes memberUID the primary of its stack and writes
// entry in the same transaction, returning the stack_uid. entry's TargetUID
// defaults to memberUID and its details name the stack, the promoted photo and
// the primary it replaced, so the change can be read back and reversed. It
// returns ErrPhotoNotFound or ErrPhotoNotStacked, each rolling the entry back
// with the mutation.
func (s *Store) SetStackPrimaryAudited(
	ctx context.Context, memberUID string, entry audit.Entry,
) (string, error) {
	var stackUID string
	err := s.inTx(ctx, func(tx pgx.Tx) error {
		su, previous, err := setStackPrimaryTx(ctx, tx, memberUID)
		if err != nil {
			return err
		}
		stackUID = su
		return writeStackAudit(ctx, tx, entry, memberUID, map[string]any{
			"stack_uid":            su,
			"photo_uid":            memberUID,
			"previous_primary_uid": previous,
		})
	})
	if err != nil {
		return "", err
	}
	return stackUID, nil
}

// UnstackMemberAudited removes memberUID from its stack and writes entry in the
// same transaction, returning the stack_uid it left. entry's TargetUID defaults
// to memberUID and its details name the stack and the photo taken out. The
// remaining stack is repaired exactly as in UnstackMember. It returns
// ErrPhotoNotFound or ErrPhotoNotStacked.
func (s *Store) UnstackMemberAudited(
	ctx context.Context, memberUID string, entry audit.Entry,
) (string, error) {
	var stackUID string
	err := s.inTx(ctx, func(tx pgx.Tx) error {
		su, err := unstackMemberTx(ctx, tx, memberUID)
		if err != nil {
			return err
		}
		stackUID = su
		return writeStackAudit(ctx, tx, entry, memberUID, map[string]any{
			"stack_uid": su,
			"photo_uid": memberUID,
		})
	})
	if err != nil {
		return "", err
	}
	return stackUID, nil
}

// UnstackAllAudited dissolves the whole stack memberUID belongs to and writes
// entry in the same transaction, returning the dissolved stack_uid. entry's
// TargetUID defaults to memberUID — the photo the stack was addressed through —
// and its details list every member the stack had, which is the only record of
// the grouping once the rows are standalone again. It returns ErrPhotoNotFound
// or ErrPhotoNotStacked.
func (s *Store) UnstackAllAudited(
	ctx context.Context, memberUID string, entry audit.Entry,
) (string, error) {
	var stackUID string
	err := s.inTx(ctx, func(tx pgx.Tx) error {
		su, members, err := unstackAllTx(ctx, tx, memberUID)
		if err != nil {
			return err
		}
		stackUID = su
		return writeStackAudit(ctx, tx, entry, memberUID, map[string]any{
			"stack_uid":  su,
			"photo_uid":  memberUID,
			"photo_uids": members,
		})
	})
	if err != nil {
		return "", err
	}
	return stackUID, nil
}

// writeStackAudit writes entry on tx, enriched by stackEntry with the facts the
// transaction learnt.
func writeStackAudit(
	ctx context.Context, tx pgx.Tx, entry audit.Entry, targetUID string, facts map[string]any,
) error {
	if err := audit.Write(ctx, tx, stackEntry(entry, targetUID, facts)); err != nil {
		return fmt.Errorf("photos: writing stack audit entry: %w", err)
	}
	return nil
}

// stackEntry returns a copy of entry with its TargetUID defaulted to targetUID
// and facts merged into a fresh details map on top of whatever the caller
// supplied. The copy is what makes it safe to complete an entry inside a
// transaction closure: stamping the caller's own entry there would be thrown
// away with the closure, and the audit row would record a NULL target.
func stackEntry(entry audit.Entry, targetUID string, facts map[string]any) audit.Entry {
	details := make(map[string]any, len(entry.Details)+len(facts))
	maps.Copy(details, entry.Details)
	maps.Copy(details, facts)
	entry.Details = details
	if entry.TargetUID == "" {
		entry.TargetUID = targetUID
	}
	return entry
}
