package photoapi

import (
	"context"
	"encoding/json"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/panbotka/kukatko/internal/avatar"
	"github.com/panbotka/kukatko/internal/photos"
)

// stubFaceCropRenderer stands in for a configured renderer in the tests that only
// need one to exist: they are answered before any crop is cut, so calling it is a
// test failure rather than a scenario.
type stubFaceCropRenderer struct{}

// Open fails the moment it is reached, since no test using this stub should get
// as far as rendering.
func (stubFaceCropRenderer) Open(
	context.Context, photos.Photo, *avatar.Box,
) (io.ReadCloser, string, error) {
	panic("face crop renderer must not be reached")
}

// faceCropRouter mounts only the face-crop handler (no auth middleware) so the
// branches that answer before the photo store is ever reached can be exercised
// without a database.
func faceCropRouter(api *API) http.Handler {
	r := chi.NewRouter()
	r.Get("/photos/{uid}/face", api.handleFaceCrop)
	return r
}

func TestParseFaceBox(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		raw  string
		want avatar.Box
		ok   bool
	}{
		{
			name: "four values",
			raw:  "0.1,0.2,0.3,0.4",
			want: avatar.Box{X: 0.1, Y: 0.2, W: 0.3, H: 0.4},
			ok:   true,
		},
		{
			name: "surrounding spaces",
			raw:  " 0.1 , 0.2 , 0.3 , 0.4 ",
			want: avatar.Box{X: 0.1, Y: 0.2, W: 0.3, H: 0.4},
			ok:   true,
		},
		{
			// The detector's boxes routinely hang over an edge; the renderer slides
			// them back inside rather than clipping, so the route must let them in.
			name: "box overhanging the frame",
			raw:  "-0.05,-0.02,0.2,0.2",
			want: avatar.Box{X: -0.05, Y: -0.02, W: 0.2, H: 0.2},
			ok:   true,
		},
		{name: "empty", raw: ""},
		{name: "three values", raw: "0.1,0.2,0.3"},
		{name: "five values", raw: "0.1,0.2,0.3,0.4,0.5"},
		{name: "not a number", raw: "0.1,0.2,0.3,x"},
		{name: "not a number NaN", raw: "0.1,0.2,0.3,NaN"},
		{name: "infinite", raw: "0.1,0.2,Inf,0.4"},
		{name: "zero width", raw: "0.1,0.2,0,0.4"},
		{name: "negative height", raw: "0.1,0.2,0.3,-0.4"},
		{name: "entirely right of the frame", raw: "1.5,0.2,0.3,0.4"},
		{name: "entirely below the frame", raw: "0.1,1,0.3,0.4"},
		{name: "entirely left of the frame", raw: "-0.4,0.2,0.3,0.4"},
		{name: "entirely above the frame", raw: "0.1,-0.4,0.3,0.4"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, ok := parseFaceBox(tc.raw)
			if ok != tc.ok {
				t.Fatalf("parseFaceBox(%q) ok = %v, want %v", tc.raw, ok, tc.ok)
			}
			if !tc.ok {
				return
			}
			if math.Abs(got.X-tc.want.X) > 1e-9 || math.Abs(got.Y-tc.want.Y) > 1e-9 ||
				math.Abs(got.W-tc.want.W) > 1e-9 || math.Abs(got.H-tc.want.H) > 1e-9 {
				t.Errorf("parseFaceBox(%q) = %+v, want %+v", tc.raw, got, tc.want)
			}
		})
	}
}

func TestHandleFaceCrop_earlyAnswers(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		renderer   FaceCropRenderer
		query      string
		wantStatus int
	}{
		{
			// A build with no derived-media cache cannot cut a crop; it says so
			// rather than pretending the face does not exist.
			name:       "no renderer",
			renderer:   nil,
			query:      "?box=0.1,0.2,0.3,0.4",
			wantStatus: http.StatusServiceUnavailable,
		},
		{
			name:       "missing box",
			renderer:   stubFaceCropRenderer{},
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "malformed box",
			renderer:   stubFaceCropRenderer{},
			query:      "?box=0.1,0.2,0.3",
			wantStatus: http.StatusBadRequest,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			api := &API{faceCrops: tc.renderer}
			rec := httptest.NewRecorder()
			req := httptest.NewRequestWithContext(
				t.Context(), http.MethodGet, "/photos/ph_1/face"+tc.query, nil,
			)
			faceCropRouter(api).ServeHTTP(rec, req)

			if rec.Code != tc.wantStatus {
				t.Fatalf("status = %d, want %d", rec.Code, tc.wantStatus)
			}
			var body errorBody
			if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
				t.Fatalf("decoding error body: %v", err)
			}
			if body.Error == "" {
				t.Error("error body carries no message")
			}
		})
	}
}
