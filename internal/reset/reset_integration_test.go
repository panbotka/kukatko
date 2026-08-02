//go:build integration

package reset_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/panbotka/kukatko/internal/audit"
	"github.com/panbotka/kukatko/internal/database"
	"github.com/panbotka/kukatko/internal/database/dbtest"
	"github.com/panbotka/kukatko/internal/reset"
	"github.com/panbotka/kukatko/internal/sidecarexport"
	"github.com/panbotka/kukatko/internal/storage"
	"github.com/panbotka/kukatko/internal/thumb"
)

// These tests run only under `make test-integration` against the database named
// by KUKATKO_TEST_DATABASE_URL. They share one database and truncate per case, so
// they intentionally do not run in parallel.

// resetEnv bundles the live collaborators a reset test needs over a freshly
// truncated database and an isolated on-disk store.
type resetEnv struct {
	db       *database.DB
	fs       *storage.FS
	root     string
	cacheDir string
	target   reset.Target
	svc      *reset.Service
}

// newResetEnv wires the reset service over a real filesystem store, a real
// thumbnailer and the integration database.
func newResetEnv(t *testing.T) *resetEnv {
	t.Helper()

	db := dbtest.New(t)
	dbtest.TruncateAll(t, db)
	root := t.TempDir()
	cacheDir := t.TempDir()
	store, err := storage.NewFS(root)
	if err != nil {
		t.Fatalf("storage.NewFS: %v", err)
	}
	// No bucket: the fixture wipes a filesystem store, so nothing is confirmed
	// beyond the database name.
	target, err := reset.TargetFromConfig(os.Getenv(dbtest.EnvTestDatabaseURL), "")
	if err != nil {
		t.Fatalf("reset.TargetFromConfig: %v", err)
	}
	svc := reset.New(reset.Config{
		Pool:     db.Pool(),
		Target:   target,
		Storage:  store,
		Thumbs:   thumb.New(store, cacheDir),
		CacheDir: cacheDir,
	})
	return &resetEnv{db: db, fs: store, root: root, cacheDir: cacheDir, target: target, svc: svc}
}

// exec runs one statement against the test database and fails the test on error.
func (e *resetEnv) exec(t *testing.T, sql string, args ...any) {
	t.Helper()
	if _, err := e.db.Pool().Exec(t.Context(), sql, args...); err != nil {
		t.Fatalf("exec %q: %v", sql, err)
	}
}

// count returns the row count of one table.
func (e *resetEnv) count(t *testing.T, table string) int64 {
	t.Helper()
	var rows int64
	if err := e.db.Pool().QueryRow(t.Context(), "SELECT count(*) FROM "+table).Scan(&rows); err != nil {
		t.Fatalf("counting %s: %v", table, err)
	}
	return rows
}

