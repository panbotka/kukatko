//go:build integration

package ocrjob_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
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
	"github.com/panbotka/kukatko/internal/jobs"
	"github.com/panbotka/kukatko/internal/ocrjob"
	"github.com/panbotka/kukatko/internal/photos"
	"github.com/panbotka/kukatko/internal/storage"
	"github.com/panbotka/kukatko/internal/thumb"
	"github.com/panbotka/kukatko/internal/worker"
)

// These tests run only under `make test-integration` against the database named
// by KUKATKO_TEST_DATABASE_URL. They share one database and truncate between
// cases, so they intentionally do not run in parallel.

// harness bundles the live collaborators an ocrjob.Service needs.
type harness struct {
	db          *database.DB
	photos      *photos.Store
	jobs        *jobs.Store
	storage     storage.Storage
	thumbnailer *thumb.Thumbnailer
}

// newHarness builds the live stores and on-disk storage over a freshly truncated
// integration database and isolated temp directories.
func newHarness(t *testing.T) *harness {
	t.Helper()
	root := t.TempDir()
	store, err := storage.NewFS(filepath.Join(root, "originals"))
	if err != nil {
		t.Fatalf("storage.NewFS: %v", err)
	}
	db := dbtest.New(t)
	dbtest.TruncateAll(t, db)
	return &harness{
		db:          db,
		photos:      photos.NewStore(db.Pool()),
		jobs:        jobs.NewStore(db.Pool()),
		storage:     store,
		thumbnailer: thumb.New(store, filepath.Join(root, "cache")),
	}
}

// storeJPEG encodes a small gradient JPEG, stores it through the originals store
// and inserts a photos row referencing it, returning the created photo.
func (h *harness) storeJPEG(t *testing.T, name string) photos.Photo {
	t.Helper()
	// Tint by the name so each photo's bytes (and thus content hash) differ;
	// identical bytes would collide on the file_hash unique constraint.
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
		MediaType:       photos.MediaImage,
		FileOrientation: 1,
	})
	if err != nil {
		t.Fatalf("create photo: %v", err)
	}
	return created
}

// fakeSidecar serves /ocr/image with a fixed reading, counting requests. A
// non-zero status overrides the 200 response (used to simulate an offline box).
func fakeSidecar(t *testing.T, status int, text string) (*embedding.HTTPClient, *atomic.Int64) {
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
			"text":         text,
			"blocks_count": 1,
			"blocks": []map[string]any{
				{"text": text, "bbox": []float64{1, 2, 3, 4}, "confidence": 0.99},
			},
			"lang":  "latin",
			"model": "PP-OCRv5_mobile",
		})
	}))
	t.Cleanup(srv.Close)

	client, err := embedding.New(embedding.Config{BaseURL: srv.URL})
	if err != nil {
		t.Fatalf("embedding.New: %v", err)
	}
	return client, &calls
}

// newService wires an ocrjob.Service over the harness with the given sidecar.
func (h *harness) newService(client ocrjob.Recognizer) *ocrjob.Service {
	return ocrjob.New(ocrjob.Config{
		Photos:            h.photos,
		Client:            client,
		Previewer:         h.thumbnailer,
		Lister:            h.photos,
		Enqueuer:          jobs.NewEnqueuer(h.jobs),
		MinConfidence:     0.5,
		OfflineRetryDelay: 5 * time.Minute,
	})
}

// TestRecognize_storesAndMakesSearchable is the end-to-end claim: the handler
// renders a preview, reads it through the sidecar, stores the text, and the
// library's search then finds that photo by what the sign says.
func TestRecognize_storesAndMakesSearchable(t *testing.T) {
	h := newHarness(t)
	ctx := t.Context()
	photo := h.storeJPEG(t, "ocr-sign")
	client, calls := fakeSidecar(t, 0, "VESELICE")
	svc := h.newService(client)

	if err := svc.Recognize(ctx, photo.UID); err != nil {
		t.Fatalf("Recognize: %v", err)
	}
	if calls.Load() != 1 {
		t.Errorf("sidecar calls = %d, want 1", calls.Load())
	}
	got, err := h.photos.GetOCR(ctx, photo.UID)
	if err != nil {
		t.Fatalf("GetOCR: %v", err)
	}
	if got.Text != "VESELICE" || got.Model != "PP-OCRv5_mobile" {
		t.Errorf("stored OCR = %+v", got)
	}

	found, err := h.photos.Search(ctx, photos.ListParams{FullText: "veselice"})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(found) != 1 || found[0].UID != photo.UID {
		t.Fatalf("Search(veselice) = %d results, want the photo whose sign says it", len(found))
	}
}

