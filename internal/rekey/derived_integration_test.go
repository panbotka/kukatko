//go:build integration

package rekey

import (
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/panbotka/kukatko/internal/audit"
	"github.com/panbotka/kukatko/internal/project"
	"github.com/panbotka/kukatko/internal/storekeys"
)

// trackedDerived returns the thumbnail and streaming keys of photo (not the
// sidecar).
func trackedDerived(photo seeded) []string {
	var out []string
	for _, key := range photo.derived {
		if !strings.HasPrefix(key, storekeys.SidecarDir+"/") {
			out = append(out, key)
		}
	}
	return out
}

// derivedRow reads one photo's derived-failure record; found is false without
// one.
func (h *harness) derivedRow(uid string) (kinds []string, message string, found bool) {
	h.t.Helper()
	err := h.db.Pool().QueryRow(h.t.Context(),
		`SELECT kinds, message FROM storage_rekey_derived_failures WHERE photo_uid = $1`, uid).
		Scan(&kinds, &message)
	if err != nil {
		return nil, "", false
	}
	return kinds, message, true
}

func TestService_derivedFailureKeepsRootAndALaterPassMovesIt(t *testing.T) {
	h := newHarness(t)
	clip := h.seedPhoto("ph1", "2023/01/clip.mp4", true)
	other := h.seedPhoto("ph2", "2023/02/b.jpg", false)
	tracked := trackedDerived(clip)
	// The first thumbnail cannot be copied (a throttled store, not a dead one).
	h.mem.failCopy[bootstrapTarget+tracked[0]] = errTransient
	run := h.start()

	if _, _, err := h.svc.Step(t.Context(), run, 1); err != nil {
		t.Fatalf("first step: %v", err)
	}
	h.checkInvariant()
	if got, _ := h.photoPath(clip.uid); got != bootstrapTarget+clip.filePath {
		t.Fatalf("photo row = %s, want it moved: the original is safe", got)
	}
	if h.mem.has(clip.filePath) {
		t.Error("the old original was kept, want it deleted once the row moved")
	}
	for _, key := range tracked {
		if !h.mem.has(key) {
			t.Errorf("root %s was deleted although a derived copy failed", key)
		}
	}
	status := h.status()
	if status.DonePhotos != 1 || status.DerivedFailed != 1 || status.ErrorCount != 1 {
		t.Errorf("run = done %d, derived_failed %d, errors %d; want 1, 1, 1",
			status.DonePhotos, status.DerivedFailed, status.ErrorCount)
	}
	if kinds, message, found := h.derivedRow(clip.uid); !found || !slices.Equal(kinds, []string{"thumbnail"}) ||
		!strings.Contains(message, errTransient.Error()) {
		t.Errorf("derived record = %v, %q, %v; want the thumbnail kind and the error", kinds, message, found)
	}

	// The store recovers; the rest of the same pass retries the recorded photo.
	delete(h.mem.failCopy, bootstrapTarget+tracked[0])
	if err := h.stepToEnd(run, 1); err != nil {
		t.Fatalf("stepping: %v", err)
	}
	h.assertMoved(clip)
	h.assertMoved(other)
	if got := h.status(); got.DerivedFailed != 0 {
		t.Errorf("derived_failed after the retry = %d, want 0", got.DerivedFailed)
	}
	if _, _, found := h.derivedRow(clip.uid); found {
		t.Error("the derived record survived a successful retry")
	}
	done, err := h.svc.Finish(t.Context(), run)
	if err != nil || done.State != StateDone || done.DerivedFailed != 0 {
		t.Fatalf("Finish = %+v, %v; want done with no derived failure", done, err)
	}
}

