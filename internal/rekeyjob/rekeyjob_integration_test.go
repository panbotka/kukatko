//go:build integration

package rekeyjob

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"os"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/panbotka/kukatko/internal/audit"
	"github.com/panbotka/kukatko/internal/database"
	"github.com/panbotka/kukatko/internal/database/dbtest"
	"github.com/panbotka/kukatko/internal/jobs"
	"github.com/panbotka/kukatko/internal/project"
	"github.com/panbotka/kukatko/internal/rekey"
)

// memStore is an in-memory object store: enough of one for a storage move.
type memStore struct {
	mu      sync.Mutex
	objects map[string][]byte
}

// memInfo is the os.FileInfo of one object.
type memInfo struct {
	name string
	size int64
}

func (i memInfo) Name() string       { return i.name }
func (i memInfo) Size() int64        { return i.size }
func (i memInfo) Mode() fs.FileMode  { return 0o644 }
func (i memInfo) ModTime() time.Time { return time.Time{} }
func (i memInfo) IsDir() bool        { return false }
func (i memInfo) Sys() any           { return nil }

// Copy duplicates the object at from to to.
func (m *memStore) Copy(_ context.Context, from, to string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	data, ok := m.objects[from]
	if !ok {
		return fmt.Errorf("copy %s: %w", from, os.ErrNotExist)
	}
	m.objects[to] = slices.Clone(data)
	return nil
}

// Stat reports the object at key.
func (m *memStore) Stat(_ context.Context, key string) (os.FileInfo, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	data, ok := m.objects[key]
	if !ok {
		return nil, fmt.Errorf("stat %s: %w", key, os.ErrNotExist)
	}
	return memInfo{name: key, size: int64(len(data))}, nil
}

// Open opens the object at key.
func (m *memStore) Open(_ context.Context, key string) (io.ReadCloser, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	data, ok := m.objects[key]
	if !ok {
		return nil, fmt.Errorf("open %s: %w", key, os.ErrNotExist)
	}
	return io.NopCloser(bytes.NewReader(slices.Clone(data))), nil
}

// Delete removes the object at key.
func (m *memStore) Delete(_ context.Context, key string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.objects[key]; !ok {
		return fmt.Errorf("delete %s: %w", key, os.ErrNotExist)
	}
	delete(m.objects, key)
	return nil
}

// KeysWithPrefix yields every key under prefix in order.
func (m *memStore) KeysWithPrefix(_ context.Context, prefix string, yield func(string) error) error {
	m.mu.Lock()
	keys := make([]string, 0, len(m.objects))
	for key := range m.objects {
		if strings.HasPrefix(key, prefix) {
			keys = append(keys, key)
		}
	}
	m.mu.Unlock()
	slices.Sort(keys)
	for _, key := range keys {
		if err := yield(key); err != nil {
			return err
		}
	}
	return nil
}

// noJobs drops the rewrites Finish schedules.
type noJobs struct{}

func (noJobs) EnqueueFamilyExport(context.Context, string) error { return nil }
func (noJobs) EnqueueSidecar(context.Context, string) error      { return nil }

// harness is one test's database, store, move and job handler.
type harness struct {
	t     *testing.T
	db    *database.DB
	mem   *memStore
	mover *rekey.Service
	svc   *Service
}

// newHarness truncates the test database and wires the job handler over a real
// move of an in-memory store, taking perSecond photos a pass.
func newHarness(t *testing.T, perSecond int) *harness {
	t.Helper()
	db := dbtest.New(t)
	dbtest.TruncateAll(t, db)
	mem := &memStore{objects: map[string][]byte{}}
	discard := slog.New(slog.NewTextHandler(io.Discard, nil))
	mover := rekey.New(rekey.Config{Pool: db.Pool(), Store: mem, Jobs: noJobs{}, Logger: discard})
	svc := New(Config{Mover: mover, PerSecond: perSecond, Logger: discard})
	return &harness{t: t, db: db, mem: mem, mover: mover, svc: svc}
}

