package sweepapi

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/panbotka/kukatko/internal/sweep"
)

// fakeService drives Sweep from an injected function.
type fakeService struct {
	run func(emit func(sweep.Event) error) error
}

// Sweep delegates to the fake's run function.
func (f *fakeService) Sweep(_ context.Context, _ sweep.Params, emit func(sweep.Event) error) error {
	return f.run(emit)
}

// newRouter mounts the API under /api/v1, mirroring the real server.
func newRouter(api *API) http.Handler {
	r := chi.NewRouter()
	r.Route("/api/v1", api.RegisterRoutes)
	return r
}

// doGet issues a GET and returns the recorder.
func doGet(t *testing.T, h http.Handler, target string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequestWithContext(context.Background(), http.MethodGet, target, nil))
	return rec
}

// decodeStream parses an NDJSON body into events.
func decodeStream(t *testing.T, body string) []sweep.Event {
	t.Helper()
	var events []sweep.Event
	for line := range strings.SplitSeq(strings.TrimSpace(body), "\n") {
		if line == "" {
			continue
		}
		var ev sweep.Event
		if err := json.Unmarshal([]byte(line), &ev); err != nil {
			t.Fatalf("unmarshalling %q: %v", line, err)
		}
		events = append(events, ev)
	}
	return events
}

// TestParseConfidence covers the percent-or-distance parsing and its errors.
func TestParseConfidence(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		raw     string
		want    float64
		wantErr bool
	}{
		{"empty defaults to 75 percent", "", 0.25, false},
		{"percentage maps to complement", "75", 0.25, false},
		{"tight percentage", "95", 0.05, false},
		{"near-100 percent floored", "100", minDistance, false},
		{"raw distance passes through", "0.4", 0.4, false},
		{"one is a distance", "1", 1, false},
		{"negative rejected", "-5", 0, true},
		{"too large rejected", "150", 0, true},
		{"non-numeric rejected", "high", 0, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := parseConfidence(tt.raw)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("parseConfidence(%q) err = nil, want error", tt.raw)
				}
				return
			}
			if err != nil {
				t.Fatalf("parseConfidence(%q) err = %v", tt.raw, err)
			}
			if math.Abs(got-tt.want) > 1e-9 {
				t.Errorf("parseConfidence(%q) = %v, want %v", tt.raw, got, tt.want)
			}
		})
	}
}

// TestParseLimit covers the per-person limit parsing.
func TestParseLimit(t *testing.T) {
	t.Parallel()
	tests := []struct {
		raw     string
		want    int
		wantErr bool
	}{
		{"", 0, false},
		{"20", 20, false},
		{"0", 0, false},
		{"-1", 0, true},
		{"lots", 0, true},
	}
	for _, tt := range tests {
		t.Run(tt.raw, func(t *testing.T) {
			t.Parallel()
			got, err := parseLimit(tt.raw)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("parseLimit(%q) err = nil, want error", tt.raw)
				}
				return
			}
			if err != nil || got != tt.want {
				t.Errorf("parseLimit(%q) = %d, %v, want %d", tt.raw, got, err, tt.want)
			}
		})
	}
}

// TestHandleSweep_streamsNDJSON checks the endpoint streams progress, person and
// summary lines as newline-delimited JSON with the right content type.
func TestHandleSweep_streamsNDJSON(t *testing.T) {
	t.Parallel()
	svc := &fakeService{run: func(emit func(sweep.Event) error) error {
		if err := emit(sweep.Event{Type: sweep.EventProgress, Progress: &sweep.Progress{Scanned: 1, Total: 1, Name: "Alice"}}); err != nil {
			return err
		}
		if err := emit(sweep.Event{Type: sweep.EventPerson, Person: &sweep.Person{Actionable: 2}}); err != nil {
			return err
		}
		return emit(sweep.Event{Type: sweep.EventSummary, Summary: &sweep.Summary{PeopleScanned: 1, PeopleWithMatches: 1, TotalActionable: 2}})
	}}
	h := newRouter(NewAPI(Config{Service: svc}))

	rec := doGet(t, h, "/api/v1/faces/sweep?confidence=75&limit=10")

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/x-ndjson" {
		t.Errorf("content-type = %q, want application/x-ndjson", ct)
	}
	events := decodeStream(t, rec.Body.String())
	if len(events) != 3 {
		t.Fatalf("events = %d, want 3: %s", len(events), rec.Body.String())
	}
	if events[0].Type != sweep.EventProgress || events[2].Type != sweep.EventSummary {
		t.Errorf("event order = %v/%v, want progress...summary", events[0].Type, events[2].Type)
	}
	if events[2].Summary == nil || events[2].Summary.TotalActionable != 2 {
		t.Errorf("summary = %+v, want TotalActionable 2", events[2].Summary)
	}
}

// TestHandleSweep_serviceUnavailable checks a nil backend answers 503.
func TestHandleSweep_serviceUnavailable(t *testing.T) {
	t.Parallel()
	h := newRouter(NewAPI(Config{Service: nil}))
	if rec := doGet(t, h, "/api/v1/faces/sweep"); rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", rec.Code)
	}
}

// TestHandleSweep_badConfidence checks an unparsable confidence answers 400.
func TestHandleSweep_badConfidence(t *testing.T) {
	t.Parallel()
	svc := &fakeService{run: func(func(sweep.Event) error) error { return nil }}
	h := newRouter(NewAPI(Config{Service: svc}))
	if rec := doGet(t, h, "/api/v1/faces/sweep?confidence=-3"); rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

// TestHandleSweep_preStreamErrorIs500 checks a failure before the first line becomes a
// clean 500, not a truncated stream.
func TestHandleSweep_preStreamErrorIs500(t *testing.T) {
	t.Parallel()
	svc := &fakeService{run: func(func(sweep.Event) error) error {
		return errors.New("listing subjects: db down")
	}}
	h := newRouter(NewAPI(Config{Service: svc}))

	rec := doGet(t, h, "/api/v1/faces/sweep")
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		t.Errorf("content-type = %q, want json error body", ct)
	}
}

// TestHandleSweep_writeGuardApplied checks the injected RequireWrite middleware guards
// the endpoint.
func TestHandleSweep_writeGuardApplied(t *testing.T) {
	t.Parallel()
	svc := &fakeService{run: func(func(sweep.Event) error) error { return nil }}
	guard := func(http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusForbidden)
		})
	}
	h := newRouter(NewAPI(Config{Service: svc, RequireWrite: guard}))
	if rec := doGet(t, h, "/api/v1/faces/sweep"); rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403 (guard applied)", rec.Code)
	}
}
