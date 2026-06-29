//go:build integration

package photos_test

import (
	"strings"
	"testing"

	"github.com/panbotka/kukatko/internal/database"
)

// zerosHalfvec returns a pgvector halfvec literal of n zero components, e.g.
// "[0,0,0]", for inserting placeholder embeddings in cascade tests.
func zerosHalfvec(n int) string {
	return "[" + strings.Repeat("0,", n-1) + "0]"
}

// countRows runs a COUNT(*) query and returns the single integer result,
// failing the test on a query error.
func countRows(t *testing.T, db *database.DB, query string, args ...any) int {
	t.Helper()
	var n int
	if err := db.Pool().QueryRow(t.Context(), query, args...).Scan(&n); err != nil {
		t.Fatalf("count query %q: %v", query, err)
	}
	return n
}

// TestDeletePhoto_cascadesAllDependents is the FK-cascade regression test for
// the hardening pass: it creates one row in every table that references a photo
// (directly via ON DELETE CASCADE or via ON DELETE SET NULL), deletes the photo
// through the store, and asserts that
//
//   - every CASCADE child row is gone (no orphans / dangling references), and
//   - every SET NULL parent keeps its row but with the photo reference nulled,
//
// while sibling rows that merely belong to the same album/label/subject/user
// survive. This locks in the audited migration constraints so a future schema
// change that drops a cascade is caught.
func TestDeletePhoto_cascadesAllDependents(t *testing.T) {
	store, db := newStore(t)
	ctx := t.Context()

	created, err := store.Create(ctx, samplePhoto("cascade"))
	if err != nil {
		t.Fatalf("create photo: %v", err)
	}
	uid := created.UID

	// Parent rows the photo will reference (SET NULL) or that own a child row.
	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := db.Pool().Exec(ctx, sql, args...); err != nil {
			t.Fatalf("seed %q: %v", sql, err)
		}
	}
	exec(`INSERT INTO users (uid, username, password_hash, role) VALUES ('u_casc', 'casc', 'x', 'viewer')`)
	exec(`INSERT INTO subjects (uid, slug, name, type, cover_photo_uid) VALUES ('su_casc', 'casc', 'Casc', 'person', $1)`, uid)
	exec(`INSERT INTO albums (uid, slug, title, cover_photo_uid) VALUES ('al_casc', 'casc', 'Casc', $1)`, uid)
	exec(`INSERT INTO labels (uid, slug, name) VALUES ('lb_casc', 'casc', 'Casc')`)
	exec(`INSERT INTO face_clusters (uid, centroid, size, model) VALUES ('fc_casc', '` + zerosHalfvec(512) + `'::halfvec(512), 1, 'm')`)

	// One child row in every photo-referencing table (CASCADE).
	exec(`INSERT INTO photo_files (photo_uid, file_path, role) VALUES ($1, '2023/06/casc.jpg', 'original')`, uid)
	exec(`INSERT INTO photo_phashes (photo_uid, phash, dhash) VALUES ($1, 1, 2)`, uid)
	exec(`INSERT INTO photo_edits (photo_uid, rotation) VALUES ($1, 90)`, uid)
	exec(`INSERT INTO embeddings (photo_uid, embedding) VALUES ($1, '`+zerosHalfvec(768)+`'::halfvec(768))`, uid)
	exec(`INSERT INTO faces (photo_uid, face_index, embedding, bbox, cluster_uid)
	      VALUES ($1, 0, '`+zerosHalfvec(512)+`'::halfvec(512), '{0,0,1,1}', 'fc_casc')`, uid)
	exec(`INSERT INTO face_detections (photo_uid, face_count, model) VALUES ($1, 1, 'm')`, uid)
	exec(`INSERT INTO markers (uid, photo_uid, subject_uid, type) VALUES ('mk_casc', $1, 'su_casc', 'face')`, uid)
	exec(`INSERT INTO album_photos (album_uid, photo_uid) VALUES ('al_casc', $1)`, uid)
	exec(`INSERT INTO photo_labels (photo_uid, label_uid) VALUES ($1, 'lb_casc')`, uid)
	exec(`INSERT INTO user_favorites (user_uid, photo_uid) VALUES ('u_casc', $1)`, uid)

	// Delete the photo through the store under test.
	if err := store.Delete(ctx, uid); err != nil {
		t.Fatalf("delete photo: %v", err)
	}

	// Every CASCADE child table must have no rows left for this photo.
	cascadeChildren := []string{
		"photo_files", "photo_phashes", "photo_edits", "embeddings",
		"faces", "face_detections", "markers", "album_photos",
		"photo_labels", "user_favorites",
	}
	for _, table := range cascadeChildren {
		if n := countRows(t, db, "SELECT count(*) FROM "+table+" WHERE photo_uid = $1", uid); n != 0 {
			t.Errorf("%s: %d rows remain after photo delete, want 0 (cascade gap)", table, n)
		}
	}

	// SET NULL parents keep their row but with the photo reference cleared.
	if n := countRows(t, db, "SELECT count(*) FROM subjects WHERE uid = 'su_casc'"); n != 1 {
		t.Errorf("subject deleted by photo cascade: count = %d, want 1", n)
	}
	if n := countRows(t, db, "SELECT count(*) FROM subjects WHERE uid = 'su_casc' AND cover_photo_uid IS NULL"); n != 1 {
		t.Error("subjects.cover_photo_uid not nulled on photo delete")
	}
	if n := countRows(t, db, "SELECT count(*) FROM albums WHERE uid = 'al_casc'"); n != 1 {
		t.Errorf("album deleted by photo cascade: count = %d, want 1", n)
	}
	if n := countRows(t, db, "SELECT count(*) FROM albums WHERE uid = 'al_casc' AND cover_photo_uid IS NULL"); n != 1 {
		t.Error("albums.cover_photo_uid not nulled on photo delete")
	}

	// Sibling rows that merely shared the photo must survive untouched.
	survivors := map[string]string{
		"labels":        "SELECT count(*) FROM labels WHERE uid = 'lb_casc'",
		"users":         "SELECT count(*) FROM users WHERE uid = 'u_casc'",
		"face_clusters": "SELECT count(*) FROM face_clusters WHERE uid = 'fc_casc'",
	}
	for name, query := range survivors {
		if n := countRows(t, db, query); n != 1 {
			t.Errorf("%s: count = %d after photo delete, want 1 (must survive)", name, n)
		}
	}

	// A defensive global check: no row anywhere keyed by this photo's uid leaks.
	leftover := countRows(t, db,
		"SELECT count(*) FROM photos WHERE uid = $1", uid)
	if leftover != 0 {
		t.Errorf("photos row still present after delete: %d", leftover)
	}
}

