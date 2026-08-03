//go:build integration

package vectors_test

import (
	"math"
	"testing"

	"github.com/panbotka/kukatko/internal/vectors"
)

// transposedFace builds a face row as a PhotoPrism-derived import wrote it for a
// quarter-turned photo: the cached frame is the DISPLAYED 3648 × 5472 pair and
// the bbox of a square 240 px face at (912, 1368) was divided by that pair
// swapped again.
func transposedFace(index, orientation int) vectors.Face {
	return vectors.Face{
		FaceIndex:   index,
		Vector:      faceVec(map[int]float32{index: 1}),
		BBox:        [4]float64{912.0 / 5472, 1368.0 / 3648, 240.0 / 5472, 240.0 / 3648},
		Model:       "buffalo_l",
		PhotoWidth:  3648,
		PhotoHeight: 5472,
		Orientation: orientation,
	}
}

// TestRepairFaceDimensions_fixesTransposedRows verifies the faces half of the
// orientation repair: the cached frame becomes the photo's stored pair and the
// box is re-normalised against the real display frame, for both quarter turns.
func TestRepairFaceDimensions_fixesTransposedRows(t *testing.T) {
	for _, orientation := range []int{6, 8} {
		store, photoStore, _ := newStore(t)
		ctx := t.Context()
		uid := makePhoto(t, photoStore, "rot_"+string(rune('0'+orientation)))

		if err := store.RecordFaceDetection(ctx, uid,
			[]vectors.Face{transposedFace(0, orientation), transposedFace(1, orientation)},
			"buffalo_l"); err != nil {
			t.Fatalf("RecordFaceDetection: %v", err)
		}

		n, err := store.RepairFaceDimensions(ctx, uid, 5472, 3648)
		if err != nil {
			t.Fatalf("RepairFaceDimensions: %v", err)
		}
		if n != 2 {
			t.Fatalf("repaired %d rows, want 2", n)
		}

		faces, err := store.ListFaces(ctx, uid)
		if err != nil {
			t.Fatalf("ListFaces: %v", err)
		}
		want := [4]float64{912.0 / 3648, 1368.0 / 5472, 240.0 / 3648, 240.0 / 5472}
		for _, f := range faces {
			if f.PhotoWidth != 5472 || f.PhotoHeight != 3648 {
				t.Errorf("orientation %d: frame = %d×%d, want 5472×3648",
					orientation, f.PhotoWidth, f.PhotoHeight)
			}
			for i := range f.BBox {
				if math.Abs(f.BBox[i]-want[i]) > 1e-9 {
					t.Errorf("orientation %d: bbox[%d] = %v, want %v",
						orientation, i, f.BBox[i], want[i])
				}
			}
		}

		// Idempotent: the fingerprint in the WHERE clause stops matching.
		if n, err = store.RepairFaceDimensions(ctx, uid, 5472, 3648); err != nil || n != 0 {
			t.Errorf("second run = (%d, %v), want (0, nil)", n, err)
		}
	}
}

// TestRepairFaceDimensions_leavesUnrotatedFacesAlone verifies the repair only
// touches quarter-turned rows: an upright or 180°-rotated face was normalised
// against the frame it caches and must not be rescaled.
func TestRepairFaceDimensions_leavesUnrotatedFacesAlone(t *testing.T) {
	for _, orientation := range []int{1, 3} {
		store, photoStore, _ := newStore(t)
		ctx := t.Context()
		uid := makePhoto(t, photoStore, "up_"+string(rune('0'+orientation)))

		face := transposedFace(0, orientation)
		if err := store.RecordFaceDetection(ctx, uid, []vectors.Face{face}, "buffalo_l"); err != nil {
			t.Fatalf("RecordFaceDetection: %v", err)
		}
		n, err := store.RepairFaceDimensions(ctx, uid, 5472, 3648)
		if err != nil {
			t.Fatalf("RepairFaceDimensions: %v", err)
		}
		if n != 0 {
			t.Errorf("orientation %d: repaired %d rows, want 0", orientation, n)
		}
		faces, err := store.ListFaces(ctx, uid)
		if err != nil || len(faces) != 1 {
			t.Fatalf("ListFaces = (%v, %v)", faces, err)
		}
		if faces[0].BBox != face.BBox {
			t.Errorf("orientation %d: bbox = %v, want it untouched %v",
				orientation, faces[0].BBox, face.BBox)
		}
	}
}
