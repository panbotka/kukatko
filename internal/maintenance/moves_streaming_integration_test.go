//go:build integration

package maintenance_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
	"testing"

	"github.com/panbotka/kukatko/internal/audit"
	"github.com/panbotka/kukatko/internal/hls"
	"github.com/panbotka/kukatko/internal/hlsjob"
	"github.com/panbotka/kukatko/internal/maintenance"
	"github.com/panbotka/kukatko/internal/photos"
	"github.com/panbotka/kukatko/internal/project"
	"github.com/panbotka/kukatko/internal/storage"
)

// These tests cover the streaming half of the integrity check across the two
// storage layouts — a video whose row a storage move put under its project's
// prefix while its segments wait at the root, and segments under a project's
// own projects/<uid>/hls/ — over the filesystem store and, when
// KUKATKO_TEST_S3_ENDPOINT is set, over the object store.

// bootstrapPrefix is the storage prefix the bootstrap project moves under.
var bootstrapPrefix = project.StoragePrefixFor(project.BootstrapUID)

// eachStore runs body over a filesystem harness and, when the integration
// bucket is configured, over an object-store one.
func eachStore(t *testing.T, body func(t *testing.T, h *harness)) {
	t.Helper()
	t.Run("fs", func(t *testing.T) { body(t, newHarness(t)) })
	t.Run("bucket", func(t *testing.T) {
		h, _, _ := bucketHarness(t)
		body(t, h)
	})
}

// putSegmentAt writes one streaming object of fileHash's rendition under the
// storage prefix through the storage layer and returns its key.
func (h *harness) putSegmentAt(t *testing.T, prefix, fileHash, rendition, name string, size int) string {
	t.Helper()
	key, err := hls.Key(prefix, fileHash, rendition, name)
	if err != nil {
		t.Fatalf("hls.Key(%s, %s, %s, %s): %v", prefix, fileHash, rendition, name, err)
	}
	payload := bytes.Repeat([]byte{0x24}, size)
	sum := sha256.Sum256(payload)
	if err := h.storage.Put(context.Background(), bytes.NewReader(payload), storage.StoredFile{
		Hash: hex.EncodeToString(sum[:]), RelPath: key, Size: int64(size), MIME: hls.MIMEFor(name),
	}); err != nil {
		t.Fatalf("storage.Put(%s): %v", key, err)
	}
	return key
}

// commitUnderPrefix points video's row and file row under the bootstrap
// project's prefix, as a storage move's commit does before it copies the
// derived objects, and returns the photo as it now is.
func (h *harness) commitUnderPrefix(t *testing.T, video photos.Photo) photos.Photo {
	t.Helper()
	moved := bootstrapPrefix + video.FilePath
	for _, sql := range []string{
		`UPDATE photos SET file_path = $2 WHERE uid = $1`,
		`UPDATE photo_files SET file_path = $2 WHERE photo_uid = $1`,
	} {
		if _, err := h.db.Pool().Exec(context.Background(), sql, video.UID, moved); err != nil {
			t.Fatalf("moving the row of %s: %v", video.UID, err)
		}
	}
	video.FilePath = moved
	return video
}

// renditionCount returns how many rendition rows video has.
func (h *harness) renditionCount(t *testing.T, video photos.Photo) int {
	t.Helper()
	var n int
	if err := h.db.Pool().QueryRow(context.Background(),
		`SELECT count(*) FROM photo_hls_renditions WHERE photo_uid = $1`, video.UID).Scan(&n); err != nil {
		t.Fatalf("counting renditions: %v", err)
	}
	return n
}

// TestRepairMissingRenditionsRefusedDuringAMove verifies the missing-rendition
// repair is refused with ErrStorageMoveActive while a storage move is running
// or paused — a video the move committed but has not copied the segments of
// would read as missing — and withdraws nothing meanwhile.
func TestRepairMissingRenditionsRefusedDuringAMove(t *testing.T) {
	eachStore(t, func(t *testing.T, h *harness) {
		ctx := context.Background()
		mover := h.withMoves(t)
		video := h.storeVideo(t, "clip", 0x61)
		h.encodeVideo(t, video, hls.Rendition1080p)
		if _, err := mover.Start(ctx, audit.Meta{}, project.BootstrapUID); err != nil {
			t.Fatalf("Start: %v", err)
		}
		h.commitUnderPrefix(t, video)

		for _, state := range []string{"running", "paused"} {
			if state == "paused" {
				if _, err := mover.Pause(ctx, audit.Meta{}, project.BootstrapUID); err != nil {
					t.Fatalf("Pause: %v", err)
				}
			}
			_, err := h.svc.Repair(ctx, maintenance.RepairOptions{MissingRenditions: true}, audit.Meta{})
			if !errors.Is(err, maintenance.ErrStorageMoveActive) {
				t.Errorf("Repair with the move %s = %v, want ErrStorageMoveActive", state, err)
			}
			if n := h.renditionCount(t, video); n != 1 {
				t.Fatalf("the move %s: %d rendition rows left, want the one kept", state, n)
			}
		}
	})
}

