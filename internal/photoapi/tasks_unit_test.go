package photoapi

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/panbotka/kukatko/internal/photos"
	"github.com/panbotka/kukatko/internal/phototask"
)

// fakeTasks is a controllable TaskLookup for the detail unit tests.
type fakeTasks struct {
	byPhoto    map[string][]phototask.Ref
	err        error
	gotUIDs    []string
	gotProject string
	inProject  bool
}

// OpenForPhotos records the requested project and UIDs and returns the
// configured answer.
func (f *fakeTasks) OpenForPhotos(
	_ context.Context, projectUID string, photoUIDs []string,
) (map[string][]phototask.Ref, error) {
	f.gotProject, f.gotUIDs = projectUID, photoUIDs
	if f.err != nil {
		return nil, f.err
	}
	return f.byPhoto, nil
}

// InProject records the requested project and task UIDs and returns the
// configured answer.
func (f *fakeTasks) InProject(_ context.Context, projectUID string, taskUIDs []string) (bool, error) {
	f.gotProject, f.gotUIDs = projectUID, taskUIDs
	return f.inProject, f.err
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

// TestRequireTaskScope verifies a task-scoped listing goes on only when every
// task it names is one of the project's: an unscoped listing and an instance
// without the work queue pass, a task elsewhere (or nowhere) is 404 "task not
// found", and a failing lookup is a 500 rather than an unscoped page.
func TestRequireTaskScope(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		lookup     *fakeTasks
		taskUIDs   []string
		wantPass   bool
		wantStatus int
	}{
		{name: "unscoped listing", lookup: &fakeTasks{}, wantPass: true},
		{name: "no lookup wired", taskUIDs: []string{"tk1"}, wantPass: true},
		{name: "a task of the project", lookup: &fakeTasks{inProject: true}, taskUIDs: []string{"tk1"}, wantPass: true},
		{
			name: "a task of another project", lookup: &fakeTasks{}, taskUIDs: []string{"tk1"},
			wantStatus: http.StatusNotFound,
		},
		{
			name: "a broken lookup", lookup: &fakeTasks{err: errors.New("boom")}, taskUIDs: []string{"tk1"},
			wantStatus: http.StatusInternalServerError,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			api := &API{}
			if tt.lookup != nil {
				api.tasks = tt.lookup
			}
			rec := httptest.NewRecorder()
			req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/photos?task=tk1", http.NoBody)
			pass := api.requireTaskScope(rec, req, photos.ListParams{TaskUIDs: tt.taskUIDs})
			if pass != tt.wantPass {
				t.Fatalf("requireTaskScope = %v, want %v", pass, tt.wantPass)
			}
			if !pass && rec.Code != tt.wantStatus {
				t.Errorf("status = %d, want %d", rec.Code, tt.wantStatus)
			}
			if tt.wantStatus == http.StatusNotFound && !strings.Contains(rec.Body.String(), "task not found") {
				t.Errorf("body = %s, want task not found", rec.Body.String())
			}
		})
	}
}
