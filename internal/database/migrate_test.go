package database

import (
	"errors"
	"testing"
	"testing/fstest"
)

// mapFS builds an in-memory filesystem with the given files (path -> contents)
// for exercising the migration loader without touching a database.
func mapFS(files map[string]string) fstest.MapFS {
	fsys := make(fstest.MapFS, len(files))
	for name, body := range files {
		fsys[name] = &fstest.MapFile{Data: []byte(body)}
	}
	return fsys
}

func TestParseMigrationFilename(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		filename    string
		wantVersion int64
		wantName    string
		wantErr     error
	}{
		{"simple", "0001_init.sql", 1, "init", nil},
		{"snake case name", "0012_add_photos_table.sql", 12, "add_photos_table", nil},
		{"wide version", "20240101_seed.sql", 20240101, "seed", nil},
		{"missing version", "init.sql", 0, "", errBadMigrationName},
		{"missing name", "0001.sql", 0, "", errBadMigrationName},
		{"wrong extension", "0001_init.txt", 0, "", errBadMigrationName},
		{"uppercase name", "0001_Init.sql", 0, "", errBadMigrationName},
		{"leading text", "v0001_init.sql", 0, "", errBadMigrationName},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			version, name, err := parseMigrationFilename(tt.filename)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("parseMigrationFilename(%q) error = %v, want %v", tt.filename, err, tt.wantErr)
			}
			if tt.wantErr != nil {
				return
			}
			if version != tt.wantVersion {
				t.Errorf("version = %d, want %d", version, tt.wantVersion)
			}
			if name != tt.wantName {
				t.Errorf("name = %q, want %q", name, tt.wantName)
			}
		})
	}
}

func TestLoadMigrations_ordering(t *testing.T) {
	t.Parallel()

	fsys := mapFS(map[string]string{
		"m/0002_second.sql": "SELECT 2;",
		"m/0001_first.sql":  "SELECT 1;",
		"m/0010_tenth.sql":  "SELECT 10;",
		"m/README.txt":      "not a migration",
	})

	got, err := loadMigrations(fsys, "m")
	if err != nil {
		t.Fatalf("loadMigrations: %v", err)
	}

	wantVersions := []int64{1, 2, 10}
	if len(got) != len(wantVersions) {
		t.Fatalf("got %d migrations, want %d", len(got), len(wantVersions))
	}
	for i, want := range wantVersions {
		if got[i].version != want {
			t.Errorf("migration %d version = %d, want %d", i, got[i].version, want)
		}
		if got[i].sql == "" {
			t.Errorf("migration %d has empty sql", i)
		}
	}
}

func TestLoadMigrations_numericNotLexicographic(t *testing.T) {
	t.Parallel()

	// Unpadded names sort differently lexicographically ("10" < "9") than
	// numerically; the loader must order by numeric version.
	fsys := mapFS(map[string]string{
		"m/9_nine.sql": "SELECT 9;",
		"m/10_ten.sql": "SELECT 10;",
		"m/2_two.sql":  "SELECT 2;",
	})

	got, err := loadMigrations(fsys, "m")
	if err != nil {
		t.Fatalf("loadMigrations: %v", err)
	}

	want := []int64{2, 9, 10}
	for i, w := range want {
		if got[i].version != w {
			t.Errorf("position %d version = %d, want %d", i, got[i].version, w)
		}
	}
}

func TestLoadMigrations_duplicateVersion(t *testing.T) {
	t.Parallel()

	fsys := mapFS(map[string]string{
		"m/0001_first.sql": "SELECT 1;",
		"m/0001_again.sql": "SELECT 2;",
	})

	_, err := loadMigrations(fsys, "m")
	if !errors.Is(err, errDuplicateMigration) {
		t.Fatalf("loadMigrations error = %v, want errDuplicateMigration", err)
	}
}

func TestLoadMigrations_badFilename(t *testing.T) {
	t.Parallel()

	fsys := mapFS(map[string]string{
		"m/not-a-migration.sql": "SELECT 1;",
	})

	_, err := loadMigrations(fsys, "m")
	if !errors.Is(err, errBadMigrationName) {
		t.Fatalf("loadMigrations error = %v, want errBadMigrationName", err)
	}
}

func TestLoadMigrations_missingDir(t *testing.T) {
	t.Parallel()

	_, err := loadMigrations(mapFS(nil), "does-not-exist")
	if err == nil {
		t.Fatal("loadMigrations on missing dir = nil error, want error")
	}
}

func TestEmbeddedMigrations_loadAndOrder(t *testing.T) {
	t.Parallel()

	got, err := loadMigrations(migrationFS, migrationsDir)
	if err != nil {
		t.Fatalf("loading embedded migrations: %v", err)
	}
	if len(got) == 0 {
		t.Fatal("expected at least one embedded migration")
	}

	if got[0].version != 1 || got[0].name != "init" {
		t.Errorf("first migration = (%d, %q), want (1, %q)", got[0].version, got[0].name, "init")
	}
	for i := 1; i < len(got); i++ {
		if got[i].version <= got[i-1].version {
			t.Errorf("migrations not strictly increasing at %d: %d <= %d",
				i, got[i].version, got[i-1].version)
		}
	}
	for _, mig := range got {
		if mig.sql == "" {
			t.Errorf("embedded migration %q has empty sql", mig.filename)
		}
	}
}