// TestRepairMissingRenditionsKeepsAFailedMovesRootSegments verifies a video a
// failed storage move committed under the prefix, its segments left at the
// root for the move's retry, is neither reported as missing nor withdrawn by
// the repair once the move has ended: the segments are all there, at the root.
// A video whose segments are under neither layout still is.
func TestRepairMissingRenditionsKeepsAFailedMovesRootSegments(t *testing.T) {
	eachStore(t, func(t *testing.T, h *harness) {
		ctx := context.Background()
		h.withMoves(t)
		waiting := h.storeVideo(t, "waiting", 0x62)
		h.encodeVideo(t, waiting, hls.Rendition1080p)
		waiting = h.commitUnderPrefix(t, waiting)
		lost := h.storeVideo(t, "lost", 0x63)
		lostKeys := h.encodeVideo(t, lost, hls.Rendition1080p)
		lost = h.commitUnderPrefix(t, lost)
		for _, key := range lostKeys {
			if err := h.storage.Delete(ctx, key); err != nil {
				t.Fatalf("deleting %s: %v", key, err)
			}
		}

		report, err := h.svc.Scan(ctx)
		if err != nil {
			t.Fatalf("Scan: %v", err)
		}
		want := lost.UID + "/" + hls.Rendition1080p
		if report.MissingRenditions.Count != 1 || report.MissingRenditions.Samples[0] != want {
			t.Errorf("MissingRenditions = %+v, want only %s", report.MissingRenditions, want)
		}
		res, err := h.svc.Repair(ctx, maintenance.RepairOptions{MissingRenditions: true}, audit.Meta{})
		if err != nil {
			t.Fatalf("Repair: %v", err)
		}
		if res.RenditionsDropped != 1 {
			t.Errorf("RenditionsDropped = %d, want 1 (the lost video's)", res.RenditionsDropped)
		}
		if n := h.renditionCount(t, waiting); n != 1 {
			t.Errorf("the waiting video has %d rendition rows, want its row kept", n)
		}
		if n := h.renditionCount(t, lost); n != 0 {
			t.Errorf("the lost video has %d rendition rows, want its row withdrawn", n)
		}
	})
}

// TestScanReportsOrphanSegmentsUnderAProjectPrefix verifies the orphan scan and
// sweep look under every storage layout: an abandoned rendition under a moved
// project's projects/<uid>/hls/ is reported (sampled with its prefix) and
// swept, while the recorded rendition beside it is neither.
func TestScanReportsOrphanSegmentsUnderAProjectPrefix(t *testing.T) {
	eachStore(t, func(t *testing.T, h *harness) {
		ctx := context.Background()
		if _, err := h.db.Pool().Exec(ctx, `UPDATE projects SET storage_prefix = $2 WHERE uid = $1`,
			project.BootstrapUID, bootstrapPrefix); err != nil {
			t.Fatalf("moving the project: %v", err)
		}
		video := h.commitUnderPrefix(t, h.storeVideo(t, "clip", 0x64))
		live := []string{
			h.putSegmentAt(t, bootstrapPrefix, video.FileHash, hls.Rendition1080p, hls.InitName, 300),
			h.putSegmentAt(t, bootstrapPrefix, video.FileHash, hls.Rendition1080p, "00000.m4s", 900),
		}
		h.recordRendition(t, video, hls.Rendition1080p)
		orphans := []string{
			h.putSegmentAt(t, bootstrapPrefix, video.FileHash, "720p", hls.InitName, 200),
			h.putSegmentAt(t, bootstrapPrefix, video.FileHash, "720p", "00000.m4s", 500),
		}

		report, err := h.svc.Scan(ctx)
		if err != nil {
			t.Fatalf("Scan: %v", err)
		}
		group := strings.TrimSuffix(orphans[0], hls.InitName)
		if report.OrphanSegments.Count != len(orphans) || report.OrphanSegments.Bytes != 700 ||
			len(report.OrphanSegments.Samples) != 1 || report.OrphanSegments.Samples[0] != group {
			t.Errorf("OrphanSegments = %+v, want the %d objects of %s (700 bytes)",
				report.OrphanSegments, len(orphans), group)
		}
		if report.MissingRenditions.Count != 0 {
			t.Errorf("MissingRenditions = %+v, want none", report.MissingRenditions)
		}

		res, err := h.svc.Repair(ctx, maintenance.RepairOptions{DeleteOrphanSegments: true}, audit.Meta{})
		if err != nil {
			t.Fatalf("Repair: %v", err)
		}
		if res.OrphanSegmentsDeleted != len(orphans) {
			t.Errorf("OrphanSegmentsDeleted = %d, want %d", res.OrphanSegmentsDeleted, len(orphans))
		}
		for _, key := range orphans {
			if h.objectExists(t, key) {
				t.Errorf("orphan %s survived the sweep", key)
			}
		}
		for _, key := range live {
			if !h.objectExists(t, key) {
				t.Errorf("recorded segment %s was swept", key)
			}
		}
	})
}

// recordRendition records a finished rendition row for video without
// publishing any object, for a test that places the objects itself.
func (h *harness) recordRendition(t *testing.T, video photos.Photo, rendition string) {
	t.Helper()
	if _, err := h.renditions.Save(context.Background(), hlsjob.Encoded{
		PhotoUID: video.UID, Rendition: rendition, Playlist: "#EXTM3U\n#EXT-X-TARGETDURATION:4\n",
		Width: 1920, Height: 1080, Bandwidth: 4_000_000, Codecs: "avc1.640028,mp4a.40.2",
		SegmentCount: 1, DurationMs: 4000,
	}); err != nil {
		t.Fatalf("renditions.Save(%s): %v", video.UID, err)
	}
}
