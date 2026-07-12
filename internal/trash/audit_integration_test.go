//go:build integration

package trash_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/panbotka/kukatko/internal/audit"
	"github.com/panbotka/kukatko/internal/auth"
	"github.com/panbotka/kukatko/internal/database"
	"github.com/panbotka/kukatko/internal/photos"
)

// These tests run only under `make test-integration` against the database named by
// KUKATKO_TEST_DATABASE_URL. They assert that every permanent purge appends a
// photo.purge audit row in the row-deletion's transaction (the durable-audit
// guarantee from ARCHITECTURE.md §5.1): an HTTP-triggered purge records the acting
// user, the scheduled retention purge records a system actor, and a purge that
// deletes nothing writes no audit row.

// makeActor inserts an admin account with the given uid/username so audit rows
// have a valid actor to reference (audit_log.actor_uid is an FK to users).
func makeActor(t *testing.T, db *database.DB, uid, username string) string {
	t.Helper()
	if err := auth.NewStore(db.Pool()).CreateUser(context.Background(), auth.User{
		UID:          uid,
		Username:     username,
		PasswordHash: "x",
		Role:         auth.RoleAdmin,
	}); err != nil {
		t.Fatalf("creating actor %s: %v", username, err)
	}
	return uid
}

// purgeAuditRows returns the photo.purge audit rows, newest first.
func purgeAuditRows(t *testing.T, ctx context.Context, store *audit.Store) []audit.Record {
	t.Helper()
	recs, err := store.List(ctx, audit.Filter{Action: audit.ActionPhotoPurge, Limit: 100})
	if err != nil {
		t.Fatalf("listing purge audit rows: %v", err)
	}
	return recs
}

// findPurgeAudit returns the photo.purge row targeting targetUID, or fails.
func findPurgeAudit(t *testing.T, recs []audit.Record, targetUID string) audit.Record {
	t.Helper()
	for _, rec := range recs {
		if rec.TargetUID != nil && *rec.TargetUID == targetUID {
			return rec
		}
	}
	t.Fatalf("no photo.purge audit row targeting %q (have %+v)", targetUID, recs)
	return audit.Record{}
}

// TestPurgePhoto_writesAuditRow confirms a manual single purge records exactly one
// photo.purge row attributed to the acting user, targeting the photo, tagged as a
// manual purge, and carrying the request's IP/User-Agent.
func TestPurgePhoto_writesAuditRow(t *testing.T) {
	env := newPurgeEnv(t)
	ctx := t.Context()
	auditStore := audit.NewStore(env.db.Pool())
	actor := makeActor(t, env.db, "usr_trasha", "trasha")
	recent := time.Now().Add(-time.Hour)
	archived, _, _ := env.seedPhoto(t, "arch", &recent)

	meta := audit.Meta{ActorUID: actor, IP: "203.0.113.7", UserAgent: "purge-agent"}
	if err := env.svc.PurgePhoto(ctx, archived.UID, meta); err != nil {
		t.Fatalf("PurgePhoto: %v", err)
	}

	recs := purgeAuditRows(t, ctx, auditStore)
	if len(recs) != 1 {
		t.Fatalf("photo.purge rows = %d, want 1 (%+v)", len(recs), recs)
	}
	rec := recs[0]
	if rec.ActorUID == nil || *rec.ActorUID != actor {
		t.Errorf("purge actor = %v, want %q", rec.ActorUID, actor)
	}
	if rec.TargetUID == nil || *rec.TargetUID != archived.UID {
		t.Errorf("purge target = %v, want %q", rec.TargetUID, archived.UID)
	}
	if rec.Details["source"] != "manual" {
		t.Errorf("purge source = %v, want manual", rec.Details["source"])
	}
	if rec.IP == nil || *rec.IP != "203.0.113.7" || rec.UserAgent == nil || *rec.UserAgent != "purge-agent" {
		t.Errorf("purge ip/ua = %v/%v, want 203.0.113.7/purge-agent", rec.IP, rec.UserAgent)
	}
}

