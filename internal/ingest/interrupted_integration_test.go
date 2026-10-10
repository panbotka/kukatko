//go:build integration

package ingest_test

import (
	"bytes"
	"context"
	"io"
	"os"
	"testing"
	"time"

	"github.com/panbotka/kukatko/internal/config"
	"github.com/panbotka/kukatko/internal/database"
	"github.com/panbotka/kukatko/internal/database/dbtest"
	"github.com/panbotka/kukatko/internal/ingest"
	"github.com/panbotka/kukatko/internal/jobs"
	"github.com/panbotka/kukatko/internal/photos"
	"github.com/panbotka/kukatko/internal/processing"
	"github.com/panbotka/kukatko/internal/storage"
	"github.com/panbotka/kukatko/internal/thumb"
)

// These tests reproduce the interrupted upload of 2026-10-05 against the real
// queue and the real processing report: a client that hangs up once its
// original is stored, the photo it used to leave behind, and the re-upload that
// used to answer `duplicate` and change nothing.

// hangUpAfterStore is a storage.Storage that cancels the uploading client's
// context the moment the original has been published — the client hanging up
// right after its last byte, while the pipeline is still working.
type hangUpAfterStore struct {
	storage.Storage
	hangUp context.CancelFunc
}

// Store publishes the original and then cancels the client's context.
func (s hangUpAfterStore) Store(
	ctx context.Context, r io.Reader, takenAt time.Time, filename string,
) (storage.StoredFile, error) {
	stored, err := s.Storage.Store(ctx, r, takenAt, filename)
	s.hangUp()
	return stored, err
}

// queueEnv is an ingest service over the real queue, with the processing service
// as its pending scheduler — the production wiring minus the switchable jobs,
// which the report is told are off.
type queueEnv struct {
	svc        *ingest.Service
	photos     *photos.Store
	thumbs     *thumb.Thumbnailer
	processing *processing.Service
	db         *database.DB
	cacheDir   string
}

// newQueueEnv builds a queueEnv. wrap, when non-nil, wraps the storage the
// pipeline publishes originals through.
func newQueueEnv(t *testing.T, wrap func(storage.Storage) storage.Storage) *queueEnv {
	t.Helper()
	db := dbtest.New(t)
	dbtest.TruncateAll(t, db)

	fsStore, err := storage.NewFS(t.TempDir())
	if err != nil {
		t.Fatalf("storage.NewFS: %v", err)
	}
	var store storage.Storage = fsStore
	if wrap != nil {
		store = wrap(fsStore)
	}
	cacheDir := t.TempDir()
	thumbs := thumb.New(fsStore, cacheDir)
	photoStore := photos.NewStore(db.Pool())
	jobStore := jobs.NewStore(db.Pool())
	enq := jobs.NewEnqueuer(jobStore)
	evidence := processing.NewStore(db.Pool())
	proc := processing.New(processing.Config{
		Evidence: evidence, Jobs: jobStore, Enqueuer: enq, Library: evidence,
		Disabled: []processing.Step{
			processing.StepOCR, processing.StepSidecar, processing.StepPlaces, processing.StepHLS,
		},
	})
	svc := ingest.New(ingest.Config{
		Storage: store, Photos: photoStore, Thumbnailer: thumbs, Enqueuer: enq,
		Pending: proc, Duplicate: config.DuplicateConfig{}, TempDir: t.TempDir(),
	})
	return &queueEnv{
		svc: svc, photos: photoStore, thumbs: thumbs, processing: proc, db: db, cacheDir: cacheDir,
	}
}

// states returns the photo's processing report as step → state.
func (e *queueEnv) states(t *testing.T, uid string) map[processing.Step]processing.State {
	t.Helper()
	report, err := e.processing.Report(t.Context(), uid)
	if err != nil {
		t.Fatalf("Report(%s): %v", uid, err)
	}
	out := make(map[processing.Step]processing.State, len(report))
	for _, st := range report {
		out[st.Step] = st.State
	}
	return out
}

