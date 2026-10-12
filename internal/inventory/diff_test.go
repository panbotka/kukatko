package inventory

import (
	"bytes"
	"errors"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// baseFiles is a small snapshot as Collect would write it.
var baseFiles = map[string]string{
	CountsFile:  "photos\t2\nsessions\t1\nusers\t3\n",
	ColumnsFile: "photos\tuid\tvarchar\nsessions\trole\ttext\nusers\trole\ttext\nusers\tuid\tvarchar\n",
	DigestsFile: "photos\t2\taaa\t-\nusers\t3\tbbb\t-\n",
	PhotosFile:  "ph1\th1\t2024/01/a.jpg\t10\t\nph2\th2\t2024/01/b.jpg\t20\t\n",
	UsersFile:   "us1\tanna\tmaintainer\tfalse\nus2\tbob\tviewer\tfalse\nus3\tcyril\teditor\tfalse\n",
}

// writeSnapshot writes the base files overlaid with overrides into a fresh
// directory and returns it.
func writeSnapshot(t *testing.T, overrides map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	files := maps.Clone(baseFiles)
	maps.Copy(files, overrides)
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
			t.Fatalf("writing %s: %v", name, err)
		}
	}
	return dir
}

// diffKeys loads two snapshots and returns the keys of their differences.
func diffKeys(t *testing.T, a, b string) []Difference {
	t.Helper()
	snapA, err := Load(a)
	if err != nil {
		t.Fatalf("Load(A): %v", err)
	}
	snapB, err := Load(b)
	if err != nil {
		t.Fatalf("Load(B): %v", err)
	}
	return Diff(snapA, snapB)
}

// keysOf returns the keys of diffs.
func keysOf(diffs []Difference) []string {
	out := make([]string, 0, len(diffs))
	for _, d := range diffs {
		out = append(out, d.Key)
	}
	return out
}

// TestDiff_identicalSnapshotsHaveNoDifferences verifies a snapshot compared
// with itself is clean.
func TestDiff_identicalSnapshotsHaveNoDifferences(t *testing.T) {
	t.Parallel()
	dir := writeSnapshot(t, nil)
	if got := diffKeys(t, dir, dir); len(got) != 0 {
		t.Errorf("Diff() = %v, want none", keysOf(got))
	}
}

// TestDiff_reportsEveryKind verifies each kind of change gets its own key.
func TestDiff_reportsEveryKind(t *testing.T) {
	t.Parallel()
	a := writeSnapshot(t, nil)
	b := writeSnapshot(t, map[string]string{
		CountsFile: "photos\t3\nprojects\t1\nusers\t3\n",
		ColumnsFile: "photos\tproject_id\ttext\nphotos\tuid\ttext\nprojects\tuid\ttext\n" +
			"users\trole\ttext\nusers\tuid\tvarchar\n",
		DigestsFile: "photos\t3\tccc\t-\nusers\t3\tbbb\t-\n",
		PhotosFile:  "ph1\th1\t2024/01/a.jpg\t11\t\nph3\th3\t2024/01/c.jpg\t30\t\n",
		UsersFile:   "us1\tanna\tmaintainer\tfalse\nus2\tbob\tuser\tfalse\nus3\tcyril\tuser\ttrue\n",
	})
	want := []string{
		"column-added:photos.project_id",
		"column-type:photos.uid",
		"count:photos",
		"digest:photos",
		"photo-added:ph3",
		"photo-removed:ph2",
		"photo:ph1",
		"table-added:projects",
		"table-removed:sessions",
		"user-role:us2",
		"user:us3",
	}
	if got := keysOf(diffKeys(t, a, b)); !slices.Equal(got, want) {
		t.Errorf("Diff() keys =\n%v\nwant\n%v", got, want)
	}
}