// TestRecognize_emptyReadingClearsTheBacklog verifies "we looked and there was
// nothing" is recorded, so the photo drops out of the backfill instead of being
// re-scheduled forever.
func TestRecognize_emptyReadingClearsTheBacklog(t *testing.T) {
	h := newHarness(t)
	ctx := t.Context()
	photo := h.storeJPEG(t, "ocr-blank")
	client, _ := fakeSidecar(t, 0, "")
	svc := h.newService(client)

	pending, err := h.photos.ListPhotosMissingOCR(ctx, 0)
	if err != nil {
		t.Fatalf("ListPhotosMissingOCR: %v", err)
	}
	if len(pending) != 1 {
		t.Fatalf("pending before = %v, want the one photo", pending)
	}

	if err := svc.Recognize(ctx, photo.UID); err != nil {
		t.Fatalf("Recognize: %v", err)
	}
	if pending, err = h.photos.ListPhotosMissingOCR(ctx, 0); err != nil {
		t.Fatalf("ListPhotosMissingOCR: %v", err)
	}
	if len(pending) != 0 {
		t.Errorf("pending after an empty reading = %v, want none", pending)
	}
}

// TestRecognize_offlineSidecarDefersTheJob verifies an unreachable box behaves
// exactly as it does for image_embed: the job is deferred, nothing is written,
// and the photo stays a candidate for when the box comes back.
func TestRecognize_offlineSidecarDefersTheJob(t *testing.T) {
	h := newHarness(t)
	ctx := t.Context()
	photo := h.storeJPEG(t, "ocr-offline")
	client, _ := fakeSidecar(t, http.StatusServiceUnavailable, "")
	svc := h.newService(client)

	err := svc.Recognize(ctx, photo.UID)
	var retry *worker.RetryAfterError
	if !errors.As(err, &retry) {
		t.Fatalf("err = %v, want a worker.RetryAfterError", err)
	}
	if _, err := h.photos.GetOCR(ctx, photo.UID); !errors.Is(err, photos.ErrPhotoNotFound) {
		t.Errorf("GetOCR = %v, want nothing stored while the box is down", err)
	}
	pending, err := h.photos.ListPhotosMissingOCR(ctx, 0)
	if err != nil {
		t.Fatalf("ListPhotosMissingOCR: %v", err)
	}
	if len(pending) != 1 {
		t.Errorf("pending = %v, want the photo still queued for a later run", pending)
	}
}

// TestBackfillOCR_enqueuesRealJobs verifies the backfill puts real `ocr` rows in
// the queue and that a second run over the same library adds nothing (the queue
// dedupes per photo).
func TestBackfillOCR_enqueuesRealJobs(t *testing.T) {
	h := newHarness(t)
	ctx := t.Context()
	h.storeJPEG(t, "bf-1")
	h.storeJPEG(t, "bf-2")
	client, _ := fakeSidecar(t, 0, "text")
	svc := h.newService(client)

	n, err := svc.BackfillOCR(ctx, false)
	if err != nil {
		t.Fatalf("BackfillOCR: %v", err)
	}
	if n != 2 {
		t.Fatalf("enqueued = %d, want 2", n)
	}
	counts, err := h.jobs.CountsByType(ctx)
	if err != nil {
		t.Fatalf("CountsByType: %v", err)
	}
	if counts[jobs.TypeOCR] != 2 {
		t.Errorf("queued ocr jobs = %d, want 2 (counts: %+v)", counts[jobs.TypeOCR], counts)
	}

	// A second run re-lists the same photos but the queue absorbs the duplicates.
	if _, err := svc.BackfillOCR(ctx, false); err != nil {
		t.Fatalf("BackfillOCR again: %v", err)
	}
	if counts, err = h.jobs.CountsByType(ctx); err != nil {
		t.Fatalf("CountsByType: %v", err)
	}
	if counts[jobs.TypeOCR] != 2 {
		t.Errorf("queued ocr jobs after a repeat run = %d, want still 2", counts[jobs.TypeOCR])
	}
}
