package worker

import (
	"context"
	"reflect"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/panbotka/kukatko/internal/jobs"
)

// liveTracker records, per job type, how many handlers are running at once and
// the highest such count ever reached. It is the instrument the concurrency
// tests measure with, and is safe for concurrent use.
type liveTracker struct {
	mu   sync.Mutex
	live map[string]int
	peak map[string]int
}

// newLiveTracker returns an empty liveTracker.
func newLiveTracker() *liveTracker {
	return &liveTracker{live: make(map[string]int), peak: make(map[string]int)}
}

// enter records a handler of jobType starting and updates that type's peak.
func (l *liveTracker) enter(jobType string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.live[jobType]++
	l.peak[jobType] = max(l.peak[jobType], l.live[jobType])
}

// exit records a handler of jobType finishing.
func (l *liveTracker) exit(jobType string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.live[jobType]--
}

// peakOf returns the highest number of jobType handlers that ever ran at once.
func (l *liveTracker) peakOf(jobType string) int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.peak[jobType]
}

// hold blocks until at least want handlers of jobType run at once or timeout
// elapses. Handlers call it instead of sleeping a fixed time: it both keeps a
// slot occupied long enough for an overlap to be observable and makes the
// expected overlap deterministic rather than a timing accident.
func (l *liveTracker) hold(jobType string, want int, timeout time.Duration) {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		l.mu.Lock()
		got := l.live[jobType]
		l.mu.Unlock()
		if got >= want {
			return
		}
		time.Sleep(time.Millisecond)
	}
}

// trackingHandler returns a handler that records its own concurrency for
// jobType and occupies its slot until want of them overlap or timeout elapses.
func trackingHandler(track *liveTracker, jobType string, want int, timeout time.Duration) HandlerFunc {
	return func(context.Context, jobs.Job) error {
		track.enter(jobType)
		defer track.exit(jobType)
		track.hold(jobType, want, timeout)
		return nil
	}
}

// queuedJobs builds count pending jobs of jobType with ids starting at firstID.
func queuedJobs(firstID int64, count int, jobType string) []jobs.Job {
	out := make([]jobs.Job, 0, count)
	for i := range count {
		out = append(out, jobs.Job{ID: firstID + int64(i), Type: jobType})
	}
	return out
}

// runUntilDrained runs w until q has recorded want completions, then cancels it
// and waits for Run to return.
func runUntilDrained(t *testing.T, w *Worker, q *fakeQueue, want int) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- w.Run(ctx) }()
	waitFor(t, func() bool {
		completed, _ := q.snapshot()
		return len(completed) >= want
	})
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not return within 2s of cancellation")
	}
}

// TestEffectiveTypeConcurrency verifies the sidecar-bound types are capped at one
// slot unless the configuration names them, that other types get a pool only when
// configured, and that non-positive entries are ignored.
func TestEffectiveTypeConcurrency(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		configured map[string]int
		want       map[string]int
	}{
		{
			name:       "no configuration serialises the sidecar-bound types",
			configured: nil,
			want:       map[string]int{jobs.TypeImageEmbed: 1, jobs.TypeFaceDetect: 1},
		},
		{
			name:       "a partial map still leaves the sidecar-bound types capped",
			configured: map[string]int{jobs.TypeThumbnail: 4},
			want: map[string]int{
				jobs.TypeImageEmbed: 1, jobs.TypeFaceDetect: 1, jobs.TypeThumbnail: 4,
			},
		},
		{
			name:       "an explicit override raises a sidecar-bound type",
			configured: map[string]int{jobs.TypeImageEmbed: 3},
			want:       map[string]int{jobs.TypeImageEmbed: 3, jobs.TypeFaceDetect: 1},
		},
		{
			name:       "non-positive and empty entries are ignored",
			configured: map[string]int{jobs.TypeImageEmbed: 0, jobs.TypeThumbnail: -1, "": 5},
			want:       map[string]int{jobs.TypeImageEmbed: 1, jobs.TypeFaceDetect: 1},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := effectiveTypeConcurrency(tt.configured); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("effectiveTypeConcurrency(%v) = %v, want %v", tt.configured, got, tt.want)
			}
		})
	}
}