func TestService_derivedFailureFailsTheRunUntilRetried(t *testing.T) {
	h := newHarness(t)
	clip := h.seedPhoto("ph1", "2023/01/clip.mp4", true)
	segment := trackedDerived(clip)[len(trackedDerived(clip))-1]
	h.mem.failCopy[bootstrapTarget+segment] = errTransient
	run := h.start()

	if err := h.stepToEnd(run, 5); err != nil {
		t.Fatalf("stepping: %v", err)
	}
	failed, err := h.svc.Finish(t.Context(), run)
	if !errors.Is(err, ErrDerivedRemain) || failed.State != StateFailed || failed.DerivedFailed != 1 {
		t.Fatalf("Finish = %+v, %v; want failed with ErrDerivedRemain and derived_failed 1", failed, err)
	}
	for _, key := range trackedDerived(clip) {
		if !h.mem.has(key) {
			t.Errorf("root %s was deleted while the photo's segments could not all move", key)
		}
	}
	if kinds, _, _ := h.derivedRow(clip.uid); !slices.Equal(kinds, []string{"hls"}) {
		t.Errorf("recorded kinds = %v, want [hls]", kinds)
	}

	delete(h.mem.failCopy, bootstrapTarget+segment)
	retried, err := h.svc.Retry(t.Context(), audit.Meta{}, project.BootstrapUID)
	if err != nil || retried.State != StateRunning || retried.DerivedFailed != 1 {
		t.Fatalf("Retry = %+v, %v; want running with the derived failure still counted", retried, err)
	}
	if err := h.stepToEnd(run, 5); err != nil {
		t.Fatalf("retried pass: %v", err)
	}
	h.assertMoved(clip)
	done, err := h.svc.Finish(t.Context(), run)
	if err != nil || done.State != StateDone || done.DerivedFailed != 0 {
		t.Fatalf("Finish after the retry = %+v, %v; want done", done, err)
	}
}

func TestService_finishSweepsRootDerivedOfMovedPhotos(t *testing.T) {
	h := newHarness(t)
	photo := h.seedPhoto("ph1", "2023/01/a.jpg", false)
	run := h.start()
	if err := h.stepToEnd(run, 5); err != nil {
		t.Fatalf("stepping: %v", err)
	}
	// What a move before the derived bookkeeping left: the photo's row and
	// tiles are under the prefix, yet one tile is also still at the root.
	stale := trackedDerived(photo)[0]
	h.mem.put(stale, []byte("stale root tile"))

	found, err := h.svc.FindRootDerived(t.Context())
	if err != nil || len(found) != 1 || found[0].UID != photo.uid || found[0].ProjectUID != project.BootstrapUID ||
		len(found[0].Moves) != 1 || found[0].Moves[0].From != stale || found[0].Moves[0].To != bootstrapTarget+stale {
		t.Fatalf("FindRootDerived = %+v, %v; want the one stale tile of %s", found, err, photo.uid)
	}

	if _, err := h.svc.Finish(t.Context(), run); !errors.Is(err, ErrPendingCleanup) {
		t.Fatalf("Finish with a stale root tile = %v, want ErrPendingCleanup", err)
	}
	if kinds, message, ok := h.derivedRow(photo.uid); !ok || !slices.Equal(kinds, []string{"thumbnail"}) ||
		message != msgFoundAtRoot {
		t.Errorf("swept record = %v, %q, %v; want the thumbnail found at the root", kinds, message, ok)
	}
	if err := h.stepToEnd(run, 5); err != nil {
		t.Fatalf("stepping the retry: %v", err)
	}
	if h.mem.has(stale) {
		t.Error("the stale root tile is still there")
	}
	if data, _ := h.mem.get(bootstrapTarget + stale); string(data) == "stale root tile" {
		t.Error("the retry copied the stale root tile over the prefixed one")
	}
	done, err := h.svc.Finish(t.Context(), run)
	if err != nil || done.State != StateDone || done.DerivedFailed != 0 {
		t.Fatalf("Finish = %+v, %v; want done", done, err)
	}
}

