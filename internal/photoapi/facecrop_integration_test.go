//go:build integration

package photoapi_test

import (
	"bytes"
	"image"
	"image/jpeg"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/panbotka/kukatko/internal/auth"
	"github.com/panbotka/kukatko/internal/photos"
)

// faceBoxQuery is a box well inside the seeded 64×48 frame, in the normalised
// space a marker's box lives in.
const faceBoxQuery = "?box=0.25,0.25,0.25,0.25"

// decodeJPEG reads the whole body and decodes it, failing the test if it is not a
// JPEG. It returns the decoded image so a test can assert on the crop's shape.
func decodeJPEG(t *testing.T, body io.Reader) image.Image {
	t.Helper()
	data, err := io.ReadAll(body)
	if err != nil {
		t.Fatalf("reading body: %v", err)
	}
	img, err := jpeg.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("decoding body as JPEG (%d bytes): %v", len(data), err)
	}
	return img
}

// TestFaceCropRoute_servesASquareCropOfTheFace proves the route hands over a
// small square JPEG cut to the requested box — the whole point of it being a
// route at all, since the browser used to fetch the entire photograph to show
// this much of it.
func TestFaceCropRoute_servesASquareCropOfTheFace(t *testing.T) {
	env := newEnv(t)
	client, _ := env.login(t, "editor", auth.RoleEditor)
	seeded := env.seedPhoto(t, photos.Photo{Title: "Face", TakenAtSource: "unknown"}, "face.jpg", 11, 22, 33)

	resp := mustDo(t, client, http.MethodGet,
		env.server.URL+"/api/v1/photos/"+seeded.UID+"/face"+faceBoxQuery, nil)
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("face crop status = %d, want 200", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "image/jpeg" {
		t.Errorf("face crop content-type = %q, want image/jpeg", ct)
	}
	if resp.Header.Get("ETag") == "" {
		t.Error("face crop carries no ETag, so a repeat view cannot be answered 304")
	}
	if cc := resp.Header.Get("Cache-Control"); !strings.Contains(cc, "immutable") {
		t.Errorf("face crop Cache-Control = %q, want an immutable policy", cc)
	}

	img := decodeJPEG(t, resp.Body)
	bounds := img.Bounds()
	if bounds.Dx() != bounds.Dy() {
		t.Errorf("crop is %dx%d, want a square", bounds.Dx(), bounds.Dy())
	}
	// The seeded original is 64×48, so the padded box is a few dozen pixels: the
	// renderer refuses to upscale, and the honest answer is the pixels that exist.
	if bounds.Dx() == 0 || bounds.Dx() > 320 {
		t.Errorf("crop side = %d, want between 1 and the renderer's 320", bounds.Dx())
	}
}

// TestFaceCropRoute_repeatViewIsAnswered304 proves the validator works, which is
// what makes a page of hundreds of faces cheap on its second visit.
func TestFaceCropRoute_repeatViewIsAnswered304(t *testing.T) {
	env := newEnv(t)
	client, _ := env.login(t, "editor", auth.RoleEditor)
	seeded := env.seedPhoto(t, photos.Photo{Title: "Face", TakenAtSource: "unknown"}, "again.jpg", 44, 55, 66)
	url := env.server.URL + "/api/v1/photos/" + seeded.UID + "/face" + faceBoxQuery

	first := mustDo(t, client, http.MethodGet, url, nil)
	etag := first.Header.Get("ETag")
	_ = first.Body.Close()
	if etag == "" {
		t.Fatal("first response carries no ETag")
	}

	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, url, nil)
	if err != nil {
		t.Fatalf("building request: %v", err)
	}
	req.Header.Set("If-None-Match", etag)
	second, err := client.Do(req)
	if err != nil {
		t.Fatalf("conditional request: %v", err)
	}
	defer func() { _ = second.Body.Close() }()

	if second.StatusCode != http.StatusNotModified {
		t.Fatalf("conditional status = %d, want 304", second.StatusCode)
	}
	body, err := io.ReadAll(second.Body)
	if err != nil {
		t.Fatalf("reading 304 body: %v", err)
	}
	if len(body) != 0 {
		t.Errorf("304 carried %d bytes of body", len(body))
	}
}

