package backup

import (
	"context"
	"errors"
	"io"
	"maps"
	"reflect"
	"sort"
	"testing"
)

// fakeRestorer records the archive it was handed and returns a configurable
// error, modelling pg_restore without a live database.
type fakeRestorer struct {
	got        []byte
	calls      int
	restoreErr error
}

// Restore reads the archive to completion (proving the dump streams) and records
// the bytes, returning the configured error.
func (r *fakeRestorer) Restore(_ context.Context, archive io.Reader) error {
	r.calls++
	if r.restoreErr != nil {
		return r.restoreErr
	}
	data, err := io.ReadAll(archive)
	if err != nil {
		return err
	}
	r.got = data
	return nil
}

// fakeLocalOriginals is an in-memory LocalOriginals for restore tests.
type fakeLocalOriginals struct {
	files    map[string][]byte
	listErr  error
	statErr  error
	writeErr error
}

// newFakeLocalOriginals returns an empty destination seeded with the given files.
func newFakeLocalOriginals(seed map[string][]byte) *fakeLocalOriginals {
	files := make(map[string][]byte, len(seed))
	maps.Copy(files, seed)
	return &fakeLocalOriginals{files: files}
}

// List returns each seeded file as a LocalOriginal.
func (o *fakeLocalOriginals) List(_ context.Context) ([]LocalOriginal, error) {
	if o.listErr != nil {
		return nil, o.listErr
	}
	var originals []LocalOriginal
	for key, data := range o.files {
		originals = append(originals, LocalOriginal{Key: key, Size: int64(len(data))})
	}
	sort.Slice(originals, func(i, j int) bool { return originals[i].Key < originals[j].Key })
	return originals, nil
}

// Stat reports whether the seeded file exists and its size.
func (o *fakeLocalOriginals) Stat(_ context.Context, key string) (LocalOriginal, bool, error) {
	if o.statErr != nil {
		return LocalOriginal{}, false, o.statErr
	}
	data, ok := o.files[key]
	if !ok {
		return LocalOriginal{}, false, nil
	}
	return LocalOriginal{Key: key, Size: int64(len(data))}, true, nil
}

// Write stores the reader's bytes under key.
func (o *fakeLocalOriginals) Write(_ context.Context, key string, reader io.Reader) error {
	if o.writeErr != nil {
		return o.writeErr
	}
	data, err := io.ReadAll(reader)
	if err != nil {
		return err
	}
	o.files[key] = data
	return nil
}

// fakePhotoCatalog is an in-memory PhotoCatalog for verify tests.
type fakePhotoCatalog struct {
	count    int
	paths    []string
	countErr error
	pathsErr error
}

// CountPhotos returns the configured photo count.
func (c *fakePhotoCatalog) CountPhotos(_ context.Context) (int, error) {
	return c.count, c.countErr
}

// ListFilePaths returns the configured catalogued file keys.
func (c *fakePhotoCatalog) ListFilePaths(_ context.Context) ([]string, error) {
	return c.paths, c.pathsErr
}

func TestRestoreService_ListDumps(t *testing.T) {
	t.Parallel()
	store := newFakeStore(map[string][]byte{
		"db/kukatko-20260101T000000Z.dump": []byte("d1"),
		"db/kukatko-20260103T000000Z.dump": []byte("d3"),
		"db/kukatko-20260102T000000Z.dump": []byte("d2"),
		"2026/01/a.jpg":                    []byte("orig"),
	})
	svc := NewRestoreService(RestoreConfig{Objects: store})

	dumps, err := svc.ListDumps(context.Background())
	if err != nil {
		t.Fatalf("ListDumps() error = %v", err)
	}
	want := []string{
		"db/kukatko-20260103T000000Z.dump",
		"db/kukatko-20260102T000000Z.dump",
		"db/kukatko-20260101T000000Z.dump",
	}
	got := make([]string, len(dumps))
	for i, d := range dumps {
		got[i] = d.Key
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("ListDumps() = %v, want newest-first %v (originals excluded)", got, want)
	}
}

