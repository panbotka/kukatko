package importverify_test

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/panbotka/kukatko/internal/importverify"
	"github.com/panbotka/kukatko/internal/photoprism"
)

// errCounts is a sentinel used to assert a library-counts failure is wrapped and
// surfaced by Verify.
var errCounts = errors.New("counts boom")

// indexed builds a photo in the shape the production defect turned on: indexed
// once and, unless touched, never modified since — so its UpdatedAt equals its
// CreatedAt and PhotoPrism's order=updated listing does not serve it.
func indexed(uid string, touched bool) photoprism.Photo {
	p := photo(uid, "image", "h-"+uid, 1)
	p.CreatedAt = time.Date(2026, 4, 27, 21, 38, 7, 0, time.UTC)
	p.UpdatedAt = p.CreatedAt
	if touched {
		p.UpdatedAt = p.CreatedAt.Add(72 * time.Hour)
	}
	return p
}

// TestService_Verify_enumeratesPhotosUntouchedSinceIndexing reproduces the
// production defect the whole listing guard exists for.
//
// Two PhotoPrism photos taken on 2026-04-25 were absent from the catalogue under
// every identifier while `import verify` printed
// "source=20660 kukatko=20647 deduplicated=13 missing=0 => COMPLETE". Neither the
// importer nor the verifier had ever seen them: both listed with order=updated,
// which compiles to `WHERE photos.updated_at > photos.created_at`, and both
// photos had been untouched since PhotoPrism indexed them on 27 April. The
// listing served a 20 660-photo window of a 20 677-photo library, and every set
// comparison drawn from that window agreed that nothing was missing.
//
// The fixture is that shape in miniature: an untouched photo that no catalogue
// row accounts for. It must be enumerated, named by uid, and refuse Complete.
func TestService_Verify_enumeratesPhotosUntouchedSinceIndexing(t *testing.T) {
	t.Parallel()

	source := &fakePhotoPrism{photos: []photoprism.Photo{
		indexed("ppTouched", true),
		indexed("ppUntouched", false),
	}}
	cat := newFakeCatalog()
	cat.importedUIDs = set("ppTouched")

	svc := importverify.NewService(importverify.Config{PhotoPrism: source, Catalog: cat})
	report, err := svc.Verify(context.Background())
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}

	if _, filters := photoprism.FilteringOrderPredicate(source.lastOrder); filters {
		t.Errorf("listing order = %q, which also filters the library", source.lastOrder)
	}
	pp := report.PhotoPrism
	if pp.SourceTotal != 2 {
		t.Errorf("SourceTotal = %d, want 2 (the untouched photo is part of the library)", pp.SourceTotal)
	}
	if !slices.Equal(pp.MissingUIDs, []string{"ppUntouched"}) {
		t.Errorf("MissingUIDs = %v, want [ppUntouched]", pp.MissingUIDs)
	}
	if pp.ListingShortfall != 0 {
		t.Errorf("ListingShortfall = %d, want 0 (the listing served the whole library)", pp.ListingShortfall)
	}
	if report.Complete {
		t.Error("Complete = true while a source photo has no catalogue row")
	}
}

// TestService_Verify_listingShortfall covers the guard that survives the fix: the
// enumerated total measured against the total PhotoPrism reports for itself.
//
// It is the only check in the photo section that does not come from the listing,
// which is the point — a narrowed listing does not fail, it pages to exhaustion
// and hands back a self-consistent subset, so no comparison drawn from it can
// tell that anything is absent.
func TestService_Verify_listingShortfall(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		reportedTotal int
		wantShortfall int
		wantComplete  bool
	}{
		{
			name:          "listing serves the whole library",
			reportedTotal: 2,
			wantShortfall: 0,
			wantComplete:  true,
		},
		{
			// The shape of the production report: nothing classified missing, because
			// what is missing was never listed to be classified.
			name:          "source reports more than the listing served",
			reportedTotal: 4,
			wantShortfall: 2,
			wantComplete:  false,
		},
		{
			// PhotoPrism subtracts pictures in review from its own total and hides
			// private ones from a restricted session, so its count is a lower bound.
			// Serving more than it reports is a normal state, not a finding.
			name:          "listing serves more than the source reports",
			reportedTotal: 1,
			wantShortfall: 0,
			wantComplete:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			cat := newFakeCatalog()
			cat.importedUIDs = set("ppA", "ppB")
			cat.fileCounts = map[string]int{"ppA": 1, "ppB": 1}
			svc := importverify.NewService(importverify.Config{
				PhotoPrism: &fakePhotoPrism{
					photos:        []photoprism.Photo{indexed("ppA", true), indexed("ppB", true)},
					reportedTotal: tt.reportedTotal,
				},
				Catalog: cat,
			})

			report, err := svc.Verify(context.Background())
			if err != nil {
				t.Fatalf("Verify: %v", err)
			}
			pp := report.PhotoPrism
			if pp.SourceReportedTotal != tt.reportedTotal {
				t.Errorf("SourceReportedTotal = %d, want %d", pp.SourceReportedTotal, tt.reportedTotal)
			}
			if pp.ListingShortfall != tt.wantShortfall {
				t.Errorf("ListingShortfall = %d, want %d", pp.ListingShortfall, tt.wantShortfall)
			}
			if pp.MissingCount != 0 {
				t.Errorf("MissingCount = %d, want 0 (both listed photos are imported)", pp.MissingCount)
			}
			if report.Complete != tt.wantComplete {
				t.Errorf("Complete = %v, want %v", report.Complete, tt.wantComplete)
			}
		})
	}
}

// TestService_Verify_surplusCatalogUIDs checks the other direction of the set
// comparison: a catalogue photo whose photoprism_uid the source enumeration never
// yielded is named, but does not fail the report — a photo deleted in PhotoPrism
// after Kukátko imported it leaves exactly that trace and cannot be re-imported.
func TestService_Verify_surplusCatalogUIDs(t *testing.T) {
	t.Parallel()

	cat := newFakeCatalog()
	cat.importedUIDs = set("ppLive", "ppDeletedUpstream")
	cat.fileCounts = map[string]int{"ppLive": 1}

	svc := importverify.NewService(importverify.Config{
		PhotoPrism: &fakePhotoPrism{photos: []photoprism.Photo{indexed("ppLive", true)}},
		Catalog:    cat,
	})
	report, err := svc.Verify(context.Background())
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	pp := report.PhotoPrism
	if pp.SurplusCount != 1 || !slices.Equal(pp.SurplusUIDs, []string{"ppDeletedUpstream"}) {
		t.Errorf("surplus = %d %v, want 1 [ppDeletedUpstream]", pp.SurplusCount, pp.SurplusUIDs)
	}
	if !report.Complete {
		t.Error("Complete = false, want true: a surplus is reported, never enforced")
	}
}

// TestService_Verify_countsError checks that a failure to read the source's own
// library totals aborts the pass instead of silently reconciling without the
// cross-check.
func TestService_Verify_countsError(t *testing.T) {
	t.Parallel()

	svc := importverify.NewService(importverify.Config{
		PhotoPrism: &fakePhotoPrism{countsErr: errCounts},
		Catalog:    newFakeCatalog(),
	})
	if _, err := svc.Verify(context.Background()); !errors.Is(err, errCounts) {
		t.Fatalf("Verify error = %v, want wrapping %v", err, errCounts)
	}
}