// TestFaceCropRoute_twoFacesOfOnePhotoDiffer proves the crop is keyed by the box,
// so one photograph yields a different picture per face rather than one shared
// rendition — the property the whole route depends on.
func TestFaceCropRoute_twoFacesOfOnePhotoDiffer(t *testing.T) {
	env := newEnv(t)
	client, _ := env.login(t, "editor", auth.RoleEditor)
	seeded := env.seedPhoto(t, photos.Photo{Title: "Two", TakenAtSource: "unknown"}, "two.jpg", 77, 88, 99)
	base := env.server.URL + "/api/v1/photos/" + seeded.UID + "/face"

	// The seeded JPEG ramps left to right, so two boxes on opposite sides differ.
	left := mustDo(t, client, http.MethodGet, base+"?box=0.05,0.3,0.2,0.2", nil)
	defer func() { _ = left.Body.Close() }()
	leftTag := left.Header.Get("ETag")
	leftBody, err := io.ReadAll(left.Body)
	if err != nil {
		t.Fatalf("reading left crop: %v", err)
	}

	right := mustDo(t, client, http.MethodGet, base+"?box=0.75,0.3,0.2,0.2", nil)
	defer func() { _ = right.Body.Close() }()
	rightTag := right.Header.Get("ETag")
	rightBody, err := io.ReadAll(right.Body)
	if err != nil {
		t.Fatalf("reading right crop: %v", err)
	}

	if leftTag == rightTag {
		t.Errorf("both faces share the ETag %s, so one crop would be cached for the other", leftTag)
	}
	if bytes.Equal(leftBody, rightBody) {
		t.Error("both faces rendered to identical bytes, so the box is not being cropped to")
	}
}

// TestFaceCropRoute_guardMatchesTheOtherPhotoImagery proves a face is reachable
// exactly as a thumbnail is: with the session cookie, with a cookie-less download
// token, and not at all without either.
func TestFaceCropRoute_guardMatchesTheOtherPhotoImagery(t *testing.T) {
	env := newEnv(t)
	client, token := env.login(t, "viewer", auth.RoleViewer)
	seeded := env.seedPhoto(t, photos.Photo{Title: "Guard", TakenAtSource: "unknown"}, "guard.jpg", 12, 34, 56)
	url := env.server.URL + "/api/v1/photos/" + seeded.UID + "/face" + faceBoxQuery

	// A viewer may see the photograph, so a viewer may see a face cut out of it.
	withCookie := mustDo(t, client, http.MethodGet, url, nil)
	defer func() { _ = withCookie.Body.Close() }()
	if withCookie.StatusCode != http.StatusOK {
		t.Errorf("cookie-authenticated status = %d, want 200", withCookie.StatusCode)
	}

	// An <img> that never sends the cookie still works, exactly as for thumbnails.
	anonymous := &http.Client{}
	withToken := mustDo(t, anonymous, http.MethodGet, url+"&t="+token, nil)
	defer func() { _ = withToken.Body.Close() }()
	if withToken.StatusCode != http.StatusOK {
		t.Errorf("download-token status = %d, want 200", withToken.StatusCode)
	}

	// And a stranger gets nothing: a face must not be reachable to someone who may
	// not see the photograph it came from.
	unauthenticated := mustDo(t, anonymous, http.MethodGet, url, nil)
	defer func() { _ = unauthenticated.Body.Close() }()
	if unauthenticated.StatusCode != http.StatusUnauthorized {
		t.Errorf("unauthenticated status = %d, want 401", unauthenticated.StatusCode)
	}
}

// TestFaceCropRoute_rejectsAnUnusableRequest proves the two ways of asking for
// nothing are told apart: a box that names no pixels is the caller's mistake, an
// unknown photo is simply not there.
func TestFaceCropRoute_rejectsAnUnusableRequest(t *testing.T) {
	env := newEnv(t)
	client, _ := env.login(t, "editor", auth.RoleEditor)
	seeded := env.seedPhoto(t, photos.Photo{Title: "Bad", TakenAtSource: "unknown"}, "bad.jpg", 21, 43, 65)

	tests := []struct {
		name       string
		path       string
		wantStatus int
	}{
		{
			name:       "malformed box",
			path:       "/api/v1/photos/" + seeded.UID + "/face?box=nonsense",
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "box of no size",
			path:       "/api/v1/photos/" + seeded.UID + "/face?box=0.2,0.2,0,0.2",
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "unknown photo",
			path:       "/api/v1/photos/ph_missing/face" + faceBoxQuery,
			wantStatus: http.StatusNotFound,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			resp := mustDo(t, client, http.MethodGet, env.server.URL+tc.path, nil)
			defer func() { _ = resp.Body.Close() }()
			if resp.StatusCode != tc.wantStatus {
				t.Errorf("status = %d, want %d", resp.StatusCode, tc.wantStatus)
			}
		})
	}
}
