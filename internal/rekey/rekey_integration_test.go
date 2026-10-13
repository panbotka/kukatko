//go:build integration

package rekey

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/panbotka/kukatko/internal/audit"
	"github.com/panbotka/kukatko/internal/database"
	"github.com/panbotka/kukatko/internal/database/dbtest"
	"github.com/panbotka/kukatko/internal/project"
	"github.com/panbotka/kukatko/internal/storekeys"
	"github.com/panbotka/kukatko/internal/thumb"
)

// bootstrapTarget is where the bootstrap project's objects move.
var bootstrapTarget = project.StoragePrefixFor(project.BootstrapUID)

// fakeJobs records what Finish schedules.
type fakeJobs struct {
	mu       sync.Mutex
	families []string
	sidecars []string
}

// EnqueueFamilyExport records the project.
func (f *fakeJobs) EnqueueFamilyExport(_ context.Context, projectUID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.families = append(f.families, projectUID)
	return nil
}

// EnqueueSidecar records the photo.
func (f *fakeJobs) EnqueueSidecar(_ context.Context, photoUID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sidecars = append(f.sidecars, photoUID)
	return nil
}

// has reports whether key holds an object.
func (m *memStore) has(key string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	_, ok := m.objects[key]
	return ok
}

// errTransient is a per-object failure that is not systemic.
var errTransient = errors.New("fake: transient failure")

// harness is one test's database, fake store and service.
type harness struct {
	t    *testing.T
	db   *database.DB
	mem  *memStore
	jobs *fakeJobs
	svc  *Service
}

