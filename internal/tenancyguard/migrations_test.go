package tenancyguard

import (
	"maps"
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
