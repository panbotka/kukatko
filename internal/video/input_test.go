package video

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// TestInputArgs verifies the input guard: the demuxer allowlist always, and a
// protocol allowlist that is the local file alone for a path and the HTTP(S)
// stack for a URL, however the scheme is cased.
func TestInputArgs(t *testing.T) {
	t.Parallel()
	tests := []struct {
		src  string
		want string
	}{
		{"/var/lib/kukatko/originals/2024/05/clip.mp4", "file"},
		{"/tmp/kukatko-ingest-123", "file"},
		{"relative/clip.mov", "file"},
		{"https://bucket.example/2024/05/clip.mp4?X-Amz-Signature=abc", "http,https,tls,tcp"},
		{"HTTPS://bucket.example/clip.mp4", "http,https,tls,tcp"},
		{"http://127.0.0.1:18100/kukatko/clip.mp4", "http,https,tls,tcp"},
	}
	for _, tt := range tests {
		got := InputArgs(tt.src)
		want := []string{"-format_whitelist", DemuxerAllowlist, "-protocol_whitelist", tt.want}
		if !slices.Equal(got, want) {
			t.Errorf("InputArgs(%q) = %v, want %v", tt.src, got, want)
		}
	}
}

// TestDemuxerAllowlist_leavesOutManifests verifies none of libavformat's
// manifest demuxers — the ones that open further URLs named inside the input —
// is on the allowlist.
func TestDemuxerAllowlist_leavesOutManifests(t *testing.T) {
	t.Parallel()
	allowed := strings.Split(DemuxerAllowlist, ",")
	for _, manifest := range []string{"dash", "hls", "applehttp", "concat", "webm_dash_manifest", "image2", "tee"} {
		if slices.Contains(allowed, manifest) {
			t.Errorf("DemuxerAllowlist contains the manifest demuxer %q", manifest)
		}
	}
}

// countingServer starts a loopback HTTP listener that counts every request it
// receives and answers 404.
func countingServer(t *testing.T) (*httptest.Server, *atomic.Int64) {
	t.Helper()
	var hits atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		w.WriteHeader(http.StatusNotFound)
	}))
	t.Cleanup(srv.Close)
	return srv, &hits
}

// manifestFixtures returns a minimal static DASH MPD and an HLS media playlist
// that both name a URL on base, the shapes an attacker would upload as clip.mp4.
func manifestFixtures(base string) map[string]string {
	return map[string]string{
		"dash": `<?xml version="1.0" encoding="UTF-8"?>
<MPD xmlns="urn:mpeg:dash:schema:mpd:2011" type="static" mediaPresentationDuration="PT10S"
     minBufferTime="PT1S" profiles="urn:mpeg:dash:profile:isoff-on-demand:2011">
  <BaseURL>` + base + `/dash/</BaseURL>
  <Period>
    <AdaptationSet mimeType="video/mp4">
      <Representation id="1" bandwidth="1000" codecs="avc1.42E01E" width="320" height="240">
        <BaseURL>v.mp4</BaseURL>
      </Representation>
    </AdaptationSet>
  </Period>
</MPD>
`,
		"hls": "#EXTM3U\n#EXT-X-TARGETDURATION:10\n#EXTINF:10,\n" + base + "/hls/seg.ts\n#EXT-X-ENDLIST\n",
	}
}

// runTool runs bin with args under a short deadline and ignores how it exits:
// refusing the input is the expected outcome, the test only watches the network.
func runTool(t *testing.T, bin string, args []string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	// #nosec G204 -- bin is a constant and args are this test's own fixture paths.
	_ = exec.CommandContext(ctx, bin, args...).Run()
}

