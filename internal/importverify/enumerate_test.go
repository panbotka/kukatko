package importverify_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/panbotka/kukatko/internal/importverify"
	"github.com/panbotka/kukatko/internal/photoprism"
)

// The merged-listing fixture below is sized around photoprism.MaxCount, the count
// the enumerator requests: the library must be wider than one page of FILE rows,
// or a merged short page would also be the last one and would prove nothing.
const (
	// libraryPhotos is how many photos the fake source holds.
	libraryPhotos = 1200
	// multiFilePrefix is how many of the leading photos hold two files. They push
	// the first page's file rows well past its photo count, so the page comes back
	// short of photoprism.MaxCount while the library is far from exhausted.
	multiFilePrefix = 150
	// straddlingPhoto is the index of a two-file photo whose rows fall either side
	// of the first page's boundary, so page one serves it with half its files and
	// page two with all of them.
	straddlingPhoto = 849
)

// mergedLibrary builds a PhotoPrism library whose merged listing pages short:
// multiFilePrefix leading photos and straddlingPhoto hold two files each, every
// other photo one.
func mergedLibrary() []photoprism.Photo {
	out := make([]photoprism.Photo, 0, libraryPhotos)
	for i := range libraryPhotos {
		uid := fmt.Sprintf("pp%04d", i)
		files := 1
		if i < multiFilePrefix || i == straddlingPhoto {
			files = 2
		}
		out = append(out, photo(uid, "image", "h-"+uid, files))
	}
	return out
}

// importedLibrary returns the catalogue sets for a library imported whole: every
// uid present, and one original file per photo except the multi-file prefix,
// whose second file was imported too. The straddling photo is deliberately left
// with a single original so a real file gap is there to be found.
func importedLibrary() (map[string]struct{}, map[string]int) {
	uids := make(map[string]struct{}, libraryPhotos)
	counts := make(map[string]int, libraryPhotos)
	for i := range libraryPhotos {
		uid := fmt.Sprintf("pp%04d", i)
		uids[uid] = struct{}{}
		counts[uid] = 1
		if i < multiFilePrefix {
			counts[uid] = 2
		}
	}
	return uids, counts
}

// TestService_Verify_enumeratesPastShortMergedPages pins that source enumeration
// pages the WHOLE PhotoPrism library. The listing is served merged, so a page is
// routinely shorter than the requested count; stopping there reconciled the
// catalogue against the first page alone and could report a library complete with
// most of it missing.
func TestService_Verify_enumeratesPastShortMergedPages(t *testing.T) {
	t.Parallel()

	cat := newFakeCatalog()
	cat.importedUIDs, cat.fileCounts = importedLibrary()
	svc := importverify.NewService(importverify.Config{
		PhotoPrism: &fakePhotoPrism{photos: mergedLibrary(), merged: true},
		Catalog:    cat,
	})

	report, err := svc.Verify(context.Background())
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	pp := report.PhotoPrism
	if pp.SourceTotal != libraryPhotos {
		t.Errorf("SourceTotal = %d, want %d", pp.SourceTotal, libraryPhotos)
	}
	if pp.ImportedCount != libraryPhotos {
		t.Errorf("ImportedCount = %d, want %d", pp.ImportedCount, libraryPhotos)
	}
	if pp.MissingCount != 0 {
		t.Errorf("MissingCount = %d, want 0", pp.MissingCount)
	}
	if got := pp.SourceByType["image"]; got != libraryPhotos {
		t.Errorf("SourceByType[image] = %d, want %d", got, libraryPhotos)
	}
}

// TestService_Verify_mergedPageOverlapCountsEachPhotoOnce pins the two properties
// the offset arithmetic depends on. The offset advances by the page length, which
// under-advances against the source's file-row offset, so the next page re-serves
// photos already seen: they must be counted once. And a photo straddling the page
// boundary arrives with only part of its files, so the widest file list must win —
// otherwise the partial one silently masks a real file gap.
func TestService_Verify_mergedPageOverlapCountsEachPhotoOnce(t *testing.T) {
	t.Parallel()

	cat := newFakeCatalog()
	cat.importedUIDs, cat.fileCounts = importedLibrary()
	svc := importverify.NewService(importverify.Config{
		PhotoPrism: &fakePhotoPrism{photos: mergedLibrary(), merged: true},
		Catalog:    cat,
	})

	report, err := svc.Verify(context.Background())
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	pp := report.PhotoPrism
	// The re-served overlap must not inflate the totals: every classified photo is
	// counted exactly once.
	if got := pp.ImportedCount + pp.DeduplicatedCount + pp.MissingCount; got != libraryPhotos {
		t.Errorf("classified photos = %d, want %d", got, libraryPhotos)
	}
	wantUID := fmt.Sprintf("pp%04d", straddlingPhoto)
	if pp.FileGapCount != 1 {
		t.Fatalf("FileGapCount = %d, want 1 (%s has 2 source files, 1 original)", pp.FileGapCount, wantUID)
	}
	gap := pp.FileGaps[0]
	if gap.PhotoprismUID != wantUID || gap.Expected != 2 || gap.Actual != 1 {
		t.Errorf("FileGaps[0] = %+v, want {%s 2 1}", gap, wantUID)
	}
	if report.Complete {
		t.Error("Complete = true, want false while a file gap is open")
	}
}
