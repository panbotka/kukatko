package psimport

import (
	"math"
	"testing"

	"github.com/panbotka/kukatko/internal/photosorter"
	"github.com/panbotka/kukatko/internal/storage"
)

// TestBuildPhotoStoresRawGeometry pins that photo-sorter's dimensions — copied
// straight out of PhotoPrism, which reports them with the EXIF orientation
// already applied — are de-oriented on the way in, so file_width/file_height and
// file_orientation describe the same, untouched bytes.
func TestBuildPhotoStoresRawGeometry(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name         string
		orientation  int
		width        int
		height       int
		wantW, wantH int
	}{
		{name: "upright", orientation: 1, width: 5472, height: 3648, wantW: 5472, wantH: 3648},
		{name: "180 degrees", orientation: 3, width: 5472, height: 3648, wantW: 5472, wantH: 3648},
		{name: "90 CW", orientation: 6, width: 3648, height: 5472, wantW: 5472, wantH: 3648},
		{name: "270 CW", orientation: 8, width: 3648, height: 5472, wantW: 5472, wantH: 3648},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := buildPhoto(photosorter.Photo{
				UID:             "ps1",
				FileWidth:       tc.width,
				FileHeight:      tc.height,
				FileOrientation: tc.orientation,
			}, storage.StoredFile{Hash: "sha256", RelPath: "2024/01/x.jpg"})
			if got.FileWidth != tc.wantW || got.FileHeight != tc.wantH {
				t.Errorf("dimensions = %d×%d, want %d×%d",
					got.FileWidth, got.FileHeight, tc.wantW, tc.wantH)
			}
			if got.FileOrientation != tc.orientation {
				t.Errorf("orientation = %d, want %d", got.FileOrientation, tc.orientation)
			}
		})
	}
}

// TestConvertFaceRepairsTransposedNormalisation pins that a migrated face lands
// in the one meaning Kukátko reads: a display-space box over the stored frame.
// photo-sorter divided the detector's display-space pixels by PhotoPrism's
// already-oriented pair swapped a second time, so for a quarter turn both the
// cached frame and the box need correcting.
func TestConvertFaceRepairsTransposedNormalisation(t *testing.T) {
	t.Parallel()
	// A square 240 px face at (912, 1368) of the displayed 3648 × 5472 frame, as
	// photo-sorter stored it (divided by the transposed 5472 × 3648).
	stored := [4]float64{912.0 / 5472, 1368.0 / 3648, 240.0 / 5472, 240.0 / 3648}
	want := [4]float64{912.0 / 3648, 1368.0 / 5472, 240.0 / 3648, 240.0 / 5472}

	for _, orientation := range []int{6, 8} {
		got := convertFace("kk1", photosorter.Face{
			BBox:        stored,
			PhotoWidth:  3648,
			PhotoHeight: 5472,
			Orientation: orientation,
		}, mappings{})
		if got.PhotoWidth != 5472 || got.PhotoHeight != 3648 {
			t.Errorf("orientation %d: frame = %d×%d, want 5472×3648",
				orientation, got.PhotoWidth, got.PhotoHeight)
		}
		for i := range got.BBox {
			if math.Abs(got.BBox[i]-want[i]) > 1e-9 {
				t.Errorf("orientation %d: bbox[%d] = %v, want %v",
					orientation, i, got.BBox[i], want[i])
			}
		}
	}
}

// TestConvertFaceLeavesUnrotatedFacesAlone keeps the correction confined to the
// quarter turns: without a swap the two frames coincide and the source values
// were already right.
func TestConvertFaceLeavesUnrotatedFacesAlone(t *testing.T) {
	t.Parallel()
	box := [4]float64{0.1, 0.2, 0.3, 0.4}
	for _, orientation := range []int{1, 3} {
		got := convertFace("kk1", photosorter.Face{
			BBox:        box,
			PhotoWidth:  5472,
			PhotoHeight: 3648,
			Orientation: orientation,
		}, mappings{})
		if got.BBox != box {
			t.Errorf("orientation %d: bbox = %v, want %v", orientation, got.BBox, box)
		}
		if got.PhotoWidth != 5472 || got.PhotoHeight != 3648 {
			t.Errorf("orientation %d: frame = %d×%d, want 5472×3648",
				orientation, got.PhotoWidth, got.PhotoHeight)
		}
	}
}