// seedPhotos puts n photos of the bootstrap project into the catalogue and the
// store, in the root layout.
func (h *harness) seedPhotos(n int) {
	h.t.Helper()
	for i := range n {
		uid := fmt.Sprintf("ph%04d", i)
		key := fmt.Sprintf("2023/01/%s.jpg", uid)
		content := []byte("bytes of " + uid)
		sum := sha256.Sum256(content)
		hash := hex.EncodeToString(sum[:])
		h.exec(`INSERT INTO photos (uid, file_hash, file_path, file_name, file_size, file_mime, project_id)
			VALUES ($1, $2, $3, $4, $5, 'image/jpeg', $6)`, uid, hash, key, uid+".jpg", len(content), project.BootstrapUID)
		h.exec(`INSERT INTO photo_files (photo_uid, file_path, file_hash, file_size, file_mime, is_primary, role)
			VALUES ($1, $2, $3, $4, 'image/jpeg', true, 'original')`, uid, key, hash, len(content))
		h.mem.objects[key] = content
	}
}

// exec runs one statement, failing the test on error.
func (h *harness) exec(sql string, args ...any) {
	h.t.Helper()
	if _, err := h.db.Pool().Exec(h.t.Context(), sql, args...); err != nil {
		h.t.Fatalf("exec %q: %v", sql, err)
	}
}

// job is the storage_rekey job of the bootstrap project.
func (h *harness) job() jobs.Job {
	return jobs.Job{ID: 1, Type: jobs.TypeStorageRekey,
		Payload: []byte(`{"project_uid":"` + project.BootstrapUID + `"}`)}
}

// status reads the bootstrap project's run.
func (h *harness) status() rekey.Run {
	h.t.Helper()
	run, err := h.mover.Status(h.t.Context(), project.BootstrapUID)
	if err != nil {
		h.t.Fatalf("Status: %v", err)
	}
	return run
}

// TestHandle_movesAProjectAcrossPasses verifies the job advances the run one
// batch per pass, asking to come back after each, finishes the run when the
// pass is over and then stops asking — and that a paused run is only re-read.
func TestHandle_movesAProjectAcrossPasses(t *testing.T) {
	h := newHarness(t, 10)
	ctx := t.Context()
	h.seedPhotos(25)
	if _, err := h.mover.Start(ctx, audit.Meta{}, project.BootstrapUID); err != nil {
		t.Fatalf("Start: %v", err)
	}

	for pass, want := range []int64{10, 20} {
		if _, deferred := deferral(h.svc.Handle(ctx, h.job())); !deferred {
			t.Fatalf("pass %d did not ask to come back", pass+1)
		}
		if got := h.status().DonePhotos; got != want {
			t.Fatalf("after pass %d: %d photos moved, want %d", pass+1, got, want)
		}
	}

	if _, err := h.mover.Pause(ctx, audit.Meta{}, project.BootstrapUID); err != nil {
		t.Fatalf("Pause: %v", err)
	}
	delay, deferred := deferral(h.svc.Handle(ctx, h.job()))
	if !deferred || delay != PausedRecheck {
		t.Fatalf("a paused run deferred %s (%v), want %s", delay, deferred, PausedRecheck)
	}
	if got := h.status().DonePhotos; got != 20 {
		t.Fatalf("a paused pass moved photos: %d done, want 20", got)
	}
	if _, err := h.mover.Resume(ctx, audit.Meta{}, project.BootstrapUID); err != nil {
		t.Fatalf("Resume: %v", err)
	}

	if _, deferred := deferral(h.svc.Handle(ctx, h.job())); !deferred {
		t.Fatal("the last batch did not ask to come back")
	}
	if got := h.status().DonePhotos; got != 25 {
		t.Fatalf("after the last batch: %d photos moved, want 25", got)
	}
	if err := h.svc.Handle(ctx, h.job()); err != nil {
		t.Fatalf("the pass that finishes = %v, want completion", err)
	}
	run := h.status()
	if run.State != rekey.StateDone {
		t.Fatalf("run after the end = %s, want done", run.State)
	}
	if err := h.svc.Handle(ctx, h.job()); err != nil {
		t.Errorf("a pass over a done run = %v, want completion without coming back", err)
	}
	for key := range h.mem.objects {
		if !strings.HasPrefix(key, run.TargetPrefix) {
			t.Errorf("object %s left in the old layout", key)
		}
	}
}

// TestHandle_noRun verifies a job for a project that was never moved completes.
func TestHandle_noRun(t *testing.T) {
	h := newHarness(t, 10)
	if err := h.svc.Handle(t.Context(), h.job()); err != nil {
		t.Errorf("Handle with no run = %v, want completion", err)
	}
}
