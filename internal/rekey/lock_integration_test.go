//go:build integration

package rekey

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/panbotka/kukatko/internal/audit"
	"github.com/panbotka/kukatko/internal/photos"
	"github.com/panbotka/kukatko/internal/project"
	"github.com/panbotka/kukatko/internal/storekeys"
	"github.com/panbotka/kukatko/internal/thumb"
	"github.com/panbotka/kukatko/internal/trash"
)

// freshTile is what a thumbnail rebuilt after an edit holds.
var freshTile = []byte("fresh tile after the edit")

// tileKey returns the key of photo's first thumbnail size under prefix.
func tileKey(t *testing.T, photo seeded, prefix string) string {
	t.Helper()
	key, err := thumb.RelPath(prefix, sha256Hex(photo.content), thumb.SizeNames()[0])
	if err != nil {
		t.Fatalf("thumbnail key: %v", err)
	}
	return key
}

// publishTile does what the thumbnail publisher does for photo's first size:
// under the gate's photo lock, it re-reads the photo's key and puts freshTile
// under the prefix found there.
func (h *harness) publishTile(gate *Gate, photo seeded) error {
	return gate.Publish(context.Background(), project.BootstrapUID, photo.uid, func(filePath string, found bool) error {
		if !found {
			h.t.Errorf("publish: photo %s has no row", photo.uid)
			return nil
		}
		h.mem.put(tileKey(h.t, photo, storekeys.PrefixOf(filePath)), freshTile)
		return nil
	})
}

// assertFreshTile fails the test unless the photo's tile under the new prefix
// is the fresh one and none is left at the root.
func (h *harness) assertFreshTile(photo seeded) {
	h.t.Helper()
	moved := tileKey(h.t, photo, bootstrapTarget)
	if data, _ := h.mem.get(moved); string(data) != string(freshTile) {
		h.t.Errorf("tile under the prefix %s = %q, want the fresh one", moved, data)
	}
	if root := tileKey(h.t, photo, ""); h.mem.has(root) {
		h.t.Errorf("a tile is left at the root %s", root)
	}
}

// TestService_freshTilePublishedDuringTheCopySurvives verifies the window
// between committing a photo under the new prefix and copying its derived
// objects: a thumbnail rebuilt after an edit in that window does not publish
// (the move holds the photo; the job defers) and, published by the deferred
// re-run, is what the bucket serves — the move never copies the pre-edit root
// tile over it.
func TestService_freshTilePublishedDuringTheCopySurvives(t *testing.T) {
	h := newHarness(t)
	photo := h.seedPhoto("ph1", "2023/01/a.jpg", false)
	run := h.start()
	gate := NewGate(h.db.Pool())
	deferred := false
	h.svc.hook = func(stage string) error {
		if stage != stageCommitted {
			return nil
		}
		err := h.publishTile(gate, photo)
		switch {
		case errors.Is(err, storekeys.ErrMoving):
			deferred = true
		case err != nil:
			t.Errorf("publish between commit and copy: %v", err)
		}
		return nil
	}

	if err := h.stepToEnd(run, 5); err != nil {
		t.Fatalf("run: %v", err)
	}
	h.svc.hook = nil
	if !deferred {
		t.Error("the publish in the window between commit and copy was not refused")
	}
	// The deferred job runs again once the move is through with the photo.
	if err := h.publishTile(gate, photo); err != nil {
		t.Fatalf("the deferred publish: %v", err)
	}
	h.assertFreshTile(photo)
}

// TestService_aPublishInFlightHoldsTheMoveOff verifies the other direction: a
// publisher that re-read the photo at the root and is uploading there holds the
// move off the photo until its upload is in, so the move then takes the fresh
// tile along instead of leaving it as a root orphan (with the stale copy under
// the prefix).
func TestService_aPublishInFlightHoldsTheMoveOff(t *testing.T) {
	h := newHarness(t)
	photo := h.seedPhoto("ph1", "2023/01/a.jpg", false)
	run := h.start()
	gate := NewGate(h.db.Pool())

	stepped := make(chan error, 1)
	err := gate.Publish(t.Context(), project.BootstrapUID, photo.uid, func(filePath string, _ bool) error {
		if filePath != photo.filePath {
			t.Fatalf("publisher read %s, want the root key %s", filePath, photo.filePath)
		}
		go func() {
			_, _, err := h.svc.Step(context.Background(), run, 5)
			stepped <- err
		}()
		// The step copies the original and then waits for the photo's lock.
		time.Sleep(300 * time.Millisecond)
		if got, _ := h.photoPath(photo.uid); got != photo.filePath {
			t.Errorf("the move committed the photo (%s) while a publish held it", got)
		}
		h.mem.put(tileKey(t, photo, ""), freshTile)
		return nil
	})
	if err != nil {
		t.Fatalf("Publish: %v", err)
	}
	if err := <-stepped; err != nil {
		t.Fatalf("Step: %v", err)
	}
	if err := h.stepToEnd(run, 5); err != nil {
		t.Fatalf("run: %v", err)
	}
	h.assertFreshTile(photo)
}

