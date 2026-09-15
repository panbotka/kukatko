package stacks

import (
	"context"
	"errors"
	"testing"

	"github.com/panbotka/kukatko/internal/audit"
	"github.com/panbotka/kukatko/internal/photos"
)

// createCall records one stack-creating invocation together with the audit entry
// it was asked to record the change under.
type createCall struct {
	primary string
	members []string
	entry   audit.Entry
}

// fakeStore is an in-memory Store for exercising the Service without a database.
type fakeStore struct {
	candidates []photos.StackCandidate
	byUID      map[string]photos.StackCandidate
	created    []createCall
	// batches records the plans handed to CreateStacksAudited, one entry per pass.
	batches []audit.Entry
	// mutated records the audit entry of every single-photo mutation, keyed by
	// the method that took it.
	mutated map[string]audit.Entry
}

func (f *fakeStore) ListStackCandidates(context.Context) ([]photos.StackCandidate, error) {
	return f.candidates, nil
}

func (f *fakeStore) StackInfoByUIDs(_ context.Context, uids []string) ([]photos.StackCandidate, error) {
	out := make([]photos.StackCandidate, 0, len(uids))
	for _, uid := range uids {
		if c, ok := f.byUID[uid]; ok {
			out = append(out, c)
		}
	}
	return out, nil
}

func (f *fakeStore) CreateStackAudited(
	_ context.Context, primary string, members []string, entry audit.Entry,
) (string, error) {
	f.created = append(f.created, createCall{primary: primary, members: members, entry: entry})
	return "st-fake", nil
}

func (f *fakeStore) CreateStacksAudited(
	_ context.Context, plans []photos.StackPlan, entry audit.Entry,
) ([]string, error) {
	f.batches = append(f.batches, entry)
	uids := make([]string, len(plans))
	for i, plan := range plans {
		f.created = append(f.created, createCall{primary: plan.PrimaryUID, members: plan.MemberUIDs, entry: entry})
		uids[i] = "st-fake"
	}
	return uids, nil
}

// record remembers the entry a single-photo mutation was called with.
func (f *fakeStore) record(method string, entry audit.Entry) (string, error) {
	if f.mutated == nil {
		f.mutated = make(map[string]audit.Entry)
	}
	f.mutated[method] = entry
	return "st-fake", nil
}

func (f *fakeStore) SetStackPrimaryAudited(_ context.Context, _ string, entry audit.Entry) (string, error) {
	return f.record("set_primary", entry)
}

func (f *fakeStore) UnstackMemberAudited(_ context.Context, _ string, entry audit.Entry) (string, error) {
	return f.record("unstack", entry)
}

func (f *fakeStore) UnstackAllAudited(_ context.Context, _ string, entry audit.Entry) (string, error) {
	return f.record("unstack_all", entry)
}

// testEntry is the audit entry the tests hand to the mutating methods.
func testEntry(action string) audit.Entry {
	return audit.Entry{ActorUID: "usr_test", Action: action, TargetType: "photos"}
}

func TestService_DetectStacks_disabledIsInert(t *testing.T) {
	t.Parallel()
	store := &fakeStore{candidates: []photos.StackCandidate{
		{UID: "a", FileName: "IMG.CR2"}, {UID: "b", FileName: "IMG.jpg"},
	}}
	svc := New(store, Config{Enabled: false, Rules: RuleSet{BaseName: true}})
	created, err := svc.DetectStacks(t.Context(), testEntry(audit.ActionStacksDetect))
	if err != nil {
		t.Fatalf("DetectStacks: %v", err)
	}
	if created != 0 || len(store.created) != 0 {
		t.Errorf("disabled detector formed %d stacks (%d calls), want 0", created, len(store.created))
	}
	if len(store.batches) != 0 {
		t.Errorf("disabled detector recorded %d passes, want 0 — nothing ran", len(store.batches))
	}
}

func TestService_DetectStacks_formsStackAndPicksPrimary(t *testing.T) {
	t.Parallel()
	store := &fakeStore{candidates: []photos.StackCandidate{
		{UID: "raw", FileName: "IMG.CR2", FileWidth: 6000, FileHeight: 4000},
		{UID: "jpg", FileName: "IMG.jpg", FileWidth: 6000, FileHeight: 4000},
	}}
	svc := New(store, Config{Enabled: true, Rules: RuleSet{BaseName: true}})
	created, err := svc.DetectStacks(t.Context(), testEntry(audit.ActionStacksDetect))
	if err != nil {
		t.Fatalf("DetectStacks: %v", err)
	}
	if created != 1 || len(store.created) != 1 {
		t.Fatalf("created %d stacks (%d calls), want 1", created, len(store.created))
	}
	if store.created[0].primary != "jpg" {
		t.Errorf("primary = %q, want jpg (rendered beats raw)", store.created[0].primary)
	}
	if len(store.created[0].members) != 2 {
		t.Errorf("members = %v, want 2", store.created[0].members)
	}
}

