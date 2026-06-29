package backup

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"
)

// storedObject is one object held by the fake store.
type storedObject struct {
	data []byte
	etag string
}

// fakeStore is an in-memory ObjectStore for testing the orchestration without a
// live S3 service. It records the size argument passed to Put so tests can assert
// the streamed (-1) upload contract.
type fakeStore struct {
	mu        sync.Mutex
	objects   map[string]storedObject
	putSizes  map[string]int64
	removed   []string
	statErr   error
	listErr   error
	putErr    error
	openErr   error
	removeErr error
}

// newFakeStore returns an empty fakeStore seeded with the given objects.
func newFakeStore(seed map[string][]byte) *fakeStore {
	objects := make(map[string]storedObject, len(seed))
	for key, data := range seed {
		objects[key] = storedObject{data: data}
	}
	return &fakeStore{objects: objects, putSizes: map[string]int64{}}
}

// Stat returns the seeded object for key, or ok=false when absent.
func (f *fakeStore) Stat(_ context.Context, key string) (Object, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.statErr != nil {
		return Object{}, false, f.statErr
	}
	obj, ok := f.objects[key]
	if !ok {
		return Object{}, false, nil
	}
	return Object{Key: key, Size: int64(len(obj.data)), ETag: obj.etag}, true, nil
}