// TestEmptyTrash_writesAuditRowPerPhoto confirms emptying the trash records one
// photo.purge row per purged photo, each attributed to the acting user and tagged
// as an empty-trash sweep.
func TestEmptyTrash_writesAuditRowPerPhoto(t *testing.T) {
	env := newPurgeEnv(t)
	ctx := t.Context()
	auditStore := audit.NewStore(env.db.Pool())
	actor := makeActor(t, env.db, "usr_trashe", "trashe")
	recent := time.Now().Add(-time.Hour)
	a, _, _ := env.seedPhoto(t, "a", &recent)
	b, _, _ := env.seedPhoto(t, "b", &recent)
	env.seedPhoto(t, "live", nil) // live photo: not purged, not audited

	res, err := env.svc.EmptyTrash(ctx, audit.Meta{ActorUID: actor})
	if err != nil {
		t.Fatalf("EmptyTrash: %v", err)
	}
	if res.Purged != 2 {
		t.Fatalf("EmptyTrash purged = %d, want 2", res.Purged)
	}

	recs := purgeAuditRows(t, ctx, auditStore)
	if len(recs) != 2 {
		t.Fatalf("photo.purge rows = %d, want 2 (%+v)", len(recs), recs)
	}
	for _, uid := range []string{a.UID, b.UID} {
		rec := findPurgeAudit(t, recs, uid)
		if rec.ActorUID == nil || *rec.ActorUID != actor {
			t.Errorf("purge %s actor = %v, want %q", uid, rec.ActorUID, actor)
		}
		if rec.Details["source"] != "empty_trash" {
			t.Errorf("purge %s source = %v, want empty_trash", uid, rec.Details["source"])
		}
	}
}

// TestPurgeExpired_writesSystemActorAuditRow confirms the scheduled retention
// purge records a photo.purge row with no actor (a system action) for the expired
// photo, tagged as a retention purge, while leaving recent photos untouched.
func TestPurgeExpired_writesSystemActorAuditRow(t *testing.T) {
	env := newPurgeEnv(t)
	ctx := t.Context()
	auditStore := audit.NewStore(env.db.Pool())
	now := time.Now()
	expired := now.Add(-72 * time.Hour)
	recent := now.Add(-time.Hour)
	old, _, _ := env.seedPhoto(t, "old", &expired)
	env.seedPhoto(t, "recent", &recent) // within retention: not purged, not audited

	res, err := env.svc.PurgeExpired(ctx)
	if err != nil {
		t.Fatalf("PurgeExpired: %v", err)
	}
	if res.Purged != 1 {
		t.Fatalf("PurgeExpired purged = %d, want 1", res.Purged)
	}

	recs := purgeAuditRows(t, ctx, auditStore)
	if len(recs) != 1 {
		t.Fatalf("photo.purge rows = %d, want 1 (%+v)", len(recs), recs)
	}
	rec := recs[0]
	if rec.ActorUID != nil {
		t.Errorf("retention purge actor = %v, want nil (system)", *rec.ActorUID)
	}
	if rec.TargetUID == nil || *rec.TargetUID != old.UID {
		t.Errorf("retention purge target = %v, want %q", rec.TargetUID, old.UID)
	}
	if rec.Details["source"] != "retention" {
		t.Errorf("retention purge source = %v, want retention", rec.Details["source"])
	}
}

// TestDeleteAudited_rollbackWritesNoAudit confirms that purging a photo that does
// not exist (a no-op delete) returns ErrPhotoNotFound and leaves no audit row —
// the rollback half of the durable-audit guarantee.
func TestDeleteAudited_rollbackWritesNoAudit(t *testing.T) {
	env := newPurgeEnv(t)
	ctx := t.Context()
	auditStore := audit.NewStore(env.db.Pool())
	actor := makeActor(t, env.db, "usr_trashr", "trashr")

	entry := audit.Meta{ActorUID: actor}.Entry(audit.ActionPhotoPurge, "photos", "ph_missing",
		map[string]any{"source": "manual"})
	if err := env.store.DeleteAudited(ctx, "ph_missing", entry); !errors.Is(err, photos.ErrPhotoNotFound) {
		t.Fatalf("DeleteAudited(missing) err = %v, want ErrPhotoNotFound", err)
	}
	if recs := purgeAuditRows(t, ctx, auditStore); len(recs) != 0 {
		t.Fatalf("photo.purge rows after failed delete = %d, want 0 (%+v)", len(recs), recs)
	}
}
