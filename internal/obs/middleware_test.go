package obs

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
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

// decodeLogLine parses the single JSON log record in b.
func decodeLogLine(t *testing.T, b []byte) map[string]any {
	t.Helper()
	var line map[string]any
	if err := json.Unmarshal(bytes.TrimSpace(b), &line); err != nil {
		t.Fatalf("decoding log line %q: %v", b, err)
	}
	return line
}