// seedLibrary creates a small but broad library: an account with a session and an
// API token (the things a wipe must preserve), two photos with their files,
// vectors, faces, an album, a label, a job and an import run (the things it must
// remove), plus the objects on disk that belong to them.
func (e *resetEnv) seedLibrary(t *testing.T) {
	t.Helper()

	e.exec(t, `INSERT INTO users (uid, username, password_hash, role) VALUES ($1,$2,$3,$4)`,
		"usr000000001", "operator", "hash", "maintainer")
	e.exec(t, `INSERT INTO sessions (id, token, download_token, user_uid, role, expires_at)
		VALUES ($1,$2,$3,$4,$5,$6)`,
		"ses1", "tok1", "dl1", "usr000000001", "maintainer", time.Now().Add(time.Hour))
	e.exec(t, `INSERT INTO api_tokens (id, user_uid, name, secret_hash) VALUES ($1,$2,$3,$4)`,
		"tokid1", "usr000000001", "migration", "secret")
	e.exec(t, `INSERT INTO announcements (id, message) VALUES (true, $1)`, "migration in progress")
	if err := audit.NewStore(e.db.Pool()).Record(t.Context(), audit.Entry{
		Action: "photo.update", TargetType: "photos", TargetUID: "pht000000001",
	}); err != nil {
		t.Fatalf("seeding an audit entry: %v", err)
	}

	for _, photo := range []struct{ uid, hash, path string }{
		{uid: "pht000000001", hash: "aa11bb22cc33dd44", path: "2024/05/IMG_1.jpg"},
		{uid: "pht000000002", hash: "bb22cc33dd44ee55", path: "2024/06/IMG_2.jpg"},
	} {
		e.exec(t, `INSERT INTO photos (uid, file_hash, file_path, file_name, file_size, file_mime)
			VALUES ($1,$2,$3,$4,$5,$6)`,
			photo.uid, photo.hash, photo.path, filepath.Base(photo.path), 100, "image/jpeg")
		e.exec(t, `INSERT INTO photo_files (photo_uid, file_path, file_hash, file_size, file_mime, is_primary, role)
			VALUES ($1,$2,$3,$4,$5,true,'original')`,
			photo.uid, photo.path, photo.hash, 100, "image/jpeg")
		e.exec(t, `INSERT INTO photo_phashes (photo_uid, phash, dhash) VALUES ($1,1,2)`, photo.uid)
		e.writeObject(t, photo.path, "original")
		sidecarKey, err := sidecarexport.KeyFor(photo.path)
		if err != nil {
			t.Fatalf("sidecarexport.KeyFor: %v", err)
		}
		e.writeObject(t, sidecarKey, "sidecar")
		for _, size := range thumb.SizeNames() {
			key, err := thumb.RelPath(photo.hash, size)
			if err != nil {
				t.Fatalf("thumb.RelPath: %v", err)
			}
			e.writeObject(t, key, "thumb")
			e.writeCacheFile(t, key)
		}
	}

	e.exec(t, `INSERT INTO albums (uid, title, slug) VALUES ($1,$2,$3)`, "alb000000001", "Trip", "trip")
	e.exec(t, `INSERT INTO album_photos (album_uid, photo_uid) VALUES ($1,$2)`, "alb000000001", "pht000000001")
	e.exec(t, `INSERT INTO labels (uid, name, slug) VALUES ($1,$2,$3)`, "lbl000000001", "Dog", "dog")
	e.exec(t, `INSERT INTO photo_labels (photo_uid, label_uid) VALUES ($1,$2)`, "pht000000001", "lbl000000001")
	e.exec(t, `INSERT INTO jobs (type, payload) VALUES ('thumbnail', '{}'::jsonb)`)
	e.exec(t, `INSERT INTO import_runs (source, status) VALUES ('photoprism', 'done')`)
	e.exec(t, `INSERT INTO user_favorites (user_uid, photo_uid) VALUES ($1,$2)`,
		"usr000000001", "pht000000001")
}

// writeObject writes a placeholder object into the store at key.
func (e *resetEnv) writeObject(t *testing.T, key, content string) {
	t.Helper()
	abs := filepath.Join(e.root, filepath.FromSlash(key))
	if err := os.MkdirAll(filepath.Dir(abs), 0o750); err != nil {
		t.Fatalf("creating %s: %v", filepath.Dir(abs), err)
	}
	if err := os.WriteFile(abs, []byte(content), 0o600); err != nil {
		t.Fatalf("writing %s: %v", abs, err)
	}
}

// writeCacheFile writes a placeholder into the local derived-image cache at the
// thumbnail's own relative path, which is where the thumbnailer keeps it.
func (e *resetEnv) writeCacheFile(t *testing.T, key string) {
	t.Helper()
	abs := filepath.Join(e.cacheDir, filepath.FromSlash(key))
	if err := os.MkdirAll(filepath.Dir(abs), 0o750); err != nil {
		t.Fatalf("creating %s: %v", filepath.Dir(abs), err)
	}
	if err := os.WriteFile(abs, []byte("thumb"), 0o600); err != nil {
		t.Fatalf("writing %s: %v", abs, err)
	}
}

// storedKeys returns every key the store currently holds, sorted.
func (e *resetEnv) storedKeys(t *testing.T) []string {
	t.Helper()
	var keys []string
	if err := e.fs.Keys(t.Context(), func(key string) error {
		keys = append(keys, key)
		return nil
	}); err != nil {
		t.Fatalf("listing the store: %v", err)
	}
	return keys
}

// executeOptions returns the options of a confirmed, executing run.
func (e *resetEnv) executeOptions() reset.Options {
	return reset.Options{Execute: true, Confirm: e.target.Database, Operator: "test@integration"}
}
