//go:build integration

package people_test

import (
	"errors"
	"testing"

	"github.com/panbotka/kukatko/internal/audit"
	"github.com/panbotka/kukatko/internal/people"
	"github.com/panbotka/kukatko/internal/projecttest"
)

// TestNamelessRepair_scopedByProject makes each seeded project's person a
// nameless catch-all (the name emptied) and asserts the repair stays inside one
// project: each project's report lists only its own catch-all, the other
// project's cannot be snapshotted or detached from it (writing nothing), and a
// snapshot restored into its project lands there again.
func TestNamelessRepair_scopedByProject(t *testing.T) {
	store, _, _, db := newStores(t)
	pool := db.Pool()
	f := projecttest.Seed(t, pool)
	ctx := t.Context()
	if _, err := pool.Exec(ctx, "UPDATE subjects SET name = '' WHERE uid IN ($1, $2)",
		f.A.SubjectUID, f.B.SubjectUID); err != nil {
		t.Fatalf("emptying the names: %v", err)
	}

	f.AssertStoreScoped(t, func(projectUID string) ([]string, error) {
		found, err := store.ListNamelessSubjects(ctx, projectUID)
		uids := make([]string, 0, len(found))
		for i := range found {
			uids = append(uids, found[i].UID)
		}
		return uids, err
	})

	if _, err := store.SnapshotSubject(ctx, f.B.ProjectUID, f.A.SubjectUID); !errors.Is(err, people.ErrSubjectNotFound) {
		t.Errorf("snapshotting A's catch-all inside B = %v, want ErrSubjectNotFound", err)
	}
	entry := audit.Entry{Action: audit.ActionSubjectDelete, TargetType: "subject"}
	if _, err := store.DetachSubject(ctx, f.B.ProjectUID, f.A.SubjectUID, entry); !errors.Is(
		err, people.ErrSubjectNotFound) {
		t.Errorf("detaching A's catch-all inside B = %v, want ErrSubjectNotFound", err)
	}
	if _, err := store.GetSubjectByUID(ctx, f.A.ProjectUID, f.A.SubjectUID); err != nil {
		t.Errorf("A's catch-all after B's detach: %v, want it untouched", err)
	}

	snap, err := store.DetachSubject(ctx, f.A.ProjectUID, f.A.SubjectUID, entry)
	if err != nil {
		t.Fatalf("detaching A's catch-all inside A: %v", err)
	}
	restored, err := store.RestoreSubject(ctx, f.A.ProjectUID, snap,
		audit.Entry{Action: audit.ActionSubjectCreate, TargetType: "subject"})
	if err != nil {
		t.Fatalf("RestoreSubject: %v", err)
	}
	if _, err := store.GetSubjectByUID(ctx, f.A.ProjectUID, restored.UID); err != nil {
		t.Errorf("the restored catch-all is not in A: %v", err)
	}
	marker, err := store.GetMarkerByUID(ctx, f.A.ProjectUID, f.A.MarkerUID)
	if err != nil || marker.SubjectUID == nil || *marker.SubjectUID != restored.UID {
		t.Errorf("A's marker after the restore = %+v (%v), want it re-linked", marker, err)
	}
	var audited int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM audit_log WHERE target_uid = $1 AND project_id = $2",
		f.A.SubjectUID, f.A.ProjectUID).Scan(&audited); err != nil {
		t.Fatalf("counting audit rows: %v", err)
	}
	if audited != 2 {
		t.Errorf("%d audit rows of A's catch-all in A, want the detach and the restore", audited)
	}
}
