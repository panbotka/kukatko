//go:build integration

package project_test

import (
	"context"
	"errors"
	"testing"

	"github.com/panbotka/kukatko/internal/audit"
	"github.com/panbotka/kukatko/internal/auth"
	"github.com/panbotka/kukatko/internal/database"
	"github.com/panbotka/kukatko/internal/database/dbtest"
	"github.com/panbotka/kukatko/internal/project"
)

// These tests run only under `make test-integration` against the database named
// by KUKATKO_TEST_DATABASE_URL. They share one database and truncate between
// cases, so they intentionally do not run in parallel.

// actorUID is the account every mutation in these tests is attributed to.
const actorUID = "us_project_admin"

// newStore returns a project.Store over a freshly truncated integration database
// (bootstrap project re-seeded) with the acting account in place, plus the
// database handle and the audit meta to pass to mutations.
func newStore(t *testing.T) (*project.Store, *database.DB, audit.Meta) {
	t.Helper()
	db := dbtest.New(t)
	dbtest.TruncateAll(t, db)
	if err := auth.NewStore(db.Pool()).CreateUser(context.Background(), auth.User{
		UID: actorUID, Username: "padmin", Email: "padmin@example.test", PasswordHash: "x",
		Role: auth.RoleMaintainer,
	}); err != nil {
		t.Fatalf("seeding the acting user: %v", err)
	}
	return project.NewStore(db.Pool()), db, audit.Meta{ActorUID: actorUID, IP: "192.0.2.1"}
}

// auditRecords returns the audit rows recorded for action, newest first.
func auditRecords(t *testing.T, db *database.DB, action string) []audit.Record {
	t.Helper()
	records, err := audit.NewStore(db.Pool()).List(context.Background(), audit.Filter{Action: action, Limit: 100})
	if err != nil {
		t.Fatalf("listing audit %q: %v", action, err)
	}
	return records
}

// mustCreate creates a project with the given slug and name or fails the test.
func mustCreate(t *testing.T, store *project.Store, meta audit.Meta, slug, name string) *project.Project {
	t.Helper()
	p, err := store.Create(context.Background(), meta, project.CreateParams{Slug: slug, Name: name})
	if err != nil {
		t.Fatalf("Create(%q): %v", slug, err)
	}
	return p
}

// TestBootstrapProject verifies the bootstrap row: fixed uid, slug v, Veselice,
// active, root storage prefix, no limits.
func TestBootstrapProject(t *testing.T) {
	store, _, _ := newStore(t)
	ctx := context.Background()

	p, err := store.Get(ctx, project.BootstrapUID)
	if err != nil {
		t.Fatalf("Get(bootstrap): %v", err)
	}
	if p.Slug != project.BootstrapSlug || p.Name != "Veselice" || p.Description != "" || !p.Active ||
		p.StoragePrefix != "" || p.MaxFileBytes != nil || p.MaxTotalBytes != nil {
		t.Errorf("bootstrap project = %+v", p)
	}
	bySlug, err := store.GetBySlug(ctx, project.BootstrapSlug)
	if err != nil || bySlug.UID != project.BootstrapUID {
		t.Errorf("GetBySlug(v) = %+v, %v; want the bootstrap project", bySlug, err)
	}
}

