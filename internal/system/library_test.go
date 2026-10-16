package system

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"
)

// fakeCounter is a LibraryCounter returning fixed counts (or an error) and
// recording how many times it was asked and for which projects, so the
// memoisation and its per-project keying can be observed. perProject, when
// set, overrides counts for the projects it names.
type fakeCounter struct {
	counts     Library
	perProject map[string]Library
	err        error
	calls      int
	projects   []string
}

// CountLibrary returns the configured counts or error, recording the call.
func (f *fakeCounter) CountLibrary(_ context.Context, projectUID string) (Library, error) {
	f.calls++
	f.projects = append(f.projects, projectUID)
	if counts, ok := f.perProject[projectUID]; ok {
		return counts, f.err
	}
	return f.counts, f.err
}

// TestLibraryDerive checks every derived value: the live/archived split, the
// photos the library grid lists, the plain-image share of the media types, both
// coverage gaps, the nameless faces and the unassigned markers, including the
// clamp that keeps a snapshot taken mid-write from reporting a negative count.
func TestLibraryDerive(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		raw  Library
		want Library
	}{
		{
			name: "gaps are the difference from the total",
			raw: Library{
				Photos: 100, PhotosArchived: 7, PhotosWithEmbedding: 90,
				PhotosWithFaces: 40, Faces: 250, FacesAssigned: 30,
				Markers: 30, MarkersAssigned: 12,
			},
			want: Library{
				Photos: 100, Images: 100, PhotosArchived: 7, PhotosLive: 93, PhotosListed: 93,
				PhotosWithEmbedding: 90, PhotosWithoutEmbedding: 10,
				PhotosWithFaces: 40, PhotosWithoutFaces: 60,
				Faces: 250, FacesAssigned: 30, FacesUnassigned: 220,
				Markers: 30, MarkersAssigned: 12, MarkersUnassigned: 18,
			},
		},
		{
			// Production-shaped numbers the statistics page was reported over: the
			// page said 19 984 while the library grid said 19 713, and the 271
			// missing photos are exactly the hidden ones plus the stacked-away
			// siblings. Both counts are now reported, so the page can say so.
			name: "the grid count is the live library minus the hidden and the stacked away",
			raw: Library{
				Photos: 20000, PhotosArchived: 16, PhotosHidden: 30, PhotosStacked: 241,
			},
			want: Library{
				Photos: 20000, Images: 20000, PhotosArchived: 16,
				PhotosLive: 19984, PhotosHidden: 30, PhotosStacked: 241, PhotosListed: 19713,
				PhotosWithoutEmbedding: 20000, PhotosWithoutFaces: 20000,
			},
		},
		{
			name: "the two halves of the face grain add up to the whole",
			raw: Library{
				Photos: 10, PhotosWithFaces: 10, Faces: 110000, FacesAssigned: 4400,
			},
			want: Library{
				Photos: 10, Images: 10, PhotosLive: 10, PhotosListed: 10,
				PhotosWithoutEmbedding: 10,
				PhotosWithFaces:        10, Faces: 110000, FacesAssigned: 4400,
				FacesUnassigned: 105600,
			},
		},
		{
			name: "plain images are the total minus the other media types",
			raw: Library{
				Photos: 20, Videos: 3, LivePhotos: 2,
			},
			want: Library{
				Photos: 20, Videos: 3, LivePhotos: 2, Images: 15,
				PhotosLive: 20, PhotosListed: 20,
				PhotosWithoutEmbedding: 20, PhotosWithoutFaces: 20,
			},
		},
		{
			name: "full coverage leaves no gap",
			raw: Library{
				Photos: 5, PhotosWithEmbedding: 5, PhotosWithFaces: 5,
				Faces: 8, FacesAssigned: 8, Markers: 2, MarkersAssigned: 2,
			},
			want: Library{
				Photos: 5, Images: 5, PhotosLive: 5, PhotosListed: 5,
				PhotosWithEmbedding: 5, PhotosWithFaces: 5,
				Faces: 8, FacesAssigned: 8, Markers: 2, MarkersAssigned: 2,
			},
		},
		{
			name: "more deductions than live photos clamps the grid count at zero",
			raw: Library{
				Photos: 4, PhotosArchived: 1, PhotosHidden: 2, PhotosStacked: 5,
			},
			want: Library{
				Photos: 4, Images: 4, PhotosArchived: 1, PhotosLive: 3,
				PhotosHidden: 2, PhotosStacked: 5,
				PhotosWithoutEmbedding: 4, PhotosWithoutFaces: 4,
			},
		},
		{
			name: "an inconsistent snapshot clamps at zero, never negative",
			raw: Library{
				Photos: 3, Videos: 9, PhotosArchived: 4, PhotosWithEmbedding: 4,
				PhotosWithFaces: 9, Faces: 2, FacesAssigned: 5,
				Markers: 1, MarkersAssigned: 2,
			},
			want: Library{
				Photos: 3, Videos: 9, PhotosArchived: 4, PhotosWithEmbedding: 4,
				PhotosWithFaces: 9, Faces: 2, FacesAssigned: 5,
				Markers: 1, MarkersAssigned: 2,
			},
		},
		{
			name: "an empty library derives all zeroes",
			raw:  Library{},
			want: Library{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := tt.raw.derive(); got != tt.want {
				t.Errorf("derive() = %+v, want %+v", got, tt.want)
			}
		})
	}
}

