package reset

import (
	"context"
	"errors"
	"os"
	"slices"
	"sort"
	"strings"
	"sync"
	"testing"

	"github.com/panbotka/kukatko/internal/storage"
	"github.com/panbotka/kukatko/internal/thumb"
)

// fakeStore is an ObjectStore that records every key it was asked to delete and
// answers each with a configurable outcome.
type fakeStore struct {
	mu      sync.Mutex
	deleted []string
	// outcomes maps a key to the error Delete returns for it; a key absent from
	// the map is deleted successfully.
	outcomes map[string]error
}

// Delete records relPath and returns the outcome configured for it.
func (f *fakeStore) Delete(_ context.Context, relPath string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.deleted = append(f.deleted, relPath)
	return f.outcomes[relPath]
}

// keys returns the recorded keys, sorted so assertions do not depend on the
// order concurrent deletions happened to run in.
func (f *fakeStore) keys() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := slices.Clone(f.deleted)
	sort.Strings(out)
	return out
}

// fakeLister is a storage.KeyLister over a fixed key set.
type fakeLister struct {
	keys []string
	err  error
}

// Keys yields every configured key, or fails with the configured error.
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

// compile-time assertion that the fake satisfies the interface it stands in for.
var _ storage.KeyLister = fakeLister{}

// TestClassifyKey verifies each of Kukátko's three prefixes is recognised and
// everything else is classified as foreign — the predicate the whole
// "never delete outside our namespace" guarantee rests on.
func TestClassifyKey(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		key  string
		want keyKind
	}{
		{name: "original", key: "2024/05/IMG_1234.jpg", want: kindOriginal},
		{name: "original with leading slash", key: "/1999/12/scan.png", want: kindOriginal},
		{name: "thumbnail", key: "thumb/aa/bb/cc/aabbcc_tile_500.jpg", want: kindThumbnail},
		{name: "sidecar", key: "sidecars/2024/05/IMG_1234.jpg.yml", want: kindSidecar},
		{name: "empty", key: "", want: kindForeign},
		{name: "bucket root file", key: "README.md", want: kindForeign},
		{name: "another app's backup", key: "backups/db/2026-07-31.dump", want: kindForeign},
		{name: "photoprism-looking tree", key: "originals/2024/05/IMG_1234.jpg", want: kindForeign},
		{name: "year only", key: "2024/IMG_1234.jpg", want: kindForeign},
		{name: "deeper than YYYY/MM", key: "2024/05/sub/IMG_1234.jpg", want: kindForeign},
		{name: "month directory marker", key: "2024/05/", want: kindForeign},
		{name: "not a year", key: "20xx/05/IMG_1234.jpg", want: kindForeign},
		{name: "prefix lookalike", key: "thumbnails/aa.jpg", want: kindForeign},
		{name: "sidecar prefix lookalike", key: "sidecars.old/x.yml", want: kindForeign},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := classifyKey(tt.key); got != tt.want {
				t.Errorf("classifyKey(%q) = %v, want %v", tt.key, got, tt.want)
			}
		})
	}
}

// TestSweepKeys_confinedToOwnedPrefixes verifies a sweep returns only the keys
// under Kukátko's own prefixes, counts them per prefix, and reports the rest as
// foreign without offering them for deletion.
func TestSweepKeys_confinedToOwnedPrefixes(t *testing.T) {
	t.Parallel()

	lister := fakeLister{keys: []string{
		"2024/05/IMG_1.jpg",
		"2024/06/IMG_2.jpg",
		"thumb/aa/bb/cc/aabbcc_tile_500.jpg",
		"sidecars/2024/05/IMG_1.jpg.yml",
		"backups/db/2026-07-31.dump",
		"some-other-app/data.bin",
		"README.md",
	}}

	keys, counts, foreign, err := sweepKeys(t.Context(), lister)
	if err != nil {
		t.Fatalf("sweepKeys: %v", err)
	}
	want := []string{
		"2024/05/IMG_1.jpg",
		"2024/06/IMG_2.jpg",
		"sidecars/2024/05/IMG_1.jpg.yml",
		"thumb/aa/bb/cc/aabbcc_tile_500.jpg",
	}
	sort.Strings(keys)
	if !slices.Equal(keys, want) {
		t.Errorf("swept keys = %v, want %v", keys, want)
	}
	wantCounts := PrefixCounts{Originals: 2, Thumbnails: 1, Sidecars: 1}
	if counts != wantCounts {
		t.Errorf("counts = %+v, want %+v", counts, wantCounts)
	}
	if foreign != 3 {
		t.Errorf("foreign = %d, want 3", foreign)
	}
	if counts.Total() != 4 {
		t.Errorf("Total() = %d, want 4", counts.Total())
	}
}

// TestSweepKeys_listingError verifies a store that cannot be listed fails the
// sweep rather than reporting an empty one, which would read as "nothing to
// delete" against a store that is simply unreachable.
func TestSweepKeys_listingError(t *testing.T) {
	t.Parallel()

	sentinel := errors.New("bucket unreachable")
	if _, _, _, err := sweepKeys(t.Context(), fakeLister{err: sentinel}); !errors.Is(err, sentinel) {
		t.Errorf("sweepKeys error = %v, want %v", err, sentinel)
	}
}

