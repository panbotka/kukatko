package thumb

import (
	"context"
	"errors"
	"io"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/panbotka/kukatko/internal/photos"
	"github.com/panbotka/kukatko/internal/storage"
)

// raceSize is the size the race tests fight over: the viewer's preview, the
// rendition a lone on-demand request asks for.
const raceSize = PreviewSize

// staticEdit is an EditResolver answering one fixed edit for every photo. Unlike
// fakeEdits it keeps no counter, so it is safe for the concurrent generations
// these tests run.
type staticEdit struct{ edit photos.Edit }

// GetEdit returns the fixed edit.
func (s staticEdit) GetEdit(context.Context, string) (photos.Edit, error) { return s.edit, nil }

// gatedStore is a publishing backend that counts how many times an original is
// materialized — one materialize per encode — and can hold the first one until
// released, so a test can pile concurrent callers up behind a render in flight.
// failArmed makes the next Put fail once, standing in for an upload rejected
// mid-race.
type gatedStore struct {
	*publishingFS
	materialized atomic.Int32
	failArmed    atomic.Bool
	gate         chan struct{} // closed by the test to release a held materialize; nil = never hold
	entered      chan struct{} // closed when the first materialize starts
	enterOnce    sync.Once
}

// newGatedStore wraps store as a counting publishing backend; hold makes the
// first materialize wait for release.
func newGatedStore(store *storage.FS, hold bool) *gatedStore {
	g := &gatedStore{publishingFS: newPublishingFS(store), entered: make(chan struct{})}
	if hold {
		g.gate = make(chan struct{})
	}
	return g
}

// Materialize counts the call, reports the first one and, when the store holds,
// waits for the test to release it before materializing.
func (g *gatedStore) Materialize(ctx context.Context, relPath string) (string, func(), error) {
	g.materialized.Add(1)
	g.enterOnce.Do(func() { close(g.entered) })
	if g.gate != nil {
		<-g.gate
	}
	return g.publishingFS.Materialize(ctx, relPath)
}

// Put fails once when armed, and otherwise publishes through the recording fake.
func (g *gatedStore) Put(ctx context.Context, src io.Reader, file storage.StoredFile) error {
	if g.failArmed.CompareAndSwap(true, false) {
		return errors.New("simulated put failure")
	}
	return g.publishingFS.Put(ctx, src, file)
}

// hookedLock is a LocationLock that runs hook once, inside the first publish,
// before letting the publish proceed — the exact moment between a writer
// installing its cache file and uploading it.
type hookedLock struct {
	once sync.Once
	hook func()
}

// Publish runs the hook on the first call, then the publish itself.
func (h *hookedLock) Publish(
	_ context.Context, _, _ string, publish func(filePath string, found bool) error,
) error {
	h.once.Do(h.hook)
	return publish("", false)
}

// raceFixture is a photo on a publishing backend plus a second, independent
// thumbnailer over the same cache and store standing in for the thumbnail job.
// The job renders the photo turned a quarter, so its bytes differ from the
// on-demand render's and a test can tell whose file sits at the cache path.
type raceFixture struct {
	store *gatedStore
	photo photos.Photo
	cache string
	job   *Thumbnailer
}

// newRaceFixture stores an 800×600 JPEG on a gated publishing store.
func newRaceFixture(t *testing.T, hold bool) *raceFixture {
	t.Helper()
	root := t.TempDir()
	fs, err := storage.NewFS(filepath.Join(root, "originals"))
	if err != nil {
		t.Fatalf("storage.NewFS: %v", err)
	}
	photo := storeJPEG(t, fs, 800, 600, 1)
	photo.UID = "ph-race"
	store := newGatedStore(fs, hold)
	cache := filepath.Join(root, "cache")
	job := New(store, cache, WithEdits(staticEdit{edit: photos.Edit{Rotation: 90}}))
	return &raceFixture{store: store, photo: photo, cache: cache, job: job}
}

// readAll opens the photo's size through th and returns its byte count.
func readAll(ctx context.Context, th *Thumbnailer, photo photos.Photo, size string) (int, error) {
	reader, err := th.OpenOrGenerate(ctx, photo, size)
	if err != nil {
		return 0, err
	}
	defer func() { _ = reader.Close() }()
	data, err := io.ReadAll(reader)
	return len(data), err
}

// TestOpenOrGenerate_concurrentRendersShareOneEncode forces a burst of first
// requests for one uncached size while the first render is still in flight, and
// proves every request succeeds off a single encode — the others wait for it
// instead of encoding and publishing the same key against each other.
func TestOpenOrGenerate_concurrentRendersShareOneEncode(t *testing.T) {
	t.Parallel()
	fx := newRaceFixture(t, true)
	th := New(fx.store, fx.cache)

	const callers = 8
	errs := make(chan error, callers)
	var wg sync.WaitGroup
	for range callers {
		wg.Go(func() {
			n, err := readAll(context.Background(), th, fx.photo, raceSize)
			if err == nil && n == 0 {
				err = errors.New("empty thumbnail")
			}
			errs <- err
		})
	}
	<-fx.store.entered
	// Give the remaining callers the time to miss the cache and join the render
	// held in flight; one that comes later finds the file and encodes nothing
	// either, so the assertion below does not depend on this pause.
	time.Sleep(50 * time.Millisecond)
	close(fx.store.gate)
	wg.Wait()
	close(errs)

	for err := range errs {
		if err != nil {
			t.Errorf("concurrent render: %v", err)
		}
	}
	if got := fx.store.materialized.Load(); got != 1 {
		t.Errorf("originals materialized %d times for %d concurrent renders, want 1", got, callers)
	}
	if puts, _ := fx.store.counts(); puts != 1 {
		t.Errorf("Put called %d times, want 1 (one encode, one upload)", puts)
	}
}