// TestPools verifies how registered job types are split into pools: a dedicated
// pool per type with a concurrency entry, one shared pool of Concurrency slots
// for the rest, no pool for a type nobody handles, and no empty shared pool
// (which the queue would read as "claim anything").
func TestPools(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name            string
		registered      []string
		concurrency     int
		typeConcurrency map[string]int
		want            []pool
	}{
		{
			name: "sidecar-bound types are split out of the shared pool",
			registered: []string{
				jobs.TypeThumbnail, jobs.TypeImageEmbed, jobs.TypeMetadata, jobs.TypeFaceDetect,
			},
			concurrency: 4,
			want: []pool{
				{name: jobs.TypeFaceDetect, types: []string{jobs.TypeFaceDetect}, size: 1},
				{name: jobs.TypeImageEmbed, types: []string{jobs.TypeImageEmbed}, size: 1},
				{
					name:  sharedPoolName,
					types: []string{jobs.TypeMetadata, jobs.TypeThumbnail},
					size:  4,
				},
			},
		},
		{
			name:        "nothing left over means no shared pool",
			registered:  []string{jobs.TypeImageEmbed, jobs.TypeFaceDetect},
			concurrency: 4,
			want: []pool{
				{name: jobs.TypeFaceDetect, types: []string{jobs.TypeFaceDetect}, size: 1},
				{name: jobs.TypeImageEmbed, types: []string{jobs.TypeImageEmbed}, size: 1},
			},
		},
		{
			name:            "an override for an unregistered type creates no pool",
			registered:      []string{jobs.TypeThumbnail},
			concurrency:     2,
			typeConcurrency: map[string]int{jobs.TypePlaces: 3},
			want: []pool{
				{name: sharedPoolName, types: []string{jobs.TypeThumbnail}, size: 2},
			},
		},
		{
			name:            "a CPU-bound type can be given its own pool",
			registered:      []string{jobs.TypeThumbnail, jobs.TypeMetadata},
			concurrency:     2,
			typeConcurrency: map[string]int{jobs.TypeThumbnail: 6},
			want: []pool{
				{name: jobs.TypeThumbnail, types: []string{jobs.TypeThumbnail}, size: 6},
				{name: sharedPoolName, types: []string{jobs.TypeMetadata}, size: 2},
			},
		},
		{
			name:            "a non-positive override leaves the type in the shared pool",
			registered:      []string{jobs.TypeThumbnail, jobs.TypeMetadata},
			concurrency:     2,
			typeConcurrency: map[string]int{jobs.TypeThumbnail: 0},
			want: []pool{
				{
					name:  sharedPoolName,
					types: []string{jobs.TypeMetadata, jobs.TypeThumbnail},
					size:  2,
				},
			},
		},
		{
			name:        "an unset Concurrency falls back to the built-in default",
			registered:  []string{jobs.TypeThumbnail},
			concurrency: 0,
			want: []pool{
				{name: sharedPoolName, types: []string{jobs.TypeThumbnail}, size: defaultConcurrency},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			reg := NewRegistry()
			for _, jobType := range tt.registered {
				reg.Register(jobType, NoopHandler)
			}
			w := New(Config{
				Queue: newFakeQueue(), Registry: reg,
				Concurrency: tt.concurrency, TypeConcurrency: tt.typeConcurrency,
			})
			if got := w.pools(); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("pools() = %+v, want %+v", got, tt.want)
			}
		})
	}
}

// TestRun_serialisesSidecarBoundTypes is the regression test for the throughput
// ceiling this design removes: with the default configuration, image_embed must
// still run strictly one at a time (the embeddings box serves one request at a
// time), while the CPU-bound thumbnail jobs run Concurrency-wide instead of
// queueing behind it.
func TestRun_serialisesSidecarBoundTypes(t *testing.T) {
	t.Parallel()

	const (
		shared = 4
		embeds = 4
		thumbs = 8
	)
	pending := append(
		queuedJobs(100, embeds, jobs.TypeImageEmbed),
		queuedJobs(200, thumbs, jobs.TypeThumbnail)...,
	)
	q := newFakeQueue(pending...)
	track := newLiveTracker()
	reg := NewRegistry()
	// A sidecar-bound job holds its slot briefly waiting for a second one that
	// must never arrive; a thumbnail holds until every shared slot is busy.
	reg.Register(jobs.TypeImageEmbed, trackingHandler(track, jobs.TypeImageEmbed, 2, 50*time.Millisecond))
	reg.Register(jobs.TypeThumbnail, trackingHandler(track, jobs.TypeThumbnail, shared, time.Second))

	w := New(Config{
		Queue: q, Registry: reg, Concurrency: shared,
		PollInterval: time.Millisecond, StaleScanInterval: time.Hour, IDPrefix: "test",
	})
	runUntilDrained(t, w, q, embeds+thumbs)

	if got := track.peakOf(jobs.TypeImageEmbed); got != 1 {
		t.Errorf("peak concurrent image_embed = %d, want 1 (the sidecar must stay serialised)", got)
	}
	if got := track.peakOf(jobs.TypeThumbnail); got != shared {
		t.Errorf("peak concurrent thumbnail = %d, want %d", got, shared)
	}
}

