//go:build integration

package trash_test

import (
	"errors"
	"testing"

	"github.com/panbotka/kukatko/internal/audit"
	"github.com/panbotka/kukatko/internal/photos"
	"github.com/panbotka/kukatko/internal/projecttest"
)

// TestPurgePhoto_foreignPhotoIsNotFound purges A's archived photo from inside B:
// it is answered exactly like a missing photo and survives untouched.
func TestPurgePhoto_foreignPhotoIsNotFound(t *testing.T) {
	env := newPurgeEnv(t)
	f := projecttest.Seed(t, env.db.Pool())
	archiveSeeded(t, env, f)

	err := env.svc.PurgePhoto(t.Context(), f.B.ProjectUID, f.A.PhotoUID, audit.Meta{})
	if !errors.Is(err, photos.ErrPhotoNotFound) {
		t.Fatalf("PurgePhoto of A's photo from B = %v, want ErrPhotoNotFound", err)
	}
	if _, err := env.store.GetByUIDInProject(t.Context(), f.A.ProjectUID, f.A.PhotoUID); err != nil {
		t.Errorf("A's photo is gone after a refused purge: %v", err)
	}
}

// TestPurgeExpired_sweepsEveryActiveProject archives both seeded photos past the
// retention window and deactivates B: the scheduled purge sweeps A's trash and
// leaves the deactivated project's data alone; once B is active again its photo
// goes too, audited under B.
func TestPurgeExpired_sweepsEveryActiveProject(t *testing.T) {
	env := newPurgeEnv(t)
	f := projecttest.Seed(t, env.db.Pool())
	archiveSeeded(t, env, f)
	setActive(t, env, f.B.ProjectUID, false)

	res, err := env.svc.PurgeExpired(t.Context())
	if err != nil {
		t.Fatalf("PurgeExpired: %v", err)
	}
	if res.Purged != 1 {
		t.Errorf("PurgeExpired with B deactivated = %+v, want only A's photo", res)
	}
	if _, err := env.store.GetForJob(t.Context(), f.A.PhotoUID); !errors.Is(err, photos.ErrPhotoNotFound) {
		t.Errorf("A's expired photo survived the retention purge (err %v)", err)
	}
	if _, err := env.store.GetForJob(t.Context(), f.B.PhotoUID); err != nil {
		t.Errorf("the deactivated project's photo was purged: %v", err)
	}

	setActive(t, env, f.B.ProjectUID, true)
	if res, err = env.svc.PurgeExpired(t.Context()); err != nil || res.Purged != 1 {
		t.Fatalf("PurgeExpired with B active = %+v, %v; want B's photo purged", res, err)
	}
	var auditedIn string
	if err := env.db.Pool().QueryRow(t.Context(),
		`SELECT project_id FROM audit_log WHERE action = $1 AND target_uid = $2`,
		audit.ActionPhotoPurge, f.B.PhotoUID).Scan(&auditedIn); err != nil {
		t.Fatalf("reading B's purge audit row: %v", err)
	}
	if auditedIn != f.B.ProjectUID {
		t.Errorf("B's purge was audited in project %q, want %q", auditedIn, f.B.ProjectUID)
	}
}

// archiveSeeded puts both tenants' photos into the trash well past the one-day
// retention window of newPurgeEnv.
func archiveSeeded(t *testing.T, env *purgeEnv, f *projecttest.Fixture) {
	t.Helper()
	if _, err := env.db.Pool().Exec(t.Context(),
		`UPDATE photos SET archived_at = now() - interval '3 days' WHERE uid = ANY($1)`,
		[]string{f.A.PhotoUID, f.B.PhotoUID}); err != nil {
		t.Fatalf("archiving the seeded photos: %v", err)
	}
}

// setActive activates or deactivates a project directly in the table.
func setActive(t *testing.T, env *purgeEnv, projectUID string, active bool) {
	t.Helper()
	if _, err := env.db.Pool().Exec(t.Context(),
		`UPDATE projects SET active = $2 WHERE uid = $1`, projectUID, active); err != nil {
		t.Fatalf("setting %s active=%v: %v", projectUID, active, err)
	}
}
