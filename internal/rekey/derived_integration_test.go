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
	"github.com/panbotka/kukatko/internal/thumb"
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

// finishToDone calls Finish on run, stepping the retry pass after every
// ErrPendingCleanup, and returns how many Finish calls it took to reach done.
// It fails the test when the move is not done within limit calls — a sweep and
// a retry that disagree about what a photo owns would go round forever.
func (h *harness) finishToDone(run Run, limit int) int {
	h.t.Helper()
	for attempt := 1; attempt <= limit; attempt++ {
		done, err := h.svc.Finish(h.t.Context(), run)
		switch {
		case err == nil && done.State == StateDone:
			return attempt
		case errors.Is(err, ErrPendingCleanup):
			if err := h.stepToEnd(run, 5); err != nil {
				h.t.Fatalf("stepping the retry after Finish %d: %v", attempt, err)
			}
		default:
			h.t.Fatalf("Finish %d = %+v, %v; want done or ErrPendingCleanup", attempt, done, err)
		}
	}
	h.t.Fatalf("the move was not done after %d Finish calls: the final sweep keeps finding the same objects", limit)
	return 0
}

// rootStrays puts three objects of photo at the root that no plan of it names:
// a thumbnail of a size no longer registered and a name the streaming layout
// cannot hold (both dropped), and a segment of a rendition nobody recorded
// (moved). It returns them in that order.
func (h *harness) rootStrays(photo seeded) (oldSize, badName, orphanSegment string) {
	h.t.Helper()
	hash := sha256Hex(photo.content)
	tile, err := thumb.RelPath("", hash, thumb.GridSize)
	if err != nil {
		h.t.Fatalf("thumbnail key: %v", err)
	}
	oldSize = strings.TrimSuffix(tile, thumb.GridSize+".jpg") + "fit_600.jpg"
	badName = "hls/" + hash + "/1080p/index.m3u8"
	orphanSegment = "hls/" + hash + "/480p/00000.m4s"
	for _, key := range []string{oldSize, badName, orphanSegment} {
		h.mem.put(key, []byte("stray "+key))
	}
	return oldSize, badName, orphanSegment
}

func TestService_finishEndsDespiteStrayRootDerived(t *testing.T) {
	h := newHarness(t)
	clip := h.seedPhoto("ph1", "2023/01/clip.mp4", true)
	other := h.seedPhoto("ph2", "2023/02/b.jpg", false)
	run := h.start()
	if err := h.stepToEnd(run, 5); err != nil {
		t.Fatalf("stepping: %v", err)
	}
	// What an earlier layout left beside the planned objects.
	oldSize, badName, orphanSegment := h.rootStrays(clip)
	thumbLists, hlsLists := h.mem.lists["thumb/"], h.mem.lists["hls/"]

	finishes := h.finishToDone(run, 3)
	if finishes != 2 {
		t.Errorf("Finish calls = %d, want 2: one sweep that records the strays, one that finds nothing", finishes)
	}
	// The root trees are listed once per Finish call — never per photo or per
	// retry.
	if got := h.mem.lists["thumb/"] - thumbLists; got != finishes {
		t.Errorf("root thumb/ listings = %d, want one per Finish call (%d)", got, finishes)
	}
	if got := h.mem.lists["hls/"] - hlsLists; got != finishes {
		t.Errorf("root hls/ listings = %d, want one per Finish call (%d)", got, finishes)
	}
	h.assertMoved(clip)
	h.assertMoved(other)
	for _, key := range []string{oldSize, badName, orphanSegment} {
		if h.mem.has(key) {
			t.Errorf("stray %s is still at the root", key)
		}
	}
	if data, _ := h.mem.get(bootstrapTarget + orphanSegment); string(data) != "stray "+orphanSegment {
		t.Errorf("the orphan segment under the prefix = %q, want it moved there", data)
	}
	for _, key := range []string{oldSize, badName} {
		if h.mem.has(bootstrapTarget + key) {
			t.Errorf("%s was carried under the prefix, want it dropped", key)
		}
	}
	if _, _, found := h.derivedRow(clip.uid); found {
		t.Error("the swept record survived the retry")
	}
}

