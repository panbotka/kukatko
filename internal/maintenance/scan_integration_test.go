//go:build integration

package maintenance_test

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/jpeg"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/panbotka/kukatko/internal/backup"
	"github.com/panbotka/kukatko/internal/database/dbtest"
	"github.com/panbotka/kukatko/internal/embedding"
	"github.com/panbotka/kukatko/internal/embedjob"
	"github.com/panbotka/kukatko/internal/facejob"
	"github.com/panbotka/kukatko/internal/jobs"
	"github.com/panbotka/kukatko/internal/maintenance"
	"github.com/panbotka/kukatko/internal/photos"
	"github.com/panbotka/kukatko/internal/storage"
	"github.com/panbotka/kukatko/internal/thumb"
	"github.com/panbotka/kukatko/internal/thumbjob"
	"github.com/panbotka/kukatko/internal/vectors"
)

// These tests run only under `make test-integration` against the database named
// by KUKATKO_TEST_DATABASE_URL, with a temporary originals root and thumbnail
// cache. They share one database and truncate up front, so they do not run in
// parallel.

// seededHash is a valid 64-hex-char file hash used for the missing-original photo
// (which has no real file on disk to hash).
const seededHash = "abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789"

// stubClient is a stand-in embeddings sidecar client. The backfill paths under
// test never call it (they only list missing photos and enqueue), so its methods
// return zero values; Healthy reports reachable.
type stubClient struct{}

func (stubClient) ImageEmbedding(context.Context, io.Reader) ([]float32, string, string, error) {
	return nil, "", "", nil
}

func (stubClient) TextEmbedding(context.Context, string) ([]float32, string, string, error) {
	return nil, "", "", nil
}

func (stubClient) FaceEmbeddings(context.Context, io.Reader) ([]embedding.Face, string, error) {
	return nil, "", nil
}

func (stubClient) Healthy(context.Context) bool { return true }

// diskScanner adapts backup.DiskOriginals to maintenance.DiskScanner.
type diskScanner struct{ disk *backup.DiskOriginals }

func (d diskScanner) List(ctx context.Context) ([]maintenance.DiskFile, error) {
	originals, err := d.disk.List(ctx)
	if err != nil {
		return nil, err
	}
	files := make([]maintenance.DiskFile, len(originals))
	for i, o := range originals {
		files[i] = maintenance.DiskFile{Key: o.Key, Size: o.Size}
	}
	return files, nil
}

// harness bundles the wired collaborators an integration test drives.
type harness struct {
	svc      *maintenance.Service
	photos   *photos.Store
	vectors  *vectors.Store
	storage  *storage.FS
	thumbs   *thumb.Thumbnailer
	jobs     *jobs.Store
	thumbjob *thumbjob.Service
	root     string
}

// newHarness wires a maintenance service over a real database, a temp originals
// root and a temp thumbnail cache.
func newHarness(t *testing.T) *harness {
	t.Helper()
	db := dbtest.New(t)
	dbtest.TruncateAll(t, db)

	root := t.TempDir()
	store, err := storage.NewFS(root)
	if err != nil {
		t.Fatalf("storage.NewFS: %v", err)
	}
	thumbnailer := thumb.New(store, t.TempDir())
	photoStore := photos.NewStore(db.Pool())
	vectorStore := vectors.NewStore(db.Pool())
	jobStore := jobs.NewStore(db.Pool())
	enqueuer := jobs.NewEnqueuer(jobStore)

	embedSvc := embedjob.New(embedjob.Config{
		Photos: photoStore, Vectors: vectorStore, Client: stubClient{},
		Previewer: thumbnailer, Enqueuer: enqueuer,
	})
	faceSvc := facejob.New(facejob.Config{
		Photos: photoStore, Vectors: vectorStore, Client: stubClient{},
		Source: facejob.NewStorageSource(store), Enqueuer: enqueuer,
	})

	svc := maintenance.New(maintenance.Config{
		Photos:    photoStore,
		Vectors:   vectorStore,
		Originals: store,
		Disk:      diskScanner{disk: backup.NewDiskOriginals(root)},
		Thumbs:    maintenance.NewThumbCache(thumbnailer),
		Enqueuer:  enqueuer,
		Embed:     embedSvc,
		Faces:     faceSvc,
	})
	tj := thumbjob.New(thumbjob.Config{
		Photos: photoStore, Thumbnailer: thumbnailer, Decoder: thumbjob.NewStorageDecoder(store),
	})
	return &harness{
		svc: svc, photos: photoStore, vectors: vectorStore, storage: store,
		thumbs: thumbnailer, jobs: jobStore, thumbjob: tj, root: root,
	}
}