// TestNonNegative checks the clamp helper on both sides of zero.
func TestNonNegative(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		in   int
		want int
	}{{-5, 0}, {-1, 0}, {0, 0}, {7, 7}} {
		if got := nonNegative(tt.in); got != tt.want {
			t.Errorf("nonNegative(%d) = %d, want %d", tt.in, got, tt.want)
		}
	}
}

// TestLibraryCache_MemoisesWithinTTL verifies a second read inside the TTL is
// served from the cache and does not re-run the aggregation.
func TestLibraryCache_MemoisesWithinTTL(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 7, 29, 12, 0, 0, 0, time.UTC)
	counter := &fakeCounter{counts: Library{Photos: 10, PhotosWithEmbedding: 4}}
	cache := newLibraryCache(counter, time.Minute, func() time.Time { return now })

	first, err := cache.get(t.Context(), testProject)
	if err != nil {
		t.Fatalf("first counts: %v", err)
	}
	if first.PhotosWithoutEmbedding != 6 {
		t.Errorf("photos without embedding = %d, want 6", first.PhotosWithoutEmbedding)
	}

	now = now.Add(30 * time.Second)
	second, err := cache.get(t.Context(), testProject)
	if err != nil {
		t.Fatalf("second counts: %v", err)
	}
	if second != first {
		t.Errorf("second counts = %+v, want the memoised %+v", second, first)
	}
	if counter.calls != 1 {
		t.Errorf("counter called %d times, want 1 (memoised)", counter.calls)
	}
}

// TestLibraryCache_RecomputesAfterTTL verifies the aggregation runs again once
// the memoised value has expired, so the page does not show stale counts.
func TestLibraryCache_RecomputesAfterTTL(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 7, 29, 12, 0, 0, 0, time.UTC)
	counter := &fakeCounter{counts: Library{Photos: 1}}
	cache := newLibraryCache(counter, time.Minute, func() time.Time { return now })

	if _, err := cache.get(t.Context(), testProject); err != nil {
		t.Fatalf("first counts: %v", err)
	}
	now = now.Add(2 * time.Minute)
	counter.counts = Library{Photos: 2}
	got, err := cache.get(t.Context(), testProject)
	if err != nil {
		t.Fatalf("second counts: %v", err)
	}
	if got.Photos != 2 {
		t.Errorf("photos = %d, want the recomputed 2", got.Photos)
	}
	if counter.calls != 2 {
		t.Errorf("counter called %d times, want 2 (TTL expired)", counter.calls)
	}
}

// TestLibraryCache_ErrorIsReturnedNotCached verifies a failed aggregation
// surfaces as an error — the page must not render zeroes as real counts — and
// that the failure is not memoised, so the next read retries.
func TestLibraryCache_ErrorIsReturnedNotCached(t *testing.T) {
	t.Parallel()

	wantErr := errors.New("db down")
	counter := &fakeCounter{err: wantErr}
	cache := newLibraryCache(counter, time.Minute, nil)

	got, err := cache.get(t.Context(), testProject)
	if !errors.Is(err, wantErr) {
		t.Fatalf("counts error = %v, want %v", err, wantErr)
	}
	if got != (Library{}) {
		t.Errorf("counts = %+v, want the zero value alongside the error", got)
	}

	counter.err = nil
	counter.counts = Library{Photos: 3}
	recovered, err := cache.get(t.Context(), testProject)
	if err != nil {
		t.Fatalf("counts after recovery: %v", err)
	}
	if recovered.Photos != 3 || counter.calls != 2 {
		t.Errorf("recovered = %+v after %d calls, want photos 3 after 2 calls",
			recovered, counter.calls)
	}
}