// TestOpenOrGenerate_waiterCancelLeavesSharedRender proves one caller giving up
// on a shared render neither fails it nor the callers still waiting: the render
// runs on a context detached from whoever started it.
func TestOpenOrGenerate_waiterCancelLeavesSharedRender(t *testing.T) {
	t.Parallel()
	fx := newRaceFixture(t, true)
	th := New(fx.store, fx.cache)

	ctx, cancel := context.WithCancel(context.Background())
	first := make(chan error, 1)
	go func() {
		_, err := readAll(ctx, th, fx.photo, raceSize)
		first <- err
	}()
	<-fx.store.entered
	second := make(chan error, 1)
	go func() {
		_, err := readAll(context.Background(), th, fx.photo, raceSize)
		second <- err
	}()
	time.Sleep(50 * time.Millisecond)

	cancel()
	if err := <-first; !errors.Is(err, context.Canceled) {
		t.Errorf("cancelled caller err = %v, want context.Canceled", err)
	}
	close(fx.store.gate)
	if err := <-second; err != nil {
		t.Errorf("caller still waiting on the shared render: %v", err)
	}
}

// TestOpenOrGenerate_renderRacingJobRegeneration lets the thumbnail job rebuild
// the photo's sizes — different bytes, the photo turned a quarter — in the
// window between an on-demand render installing its cache file and uploading it.
// Both must succeed: the render uploads the file it installed, never whatever
// the cache path names by then, so its digest and its stream cannot disagree.
func TestOpenOrGenerate_renderRacingJobRegeneration(t *testing.T) {
	t.Parallel()
	fx := newRaceFixture(t, false)
	var jobErr error
	lock := &hookedLock{hook: func() {
		_, jobErr = fx.job.RegenerateAll(context.Background(), fx.photo)
	}}
	th := New(fx.store, fx.cache, WithLocation(lock))

	n, err := readAll(context.Background(), th, fx.photo, raceSize)
	if err != nil {
		t.Fatalf("render racing a regeneration: %v", err)
	}
	if n == 0 {
		t.Error("render racing a regeneration returned an empty thumbnail")
	}
	if jobErr != nil {
		t.Fatalf("regeneration racing a render: %v", jobErr)
	}
	abs, err := th.Path("", fx.photo.FileHash, raceSize)
	if err != nil {
		t.Fatalf("Path: %v", err)
	}
	if !fileExists(abs) {
		t.Errorf("cache file %s missing after both writers succeeded", abs)
	}
}

// TestGenerate_failedPublishKeepsAnotherWritersFile makes an on-demand render's
// upload fail after the thumbnail job has renamed its own rendition over the
// render's cache file. The render's clean-up must remove only the file it
// installed: the job's thumbnail stays, rather than the render's failure
// deleting a size somebody else just produced.
func TestGenerate_failedPublishKeepsAnotherWritersFile(t *testing.T) {
	t.Parallel()
	fx := newRaceFixture(t, false)
	var jobErr error
	lock := &hookedLock{hook: func() {
		_, jobErr = fx.job.RegenerateAll(context.Background(), fx.photo)
		fx.store.failArmed.Store(true)
	}}
	th := New(fx.store, fx.cache, WithLocation(lock))

	if _, err := th.Generate(context.Background(), fx.photo, raceSize); err == nil {
		t.Fatal("Generate: expected the armed publish failure, got nil")
	}
	if jobErr != nil {
		t.Fatalf("regeneration: %v", jobErr)
	}
	abs, err := th.Path("", fx.photo.FileHash, raceSize)
	if err != nil {
		t.Fatalf("Path: %v", err)
	}
	if !fileExists(abs) {
		t.Fatalf("the job's cache file %s was removed by another writer's failed publish", abs)
	}
	// The surviving file is the job's: the photo turned a quarter, 540×720.
	if w, h := jpegBounds(t, abs); w != 540 || h != 720 {
		t.Errorf("cache file is %dx%d, want the job's rotated 540x720", w, h)
	}
}

// TestOpenOrGenerate_stressRendersAndRegenerations runs on-demand renders of
// several sizes alongside forced regenerations that write different bytes to the
// same keys, and requires every one of them to succeed.
func TestOpenOrGenerate_stressRendersAndRegenerations(t *testing.T) {
	t.Parallel()
	fx := newRaceFixture(t, false)
	th := New(fx.store, fx.cache)
	sizes := []string{raceSize, GridSize, AvatarSize}

	const rounds = 4
	var wg sync.WaitGroup
	errs := make(chan error, rounds*(len(sizes)*2+1))
	for range rounds {
		for _, size := range sizes {
			for range 2 {
				wg.Go(func() {
					_, err := readAll(context.Background(), th, fx.photo, size)
					errs <- err
				})
			}
		}
		wg.Go(func() {
			_, err := fx.job.RegenerateAll(context.Background(), fx.photo)
			errs <- err
		})
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Errorf("concurrent render/regeneration: %v", err)
		}
	}
}
