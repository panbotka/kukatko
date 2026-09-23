package metrics

import (
	"fmt"
	"maps"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/panbotka/kukatko/internal/version"
)

// upperBounds returns the finite bucket boundaries of the histogram family
// called name, ascending, read from a scrape. Every series of a family shares
// one bucket layout, so the distinct `le` values are that layout.
func upperBounds(t *testing.T, r *Registry, name string) []float64 {
	t.Helper()
	seen := map[float64]bool{}
	for line := range strings.Lines(scrape(t, r)) {
		if !strings.HasPrefix(line, name+"_bucket{") {
			continue
		}
		_, rest, _ := strings.Cut(line, `le="`)
		le, _, _ := strings.Cut(rest, `"`)
		if le == "+Inf" {
			continue
		}
		bound, err := strconv.ParseFloat(le, 64)
		if err != nil {
			t.Fatalf("bucket bound %q: %v", le, err)
		}
		seen[bound] = true
	}
	if len(seen) == 0 {
		t.Fatalf("family %q exports no buckets", name)
	}
	bounds := slices.Collect(maps.Keys(seen))
	slices.Sort(bounds)
	return bounds
}

// TestHistograms_bucketsReachTheWorkTheyMeasure verifies the long-running
// families have finite buckets well into the minutes, so a p95 of an embed job
// or a RAW thumbnail is an answer and not "+Inf", while HTTP keeps the default
// resolution and only gains a short tail for streaming routes.
func TestHistograms_bucketsReachTheWorkTheyMeasure(t *testing.T) {
	t.Parallel()

	r := New()
	exerciseAll(r)
	tests := []struct {
		name      string
		family    string
		wantAbove float64
		wantBelow float64
	}{
		{name: "job duration", family: "kukatko_jobs_execution_duration_seconds", wantAbove: 600, wantBelow: 0.05},
		{name: "embedding call", family: "kukatko_embedding_request_duration_seconds", wantAbove: 600, wantBelow: 0.05},
		{name: "thumbnail", family: "kukatko_thumbnail_generation_duration_seconds", wantAbove: 600, wantBelow: 0.05},
		{name: "http", family: "kukatko_http_request_duration_seconds", wantAbove: 300, wantBelow: 0.01},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			bounds := upperBounds(t, r, tt.family)
			if top := bounds[len(bounds)-1]; top < tt.wantAbove {
				t.Errorf("top finite bucket = %v s, want at least %v s", top, tt.wantAbove)
			}
			if bounds[0] > tt.wantBelow {
				t.Errorf("lowest bucket = %v s, want at most %v s", bounds[0], tt.wantBelow)
			}
		})
	}
}

// TestHTTPBuckets_keepTheDefaults verifies the HTTP histogram is the default
// set plus a tail, so the resolution where nearly every request lands is kept
// and existing dashboards' bucket boundaries still exist.
func TestHTTPBuckets_keepTheDefaults(t *testing.T) {
	t.Parallel()

	r := New()
	serveOnce(r, http.MethodGet, "/probe")
	bounds := upperBounds(t, r, "kukatko_http_request_duration_seconds")
	defaults := []float64{0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10}
	if len(bounds) <= len(defaults) {
		t.Fatalf("buckets = %v, want the defaults plus a tail", bounds)
	}
	for i, want := range defaults {
		if bounds[i] != want {
			t.Errorf("bucket %d = %v, want the default %v", i, bounds[i], want)
		}
	}
}

// TestBuildInfo_namesTheRunningBinary verifies kukatko_build_info is exported
// from start with the version and commit the binary was linked with.
func TestBuildInfo_namesTheRunningBinary(t *testing.T) {
	t.Parallel()

	build := version.Get()
	want := fmt.Sprintf(`kukatko_build_info{commit=%q,version=%q} 1`, build.Commit, build.Version)
	if body := scrape(t, New()); !strings.Contains(body, want) {
		t.Errorf("/metrics output missing %q\n--- got ---\n%s", want, body)
	}
}

// TestStaleLocksRecovered_counts verifies the stale-lock counter exists at zero
// from start and adds up the recovered jobs, ignoring an empty scan.
func TestStaleLocksRecovered_counts(t *testing.T) {
	t.Parallel()

	r := New()
	if body := scrape(t, r); !strings.Contains(body, "kukatko_jobs_stale_locks_recovered_total 0") {
		t.Errorf("a fresh registry should expose a zero stale-lock counter, got:\n%s", body)
	}
	r.StaleLocksRecovered(2)
	r.StaleLocksRecovered(0)
	r.StaleLocksRecovered(3)
	if body := scrape(t, r); !strings.Contains(body, "kukatko_jobs_stale_locks_recovered_total 5") {
		t.Errorf("expected 5 recovered stale locks, got:\n%s", body)
	}
}

// TestSetEmbeddingUp_perTarget verifies the reachability gauge keeps the box and
// the text host apart, so the text instance answering does not hide a sleeping
// box and vice versa.
func TestSetEmbeddingUp_perTarget(t *testing.T) {
	t.Parallel()

	r := New()
	r.SetEmbeddingUp("box", false)
	r.SetEmbeddingUp("text", true)
	body := scrape(t, r)
	for _, want := range []string{
		`kukatko_embedding_service_up{target="box"} 0`,
		`kukatko_embedding_service_up{target="text"} 1`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("/metrics output missing %q\n--- got ---\n%s", want, body)
		}
	}

	r.SetEmbeddingUp("box", true)
	if body := scrape(t, r); !strings.Contains(body, `kukatko_embedding_service_up{target="box"} 1`) {
		t.Errorf("the box gauge should follow the latest probe, got:\n%s", body)
	}
}

// TestJobFinished_terminalOutcome verifies a permanently failed job is counted
// under its own outcome, apart from a failure that will be retried.
func TestJobFinished_terminalOutcome(t *testing.T) {
	t.Parallel()

	r := New()
	r.JobFinished("ocr", OutcomeTerminal, time.Second)
	r.JobFinished("ocr", OutcomeError, time.Second)
	body := scrape(t, r)
	for _, want := range []string{
		`kukatko_jobs_finished_total{outcome="terminal",type="ocr"} 1`,
		`kukatko_jobs_finished_total{outcome="error",type="ocr"} 1`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("/metrics output missing %q\n--- got ---\n%s", want, body)
		}
	}
}
