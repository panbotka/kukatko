//go:build integration

package ingest_test

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/jpeg"
	"sync"
	"testing"
	"time"

	"github.com/panbotka/kukatko/internal/auth"
	"github.com/panbotka/kukatko/internal/config"
	"github.com/panbotka/kukatko/internal/database"
	"github.com/panbotka/kukatko/internal/database/dbtest"
	"github.com/panbotka/kukatko/internal/ingest"
	"github.com/panbotka/kukatko/internal/photos"
	"github.com/panbotka/kukatko/internal/storage"
	"github.com/panbotka/kukatko/internal/thumb"
)

// These tests run only under `make test-integration` against the database named
// by KUKATKO_TEST_DATABASE_URL. They share one database and truncate between
// cases, so they intentionally do not run in parallel.

// testEnv bundles a ready ingest service over a freshly truncated database and
// temp-backed storage/cache directories.
type testEnv struct {
	svc      *ingest.Service
	store    *photos.Store
	thumbs   *thumb.Thumbnailer
	db       *database.DB
	uploader string
}

// newEnv builds an ingest service wired to real storage, thumbnailer and photo
// repository over the integration database, plus an editor user whose UID is
// used as the uploader (the photos.uploaded_by foreign key requires a real
// user).
func newEnv(t *testing.T, dup config.DuplicateConfig) *testEnv {
	t.Helper()
	db := dbtest.New(t)
	dbtest.TruncateAll(t, db)

	authSvc := auth.NewService(auth.NewStore(db.Pool()),
		auth.SessionPolicy{TTL: time.Hour, MaxLifetime: 3 * time.Hour})
	uploader, err := authSvc.CreateUser(t.Context(), auth.CreateUserInput{
		Username: "uploader", Password: "correct horse battery staple", Role: auth.RoleEditor,
	})
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}

	fs, err := storage.NewFS(t.TempDir())
	if err != nil {
		t.Fatalf("storage.NewFS: %v", err)
	}
	thumbs := thumb.New(fs, t.TempDir())
	store := photos.NewStore(db.Pool())
	svc := ingest.New(ingest.Config{
		Storage:     fs,
		Photos:      store,
		Thumbnailer: thumbs,
		Duplicate:   dup,
		TempDir:     t.TempDir(),
	})
	return &testEnv{svc: svc, store: store, thumbs: thumbs, db: db, uploader: uploader.UID}
}

// jpegBytes encodes a small solid-colour JPEG at the given quality. Different
// colours produce different content hashes; the same colour at two qualities
// produces perceptually similar but byte-distinct files.
func jpegBytes(t *testing.T, r, g, b uint8, quality int) []byte {
	t.Helper()
	const w, h = 64, 48
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			// A smooth left-to-right ramp blended into the base colour gives the
			// pHash stable low-frequency structure that survives JPEG recompression,
			// while distinct base colours still produce distinct content hashes.
			ramp := uint8(x * 255 / (w - 1))
			img.Set(x, y, color.RGBA{
				R: uint8((int(r) + int(ramp)) / 2),
				G: uint8((int(g) + int(ramp)) / 2),
				B: b,
				A: 255,
			})
		}
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: quality}); err != nil {
		t.Fatalf("jpeg.Encode: %v", err)
	}
	return buf.Bytes()
}

// ingest is a helper that runs one in-memory file through the pipeline.
func (e *testEnv) ingest(ctx context.Context, data []byte, name string) ingest.FileResult {
	return e.svc.Ingest(ctx, bytes.NewReader(data), name, e.uploader)
}

// TestIngest_singleCreatesEverything verifies a fresh upload creates the photo,
// its primary file, its perceptual hashes and its thumbnails.
func TestIngest_singleCreatesEverything(t *testing.T) {
	env := newEnv(t, config.DuplicateConfig{Enabled: true, PhashMaxDiff: 8})
	ctx := t.Context()

	res := env.ingest(ctx, jpegBytes(t, 200, 50, 50, 90), "beach.jpg")
	if res.Outcome != ingest.OutcomeCreated || res.Status != 201 {
		t.Fatalf("result = %+v, want created/201", res)
	}
	if res.PhotoUID == "" {
		t.Fatal("created result has no photo UID")
	}

	photo, err := env.store.GetByUID(ctx, res.PhotoUID)
	if err != nil {
		t.Fatalf("GetByUID: %v", err)
	}
	if photo.UploadedBy == nil || *photo.UploadedBy != env.uploader {
		t.Errorf("uploaded_by = %v, want %s", photo.UploadedBy, env.uploader)
	}
	if photo.FileWidth != 64 || photo.FileHeight != 48 {
		t.Errorf("dimensions = %dx%d, want 64x48", photo.FileWidth, photo.FileHeight)
	}

	files, err := env.store.ListFiles(ctx, res.PhotoUID)
	if err != nil || len(files) != 1 || !files[0].IsPrimary || files[0].Role != photos.RoleOriginal {
		t.Fatalf("ListFiles = %+v, %v; want one primary original", files, err)
	}

	if _, err := env.store.GetPhash(ctx, res.PhotoUID); err != nil {
		t.Errorf("GetPhash: %v", err)
	}

	for _, size := range []string{"tile_224", "fit_1280"} {
		rc, err := env.thumbs.Open(photo.FileHash, size)
		if err != nil {
			t.Errorf("thumbnail %s not generated: %v", size, err)
			continue
		}
		_ = rc.Close()
	}
}

