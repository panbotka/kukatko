//go:build integration

package facejob_test

import (
	"bytes"
	"context"
	"encoding/json"
	"image"
	"image/color"
	"image/jpeg"
	"math"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/panbotka/kukatko/internal/database"
	"github.com/panbotka/kukatko/internal/database/dbtest"
	"github.com/panbotka/kukatko/internal/embedding"
	"github.com/panbotka/kukatko/internal/facejob"
	"github.com/panbotka/kukatko/internal/jobs"
	"github.com/panbotka/kukatko/internal/photos"
	"github.com/panbotka/kukatko/internal/storage"
	"github.com/panbotka/kukatko/internal/vectors"
	"github.com/panbotka/kukatko/internal/worker"
)

// These tests run only under `make test-integration` against the database named
// by KUKATKO_TEST_DATABASE_URL. They share one database and truncate between
// cases, so they intentionally do not run in parallel.

const (
	imgWidth  = 64
	imgHeight = 48
)

// harness bundles the live collaborators a facejob.Service needs.
type harness struct {
	db      *database.DB
	photos  *photos.Store
	vectors *vectors.Store
	jobs    *jobs.Store
	storage *storage.FS
}

// newHarness builds the live stores and on-disk storage over a freshly truncated
// integration database and an isolated temp directory.
func newHarness(t *testing.T) *harness {
	t.Helper()
	db := dbtest.New(t)
	dbtest.TruncateAll(t, db)

	store, err := storage.NewFS(filepath.Join(t.TempDir(), "originals"))
	if err != nil {
		t.Fatalf("storage.NewFS: %v", err)
	}
	return &harness{
		db:      db,
		photos:  photos.NewStore(db.Pool()),
		vectors: vectors.NewStore(db.Pool()),
		jobs:    jobs.NewStore(db.Pool()),
		storage: store,
	}
}

// storeJPEG encodes a small gradient JPEG, stores it through the originals store,
// and inserts a photos row referencing it, returning the created photo.
func (h *harness) storeJPEG(t *testing.T, name string) photos.Photo {
	t.Helper()
	var tint uint8
	for i := range len(name) {
		tint += name[i]
	}
	img := image.NewRGBA(image.Rect(0, 0, imgWidth, imgHeight))
	for y := range imgHeight {
		for x := range imgWidth {
			img.Set(x, y, color.RGBA{R: uint8(x) + tint, G: uint8(y), B: 128, A: 255})
		}
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 90}); err != nil {
		t.Fatalf("encode jpeg: %v", err)
	}
	sf, err := h.storage.Store(context.Background(), &buf, time.Time{}, name+".jpg")
	if err != nil {
		t.Fatalf("store original: %v", err)
	}
	created, err := h.photos.Create(context.Background(), photos.Photo{
		FileHash:        sf.Hash,
		FilePath:        sf.RelPath,
		FileName:        name + ".jpg",
		FileSize:        sf.Size,
		FileMime:        "image/jpeg",
		FileWidth:       imgWidth,
		FileHeight:      imgHeight,
		FileOrientation: 1,
	})
	if err != nil {
		t.Fatalf("create photo: %v", err)
	}
	return created
}

// faceVec builds a 512-dim vector with index 0 set so the response is non-empty
// and correctly sized.
func faceVec() []float32 {
	v := make([]float32, embedding.DefaultFaceDim)
	v[0] = 1
	return v
}

// fakeSidecar serves /embed/face with a fixed pair of detections, counting
// requests. A non-zero status overrides the 200 response (used to simulate an
// offline box). The first face scores high and the second below 0.5 so the
// det_score filter can be exercised.
func fakeSidecar(t *testing.T, status int) (*embedding.HTTPClient, *atomic.Int64) {
	t.Helper()
	var calls atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		if status != 0 {
			w.WriteHeader(status)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"faces_count": 2,
			"model":       "buffalo_l",
			"faces": []map[string]any{
				{
					"face_index": 0, "dim": embedding.DefaultFaceDim, "embedding": faceVec(),
					"bbox": []float64{16, 12, 48, 36}, "det_score": 0.98,
				},
				{
					"face_index": 1, "dim": embedding.DefaultFaceDim, "embedding": faceVec(),
					"bbox": []float64{0, 0, 4, 4}, "det_score": 0.20,
				},
			},
		})
	}))
	t.Cleanup(srv.Close)

	client, err := embedding.New(embedding.Config{BaseURL: srv.URL})
	if err != nil {
		t.Fatalf("embedding.New: %v", err)
	}
	return client, &calls
}

// newService wires a facejob.Service over the harness with the given sidecar.
func (h *harness) newService(client embedding.Client) *facejob.Service {
	return facejob.New(facejob.Config{
		Photos:            h.photos,
		Vectors:           h.vectors,
		Client:            client,
		Source:            facejob.NewStorageSource(h.storage),
		Enqueuer:          jobs.NewEnqueuer(h.jobs),
		OfflineRetryDelay: 5 * time.Minute,
		MinDetScore:       0.5,
	})
}

// bboxClose reports whether two boxes match within a small epsilon.
func bboxClose(a, b [4]float64) bool {
	for i := range a {
		if math.Abs(a[i]-b[i]) > 1e-6 {
			return false
		}
	}
	return true
}

