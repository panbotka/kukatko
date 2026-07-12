//go:build integration

package organize_test

import (
	"context"
	"errors"
	"testing"

	"github.com/panbotka/kukatko/internal/audit"
	"github.com/panbotka/kukatko/internal/organize"
)

// These tests run only under `make test-integration`. They exercise the audited
// mutation methods against the real database and assert that each one appends the
// expected audit_log row in the mutation's transaction, and that a rolled-back
// mutation writes none — the durable-audit guarantee from ARCHITECTURE.md §5.1.

// actorEntry builds an audit entry attributed to actorUID the way a handler would
// (minus the request IP/UA, which the store does not depend on).
func actorEntry(actorUID, action, targetType, targetUID string, details map[string]any) audit.Entry {
	return audit.Entry{
		ActorUID:   actorUID,
		Action:     action,
		TargetType: targetType,
		TargetUID:  targetUID,
		Details:    details,
	}
}

// auditRecords returns the audit_log rows for action, newest first.
func auditRecords(t *testing.T, ctx context.Context, store *audit.Store, action string) []audit.Record {
	t.Helper()
	recs, err := store.List(ctx, audit.Filter{Action: action, Limit: 50})
	if err != nil {
		t.Fatalf("listing audit %s: %v", action, err)
	}
	return recs
}

// requireOneAudit asserts exactly one audit_log row exists for action with the
// given actor and target, and returns it for further detail assertions.
func requireOneAudit(
	t *testing.T, ctx context.Context, store *audit.Store, action, actorUID, targetUID string,
) audit.Record {
	t.Helper()
	recs := auditRecords(t, ctx, store, action)
	if len(recs) != 1 {
		t.Fatalf("audit %s rows = %d, want 1 (%+v)", action, len(recs), recs)
	}
	rec := recs[0]
	if rec.ActorUID == nil || *rec.ActorUID != actorUID {
		t.Errorf("audit %s actor = %v, want %q", action, rec.ActorUID, actorUID)
	}
	if rec.TargetUID == nil || *rec.TargetUID != targetUID {
		t.Errorf("audit %s target = %v, want %q", action, rec.TargetUID, targetUID)
	}
	return rec
}

// requireNoAudit asserts no audit_log row exists for action, proving a rolled-back
// mutation left no trail.
func requireNoAudit(t *testing.T, ctx context.Context, store *audit.Store, action string) {
	t.Helper()
	if recs := auditRecords(t, ctx, store, action); len(recs) != 0 {
		t.Fatalf("audit %s rows = %d, want 0 (%+v)", action, len(recs), recs)
	}
}

// TestAuditAlbumMutations checks every album mutation records one audit row for
// the acting user, targeting the album.
func TestAuditAlbumMutations(t *testing.T) {
	store, photoStore, authStore, db := newStores(t)
	ctx := t.Context()
	auditStore := audit.NewStore(db.Pool())
	actor := makeUser(t, authStore, "auda_actor", "auda")
	photoUID := makePhoto(t, photoStore, "auditalbumphoto")

	created, err := store.CreateAlbumAudited(ctx, organize.Album{Title: "Trip"},
		actorEntry(actor, audit.ActionAlbumCreate, "albums", "", map[string]any{"title": "Trip"}))
	if err != nil {
		t.Fatalf("CreateAlbumAudited: %v", err)
	}
	rec := requireOneAudit(t, ctx, auditStore, audit.ActionAlbumCreate, actor, created.UID)
	if rec.Details["title"] != "Trip" {
		t.Errorf("create details title = %v, want Trip", rec.Details["title"])
	}

	if _, err := store.UpdateAlbumAudited(ctx, created.UID, organize.AlbumUpdate{Title: "Trip 2"},
		actorEntry(actor, audit.ActionAlbumUpdate, "albums", created.UID, nil)); err != nil {
		t.Fatalf("UpdateAlbumAudited: %v", err)
	}
	requireOneAudit(t, ctx, auditStore, audit.ActionAlbumUpdate, actor, created.UID)

	if err := store.AddPhotosAudited(ctx, created.UID, []string{photoUID},
		actorEntry(actor, audit.ActionAlbumAddPhotos, "albums", created.UID,
			map[string]any{"count": 1})); err != nil {
		t.Fatalf("AddPhotosAudited: %v", err)
	}
	requireOneAudit(t, ctx, auditStore, audit.ActionAlbumAddPhotos, actor, created.UID)

	if err := store.RemovePhotosAudited(ctx, created.UID, []string{photoUID},
		actorEntry(actor, audit.ActionAlbumRemovePhotos, "albums", created.UID, nil)); err != nil {
		t.Fatalf("RemovePhotosAudited: %v", err)
	}
	requireOneAudit(t, ctx, auditStore, audit.ActionAlbumRemovePhotos, actor, created.UID)

	if err := store.DeleteAlbumAudited(ctx, created.UID,
		actorEntry(actor, audit.ActionAlbumDelete, "albums", created.UID, nil)); err != nil {
		t.Fatalf("DeleteAlbumAudited: %v", err)
	}
	requireOneAudit(t, ctx, auditStore, audit.ActionAlbumDelete, actor, created.UID)
}