// wantStates fails the test for every step whose state is not the expected one.
func wantStates(t *testing.T, got, want map[processing.Step]processing.State) {
	t.Helper()
	for step, state := range want {
		if got[step] != state {
			t.Errorf("%s = %s, want %s (report %v)", step, got[step], state, got)
		}
	}
}

// hasThumbnail reports whether the representative grid thumbnail of photo is
// cached.
func (e *queueEnv) hasThumbnail(t *testing.T, uid string) bool {
	t.Helper()
	photo, err := e.photos.GetByUID(t.Context(), uid)
	if err != nil {
		t.Fatalf("GetByUID(%s): %v", uid, err)
	}
	rc, err := e.thumbs.OpenCached(photo.FileHash, "tile_224")
	if err != nil {
		return false
	}
	_ = rc.Close()
	return true
}

// TestIngest_clientHangsUpAfterTheOriginalIsStored is the 2026-10-05 upload: the
// client disconnects once the original is in storage. The photo must still get
// its thumbnails and its queued jobs.
func TestIngest_clientHangsUpAfterTheOriginalIsStored(t *testing.T) {
	ctx, hangUp := context.WithCancel(t.Context())
	defer hangUp()
	env := newQueueEnv(t, func(s storage.Storage) storage.Storage {
		return hangUpAfterStore{Storage: s, hangUp: hangUp}
	})

	res := env.svc.Ingest(ctx, bytes.NewReader(jpegBytes(t, 120, 40, 200, 90)), "les.jpg", "")
	if ctx.Err() == nil {
		t.Fatal("the client never hung up — the test proves nothing")
	}
	if res.Outcome != ingest.OutcomeCreated {
		t.Fatalf("result = %+v, want created despite the hang-up", res)
	}
	if !env.hasThumbnail(t, res.PhotoUID) {
		t.Error("no thumbnail after the client hung up")
	}
	wantStates(t, env.states(t, res.PhotoUID), map[processing.Step]processing.State{
		processing.StepMetadata:   processing.StateDone,
		processing.StepThumbnail:  processing.StateDone,
		processing.StepImageEmbed: processing.StateQueued,
		processing.StepFaceDetect: processing.StateQueued,
	})
}