// tinyJPEG encodes a 32×32 solid-colour JPEG; distinct seeds yield distinct bytes
// (and therefore distinct content hashes).
func tinyJPEG(t *testing.T, seed uint8) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 32, 32))
	for y := 0; y < 32; y++ {
		for x := 0; x < 32; x++ {
			img.Set(x, y, color.RGBA{R: seed, G: seed ^ 0x5a, B: 0x20, A: 0xff})
		}
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 80}); err != nil {
		t.Fatalf("encode jpeg: %v", err)
	}
	return buf.Bytes()
}

// storeRealPhoto stores a real JPEG original and catalogues it with its primary
// file, returning the created photo.
func (h *harness) storeRealPhoto(t *testing.T, name string, seed uint8) photos.Photo {
	t.Helper()
	ctx := context.Background()
	stored, err := h.storage.Store(ctx, bytes.NewReader(tinyJPEG(t, seed)),
		time.Date(2023, 6, 1, 0, 0, 0, 0, time.UTC), name+".jpg")
	if err != nil {
		t.Fatalf("storage.Store(%s): %v", name, err)
	}
	created, err := h.photos.Create(ctx, photos.Photo{
		FileHash: stored.Hash, FilePath: stored.RelPath, FileName: name + ".jpg",
		FileSize: stored.Size, FileMime: "image/jpeg", FileWidth: 32, FileHeight: 32,
		FileOrientation: 1, TakenAtSource: "unknown",
	})
	if err != nil {
		t.Fatalf("photos.Create(%s): %v", name, err)
	}
	if _, err := h.photos.CreateFile(ctx, photos.PhotoFile{
		PhotoUID: created.UID, FilePath: stored.RelPath, FileHash: stored.Hash,
		FileSize: stored.Size, FileMime: "image/jpeg", IsPrimary: true, Role: photos.RoleOriginal,
	}); err != nil {
		t.Fatalf("photos.CreateFile(%s): %v", name, err)
	}
	return created
}

// seedEmbedding saves a present (non-zero) image embedding for a photo.
func (h *harness) seedEmbedding(t *testing.T, uid string) {
	t.Helper()
	vec := make([]float32, vectors.ImageDim)
	for i := range vec {
		vec[i] = 0.1
	}
	if _, err := h.vectors.SaveEmbedding(context.Background(), vectors.Embedding{
		PhotoUID: uid, Vector: vec, Model: "stub", Pretrained: "stub",
	}); err != nil {
		t.Fatalf("SaveEmbedding(%s): %v", uid, err)
	}
}

// seedThumb writes a placeholder cache file so the photo's representative
// thumbnail counts as present.
func (h *harness) seedThumb(t *testing.T, fileHash string) {
	t.Helper()
	abs, err := h.thumbs.Path(fileHash, "tile_224")
	if err != nil {
		t.Fatalf("thumb.Path(%s): %v", fileHash, err)
	}
	if err := os.MkdirAll(filepath.Dir(abs), 0o750); err != nil {
		t.Fatalf("mkdir cache: %v", err)
	}
	if err := os.WriteFile(abs, []byte("jpeg"), 0o600); err != nil {
		t.Fatalf("seed thumb: %v", err)
	}
}

