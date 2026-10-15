//go:build integration

package database_test

import (
	"strings"
	"testing"

	"github.com/panbotka/kukatko/internal/inventory"
)

// TestReleaseExpectList_provenOnTheRealPath replays the real upgrade path of a
// pre-tenancy instance — schema 0099 (0.31.0) to the newest migration — and
// judges it with the documented inventory-diff expect list. The
// list must leave no difference unexpected, and every entry must allow at
// least one difference (an entry that matches nothing is stale or too broad).
//
// The database reaches 0099 the way a rollback does: a fresh schema migrated
// to the newest file, then the whole down chain. It is then seeded the way
// a pre-tenancy library looks at 0099 — accounts on the old roles (a viewer and an editor
// collapse to `user`, a maintainer stays), a session caching a role, a photo
// in an album — so the digests have rows to compare and role-collapse has
// accounts to judge.
func TestReleaseExpectList_provenOnTheRealPath(t *testing.T) {
	list := readReleaseExpectList(t)
	db := freshSchemaDB(t)
	ctx := t.Context()
	if _, err := db.Migrate(ctx); err != nil {
		t.Fatalf("migrating a fresh schema: %v", err)
	}
	for _, version := range rollbackChain {
		runDownScript(t, db, version)
	}
	if _, err := db.Pool().Exec(ctx, `
		INSERT INTO users (uid, username, email, password_hash, role, approved_at) VALUES
			('us_rel_viewer', 'relviewer', 'relviewer@example.test', 'x', 'viewer', now()),
			('us_rel_editor', 'releditor', 'releditor@example.test', 'x', 'editor', now()),
			('us_rel_maint', 'relmaint', 'relmaint@example.test', 'x', 'maintainer', now());
		INSERT INTO sessions (id, token, download_token, user_uid, role, expires_at)
		VALUES ('ses_rel', 'tok_rel', 'dl_rel', 'us_rel_viewer', 'viewer', now() + interval '1 hour');
		INSERT INTO photos (uid, file_hash, file_path, file_size)
		VALUES ('ph_rel', 'hash_rel', '2024/01/rel.jpg', 3);
		INSERT INTO albums (uid, slug, title) VALUES ('al_rel', 'release', 'Release');
		INSERT INTO album_photos (album_uid, photo_uid) VALUES ('al_rel', 'ph_rel')`); err != nil {
		t.Fatalf("seeding the 0099 library: %v", err)
	}

	before, after := t.TempDir(), t.TempDir()
	if err := inventory.Collect(ctx, db.Pool(), before, nil); err != nil {
		t.Fatalf("inventory A: %v", err)
	}
	if _, err := db.Migrate(ctx); err != nil {
		t.Fatalf("migrating 0099 to the newest file: %v", err)
	}
	if err := inventory.Collect(ctx, db.Pool(), after, nil); err != nil {
		t.Fatalf("inventory B: %v", err)
	}
	snapA, err := inventory.Load(before)
	if err != nil {
		t.Fatalf("loading inventory A: %v", err)
	}
	snapB, err := inventory.Load(after)
	if err != nil {
		t.Fatalf("loading inventory B: %v", err)
	}
	exp, err := inventory.ParseExpectations(list.expects)
	if err != nil {
		t.Fatalf("the documented expect list does not parse: %v", err)
	}
	result := inventory.Judge(inventory.Diff(snapA, snapB), exp)
	var report strings.Builder
	if err := result.Write(&report); err != nil {
		t.Fatalf("writing the diff: %v", err)
	}
	if result.Unexpected() > 0 {
		t.Errorf("the documented expect list leaves %d difference(s) of 0099 → %04d unexpected "+
			"(docs/migrations-rollback/README.md, release-expect):\n%s", result.Unexpected(), list.to, report.String())
	}
	used := map[string]bool{}
	for _, d := range result.Differences {
		used[d.AllowedBy] = true
	}
	for _, e := range list.expects {
		if !used[e] {
			t.Errorf("--expect %s allows no difference of 0099 → %04d: drop it or name what it is for\n%s",
				e, list.to, report.String())
		}
	}
}