// TestDetect_storesFiltersAndIsIdempotent verifies the handler detects, filters
// low-score faces, stores normalized boxes, then skips the sidecar on a re-run.
func TestDetect_storesFiltersAndIsIdempotent(t *testing.T) {
	h := newHarness(t)
	ctx := t.Context()
	photo := h.storeJPEG(t, "detect-me")
	client, calls := fakeSidecar(t, 0)
	svc := h.newService(client)

	if err := svc.Detect(ctx, photo.UID); err != nil {
		t.Fatalf("Detect: %v", err)
	}
	faces, err := h.vectors.ListFaces(ctx, photo.UID)
	if err != nil {
		t.Fatalf("ListFaces: %v", err)
	}
	if len(faces) != 1 {
		t.Fatalf("stored %d faces, want 1 (low-score dropped)", len(faces))
	}
	face := faces[0]
	if want := [4]float64{0.25, 0.25, 0.5, 0.5}; !bboxClose(face.BBox, want) {
		t.Errorf("normalized bbox = %v, want %v", face.BBox, want)
	}
	if face.Model != "buffalo_l" || face.PhotoWidth != imgWidth || face.PhotoHeight != imgHeight {
		t.Errorf("face metadata = %+v, want buffalo_l / %dx%d", face, imgWidth, imgHeight)
	}
	if calls.Load() != 1 {
		t.Fatalf("sidecar calls = %d, want 1", calls.Load())
	}

	// Second run is a no-op: the detection is already recorded.
	if err := svc.Detect(ctx, photo.UID); err != nil {
		t.Fatalf("Detect (idempotent): %v", err)
	}
	if calls.Load() != 1 {
		t.Errorf("sidecar calls after idempotent run = %d, want 1", calls.Load())
	}
}

// TestDetect_requeuesWhenOffline verifies that, end to end through the worker, an
// offline sidecar leaves the job queued for a later run without burning an
// attempt rather than failing or dead-lettering it.
func TestDetect_requeuesWhenOffline(t *testing.T) {
	h := newHarness(t)
	ctx := t.Context()
	photo := h.storeJPEG(t, "offline")
	client, _ := fakeSidecar(t, http.StatusServiceUnavailable)
	svc := h.newService(client)

	reg := worker.NewRegistry()
	reg.Register(jobs.TypeFaceDetect, svc.Handle)
	w := worker.New(worker.Config{
		Queue:             h.jobs,
		Registry:          reg,
		Concurrency:       1,
		PollInterval:      2 * time.Millisecond,
		StaleAfter:        time.Hour,
		StaleScanInterval: time.Hour,
		IDPrefix:          "itest",
	})

	payload, err := json.Marshal(map[string]string{"photo_uid": photo.UID})
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	job, err := h.jobs.Enqueue(ctx, jobs.TypeFaceDetect, payload, jobs.EnqueueOptions{})
	if err != nil {
		t.Fatalf("enqueue: %v", err)
	}

	wctx, cancel := context.WithCancel(context.Background())
	stopped := make(chan struct{})
	go func() { _ = w.Run(wctx); close(stopped) }()
	defer func() { cancel(); <-stopped }()

	// Poll until the job has been claimed and deferred: still queued, no attempt
	// burned, run_after pushed well into the future by the offline deferral.
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		got, err := h.jobs.Get(ctx, job.ID)
		if err != nil {
			t.Fatalf("Get: %v", err)
		}
		if got.State == jobs.StateQueued && got.Attempts == 0 &&
			got.RunAfter.After(time.Now().Add(time.Minute)) {
			return // deferred as expected
		}
		if got.State == jobs.StateDead || got.State == jobs.StateFailed {
			t.Fatalf("job %d reached %q, want a no-attempt deferral", job.ID, got.State)
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("job was not deferred within 5s")
}

// TestBackfillFaces_enqueuesOnlyUnprocessed verifies the backfill enqueues a job
// only for photos that have never had face detection run.
func TestBackfillFaces_enqueuesOnlyUnprocessed(t *testing.T) {
	h := newHarness(t)
	ctx := t.Context()
	client, _ := fakeSidecar(t, 0)
	svc := h.newService(client)

	processed := h.storeJPEG(t, "already")
	missing1 := h.storeJPEG(t, "missing-1")
	missing2 := h.storeJPEG(t, "missing-2")
	if err := h.vectors.RecordFaceDetection(ctx, processed.UID, nil, "buffalo_l"); err != nil {
		t.Fatalf("seed detection: %v", err)
	}

	n, err := svc.BackfillFaces(ctx)
	if err != nil {
		t.Fatalf("BackfillFaces: %v", err)
	}
	if n != 2 {
		t.Errorf("enqueued = %d, want 2", n)
	}

	queued, err := h.jobs.List(ctx, jobs.ListOptions{})
	if err != nil {
		t.Fatalf("List jobs: %v", err)
	}
	enqueuedUIDs := map[string]bool{}
	for _, j := range queued {
		if j.Type != jobs.TypeFaceDetect {
			continue
		}
		var p struct {
			PhotoUID string `json:"photo_uid"`
		}
		if err := json.Unmarshal(j.Payload, &p); err != nil {
			t.Fatalf("unmarshal job payload: %v", err)
		}
		enqueuedUIDs[p.PhotoUID] = true
	}
	if !enqueuedUIDs[missing1.UID] || !enqueuedUIDs[missing2.UID] {
		t.Errorf("unprocessed photos not enqueued: %v", enqueuedUIDs)
	}
	if enqueuedUIDs[processed.UID] {
		t.Errorf("already-processed photo %s was enqueued", processed.UID)
	}
}