func TestService_strayThatCannotMoveFailsTheRunInsteadOfLooping(t *testing.T) {
	h := newHarness(t)
	clip := h.seedPhoto("ph1", "2023/01/clip.mp4", true)
	run := h.start()
	if err := h.stepToEnd(run, 5); err != nil {
		t.Fatalf("stepping: %v", err)
	}
	_, _, orphanSegment := h.rootStrays(clip)
	h.mem.failCopy[bootstrapTarget+orphanSegment] = errTransient

	if _, err := h.svc.Finish(t.Context(), run); !errors.Is(err, ErrPendingCleanup) {
		t.Fatalf("first Finish = %v, want ErrPendingCleanup", err)
	}
	if kinds, _, found := h.derivedRow(clip.uid); !found || !slices.Equal(kinds, []string{"hls", "thumbnail"}) {
		t.Errorf("swept record = %v, %v; want hls and thumbnail", kinds, found)
	}
	if err := h.stepToEnd(run, 5); err != nil {
		t.Fatalf("stepping the retry: %v", err)
	}
	// The segment could not move, so nothing of the photo's left the root; the
	// next sweep finds only what is recorded already and the run fails.
	failed, err := h.svc.Finish(t.Context(), run)
	if !errors.Is(err, ErrDerivedRemain) || failed.State != StateFailed || failed.DerivedFailed != 1 {
		t.Fatalf("second Finish = %+v, %v; want failed with ErrDerivedRemain", failed, err)
	}
	if !h.mem.has(orphanSegment) {
		t.Error("the stray segment was deleted although its copy failed")
	}

	delete(h.mem.failCopy, bootstrapTarget+orphanSegment)
	if _, err := h.svc.Retry(t.Context(), audit.Meta{}, project.BootstrapUID); err != nil {
		t.Fatalf("Retry: %v", err)
	}
	if err := h.stepToEnd(run, 5); err != nil {
		t.Fatalf("retried pass: %v", err)
	}
	h.finishToDone(run, 1)
	if h.mem.has(orphanSegment) || !h.mem.has(bootstrapTarget+orphanSegment) {
		t.Error("the stray segment did not move once the store recovered")
	}
}

// TestService_rootDerivedLeftAloneWhileAnotherProjectUsesTheRoot verifies the
// sweep of Finish and the root-derived repair touch nothing at the root while
// another project is still in the root layout: thumbnail keys are named after a
// content hash, which is unique only within a project, so a root tile matching a
// moved photo may be the other project's own. Once no project is left there,
// the same tile is found again.
func TestService_rootDerivedLeftAloneWhileAnotherProjectUsesTheRoot(t *testing.T) {
	h := newHarness(t)
	photo := h.seedPhoto("ph1", "2023/01/a.jpg", false)
	run := h.start()
	if err := h.stepToEnd(run, 5); err != nil {
		t.Fatalf("stepping: %v", err)
	}
	stale := trackedDerived(photo)[0]
	h.mem.put(stale, []byte("a root tile"))
	h.exec(`INSERT INTO projects (uid, slug, name, description, active, storage_prefix)
		VALUES ('pjrrrrrrrrrrrrrrrrrrrrrrrr', 'rootlayout', 'Root', '', true, '')`)

	if found, err := h.svc.FindRootDerived(t.Context()); err != nil || len(found) != 0 {
		t.Fatalf("FindRootDerived with another root-layout project = %+v, %v; want nothing", found, err)
	}
	done, err := h.svc.Finish(t.Context(), run)
	if err != nil || done.State != StateDone {
		t.Fatalf("Finish with another root-layout project = %+v, %v; want done without a sweep", done, err)
	}
	if data, _ := h.mem.get(stale); string(data) != "a root tile" {
		t.Errorf("the root tile = %q, want it left alone", data)
	}

	h.exec(`DELETE FROM projects WHERE uid = 'pjrrrrrrrrrrrrrrrrrrrrrrrr'`)
	if found, err := h.svc.FindRootDerived(t.Context()); err != nil || len(found) != 1 {
		t.Errorf("FindRootDerived once the root is the moved projects' alone = %+v, %v; want the tile", found, err)
	}
}
