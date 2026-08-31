package ctl

import (
	"bytes"
	"errors"
	"net/http"
	"strings"
	"testing"
)

// TestClient_RebuildPhoto verifies every rebuild posts to its own endpoint, so a
// name in the CLI cannot quietly reach the wrong one.
func TestClient_RebuildPhoto(t *testing.T) {
	t.Parallel()

	for _, spec := range RebuildSpecs {
		t.Run(spec.Name, func(t *testing.T) {
			t.Parallel()

			var gotMethod, gotPath string
			client := testClient(t, "kkt_a_b", func(w http.ResponseWriter, r *http.Request) {
				gotMethod, gotPath = r.Method, r.URL.Path
				w.Write([]byte(`{"status":"rebuilt"}`))
			})

			raw, err := client.RebuildPhoto(t.Context(), "pht01", spec.Name)
			if err != nil {
				t.Fatalf("RebuildPhoto(%q) returned %v", spec.Name, err)
			}
			if gotMethod != http.MethodPost || gotPath != "/api/v1/photos/pht01/"+spec.Path {
				t.Errorf("request = %s %s, want POST the %s endpoint", gotMethod, gotPath, spec.Path)
			}
			decoded, err := DecodePhotoRebuild(raw, spec.Name)
			if err != nil {
				t.Fatalf("DecodePhotoRebuild returned %v", err)
			}
			if decoded.Step != spec.Name {
				t.Errorf("step = %q, want %q stamped on a response that does not name one", decoded.Step, spec.Name)
			}
		})
	}
}

// TestClient_RebuildPhoto_rejectsBadInput verifies an unknown rebuild and a blank
// uid are both refused locally, before a request is spent on them.
func TestClient_RebuildPhoto_rejectsBadInput(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		uid     string
		rebuild string
		wantErr error
	}{
		{name: "unknown rebuild", uid: "pht01", rebuild: "colour", wantErr: ErrUnknownRebuild},
		{name: "blank uid", uid: "", rebuild: RebuildFaces, wantErr: ErrEmptyUID},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			called := false
			client := testClient(t, "kkt_a_b", func(w http.ResponseWriter, _ *http.Request) {
				called = true
				w.Write([]byte(`{"status":"rebuilt"}`))
			})
			if _, err := client.RebuildPhoto(t.Context(), tt.uid, tt.rebuild); !errors.Is(err, tt.wantErr) {
				t.Fatalf("RebuildPhoto = %v, want %v", err, tt.wantErr)
			}
			if called {
				t.Error("a request was sent for input that could be refused locally")
			}
		})
	}
}

// TestDecodePhotoRebuild_keepsTheServersStep verifies a response that names its
// own step is left alone: the three newer endpoints say what they rebuilt, and
// their word beats the CLI's name for it.
func TestDecodePhotoRebuild_keepsTheServersStep(t *testing.T) {
	t.Parallel()

	decoded, err := DecodePhotoRebuild([]byte(`{"step":"face_detect","status":"rebuilt","faces":2}`), RebuildFaces)
	if err != nil {
		t.Fatalf("DecodePhotoRebuild returned %v", err)
	}
	if decoded.Step != "face_detect" || decoded.Faces == nil || *decoded.Faces != 2 {
		t.Errorf("decoded = %+v, want the server's face_detect with 2 faces", decoded)
	}
}

// TestWritePhotoRebuild reports what the recomputation produced rather than a
// bare acknowledgement — the new answer is what the operator ran the rebuild for.
func TestWritePhotoRebuild(t *testing.T) {
	t.Parallel()

	two := 2
	tests := []struct {
		name    string
		rebuild PhotoRebuild
		want    []string
	}{
		{
			name:    "faces report the count",
			rebuild: PhotoRebuild{Step: "face_detect", Status: "rebuilt", Faces: &two},
			want:    []string{"face_detect", "rebuilt", "2 faces"},
		},
		{
			name:    "one face is singular",
			rebuild: PhotoRebuild{Step: "face_detect", Status: "rebuilt", Faces: new(1)},
			want:    []string{"1 face on the photo"},
		},
		{
			name:    "thumbnails list the sizes",
			rebuild: PhotoRebuild{Step: "thumbnail", Status: "regenerated", Sizes: []string{"fit_720", "tile_500"}},
			want:    []string{"2 sizes", "fit_720", "tile_500"},
		},
		{
			name:    "an offline service says so",
			rebuild: PhotoRebuild{Step: "image_embed", Status: RebuildStatusQueued},
			want:    []string{"offline", "queued"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var buf bytes.Buffer
			if err := WritePhotoRebuild(&buf, tt.rebuild); err != nil {
				t.Fatalf("WritePhotoRebuild returned %v", err)
			}
			for _, want := range tt.want {
				if !strings.Contains(buf.String(), want) {
					t.Errorf("output %q does not mention %q", buf.String(), want)
				}
			}
		})
	}
}

// TestRebuildSpecFor covers the lookup the client validates a name with.
func TestRebuildSpecFor(t *testing.T) {
	t.Parallel()

	if spec, ok := RebuildSpecFor(RebuildThumbnail); !ok || spec.Path != "regenerate-thumbnail" {
		t.Errorf("RebuildSpecFor(thumbnail) = %+v, %v; want the regenerate-thumbnail endpoint", spec, ok)
	}
	if _, ok := RebuildSpecFor("nope"); ok {
		t.Error("RebuildSpecFor accepted a rebuild that does not exist")
	}
}
