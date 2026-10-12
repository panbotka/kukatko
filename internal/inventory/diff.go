package inventory

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
)

// RoleCollapse is the named expectation that allows the designed collapse of
// users.role to the instance role: an account whose only change is its role
// going from viewer, curator, editor or admin to "user". Any other role change
// stays unexpected.
const RoleCollapse = "role-collapse"

// ErrUnexpected is returned by Result.Err when a diff holds a difference no
// expectation allows.
var ErrUnexpected = errors.New("inventory: unexpected differences")

// errBadExpectation reports an --expect value that is not a valid pattern.
var errBadExpectation = errors.New("inventory: invalid expectation pattern")

// Snapshot is one inventory directory loaded into memory, keyed for comparison.
type Snapshot struct {
	counts  map[string]string
	columns map[string]string
	digests map[string]string
	photos  map[string]string
	users   map[string][]string
	// storage is nil when the snapshot was taken without a storage check.
	storage map[string]string
}

// Load reads the snapshot in dir. Every file Collect always writes must be
// present; the storage file is optional (a snapshot taken without --storage).
func Load(dir string) (Snapshot, error) {
	var snap Snapshot
	var err error
	if snap.counts, err = loadKeyed(dir, CountsFile, 1); err != nil {
		return Snapshot{}, err
	}
	if snap.columns, err = loadKeyed(dir, ColumnsFile, 2); err != nil {
		return Snapshot{}, err
	}
	if snap.digests, err = loadKeyed(dir, DigestsFile, 1); err != nil {
		return Snapshot{}, err
	}
	if snap.photos, err = loadKeyed(dir, PhotosFile, 1); err != nil {
		return Snapshot{}, err
	}
	users, err := loadKeyed(dir, UsersFile, 1)
	if err != nil {
		return Snapshot{}, err
	}
	snap.users = make(map[string][]string, len(users))
	for uid, rest := range users {
		snap.users[uid] = strings.Split(rest, "\t")
	}
	snap.storage, err = loadKeyed(dir, StorageMissingFile, 1)
	if errors.Is(err, os.ErrNotExist) {
		return snap, nil
	}
	return snap, err
}

// loadKeyed reads a TSV file into a map from its first keyFields fields (joined
// with ".") to the rest of the line.
func loadKeyed(dir, name string, keyFields int) (map[string]string, error) {
	body, err := os.ReadFile(filepath.Join(dir, name)) //nolint:gosec // G304: the operator names the snapshot dir
	if err != nil {
		return nil, fmt.Errorf("inventory: reading %s: %w", name, err)
	}
	out := map[string]string{}
	for line := range strings.SplitSeq(strings.TrimSuffix(string(body), "\n"), "\n") {
		if line == "" {
			continue
		}
		fields := strings.Split(line, "\t")
		if len(fields) < keyFields {
			return nil, fmt.Errorf("inventory: %s: short line %q", name, line)
		}
		out[strings.Join(fields[:keyFields], ".")] = strings.Join(fields[keyFields:], "\t")
	}
	return out, nil
}

// Difference is one way snapshot B differs from snapshot A. Key names the kind
// and the object ("count:photos", "column-added:photos.project_id",
// "user-role:us123", …) and is what an expectation pattern matches.
type Difference struct {
	Key    string
	Detail string
	// roleFrom and roleTo are set on a user-role difference only.
	roleFrom, roleTo string
}

// Diff compares two snapshots and returns every difference, sorted by key.
func Diff(a, b Snapshot) []Difference {
	out := diffCounts(a, b)
	out = append(out, diffColumns(a, b)...)
	out = append(out, diffMaps("digest", a.digests, b.digests)...)
	out = append(out, diffMaps("photo", a.photos, b.photos)...)
	out = append(out, diffUsers(a.users, b.users)...)
	out = append(out, diffStorage(a.storage, b.storage)...)
	slices.SortFunc(out, func(x, y Difference) int { return strings.Compare(x.Key, y.Key) })
	return out
}

// diffCounts reports added and removed tables and changed row counts.
func diffCounts(a, b Snapshot) []Difference {
	var out []Difference
	for table, before := range a.counts {
		after, ok := b.counts[table]
		switch {
		case !ok:
			out = append(out, Difference{Key: "table-removed:" + table, Detail: before + " rows"})
		case after != before:
			out = append(out, Difference{Key: "count:" + table, Detail: before + " → " + after})
		}
	}
	for table, after := range b.counts {
		if _, ok := a.counts[table]; !ok {
			out = append(out, Difference{Key: "table-added:" + table, Detail: after + " rows"})
		}
	}
	return out
}

// diffColumns reports added, removed and retyped columns of the tables both
// snapshots have; the columns of an added or removed table are its table's
// difference already.
func diffColumns(a, b Snapshot) []Difference {
	var out []Difference
	for col, before := range a.columns {
		after, ok := b.columns[col]
		switch {
		case !ok && hasTable(b, col):
			out = append(out, Difference{Key: "column-removed:" + col, Detail: before})
		case ok && after != before:
			out = append(out, Difference{Key: "column-type:" + col, Detail: before + " → " + after})
		}
	}
	for col, after := range b.columns {
		if _, ok := a.columns[col]; !ok && hasTable(a, col) {
			out = append(out, Difference{Key: "column-added:" + col, Detail: after})
		}
	}
	return out
}

// hasTable reports whether snap has the table a "table.column" key names.
func hasTable(snap Snapshot, column string) bool {
	table, _, _ := strings.Cut(column, ".")
	_, ok := snap.counts[table]
	return ok
}