// TestLibraryCache_Defaults verifies the zero-value knobs fall back to the
// package defaults rather than a zero TTL or a nil clock.
func TestLibraryCache_Defaults(t *testing.T) {
	t.Parallel()

	cache := newLibraryCache(&fakeCounter{}, 0, nil)
	if _, err := cache.get(t.Context(), testProject); err != nil {
		t.Fatalf("counts: %v", err)
	}
	entry := cache.byProject[testProject]
	if entry.ttl != defaultLibraryTTL {
		t.Errorf("ttl = %v, want the default %v", entry.ttl, defaultLibraryTTL)
	}
	if entry.now == nil {
		t.Error("clock is nil, want the default time.Now")
	}
}

// TestLibraryCache_KeyedByProject verifies the memoisation is per project: a
// second project is counted on its own rather than served the first one's
// memoised snapshot, and each project's snapshot stays its own afterwards.
func TestLibraryCache_KeyedByProject(t *testing.T) {
	t.Parallel()

	counter := &fakeCounter{perProject: map[string]Library{
		"pr-a": {Photos: 10},
		"pr-b": {Photos: 2},
	}}
	cache := newLibraryCache(counter, time.Minute, nil)

	for _, step := range []struct {
		project string
		photos  int
	}{{"pr-a", 10}, {"pr-b", 2}, {"pr-a", 10}, {"pr-b", 2}} {
		got, err := cache.get(t.Context(), step.project)
		if err != nil {
			t.Fatalf("counts of %s: %v", step.project, err)
		}
		if got.Photos != step.photos {
			t.Errorf("photos of %s = %d, want %d", step.project, got.Photos, step.photos)
		}
	}
	if want := []string{"pr-a", "pr-b"}; !slices.Equal(counter.projects, want) {
		t.Errorf("counted projects = %v, want %v (each once, then memoised)", counter.projects, want)
	}
}

// TestLibraryCache_NoProjectIsAnError verifies an empty project uid is refused
// before the counter is asked: it must never mean "every project".
func TestLibraryCache_NoProjectIsAnError(t *testing.T) {
	t.Parallel()

	counter := &fakeCounter{counts: Library{Photos: 5}}
	svc := New(Config{Library: counter})
	if _, err := svc.LibraryStats(t.Context(), ""); !errors.Is(err, ErrNoProject) {
		t.Errorf("LibraryStats error = %v, want %v", err, ErrNoProject)
	}
	if counter.calls != 0 {
		t.Errorf("counter called %d times, want 0", counter.calls)
	}
}

// TestServiceLibraryStats_NoCounter verifies a service wired without a library
// counter answers with an error instead of panicking on a nil interface.
func TestServiceLibraryStats_NoCounter(t *testing.T) {
	t.Parallel()

	svc := New(Config{})
	if _, err := svc.LibraryStats(t.Context(), testProject); !errors.Is(err, errNoLibraryCounter) {
		t.Errorf("LibraryStats error = %v, want %v", err, errNoLibraryCounter)
	}
}

// TestServiceLibraryStats_Derives verifies the service returns the counter's
// numbers with the derived gaps filled in, the library-grid count among them.
func TestServiceLibraryStats_Derives(t *testing.T) {
	t.Parallel()

	svc := New(Config{Library: &fakeCounter{counts: Library{
		Photos: 20, PhotosArchived: 2, PhotosHidden: 1, PhotosStacked: 3,
		PhotosWithEmbedding: 18, PhotosWithFaces: 11,
	}}})
	got, err := svc.LibraryStats(t.Context(), testProject)
	if err != nil {
		t.Fatalf("LibraryStats: %v", err)
	}
	if got.PhotosLive != 18 || got.PhotosWithoutEmbedding != 2 || got.PhotosWithoutFaces != 9 {
		t.Errorf("stats = %+v, want live 18 / no-embedding 2 / no-faces 9", got)
	}
	if got.PhotosListed != 14 {
		t.Errorf("photos listed = %d, want 14 (18 live − 1 hidden − 3 stacked away)", got.PhotosListed)
	}
}