// photoObjects returns every key of the store that belongs to photo, under the
// root or the new prefix: its original, sidecar and derived objects.
func (h *harness) photoObjects(photo seeded) []string {
	h.t.Helper()
	hash := sha256Hex(photo.content)
	var found []string
	for _, key := range h.mem.keys() {
		_, rest := storekeys.Strip(key)
		if rest == photo.filePath || strings.Contains(rest, hash) || strings.Contains(rest, photo.filePath) {
			found = append(found, key)
		}
	}
	return found
}

// noThumbCache is the trash purge's local thumbnail cache in these tests: the
// harness has none, every thumbnail lives in the fake store.
type noThumbCache struct{}

// Remove has no cache to remove from.
func (noThumbCache) Remove(string, string) error { return nil }

// purge moves photo to the trash and purges it with the real trash service over
// the fake store, under the gate: everything it deletes — the original, the
// sidecar and every derived object, under whichever layout holds it — it finds
// itself, from the photo's rows and the store's listing.
func (h *harness) purge(gate *Gate, photo seeded) error {
	ctx := context.Background()
	pool := h.db.Pool()
	if _, err := pool.Exec(ctx, `UPDATE photos SET archived_at = now() WHERE uid = $1 AND project_id = $2`,
		photo.uid, project.BootstrapUID); err != nil {
		return err //nolint:wrapcheck // test helper.
	}
	svc := trash.New(trash.Config{
		Photos: photos.NewStore(pool), Projects: project.NewStore(pool), Storage: h.mem,
		Thumbnailer: noThumbCache{}, Moves: gate,
	})
	return svc.PurgePhoto(ctx, project.BootstrapUID, photo.uid, audit.Meta{}) //nolint:wrapcheck // test helper.
}

// assertRootEmpty fails the test for every key the fake store still holds
// outside the bootstrap project's new prefix.
func (h *harness) assertRootEmpty() {
	h.t.Helper()
	for _, key := range h.mem.keys() {
		if !strings.HasPrefix(key, bootstrapTarget) {
			h.t.Errorf("%s is left at the root", key)
		}
	}
}

// TestService_purgeOfAPhotoWithDerivedAtTheRootLeavesNothing verifies a purge of
// a photo the move committed but whose streaming segments it could not copy —
// recorded as a derived failure, the segments still at the root — removes them
// from the root as well: the failure record goes with the photo, so nothing
// else would ever remove them, and the move then finishes with the root empty.
func TestService_purgeOfAPhotoWithDerivedAtTheRootLeavesNothing(t *testing.T) {
	h := newHarness(t)
	clip := h.seedPhoto("ph1", "2023/01/clip.mp4", true)
	kept := h.seedPhoto("ph2", "2023/02/b.jpg", false)
	segment := trackedDerived(clip)[len(trackedDerived(clip))-1]
	h.mem.failCopy[bootstrapTarget+segment] = errTransient
	run := h.start()
	if err := h.stepToEnd(run, 5); err != nil {
		t.Fatalf("run: %v", err)
	}
	if !h.mem.has(segment) {
		t.Fatalf("the failed copy left no segment at the root to purge: %v", h.mem.keys())
	}

	if err := h.purge(NewGate(h.db.Pool()), clip); err != nil {
		t.Fatalf("purge: %v", err)
	}
	if left := h.photoObjects(clip); len(left) != 0 {
		t.Errorf("objects of the purged photo survived: %v", left)
	}
	done, err := h.svc.Finish(t.Context(), run)
	if err != nil || done.State != StateDone {
		t.Fatalf("Finish = %+v, %v; want done", done, err)
	}
	h.assertMoved(kept)
	h.assertRootEmpty()
}

