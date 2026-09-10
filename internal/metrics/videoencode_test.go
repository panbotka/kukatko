package metrics

import (
	"strings"
	"testing"
	"time"
)

// TestObserveRenditionEncode_recordsPerRendition verifies the encode is measured
// at the grain the job cannot be measured at: one observation per rendition, so
// a cheap 480p pass and an expensive 1080p one of the same video are separate
// series rather than one average, and a failed encode is kept apart from a
// successful one.
func TestObserveRenditionEncode_recordsPerRendition(t *testing.T) {
	t.Parallel()

	r := New()
	r.ObserveRenditionEncode("1080p", OutcomeSuccess, 90*time.Second, 12_000_000)
	r.ObserveRenditionEncode("1080p", OutcomeSuccess, 60*time.Second, 8_000_000)
	r.ObserveRenditionEncode("480p", OutcomeError, 30*time.Second, 0)

	body := scrape(t, r)
	want := []string{
		`kukatko_video_encode_duration_seconds_count{outcome="success",rendition="1080p"} 2`,
		`kukatko_video_encode_duration_seconds_sum{outcome="success",rendition="1080p"} 150`,
		`kukatko_video_encode_duration_seconds_count{outcome="error",rendition="480p"} 1`,
		`kukatko_video_encode_output_bytes_total{outcome="success",rendition="1080p"} 2e+07`,
		`kukatko_video_encode_output_bytes_total{outcome="error",rendition="480p"} 0`,
	}
	for _, series := range want {
		if !strings.Contains(body, series) {
			t.Errorf("/metrics output missing %q\n--- got ---\n%s", series, body)
		}
	}
}

// TestObserveEncodedSource_countsFootage verifies the footage counter adds up
// the seconds of video that have been through the encoder, which is what makes
// the cost of a minute of footage a query rather than an exported ratio.
func TestObserveEncodedSource_countsFootage(t *testing.T) {
	t.Parallel()

	r := New()
	if body := scrape(t, r); !strings.Contains(body, "kukatko_video_encode_source_seconds_total 0") {
		t.Errorf("a fresh registry should expose a zero footage counter, got:\n%s", body)
	}
	r.ObserveEncodedSource(90 * time.Second)
	r.ObserveEncodedSource(30 * time.Second)
	if body := scrape(t, r); !strings.Contains(body, "kukatko_video_encode_source_seconds_total 120") {
		t.Errorf("expected 120 seconds of footage, got:\n%s", body)
	}
}
