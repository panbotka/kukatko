package metrics

import (
	"strings"
	"testing"
)

// TestInitJobTypes_exportsZeroSeries verifies every job type × outcome exists at
// zero before any job ran, so a dashboard on an idle instance reads "nothing
// happened" instead of "no data".
func TestInitJobTypes_exportsZeroSeries(t *testing.T) {
	t.Parallel()

	r := New()
	r.InitJobTypes([]string{"image_embed", "ocr"})
	body := scrape(t, r)
	jobTypes := []string{"image_embed", "ocr"}
	want := make([]string, 0, len(jobTypes)*(1+2*len(jobOutcomes)))
	for _, jobType := range jobTypes {
		want = append(want, `kukatko_jobs_started_total{type="`+jobType+`"} 0`)
		for _, outcome := range []string{OutcomeSuccess, OutcomeError, OutcomeDeferred, OutcomeTerminal} {
			want = append(want,
				`kukatko_jobs_finished_total{outcome="`+outcome+`",type="`+jobType+`"} 0`,
				`kukatko_jobs_execution_duration_seconds_count{outcome="`+outcome+`",type="`+jobType+`"} 0`,
			)
		}
	}
	for _, series := range want {
		if !strings.Contains(body, series) {
			t.Errorf("/metrics output missing %q", series)
		}
	}
}

// TestInitEmbeddingOperations_exportsZeroSeries verifies every operation ×
// outcome of the call histogram exists at zero, and that the reachability gauge
// is not invented: an unprobed target is unknown, not down.
func TestInitEmbeddingOperations_exportsZeroSeries(t *testing.T) {
	t.Parallel()

	r := New()
	r.InitEmbeddingOperations([]string{"image", "text"})
	body := scrape(t, r)
	for _, series := range []string{
		`kukatko_embedding_request_duration_seconds_count{operation="image",outcome="success"} 0`,
		`kukatko_embedding_request_duration_seconds_count{operation="image",outcome="error"} 0`,
		`kukatko_embedding_request_duration_seconds_count{operation="text",outcome="success"} 0`,
		`kukatko_embedding_request_duration_seconds_count{operation="text",outcome="error"} 0`,
	} {
		if !strings.Contains(body, series) {
			t.Errorf("/metrics output missing %q", series)
		}
	}
	if strings.Contains(body, "kukatko_embedding_service_up{") {
		t.Errorf("reachability must not be pre-set before a probe, got:\n%s", body)
	}
}

// TestInitVideoEncode_exportsZeroSeries verifies every configured rendition ×
// outcome exists at zero for both per-rendition families.
func TestInitVideoEncode_exportsZeroSeries(t *testing.T) {
	t.Parallel()

	r := New()
	r.InitVideoEncode([]string{"720p"})
	body := scrape(t, r)
	for _, series := range []string{
		`kukatko_video_encode_duration_seconds_count{outcome="success",rendition="720p"} 0`,
		`kukatko_video_encode_duration_seconds_count{outcome="error",rendition="720p"} 0`,
		`kukatko_video_encode_output_bytes_total{outcome="success",rendition="720p"} 0`,
		`kukatko_video_encode_output_bytes_total{outcome="error",rendition="720p"} 0`,
	} {
		if !strings.Contains(body, series) {
			t.Errorf("/metrics output missing %q", series)
		}
	}
}

// TestInit_isIdempotent verifies pre-initialising twice neither panics nor
// disturbs a count already recorded.
func TestInit_isIdempotent(t *testing.T) {
	t.Parallel()

	r := New()
	r.InitJobTypes([]string{"ocr"})
	r.JobStarted("ocr")
	r.InitJobTypes([]string{"ocr"})
	if body := scrape(t, r); !strings.Contains(body, `kukatko_jobs_started_total{type="ocr"} 1`) {
		t.Errorf("a second init must keep the recorded count, got:\n%s", body)
	}
}
