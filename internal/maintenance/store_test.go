package maintenance

import (
	"context"
	"errors"
	"reflect"
	"slices"
	"strings"
	"testing"
)

// fakeLister is a KeyLister over a fixed key list, or one that fails the way an
// unreachable bucket does.
type fakeLister struct {
	keys []string
	err  error
}

func (f fakeLister) Keys(_ context.Context, yield func(key string) error) error {
	if f.err != nil {
		return f.err
	}
	for _, key := range f.keys {
		if err := yield(key); err != nil {
			return err
		}
	}
	return nil
}

// storeKeys is what either backend holds for a two-photo library: the originals
// under YYYY/MM, the metadata sidecar beside each, the published thumbnails (only
// an object store holds these), and an object something else put in the bucket.
var storeKeys = []string{
	"2024/05/one.jpg",
	"2024/05/two.jpg",
	"sidecars/2024/05/one.jpg.yaml",
	"thumb/ab/cd/ef/abcdef_tile_224.jpg",
	"exports/report.pdf",
	"stray.jpg",
	"2024/05/",
}

// TestStoreOriginals_listsOnlyOriginals verifies the scanner yields the originals
// and nothing else the store holds, on both backends — a sidecar, a published
// thumbnail, a foreign object and a bare directory marker are all not originals,
// and an orphan-import repair must never be offered any of them.
func TestStoreOriginals_listsOnlyOriginals(t *testing.T) {
	t.Parallel()

	for _, kind := range []StoreKind{StoreDisk, StoreObject} {
		t.Run(string(kind), func(t *testing.T) {
			t.Parallel()
			scanner := NewStoreOriginals(kind, fakeLister{keys: storeKeys})
			var got []string
			if err := scanner.ListOriginals(context.Background(), func(key string) error {
				got = append(got, key)
				return nil
			}); err != nil {
				t.Fatalf("ListOriginals: %v", err)
			}
			want := []string{"2024/05/one.jpg", "2024/05/two.jpg"}
			if !reflect.DeepEqual(got, want) {
				t.Errorf("ListOriginals yielded %v, want %v", got, want)
			}
			if scanner.Kind() != kind {
				t.Errorf("Kind() = %q, want %q", scanner.Kind(), kind)
			}
		})
	}
}

// TestStoreOriginals_emptyStore verifies an empty store is an empty listing and
// not an error: a fresh instance has nothing in it yet.
func TestStoreOriginals_emptyStore(t *testing.T) {
	t.Parallel()

	scanner := NewStoreOriginals(StoreObject, fakeLister{})
	count := 0
	if err := scanner.ListOriginals(context.Background(), func(string) error {
		count++
		return nil
	}); err != nil {
		t.Fatalf("ListOriginals: %v", err)
	}
	if count != 0 {
		t.Errorf("yielded %d keys, want 0", count)
	}
}

// TestStoreOriginals_listingError verifies a backend failure reaches the caller
// unwrapped, so a scan can tell "the store is empty" from "the store could not be
// read".
func TestStoreOriginals_listingError(t *testing.T) {
	t.Parallel()

	boom := errors.New("listing bucket kukatko: connection refused")
	scanner := NewStoreOriginals(StoreObject, fakeLister{keys: storeKeys, err: boom})
	err := scanner.ListOriginals(context.Background(), func(string) error { return nil })
	if !errors.Is(err, boom) {
		t.Errorf("ListOriginals error = %v, want %v", err, boom)
	}
}

// TestStoreOriginals_yieldErrorStops verifies the caller's own sentinel ends the
// walk and comes back untouched, which is what lets a caller stop early.
func TestStoreOriginals_yieldErrorStops(t *testing.T) {
	t.Parallel()

	stop := errors.New("enough")
	scanner := NewStoreOriginals(StoreDisk, fakeLister{keys: storeKeys})
	seen := 0
	err := scanner.ListOriginals(context.Background(), func(string) error {
		seen++
		return stop
	})
	if !errors.Is(err, stop) {
		t.Errorf("ListOriginals error = %v, want %v", err, stop)
	}
	if seen != 1 {
		t.Errorf("yielded %d keys before stopping, want 1", seen)
	}
}

// storeScenario builds a Service whose only interesting collaborator is the
// store: the catalogue holds the given file paths and nothing else drifts, so the
// assertions are about the inventory and the orphans alone.
func storeScenario(catalogued []string, store StoreScanner) *Service {
	return New(Config{
		Photos:    &fakePhotos{count: len(catalogued), filePaths: catalogued},
		Vectors:   &fakeVectors{},
		Originals: fakeOriginals{present: map[string]bool{}},
		Store:     store,
		Thumbs:    fakeThumbs{have: map[string]bool{}},
		Enqueuer:  &fakeEnqueuer{},
		Embed:     &fakeBackfiller{},
		Faces:     &fakeFaceBackfiller{},
		FaceCache: &fakeFaceCache{},
		Importer:  &fakeImporter{},
	})
}

