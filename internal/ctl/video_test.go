package ctl

import (
	"bytes"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"
)

// TestClient_videoRequests verifies each video call reaches its own endpoint with
// its own method, so a command cannot quietly schedule the wrong work.
func TestClient_videoRequests(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		call       func(*Client) error
		wantMethod string
		wantPath   string
		wantQuery  string
	}{
		{
			name:       "prepare a video",
			call:       func(c *Client) error { _, err := c.PrepareVideo(t.Context(), "pht01"); return err },
			wantMethod: http.MethodPost,
			wantPath:   "/api/v1/photos/pht01/process/hls_transcode",
		},
		{
			name:       "rebuild the scrub preview",
			call:       func(c *Client) error { _, err := c.RebuildStoryboard(t.Context(), "pht01"); return err },
			wantMethod: http.MethodPost,
			wantPath:   "/api/v1/photos/pht01/regenerate-storyboard",
		},
		{
			name:       "list the renditions",
			call:       func(c *Client) error { _, err := c.VideoRenditions(t.Context(), "pht01"); return err },
			wantMethod: http.MethodGet,
			wantPath:   "/api/v1/photos/pht01/renditions",
		},
		{
			name:       "backfill the missing encodes",
			call:       func(c *Client) error { _, err := c.BackfillHLS(t.Context(), false); return err },
			wantMethod: http.MethodPost,
			wantPath:   "/api/v1/process/hls",
		},
		{
			name:       "backfill every encode",
			call:       func(c *Client) error { _, err := c.BackfillHLS(t.Context(), true); return err },
			wantMethod: http.MethodPost,
			wantPath:   "/api/v1/process/hls",
			wantQuery:  "all=true",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var gotMethod, gotPath, gotQuery string
			client := testClient(t, "kkt_a_b", func(w http.ResponseWriter, r *http.Request) {
				gotMethod, gotPath, gotQuery = r.Method, r.URL.Path, r.URL.RawQuery
				w.Write([]byte(`{}`))
			})

			if err := tt.call(client); err != nil {
				t.Fatalf("call returned %v", err)
			}
			if gotMethod != tt.wantMethod || gotPath != tt.wantPath {
				t.Errorf("request = %s %s, want %s %s", gotMethod, gotPath, tt.wantMethod, tt.wantPath)
			}
			if gotQuery != tt.wantQuery {
				t.Errorf("query = %q, want %q", gotQuery, tt.wantQuery)
			}
		})
	}
}

// TestClient_videoRejectsBlankUID verifies a blank uid is refused locally, before
// a request is spent on it — the same guard every other per-photo call carries.
func TestClient_videoRejectsBlankUID(t *testing.T) {
	t.Parallel()

	client := testClient(t, "kkt_a_b", func(w http.ResponseWriter, _ *http.Request) {
		t.Error("a blank uid reached the server")
		w.Write([]byte(`{}`))
	})

	calls := map[string]func() error{
		"prepare":    func() error { _, err := client.PrepareVideo(t.Context(), " "); return err },
		"storyboard": func() error { _, err := client.RebuildStoryboard(t.Context(), " "); return err },
		"renditions": func() error { _, err := client.VideoRenditions(t.Context(), " "); return err },
	}
	for name, call := range calls {
		if err := call(); !errors.Is(err, ErrEmptyUID) {
			t.Errorf("%s with a blank uid = %v, want ErrEmptyUID", name, err)
		}
	}
}

// TestDecodePhotoStep_stampsTheStep verifies the step name is filled in for the
// endpoint that does not report one — the forced storyboard rebuild — and left
// alone for the one that does.
func TestDecodePhotoStep_stampsTheStep(t *testing.T) {
	t.Parallel()

	step, err := DecodePhotoStep([]byte(`{"state":"pending"}`), RebuildStoryboard)
	if err != nil {
		t.Fatalf("DecodePhotoStep: %v", err)
	}
	if step.Step != RebuildStoryboard {
		t.Errorf("step = %q, want the fallback %q", step.Step, RebuildStoryboard)
	}

	step, err = DecodePhotoStep([]byte(`{"step":"hls_transcode","state":"queued"}`), RebuildVideo)
	if err != nil {
		t.Fatalf("DecodePhotoStep: %v", err)
	}
	if step.Step != "hls_transcode" {
		t.Errorf("step = %q, want the server's own name", step.Step)
	}
}