// TestAuditAlbumRollback checks a failed album mutation writes no audit row.
func TestAuditAlbumRollback(t *testing.T) {
	store, _, authStore, db := newStores(t)
	ctx := t.Context()
	auditStore := audit.NewStore(db.Pool())
	actor := makeUser(t, authStore, "auda_actor", "auda")

	created, err := store.CreateAlbumAudited(ctx, organize.Album{Title: "Trip"},
		actorEntry(actor, audit.ActionAlbumCreate, "albums", "", nil))
	if err != nil {
		t.Fatalf("CreateAlbumAudited: %v", err)
	}

	// Adding a non-existent photo violates the FK, rolls the batch back, and must
	// write no audit row.
	err = store.AddPhotosAudited(ctx, created.UID, []string{"ph_missing"},
		actorEntry(actor, audit.ActionAlbumAddPhotos, "albums", created.UID, nil))
	if !errors.Is(err, organize.ErrPhotoNotFound) {
		t.Fatalf("AddPhotosAudited err = %v, want ErrPhotoNotFound", err)
	}
	requireNoAudit(t, ctx, auditStore, audit.ActionAlbumAddPhotos)

	// Deleting a missing album changes nothing and writes no audit row.
	err = store.DeleteAlbumAudited(ctx, "al_missing",
		actorEntry(actor, audit.ActionAlbumDelete, "albums", "al_missing", nil))
	if !errors.Is(err, organize.ErrAlbumNotFound) {
		t.Fatalf("DeleteAlbumAudited err = %v, want ErrAlbumNotFound", err)
	}
	requireNoAudit(t, ctx, auditStore, audit.ActionAlbumDelete)
}

// TestAuditAlbumCreate_uniqueSlugRetry checks that when a create retries past a
// slug collision it still records exactly one audit row per successful create —
// the collided attempt's transaction (audit included) is rolled back.
func TestAuditAlbumCreate_uniqueSlugRetry(t *testing.T) {
	store, _, authStore, db := newStores(t)
	ctx := t.Context()
	auditStore := audit.NewStore(db.Pool())
	actor := makeUser(t, authStore, "auda_actor", "auda")

	first, err := store.CreateAlbumAudited(ctx, organize.Album{Title: "Trip"},
		actorEntry(actor, audit.ActionAlbumCreate, "albums", "", nil))
	if err != nil {
		t.Fatalf("first CreateAlbumAudited: %v", err)
	}
	second, err := store.CreateAlbumAudited(ctx, organize.Album{Title: "Trip"},
		actorEntry(actor, audit.ActionAlbumCreate, "albums", "", nil))
	if err != nil {
		t.Fatalf("second CreateAlbumAudited: %v", err)
	}
	if first.Slug != "trip" || second.Slug != "trip-2" {
		t.Fatalf("slugs = %q, %q, want trip, trip-2", first.Slug, second.Slug)
	}
	if recs := auditRecords(t, ctx, auditStore, audit.ActionAlbumCreate); len(recs) != 2 {
		t.Fatalf("album.create rows = %d, want 2 (one per create, none for the collision)", len(recs))
	}
}

