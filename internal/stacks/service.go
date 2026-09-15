package stacks

import (
	"context"
	"fmt"

	"github.com/panbotka/kukatko/internal/audit"
	"github.com/panbotka/kukatko/internal/photos"
)

// Config controls stack detection: the master switch for the whole feature plus
// which detection rules the backfill runs.
type Config struct {
	// Enabled is the master switch; when false the detector is inert and no stacks
	// are formed automatically.
	Enabled bool
	// Rules selects which detection rules run (see RuleSet).
	Rules RuleSet
}

// Store is the persistence the stack Service needs: enumerating the photos to
// consider and applying the reversible grouping. *photos.Store satisfies it; a
// fake stands in for unit tests.
//
// Every mutating method takes the audit entry its change is to be recorded with
// and writes it in the same transaction as the change, so a stack operation can
// never commit without leaving a trace (and a failed one leaves none). The
// Service only carries the entry through — who acted and from where is the
// caller's knowledge, while the stack uid and the members are the store's.
type Store interface {
	ListStackCandidates(ctx context.Context) ([]photos.StackCandidate, error)
	StackInfoByUIDs(ctx context.Context, uids []string) ([]photos.StackCandidate, error)
	CreateStackAudited(
		ctx context.Context, primaryUID string, memberUIDs []string, entry audit.Entry,
	) (string, error)
	CreateStacksAudited(ctx context.Context, plans []photos.StackPlan, entry audit.Entry) ([]string, error)
	SetStackPrimaryAudited(ctx context.Context, memberUID string, entry audit.Entry) (string, error)
	UnstackMemberAudited(ctx context.Context, memberUID string, entry audit.Entry) (string, error)
	UnstackAllAudited(ctx context.Context, memberUID string, entry audit.Entry) (string, error)
}

// Service detects stacks over the library and carries out the manual stacking
// operations (stack a selection, set a primary, unstack a member or a whole
// stack). It holds no connection; it borrows a Store.
type Service struct {
	store   Store
	enabled bool
	rules   RuleSet
}

// New returns a Service backed by store and configured by cfg. It panics if store
// is nil, since a nil store is a wiring bug the caller cannot recover from.
func New(store Store, cfg Config) *Service {
	if store == nil {
		panic("stacks: nil store")
	}
	return &Service{store: store, enabled: cfg.Enabled, rules: cfg.Rules}
}

// DetectStacks groups the currently unstacked, non-archived photos into stacks by
// the enabled rules and returns how many stacks were created, recording the pass
// under entry. It is incremental and idempotent: already-stacked photos are never
// candidates, so a re-run over a settled library creates nothing and never
// disturbs an existing or manually curated stack. It is a no-op returning 0 when
// the feature or every rule is off — and then writes no audit entry either,
// because nothing ran.
//
// The whole pass is applied in one transaction together with its single audit
// entry, so the trail's promise holds for a bulk decision too: what the entry
// claims was created is exactly what committed.
func (s *Service) DetectStacks(ctx context.Context, entry audit.Entry) (int, error) {
	if !s.enabled || !s.rules.Any() {
		return 0, nil
	}
	candidates, err := s.store.ListStackCandidates(ctx)
	if err != nil {
		return 0, fmt.Errorf("stacks: listing candidates: %w", err)
	}
	components := Group(candidates, s.rules)
	plans := make([]photos.StackPlan, len(components))
	for i, component := range components {
		plans[i] = planComponent(candidates, component)
	}
	created, err := s.store.CreateStacksAudited(ctx, plans, entry)
	if err != nil {
		return 0, fmt.Errorf("stacks: creating stacks: %w", err)
	}
	return len(created), nil
}

// planComponent turns the candidates at the component's indices into the plan for
// one stack, choosing the primary with PickPrimary. It is pure: deciding is this
// package's job, applying is the store's.
func planComponent(candidates []photos.StackCandidate, component []int) photos.StackPlan {
	members := make([]photos.StackCandidate, len(component))
	uids := make([]string, len(component))
	for i, idx := range component {
		members[i] = candidates[idx]
		uids[i] = candidates[idx].UID
	}
	return photos.StackPlan{PrimaryUID: PickPrimary(members), MemberUIDs: uids}
}

// StackSelection groups the given photos into one new stack for the cases the
// rules miss, choosing the primary with PickPrimary, and returns the new
// stack_uid. The grouping is recorded under entry in the same transaction. It
// returns photos.ErrStackTooSmall for fewer than two distinct photos and
// photos.ErrPhotoNotFound when one is missing or archived — neither writes an
// entry, because neither changes anything.
func (s *Service) StackSelection(ctx context.Context, uids []string, entry audit.Entry) (string, error) {
	distinct := distinctStrings(uids)
	if len(distinct) < 2 {
		return "", photos.ErrStackTooSmall
	}
	info, err := s.store.StackInfoByUIDs(ctx, distinct)
	if err != nil {
		return "", fmt.Errorf("stacks: loading selection: %w", err)
	}
	if len(info) != len(distinct) {
		return "", photos.ErrPhotoNotFound
	}
	stackUID, err := s.store.CreateStackAudited(ctx, PickPrimary(info), distinct, entry)
	if err != nil {
		return "", fmt.Errorf("stacks: stacking selection: %w", err)
	}
	return stackUID, nil
}

// SetPrimary makes uid the primary of its stack, returning the stack_uid and
// recording the change under entry in the same transaction.
func (s *Service) SetPrimary(ctx context.Context, uid string, entry audit.Entry) (string, error) {
	stackUID, err := s.store.SetStackPrimaryAudited(ctx, uid, entry)
	if err != nil {
		return "", fmt.Errorf("stacks: setting primary: %w", err)
	}
	return stackUID, nil
}

// Unstack removes uid from its stack, turning it back into a standalone photo,
// returning the stack_uid it left and recording the change under entry in the
// same transaction.
func (s *Service) Unstack(ctx context.Context, uid string, entry audit.Entry) (string, error) {
	stackUID, err := s.store.UnstackMemberAudited(ctx, uid, entry)
	if err != nil {
		return "", fmt.Errorf("stacks: unstacking member: %w", err)
	}
	return stackUID, nil
}

// UnstackWhole dissolves the entire stack uid belongs to, returning its stack_uid
// and recording the change under entry in the same transaction.
func (s *Service) UnstackWhole(ctx context.Context, uid string, entry audit.Entry) (string, error) {
	stackUID, err := s.store.UnstackAllAudited(ctx, uid, entry)
	if err != nil {
		return "", fmt.Errorf("stacks: dissolving stack: %w", err)
	}
	return stackUID, nil
}

// distinctStrings returns the distinct values of in, preserving first-seen order.
func distinctStrings(in []string) []string {
	seen := make(map[string]struct{}, len(in))
	out := make([]string, 0, len(in))
	for _, v := range in {
		if _, ok := seen[v]; ok {
			continue
		}
		seen[v] = struct{}{}
		out = append(out, v)
	}
	return out
}