// TestIngest_duplicateCompletesAnInterruptedPhoto is the re-upload half: a
// photo left with no thumbnails and no jobs is completed by sending the same
// bytes again, and the answer is still `duplicate`.
func TestIngest_duplicateCompletesAnInterruptedPhoto(t *testing.T) {
	env := newQueueEnv(t, nil)
	ctx := t.Context()
	data := jpegBytes(t, 30, 160, 60, 90)

	first := env.svc.Ingest(ctx, bytes.NewReader(data), "first.jpg", "")
	if first.Outcome != ingest.OutcomeCreated {
		t.Fatalf("first upload = %+v, want created", first)
	}

	// Leave exactly what the interrupted upload left: the photo and its pHash,
	// but no thumbnails (in the cache or in the stamp) and nothing queued.
	photo, err := env.photos.GetByUID(ctx, first.PhotoUID)
	if err != nil {
		t.Fatalf("GetByUID: %v", err)
	}
	if err := env.thumbs.Remove(photo.FileHash); err != nil {
		t.Fatalf("removing thumbnails: %v", err)
	}
	if _, err := env.db.Pool().Exec(ctx, `UPDATE photos SET thumbnails_at = NULL WHERE uid = $1`,
		photo.UID); err != nil {
		t.Fatalf("clearing the stamp: %v", err)
	}
	if _, err := env.db.Pool().Exec(ctx, `DELETE FROM jobs`); err != nil {
		t.Fatalf("emptying the queue: %v", err)
	}
	wantStates(t, env.states(t, photo.UID), map[processing.Step]processing.State{
		processing.StepThumbnail:  processing.StatePending,
		processing.StepImageEmbed: processing.StatePending,
		processing.StepFaceDetect: processing.StatePending,
	})
	gaps, err := env.processing.Unscheduled(ctx)
	if err != nil || len(gaps) != 3 {
		t.Errorf("Unscheduled = %v, %v; want the photo's three gaps", gaps, err)
	}

	second := env.svc.Ingest(ctx, bytes.NewReader(data), "again.jpg", "")
	if second.Outcome != ingest.OutcomeDuplicate || second.Status != 409 || second.PhotoUID != photo.UID {
		t.Fatalf("re-upload = %+v, want duplicate/409 of %s", second, photo.UID)
	}
	if !env.hasThumbnail(t, photo.UID) {
		t.Error("the re-upload did not regenerate the missing thumbnails")
	}
	wantStates(t, env.states(t, photo.UID), map[processing.Step]processing.State{
		processing.StepThumbnail:  processing.StateDone,
		processing.StepImageEmbed: processing.StateQueued,
		processing.StepFaceDetect: processing.StateQueued,
	})
	if gaps, err := env.processing.Unscheduled(ctx); err != nil || len(gaps) != 0 {
		t.Errorf("Unscheduled after the re-upload = %v, %v; want none", gaps, err)
	}

	// A third upload of a complete photo changes nothing and stays a duplicate.
	third := env.svc.Ingest(ctx, bytes.NewReader(data), "third.jpg", "")
	if third.Outcome != ingest.OutcomeDuplicate {
		t.Errorf("third upload = %+v, want duplicate", third)
	}
	var queued int
	if err := env.db.Pool().QueryRow(ctx, `SELECT count(*) FROM jobs`).Scan(&queued); err != nil {
		t.Fatalf("counting jobs: %v", err)
	}
	if queued != 2 {
		t.Errorf("queue holds %d jobs after a third upload, want the same 2", queued)
	}
}

// TestIngest_thumbnailStateMatchesReality checks the report against the cache:
// when the thumbnails cannot be written, the pHash still lands (it is computed
// first), and the step must read as owed rather than done.
func TestIngest_thumbnailStateMatchesReality(t *testing.T) {
	env := newQueueEnv(t, nil)
	ctx := t.Context()
	// A cache nobody may write into: every size fails, nothing else does.
	if err := os.Chmod(env.cacheDir, 0o500); err != nil {
		t.Fatalf("chmod cache: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(env.cacheDir, 0o700) })

	res := env.svc.Ingest(ctx, bytes.NewReader(jpegBytes(t, 90, 90, 10, 90)), "nocache.jpg", "")
	if res.Outcome != ingest.OutcomeCreated || !hasWarning(res.Warnings, "thumbnail_failed") {
		t.Fatalf("result = %+v, want created with a thumbnail_failed warning", res)
	}
	if _, err := env.photos.GetPhash(ctx, res.PhotoUID); err != nil {
		t.Fatalf("GetPhash: %v — the pHash should have landed", err)
	}
	if env.hasThumbnail(t, res.PhotoUID) {
		t.Fatal("a thumbnail was written into a read-only cache")
	}
	if got := env.states(t, res.PhotoUID)[processing.StepThumbnail]; got == processing.StateDone {
		t.Errorf("thumbnail = %s with no thumbnail in the cache", got)
	}

	if err := os.Chmod(env.cacheDir, 0o700); err != nil {
		t.Fatalf("chmod cache back: %v", err)
	}
	again := env.svc.Ingest(ctx, bytes.NewReader(jpegBytes(t, 90, 90, 10, 90)), "nocache.jpg", "")
	if again.Outcome != ingest.OutcomeDuplicate {
		t.Fatalf("re-upload = %+v, want duplicate", again)
	}
	if !env.hasThumbnail(t, res.PhotoUID) {
		t.Error("the re-upload did not fill in the thumbnails")
	}
	if got := env.states(t, res.PhotoUID)[processing.StepThumbnail]; got != processing.StateDone {
		t.Errorf("thumbnail = %s once the thumbnails exist, want done", got)
	}
}
