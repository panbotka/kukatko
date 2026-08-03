package psfeedsimport

import (
	"context"
	"math"
	"testing"

	"github.com/panbotka/kukatko/internal/psfeeds"
)

// TestAddFaceNormalisesAgainstTheDisplayFrame pins the conversion the feed needs:
// its raw-pixel boxes are in DISPLAY space (the sidecar auto-rotates before
// detecting) and its photo_width/photo_height are PhotoPrism's, which already
// have the orientation applied. Dividing by that pair swapped again — which is
// what NormalizeBBox does when handed an already-oriented frame — normalised
// every quarter-turned face against the transposed frame.
//
// The fixture is a square 240 px face at (912, 1368) of the displayed 3648 × 5472
// frame of the bug report's photo.
func TestAddFaceNormalisesAgainstTheDisplayFrame(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name        string
		orientation int
		// The feed's frame: PhotoPrism's, i.e. already displayed.
		feedWidth  int
		feedHeight int
		// The stored frame the face row should cache.
		wantWidth  int
		wantHeight int
		want       [4]float64
	}{
		{
			name: "upright", orientation: 1,
			feedWidth: 3648, feedHeight: 5472, wantWidth: 3648, wantHeight: 5472,
			want: [4]float64{912.0 / 3648, 1368.0 / 5472, 240.0 / 3648, 240.0 / 5472},
		},
		{
			name: "180 degrees", orientation: 3,
			feedWidth: 3648, feedHeight: 5472, wantWidth: 3648, wantHeight: 5472,
			want: [4]float64{912.0 / 3648, 1368.0 / 5472, 240.0 / 3648, 240.0 / 5472},
		},
		{
			name: "90 CW", orientation: 6,
			feedWidth: 3648, feedHeight: 5472, wantWidth: 5472, wantHeight: 3648,
			want: [4]float64{912.0 / 3648, 1368.0 / 5472, 240.0 / 3648, 240.0 / 5472},
		},
		{
			name: "270 CW", orientation: 8,
			feedWidth: 3648, feedHeight: 5472, wantWidth: 5472, wantHeight: 3648,
			want: [4]float64{912.0 / 3648, 1368.0 / 5472, 240.0 / 3648, 240.0 / 5472},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			fi := &facesImport{
				svc:       &Service{},
				st:        &runState{},
				subjects:  map[string]string{},
				seen:      map[string]struct{}{},
				photoUID:  "kk1",
				havePhoto: true,
			}
			// A nameless face with no marker resolves to nobody, so the conversion
			// runs without touching the subject or marker collaborators.
			if err := fi.addFace(context.Background(), psfeeds.Face{
				FaceIndex:   0,
				BBox:        []float64{912, 1368, 912 + 240, 1368 + 240},
				PhotoWidth:  tc.feedWidth,
				PhotoHeight: tc.feedHeight,
				Orientation: tc.orientation,
			}); err != nil {
				t.Fatalf("addFace: %v", err)
			}
			if len(fi.faces) != 1 {
				t.Fatalf("got %d faces, want 1", len(fi.faces))
			}
			face := fi.faces[0]
			if face.PhotoWidth != tc.wantWidth || face.PhotoHeight != tc.wantHeight {
				t.Errorf("cached frame = %d×%d, want %d×%d",
					face.PhotoWidth, face.PhotoHeight, tc.wantWidth, tc.wantHeight)
			}
			for i := range face.BBox {
				if math.Abs(face.BBox[i]-tc.want[i]) > 1e-9 {
					t.Errorf("bbox[%d] = %v, want %v (full %v)", i, face.BBox[i], tc.want[i], face.BBox)
				}
			}
			// A square face normalised against a portrait display frame has
			// w/h = height/width — 1.5 here, the ratio the bug report measured on
			// the markers PhotoPrism had normalised correctly.
			if ratio := face.BBox[2] / face.BBox[3]; math.Abs(ratio-5472.0/3648.0) > 1e-9 {
				t.Errorf("w/h = %v, want %v", ratio, 5472.0/3648.0)
			}
		})
	}
}
