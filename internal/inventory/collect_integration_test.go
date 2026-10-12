//go:build integration

package inventory_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/panbotka/kukatko/internal/database/dbtest"
	"github.com/panbotka/kukatko/internal/inventory"
	"github.com/panbotka/kukatko/internal/storage"
)

// readFile returns the content of name in dir.
func readFile(t *testing.T, dir, name string) string {
	t.Helper()
	body, err := os.ReadFile(filepath.Join(dir, name))
	if err != nil {
		t.Fatalf("reading %s: %v", name, err)
	}
	return string(body)
}

// TestCollect_deterministicSnapshotAndStorageCheck verifies Collect on a real
// database: two snapshots of unchanged data are byte-identical, the rosters
// and counts carry the seeded rows, a changed allow-listed column changes the
// table's digest, and the storage check reports exactly the original that is
// absent and the one with the wrong size.
func TestCollect_deterministicSnapshotAndStorageCheck(t *testing.T) {
	db := dbtest.New(t)
	ctx := t.Context()
	if _, err := db.Pool().Exec(ctx, `
		INSERT INTO users (uid, username, email, password_hash, role, approved_at)
		VALUES ('us_inv', 'inv', 'inv@example.test', 'x', 'user', now());
		INSERT INTO photos (uid, file_hash, file_path, file_size) VALUES
			('ph_inv_ok', 'h1', '2024/01/ok.jpg', 3),
			('ph_inv_gone', 'h2', '2024/01/gone.jpg', 5),
			('ph_inv_short', 'h3', '2024/01/short.jpg', 9)`); err != nil {
		t.Fatalf("seeding: %v", err)
	}
	root := t.TempDir()
	store, err := storage.NewFS(root)
	if err != nil {
		t.Fatalf("NewFS: %v", err)
	}
	for name, body := range map[string]string{"ok.jpg": "abc", "short.jpg": "abcd"} {
		if err := os.MkdirAll(filepath.Join(root, "2024", "01"), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, "2024", "01", name), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	first, second := t.TempDir(), t.TempDir()
	if err := inventory.Collect(ctx, db.Pool(), first, store); err != nil {
		t.Fatalf("Collect: %v", err)
	}
	if err := inventory.Collect(ctx, db.Pool(), second, nil); err != nil {
		t.Fatalf("Collect (again): %v", err)
	}
	for _, name := range []string{inventory.CountsFile, inventory.ColumnsFile, inventory.DigestsFile,
		inventory.PhotosFile, inventory.UsersFile} {
		if a, b := readFile(t, first, name), readFile(t, second, name); a != b {
			t.Errorf("%s differs between two snapshots of unchanged data", name)
		}
	}
	if got := readFile(t, first, inventory.UsersFile); !strings.Contains(got, "us_inv\tinv\tuser\tfalse\n") {
		t.Errorf("users.tsv = %q, want the seeded account", got)
	}
	if got := readFile(t, first, inventory.CountsFile); !strings.Contains(got, "photos\t3\n") {
		t.Errorf("counts.tsv = %q, want 3 photos", got)
	}
	wantMissing := "2024/01/gone.jpg\tph_inv_gone\t5\tabsent\n2024/01/short.jpg\tph_inv_short\t9\t4\n"
	if got := readFile(t, first, inventory.StorageMissingFile); got != wantMissing {
		t.Errorf("storage-missing.tsv = %q, want %q", got, wantMissing)
	}

	if _, err := db.Pool().Exec(ctx, `UPDATE photos SET title = 'changed' WHERE uid = 'ph_inv_ok'`); err != nil {
		t.Fatalf("updating: %v", err)
	}
	third := t.TempDir()
	if err := inventory.Collect(ctx, db.Pool(), third, nil); err != nil {
		t.Fatalf("Collect (after change): %v", err)
	}
	a, _ := inventory.Load(second)
	b, _ := inventory.Load(third)
	diffs := inventory.Diff(a, b)
	if len(diffs) != 1 || diffs[0].Key != "digest:photos" {
		t.Errorf("Diff after a title change = %+v, want only digest:photos", diffs)
	}
}
