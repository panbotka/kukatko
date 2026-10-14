//go:build integration

package maintenance_test

import (
	"context"
	"errors"
	"testing"

	"github.com/panbotka/kukatko/internal/audit"
	"github.com/panbotka/kukatko/internal/hls"
	"github.com/panbotka/kukatko/internal/jobs"
	"github.com/panbotka/kukatko/internal/maintenance"
	"github.com/panbotka/kukatko/internal/project"
	"github.com/panbotka/kukatko/internal/rekey"
)

// withMoves rebuilds the harness's service with the storage move wired as its
// MoveWatcher and returns the move, so a test can start, pause and end runs.
func (h *harness) withMoves(t *testing.T) *rekey.Service {
	t.Helper()
	store, ok := h.storage.(rekey.ObjectStore)
	if !ok {
		t.Fatalf("storage backend %T cannot be moved", h.storage)
	}
	mover := rekey.New(rekey.Config{
		Pool: h.db.Pool(), Store: store, Jobs: jobs.NewEnqueuer(h.jobs),
	})
	cfg := h.cfg
	cfg.Moves = mover
	cfg.RootDerived = mover
	h.svc = maintenance.New(cfg)
	return mover
}

// TestRepairOrphanSegmentsRefusedDuringAMove verifies the orphan-segment sweep —
// and the root-derived repair, the other repair that deletes at the root, and
// the orphan import, which mid-move would re-catalogue a purged photo's copy — is
// refused with ErrStorageMoveActive, deleting nothing, while a storage move is
// running and while it is paused, and runs again once the move has ended.
func TestRepairOrphanSegmentsRefusedDuringAMove(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	mover := h.withMoves(t)
	video := h.storeVideo(t, "clip", 0x51)
	h.encodeVideo(t, video, hls.Rendition1080p)
	orphans := []string{
		h.putSegment(t, video.FileHash, "720p", hls.InitName, 300),
		h.putSegment(t, video.FileHash, "720p", "00000.m4s", 700),
	}
	if _, err := mover.Start(ctx, audit.Meta{}, project.BootstrapUID); err != nil {
		t.Fatalf("Start: %v", err)
	}

	refused := func(state string) {
		t.Helper()
		for _, opts := range []maintenance.RepairOptions{
			{DeleteOrphanSegments: true}, {RootDerived: true}, {ImportOrphans: true},
		} {
			_, err := h.svc.Repair(ctx, opts, audit.Meta{})
			if !errors.Is(err, maintenance.ErrStorageMoveActive) {
				t.Fatalf("Repair(%+v) with the move %s = %v, want ErrStorageMoveActive", opts, state, err)
			}
		}
		for _, key := range orphans {
			if !h.objectExists(t, key) {
				t.Fatalf("orphan %s was deleted while the move was %s", key, state)
			}
		}
	}
	refused("running")
	if _, err := mover.Pause(ctx, audit.Meta{}, project.BootstrapUID); err != nil {
		t.Fatalf("Pause: %v", err)
	}
	refused("paused")

	// The move ends (here: it fails); the sweep is allowed again.
	if _, err := h.db.Pool().Exec(ctx, `UPDATE storage_rekey_runs SET state = 'failed'`); err != nil {
		t.Fatalf("failing the run: %v", err)
	}
	res, err := h.svc.Repair(ctx, maintenance.RepairOptions{DeleteOrphanSegments: true}, audit.Meta{})
	if err != nil {
		t.Fatalf("Repair after the move: %v", err)
	}
	if res.OrphanSegmentsDeleted != len(orphans) {
		t.Errorf("OrphanSegmentsDeleted = %d, want %d", res.OrphanSegmentsDeleted, len(orphans))
	}
	for _, key := range orphans {
		if h.objectExists(t, key) {
			t.Errorf("orphan %s survived the sweep after the move", key)
		}
	}
}

// TestRepairOrphanSegmentsKeepsAMovedVideosRootSegments verifies the root
// segments of a video whose row a storage move has already put under the
// project prefix are not swept as orphans once the move has ended: they are
// what the move left at the root for its retry (or the root-derived repair),
// so the sweep keeps and counts them.
func TestRepairOrphanSegmentsKeepsAMovedVideosRootSegments(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	h.withMoves(t)
	video := h.storeVideo(t, "clip", 0x52)
	rootKeys := h.encodeVideo(t, video, hls.Rendition1080p)
	// The move committed the row and failed to copy the segments.
	if _, err := h.db.Pool().Exec(ctx, `UPDATE photos SET file_path = $2 WHERE uid = $1 AND project_id = $3`,
		video.UID, project.StoragePrefixFor(project.BootstrapUID)+video.FilePath, project.BootstrapUID); err != nil {
		t.Fatalf("moving the row: %v", err)
	}

	res, err := h.svc.Repair(ctx, maintenance.RepairOptions{DeleteOrphanSegments: true}, audit.Meta{})
	if err != nil {
		t.Fatalf("Repair: %v", err)
	}
	if res.OrphanSegmentsDeleted != 0 || res.OrphanSegmentsKept != len(rootKeys) {
		t.Errorf("deleted %d, kept %d; want 0 deleted and the %d root segments kept",
			res.OrphanSegmentsDeleted, res.OrphanSegmentsKept, len(rootKeys))
	}
	for _, key := range rootKeys {
		if !h.objectExists(t, key) {
			t.Errorf("root segment %s of the moved video was deleted", key)
		}
	}
}