// TestCreate verifies a created project gets a pj uid, its own storage prefix,
// trimmed fields and the limits, and that the creation is audited.
func TestCreate(t *testing.T) {
	store, db, meta := newStore(t)
	ctx := context.Background()

	fileLimit, totalLimit := int64(50<<20), int64(10<<30)
	p, err := store.Create(ctx, meta, project.CreateParams{
		Slug: "rodina", Name: "  Rodina ", Description: " Rodinný archiv ",
		MaxFileBytes: &fileLimit, MaxTotalBytes: &totalLimit,
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if len(p.UID) != 26 || p.UID[:2] != "pj" || p.UID == project.BootstrapUID {
		t.Errorf("uid = %q, want a fresh 26-character pj uid", p.UID)
	}
	if p.StoragePrefix != "projects/"+p.UID+"/" {
		t.Errorf("storage prefix = %q, want projects/<uid>/", p.StoragePrefix)
	}
	if p.Name != "Rodina" || p.Description != "Rodinný archiv" || !p.Active {
		t.Errorf("created = %+v", p)
	}
	if p.MaxFileBytes == nil || *p.MaxFileBytes != fileLimit || p.MaxTotalBytes == nil || *p.MaxTotalBytes != totalLimit {
		t.Errorf("limits = %v / %v", p.MaxFileBytes, p.MaxTotalBytes)
	}

	got, err := store.Get(ctx, p.UID)
	if err != nil || got.Slug != "rodina" {
		t.Errorf("Get(created) = %+v, %v", got, err)
	}

	records := auditRecords(t, db, audit.ActionProjectCreate)
	if len(records) != 1 {
		t.Fatalf("project.create audit rows = %d, want 1", len(records))
	}
	rec := records[0]
	if rec.TargetType != project.TargetType || rec.TargetUID == nil || *rec.TargetUID != p.UID ||
		rec.ActorUID == nil || *rec.ActorUID != actorUID {
		t.Errorf("audit record = %+v", rec)
	}
	if details := rec.Details; details["slug"] != "rodina" || details["name"] != "Rodina" {
		t.Errorf("audit details = %v", details)
	}
}

// TestCreate_errors verifies every refusal and that a refused create writes
// neither a row nor an audit entry.
func TestCreate_errors(t *testing.T) {
	store, db, meta := newStore(t)
	ctx := context.Background()
	zero := int64(0)

	tests := []struct {
		name    string
		params  project.CreateParams
		wantErr error
	}{
		{name: "taken slug", params: project.CreateParams{Slug: "v", Name: "Jiná"}, wantErr: project.ErrSlugTaken},
		{name: "invalid slug", params: project.CreateParams{Slug: "Ab", Name: "X"}, wantErr: project.ErrInvalidSlug},
		{name: "reserved slug", params: project.CreateParams{Slug: "api", Name: "X"}, wantErr: project.ErrInvalidSlug},
		{name: "blank name", params: project.CreateParams{Slug: "x", Name: "  "}, wantErr: project.ErrInvalidName},
		{
			name:    "zero file limit",
			params:  project.CreateParams{Slug: "x", Name: "X", MaxFileBytes: &zero},
			wantErr: project.ErrInvalidLimit,
		},
		{
			name:    "zero total limit",
			params:  project.CreateParams{Slug: "x", Name: "X", MaxTotalBytes: &zero},
			wantErr: project.ErrInvalidLimit,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := store.Create(ctx, meta, tt.params); !errors.Is(err, tt.wantErr) {
				t.Errorf("Create = %v, want %v", err, tt.wantErr)
			}
		})
	}

	list, err := store.List(ctx)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(list) != 1 {
		t.Errorf("projects after refused creates = %d, want only the bootstrap one", len(list))
	}
	if n := len(auditRecords(t, db, audit.ActionProjectCreate)); n != 0 {
		t.Errorf("project.create audit rows after refusals = %d, want 0", n)
	}
}

// TestGet_notFound verifies unknown uids and slugs are ErrNotFound.
func TestGet_notFound(t *testing.T) {
	store, _, _ := newStore(t)
	ctx := context.Background()

	if _, err := store.Get(ctx, "pjnope"); !errors.Is(err, project.ErrNotFound) {
		t.Errorf("Get(unknown) = %v, want ErrNotFound", err)
	}
	if _, err := store.GetBySlug(ctx, "nope"); !errors.Is(err, project.ErrNotFound) {
		t.Errorf("GetBySlug(unknown) = %v, want ErrNotFound", err)
	}
}

// TestList verifies projects come back ordered by name.
func TestList(t *testing.T) {
	store, _, meta := newStore(t)

	mustCreate(t, store, meta, "zz", "Zlín")
	mustCreate(t, store, meta, "aa", "Adamov")
	list, err := store.List(context.Background())
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	var names []string
	for _, p := range list {
		names = append(names, p.Name)
	}
	if want := []string{"Adamov", "Veselice", "Zlín"}; len(names) != len(want) ||
		names[0] != want[0] || names[1] != want[1] || names[2] != want[2] {
		t.Errorf("List names = %v, want %v", names, want)
	}
}

// TestUpdate verifies a patch changes only what it sets, clears a limit, keeps
// the slug and prefix, and records the old → new diff.
func TestUpdate(t *testing.T) {
	store, db, meta := newStore(t)
	ctx := context.Background()

	limit := int64(1000)
	created, err := store.Create(ctx, meta, project.CreateParams{Slug: "klub", Name: "Klub", MaxFileBytes: &limit})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	name, desc, inactive, total := "Klub přátel", "popis", false, int64(5000)
	updated, err := store.Update(ctx, meta, created.UID, project.UpdatePatch{
		Name: &name, Description: &desc, Active: &inactive, ClearMaxFileBytes: true, MaxTotalBytes: &total,
	})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if updated.Name != name || updated.Description != desc || updated.Active || updated.MaxFileBytes != nil ||
		updated.MaxTotalBytes == nil || *updated.MaxTotalBytes != total {
		t.Errorf("updated = %+v", updated)
	}
	if updated.Slug != "klub" || updated.StoragePrefix != created.StoragePrefix {
		t.Errorf("slug/prefix moved: %+v", updated)
	}
	if !updated.UpdatedAt.After(created.UpdatedAt) && !updated.UpdatedAt.Equal(created.UpdatedAt) {
		t.Errorf("updated_at went backwards: %v → %v", created.UpdatedAt, updated.UpdatedAt)
	}

	records := auditRecords(t, db, audit.ActionProjectUpdate)
	if len(records) != 1 {
		t.Fatalf("project.update audit rows = %d, want 1", len(records))
	}
	changes, ok := records[0].Details[audit.ChangesKey].(map[string]any)
	if !ok {
		t.Fatalf("audit details carry no changes: %v", records[0].Details)
	}
	for _, field := range []string{"name", "description", "active", "max_file_bytes", "max_total_bytes"} {
		if _, ok := changes[field]; !ok {
			t.Errorf("changes lack %q: %v", field, changes)
		}
	}
}

// TestUpdate_noChangeIsSilent verifies an empty (or same-value) patch writes no
// audit entry.
func TestUpdate_noChangeIsSilent(t *testing.T) {
	store, db, meta := newStore(t)
	ctx := context.Background()

	same := "Veselice"
	for _, patch := range []project.UpdatePatch{{}, {Name: &same}} {
		p, err := store.Update(ctx, meta, project.BootstrapUID, patch)
		if err != nil || p.Name != "Veselice" {
			t.Errorf("Update(no change) = %+v, %v", p, err)
		}
	}
	if n := len(auditRecords(t, db, audit.ActionProjectUpdate)); n != 0 {
		t.Errorf("project.update audit rows after no-op patches = %d, want 0", n)
	}
}

// TestUpdate_errors verifies the refusals of Update.
func TestUpdate_errors(t *testing.T) {
	store, db, meta := newStore(t)
	ctx := context.Background()
	blank, negative, limit := " ", int64(-1), int64(7)

	tests := []struct {
		name    string
		uid     string
		patch   project.UpdatePatch
		wantErr error
	}{
		{name: "unknown uid", uid: "pjnope", patch: project.UpdatePatch{}, wantErr: project.ErrNotFound},
		{name: "blank name", uid: project.BootstrapUID, patch: project.UpdatePatch{Name: &blank},
			wantErr: project.ErrInvalidName},
		{name: "negative limit", uid: project.BootstrapUID, patch: project.UpdatePatch{MaxTotalBytes: &negative},
			wantErr: project.ErrInvalidLimit},
		{name: "set and clear", uid: project.BootstrapUID,
			patch:   project.UpdatePatch{MaxFileBytes: &limit, ClearMaxFileBytes: true},
			wantErr: project.ErrInvalidLimit},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := store.Update(ctx, meta, tt.uid, tt.patch); !errors.Is(err, tt.wantErr) {
				t.Errorf("Update = %v, want %v", err, tt.wantErr)
			}
		})
	}
	if n := len(auditRecords(t, db, audit.ActionProjectUpdate)); n != 0 {
		t.Errorf("project.update audit rows after refusals = %d, want 0", n)
	}
}

