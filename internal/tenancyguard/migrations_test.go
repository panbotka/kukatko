package tenancyguard

import (
	"maps"
	"slices"
	"testing"
	"testing/fstest"
)

func TestProjectColumnTables(t *testing.T) {
	t.Parallel()
	fsys := fstest.MapFS{
		"m/0001_init.sql": {Data: []byte(`
CREATE TABLE IF NOT EXISTS photos (uid TEXT PRIMARY KEY);
CREATE TABLE gone (uid TEXT, project_id TEXT NOT NULL);
-- ALTER TABLE commented ADD COLUMN project_id TEXT;
`)},
		"m/0002_projects.sql": {Data: []byte(`
ALTER TABLE photos ADD COLUMN IF NOT EXISTS project_id TEXT NOT NULL
    DEFAULT 'v' REFERENCES projects (uid);
ALTER TABLE ONLY dropped ADD COLUMN project_id TEXT;
CREATE TABLE IF NOT EXISTS members (
    project_uid TEXT NOT NULL REFERENCES projects (uid),
    user_uid    TEXT NOT NULL,
    PRIMARY KEY (project_uid, user_uid)
);
CREATE TABLE keyed_only (a TEXT, b TEXT, PRIMARY KEY (project_uid, a));
CREATE INDEX idx ON photos (project_id);
`)},
		"m/0003_drop.sql": {Data: []byte(`
ALTER TABLE dropped DROP COLUMN IF EXISTS project_id;
DROP TABLE IF EXISTS gone CASCADE;
`)},
		"m/readme.txt": {Data: []byte("ALTER TABLE ignored ADD COLUMN project_id TEXT;")},
	}
	got, err := ProjectColumnTables(fsys, "m")
	if err != nil {
		t.Fatalf("ProjectColumnTables: %v", err)
	}
	want := map[string]string{"photos": "project_id", "members": "project_uid"}
	if !maps.Equal(got, want) {
		t.Errorf("tables = %v, want %v", got, want)
	}
}

func TestProjectColumnTables_missingDir(t *testing.T) {
	t.Parallel()
	if _, err := ProjectColumnTables(fstest.MapFS{}, "nope"); err == nil {
		t.Error("want an error for a missing directory")
	}
}

func TestRootAndExemptTables_disjoint(t *testing.T) {
	t.Parallel()
	for table := range ExemptTables {
		if _, ok := RootTables[table]; ok {
			t.Errorf("%s is both a root table and exempt", table)
		}
		if _, ok := ReadScopedTables[table]; ok {
			t.Errorf("%s is both read-scoped and exempt", table)
		}
	}
	for table := range ReadScopedTables {
		if _, ok := RootTables[table]; ok {
			t.Errorf("%s is both a root table and read-scoped", table)
		}
	}
	for _, tables := range []map[string]string{RootTables, ReadScopedTables} {
		for table, col := range tables {
			if col != "project_id" && col != "project_uid" {
				t.Errorf("%s: project column %q is neither project_id nor project_uid", table, col)
			}
		}
	}
}

func TestForeignKeys(t *testing.T) {
	t.Parallel()
	fsys := fstest.MapFS{
		"m/0001_init.sql": {Data: []byte(`
CREATE TABLE photos (uid TEXT PRIMARY KEY, cover TEXT REFERENCES photos (uid));
CREATE TABLE IF NOT EXISTS photo_comments (
    uid       TEXT PRIMARY KEY,
    photo_uid TEXT NOT NULL REFERENCES photos (uid) ON DELETE CASCADE, -- a comment, REFERENCES nothing
    author    TEXT,
    CONSTRAINT one_author CHECK (author <> ''),
    FOREIGN KEY (author) REFERENCES users (uid)
);
CREATE TABLE gone (photo_uid TEXT REFERENCES photos (uid));
`)},
		"m/0002_more.sql": {Data: []byte(`
ALTER TABLE photo_comments RENAME TO comments;
ALTER TABLE comments ADD COLUMN album_uid TEXT REFERENCES albums (uid),
    ADD CONSTRAINT task_fk FOREIGN KEY (task_uid) REFERENCES tasks (uid);
ALTER TABLE comments RENAME COLUMN author TO author_uid;
ALTER TABLE photos DROP CONSTRAINT IF EXISTS cover_fk, DROP COLUMN IF EXISTS cover;
DROP TABLE IF EXISTS gone;
CREATE TABLE members (
    subject_uid TEXT NULL CONSTRAINT members_subject_fkey REFERENCES subjects (uid),
    PRIMARY KEY (subject_uid)
);
`)},
	}
	got, err := ForeignKeys(fsys, "m")
	if err != nil {
		t.Fatalf("ForeignKeys: %v", err)
	}
	want := map[string]map[string]string{
		"photos":   {},
		"comments": {"photo_uid": "photos", "author_uid": "users", "album_uid": "albums", "task_uid": "tasks"},
		"members":  {"subject_uid": "subjects"},
	}
	if len(got) != len(want) {
		t.Errorf("tables = %v, want %v", got, want)
	}
	for table, cols := range want {
		if !maps.Equal(got[table], cols) {
			t.Errorf("%s = %v, want %v", table, got[table], cols)
		}
	}
	if _, err := ForeignKeys(fstest.MapFS{}, "nope"); err == nil {
		t.Error("want an error for a missing directory")
	}
}

func TestChildTables(t *testing.T) {
	t.Parallel()
	fks := map[string]map[string]string{
		"photos":        {"uploaded_by": "users"},
		"photo_phashes": {"photo_uid": "photos"},
		"comments":      {"photo_uid": "photos", "album_uid": "albums", "author_uid": "users"},
		"mentions":      {"comment_uid": "comments", "user_uid": "users"},
		"sessions":      {"user_uid": "users"},
		"members":       {"subject_uid": "subjects", "project_uid": "projects"},
	}
	got := ChildTables(fks, map[string]string{"photos": "project_id", "members": "project_uid"})
	want := map[string][]string{
		"photo_phashes": {"photo_uid"},
		"comments":      {"album_uid", "photo_uid"},
		"mentions":      {"comment_uid"},
	}
	if !maps.EqualFunc(got, want, slices.Equal) {
		t.Errorf("ChildTables = %v, want %v", got, want)
	}
}
