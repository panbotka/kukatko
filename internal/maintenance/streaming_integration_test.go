//go:build integration

package maintenance_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"testing"
	"time"

	"github.com/panbotka/kukatko/internal/audit"
	"github.com/panbotka/kukatko/internal/hls"
	"github.com/panbotka/kukatko/internal/hlsjob"
	"github.com/panbotka/kukatko/internal/maintenance"
	"github.com/panbotka/kukatko/internal/photos"
	"github.com/panbotka/kukatko/internal/storage"
)

// storeVideo catalogues a video with a real original in the store, so the
// streaming half of the scan has something to reconcile. The bytes are the same
// tiny JPEG the other fixtures use — nothing here decodes them, what matters is
// that the file exists and the catalogue calls it a video.
func (h *harness) storeVideo(t *testing.T, name string, seed uint8) photos.Photo {
	t.Helper()
	ctx := context.Background()
	stored, err := h.storage.Store(ctx, bytes.NewReader(tinyJPEG(t, seed)),
		time.Date(2023, 6, 1, 0, 0, 0, 0, time.UTC), name+".mp4")
	if err != nil {
		t.Fatalf("storage.Store(%s): %v", name, err)
	}
	created, err := h.photos.Create(ctx, photos.Photo{
		FileHash: stored.Hash, FilePath: stored.RelPath, FileName: name + ".mp4",
		FileSize: stored.Size, FileMime: "video/mp4", FileWidth: 1920, FileHeight: 1080,
		FileOrientation: 1, TakenAtSource: "unknown", MediaType: photos.MediaVideo,
	})
	if err != nil {
		t.Fatalf("photos.Create(%s): %v", name, err)
	}
	if _, err := h.photos.CreateFile(ctx, photos.PhotoFile{
		PhotoUID: created.UID, FilePath: stored.RelPath, FileHash: stored.Hash,
		FileSize: stored.Size, FileMime: "video/mp4", IsPrimary: true, Role: photos.RoleOriginal,
	}); err != nil {
		t.Fatalf("photos.CreateFile(%s): %v", name, err)
	}
	return created
}

// putSegment writes one streaming object through the storage layer, exactly as
// the encoder publishes it, and returns its key. Going through Put rather than
// the filesystem is what lets this fixture run over the object store too.
func (h *harness) putSegment(t *testing.T, fileHash, rendition, name string, size int) string {
	t.Helper()
	key, err := hls.Key(fileHash, rendition, name)
	if err != nil {
		t.Fatalf("hls.Key(%s, %s, %s): %v", fileHash, rendition, name, err)
	}
	payload := bytes.Repeat([]byte{0x42}, size)
	sum := sha256.Sum256(payload)
	if err := h.storage.Put(context.Background(), bytes.NewReader(payload), storage.StoredFile{
		Hash: hex.EncodeToString(sum[:]), RelPath: key, Size: int64(size), MIME: hls.MIMEFor(name),
	}); err != nil {
		t.Fatalf("storage.Put(%s): %v", key, err)
	}
	return key
}

// encodeVideo publishes a rendition's objects and records the row describing
// them, which together are what a finished encode leaves behind.
func (h *harness) encodeVideo(t *testing.T, photo photos.Photo, rendition string) []string {
	t.Helper()
	keys := []string{
		h.putSegment(t, photo.FileHash, rendition, hls.InitName, 800),
		h.putSegment(t, photo.FileHash, rendition, "00000.m4s", 4096),
		h.putSegment(t, photo.FileHash, rendition, "00001.m4s", 4096),
	}
	if _, err := h.renditions.Save(context.Background(), hlsjob.Encoded{
		PhotoUID: photo.UID, Rendition: rendition,
		Playlist:  "#EXTM3U\n#EXT-X-TARGETDURATION:4\n",
		Width:     1920,
		Height:    1080,
		Bandwidth: 4_000_000, Codecs: "avc1.640028,mp4a.40.2",
		SegmentCount: 2, DurationMs: 8000,
	}); err != nil {
		t.Fatalf("renditions.Save(%s): %v", photo.UID, err)
	}
	return keys
}

// objectExists reports whether the store still holds key.
func (h *harness) objectExists(t *testing.T, key string) bool {
	t.Helper()
	_, err := h.storage.Stat(context.Background(), key)
	return err == nil
}

// TestScanStreamingHealthyVideoIsClean verifies a video whose rendition is
// recorded and whose objects are all in the store produces neither finding — its
// own segments must not read as orphans of themselves.
func TestScanStreamingHealthyVideoIsClean(t *testing.T) {
	h := newHarness(t)
	video := h.storeVideo(t, "clip", 0x40)
	h.encodeVideo(t, video, hls.Rendition1080p)

	report, err := h.svc.Scan(context.Background())
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if report.MissingRenditions.Count != 0 {
		t.Errorf("MissingRenditions = %+v, want none", report.MissingRenditions)
	}
	if report.OrphanSegments.Count != 0 || report.OrphanSegments.Bytes != 0 {
		t.Errorf("OrphanSegments = %+v, want none", report.OrphanSegments)
	}
	if report.OrphanFiles.Count != 0 {
		t.Errorf("OrphanFiles = %+v; streaming objects are not originals", report.OrphanFiles)
	}
}