// TestService_purgeBeforeTheCommitLeavesNothing verifies a trash purge that
// lands after the move copied a photo's original but before it committed the
// row: the move finds the photo gone and deletes the copy it made, so nothing of
// the purged photo survives under the new prefix for an orphan import to bring
// back.
func TestService_purgeBeforeTheCommitLeavesNothing(t *testing.T) {
	h := newHarness(t)
	photo := h.seedPhoto("ph1", "2023/01/a.jpg", true)
	kept := h.seedPhoto("ph2", "2023/02/b.jpg", false)
	run := h.start()
	gate := NewGate(h.db.Pool())
	h.svc.hook = func(stage string) error {
		if stage == stageCopied && h.mem.has(bootstrapTarget+photo.filePath) && h.mem.has(photo.filePath) {
			if err := h.purge(gate, photo); err != nil {
				t.Errorf("purge after the copy: %v", err)
			}
		}
		return nil
	}

	if err := h.stepToEnd(run, 5); err != nil {
		t.Fatalf("run: %v", err)
	}
	if left := h.photoObjects(photo); len(left) != 0 {
		t.Errorf("objects of the purged photo survived: %v", left)
	}
	h.assertMoved(kept)
}

// TestService_purgeDuringTheCopyWaitsForTheMove verifies a purge that comes
// while the move holds the photo — committed, its derived objects being copied
// — waits for the move to finish the photo and then deletes everything under
// the new prefix, so neither layout keeps an object of it.
func TestService_purgeDuringTheCopyWaitsForTheMove(t *testing.T) {
	h := newHarness(t)
	photo := h.seedPhoto("ph1", "2023/01/a.jpg", true)
	run := h.start()
	gate := NewGate(h.db.Pool())
	purged := make(chan error, 1)
	h.svc.hook = func(stage string) error {
		if stage == stageCommitted {
			go func() { purged <- h.purge(gate, photo) }()
			// Give the purge time to queue up behind the move's lock.
			time.Sleep(200 * time.Millisecond)
		}
		return nil
	}

	// One step takes the photo; the purge, queued behind its lock, runs once the
	// step is through with it.
	if _, _, err := h.svc.Step(t.Context(), run, 5); err != nil {
		t.Fatalf("Step: %v", err)
	}
	if err := <-purged; err != nil {
		t.Fatalf("purge: %v", err)
	}
	h.svc.hook = nil
	if err := h.stepToEnd(run, 5); err != nil {
		t.Fatalf("run: %v", err)
	}
	if left := h.photoObjects(photo); len(left) != 0 {
		t.Errorf("objects of the purged photo survived: %v", left)
	}
}

// TestService_purgeRefusesAPhotoACrashedMoveHolds verifies a photo a crashed
// step committed but did not finish — its objects under both prefixes — is
// neither purged nor published for (storekeys.ErrMoving) until the next step
// has finished it; afterwards both go through.
func TestService_purgeRefusesAPhotoACrashedMoveHolds(t *testing.T) {
	h := newHarness(t)
	photo := h.seedPhoto("ph1", "2023/01/a.jpg", false)
	run := h.start()
	gate := NewGate(h.db.Pool())
	h.svc.hook = func(stage string) error {
		if stage == stageCommitted {
			return errCrash
		}
		return nil
	}
	if _, _, err := h.svc.Step(t.Context(), run, 5); !errors.Is(err, errCrash) {
		t.Fatalf("crashing Step = %v, want the crash", err)
	}
	h.svc.hook = nil

	if err := h.purge(gate, photo); !errors.Is(err, storekeys.ErrMoving) {
		t.Errorf("purge of the pending photo = %v, want storekeys.ErrMoving", err)
	}
	if err := h.publishTile(gate, photo); !errors.Is(err, storekeys.ErrMoving) {
		t.Errorf("publish for the pending photo = %v, want storekeys.ErrMoving", err)
	}
	if err := h.stepToEnd(run, 5); err != nil {
		t.Fatalf("resumed run: %v", err)
	}
	h.assertMoved(photo)
	if err := h.purge(gate, photo); err != nil {
		t.Fatalf("purge after the move finished the photo: %v", err)
	}
	if left := h.photoObjects(photo); len(left) != 0 {
		t.Errorf("objects of the purged photo survived: %v", left)
	}
}
