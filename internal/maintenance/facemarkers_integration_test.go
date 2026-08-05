//go:build integration

package maintenance_test

import (
	"context"
	"testing"

	"github.com/panbotka/kukatko/internal/maintenance"
	"github.com/panbotka/kukatko/internal/people"
	"github.com/panbotka/kukatko/internal/photos"
	"github.com/panbotka/kukatko/internal/vectors"
)

// The geometry of the production photo that motivated the exclusive pairing: one
// marker for "Tomáš Kozák" and two detected faces, of which face 7 overlaps the
// marker far better (~0.80) than face 6 (~0.16). Both cleared the matching
// threshold, so the old per-face search handed the marker to each of them and the
// person rendered twice.
var (
	tomasMarkerBox = [4]float64{0.2889, 0.3241, 0.0917, 0.1222}
	looserFaceBox  = [4]float64{0.3489, 0.2873, 0.0464, 0.1067}
	winnerFaceBox  = [4]float64{0.2965, 0.3217, 0.0755, 0.1227}
)

// seedDuplicateMarker catalogues a photo with one named face marker and two faces
// whose cached links both point at that marker, and returns the photo and the
// marker.
func (h *harness) seedDuplicateMarker(t *testing.T, name string, seed uint8) (photos.Photo, people.Marker) {
	t.Helper()
	ctx := context.Background()

	photo := h.storeRealPhoto(t, name, seed)
	subject, err := h.people.CreateSubject(ctx, people.Subject{Name: "Tomáš " + name})
	if err != nil {
		t.Fatalf("CreateSubject: %v", err)
	}
	marker, err := h.people.CreateMarker(ctx, people.Marker{
		PhotoUID: photo.UID, SubjectUID: &subject.UID, Type: people.MarkerFace,
		X: tomasMarkerBox[0], Y: tomasMarkerBox[1], W: tomasMarkerBox[2], H: tomasMarkerBox[3],
	})
	if err != nil {
		t.Fatalf("CreateMarker: %v", err)
	}
	if err := h.vectors.SaveFaces(ctx, photo.UID, []vectors.Face{
		{PhotoUID: photo.UID, FaceIndex: 6, Vector: faceVector(6), BBox: looserFaceBox, Model: "stub"},
		{PhotoUID: photo.UID, FaceIndex: 7, Vector: faceVector(7), BBox: winnerFaceBox, Model: "stub"},
	}); err != nil {
		t.Fatalf("SaveFaces: %v", err)
	}
	// Both faces claim the one marker, exactly as the database holds them today.
	for _, index := range []int{6, 7} {
		if err := h.vectors.UpdateFaceMarker(
			ctx, photo.UID, index, marker.UID, subject.UID, subject.Name,
		); err != nil {
			t.Fatalf("seeding duplicate link on face %d: %v", index, err)
		}
	}
	return photo, marker
}

// faceVector builds a FaceDim unit vector with the given index set to 1.
func faceVector(index int) []float32 {
	v := make([]float32, vectors.FaceDim)
	v[index] = 1
	return v
}

// markerLinks returns how many of a photo's faces cache each marker, keyed by
// marker uid.
func (h *harness) markerLinks(t *testing.T, photoUID string) map[string]int {
	t.Helper()
	faces, err := h.vectors.ListFaces(context.Background(), photoUID)
	if err != nil {
		t.Fatalf("ListFaces: %v", err)
	}
	counts := make(map[string]int, len(faces))
	for _, face := range faces {
		if face.MarkerUID != nil {
			counts[*face.MarkerUID]++
		}
	}
	return counts
}