// TestScanStreamingReportsAndRepairsAMissingRendition verifies a rendition whose
// objects have left the store is reported, and that the repair withdraws the row
// while leaving the video's original where it is.
func TestScanStreamingReportsAndRepairsAMissingRendition(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	video := h.storeVideo(t, "clip", 0x41)
	keys := h.encodeVideo(t, video, hls.Rendition1080p)
	for _, key := range keys {
		if err := h.storage.Delete(ctx, key); err != nil {
			t.Fatalf("storage.Delete(%s): %v", key, err)
		}
	}

	report, err := h.svc.Scan(ctx)
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	want := video.UID + "/" + hls.Rendition1080p
	if report.MissingRenditions.Count != 1 || report.MissingRenditions.Samples[0] != want {
		t.Fatalf("MissingRenditions = %+v, want one sample %q", report.MissingRenditions, want)
	}
	if report.Clean() {
		t.Error("a library whose catalogue promises segments that are gone is not clean")
	}

	res, err := h.svc.Repair(ctx, maintenance.RepairOptions{MissingRenditions: true}, audit.Meta{})
	if err != nil {
		t.Fatalf("Repair: %v", err)
	}
	if res.RenditionsDropped != 1 {
		t.Errorf("RenditionsDropped = %d, want 1", res.RenditionsDropped)
	}
	rows, err := h.renditions.ListForPhoto(ctx, video.UID)
	if err != nil {
		t.Fatalf("ListForPhoto: %v", err)
	}
	if len(rows) != 0 {
		t.Errorf("renditions after the repair = %+v, want none", rows)
	}
	if !h.objectExists(t, video.FilePath) {
		t.Error("the repair removed the video's original; the integrity check never deletes originals")
	}

	after, err := h.svc.Scan(ctx)
	if err != nil {
		t.Fatalf("Scan after repair: %v", err)
	}
	if after.MissingRenditions.Count != 0 {
		t.Errorf("MissingRenditions after the repair = %+v, want none", after.MissingRenditions)
	}
}

// TestScanStreamingReportsOrphansWithoutDeletingThem verifies objects under the
// streaming prefix that no rendition row claims are counted with their weight and
// sampled by prefix — and that a scan, and a repair that was not asked to sweep,
// both leave every one of them in place.
func TestScanStreamingReportsOrphansWithoutDeletingThem(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	video := h.storeVideo(t, "clip", 0x42)
	h.encodeVideo(t, video, hls.Rendition1080p)
	// A second, unrecorded rendition: what a re-run or a failed encode leaves.
	orphans := []string{
		h.putSegment(t, video.FileHash, "720p", hls.InitName, 300),
		h.putSegment(t, video.FileHash, "720p", "00000.m4s", 700),
	}

	report, err := h.svc.Scan(ctx)
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if report.OrphanSegments.Count != 2 || report.OrphanSegments.Bytes != 1000 {
		t.Fatalf("OrphanSegments = %+v, want 2 objects of 1000 bytes", report.OrphanSegments)
	}
	want := hls.Prefix + "/" + video.FileHash + "/720p/"
	if len(report.OrphanSegments.Samples) != 1 || report.OrphanSegments.Samples[0] != want {
		t.Errorf("OrphanSegments.Samples = %v, want [%s]", report.OrphanSegments.Samples, want)
	}
	if report.MissingRenditions.Count != 0 {
		t.Errorf("MissingRenditions = %+v; the recorded rendition is intact", report.MissingRenditions)
	}

	// The rendition repair is not the sweep: it must leave the objects alone.
	if _, err := h.svc.Repair(ctx, maintenance.RepairOptions{MissingRenditions: true}, audit.Meta{}); err != nil {
		t.Fatalf("Repair: %v", err)
	}
	for _, key := range orphans {
		if !h.objectExists(t, key) {
			t.Errorf("orphan %s was deleted; orphan segments are reported, not swept", key)
		}
	}
}

// TestScanStreamingOffReportsNothing verifies an instance that does not stream
// produces no streaming finding even over a library whose store is full of
// segments no rendition row claims.
func TestScanStreamingOffReportsNothing(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	video := h.storeVideo(t, "clip", 0x43)
	h.putSegment(t, video.FileHash, "720p", hls.InitName, 300)

	report, err := h.serviceWithoutStreaming().Scan(ctx)
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if report.MissingRenditions.Count != 0 || report.OrphanSegments.Count != 0 {
		t.Errorf("findings = %+v / %+v, want none over a non-streaming instance",
			report.MissingRenditions, report.OrphanSegments)
	}
	if report.OrphanSegments.Bytes != 0 {
		t.Errorf("OrphanSegments.Bytes = %d, want 0", report.OrphanSegments.Bytes)
	}

	_, err = h.serviceWithoutStreaming().Repair(
		ctx, maintenance.RepairOptions{DeleteOrphanSegments: true}, audit.Meta{})
	if err == nil {
		t.Error("sweeping orphan segments on a non-streaming instance must refuse")
	}
}