// TestManifestUpload_fetchesNothing is the regression test for SEC-017: a DASH
// manifest or an HLS playlist uploaded as a video, staged extensionless as the
// ingest pipeline stages it (and again under the name clip.mp4), must not make
// ffprobe or ffmpeg contact the URL it names — through the admission probe, the
// poster and its candidate samples, or the transcode. Runs only when ffprobe and
// ffmpeg are installed.
func TestManifestUpload_fetchesNothing(t *testing.T) {
	t.Parallel()
	if !FFprobeAvailable() || !FFmpegAvailable() {
		t.Skip("ffprobe/ffmpeg not installed; nothing to probe the manifests with")
	}
	for _, name := range []string{"dash", "hls"} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			srv, hits := countingServer(t)
			body := manifestFixtures(srv.URL)[name]
			dir := t.TempDir()
			staged := filepath.Join(dir, "kukatko-ingest-123")
			named := filepath.Join(dir, "clip.mp4")
			for _, path := range []string{staged, named} {
				if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
					t.Fatalf("writing fixture: %v", err)
				}
			}
			out := filepath.Join(dir, "out.jpg")

			runs := map[string]struct {
				bin  string
				args []string
			}{
				"ffprobe staged":     {ffprobeBinary, ffprobeArgs(staged)},
				"ffprobe clip.mp4":   {ffprobeBinary, ffprobeArgs(named)},
				"poster clip.mp4":    {ffmpegBinary, posterArgs(named, out, 1)},
				"sample clip.mp4":    {ffmpegBinary, sampleArgs(named, out, 1)},
				"transcode staged":   {ffmpegBinary, TranscodeArgs(staged)},
				"poster staged":      {ffmpegBinary, posterArgs(staged, out, 1)},
				"transcode clip.mp4": {ffmpegBinary, TranscodeArgs(named)},
			}
			for label, run := range runs {
				runTool(t, run.bin, run.args)
				if n := hits.Load(); n != 0 {
					t.Fatalf("%s: the listener received %d request(s); the manifest was followed", label, n)
				}
			}
			meta, err := Probe(t.Context(), staged)
			if err == nil && meta.HasContainerMetadata() {
				t.Errorf("Probe(%s manifest) = %+v, want no container metadata", name, meta)
			}
			if n := hits.Load(); n != 0 {
				t.Errorf("Probe: the listener received %d request(s)", n)
			}
		})
	}
}

// TestManifestUpload_unguardedControl documents what the guard prevents on the
// installed build: the same ffprobe argv without InputArgs. Whether it fetches
// depends on how ffmpeg was built (dash needs libxml2; hls refuses a playlist
// without an .m3u8 name since FFmpeg 6), so it only logs.
func TestManifestUpload_unguardedControl(t *testing.T) {
	t.Parallel()
	if !FFprobeAvailable() {
		t.Skip("ffprobe not installed")
	}
	srv, hits := countingServer(t)
	for name, body := range manifestFixtures(srv.URL) {
		path := filepath.Join(t.TempDir(), "kukatko-ingest-123")
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatalf("writing fixture: %v", err)
		}
		before := hits.Load()
		runTool(t, ffprobeBinary, []string{
			"-v", "error", "-print_format", "json", "-show_format", "-show_streams", "--", path,
		})
		t.Logf("unguarded ffprobe on the %s fixture: %d request(s) to the listener", name, hits.Load()-before)
	}
}

// TestInputArgs_remoteClipStillOpens verifies the guard keeps the remote half
// working: a real clip served over HTTP — the shape of a signed object-store URL —
// is still probed and transcoded. Runs only when ffmpeg and ffprobe are installed.
func TestInputArgs_remoteClipStillOpens(t *testing.T) {
	t.Parallel()
	src := makeSampleVideo(t)
	if !FFprobeAvailable() {
		t.Skip("ffprobe not installed")
	}
	srv := httptest.NewServer(http.FileServer(http.Dir(filepath.Dir(src))))
	t.Cleanup(srv.Close)
	url := srv.URL + "/" + filepath.Base(src)

	meta, err := probeWithFFprobe(t.Context(), url)
	if err != nil {
		t.Fatalf("probeWithFFprobe(remote clip): %v", err)
	}
	if !meta.HasContainerMetadata() || meta.VideoCodec == "" {
		t.Errorf("remote clip probed as %+v, want its container metadata", meta)
	}

	stream, err := Transcode(t.Context(), url)
	if err != nil {
		t.Fatalf("Transcode(remote clip): %v", err)
	}
	defer func() { _ = stream.Close() }()
	n, err := io.Copy(io.Discard, stream)
	if err != nil {
		t.Fatalf("reading the transcode: %v", err)
	}
	if n == 0 {
		t.Error("the remote clip transcoded to nothing")
	}
}