// TestWritePhotoStep_saysWhatHappensNext verifies each state is rendered as what
// the caller should expect rather than as the bare state word: a queued encode is
// waiting for the worker, a done one already landed, a skipped one never will.
func TestWritePhotoStep_saysWhatHappensNext(t *testing.T) {
	t.Parallel()

	at := time.Date(2026, 9, 1, 8, 30, 0, 0, time.UTC)
	tests := []struct {
		name string
		step PhotoStep
		want []string
	}{
		{
			name: "queued",
			step: PhotoStep{Step: "hls_transcode", State: "queued"},
			want: []string{"hls_transcode", "queued", "background"},
		},
		{
			name: "running",
			step: PhotoStep{Step: "hls_transcode", State: "running"},
			want: []string{"already running"},
		},
		{
			// The trap: evidence wins over the queue in the report, so an
			// already-encoded clip answers done — with the old stamp — while the
			// encode this command just asked for sits in the queue behind it.
			name: "done names the previous run, not this one",
			step: PhotoStep{Step: "hls_transcode", State: "done", At: &at},
			want: []string{"previous run", "2026-09-01 08:30", "queued behind it"},
		},
		{
			name: "failed carries why",
			step: PhotoStep{Step: "hls_transcode", State: "failed", Error: "ffmpeg exited 1"},
			want: []string{"failed", "ffmpeg exited 1", "retry"},
		},
		{
			name: "pending says nothing is scheduled",
			step: PhotoStep{Step: "storyboard", State: "pending"},
			want: []string{"pending", "nothing is scheduled"},
		},
		{
			name: "skipped explains itself",
			step: PhotoStep{Step: "hls_transcode", State: "skipped"},
			want: []string{"skipped", "does not apply"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var buf bytes.Buffer
			if err := WritePhotoStep(&buf, tt.step); err != nil {
				t.Fatalf("WritePhotoStep: %v", err)
			}
			for _, want := range tt.want {
				if !strings.Contains(buf.String(), want) {
					t.Errorf("output %q does not mention %q", buf.String(), want)
				}
			}
		})
	}
}

// TestWriteVideoRenditions_table verifies the table carries every number an
// operator came for and that the footer names the oldest encode, which is what
// answers "is what is stored still current?".
func TestWriteVideoRenditions_table(t *testing.T) {
	t.Parallel()

	old := time.Date(2026, 1, 2, 3, 4, 0, 0, time.UTC)
	recent := time.Date(2026, 8, 9, 10, 11, 0, 0, time.UTC)
	var buf bytes.Buffer
	err := WriteVideoRenditions(&buf, []VideoRendition{
		{
			Rendition: "1080p", Width: 1920, Height: 1080, Bandwidth: 5_128_000,
			Codecs: "avc1.640028", SegmentCount: 7, DurationMs: 125_000, EncodedAt: recent,
		},
		{
			Rendition: "720p", Width: 1280, Height: 720, Bandwidth: 2_500_000,
			Codecs: "avc1.64001f", SegmentCount: 7, DurationMs: 125_000, EncodedAt: old,
		},
	})
	if err != nil {
		t.Fatalf("WriteVideoRenditions: %v", err)
	}
	out := buf.String()
	for _, want := range []string{
		"RENDITION", "1080p", "1920×1080", "5.1 Mbit/s", "avc1.640028", "2:05",
		"720p", "2 renditions", "oldest encoded 2026-01-02 03:04",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output does not mention %q:\n%s", want, out)
		}
	}
}

// TestWriteVideoRenditions_empty verifies an unencoded clip prints a sentence
// rather than a headed table with no rows, which would read like a failure.
func TestWriteVideoRenditions_empty(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	if err := WriteVideoRenditions(&buf, nil); err != nil {
		t.Fatalf("WriteVideoRenditions: %v", err)
	}
	if strings.Contains(buf.String(), "RENDITION") {
		t.Errorf("an empty inventory printed a table header:\n%s", buf.String())
	}
	if !strings.Contains(buf.String(), "never been encoded") {
		t.Errorf("output %q does not say the clip was never encoded", buf.String())
	}
}

// TestWriteBackfill verifies the count is reported as scheduled work, and that a
// zero reads as "nothing left to do" rather than as a failure.
func TestWriteBackfill(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		in   Backfill
		want string
	}{
		{name: "nothing to do", in: Backfill{}, want: "nothing to do"},
		{name: "one", in: Backfill{Enqueued: 1}, want: "1 video was scheduled"},
		{name: "many", in: Backfill{Enqueued: 12}, want: "12 videos were scheduled"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var buf bytes.Buffer
			if err := WriteBackfill(&buf, "video", tt.in); err != nil {
				t.Fatalf("WriteBackfill: %v", err)
			}
			if !strings.Contains(buf.String(), tt.want) {
				t.Errorf("output %q does not mention %q", buf.String(), tt.want)
			}
		})
	}
}

// TestFormatBitrateAndDuration covers the two small renderers the rendition table
// depends on, including the values that mean "not recorded".
func TestFormatBitrateAndDuration(t *testing.T) {
	t.Parallel()

	if got := formatBitrate(0); got != "-" {
		t.Errorf("formatBitrate(0) = %q, want a dash", got)
	}
	if got := formatBitrate(2_500_000); got != "2.5 Mbit/s" {
		t.Errorf("formatBitrate(2500000) = %q", got)
	}
	if got := formatDuration(0); got != "-" {
		t.Errorf("formatDuration(0) = %q, want a dash", got)
	}
	if got := formatDuration(9_000); got != "0:09" {
		t.Errorf("formatDuration(9000) = %q, want 0:09", got)
	}
	if got := formatDuration(3_725_000); got != "62:05" {
		t.Errorf("formatDuration(3725000) = %q, want 62:05", got)
	}
}