// Put reads reader to completion (proving the upload streams without a known
// size) and stores the bytes, recording the size argument it was given.
func (f *fakeStore) Put(_ context.Context, key string, reader io.Reader, size int64, _ string) error {
	if f.putErr != nil {
		return f.putErr
	}
	data, err := io.ReadAll(reader)
	if err != nil {
		return err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.objects[key] = storedObject{data: data}
	f.putSizes[key] = size
	return nil
}

// Open returns a reader over the stored object at key, or an error when absent.
func (f *fakeStore) Open(_ context.Context, key string) (io.ReadCloser, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.openErr != nil {
		return nil, f.openErr
	}
	obj, ok := f.objects[key]
	if !ok {
		return nil, fmt.Errorf("not found: %s", key)
	}
	return io.NopCloser(bytes.NewReader(obj.data)), nil
}

// List returns every stored object whose key begins with prefix.
func (f *fakeStore) List(_ context.Context, prefix string) ([]Object, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.listErr != nil {
		return nil, f.listErr
	}
	var objects []Object
	for key, obj := range f.objects {
		if strings.HasPrefix(key, prefix) {
			objects = append(objects, Object{Key: key, Size: int64(len(obj.data)), ETag: obj.etag})
		}
	}
	sort.Slice(objects, func(i, j int) bool { return objects[i].Key < objects[j].Key })
	return objects, nil
}

// Remove deletes the object at key, recording the removal.
func (f *fakeStore) Remove(_ context.Context, key string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.removeErr != nil {
		return f.removeErr
	}
	delete(f.objects, key)
	f.removed = append(f.removed, key)
	return nil
}

// fakeDumper returns a fixed dump payload, recording that it was called.
type fakeDumper struct {
	data     []byte
	startErr error
	closeErr error
	calls    int
}

// Dump returns a reader over the fixed payload.
func (d *fakeDumper) Dump(_ context.Context) (io.ReadCloser, error) {
	d.calls++
	if d.startErr != nil {
		return nil, d.startErr
	}
	return &fakeDumpReader{Reader: bytes.NewReader(d.data), closeErr: d.closeErr}, nil
}

// fakeDumpReader is a ReadCloser whose Close returns a configurable error,
// modelling a pg_dump process exit.
type fakeDumpReader struct {
	io.Reader
	closeErr error
}

// Close returns the configured close error.
func (r *fakeDumpReader) Close() error { return r.closeErr }

// fakeOriginals is an in-memory OriginalSource.
type fakeOriginals struct {
	files   map[string][]byte
	listErr error
	openErr error
}

// List returns each seeded file as a LocalOriginal.
func (o *fakeOriginals) List(_ context.Context) ([]LocalOriginal, error) {
	if o.listErr != nil {
		return nil, o.listErr
	}
	var originals []LocalOriginal
	for key, data := range o.files {
		originals = append(originals, LocalOriginal{Key: key, Size: int64(len(data))})
	}
	sort.Slice(originals, func(i, j int) bool { return originals[i].Key < originals[j].Key })
	return originals, nil
}

// Open returns a reader over the seeded file.
func (o *fakeOriginals) Open(_ context.Context, key string) (io.ReadCloser, error) {
	if o.openErr != nil {
		return nil, o.openErr
	}
	data, ok := o.files[key]
	if !ok {
		return nil, fmt.Errorf("not found: %s", key)
	}
	return io.NopCloser(bytes.NewReader(data)), nil
}

// newTestService builds a Service over the given fakes with retention.
func newTestService(t *testing.T, store *fakeStore, dumper *fakeDumper, originals *fakeOriginals, retention int) *Service {
	t.Helper()
	return New(Config{Objects: store, Originals: originals, Dumper: dumper, Retention: retention})
}

func TestDumpKey_format(t *testing.T) {
	t.Parallel()
	ts := time.Date(2026, 6, 27, 9, 8, 7, 0, time.FixedZone("CEST", 2*3600))
	got := dumpKey(ts)
	want := "db/kukatko-20260627T070807Z.dump"
	if got != want {
		t.Errorf("dumpKey(%v) = %q, want %q", ts, got, want)
	}
}

func TestService_Run_streamsDumpWithUnknownSize(t *testing.T) {
	t.Parallel()
	store := newFakeStore(nil)
	dumper := &fakeDumper{data: []byte("PGDMP-archive-bytes")}
	originals := &fakeOriginals{files: map[string][]byte{}}
	svc := newTestService(t, store, dumper, originals, 7)

	ts := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	res, err := svc.Run(context.Background(), ts)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	key := "db/kukatko-20260102T030405Z.dump"
	if res.DumpKey != key {
		t.Errorf("DumpKey = %q, want %q", res.DumpKey, key)
	}
	if got := store.putSizes[key]; got != streamUnknownSize {
		t.Errorf("dump Put size = %d, want %d (streamed)", got, streamUnknownSize)
	}
	if got := string(store.objects[key].data); got != "PGDMP-archive-bytes" {
		t.Errorf("dump body = %q, want streamed payload", got)
	}
}

func TestService_SyncOriginals_incrementalSkip(t *testing.T) {
	t.Parallel()
	// present.jpg already in the bucket at the same size -> skipped; changed.jpg
	// present but a different size -> re-uploaded; new.jpg absent -> uploaded.
	store := newFakeStore(map[string][]byte{
		"2026/01/present.jpg": []byte("same-bytes"),
		"2026/01/changed.jpg": []byte("old"),
	})
	originals := &fakeOriginals{files: map[string][]byte{
		"2026/01/present.jpg": []byte("same-bytes"),
		"2026/01/changed.jpg": []byte("brand-new-longer"),
		"2026/02/new.jpg":     []byte("fresh"),
	}}
	svc := newTestService(t, store, &fakeDumper{}, originals, 0)

	uploaded, skipped, err := svc.SyncOriginals(context.Background())
	if err != nil {
		t.Fatalf("SyncOriginals() error = %v", err)
	}
	if uploaded != 2 || skipped != 1 {
		t.Errorf("uploaded=%d skipped=%d, want uploaded=2 skipped=1", uploaded, skipped)
	}
	if got := string(store.objects["2026/01/changed.jpg"].data); got != "brand-new-longer" {
		t.Errorf("changed.jpg = %q, want re-uploaded content", got)
	}
	if _, ok := store.objects["2026/02/new.jpg"]; !ok {
		t.Errorf("new.jpg was not uploaded")
	}
}

func TestService_SyncOriginals_statError(t *testing.T) {
	t.Parallel()
	store := newFakeStore(nil)
	store.statErr = errors.New("boom")
	originals := &fakeOriginals{files: map[string][]byte{"a.jpg": []byte("x")}}
	svc := newTestService(t, store, &fakeDumper{}, originals, 0)

	if _, _, err := svc.SyncOriginals(context.Background()); err == nil {
		t.Fatal("SyncOriginals() error = nil, want stat error")
	}
}

func TestService_PruneDumps(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		dumps      []string
		retention  int
		wantPruned int
		wantKept   []string
	}{
		{
			name: "prunes oldest beyond retention",
			dumps: []string{
				"db/kukatko-20260101T000000Z.dump",
				"db/kukatko-20260102T000000Z.dump",
				"db/kukatko-20260103T000000Z.dump",
				"db/kukatko-20260104T000000Z.dump",
			},
			retention:  2,
			wantPruned: 2,
			wantKept: []string{
				"db/kukatko-20260103T000000Z.dump",
				"db/kukatko-20260104T000000Z.dump",
			},
		},
		{
			name:       "fewer than retention keeps all",
			dumps:      []string{"db/kukatko-20260101T000000Z.dump"},
			retention:  3,
			wantPruned: 0,
			wantKept:   []string{"db/kukatko-20260101T000000Z.dump"},
		},
		{
			name:       "retention disabled keeps all",
			dumps:      []string{"db/kukatko-20260101T000000Z.dump", "db/kukatko-20260102T000000Z.dump"},
			retention:  0,
			wantPruned: 0,
			wantKept:   []string{"db/kukatko-20260101T000000Z.dump", "db/kukatko-20260102T000000Z.dump"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			seed := map[string][]byte{}
			for _, key := range tt.dumps {
				seed[key] = []byte("d")
			}
			// An unrelated original must never be pruned.
			seed["2026/01/keep.jpg"] = []byte("o")
			store := newFakeStore(seed)
			svc := newTestService(t, store, &fakeDumper{}, &fakeOriginals{}, tt.retention)

			pruned, err := svc.PruneDumps(context.Background())
			if err != nil {
				t.Fatalf("PruneDumps() error = %v", err)
			}
			if pruned != tt.wantPruned {
				t.Errorf("pruned = %d, want %d", pruned, tt.wantPruned)
			}
			if _, ok := store.objects["2026/01/keep.jpg"]; !ok {
				t.Error("PruneDumps removed an original; it must only touch dumps")
			}
			for _, key := range tt.wantKept {
				if _, ok := store.objects[key]; !ok {
					t.Errorf("dump %s was pruned but should be kept", key)
				}
			}
		})
	}
}