// newHarness truncates the test database and wires a service over a fresh fake.
func newHarness(t *testing.T) *harness {
	t.Helper()
	db := dbtest.New(t)
	dbtest.TruncateAll(t, db)
	mem := newMemStore()
	jobs := &fakeJobs{}
	svc := New(Config{
		Pool: db.Pool(), Store: mem, Jobs: jobs,
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	return &harness{t: t, db: db, mem: mem, jobs: jobs, svc: svc}
}

// seeded is one photo the harness put into the catalogue and the store.
type seeded struct {
	uid      string
	filePath string
	content  []byte
	derived  []string
}

// seedPhoto inserts a photo of the bootstrap project at filePath with its file
// row, stores its original, two thumbnail sizes and its sidecar, and — with
// video set — one streaming rendition of three segments.
func (h *harness) seedPhoto(uid, filePath string, video bool) seeded {
	h.t.Helper()
	content := []byte("bytes of " + uid)
	hash := sha256Hex(content)
	h.exec(`INSERT INTO photos (uid, file_hash, file_path, file_name, file_size, file_mime)
		VALUES ($1, $2, $3, $4, $5, 'image/jpeg')`, uid, hash, filePath, uid+".jpg", len(content))
	h.exec(`INSERT INTO photo_files (photo_uid, file_path, file_hash, file_size, file_mime, is_primary, role)
		VALUES ($1, $2, $3, $4, 'image/jpeg', true, 'original')`, uid, filePath, hash, len(content))
	h.mem.put(filePath, content)
	photo := seeded{uid: uid, filePath: filePath, content: content}
	for _, size := range thumb.SizeNames()[:2] {
		key, err := thumb.RelPath("", hash, size)
		if err != nil {
			h.t.Fatalf("thumbnail key: %v", err)
		}
		photo.derived = append(photo.derived, key)
	}
	photo.derived = append(photo.derived, "sidecars/"+filePath+".yml")
	if video {
		h.exec(`INSERT INTO photo_hls_renditions
			(photo_uid, rendition, playlist, width, height, bandwidth, codecs, segment_count, duration_ms)
			VALUES ($1, '1080p', $2, 1920, 1080, 5000000, 'avc1.640028', 3, 14500)`, uid, testPlaylist)
		for _, name := range []string{"init.mp4", "00000.m4s", "00001.m4s", "00002.m4s"} {
			photo.derived = append(photo.derived, "hls/"+hash+"/1080p/"+name)
		}
	}
	for _, key := range photo.derived {
		h.mem.put(key, []byte("derived "+key))
	}
	return photo
}

// exec runs one statement, failing the test on error.
func (h *harness) exec(sql string, args ...any) {
	h.t.Helper()
	if _, err := h.db.Pool().Exec(h.t.Context(), sql, args...); err != nil {
		h.t.Fatalf("exec %q: %v", sql, err)
	}
}

// start starts the bootstrap project's move.
func (h *harness) start() Run {
	h.t.Helper()
	run, err := h.svc.Start(h.t.Context(), audit.Meta{}, project.BootstrapUID)
	if err != nil {
		h.t.Fatalf("Start: %v", err)
	}
	return run
}

// checkInvariant fails the test unless every photo row points at an existing
// object holding file_size bytes.
func (h *harness) checkInvariant() {
	h.t.Helper()
	rows, err := h.db.Pool().Query(h.t.Context(), `SELECT uid, file_path, file_size FROM photos`)
	if err != nil {
		h.t.Fatalf("listing photos: %v", err)
	}
	defer rows.Close()
	for rows.Next() {
		var (
			uid, key string
			size     int64
		)
		if err := rows.Scan(&uid, &key, &size); err != nil {
			h.t.Fatalf("scanning photo: %v", err)
		}
		data, ok := h.mem.get(key)
		if !ok {
			h.t.Errorf("invariant: photo %s points at %s, which does not exist", uid, key)
			continue
		}
		if int64(len(data)) != size {
			h.t.Errorf("invariant: photo %s at %s holds %d bytes, want %d", uid, key, len(data), size)
		}
	}
	if err := rows.Err(); err != nil {
		h.t.Fatalf("iterating photos: %v", err)
	}
}

// stepToEnd steps run until the pass is over, checking the invariant after
// every step, and returns the last step's error (nil when the pass ended).
func (h *harness) stepToEnd(run Run, batch int) error {
	h.t.Helper()
	for range 1000 {
		_, done, err := h.svc.Step(h.t.Context(), run, batch)
		h.checkInvariant()
		if err != nil {
			return err
		}
		if done {
			return nil
		}
	}
	h.t.Fatal("the pass never ended")
	return nil
}

// status reads the bootstrap project's latest run.
func (h *harness) status() Run {
	h.t.Helper()
	run, err := h.svc.Status(h.t.Context(), project.BootstrapUID)
	if err != nil {
		h.t.Fatalf("Status: %v", err)
	}
	return run
}

// photoPath reads a photo's file_path and its primary file row's path.
func (h *harness) photoPath(uid string) (string, string) {
	h.t.Helper()
	var photoPath, filePath string
	err := h.db.Pool().QueryRow(h.t.Context(), `SELECT p.file_path, f.file_path FROM photos p
		JOIN photo_files f ON f.photo_uid = p.uid AND f.is_primary WHERE p.uid = $1`, uid).Scan(&photoPath, &filePath)
	if err != nil {
		h.t.Fatalf("reading paths of %s: %v", uid, err)
	}
	return photoPath, filePath
}

// assertMoved fails the test unless photo's row, file row, original and
// derived objects are all under the bootstrap target and nothing is left at
// the old keys.
func (h *harness) assertMoved(photo seeded) {
	h.t.Helper()
	want := bootstrapTarget + photo.filePath
	if got, file := h.photoPath(photo.uid); got != want || file != want {
		h.t.Errorf("photo %s paths = %s / %s, want %s", photo.uid, got, file, want)
	}
	if data, _ := h.mem.get(want); string(data) != string(photo.content) {
		h.t.Errorf("new original %s = %q, want the photo's bytes", want, data)
	}
	for _, key := range append([]string{photo.filePath}, photo.derived...) {
		if h.mem.has(key) {
			h.t.Errorf("old key %s still exists", key)
		}
		if key != photo.filePath && !h.mem.has(bootstrapTarget+key) {
			h.t.Errorf("derived %s was not moved under the prefix", key)
		}
	}
}

func TestService_fullRun(t *testing.T) {
	h := newHarness(t)
	photos := []seeded{
		h.seedPhoto("ph1", "2023/01/a.jpg", false),
		h.seedPhoto("ph2", "2023/02/b.jpg", false),
		h.seedPhoto("ph3", "2024/05/clip.mp4", true),
	}
	h.mem.put(storekeys.FamiliesKey, []byte("families"))
	h.mem.put("sidecars/1999/01/orphan.jpg.yml", []byte("orphan sidecar"))

	run := h.start()
	if run.State != StateRunning || run.TotalPhotos != 3 || run.TargetPrefix != bootstrapTarget {
		t.Fatalf("started run = %+v, want running over 3 photos into %s", run, bootstrapTarget)
	}
	var wantBytes int64
	for _, photo := range photos {
		wantBytes += int64(len(photo.content))
	}
	if run.TotalBytes != wantBytes {
		t.Errorf("total bytes = %d, want %d", run.TotalBytes, wantBytes)
	}
	// An upload after Start lands in the new layout and is not part of the move.
	h.exec(`INSERT INTO photos (uid, file_hash, file_path, file_name, file_size, file_mime)
		VALUES ('ph9', $1, $2, 'new.jpg', 3, 'image/jpeg')`, sha256Hex([]byte("new")), bootstrapTarget+"2026/10/new.jpg")
	h.mem.put(bootstrapTarget+"2026/10/new.jpg", []byte("new"))

	if err := h.stepToEnd(run, 2); err != nil {
		t.Fatalf("stepping: %v", err)
	}
	for _, photo := range photos {
		h.assertMoved(photo)
	}
	finished, err := h.svc.Finish(t.Context(), run)
	if err != nil {
		t.Fatalf("Finish: %v", err)
	}
	if finished.State != StateDone || finished.FinishedAt == nil || finished.DonePhotos != 3 ||
		finished.DoneBytes != wantBytes || finished.ErrorCount != 0 {
		t.Errorf("finished run = %+v, want done with 3 photos and no errors", finished)
	}
	if !slices.Equal(h.jobs.families, []string{project.BootstrapUID}) || len(h.jobs.sidecars) != 4 {
		t.Errorf("scheduled families %v, sidecars %v; want the project and all 4 photos", h.jobs.families, h.jobs.sidecars)
	}
	if h.mem.has(storekeys.FamiliesKey) || !h.mem.has(bootstrapTarget+storekeys.FamiliesKey) {
		t.Error("families.yaml was not moved under the prefix")
	}
	if !h.mem.has("sidecars/1999/01/orphan.jpg.yml") {
		t.Error("a root sidecar without a replacement was deleted")
	}
	for _, key := range h.mem.keys() {
		if !strings.HasPrefix(key, bootstrapTarget) && key != "sidecars/1999/01/orphan.jpg.yml" {
			t.Errorf("root key %s left behind", key)
		}
	}
	// Finishing a done run again is a no-op.
	if again, err := h.svc.Finish(t.Context(), finished); err != nil || again.State != StateDone {
		t.Errorf("second Finish = %+v, %v; want done", again, err)
	}
}

func TestService_crashAfterCopyBeforeCommit(t *testing.T) {
	h := newHarness(t)
	photo := h.seedPhoto("ph1", "2023/01/a.jpg", true)
	run := h.start()
	h.svc.hook = func(stage string) error {
		if stage == stageCopied {
			return errCrash
		}
		return nil
	}

	if _, _, err := h.svc.Step(t.Context(), run, 5); !errors.Is(err, errCrash) {
		t.Fatalf("crashing Step = %v, want the crash", err)
	}
	h.checkInvariant()
	if got, _ := h.photoPath(photo.uid); got != photo.filePath {
		t.Fatalf("row moved before the commit: %s", got)
	}
	if !h.mem.has(photo.filePath) || !h.mem.has(bootstrapTarget+photo.filePath) {
		t.Fatal("after the crash both copies must exist")
	}

	h.svc.hook = nil
	if err := h.stepToEnd(run, 5); err != nil {
		t.Fatalf("resumed run: %v", err)
	}
	h.assertMoved(photo)
	if got := h.status(); got.DonePhotos != 1 || got.ErrorCount != 0 {
		t.Errorf("run after resume = %+v, want 1 photo done and no errors", got)
	}
}

func TestService_crashAfterCommitBeforeDelete(t *testing.T) {
	h := newHarness(t)
	first := h.seedPhoto("ph1", "2023/01/a.jpg", true)
	second := h.seedPhoto("ph2", "2023/02/b.jpg", false)
	run := h.start()
	h.svc.hook = func(stage string) error {
		if stage == stageCommitted {
			return errCrash
		}
		return nil
	}

	if _, _, err := h.svc.Step(t.Context(), run, 5); !errors.Is(err, errCrash) {
		t.Fatalf("crashing Step = %v, want the crash", err)
	}
	h.checkInvariant()
	pending := h.status()
	if pending.PendingPhotoUID != first.uid || pending.PendingOldKey != first.filePath || pending.DonePhotos != 0 {
		t.Fatalf("run after the crash = %+v, want ph1 pending and nothing counted", pending)
	}
	if !h.mem.has(first.filePath) {
		t.Fatal("the old original was deleted before the crash point")
	}
	if _, err := h.svc.Finish(t.Context(), run); !errors.Is(err, ErrPendingCleanup) {
		t.Errorf("Finish with a pending photo = %v, want ErrPendingCleanup", err)
	}

	h.svc.hook = nil
	if err := h.stepToEnd(run, 5); err != nil {
		t.Fatalf("resumed run: %v", err)
	}
	h.assertMoved(first)
	h.assertMoved(second)
	got := h.status()
	if got.DonePhotos != 2 || got.PendingPhotoUID != "" || got.ErrorCount != 0 {
		t.Errorf("run after resume = %+v, want 2 photos done, nothing pending", got)
	}
}

func TestService_verificationFailureKeepsTheOriginal(t *testing.T) {
	h := newHarness(t)
	bad := h.seedPhoto("ph1", "2023/01/a.jpg", false)
	good := h.seedPhoto("ph2", "2023/02/b.jpg", false)
	h.mem.corrupt[bootstrapTarget+bad.filePath] = true
	run := h.start()

	if err := h.stepToEnd(run, 5); err != nil {
		t.Fatalf("stepping: %v", err)
	}
	h.assertMoved(good)
	if got, _ := h.photoPath(bad.uid); got != bad.filePath {
		t.Errorf("a photo whose copy failed verification was moved to %s", got)
	}
	if data, _ := h.mem.get(bad.filePath); string(data) != string(bad.content) {
		t.Error("the original of a photo whose copy failed verification is gone")
	}
	status := h.status()
	if status.ErrorCount != 1 || len(status.Errors) != 1 || status.Errors[0].Key != bad.filePath ||
		!strings.Contains(status.Errors[0].Message, ErrVerifyFailed.Error()) {
		t.Errorf("run = %+v, want one verification error for %s", status, bad.filePath)
	}

	failed, err := h.svc.Finish(t.Context(), run)
	if !errors.Is(err, ErrPhotosRemain) || failed.State != StateFailed {
		t.Fatalf("Finish with a photo left = %+v, %v; want failed, ErrPhotosRemain", failed, err)
	}
	if h.status().State != StateFailed {
		t.Fatal("the run is not failed after Finish found a photo left")
	}

	delete(h.mem.corrupt, bootstrapTarget+bad.filePath)
	retried, err := h.svc.Retry(t.Context(), project.BootstrapUID)
	if err != nil || retried.State != StateRunning || retried.LastKey != "" {
		t.Fatalf("Retry = %+v, %v; want running with the cursor reset", retried, err)
	}
	if err := h.stepToEnd(run, 5); err != nil {
		t.Fatalf("retried pass: %v", err)
	}
	h.assertMoved(bad)
	if done, err := h.svc.Finish(t.Context(), run); err != nil || done.State != StateDone {
		t.Errorf("Finish after the retry = %+v, %v; want done", done, err)
	}
}

func TestService_errorThresholdFailsTheRun(t *testing.T) {
	h := newHarness(t)
	for i := range MaxErrors + 2 {
		key := fmt.Sprintf("2023/01/%04d.jpg", i)
		uid := fmt.Sprintf("ph%04d", i)
		content := []byte("bytes of " + uid)
		h.exec(`INSERT INTO photos (uid, file_hash, file_path, file_name, file_size, file_mime)
			VALUES ($1, $2, $3, 'x.jpg', $4, 'image/jpeg')`, uid, sha256Hex(content), key, len(content))
		h.mem.put(key, content)
		h.mem.failCopy[bootstrapTarget+key] = errTransient
	}
	run := h.start()

	err := h.stepToEnd(run, 500)
	if !errors.Is(err, ErrRunFailed) {
		t.Fatalf("stepping = %v, want ErrRunFailed", err)
	}
	got := h.status()
	if got.State != StateFailed || got.ErrorCount != MaxErrors+1 || len(got.Errors) != MaxErrorRing {
		t.Errorf("run = state %s, %d errors, ring %d; want failed, %d, %d",
			got.State, got.ErrorCount, len(got.Errors), MaxErrors+1, MaxErrorRing)
	}
	if last := got.Errors[len(got.Errors)-1]; last.Key != fmt.Sprintf("2023/01/%04d.jpg", MaxErrors) {
		t.Errorf("newest error = %+v, want the %dth photo", last, MaxErrors+1)
	}
	if _, _, err := h.svc.Step(t.Context(), run, 5); !errors.Is(err, ErrNotRunning) {
		t.Errorf("Step of a failed run = %v, want ErrNotRunning", err)
	}
}

func TestService_pauseAndResume(t *testing.T) {
	h := newHarness(t)
	photo := h.seedPhoto("ph1", "2023/01/a.jpg", false)
	run := h.start()

	paused, err := h.svc.Pause(t.Context(), project.BootstrapUID)
	if err != nil || paused.State != StatePaused {
		t.Fatalf("Pause = %+v, %v; want paused", paused, err)
	}
	if _, err := h.svc.Pause(t.Context(), project.BootstrapUID); !errors.Is(err, ErrInvalidState) {
		t.Errorf("second Pause = %v, want ErrInvalidState", err)
	}
	if _, err := h.svc.Retry(t.Context(), project.BootstrapUID); !errors.Is(err, ErrInvalidState) {
		t.Errorf("Retry of a paused run = %v, want ErrInvalidState", err)
	}
	if _, _, err := h.svc.Step(t.Context(), run, 5); !errors.Is(err, ErrNotRunning) {
		t.Errorf("Step of a paused run = %v, want ErrNotRunning", err)
	}
	if _, err := h.svc.Finish(t.Context(), run); !errors.Is(err, ErrNotRunning) {
		t.Errorf("Finish of a paused run = %v, want ErrNotRunning", err)
	}
	if got, _ := h.photoPath(photo.uid); got != photo.filePath {
		t.Fatalf("a paused run moved a photo to %s", got)
	}

	resumed, err := h.svc.Resume(t.Context(), project.BootstrapUID)
	if err != nil || resumed.State != StateRunning {
		t.Fatalf("Resume = %+v, %v; want running", resumed, err)
	}
	if _, err := h.svc.Resume(t.Context(), project.BootstrapUID); !errors.Is(err, ErrInvalidState) {
		t.Errorf("second Resume = %v, want ErrInvalidState", err)
	}
	if err := h.stepToEnd(run, 5); err != nil {
		t.Fatalf("stepping: %v", err)
	}
	h.assertMoved(photo)
}

func TestService_startRefusals(t *testing.T) {
	h := newHarness(t)
	ctx := t.Context()
	if _, err := h.svc.Status(ctx, project.BootstrapUID); !errors.Is(err, ErrNoRun) {
		t.Errorf("Status before any run = %v, want ErrNoRun", err)
	}
	if _, err := h.svc.Pause(ctx, project.BootstrapUID); !errors.Is(err, ErrNoRun) {
		t.Errorf("Pause before any run = %v, want ErrNoRun", err)
	}
	if _, err := h.svc.Start(ctx, audit.Meta{}, "pjunknownunknownunknown00"); !errors.Is(err, ErrProjectNotFound) {
		t.Errorf("Start of an unknown project = %v, want ErrProjectNotFound", err)
	}
	const created = "pjbbbbbbbbbbbbbbbbbbbbbbbb"
	h.exec(`INSERT INTO projects (uid, slug, name, description, active, storage_prefix)
		VALUES ($1, 'b', 'B', '', true, $2)`, created, project.StoragePrefixFor(created))
	if _, err := h.svc.Start(ctx, audit.Meta{}, created); !errors.Is(err, ErrAlreadyPrefixed) {
		t.Errorf("Start of a created project = %v, want ErrAlreadyPrefixed", err)
	}

	run := h.start()
	var prefix string
	if err := h.db.Pool().QueryRow(ctx, `SELECT storage_prefix FROM projects WHERE uid = $1`,
		project.BootstrapUID).Scan(&prefix); err != nil || prefix != bootstrapTarget {
		t.Errorf("storage prefix after Start = %q, %v; want %s", prefix, err, bootstrapTarget)
	}
	var audited int
	if err := h.db.Pool().QueryRow(ctx, `SELECT count(*) FROM audit_log WHERE action = $1 AND target_uid = $2`,
		audit.ActionProjectStoragePrefix, project.BootstrapUID).Scan(&audited); err != nil || audited != 1 {
		t.Errorf("audit entries for the switch = %d, %v; want 1", audited, err)
	}
	if _, err := h.svc.Start(ctx, audit.Meta{}, project.BootstrapUID); !errors.Is(err, ErrRunActive) {
		t.Errorf("Start while running = %v, want ErrRunActive", err)
	}
	if _, err := h.svc.Pause(ctx, project.BootstrapUID); err != nil {
		t.Fatalf("Pause: %v", err)
	}
	if _, err := h.svc.Start(ctx, audit.Meta{}, project.BootstrapUID); !errors.Is(err, ErrRunActive) {
		t.Errorf("Start while paused = %v, want ErrRunActive", err)
	}
	if _, err := h.svc.Resume(ctx, project.BootstrapUID); err != nil {
		t.Fatalf("Resume: %v", err)
	}
	if err := h.stepToEnd(run, 5); err != nil {
		t.Fatalf("stepping: %v", err)
	}
	if _, err := h.svc.Finish(ctx, run); err != nil {
		t.Fatalf("Finish: %v", err)
	}
	// A moved project may be started again; the new run has nothing to move and
	// the prefix is not switched (nor audited) a second time.
	again := h.start()
	if again.UID == run.UID || again.TotalPhotos != 0 {
		t.Errorf("second run = %+v, want a fresh, empty run", again)
	}
	if err := h.db.Pool().QueryRow(ctx, `SELECT count(*) FROM audit_log WHERE action = $1`,
		audit.ActionProjectStoragePrefix).Scan(&audited); err != nil || audited != 1 {
		t.Errorf("audit entries after the second start = %d, %v; want still 1", audited, err)
	}
}
