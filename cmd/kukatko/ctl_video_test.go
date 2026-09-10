package main

import (
	"net/http"
	"strings"
	"testing"
)

// renditionsBody is a two-quality inventory exactly as internal/photoapi shapes
// it, oldest encode last so the footer has something to pick out.
const renditionsBody = `{"renditions":[` +
	`{"rendition":"1080p","width":1920,"height":1080,"bandwidth":5128000,` +
	`"codecs":"avc1.640028,mp4a.40.2","segment_count":7,"duration_ms":125000,` +
	`"encoded_at":"2026-08-09T10:11:00Z"},` +
	`{"rendition":"720p","width":1280,"height":720,"bandwidth":2500000,` +
	`"codecs":"avc1.64001f,mp4a.40.2","segment_count":7,"duration_ms":125000,` +
	`"encoded_at":"2026-01-02T03:04:00Z"}]}`

// TestCtlPhotosRebuildVideo verifies preparing a video for playback posts to the
// processing endpoint for the streaming encode and reports the state it came back
// with, rather than a bare acknowledgement: the encode is queued, and the operator
// has to know that is what happened.
func TestCtlPhotosRebuildVideo(t *testing.T) {
	var gotMethod, gotPath string
	configPath := ctlServer(t, func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath = r.Method, r.URL.Path
		w.Write([]byte(`{"step":"hls_transcode","state":"queued"}`))
	})

	out, err := runCtl(t, "", "ctl", "--ctl-config", configPath, "photos", "rebuild", "video", "pht01")
	if err != nil {
		t.Fatalf("photos rebuild video: %v (%s)", err, out)
	}
	if gotMethod != http.MethodPost || gotPath != "/api/v1/photos/pht01/process/hls_transcode" {
		t.Errorf("request = %s %s, want POST the hls_transcode step", gotMethod, gotPath)
	}
	for _, want := range []string{"hls_transcode", "queued", "background"} {
		if !strings.Contains(out, want) {
			t.Errorf("output %q does not mention %q", out, want)
		}
	}
}

// TestCtlPhotosRebuildVideo_alreadyDone pins the one answer that is easy to
// misread. The processing report resolves persisted evidence before the queue, so
// a clip that is already encoded comes back `done` with the old stamp even though
// this call really did schedule a fresh encode — the line has to say both, or an
// operator reads it as "nothing happened".
func TestCtlPhotosRebuildVideo_alreadyDone(t *testing.T) {
	configPath := ctlServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte(`{"step":"hls_transcode","state":"done","at":"2026-09-01T08:30:00Z"}`))
	})

	out, err := runCtl(t, "", "ctl", "--ctl-config", configPath, "photos", "rebuild", "video", "pht01")
	if err != nil {
		t.Fatalf("photos rebuild video: %v (%s)", err, out)
	}
	if !strings.Contains(out, "2026-09-01 08:30") || !strings.Contains(out, "queued behind it") {
		t.Errorf("output %q does not say the recorded encode is the previous one: %s", out, out)
	}
}

// TestCtlPhotosRebuildVideo_json passes the server's own bytes through, so an
// agent reads the response rather than the rendering of it.
func TestCtlPhotosRebuildVideo_json(t *testing.T) {
	configPath := ctlServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte(`{"step":"hls_transcode","state":"queued"}`))
	})

	out, err := runCtl(t, "", "ctl", "--ctl-config", configPath, "-o", "json",
		"photos", "rebuild", "video", "pht01")
	if err != nil {
		t.Fatalf("photos rebuild video -o json: %v (%s)", err, out)
	}
	if !strings.Contains(out, `"hls_transcode"`) || !strings.Contains(out, `"queued"`) {
		t.Errorf("json output %q is not the server's own body", out)
	}
}

