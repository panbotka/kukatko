package maintenance

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/panbotka/kukatko/internal/audit"
	"github.com/panbotka/kukatko/internal/photos"
	"github.com/panbotka/kukatko/internal/rekey"
	"github.com/panbotka/kukatko/internal/storekeys"
)

// fakeRootDerived is a RootDerivedMover over a fixed finding.
type fakeRootDerived struct {
	found   []rekey.RootDerivedPhoto
	findErr error
	moved   [][]rekey.RootDerivedPhoto
	result  rekey.RootDerivedResult
}

func (f *fakeRootDerived) FindRootDerived(context.Context) ([]rekey.RootDerivedPhoto, error) {
	return f.found, f.findErr
}

func (f *fakeRootDerived) MoveRootDerived(
	_ context.Context, found []rekey.RootDerivedPhoto,
) (rekey.RootDerivedResult, error) {
	f.moved = append(f.moved, found)
	return f.result, nil
}

// rootTile is a root thumbnail of a photo that already lives under its prefix.
const rootTile = "thumb/aa/bb/cc/aabbcc_tile_224.jpg"

// movedScenario is the bootstrap project after its move with one root tile the
// move left behind, wired with mover.
func movedScenario(mover RootDerivedMover) *Service {
	files := []photos.CataloguedFile{{ProjectUID: uidV, FilePath: prefixOf(uidV) + "2024/05/moved.jpg"}}
	svc := projectScenario(projectsAfterMove(), files,
		[]string{prefixOf(uidV) + "2024/05/moved.jpg", rootTile, "families.yaml"}, nil)
	svc.rootDerived = mover
	return svc
}

// oneRootTile is the finding of movedScenario.
func oneRootTile() []rekey.RootDerivedPhoto {
	return []rekey.RootDerivedPhoto{{UID: "pha", ProjectUID: uidV, Moves: []rekey.Move{{
		From: rootTile, To: prefixOf(uidV) + rootTile, Kind: storekeys.KindThumbnail,
	}}}}
}

func TestScan_reportsRootDerivedApartFromLegacy(t *testing.T) {
	t.Parallel()
	report, err := movedScenario(&fakeRootDerived{found: oneRootTile()}).Scan(context.Background())
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if report.RootDerived.Count != 1 || !slices.Equal(report.RootDerived.Samples, []string{rootTile}) {
		t.Errorf("root_derived = %+v, want the one root tile", report.RootDerived)
	}
	if !slices.Equal(report.LegacyLayout.Samples, []string{"families.yaml"}) {
		t.Errorf("legacy_layout = %+v, want only families.yaml: the tile is root_derived", report.LegacyLayout)
	}
	if report.Clean() {
		t.Error("a scan with derived objects left at the root must not be clean")
	}
}

func TestScan_withoutMoverHasNoRootDerived(t *testing.T) {
	t.Parallel()
	report, err := movedScenario(nil).Scan(context.Background())
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if report.RootDerived.Count != 0 || report.RootDerived.Samples == nil {
		t.Errorf("root_derived = %+v, want an empty finding", report.RootDerived)
	}
	if !slices.Contains(report.LegacyLayout.Samples, rootTile) {
		t.Errorf("legacy_layout = %+v, want the tile counted as legacy without the check", report.LegacyLayout)
	}
}

func TestScan_rootDerivedFailureFailsTheScan(t *testing.T) {
	t.Parallel()
	errList := errors.New("bucket unreachable")
	if _, err := movedScenario(&fakeRootDerived{findErr: errList}).Scan(context.Background()); !errors.Is(err, errList) {
		t.Errorf("Scan = %v, want the finder's error", err)
	}
}

func TestRepairRootDerived(t *testing.T) {
	t.Parallel()
	mover := &fakeRootDerived{found: oneRootTile(), result: rekey.RootDerivedResult{Photos: 1, Objects: 1}}
	svc := movedScenario(mover)

	res, err := svc.Repair(context.Background(), RepairOptions{RootDerived: true}, audit.Meta{})
	if err != nil {
		t.Fatalf("Repair: %v", err)
	}
	if res.RootDerivedMoved != 1 || res.RootDerivedObjects != 1 || res.RootDerivedFailed != 0 {
		t.Errorf("result = %+v, want one photo, one object moved", res)
	}
	if len(mover.moved) != 1 || len(mover.moved[0]) != 1 || mover.moved[0][0].UID != "pha" {
		t.Errorf("moved %+v, want the found photo handed over once", mover.moved)
	}

	if _, err := svc.Repair(context.Background(), RepairOptions{Thumbnails: true}, audit.Meta{}); err != nil ||
		len(mover.moved) != 1 {
		t.Errorf("an unrelated repair moved root objects (%v, %d calls)", err, len(mover.moved))
	}
	if !(RepairOptions{RootDerived: true}).Any() {
		t.Error("RepairOptions.Any ignores root_derived")
	}
	_, err = movedScenario(nil).Repair(context.Background(), RepairOptions{RootDerived: true}, audit.Meta{})
	if !errors.Is(err, ErrRootDerivedUnavailable) {
		t.Errorf("Repair without the storage move = %v, want ErrRootDerivedUnavailable", err)
	}
}