// TestIngest_exactDuplicate verifies re-uploading byte-identical content returns
// a duplicate result pointing at the original and creates no second photo.
func TestIngest_exactDuplicate(t *testing.T) {
	env := newEnv(t, config.DuplicateConfig{Enabled: true, PhashMaxDiff: 8})
	ctx := t.Context()
	data := jpegBytes(t, 10, 180, 90, 88)

	first := env.ingest(ctx, data, "first.jpg")
	if first.Outcome != ingest.OutcomeCreated {
		t.Fatalf("first upload = %+v, want created", first)
	}

	second := env.ingest(ctx, data, "second.jpg")
	if second.Outcome != ingest.OutcomeDuplicate || second.Status != 409 {
		t.Fatalf("second upload = %+v, want duplicate/409", second)
	}
	if second.PhotoUID != first.PhotoUID {
		t.Errorf("duplicate UID = %q, want %q", second.PhotoUID, first.PhotoUID)
	}
}

// TestIngest_batchMixedOutcomes verifies a batch reports created/duplicate per
// file independently.
func TestIngest_batchMixedOutcomes(t *testing.T) {
	env := newEnv(t, config.DuplicateConfig{Enabled: false})
	ctx := t.Context()

	red := jpegBytes(t, 220, 20, 20, 90)
	blue := jpegBytes(t, 20, 20, 220, 90)

	r1 := env.ingest(ctx, red, "red.jpg")
	r2 := env.ingest(ctx, red, "red-again.jpg") // exact duplicate of r1
	r3 := env.ingest(ctx, blue, "blue.jpg")     // new

	if r1.Outcome != ingest.OutcomeCreated {
		t.Errorf("r1 = %+v, want created", r1)
	}
	if r2.Outcome != ingest.OutcomeDuplicate || r2.PhotoUID != r1.PhotoUID {
		t.Errorf("r2 = %+v, want duplicate of r1", r2)
	}
	if r3.Outcome != ingest.OutcomeCreated || r3.PhotoUID == r1.PhotoUID {
		t.Errorf("r3 = %+v, want a distinct created photo", r3)
	}
}

// TestIngest_nearDuplicateWarning verifies a perceptually similar but
// byte-distinct image (same picture, different JPEG quality) is created but
// flagged with a near_duplicate warning.
func TestIngest_nearDuplicateWarning(t *testing.T) {
	env := newEnv(t, config.DuplicateConfig{Enabled: true, PhashMaxDiff: 12})
	ctx := t.Context()

	first := env.ingest(ctx, jpegBytes(t, 120, 120, 120, 95), "hi.jpg")
	if first.Outcome != ingest.OutcomeCreated {
		t.Fatalf("first = %+v, want created", first)
	}

	second := env.ingest(ctx, jpegBytes(t, 120, 120, 120, 40), "lo.jpg")
	if second.Outcome != ingest.OutcomeCreated {
		t.Fatalf("second = %+v, want created (near-dup must not block)", second)
	}
	if !hasWarning(second.Warnings, "near_duplicate") {
		t.Errorf("second.Warnings = %+v, want a near_duplicate warning", second.Warnings)
	}
}

// TestIngest_concurrentIdentical verifies that many simultaneous uploads of the
// same content yield exactly one created photo and clean duplicates for the
// rest, with no corruption or 500s.
func TestIngest_concurrentIdentical(t *testing.T) {
	env := newEnv(t, config.DuplicateConfig{Enabled: false})
	ctx := t.Context()
	data := jpegBytes(t, 77, 88, 99, 85)

	const n = 8
	results := make([]ingest.FileResult, n)
	var wg sync.WaitGroup
	for i := range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			results[i] = env.svc.Ingest(ctx, bytes.NewReader(data), "race.jpg", env.uploader)
		}()
	}
	wg.Wait()

	created, duplicate, uid := 0, 0, ""
	for _, res := range results {
		switch res.Outcome {
		case ingest.OutcomeCreated:
			created++
			uid = res.PhotoUID
		case ingest.OutcomeDuplicate:
			duplicate++
		case ingest.OutcomeError:
			t.Fatalf("concurrent upload errored: %+v", res)
		}
	}
	if created != 1 || duplicate != n-1 {
		t.Fatalf("created=%d duplicate=%d, want 1 created and %d duplicates", created, duplicate, n-1)
	}

	// Exactly one photo row must exist for the content hash, and every duplicate
	// must point at it.
	if _, err := env.store.GetByUID(ctx, uid); err != nil {
		t.Fatalf("winning photo missing: %v", err)
	}
	for _, res := range results {
		if res.Outcome == ingest.OutcomeDuplicate && res.PhotoUID != "" && res.PhotoUID != uid {
			t.Errorf("duplicate points at %q, want %q", res.PhotoUID, uid)
		}
	}
}

// hasWarning reports whether warnings contains one with the given code.
func hasWarning(warnings []ingest.Warning, code string) bool {
	for _, w := range warnings {
		if w.Code == code {
			return true
		}
	}
	return false
}