// TestScanAndRepairFaceMarkers verifies the scan reports a marker cached on two
// faces and the repair clears the surplus link only: the closer face keeps the
// marker, the other is left unlinked (its face row survives), a re-scan is clean
// and a re-run changes nothing.
func TestScanAndRepairFaceMarkers(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()

	photo, marker := h.seedDuplicateMarker(t, "duplicate", 0x55)

	report, err := h.svc.Scan(ctx)
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	assertFinding(t, "duplicate face markers", report.DuplicateFaceMarkers, 1, marker.UID)

	res, err := h.svc.Repair(ctx, maintenance.RepairOptions{FaceMarkers: true})
	if err != nil {
		t.Fatalf("Repair(face markers): %v", err)
	}
	if res.FaceLinksCleared != 1 {
		t.Fatalf("FaceLinksCleared = %d, want 1", res.FaceLinksCleared)
	}

	faces, err := h.vectors.ListFaces(ctx, photo.UID)
	if err != nil {
		t.Fatalf("ListFaces: %v", err)
	}
	if len(faces) != 2 {
		t.Fatalf("faces after repair = %d, want 2 (rows are never deleted)", len(faces))
	}
	for _, face := range faces {
		switch face.FaceIndex {
		case 7:
			if face.MarkerUID == nil || *face.MarkerUID != marker.UID || face.SubjectUID == nil {
				t.Errorf("face 7 = %+v, want the marker and its subject kept", face)
			}
		case 6:
			if face.MarkerUID != nil || face.SubjectUID != nil || face.SubjectName != "" {
				t.Errorf("face 6 = %+v, want its surplus link cleared", face)
			}
		}
	}
	// The marker itself is untouched — only the cache columns move.
	if _, err := h.people.GetMarkerByUID(ctx, marker.UID); err != nil {
		t.Errorf("GetMarkerByUID after repair: %v", err)
	}

	report, err = h.svc.Scan(ctx)
	if err != nil {
		t.Fatalf("Scan after repair: %v", err)
	}
	if report.DuplicateFaceMarkers.Count != 0 {
		t.Errorf("duplicate face markers after repair = %d, want 0", report.DuplicateFaceMarkers.Count)
	}
	res, err = h.svc.Repair(ctx, maintenance.RepairOptions{FaceMarkers: true})
	if err != nil {
		t.Fatalf("Repair(face markers) re-run: %v", err)
	}
	if res.FaceLinksCleared != 0 {
		t.Errorf("re-run cleared %d links, want 0 (idempotent)", res.FaceLinksCleared)
	}
}

// TestRepairFaceMarkersLeavesSingleLinks verifies the repair never touches a
// marker only one face claims — the duplicate markers an import created are a
// different problem and must not be quietly swept up here.
func TestRepairFaceMarkersLeavesSingleLinks(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()

	// Two markers over the same region, one face each: two people on one face is a
	// duplicate-marker problem, not a duplicate-link one.
	photo := h.storeRealPhoto(t, "twomarkers", 0x66)
	markers := make([]people.Marker, 0, 2)
	for _, name := range []string{"Anna", "Bara"} {
		subject, err := h.people.CreateSubject(ctx, people.Subject{Name: name})
		if err != nil {
			t.Fatalf("CreateSubject(%s): %v", name, err)
		}
		marker, err := h.people.CreateMarker(ctx, people.Marker{
			PhotoUID: photo.UID, SubjectUID: &subject.UID, Type: people.MarkerFace,
			X: tomasMarkerBox[0], Y: tomasMarkerBox[1], W: tomasMarkerBox[2], H: tomasMarkerBox[3],
		})
		if err != nil {
			t.Fatalf("CreateMarker(%s): %v", name, err)
		}
		markers = append(markers, marker)
	}
	if err := h.vectors.SaveFaces(ctx, photo.UID, []vectors.Face{
		{PhotoUID: photo.UID, FaceIndex: 0, Vector: faceVector(0), BBox: winnerFaceBox, Model: "stub"},
		{PhotoUID: photo.UID, FaceIndex: 1, Vector: faceVector(1), BBox: looserFaceBox, Model: "stub"},
	}); err != nil {
		t.Fatalf("SaveFaces: %v", err)
	}
	for i, marker := range markers {
		if err := h.vectors.UpdateFaceMarker(ctx, photo.UID, i, marker.UID, "", ""); err != nil {
			t.Fatalf("linking face %d: %v", i, err)
		}
	}

	report, err := h.svc.Scan(ctx)
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if report.DuplicateFaceMarkers.Count != 0 {
		t.Fatalf("duplicate face markers = %d, want 0 (each marker has one face)",
			report.DuplicateFaceMarkers.Count)
	}
	if _, err := h.svc.Repair(ctx, maintenance.RepairOptions{FaceMarkers: true}); err != nil {
		t.Fatalf("Repair(face markers): %v", err)
	}
	links := h.markerLinks(t, photo.UID)
	for _, marker := range markers {
		if links[marker.UID] != 1 {
			t.Errorf("marker %s has %d links after repair, want 1 (untouched)", marker.UID, links[marker.UID])
		}
	}
}