// TestSetStoragePrefix verifies the prefix moves, is audited with old and new,
// and malformed prefixes or unknown uids are refused.
func TestSetStoragePrefix(t *testing.T) {
	store, db, meta := newStore(t)
	ctx := context.Background()

	target := project.StoragePrefixFor(project.BootstrapUID)
	p, err := store.SetStoragePrefix(ctx, meta, project.BootstrapUID, target)
	if err != nil {
		t.Fatalf("SetStoragePrefix: %v", err)
	}
	if p.StoragePrefix != target {
		t.Errorf("storage prefix = %q, want %q", p.StoragePrefix, target)
	}
	records := auditRecords(t, db, audit.ActionProjectStoragePrefix)
	if len(records) != 1 {
		t.Fatalf("project.storage_prefix audit rows = %d, want 1", len(records))
	}
	if details := records[0].Details; details["old"] != "" || details["new"] != target {
		t.Errorf("audit details = %v", details)
	}
	if rec := records[0]; rec.TargetUID == nil || *rec.TargetUID != project.BootstrapUID {
		t.Errorf("audit target = %v", rec.TargetUID)
	}

	for _, bad := range []string{"/abs/", "no-slash", "/"} {
		if _, err := store.SetStoragePrefix(ctx, meta, project.BootstrapUID, bad); !errors.Is(err,
			project.ErrInvalidStoragePrefix) {
			t.Errorf("SetStoragePrefix(%q) = %v, want ErrInvalidStoragePrefix", bad, err)
		}
	}
	if _, err := store.SetStoragePrefix(ctx, meta, "pjnope", ""); !errors.Is(err, project.ErrNotFound) {
		t.Errorf("SetStoragePrefix(unknown) = %v, want ErrNotFound", err)
	}
	if n := len(auditRecords(t, db, audit.ActionProjectStoragePrefix)); n != 1 {
		t.Errorf("project.storage_prefix audit rows after refusals = %d, want 1", n)
	}
}