func TestService_Run_full(t *testing.T) {
	t.Parallel()
	store := newFakeStore(map[string][]byte{
		"db/kukatko-20250101T000000Z.dump": []byte("old1"),
		"db/kukatko-20250102T000000Z.dump": []byte("old2"),
	})
	originals := &fakeOriginals{files: map[string][]byte{"2026/01/a.jpg": []byte("a")}}
	svc := newTestService(t, store, &fakeDumper{data: []byte("dump")}, originals, 1)

	res, err := svc.Run(context.Background(), time.Date(2026, 6, 27, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if res.OriginalsUploaded != 1 {
		t.Errorf("OriginalsUploaded = %d, want 1", res.OriginalsUploaded)
	}
	// 3 dumps now exist (2 old + new); retention 1 prunes the 2 oldest.
	if res.DumpsPruned != 2 {
		t.Errorf("DumpsPruned = %d, want 2", res.DumpsPruned)
	}
	status := svc.Status()
	if status.Running {
		t.Error("Status.Running = true after Run returned")
	}
	if status.LastResult == nil || status.LastResult.DumpKey != res.DumpKey {
		t.Errorf("Status.LastResult = %+v, want last run", status.LastResult)
	}
	if status.LastError != "" {
		t.Errorf("Status.LastError = %q, want empty", status.LastError)
	}
}

func TestService_Run_dumpFailureSkipsPrune(t *testing.T) {
	t.Parallel()
	// A failed dump must not prune existing dumps, or a backup failure could
	// destroy the only good backups.
	store := newFakeStore(map[string][]byte{
		"db/kukatko-20250101T000000Z.dump": []byte("old1"),
		"db/kukatko-20250102T000000Z.dump": []byte("old2"),
	})
	dumper := &fakeDumper{startErr: errors.New("pg_dump exploded")}
	svc := newTestService(t, store, dumper, &fakeOriginals{}, 1)

	if _, err := svc.Run(context.Background(), time.Now()); err == nil {
		t.Fatal("Run() error = nil, want dump failure")
	}
	if len(store.removed) != 0 {
		t.Errorf("pruned %v after a failed dump; must keep existing dumps", store.removed)
	}
	if svc.Status().LastError == "" {
		t.Error("Status.LastError empty after a failed run")
	}
}

func TestService_Run_alreadyRunning(t *testing.T) {
	t.Parallel()
	svc := newTestService(t, newFakeStore(nil), &fakeDumper{}, &fakeOriginals{}, 0)
	// Reserve a run manually, mimicking an in-progress backup.
	if !svc.reserve(time.Now()) {
		t.Fatal("reserve() = false on a fresh service")
	}
	if _, err := svc.Run(context.Background(), time.Now()); !errors.Is(err, ErrAlreadyRunning) {
		t.Errorf("Run() error = %v, want ErrAlreadyRunning", err)
	}
	if err := svc.Trigger(context.Background(), time.Now()); !errors.Is(err, ErrAlreadyRunning) {
		t.Errorf("Trigger() error = %v, want ErrAlreadyRunning", err)
	}
}

func TestService_Trigger_runsInBackground(t *testing.T) {
	t.Parallel()
	store := newFakeStore(nil)
	originals := &fakeOriginals{files: map[string][]byte{"2026/01/a.jpg": []byte("a")}}
	svc := newTestService(t, store, &fakeDumper{data: []byte("d")}, originals, 0)

	if err := svc.Trigger(context.Background(), time.Now()); err != nil {
		t.Fatalf("Trigger() error = %v", err)
	}
	waitFor(t, func() bool {
		status := svc.Status()
		return !status.Running && status.LastFinishedAt != nil
	})
	if got := svc.Status().LastResult; got == nil || got.OriginalsUploaded != 1 {
		t.Errorf("Status.LastResult = %+v, want a completed run", got)
	}
}

func TestNew_panicsOnNilDependency(t *testing.T) {
	t.Parallel()
	defer func() {
		if recover() == nil {
			t.Error("New() with nil dependencies did not panic")
		}
	}()
	New(Config{})
}

// waitFor polls cond until it is true or a short timeout elapses, failing the
// test on timeout. It is used to await a background run's completion.
func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("condition not met within timeout")
}
