package stacks

import (
	"context"
	"errors"
	"testing"

	"github.com/panbotka/kukatko/internal/photos"
)

// createCall records one CreateStack invocation.
type createCall struct {
	primary string
	members []string
}

// fakeStore is an in-memory Store for exercising the Service without a database.
type fakeStore struct {
	candidates []photos.StackCandidate
	byUID      map[string]photos.StackCandidate
	created    []createCall
	nextStack  int
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

func (f *fakeStore) CreateStack(_ context.Context, primary string, members []string) (string, error) {
	f.created = append(f.created, createCall{primary: primary, members: members})
	f.nextStack++
	return "st-fake", nil
}

func (f *fakeStore) SetStackPrimary(context.Context, string) (string, error) { return "st-fake", nil }
func (f *fakeStore) UnstackMember(context.Context, string) (string, error)   { return "st-fake", nil }
func (f *fakeStore) UnstackAll(context.Context, string) (string, error)      { return "st-fake", nil }

func TestService_DetectStacks_disabledIsInert(t *testing.T) {
	t.Parallel()
	store := &fakeStore{candidates: []photos.StackCandidate{
		{UID: "a", FileName: "IMG.CR2"}, {UID: "b", FileName: "IMG.jpg"},
	}}
	svc := New(store, Config{Enabled: false, Rules: RuleSet{BaseName: true}})
	created, err := svc.DetectStacks(t.Context())
	if err != nil {
		t.Fatalf("DetectStacks: %v", err)
	}
	if created != 0 || len(store.created) != 0 {
		t.Errorf("disabled detector formed %d stacks (%d calls), want 0", created, len(store.created))
	}
}

func TestService_DetectStacks_formsStackAndPicksPrimary(t *testing.T) {
	t.Parallel()
	store := &fakeStore{candidates: []photos.StackCandidate{
		{UID: "raw", FileName: "IMG.CR2", FileWidth: 6000, FileHeight: 4000},
		{UID: "jpg", FileName: "IMG.jpg", FileWidth: 6000, FileHeight: 4000},
	}}
	svc := New(store, Config{Enabled: true, Rules: RuleSet{BaseName: true}})
	created, err := svc.DetectStacks(t.Context())
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
	created, err := svc.DetectStacks(t.Context())
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

	if _, err := svc.StackSelection(t.Context(), []string{"a", "a"}); !errors.Is(err, photos.ErrStackTooSmall) {
		t.Errorf("duplicate selection error = %v, want ErrStackTooSmall", err)
	}
	if _, err := svc.StackSelection(t.Context(), []string{"a", "missing"}); !errors.Is(err, photos.ErrPhotoNotFound) {
		t.Errorf("missing member error = %v, want ErrPhotoNotFound", err)
	}
	if _, err := svc.StackSelection(t.Context(), []string{"a", "b"}); err != nil {
		t.Fatalf("valid selection: %v", err)
	}
	if len(store.created) != 1 || store.created[0].primary == "" {
		t.Errorf("expected one CreateStack with a chosen primary, got %v", store.created)
	}
}
