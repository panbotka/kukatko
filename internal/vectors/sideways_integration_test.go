//go:build integration

package vectors_test

import (
	"context"
	"testing"

	"github.com/panbotka/kukatko/internal/photos"
	"github.com/panbotka/kukatko/internal/vectors"
)

// makePhotoWithGeometry inserts a photo with the given raw pair and orientation
// and returns its uid.
func makePhotoWithGeometry(
	t *testing.T, store *photos.Store, hash string, width, height, orientation int,
) string {
	t.Helper()
	created, err := store.Create(context.Background(), photos.Photo{
		FileHash:        hash,
		FilePath:        "2026/08/" + hash + ".jpg",
		FileName:        hash + ".jpg",
		FileWidth:       width,
		FileHeight:      height,
		FileOrientation: orientation,
	})
	if err != nil {
		t.Fatalf("creating photo %s: %v", hash, err)
	}
	return created.UID
}

// TestSidewaysDetections_fingerprint checks which detections the repair claims,
// over the four orientations that matter and both provenances of a recorded frame.
//
// The set is "a quarter-turned photo whose detection cannot be shown to have run
// upright": an unrecorded frame (every detection written before migration 0061) or
// a frame that is the photo's raw pair. A detection recorded against the display
// frame — what the fixed job writes — is out, and so is every non-quarter-turned
// photo, whose two frames coincide.
func TestSidewaysDetections_fingerprint(t *testing.T) {
	store, photoStore, _ := newStore(t)
	ctx := t.Context()

	const rawW, rawH = 5712, 4284
	cases := []struct {
		hash        string
		orientation int
		frameW      int // 0 = record no frame at all (the pre-0061 state)
		frameH      int
		wantListed  bool
	}{
		{"sw_o6_noframe", 6, 0, 0, true},
		{"sw_o8_noframe", 8, 0, 0, true},
		{"sw_o6_rawframe", 6, rawW, rawH, true},
		{"sw_o6_displayframe", 6, rawH, rawW, false},
		{"sw_o8_displayframe", 8, rawH, rawW, false},
		{"sw_o1_noframe", 1, 0, 0, false},
		{"sw_o3_noframe", 3, 0, 0, false},
	}

	want := make(map[string]bool, len(cases))
	for _, tc := range cases {
		uid := makePhotoWithGeometry(t, photoStore, tc.hash, rawW, rawH, tc.orientation)
		det := vectors.Detection{Model: "buffalo_l", FrameWidth: tc.frameW, FrameHeight: tc.frameH}
		if err := store.RecordFaceDetection(ctx, uid, det); err != nil {
			t.Fatalf("RecordFaceDetection %s: %v", tc.hash, err)
		}
		if tc.wantListed {
			want[uid] = true
		}
	}

	listed, err := store.ListSidewaysDetections(ctx)
	if err != nil {
		t.Fatalf("ListSidewaysDetections: %v", err)
	}
	got := make(map[string]bool, len(listed))
	for _, uid := range listed {
		got[uid] = true
	}
	for uid := range want {
		if !got[uid] {
			t.Errorf("uid %s not listed as a sideways detection", uid)
		}
	}
	for uid := range got {
		if !want[uid] {
			t.Errorf("uid %s listed as a sideways detection, want it left out", uid)
		}
	}
}

// TestSidewaysDetections_redetectionRetiresTheRow is the convergence property the
// repair rests on: clearing the record makes the photo eligible again (and keeps
// its faces meanwhile), and a re-detection recorded against the display frame takes
// it out of the set for good, so re-running the repair is a no-op.
func TestSidewaysDetections_redetectionRetiresTheRow(t *testing.T) {
	store, photoStore, _ := newStore(t)
	ctx := t.Context()

	const rawW, rawH = 4000, 3000
	uid := makePhotoWithGeometry(t, photoStore, "sw_cycle", rawW, rawH, 6)
	face := vectors.Face{
		FaceIndex: 0, Vector: faceVec(map[int]float32{0: 1}),
		BBox: [4]float64{0.1, 0.1, 0.2, 0.2}, Model: "buffalo_l",
		PhotoWidth: rawW, PhotoHeight: rawH, Orientation: 6,
	}
	if err := store.RecordFaceDetection(ctx, uid,
		vectors.Detection{Faces: []vectors.Face{face}, Model: "buffalo_l"}); err != nil {
		t.Fatalf("RecordFaceDetection: %v", err)
	}
	if listed, _ := store.ListSidewaysDetections(ctx); len(listed) != 1 {
		t.Fatalf("listed %v, want the one sideways photo", listed)
	}

	cleared, err := store.ClearFaceDetection(ctx, uid)
	if err != nil || !cleared {
		t.Fatalf("ClearFaceDetection = (%v, %v), want (true, nil)", cleared, err)
	}
	if detected, _ := store.FacesDetected(ctx, uid); detected {
		t.Error("photo still counts as detected after clearing its record")
	}
	if faces, _ := store.ListFaces(ctx, uid); len(faces) != 1 {
		t.Errorf("faces after clearing = %d, want the existing box kept until re-detection", len(faces))
	}
	if listed, _ := store.ListSidewaysDetections(ctx); len(listed) != 0 {
		t.Errorf("listed %v after clearing, want none (there is no detection to judge)", listed)
	}

	// Re-detection the way the fixed job records it: the display frame.
	if err := store.RecordFaceDetection(ctx, uid, vectors.Detection{
		Faces: []vectors.Face{face}, Model: "buffalo_l", FrameWidth: rawH, FrameHeight: rawW,
	}); err != nil {
		t.Fatalf("re-RecordFaceDetection: %v", err)
	}
	if listed, _ := store.ListSidewaysDetections(ctx); len(listed) != 0 {
		t.Errorf("listed %v after an upright re-detection, want none", listed)
	}

	// Clearing a photo with no record is a no-op, which keeps the repair re-runnable.
	if _, err := store.ClearFaceDetection(ctx, uid); err != nil {
		t.Fatalf("ClearFaceDetection second: %v", err)
	}
	if cleared, err := store.ClearFaceDetection(ctx, uid); err != nil || cleared {
		t.Errorf("ClearFaceDetection on a cleared photo = (%v, %v), want (false, nil)", cleared, err)
	}
}