// TestAuditLabelMutations checks every label mutation records one audit row for
// the acting user, targeting the label.
func TestAuditLabelMutations(t *testing.T) {
	store, photoStore, authStore, db := newStores(t)
	ctx := t.Context()
	auditStore := audit.NewStore(db.Pool())
	actor := makeUser(t, authStore, "auda_actor", "auda")
	photoUID := makePhoto(t, photoStore, "auditlabelphoto")

	created, err := store.CreateLabelAudited(ctx, organize.Label{Name: "Beach"},
		actorEntry(actor, audit.ActionLabelCreate, "labels", "", map[string]any{"name": "Beach"}))
	if err != nil {
		t.Fatalf("CreateLabelAudited: %v", err)
	}
	requireOneAudit(t, ctx, auditStore, audit.ActionLabelCreate, actor, created.UID)

	if _, err := store.UpdateLabelAudited(ctx, created.UID, organize.LabelUpdate{Name: "Sea", Priority: 3},
		actorEntry(actor, audit.ActionLabelUpdate, "labels", created.UID, nil)); err != nil {
		t.Fatalf("UpdateLabelAudited: %v", err)
	}
	requireOneAudit(t, ctx, auditStore, audit.ActionLabelUpdate, actor, created.UID)

	if err := store.AttachLabelAudited(ctx, photoUID, created.UID, organize.SourceManual, 0,
		actorEntry(actor, audit.ActionLabelAttach, "labels", created.UID,
			map[string]any{"photo_uid": photoUID})); err != nil {
		t.Fatalf("AttachLabelAudited: %v", err)
	}
	rec := requireOneAudit(t, ctx, auditStore, audit.ActionLabelAttach, actor, created.UID)
	if rec.Details["photo_uid"] != photoUID {
		t.Errorf("attach details photo_uid = %v, want %q", rec.Details["photo_uid"], photoUID)
	}

	if err := store.DetachLabelAudited(ctx, photoUID, created.UID,
		actorEntry(actor, audit.ActionLabelDetach, "labels", created.UID,
			map[string]any{"photo_uid": photoUID})); err != nil {
		t.Fatalf("DetachLabelAudited: %v", err)
	}
	requireOneAudit(t, ctx, auditStore, audit.ActionLabelDetach, actor, created.UID)

	if err := store.DeleteLabelAudited(ctx, created.UID,
		actorEntry(actor, audit.ActionLabelDelete, "labels", created.UID, nil)); err != nil {
		t.Fatalf("DeleteLabelAudited: %v", err)
	}
	requireOneAudit(t, ctx, auditStore, audit.ActionLabelDelete, actor, created.UID)
}

// TestAuditLabelRollback checks a failed label attach writes no audit row.
func TestAuditLabelRollback(t *testing.T) {
	store, _, authStore, db := newStores(t)
	ctx := t.Context()
	auditStore := audit.NewStore(db.Pool())
	actor := makeUser(t, authStore, "auda_actor", "auda")

	created, err := store.CreateLabelAudited(ctx, organize.Label{Name: "Beach"},
		actorEntry(actor, audit.ActionLabelCreate, "labels", "", nil))
	if err != nil {
		t.Fatalf("CreateLabelAudited: %v", err)
	}

	err = store.AttachLabelAudited(ctx, "ph_missing", created.UID, organize.SourceManual, 0,
		actorEntry(actor, audit.ActionLabelAttach, "labels", created.UID, nil))
	if !errors.Is(err, organize.ErrPhotoNotFound) {
		t.Fatalf("AttachLabelAudited err = %v, want ErrPhotoNotFound", err)
	}
	requireNoAudit(t, ctx, auditStore, audit.ActionLabelAttach)
}