// TestUsage verifies the bootstrap project counts the whole library (archived
// photos included), another project is empty and an unknown uid is ErrNotFound.
func TestUsage(t *testing.T) {
	store, db, meta := newStore(t)
	ctx := context.Background()

	empty, err := store.Usage(ctx, project.BootstrapUID)
	if err != nil || empty != (project.Usage{}) {
		t.Errorf("Usage(empty library) = %+v, %v; want zeros", empty, err)
	}
	if _, err := db.Pool().Exec(ctx, `
		INSERT INTO photos (uid, file_hash, file_path, file_size, archived_at) VALUES
		('phusage00000000000000001', 'h1', '2020/01/a.jpg', 100, NULL),
		('phusage00000000000000002', 'h2', '2020/01/b.jpg', 250, now())`); err != nil {
		t.Fatalf("seeding photos: %v", err)
	}

	usage, err := store.Usage(ctx, project.BootstrapUID)
	if err != nil {
		t.Fatalf("Usage(bootstrap): %v", err)
	}
	if usage.Photos != 2 || usage.OriginalsBytes != 350 {
		t.Errorf("Usage(bootstrap) = %+v, want 2 photos / 350 bytes", usage)
	}

	other := mustCreate(t, store, meta, "jiny", "Jiný")
	if usage, err := store.Usage(ctx, other.UID); err != nil || usage != (project.Usage{}) {
		t.Errorf("Usage(other) = %+v, %v; want zeros", usage, err)
	}
	if _, err := store.Usage(ctx, "pjnope"); !errors.Is(err, project.ErrNotFound) {
		t.Errorf("Usage(unknown) = %v, want ErrNotFound", err)
	}
}