// TestRun_typeConcurrencyOverride verifies an explicit per-type entry raises just
// that type's parallelism: image_embed runs three at a time when asked to, while
// face_detect — not named — keeps its one-at-a-time default.
func TestRun_typeConcurrencyOverride(t *testing.T) {
	t.Parallel()

	const (
		embedSlots = 3
		embeds     = 6
		faces      = 3
	)
	pending := append(
		queuedJobs(100, embeds, jobs.TypeImageEmbed),
		queuedJobs(200, faces, jobs.TypeFaceDetect)...,
	)
	q := newFakeQueue(pending...)
	track := newLiveTracker()
	reg := NewRegistry()
	reg.Register(jobs.TypeImageEmbed,
		trackingHandler(track, jobs.TypeImageEmbed, embedSlots, time.Second))
	reg.Register(jobs.TypeFaceDetect,
		trackingHandler(track, jobs.TypeFaceDetect, 2, 50*time.Millisecond))

	w := New(Config{
		Queue: q, Registry: reg, Concurrency: 1,
		TypeConcurrency: map[string]int{jobs.TypeImageEmbed: embedSlots},
		PollInterval:    time.Millisecond, StaleScanInterval: time.Hour, IDPrefix: "test",
	})
	runUntilDrained(t, w, q, embeds+faces)

	if got := track.peakOf(jobs.TypeImageEmbed); got != embedSlots {
		t.Errorf("peak concurrent image_embed = %d, want %d", got, embedSlots)
	}
	if got := track.peakOf(jobs.TypeFaceDetect); got != 1 {
		t.Errorf("peak concurrent face_detect = %d, want 1 (it was not overridden)", got)
	}
}

// TestRun_claimsOnlyItsOwnTypes verifies a pool never claims a job outside its
// type set: with no shared pool at all, an unhandled type is left in the queue
// rather than claimed and failed.
func TestRun_claimsOnlyItsOwnTypes(t *testing.T) {
	t.Parallel()

	q := newFakeQueue(
		jobs.Job{ID: 1, Type: jobs.TypeImageEmbed},
		jobs.Job{ID: 2, Type: jobs.TypeBackup},
	)
	reg := NewRegistry()
	reg.Register(jobs.TypeImageEmbed, NoopHandler)

	w := New(Config{
		Queue: q, Registry: reg, Concurrency: 2,
		PollInterval: time.Millisecond, StaleScanInterval: time.Hour, IDPrefix: "test",
	})
	runUntilDrained(t, w, q, 1)

	done, failed := q.snapshot()
	if len(done) != 1 || done[0] != 1 {
		t.Errorf("completed = %v, want [1]", done)
	}
	if len(failed) != 0 {
		t.Errorf("failed = %v, want empty (an unhandled type must stay queued)", failed)
	}
}

// TestRun_workerIDsAreUnique verifies every goroutine across every pool claims
// under its own worker id: the queue's ownership guard (and stale-lock recovery)
// depends on two workers never sharing one.
func TestRun_workerIDsAreUnique(t *testing.T) {
	t.Parallel()

	reg := NewRegistry()
	for _, jobType := range []string{jobs.TypeImageEmbed, jobs.TypeFaceDetect, jobs.TypeThumbnail} {
		reg.Register(jobType, NoopHandler)
	}
	w := New(Config{Queue: newFakeQueue(), Registry: reg, Concurrency: 3, IDPrefix: "test"})

	seen := make(map[string]bool)
	for _, p := range w.pools() {
		for i := range p.size {
			id := w.idPrefix + "-" + p.name + "-" + strconv.Itoa(i)
			if seen[id] {
				t.Errorf("duplicate worker id %q", id)
			}
			seen[id] = true
		}
	}
	if len(seen) != 5 { // one slot each for image_embed and face_detect, three shared
		t.Errorf("worker ids = %d, want 5", len(seen))
	}
}
