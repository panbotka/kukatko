package obs

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
)

// TestAccessLog_emitsStructuredLine verifies the access log emits one line per
// request carrying the request id, method, route pattern, status, and the user
// stamped by a downstream handler via SetUser.
func TestAccessLog_emitsStructuredLine(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	logger, err := NewLogger(&buf, "info")
	if err != nil {
		t.Fatalf("NewLogger: %v", err)
	}

	router := chi.NewRouter()
	router.Use(middleware.RequestID)
	router.Use(AccessLog(logger))
	router.Get("/photos/{uid}", func(w http.ResponseWriter, r *http.Request) {
		SetUser(r.Context(), "user-42")
		w.WriteHeader(http.StatusCreated)
	})

	rec := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/photos/ph1", nil)
	router.ServeHTTP(rec, req)

	line := decodeLogLine(t, buf.Bytes())
	if line["msg"] != "http request" {
		t.Errorf("msg = %v, want %q", line["msg"], "http request")
	}
	if line["route"] != "/photos/{uid}" {
		t.Errorf("route = %v, want %q", line["route"], "/photos/{uid}")
	}
	if line["method"] != http.MethodGet {
		t.Errorf("method = %v, want GET", line["method"])
	}
	if line["status"] != float64(http.StatusCreated) {
		t.Errorf("status = %v, want %d", line["status"], http.StatusCreated)
	}
	if line["user"] != "user-42" {
		t.Errorf("user = %v, want user-42", line["user"])
	}
	if rid, ok := line["request_id"].(string); !ok || rid == "" {
		t.Errorf("request_id = %v, want non-empty string", line["request_id"])
	}
}

// TestAccessLog_skipsMetrics verifies scrape traffic to /metrics is not logged.
func TestAccessLog_skipsMetrics(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	logger, err := NewLogger(&buf, "info")
	if err != nil {
		t.Fatalf("NewLogger: %v", err)
	}
	handler := AccessLog(logger)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	rec := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/metrics", nil)
	handler.ServeHTTP(rec, req)

	if buf.Len() != 0 {
		t.Errorf("expected no log output for /metrics, got: %s", buf.String())
	}
}

// TestAccessLog_project verifies the line names the project the request was
// admitted into by its slug, the last SetProject winning, and leaves the field
// out of a request admitted into none.
func TestAccessLog_project(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		stamp    func(ctx context.Context)
		wantSlug any
	}{
		{name: "no project", stamp: func(context.Context) {}, wantSlug: nil},
		{name: "one project", stamp: func(ctx context.Context) { SetProject(ctx, "pj1", "veselice") },
			wantSlug: "veselice"},
		{name: "the inner resolution wins", stamp: func(ctx context.Context) {
			SetProject(ctx, "pj0", "v")
			SetProject(ctx, "pj1", "veselice")
		}, wantSlug: "veselice"},
		{name: "an empty uid is ignored", stamp: func(ctx context.Context) {
			SetProject(ctx, "pj1", "veselice")
			SetProject(ctx, "", "other")
		}, wantSlug: "veselice"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var buf bytes.Buffer
			logger, err := NewLogger(&buf, "info")
			if err != nil {
				t.Fatalf("NewLogger: %v", err)
			}
			var uid string
			handler := AccessLog(logger)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				tt.stamp(r.Context())
				uid = ProjectUID(r.Context())
				w.WriteHeader(http.StatusOK)
			}))
			req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/p/x/photos", nil)
			handler.ServeHTTP(httptest.NewRecorder(), req)

			line := decodeLogLine(t, buf.Bytes())
			if line["project"] != tt.wantSlug {
				t.Errorf("project = %v, want %v", line["project"], tt.wantSlug)
			}
			if tt.wantSlug != nil && uid == "" {
				t.Error("ProjectUID is empty for a stamped request")
			}
		})
	}
}

// TestProjectUID_withoutBag verifies SetProject is inert outside AccessLog,
// while WithProject still answers through the context it returns.
func TestProjectUID_withoutBag(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	SetProject(ctx, "pj1", "veselice")
	if got := ProjectUID(ctx); got != "" {
		t.Errorf("ProjectUID after SetProject = %q, want empty", got)
	}
	if got := ProjectUID(WithProject(ctx, "pj1", "veselice")); got != "pj1" {
		t.Errorf("ProjectUID after WithProject = %q, want pj1", got)
	}
	if WithProject(ctx, "", "x") != ctx {
		t.Error("WithProject without a uid returned a new context")
	}
}

// decodeLogLine parses the single JSON log record in b.
func decodeLogLine(t *testing.T, b []byte) map[string]any {
	t.Helper()
	var line map[string]any
	if err := json.Unmarshal(bytes.TrimSpace(b), &line); err != nil {
		t.Fatalf("decoding log line %q: %v", b, err)
	}
	return line
}

// TestLogPath_redactsAResetToken pins that a one-time password-reset link never
// reaches the access log: the endpoint stays readable, the credential does not.
func TestLogPath_redactsAResetToken(t *testing.T) {
	const token = "3PYuXZ0m7Qk1nB2vLd4sTgHjKfWc8RaE"
	got := logPath("/api/v1/auth/password-reset/" + token)
	if strings.Contains(got, token) {
		t.Errorf("logPath kept the token: %q", got)
	}
	if want := "/api/v1/auth/password-reset/" + redactedSegment; got != want {
		t.Errorf("logPath = %q, want %q", got, want)
	}
	if plain := "/api/v1/photos/ph123"; logPath(plain) != plain {
		t.Errorf("logPath rewrote an ordinary path: %q", logPath(plain))
	}
}