// TestJudge_expectations verifies patterns and the role-collapse rule allow
// exactly what they name, and that anything else stays unexpected.
func TestJudge_expectations(t *testing.T) {
	t.Parallel()
	diffs := []Difference{
		{Key: "table-added:projects"},
		{Key: "column-added:photos.project_id"},
		{Key: "column-added:photos.secret"},
		{Key: "user-role:us2", roleFrom: "viewer", roleTo: "user"},
		{Key: "user-role:us4", roleFrom: "admin", roleTo: "user"},
		{Key: "user-role:us3", roleFrom: "user", roleTo: "maintainer"},
		{Key: "count:album_photos"},
	}
	exp, err := ParseExpectations([]string{"table-added:projects", "column-added:*.project_id", RoleCollapse})
	if err != nil {
		t.Fatalf("ParseExpectations: %v", err)
	}
	res := Judge(diffs, exp)
	want := map[string]string{
		"table-added:projects":           "table-added:projects",
		"column-added:photos.project_id": "column-added:*.project_id",
		"column-added:photos.secret":     "",
		"user-role:us2":                  RoleCollapse,
		"user-role:us4":                  RoleCollapse,
		"user-role:us3":                  "",
		"count:album_photos":             "",
	}
	for _, d := range res.Differences {
		if d.AllowedBy != want[d.Key] {
			t.Errorf("%s allowed by %q, want %q", d.Key, d.AllowedBy, want[d.Key])
		}
	}
	if got := res.Unexpected(); got != 3 {
		t.Errorf("Unexpected() = %d, want 3", got)
	}
	if err := res.Err(); !errors.Is(err, ErrUnexpected) {
		t.Errorf("Err() = %v, want ErrUnexpected", err)
	}
}

// TestJudge_allExpectedIsClean verifies a fully explained diff has no error.
func TestJudge_allExpectedIsClean(t *testing.T) {
	t.Parallel()
	exp, err := ParseExpectations([]string{"table-added:*"})
	if err != nil {
		t.Fatalf("ParseExpectations: %v", err)
	}
	res := Judge([]Difference{{Key: "table-added:projects", Detail: "1 rows"}}, exp)
	if err := res.Err(); err != nil {
		t.Errorf("Err() = %v, want nil", err)
	}
	var out bytes.Buffer
	if err := res.Write(&out); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if !strings.Contains(out.String(), "expected   table-added:projects  1 rows  [table-added:*]") ||
		!strings.Contains(out.String(), "1 difference(s), 0 unexpected") {
		t.Errorf("Write() = %q", out.String())
	}
}

// TestParseExpectations_rejectsBadPattern verifies a malformed glob is refused
// up front instead of silently allowing nothing.
func TestParseExpectations_rejectsBadPattern(t *testing.T) {
	t.Parallel()
	if _, err := ParseExpectations([]string{"count:["}); !errors.Is(err, errBadExpectation) {
		t.Errorf("ParseExpectations() = %v, want errBadExpectation", err)
	}
}

// TestDiff_storage verifies only originals newly missing in B are reported, and
// that a snapshot without a storage check contributes nothing.
func TestDiff_storage(t *testing.T) {
	t.Parallel()
	plain := writeSnapshot(t, nil)
	a := writeSnapshot(t, map[string]string{StorageMissingFile: "2024/01/a.jpg\tph1\t10\tabsent\n"})
	b := writeSnapshot(t, map[string]string{
		StorageMissingFile: "2024/01/a.jpg\tph1\t10\tabsent\n2024/01/b.jpg\tph2\t20\t19\n",
	})
	if got, want := keysOf(diffKeys(t, a, b)), []string{"storage-missing:2024/01/b.jpg"}; !slices.Equal(got, want) {
		t.Errorf("Diff(A, B) = %v, want %v", got, want)
	}
	if got := diffKeys(t, b, plain); len(got) != 0 {
		t.Errorf("Diff(B, no storage check) = %v, want none", keysOf(got))
	}
	if got := diffKeys(t, plain, a); len(got) != 1 {
		t.Errorf("Diff(no storage check, A) = %v, want the one missing original", keysOf(got))
	}
}

// TestLoad_missingFile verifies a snapshot lacking a required file is refused.
func TestLoad_missingFile(t *testing.T) {
	t.Parallel()
	dir := writeSnapshot(t, nil)
	if err := os.Remove(filepath.Join(dir, UsersFile)); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(dir); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("Load() = %v, want a not-exist error", err)
	}
}

// TestEscapeField verifies fields stay on one line and free of separators.
func TestEscapeField(t *testing.T) {
	t.Parallel()
	if got, want := escapeField("a\tb\nc\\d"), `a\tb\nc\\d`; got != want {
		t.Errorf("escapeField() = %q, want %q", got, want)
	}
}