func TestService_DetectStacks_idempotentWhenSettled(t *testing.T) {
	t.Parallel()
	// A settled library exposes no unstacked candidates, so detection is a no-op.
	store := &fakeStore{candidates: nil}
	svc := New(store, Config{Enabled: true, Rules: RuleSet{BaseName: true}})
	created, err := svc.DetectStacks(t.Context(), testEntry(audit.ActionStacksDetect))
	if err != nil {
		t.Fatalf("DetectStacks: %v", err)
	}
	if created != 0 {
		t.Errorf("settled library created %d stacks, want 0", created)
	}
}

func TestService_StackSelection_validation(t *testing.T) {
	t.Parallel()
	store := &fakeStore{byUID: map[string]photos.StackCandidate{
		"a": {UID: "a", FileName: "a.jpg"}, "b": {UID: "b", FileName: "b.jpg"},
	}}
	svc := New(store, Config{Enabled: true})

	if _, err := svc.StackSelection(t.Context(), []string{"a", "a"}, testEntry(audit.ActionPhotosStack)); !errors.Is(err, photos.ErrStackTooSmall) {
		t.Errorf("duplicate selection error = %v, want ErrStackTooSmall", err)
	}
	if _, err := svc.StackSelection(t.Context(), []string{"a", "missing"}, testEntry(audit.ActionPhotosStack)); !errors.Is(err, photos.ErrPhotoNotFound) {
		t.Errorf("missing member error = %v, want ErrPhotoNotFound", err)
	}
	if _, err := svc.StackSelection(t.Context(), []string{"a", "b"}, testEntry(audit.ActionPhotosStack)); err != nil {
		t.Fatalf("valid selection: %v", err)
	}
	if len(store.created) != 1 || store.created[0].primary == "" {
		t.Errorf("expected one CreateStack with a chosen primary, got %v", store.created)
	}
}

func TestService_DetectStacks_appliesOnePassWithItsAuditEntry(t *testing.T) {
	t.Parallel()
	// Two independent base-name groups: one pass, one audit entry, two stacks.
	store := &fakeStore{candidates: []photos.StackCandidate{
		{UID: "a-raw", FileName: "A.CR2"}, {UID: "a-jpg", FileName: "A.jpg"},
		{UID: "b-raw", FileName: "B.CR2"}, {UID: "b-jpg", FileName: "B.jpg"},
	}}
	svc := New(store, Config{Enabled: true, Rules: RuleSet{BaseName: true}})
	entry := testEntry(audit.ActionStacksDetect)

	created, err := svc.DetectStacks(t.Context(), entry)
	if err != nil {
		t.Fatalf("DetectStacks: %v", err)
	}
	if created != 2 {
		t.Errorf("created = %d, want 2", created)
	}
	if len(store.batches) != 1 {
		t.Fatalf("store saw %d passes, want 1 (the whole detection is one audited transaction)", len(store.batches))
	}
	if store.batches[0].Action != audit.ActionStacksDetect || store.batches[0].ActorUID != "usr_test" {
		t.Errorf("pass entry = %+v, want the caller's stacks.detect entry", store.batches[0])
	}
}

func TestService_mutations_carryTheAuditEntry(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		action string
		call   func(svc *Service, entry audit.Entry) error
	}{
		{
			name:   "set primary",
			action: audit.ActionStackSetPrimary,
			call: func(svc *Service, entry audit.Entry) error {
				_, err := svc.SetPrimary(t.Context(), "ph1", entry)
				return err
			},
		},
		{
			name:   "unstack",
			action: audit.ActionStackUngroup,
			call: func(svc *Service, entry audit.Entry) error {
				_, err := svc.Unstack(t.Context(), "ph1", entry)
				return err
			},
		},
		{
			name:   "unstack all",
			action: audit.ActionStackUngroupAll,
			call: func(svc *Service, entry audit.Entry) error {
				_, err := svc.UnstackWhole(t.Context(), "ph1", entry)
				return err
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			store := &fakeStore{}
			svc := New(store, Config{Enabled: true})
			if err := tt.call(svc, testEntry(tt.action)); err != nil {
				t.Fatalf("%s: %v", tt.name, err)
			}
			if len(store.mutated) != 1 {
				t.Fatalf("store saw %d audited mutations, want 1", len(store.mutated))
			}
			for _, got := range store.mutated {
				if got.Action != tt.action || got.ActorUID != "usr_test" {
					t.Errorf("entry = %+v, want action %q by usr_test", got, tt.action)
				}
			}
		})
	}
}

func TestService_StackSelection_carriesTheAuditEntry(t *testing.T) {
	t.Parallel()
	store := &fakeStore{byUID: map[string]photos.StackCandidate{
		"a": {UID: "a", FileName: "a.jpg"}, "b": {UID: "b", FileName: "b.jpg"},
	}}
	svc := New(store, Config{Enabled: true})

	if _, err := svc.StackSelection(t.Context(), []string{"a", "b"}, testEntry(audit.ActionPhotosStack)); err != nil {
		t.Fatalf("StackSelection: %v", err)
	}
	if len(store.created) != 1 {
		t.Fatalf("store saw %d creations, want 1", len(store.created))
	}
	if store.created[0].entry.Action != audit.ActionPhotosStack {
		t.Errorf("entry action = %q, want %q", store.created[0].entry.Action, audit.ActionPhotosStack)
	}
}