// TestDeleteUser_setsNullAndCascades verifies the user-referencing foreign keys:
// deleting a user nulls uploaded_by/created_by/actor references (SET NULL) and
// removes the user's sessions and favorites (CASCADE), without deleting the
// photos or albums they touched.
func TestDeleteUser_setsNullAndCascades(t *testing.T) {
	store, db := newStore(t)
	ctx := t.Context()

	created, err := store.Create(ctx, samplePhoto("userdel"))
	if err != nil {
		t.Fatalf("create photo: %v", err)
	}
	uid := created.UID

	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := db.Pool().Exec(ctx, sql, args...); err != nil {
			t.Fatalf("seed %q: %v", sql, err)
		}
	}
	exec(`INSERT INTO users (uid, username, password_hash, role) VALUES ('u_del', 'udel', 'x', 'editor')`)
	exec(`UPDATE photos SET uploaded_by = 'u_del' WHERE uid = $1`, uid)
	exec(`INSERT INTO albums (uid, slug, title, created_by) VALUES ('al_del', 'aldel', 'AlDel', 'u_del')`)
	exec(`INSERT INTO user_favorites (user_uid, photo_uid) VALUES ('u_del', $1)`, uid)

	if _, err := db.Pool().Exec(ctx, "DELETE FROM users WHERE uid = 'u_del'"); err != nil {
		t.Fatalf("delete user: %v", err)
	}

	// CASCADE: the user's favorites are gone.
	if n := countRows(t, db, "SELECT count(*) FROM user_favorites WHERE user_uid = 'u_del'"); n != 0 {
		t.Errorf("user_favorites not cascaded on user delete: %d rows remain", n)
	}
	// SET NULL: the photo survives with a null uploader; the album survives with
	// a null creator.
	if n := countRows(t, db, "SELECT count(*) FROM photos WHERE uid = $1 AND uploaded_by IS NULL", uid); n != 1 {
		t.Error("photos.uploaded_by not nulled on user delete (or photo deleted)")
	}
	if n := countRows(t, db, "SELECT count(*) FROM albums WHERE uid = 'al_del' AND created_by IS NULL"); n != 1 {
		t.Error("albums.created_by not nulled on user delete (or album deleted)")
	}
}