// TestCtlPhotosRebuildStoryboard verifies the forced scrub-preview rebuild posts
// to its own endpoint and stamps the step onto a body that does not name one.
func TestCtlPhotosRebuildStoryboard(t *testing.T) {
	var gotMethod, gotPath string
	configPath := ctlServer(t, func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath = r.Method, r.URL.Path
		w.Write([]byte(`{"state":"queued"}`))
	})

	out, err := runCtl(t, "", "ctl", "--ctl-config", configPath,
		"photos", "rebuild", "storyboard", "pht01")
	if err != nil {
		t.Fatalf("photos rebuild storyboard: %v (%s)", err, out)
	}
	if gotMethod != http.MethodPost || gotPath != "/api/v1/photos/pht01/regenerate-storyboard" {
		t.Errorf("request = %s %s, want POST the storyboard rebuild", gotMethod, gotPath)
	}
	if !strings.Contains(out, "storyboard") || !strings.Contains(out, "queued") {
		t.Errorf("output %q does not name the step and its state", out)
	}
}

// TestCtlPhotosRebuildStoryboard_llm verifies the agent format is available here
// like everywhere else, so a compact answer can be read without a table parser.
func TestCtlPhotosRebuildStoryboard_llm(t *testing.T) {
	configPath := ctlServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte(`{"state":"queued"}`))
	})

	out, err := runCtl(t, "", "ctl", "--ctl-config", configPath, "-o", "llm",
		"photos", "rebuild", "storyboard", "pht01")
	if err != nil {
		t.Fatalf("photos rebuild storyboard -o llm: %v (%s)", err, out)
	}
	if !strings.Contains(out, `"state"`) {
		t.Errorf("llm output %q does not carry the state", out)
	}
}

// TestCtlPhotosRenditions verifies the inventory is fetched from its own endpoint
// and rendered as the table an operator reads: every quality with its picture,
// bitrate, segments and encode stamp, plus the oldest encode in the footer.
func TestCtlPhotosRenditions(t *testing.T) {
	var gotMethod, gotPath string
	configPath := ctlServer(t, func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath = r.Method, r.URL.Path
		w.Write([]byte(renditionsBody))
	})

	out, err := runCtl(t, "", "ctl", "--ctl-config", configPath, "photos", "renditions", "pht01")
	if err != nil {
		t.Fatalf("photos renditions: %v (%s)", err, out)
	}
	if gotMethod != http.MethodGet || gotPath != "/api/v1/photos/pht01/renditions" {
		t.Errorf("request = %s %s, want GET the rendition inventory", gotMethod, gotPath)
	}
	for _, want := range []string{
		"RENDITION", "1080p", "1920×1080", "5.1 Mbit/s", "2:05", "720p",
		"2 renditions", "oldest encoded 2026-01-02 03:04",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output does not mention %q:\n%s", want, out)
		}
	}
}

// TestCtlPhotosRenditions_empty verifies a clip with nothing encoded prints a
// sentence rather than an empty table, and still exits zero: "never encoded" is a
// normal state of a video, not a failure.
func TestCtlPhotosRenditions_empty(t *testing.T) {
	configPath := ctlServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte(`{"renditions":[]}`))
	})

	out, err := runCtl(t, "", "ctl", "--ctl-config", configPath, "photos", "renditions", "pht01")
	if err != nil {
		t.Fatalf("photos renditions: %v (%s)", err, out)
	}
	if !strings.Contains(out, "never been encoded") {
		t.Errorf("output %q does not say the clip was never encoded", out)
	}
}

// TestCtlPhotosRenditions_outputForms verifies the two machine formats: json is
// the server's own bytes, llm is the compact agent shape.
func TestCtlPhotosRenditions_outputForms(t *testing.T) {
	tests := []struct {
		format string
		want   string
	}{
		{format: "json", want: `"segment_count"`},
		{format: "llm", want: `"rendition"`},
	}

	for _, tt := range tests {
		t.Run(tt.format, func(t *testing.T) {
			configPath := ctlServer(t, func(w http.ResponseWriter, _ *http.Request) {
				w.Write([]byte(renditionsBody))
			})
			out, err := runCtl(t, "", "ctl", "--ctl-config", configPath, "-o", tt.format,
				"photos", "renditions", "pht01")
			if err != nil {
				t.Fatalf("photos renditions -o %s: %v (%s)", tt.format, err, out)
			}
			if !strings.Contains(out, tt.want) {
				t.Errorf("%s output %q does not carry %q", tt.format, out, tt.want)
			}
		})
	}
}

