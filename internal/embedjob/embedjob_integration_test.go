//go:build integration

package embedjob_test

import (
	"bytes"
	"context"
	"encoding/json"
	"image"
	"image/color"
	"image/jpeg"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/panbotka/kukatko/internal/database"
	"github.com/panbotka/kukatko/internal/database/dbtest"
	"github.com/panbotka/kukatko/internal/embedding"
	"github.com/panbotka/kukatko/internal/embedjob"
	"github.com/panbotka/kukatko/internal/jobs"
	"github.com/panbotka/kukatko/internal/photos"
	"github.com/panbotka/kukatko/internal/storage"
	"github.com/panbotka/kukatko/internal/thumb"
	"github.com/panbotka/kukatko/internal/vectors"
	"github.com/panbotka/kukatko/internal/worker"
)

// These tests run only under `make test-integration` against the database named
// by KUKATKO_TEST_DATABASE_URL. They share one database and truncate between
// cases, so they intentionally do not run in parallel.

// harness bundles the live collaborators an embedjob.Service needs.
type harness struct {
	db          *database.DB
	photos      *photos.Store
	vectors     *vectors.Store
	jobs        *jobs.Store
	storage     *storage.FS
	thumbnailer *thumb.Thumbnailer
}

// newHarness builds the live stores and on-disk storage over a freshly truncated
// integration database and isolated temp directories.
func newHarness(t *testing.T) *harness {
	t.Helper()
	db := dbtest.New(t)
	dbtest.TruncateAll(t, db)

	root := t.TempDir()
	store, err := storage.NewFS(filepath.Join(root, "originals"))
	if err != nil {
		t.Fatalf("storage.NewFS: %v", err)
	}
	return &harness{
		db:          db,
		photos:      photos.NewStore(db.Pool()),
		vectors:     vectors.NewStore(db.Pool()),
		jobs:        jobs.NewStore(db.Pool()),
		storage:     store,
		thumbnailer: thumb.New(store, filepath.Join(root, "cache")),
	}
}

// storeJPEG encodes a small gradient JPEG, stores it through the originals store,
// and inserts a photos row referencing it, returning the created photo.
func (h *harness) storeJPEG(t *testing.T, name string) photos.Photo {
	t.Helper()
	// Tint by the name so each photo's encoded bytes (and thus content hash)
	// differ; identical bytes would collide on the file_hash unique constraint.
	var tint uint8
	for i := range len(name) {
		tint += name[i]
	}
	img := image.NewRGBA(image.Rect(0, 0, 64, 48))
	for y := range 48 {
		for x := range 64 {
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
		FileOrientation: 1,
	})
	if err != nil {
		t.Fatalf("create photo: %v", err)
	}
	return created
}

// imageVec builds a 768-dim vector with index 0 set so the response is non-empty
// and correctly sized.
func imageVec() []float32 {
	v := make([]float32, embedding.DefaultImageDim)
	v[0] = 1
	return v
}

// fakeSidecar serves /embed/image with a fixed embedding, counting requests. A
// non-zero status overrides the 200 response (used to simulate an offline box).
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
			"dim":        embedding.DefaultImageDim,
			"embedding":  imageVec(),
			"model":      "ViT-B-32",
			"pretrained": "laion2b",
		})
	}))
	t.Cleanup(srv.Close)

	client, err := embedding.New(embedding.Config{BaseURL: srv.URL})
	if err != nil {
		t.Fatalf("embedding.New: %v", err)
	}
	return client, &calls
}

// newService wires an embedjob.Service over the harness with the given sidecar.
func (h *harness) newService(client embedding.Client) *embedjob.Service {
	return embedjob.New(embedjob.Config{
		Photos:            h.photos,
		Vectors:           h.vectors,
		Client:            client,
		Previewer:         h.thumbnailer,
		Enqueuer:          jobs.NewEnqueuer(h.jobs),
		OfflineRetryDelay: 5 * time.Minute,
		DuplicateMaxDist:  0.05,
	})
}

// TestEmbed_computesStoresAndIsIdempotent verifies the handler computes and
// stores the embedding, then skips the sidecar on a second run.
func TestEmbed_computesStoresAndIsIdempotent(t *testing.T) {
	h := newHarness(t)
	ctx := t.Context()
	photo := h.storeJPEG(t, "embed-me")
	client, calls := fakeSidecar(t, 0)
	svc := h.newService(client)

	if err := svc.Embed(ctx, photo.UID); err != nil {
		t.Fatalf("Embed: %v", err)
	}
	got, err := h.vectors.GetEmbedding(ctx, photo.UID)
	if err != nil {
		t.Fatalf("GetEmbedding: %v", err)
	}
	if len(got.Vector) != vectors.ImageDim || got.Model != "ViT-B-32" || got.Pretrained != "laion2b" {
		t.Errorf("stored embedding = %+v, want 768-dim ViT-B-32/laion2b", got)
	}
	if calls.Load() != 1 {
		t.Fatalf("sidecar calls = %d, want 1", calls.Load())
	}

	// Second run is a no-op: the embedding already exists.
	if err := svc.Embed(ctx, photo.UID); err != nil {
		t.Fatalf("Embed (idempotent): %v", err)
	}
	if calls.Load() != 1 {
		t.Errorf("sidecar calls after idempotent run = %d, want 1", calls.Load())
	}
}

// TestEmbed_requeuesWhenOffline verifies that, end to end through the worker, an
// offline sidecar leaves the job queued for a later run without burning an
// attempt rather than failing or dead-lettering it.
func TestEmbed_requeuesWhenOffline(t *testing.T) {
	h := newHarness(t)
	ctx := t.Context()
	photo := h.storeJPEG(t, "offline")
	client, _ := fakeSidecar(t, http.StatusServiceUnavailable)
	svc := h.newService(client)

	reg := worker.NewRegistry()
	reg.Register(jobs.TypeImageEmbed, svc.Handle)
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
	job, err := h.jobs.Enqueue(ctx, jobs.TypeImageEmbed, payload, jobs.EnqueueOptions{})
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

// TestBackfillEmbeddings_enqueuesOnlyMissing verifies the backfill enqueues a
// job only for photos without an embedding.
func TestBackfillEmbeddings_enqueuesOnlyMissing(t *testing.T) {
	h := newHarness(t)
	ctx := t.Context()
	client, _ := fakeSidecar(t, 0)
	svc := h.newService(client)

	embedded := h.storeJPEG(t, "already")
	missing1 := h.storeJPEG(t, "missing-1")
	missing2 := h.storeJPEG(t, "missing-2")
	if _, err := h.vectors.SaveEmbedding(ctx, vectors.Embedding{
		PhotoUID: embedded.UID, Vector: imageVec(),
	}); err != nil {
		t.Fatalf("seed embedding: %v", err)
	}

	n, err := svc.BackfillEmbeddings(ctx)
	if err != nil {
		t.Fatalf("BackfillEmbeddings: %v", err)
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
		if j.Type != jobs.TypeImageEmbed {
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
		t.Errorf("missing photos not enqueued: %v", enqueuedUIDs)
	}
	if enqueuedUIDs[embedded.UID] {
		t.Errorf("already-embedded photo %s was enqueued", embedded.UID)
	}
}