// TestObjectKeys_expandsEveryArtifact verifies a catalogued file expands into its
// original, its sidecar and one thumbnail per registered size, and that the
// per-prefix counts match.
func TestObjectKeys_expandsEveryArtifact(t *testing.T) {
	t.Parallel()

	hash := strings.Repeat("ab", 32)
	files := catalogueFiles{paths: []string{"2024/05/IMG_1.jpg"}, hashes: []string{hash}}
	keys, counts := files.objectKeys()

	sizes := thumb.SizeNames()
	if counts.Originals != 1 || counts.Sidecars != 1 || counts.Thumbnails != len(sizes) {
		t.Errorf("counts = %+v, want 1 original, 1 sidecar, %d thumbnails", counts, len(sizes))
	}
	if len(keys) != counts.Total() {
		t.Errorf("len(keys) = %d, want %d", len(keys), counts.Total())
	}
	for _, want := range []string{"2024/05/IMG_1.jpg", "sidecars/2024/05/IMG_1.jpg.yml"} {
		if !slices.Contains(keys, want) {
			t.Errorf("keys %v do not contain %q", keys, want)
		}
	}
	for _, key := range keys {
		if classifyKey(key) == kindForeign {
			t.Errorf("expanded key %q classifies as foreign", key)
		}
	}
}

// TestObjectKeys_skipsUnusableRows verifies a row with no path yields no sidecar
// and a malformed hash yields no thumbnails, instead of failing the whole wipe
// over one corrupt row.
func TestObjectKeys_skipsUnusableRows(t *testing.T) {
	t.Parallel()

	files := catalogueFiles{paths: []string{"", "2024/05/IMG_1.jpg"}, hashes: []string{"xy", ""}}
	keys, counts := files.objectKeys()

	if counts.Thumbnails != 0 {
		t.Errorf("thumbnails = %d, want 0 for malformed hashes", counts.Thumbnails)
	}
	if counts.Sidecars != 1 {
		t.Errorf("sidecars = %d, want 1 (only the usable path has one)", counts.Sidecars)
	}
	if counts.Originals != 2 {
		t.Errorf("originals = %d, want 2 (both rows are still attempted)", counts.Originals)
	}
	if len(keys) != 3 {
		t.Errorf("len(keys) = %d, want 3", len(keys))
	}
}

// TestDeleteKeys_classifiesOutcomes verifies each per-object outcome lands in the
// right counter and that a bounded sample of failures is kept.
func TestDeleteKeys_classifiesOutcomes(t *testing.T) {
	t.Parallel()

	store := &fakeStore{outcomes: map[string]error{
		"2024/05/gone.jpg":    os.ErrNotExist,
		"2024/05/broken.jpg":  storage.ErrInvalidPath,
		"2024/05/refused.jpg": errors.New("access denied"),
	}}
	keys := []string{
		"2024/05/ok.jpg", "2024/05/gone.jpg", "2024/05/broken.jpg", "2024/05/refused.jpg",
	}

	result, err := deleteKeys(t.Context(), store, keys, 2)
	if err != nil {
		t.Fatalf("deleteKeys: %v", err)
	}
	if result.Deleted != 1 || result.Missing != 1 || result.Skipped != 1 || result.Failed != 1 {
		t.Errorf("result = %+v, want one of each outcome", result)
	}
	if len(result.Failures) != 1 || !strings.Contains(result.Failures[0], "refused.jpg") {
		t.Errorf("failures = %v, want one naming refused.jpg", result.Failures)
	}
	if got := len(store.keys()); got != len(keys) {
		t.Errorf("deleted %d key(s), want %d", got, len(keys))
	}
}

// TestDeleteKeys_boundsTheFailureSample verifies the failure count stays exact
// while the retained sample is capped, so a wipe against a broken store reports
// the scale of the problem without printing every instance of it.
func TestDeleteKeys_boundsTheFailureSample(t *testing.T) {
	t.Parallel()

	const total = failureSampleLimit + 7
	store := &fakeStore{outcomes: map[string]error{}}
	keys := make([]string, 0, total)
	for i := range total {
		key := "2024/05/" + strings.Repeat("x", i+1) + ".jpg"
		keys = append(keys, key)
		store.outcomes[key] = errors.New("nope")
	}

	result, err := deleteKeys(t.Context(), store, keys, 4)
	if err != nil {
		t.Fatalf("deleteKeys: %v", err)
	}
	if result.Failed != total {
		t.Errorf("Failed = %d, want %d", result.Failed, total)
	}
	if len(result.Failures) != failureSampleLimit {
		t.Errorf("len(Failures) = %d, want %d", len(result.Failures), failureSampleLimit)
	}
}

// TestDeleteKeys_cancelled verifies a cancelled context stops the deletion with
// an error rather than marching through the rest of the library.
func TestDeleteKeys_cancelled(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	store := &fakeStore{outcomes: map[string]error{}}
	if _, err := deleteKeys(ctx, store, []string{"2024/05/a.jpg"}, 2); !errors.Is(err, context.Canceled) {
		t.Errorf("deleteKeys error = %v, want context.Canceled", err)
	}
}

// TestPrefixCounts_with verifies each kind increments its own counter and a
// foreign key increments none.
func TestPrefixCounts_with(t *testing.T) {
	t.Parallel()

	var counts PrefixCounts
	for _, kind := range []keyKind{kindOriginal, kindOriginal, kindThumbnail, kindSidecar, kindForeign} {
		counts = counts.with(kind)
	}
	want := PrefixCounts{Originals: 2, Thumbnails: 1, Sidecars: 1}
	if counts != want {
		t.Errorf("counts = %+v, want %+v", counts, want)
	}
	if counts.Total() != 4 {
		t.Errorf("Total() = %d, want 4", counts.Total())
	}
}
