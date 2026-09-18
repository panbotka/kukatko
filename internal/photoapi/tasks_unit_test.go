package photoapi

import (
	"context"
	"errors"
	"testing"

	"github.com/panbotka/kukatko/internal/phototask"
)

// fakeTasks is a controllable TaskLookup for the detail unit tests.
type fakeTasks struct {
	byPhoto map[string][]phototask.Ref
	err     error
	gotUIDs []string
}

// OpenForPhotos records the requested UIDs and returns the configured answer.
func (f *fakeTasks) OpenForPhotos(
	_ context.Context, photoUIDs []string,
) (map[string][]phototask.Ref, error) {
	f.gotUIDs = photoUIDs
	if f.err != nil {
		return nil, f.err
	}
	return f.byPhoto, nil
}

// TestResolveTasks verifies the detail's open-task chip: the refs for a photo in
// one, nothing for a photo in none, nothing when no lookup is wired, and nothing
// — rather than a failure — when the lookup breaks.
func TestResolveTasks(t *testing.T) {
	t.Parallel()

	refs := []phototask.Ref{{UID: "tk1", Title: "V kterém roce?", State: phototask.StateQuestion}}

	tests := []struct {
		name   string
		lookup TaskLookup
		want   int
	}{
		{
			name:   "a photo in an open task",
			lookup: &fakeTasks{byPhoto: map[string][]phototask.Ref{"ph1": refs}},
			want:   1,
		},
		{name: "a photo in none", lookup: &fakeTasks{byPhoto: map[string][]phototask.Ref{}}},
		{name: "no lookup wired", lookup: nil},
		{name: "a broken lookup", lookup: &fakeTasks{err: errors.New("boom")}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			api := &API{tasks: tt.lookup}
			got := api.resolveTasks(context.Background(), "ph1")
			if len(got) != tt.want {
				t.Errorf("resolveTasks = %v, want %d refs", got, tt.want)
			}
		})
	}
}

// TestResolveTasksAsksInBulk verifies the single-photo detail routes through the
// bulk lookup, so the same call can annotate a page of photos without becoming a
// query per tile.
func TestResolveTasksAsksInBulk(t *testing.T) {
	t.Parallel()

	fake := &fakeTasks{byPhoto: map[string][]phototask.Ref{}}
	api := &API{tasks: fake}
	api.resolveTasks(context.Background(), "ph1")
	if len(fake.gotUIDs) != 1 || fake.gotUIDs[0] != "ph1" {
		t.Errorf("OpenForPhotos called with %v, want exactly the one photo", fake.gotUIDs)
	}
}
