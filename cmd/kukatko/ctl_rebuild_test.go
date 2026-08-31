package main

import (
	"net/http"
	"strings"
	"testing"

	"github.com/panbotka/kukatko/internal/ctl"
)

// TestCtlPhotosRebuild verifies each rebuild subcommand posts to its own endpoint
// and reports what came back — the face count, the regenerated sizes — rather than
// a bare acknowledgement.
func TestCtlPhotosRebuild(t *testing.T) {
	tests := []struct {
		name     string
		body     string
		wantPath string
		want     []string
	}{
		{
			name:     ctl.RebuildThumbnail,
			body:     `{"status":"regenerated","sizes":["fit_1920","tile_500"]}`,
			wantPath: "/api/v1/photos/pht01/regenerate-thumbnail",
			want:     []string{"thumbnail", "2 sizes", "fit_1920"},
		},
		{
			name:     ctl.RebuildEmbedding,
			body:     `{"step":"image_embed","status":"rebuilt"}`,
			wantPath: "/api/v1/photos/pht01/reembed",
			want:     []string{"image_embed", "rebuilt"},
		},
		{
			name:     ctl.RebuildFaces,
			body:     `{"step":"face_detect","status":"rebuilt","faces":3}`,
			wantPath: "/api/v1/photos/pht01/redetect-faces",
			want:     []string{"face_detect", "3 faces"},
		},
		{
			name:     ctl.RebuildPlace,
			body:     `{"step":"places","status":"rebuilt"}`,
			wantPath: "/api/v1/photos/pht01/regeocode",
			want:     []string{"places", "rebuilt"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var gotMethod, gotPath string
			configPath := ctlServer(t, func(w http.ResponseWriter, r *http.Request) {
				gotMethod, gotPath = r.Method, r.URL.Path
				w.Write([]byte(tt.body))
			})

			out, err := runCtl(t, "", "ctl", "--ctl-config", configPath,
				"photos", "rebuild", tt.name, "pht01")
			if err != nil {
				t.Fatalf("photos rebuild %s: %v (%s)", tt.name, err, out)
			}
			if gotMethod != http.MethodPost || gotPath != tt.wantPath {
				t.Errorf("request = %s %s, want POST %s", gotMethod, gotPath, tt.wantPath)
			}
			for _, want := range tt.want {
				if !strings.Contains(out, want) {
					t.Errorf("output %q does not mention %q", out, want)
				}
			}
		})
	}
}

// TestCtlPhotosRebuild_offlineIsNotAFailure verifies a queued rebuild is reported
// as the wait it is, with a zero exit status: the box being asleep is not an
// error, and a command that failed there would send the operator looking for a
// problem that is not theirs.
func TestCtlPhotosRebuild_offlineIsNotAFailure(t *testing.T) {
	configPath := ctlServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte(`{"step":"image_embed","status":"queued"}`))
	})

	out, err := runCtl(t, "", "ctl", "--ctl-config", configPath,
		"photos", "rebuild", ctl.RebuildEmbedding, "pht01")
	if err != nil {
		t.Fatalf("photos rebuild embedding: %v (%s)", err, out)
	}
	if !strings.Contains(out, "queued") {
		t.Errorf("output %q does not say the rebuild is queued", out)
	}
}

// TestCtlPhotosRebuild_json passes the server's own bytes through, so an agent
// reads the response rather than the rendering of it.
func TestCtlPhotosRebuild_json(t *testing.T) {
	configPath := ctlServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte(`{"step":"face_detect","status":"rebuilt","faces":3}`))
	})

	out, err := runCtl(t, "", "ctl", "--ctl-config", configPath, "-o", "json",
		"photos", "rebuild", ctl.RebuildFaces, "pht01")
	if err != nil {
		t.Fatalf("photos rebuild faces -o json: %v (%s)", err, out)
	}
	if !strings.Contains(out, `"faces": 3`) && !strings.Contains(out, `"faces":3`) {
		t.Errorf("json output %q does not carry the face count", out)
	}
}
