//go:build integration

package rekey

import (
	"errors"
	"testing"

	"github.com/panbotka/kukatko/internal/audit"
	"github.com/panbotka/kukatko/internal/project"
	"github.com/panbotka/kukatko/internal/storekeys"
)

// TestService_familyExportScheduledAfterTheCopy verifies the race between
// Finish copying the root families.yaml under the prefix and a family export
// job writing the fresh file there: a job that runs between Finish's listing
// and its copy has its file overwritten by the pre-move root one, so Finish
// must schedule the export only after the copy — the queued job then writes the
// fresh file last.
func TestService_familyExportScheduledAfterTheCopy(t *testing.T) {
	h := newHarness(t)
	photo := h.seedPhoto("ph1", "2023/01/a.jpg", false)
	stale, fresh := []byte("families before the move"), []byte("families with the relation added during it")
	h.mem.put(storekeys.FamiliesKey, stale)
	target := bootstrapTarget + storekeys.FamiliesKey

	// The queue: a family export job is queued (a relation changed during the
	// move); running it writes the fresh export under the project's prefix.
	queued := true
	runQueued := func() {
		if queued {
			queued = false
			h.mem.put(target, fresh)
		}
	}
	h.jobs.onFamilies = func() { queued = true }
	run := h.start()
	if err := h.stepToEnd(run, 5); err != nil {
		t.Fatalf("run: %v", err)
	}
	// A worker claims the queued export right after Finish listed the prefix.
	h.svc.hook = func(stage string) error {
		if stage == stageFamiliesListed {
			runQueued()
		}
		return nil
	}

	done, err := h.svc.Finish(t.Context(), run)
	if err != nil || done.State != StateDone {
		t.Fatalf("Finish = %+v, %v; want done", done, err)
	}
	runQueued() // the worker drains the queue
	if data, _ := h.mem.get(target); string(data) != string(fresh) {
		t.Errorf("%s = %q after the export jobs ran, want the fresh export", target, data)
	}
	if h.mem.has(storekeys.FamiliesKey) {
		t.Error("the root families.yaml is left")
	}
	h.assertMoved(photo)
}

// TestService_uploadInFlightAtStartIsTakenWithoutARetry verifies an upload that
// read the project's prefix before Start and stored at the root after the pass
// went past its key: Finish finds the photo the pass never saw, rewinds the pass
// instead of failing the run, and the next steps move it — the move ends done
// with the root empty and no Retry.
func TestService_uploadInFlightAtStartIsTakenWithoutARetry(t *testing.T) {
	h := newHarness(t)
	first := h.seedPhoto("ph1", "2023/05/a.jpg", false)
	run := h.start()
	if err := h.stepToEnd(run, 5); err != nil {
		t.Fatalf("run: %v", err)
	}
	// The in-flight upload commits at the root, below the cursor.
	late := h.seedPhoto("ph2", "2023/01/late.jpg", false)

	if _, err := h.svc.Finish(t.Context(), run); !errors.Is(err, ErrPendingCleanup) {
		t.Fatalf("Finish with an unseen photo = %v, want ErrPendingCleanup", err)
	}
	if state := h.status().State; state != StateRunning {
		t.Fatalf("the run is %s after Finish found an unseen photo, want running", state)
	}
	if err := h.stepToEnd(run, 5); err != nil {
		t.Fatalf("the rewound pass: %v", err)
	}
	done, err := h.svc.Finish(t.Context(), run)
	if err != nil || done.State != StateDone {
		t.Fatalf("Finish after the rewound pass = %+v, %v; want done", done, err)
	}
	h.assertMoved(first)
	h.assertMoved(late)
	h.assertRootEmpty()
}

// TestService_unseenPhotoAfterErrorsNeedsARetry verifies a pass that recorded
// an error is not rewound: Finish fails the run with ErrPhotosRemain, and the
// operator's Retry takes the failed photo and the unseen upload alike.
func TestService_unseenPhotoAfterErrorsNeedsARetry(t *testing.T) {
	h := newHarness(t)
	bad := h.seedPhoto("ph1", "2023/05/a.jpg", false)
	h.mem.corrupt[bootstrapTarget+bad.filePath] = true
	run := h.start()
	if err := h.stepToEnd(run, 5); err != nil {
		t.Fatalf("run: %v", err)
	}
	late := h.seedPhoto("ph2", "2023/01/late.jpg", false)

	if failed, err := h.svc.Finish(t.Context(), run); !errors.Is(err, ErrPhotosRemain) || failed.State != StateFailed {
		t.Fatalf("Finish = %+v, %v; want failed with ErrPhotosRemain", failed, err)
	}
	delete(h.mem.corrupt, bootstrapTarget+bad.filePath)
	if _, err := h.svc.Retry(t.Context(), audit.Meta{}, project.BootstrapUID); err != nil {
		t.Fatalf("Retry: %v", err)
	}
	if err := h.stepToEnd(run, 5); err != nil {
		t.Fatalf("retried pass: %v", err)
	}
	if done, err := h.svc.Finish(t.Context(), run); err != nil || done.State != StateDone {
		t.Fatalf("Finish after the retry = %+v, %v; want done", done, err)
	}
	h.assertMoved(bad)
	h.assertMoved(late)
	h.assertRootEmpty()
}