// TestCtlProcessHLS verifies the plain backfill posts with no query at all — the
// repair over videos that have never been encoded — and reports the scheduled
// count as work now in the queue.
func TestCtlProcessHLS(t *testing.T) {
	var gotMethod, gotPath, gotQuery string
	configPath := ctlServer(t, func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath, gotQuery = r.Method, r.URL.Path, r.URL.RawQuery
		w.Write([]byte(`{"enqueued":12}`))
	})

	out, err := runCtl(t, "", "ctl", "--ctl-config", configPath, "process", "hls")
	if err != nil {
		t.Fatalf("process hls: %v (%s)", err, out)
	}
	if gotMethod != http.MethodPost || gotPath != "/api/v1/process/hls" || gotQuery != "" {
		t.Errorf("request = %s %s?%s, want POST /api/v1/process/hls with no query",
			gotMethod, gotPath, gotQuery)
	}
	if !strings.Contains(out, "12 videos were scheduled") {
		t.Errorf("output %q does not report the scheduled count", out)
	}
}

// TestCtlProcessHLS_allNeedsConfirmation verifies the full re-encode is gated: it
// occupies the worker for hours, so a bare --all refuses and changes nothing,
// while --all --yes goes through and carries the flag to the server.
func TestCtlProcessHLS_allNeedsConfirmation(t *testing.T) {
	var calls int
	var gotQuery string
	configPath := ctlServer(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		gotQuery = r.URL.RawQuery
		w.Write([]byte(`{"enqueued":40}`))
	})

	out, err := runCtl(t, "", "ctl", "--ctl-config", configPath, "process", "hls", "--all")
	if err == nil {
		t.Fatalf("process hls --all without --yes succeeded: %s", out)
	}
	if calls != 0 {
		t.Errorf("a refused re-encode still reached the server %d times", calls)
	}

	out, err = runCtl(t, "", "ctl", "--ctl-config", configPath, "process", "hls", "--all", "--yes")
	if err != nil {
		t.Fatalf("process hls --all --yes: %v (%s)", err, out)
	}
	if gotQuery != "all=true" {
		t.Errorf("query = %q, want all=true", gotQuery)
	}
	if !strings.Contains(out, "40 videos were scheduled") {
		t.Errorf("output %q does not report the scheduled count", out)
	}
}

// TestCtlProcessHLS_nothingToDo verifies a library with every video encoded reads
// as done rather than as a failure, and exits zero.
func TestCtlProcessHLS_nothingToDo(t *testing.T) {
	configPath := ctlServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte(`{"enqueued":0}`))
	})

	out, err := runCtl(t, "", "ctl", "--ctl-config", configPath, "process", "hls")
	if err != nil {
		t.Fatalf("process hls: %v (%s)", err, out)
	}
	if !strings.Contains(out, "nothing to do") {
		t.Errorf("output %q does not say there was nothing to schedule", out)
	}
}

// TestCtlProcessHLS_json passes the server's own bytes through.
func TestCtlProcessHLS_json(t *testing.T) {
	configPath := ctlServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte(`{"enqueued":3}`))
	})

	out, err := runCtl(t, "", "ctl", "--ctl-config", configPath, "-o", "json", "process", "hls")
	if err != nil {
		t.Fatalf("process hls -o json: %v (%s)", err, out)
	}
	if !strings.Contains(out, `"enqueued"`) {
		t.Errorf("json output %q is not the server's own body", out)
	}
}
