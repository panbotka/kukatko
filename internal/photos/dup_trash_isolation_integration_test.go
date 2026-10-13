//go:build integration

package photos_test

import (
	"errors"
	"testing"

	"github.com/panbotka/kukatko/internal/photos"
	"github.com/panbotka/kukatko/internal/projecttest"
)

// These are the store-level tenant-isolation tests of the reads behind
// duplicate detection, automatic stacking and the trash: each lists only the
// photos of the project it is given, so a scan, a detection pass or a purge in
// one project never reaches another's photos.

// TestDuplicateStackTrashReads_seeOnlyTheirProject gives both seeded photos the
// SAME perceptual hash and archives a second photo in each project, then runs
// every read from inside A and B: each must return its own project's rows and
// never the other's, and refuse without a project.
func TestDuplicateStackTrashReads_seeOnlyTheirProject(t *testing.T) {
	store, db := newStore(t)
	f := projecttest.Seed(t, db.Pool())
	ctx := t.Context()
	for _, tn := range []*projecttest.Tenant{&f.A, &f.B} {
		if err := store.SetPhash(ctx, photos.Phash{PhotoUID: tn.PhotoUID, Phash: 0x0F0F, Dhash: 0x0F0F}); err != nil {
			t.Fatalf("SetPhash(%s): %v", tn.PhotoUID, err)
		}
		if _, err := db.Pool().Exec(ctx,
			`UPDATE photos SET archived_at = now() - interval '3 days' WHERE uid = $1`, tn.PhotoUID); err != nil {
			t.Fatalf("archiving %s: %v", tn.PhotoUID, err)
		}
	}

	f.AssertStoreScoped(t, func(projectUID string) ([]string, error) {
		uids, err := store.ListArchivedUIDs(ctx, projectUID, nil, 10, 0)
		return uids, err
	})
	if _, err := store.ListArchivedUIDs(ctx, "", nil, 10, 0); !errors.Is(err, photos.ErrNoProject) {
		t.Errorf("ListArchivedUIDs without a project = %v, want ErrNoProject", err)
	}

	// Restore the photos: the pHash and stack readers skip the trash.
	if _, err := db.Pool().Exec(ctx, `UPDATE photos SET archived_at = NULL`); err != nil {
		t.Fatalf("restoring: %v", err)
	}
	f.AssertStoreScoped(t, func(projectUID string) ([]string, error) {
		hashes, err := store.ListActivePhashes(ctx, projectUID)
		uids := make([]string, 0, len(hashes))
		for _, h := range hashes {
			uids = append(uids, h.PhotoUID)
		}
		return uids, err
	})
	f.AssertStoreScoped(t, func(projectUID string) ([]string, error) {
		return candidateUIDs(store.ListStackCandidates(ctx, projectUID))
	})
	both := []string{f.A.PhotoUID, f.B.PhotoUID}
	f.AssertStoreScoped(t, func(projectUID string) ([]string, error) {
		return candidateUIDs(store.StackInfoByUIDs(ctx, projectUID, both))
	})
	for name, read := range map[string]func() error{
		"ListActivePhashes":   func() error { _, err := store.ListActivePhashes(ctx, ""); return err },
		"ListStackCandidates": func() error { _, err := store.ListStackCandidates(ctx, ""); return err },
		"StackInfoByUIDs":     func() error { _, err := store.StackInfoByUIDs(ctx, "", both); return err },
	} {
		if err := read(); !errors.Is(err, photos.ErrNoProject) {
			t.Errorf("%s without a project = %v, want ErrNoProject", name, err)
		}
	}
}

// candidateUIDs projects a stack-candidate read onto its uids, passing the
// error through.
func candidateUIDs(list []photos.StackCandidate, err error) ([]string, error) {
	uids := make([]string, 0, len(list))
	for _, c := range list {
		uids = append(uids, c.UID)
	}
	return uids, err
}
