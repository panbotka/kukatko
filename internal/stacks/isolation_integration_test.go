//go:build integration

package stacks_test

import (
	"testing"
	"time"

	"github.com/panbotka/kukatko/internal/audit"
	"github.com/panbotka/kukatko/internal/photos"
	"github.com/panbotka/kukatko/internal/projecttest"
)

// TestDetectStacks_neverSpansProjects puts a RAW and its JPEG — a textbook
// base-name pair — into each project and, crosswise, a lone RAW in A whose
// JPEG twin lives in B. A detection pass inside A must stack only A's own pair:
// the lone RAW stays alone, and nothing of B is stacked or touched.
func TestDetectStacks_neverSpansProjects(t *testing.T) {
	store, _, _, db := newStores(t)
	f := projecttest.Seed(t, db.Pool())
	ctx := t.Context()
	inProject := func(projectUID, hash, name, mime string) photos.Photo {
		taken := time.Date(2021, 7, 4, 10, 0, 0, 0, time.UTC)
		created, err := store.Create(ctx, photos.Photo{
			ProjectID: projectUID, FileHash: hash, FilePath: "2021/07/" + hash, FileName: name,
			FileMime: mime, FileWidth: 6000, FileHeight: 4000, MediaType: photos.MediaImage, TakenAt: &taken,
		})
		if err != nil {
			t.Fatalf("Create %s: %v", name, err)
		}
		return created
	}
	aRaw := inProject(f.A.ProjectUID, "a-raw", "IMG_1.CR2", "image/x-canon-cr2")
	aJpg := inProject(f.A.ProjectUID, "a-jpg", "IMG_1.jpg", "image/jpeg")
	lone := inProject(f.A.ProjectUID, "a-lone", "IMG_9.CR2", "image/x-canon-cr2")
	twin := inProject(f.B.ProjectUID, "b-twin", "IMG_9.jpg", "image/jpeg")
	bRaw := inProject(f.B.ProjectUID, "b-raw", "IMG_1.CR2", "image/x-canon-cr2")

	created, err := detector(store).DetectStacks(ctx, f.A.ProjectUID, actorEntry("", audit.ActionStacksDetect))
	if err != nil {
		t.Fatalf("DetectStacks in A: %v", err)
	}
	if created != 1 {
		t.Fatalf("DetectStacks in A created %d stacks, want 1 (A's own pair)", created)
	}
	stackOf := func(uid string) *string {
		p, err := store.GetForJob(ctx, uid)
		if err != nil {
			t.Fatalf("loading %s: %v", uid, err)
		}
		return p.StackUID
	}
	pair, other := stackOf(aRaw.UID), stackOf(aJpg.UID)
	if pair == nil || other == nil || *pair != *other {
		t.Errorf("A's RAW and JPEG were not stacked together: %v / %v", pair, other)
	}
	for _, uid := range []string{lone.UID, twin.UID, bRaw.UID, f.B.PhotoUID} {
		if s := stackOf(uid); s != nil {
			t.Errorf("photo %s was stacked by A's detection pass (stack %s)", uid, *s)
		}
	}
	var auditedIn string
	if err := db.Pool().QueryRow(ctx, `SELECT project_id FROM audit_log WHERE action = $1`,
		audit.ActionStacksDetect).Scan(&auditedIn); err != nil {
		t.Fatalf("reading the pass's audit row: %v", err)
	}
	if auditedIn != f.A.ProjectUID {
		t.Errorf("the pass was audited in project %q, want A's %q", auditedIn, f.A.ProjectUID)
	}
}