func TestRestoreService_RestoreDatabase_latest(t *testing.T) {
	t.Parallel()
	store := newFakeStore(map[string][]byte{
		"db/kukatko-20260101T000000Z.dump": []byte("old-archive"),
		"db/kukatko-20260102T000000Z.dump": []byte("new-archive"),
	})
	restorer := &fakeRestorer{}
	svc := NewRestoreService(RestoreConfig{Objects: store, Restorer: restorer})

	key, err := svc.RestoreDatabase(context.Background(), "")
	if err != nil {
		t.Fatalf("RestoreDatabase() error = %v", err)
	}
	if key != "db/kukatko-20260102T000000Z.dump" {
		t.Errorf("restored key = %q, want the latest dump", key)
	}
	if string(restorer.got) != "new-archive" {
		t.Errorf("restorer received %q, want streamed latest archive", restorer.got)
	}
}

func TestRestoreService_RestoreDatabase_specificKey(t *testing.T) {
	t.Parallel()
	store := newFakeStore(map[string][]byte{
		"db/kukatko-20260101T000000Z.dump": []byte("first"),
		"db/kukatko-20260102T000000Z.dump": []byte("second"),
	})
	restorer := &fakeRestorer{}
	svc := NewRestoreService(RestoreConfig{Objects: store, Restorer: restorer})

	if _, err := svc.RestoreDatabase(context.Background(), "db/kukatko-20260101T000000Z.dump"); err != nil {
		t.Fatalf("RestoreDatabase() error = %v", err)
	}
	if string(restorer.got) != "first" {
		t.Errorf("restorer received %q, want the chosen dump", restorer.got)
	}
}

func TestRestoreService_RestoreDatabase_errors(t *testing.T) {
	t.Parallel()
	t.Run("no dumps", func(t *testing.T) {
		t.Parallel()
		svc := NewRestoreService(RestoreConfig{Objects: newFakeStore(nil), Restorer: &fakeRestorer{}})
		if _, err := svc.RestoreDatabase(context.Background(), ""); !errors.Is(err, ErrNoDumps) {
			t.Errorf("RestoreDatabase() error = %v, want ErrNoDumps", err)
		}
	})
	t.Run("unknown key", func(t *testing.T) {
		t.Parallel()
		store := newFakeStore(map[string][]byte{"db/kukatko-20260101T000000Z.dump": []byte("d")})
		svc := NewRestoreService(RestoreConfig{Objects: store, Restorer: &fakeRestorer{}})
		if _, err := svc.RestoreDatabase(context.Background(), "db/missing.dump"); !errors.Is(err, ErrDumpNotFound) {
			t.Errorf("RestoreDatabase() error = %v, want ErrDumpNotFound", err)
		}
	})
	t.Run("restore failure", func(t *testing.T) {
		t.Parallel()
		store := newFakeStore(map[string][]byte{"db/kukatko-20260101T000000Z.dump": []byte("d")})
		restorer := &fakeRestorer{restoreErr: errors.New("pg_restore exploded")}
		svc := NewRestoreService(RestoreConfig{Objects: store, Restorer: restorer})
		if _, err := svc.RestoreDatabase(context.Background(), ""); err == nil {
			t.Error("RestoreDatabase() error = nil, want restore failure")
		}
	})
}