// TestScan_storeInventory asserts the scan inventories whichever store the
// instance uses and says which one it read: an object-store library counts the
// bucket's originals instead of reporting the empty local root, and an empty
// store is reported as empty rather than as unread.
func TestScan_storeInventory(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		catalogued []string
		store      fakeStore
		wantKind   StoreKind
		wantCount  int
		wantOrphan []string
		wantClean  bool
	}{
		"object store in step with the catalogue": {
			catalogued: []string{"2024/05/one.jpg", "2024/05/two.jpg"},
			store:      fakeStore{kind: StoreObject, keys: []string{"2024/05/one.jpg", "2024/05/two.jpg"}},
			wantKind:   StoreObject,
			wantCount:  2,
			wantOrphan: []string{},
			wantClean:  true,
		},
		"empty object store": {
			store:      fakeStore{kind: StoreObject},
			wantKind:   StoreObject,
			wantCount:  0,
			wantOrphan: []string{},
			wantClean:  true,
		},
		"object store holding an orphan": {
			catalogued: []string{"2024/05/one.jpg"},
			store: fakeStore{
				kind: StoreObject,
				keys: []string{"2024/05/one.jpg", "2024/05/orphan.jpg"},
			},
			wantKind:   StoreObject,
			wantCount:  2,
			wantOrphan: []string{"2024/05/orphan.jpg"},
			wantClean:  false,
		},
		"local disk holding an orphan": {
			catalogued: []string{"2024/05/one.jpg"},
			store: fakeStore{
				kind: StoreDisk,
				keys: []string{"2024/05/one.jpg", "2024/05/orphan.jpg"},
			},
			wantKind:   StoreDisk,
			wantCount:  2,
			wantOrphan: []string{"2024/05/orphan.jpg"},
			wantClean:  false,
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			report, err := storeScenario(tt.catalogued, tt.store).Scan(context.Background())
			if err != nil {
				t.Fatalf("Scan: %v", err)
			}
			if report.Store.Kind != tt.wantKind || report.Store.Originals != tt.wantCount {
				t.Errorf("store inventory = %+v, want kind %q with %d originals",
					report.Store, tt.wantKind, tt.wantCount)
			}
			if !report.Store.Listed() {
				t.Errorf("store inventory = %+v, want a listed one", report.Store)
			}
			if !slices.Equal(report.OrphanFiles.Samples, tt.wantOrphan) {
				t.Errorf("orphans = %v, want %v", report.OrphanFiles.Samples, tt.wantOrphan)
			}
			if report.Clean() != tt.wantClean {
				t.Errorf("Clean() = %v, want %v", report.Clean(), tt.wantClean)
			}
		})
	}
}

// TestScan_storeListingFailure asserts a store that cannot be listed is reported
// as a failure rather than as zero originals: the rest of the scan still comes
// back, the inventory carries the reason, and the report is not clean — a green
// "nothing to repair" over an unread store is the bug this guards.
func TestScan_storeListingFailure(t *testing.T) {
	t.Parallel()

	store := fakeStore{kind: StoreObject, err: errors.New("connection refused")}
	report, err := storeScenario([]string{"2024/05/one.jpg"}, store).Scan(context.Background())
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if report.Store.Listed() || report.Store.Originals != 0 {
		t.Errorf("store inventory = %+v, want an unlisted one with no count", report.Store)
	}
	if report.Store.Kind != StoreObject {
		t.Errorf("store kind = %q, want %q", report.Store.Kind, StoreObject)
	}
	if !strings.Contains(report.Store.Error, "connection refused") {
		t.Errorf("store error = %q, want it to carry the backend's reason", report.Store.Error)
	}
	if report.Clean() {
		t.Error("a scan whose store could not be listed must not report itself clean")
	}
	// The catalogue half still ran: the photo's original is absent from the
	// presence check, and that finding is worth having even with no inventory.
	if report.FilesInDB != 1 {
		t.Errorf("FilesInDB = %d, want 1", report.FilesInDB)
	}
}

// TestRepairOrphans_storeListingFails asserts the orphan import fails loudly when
// the store cannot be listed, rather than importing nothing and reporting
// success.
func TestRepairOrphans_storeListingFails(t *testing.T) {
	t.Parallel()

	boom := errors.New("connection refused")
	svc := storeScenario(nil, fakeStore{kind: StoreObject, err: boom})
	if _, err := svc.Repair(context.Background(), RepairOptions{ImportOrphans: true}); !errors.Is(err, boom) {
		t.Errorf("Repair error = %v, want it to wrap %v", err, boom)
	}
}