func TestService_moveRootDerived(t *testing.T) {
	h := newHarness(t)
	photo := h.seedPhoto("ph1", "2023/01/a.jpg", true)
	run := h.start()
	if err := h.stepToEnd(run, 5); err != nil {
		t.Fatalf("stepping: %v", err)
	}
	if _, err := h.svc.Finish(t.Context(), run); err != nil {
		t.Fatalf("Finish: %v", err)
	}
	// Two objects reappear at the root; one of them has no prefixed copy.
	tracked := trackedDerived(photo)
	h.mem.put(tracked[0], []byte("root tile"))
	h.mem.put(tracked[1], []byte("root tile 2"))
	if _, err := h.db.Pool().Exec(t.Context(), `DELETE FROM storage_rekey_runs`); err != nil {
		t.Fatalf("forgetting the run: %v", err)
	}
	if found, err := h.svc.FindRootDerived(t.Context()); err != nil || len(found) != 0 {
		t.Fatalf("FindRootDerived of a project no move touched = %+v, %v; want nothing", found, err)
	}
	h.exec(`INSERT INTO storage_rekey_runs (uid, project_uid, state, target_prefix)
		VALUES ('rk1', $1, 'done', $2)`, project.BootstrapUID, bootstrapTarget)
	if err := h.mem.Delete(t.Context(), bootstrapTarget+tracked[1]); err != nil {
		t.Fatalf("deleting a prefixed copy: %v", err)
	}

	found, err := h.svc.FindRootDerived(t.Context())
	if err != nil || len(found) != 1 || len(found[0].Moves) != 2 {
		t.Fatalf("FindRootDerived = %+v, %v; want ph1 with two objects", found, err)
	}
	result, err := h.svc.MoveRootDerived(t.Context(), found)
	if err != nil || result != (RootDerivedResult{Photos: 1, Objects: 2}) {
		t.Fatalf("MoveRootDerived = %+v, %v; want one photo, two objects", result, err)
	}
	h.assertMoved(photo)
	if data, _ := h.mem.get(bootstrapTarget + tracked[1]); string(data) != "root tile 2" {
		t.Errorf("the missing prefixed copy = %q, want it copied from the root", data)
	}
	if again, err := h.svc.FindRootDerived(t.Context()); err != nil || len(again) != 0 {
		t.Errorf("FindRootDerived after the repair = %+v, %v; want nothing", again, err)
	}
}

func TestService_derivedBatchIsProjectScopedAndOrdered(t *testing.T) {
	h := newHarness(t)
	const other = "pjbbbbbbbbbbbbbbbbbbbbbbbb"
	h.exec(`INSERT INTO projects (uid, slug, name, description, active, storage_prefix)
		VALUES ($1, 'b', 'B', '', true, $2)`, other, project.StoragePrefixFor(other))
	for _, p := range []struct{ uid, project string }{
		{"ph3", project.BootstrapUID}, {"ph1", project.BootstrapUID}, {"ph2", other}, {"ph4", project.BootstrapUID},
	} {
		h.exec(`INSERT INTO photos (uid, file_hash, file_path, file_name, file_size, file_mime, project_id)
			VALUES ($1, $2, $3, 'x.jpg', 1, 'image/jpeg', $4)`,
			p.uid, sha256Hex([]byte(p.uid)), project.StoragePrefixFor(p.project)+"2023/01/"+p.uid+".jpg", p.project)
		h.exec(`INSERT INTO storage_rekey_derived_failures (photo_uid, project_uid, kinds)
			VALUES ($1, $2, '{thumbnail}')`, p.uid, p.project)
	}
	run := Run{ProjectUID: project.BootstrapUID}
	if got, err := h.svc.derivedBatch(t.Context(), run, 2); err != nil || !slices.Equal(got, []string{"ph1", "ph3"}) {
		t.Errorf("first batch = %v, %v; want [ph1 ph3]", got, err)
	}
	run.DerivedCursor = "ph3"
	if got, err := h.svc.derivedBatch(t.Context(), run, 2); err != nil || !slices.Equal(got, []string{"ph4"}) {
		t.Errorf("batch after ph3 = %v, %v; want [ph4]", got, err)
	}
	h.exec(`INSERT INTO storage_rekey_runs (uid, project_uid, state, target_prefix)
		VALUES ('rk1', $1, 'running', $2)`, project.BootstrapUID, bootstrapTarget)
	if got := h.status(); got.DerivedFailed != 3 {
		t.Errorf("derived_failed = %d, want the bootstrap project's 3 only", got.DerivedFailed)
	}
}