func TestRestoreService_RestoreOriginals_skipExisting(t *testing.T) {
	t.Parallel()
	// present.jpg already on disk at the same size -> skipped; changed.jpg present
	// but a different size -> re-downloaded; new.jpg absent -> downloaded; the dump
	// under db/ must never be downloaded as an original.
	store := newFakeStore(map[string][]byte{
		"2026/01/present.jpg":              []byte("same-bytes"),
		"2026/01/changed.jpg":              []byte("brand-new-longer"),
		"2026/02/new.jpg":                  []byte("fresh"),
		"db/kukatko-20260101T000000Z.dump": []byte("a-dump"),
	})
	dest := newFakeLocalOriginals(map[string][]byte{
		"2026/01/present.jpg": []byte("same-bytes"),
		"2026/01/changed.jpg": []byte("old"),
	})
	svc := NewRestoreService(RestoreConfig{Objects: store, Originals: dest})

	res, err := svc.RestoreOriginals(context.Background())
	if err != nil {
		t.Fatalf("RestoreOriginals() error = %v", err)
	}
	if res.Downloaded != 2 || res.Skipped != 1 {
		t.Errorf("downloaded=%d skipped=%d, want downloaded=2 skipped=1", res.Downloaded, res.Skipped)
	}
	if string(dest.files["2026/01/changed.jpg"]) != "brand-new-longer" {
		t.Errorf("changed.jpg = %q, want re-downloaded content", dest.files["2026/01/changed.jpg"])
	}
	if string(dest.files["2026/02/new.jpg"]) != "fresh" {
		t.Errorf("new.jpg = %q, want downloaded", dest.files["2026/02/new.jpg"])
	}
	if _, ok := dest.files["db/kukatko-20260101T000000Z.dump"]; ok {
		t.Error("a database dump was downloaded as an original; dumps must be skipped")
	}
}

func TestRestoreService_Verify(t *testing.T) {
	t.Parallel()
	catalog := &fakePhotoCatalog{
		count: 3,
		paths: []string{"2026/01/a.jpg", "2026/01/b.jpg", "2026/02/c.jpg"},
	}
	// On disk: a.jpg present, b.jpg missing, c.jpg present, stray.jpg extra.
	dest := newFakeLocalOriginals(map[string][]byte{
		"2026/01/a.jpg":     []byte("a"),
		"2026/02/c.jpg":     []byte("c"),
		"2026/02/stray.jpg": []byte("x"),
	})
	svc := NewRestoreService(RestoreConfig{Objects: newFakeStore(nil), Photos: catalog, Originals: dest})

	report, err := svc.Verify(context.Background())
	if err != nil {
		t.Fatalf("Verify() error = %v", err)
	}
	if report.PhotosInDB != 3 || report.FilesInDB != 3 || report.OriginalsOnDisk != 3 {
		t.Errorf("report counts = %+v, want photos=3 files=3 disk=3", report)
	}
	if !reflect.DeepEqual(report.MissingOnDisk, []string{"2026/01/b.jpg"}) {
		t.Errorf("MissingOnDisk = %v, want [2026/01/b.jpg]", report.MissingOnDisk)
	}
	if !reflect.DeepEqual(report.ExtraOnDisk, []string{"2026/02/stray.jpg"}) {
		t.Errorf("ExtraOnDisk = %v, want [2026/02/stray.jpg]", report.ExtraOnDisk)
	}
	if report.Consistent {
		t.Error("Consistent = true despite mismatches")
	}
}

func TestReconcile(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name        string
		dbPaths     []string
		diskKeys    []string
		wantMissing []string
		wantExtra   []string
	}{
		{
			name:     "all match",
			dbPaths:  []string{"a", "b"},
			diskKeys: []string{"b", "a"},
		},
		{
			name:        "missing on disk",
			dbPaths:     []string{"a", "b", "c"},
			diskKeys:    []string{"a"},
			wantMissing: []string{"b", "c"},
		},
		{
			name:      "extra on disk",
			dbPaths:   []string{"a"},
			diskKeys:  []string{"a", "z", "y"},
			wantExtra: []string{"y", "z"},
		},
		{
			name:        "both",
			dbPaths:     []string{"a", "b"},
			diskKeys:    []string{"a", "c"},
			wantMissing: []string{"b"},
			wantExtra:   []string{"c"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			missing, extra := reconcile(tt.dbPaths, tt.diskKeys)
			if !reflect.DeepEqual(missing, tt.wantMissing) {
				t.Errorf("missing = %v, want %v", missing, tt.wantMissing)
			}
			if !reflect.DeepEqual(extra, tt.wantExtra) {
				t.Errorf("extra = %v, want %v", extra, tt.wantExtra)
			}
		})
	}
}

func TestNewRestoreService_panicsOnNilObjects(t *testing.T) {
	t.Parallel()
	defer func() {
		if recover() == nil {
			t.Error("NewRestoreService() with nil Objects did not panic")
		}
	}()
	NewRestoreService(RestoreConfig{})
}