// TestScanDetectsDrift sets up one clean photo, one missing its thumbnail, one
// missing its original (and embedding), and an orphan file on disk, then verifies
// the scan reports each case.
func TestScanDetectsDrift(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()

	clean := h.storeRealPhoto(t, "clean", 0x10)
	if _, err := h.thumbs.GenerateAll(ctx, clean); err != nil {
		t.Fatalf("GenerateAll(clean): %v", err)
	}
	h.seedEmbedding(t, clean.UID)

	noThumb := h.storeRealPhoto(t, "nothumb", 0x90)
	h.seedEmbedding(t, noThumb.UID)

	noOrig := h.catalogueMissingOriginal(t)
	h.seedThumb(t, seededHash) // so the missing-original photo is not also a missing-thumb

	// An orphan original on disk that no catalogue row references.
	if err := os.MkdirAll(filepath.Join(h.root, "2099", "01"), 0o750); err != nil {
		t.Fatalf("mkdir orphan dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(h.root, "2099", "01", "orphan.jpg"), []byte("data"), 0o600); err != nil {
		t.Fatalf("write orphan: %v", err)
	}

	report, err := h.svc.Scan(ctx)
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	assertFinding(t, "missing originals", report.MissingOriginals, 1, noOrig.UID)
	assertFinding(t, "orphan files", report.OrphanFiles, 1, "2099/01/orphan.jpg")
	assertFinding(t, "missing thumbnails", report.MissingThumbnails, 1, noThumb.UID)
	assertFinding(t, "missing embeddings", report.MissingEmbeddings, 1, noOrig.UID)
	if report.Photos != 3 || report.OriginalsOnDisk != 3 {
		t.Errorf("totals photos=%d disk=%d, want 3/3", report.Photos, report.OriginalsOnDisk)
	}
}

// catalogueMissingOriginal creates a photo whose primary file path points at a
// file that does not exist on disk.
func (h *harness) catalogueMissingOriginal(t *testing.T) photos.Photo {
	t.Helper()
	ctx := context.Background()
	created, err := h.photos.Create(ctx, photos.Photo{
		FileHash: seededHash, FilePath: "2098/01/missing.jpg", FileName: "missing.jpg",
		FileSize: 100, FileMime: "image/jpeg", FileOrientation: 1, TakenAtSource: "unknown",
	})
	if err != nil {
		t.Fatalf("Create(missing): %v", err)
	}
	if _, err := h.photos.CreateFile(ctx, photos.PhotoFile{
		PhotoUID: created.UID, FilePath: "2098/01/missing.jpg", FileHash: seededHash,
		FileSize: 100, FileMime: "image/jpeg", IsPrimary: true, Role: photos.RoleOriginal,
	}); err != nil {
		t.Fatalf("CreateFile(missing): %v", err)
	}
	return created
}

// TestRepairThumbnailsRegenerates verifies the thumbnail repair enqueues a job
// only for the photo missing its thumbnail, and that running the handler actually
// regenerates the cached thumbnail so a re-scan reports no missing thumbnails.
func TestRepairThumbnailsRegenerates(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()

	clean := h.storeRealPhoto(t, "clean", 0x11)
	if _, err := h.thumbs.GenerateAll(ctx, clean); err != nil {
		t.Fatalf("GenerateAll(clean): %v", err)
	}
	noThumb := h.storeRealPhoto(t, "nothumb", 0x22)

	res, err := h.svc.Repair(ctx, maintenance.RepairOptions{Thumbnails: true})
	if err != nil {
		t.Fatalf("Repair(thumbnails): %v", err)
	}
	if res.ThumbnailsEnqueued != 1 {
		t.Fatalf("ThumbnailsEnqueued = %d, want 1", res.ThumbnailsEnqueued)
	}
	if got := h.countJobs(t, jobs.TypeThumbnail); got != 1 {
		t.Fatalf("thumbnail jobs = %d, want 1", got)
	}

	// Run the handler to actually rebuild the cache.
	if err := h.thumbjob.Regenerate(ctx, noThumb.UID); err != nil {
		t.Fatalf("Regenerate: %v", err)
	}
	report, err := h.svc.Scan(ctx)
	if err != nil {
		t.Fatalf("Scan after repair: %v", err)
	}
	if report.MissingThumbnails.Count != 0 {
		t.Errorf("missing thumbnails after repair = %d, want 0", report.MissingThumbnails.Count)
	}
}

// TestRepairEmbeddingsEnqueuesOnlyMissingAndIdempotent verifies the embedding
// backfill enqueues a job for the photo missing an embedding only, and a re-run
// schedules no duplicate job.
func TestRepairEmbeddingsEnqueuesOnlyMissingAndIdempotent(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()

	withEmb := h.storeRealPhoto(t, "withemb", 0x33)
	h.seedEmbedding(t, withEmb.UID)
	h.storeRealPhoto(t, "noemb", 0x44)

	res, err := h.svc.Repair(ctx, maintenance.RepairOptions{Embeddings: true})
	if err != nil {
		t.Fatalf("Repair(embeddings): %v", err)
	}
	if res.EmbeddingsEnqueued != 1 {
		t.Errorf("EmbeddingsEnqueued = %d, want 1 (only the missing one)", res.EmbeddingsEnqueued)
	}
	if got := h.countJobs(t, jobs.TypeImageEmbed); got != 1 {
		t.Fatalf("image_embed jobs = %d, want 1", got)
	}
	// Idempotent re-run: still reports 1 attempt but dedupes to no new job.
	if _, err := h.svc.Repair(ctx, maintenance.RepairOptions{Embeddings: true}); err != nil {
		t.Fatalf("Repair(embeddings) re-run: %v", err)
	}
	if got := h.countJobs(t, jobs.TypeImageEmbed); got != 1 {
		t.Errorf("image_embed jobs after re-run = %d, want 1 (deduped)", got)
	}
}

// countJobs returns the number of jobs of the given type currently in the queue.
func (h *harness) countJobs(t *testing.T, jobType string) int {
	t.Helper()
	counts, err := h.jobs.CountsByType(context.Background())
	if err != nil {
		t.Fatalf("CountsByType: %v", err)
	}
	return counts[jobType]
}

// assertFinding checks a Finding's count and that its samples contain want.
func assertFinding(t *testing.T, name string, f maintenance.Finding, count int, want string) {
	t.Helper()
	if f.Count != count {
		t.Errorf("%s count = %d, want %d (samples %v)", name, f.Count, count, f.Samples)
		return
	}
	for _, s := range f.Samples {
		if s == want {
			return
		}
	}
	t.Errorf("%s samples = %v, want to contain %q", name, f.Samples, want)
}