// diffMaps reports added, removed and changed entries of two keyed files under
// kind ("digest:photos", "photo-added:ph123", …).
func diffMaps(kind string, a, b map[string]string) []Difference {
	var out []Difference
	for key, before := range a {
		after, ok := b[key]
		switch {
		case !ok:
			out = append(out, Difference{Key: kind + "-removed:" + key, Detail: show(before)})
		case after != before:
			out = append(out, Difference{Key: kind + ":" + key, Detail: show(before) + " → " + show(after)})
		}
	}
	for key, after := range b {
		if _, ok := a[key]; !ok {
			out = append(out, Difference{Key: kind + "-added:" + key, Detail: show(after)})
		}
	}
	return out
}

// diffUsers reports account changes; a change of the role alone is reported
// as user-role so the role-collapse expectation can judge it.
func diffUsers(a, b map[string][]string) []Difference {
	flat := func(m map[string][]string) map[string]string {
		out := make(map[string]string, len(m))
		for uid, fields := range m {
			out[uid] = strings.Join(fields, "\t")
		}
		return out
	}
	var out []Difference
	for _, d := range diffMaps("user", flat(a), flat(b)) {
		uid, isChange := strings.CutPrefix(d.Key, "user:")
		if !isChange || !onlyRoleDiffers(a[uid], b[uid]) {
			out = append(out, d)
			continue
		}
		from, to := a[uid][1], b[uid][1]
		out = append(out, Difference{
			Key: "user-role:" + uid, Detail: a[uid][0] + ": " + from + " → " + to, roleFrom: from, roleTo: to,
		})
	}
	return out
}

// onlyRoleDiffers reports whether two username, role, disabled rows differ in
// the role field and nowhere else.
func onlyRoleDiffers(a, b []string) bool {
	if len(a) != 3 || len(b) != 3 {
		return false
	}
	return a[0] == b[0] && a[2] == b[2] && a[1] != b[1]
}

// diffStorage reports originals missing in B that were not already missing in
// A. A snapshot without a storage check contributes nothing.
func diffStorage(a, b map[string]string) []Difference {
	var out []Difference
	for path, detail := range b {
		if _, known := a[path]; !known {
			out = append(out, Difference{Key: "storage-missing:" + path, Detail: show(detail)})
		}
	}
	return out
}

// show renders a TSV remainder for a human: tabs become " | ".
func show(s string) string { return strings.ReplaceAll(s, "\t", " | ") }

// Expectations is the parsed --expect list: glob patterns over difference
// keys (path.Match syntax) plus the named role-collapse rule.
type Expectations struct {
	patterns     []string
	roleCollapse bool
}

// ParseExpectations validates the --expect values. Each is either RoleCollapse
// or a path.Match pattern over a difference key, e.g. "table-added:projects" or
// "column-added:*.project_id".
func ParseExpectations(values []string) (Expectations, error) {
	var exp Expectations
	for _, v := range values {
		if v == RoleCollapse {
			exp.roleCollapse = true
			continue
		}
		if _, err := path.Match(v, ""); err != nil {
			return Expectations{}, fmt.Errorf("%w: %q: %w", errBadExpectation, v, err)
		}
		exp.patterns = append(exp.patterns, v)
	}
	return exp, nil
}

// allows returns the expectation that allows d, or "" when none does.
func (e Expectations) allows(d Difference) string {
	if e.roleCollapse && d.roleTo == "user" && slices.Contains(collapsedRoles, d.roleFrom) {
		return RoleCollapse
	}
	for _, p := range e.patterns {
		if ok, _ := path.Match(p, d.Key); ok {
			return p
		}
	}
	return ""
}

// collapsedRoles are the old per-library roles migration 0103 turns into "user".
var collapsedRoles = []string{"viewer", "curator", "editor", "admin"}

// Judged is a difference together with the expectation that allows it ("" for
// an unexpected one).
type Judged struct {
	Difference
	AllowedBy string
}

// Result is a judged diff.
type Result struct {
	Differences []Judged
}

// Judge applies the expectations to every difference.
func Judge(diffs []Difference, exp Expectations) Result {
	out := Result{Differences: make([]Judged, 0, len(diffs))}
	for _, d := range diffs {
		out.Differences = append(out.Differences, Judged{Difference: d, AllowedBy: exp.allows(d)})
	}
	return out
}

// Unexpected counts the differences no expectation allows.
func (r Result) Unexpected() int {
	n := 0
	for _, d := range r.Differences {
		if d.AllowedBy == "" {
			n++
		}
	}
	return n
}

// Err returns ErrUnexpected when any difference is unexpected, nil otherwise.
func (r Result) Err() error {
	if n := r.Unexpected(); n > 0 {
		return fmt.Errorf("%w: %d", ErrUnexpected, n)
	}
	return nil
}

// Write prints the result for a human: unexpected differences first, then the
// expected ones with the expectation that allowed each, then a summary line.
func (r Result) Write(w io.Writer) error {
	var b strings.Builder
	for _, d := range r.Differences {
		if d.AllowedBy == "" {
			fmt.Fprintf(&b, "UNEXPECTED %s  %s\n", d.Key, d.Detail)
		}
	}
	for _, d := range r.Differences {
		if d.AllowedBy != "" {
			fmt.Fprintf(&b, "expected   %s  %s  [%s]\n", d.Key, d.Detail, d.AllowedBy)
		}
	}
	fmt.Fprintf(&b, "%d difference(s), %d unexpected\n", len(r.Differences), r.Unexpected())
	if _, err := io.WriteString(w, b.String()); err != nil {
		return fmt.Errorf("inventory: writing the diff: %w", err)
	}
	return nil
}
